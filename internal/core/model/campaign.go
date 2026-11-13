package model

type CampaignState string

const (
	CampaignDraft    CampaignState = "draft"
	CampaignReady    CampaignState = "ready" // programada, esperando StartAt
	CampaignRunning  CampaignState = "running"
	CampaignPaused   CampaignState = "paused" // a mano
	CampaignBraked   CampaignState = "braked" // freno automático (pacing.brakeReason)
	CampaignDone     CampaignState = "done"
	CampaignCanceled CampaignState = "canceled"
)

type SendWindow struct {
	FromMinute int   `json:"fromMinute"` // minutos desde 00:00 locales; 540 = 09:00
	ToMinute   int   `json:"toMinute"`
	Weekdays   []int `json:"weekdays"` // 0=domingo … 6=sábado
}

// PacingSettings: ritmo anti-bloqueo POR CUENTA. Sale de la base: outreach.ts (SAFE_SENDING),
// pacing.ts (PACING_DEFAULTS) y los ajustes freq_* de schema.ts. Los valores por defecto están en
// DefaultPacing y son los mismos de D'Ventas.
type PacingSettings struct {
	MinDelaySec      int     `json:"minDelaySec"` // espera entre envíos a conocidos/sin respuesta
	MaxDelaySec      int     `json:"maxDelaySec"`
	ColdMinDelaySec  int     `json:"coldMinDelaySec"` // espera con contactos sin conversación
	ColdMaxDelaySec  int     `json:"coldMaxDelaySec"`
	DailyLimit       int     `json:"dailyLimit"`       // 0 = sin límite
	ColdDailyLimit   int     `json:"coldDailyLimit"`   // 0 = sin límite propio
	TypingMsPerChar  int     `json:"typingMsPerChar"`  // «escribiendo…» antes de enviar
	BatchSize        int     `json:"batchSize"`        // 0 = sin tandas
	BatchRestMinutes int     `json:"batchRestMinutes"` // ±30 %
	HourlyLimit      int     `json:"hourlyLimit"`      // 0 = sin límite
	WarmupEnabled    bool    `json:"warmupEnabled"`
	WarmupStart      int     `json:"warmupStart"`
	WarmupDays       int     `json:"warmupDays"`
	BrakeEnabled     bool    `json:"brakeEnabled"`
	BrakeFailRate    float64 `json:"brakeFailRate"` // %
	BrakeMinSent     int     `json:"brakeMinSent"`
	BrakeUnreadRate  float64 `json:"brakeUnreadRate"` // %
	BrakeOptOuts     int     `json:"brakeOptOuts"`
	FreqMax          int     `json:"freqMax"` // campañas máx. por contacto en FreqDays (0 = sin límite)
	FreqDays         int     `json:"freqDays"`
}

var DefaultPacing = PacingSettings{
	MinDelaySec: 20, MaxDelaySec: 45, ColdMinDelaySec: 60, ColdMaxDelaySec: 120,
	DailyLimit: 200, ColdDailyLimit: 20, TypingMsPerChar: 300,
	BatchSize: 25, BatchRestMinutes: 10, HourlyLimit: 60,
	WarmupEnabled: true, WarmupStart: 20, WarmupDays: 14,
	BrakeEnabled: true, BrakeFailRate: 30, BrakeMinSent: 30, BrakeUnreadRate: 95, BrakeOptOuts: 3,
	FreqMax: 3, FreqDays: 7,
}

type Campaign struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	AccountID    AccountID      `json:"accountId"`
	State        CampaignState  `json:"state"`
	Text         string         `json:"text"`     // plantilla con {nombre} y {a|b} (template.ts)
	Variants     []string       `json:"variants"` // B…E (variants.ts)
	FilePath     string         `json:"filePath"` // adjunto opcional (copiado a <data>/campaign-media)
	ListIDs      []string       `json:"listIds"`
	ContactIDs   []string       `json:"contactIds"`
	StartAt      int64          `json:"startAt"` // 0 = al pulsar «Iniciar»
	Window       SendWindow     `json:"window"`
	Pacing       PacingSettings `json:"pacing"`
	OptOutFooter bool           `json:"optOutFooter"`
	ABEnabled    bool           `json:"abEnabled"`
	BrakeReason  string         `json:"brakeReason"`
	CreatedAt    int64          `json:"createdAt"`
	UpdatedAt    int64          `json:"updatedAt"`
	Totals       CampaignTotals `json:"totals"`
}

type CampaignTotals struct {
	Queued  int `json:"queued"`
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Replied int `json:"replied"`
	OptOut  int `json:"optOut"`
}

type QueueItem struct {
	ID           string     `json:"id"`
	CampaignID   string     `json:"campaignId"`
	ContactID    string     `json:"contactId"`
	ChatNativeID string     `json:"chatNativeId"`
	Risk         string     `json:"risk"` // "warm" | "lukewarm" | "cold" (outreach.ts: WARM/LUKEWARM/COLD)
	Variant      int        `json:"variant"`
	ABSample     bool       `json:"abSample"`
	Text         string     `json:"text"`  // ya personalizado
	State        string     `json:"state"` // "pending" | "held" | "sent" | "failed" | "skipped"
	SendAfter    int64      `json:"sendAfter"`
	SentAt       int64      `json:"sentAt"`
	MessageKey   MessageKey `json:"messageKey"`
	Error        string     `json:"error"`
}
