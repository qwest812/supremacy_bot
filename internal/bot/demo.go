package bot

// Демо: записанная партия, прогнанная через три сценария, чтобы посмотреть
// интерфейс и показать бота, не заходя в игру. Сценарии собраны из
// настоящего состояния (x4, второй день, Швеция) и меняют в нём только то,
// что нужно для показа: соседа-бота и вражеские армии.

import (
	"path/filepath"
	"time"

	"supermessy_bot/internal/s1914"
)

// WriteDemo пишет журналы и снимки трёх демо-заходов в logDir/demo.
// load каждый раз даёт свежую копию записанной партии.
func WriteDemo(load func() (*s1914.GameState, *s1914.MapGeometry, error), logDir string, r Rules) error {
	dir := filepath.Join(logDir, "demo")
	mem := &Memory{SeenTrades: map[int64]bool{}, Refused: map[string]time.Time{}, Notes: map[string]bool{}}

	scenarios := []func(w *World){
		// 1. Как записано: стройка, рынок, право прохода.
		func(w *World) {},
		// 2. Третий день, у границы провинция бота — война и захват.
		func(w *World) {
			w.G.Day = 3
			demoBorder(w)
		},
		// 3. Война идёт: враг вошёл в нашу провинцию и стоит у границы.
		func(w *World) {
			w.G.Day = 3
			bot, home, lost := demoBorder(w)
			if bot == 0 {
				return
			}
			w.G.Relations[bot] = relWar
			inf := w.Unit(uInfantry)
			w.G.Armies = append(w.G.Armies,
				s1914.Army{ID: 990001, Owner: bot, Location: home, Size: 3, Pos: w.Geo.Centers[home],
					Units: []s1914.ArmyUnit{{Type: inf, Size: 3}}},
				s1914.Army{ID: 990002, Owner: bot, Location: lost, Size: 40, Pos: w.Geo.Centers[lost],
					Units: []s1914.ArmyUnit{{Type: inf, Size: 40}}})
		},
	}

	for i, scenario := range scenarios {
		g, geo, err := load()
		if err != nil {
			return err
		}
		now := g.ServerTime.Add(time.Duration(i) * 20 * time.Minute)
		w := NewWorld(g, geo, now)
		scenario(w)
		w = NewWorld(w.G, w.Geo, now)

		j, err := OpenJournal(dir, true)
		if err != nil {
			return err
		}
		j.SetPrevNotes(mem.Notes)
		j.Visit(w, mem)
		j.WriteMap(w)
		plan := Decide(w, r, mem)
		rec := &visitLog{}
		done := map[Category]int{}
		for _, d := range plan.Decisions {
			out := ""
			if !d.IsNote() {
				out = "план"
				done[d.Category]++
			}
			j.Decision(w, d, "")
			rec.add(d, out)
		}
		j.Snapshot(w, mem, rec.decisions, rec.outcomes)
		j.End(Summary(plan, done), plan.Next, plan.NextWhy)
		mem.Visits++
		mem.Notes = j.Notes()
		j.Close()
	}
	return nil
}

// demoBorder отдаёт ближайшему боту нашу провинцию у провинции с армией.
func demoBorder(w *World) (bot, home, lost int) {
	for id, p := range w.G.Players {
		if p.NativeAI && !p.Defeated && (bot == 0 || id < bot) {
			bot = id
		}
	}
	armyAt := map[int]bool{}
	for _, a := range w.MyArmies() {
		if w.landArmy(a) {
			armyAt[a.Location] = true
		}
	}
	for _, h := range w.Mine() {
		if !armyAt[h.ID] {
			continue
		}
		for _, nb := range w.Geo.LandNeighbors(h.ID) {
			p, ok := w.Province(nb)
			if !ok || p.Owner != w.G.Me || armyAt[nb] || p.Capital {
				continue
			}
			for i := range w.G.Provinces {
				if w.G.Provinces[i].ID == nb {
					w.G.Provinces[i].Owner = bot
				}
			}
			return bot, h.ID, nb
		}
	}
	return 0, 0, 0
}
