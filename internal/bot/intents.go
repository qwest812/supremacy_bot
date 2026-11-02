package bot

// Планы бота — что он собирается сделать дальше, словами. Нужны, чтобы
// человек, не заходя в игру, видел не только сделанное, но и задуманное:
// какие стройки ждут ресурсов и до какого часа, что откроется со сменой
// дня, с кем и как он воюет, когда придёт снова.
//
// Ничего нового здесь не решается: планы собираются из заметок этого
// захода (решений «пока не делаем, потому что…») и расписания правил.

import (
	"fmt"
	"strings"
)

// Intent — один пункт планов.
type Intent struct {
	// When — когда: «15:20», «день 3», «следующий заход».
	When     string   `json:"when"`
	Category Category `json:"category"`
	What     string   `json:"what"`
	Why      string   `json:"why"`
	Province int      `json:"province,omitempty"`
}

// intentRules — заметки каких правил означают «сделаем позже».
var intentRules = map[string]bool{
	"пункт-призыва-везде": true, "мастерская-в-дабл": true, "казарма-в-дабл": true,
	"казарма-в-столице": true, "порт-в-дабл": true, "жд-если-есть-ресурсы": true,
	"копим-на-фабрику": true, "фабрика-на-6-день": true, "машинки-в-мастерских": true,
	"купить-на-бирже": true, "обмен": true, "дефицит": true,
	"сбор-стака": true, "захват": true, "объявить-войну": true, "период-чести": true, "цель-войны": true,
}

// intents — планы после захода.
func intents(w *World, r Rules, mem *Memory, plan *Plan) []Intent {
	var out []Intent
	day := w.G.Day

	// 1. Отложенное в этом заходе: с причиной и, если известно, сроком.
	for _, d := range plan.Decisions {
		if !d.IsNote() || !intentRules[d.Rule] {
			continue
		}
		out = append(out, Intent{When: whenOf(d.Why), Category: d.Category, What: d.What, Why: d.Why, Province: d.Province})
	}

	// Наши предложения без ответа.
	for _, o := range w.G.Market.Outgoing {
		out = append(out, Intent{When: "ждёт ответа", Category: CatMarket,
			What: fmt.Sprintf("%s → %s", offerText(w, o.OffersA), nationOf(w, o.PartyB)),
			Why:  "просим " + offerText(w, o.OffersB) + "; отправлено " + clock(o.Time, w.Now)})
	}

	// 2. Расписание правил: что откроется со сменой дня.
	sched := func(from int, cat Category, what, why string) {
		if day < from {
			out = append(out, Intent{When: fmt.Sprintf("день %d", from), Category: cat, What: what, Why: why})
		}
	}
	sched(r.HarbourDay, CatBuild, "порты в двойных провинциях у моря", "по правилам открытия: порт со 2-го дня")
	sched(r.RailwayDay, CatBuild, "железные дороги в двойных и в столице", "если хватит ресурсов и останется запас денег")
	sched(r.FactoryReserveDay, CatBuild, "откладывать ресурсы на фабрику", "чтобы на следующий день хватило на неё")
	sched(r.FactoryReserveDay+1, CatBuild, "фабрика в столице", "по правилам открытия: к 6-му дню быть готовыми к фабрике")
	sched(r.OilFromDay, CatMarket, "нефть — главный дефицит", fmt.Sprintf("держим не меньше %s, докупаем впрок, когда дёшево", num(r.StrategicStock)))
	if r.War && mem.WarTarget == 0 {
		sched(r.WarFromDay, CatAttack, "объявить войну боту-соседу",
			fmt.Sprintf("самому выгодному по ценности его границы и потерям по газете, если свободных сил хотя бы %.0f", r.MinWarPower))
	}
	if mem.WarTarget != 0 {
		out = append(out, Intent{When: "каждый заход", Category: CatAttack,
			What: "брать провинции: " + nationOf(w, mem.WarTarget),
			Why:  fmt.Sprintf("до %d захватов за заход, на защищённые — стаком с перевесом ×%.1f", r.MaxOps, r.AttackRatio)})
	}

	// 3. Ближайшее событие партии, к которому бот вернётся.
	if !plan.Next.IsZero() {
		out = append(out, Intent{When: clock(plan.Next, w.Now), Category: CatBuild, What: "ближайшее событие", Why: plan.NextWhy})
	}
	return out
}

// whenOf — срок из причины заметки: «накопится к 15:20», «с 25.10 15:04».
func whenOf(why string) string {
	for _, mark := range []string{"накопится к ", "занят до ", "нападать можно с ", "мирный период до "} {
		if i := strings.Index(why, mark); i >= 0 {
			rest := why[i+len(mark):]
			if j := strings.IndexAny(rest, ",;("); j >= 0 {
				rest = rest[:j]
			}
			return strings.TrimSpace(rest)
		}
	}
	if strings.Contains(why, "до ") && strings.Contains(why, "-го дня") {
		return "позже"
	}
	return "когда сможем"
}
