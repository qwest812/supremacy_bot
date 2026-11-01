package bot

// Дипломатия. Пока одно правило: всем ботам — право прохода. Отношение
// в игре одностороннее: выдаём его мы, согласия бота не нужно, а нам
// оно ничего не стоит. Войну это не исключает: объявить её можно в любой
// момент, отношение просто сменится.

import (
	"fmt"

	"supermessy_bot/internal/s1914"
)

func planDiplomacy(w *World, r Rules, mem *Memory, plan *Plan) {
	if !r.RightOfWay {
		return
	}
	targets, total := w.G.RightOfWayTargets()
	for _, p := range targets {
		// С тем, с кем воюем или собрались воевать, право прохода означало
		// бы мир: не выдаём.
		if w.Hostile(p.ID) || p.ID == mem.WarTarget {
			continue
		}
		was := "не выдавали"
		if rel, ok := w.G.Relations[p.ID]; ok {
			was = relationName(rel)
		}
		plan.add(Decision{Category: CatDiplomacy, Rule: "проход-ботам",
			What:   p.Nation + ": право прохода",
			Why:    fmt.Sprintf("бот (всего ботов %d); сейчас наше отношение — %s", total, was),
			Action: s1914.RelationAction(p.ID, s1914.RelationRightOfWay)})
	}
}

// relationName — отношение словами, по шкале клиента.
func relationName(rel int) string {
	switch rel {
	case -10:
		return "бунт"
	case -2:
		return "война"
	case -1:
		return "перемирие"
	case 0:
		return "торговое эмбарго"
	case 1:
		return "мир"
	case 2:
		return "пакт о ненападении"
	case 3:
		return "право прохода"
	case 4:
		return "общая карта"
	case 5:
		return "взаимная защита"
	case 6:
		return "общая разведка"
	case 7:
		return "командование армиями"
	}
	return fmt.Sprintf("отношение %d", rel)
}
