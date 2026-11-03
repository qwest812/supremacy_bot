package bot

// Торговля. Порядок за заход:
//
//  1. Дефицит. Что нам нужно: чего не хватило стройке сейчас; что
//     с отрицательным приростом кончится за ForecastDays суток; и что
//     скоро станет редким на всей карте — в начале партии руда и дерево,
//     с OilFromDay нефть (их держим не меньше StrategicStock).
//  2. Покупка на бирже. Нужное сейчас — не дороже справочной цены
//     ×BuyMarkup; впрок — только дёшево, не дороже ×CheapMarkup.
//  3. Обмен. Чего не купили — просим у живого игрока в обмен на свой
//     излишек, по справочным ценам с небольшой переплатой (SwapRatio),
//     чтобы согласились. Боты прямых сделок не принимают.
//  4. Продажа за деньги — живым игрокам и только того, что лежит сверх
//     KeepStock + SellAbove: излишек сначала нужен на обмен. На бирже
//     не продаём: там теряется 20%.
//
// Предложения нам принимаем, если по справочным ценам получаем заметно
// больше, чем отдаём, или если дают то, чего нам не хватает, не в убыток.
// Кто отказал — тому этот ресурс сутки не предлагаем. Еду не отдаём,
// пока её прирост отрицательный.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"supermessy_bot/internal/s1914"
)

// sellOrder — что отдаём в первую очередь: сырьё, потом еду.
var sellOrder = []int{s1914.ResourceWood, s1914.ResourceIron, s1914.ResourceCoal,
	s1914.ResourceOil, s1914.ResourceGas, s1914.ResourceFish, s1914.ResourceGrain}

// need — дефицитный ресурс: сколько добрать и почему. urgent — нужен
// сейчас или скоро кончится; иначе это запас впрок. emergency — уже
// в минусе или кончится за EmergencyDays: тогда emergency единиц берём
// и дорого.
type need struct {
	res       int
	amount    float64
	urgent    bool
	emergency float64
	why       []string
}

func planMarket(w *World, b *Budget, r Rules, mem *Memory, plan *Plan) {
	tradeResults(w, mem, plan)
	// Обещанное в висящих предложениях уйдёт, как только их примут:
	// считаем его отданным.
	for _, o := range waiting(w) {
		b.Pay(o.OffersA, false)
	}
	needs := deficits(w, b, r)
	answerOffers(w, b, r, needs, plan)
	if !r.Trade {
		return
	}
	if len(needs) > 0 {
		var parts []string
		for _, n := range needs {
			parts = append(parts, fmt.Sprintf("%s %s (%s)", w.ResName(n.res), num(n.amount), strings.Join(n.why, "; ")))
		}
		plan.note(CatMarket, "дефицит", "дефицит", strings.Join(parts, "; "))
	}
	buyNeeds(w, b, r, needs, plan)
	if n := len(waiting(w)); n >= r.MaxPending {
		plan.note(CatMarket, "ждём-ответа", "предложения",
			fmt.Sprintf("без ответа висит %d наших предложений (предел %d) — новых не шлём, ждём ответов", n, r.MaxPending))
		return
	}
	sent := swapForNeeds(w, b, r, mem, needs, plan)
	sellSurplus(w, b, r, mem, needs, plan, sent)
}

func refusedKey(player, res int) string { return fmt.Sprintf("%d:%d", player, res) }

// strategic — что скоро станет редким на всей карте: в начале партии
// руда и дерево (на них стройка и бронеавтомобили), потом нефть.
func strategic(w *World, r Rules) ([]int, string) {
	if w.G.Day >= r.OilFromDay {
		return []int{s1914.ResourceOil}, fmt.Sprintf("с %d-го дня на всей карте не хватает нефти", r.OilFromDay)
	}
	return []int{s1914.ResourceIron, s1914.ResourceWood}, "в начале партии на всей карте не хватает руды и дерева"
}

