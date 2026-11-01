package bot

// Разведка по газете. Каждый день газета печатает по каждой идущей войне
// потери обеих сторон (s1914/paper.go). На стандартной карте в начале
// партии в каждой провинции стоит StartGarrison пехоты, поэтому стартовую
// армию страны знаем: число её провинций на старте, помноженное на это.
// Кто потерял большую долю стартовой армии, тот ослаб, и на него выгодно
// напасть: его видимая оборона у границы может ещё держаться, а резервов
// за ней уже нет.
//
// Провинции на старте помним с первого захода (Memory.StartProvinces).
// Если бот пришёл в партию не с первого дня, это число приблизительное:
// страна могла уже что-то потерять или захватить. Об этом пишем в журнал.

import (
	"fmt"
	"sort"
	"strings"
)

// menPerUnit — газета считает солдат, а армии — юниты: юнит — тысяча
// солдат. Видно по живой газете (партия 10937470, 30.10.2026): все
// потери кратны тысяче, «Великобритания — 148 000».
const menPerUnit = 1000

// weakness — что газета говорит о силе игрока. Всё в юнитах.
type weakness struct {
	lost   int64   // потерял за партию
	killed int64   // уничтожил чужих
	start  float64 // стартовая армия, оценка
	share  float64 // доля потерянного от стартовой армии, 0…1
}

// rememberStart — запомнить, сколько провинций у каждого было, когда мы
// впервые увидели партию.
func rememberStart(w *World, mem *Memory) {
	if mem.StartProvinces == nil {
		mem.StartProvinces = map[int]int{}
	}
	if len(mem.StartProvinces) > 0 {
		return
	}
	for _, p := range w.G.Provinces {
		if p.Owner > 0 {
			mem.StartProvinces[p.Owner]++
		}
	}
	mem.StartDay = w.G.Day
}

// weaknessOf — доля потерь игрока по газете. Без газеты — нули.
func (w *World) weaknessOf(player int, r Rules, mem *Memory) weakness {
	var out weakness
	if w.Losses == nil {
		return out
	}
	for _, p := range w.Losses.Players {
		if p.ID == player {
			out.lost, out.killed = p.Lost/menPerUnit, p.Killed/menPerUnit
			break
		}
	}
	out.start = float64(mem.StartProvinces[player]) * r.StartGarrison
	if out.start > 0 {
		out.share = min(float64(out.lost)/out.start, 1)
	}
	return out
}

// noteIntel — сводка газеты в журнал: кто из соседей сколько потерял.
func noteIntel(w *World, r Rules, mem *Memory, plan *Plan) {
	if w.Losses == nil {
		plan.note(CatAttack, "газета", "газета", "газету в этот заход не прочитали — о потерях соседей не знаем")
		return
	}
	if len(w.Losses.Missing) > 0 {
		plan.note(CatAttack, "газета", "газета",
			fmt.Sprintf("игра не отдала дни %v (%s) — потери посчитаны не полностью", w.Losses.Missing, w.Losses.Why))
	}
	type row struct {
		id int
		wk weakness
	}
	var rows []row
	for id, pl := range w.G.Players {
		if id == w.G.Me || pl.Defeated || pl.Retired {
			continue
		}
		if wk := w.weaknessOf(id, r, mem); wk.lost > 0 {
			rows = append(rows, row{id, wk})
		}
	}
	if len(rows) == 0 {
		plan.note(CatAttack, "газета", "газета", fmt.Sprintf("прочитано дней: %d; потерь в войнах пока нет ни у кого", w.Losses.Days))
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].wk.share != rows[j].wk.share {
			return rows[i].wk.share > rows[j].wk.share
		}
		return rows[i].id < rows[j].id
	})
	var parts []string
	for _, x := range rows {
		parts = append(parts, fmt.Sprintf("%s: потери %s из ~%s стартовых (%.0f%%), уничтожено врагов %s",
			nationOf(w, x.id), num(float64(x.wk.lost)), num(x.wk.start), x.wk.share*100, num(float64(x.wk.killed))))
	}
	why := strings.Join(parts, "; ")
	if mem.StartDay > 1 {
		why += fmt.Sprintf(". Стартовые армии оценены по провинциям %d-го дня — приблизительно", mem.StartDay)
	}
	plan.note(CatAttack, "газета", "потери по газете", why)
}

func nationOf(w *World, player int) string {
	if p, ok := w.G.Players[player]; ok && p.Nation != "" {
		return p.Nation
	}
	return fmt.Sprintf("игрок %d", player)
}
