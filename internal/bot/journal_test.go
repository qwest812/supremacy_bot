package bot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalFilesAndNoteDedup(t *testing.T) {
	w := fixtureWorld(t)
	dir := t.TempDir()
	note := Decision{Category: CatBuild, Rule: "r", What: "Берлин: Порт", Why: "ждём 2-го дня"}
	act := Decision{Category: CatMarket, Rule: "продажа-ботам", What: "дерево", Why: "излишек",
		Action: map[string]any{"@c": "x"}}

	j, err := OpenJournal(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	j.Decision(w, note, "")
	j.Decision(w, act, "сделано")
	prev := j.Notes()
	j.Close()

	// Следующий заход: та же заметка второй раз не пишется.
	j, _ = OpenJournal(dir, false)
	j.SetPrevNotes(prev)
	j.Decision(w, note, "")
	j.Close()

	read := func(name string) string {
		raw, _ := os.ReadFile(filepath.Join(dir, name))
		return string(raw)
	}
	if n := strings.Count(read("all.log"), "Берлин: Порт"); n != 1 {
		t.Errorf("заметка в общем журнале %d раз, ждали 1", n)
	}
	if !strings.Contains(read("build.log"), "Берлин: Порт") || strings.Contains(read("build.log"), "дерево") {
		t.Error("журнал стройки собран неверно")
	}
	if !strings.Contains(read("market.log"), "СДЕЛАНО") {
		t.Error("в журнале рынка нет сделанного решения")
	}
	if strings.Count(read("decisions.jsonl"), "\n") != 2 {
		t.Error("в decisions.jsonl не две строки")
	}
}
