package model

// Frecuencias de una Recurrence.
const (
	RecurDaily   = "daily"
	RecurWeekly  = "weekly"
	RecurMonthly = "monthly"
	RecurYearly  = "yearly"
	RecurCustom  = "custom"
)

// Recurrence: regla de repetición de un programado. Cada ocurrencia es su propia fila (con el
// mismo SeriesID); la regla viaja en todas para poder calcular la siguiente. Las fechas se
// calculan en la hora local de la máquina, conservando la hora de pared de FirstAt.
type Recurrence struct {
	// Frequency: "daily" | "weekly" | "monthly" | "yearly" | "custom".
	Frequency string `json:"frequency"`
	// Every: cada N unidades (1..365). En "custom" las unidades son Unit.
	Every int `json:"every"`
	// Unit: solo en "custom": "minutes" | "hours" | "days" | "weeks" | "months".
	Unit string `json:"unit,omitempty"`
	// Weekdays: solo en "weekly" (0 = domingo … 6 = sábado). Vacío = el día de la semana de FirstAt.
	Weekdays []int `json:"weekdays,omitempty"`
	// EndAt: fecha límite en ms (no se envía nada después). 0 = nunca.
	EndAt int64 `json:"endAt"`
	// MaxCount: tope de ocurrencias de la serie (1..1000). 0 = sin tope.
	MaxCount int `json:"maxCount"`
	// Count: cuántas ocurrencias de la serie ya se cerraron (enviadas, fallidas u omitidas)
	// antes de esta fila. La primera fila lleva 0. Lo lleva el servicio.
	Count int `json:"count"`
	// FirstAt: primera fecha de la serie (ms). Ancla de la hora de pared y del día del mes
	// original (un mensual del 31 vuelve al 31 tras un febrero). Lo fija el servicio.
	FirstAt int64 `json:"firstAt"`
}

type Scheduled struct {
	ID           string          `json:"id"`
	AccountID    AccountID       `json:"accountId"`
	Kind         string          `json:"kind"` // "message" | "status"
	ChatNativeID string          `json:"chatNativeId"`
	ChatTitle    string          `json:"chatTitle"`
	Message      *Outgoing       `json:"message,omitempty"`
	Status       *OutgoingStatus `json:"status,omitempty"`
	SendAt       int64           `json:"sendAt"`
	State        string          `json:"state"` // "pending" | "sent" | "failed" | "canceled"
	SentAt       int64           `json:"sentAt"`
	Error        string          `json:"error"`
	CreatedAt    int64           `json:"createdAt"`
	// Recurrence: nil = se envía una sola vez.
	Recurrence *Recurrence `json:"recurrence,omitempty"`
	// SeriesID agrupa las ocurrencias de una serie ("" = programado suelto).
	SeriesID string `json:"seriesId"`
}
