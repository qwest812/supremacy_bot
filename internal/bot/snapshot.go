package bot

// Снимки для карты в веб-интерфейсе. На каждый заход —
// snapshots/<номер>.json: кто чем владеет, где чьи армии и какие приказы
// бот отдал (с путями). Рядом один раз map.json — очертания провинций:
// файл карты не меняется всю партию.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"supermessy_bot/internal/s1914"
)

// Snapshot — партия на миг захода и решения этого захода.
type Snapshot struct {
	Visit     int                `json:"visit"`
	Time      string             `json:"time"`
	Day       int                `json:"day"`
	Me        int                `json:"me"`
	Dry       bool               `json:"dry"`
	Players   map[int]SnapPlayer `json:"players"`
	Provinces []SnapProvince     `json:"provinces"`
	Armies    []SnapArmy         `json:"armies"`
	Decisions []SnapDecision     `json:"decisions"`
}

type SnapPlayer struct {
	Nation  string `json:"nation"`
	Color   string `json:"color"`
	Bot     bool   `json:"bot"`
	Human   bool   `json:"human"`
	Hostile bool   `json:"hostile"`
	Target  bool   `json:"target"`
	Points  int    `json:"points,omitempty"`
	Gone    bool   `json:"gone,omitempty"`
}

type SnapProvince struct {
	ID      int     `json:"id"`
	Owner   int     `json:"owner"`
	Morale  float64 `json:"morale,omitempty"`
	Double  bool    `json:"double,omitempty"`
	Capital bool    `json:"capital,omitempty"`
	Danger  bool    `json:"danger,omitempty"`
	Value   float64 `json:"value"`
	// Buildings и Building — здания и идущие стройки, только у наших.
	Buildings []string `json:"buildings,omitempty"`
	Building  []string `json:"building,omitempty"`
	Resource  string   `json:"resource,omitempty"`
}

type SnapArmy struct {
	ID       int64       `json:"id"`
	Owner    int         `json:"owner"`
	X        float64     `json:"x"`
	Y        float64     `json:"y"`
	Location int         `json:"location"`
	Power    float64     `json:"power"`
	Units    string      `json:"units"`
	Fighting bool        `json:"fighting,omitempty"`
	To       *[2]float64 `json:"to,omitempty"`
}

type SnapDecision struct {
	Category Category     `json:"category"`
	Rule     string       `json:"rule"`
	What     string       `json:"what"`
	Why      string       `json:"why"`
	Note     bool         `json:"note"`
	Outcome  string       `json:"outcome"`
	Province int          `json:"province,omitempty"`
	Army     int64        `json:"army,omitempty"`
	Enemy    int64        `json:"enemy,omitempty"`
	Path     [][2]float64 `json:"path,omitempty"`
}

func points(path []s1914.MapPoint) [][2]float64 {
	out := make([][2]float64, 0, len(path))
	for _, p := range path {
		out = append(out, [2]float64{p.X, p.Y})
	}
	return out
}

