package bot

import (
	"strings"
	"testing"

	"supermessy_bot/internal/s1914"
)

// borderSetup — отдаёт боту одну нашу провинцию, соседнюю с другой нашей,
// где стоят наши армии. Возвращает мир, номер бота и эту провинцию.
func borderSetup(t *testing.T) (*World, int, s1914.Province, s1914.Province) {
	t.Helper()
	w := fixtureWorld(t)
	bot := 0
	for id, p := range w.G.Players {
		if p.NativeAI && !p.Defeated && (bot == 0 || id < bot) {
			bot = id
		}
	}
	// Наша провинция с армией и её наш сосед без армий.
	armyAt := map[int]bool{}
	for _, a := range w.MyArmies() {
		if w.landArmy(a) {
			armyAt[a.Location] = true
		}
	}
	for _, home := range w.Mine() {
		if !armyAt[home.ID] {
			continue
		}
		for _, nb := range w.Geo.LandNeighbors(home.ID) {
			p, ok := w.Province(nb)
			if !ok || p.Owner != w.G.Me || armyAt[nb] || p.Capital {
				continue
			}
			for i := range w.G.Provinces {
				if w.G.Provinces[i].ID == nb {
					w.G.Provinces[i].Owner = bot
				}
			}
			w = NewWorld(w.G, w.Geo, w.Now)
			lost, _ := w.Province(nb)
			return w, bot, home, lost
		}
	}
	t.Fatal("не нашли пару провинций для теста")
	return nil, 0, s1914.Province{}, s1914.Province{}
}

func enemyArmy(w *World, owner, at, infantry int) s1914.Army {
	return s1914.Army{ID: 900000 + int64(at), Owner: owner, Location: at, Size: infantry,
		Pos: w.Geo.Centers[at], Units: []s1914.ArmyUnit{{Type: w.Unit(uInfantry), Size: infantry}}}
}

func decisions(plan *Plan, cat Category, action bool) []Decision {
	var out []Decision
	for _, d := range plan.Decisions {
		if d.Category == cat && d.IsNote() != action {
			out = append(out, d)
		}
	}
	return out
}

func dump(t *testing.T, plan *Plan) {
	for _, d := range plan.Decisions {
		if d.Category == CatAttack || d.Category == CatDefense || d.Category == CatDiplomacy {
			t.Logf("%s %v [%s] %s — %s", d.Category.Title(), !d.IsNote(), d.Rule, d.What, d.Why)
		}
	}
}

func TestDeclareWarAndTakeEmptyProvince(t *testing.T) {
	w, bot, _, lost := borderSetup(t)
	w.G.Day = 3
	mem := freshMem()
	plan := Decide(w, DefaultRules(), mem)
	dump(t, plan)
	var declared, took bool
	for _, d := range plan.Actions() {
		if d.Rule == "объявить-войну" && d.Category == CatDiplomacy {
			declared = true
		}
		if d.Rule == "захват" && strings.Contains(d.What, lost.Name) {
			took = true
			if strings.Contains(d.What, "атака") {
				t.Error("в пустую провинцию идём атакой на армию")
			}
		}
	}
	if !declared || !took {
		t.Fatalf("войну объявили: %v, провинцию взяли: %v", declared, took)
	}
	if mem.WarTarget != bot {
		t.Errorf("цель войны %d, ждали %d", mem.WarTarget, bot)
	}
	// Тому, кому объявили войну, право прохода не выдаём — это был бы мир.
	for _, d := range decisions(plan, CatDiplomacy, true) {
		if d.Rule == "проход-ботам" && strings.HasPrefix(d.What, w.G.Players[bot].Nation) {
			t.Error("цели войны выдали право прохода")
		}
	}
}

func TestNoWarBeforeDay(t *testing.T) {
	w, _, _, _ := borderSetup(t)
	w.G.Day = 2
	plan := Decide(w, DefaultRules(), freshMem())
	for _, d := range plan.Actions() {
		if d.Rule == "объявить-войну" || d.Rule == "захват" {
			t.Fatalf("воюем на 2-й день: %s", d.What)
		}
	}
}

func TestNoWarWhenDisabled(t *testing.T) {
	w, _, _, _ := borderSetup(t)
	w.G.Day = 5
	r := DefaultRules()
	r.War = false
	for _, d := range Decide(w, r, freshMem()).Actions() {
		if d.Rule == "объявить-войну" || d.Rule == "захват" {
			t.Fatalf("WAR=false, а бот воюет: %s", d.What)
		}
	}
}

