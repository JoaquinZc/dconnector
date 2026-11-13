package facade

import (
	"context"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Scheduled: mensajes y estados programados, sueltos o en serie (recurrentes).
type Scheduled struct{ s *service.Scheduler }

func (s *Scheduled) List(ctx context.Context, from, to int64) ([]model.Scheduled, error) {
	return s.s.List(ctx, from, to)
}

// Save crea o corrige un programado. Con it.Recurrence crea una serie (el primer envío es it.SendAt).
func (s *Scheduled) Save(ctx context.Context, it model.Scheduled) (model.Scheduled, error) {
	return s.s.Save(ctx, it)
}

// ListForChat: los pendientes de un chat y, con includeDone, los enviados, fallidos u omitidos recientes.
func (s *Scheduled) ListForChat(ctx context.Context, accountID, chatNativeID string, includeDone bool) ([]model.Scheduled, error) {
	return s.s.ListForChat(ctx, model.AccountID(accountID), chatNativeID, includeDone)
}

// Update cambia un programado que ya existe (texto, hora, chat o repetición).
func (s *Scheduled) Update(ctx context.Context, it model.Scheduled) (model.Scheduled, error) {
	return s.s.Update(ctx, it)
}

// UpdateSeries cambia la ocurrencia pendiente de una serie; lo cambiado vale también para las siguientes.
func (s *Scheduled) UpdateSeries(ctx context.Context, seriesID string, it model.Scheduled) (model.Scheduled, error) {
	return s.s.UpdateSeries(ctx, seriesID, it)
}

// CancelSeries cancela las ocurrencias pendientes de toda la serie.
func (s *Scheduled) CancelSeries(ctx context.Context, seriesID string) error {
	return s.s.CancelSeries(ctx, seriesID)
}

// DeleteSeries borra todas las ocurrencias de una serie, historial incluido.
func (s *Scheduled) DeleteSeries(ctx context.Context, seriesID string) error {
	return s.s.DeleteSeries(ctx, seriesID)
}

// PreviewRecurrence devuelve las próximas n fechas (ms; la primera es first) de una repetición, o
// el error que explica por qué la regla no es válida.
func (s *Scheduled) PreviewRecurrence(r model.Recurrence, first int64, n int) ([]int64, error) {
	if err := service.ValidateRecurrence(r, first); err != nil {
		return nil, err
	}
	return s.s.Preview(r, first, n), nil
}

func (s *Scheduled) Cancel(ctx context.Context, id string) error  { return s.s.Cancel(ctx, id) }
func (s *Scheduled) Delete(ctx context.Context, id string) error  { return s.s.Delete(ctx, id) }
func (s *Scheduled) SendNow(ctx context.Context, id string) error { return s.s.SendNow(ctx, id) }
