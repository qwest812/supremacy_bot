package s1914

// Router — тот же поиск пути, что FindRoute, но для многих армий и целей
// разом: граф дорог строится один раз, а от каждой армии одним проходом
// Дейкстры считаются пути до всех точек сразу. Боту это нужно, чтобы
// выбрать, какая армия ближе к какой цели, не строя граф заново на каждую
// пару.

import (
	"container/heap"
	"math"
)

type Router struct {
	graph  map[nodeKey][]routeEdge
	points map[nodeKey]MapPoint
	roads  []Road
	cost   func(province int) float64
}

// NewRouter строит граф дорог суши. cost — цена прохода по провинции на
// единицу длины; бесконечная или неположительная закрывает провинцию.
func NewRouter(geo *MapGeometry, cost func(province int) float64) *Router {
	r := &Router{graph: map[nodeKey][]routeEdge{}, points: map[nodeKey]MapPoint{}, cost: cost}
	if geo == nil {
		return r
	}
	for _, road := range geo.Roads {
		price := cost(road.Province)
		if !(price > 0) || math.IsInf(price, 1) {
			continue
		}
		ka, kb := keyOf(road.A), keyOf(road.B)
		r.points[ka], r.points[kb] = road.A, road.B
		w := dist(road.A, road.B) * price
		r.graph[ka] = append(r.graph[ka], routeEdge{to: kb, cost: w})
		r.graph[kb] = append(r.graph[kb], routeEdge{to: ka, cost: w})
		r.roads = append(r.roads, road)
	}
	return r
}

// Paths — пути от одной точки до всех, куда можно дойти.
type Paths struct {
	r     *Router
	from  MapPoint
	start nodeKey
	best  map[nodeKey]float64
	prev  map[nodeKey]nodeKey
}

// From считает пути от точки from. Армия на марше стоит посреди дороги —
// тогда пути начинаются с обоих концов её отрезка, как у FindRoute.
// Ложь — точка не на дороге.
func (r *Router) From(from MapPoint) (*Paths, bool) {
	p := &Paths{r: r, from: from, start: keyOf(from), best: map[nodeKey]float64{}, prev: map[nodeKey]nodeKey{}}
	q := &routeQueue{}
	if _, ok := r.graph[p.start]; ok {
		p.best[p.start] = 0
		heap.Push(q, routeItem{key: p.start})
	} else {
		nearest, onRoad := math.Inf(1), Road{}
		for _, road := range r.roads {
			if d := segmentDist(from, road.A, road.B); d < nearest {
				nearest, onRoad = d, road
			}
		}
		if nearest > routeTolerance {
			return nil, false
		}
		price := r.cost(onRoad.Province)
		for _, end := range []MapPoint{onRoad.A, onRoad.B} {
			k := keyOf(end)
			c := dist(from, end) * price
			if old, ok := p.best[k]; !ok || c < old {
				p.best[k], p.prev[k] = c, p.start
				heap.Push(q, routeItem{key: k, cost: c})
			}
		}
	}
	for q.Len() > 0 {
		cur := heap.Pop(q).(routeItem)
		if cur.cost > p.best[cur.key] {
			continue
		}
		for _, e := range r.graph[cur.key] {
			next := cur.cost + e.cost
			if old, seen := p.best[e.to]; seen && old <= next {
				continue
			}
			p.best[e.to], p.prev[e.to] = next, cur.key
			heap.Push(q, routeItem{key: e.to, cost: next})
		}
	}
	return p, true
}

// Cost — цена пути до точки (длина с учётом цены провинций). Ложь — не дойти.
func (p *Paths) Cost(to MapPoint) (float64, bool) {
	k := keyOf(to)
	if k == p.start {
		return 0, true
	}
	c, ok := p.best[k]
	return c, ok
}

// To — путь до точки: первая точка — откуда вышли.
func (p *Paths) To(to MapPoint) ([]MapPoint, bool) {
	goal := keyOf(to)
	if goal == p.start {
		return []MapPoint{p.from}, true
	}
	if _, ok := p.best[goal]; !ok {
		return nil, false
	}
	var path []MapPoint
	for k := goal; ; {
		if k == p.start {
			path = append(path, p.from)
			break
		}
		path = append(path, p.r.points[k])
		prev, ok := p.prev[k]
		if !ok {
			return nil, false
		}
		k = prev
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, true
}
