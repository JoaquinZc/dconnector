package service

import (
	"errors"
	"testing"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Ecuador no tiene horario de verano (UTC-5 fijo); Nueva York sí (se prueba el cambio de hora).
var (
	locEC = time.FixedZone("Ecuador", -5*3600)
)

func locNY(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("sin base de zonas horarias: %v", err)
	}
	return l
}

func at(loc *time.Location, y int, m time.Month, d, h, mi int) int64 {
	return time.Date(y, m, d, h, mi, 0, 0, loc).UnixMilli()
}

func fmtAt(ms int64, loc *time.Location) string {
	return time.UnixMilli(ms).In(loc).Format("2006-01-02 15:04 Mon")
}

// seq devuelve las próximas n ocurrencias después de first (sin contar first).
func seq(t *testing.T, r model.Recurrence, first int64, n int, loc *time.Location) []int64 {
	t.Helper()
	r.FirstAt = first
	var out []int64
	cur := first
	for i := 0; i < n; i++ {
		r.Count = i + 1
		next, ok := nextOccurrenceIn(r, first, cur, loc)
		if !ok {
			return out
		}
		out = append(out, next)
		cur = next
	}
	return out
}

func expectSeq(t *testing.T, name string, got []int64, loc *time.Location, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d fechas, se esperaban %d: %v", name, len(got), len(want), fmtAll(got, loc))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: fecha %d = %s, se esperaba %s", name, i, fmtAt(got[i], loc), fmtAt(want[i], loc))
		}
	}
}

func fmtAll(ms []int64, loc *time.Location) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = fmtAt(m, loc)
	}
	return out
}

func TestRecurrenceDaily(t *testing.T) {
	first := at(locEC, 2026, 11, 12, 9, 30)
	expectSeq(t, "diaria", seq(t, model.Recurrence{Frequency: "daily", Every: 1}, first, 3, locEC), locEC,
		at(locEC, 2026, 11, 13, 9, 30), at(locEC, 2026, 11, 14, 9, 30), at(locEC, 2026, 11, 15, 9, 30))
	expectSeq(t, "cada 3 días", seq(t, model.Recurrence{Frequency: "daily", Every: 3}, first, 3, locEC), locEC,
		at(locEC, 2026, 11, 15, 9, 30), at(locEC, 2026, 11, 18, 9, 30), at(locEC, 2026, 11, 21, 9, 30))
	// Cruza fin de mes y de año.
	expectSeq(t, "fin de año", seq(t, model.Recurrence{Frequency: "daily", Every: 1}, at(locEC, 2026, 12, 30, 8, 0), 3, locEC), locEC,
		at(locEC, 2026, 12, 31, 8, 0), at(locEC, 2027, 1, 1, 8, 0), at(locEC, 2027, 1, 2, 8, 0))
}

func TestRecurrenceAfterFarInTheFutureJumpsDirectly(t *testing.T) {
	first := at(locEC, 2026, 1, 1, 7, 0)
	after := at(locEC, 2040, 6, 15, 12, 0)
	got, ok := nextOccurrenceIn(model.Recurrence{Frequency: "daily", Every: 1}, first, after, locEC)
	if !ok || got != at(locEC, 2040, 6, 16, 7, 0) {
		t.Fatalf("diaria lejana: %s %v", fmtAt(got, locEC), ok)
	}
	got, ok = nextOccurrenceIn(model.Recurrence{Frequency: "monthly", Every: 1}, at(locEC, 2026, 1, 31, 7, 0), after, locEC)
	if !ok || got != at(locEC, 2040, 6, 30, 7, 0) {
		t.Fatalf("mensual lejana: %s %v", fmtAt(got, locEC), ok)
	}
	got, ok = nextOccurrenceIn(model.Recurrence{Frequency: "yearly", Every: 2}, at(locEC, 2026, 3, 1, 7, 0), after, locEC)
	if !ok || got != at(locEC, 2042, 3, 1, 7, 0) {
		t.Fatalf("anual lejana: %s %v", fmtAt(got, locEC), ok)
	}
	// Si `after` es anterior al primero, la siguiente es la primera repetición.
	got, ok = nextOccurrenceIn(model.Recurrence{Frequency: "daily", Every: 1}, first, first-1000, locEC)
	if !ok || got != first+int64(24*time.Hour/time.Millisecond) {
		t.Fatalf("after < first: %s", fmtAt(got, locEC))
	}
}

