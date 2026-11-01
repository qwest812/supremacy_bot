package s1914

import (
	"encoding/json"
	"fmt"
	"time"
)

// ParseState разбирает ответ игрового сервера на UltUpdateGameStateAction
// целиком (с обёрткой {"result": …}) так же, как заход в партию. Нужен
// тестам и разбору записанных партий: сеть он не трогает.
func ParseState(raw []byte, gameID string, me int, received time.Time) (*GameState, error) {
	var env struct {
		Result gameStateResponse `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("разбор состояния: %w", err)
	}
	env.Result.received = received
	return env.Result.build(gameID, me)
}

// ParseMap разбирает файл карты со static-сервера.
func ParseMap(mapID string, raw []byte) (*MapGeometry, error) {
	return parseMapGeometry(mapID, raw)
}
