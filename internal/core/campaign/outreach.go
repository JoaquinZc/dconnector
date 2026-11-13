package campaign

import (
	"fmt"
	"math"

	"dconnector/internal/core/model"
)

// Protección anti-bloqueo: riesgo de cada destinatario y ritmo de envío según ese riesgo
// (portado de outreach.ts, sin waMeLink ni sesión).

// Risk: relación previa con un destinatario.
//
//	warm      te escribió alguna vez (o respondió a una campaña): riesgo bajo
//	lukewarm  solo le has escrito tú, nunca respondió
//	cold      nunca habéis hablado: es quien más reporta y bloquea
type Risk string

const (
	RiskWarm     Risk = "warm"
	RiskLukewarm Risk = "lukewarm"
	RiskCold     Risk = "cold"
)

// RiskOrder: del más cálido al más frío (orden de envío).
var RiskOrder = []Risk{RiskWarm, RiskLukewarm, RiskCold}

func riskIndex(r Risk) int {
	for i, x := range RiskOrder {
		if x == r {
			return i
		}
	}
	return len(RiskOrder) // desconocido = lo más frío
}

// RiskRank: posición de un riesgo en RiskOrder (para ordenar).
func RiskRank(r Risk) int { return riskIndex(r) }

// Warmest devuelve el más «cálido» de dos niveles (una fuente puede saber más que otra).
func Warmest(a, b Risk) Risk {
	if riskIndex(a) <= riskIndex(b) {
		return a
	}
	return b
}

// CountRisks cuenta por nivel; los ids sin riesgo conocido cuentan como fríos.
func CountRisks(ids []string, risks map[string]Risk) map[Risk]int {
	out := map[Risk]int{RiskWarm: 0, RiskLukewarm: 0, RiskCold: 0}
	for _, id := range ids {
		r, ok := risks[id]
		if !ok {
			r = RiskCold
		}
		out[r]++
	}
	return out
}

// SendingSettings: ritmo de envío que usan la estimación y los avisos.
type SendingSettings struct {
	MinDelay, MaxDelay         int
	DailyLimit                 int
	ColdMinDelay, ColdMaxDelay int
	// ColdDailyLimit: 0 = sin límite propio para los contactos sin conversación.
	ColdDailyLimit int
	// TypingSpeed: ms de «escribiendo…» por carácter.
	TypingSpeed int
}

// SendingFromPacing saca los ajustes de envío del ritmo de la cuenta o campaña.
func SendingFromPacing(p model.PacingSettings) SendingSettings {
	return SendingSettings{
		MinDelay: p.MinDelaySec, MaxDelay: p.MaxDelaySec, DailyLimit: p.DailyLimit,
		ColdMinDelay: p.ColdMinDelaySec, ColdMaxDelay: p.ColdMaxDelaySec, ColdDailyLimit: p.ColdDailyLimit,
		TypingSpeed: p.TypingMsPerChar,
	}
}

// EstimateOutreach estima cuánto tardará una campaña según el riesgo de sus destinatarios: los
// conocidos con el ritmo normal, los fríos con más pausa y como mucho ColdDailyLimit al día.
func EstimateOutreach(counts map[Risk]int, avgMessageLength int, s SendingSettings) Estimate {
	normal := counts[RiskWarm] + counts[RiskLukewarm]
	cold := counts[RiskCold]
	seconds := EstimateSeconds(normal, avgMessageLength, s.MinDelay, s.MaxDelay, s.TypingSpeed) +
		EstimateSeconds(cold, avgMessageLength, s.ColdMinDelay, s.ColdMaxDelay, s.TypingSpeed)
	total := normal + cold
	days := 0
	if total > 0 {
		days = 1
	}
	if s.DailyLimit > 0 {
		days = max(days, ceilDiv(total, s.DailyLimit))
	} else {
		days = max(days, 1)
	}
	if s.ColdDailyLimit > 0 && cold > 0 {
		days = max(days, ceilDiv(cold, s.ColdDailyLimit))
	}
	return Estimate{Seconds: seconds, Days: days}
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// OutreachWarnings: avisos sobre una campaña según sus destinatarios y el ritmo configurado.
func OutreachWarnings(counts map[Risk]int, s SendingSettings) []string {
	total := counts[RiskWarm] + counts[RiskLukewarm] + counts[RiskCold]
	out := []string{}
	if total == 0 {
		return out
	}
	cold := counts[RiskCold]
	if float64(cold)/float64(total) >= 0.5 {
		out = append(out, fmt.Sprintf("%d %% de los destinatarios nunca ha hablado contigo. Es el principal motivo de bloqueo.",
			int(math.Round(float64(cold)/float64(total)*100))))
	}
	if cold > 0 && (s.ColdDailyLimit <= 0 || s.ColdDailyLimit > 50) {
		out = append(out, "No hay un límite prudente de contactos nuevos al día (recomendado: 20 a 50).")
	}
	if cold > 0 && s.ColdMinDelay < 45 {
		out = append(out, fmt.Sprintf("La pausa con contactos nuevos empieza en %d s: se recomienda 60 s o más.", s.ColdMinDelay))
	}
	if s.MinDelay < 15 {
		out = append(out, fmt.Sprintf("La pausa entre mensajes empieza en %d s: es muy rápida.", s.MinDelay))
	}
	return out
}
