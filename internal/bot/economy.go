package bot

// Стройка. Правила — те, что описал игрок для карты «Европа» x4:
//
//  1. С первого дня — пункт призыва в каждой провинции.
//  2. В провинциях с двойными ресурсами — мастерская (под бронеавтомобили)
//     и казарма; казарму ставим и в столице.
//  3. Со второго дня — порт в прибрежных провинциях с двойными ресурсами.
//  4. С третьего — железная дорога, если позволяют ресурсы: сначала
//     двойные провинции и столица.
//  5. С пятого дня копим на фабрику, на шестой (когда игра её откроет)
//     ставим — в столице, а без столицы в лучшей двойной провинции.
//
// Порядок шагов задаёт planner.go: от него зависит, кому достанутся
// ресурсы, когда на всех не хватает.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"supermessy_bot/internal/s1914"
)

// buildAfter — насколько позже окончания стройки возвращаться: игровому
// серверу нужно мгновение, чтобы её закрыть.
const buildAfter = time.Minute

// builder — шаги стройки одного захода с общим бюджетом.
type builder struct {
	w     *World
	b     *Budget
	r     Rules
	plan  *Plan
	order []s1914.Province // наши провинции по важности
	busy  map[int]bool     // провинции, куда в этот заход уже что-то поставили
	// factoryHeld — на фабрику уже отложено: стройки ниже по списку её
	// деньги не трогают.
	factoryHeld bool
	// lacked — правила, которым в этот заход уже не хватило ресурсов:
	// остальные провинции того же правила ждут того же, и писать про
	// каждую отдельно незачем — их имена копятся в waiting.
	lacked  map[string]bool
	waiting map[string][]string
}

func newBuilder(w *World, b *Budget, r Rules, plan *Plan) *builder {
	return &builder{w: w, b: b, r: r, plan: plan, order: ordered(w), busy: map[int]bool{},
		lacked: map[string]bool{}, waiting: map[string][]string{}}
}

