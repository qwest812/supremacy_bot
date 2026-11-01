package bot

// Война: оборона (этап 3), расширение за счёт ботов (этап 4) и бои (этап 5).
//
// Порядок за заход:
//
//  1. Разведка. Где стоят вражеские сухопутные армии, сколько они весят,
//     какие наши провинции под угрозой: вражеская армия внутри или
//     в соседней провинции.
//  2. Оборона. На каждую угрозу — по важности провинции (столица, двойные,
//     очки победы):
//     – хватает свободных сил с перевесом AttackRatio — бьём врага первыми;
//     – нет — подтягиваем подкрепления, пока защита не станет
//     DefenseRatio от угрозы;
//     – не удержать никак — уводим гарнизон, если враг сильнее его
//     в RetreatRatio раз (столицу не бросаем никогда).
//  3. Объявление войны. Одна война за раз: если с соседом уже воюем —
//     воюем с ним; если нет — выбираем бота-соседа, чьи пограничные
//     провинции ценнее всего на единицу его силы, и объявляем ему войну,
//     когда своих свободных сил хватает и угроз нет.
//  4. Захват. Вражеские провинции у нашей границы — по ценности на единицу
//     обороны. На каждую собираем ближайшие свободные армии с перевесом
//     AttackRatio; пустую берёт одна ближайшая армия.
//
// «Свободная» армия — наша сухопутная армия на суше, не в бою, без
// приказа марша и не на месте, которое само под угрозой.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"supermessy_bot/internal/s1914"
)

type force struct {
	a     s1914.Army
	power float64
	paths *s1914.Paths
	ok    bool // пути посчитаны и армия на дороге
	done  bool // пути уже пробовали считать
}

// threat — вражеская группа: все враги в одной провинции.
type threat struct {
	at      int // где стоит враг
	armies  []s1914.Army
	power   float64
	defend  int  // какую нашу провинцию это прежде всего задевает
	inside  bool // враг уже в нашей провинции
	exposed []int
}

type commander struct {
	w      *World
	r      Rules
	mem    *Memory
	plan   *Plan
	router *s1914.Router
	hop    float64

	forces  []*force       // все наши сухопутные армии на суше
	used    map[int64]bool // армии, получившие приказ в этот заход
	enemyAt map[int][]s1914.Army
	ourAt   map[int]float64 // наша сила в провинции (стоящие там и идущие туда)
	danger  map[int]bool    // наши провинции под угрозой
	threats []*threat
	// pressed — оборона не справилась: новых войн не начинаем.
	pressed bool
}

func planMilitary(w *World, r Rules, mem *Memory, plan *Plan) {
	if w.Geo == nil {
		plan.note(CatAttack, "война", "армии", "нет карты партии — армиями не командуем")
		return
	}
	c := &commander{w: w, r: r, mem: mem, plan: plan, hop: w.hop(),
		used: map[int64]bool{}, enemyAt: map[int][]s1914.Army{}, ourAt: map[int]float64{}, danger: map[int]bool{}}
	c.router = s1914.NewRouter(w.Geo, w.routeCost())
	c.scan()
	c.defend()
	if r.War {
		target := c.chooseWar()
		c.conquer(target)
	}
}

// scan — разведка: наши армии, враги, угрозы.
func (c *commander) scan() {
	w := c.w
	centerOf := map[s1914.MapPoint]int{}
	for id, p := range w.Geo.Centers {
		centerOf[p] = id
	}
	for _, a := range w.G.Armies {
		if !w.landArmy(a) {
			continue
		}
		switch {
		case a.Owner == w.G.Me:
			f := &force{a: a, power: w.Power(a)}
			c.forces = append(c.forces, f)
			// Сила в провинции — там, где армия будет: идущая считается
			// в цели марша, стоящая — на месте.
			where := a.Location
			if a.MarchTo != nil {
				if id, ok := centerOf[*a.MarchTo]; ok {
					where = id
				}
			}
			c.ourAt[where] += f.power
		case w.Hostile(a.Owner):
			c.enemyAt[a.Location] = append(c.enemyAt[a.Location], a)
		}
	}

	for at, armies := range c.enemyAt {
		t := &threat{at: at, armies: armies}
		for _, a := range armies {
			t.power += w.Power(a)
		}
		if p, ok := w.Province(at); ok && p.Owner == w.G.Me {
			t.inside, t.exposed = true, []int{at}
		} else {
			for _, nb := range w.Geo.LandNeighbors(at) {
				if p, ok := w.Province(nb); ok && p.Owner == w.G.Me {
					t.exposed = append(t.exposed, nb)
				}
			}
		}
		if len(t.exposed) == 0 {
			continue
		}
		// Защищаем прежде всего самую ценную из задетых провинций.
		sort.Slice(t.exposed, func(i, j int) bool {
			pi, _ := w.Province(t.exposed[i])
			pj, _ := w.Province(t.exposed[j])
			return w.Value(pi) > w.Value(pj)
		})
		t.defend = t.exposed[0]
		for _, id := range t.exposed {
			c.danger[id] = true
		}
		c.threats = append(c.threats, t)
	}
	sort.Slice(c.threats, func(i, j int) bool {
		ti, tj := c.threats[i], c.threats[j]
		if ti.inside != tj.inside {
			return ti.inside
		}
		pi, _ := w.Province(ti.defend)
		pj, _ := w.Province(tj.defend)
		return w.Value(pi) > w.Value(pj)
	})
}

