package service

import (
	"sort"
	"strings"
	"time"

	"dconnector/internal/core/model"
)

// Límites de una recurrencia.
const (
	recurMaxEvery     = 365
	recurMaxCount     = 1000
	recurMinMinutes   = 5   // intervalo mínimo de "custom/minutes": evita saturar el canal
	recurPreviewMax   = 200 // tope de fechas de Preview
	recurSearchGuard  = 100000
	recurMaxYear      = 9999
	recurDefaultEvery = 1
)

// normalizeRecurrence deja la regla en su forma canónica: minúsculas, Every 0 = 1, Unit solo en
// "custom", Weekdays solo en "weekly" (sin repetidos y ordenados). No valida.
func normalizeRecurrence(r model.Recurrence) model.Recurrence {
	r.Frequency = strings.ToLower(strings.TrimSpace(r.Frequency))
	r.Unit = strings.ToLower(strings.TrimSpace(r.Unit))
	if r.Every == 0 {
		r.Every = recurDefaultEvery
	}
	if r.Frequency != model.RecurCustom {
		r.Unit = ""
	}
	if r.Frequency != model.RecurWeekly {
		r.Weekdays = nil
	} else if len(r.Weekdays) > 0 {
		seen := map[int]bool{}
		days := make([]int, 0, len(r.Weekdays))
		for _, d := range r.Weekdays {
			if !seen[d] {
				seen[d] = true
				days = append(days, d)
			}
		}
		sort.Ints(days)
		r.Weekdays = days
	}
	return r
}

// ValidateRecurrence comprueba una regla (first = fecha del primer envío, en ms). Los errores
// son legibles y envuelven ErrInvalid.
func ValidateRecurrence(r model.Recurrence, first int64) error {
	r = normalizeRecurrence(r)
	switch r.Frequency {
	case model.RecurDaily, model.RecurWeekly, model.RecurMonthly, model.RecurYearly:
	case model.RecurCustom:
		switch r.Unit {
		case "minutes", "hours", "days", "weeks", "months":
		default:
			return msgInvalid("la repetición personalizada necesita una unidad: minutos, horas, días, semanas o meses")
		}
	default:
		return msgInvalid("la frecuencia de repetición debe ser diaria, semanal, mensual, anual o personalizada")
	}
	if r.Every < 1 || r.Every > recurMaxEvery {
		return msgInvalid("la repetición debe ser cada 1 a %d veces la unidad", recurMaxEvery)
	}
	if r.Frequency == model.RecurCustom && r.Unit == "minutes" && r.Every < recurMinMinutes {
		return msgInvalid("la repetición mínima es cada %d minutos", recurMinMinutes)
	}
	for _, d := range r.Weekdays {
		if d < 0 || d > 6 {
			return msgInvalid("los días de la semana van de 0 (domingo) a 6 (sábado)")
		}
	}
	if r.MaxCount < 0 || r.MaxCount > recurMaxCount {
		return msgInvalid("el máximo de repeticiones es %d", recurMaxCount)
	}
	if r.Count < 0 {
		return msgInvalid("el contador de repeticiones no puede ser negativo")
	}
	if r.EndAt != 0 && r.EndAt <= first {
		return msgInvalid("la fecha final de la repetición debe ser posterior al primer envío")
	}
	return nil
}

// NextOccurrence devuelve la siguiente ocurrencia de la serie posterior a `after` (ms) y a
// `first` (la primera fecha de la serie, ancla de la hora de pared y del día del mes original).
// Las fechas se calculan en la hora local de la máquina. ok es false si la serie ya terminó:
// la ocurrencia pasaría de r.EndAt o r.Count ya llegó a r.MaxCount (Count = cuántas
// ocurrencias están cerradas, contando la que acaba de enviarse).
//
//   - daily/custom días: cada N días a la misma hora de pared.
//   - weekly: en los días de r.Weekdays (vacío = el de first), cada N semanas (las semanas
//     empiezan el domingo y se cuentan desde la de first).
//   - monthly/custom meses: cada N meses; un día 29–31 se ajusta al último día del mes y
//     vuelve a su día original cuando el mes lo tiene.
//   - yearly: cada N años (29 de febrero → 28 en años no bisiestos).
//   - custom minutos/horas: intervalos absolutos.
func NextOccurrence(r model.Recurrence, first, after int64) (next int64, ok bool) {
	return nextOccurrenceIn(r, first, after, time.Local)
}

func nextOccurrenceIn(r model.Recurrence, first, after int64, loc *time.Location) (int64, bool) {
	r = normalizeRecurrence(r)
	if ValidateRecurrence(r, first) != nil {
		// Una regla inválida no produce fechas.
		return 0, false
	}
	if r.MaxCount > 0 && r.Count >= r.MaxCount {
		return 0, false
	}
	var (
		t  int64
		ok bool
	)
	switch r.Frequency {
	case model.RecurDaily:
		t, ok = nextByCalendar(first, after, loc, stepDays, r.Every)
	case model.RecurMonthly:
		t, ok = nextByCalendar(first, after, loc, stepMonths, r.Every)
	case model.RecurYearly:
		t, ok = nextByCalendar(first, after, loc, stepYears, r.Every)
	case model.RecurWeekly:
		t, ok = nextWeekly(r, first, after, loc)
	case model.RecurCustom:
		switch r.Unit {
		case "minutes":
			t, ok = nextAbsolute(first, after, time.Duration(r.Every)*time.Minute)
		case "hours":
			t, ok = nextAbsolute(first, after, time.Duration(r.Every)*time.Hour)
		case "days":
			t, ok = nextByCalendar(first, after, loc, stepDays, r.Every)
		case "weeks":
			t, ok = nextByCalendar(first, after, loc, stepDays, r.Every*7)
		case "months":
			t, ok = nextByCalendar(first, after, loc, stepMonths, r.Every)
		}
	}
	if !ok {
		return 0, false
	}
	if r.EndAt > 0 && t > r.EndAt {
		return 0, false
	}
	return t, true
}

