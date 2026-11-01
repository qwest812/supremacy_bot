package s1914

// Заход в партию и приказы в ней. Заход игра засчитывает как вход, поэтому
// за один заход делается всё сразу: читаем состояние, отправляем приказы
// и уходим. Каждый приказ — отдельный запрос: так видно, какой именно
// игра не приняла, а остальные от этого не страдают.
//
// Все приказы — те же действия, что шлёт браузерный клиент; откуда снят
// каждый, написано у его сборщика.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
)

// Visit — один заход в партию: пропуск на игровой сервер и состояние,
// с которым мы вошли.
type Visit struct {
	c      *Client
	acc    *gameAccess
	GameID string
	State  *GameState
	reqID  int
}

// Enter заходит в партию игроком и забирает её состояние.
func (c *Client) Enter(ctx context.Context, gameID string) (*Visit, error) {
	acc, state, err := c.enter(ctx, gameID)
	if err != nil {
		return nil, err
	}
	// Вход — запрос 1, состояние — 2; приказы идут дальше.
	return &Visit{c: c, acc: acc, GameID: gameID, State: state, reqID: 3}, nil
}

// Send отправляет одно действие, вложенное в UltUpdateGameStateAction, как
// это делает очередь клиента.
func (v *Visit) Send(ctx context.Context, action map[string]any) error {
	id := v.reqID
	v.reqID++
	act := make(map[string]any, len(action)+1)
	for k, val := range action {
		act[k] = val
	}
	act["requestID"] = "actionReq-" + strconv.Itoa(id)
	var res json.RawMessage
	return v.c.gsCall(ctx, v.acc, v.GameID, id, "ultshared.action.UltUpdateGameStateAction", v.State.Me,
		map[string]any{"actions": []any{act}, "lastCallDuration": 0}, &res)
}

// Refresh перечитывает состояние партии в том же заходе: без повторной
// активации, поэтому новым входом это не считается.
func (v *Visit) Refresh(ctx context.Context) error {
	id := v.reqID
	v.reqID++
	state, err := v.c.gameState(ctx, v.acc, v.GameID, id, v.State.Me)
	if err != nil {
		return err
	}
	v.State = state
	return nil
}

// Режимы UltUpdateProvinceAction (UltUpdateProvinceAction.modes у клиента).
const (
	modeProvince = 0 // свойства провинции: так включают и выключают здание
	modeUpgrade  = 1 // построить здание
	modeUnit     = 2 // произвести войско
)

// BuildAction — поставить здание в провинции. Снято с клиента вживую
// 21.09.2026 (см. vendetta_admin, build.go):
//
//	{"@c":"ultshared.action.UltUpdateProvinceAction","mode":1,
//	 "provinceIDs":[484],"upgrade":{"@c":"mu","id":18,"c":16,"e":true},"slot":0}
//
// У недостроенного здания в c — его нынешнее состояние, у нового — один
// уровень (bc).
func BuildAction(g *GameState, province, upgrade int) map[string]any {
	condition := g.Upgrades[upgrade].BuildCondition
	if p, ok := g.Province(province); ok {
		if c, ok := p.Condition[upgrade]; ok {
			condition = c
		}
	}
	return map[string]any{
		"@c":          "ultshared.action.UltUpdateProvinceAction",
		"mode":        modeUpgrade,
		"provinceIDs": []int{province},
		"upgrade":     map[string]any{"@c": "mu", "id": upgrade, "c": condition, "e": true},
		"slot":        0,
	}
}

// ToggleAction — включить или выключить здание. Клиент делает это так
// (toggleUpgradeInProvinces): копия записи здания с перевёрнутым «e»
// в режиме PROVINCE. Вживую ещё не проверено.
func ToggleAction(g *GameState, province, upgrade int, enabled bool) map[string]any {
	condition := 0
	if p, ok := g.Province(province); ok {
		condition = p.Condition[upgrade]
	}
	return map[string]any{
		"@c":          "ultshared.action.UltUpdateProvinceAction",
		"mode":        modeProvince,
		"provinceIDs": []int{province},
		"upgrade":     map[string]any{"@c": "mu", "id": upgrade, "c": condition, "e": enabled, "rp": nil, "pl": 0},
		"slot":        0,
	}
}

// ProduceAction — заказать войско в провинции. В upgrade уходит запись
// «su» из possibleProductions провинции как есть — так делает клиент.
func ProduceAction(g *GameState, province, unit int) (map[string]any, error) {
	p, ok := g.Province(province)
	if !ok {
		return nil, fmt.Errorf("провинции %d нет", province)
	}
	su, ok := p.CanProduce[unit]
	if !ok {
		return nil, fmt.Errorf("%s не может производить юнит %d", p.Name, unit)
	}
	return map[string]any{
		"@c":          "ultshared.action.UltUpdateProvinceAction",
		"mode":        modeUnit,
		"provinceIDs": []int{province},
		"upgrade":     su,
		"slot":        0,
	}, nil
}

// MoveAction — повести армию по пути. Снято с клиента 14.10.2026
// (vendetta_admin, rally.go): путь целиком отрезками «gc», команды
// заменяют очередь армии.
func MoveAction(a Army, path []MapPoint) map[string]any {
	return map[string]any{
		"@c":     "ultshared.action.UltArmyAction",
		"armies": []any{armyWire(a, gotoCommands(path))},
	}
}

// AttackAction — повести армию к чужой армии и напасть на неё. Клиент
// (addAttackCommand) ставит путь до цели и в конце команду «ac» с номером
// цели и её точкой. Вживую ещё не проверено.
func AttackAction(a Army, path []MapPoint, target Army) map[string]any {
	cmds := gotoCommands(path)
	cmds = append(cmds, map[string]any{
		"@c":           "ac",
		"targetUnitID": target.ID,
		"targetPos":    map[string]any{"x": target.Pos.X, "y": target.Pos.Y},
	})
	return map[string]any{
		"@c":     "ultshared.action.UltArmyAction",
		"armies": []any{armyWire(a, cmds)},
	}
}

// RelationAction — наше отношение к игроку. Снято с окна дипломатии
// (vendetta_admin, relations.go): одно отношение одним байтом в base64.
// Отрицательные (война −2) клиент кладёт тем же байтом через Uint8Array,
// то есть по модулю 256.
func RelationAction(player, relation int) map[string]any {
	b := byte(int8(relation))
	return map[string]any{
		"@c":           "ultshared.action.UltChangeRelationAction",
		"playerB":      []int{player},
		"relationType": base64.StdEncoding.EncodeToString([]byte{b}),
	}
}
