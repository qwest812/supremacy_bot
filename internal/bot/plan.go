package bot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"supermessy_bot/internal/s1914"
)

// Category — в какой журнал идёт решение. Общий журнал получает все.
type Category string

const (
	CatBuild     Category = "build"     // строительство зданий
	CatRecruit   Category = "recruit"   // призыв и производство войск
	CatAttack    Category = "attack"    // объявление войны, захваты, атаки
	CatDefense   Category = "defense"   // угрозы, подкрепления, отступление
	CatMarket    Category = "market"    // торговля
	CatDiplomacy Category = "diplomacy" // отношения с игроками
)

// Title — подпись категории в журнале.
func (c Category) Title() string {
	switch c {
	case CatBuild:
		return "СТРОЙКА"
	case CatRecruit:
		return "ВОЙСКА"
	case CatAttack:
		return "АТАКА"
	case CatDefense:
		return "ОБОРОНА"
	case CatMarket:
		return "РЫНОК"
	case CatDiplomacy:
		return "ДИПЛОМАТИЯ"
	}
	return strings.ToUpper(string(c))
}

// Decision — одно решение бота: что делаем и почему. Action пустой у
// заметок — решений «не делаем, потому что…»: они тоже нужны, чтобы было
// видно, почему бот стоит.
type Decision struct {
	Category Category
	// Rule — какое правило сработало, коротким именем: по нему решения
	// удобно искать в журнале.
	Rule string
	// What — что именно: «Берлин: Пункт призыва».
	What string
	// Why — почему, словами и с цифрами.
	Why    string
	Action map[string]any
	Cost   map[int]float64
	// Province — к какой провинции решение относится (для карты): где
	// строим, куда идёт армия. Ноль — ни к какой. Army и Path — армия
	// и её путь, если это приказ армии.
	Province int
	Army     int64
	Path     []s1914.MapPoint
	// Enemy — вражеская армия, на которую нападаем.
	Enemy int64
}

// IsNote — решение без действия.
func (d Decision) IsNote() bool { return d.Action == nil }

// Plan — всё, что бот решил за один заход.
type Plan struct {
	Decisions []Decision
	// Next — когда стоит зайти снова, и почему именно тогда.
	Next    time.Time
	NextWhy string
}

func (p *Plan) add(d Decision) { p.Decisions = append(p.Decisions, d) }

func (p *Plan) note(cat Category, rule, what, why string) {
	p.add(Decision{Category: cat, Rule: rule, What: what, Why: why})
}

// noteAt — заметка о конкретной провинции.
func (p *Plan) noteAt(province int, cat Category, rule, what, why string) {
	p.add(Decision{Category: cat, Rule: rule, What: what, Why: why, Province: province})
}

// wake — запомнить событие, к которому стоит вернуться.
func (p *Plan) wake(at time.Time, why string) {
	if at.IsZero() {
		return
	}
	if p.Next.IsZero() || at.Before(p.Next) {
		p.Next, p.NextWhy = at, why
	}
}

// Actions — решения с действием.
func (p *Plan) Actions() []Decision {
	var out []Decision
	for _, d := range p.Decisions {
		if !d.IsNote() {
			out = append(out, d)
		}
	}
	return out
}

// Budget — ресурсы одного захода. Каждое решение платит из общего запаса,
// иначе вторая стройка полезла бы в те же деньги, что уже отданы первой.
// Reserve откладывает ресурсы под будущее (фабрику): тратить их нельзя
// никому, кроме того, ради чего отложено.
type Budget struct {
	w        *World
	spent    map[int]float64
	reserved map[int]float64
	// Lacks — чего не хватило решениям этого захода: по этому рынок
	// решает, что покупать.
	Lacks map[int]float64
}

func newBudget(w *World) *Budget {
	return &Budget{w: w, spent: map[int]float64{}, reserved: map[int]float64{}, Lacks: map[int]float64{}}
}

// Free — сколько ресурса можно тратить прямо сейчас.
func (b *Budget) Free(res int) float64 {
	return b.w.Amount(res) - b.spent[res] - b.reserved[res]
}

// Check — хватает ли на cost. Если нет — какого ресурса и сколько.
// useReserve разрешает брать из отложенного (тому, ради кого отложено).
func (b *Budget) Check(cost map[int]float64, useReserve bool) (bool, int, float64) {
	worstRes, worstLack := -1, 0.0
	for _, res := range sortedRes(cost) {
		need := cost[res]
		if need <= 0 {
			continue
		}
		if _, known := b.w.G.Resources[res]; !known {
			continue // игра не назвала — не стопорим
		}
		have := b.Free(res)
		if useReserve {
			have += b.reserved[res]
		}
		if lack := need - have; lack > worstLack {
			worstRes, worstLack = res, lack
		}
	}
	return worstRes < 0, worstRes, worstLack
}

// Pay — списать cost с запаса захода.
func (b *Budget) Pay(cost map[int]float64, fromReserve bool) {
	for res, v := range cost {
		if fromReserve {
			take := min(v, b.reserved[res])
			b.reserved[res] -= take
			v -= take
		}
		b.spent[res] += v
	}
}

// Reserve — отложить cost.
func (b *Budget) Reserve(cost map[int]float64) {
	for res, v := range cost {
		b.reserved[res] += v
	}
}

// noteLack — запомнить нехватку для рынка.
func (b *Budget) noteLack(res int, lack float64) {
	if res >= 0 && lack > b.Lacks[res] {
		b.Lacks[res] = lack
	}
}

// When — когда хватит ресурса res на need сверх уже потраченного. Ложь —
// не накопится никогда (прирост не положительный).
func (b *Budget) When(res int, lack float64) (time.Time, bool) {
	r, ok := b.w.G.Resources[res]
	if !ok || r.Rate <= 0 {
		return time.Time{}, false
	}
	secs := lack / r.Rate
	return b.w.Now.Add(time.Duration(secs * float64(time.Second))), true
}

// costText — цена словами: «руда 250, дерево 250, деньги 1000».
func costText(w *World, cost map[int]float64) string {
	var parts []string
	for _, res := range sortedRes(cost) {
		if cost[res] > 0 {
			parts = append(parts, fmt.Sprintf("%s %s", w.ResName(res), num(cost[res])))
		}
	}
	return strings.Join(parts, ", ")
}

func sortedRes(m map[int]float64) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// num — число для журнала: целое, с пробелами по тысячам.
func num(v float64) string {
	n := int64(v + 0.5)
	if v < 0 {
		n = int64(v - 0.5)
	}
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(ch)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func itoa(n int) string { return strconv.Itoa(n) }

// clock — время для журнала: «15:04», а не сегодня — «25.10 15:04».
func clock(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	if t.Format("2006-01-02") == now.Format("2006-01-02") {
		return t.Format("15:04")
	}
	return t.Format("02.01 15:04")
}

// realDur — длительность для журнала: «12 мин», «2 ч 5 мин».
func realDur(d time.Duration) string {
	if d < time.Minute {
		return "меньше минуты"
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h == 0 {
		return fmt.Sprintf("%d мин", m)
	}
	return fmt.Sprintf("%d ч %d мин", h, m)
}
