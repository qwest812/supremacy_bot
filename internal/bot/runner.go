package bot

// Цикл бота: заход в партию → решения → приказы → журнал → сон до
// следующего события. Заход игра засчитывает как вход в партию, поэтому
// ходим не по таймеру, а к событию (стройка кончится, ресурсы накопятся,
// день сменится), и не чаще VisitMin. К сроку добавляется случайный
// разброс: ровный ритм круглые сутки — подпись робота.
//
// В одном заходе бывает несколько кругов: пункт призыва в x4 строится
// за секунды, и ждать ради следующего здания отдельного захода незачем.
// Тогда бот выжидает немного и перечитывает партию, не входя заново.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"time"

	"supermessy_bot/internal/s1914"
)

// Config — как бот ходит в партию.
type Config struct {
	GameID string
	// Dry — только смотреть и писать, что сделал бы; приказы не отправлять.
	Dry bool
	// VisitMin и VisitMax — границы паузы между заходами.
	VisitMin, VisitMax time.Duration
	// Rounds — сколько кругов решений в одном заходе самое большее.
	Rounds int
	// ActionPause — пауза между приказами: человек тоже не жмёт десять
	// кнопок за одну миллисекунду.
	ActionPause time.Duration
	LogDir      string
	DataDir     string
	Rules       Rules
}

// Runner ведёт одну партию.
type Runner struct {
	c   *s1914.Client
	cfg Config
	log *slog.Logger
}

func NewRunner(c *s1914.Client, cfg Config, log *slog.Logger) *Runner {
	if cfg.Rounds < 1 {
		cfg.Rounds = 1
	}
	return &Runner{c: c, cfg: cfg, log: log}
}

// Run ходит в партию, пока не отменят ctx или партия не кончится.
func (r *Runner) Run(ctx context.Context) error {
	for {
		next, err := r.Visit(ctx)
		switch {
		case errors.Is(err, errGameOver):
			r.log.Info("партия окончена, бот останавливается", "gameID", r.cfg.GameID)
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			// Неудачный заход — не повод долбить игру: следующая попытка
			// через обычную паузу.
			r.log.Error("заход не удался", "err", err)
			next = time.Now().Add(r.jitter(r.cfg.VisitMin))
		}
		r.log.Info("следующий заход", "в", next.Format("15:04:05"), "через", time.Until(next).Round(time.Second))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(next)):
		}
	}
}

var errGameOver = errors.New("партия окончена")