// deficits — правило 1. Срочное первым, потом дорогое.
func deficits(w *World, b *Budget, r Rules) []need {
	byRes := map[int]*need{}
	add := func(res int, amount float64, urgent bool, why string) {
		if res == s1914.ResourceMoney || amount <= 0 {
			return
		}
		if _, known := w.G.Resources[res]; !known {
			return
		}
		n := byRes[res]
		if n == nil {
			n = &need{res: res}
			byRes[res] = n
		}
		n.amount = max(n.amount, amount)
		n.urgent = n.urgent || urgent
		n.why = append(n.why, why)
	}
	for _, res := range sortedRes(b.Lacks) {
		add(res, b.Lacks[res], true, "не хватило стройке")
	}
	for _, res := range sellOrder {
		daily := w.Daily(res)
		if daily >= 0 {
			continue
		}
		left := b.Free(res) + daily*r.ForecastDays
		if left < r.KeepStock {
			add(res, r.KeepStock-left, true, fmt.Sprintf("прирост %s в сутки, через %.0f суток останется %s",
				num(daily), r.ForecastDays, num(max(left, 0))))
		}
	}
	// Экстренно: в минусе или вот-вот будет.
	for _, res := range sellOrder {
		left := w.Amount(res) + min(w.Daily(res), 0)*r.EmergencyDays
		if left >= 0 {
			continue
		}
		amount := -left - min(w.Daily(res), 0)*r.EmergencyDays
		add(res, amount, true, fmt.Sprintf("СРОЧНО: запас %s, прирост %s в сутки — уходим в минус", num(w.Amount(res)), num(w.Daily(res))))
		if n := byRes[res]; n != nil {
			n.emergency = max(n.emergency, amount)
		}
	}
	list, why := strategic(w, r)
	for _, res := range list {
		if free := b.Free(res); free < r.StrategicStock {
			add(res, r.StrategicStock-free, false, why)
		}
	}

	out := make([]need, 0, len(byRes))
	for _, n := range byRes {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].emergency > 0) != (out[j].emergency > 0) {
			return out[i].emergency > 0
		}
		if out[i].urgent != out[j].urgent {
			return out[i].urgent
		}
		vi, vj := out[i].amount*w.G.Market.Prices[out[i].res], out[j].amount*w.G.Market.Prices[out[j].res]
		if vi != vj {
			return vi > vj
		}
		return out[i].res < out[j].res
	})
	return out
}

func needed(needs []need, res int) bool {
	for _, n := range needs {
		if n.res == res && n.amount > 0 {
			return true
		}
	}
	return false
}

// tradeResults — чем кончились наши прошлые предложения. В первый заход
// партии только запоминаем историю: она была до бота.
func tradeResults(w *World, mem *Memory, plan *Plan) {
	first := mem.Visits == 0
	me := w.G.Me
	for _, t := range w.G.Market.Past {
		if !t.Processed || mem.SeenTrades[t.ID] {
			continue
		}
		mem.SeenTrades[t.ID] = true
		if first || t.PartyA != me {
			continue
		}
		other := w.G.Players[t.PartyB].Nation
		what := fmt.Sprintf("сделка с %s: %s за %s", other, offerText(w, t.OffersA), offerText(w, t.OffersB))
		if t.Successful {
			plan.note(CatMarket, "итог-сделки", what, "принята")
			continue
		}
		plan.note(CatMarket, "итог-сделки", what, "отклонена — сутки этот ресурс ему не предлагаем")
		for res := range t.OffersA {
			mem.Refused[refusedKey(t.PartyB, res)] = w.Now.Add(w.GameDay())
		}
	}
}

// answerOffers — ответы на предложения нам.
func answerOffers(w *World, b *Budget, r Rules, needs []need, plan *Plan) {
	for _, o := range w.G.Market.Incoming {
		from := w.G.Players[o.PartyA].Nation
		get, give := o.OffersA, o.OffersB
		what := fmt.Sprintf("предложение от %s: дают %s, просят %s", from, offerText(w, get), offerText(w, give))
		if !onlyResources(get) || !onlyResources(give) {
			plan.note(CatMarket, "предложение-нам", what, "в сделке не только ресурсы — решать человеку")
			continue
		}
		vGet, vGive := value(w, get), value(w, give)
		if ok, res, lack := b.Check(give, false); !ok {
			plan.add(Decision{Category: CatMarket, Rule: "предложение-нам", What: what + " — отказ",
				Why:    fmt.Sprintf("нечем платить: %s не хватает %s", w.ResName(res), num(lack)),
				Action: s1914.AnswerTradeOffer(o.ID, false)})
			continue
		}
		var useful []string
		for res := range get {
			if needed(needs, res) {
				useful = append(useful, w.ResName(res))
			}
		}
		// Отдаём то, что самим в дефиците, — только с большим перевесом.
		givesNeeded := false
		for res := range give {
			givesNeeded = givesNeeded || needed(needs, res)
		}
		switch {
		case vGet >= vGive*r.AcceptRatio:
			b.Pay(give, false)
			plan.add(Decision{Category: CatMarket, Rule: "предложение-нам", What: what + " — принять",
				Why:    fmt.Sprintf("по справочным ценам получаем %s, отдаём %s", num(vGet), num(vGive)),
				Action: s1914.AnswerTradeOffer(o.ID, true)})
		case len(useful) > 0 && !givesNeeded && vGet >= vGive:
			b.Pay(give, false)
			sort.Strings(useful)
			plan.add(Decision{Category: CatMarket, Rule: "предложение-нам", What: what + " — принять",
				Why: fmt.Sprintf("дают то, чего нам не хватает (%s), не в убыток: получаем %s, отдаём %s",
					strings.Join(useful, ", "), num(vGet), num(vGive)),
				Action: s1914.AnswerTradeOffer(o.ID, true)})
		default:
			plan.add(Decision{Category: CatMarket, Rule: "предложение-нам", What: what + " — отказ",
				Why: fmt.Sprintf("невыгодно: получаем %s, отдаём %s (нужно в %.1f раза больше)",
					num(vGet), num(vGive), r.AcceptRatio),
				Action: s1914.AnswerTradeOffer(o.ID, false)})
		}
	}
}