func TestRecurrenceWeekly(t *testing.T) {
	// 2026-11-12 es jueves.
	first := at(locEC, 2026, 11, 12, 18, 0)
	expectSeq(t, "semanal sin días", seq(t, model.Recurrence{Frequency: "weekly", Every: 1}, first, 3, locEC), locEC,
		at(locEC, 2026, 11, 19, 18, 0), at(locEC, 2026, 11, 26, 18, 0), at(locEC, 2026, 12, 3, 18, 0))
	// Lunes, miércoles y viernes: desde un jueves sigue viernes 13, lunes 16, miércoles 18, viernes 20.
	expectSeq(t, "L-X-V", seq(t, model.Recurrence{Frequency: "weekly", Every: 1, Weekdays: []int{5, 1, 3, 1}}, first, 4, locEC), locEC,
		at(locEC, 2026, 11, 13, 18, 0), at(locEC, 2026, 11, 16, 18, 0), at(locEC, 2026, 11, 18, 18, 0), at(locEC, 2026, 11, 20, 18, 0))
	// Cada 2 semanas, lunes y viernes: la semana de first (dom 8 - sáb 14) cuenta; luego la del 22.
	expectSeq(t, "cada 2 semanas", seq(t, model.Recurrence{Frequency: "weekly", Every: 2, Weekdays: []int{1, 5}}, first, 4, locEC), locEC,
		at(locEC, 2026, 11, 13, 18, 0), at(locEC, 2026, 11, 23, 18, 0), at(locEC, 2026, 11, 27, 18, 0), at(locEC, 2026, 12, 7, 18, 0))
	// Domingo (0) y sábado (6).
	expectSeq(t, "fin de semana", seq(t, model.Recurrence{Frequency: "weekly", Every: 1, Weekdays: []int{0, 6}}, first, 3, locEC), locEC,
		at(locEC, 2026, 11, 14, 18, 0), at(locEC, 2026, 11, 15, 18, 0), at(locEC, 2026, 11, 21, 18, 0))
	// El mismo día con la hora aún por llegar cuenta: first a las 08:00 lunes, después a las 07:00 del mismo día.
	mon := at(locEC, 2026, 11, 16, 8, 0)
	got, ok := nextOccurrenceIn(model.Recurrence{Frequency: "weekly", Every: 1, Weekdays: []int{1, 2}}, mon, mon, locEC)
	if !ok || got != at(locEC, 2026, 11, 17, 8, 0) {
		t.Fatalf("lunes→martes: %s", fmtAt(got, locEC))
	}
}

func TestRecurrenceMonthlyDay31(t *testing.T) {
	first := at(locEC, 2026, 1, 31, 10, 0)
	expectSeq(t, "mensual 31", seq(t, model.Recurrence{Frequency: "monthly", Every: 1}, first, 6, locEC), locEC,
		at(locEC, 2026, 2, 28, 10, 0), at(locEC, 2026, 3, 31, 10, 0), at(locEC, 2026, 4, 30, 10, 0),
		at(locEC, 2026, 5, 31, 10, 0), at(locEC, 2026, 6, 30, 10, 0), at(locEC, 2026, 7, 31, 10, 0))
	// Año bisiesto: febrero tiene 29.
	expectSeq(t, "bisiesto", seq(t, model.Recurrence{Frequency: "monthly", Every: 1}, at(locEC, 2028, 1, 31, 10, 0), 2, locEC), locEC,
		at(locEC, 2028, 2, 29, 10, 0), at(locEC, 2028, 3, 31, 10, 0))
	// Cada 2 meses desde el 31 de enero: marzo, mayo, julio, septiembre, noviembre (31 en los que lo tienen).
	expectSeq(t, "cada 2 meses", seq(t, model.Recurrence{Frequency: "monthly", Every: 2}, first, 5, locEC), locEC,
		at(locEC, 2026, 3, 31, 10, 0), at(locEC, 2026, 5, 31, 10, 0), at(locEC, 2026, 7, 31, 10, 0),
		at(locEC, 2026, 9, 30, 10, 0), at(locEC, 2026, 11, 30, 10, 0))
	// El día 29 y 30 también se ajustan.
	expectSeq(t, "día 30", seq(t, model.Recurrence{Frequency: "monthly", Every: 1}, at(locEC, 2026, 1, 30, 10, 0), 2, locEC), locEC,
		at(locEC, 2026, 2, 28, 10, 0), at(locEC, 2026, 3, 30, 10, 0))
	// Cruza el año.
	expectSeq(t, "cruza el año", seq(t, model.Recurrence{Frequency: "monthly", Every: 1}, at(locEC, 2026, 11, 15, 10, 0), 3, locEC), locEC,
		at(locEC, 2026, 12, 15, 10, 0), at(locEC, 2027, 1, 15, 10, 0), at(locEC, 2027, 2, 15, 10, 0))
}

