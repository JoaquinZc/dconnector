package facade

import (
	"context"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// History: sincronización de historial por cuenta (pide hacia atrás lo que el canal tenga,
// despacio y reanudable). El avance llega a la interfaz con el evento history:sync (EvtHistorySync).
type History struct{ h *service.History }

// Start lanza una tanda en segundo plano; si ya corre una en esa cuenta no hace nada.
func (s *History) Start(ctx context.Context, accountID string) error {
	if s.h == nil {
		return errNotWired
	}
	return s.h.Start(ctx, model.AccountID(accountID), service.HistoryOptions{})
}

// Stop detiene la tanda de la cuenta; el avance queda guardado.
func (s *History) Stop(accountID string) {
	if s.h != nil {
		s.h.Stop(model.AccountID(accountID))
	}
}

// Status devuelve el estado de la cuenta.
func (s *History) Status(accountID string) service.HistoryStatus {
	if s.h == nil {
		return service.HistoryStatus{AccountID: model.AccountID(accountID), Phase: service.HistoryPhaseIdle}
	}
	return s.h.Status(model.AccountID(accountID))
}

// StatusAll devuelve el estado de todas las cuentas.
func (s *History) StatusAll() []service.HistoryStatus {
	if s.h == nil {
		return []service.HistoryStatus{}
	}
	return s.h.StatusAll()
}

// Reset borra el avance de la cuenta para volver a recorrer todos sus chats.
func (s *History) Reset(ctx context.Context, accountID string) error {
	if s.h == nil {
		return errNotWired
	}
	return s.h.Reset(model.AccountID(accountID))
}

// LoadMore pide más historial de un solo chat (el que está abierto), con prioridad.
func (s *History) LoadMore(ctx context.Context, accountID, chatNativeID string) error {
	if s.h == nil {
		return errNotWired
	}
	return s.h.RequestChat(ctx, model.AccountID(accountID), chatNativeID)
}
