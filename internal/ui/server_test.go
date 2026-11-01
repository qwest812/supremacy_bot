package ui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServer(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "123")
	os.MkdirAll(filepath.Join(game, "snapshots"), 0o755)
	os.WriteFile(filepath.Join(game, "decisions.jsonl"), []byte(
		`{"type":"visit","visit":1,"day":2,"nation":"Швеция"}`+"\n"+
			`{"type":"decision","visit":1,"category":"build","what":"x"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(game, "snapshots", "1.json"), []byte(`{"visit":1}`), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.json"), []byte(`{"secret":1}`), 0o644)

	h := New(dir, slog.Default()).Handler()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	var games []gameInfo
	json.Unmarshal(get("/api/games").Body.Bytes(), &games)
	if len(games) != 1 || games[0].ID != "123" || games[0].Visits != 1 {
		t.Fatalf("список партий: %+v", games)
	}
	var ev struct {
		Total  int               `json:"total"`
		Events []json.RawMessage `json:"events"`
	}
	json.Unmarshal(get("/api/games/123/events?after=1").Body.Bytes(), &ev)
	if ev.Total != 2 || len(ev.Events) != 1 {
		t.Fatalf("события после первой строки: %+v", ev)
	}
	if rec := get("/api/games/123/snapshots/1"); rec.Code != 200 {
		t.Fatalf("снимок: %d", rec.Code)
	}
	for _, bad := range []string{"/api/games/../snapshots/1", "/api/games/123/snapshots/..%2F..%2Fsecret", "/api/games/x.y/map"} {
		if rec := get(bad); rec.Code == 200 && strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("%s отдал файл вне партии", bad)
		}
	}
	if rec := get("/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Supermessy") {
		t.Fatalf("страница: %d", rec.Code)
	}
}
