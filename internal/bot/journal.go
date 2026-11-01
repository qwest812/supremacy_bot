package bot

// Журналы партии. Каждое решение пишется дважды: в общий журнал (all.log),
// где видна вся логика захода по порядку, и в журнал своей категории —
// стройка, войска, атаки, рынок, дипломатия. Рядом decisions.jsonl — то же
// построчно в JSON, его читает веб-интерфейс (internal/ui). Строки трёх
// видов (поле type): visit — шапка захода, decision — решение, end — итог.
//
// Заметки («не делаем, потому что…») пишутся тоже, но одинаковая заметка
// два захода подряд — только раз: иначе «ждём 3-го дня» заполнит журнал.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Journal — журналы одной партии.
type Journal struct {
	dir   string
	dry   bool
	files map[string]*os.File
	notes map[string]bool // заметки этого захода
	// prevNotes — заметки прошлого захода: повторять их незачем.
	prevNotes map[string]bool
	// visit — номер захода: им решения в decisions.jsonl привязаны к шапке.
	visit int
}

// VisitRecord — шапка захода в decisions.jsonl.
type VisitRecord struct {
	Type      string             `json:"type"`
	Time      string             `json:"time"`
	Visit     int                `json:"visit"`
	Day       int                `json:"day"`
	Dry       bool               `json:"dry"`
	Nation    string             `json:"nation"`
	Provinces int                `json:"provinces"`
	Doubles   int                `json:"doubles"`
	Armies    int                `json:"armies"`
	Power     float64            `json:"power"`
	Speed     float64            `json:"speed"`
	Resources []ResourceRecord   `json:"resources"`
	Wars      []string           `json:"wars,omitempty"`
	Target    string             `json:"target,omitempty"`
	Points    int                `json:"points,omitempty"`
	Extra     map[string]float64 `json:"extra,omitempty"`
}

// ResourceRecord — запас ресурса на миг захода.
type ResourceRecord struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Daily  float64 `json:"daily"`
}

func (j *Journal) record(v any) {
	if raw, err := json.Marshal(v); err == nil {
		j.write("decisions.jsonl", string(raw))
	}
}

// OpenJournal открывает (или заводит) журналы партии в dir. dry — бот
// только смотрит: решения пишутся как план, в игру ничего не уходит.
func OpenJournal(dir string, dry bool) (*Journal, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Journal{dir: dir, dry: dry, files: map[string]*os.File{}, notes: map[string]bool{}}, nil
}

