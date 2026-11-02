package bot

import "fmt"

// Decide — всё, что бот решает за один заход. Сеть не трогает: только
// состояние партии и память, поэтому правила проверяются тестами.
//
// Порядок шагов — это порядок, в котором решения получают ресурсы, когда
// на всех не хватает:
//
//  1. пункты призыва — дёшевы и нужны везде;
//  2. фабрика — с пятого дня её цена откладывается заранее;
//  3. мастерские в двойных провинциях, сразу за ними войска (recruit.go);
//  4. казармы, порты, железные дороги;
//  5. пауза казарм без кавалерии и штурмовиков и торговля — им ресурсы
//     не нужны или нужен только излишек;
//  6. армии: оборона, объявление войны (с оглядкой на газету: кто сколько
//     потерял, intel.go), захват стаками (military.go), и только потом
//     право прохода ботам — не тому, кому только что объявили войну.
func Decide(w *World, r Rules, mem *Memory) *Plan {
	plan := &Plan{}
	g := w.G
	switch {
	case g.Me <= 0:
		plan.note(CatBuild, "партия", "партия", "мы в этой партии не играем")
		return plan
	case g.Ended:
		plan.note(CatBuild, "партия", "партия", "партия окончена")
		return plan
	case len(w.Mine()) == 0:
		plan.note(CatBuild, "партия", "партия", "своих провинций нет — страна разбита")
		return plan
	case g.Day <= 0:
		plan.note(CatBuild, "партия", "партия", "партия ещё не началась")
		plan.wake(g.NextDay.Add(buildAfter), "начало партии")
		return plan
	}

	b := newBudget(w)
	observeInfantry(w, mem, plan)
	rememberStart(w, mem)
	noteIntel(w, r, mem, plan)

	s := newBuilder(w, b, r, plan)
	s.stepRecruiting()
	s.stepFactory()
	s.stepWorkshop()
	units := stepUnits(w, r, b, plan)
	s.stepBarracks()
	s.stepHarbour()
	s.stepRailway()

	stepBarracksPause(w, r, plan, units)
	planMarket(w, b, r, mem, plan)
	// Армии раньше дипломатии: если в этот заход объявим войну, право
	// прохода этому боту выдавать уже нельзя.
	planMilitary(w, r, mem, plan)
	planDiplomacy(w, r, mem, plan)

	plan.wake(g.NextDay.Add(buildAfter), fmt.Sprintf("смена дня (наступит %d-й)", g.Day+1))
	plan.Intents = intents(w, r, mem, plan)
	return plan
}
