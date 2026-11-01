package s1914

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Game — одна игра из списка. Числа игра отдаёт строками, оставляем как есть:
// нам они нужны для лога и сравнения, а не для арифметики.
type Game struct {
	GameID      string `json:"gameID"`
	Title       string `json:"title"`
	Comment     string `json:"comment"`
	ScenarioID  string `json:"scenarioID"`
	OpenSlots   string `json:"openSlots"`
	NrOfPlayers string `json:"nrofplayers"`
	DayOfGame   string `json:"dayofgame"`
	State       string `json:"state"`
	Language    string `json:"language"`
	PasswordSet string `json:"passwordset"`
	Ranked      string `json:"ranked"`
	StartOfGame string `json:"startofgame2"`
	MinRank     string `json:"minRank"`
	// TimeScale — во сколько игровое время медленнее ускоренного предела:
	// 1 у обычной партии, 0.25 у «[Speed]», 0.1 у «[Event]».
	TimeScale looseFloat `json:"timeScale"`
	// AnonymousRound — событийная партия с анонимным раундом: в состоянии
	// такой партии игра подменяет имя каждого участника на «анонимный».
	// Ключ лежит в тех же свойствах партии, что и timeScale, и клиент игры
	// читает его так же — см. IsAnonymous.
	AnonymousRound looseFlag `json:"anonymousRound"`

	// Приходят только в списке своих игр: наш номер в партии и когда мы
	// в неё вошли. У игр из лобби они пустые.
	PlayerID string `json:"playerID"`
	JoinTime string `json:"joinTime"`
}

// UnmarshalJSON — разбор, терпимый к тому, что игра шлёт число вместо строки.
// Подробности у looseUnmarshal.
func (g *Game) UnmarshalJSON(b []byte) error {
	// plain — та же структура без этого метода, иначе разбор зациклится.
	type plain Game
	return looseUnmarshal(b, (*plain)(g))
}

// looseUnmarshal заворачивает в кавычки числа, попавшие в строковые поля,
// и только потом разбирает. Один и тот же ключ игра шлёт то так, то эдак:
// в лобби startofgame2 приходит как "1785920128", а в списке своих игр —
// как 1785920128; siteUserID и allianceID у обычного игрока строки, а у
// удалённого — ноль числом. Одна такая мелочь роняла разбор целиком:
// не поле терялось, а весь список игр или весь состав партии.
//
// Чинить это здесь дешевле, чем заводить свой тип на каждый капризный ключ:
// поля структур остаются строками, а новые ключи лечатся сами. Числовые
// поля (playerLevel, timeScale) не трогаем — их разбирают как числа.
func looseUnmarshal(b []byte, v any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}

	asString := stringFields(reflect.TypeOf(v).Elem())
	for k, raw := range fields {
		if asString[k] && isJSONNumber(raw) {
			fields[k] = strconv.AppendQuote(nil, string(raw))
		}
	}

	normalized, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(normalized, v)
}

// stringFields — имена ключей json, которые структура держит строками.
func stringFields(t reflect.Type) map[string]bool {
	out := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type.Kind() != reflect.String {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" {
			name = f.Name
		}
		if name != "-" {
			out[name] = true
		}
	}
	return out
}

func isJSONNumber(v json.RawMessage) bool {
	if len(v) == 0 {
		return false
	}
	return v[0] == '-' || (v[0] >= '0' && v[0] <= '9')
}

// looseFlag — признак «да/нет», который игра шлёт то единицей, то строкой,
// то булевым. Разбирается терпимо и не ошибается никогда: ключ для нас новый,
// а уронить им разбор всего лобби мы права не имеем — как это бывает,
// написано у looseUnmarshal. Незнакомое значение означает «нет»: не открыть
// лишнего важнее, чем угадать.
type looseFlag bool

func (f *looseFlag) UnmarshalJSON(b []byte) error {
	switch strings.Trim(string(b), `"`) {
	case "1", "true":
		*f = true
	default:
		*f = false
	}
	return nil
}

// looseFloat — число, которое игра шлёт то так, то эдак: в списке лобби
// timeScale приходит строкой «0.25», а в свойствах одной партии — числом
// 0.25. Один и тот же ключ, два разных вида, и выбирать не нам.
type looseFloat float64