// buyNeeds — правило 2. Купленное уменьшает need.amount.
func buyNeeds(w *World, b *Budget, r Rules, needs []need, plan *Plan) {
	for i := range needs {
		n := &needs[i]
		res := n.res
		price := w.G.Market.Prices[res]
		markup, keep, kind := r.BuyMarkup, r.MoneyKeep, "нужно сейчас"
		if !n.urgent {
			// Впрок — только дёшево и не в последние деньги.
			markup, keep, kind = r.CheapMarkup, 2*r.MoneyKeep, "впрок"
		}
		var asks []s1914.Order
		for _, o := range w.G.Market.Asks {
			if o.Resource == res && !o.Buy && o.Player != w.G.Me && o.Amount > 0 {
				asks = append(asks, o)
			}
		}
		sort.Slice(asks, func(i, j int) bool { return asks[i].Limit < asks[j].Limit })
		if len(asks) == 0 {
			plan.note(CatMarket, "купить-на-бирже", w.ResName(res),
				fmt.Sprintf("нужно %s (%s), но на бирже его никто не продаёт", num(n.amount), kind))
			continue
		}
		for _, o := range asks {
			if n.amount <= 0 {
				break
			}
			// Экстренную часть берём и дорого.
			if n.emergency > 0 && price > 0 && o.Limit > price*markup && o.Limit <= price*r.EmergencyMarkup {
				amount := math.Ceil(min(n.emergency, o.Amount))
				if spare := b.Free(s1914.ResourceMoney) - r.MoneyKeep; amount*o.Limit > spare {
					amount = math.Floor(spare / o.Limit)
				}
				if amount >= 1 {
					cost := amount * o.Limit
					b.Pay(map[int]float64{s1914.ResourceMoney: cost}, false)
					n.amount -= amount
					n.emergency -= amount
					plan.add(Decision{Category: CatMarket, Rule: "купить-срочно",
						What: fmt.Sprintf("купить %s %s по %.1f", w.ResName(res), num(amount), o.Limit),
						Why: fmt.Sprintf("%s; дорого (справочная %.1f), но минус по ресурсу хуже — берём до ×%.0f, "+
							"сколько нужно, чтобы выйти в плюс на %.1f суток", strings.Join(n.why, "; "), price,
							r.EmergencyMarkup, r.EmergencyDays),
						Action: s1914.MarketDeal(w.G.Me, o, amount),
						Cost:   map[int]float64{s1914.ResourceMoney: cost}})
					continue
				}
			}
			if price > 0 && o.Limit > price*markup {
				plan.note(CatMarket, "купить-на-бирже", w.ResName(res),
					fmt.Sprintf("нужно %s (%s), но дешевле %.1f за единицу не продают; берём не дороже %.1f (справочная %.1f ×%.1f)",
						num(n.amount), kind, o.Limit, price*markup, price, markup))
				break
			}
			amount := math.Floor(min(n.amount, o.Amount))
			// Денег не хватает на всё — берём сколько можно.
			if spare := b.Free(s1914.ResourceMoney) - keep; amount*o.Limit > spare {
				amount = math.Floor(spare / o.Limit)
			}
			if amount < 100 {
				plan.note(CatMarket, "купить-на-бирже", w.ResName(res),
					fmt.Sprintf("нужно %s (%s), но свободных денег нет: держим %s про запас", num(n.amount), kind, num(keep)))
				break
			}
			cost := amount * o.Limit
			b.Pay(map[int]float64{s1914.ResourceMoney: cost}, false)
			n.amount -= amount
			if b.Lacks[res] > 0 {
				b.Lacks[res] = max(b.Lacks[res]-amount, 0)
			}
			plan.add(Decision{Category: CatMarket, Rule: "купить-на-бирже",
				What: fmt.Sprintf("купить %s %s по %.1f", w.ResName(res), num(amount), o.Limit),
				Why: fmt.Sprintf("%s — %s; цена не выше справочной %.1f ×%.1f",
					strings.Join(n.why, "; "), kind, price, markup),
				Action: s1914.MarketDeal(w.G.Me, o, amount),
				Cost:   map[int]float64{s1914.ResourceMoney: cost}})
		}
	}
}