// ordered — наши провинции по важности: двойные (по добыче), столица,
// остальные (по добыче). Важные первыми получают ресурсы.
func ordered(w *World) []s1914.Province {
	out := append([]s1914.Province(nil), w.Mine()...)
	rank := func(p s1914.Province) int {
		switch {
		case w.IsDouble(p):
			return 0
		case p.Capital:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		bi, bj := w.BaseOutput(out[i]), w.BaseOutput(out[j])
		if bi != bj {
			return bi > bj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// try ставит здание вида image в провинцию p, если это возможно, и
// объясняет, если нет. keepMoney — сколько денег оставить нетронутыми
// сверх цены. Возвращает, поставили ли.
func (s *builder) try(p s1914.Province, image, rule, why string, useReserve bool, keepMoney float64) bool {
	w := s.w
	if w.HasFinished(p, image) || w.Building(p, image) {
		return false
	}
	_, id := w.Has(p, image)
	u, ok := w.G.Upgrades[id]
	if !ok {
		if u, ok = w.FirstTier(image); !ok {
			return false // такого здания в сценарии нет
		}
	}
	what := p.Name + ": " + u.Name
	if u.Available > w.G.Day {
		s.plan.noteAt(p.ID, CatBuild, rule, what, fmt.Sprintf("ждём %d-го дня: раньше игра это здание не строит (сейчас %d-й)", u.Available, w.G.Day))
		s.plan.wake(w.G.NextDay.Add(buildAfter), "смена дня")
		return false
	}
	if s.busy[p.ID] || !p.Free() {
		if !p.Free() {
			until := p.BuildsUntil()
			s.plan.noteAt(p.ID, CatBuild, rule, what, fmt.Sprintf("слот стройки занят до %s", clock(until, w.Now)))
			s.plan.wake(until.Add(buildAfter), p.Name+": закончится стройка")
		}
		return false
	}
	cost := u.Cost
	if keepMoney > 0 {
		cost = withExtra(cost, s1914.ResourceMoney, keepMoney)
	}
	if ok, res, lack := s.b.Check(cost, useReserve); !ok {
		if s.lacked[rule] {
			s.waiting[rule] = append(s.waiting[rule], p.Name)
			return false
		}
		s.lacked[rule] = true
		why := fmt.Sprintf("не хватает: %s ещё %s (есть %s)", w.ResName(res), num(lack), num(s.b.Free(res)))
		if keepMoney > 0 && res == s1914.ResourceMoney {
			why += fmt.Sprintf(", с запасом денег %s", num(keepMoney))
		}
		s.b.noteLack(res, lack)
		if at, ever := s.b.When(res, lack); ever {
			why += ", накопится к " + clock(at, w.Now)
			s.plan.wake(at.Add(buildAfter), "накопятся ресурсы на "+u.Name)
		} else {
			why += ", и прирост не положительный"
		}
		s.plan.noteAt(p.ID, CatBuild, rule, what, why)
		return false
	}
	s.b.Pay(u.Cost, useReserve)
	s.busy[p.ID] = true
	// Время по справочнику: настоящее назовёт игра, когда стройка встанет,
	// но вернуться к его концу стоит уже сейчас.
	if u.Build > 0 {
		s.plan.wake(w.Now.Add(u.Build+buildAfter), p.Name+": достроится "+u.Name)
	}
	s.plan.add(Decision{Category: CatBuild, Rule: rule, What: what,
		Why:    why + "; цена: " + costText(w, u.Cost),
		Action: s1914.BuildAction(w.G, p.ID, u.ID), Cost: u.Cost, Province: p.ID})
	return true
}

// flush — одна заметка обо всех провинциях, которые ждут того же, что
// и первая из них.
func (s *builder) flush(rule, what string) {
	if names := s.waiting[rule]; len(names) > 0 {
		s.plan.note(CatBuild, rule, what, fmt.Sprintf("тоже ждут ресурсов (%d): %s", len(names), strings.Join(names, ", ")))
		delete(s.waiting, rule)
	}
}

// stepRecruiting — правило 1: пункт призыва везде.
func (s *builder) stepRecruiting() {
	for _, p := range s.order {
		s.try(p, bRecruiting, "пункт-призыва-везде", "пункт призыва ставим в каждой провинции с первого дня", false, 0)
	}
	s.flush("пункт-призыва-везде", "пункт призыва")
}

// stepWorkshop — правило 2: мастерская в двойных провинциях.
func (s *builder) stepWorkshop() {
	for _, p := range s.order {
		if s.w.IsDouble(p) {
			s.try(p, bWorkshop, "мастерская-в-дабл",
				fmt.Sprintf("двойная провинция (%s %s в сутки): мастерская, чтобы сразу делать бронеавтомобили",
					s.w.ResName(p.MainResource()), num(s.w.BaseOutput(p))), false, 0)
		}
	}
	s.flush("мастерская-в-дабл", "мастерская")
}

// stepBarracks — правило 2: казарма в двойных провинциях и в столице.
func (s *builder) stepBarracks() {
	for _, p := range s.order {
		switch {
		case s.w.IsDouble(p):
			s.try(p, bBarracks, "казарма-в-дабл", "двойная провинция: казарма ускоряет призыв", false, 0)
		case p.Capital:
			s.try(p, bBarracks, "казарма-в-столице", "столица: казарма ускоряет призыв", false, 0)
		}
	}
	s.flush("казарма-в-дабл", "казарма")
}

// stepHarbour — правило 3: порт в прибрежных двойных со второго дня.
func (s *builder) stepHarbour() {
	if s.w.G.Day < s.r.HarbourDay {
		return
	}
	for _, p := range s.order {
		if s.w.IsDouble(p) && s.w.Coastal(p) {
			s.try(p, bHarbour, "порт-в-дабл",
				fmt.Sprintf("день %d, двойная провинция у моря: порт", s.w.G.Day), false, 0)
		}
	}
	s.flush("порт-в-дабл", "порт")
}

// stepRailway — правило 4: железная дорога с третьего дня, если позволяют
// ресурсы, сначала в двойных и в столице. Деньги на содержание не трогаем.
func (s *builder) stepRailway() {
	if s.w.G.Day < s.r.RailwayDay {
		return
	}
	for _, p := range s.order {
		if s.w.IsDouble(p) || p.Capital {
			s.try(p, bRailway, "жд-если-есть-ресурсы",
				fmt.Sprintf("день %d: железная дорога там, где важнее всего (двойная провинция или столица)", s.w.G.Day),
				false, s.r.MoneyKeep)
		}
	}
	s.flush("жд-если-есть-ресурсы", "железная дорога")
}

// stepFactory — правило 5: с пятого дня копим на фабрику, как только игра
// её откроет — ставим.
func (s *builder) stepFactory() {
	w := s.w
	if w.G.Day < s.r.FactoryReserveDay {
		return
	}
	for _, p := range w.Mine() {
		if _, id := w.Has(p, bFactory); id != 0 && w.G.Finished(p, id) || w.Building(p, bFactory) {
			return // фабрика уже есть или строится
		}
	}
	u, ok := w.FirstTier(bFactory)
	if !ok {
		return
	}
	target, ok := s.factorySite()
	if !ok {
		return
	}
	// Копим заранее: откладываем цену фабрики, и стройки ниже по списку
	// её не трогают.
	s.b.Reserve(u.Cost)
	s.factoryHeld = true
	if w.G.Day < u.Available {
		s.plan.noteAt(target.ID, CatBuild, "копим-на-фабрику", target.Name+": "+u.Name,
			fmt.Sprintf("день %d: откладываем %s, фабрику игра откроет на %d-й день",
				w.G.Day, costText(w, u.Cost), u.Available))
		s.plan.wake(w.G.NextDay.Add(buildAfter), "смена дня: откроется фабрика")
		return
	}
	s.try(target, bFactory, "фабрика-на-6-день",
		fmt.Sprintf("день %d: фабрика в %s", w.G.Day, target.Name), true, 0)
}

// factorySite — где ставить фабрику: в столице, иначе в лучшей двойной,
// иначе в самой доходной провинции.
func (s *builder) factorySite() (s1914.Province, bool) {
	if c, ok := s.w.Capital(); ok {
		return c, true
	}
	if len(s.order) > 0 {
		return s.order[0], true
	}
	return s1914.Province{}, false
}

func withExtra(cost map[int]float64, res int, extra float64) map[int]float64 {
	out := make(map[int]float64, len(cost)+1)
	for k, v := range cost {
		out[k] = v
	}
	out[res] += extra
	return out
}