func (n *looseFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*n = looseFloat(f)
	return nil
}

// IsAnonymous — анонимная ли это партия. Клиент игры решает так же:
// свойство anonymousRound, приведённое к числу, равно единице.
//
// В такой партии игра называет «анонимным» каждого участника, кто бы он
// ни был. Скрыт при этом только ник: номер аккаунта в состоянии настоящий,
// и состав с сайта по нему называет живого человека.
func (g *Game) IsAnonymous() bool { return bool(g.AnonymousRound) }

// Speed — во сколько раз партия быстрее обычной: 1, 2, 4, 10. Игра называет
// обратную величину, а считать и группировать удобнее по скорости: «в x4»
// читается, «в 0.25» — нет.
//
// Округление здесь не косметика. Игра шлёт timeScale приближённо — у x10 это
// 0.10000000149011612, и деление даёт 9.99999985. По такой скорости партии
// группируются в аналитике, и без округления одна и та же x10 разъезжалась бы
// на несколько групп.
//
// Ноль у timeScale означает, что поля не было: такую партию считаем обычной —
// это худшее, что может случиться от догадки.
func (g Game) Speed() float64 {
	if g.TimeScale <= 0 {
		return 1
	}
	speed := math.Round(1 / float64(g.TimeScale))
	if speed < 1 {
		return 1
	}
	return speed
}

// Started и Joined — время начала партии и нашего входа в неё. Игра шлёт
// секунды строкой; ноль и мусор одинаково означают «неизвестно», и разбирать
// это стоит здесь, у самой партии, а не у каждого, кто её показывает.
func (g Game) Started() time.Time { return unixSeconds(g.StartOfGame) }
func (g Game) Joined() time.Time  { return unixSeconds(g.JoinTime) }

func unixSeconds(s string) time.Time {
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// OpenGames возвращает игры из лобби, куда сейчас можно вступить.
func (c *Client) OpenGames(ctx context.Context) ([]Game, error) {
	const perPage = 100

	var out []Game
	for page := 1; ; page++ {
		raw, err := c.call(ctx, "getGames", []param{
			{"numEntriesPerPage", strconv.Itoa(perPage)},
			{"page", strconv.Itoa(page)},
			{"lang", c.lang},
			{"password", "0"},
			{"allianceGame", "0"},
			{"openSlots", "1"},
			{"isFilterSearch", "false"},
			{"global", "1"},
			{"loadUserLoginData", "1"},
		})
		if err != nil {
			return nil, err
		}

		var res struct {
			NumGames int `json:"numGames"`
			Games    []struct {
				Properties Game `json:"properties"`
			} `json:"games"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("разбор списка игр: %w", err)
		}
		for _, g := range res.Games {
			out = append(out, g.Properties)
		}
		// Страница пришла неполной или мы уже собрали всё обещанное —
		// дальше листать нечего.
		if len(res.Games) < perPage || len(out) >= res.NumGames {
			return out, nil
		}
	}
}

// MyGames возвращает игры аккаунта, которые идут сейчас, — то же, что
// показывает вкладка «Обзор» на /game.php.
func (c *Client) MyGames(ctx context.Context) ([]Game, error) {
	sess, err := c.ensureSession(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.call(ctx, "getGames", myGamesParams(sess.userID))
	if err != nil {
		return nil, err
	}
	return parseMyGames(raw)
}

// myGamesParams вынесены отдельно, чтобы порядок параметров можно было
// сверить с подписанным вектором из HAR, не выходя в сеть.
func myGamesParams(userID string) []param {
	return []param{
		{"userID", userID},
		{"loadUserLoginData", "1"},
	}
}

// parseMyGames разбирает ответ на список своих игр. В отличие от лобби
// result здесь — сразу массив игр, без обёртки с numGames.
func parseMyGames(raw json.RawMessage) ([]Game, error) {
	var res []struct {
		Properties Game `json:"properties"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("разбор списка своих игр: %w", err)
	}
	out := make([]Game, 0, len(res))
	for _, g := range res {
		out = append(out, g.Properties)
	}
	return out, nil
}
