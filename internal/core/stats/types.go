// Package stats: lógica pura de estadísticas portada de D'Ventas (sin E/S).
package stats

// Range: periodo a analizar. Days > 0 (últimos N días) o From/To con fechas locales "AAAA-MM-DD".
type Range struct {
	Days int    `json:"days"`
	From string `json:"from"`
	To   string `json:"to"`
}
