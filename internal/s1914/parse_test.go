package s1914

import (
	"os"
	"testing"
	"time"
)

// Записанная партия: x4, второй день, мы — игрок 6 (HAR 14.10.2026).
func fixture(t *testing.T) (*GameState, *MapGeometry) {
	t.Helper()
	raw, err := os.ReadFile("testdata/state_x4_day2.json")
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParseState(raw, "test", 6, time.UnixMilli(1792138756636))
	if err != nil {
		t.Fatal(err)
	}
	mraw, err := os.ReadFile("testdata/map_51_5.json")
	if err != nil {
		t.Fatal(err)
	}
	geo, err := ParseMap("51_5", mraw)
	if err != nil {
		t.Fatal(err)
	}
	return g, geo
}

func TestParseFixture(t *testing.T) {
	g, geo := fixture(t)
	if g.Speed != 4 {
		t.Errorf("скорость %v, ждали 4", g.Speed)
	}
	if g.DoubleThreshold != 5000 {
		t.Errorf("порог двойной добычи %v", g.DoubleThreshold)
	}
	mine := g.Owned(g.Me)
	if len(mine) == 0 {
		t.Fatal("своих провинций нет")
	}
	if len(g.Resources) == 0 {
		t.Fatal("запасов нет")
	}
	if len(g.Market.Prices) < 7 || len(g.Market.Asks) == 0 {
		t.Errorf("рынок не разобрался: %+v", g.Market.Prices)
	}
	if len(g.Market.Past) == 0 {
		t.Error("прошлых сделок нет")
	}
	if len(geo.Neighbors) == 0 || len(geo.Production) == 0 {
		t.Error("соседи или добыча карты не разобрались")
	}
	doubles := 0
	for _, p := range mine {
		if p.MainResource() < 0 {
			t.Errorf("%s: не нашли ресурс", p.Name)
		}
		if geo.Production[p.ID][p.MainResource()] >= g.DoubleThreshold {
			doubles++
		}
	}
	t.Logf("провинций %d, двойных %d, день %d, армий %d", len(mine), doubles, g.Day, len(g.Armies))
	if len(g.UnitStats) == 0 || g.UnitStats[198].Power == 0 {
		t.Errorf("сила бронеавтомобиля не разобралась: %+v", g.UnitStats[198])
	}
}

// Router должен давать тот же путь, что FindRoute: на нём стоят приказы.
func TestRouterMatchesFindRoute(t *testing.T) {
	g, geo := fixture(t)
	cost := func(int) float64 { return 1 }
	router := NewRouter(geo, cost)
	checked := 0
	for _, a := range g.Armies {
		if a.Owner != g.Me {
			continue
		}
		if _, ok := geo.Centers[a.Location]; !ok {
			continue
		}
		paths, ok := router.From(a.Pos)
		if !ok {
			continue
		}
		for _, p := range g.Owned(g.Me)[:5] {
			to := geo.Centers[p.ID]
			want, ok1 := FindRoute(geo, a.Pos, to, cost)
			got, ok2 := paths.To(to)
			if ok1 != ok2 {
				t.Fatalf("армия %d → %s: FindRoute %v, Router %v", a.ID, p.Name, ok1, ok2)
			}
			if ok1 && (len(got) == 0 || keyOf(got[len(got)-1]) != keyOf(want[len(want)-1]) || keyOf(got[0]) != keyOf(want[0])) {
				t.Fatalf("армия %d → %s: концы путей разные", a.ID, p.Name)
			}
			checked++
		}
		if checked > 30 {
			break
		}
	}
	if checked == 0 {
		t.Fatal("ни одного пути не проверили")
	}
}
