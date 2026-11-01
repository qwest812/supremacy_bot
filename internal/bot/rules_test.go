package bot

import (
	"strings"
	"testing"
	"time"

	"supermessy_bot/internal/s1914"
)

func freshMem() *Memory {
	return &Memory{SeenTrades: map[int64]bool{}, Refused: map[string]time.Time{}, Notes: map[string]bool{}}
}

func find(plan *Plan, rule string, action bool) []Decision {
	var out []Decision
	for _, d := range plan.Decisions {
		if d.Rule == rule && d.IsNote() != action {
			out = append(out, d)
		}
	}
	return out
}

// rich — бездонные запасы: проверяем правила, а не бедность.
func rich(w *World) {
	for id, r := range w.G.Resources {
		r.Amount, r.Measured, r.Rate = 1e6, w.Now, 1
		w.G.Resources[id] = r
	}
}

func TestDoublesFound(t *testing.T) {
	w := fixtureWorld(t)
	var doubles []string
	for _, p := range w.Mine() {
		if w.IsDouble(p) {
			doubles = append(doubles, p.Name)
		}
	}
	if len(doubles) == 0 {
		t.Fatal("двойных провинций не нашлось")
	}
	// Двойные идут первыми в очереди на ресурсы.
	if o := ordered(w); !w.IsDouble(o[0]) {
		t.Errorf("первой идёт не двойная: %s", o[0].Name)
	}
	t.Log("двойные:", strings.Join(doubles, ", "))
}

func TestFactoryReservedBeforeItOpens(t *testing.T) {
	w := fixtureWorld(t)
	rich(w)
	w.G.Day = 5
	plan := Decide(w, DefaultRules(), freshMem())
	if len(find(plan, "копим-на-фабрику", false)) != 1 {
		t.Fatalf("на 5-й день нет заметки про копилку на фабрику")
	}
	if len(find(plan, "фабрика-на-6-день", true)) != 0 {
		t.Fatal("фабрику поставили раньше, чем игра её открывает")
	}
}

func TestFactoryBuiltOnDay6(t *testing.T) {
	w := fixtureWorld(t)
	rich(w)
	w.G.Day = 6
	// Столица свободна, чтобы фабрике было где встать.
	for i, p := range w.G.Provinces {
		if p.Capital && p.Owner == w.G.Me {
			p.Building = nil
			w.G.Provinces[i] = p
		}
	}
	w = NewWorld(w.G, w.Geo, w.Now)
	plan := Decide(w, DefaultRules(), freshMem())
	got := find(plan, "фабрика-на-6-день", true)
	if len(got) != 1 {
		for _, d := range plan.Decisions {
			t.Log(d.Rule, d.What, d.Why)
		}
		t.Fatalf("на 6-й день фабрика не поставлена")
	}
	capital, _ := w.Capital()
	if !strings.HasPrefix(got[0].What, capital.Name) {
		t.Errorf("фабрика не в столице: %s", got[0].What)
	}
}

func TestRailwayNotBeforeDay3(t *testing.T) {
	w := fixtureWorld(t)
	rich(w)
	w.G.Day = 2
	if n := len(find(Decide(w, DefaultRules(), freshMem()), "жд-если-есть-ресурсы", true)); n != 0 {
		t.Fatalf("железную дорогу ставят на 2-й день: %d", n)
	}
}

// barracksWorld — наша провинция с готовой казармой.
func barracksWorld(t *testing.T, enabled bool, grain, daily float64) *World {
	w := fixtureWorld(t)
	barracks, _ := w.FirstTier(bBarracks)
	for i, p := range w.G.Provinces {
		if p.Owner == w.G.Me {
			p.Built = append(p.Built, barracks.ID)
			p.Condition = map[int]int{barracks.ID: barracks.BuildCondition}
			p.Enabled = map[int]bool{barracks.ID: enabled}
			w.G.Provinces[i] = p
			break
		}
	}
	r := w.G.Resources[s1914.ResourceGrain]
	r.Amount, r.Measured, r.Daily, r.Rate = grain, w.Now, daily, daily/86400
	w.G.Resources[s1914.ResourceGrain] = r
	return NewWorld(w.G, w.Geo, w.Now)
}

func TestBarracksOffWhenFoodLow(t *testing.T) {
	w := barracksWorld(t, true, 1000, -500)
	got := find(Decide(w, DefaultRules(), freshMem()), "казарма-по-еде", true)
	if len(got) != 1 || !strings.Contains(got[0].What, "выключить") {
		t.Fatalf("казарму не выключили при нехватке зерна: %+v", got)
	}
}

func TestBarracksOnWhenFoodOK(t *testing.T) {
	// В записанной партии игрок сам выключил три казармы, когда зерна
	// было мало, — при достатке бот включает их все, и нашу тоже.
	w := barracksWorld(t, false, 20000, 100)
	got := find(Decide(w, DefaultRules(), freshMem()), "казарма-по-еде", true)
	if len(got) < 1 {
		t.Fatal("казарму не включили, когда зерна хватает")
	}
	for _, d := range got {
		if !strings.Contains(d.What, "включить") {
			t.Errorf("при достатке зерна: %s", d.What)
		}
	}
}

func TestBarracksLeftAloneBetweenThresholds(t *testing.T) {
	w := barracksWorld(t, true, 5000, -100)
	if got := find(Decide(w, DefaultRules(), freshMem()), "казарма-по-еде", true); len(got) != 0 {
		t.Fatalf("казарму дёрнули между порогами: %+v", got)
	}
}

func TestRefusingBotSkipped(t *testing.T) {
	w := fixtureWorld(t)
	rich(w)
	mem := freshMem()
	bots := w.NativeBots()
	for _, b := range bots[:len(bots)-1] {
		mem.Refused[refusedKey(b.ID, s1914.ResourceWood)] = w.Now.Add(time.Hour)
	}
	bot, ok := pickBot(w, bots, s1914.ResourceWood, mem, map[int]bool{})
	if !ok || bot.ID != bots[len(bots)-1].ID {
		t.Fatalf("выбран отказавший бот: %+v", bot)
	}
}

func TestBudgetSharedAndReserved(t *testing.T) {
	w := fixtureWorld(t)
	rich(w)
	b := newBudget(w)
	b.Reserve(map[int]float64{s1914.ResourceMoney: 1e6 - 100})
	if ok, _, _ := b.Check(map[int]float64{s1914.ResourceMoney: 500}, false); ok {
		t.Fatal("потратили отложенное")
	}
	if ok, _, _ := b.Check(map[int]float64{s1914.ResourceMoney: 500}, true); !ok {
		t.Fatal("тому, ради кого отложено, не дали")
	}
}

func TestEveryActionExplained(t *testing.T) {
	w := fixtureWorld(t)
	rich(w)
	for _, d := range Decide(w, DefaultRules(), freshMem()).Actions() {
		if d.Why == "" || d.Rule == "" || d.What == "" {
			t.Errorf("решение без объяснения: %+v", d)
		}
	}
}