func TestGuardedProvinceNeedsSuperiority(t *testing.T) {
	w, bot, _, lost := borderSetup(t)
	w.G.Day = 3
	// Огромный гарнизон: брать нечем — войну не объявляем вовсе.
	w.G.Armies = append(w.G.Armies, enemyArmy(w, bot, lost.ID, 5000))
	w = NewWorld(w.G, w.Geo, w.Now)
	plan := Decide(w, DefaultRules(), freshMem())
	dump(t, plan)
	for _, d := range plan.Actions() {
		if d.Rule == "объявить-войну" || d.Rule == "захват" {
			t.Fatalf("напали на неприступное: %s", d.What)
		}
	}
}

func TestDefendAgainstEnemyAtBorder(t *testing.T) {
	w, bot, home, lost := borderSetup(t)
	w.G.Day = 3
	w.G.Relations[bot] = relWar
	// Враг у границы, втрое сильнее гарнизона, но слабее всех наших вместе.
	local := 0.0
	for _, a := range w.MyArmies() {
		if a.Location == home.ID {
			local += w.Power(a)
		}
	}
	n := int(local*3/4.5) + 1
	w.G.Armies = append(w.G.Armies, enemyArmy(w, bot, lost.ID, n))
	w = NewWorld(w.G, w.Geo, w.Now)
	plan := Decide(w, DefaultRules(), freshMem())
	dump(t, plan)
	if len(decisions(plan, CatDefense, true))+len(decisions(plan, CatDefense, false)) == 0 {
		t.Fatal("угрозу у границы не заметили")
	}
}

func TestEnemyInsideCountered(t *testing.T) {
	w, bot, home, _ := borderSetup(t)
	w.G.Relations[bot] = relWar
	// Один вражеский пехотинец в нашей провинции с армией — выбиваем.
	w.G.Armies = append(w.G.Armies, enemyArmy(w, bot, home.ID, 1))
	w = NewWorld(w.G, w.Geo, w.Now)
	plan := Decide(w, DefaultRules(), freshMem())
	dump(t, plan)
	found := false
	for _, d := range decisions(plan, CatDefense, true) {
		if d.Rule == "выбить-из-провинции" && strings.Contains(d.What, "атака") {
			found = true
		}
	}
	if !found {
		t.Fatal("врага в своей провинции не атаковали")
	}
}

func TestHostileRules(t *testing.T) {
	w := fixtureWorld(t)
	var bot int
	for id, p := range w.G.Players {
		if p.NativeAI && !p.Defeated {
			bot = id
			break
		}
	}
	delete(w.G.Relations, bot)
	delete(w.G.TheirRelations, bot)
	if w.Hostile(bot) || w.CanEnter(bot) {
		t.Error("по умолчанию — мир, и без права прохода войти нельзя")
	}
	w.G.TheirRelations[bot] = s1914.RelationRightOfWay
	if !w.CanEnter(bot) || w.Hostile(bot) {
		t.Error("с его правом прохода войти можно, и это не война")
	}
	w.G.Relations[bot] = relWar
	if !w.Hostile(bot) {
		t.Error("наша война — это война")
	}
}

func TestRetreatFromHopeless(t *testing.T) {
	w, bot, home, lost := borderSetup(t)
	w.G.Day = 3
	w.G.Relations[bot] = relWar
	if home.Capital {
		t.Skip("провинция с армией — столица, её не бросают")
	}
	// Враг в сотни раз сильнее всего, что у нас есть.
	w.G.Armies = append(w.G.Armies, enemyArmy(w, bot, lost.ID, 100000))
	w = NewWorld(w.G, w.Geo, w.Now)
	plan := Decide(w, DefaultRules(), freshMem())
	retreat, reinforce := 0, 0
	for _, d := range decisions(plan, CatDefense, true) {
		switch d.Rule {
		case "отступление":
			retreat++
		case "подкрепление":
			if strings.Contains(d.What, "→ "+home.Name) {
				reinforce++
			}
		}
	}
	if retreat == 0 || reinforce > 0 {
		dump(t, plan)
		t.Fatalf("отступлений %d, подкреплений в обречённую провинцию %d", retreat, reinforce)
	}
}

// На слабого врага у границы уже идёт наша армия (атака ведёт к его
// позиции, а не в центр провинции) — вторую не посылаем.
func TestNoSecondStrikeOnSameEnemy(t *testing.T) {
	w, bot, home, lost := borderSetup(t)
	w.G.Day = 3
	w.G.Relations[bot] = relWar
	enemy := enemyArmy(w, bot, lost.ID, 2)
	enemy.Pos.X += 3
	going := infantryArmy(w, 77, home.ID, 20)
	to := enemy.Pos
	going.MarchTo = &to
	w.G.Armies = append(w.G.Armies, enemy, going)
	w = NewWorld(w.G, w.Geo, w.Now)
	plan := Decide(w, DefaultRules(), freshMem())
	dump(t, plan)
	for _, d := range decisions(plan, CatDefense, true) {
		if d.Rule == "разбить-армию-у-границы" {
			t.Fatalf("послали вторую армию: %s", d.What)
		}
	}
}