func TestRecurrenceYearly(t *testing.T) {
	expectSeq(t, "anual", seq(t, model.Recurrence{Frequency: "yearly", Every: 1}, at(locEC, 2026, 11, 12, 9, 0), 2, locEC), locEC,
		at(locEC, 2027, 11, 12, 9, 0), at(locEC, 2028, 11, 12, 9, 0))
	// 29 de febrero: 28 en años no bisiestos y vuelve al 29 en bisiestos.
	expectSeq(t, "29 de febrero", seq(t, model.Recurrence{Frequency: "yearly", Every: 1}, at(locEC, 2028, 2, 29, 9, 0), 4, locEC), locEC,
		at(locEC, 2029, 2, 28, 9, 0), at(locEC, 2030, 2, 28, 9, 0), at(locEC, 2031, 2, 28, 9, 0), at(locEC, 2032, 2, 29, 9, 0))
}

func TestRecurrenceCustom(t *testing.T) {
	first := at(locEC, 2026, 11, 12, 9, 0)
	expectSeq(t, "cada 5 minutos", seq(t, model.Recurrence{Frequency: "custom", Every: 5, Unit: "minutes"}, first, 3, locEC), locEC,
		at(locEC, 2026, 11, 12, 9, 5), at(locEC, 2026, 11, 12, 9, 10), at(locEC, 2026, 11, 12, 9, 15))
	expectSeq(t, "cada 6 horas", seq(t, model.Recurrence{Frequency: "custom", Every: 6, Unit: "hours"}, first, 3, locEC), locEC,
		at(locEC, 2026, 11, 12, 15, 0), at(locEC, 2026, 11, 12, 21, 0), at(locEC, 2026, 11, 13, 3, 0))
	expectSeq(t, "cada 10 días", seq(t, model.Recurrence{Frequency: "custom", Every: 10, Unit: "days"}, first, 2, locEC), locEC,
		at(locEC, 2026, 11, 22, 9, 0), at(locEC, 2026, 12, 2, 9, 0))
	expectSeq(t, "cada 2 semanas", seq(t, model.Recurrence{Frequency: "custom", Every: 2, Unit: "weeks"}, first, 2, locEC), locEC,
		at(locEC, 2026, 11, 26, 9, 0), at(locEC, 2026, 12, 10, 9, 0))
	expectSeq(t, "cada 3 meses", seq(t, model.Recurrence{Frequency: "custom", Every: 3, Unit: "months"}, first, 2, locEC), locEC,
		at(locEC, 2027, 2, 12, 9, 0), at(locEC, 2027, 5, 12, 9, 0))
	// Con `after` muy adelantado en minutos salta directo a la rejilla.
	got, ok := nextOccurrenceIn(model.Recurrence{Frequency: "custom", Every: 15, Unit: "minutes"}, first, first+7*3600*1000+60*1000, locEC)
	if !ok || got != at(locEC, 2026, 11, 12, 16, 15) {
		t.Fatalf("minutos lejanos: %s", fmtAt(got, locEC))
	}
}