// free — армия, которой можно дать приказ: не в бою, без марша, не
// размещается и не получила приказа в этот заход. home разрешает брать
// армии, стоящие в угрожаемых провинциях (для их собственной защиты).
func (c *commander) free(f *force, home bool) bool {
	a := f.a
	if c.used[a.ID] || a.Fighting || a.Deploying || a.MarchTo != nil || f.power <= 0 {
		return false
	}
	return home || !c.danger[a.Location]
}

// pathsOf — пути от армии, считаются один раз и по требованию.
func (c *commander) pathsOf(f *force) (*s1914.Paths, bool) {
	if !f.done {
		f.done = true
		f.paths, f.ok = c.router.From(f.a.Pos)
	}
	return f.paths, f.ok
}

// candidate — армия и её путь до цели.
type candidate struct {
	f    *force
	path []s1914.MapPoint
	cost float64
}

// nearest — свободные армии, которые дотянутся до центра провинции
// target не дальше чем за hops провинций, ближние первыми.
func (c *commander) nearest(target int, hops float64, home bool) []candidate {
	to, ok := c.w.Geo.Centers[target]
	if !ok {
		return nil
	}
	var out []candidate
	for _, f := range c.forces {
		if !c.free(f, home) {
			continue
		}
		paths, ok := c.pathsOf(f)
		if !ok {
			continue
		}
		cost, ok := paths.Cost(to)
		if !ok || cost > hops*c.hop {
			continue
		}
		path, ok := paths.To(to)
		if !ok {
			continue
		}
		out = append(out, candidate{f: f, path: path, cost: cost})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].cost < out[j].cost })
	return out
}

// gather — ближайшие армии, пока их сила не дойдёт до need. Ложь — всех
// вместе не хватает; тогда отдаётся сколько есть.
func gather(cands []candidate, need float64) ([]candidate, float64, bool) {
	var picked []candidate
	sum := 0.0
	for _, cand := range cands {
		if sum >= need && len(picked) > 0 {
			break
		}
		picked = append(picked, cand)
		sum += cand.f.power
	}
	return picked, sum, sum >= need && len(picked) > 0
}

func (c *commander) name(id int) string {
	if p, ok := c.w.Province(id); ok && p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("провинция %d", id)
}

func (c *commander) nation(player int) string {
	if p, ok := c.w.G.Players[player]; ok && p.Nation != "" {
		return p.Nation
	}
	return fmt.Sprintf("игрок %d", player)
}

func (c *commander) owners(armies []s1914.Army) string {
	seen := map[int]bool{}
	var names []string
	for _, a := range armies {
		if !seen[a.Owner] {
			seen[a.Owner] = true
			names = append(names, c.nation(a.Owner))
		}
	}
	return strings.Join(names, ", ")
}

func strongest(armies []s1914.Army, w *World) s1914.Army {
	best, bp := armies[0], -1.0
	for _, a := range armies {
		if p := w.Power(a); p > bp {
			best, bp = a, p
		}
	}
	return best
}

