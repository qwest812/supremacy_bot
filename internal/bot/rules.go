package bot

// Rules — числа, на которых стоят правила бота. Сами правила записаны
// в коде (economy.go, recruit.go, market.go, diplomacy.go), а пороги —
// здесь, чтобы их можно было подстраивать из .env, не трогая логику.
type Rules struct {
	// Units — какие рода войск бот заказывает (имена типов у игры
	// строчными). Из них в каждой провинции берётся то, что игра там
	// разрешает и что даёт больше силы на единицу цены.
	Units []string

	// FoodLow — мало зерна: казарма, в которой производится кавалерия или
	// штурмовики, остаётся включённой, но бот об этом предупреждает.
	// Без такого производства казарма и так стоит на паузе.
	FoodLow float64

	// RailwayDay, HarbourDay, FactoryReserveDay — с какого дня партии
	// работает правило. Сама игра тоже не даёт строить раньше своего дня
	// (doa у здания), поэтому меньше игрового дня ставить бессмысленно.
	HarbourDay        int
	RailwayDay        int
	FactoryReserveDay int

	// MoneyKeep — сколько денег не тратить на дорогие стройки (железная
	// дорога): деньги нужны на содержание и войска.
	MoneyKeep float64

	// Торговля с ботами: продаём излишек по TradeRate денег за единицу
	// партиями по TradeChunk, не больше TradeOffersPerVisit предложений
	// за заход. KeepStock — сколько каждого ресурса оставлять себе.
	Trade               bool
	TradeRate           float64
	TradeChunk          float64
	TradeOffersPerVisit int
	KeepStock           float64
	// BuyMarkup — покупать на бирже, только если цена заявки не выше
	// справочной больше чем в BuyMarkup раз.
	BuyMarkup float64
	// AcceptRatio — принимать чужое предложение, если получаем по
	// справочным ценам хотя бы в AcceptRatio раз больше, чем отдаём.
	AcceptRatio float64
	// Дефицит (market.go). StrategicStock — сколько держать ресурса,
	// которого скоро станет мало на всей карте: в начале руда и дерево,
	// с OilFromDay — нефть. ForecastDays — на сколько игровых суток вперёд
	// смотрим, не кончится ли ресурс с отрицательным приростом.
	StrategicStock float64
	OilFromDay     int
	ForecastDays   float64
	// CheapMarkup — впрок (под будущий дефицит) покупаем на бирже, только
	// если цена не выше справочной больше чем в CheapMarkup раз.
	CheapMarkup float64
	// SwapRatio — в обмене отдаём по справочным ценам во столько раз
	// больше, чем просим: небольшая переплата, чтобы обмен приняли.
	SwapRatio float64
	// SellAbove — продавать за деньги только излишек сверх KeepStock
	// и SellAbove: сначала излишек идёт на обмен.
	SellAbove float64
	// TradeHumans — предлагать обмен и живым игрокам, не только ботам.
	TradeHumans bool
	// MaxPending — сколько наших предложений может висеть без ответа:
	// больше — новых не шлём, ждём ответов.
	MaxPending int

	// RightOfWay — выдавать ботам право прохода.
	RightOfWay bool

	// War — воевать: объявлять войну ботам и брать их провинции. Оборона
	// работает всегда. WarOnHumans — объявлять войну и людям (по умолчанию
	// нет: с людьми воюем, только если они начали).
	War         bool
	WarOnHumans bool
	// WarFromDay — с какого дня можно объявлять войну.
	WarFromDay int
	// MinWarPower — сколько свободных сил нужно, чтобы начать новую войну.
	// Сила — оценка самой игры: пехотинец 4.5, бронеавтомобиль 9.
	MinWarPower float64
	// AttackRatio — во сколько раз мы должны быть сильнее, чтобы напасть.
	// DefenseRatio — сколько защиты держать против угрозы (от её силы).
	// RetreatRatio — во сколько раз враг сильнее гарнизона, чтобы уйти.
	AttackRatio  float64
	DefenseRatio float64
	RetreatRatio float64
	// Газета (intel.go). StartGarrison — сколько пехоты на старте стоит
	// в каждой провинции стандартной карты: от этого считаем, какую долю
	// армии игрок уже потерял. Ослабший на WeakShare и больше — лакомая
	// цель: ему можно объявить войну и раньше WarFromDay, а его оценка
	// цели растёт в 1 + WeakBonus × доля потерь раз.
	StartGarrison float64
	WeakShare     float64
	WeakBonus     float64
	// Stacks — бить одним стаком: если на цель нужно несколько армий
	// из разных мест, сначала сводим их в нашу провинцию у цели (там они
	// сливаются в одну), и бьём уже оттуда.
	Stacks bool
	// StrikeHops и DefendHops — на сколько провинций тянуться за армиями
	// для захвата и для обороны. MaxOps — сколько захватов за заход.
	StrikeHops float64
	DefendHops float64
	MaxOps     int
}

// DefaultRules — правила по умолчанию: так, как их описал игрок.
func DefaultRules() Rules {
	return Rules{
		FoodLow:             3000,
		Units:               []string{uCar, uCavalry, uStormtroopers, "tank", "mkiv_tank"},
		HarbourDay:          2,
		RailwayDay:          3,
		FactoryReserveDay:   5,
		MoneyKeep:           5000,
		Trade:               true,
		TradeRate:           10,
		TradeChunk:          2000,
		TradeOffersPerVisit: 2,
		KeepStock:           3000,
		BuyMarkup:           1.2,
		AcceptRatio:         1.2,
		StrategicStock:      5000,
		OilFromDay:          6,
		ForecastDays:        2,
		CheapMarkup:         1.0,
		SwapRatio:           1.1,
		SellAbove:           8000,
		TradeHumans:         true,
		MaxPending:          4,
		RightOfWay:          true,
		War:                 true,
		WarOnHumans:         false,
		WarFromDay:          3,
		MinWarPower:         60,
		AttackRatio:         1.5,
		DefenseRatio:        1.2,
		RetreatRatio:        2.5,
		StartGarrison:       10,
		WeakShare:           0.3,
		WeakBonus:           2,
		Stacks:              true,
		StrikeHops:          5,
		DefendHops:          6,
		MaxOps:              3,
	}
}