func TestRecurrenceKeepsWallClockAcrossDST(t *testing.T) {
	ny := locNY(t)
	// El cambio de hora de EE. UU. fue el 8 de marzo de 2026 (02:00 → 03:00).
	first := at(ny, 2026, 3, 7, 9, 0)
	got := seq(t, model.Recurrence{Frequency: "daily", Every: 1}, first, 3, ny)
	expectSeq(t, "diaria con DST", got, ny, at(ny, 2026, 3, 8, 9, 0), at(ny, 2026, 3, 9, 9, 0), at(ny, 2026, 3, 10, 9, 0))
	if d := got[0] - first; d != int64(23*time.Hour/time.Millisecond) {
		t.Fatalf("el día del cambio dura 23 h, no %v", time.Duration(d)*time.Millisecond)
	}
	expectSeq(t, "semanal con DST", seq(t, model.Recurrence{Frequency: "weekly", Every: 1}, at(ny, 2026, 3, 5, 18, 30), 2, ny), ny,
		at(ny, 2026, 3, 12, 18, 30), at(ny, 2026, 3, 19, 18, 30))
	// Las horas absolutas no siguen la hora de pared: cada 24 h reales.
	h := seq(t, model.Recurrence{Frequency: "custom", Every: 24, Unit: "hours"}, first, 1, ny)
	if h[0]-first != int64(24*time.Hour/time.Millisecond) {
		t.Fatalf("24 h reales: %v", h[0]-first)
	}
}

func TestRecurrenceEndAtAndMaxCount(t *testing.T) {
	first := at(locEC, 2026, 11, 12, 9, 0)
	// EndAt incluyente: la del 14 entra, la del 15 no.
	r := model.Recurrence{Frequency: "daily", Every: 1, EndAt: at(locEC, 2026, 11, 14, 9, 0)}
	expectSeq(t, "EndAt", seq(t, r, first, 10, locEC), locEC, at(locEC, 2026, 11, 13, 9, 0), at(locEC, 2026, 11, 14, 9, 0))
	// MaxCount 3: la primera más dos repeticiones.
	r = model.Recurrence{Frequency: "daily", Every: 1, MaxCount: 3}
	expectSeq(t, "MaxCount", seq(t, r, first, 10, locEC), locEC, at(locEC, 2026, 11, 13, 9, 0), at(locEC, 2026, 11, 14, 9, 0))
	// MaxCount 1: solo la primera.
	r.MaxCount = 1
	if got := seq(t, r, first, 5, locEC); len(got) != 0 {
		t.Fatalf("MaxCount 1: %v", fmtAll(got, locEC))
	}
	// Count ya en el tope: no hay más.
	r = model.Recurrence{Frequency: "daily", Every: 1, MaxCount: 4, Count: 4}
	if _, ok := nextOccurrenceIn(r, first, first, locEC); ok {
		t.Fatal("con Count == MaxCount no debía haber siguiente")
	}
	// Lo que pasaría de EndAt se corta aunque falten repeticiones.
	r = model.Recurrence{Frequency: "monthly", Every: 1, EndAt: at(locEC, 2026, 12, 11, 0, 0)}
	if got := seq(t, r, first, 5, locEC); len(got) != 0 {
		t.Fatalf("monthly EndAt: %v", fmtAll(got, locEC))
	}
}

