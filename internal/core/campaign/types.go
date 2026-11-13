// Package campaign: lógica pura de campañas portada de D'Ventas (sin E/S).
package campaign

// ReviewItem: un hallazgo de la revisión de una campaña.
type ReviewItem struct {
	Level   string `json:"level"` // green | yellow | red
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Estimate: duración estimada de una campaña.
type Estimate struct {
	Seconds int64 `json:"seconds"`
	Days    int   `json:"days"`
}