// Visit — один заход. Возвращает, когда приходить снова.
func (r *Runner) Visit(ctx context.Context) (time.Time, error) {
	v, err := r.c.Enter(ctx, r.cfg.GameID)
	if err != nil {
		return time.Time{}, fmt.Errorf("вход в партию %s: %w", r.cfg.GameID, err)
	}
	geo, err := r.c.MapGeometry(ctx, v.State.MapID)
	if err != nil {
		// Без карты бот слепнет наполовину (соседи, побережье), но экономику
		// вести может.
		r.log.Warn("карта не скачалась, играем без неё", "карта", v.State.MapID, "err", err)
		geo = nil
	}

	memPath := filepath.Join(r.cfg.DataDir, r.cfg.GameID+".json")
	mem, err := LoadMemory(memPath, r.cfg.GameID)
	if err != nil {
		return time.Time{}, fmt.Errorf("память партии: %w", err)
	}
	j, err := OpenJournal(filepath.Join(r.cfg.LogDir, r.cfg.GameID), r.cfg.Dry)
	if err != nil {
		return time.Time{}, fmt.Errorf("журнал: %w", err)
	}
	defer j.Close()
	j.SetPrevNotes(mem.Notes)

	// Газета: кто сколько потерял в войнах. Читается наблюдателем
	// и входом в партию не считается; не прочиталась — играем без неё.
	losses, err := r.c.GameLosses(ctx, r.cfg.GameID, v.State.Day)
	if err != nil {
		r.log.Warn("газета не прочиталась", "err", err)
		losses = nil
	}

	var plan *Plan
	var first *World
	rec := &visitLog{}
	done := map[Category]int{}
	for round := 0; round < r.cfg.Rounds; round++ {
		w := NewWorld(v.State, geo, time.Now())
		w.Losses = losses
		if round == 0 {
			first = w
			j.Visit(w, mem)
			j.WriteMap(w)
		} else {
			j.Line("— круг %d того же захода", round+1)
		}
		// Торговые предложения — только в первом круге: на круг их лимит,
		// а круги идут друг за другом за минуту.
		rules := r.cfg.Rules
		if round > 0 {
			rules.TradeOffersPerVisit = 0
		}
		plan = Decide(w, rules, mem)
		sent := r.execute(ctx, v, w, plan, j, done, rec)
		if v.State.Ended {
			j.Snapshot(first, mem, rec.decisions, rec.outcomes, plan.Intents)
			mem.Notes = j.Notes()
			_ = mem.Save(memPath)
			return time.Time{}, errGameOver
		}
		// Ещё круг — только если что-то отправили и следующее событие
		// наступит совсем скоро (быстрая стройка).
		soon := time.Until(plan.Next)
		if r.cfg.Dry || sent == 0 || round == r.cfg.Rounds-1 || soon > 2*time.Minute {
			break
		}
		wait := max(soon, 5*time.Second) + time.Duration(rand.IntN(5))*time.Second
		j.Line("ждём %s и перечитываем партию: %s", wait.Round(time.Second), plan.NextWhy)
		select {
		case <-ctx.Done():
			return time.Time{}, ctx.Err()
		case <-time.After(wait):
		}
		if err := v.Refresh(ctx); err != nil {
			j.Line("перечитать партию не вышло: %v", err)
			break
		}
	}

	j.Snapshot(first, mem, rec.decisions, rec.outcomes, plan.Intents)
	mem.Visits++
	mem.LastVisit = time.Now()
	mem.Notes = j.Notes()
	if err := mem.Save(memPath); err != nil {
		r.log.Warn("память не сохранилась", "err", err)
	}

	next, why := r.nextVisit(plan)
	j.End(Summary(plan, done), next, why)
	r.log.Info("заход окончен", "день", v.State.Day, "итог", Summary(plan, done))
	return next, nil
}

// visitLog — все решения захода (всех кругов) и чем они кончились:
// из них собирается снимок для карты.
type visitLog struct {
	decisions []Decision
	outcomes  []string
}

func (l *visitLog) add(d Decision, outcome string) {
	l.decisions = append(l.decisions, d)
	l.outcomes = append(l.outcomes, outcome)
}

// execute пишет решения в журнал и отправляет приказы. Возвращает,
// сколько приказов ушло.
func (r *Runner) execute(ctx context.Context, v *s1914.Visit, w *World, plan *Plan, j *Journal,
	done map[Category]int, rec *visitLog) int {
	sent := 0
	for _, d := range plan.Decisions {
		if d.IsNote() {
			j.Decision(w, d, "")
			rec.add(d, "")
			continue
		}
		if r.cfg.Dry {
			j.Decision(w, d, "")
			rec.add(d, "план")
			done[d.Category]++
			continue
		}
		if sent > 0 && r.cfg.ActionPause > 0 {
			pause := r.cfg.ActionPause/2 + time.Duration(rand.Int64N(int64(r.cfg.ActionPause)))
			select {
			case <-ctx.Done():
				return sent
			case <-time.After(pause):
			}
		}
		if err := v.Send(ctx, d.Action); err != nil {
			j.Decision(w, d, "отказ игры: "+err.Error())
			rec.add(d, "отказ игры: "+err.Error())
			r.log.Warn("игра не приняла приказ", "что", d.What, "err", err)
			continue
		}
		sent++
		done[d.Category]++
		j.Decision(w, d, "сделано")
		rec.add(d, "сделано")
	}
	return sent
}

// nextVisit — когда приходить: к ближайшему событию партии, но не раньше
// VisitMin и не позже VisitMax, плюс разброс.
func (r *Runner) nextVisit(plan *Plan) (time.Time, string) {
	now := time.Now()
	at, why := plan.Next, plan.NextWhy
	switch {
	case at.IsZero() || at.After(now.Add(r.cfg.VisitMax)):
		at, why = now.Add(r.cfg.VisitMax), "плановый обход"
	case at.Before(now.Add(r.cfg.VisitMin)):
		at = now.Add(r.cfg.VisitMin)
	}
	return at.Add(r.jitter(r.cfg.VisitMin) / 3), why
}

// jitter — случайная добавка от нуля до d.
func (r *Runner) jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d)))
}