func TestValidateRecurrence(t *testing.T) {
	first := at(locEC, 2026, 11, 12, 9, 0)
	ok := []model.Recurrence{
		{Frequency: "daily", Every: 1},
		{Frequency: "daily"}, // Every 0 = 1
		{Frequency: "weekly", Every: 2, Weekdays: []int{0, 6}},
		{Frequency: "monthly", Every: 365},
		{Frequency: "yearly", Every: 1, MaxCount: 1000},
		{Frequency: "custom", Every: 5, Unit: "minutes"},
		{Frequency: "custom", Every: 1, Unit: "hours"},
		{Frequency: "DAILY ", Every: 1},
		{Frequency: "daily", Every: 1, EndAt: first + 1},
		{Frequency: "daily", Every: 1, Unit: "ignorada", Weekdays: []int{9}}, // no aplican fuera de custom/weekly
	}
	for i, r := range ok {
		if err := ValidateRecurrence(r, first); err != nil {
			t.Errorf("válida %d: %v", i, err)
		}
	}
	bad := []model.Recurrence{
		{},
		{Frequency: "cada rato", Every: 1},
		{Frequency: "daily", Every: -1},
		{Frequency: "daily", Every: 366},
		{Frequency: "custom", Every: 4, Unit: "minutes"},
		{Frequency: "custom", Every: 1, Unit: "minutes"},
		{Frequency: "custom", Every: 1},
		{Frequency: "custom", Every: 1, Unit: "segundos"},
		{Frequency: "weekly", Every: 1, Weekdays: []int{7}},
		{Frequency: "weekly", Every: 1, Weekdays: []int{-1}},
		{Frequency: "daily", Every: 1, MaxCount: 1001},
		{Frequency: "daily", Every: 1, MaxCount: -1},
		{Frequency: "daily", Every: 1, Count: -1},
		{Frequency: "daily", Every: 1, EndAt: first},
		{Frequency: "daily", Every: 1, EndAt: first - 1},
	}
	for i, r := range bad {
		err := ValidateRecurrence(r, first)
		if err == nil || !errors.Is(err, port.ErrInvalid) {
			t.Errorf("inválida %d (%+v): %v", i, r, err)
		}
	}
	// Una regla inválida no produce fechas.
	if _, ok := nextOccurrenceIn(model.Recurrence{Frequency: "custom", Every: 1, Unit: "minutes"}, first, first, locEC); ok {
		t.Error("cada 1 minuto no debe producir fechas")
	}
}

func TestPreview(t *testing.T) {
	first := at(locEC, 2026, 11, 12, 9, 0)
	got := previewIn(model.Recurrence{Frequency: "daily", Every: 1}, first, 4, locEC)
	expectSeq(t, "preview", got, locEC, first, at(locEC, 2026, 11, 13, 9, 0), at(locEC, 2026, 11, 14, 9, 0), at(locEC, 2026, 11, 15, 9, 0))
	// Respeta el tope y la fecha final.
	got = previewIn(model.Recurrence{Frequency: "daily", Every: 1, MaxCount: 2}, first, 10, locEC)
	expectSeq(t, "preview MaxCount", got, locEC, first, at(locEC, 2026, 11, 13, 9, 0))
	got = previewIn(model.Recurrence{Frequency: "weekly", Every: 1, EndAt: at(locEC, 2026, 11, 20, 0, 0)}, first, 10, locEC)
	expectSeq(t, "preview EndAt", got, locEC, first, at(locEC, 2026, 11, 19, 9, 0))
	// Un Count previo en la regla no altera la vista previa.
	got = previewIn(model.Recurrence{Frequency: "daily", Every: 1, MaxCount: 2, Count: 7}, first, 10, locEC)
	if len(got) != 2 {
		t.Fatalf("preview con Count: %v", fmtAll(got, locEC))
	}
	if got := previewIn(model.Recurrence{Frequency: "nada"}, first, 5, locEC); got != nil {
		t.Errorf("regla inválida: %v", got)
	}
	if got := previewIn(model.Recurrence{Frequency: "daily", Every: 1}, first, 0, locEC); got != nil {
		t.Errorf("n = 0: %v", got)
	}
	if got := previewIn(model.Recurrence{Frequency: "daily", Every: 1}, first, 100000, locEC); len(got) != recurPreviewMax {
		t.Errorf("tope de la vista previa: %d", len(got))
	}
}

func TestNextOccurrenceUsesLocalZone(t *testing.T) {
	// NextOccurrence público usa time.Local: con la misma regla coincide con el cálculo en esa zona.
	first := time.Date(2026, 11, 12, 9, 0, 0, 0, time.Local).UnixMilli()
	got, ok := NextOccurrence(model.Recurrence{Frequency: "daily", Every: 1}, first, first)
	if !ok || got != time.Date(2026, 11, 13, 9, 0, 0, 0, time.Local).UnixMilli() {
		t.Fatalf("NextOccurrence: %s", fmtAt(got, time.Local))
	}
}
