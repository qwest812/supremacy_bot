package bot

// Силы и отношения: кто нам враг, чья земля проходима и сколько весит армия.
//
// Отношение по умолчанию у игры — мир (getRelation у клиента). Война идёт,
// если хотя бы одна сторона выставила войну (isAtWar смотрит обе).
// Пройти по чужой земле можно, только если хозяин дал нам право прохода
// (его отношение к нам ≥ 3, hasRightOfWay у клиента), или если мы с ним
// воюем — тогда это и есть наступление.

import (
	"math"

	"supermessy_bot/internal/s1914"
)

const (
	relWar   = -2
	relPeace = 1
)

// relTo — наше отношение к игроку, relFrom — его к нам.
func (w *World) relTo(p int) int {
	if r, ok := w.G.Relations[p]; ok {
		return r
	}
	return relPeace
}

func (w *World) relFrom(p int) int {
	if r, ok := w.G.TheirRelations[p]; ok {
		return r
	}
	return relPeace
}

// Hostile — мы с игроком воюем.
func (w *World) Hostile(p int) bool {
	if p <= 0 || p == w.G.Me {
		return false
	}
	if pl, ok := w.G.Players[p]; ok && (pl.Defeated || pl.Retired) {
		return false
	}
	if w.declared[p] {
		return true
	}
	return w.relTo(p) <= relWar || w.relFrom(p) <= relWar
}

// CanEnter — может ли наша армия войти в провинцию этого владельца.
func (w *World) CanEnter(owner int) bool {
	return owner <= 0 || owner == w.G.Me || w.Hostile(owner) || w.relFrom(owner) >= s1914.RelationRightOfWay
}

// unitPower — сила одного юнита: оценка самой игры (powerLevel), а без неё —
// среднее атаки и защиты.
func (w *World) unitPower(t int) float64 {
	s := w.G.UnitStats[t]
	if s.Power > 0 {
		return s.Power
	}
	if v := (s.Strength + s.Defence) / 2; v > 0 {
		return v
	}
	return 1
}

// Power — сила армии.
func (w *World) Power(a s1914.Army) float64 {
	p := 0.0
	for _, u := range a.Units {
		p += float64(u.Size) * w.unitPower(u.Type)
	}
	return p
}

// landArmy — армия только из сухопутных войск и героев, стоящая на суше:
// такую водим по дорогам. Флот, авиацию и войска на кораблях — нет.
func (w *World) landArmy(a s1914.Army) bool {
	if len(a.Units) == 0 {
		return false
	}
	for _, u := range a.Units {
		if set := w.G.UnitSets[u.Type]; set != 1 && set != 4 {
			return false
		}
	}
	if w.Geo == nil {
		return false
	}
	_, onLand := w.Geo.Centers[a.Location]
	return onLand
}

// fortMult — во сколько раз стоящим в провинции легче обороняться:
// крепость каждого уровня — плюс четверть. Грубо, зато с запасом.
func (w *World) fortMult(p s1914.Province) float64 {
	tier, _ := w.Has(p, bFortress)
	return 1 + 0.25*float64(tier)
}

// Value — чего стоит провинция: добыча (двойная — вдвое ценнее), очки
// победы, столица.
func (w *World) Value(p s1914.Province) float64 {
	v := w.BaseOutput(p) / 1000
	if w.IsDouble(p) {
		v *= 2
	}
	v += float64(p.VictoryPoints) / 2
	if p.Capital {
		v += 5
	}
	return max(v, 0.5)
}

// hop — среднее расстояние между центрами соседних провинций: мерило
// «на сколько провинций армия дотянется».
func (w *World) hop() float64 {
	if w.Geo == nil {
		return 50
	}
	sum, n := 0.0, 0
	for id, c := range w.Geo.Centers {
		for _, nb := range w.Geo.LandNeighbors(id) {
			if d, ok := w.Geo.Centers[nb]; ok {
				sum += math.Hypot(c.X-d.X, c.Y-d.Y)
				n++
			}
		}
	}
	if n == 0 {
		return 50
	}
	return sum / float64(n)
}

// routeCost — цена прохода по провинции для наших армий: своя дешевле
// всего, по праву прохода — чуть дороже, вражеская втрое (сервер считает
// там скорость ниже, а ещё там бьют). Куда войти нельзя — закрыто.
func (w *World) routeCost() func(int) float64 {
	return func(id int) float64 {
		p, ok := w.byID[id]
		if !ok {
			return 1.5
		}
		switch {
		case p.Owner == w.G.Me:
			return 1
		case w.Hostile(p.Owner):
			return 3
		case w.CanEnter(p.Owner):
			return 1.5
		}
		return math.Inf(1)
	}
}
