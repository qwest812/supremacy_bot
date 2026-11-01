package bot

import (
	"strings"
	"testing"

	"supermessy_bot/internal/s1914"
)

// market — запасы и биржа для торговых тестов: всего по amount, прирост
// нулевой, справочные цены 5 за единицу.
func market(w *World, amount float64, asks ...s1914.Order) {
	for id, r := range w.G.Resources {
		r.Amount, r.Measured, r.Rate, r.Daily = amount, w.Now, 0, 0
		if id == s1914.ResourceMoney {
			r.Amount = 1e6
		}
		w.G.Resources[id] = r
	}
	w.G.Market = s1914.Market{Prices: map[int]float64{s1914.ResourceMoney: 1}, Asks: asks}
	for _, res := range sellOrder {
		w.G.Market.Prices[res] = 5
	}
}

func marketOnly(w *World, mem *Memory) *Plan {
	plan := &Plan{}
	planMarket(w, newBudget(w), DefaultRules(), mem, plan)
	return plan
}

// В начале партии руды и дерева не хватает всем: впрок берём дёшево.
func TestStrategicIronBoughtCheap(t *testing.T) {
	w := fixtureWorld(t)
	w.G.Day = 2
	market(w, 1000, s1914.Order{ID: 1, Resource: s1914.ResourceIron, Amount: 10000, Limit: 4.5, Player: 99})
	plan := marketOnly(w, freshMem())
	buys := find(plan, "купить-на-бирже", true)
	if len(buys) != 1 || !strings.Contains(buys[0].Why, "руды и дерева") {
		t.Fatalf("руду впрок не купили: %+v", buys)
	}
	// Дорогую — впрок не берём.
	market(w, 1000, s1914.Order{ID: 1, Resource: s1914.ResourceIron, Amount: 10000, Limit: 5.5, Player: 99})
	if buys := find(marketOnly(w, freshMem()), "купить-на-бирже", true); len(buys) != 0 {
		t.Fatalf("впрок купили дороже справочной: %s", buys[0].What)
	}
}

// С OilFromDay в дефиците нефть, а не руда.
func TestOilIsStrategicLater(t *testing.T) {
	w := fixtureWorld(t)
	w.G.Day = DefaultRules().OilFromDay
	market(w, 1000)
	needs := deficits(w, newBudget(w), DefaultRules())
	if !needed(needs, s1914.ResourceOil) || needed(needs, s1914.ResourceIron) {
		t.Fatalf("дефицит: %+v", needs)
	}
}

// Не купили — меняем излишек на дефицит; за деньги умеренный излишек
// не продаём.
func TestSwapBeforeSelling(t *testing.T) {
	w := fixtureWorld(t)
	w.G.Day = 2
	r := DefaultRules()
	market(w, r.KeepStock+r.SellAbove-500)
	// Руды и дерева мало, угля — излишек.
	for _, res := range []int{s1914.ResourceIron, s1914.ResourceWood} {
		x := w.G.Resources[res]
		x.Amount = 1000
		w.G.Resources[res] = x
	}
	plan := marketOnly(w, freshMem())
	if sells := find(plan, "продажа-ботам", true); len(sells) != 0 {
		t.Fatalf("продали за деньги излишек меньше порога: %s", sells[0].What)
	}
	swaps := find(plan, "обмен", true)
	if len(swaps) == 0 {
		t.Fatal("обмена на дефицит нет")
	}
	for _, d := range swaps {
		for res := range d.Cost {
			if res == s1914.ResourceIron || res == s1914.ResourceWood {
				t.Errorf("в обмен отдаём дефицитное: %s", d.What)
			}
		}
	}
}

// Большой излишек, которого никто не просит, продаём ботам.
func TestBigSurplusSold(t *testing.T) {
	w := fixtureWorld(t)
	w.G.Day = 2
	r := DefaultRules()
	market(w, r.KeepStock+r.SellAbove+r.TradeChunk*2)
	plan := marketOnly(w, freshMem())
	if len(find(plan, "продажа-ботам", true)) == 0 {
		t.Fatal("большой излишек не продали")
	}
	if len(find(plan, "обмен", true)) != 0 {
		t.Fatal("обмен без дефицита")
	}
}
