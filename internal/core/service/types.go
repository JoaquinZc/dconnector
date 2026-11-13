package service

import (
	"context"
	"database/sql"

	"dconnector/internal/core/campaign"
	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// OpenAccountStore abre accounts/<id>/account.db con migraciones (lo da main.go).
type OpenAccountStore func(id model.AccountID) (port.AccountStore, *sql.DB, error)

// OutboxWriter: lo implementa el manager del protocolo; el Router lo llama de forma síncrona.
type OutboxWriter interface {
	Enqueue(ctx context.Context, ev model.Event)
}

// IncomingObserver: lo implementa Campaigns (06 §2).
type IncomingObserver interface {
	OnIncoming(acc model.AccountID, chatNativeID string, ts int64)
	OnOptOut(acc model.AccountID, contactID string)
}

// AccountView: una cuenta con su estado en vivo (06 §3).
type AccountView struct {
	Account      model.Account       `json:"account"`
	Status       model.AccountStatus `json:"status"`
	Capabilities capability.Set      `json:"capabilities"`
	Unread       int                 `json:"unread"`
}

// CampaignReview: resultado de revisar una campaña antes de lanzarla (06 §8.2).
type CampaignReview struct {
	Recipients int                   `json:"recipients"`
	Risks      map[string]int        `json:"risks"`
	Required   int                   `json:"requiredVariants"`
	Items      []campaign.ReviewItem `json:"items"`
	Level      string                `json:"level"` // green | yellow | red (red impide lanzar)
	Estimate   campaign.Estimate     `json:"estimate"`
	Warnings   []string              `json:"warnings"`
	Skipped    map[string]int        `json:"skipped"` // optedOut, noIdentity, frequency
}

// CampaignProgress: aviso de cambio de una campaña (canal propio, no el bus).
type CampaignProgress struct {
	CampaignID  string               `json:"campaignId"`
	State       model.CampaignState  `json:"state"`
	Totals      model.CampaignTotals `json:"totals"`
	NextAt      int64                `json:"nextAt"`  // próximo envío previsto, 0 si no hay
	Waiting     string               `json:"waiting"` // "" | "window" | "hourly" | "daily" | "batch_rest" | "disconnected"
	BrakeReason string               `json:"brakeReason"`
}

// ImportResult: resultado de importar contactos.
type ImportResult struct{ Created, Updated, Skipped int }

// CSVMapping: índice de columna de cada campo (-1 = no).
type CSVMapping struct {
	Name, Phone, Tags, Notes int
	ListID                   string
	HasHeader                bool
}

// AppSettings: ajustes de la aplicación (06 §12).
type AppSettings struct {
	CloseToTray      bool           `json:"closeToTray"`
	StartWithWindows bool           `json:"startWithWindows"`
	StartMinimized   bool           `json:"startMinimized"`
	Notifications    bool           `json:"notifications"`
	Theme            string         `json:"theme"`
	VerboseLog       bool           `json:"verboseLog"`
	AutoDownload     map[string]any `json:"autoDownload"`
	OptOutKeywords   string         `json:"optOutKeywords"`
	OptOutFooterText string         `json:"optOutFooterText"`
	// AutoHistory: pedir solo, despacio y en segundo plano, el historial anterior de los chats al conectar.
	AutoHistory bool `json:"autoHistory"`
	// NotificationsSound: el aviso de un mensaje nuevo suena (con la notificación, o solo si está apagada
	// o bloqueada). Por defecto encendido.
	NotificationsSound bool `json:"notificationsSound"`
	// NotificationsPreview: la notificación muestra el texto del mensaje; apagado, dice «Mensaje nuevo»
	// (privacidad). Por defecto encendido.
	NotificationsPreview bool `json:"notificationsPreview"`
}
