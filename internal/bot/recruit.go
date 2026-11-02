package bot

// Войска. Пехоту игра даёт сама — призывом в провинциях с пунктом призыва;
// заказывать её не нужно. Бот следит за ней (сколько прибавилось
// с прошлого захода) и решает две вещи:
//
//   - что производить в каждой провинции со свободным слотом (stepUnits);
//   - казармы — на паузу, пока в провинции не делают кавалерию или
//     штурмовиков (stepBarracksPause).

import (
	"fmt"
	"sort"
	"strings"

	"supermessy_bot/internal/s1914"
)

// Производство войск. Что провинция может заказать, решает игра
// (CanProduce: нужное здание, исследование). Бот выбирает из этого то,
// что даёт больше силы на единицу цены: цену считает по справочным ценам
// биржи, а ресурс, которого не хватает (дефицит по карте или убывающий
// запас), — вдвое дороже. Так в начале партии, когда руды и нефти мало,
// выгоднее кавалерия за еду, чем бронеавтомобиль за руду и нефть.
// Заказываем только те рода войск, что в Rules.Units.

// unitOption — войско, которое можно заказать в провинции, и его оценка.
type unitOption struct {
	id    int
	name  string
	power float64
	cost  map[int]float64
	value float64 // цена в деньгах с поправкой на дефицит
	score float64 // сила на тысячу этой цены
}

// scarce — ресурс в дефиците: в стратегическом списке и его мало, или
// запас убывает.
func scarce(w *World, r Rules, b *Budget, res int) bool {
	if res == s1914.ResourceMoney {
		return false
	}
	if w.Daily(res) < 0 {
		return true
	}
	list, _ := strategic(w, r)
	for _, s := range list {
		if s == res && b.Free(res) < r.StrategicStock {
			return true
		}
	}
	return false
}

// unitOptions — что можно заказать в провинции, лучшие первыми.
func unitOptions(w *World, r Rules, b *Budget, p s1914.Province) []unitOption {
	allowed := map[int]bool{}
	for _, ident := range r.Units {
		if id := w.Unit(ident); id != 0 {
			allowed[id] = true
		}
	}
	var out []unitOption
	for id := range p.CanProduce {
		u, known := w.G.Units[id]
		if !allowed[id] || !known {
			continue
		}
		o := unitOption{id: id, name: w.G.UnitStats[id].Name, power: w.unitPower(id), cost: u.Cost}
		for res, n := range u.Cost {
			price := w.G.Market.Prices[res]
			if price <= 0 {
				price = 5
			}
			if res == s1914.ResourceMoney {
				price = 1
			}
			v := n * price
			if scarce(w, r, b, res) {
				v *= 2
			}
			o.value += v
		}
		if o.value > 0 {
			o.score = o.power / o.value * 1000
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	return out
}

// stepUnits — войска во всех провинциях со свободным слотом. Возвращает,
// что и где заказано: казармам это нужно знать в этом же заходе.
func stepUnits(w *World, r Rules, b *Budget, plan *Plan) map[int][]int {
	placed := map[int][]int{}
	for _, p := range ordered(w) {
		if len(p.CanProduce) == 0 {
			continue
		}
		if !p.ProdFree() {
			if until := p.ProducesUntil(); !until.IsZero() {
				plan.wake(until.Add(buildAfter), p.Name+": закончится производство")
			}
			continue
		}
		opts := unitOptions(w, r, b, p)
		if len(opts) == 0 {
			continue
		}
		// Лучшее, на что хватает; если не хватает ни на что — ждём лучшее.
		var pick *unitOption
		for i := range opts {
			if ok, _, _ := b.Check(opts[i].cost, false); ok {
				pick = &opts[i]
				break
			}
		}
		if pick == nil {
			best := opts[0]
			_, res, lack := b.Check(best.cost, false)
			b.noteLack(res, lack)
			why := fmt.Sprintf("лучшее здесь — %s, но не хватает: %s ещё %s", best.name, w.ResName(res), num(lack))
			if at, ever := b.When(res, lack); ever {
				why += ", накопится к " + clock(at, w.Now)
				plan.wake(at.Add(buildAfter), "накопятся ресурсы на "+best.name)
			}
			plan.noteAt(p.ID, CatRecruit, "производство", p.Name+": "+best.name, why)
			continue
		}
		action, err := s1914.ProduceAction(w.G, p.ID, pick.id)
		if err != nil {
			plan.noteAt(p.ID, CatRecruit, "производство", p.Name+": "+pick.name, err.Error())
			continue
		}
		var others []string
		for _, o := range opts {
			if o.id != pick.id {
				others = append(others, fmt.Sprintf("%s %.2f", o.name, o.score))
			}
		}
		why := fmt.Sprintf("сила %.1f за %s (в деньгах с поправкой на дефицит %s) — %.2f силы на тысячу",
			pick.power, costText(w, pick.cost), num(pick.value), pick.score)
		if len(others) > 0 {
			why += "; другие: " + strings.Join(others, ", ")
		}
		if pick != &opts[0] {
			why += fmt.Sprintf("; на %s пока не хватает ресурсов", opts[0].name)
		}
		b.Pay(pick.cost, false)
		placed[p.ID] = append(placed[p.ID], pick.id)
		plan.add(Decision{Category: CatRecruit, Rule: "производство", What: p.Name + ": " + pick.name,
			Why: why, Action: action, Cost: pick.cost, Province: p.ID})
	}
	return placed
}

// barracksUnits — войска, ради которых казарма нужна: пока их не
// производят, казарма стоит на паузе и не ест зерно (правило игрока,
// 02.11.2026).
var barracksUnits = []string{uCavalry, uStormtroopers}

// stepBarracksPause — казарма включена, только пока в провинции производится
// кавалерия или штурмовики; иначе — пауза.
func stepBarracksPause(w *World, r Rules, plan *Plan, ordered map[int][]int) {
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
		for _, id := range ordered[p.ID] {
			if want[id] {
				making = append(making, w.G.UnitStats[id].Name+" (заказали сейчас)")
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
