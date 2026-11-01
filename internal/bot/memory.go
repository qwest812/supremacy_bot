package bot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Memory — то, что бот помнит о партии между заходами и перезапусками:
// без этого он не узнал бы, сколько пехоты прибавилось, и предлагал бы
// одну и ту же отвергнутую сделку каждый заход.
type Memory struct {
	GameID    string    `json:"gameID"`
	LastVisit time.Time `json:"lastVisit"`
	Visits    int       `json:"visits"`
	// Infantry — сколько пехоты было в прошлый заход.
	Infantry int `json:"infantry"`
	// SeenTrades — сделки, итог которых уже записан в журнал.
	SeenTrades map[int64]bool `json:"seenTrades"`
	// Refused — кому какой ресурс не предлагать до срока: бот отказал.
	// Ключ — «игрок:ресурс».
	Refused map[string]time.Time `json:"refused"`
	// NextBot — с какого бота начинать раздачу предложений: по кругу,
	// чтобы не заваливать одного.
	NextBot int `json:"nextBot"`
	// WarTarget — с кем воюем сейчас (одна война за раз) и с какого мига.
	WarTarget int       `json:"warTarget"`
	WarSince  time.Time `json:"warSince"`
	// StartProvinces — сколько провинций было у каждого игрока, когда бот
	// впервые увидел партию, и какой это был день: по ним оцениваем
	// стартовые армии (intel.go).
	StartProvinces map[int]int `json:"startProvinces"`
	StartDay       int         `json:"startDay"`
	// Notes — заметки прошлого захода: одинаковые подряд в журнал не пишем.
	Notes map[string]bool `json:"notes"`
}

// LoadMemory читает память партии; нет файла — чистая память.
func LoadMemory(path, gameID string) (*Memory, error) {
	m := &Memory{GameID: gameID}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, m); err != nil {
			return nil, err
		}
	}
	if m.SeenTrades == nil {
		m.SeenTrades = map[int64]bool{}
	}
	if m.Refused == nil {
		m.Refused = map[string]time.Time{}
	}
	if m.StartProvinces == nil {
		m.StartProvinces = map[int]int{}
	}
	if m.Notes == nil {
		m.Notes = map[string]bool{}
	}
	return m, nil
}

// Save пишет память атомарно: через временный файл и переименование.
func (m *Memory) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