// order — приказ армии: напасть на армию, если она есть, иначе идти
// в центр провинции.
func (c *commander) order(cat Category, rule string, cand candidate, target int, enemy []s1914.Army, why string) {
	c.used[cand.f.a.ID] = true
	what := fmt.Sprintf("армия %d (сила %.0f) → %s", cand.f.a.ID, cand.f.power, c.name(target))
	var action map[string]any
	var enemyID int64
	if len(enemy) > 0 {
		e := strongest(enemy, c.w)
		enemyID = e.ID
		action = s1914.AttackAction(cand.f.a, cand.path, e)
		what += fmt.Sprintf(", атака на армию %s (сила %.0f)", c.nation(e.Owner), c.w.Power(e))
	} else {
		action = s1914.MoveAction(cand.f.a, cand.path)
	}
	c.plan.add(Decision{Category: cat, Rule: rule, What: what, Why: why, Action: action,
		Province: target, Army: cand.f.a.ID, Path: cand.path, Enemy: enemyID})
}

// defend — этап 3.
func (c *commander) defend() {
	w, r := c.w, c.r
	for _, t := range c.threats {
		def := c.name(t.defend)
		dp, _ := w.Province(t.defend)
		enemies := c.owners(t.armies)
		local := c.ourAt[t.defend] * w.fortMult(dp)
		where := "у границы, в " + c.name(t.at)
		if t.inside {
			where = "в нашей провинции " + def
		}
		head := fmt.Sprintf("враг (%s, сила %.0f) %s", enemies, t.power, where)

		// Бить первыми, если перевес. Враг в своей провинции — туда можно,
		// раз мы воюем; в нашей — тем более.
		cands := c.nearest(t.at, r.DefendHops, true)
		if picked, sum, ok := gather(cands, t.power*r.AttackRatio); ok {
			rule := "разбить-армию-у-границы"
			if t.inside {
				rule = "выбить-из-провинции"
			}
			for _, cand := range picked {
				c.order(CatDefense, rule, cand, t.at, t.armies,
					fmt.Sprintf("%s; собираем %.0f против %.0f (нужен перевес ×%.1f)", head, sum, t.power, r.AttackRatio))
			}
			continue
		}
		if t.inside {
			// Выбить нечем — защищаем соседние свои провинции подкреплениями
			// ниже по списку угроз; здесь только объясняем.
			c.plan.noteAt(t.defend, CatDefense, "выбить-из-провинции", def,
				fmt.Sprintf("%s; выбить нечем: в досягаемости только %.0f из нужных %.0f", head,
					sumPower(cands), t.power*r.AttackRatio))
			c.pressed = true
			continue
		}

		need := t.power*r.DefenseRatio - local
		if need <= 0 {
			c.plan.noteAt(t.defend, CatDefense, "оборона", def, fmt.Sprintf("%s; защита %.0f — хватает", head, local))
			continue
		}
		cands = c.keepCapital(c.nearest(t.defend, r.DefendHops, false))
		picked, sum, ok := gather(cands, need)
		reinforce := func(why string) {
			for _, cand := range picked {
				c.order(CatDefense, "подкрепление", cand, t.defend, nil, why)
			}
		}
		if ok {
			reinforce(fmt.Sprintf("%s; защита %.0f, нужно %.0f — подтягиваем %.0f", head, local, t.power*r.DefenseRatio, sum))
			continue
		}
		c.pressed = true
		// Не удержать. Уходим, если враг сильнее всего, что мы можем там
		// собрать, в RetreatRatio раз; столицу не бросаем никогда. Иначе стоим
		// и подтягиваем всё, что есть.
		if !dp.Capital && local > 0 && t.power >= (local+sum)*r.RetreatRatio {
			c.retreat(t, head)
			continue
		}
		if sum > 0 {
			reinforce(fmt.Sprintf("%s; защита %.0f, нужно %.0f, больше %.0f не собрать — стоим, подтягиваем всё",
				head, local, t.power*r.DefenseRatio, sum))
		} else {
			c.plan.noteAt(t.defend, CatDefense, "оборона", def,
				fmt.Sprintf("%s; защита %.0f, подкреплений в досягаемости нет — стоим", head, local))
		}
	}
}

func sumPower(cands []candidate) float64 {
	s := 0.0
	for _, c := range cands {
		s += c.f.power
	}
	return s
}

