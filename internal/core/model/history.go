package model

// Estados de un chat en la tabla history_sync (05-ALMACEN, migración 2).
const (
	HistoryPending = "pending" // aún queda historial por pedir (o no se ha empezado)
	HistoryDone    = "done"    // el canal ya no tiene nada más antiguo
	HistoryFailed  = "failed"  // el último intento falló; se reintenta en la siguiente tanda
	HistorySkipped = "skipped" // el canal no permite pedir historial de este chat
)

// HistorySyncRow: avance de la sincronización de historial de un chat (una fila de history_sync).
type HistorySyncRow struct {
	AccountID     AccountID `json:"accountId"`
	ChatNativeID  string    `json:"chatNativeId"`
	State         string    `json:"state"`         // pending | done | failed | skipped
	Requests      int       `json:"requests"`      // peticiones hechas al canal en total
	EmptyStreak   int       `json:"emptyStreak"`   // peticiones seguidas sin nada más antiguo
	Added         int       `json:"added"`         // mensajes nuevos traídos en total
	OldestTs      int64     `json:"oldestTs"`      // ms del mensaje más antiguo conocido (0 = ninguno)
	LastRequestAt int64     `json:"lastRequestAt"` // ms de la última petición
	DoneAt        int64     `json:"doneAt"`        // ms en que se dio por completo (0 = no)
	LastError     string    `json:"lastError"`     // último error, sin datos personales
}

// ChatHistoryStats: lo que el almacén de la cuenta sabe de los mensajes guardados de un chat.
type ChatHistoryStats struct {
	Count    int        `json:"count"`    // mensajes guardados
	Oldest   MessageRef `json:"oldest"`   // el más antiguo (NativeID vacío si no hay ninguno)
	NewestTs int64      `json:"newestTs"` // ms del más reciente (0 si no hay)
}
