package campaign

import (
	"strings"
	"testing"
	"time"

	"dconnector/internal/core/model"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWarmupLimit(t *testing.T) {
	s := model.PacingSettings{WarmupEnabled: true, WarmupStart: 20, WarmupDays: 10}
	first := mustTime(t, "2026-10-01T10:00:00")
	if got := WarmupLimit(200, s, time.Time{}, time.Time{}); got != 20 {
		t.Errorf("sin envíos = %d", got)
	}
	if got := WarmupLimit(200, s, first, mustTime(t, "2026-10-01T18:00:00")); got != 20 {
		t.Errorf("día 0 = %d", got)
	}
	if got := WarmupLimit(200, s, first, mustTime(t, "2026-10-06T10:00:00")); got != 110 {
		t.Errorf("día 5 = %d", got)
	}
	if got := WarmupLimit(200, s, first, mustTime(t, "2026-10-20T10:00:00")); got != 200 {
		t.Errorf("día 19 = %d", got)
	}
	off := s
	off.WarmupEnabled = false
	if got := WarmupLimit(200, off, time.Time{}, time.Time{}); got != 200 {
		t.Errorf("apagado = %d", got)
	}
	if got := WarmupLimit(0, s, time.Time{}, time.Time{}); got != 0 {
		t.Errorf("sin límite = %d", got)
	}
	// El inicio nunca supera el límite diario.
	if got := WarmupLimit(10, s, time.Time{}, time.Time{}); got != 10 {
		t.Errorf("tope = %d", got)
	}
}

func TestBatchRestMs(t *testing.T) {
	if got := BatchRestMs(10, func() float64 { return 0 }); got != 420_000 {
		t.Errorf("mínimo = %d", got)
	}
	if got := BatchRestMs(10, func() float64 { return 1 }); got != 780_000 {
		t.Errorf("máximo = %d", got)
	}
	if got := BatchRestMs(10, nil); got != 600_000 {
		t.Errorf("sin variación = %d", got)
	}
	if got := BatchRestMs(-5, nil); got != 0 {
		t.Errorf("negativo = %d", got)
	}
}

func TestBrakeReason(t *testing.T) {
	s := model.DefaultPacing
	h := func(f func(*CampaignHealth)) CampaignHealth {
		v := CampaignHealth{}
		f(&v)
		return v
	}
	if got := BrakeReason(h(func(v *CampaignHealth) { v.Sent, v.Failed = 7, 4 }), s); !strings.Contains(got, "fallaron 4 de 11") {
		t.Errorf("por fallos: %q", got)
	}
	if got := BrakeReason(h(func(v *CampaignHealth) { v.Sent, v.OptOuts = 20, 3 }), s); !strings.Contains(got, "3 personas") {
		t.Errorf("por bajas: %q", got)
	}
	if got := BrakeReason(h(func(v *CampaignHealth) { v.Sent, v.Read, v.MinutesSinceFirst = 40, 1, 180 }), s); !strings.Contains(got, "casi nadie") {
		t.Errorf("por lecturas: %q", got)
	}
	// Pocas lecturas pero aún es pronto: no frena.
	if got := BrakeReason(h(func(v *CampaignHealth) { v.Sent, v.Read, v.MinutesSinceFirst = 40, 1, 30 }), s); got != "" {
		t.Errorf("pronto: %q", got)
	}
	if got := BrakeReason(h(func(v *CampaignHealth) { v.Sent, v.Read, v.MinutesSinceFirst = 40, 20, 180 }), s); got != "" {
		t.Errorf("con lecturas: %q", got)
	}
	// Si alguien respondió, no frena por falta de lecturas.
	if got := BrakeReason(h(func(v *CampaignHealth) { v.Sent, v.Read, v.Responded, v.MinutesSinceFirst = 40, 1, 1, 180 }), s); got != "" {
		t.Errorf("con respuesta: %q", got)
	}
	// Menos de 10 intentos no frena por fallos.
	if got := BrakeReason(h(func(v *CampaignHealth) { v.Sent, v.Failed = 2, 7 }), s); got != "" {
		t.Errorf("pocos intentos: %q", got)
	}
	off := s
	off.BrakeEnabled = false
	if got := BrakeReason(h(func(v *CampaignHealth) { v.Sent, v.Failed = 7, 4 }), off); got != "" {
		t.Errorf("apagado: %q", got)
	}
}

func TestInWindow(t *testing.T) {
	// 2026-11-09 es lunes.
	mon := func(hhmm string) time.Time { return mustTime(t, "2026-11-09T"+hhmm+":00") }
	day := model.SendWindow{FromMinute: 9 * 60, ToMinute: 18 * 60, Weekdays: []int{1, 2, 3, 4, 5}}
	if !InWindow(day, mon("09:00")) || !InWindow(day, mon("17:59")) {
		t.Error("debería estar abierta")
	}
	if InWindow(day, mon("18:00")) || InWindow(day, mon("08:59")) {
		t.Error("debería estar cerrada")
	}
	if InWindow(day, mustTime(t, "2026-11-08T10:00:00")) { // domingo
		t.Error("el domingo no se envía")
	}
	if !InWindow(model.SendWindow{}, mon("03:00")) {
		t.Error("ventana vacía = sin límite")
	}
	// Cruza la medianoche: 22:00–06:00 de lunes a viernes.
	night := model.SendWindow{FromMinute: 22 * 60, ToMinute: 6 * 60, Weekdays: []int{1, 2, 3, 4, 5}}
	if !InWindow(night, mon("23:00")) || !InWindow(night, mustTime(t, "2026-11-10T05:59:00")) {
		t.Error("de noche debería estar abierta")
	}
	if InWindow(night, mon("12:00")) {
		t.Error("al mediodía debería estar cerrada")
	}
	// La madrugada del sábado pertenece a la ventana del viernes; la del lunes a la del domingo (cerrada).
	if !InWindow(night, mustTime(t, "2026-11-14T02:00:00")) {
		t.Error("madrugada del sábado: ventana del viernes")
	}
	if InWindow(night, mon("02:00")) {
		t.Error("madrugada del lunes: ventana del domingo, cerrada")
	}
}

func TestNextWindowStart(t *testing.T) {
	w := model.SendWindow{FromMinute: 9 * 60, ToMinute: 18 * 60, Weekdays: []int{1, 2, 3, 4, 5}}
	now := mustTime(t, "2026-11-09T07:00:00") // lunes
	if got := NextWindowStart(w, now); !got.Equal(mustTime(t, "2026-11-09T09:00:00")) {
		t.Errorf("antes de abrir: %v", got)
	}
	if got := NextWindowStart(w, mustTime(t, "2026-11-09T10:00:00")); !got.Equal(mustTime(t, "2026-11-09T10:00:00")) {
		t.Errorf("abierta: %v", got)
	}
	if got := NextWindowStart(w, mustTime(t, "2026-11-13T19:00:00")); !got.Equal(mustTime(t, "2026-11-16T09:00:00")) { // viernes tarde → lunes
		t.Errorf("fin de semana: %v", got)
	}
	never := model.SendWindow{FromMinute: 600, ToMinute: 700, Weekdays: []int{9}}
	if got := NextWindowStart(never, now); !got.IsZero() {
		t.Errorf("nunca abre: %v", got)
	}
}

func TestTypingAndEstimateSeconds(t *testing.T) {
	if TypingMs(10, 300) != 3000 || TypingMs(1000, 300) != MaxTypingMs || TypingMs(10, -5) != 0 {
		t.Error("TypingMs")
	}
	lo, hi := NormalizeDelayRange(50, 10)
	if lo != 10 || hi != 50 {
		t.Errorf("rango = %d..%d", lo, hi)
	}
	if lo, _ := NormalizeDelayRange(0, 0); lo != 1 {
		t.Errorf("mínimo 1 s: %d", lo)
	}
	if got := EstimateSeconds(10, 0, 20, 40, 0); got != 300 {
		t.Errorf("estimación = %d", got)
	}
	if EstimateSeconds(0, 100, 20, 40, 300) != 0 {
		t.Error("sin mensajes")
	}
}