// retreat уводит армии из обречённой провинции в ближайшую свою,
// которой ничто не грозит.
func (c *commander) retreat(t *threat, head string) {
	w := c.w
	for _, f := range c.forces {
		if f.a.Location != t.defend || !c.free(f, true) {
			continue
		}
		paths, ok := c.pathsOf(f)
		if !ok {
			continue
		}
		best, bestCost := 0, 0.0
		var bestPath []s1914.MapPoint
		for _, p := range w.Mine() {
			if c.danger[p.ID] || p.ID == t.defend {
				continue
			}
			to := w.Geo.Centers[p.ID]
			cost, ok := paths.Cost(to)
			if !ok || (best != 0 && cost >= bestCost) {
				continue
			}
			if path, ok := paths.To(to); ok {
				best, bestCost, bestPath = p.ID, cost, path
			}
		}
		if best == 0 {
			c.plan.note(CatDefense, "отступление", c.name(t.defend), head+"; отступать некуда")
			return
		}
		c.order(CatDefense, "отступление", candidate{f: f, path: bestPath, cost: bestCost}, best, nil,
			fmt.Sprintf("%s; наша сила там %.0f — в %.1f раза меньше, отходим в %s", head, f.power,
				t.power/max(f.power, 1), c.name(best)))
	}
}