// partners — с кем торговать напрямую: только живые игроки. Боты прямых
// сделок не принимают (сказал игрок 02.11.2026; вживую наши предложения
// ботам так и висели без ответа). С врагом игра торговать не даёт.
func partners(w *World, r Rules) []s1914.Player {
	if !r.TradeHumans {
		return nil
	}
	var out []s1914.Player
	for _, id := range sortedPlayers(w) {
		pl := w.G.Players[id]
		if id != w.G.Me && !pl.IsAI && !pl.Defeated && !pl.Retired {
			out = append(out, pl)
		}
	}
	return out
}

// waiting — наши предложения, на которые ещё может прийти ответ: ботам
// не в счёт, они не отвечают; без ответа дольше игровых суток — тоже:
// их уже не примут, а лимит висящих они занимали бы вечно.
func waiting(w *World) []s1914.TradeOffer {
	var out []s1914.TradeOffer
	for _, o := range w.G.Market.Outgoing {
		pl, ok := w.G.Players[o.PartyB]
		if !ok || pl.IsAI || (!o.Time.IsZero() && w.Now.Sub(o.Time) > w.GameDay()) {
			continue
		}
		out = append(out, o)
	}
	return out
}

func sortedPlayers(w *World) []int {
	ids := make([]int, 0, len(w.G.Players))
	for id := range w.G.Players {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// pickBot — следующий по кругу партнёр, который этот ресурс не отвергал.
func pickBot(w *World, bots []s1914.Player, res int, mem *Memory, used map[int]bool) (s1914.Player, bool) {
	for i := range bots {
		k := (mem.NextBot + i) % len(bots)
		bot := bots[k]
		if used[bot.ID] || pending(w, bot.ID) || w.Hostile(bot.ID) || bot.ID == mem.WarTarget ||
			w.Now.Before(mem.Refused[refusedKey(bot.ID, res)]) {
			continue
		}
		mem.NextBot = k + 1
		return bot, true
	}
	return s1914.Player{}, false
}

// pending — у игрока висит наше предложение без ответа: второе не шлём.
func pending(w *World, player int) bool {
	for _, o := range waiting(w) {
		if o.PartyB == player {
			return true
		}
	}
	return false
}

// round100 — вверх до сотни.
func round100(v float64) float64 { return math.Ceil(v/100) * 100 }

// swapForNeeds — правило 3. Возвращает, сколько предложений ушло.
func swapForNeeds(w *World, b *Budget, r Rules, mem *Memory, needs []need, plan *Plan) int {
	list := partners(w, r)
	sent := 0
	used := map[int]bool{}
	prices := w.G.Market.Prices
	for _, n := range needs {
		if n.amount <= 0 || sent >= r.TradeOffersPerVisit || len(list) == 0 {
			continue
		}
		want := round100(max(n.amount, 500))
		// Отдаём то, чего больше всего сверх запаса и что нам не нужно.
		give, giveAmount, best := -1, 0.0, 0.0
		for _, res := range sellOrder {
			if res == n.res || needed(needs, res) {
				continue
			}
			food := res == s1914.ResourceGrain || res == s1914.ResourceFish
			if food && w.Daily(res) < 0 {
				continue
			}
			amount := want * r.SwapRatio
			if prices[n.res] > 0 && prices[res] > 0 {
				amount = want * prices[n.res] / prices[res] * r.SwapRatio
			}
			amount = round100(amount)
			if surplus := b.Free(res) - r.KeepStock; surplus >= amount && surplus > best {
				give, giveAmount, best = res, amount, surplus
			}
		}
		if give < 0 {
			plan.note(CatMarket, "обмен", w.ResName(n.res),
				fmt.Sprintf("нужно %s, а менять нечего: лишнего ни в чём не хватает на обмен", num(n.amount)))
			continue
		}
		partner, ok := pickBot(w, list, give, mem, used)
		if !ok {
			plan.note(CatMarket, "обмен", w.ResName(n.res), "все, с кем можно меняться, недавно отказались")
			continue
		}
		used[partner.ID] = true
		offer := map[int]float64{give: giveAmount}
		b.Pay(offer, false)
		sent++
		who := "игроку"
		plan.add(Decision{Category: CatMarket, Rule: "обмен",
			What: fmt.Sprintf("%s → %s: %s %s за %s %s", w.ResName(give), partner.Nation,
				num(giveAmount), w.ResName(give), num(want), w.ResName(n.res)),
			Why: fmt.Sprintf("нужно %s %s (%s); на бирже дёшево не купили. Предлагаем %s свой излишек %s (%s), "+
				"по справочным ценам с переплатой ×%.1f, чтобы согласились",
				w.ResName(n.res), num(n.amount), strings.Join(n.why, "; "), who, w.ResName(give), num(best), r.SwapRatio),
			Action: s1914.PlayerTradeOffer(w.G.Me, partner.ID, offer, map[int]float64{n.res: want}, w.Now),
			Cost:   offer})
	}
	return sent
}

// sellSurplus — правило 4.
func sellSurplus(w *World, b *Budget, r Rules, mem *Memory, needs []need, plan *Plan, sent int) {
	bots := partners(w, r)
	if len(bots) == 0 {
		return
	}
	used := map[int]bool{}
	for _, res := range sellOrder {
		if sent >= r.TradeOffersPerVisit {
			return
		}
		if _, known := w.G.Resources[res]; !known || needed(needs, res) {
			continue
		}
		food := res == s1914.ResourceGrain || res == s1914.ResourceFish
		if food && w.Daily(res) < 0 {
			continue
		}
		surplus := b.Free(res) - r.KeepStock - r.SellAbove
		if surplus < r.TradeChunk/2 {
			continue
		}
		amount := math.Floor(min(surplus, r.TradeChunk)/100) * 100
		bot, ok := pickBot(w, bots, res, mem, used)
		if !ok {
			plan.note(CatMarket, "продажа-игрокам", w.ResName(res), "все игроки недавно отказались брать этот ресурс")
			continue
		}
		used[bot.ID] = true
		money := amount * r.TradeRate
		give := map[int]float64{res: amount}
		b.Pay(give, false)
		sent++
		plan.add(Decision{Category: CatMarket, Rule: "продажа-игрокам",
			What: fmt.Sprintf("%s → %s: %s за %s денег", w.ResName(res), bot.Nation, num(amount), num(money)),
			Why: fmt.Sprintf("излишек %s %s сверх запаса %s и резерва на обмен %s; предлагаем игроку по %.0f за единицу, "+
				"на бирже не продаём (теряем 20%%, справочная %.1f)",
				w.ResName(res), num(surplus+r.SellAbove), num(r.KeepStock), num(r.SellAbove), r.TradeRate, w.G.Market.Prices[res]),
			Action: s1914.PlayerTradeOffer(w.G.Me, bot.ID, give, map[int]float64{s1914.ResourceMoney: money}, w.Now),
			Cost:   give})
	}
}

// value — стоимость набора ресурсов по справочным ценам биржи.
func value(w *World, m map[int]float64) float64 {
	v := 0.0
	for res, n := range m {
		v += n * w.G.Market.Prices[res]
	}
	return v
}

// onlyResources — в наборе только ресурсы: номера, которые игра знает
// как ресурсы. Отношения и провинции в сделках идут особыми номерами.
func onlyResources(m map[int]float64) bool {
	for res := range m {
		if res != s1914.ResourceMoney && (res < 0 || res > s1914.ResourceGas) {
			return false
		}
	}
	return true
}

func offerText(w *World, m map[int]float64) string {
	var parts []string
	for _, res := range sortedRes(m) {
		parts = append(parts, fmt.Sprintf("%s %s", w.ResName(res), num(m[res])))
	}
	if len(parts) == 0 {
		return "ничего"
	}
	return strings.Join(parts, ", ")
}
