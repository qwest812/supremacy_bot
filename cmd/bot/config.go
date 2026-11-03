package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"supermessy_bot/internal/bot"
)

// loadDotEnv читает файл KEY=VALUE в окружение. Уже заданные переменные
// не перекрывает: окружение важнее файла. Нет файла — не ошибка.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: нет «=»", path, n)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}

type settings struct {
	User, Pass, Lang string
	LobbyTitle       string
	LobbySpeed       float64
	// UIAddr — где поднять веб-интерфейс к журналам; пусто — не поднимать.
	UIAddr string
	Bot    bot.Config
}

func loadSettings() (settings, error) {
	var errs []string
	str := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	dur := func(k string, def time.Duration) time.Duration {
		v := os.Getenv(k)
		if v == "" {
			return def
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%q: %v", k, v, err))
			return def
		}
		return d
	}
	flt := func(k string, def float64) float64 {
		v := os.Getenv(k)
		if v == "" {
			return def
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%q: %v", k, v, err))
			return def
		}
		return f
	}
	integer := func(k string, def int) int { return int(flt(k, float64(def))) }
	boolean := func(k string, def bool) bool {
		v := strings.ToLower(os.Getenv(k))
		switch v {
		case "":
			return def
		case "1", "true", "yes", "да":
			return true
		case "0", "false", "no", "нет":
			return false
		}
		errs = append(errs, fmt.Sprintf("%s=%q: ждём true или false", k, v))
		return def
	}

	r := bot.DefaultRules()
	r.FoodLow = flt("RULE_FOOD_LOW", r.FoodLow)
	if v := strings.TrimSpace(os.Getenv("UNITS")); v != "" {
		r.Units = nil
		for _, u := range strings.Split(v, ",") {
			if u = strings.ToLower(strings.TrimSpace(u)); u != "" {
				r.Units = append(r.Units, u)
			}
		}
	}
	r.HarbourDay = integer("RULE_HARBOUR_DAY", r.HarbourDay)
	r.RailwayDay = integer("RULE_RAILWAY_DAY", r.RailwayDay)
	r.FactoryReserveDay = integer("RULE_FACTORY_RESERVE_DAY", r.FactoryReserveDay)
	r.MoneyKeep = flt("RULE_MONEY_KEEP", r.MoneyKeep)
	r.Trade = boolean("TRADE", r.Trade)
	r.TradeRate = flt("TRADE_RATE", r.TradeRate)
	r.TradeChunk = flt("TRADE_CHUNK", r.TradeChunk)
	r.TradeOffersPerVisit = integer("TRADE_OFFERS_PER_VISIT", r.TradeOffersPerVisit)
	r.KeepStock = flt("TRADE_KEEP_STOCK", r.KeepStock)
	r.BuyMarkup = flt("TRADE_BUY_MARKUP", r.BuyMarkup)
	r.AcceptRatio = flt("TRADE_ACCEPT_RATIO", r.AcceptRatio)
	r.StrategicStock = flt("TRADE_STRATEGIC_STOCK", r.StrategicStock)
	r.OilFromDay = integer("TRADE_OIL_FROM_DAY", r.OilFromDay)
	r.ForecastDays = flt("TRADE_FORECAST_DAYS", r.ForecastDays)
	r.CheapMarkup = flt("TRADE_CHEAP_MARKUP", r.CheapMarkup)
	r.EmergencyDays = flt("TRADE_EMERGENCY_DAYS", r.EmergencyDays)
	r.EmergencyMarkup = flt("TRADE_EMERGENCY_MARKUP", r.EmergencyMarkup)
	r.SwapRatio = flt("TRADE_SWAP_RATIO", r.SwapRatio)
	r.SellAbove = flt("TRADE_SELL_ABOVE", r.SellAbove)
	r.TradeHumans = boolean("TRADE_WITH_HUMANS", r.TradeHumans)
	r.MaxPending = integer("TRADE_MAX_PENDING", r.MaxPending)
	r.RightOfWay = boolean("RIGHT_OF_WAY_TO_BOTS", r.RightOfWay)
	r.War = boolean("WAR", r.War)
	r.WarOnHumans = boolean("WAR_ON_HUMANS", r.WarOnHumans)
	r.WarFromDay = integer("WAR_FROM_DAY", r.WarFromDay)
	r.MinWarPower = flt("WAR_MIN_POWER", r.MinWarPower)
	r.AttackRatio = flt("ATTACK_RATIO", r.AttackRatio)
	r.DefenseRatio = flt("DEFENSE_RATIO", r.DefenseRatio)
	r.RetreatRatio = flt("RETREAT_RATIO", r.RetreatRatio)
	r.StrikeHops = flt("STRIKE_HOPS", r.StrikeHops)
	r.DefendHops = flt("DEFEND_HOPS", r.DefendHops)
	r.MaxOps = integer("MAX_ATTACKS_PER_VISIT", r.MaxOps)
	r.StartGarrison = flt("START_GARRISON", r.StartGarrison)
	r.WeakShare = flt("WAR_WEAK_SHARE", r.WeakShare)
	r.WeakBonus = flt("WAR_WEAK_BONUS", r.WeakBonus)
	r.Stacks = boolean("WAR_STACKS", r.Stacks)

	s := settings{
		User:       os.Getenv("S1914_USER"),
		Pass:       os.Getenv("S1914_PASSWORD"),
		Lang:       str("S1914_LANG", "ru"),
		LobbyTitle: str("LOBBY_TITLE", "Europe"),
		LobbySpeed: flt("LOBBY_SPEED", 4),
		UIAddr:     uiAddr(),
		Bot: bot.Config{
			GameID:      os.Getenv("GAME_ID"),
			Dry:         boolean("DRY_RUN", true),
			VisitMin:    dur("VISIT_MIN", 4*time.Minute),
			VisitMax:    dur("VISIT_MAX", 25*time.Minute),
			Rounds:      integer("VISIT_ROUNDS", 3),
			ActionPause: dur("ACTION_PAUSE", 2*time.Second),
			LogDir:      str("LOG_DIR", "logs"),
			DataDir:     str("DATA_DIR", "data"),
			Rules:       r,
		},
	}
	if s.Bot.VisitMax < s.Bot.VisitMin {
		errs = append(errs, "VISIT_MAX меньше VISIT_MIN")
	}
	if len(errs) > 0 {
		return s, fmt.Errorf("ошибки в настройках:\n  %s", strings.Join(errs, "\n  "))
	}
	return s, nil
}

// uiAddr — адрес интерфейса. Пустое значение в .env выключает его, поэтому
// отличаем «не задано» от «задано пустым».
func uiAddr() string {
	if v, set := os.LookupEnv("UI_ADDR"); set {
		return v
	}
	return "127.0.0.1:8090"
}
