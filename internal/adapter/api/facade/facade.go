package facade

import (
	"context"
	"errors"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Deps agrupa lo que las fachadas necesitan del núcleo y del protocolo (equivale al Deps de ui/wails).
type Deps struct {
	Accounts      *service.Accounts
	Inbox         *service.Inbox
	Messaging     *service.Messaging
	Statuses      *service.Statuses
	Contacts      *service.Contacts
	Campaigns     *service.Campaigns
	Scheduler     *service.Scheduler
	Stats         *service.Stats
	Destinations  *service.Destinations
	Settings      *service.Settings
	History       *service.History
	Notifications *service.Notifications // puede ser nil y fijarse después con Facade.SetNotifications

	DestinationStates func() []model.DestinationState
	DestinationTest   func(ctx context.Context, id string) error
	// OnSettingsSaved se llama tras guardar ajustes con éxito (registro detallado, etc.).
	OnSettingsSaved func(service.AppSettings)
}

var errNotWired = errors.New("el servicio no está conectado todavía")

// Facade reúne una fachada por servicio.
type Facade struct {
	Accounts      *Accounts
	Inbox         *Inbox
	Messaging     *Messaging
	Statuses      *Statuses
	Contacts      *Contacts
	Campaigns     *Campaigns
	Scheduled     *Scheduled
	Stats         *Stats
	Destinations  *Destinations
	Settings      *Settings
	History       *History
	Notifications *Notifications
}

// New crea las fachadas.
func New(d Deps) *Facade {
	return &Facade{
		Accounts:     &Accounts{a: d.Accounts},
		Inbox:        &Inbox{i: d.Inbox},
		Messaging:    &Messaging{m: d.Messaging},
		Statuses:     &Statuses{s: d.Statuses},
		Contacts:     &Contacts{c: d.Contacts},
		Campaigns:    &Campaigns{c: d.Campaigns},
		Scheduled:    &Scheduled{s: d.Scheduler},
		Stats:        &Stats{s: d.Stats},
		Destinations: &Destinations{d: d.Destinations, states: d.DestinationStates, test: d.DestinationTest},
		Settings:     &Settings{s: d.Settings, onSaved: d.OnSettingsSaved},
		History:      &History{h: d.History},
		Notifications: &Notifications{
			n:     d.Notifications,
			conns: map[string]connFocus{},
		},
	}
}

// Services devuelve cada fachada por su nombre de servicio en la URL (/rpc/{servicio}/{método}).
func (f *Facade) Services() map[string]any {
	return map[string]any{
		"Accounts": f.Accounts, "Inbox": f.Inbox, "Messaging": f.Messaging, "Statuses": f.Statuses,
		"Contacts": f.Contacts, "Campaigns": f.Campaigns, "Scheduled": f.Scheduled, "Stats": f.Stats,
		"Destinations": f.Destinations, "Settings": f.Settings, "History": f.History,
		"Notifications": f.Notifications,
	}
}

// SetNotifications conecta el servicio de avisos (se crea después de la API, porque necesita su Notifier).
func (f *Facade) SetNotifications(n *service.Notifications) { f.Notifications.setService(n) }

// ConnClosed olvida el foco de una conexión que se cerró (y recalcula el foco efectivo).
func (f *Facade) ConnClosed(connID string) { f.Notifications.dropConn(connID) }

func accIDs(in []string) []model.AccountID {
	out := make([]model.AccountID, len(in))
	for i, s := range in {
		out[i] = model.AccountID(s)
	}
	return out
}

type connKey struct{}

// WithConnID anota en el contexto la conexión (X-DConnector-Conn) de la que viene la llamada.
func WithConnID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, connKey{}, id)
}

// ConnID devuelve la conexión anotada ("" si no hay).
func ConnID(ctx context.Context) string {
	s, _ := ctx.Value(connKey{}).(string)
	return s
}

// FocusConns dice cuántas conexiones tienen anotado un foco (para diagnóstico y pruebas).
func (f *Facade) FocusConns() int { return f.Notifications.count() }
