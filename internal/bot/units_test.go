package bot

import (
	"encoding/json"
	"strings"
	"testing"

	"supermessy_bot/internal/s1914"
)

// canProduce — наша первая провинция может заказать эти войска, слот свободен.
func canProduce(t *testing.T, w *World, units ...string) (*World, int) {
	t.Helper()
	for i, p := range w.G.Provinces {
		if p.Owner != w.G.Me {
			continue
		}
		p.CanProduce = map[int]json.RawMessage{}
		for _, u := range units {
			id := w.Unit(u)
			if id == 0 {
				t.Skipf("в записанной партии нет войска %s", u)
			}
			p.CanProduce[id] = json.RawMessage(`{"@c":"su"}`)
		}
		p.Producing = nil
		w.G.Provinces[i] = p
		return NewWorld(w.G, w.Geo, w.Now), p.ID
	}
	t.Fatal("нет своих провинций")
	return nil, 0
}

// onlyUnits — заказы войск в провинции at.
func onlyUnits(w *World, r Rules, at int) []Decision {
	plan := &Plan{}
	stepUnits(w, r, newBudget(w), plan)
	var out []Decision
	for _, d := range find(plan, "производство", true) {
		if d.Province == at {
			out = append(out, d)
		}
	}
	return out
}

// Кавалерия за еду даёт больше силы на единицу цены, чем бронеавтомобиль
// за руду и нефть.
func TestCavalryPreferredOverCar(t *testing.T) {
	w := fixtureWorld(t)
	market(w, 1e6)
	w, at := canProduce(t, w, uCar, uCavalry)
	got := onlyUnits(w, DefaultRules(), at)
	if len(got) != 1 || !strings.Contains(got[0].What, w.G.UnitStats[w.Unit(uCavalry)].Name) {
		t.Fatalf("ждали кавалерию: %+v", got)
	}
	if !strings.Contains(got[0].Why, "другие:") {
		t.Errorf("в причине нет сравнения: %s", got[0].Why)
	}
}

// Не хватает на лучшее — берём то, на что хватает.
func TestAffordableUnitWhenBestTooExpensive(t *testing.T) {
	w := fixtureWorld(t)
	market(w, 1e6)
	for _, res := range []int{s1914.ResourceGrain, s1914.ResourceFish} {
		x := w.G.Resources[res]
		x.Amount = 0
		w.G.Resources[res] = x
	}
	w, at := canProduce(t, w, uCar, uCavalry)
	got := onlyUnits(w, DefaultRules(), at)
	if len(got) != 1 || !strings.Contains(got[0].What, w.G.UnitStats[w.Unit(uCar)].Name) {
		t.Fatalf("без еды ждали бронеавтомобиль: %+v", got)
	}
}

// Рода войск не из UNITS не заказываем.
func TestOnlyAllowedUnits(t *testing.T) {
	w := fixtureWorld(t)
	market(w, 1e6)
	w, at := canProduce(t, w, uCar, uCavalry)
	r := DefaultRules()
	r.Units = []string{uCar}
	got := onlyUnits(w, r, at)
	if len(got) != 1 || !strings.Contains(got[0].What, w.G.UnitStats[w.Unit(uCar)].Name) {
		t.Fatalf("ждали только бронеавтомобиль: %+v", got)
	}
}

// Заказали кавалерию там, где казарма на паузе, — казарму включаем
// в этом же заходе.
func TestBarracksOnWhenCavalryOrdered(t *testing.T) {
	w := barracksWorld(t, false, 20000, 100)
	market(w, 1e6)
	at := barracksAt(w)
	for i, p := range w.G.Provinces {
		if p.ID == at {
			p.CanProduce = map[int]json.RawMessage{w.Unit(uCavalry): json.RawMessage(`{"@c":"su"}`)}
			p.Producing = nil
			w.G.Provinces[i] = p
		}
	}
	w = NewWorld(w.G, w.Geo, w.Now)
	plan := Decide(w, DefaultRules(), freshMem())
	got := find(plan, "казарма-пауза", true)
	if len(got) != 1 || !strings.Contains(got[0].What, "включить") {
		t.Fatalf("казарму под заказанную кавалерию не включили: %+v", got)
	}
}

// Нефть убывает и её мало — бронеавтомобиль за нефть не заказываем.
func TestNoUnitsFromDrainingResource(t *testing.T) {
	w := fixtureWorld(t)
	market(w, 1e6)
	x := w.G.Resources[s1914.ResourceOil]
	x.Amount, x.Daily = 1500, -3900
	w.G.Resources[s1914.ResourceOil] = x
	w, at := canProduce(t, w, uCar)
	if got := onlyUnits(w, DefaultRules(), at); len(got) != 0 {
		t.Fatalf("заказали войско за последнюю нефть: %s", got[0].What)
	}
}