// Snapshot пишет снимок захода. w — партия на миг входа; decisions
// и outcomes — все решения захода (всех кругов) и чем они кончились.
func (j *Journal) Snapshot(w *World, mem *Memory, decisions []Decision, outcomes []string) {
	g := w.G
	snap := Snapshot{Visit: j.visit, Time: w.Now.Format(time.RFC3339), Day: g.Day, Me: g.Me, Dry: j.dry,
		Players: map[int]SnapPlayer{}}
	for id, p := range g.Players {
		snap.Players[id] = SnapPlayer{Nation: p.Nation, Color: p.Color, Bot: p.NativeAI, Human: !p.IsAI,
			Hostile: w.Hostile(id), Target: id == mem.WarTarget, Points: g.Race.Points[id],
			Gone: p.Defeated || p.Retired}
	}

	danger := map[int]bool{}
	for _, d := range decisions {
		if d.Category == CatDefense && d.Province != 0 {
			danger[d.Province] = true
		}
	}
	for _, p := range g.Provinces {
		sp := SnapProvince{ID: p.ID, Owner: p.Owner, Morale: p.Morale, Double: w.IsDouble(p),
			Capital: p.Capital, Danger: danger[p.ID], Value: w.Value(p)}
		if res := p.MainResource(); res >= 0 {
			sp.Resource = w.ResName(res)
		}
		if p.Owner == g.Me {
			for _, b := range p.Built {
				if u, ok := g.Upgrades[b]; ok {
					name := u.Name
					if !g.Finished(p, b) {
						name += " (недострой)"
					} else if on, known := p.Enabled[b]; known && !on {
						name += " (выкл.)"
					}
					sp.Buildings = append(sp.Buildings, name)
				}
			}
			for _, c := range p.Building {
				sp.Building = append(sp.Building, g.Upgrades[c.UpgradeID].Name+" до "+clock(c.Ends, w.Now))
			}
		}
		snap.Provinces = append(snap.Provinces, sp)
	}

	for _, a := range g.Armies {
		if a.Owner <= 0 || len(a.Units) == 0 {
			continue
		}
		sa := SnapArmy{ID: a.ID, Owner: a.Owner, X: a.Pos.X, Y: a.Pos.Y, Location: a.Location,
			Power: w.Power(a), Units: unitsText(w, a), Fighting: a.Fighting}
		if a.MarchTo != nil {
			sa.To = &[2]float64{a.MarchTo.X, a.MarchTo.Y}
		}
		snap.Armies = append(snap.Armies, sa)
	}

	for i, d := range decisions {
		out := ""
		if i < len(outcomes) {
			out = outcomes[i]
		}
		snap.Decisions = append(snap.Decisions, SnapDecision{Category: d.Category, Rule: d.Rule, What: d.What,
			Why: d.Why, Note: d.IsNote(), Outcome: out, Province: d.Province, Army: d.Army, Enemy: d.Enemy,
			Path: points(d.Path)})
	}

	dir := filepath.Join(j.dir, "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	if raw, err := json.Marshal(snap); err == nil {
		_ = os.WriteFile(filepath.Join(dir, itoa(j.visit)+".json"), raw, 0o644)
	}
}

// unitsText — состав армии словами: «Пехота 12, Бронеавтомобиль 2».
func unitsText(w *World, a s1914.Army) string {
	counts := map[string]int{}
	for _, u := range a.Units {
		name := w.G.UnitStats[u.Type].Name
		if name == "" {
			name = "юнит " + itoa(u.Type)
		}
		counts[name] += u.Size
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return counts[names[i]] > counts[names[j]] })
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n + " " + itoa(counts[n])
	}
	return out
}

// MapFile — очертания карты для интерфейса.
type MapFile struct {
	ID        string        `json:"id"`
	Width     int           `json:"width"`
	Height    int           `json:"height"`
	Provinces []MapProvince `json:"provinces"`
}

type MapProvince struct {
	ID     int        `json:"id"`
	Name   string     `json:"name"`
	Points []int      `json:"points"` // x0,y0,x1,y1,…
	Center [2]float64 `json:"center"`
}

// WriteMap кладёт map.json, если его ещё нет или карта другая.
func (j *Journal) WriteMap(w *World) {
	if w.Geo == nil {
		return
	}
	path := filepath.Join(j.dir, "map.json")
	if raw, err := os.ReadFile(path); err == nil {
		var head struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &head) == nil && head.ID == w.Geo.ID {
			return
		}
	}
	m := MapFile{ID: w.Geo.ID, Width: w.Geo.Width, Height: w.Geo.Height}
	ids := make([]int, 0, len(w.Geo.Land))
	for id := range w.Geo.Land {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		mp := MapProvince{ID: id}
		if p, ok := w.Province(id); ok {
			mp.Name = p.Name
		}
		for _, pt := range w.Geo.Land[id] {
			mp.Points = append(mp.Points, pt.X, pt.Y)
		}
		if c, ok := w.Geo.Centers[id]; ok {
			mp.Center = [2]float64{c.X, c.Y}
		}
		m.Provinces = append(m.Provinces, mp)
	}
	if raw, err := json.Marshal(m); err == nil {
		_ = os.WriteFile(path, raw, 0o644)
	}
}
