// Package bot — «мозг» и цикл работы бота Supremacy 1914: по состоянию
// партии решает, что делать, объясняет каждое решение и отдаёт приказы.
//
// Решения отделены от сети: планировщик получает World и возвращает план,
// а отправкой занимается Runner. Поэтому правила проверяются тестами
// на записанной партии, без захода в игру.
package bot

import (
	"sort"
	"time"

	"supermessy_bot/internal/s1914"
)

// Имена зданий у игры (поле «ap» справочника строчными буквами). По имени,
// а не по номеру: номера в разных сценариях разные, а имена одни.
const (
	bRecruiting = "recruiting_office"
	bWorkshop   = "workshop"
	bBarracks   = "barracks"
	bHarbour    = "harbour"
	bRailway    = "railway"
	bFactory    = "factory"
	bFortress   = "fortress"
)

// Имена типов юнитов у игры (identifier строчными).
const (
	uInfantry      = "infantry"
	uCar           = "car"
	uCavalry       = "cavalry"
	uStormtroopers = "stormtroopers"
)

// World — партия глазами бота на один заход: состояние, карта и всё, что
// из них удобно спрашивать.
type World struct {
	G   *s1914.GameState
	Geo *s1914.MapGeometry
	Now time.Time

	mine  []s1914.Province
	byID  map[int]s1914.Province
	units map[string]int // имя типа юнита → номер
	// Losses — потери в войнах по газете партии. Пусто — газету
	// не прочитали (или бот разбирает записанную партию без сети).
	Losses *s1914.Losses

	// declared — кому мы объявили войну в этот заход: в состоянии это
	// появится только в следующий, а воевать можно уже сейчас.
	declared map[int]bool
}

// NewWorld собирает модель партии. geo может быть nil: тогда о соседях,
// побережье и базовой добыче судим по самому состоянию.
func NewWorld(g *s1914.GameState, geo *s1914.MapGeometry, now time.Time) *World {
	w := &World{G: g, Geo: geo, Now: now, byID: make(map[int]s1914.Province, len(g.Provinces)),
		units: map[string]int{}}
	for _, p := range g.Provinces {
		w.byID[p.ID] = p
		if p.Owner == g.Me && g.Me > 0 {
			w.mine = append(w.mine, p)
		}
	}
	for id, u := range g.UnitStats {
		if _, dup := w.units[u.Ident]; !dup || id < w.units[u.Ident] {
			w.units[u.Ident] = id
		}
	}
	return w
}

// Mine — наши провинции.
func (w *World) Mine() []s1914.Province { return w.mine }

// Province — провинция по номеру.
func (w *World) Province(id int) (s1914.Province, bool) {
	p, ok := w.byID[id]
	return p, ok
}

// Unit — номер типа юнита по имени у игры; ноль — такого нет.
func (w *World) Unit(ident string) int { return w.units[ident] }

// BaseOutput — базовая добыча главного ресурса провинции: из файла карты,
// а если его нет — из состояния.
func (w *World) BaseOutput(p s1914.Province) float64 {
	res := p.MainResource()
	if w.Geo != nil && res >= 0 {
		if v, ok := w.Geo.Production[p.ID][res]; ok {
			return v
		}
	}
	return p.BaseOutput()
}

// IsDouble — «дабл рес»: провинция с двойной добычей. Игра решает так же
// (Province.isDoubleProduction): базовая добыча не ниже порога сценария.
func (w *World) IsDouble(p s1914.Province) bool {
	return w.BaseOutput(p) >= w.G.DoubleThreshold
}

// Coastal — у провинции есть выход к морю. Без файла карты не знаем —
// считаем, что нет: порт тогда просто не предложим.
func (w *World) Coastal(p s1914.Province) bool {
	return w.Geo != nil && w.Geo.Coastal[p.ID]
}

// Capital — наша столица.
func (w *World) Capital() (s1914.Province, bool) {
	for _, p := range w.mine {
		if p.Capital {
			return p, true
		}
	}
	return s1914.Province{}, false
}

// Family — уровни здания одного вида, от первого к последнему.
func (w *World) Family(image string) []s1914.Upgrade {
	var out []s1914.Upgrade
	for _, u := range w.G.Upgrades {
		if u.Image == image {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier < out[j].Tier
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Has — какой уровень здания этого вида стоит в провинции (0 — никакого)
// и номер этого уровня. Недостроенный уровень тоже считается: его
// достраивают тем же приказом.
func (w *World) Has(p s1914.Province, image string) (tier, id int) {
	for _, b := range p.Built {
		u, ok := w.G.Upgrades[b]
		if ok && u.Image == image && u.Tier > tier {
			tier, id = u.Tier, b
		}
	}
	return tier, id
}

// HasFinished — стоит ли в провинции готовое здание этого вида.
func (w *World) HasFinished(p s1914.Province, image string) bool {
	_, id := w.Has(p, image)
	return id != 0 && w.G.Finished(p, id)
}

// Building — строится ли сейчас в провинции здание этого вида.
func (w *World) Building(p s1914.Province, image string) bool {
	for _, c := range p.Building {
		if w.G.Upgrades[c.UpgradeID].Image == image {
			return true
		}
	}
	return false
}

// FirstTier — первый уровень здания этого вида.
func (w *World) FirstTier(image string) (s1914.Upgrade, bool) {
	f := w.Family(image)
	if len(f) == 0 {
		return s1914.Upgrade{}, false
	}
	return f[0], true
}

// Amount — сколько ресурса у нас сейчас.
func (w *World) Amount(res int) float64 {
	r, ok := w.G.Resources[res]
	if !ok {
		return 0
	}
	return r.AmountAt(w.Now)
}

// Daily — прирост ресурса за игровые сутки (уже за вычетом расходов).
func (w *World) Daily(res int) float64 { return w.G.Resources[res].Daily }

// ResName — название ресурса.
func (w *World) ResName(res int) string {
	if r, ok := w.G.Resources[res]; ok && r.Name != "" {
		return r.Name
	}
	if n, ok := w.G.ResourceNames[res]; ok {
		return n
	}
	return "ресурс " + itoa(res)
}

// MyArmies — наши армии.
func (w *World) MyArmies() []s1914.Army {
	var out []s1914.Army
	for _, a := range w.G.Armies {
		if a.Owner == w.G.Me {
			out = append(out, a)
		}
	}
	return out
}

// UnitCount — сколько у нас юнитов этого типа во всех армиях.
func (w *World) UnitCount(unit int) int {
	n := 0
	for _, a := range w.MyArmies() {
		for _, u := range a.Units {
			if u.Type == unit {
				n += u.Size
			}
		}
	}
	return n
}

// NativeBots — компьютерные страны с начала партии, живые.
func (w *World) NativeBots() []s1914.Player {
	var out []s1914.Player
	for id, p := range w.G.Players {
		if id != w.G.Me && p.NativeAI && !p.Defeated && !p.Retired {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GameDay — сколько настоящего времени в игровых сутках.
func (w *World) GameDay() time.Duration {
	if w.G.DayLength > 0 {
		return w.G.DayLength
	}
	return 24 * time.Hour
}
