package bot

import (
	"os"
	"strings"
	"testing"
	"time"

	"supermessy_bot/internal/s1914"
)

// Записанная партия: x4, второй день, мы — игрок 6 (HAR 14.10.2026).
func fixtureWorld(t *testing.T) *World {
	t.Helper()
	raw, err := os.ReadFile("../s1914/testdata/state_x4_day2.json")
	if err != nil {
		t.Fatal(err)
	}
	received := time.UnixMilli(1792138756636)
	g, err := s1914.ParseState(raw, "test", 6, received)
	if err != nil {
		t.Fatal(err)
	}
	mraw, err := os.ReadFile("../s1914/testdata/map_51_5.json")
	if err != nil {
		t.Fatal(err)
	}
	geo, err := s1914.ParseMap("51_5", mraw)
	if err != nil {
		t.Fatal(err)
	}
	return NewWorld(g, geo, g.ServerTime)
}

func TestDecideFixture(t *testing.T) {
	w := fixtureWorld(t)
	mem, _ := LoadMemory(t.TempDir()+"/m.json", "test")
	plan := Decide(w, DefaultRules(), mem)
	for _, d := range plan.Decisions {
		mark := "ДЕЛАЕМ"
		if d.IsNote() {
			mark = "заметка"
		}
		t.Logf("%-10s %-7s [%s] %s — %s", d.Category.Title(), mark, d.Rule, d.What, d.Why)
	}
	t.Logf("следующий заход: %s (%s)", plan.Next.Format("15:04"), plan.NextWhy)
	if plan.Next.IsZero() {
		t.Error("не назначен следующий заход")
	}
	for _, d := range plan.Actions() {
		if strings.TrimSpace(d.Why) == "" {
			t.Errorf("решение без причины: %+v", d)
		}
	}
}
