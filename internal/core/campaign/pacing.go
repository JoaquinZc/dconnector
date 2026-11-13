package campaign

import (
	"fmt"
	"math"
	"time"

	"dconnector/internal/core/model"
)

// Envío «humano» de campañas: tandas con descanso, límite por hora, calentamiento de un número
// nuevo y freno automático. Son reglas puras para poder probarlas sin el canal (pacing.ts).

// WarmupLimit es el límite del día durante el calentamiento: empieza en WarmupStart y sube en
// línea recta hasta dailyLimit en WarmupDays días desde el primer envío de campaña. firstSendAt
// cero = todavía no se ha enviado nada.
func WarmupLimit(dailyLimit int, s model.PacingSettings, firstSendAt, now time.Time) int {
	if !s.WarmupEnabled || dailyLimit <= 0 {
		return dailyLimit
	}
	if firstSendAt.IsZero() {
		return min(dailyLimit, s.WarmupStart)
	}
	day := int(math.Max(0, math.Floor(float64(now.Sub(firstSendAt).Milliseconds())/86_400_000)))
	if day >= s.WarmupDays {
		return dailyLimit
	}
	value := float64(s.WarmupStart) + float64(dailyLimit-s.WarmupStart)*float64(day)/float64(s.WarmupDays)
	return max(1, min(dailyLimit, int(math.Round(value))))
}

// BatchRestMs es el descanso tras una tanda, en ms, con ±30 % para que no sea regular. rnd nil
// = 0,5 (sin variación).
func BatchRestMs(minutes float64, rnd func() float64) int64 {
	if rnd == nil {
		rnd = func() float64 { return 0.5 }
	}
	base := math.Max(0, minutes) * 60_000
	return int64(math.Round(base * (0.7 + rnd()*0.6)))
}

// CampaignHealth: cómo va una campaña, para decidir si hay que frenarla.
type CampaignHealth struct {
	Sent      int
	Failed    int
	Delivered int
	Read      int
	Responded int
	OptOuts   int
	// MinutesSinceFirst: minutos desde el primer envío de la campaña.
	MinutesSinceFirst int
}

// BrakeReason dice si hay que frenar la campaña: devuelve el motivo (para mostrarlo) o "".
//   - muchos fallos entre los intentos;
//   - casi nadie lo lee tras un buen número de envíos y al menos 2 horas;
//   - varias bajas durante la campaña.
func BrakeReason(h CampaignHealth, s model.PacingSettings) string {
	if !s.BrakeEnabled {
		return ""
	}
	attempts := h.Sent + h.Failed
	if attempts >= 10 && float64(h.Failed)/float64(attempts)*100 >= s.BrakeFailRate {
		return fmt.Sprintf("Freno automático: fallaron %d de %d envíos (%d %%). Revisa los números o la conexión.",
			h.Failed, attempts, int(math.Round(float64(h.Failed)/float64(attempts)*100)))
	}
	if h.OptOuts >= s.BrakeOptOuts {
		return fmt.Sprintf("Freno automático: %d personas pidieron no recibir más mensajes durante esta campaña.", h.OptOuts)
	}
	if h.Sent >= s.BrakeMinSent && h.MinutesSinceFirst >= 120 {
		unread := h.Sent - h.Read
		if float64(unread)/float64(h.Sent)*100 >= s.BrakeUnreadRate && h.Responded == 0 {
			return fmt.Sprintf("Freno automático: de %d enviados casi nadie lo leyó (%d). Puede que WhatsApp no los esté entregando; espera y revisa el mensaje.", h.Sent, h.Read)
		}
	}
	return ""
}

// ---- Ventana horaria y esperas (schedule.ts) ----

// MaxTypingMs: tope del «escribiendo…» simulado antes de cada envío.
const MaxTypingMs = 8000

// NormalizeDelayRange devuelve [min, max] ordenados y con un mínimo de 1 segundo.
func NormalizeDelayRange(minSeconds, maxSeconds int) (int, int) {
	a, b := max(1, minSeconds), max(1, maxSeconds)
	if a <= b {
		return a, b
	}
	return b, a
}

// TypingMs: tiempo de «escribiendo…» para un texto (n caracteres × msPerChar, con tope).
func TypingMs(chars, msPerChar int) int {
	return min(chars*max(0, msPerChar), MaxTypingMs)
}

// EstimateSeconds estima (en segundos) lo que tardará un grupo de mensajes: por mensaje =
// escritura simulada media + pausa media entre mensajes.
func EstimateSeconds(messageCount, avgLen, minDelay, maxDelay, msPerChar int) int64 {
	if messageCount <= 0 {
		return 0
	}
	lo, hi := NormalizeDelayRange(minDelay, maxDelay)
	typing := float64(TypingMs(avgLen, msPerChar)) / 1000
	return int64(math.Round(float64(messageCount) * (typing + float64(lo+hi)/2)))
}

func weekdayAllowed(w model.SendWindow, day int) bool {
	if len(w.Weekdays) == 0 {
		return true
	}
	for _, d := range w.Weekdays {
		if d == day {
			return true
		}
	}
	return false
}

// InWindow dice si t (en su zona horaria) cae dentro de la ventana de envío. Sin horas
// (FromMinute == ToMinute) no hay límite de hora; sin días, valen todos. Soporta ventanas que
// cruzan la medianoche (22:00–06:00): la parte de madrugada cuenta como del día anterior.
func InWindow(w model.SendWindow, t time.Time) bool {
	cur := t.Hour()*60 + t.Minute()
	day := int(t.Weekday())
	switch {
	case w.FromMinute == w.ToMinute:
		return weekdayAllowed(w, day)
	case w.FromMinute < w.ToMinute:
		return cur >= w.FromMinute && cur < w.ToMinute && weekdayAllowed(w, day)
	case cur >= w.FromMinute:
		return weekdayAllowed(w, day)
	case cur < w.ToMinute:
		return weekdayAllowed(w, (day+6)%7)
	}
	return false
}

// NextWindowStart devuelve el primer instante >= t en que la ventana está abierta (t mismo si ya
// lo está). Cero si no se abre en los próximos 8 días.
func NextWindowStart(w model.SendWindow, t time.Time) time.Time {
	if InWindow(w, t) {
		return t
	}
	for d := 0; d <= 8; d++ {
		cand := time.Date(t.Year(), t.Month(), t.Day()+d, 0, w.FromMinute, 0, 0, t.Location())
		if cand.After(t) && InWindow(w, cand) {
			return cand
		}
	}
	return time.Time{}
}
