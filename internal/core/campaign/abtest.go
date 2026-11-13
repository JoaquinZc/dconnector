package campaign

import (
	"encoding/json"
	"math"
	"sort"
)

// «Prueba y gana»: primero se envía una muestra con todas las variantes; pasado un tiempo se
// eligen las mejores y el resto de la campaña sale solo con ellas (portado de abtest.ts).

// ABConfig: ajustes de la prueba.
type ABConfig struct {
	// SamplePct: porcentaje de destinatarios de la muestra.
	SamplePct int `json:"samplePct"`
	// WaitHours: horas a esperar tras la muestra antes de elegir.
	WaitHours int `json:"waitHours"`
	// Winners: cuántas variantes ganan (1 o 2).
	Winners int `json:"winners"`
}

// ABDefaults: valores por defecto.
var ABDefaults = ABConfig{SamplePct: 20, WaitHours: 4, Winners: 2}

func clampRound(v float64, lo, hi int) int {
	return int(math.Min(float64(hi), math.Max(float64(lo), math.Round(v))))
}

// NormalizeABConfig fija cada valor dentro de su rango (muestra 5–50 %, espera 1–48 h, 1–2 ganadoras).
func NormalizeABConfig(c ABConfig) ABConfig {
	return ABConfig{
		SamplePct: clampRound(float64(c.SamplePct), 5, 50),
		WaitHours: clampRound(float64(c.WaitHours), 1, 48),
		Winners:   clampRound(float64(c.Winners), 1, 2),
	}
}

// ParseABConfig lee la configuración guardada (JSON); nil si no hay o está dañada. Lo que falte
// toma el valor por defecto.
func ParseABConfig(raw string) *ABConfig {
	if raw == "" {
		return nil
	}
	c := ABDefaults
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil
	}
	n := NormalizeABConfig(c)
	return &n
}

// ABSample elige la muestra: de cada variante, el mismo porcentaje de sus destinatarios (al menos
// 1), para que todas se prueben por igual. Toma los primeros de cada variante en el orden dado.
func ABSample(assigned []Assignment, pct int) map[string]bool {
	byVariant := map[int][]string{}
	var order []int
	for _, a := range assigned {
		if _, ok := byVariant[a.Variant]; !ok {
			order = append(order, a.Variant)
		}
		byVariant[a.Variant] = append(byVariant[a.Variant], a.ID)
	}
	out := map[string]bool{}
	for _, v := range order {
		ids := byVariant[v]
		k := max(1, int(math.Ceil(float64(len(ids)*pct)/100)))
		for _, id := range ids[:min(k, len(ids))] {
			out[id] = true
		}
	}
	return out
}

// VariantStat: resultados de una variante en la muestra.
type VariantStat struct {
	Variant   int
	Sent      int
	Read      int
	Responded int
	OptOuts   int
}

// VariantScore es el puntaje: respuestas sobre enviados, luego lecturas; las bajas restan mucho.
func VariantScore(s VariantStat) float64 {
	if s.Sent == 0 {
		return -1
	}
	sent := float64(s.Sent)
	return float64(s.Responded)/sent*100 + float64(s.Read)/sent*10 - float64(s.OptOuts)/sent*300
}

// PickWinners devuelve las mejores variantes (por puntaje; a igualdad, la de letra menor).
func PickWinners(stats []VariantStat, count int) []int {
	list := make([]VariantStat, 0, len(stats))
	for _, s := range stats {
		if s.Sent > 0 {
			list = append(list, s)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		si, sj := VariantScore(list[i]), VariantScore(list[j])
		if si != sj {
			return si > sj
		}
		return list[i].Variant < list[j].Variant
	})
	out := []int{}
	for i := 0; i < len(list) && i < max(1, count); i++ {
		out = append(out, list[i].Variant)
	}
	return out
}
