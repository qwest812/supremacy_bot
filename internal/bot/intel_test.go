package bot

import (
	"strings"
	"testing"

	"supermessy_bot/internal/s1914"
)

// Сосед, потерявший по газете почти всю стартовую армию, — цель даже до
// дня, с которого бот обычно начинает войны.
func TestWeakenedNeighborAttackedEarly(t *testing.T) {
	w, bot, _, _ := borderSetup(t)
	w.G.Day = 2
	provinces := 0
	for _, p := range w.G.Provinces {
		if p.Owner == bot {
			provinces++
		}
	}
	w.Losses = &s1914.Losses{Days: 2, Players: []s1914.PlayerLosses{{ID: bot, Lost: int64(provinces * 9 * menPerUnit)}}}
	plan := Decide(w, DefaultRules(), freshMem())
	dump(t, plan)
	var declared bool
	for _, d := range plan.Actions() {
		if d.Rule == "объявить-войну" && d.Category == CatDiplomacy {
			declared = true
			if !strings.Contains(d.Why, "по газете потери") {
				t.Errorf("в причине нет газеты: %s", d.Why)
			}
		}
	}
	if !declared {
		t.Fatal("ослабшему соседу войну не объявили")
	}
}

// Без потерь по газете на 2-й день не воюем, как и раньше.
func TestHealthyNeighborWaits(t *testing.T) {
	w, bot, _, _ := borderSetup(t)
	w.G.Day = 2
	w.Losses = &s1914.Losses{Days: 2, Players: []s1914.PlayerLosses{{ID: bot, Lost: 3 * menPerUnit}}}
	for _, d := range Decide(w, DefaultRules(), freshMem()).Actions() {
		if d.Rule == "объявить-войну" {
			t.Fatalf("напали на целого соседа до срока: %s", d.Why)
		}
	}
}

// infantryArmy — наша армия из пехоты в центре провинции.
func infantryArmy(w *World, id int64, at, size int) s1914.Army {
	return s1914.Army{ID: id, Owner: w.G.Me, Location: at, Size: size, Pos: w.Geo.Centers[at],
		Units: []s1914.ArmyUnit{{Type: w.Unit(uInfantry), Size: size}}}
}

// Цель, на которую нужны две армии из разных мест: сначала сводим их
// в нашу провинцию у цели, а не бьём по одной.
func TestArmiesStackBeforeAttack(t *testing.T) {
	w, bot, home, lost := borderSetup(t)
	w.G.Day = 3
	// Вторая наша провинция рядом с home, не та, что отдана боту.
	other := 0
	for _, nb := range w.Geo.LandNeighbors(home.ID) {
		if p, ok := w.Province(nb); ok && p.Owner == w.G.Me && nb != lost.ID {
			other = nb
			break
		}
	}
	if other == 0 {
		t.Skip("у home нет второго нашего соседа")
	}
	var armies []s1914.Army
	for _, a := range w.G.Armies {
		if a.Owner != w.G.Me {
			armies = append(armies, a)
		}
	}
	armies = append(armies, infantryArmy(w, 1, home.ID, 20), infantryArmy(w, 2, other, 20))
	if capital, ok := w.Capital(); ok {
		// Столица не пустеет — пусть там стоят свои.
		armies = append(armies, infantryArmy(w, 3, capital.ID, 1), infantryArmy(w, 4, capital.ID, 1))
	}
	w.G.Armies = append(armies, enemyArmy(w, bot, lost.ID, 1))
	w = NewWorld(w.G, w.Geo, w.Now)
	// Подбираем гарнизон так, чтобы одной армии (90) не хватало, а двум — да.
	c := &commander{w: w}
	unit := c.guard(lost, bot)
	enemy := int(170 / 1.5 / unit)
	w.G.Armies[len(w.G.Armies)-1] = enemyArmy(w, bot, lost.ID, enemy)
	w = NewWorld(w.G, w.Geo, w.Now)

	plan := Decide(w, DefaultRules(), freshMem())
	dump(t, plan)
	if got := find(plan, "захват", true); len(got) > 0 {
		t.Fatalf("бьём по одной вместо стака: %s", got[0].What)
	}
	moves := find(plan, "сбор-стака", true)
	if len(moves) != 1 || moves[0].Army != 2 || moves[0].Province != home.ID {
		t.Fatalf("ждали, что армия 2 пойдёт к армии 1 в %s: %+v", home.Name, moves)
	}
}
