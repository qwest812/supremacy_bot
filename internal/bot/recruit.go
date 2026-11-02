package bot

// Войска. Пехоту игра даёт сама — призывом в провинциях с пунктом призыва,
// казарма его ускоряет; заказывать её не нужно. Бот следит за ней
// (сколько прибавилось с прошлого захода) и решает две вещи:
//
//   - бронеавтомобили — в каждой провинции с готовой мастерской, как только
//     свободен слот производства и хватает ресурсов;
//   - казармы — выключить, когда зерна мало и оно тает (казарма ест 1000
//     зерна в сутки), и включить обратно, когда запас восстановился.

import (
	"fmt"
	"strings"

	"supermessy_bot/internal/s1914"
)

// stepCars — бронеавтомобили в мастерских.
func stepCars(w *World, b *Budget, plan *Plan, busy map[int]bool) {
	car := w.Unit(uCar)
	if car == 0 {
		return
	}
	for _, p := range ordered(w) {
		if !w.HasFinished(p, bWorkshop) {
			continue
		}
		name := w.G.UnitStats[car].Name
		what := p.Name + ": " + name
		if !p.ProdFree() || busy[p.ID] {
			if until := p.ProducesUntil(); !until.IsZero() {
				plan.wake(until.Add(buildAfter), p.Name+": закончится производство")
			}
			continue
		}
		if _, can := p.CanProduce[car]; !can {
			plan.noteAt(p.ID, CatRecruit, "машинки-в-мастерских", what, "игра пока не даёт заказать здесь бронеавтомобиль")
			continue
		}
		unit, known := w.G.Units[car]
		if !known {
			continue
		}
		if ok, res, lack := b.Check(unit.Cost, false); !ok {
			b.noteLack(res, lack)
			why := fmt.Sprintf("не хватает: %s ещё %s", w.ResName(res), num(lack))
			if at, ever := b.When(res, lack); ever {
				why += ", накопится к " + clock(at, w.Now)
				plan.wake(at.Add(buildAfter), "накопятся ресурсы на бронеавтомобиль")
			}
			plan.noteAt(p.ID, CatRecruit, "машинки-в-мастерских", what, why)
			continue
		}
		action, err := s1914.ProduceAction(w.G, p.ID, car)
		if err != nil {
			plan.noteAt(p.ID, CatRecruit, "машинки-в-мастерских", what, err.Error())
			continue
		}
		b.Pay(unit.Cost, false)
		busy[p.ID] = true
		plan.add(Decision{Category: CatRecruit, Rule: "машинки-в-мастерских", What: what,
			Why:    "в мастерской свободен слот производства; цена: " + costText(w, unit.Cost),
			Action: action, Cost: unit.Cost, Province: p.ID})
	}
}

// barracksUnits — войска, ради которых казарма нужна: пока их не
// производят, казарма стоит на паузе и не ест зерно (правило игрока,
// 02.11.2026).
var barracksUnits = []string{uCavalry, uStormtroopers}

// stepBarracksPause — казарма включена, только пока в провинции производится
// кавалерия или штурмовики; иначе — пауза.
func stepBarracksPause(w *World, r Rules, plan *Plan) {
	want := map[int]bool{}
	var names []string
	for _, ident := range barracksUnits {
		if id := w.Unit(ident); id != 0 {
			want[id] = true
			names = append(names, w.G.UnitStats[id].Name)
		}
	}
	kinds := strings.Join(names, " и ")
	if kinds == "" {
		kinds = "кавалерия и штурмовики"
	}
	grain := w.Amount(s1914.ResourceGrain)
	daily := w.Daily(s1914.ResourceGrain)
	for _, p := range w.Mine() {
		_, id := w.Has(p, bBarracks)
		if id == 0 || !w.G.Finished(p, id) {
			continue
		}
		var making []string
		for _, pr := range p.Producing {
			if want[pr.UnitTypeID] {
				making = append(making, w.G.UnitStats[pr.UnitTypeID].Name+" до "+clock(pr.Ends, w.Now))
			}
		}
		on := p.Enabled[id]
		what := p.Name + ": " + w.G.Upgrades[id].Name
		switch {
		case len(making) == 0 && on:
			plan.add(Decision{Category: CatRecruit, Rule: "казарма-пауза", What: what + " — пауза",
				Why: fmt.Sprintf("здесь не производятся %s — казарма простаивает и ест зерно (сейчас %s, прирост %s в сутки)",
					kinds, num(grain), num(daily)),
				Action: s1914.ToggleAction(w.G, p.ID, id, false), Province: p.ID})
		case len(making) > 0 && !on:
			plan.add(Decision{Category: CatRecruit, Rule: "казарма-пауза", What: what + " — включить",
				Why:    "здесь производится " + strings.Join(making, ", ") + " — казарма нужна",
				Action: s1914.ToggleAction(w.G, p.ID, id, true), Province: p.ID})
		case len(making) > 0 && grain < r.FoodLow && daily < 0:
			plan.noteAt(p.ID, CatRecruit, "казарма-пауза", what,
				fmt.Sprintf("зерна мало (%s, прирост %s в сутки), но здесь производится %s — казарму не выключаем",
					num(grain), num(daily), strings.Join(making, ", ")))
		}
	}
}

// observeInfantry — сколько пехоты прибавилось с прошлого захода. Только
// заметка: пехоту игра призывает сама.
func observeInfantry(w *World, mem *Memory, plan *Plan) {
	inf := w.Unit(uInfantry)
	if inf == 0 {
		return
	}
	now := w.UnitCount(inf)
	if mem.Infantry > 0 && now != mem.Infantry {
		diff := now - mem.Infantry
		sign := "+"
		if diff < 0 {
			sign = ""
		}
		plan.note(CatRecruit, "призыв-пехоты", "пехота",
			fmt.Sprintf("%s%d с прошлого захода, всего %d", sign, diff, now))
	}
	mem.Infantry = now
}