type calendarStep int

const (
	stepDays calendarStep = iota
	stepMonths
	stepYears
)

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// occurrenceAt: la k-ésima ocurrencia (k >= 1) de un paso de calendario, a la hora de pared de first.
func occurrenceAt(first time.Time, step calendarStep, n, k int, loc *time.Location) (time.Time, bool) {
	y, m, d := first.Date()
	h, mi, s := first.Clock()
	ns := first.Nanosecond()
	switch step {
	case stepDays:
		return time.Date(y, m, d+k*n, h, mi, s, ns, loc), true
	case stepMonths:
		total := y*12 + int(m-1) + k*n
		ny, nm := total/12, time.Month(total%12+1)
		if ny > recurMaxYear {
			return time.Time{}, false
		}
		day := d
		if dim := daysIn(ny, nm); day > dim {
			day = dim
		}
		return time.Date(ny, nm, day, h, mi, s, ns, loc), true
	default: // años
		ny := y + k*n
		if ny > recurMaxYear {
			return time.Time{}, false
		}
		day := d
		if dim := daysIn(ny, m); day > dim {
			day = dim
		}
		return time.Date(ny, m, day, h, mi, s, ns, loc), true
	}
}

// stepUpperBound: cota superior de lo que dura un paso (para saltar directo cerca del destino).
func stepUpperBound(step calendarStep, n int) time.Duration {
	switch step {
	case stepDays:
		return time.Duration(n) * 25 * time.Hour
	case stepMonths:
		return time.Duration(n) * 32 * 24 * time.Hour
	default:
		return time.Duration(n) * 367 * 24 * time.Hour
	}
}

func nextByCalendar(first, after int64, loc *time.Location, step calendarStep, n int) (int64, bool) {
	ft := time.UnixMilli(first).In(loc)
	k := 1
	if after > first {
		// occ(k) <= first + k*cota: con k = elapsed/cota ya se cumple occ(k) <= after.
		if est := int((time.Duration(after-first) * time.Millisecond) / stepUpperBound(step, n)); est > 1 {
			k = est
		}
	}
	for i := 0; i < recurSearchGuard; i, k = i+1, k+1 {
		t, ok := occurrenceAt(ft, step, n, k, loc)
		if !ok {
			return 0, false
		}
		if ms := t.UnixMilli(); ms > after && ms > first {
			return ms, true
		}
	}
	return 0, false
}

func nextAbsolute(first, after int64, step time.Duration) (int64, bool) {
	stepMs := step.Milliseconds()
	if stepMs <= 0 {
		return 0, false
	}
	k := int64(1)
	if after >= first {
		k = (after-first)/stepMs + 1
	}
	return first + k*stepMs, true
}

// epochDay: número de día civil (días desde 1970-01-01 de la fecha de t en su zona).
func epochDay(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

func weekdayOf(day int64) int     { return int(((day+4)%7 + 7) % 7) } // 1970-01-01 fue jueves
func weekIndexOf(day int64) int64 { return floorDiv(day+4, 7) }       // semanas que empiezan el domingo

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func nextWeekly(r model.Recurrence, first, after int64, loc *time.Location) (int64, bool) {
	ft := time.UnixMilli(first).In(loc)
	h, mi, s := ft.Clock()
	ns := ft.Nanosecond()
	firstDay := epochDay(ft)
	allowed := map[int]bool{}
	if len(r.Weekdays) == 0 {
		allowed[weekdayOf(firstDay)] = true
	}
	for _, d := range r.Weekdays {
		allowed[d] = true
	}
	from := after
	if first > from {
		from = first
	}
	start := epochDay(time.UnixMilli(from).In(loc))
	if start < firstDay {
		start = firstDay
	}
	limit := start + int64(7*r.Every) + 7
	for dn := start; dn <= limit; dn++ {
		if !allowed[weekdayOf(dn)] {
			continue
		}
		if (weekIndexOf(dn)-weekIndexOf(firstDay))%int64(r.Every) != 0 {
			continue
		}
		c := time.Unix(dn*86400, 0).UTC()
		t := time.Date(c.Year(), c.Month(), c.Day(), h, mi, s, ns, loc).UnixMilli()
		if t > after && t > first {
			return t, true
		}
	}
	return 0, false
}

// previewIn: las primeras n fechas de la serie (la primera es first), respetando EndAt y MaxCount.
func previewIn(r model.Recurrence, first int64, n int, loc *time.Location) []int64 {
	if n <= 0 || first <= 0 {
		return nil
	}
	if n > recurPreviewMax {
		n = recurPreviewMax
	}
	r = normalizeRecurrence(r)
	if ValidateRecurrence(r, first) != nil {
		return nil
	}
	r.Count = 0
	out := []int64{first}
	cur := first
	for len(out) < n {
		r.Count++
		next, ok := nextOccurrenceIn(r, first, cur, loc)
		if !ok {
			break
		}
		out = append(out, next)
		cur = next
	}
	return out
}

// Preview devuelve las próximas n fechas (ms) de una serie que empezaría en first, la primera
// incluida, para que la interfaz las muestre antes de guardar. Vacío si la regla no es válida.
func (s *Scheduler) Preview(r model.Recurrence, first int64, n int) []int64 {
	return previewIn(r, first, n, time.Local)
}
