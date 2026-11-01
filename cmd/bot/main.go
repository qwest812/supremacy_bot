// Команда bot — бот, который сам играет партию Supremacy 1914.
//
//	go run ./cmd/bot            играть партию GAME_ID (или единственную подходящую)
//	go run ./cmd/bot -once      один заход и выход
//	go run ./cmd/bot -games     наши партии
//	go run ./cmd/bot -lobby     открытые партии под фильтр LOBBY_TITLE/LOBBY_SPEED
//	go run ./cmd/bot -state f.json -map m.json -me 6
//	                            решения по сохранённому состоянию, без сети
//	go run ./cmd/bot -ui        только интерфейс к журналам, без игры
//	go run ./cmd/bot -demo      демо-заходы по записанной партии и интерфейс
//
// Интерфейс поднимается и вместе с ботом, на UI_ADDR (http://127.0.0.1:8090).
//
// Настройки — в .env рядом (см. .env.example). По умолчанию DRY_RUN=true:
// бот заходит в партию и пишет в журнал, что сделал бы, но ничего
// не отправляет.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"supermessy_bot/internal/bot"
	"supermessy_bot/internal/s1914"
	"supermessy_bot/internal/ui"
)

func main() {
	var (
		envFile = flag.String("env", ".env", "файл с настройками")
		once    = flag.Bool("once", false, "один заход и выход")
		games   = flag.Bool("games", false, "показать наши партии и выйти")
		lobby   = flag.Bool("lobby", false, "показать открытые партии под фильтр и выйти")
		paper   = flag.String("paper", "", "показать потери по газете партии (номер партии) и выйти")
		state   = flag.String("state", "", "решить по сохранённому состоянию (ответ игрового сервера), без сети")
		mapFile = flag.String("map", "", "файл карты к -state")
		me      = flag.Int("me", 0, "наш номер игрока в -state")
		uiOnly  = flag.Bool("ui", false, "только интерфейс к журналам, в игру не ходить")
		demo    = flag.Bool("demo", false, "записать демо-заходы по записанной партии и открыть интерфейс")
	)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := loadDotEnv(*envFile); err != nil {
		fail(log, err)
	}
	cfg, err := loadSettings()
	if err != nil {
		fail(log, err)
	}

	if *state != "" {
		if err := offline(*state, *mapFile, *me, cfg.Bot.Rules); err != nil {
			fail(log, err)
		}
		return
	}

	if *demo {
		load := func() (*s1914.GameState, *s1914.MapGeometry, error) {
			raw, err := os.ReadFile("internal/s1914/testdata/state_x4_day2.json")
			if err != nil {
				return nil, nil, fmt.Errorf("демо запускается из корня проекта: %w", err)
			}
			g, err := s1914.ParseState(raw, "demo", 6, time.Now())
			if err != nil {
				return nil, nil, err
			}
			mraw, err := os.ReadFile("internal/s1914/testdata/map_51_5.json")
			if err != nil {
				return nil, nil, err
			}
			geo, err := s1914.ParseMap("51_5", mraw)
			return g, geo, err
		}
		if err := os.RemoveAll(filepath.Join(cfg.Bot.LogDir, "demo")); err != nil {
			fail(log, err)
		}
		if err := bot.WriteDemo(load, cfg.Bot.LogDir, cfg.Bot.Rules); err != nil {
			fail(log, err)
		}
		log.Info("демо-заходы записаны", "журналы", filepath.Join(cfg.Bot.LogDir, "demo"))
		*uiOnly = true
	}
	if *uiOnly {
		addr := cfg.UIAddr
		if addr == "" {
			addr = "127.0.0.1:8090"
		}
		log.Info("интерфейс", "адрес", "http://"+addr)
		if err := ui.New(cfg.Bot.LogDir, log).ListenAndServe(addr); err != nil {
			fail(log, err)
		}
		return
	}

	if cfg.User == "" || cfg.Pass == "" {
		fail(log, errors.New("впишите S1914_USER и S1914_PASSWORD в .env"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := s1914.NewClient(cfg.User, cfg.Pass, cfg.Lang, log)
	client.UseSessionStore(fileSessions{path: filepath.Join(cfg.Bot.DataDir, "session.json")})

	switch {
	case *games:
		err = listMine(ctx, client)
	case *lobby:
		err = listLobby(ctx, client, cfg)
	case *paper != "":
		err = showPaper(ctx, client, *paper)
	default:
		if cfg.Bot.GameID == "" {
			cfg.Bot.GameID, err = pickGame(ctx, client, cfg)
			if err != nil {
				fail(log, err)
			}
		}
		if cfg.UIAddr != "" {
			go func() {
				log.Info("интерфейс", "адрес", "http://"+cfg.UIAddr)
				if err := ui.New(cfg.Bot.LogDir, log).ListenAndServe(cfg.UIAddr); err != nil {
					log.Warn("интерфейс не поднялся", "err", err)
				}
			}()
		}
		runner := bot.NewRunner(client, cfg.Bot, log)
		log.Info("бот запущен", "партия", cfg.Bot.GameID, "только смотрим", cfg.Bot.Dry,
			"журналы", filepath.Join(cfg.Bot.LogDir, cfg.Bot.GameID))
		if *once {
			_, err = runner.Visit(ctx)
		} else {
			err = runner.Run(ctx)
		}
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fail(log, err)
	}
}

func fail(log *slog.Logger, err error) {
	log.Error(err.Error())
	os.Exit(1)
}

// matches — подходит ли партия под фильтр: название и скорость.
func matches(g s1914.Game, cfg settings) bool {
	title := strings.ToLower(g.Title)
	return strings.Contains(title, strings.ToLower(cfg.LobbyTitle)) &&
		(cfg.LobbySpeed <= 0 || g.Speed() == cfg.LobbySpeed) &&
		!strings.Contains(title, "tutorial")
}

// pickGame — партия, когда GAME_ID не задан: единственная наша партия под
// фильтр. Несколько — пусть выберет человек: бот ведёт одну за раз.
func pickGame(ctx context.Context, c *s1914.Client, cfg settings) (string, error) {
	mine, err := c.MyGames(ctx)
	if err != nil {
		return "", fmt.Errorf("список наших партий: %w", err)
	}
	var fit []s1914.Game
	for _, g := range mine {
		if matches(g, cfg) {
			fit = append(fit, g)
		}
	}
	switch len(fit) {
	case 1:
		return fit[0].GameID, nil
	case 0:
		return "", fmt.Errorf("нет нашей партии «%s» x%.0f: вступите в партию в игре или впишите GAME_ID", cfg.LobbyTitle, cfg.LobbySpeed)
	}
	var ids []string
	for _, g := range fit {
		ids = append(ids, fmt.Sprintf("%s (%s, день %s)", g.GameID, g.Title, g.DayOfGame))
	}
	return "", fmt.Errorf("подходящих партий несколько, впишите одну в GAME_ID: %s", strings.Join(ids, "; "))
}

func listMine(ctx context.Context, c *s1914.Client) error {
	mine, err := c.MyGames(ctx)
	if err != nil {
		return err
	}
	if len(mine) == 0 {
		fmt.Println("идущих партий нет")
	}
	for _, g := range mine {
		fmt.Printf("%s  x%.0f  день %-3s  %s\n", g.GameID, g.Speed(), g.DayOfGame, g.Title)
	}
	return nil
}

func listLobby(ctx context.Context, c *s1914.Client, cfg settings) error {
	open, err := c.OpenGames(ctx)
	if err != nil {
		return err
	}
	n := 0
	for _, g := range open {
		if !matches(g, cfg) {
			continue
		}
		n++
		fmt.Printf("%s  x%.0f  день %-3s  свободно %-3s игроков %-3s  %s\n",
			g.GameID, g.Speed(), g.DayOfGame, g.OpenSlots, g.NrOfPlayers, g.Title)
	}
	fmt.Printf("подходит под «%s» x%.0f: %d из %d открытых\n", cfg.LobbyTitle, cfg.LobbySpeed, n, len(open))
	return nil
}

// offline — решения по сохранённому состоянию: проверить правила, не
// заходя в игру.
func offline(statePath, mapPath string, me int, rules bot.Rules) error {
	raw, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}
	g, err := s1914.ParseState(raw, "offline", me, time.Now())
	if err != nil {
		return err
	}
	var geo *s1914.MapGeometry
	if mapPath != "" {
		mraw, err := os.ReadFile(mapPath)
		if err != nil {
			return err
		}
		if geo, err = s1914.ParseMap(g.MapID, mraw); err != nil {
			return err
		}
	}
	// Состояние записано в прошлом: «сейчас» для решений — его миг.
	now := g.ServerTime
	if now.IsZero() {
		now = time.Now()
	}
	w := bot.NewWorld(g, geo, now)
	mem := &bot.Memory{SeenTrades: map[int64]bool{}, Refused: map[string]time.Time{}, Notes: map[string]bool{}}
	plan := bot.Decide(w, rules, mem)
	for _, d := range plan.Decisions {
		mark := "ДЕЛАЕМ "
		if d.IsNote() {
			mark = "заметка"
		}
		fmt.Printf("%-10s %s [%s] %s — %s\n", d.Category.Title(), mark, d.Rule, d.What, d.Why)
	}
	fmt.Printf("следующий заход: %s — %s\n", plan.Next.Format("02.01 15:04"), plan.NextWhy)
	return nil
}

// fileSessions — подпись входа в файле: перезапуск бота не должен быть
// поводом входить на сайт заново, частые входы сайт считает подозрительными.
type fileSessions struct{ path string }

func (f fileSessions) LoadSession(context.Context) (s1914.SavedSession, bool, error) {
	raw, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		return s1914.SavedSession{}, false, nil
	}
	if err != nil {
		return s1914.SavedSession{}, false, err
	}
	var s s1914.SavedSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return s1914.SavedSession{}, false, err
	}
	return s, true, nil
}

func (f fileSessions) SaveSession(_ context.Context, s s1914.SavedSession) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, raw, 0o600)
}

// showPaper печатает потери в войнах по газете партии. Заходит
// наблюдателем: в партии ничего не меняется.
func showPaper(ctx context.Context, c *s1914.Client, gameID string) error {
	g, err := c.ObserveGame(ctx, gameID)
	if err != nil {
		return err
	}
	l, err := c.GameLosses(ctx, gameID, g.Day)
	if err != nil {
		return err
	}
	fmt.Printf("партия %s, день %d, прочитано дней газеты: %d\n", gameID, g.Day, l.Days)
	if len(l.Missing) > 0 {
		fmt.Printf("игра не отдала дни %v: %s\n", l.Missing, l.Why)
	}
	provinces := map[int]int{}
	for _, p := range g.Provinces {
		provinces[p.Owner]++
	}
	for _, p := range l.Players {
		fmt.Printf("  %-28s потери %7d  уничтожено %7d  провинций %3d\n", p.Name, p.Lost, p.Killed, provinces[p.ID])
	}
	return nil
}