func (j *Journal) file(name string) *os.File {
	if f, ok := j.files[name]; ok {
		return f
	}
	f, err := os.OpenFile(filepath.Join(j.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	j.files[name] = f
	return f
}

func (j *Journal) write(name, line string) {
	if f := j.file(name); f != nil {
		fmt.Fprintln(f, line)
	}
}

// Close закрывает файлы.
func (j *Journal) Close() {
	for _, f := range j.files {
		f.Close()
	}
}

func stamp(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

// Visit — шапка захода в общем журнале: день, страна, запасы.
func (j *Journal) Visit(w *World, mem *Memory) {
	g := w.G
	mode := "играем"
	if j.dry {
		mode = "только смотрим (DRY_RUN), в игру ничего не отправляем"
	}
	me := g.Players[g.Me]
	var res []string
	for _, id := range []int{20, 0, 1, 2, 3, 4, 5, 6} {
		r, ok := g.Resources[id]
		if !ok {
			continue
		}
		res = append(res, fmt.Sprintf("%s %s (%+.0f/сут)", w.ResName(id), num(w.Amount(id)), r.Daily))
	}
	doubles := 0
	for _, p := range w.Mine() {
		if w.IsDouble(p) {
			doubles++
		}
	}
	lines := []string{
		"",
		fmt.Sprintf("===== %s | заход №%d | день %d | %s", stamp(w.Now), mem.Visits+1, g.Day, mode),
		fmt.Sprintf("страна: %s; провинций %d, из них двойных %d; армий %d; скорость x%.0f",
			me.Nation, len(w.Mine()), doubles, len(w.MyArmies()), g.Speed),
		"запасы: " + strings.Join(res, "; "),
	}
	var wars []string
	for id, p := range g.Players {
		if w.Hostile(id) {
			wars = append(wars, p.Nation)
		}
	}
	sort.Strings(wars)
	if len(wars) > 0 {
		line := "воюем с: " + strings.Join(wars, ", ")
		if t, ok := g.Players[mem.WarTarget]; ok {
			line += "; цель — " + t.Nation
		}
		lines = append(lines, line)
	}
	for _, l := range lines {
		j.write("all.log", l)
	}

	j.visit = mem.Visits + 1
	rec := VisitRecord{Type: "visit", Time: w.Now.Format(time.RFC3339), Visit: j.visit, Day: g.Day,
		Dry: j.dry, Nation: me.Nation, Provinces: len(w.Mine()), Doubles: doubles,
		Armies: len(w.MyArmies()), Speed: g.Speed, Wars: wars, Points: g.Race.Points[g.Me]}
	for _, a := range w.MyArmies() {
		rec.Power += w.Power(a)
	}
	if t, ok := g.Players[mem.WarTarget]; ok {
		rec.Target = t.Nation
	}
	for _, id := range []int{20, 0, 1, 2, 3, 4, 5, 6} {
		if r, ok := g.Resources[id]; ok {
			rec.Resources = append(rec.Resources, ResourceRecord{ID: id, Name: w.ResName(id),
				Amount: w.Amount(id), Daily: r.Daily})
		}
	}
	j.record(rec)
}

// End — итог захода: сколько сделано и когда следующий.
func (j *Journal) End(summary string, next time.Time, why string) {
	j.Line("итог захода: %s; следующий заход %s — %s", summary, next.Format("15:04:05"), why)
	j.record(map[string]any{"type": "end", "time": time.Now().Format(time.RFC3339), "visit": j.visit,
		"summary": summary, "next": next.Format(time.RFC3339), "why": why})
}

// Decision пишет решение. outcome — чем кончилось: пусто у заметок,
// «сделано», «план» или текст отказа игры.
func (j *Journal) Decision(w *World, d Decision, outcome string) {
	if d.IsNote() {
		key := string(d.Category) + "|" + d.Rule + "|" + d.What + "|" + d.Why
		j.notes[key] = true
		if j.prevNotes[key] {
			return
		}
	}
	mark := "заметка"
	switch {
	case d.IsNote():
	case outcome == "":
		mark = "ПЛАН"
	default:
		mark = strings.ToUpper(outcome)
	}
	day := 0
	if w != nil {
		day = w.G.Day
	}
	line := fmt.Sprintf("%s | день %d | %-10s | %-8s | [%s] %s — %s",
		stamp(time.Now()), day, d.Category.Title(), mark, d.Rule, d.What, d.Why)
	j.write("all.log", line)
	j.write(string(d.Category)+".log", line)

	rec := map[string]any{
		"type": "decision", "visit": j.visit,
		"time": time.Now().Format(time.RFC3339), "day": day, "category": d.Category,
		"rule": d.Rule, "what": d.What, "why": d.Why, "note": d.IsNote(), "outcome": outcome,
	}
	if len(d.Cost) > 0 {
		rec["cost"] = d.Cost
	}
	if d.Province != 0 {
		rec["province"] = d.Province
	}
	if d.Army != 0 {
		rec["army"] = d.Army
	}
	j.record(rec)
}

// Line — свободная строка в общий журнал.
func (j *Journal) Line(format string, args ...any) {
	j.write("all.log", fmt.Sprintf("%s | %s", stamp(time.Now()), fmt.Sprintf(format, args...)))
}

// Notes — заметки этого захода: их кладут в память, чтобы в следующий
// раз не повторять.
func (j *Journal) Notes() map[string]bool { return j.notes }

// SetPrevNotes — заметки прошлого захода.
func (j *Journal) SetPrevNotes(prev map[string]bool) { j.prevNotes = prev }

// Summary — короткий итог захода по категориям для общего журнала.
func Summary(plan *Plan, done map[Category]int) string {
	var parts []string
	cats := make([]string, 0, len(done))
	for c := range done {
		cats = append(cats, string(c))
	}
	sort.Strings(cats)
	for _, c := range cats {
		parts = append(parts, fmt.Sprintf("%s %d", Category(c).Title(), done[Category(c)]))
	}
	if len(parts) == 0 {
		return "действий нет"
	}
	return strings.Join(parts, ", ")
}
