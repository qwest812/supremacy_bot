package s1914

// Разбор строек и производства из состояния партии. Стройки и войска игра
// шлёт особым списком ["ultshared.UltProductionList",[запись, null, …]].

import "encoding/json"

type buildWire struct {
	Upgrade struct {
		ID int `json:"id"`
	} `json:"u"`
	Ends  int64 `json:"t"`
	Start int64 `json:"s"`
}

// buildList разбирает идущие стройки провинции. Список игра шлёт особым
// видом — ["ultshared.UltProductionList",[запись, null, …]], — и пустые
// места в нём означают свободные слоты. Нет списка — берём одиночную
// запись bi: в ней лежит та же первая стройка.
func buildList(raw json.RawMessage, single *buildWire, clock gameClock) []Construction {
	var out []Construction
	add := func(w *buildWire) {
		if w == nil || w.Upgrade.ID <= 0 {
			return
		}
		out = append(out, Construction{UpgradeID: w.Upgrade.ID, Ends: clock.real(w.Ends)})
	}

	if entries, ok := productionEntries(raw); ok {
		for _, e := range entries {
			var w *buildWire
			if json.Unmarshal(e, &w) == nil {
				add(w)
			}
		}
		return out
	}
	add(single)
	return out
}

// productionEntries разворачивает особый список игры
// ["ultshared.UltProductionList",[запись, null, …]] в записи. Ложь —
// списка нет или он другого вида.
func productionEntries(raw json.RawMessage) ([]json.RawMessage, bool) {
	var wrapper []json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil || len(wrapper) < 2 {
		return nil, false
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(wrapper[1], &entries); err != nil {
		return nil, false
	}
	return entries, true
}

// unitWire — войско, как оно лежит в заказе и в производстве: «su»
// с юнитом внутри, у юнита t — номер типа.
type unitWire struct {
	Unit struct {
		Type int `json:"t"`
	} `json:"unit"`
}

// productionList разбирает идущее производство войск провинции (prs).
// Вид тот же, что у строек, только в u лежит не здание, а «su».
func productionList(raw json.RawMessage, clock gameClock) []Production {
	entries, _ := productionEntries(raw)
	var out []Production
	for _, e := range entries {
		var w *struct {
			U    unitWire `json:"u"`
			Ends int64    `json:"t"`
		}
		if json.Unmarshal(e, &w) != nil || w == nil || w.U.Unit.Type <= 0 {
			continue
		}
		out = append(out, Production{UnitTypeID: w.U.Unit.Type, Ends: clock.real(w.Ends)})
	}
	return out
}

// possibleUnits — что провинция может производить, по номеру типа.
// Запись сохраняется целиком: в заказ её отправляют как есть, так же
// делает клиент игры.
func possibleUnits(list []json.RawMessage) map[int]json.RawMessage {
	out := make(map[int]json.RawMessage, len(list))
	for _, raw := range list {
		var w unitWire
		if json.Unmarshal(raw, &w) == nil && w.Unit.Type > 0 {
			out[w.Unit.Type] = raw
		}
	}
	return out
}

// upgradeWire — здание в действии постройки, как его шлёт клиент игры:
// у недостроенного — его нынешнее состояние, у нового — один уровень.
// Одного номера игре мало: недостроенную дорогу в Сурселе (16 из 60)
// она приняла только в таком виде.