// border — провинции игрока, граничащие с нашими.
func (c *commander) border(player int) []s1914.Province {
	w := c.w
	seen := map[int]bool{}
	var out []s1914.Province
	for _, mine := range w.Mine() {
		for _, nb := range w.Geo.LandNeighbors(mine.ID) {
			p, ok := w.Province(nb)
			if ok && p.Owner == player && !seen[nb] {
				seen[nb] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// guard — сила обороны провинции: армии игрока в ней (видимые) с поправкой
// на крепость.
func (c *commander) guard(p s1914.Province, owner int) float64 {
	s := 0.0
	for _, a := range c.w.G.Armies {
		if a.Location == p.ID && a.Owner == owner {
			s += c.w.Power(a)
		}
	}
	return s * c.w.fortMult(p)
}

func (c *commander) idlePower() float64 {
	s := 0.0
	for _, f := range c.forces {
		if c.free(f, false) {
			s += f.power
		}
	}
	return s
}

// chooseWar — этап 4: с кем воюем. Возвращает игрока, чьи провинции берём;
// ноль — ни с кем.
func (c *commander) chooseWar() int {
	w, r, mem := c.w, c.r, c.mem
	// Уже воюем с соседом — продолжаем: сначала тот, кого выбрали сами.
	var atWar []int
	for id := range w.G.Players {
		if w.Hostile(id) && len(c.border(id)) > 0 {
			atWar = append(atWar, id)
		}
	}
	sort.Ints(atWar)
	for _, id := range atWar {
		if id == mem.WarTarget {
			return c.honorCheck(id)
		}
	}
	if len(atWar) > 0 {
		mem.WarTarget = atWar[0]
		c.plan.note(CatAttack, "цель-войны", c.nation(atWar[0]),
			"уже воюем, и у нас общая граница — берём его провинции")
		return c.honorCheck(atWar[0])
	}
	if mem.WarTarget != 0 {
		c.plan.note(CatAttack, "цель-войны", c.nation(mem.WarTarget), "общей границы больше нет — выбираем новую цель")
		mem.WarTarget = 0
	}

	// Новая война — только когда всё спокойно и есть чем воевать.
	idle := c.idlePower()
	switch {
	case w.G.Day < r.WarFromDay && c.weakest().share < r.WeakShare:
		c.plan.note(CatAttack, "объявить-войну", "война", fmt.Sprintf(
			"до %d-го дня копим силы (сейчас %d-й); раньше напали бы только на соседа, потерявшего по газете %.0f%% армии",
			r.WarFromDay, w.G.Day, r.WeakShare*100))
		return 0
	case !w.G.PeaceAIUntil.IsZero() && w.Now.Before(w.G.PeaceAIUntil):
		c.plan.note(CatAttack, "объявить-войну", "война", "с ботами мирный период до "+clock(w.G.PeaceAIUntil, w.Now))
		c.plan.wake(w.G.PeaceAIUntil.Add(time.Minute), "кончится мирный период")
		return 0
	case c.pressed:
		c.plan.note(CatAttack, "объявить-войну", "война", "сначала отбиваемся: оборона не справляется")
		return 0
	case idle < r.MinWarPower:
		c.plan.note(CatAttack, "объявить-войну", "война",
			fmt.Sprintf("свободных сил %.0f, для новой войны нужно хотя бы %.0f", idle, r.MinWarPower))
		return 0
	}

	type option struct {
		id           int
		value, guard float64
		weakest      float64
		provinces    int
		wk           weakness
		score        float64
	}
	var options []option
	for _, id := range c.warCandidates() {
		b := c.border(id)
		o := option{id: id, provinces: len(b), weakest: -1, wk: w.weaknessOf(id, r, mem)}
		for _, p := range b {
			o.value += w.Value(p)
			g := c.guard(p, id)
			o.guard += g
			if o.weakest < 0 || g < o.weakest {
				o.weakest = g
			}
		}
		// Ослабший по газете стоит дороже: резервов за его границей нет.
		o.score = o.value / (o.guard + 10) * (1 + r.WeakBonus*o.wk.share)
		options = append(options, o)
	}
	if len(options) == 0 {
		c.plan.note(CatAttack, "объявить-войну", "война", "у нас нет общей границы ни с одним ботом")
		return 0
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].score != options[j].score {
			return options[i].score > options[j].score
		}
		return options[i].id < options[j].id
	})
	best := options[0]
	if idle < best.weakest*r.AttackRatio {
		c.plan.note(CatAttack, "объявить-войну", c.nation(best.id),
			fmt.Sprintf("лучшая цель, но даже слабейшую её провинцию (оборона %.0f) не взять: свободных сил %.0f", best.weakest, idle))
		return 0
	}
	var others []string
	for _, o := range options[1:] {
		others = append(others, fmt.Sprintf("%s %.1f", c.nation(o.id), o.score))
	}
	why := fmt.Sprintf("бот-сосед: %d пограничных провинций ценностью %.1f, видимая оборона %.0f; свободных сил %.0f",
		best.provinces, best.value, best.guard, idle)
	if best.wk.lost > 0 {
		why += fmt.Sprintf("; по газете потери %s из ~%s стартовой армии (%.0f%%)",
			num(float64(best.wk.lost)), num(best.wk.start), best.wk.share*100)
	}
	if w.G.Day < r.WarFromDay {
		why += fmt.Sprintf("; до %d-го дня не воюем, но он ослаб — нападаем раньше", r.WarFromDay)
	}
	why += fmt.Sprintf(". Оценка %.1f", best.score)
	if len(others) > 0 {
		why += "; другие: " + strings.Join(others, ", ")
	}
	c.plan.add(Decision{Category: CatDiplomacy, Rule: "объявить-войну", What: c.nation(best.id) + ": война",
		Why: why, Action: s1914.RelationAction(best.id, relWar)})
	c.plan.add(Decision{Category: CatAttack, Rule: "объявить-войну", What: c.nation(best.id) + ": война", Why: why})
	mem.WarTarget, mem.WarSince = best.id, w.Now
	// Война объявлена: дальше в этом заходе бот — враг, и по его земле
	// прокладываем пути заново.
	if w.declared == nil {
		w.declared = map[int]bool{}
	}
	w.declared[best.id] = true
	c.router = s1914.NewRouter(w.Geo, w.routeCost())
	for _, f := range c.forces {
		f.done, f.paths, f.ok = false, nil, false
	}
	// С периодом чести нападать сразу нельзя; без него — в этом же заходе.
	if w.G.HonorPeriod {
		c.plan.note(CatAttack, "объявить-войну", c.nation(best.id), "в партии период чести: нападём, когда он кончится")
		return 0
	}
	return best.id
}

// warCandidates — кому вообще можно объявить войну: живые боты (и люди,
// если разрешено) не из нашей коалиции и с общей с нами границей.
func (c *commander) warCandidates() []int {
	w, r := c.w, c.r
	var out []int
	for id, pl := range w.G.Players {
		if id == w.G.Me || pl.Defeated || pl.Retired {
			continue
		}
		if !pl.NativeAI && !(r.WarOnHumans && !pl.IsAI) {
			continue
		}
		if t := w.G.Players[w.G.Me].TeamID; t != 0 && t == pl.TeamID {
			continue
		}
		if len(c.border(id)) > 0 {
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out
}

// weakest — самый ослабший по газете из тех, кому можно объявить войну.
func (c *commander) weakest() weakness {
	var best weakness
	for _, id := range c.warCandidates() {
		if wk := c.w.weaknessOf(id, c.r, c.mem); wk.share > best.share {
			best = wk
		}
	}
	return best
}

// honorCheck — можно ли уже нападать на игрока.
func (c *commander) honorCheck(id int) int {
	w := c.w
	if !w.G.HonorPeriod {
		return id
	}
	end, ok := w.G.HonorEnd[id]
	if !ok || !w.Now.Before(end) {
		return id
	}
	c.plan.note(CatAttack, "период-чести", c.nation(id), "нападать можно с "+clock(end, w.Now))
	c.plan.wake(end.Add(time.Minute), "кончится период чести")
	return 0
}

// conquer — этап 4–5: берём провинции врага у нашей границы.
func (c *commander) conquer(enemy int) {
	if enemy == 0 {
		return
	}
	w, r := c.w, c.r
	type target struct {
		p     s1914.Province
		guard float64
		score float64
	}
	var targets []target
	for _, p := range c.border(enemy) {
		g := c.guard(p, enemy)
		targets = append(targets, target{p: p, guard: g, score: w.Value(p) / (g + 5)})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].score != targets[j].score {
			return targets[i].score > targets[j].score
		}
		return targets[i].p.ID < targets[j].p.ID
	})
	ops := 0
	for _, t := range targets {
		if ops >= r.MaxOps {
			break
		}
		// Туда уже идут наши армии — второй раз не посылаем.
		if c.ourAt[t.p.ID] > 0 && c.ourAt[t.p.ID] >= t.guard*r.AttackRatio {
			continue
		}
		cands := c.nearest(t.p.ID, r.StrikeHops, false)
		cands = c.keepCapital(cands)
		what := fmt.Sprintf("%s (%s)", t.p.Name, c.nation(enemy))
		if len(cands) == 0 {
			c.plan.noteAt(t.p.ID, CatAttack, "захват", what, "свободных армий в досягаемости нет")
			continue
		}
		need := t.guard * r.AttackRatio
		var picked []candidate
		var sum float64
		if t.guard <= 0 {
			// Пустую провинцию берёт одна армия — самая слабая из
			// ближайших, сильные пригодятся там, где бьются.
			near := cands[0].cost * 1.5
			pick := cands[0]
			for _, cand := range cands {
				if cand.cost <= near && cand.f.power < pick.f.power {
					pick = cand
				}
			}
			picked, sum = []candidate{pick}, pick.f.power
		} else {
			// Стак у цели уже собирается — ждём, пока все дойдут.
			if stage := c.stagingPoint(t.p.ID); r.Stacks && stage != 0 {
				if here := c.freeAt(stage); here < need && c.ourAt[stage] >= need {
					c.hold(stage)
					ops++
					c.plan.noteAt(stage, CatAttack, "сбор-стака", what, fmt.Sprintf(
						"стак в %s: на месте %.0f, с подходящими %.0f из нужных %.0f — бьём, когда соберутся",
						c.name(stage), here, c.ourAt[stage], need))
					continue
				}
			}
			var ok bool
			picked, sum, ok = gather(cands, need)
			// Стаку, которому сил хватает одному, помощники не нужны.
			if r.Stacks {
				for _, cand := range cands {
					if cand.f.power >= need {
						picked, sum, ok = []candidate{cand}, cand.f.power, true
						break
					}
				}
			}
			if !ok {
				c.plan.noteAt(t.p.ID, CatAttack, "захват", what,
					fmt.Sprintf("оборона %.0f, нужно %.0f, в досягаемости только %.0f", t.guard, need, sum))
				continue
			}
			if r.Stacks && !oneSpot(picked) {
				if c.stack(t.p, picked, sum, need, what) {
					ops++
					continue
				}
			}
		}
		ops++
		why := fmt.Sprintf("ценность %.1f, оборона %.0f; идём силой %.0f", w.Value(t.p), t.guard, sum)
		if t.guard <= 0 {
			why = fmt.Sprintf("ценность %.1f, видимой обороны нет — берём одной армией", w.Value(t.p))
		}
		var enemyHere []s1914.Army
		for _, a := range c.enemyAt[t.p.ID] {
			if a.Owner == enemy {
				enemyHere = append(enemyHere, a)
			}
		}
		for _, cand := range picked {
			c.order(CatAttack, "захват", cand, t.p.ID, enemyHere, why)
		}
	}
}

// Стаки. Армии, пришедшие в одну точку, игра сливает в одну (кроме армий
// с героями — их у бота нет). Большой стак теряет в бою меньше и бьёт
// сильнее, чем те же войска, пришедшие к цели по одной. Поэтому, когда на
// цель нужно несколько армий из разных мест, бот сначала сводит их в нашу
// провинцию рядом с целью и только следующим заходом бьёт оттуда одной
// армией.

// oneSpot — все армии стоят в одной точке: это уже один стак.
func oneSpot(picked []candidate) bool {
	for _, cand := range picked[1:] {
		if cand.f.a.Pos != picked[0].f.a.Pos {
			return false
		}
	}
	return true
}

// stagingPoint — где собирать стак на цель: наша провинция по соседству
// с ней, по возможности не под угрозой, и та, где наших сил уже больше
// всего (стоящих и идущих туда). Так сбор сходится в одну точку из захода
// в заход. Ноль — такой провинции нет.
func (c *commander) stagingPoint(target int) int {
	best, bestPower, bestSafe := 0, -1.0, false
	for _, nb := range c.w.Geo.LandNeighbors(target) {
		p, ok := c.w.Province(nb)
		if !ok || p.Owner != c.w.G.Me {
			continue
		}
		if _, ok := c.w.Geo.Centers[nb]; !ok {
			continue
		}
		safe, power := !c.danger[nb], c.ourAt[nb]
		better := best == 0 || (safe && !bestSafe) ||
			(safe == bestSafe && (power > bestPower || power == bestPower && nb < best))
		if better {
			best, bestPower, bestSafe = nb, power, safe
		}
	}
	return best
}

// freeAt — сила наших свободных армий, стоящих в провинции.
func (c *commander) freeAt(province int) float64 {
	s := 0.0
	for _, f := range c.forces {
		if f.a.Location == province && c.free(f, true) {
			s += f.power
		}
	}
	return s
}

// hold — армии в провинции ждут стак: другим приказам их не отдаём.
func (c *commander) hold(province int) {
	for _, f := range c.forces {
		if f.a.Location == province && c.free(f, true) {
			c.used[f.a.ID] = true
		}
	}
}

// stack — свести армии в точку сбора у цели. Ложь — точки нет или до неё
// никто не дойдёт; тогда бьём как есть.
func (c *commander) stack(target s1914.Province, picked []candidate, sum, need float64, what string) bool {
	stage := c.stagingPoint(target.ID)
	if stage == 0 {
		return false
	}
	to := c.w.Geo.Centers[stage]
	type move struct {
		cand candidate
		path []s1914.MapPoint
		cost float64
	}
	var moves []move
	moving := 0.0
	for _, cand := range picked {
		if cand.f.a.Location == stage && cand.f.a.Pos == to {
			continue
		}
		paths, ok := c.pathsOf(cand.f)
		if !ok {
			return false
		}
		cost, ok := paths.Cost(to)
		path, ok2 := paths.To(to)
		if !ok || !ok2 {
			return false
		}
		moves = append(moves, move{cand, path, cost})
		moving += cand.f.power
	}
	c.hold(stage)
	why := fmt.Sprintf("цель %s: оборона %.0f, нужно %.0f, набрали %.0f из %d армий в разных местах. "+
		"Сводим их в %s — там сольются в один стак, он теряет меньше и бьёт сильнее; ударим следующим заходом",
		target.Name, need/c.r.AttackRatio, need, sum, len(picked), c.name(stage))
	for _, m := range moves {
		c.order(CatAttack, "сбор-стака", candidate{f: m.cand.f, path: m.path, cost: m.cost}, stage, nil, why)
	}
	c.ourAt[stage] += moving
	c.plan.noteAt(stage, CatAttack, "сбор-стака", what, why)
	return true
}

// keepCapital — столицу не оставляем без войск: её последнюю армию
// в наступление не берём.
func (c *commander) keepCapital(cands []candidate) []candidate {
	capital, ok := c.w.Capital()
	if !ok {
		return cands
	}
	there := 0
	for _, f := range c.forces {
		if f.a.Location == capital.ID && f.a.MarchTo == nil && !c.used[f.a.ID] {
			there++
		}
	}
	if there > 1 {
		return cands
	}
	var out []candidate
	for _, cand := range cands {
		if cand.f.a.Location != capital.ID {
			out = append(out, cand)
		}
	}
	return out
}
