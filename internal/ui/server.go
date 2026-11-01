// Package ui — веб-интерфейс к журналам бота: заходы, решения с причинами
// и карта партии на каждый заход. Только читает то, что бот пишет
// в LOG_DIR, поэтому работает и рядом с играющим ботом, и сам по себе.
package ui

import (
	"bufio"
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed static
var static embed.FS

// gameID — номер партии в адресе: только цифры, буквы и дефис, чтобы
// из адреса нельзя было выйти за LOG_DIR.
var gameID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type Server struct {
	dir string
	log *slog.Logger
}

func New(logDir string, log *slog.Logger) *Server { return &Server{dir: logDir, log: log} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(static, "static")
	mux.Handle("GET /", http.FileServerFS(sub))
	mux.HandleFunc("GET /api/games", s.games)
	mux.HandleFunc("GET /api/games/{id}/events", s.events)
	mux.HandleFunc("GET /api/games/{id}/map", s.file("map.json"))
	mux.HandleFunc("GET /api/games/{id}/snapshots/{visit}", s.snapshot)
	return mux
}

// ListenAndServe поднимает интерфейс на addr.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

type gameInfo struct {
	ID      string          `json:"id"`
	Updated time.Time       `json:"updated"`
	Visits  int             `json:"visits"`
	Last    json.RawMessage `json:"last,omitempty"`
}

func (s *Server) games(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.dir)
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := []gameInfo{}
	for _, e := range entries {
		if !e.IsDir() || !gameID.MatchString(e.Name()) {
			continue
		}
		path := filepath.Join(s.dir, e.Name(), "decisions.jsonl")
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		g := gameInfo{ID: e.Name(), Updated: st.ModTime()}
		_ = eachLine(path, func(line []byte) {
			var head struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(line, &head) == nil && head.Type == "visit" {
				g.Visits++
				g.Last = append(json.RawMessage(nil), line...)
			}
		})
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	writeJSON(w, out)
}

// events — все строки decisions.jsonl партии массивом. after — сколько
// строк клиент уже видел: отдаются только новые.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !gameID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	var lines []json.RawMessage
	n := 0
	err := eachLine(filepath.Join(s.dir, id, "decisions.jsonl"), func(line []byte) {
		n++
		if n > after && json.Valid(line) {
			lines = append(lines, append(json.RawMessage(nil), line...))
		}
	})
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if lines == nil {
		lines = []json.RawMessage{}
	}
	writeJSON(w, map[string]any{"total": n, "events": lines})
}

func (s *Server) file(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !gameID.MatchString(id) {
			http.NotFound(w, r)
			return
		}
		serveJSONFile(w, r, filepath.Join(s.dir, id, name))
	}
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	id, visit := r.PathValue("id"), r.PathValue("visit")
	if !gameID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	if _, err := strconv.Atoi(visit); err != nil {
		http.NotFound(w, r)
		return
	}
	serveJSONFile(w, r, filepath.Join(s.dir, id, "snapshots", visit+".json"))
}

func serveJSONFile(w http.ResponseWriter, r *http.Request, path string) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(raw)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(v)
}

func eachLine(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			fn([]byte(line))
		}
	}
	return sc.Err()
}
