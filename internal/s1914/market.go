package s1914

// Рынок: биржа и прямые сделки с игроками. Биржа лежит в состоянии
// ресурсов (ключ 4): asks и bids — по списку заявок на каждый ресурс,
// prices — справочная цена. Прямые сделки там же, в trading: ждущие
// нашего ответа (tradingOffers) и прошлые с итогом (pastTrades).
//
// Боты принимают сделки сами и сразу: в записанной партии 14.10.2026
// они покупали дерево и руду по 10 денег за единицу, а на бирже та же
// единица шла по 5–6.

import (
	"encoding/json"
	"strconv"
	"time"
)

// tradeWire — сделка, как её шлёт игра. Ключи offersA/offersB — номера
// ресурсов строками; кроме ресурсов там бывают отношения и провинции
// (особые номера), их мы не трогаем.
type tradeWire struct {
	ID         int64              `json:"tradeId"`
	PartyA     int                `json:"partyA"`
	PartyB     int                `json:"partyB"`
	OffersA    map[string]float64 `json:"offersA"`
	OffersB    map[string]float64 `json:"offersB"`
	Processed  bool               `json:"processed"`
	Successful bool               `json:"successful"`
	TimeStamp  int64              `json:"timeStamp"`
}

func (w tradeWire) offer(clock gameClock) TradeOffer {
	return TradeOffer{
		ID: w.ID, PartyA: w.PartyA, PartyB: w.PartyB,
		OffersA: intKeys(w.OffersA), OffersB: intKeys(w.OffersB),
		Processed: w.Processed, Successful: w.Successful,
		Time: clock.real(w.TimeStamp),
	}
}

type orderWire struct {
	ID       int64   `json:"orderID"`
	Buy      bool    `json:"buy"`
	Amount   float64 `json:"amount"`
	Limit    float64 `json:"limit"`
	Resource int     `json:"resourceType"`
	Player   int     `json:"playerID"`
}

// market собирает биржу и сделки. me нужен, чтобы отличить предложения
// нам от наших собственных.
func (r *gameStateResponse) market(me int, clock gameClock) Market {
	res := r.States.Resources
	m := Market{Prices: map[int]float64{}}
	for id, p := range res.Prices {
		if p > 0 {
			m.Prices[id] = p
		}
	}
	m.Prices[ResourceMoney] = 1
	m.Asks = orderLists(res.Asks)
	m.Bids = orderLists(res.Bids)
	for _, w := range res.Trading.Offers {
		if w.PartyB == me && !w.Processed {
			m.Incoming = append(m.Incoming, w.offer(clock))
		}
	}
	for _, w := range res.Trading.Past {
		m.Past = append(m.Past, w.offer(clock))
	}
	return m
}

// orderLists разворачивает списки заявок вида ["ultshared.UltAskList",[…]].
func orderLists(lists []json.RawMessage) []Order {
	var out []Order
	for _, raw := range lists {
		entries, ok := productionEntries(raw)
		if !ok {
			continue
		}
		for _, e := range entries {
			var w orderWire
			if json.Unmarshal(e, &w) != nil || w.ID == 0 && w.Amount == 0 {
				continue
			}
			out = append(out, Order{ID: w.ID, Buy: w.Buy, Amount: w.Amount,
				Limit: w.Limit, Resource: w.Resource, Player: w.Player})
		}
	}
	return out
}

func intKeys(in map[string]float64) map[int]float64 {
	out := make(map[int]float64, len(in))
	for k, v := range in {
		if n, err := strconv.Atoi(k); err == nil {
			out[n] = v
		}
	}
	return out
}

// maxValue — наибольшее значение из словаря «класс цели → число»: сила
// атаки и защиты у юнита своя против каждого класса, а для грубой оценки
// армии хватает лучшей.
func maxValue(m map[string]float64) float64 {
	best := 0.0
	for _, v := range m {
		if v > best {
			best = v
		}
	}
	return best
}

// PlayerTradeOffer — наше предложение игроку: мы отдаём give, он — take.
// Собирается так же, как клиент (UltTradeOffer.toSendable).
func PlayerTradeOffer(me, to int, give, take map[int]float64, now time.Time) map[string]any {
	return map[string]any{
		"@c":           "ultshared.action.UltTradeOfferAction",
		"flag":         0, // CREATE_OFFER
		"tradeOfferID": -1,
		"tradeOffer": map[string]any{
			"@c":             "ultshared.UltTradeOffer",
			"partyA":         me,
			"partyB":         to,
			"offersA":        strKeys(give),
			"offersB":        strKeys(take),
			"message":        "",
			"timeStamp":      now.UnixMilli(),
			"tradeId":        -1,
			"readByReceiver": nil,
		},
	}
}

// AnswerTradeOffer — ответ на предложение нам: принять или отказать.
func AnswerTradeOffer(id int64, accept bool) map[string]any {
	flag := 2 // REFUSE_OFFER
	if accept {
		flag = 1 // ACCEPT_OFFER
	}
	return map[string]any{
		"@c":           "ultshared.action.UltTradeOfferAction",
		"flag":         flag,
		"tradeOfferID": id,
		"tradeOffer":   nil,
	}
}

// MarketDeal — сделка по чужой заявке на бирже: купить у продающего или
// продать покупающему. Так её собирает клиент (onAcceptTrade): новая
// заявка с тем же номером, обратной стороной и нашим объёмом.
func MarketDeal(me int, o Order, amount float64) map[string]any {
	player := me
	if o.Player == -1 {
		player = -1
	}
	total := amount * o.Limit
	return map[string]any{
		"@c": "ultshared.action.UltOrderAction",
		"order": map[string]any{
			"@c":           "ultshared.UltOrder",
			"orderID":      o.ID,
			"buy":          !o.Buy,
			"amount":       int64(amount),
			"limit":        o.Limit,
			"resourceType": o.Resource,
			"playerID":     player,
			"totalPrice":   int64(total),
			"embargo":      false,
			"isOwner":      false,
			"buyable":      false,
		},
		"cancel": false,
	}
}

func strKeys(in map[int]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[strconv.Itoa(k)] = v
	}
	return out
}
