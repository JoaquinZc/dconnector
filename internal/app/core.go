// Package app cablea el núcleo, los almacenes y los adaptadores de canal. Lo comparten el servidor
// (cmd/dconnector-server) y el escritorio (cmd/dconnector): un único camino de arranque.
package app

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"dconnector/internal/adapter/channel"
	"dconnector/internal/adapter/protocol/ws"
	"dconnector/internal/adapter/store/sqlite"
	"dconnector/internal/core/bus"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/service"
	"dconnector/internal/paths"
)

// Core agrupa los servicios del núcleo ya construidos.
type Core struct {
	App          port.AppStore
	Bus          *bus.Bus
	Router       *service.Router
	Accounts     *service.Accounts
	Inbox        *service.Inbox
	Messaging    *service.Messaging
	Statuses     *service.Statuses
	Contacts     *service.Contacts
	Campaigns    *service.Campaigns
	Scheduler    *service.Scheduler
	Stats        *service.Stats
	Destinations *service.Destinations
	Settings     *service.Settings
	Protocol     *ws.Manager
	History      *service.History
}

// OpenAccountStore abre accounts/<id>/account.db con sus migraciones (02 §4, paso 5).
func OpenAccountStore(id model.AccountID) (port.AccountStore, *sql.DB, error) {
	if err := os.MkdirAll(paths.AccountDir(id), 0o755); err != nil {
		return nil, nil, fmt.Errorf("crear la carpeta de la cuenta: %w", err)
	}
	return sqlite.OpenAccount(id, paths.AccountDBPath(id))
}

// Build construye todos los servicios del núcleo sobre los almacenes dados.
func Build(app port.AppStore, open service.OpenAccountStore) *Core {
	b := bus.New()
	r := service.NewRouter(app, b)
	acc := service.NewAccounts(app, r, b, channel.Registry, open)
	msg := service.NewMessaging(acc, r, b, app)
	st := service.NewStatuses(acc, r, app)
	camp := service.NewCampaigns(app, acc, msg, b)
	r.AddObserver(camp)
	inbox := service.NewInbox(acc)
	contacts := service.NewContacts(app, acc)
	r.SetContactSync(contacts) // cada persona o chat directo nuevo se vuelve contacto solo
	dest := service.NewDestinations(app)
	mgr := ws.NewManager(dest, ws.Services{Accounts: acc, Inbox: inbox, Messaging: msg, Statuses: st, Contacts: contacts}, b, app, slog.Default())
	r.SetOutbox(mgr)
	return &Core{
		App: app, Bus: b, Router: r, Accounts: acc,
		Inbox:        inbox,
		Messaging:    msg,
		Statuses:     st,
		Contacts:     contacts,
		Campaigns:    camp,
		Scheduler:    service.NewScheduler(app, msg, st, acc),
		Stats:        service.NewStats(acc),
		Destinations: dest,
		Settings:     service.NewSettings(app),
		Protocol:     mgr,
		History:      service.NewHistory(app, acc, b),
	}
}
