package sqlite

import (
	"context"
	"fmt"

	"dconnector/internal/core/model"
)

// ---- Sincronización de historial (tabla history_sync de app.db) ----
//
// Estos métodos no forman parte de port.AppStore: el servicio History los descubre con una
// aserción de tipo (interfaz opcional service.HistoryStore).

const historyCols = `account_id, chat_native_id, state, requests, empty_streak, added, oldest_ts, last_request_at, done_at, last_error`

// ListHistorySync devuelve el avance guardado de todos los chats de una cuenta.
func (s *AppStore) ListHistorySync(ctx context.Context, acc model.AccountID) ([]model.HistorySyncRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+historyCols+` FROM history_sync WHERE account_id = ? ORDER BY chat_native_id`, string(acc))
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudo leer el avance del historial: %w", err)
	}
	defer rows.Close()
	out := []model.HistorySyncRow{}
	for rows.Next() {
		var r model.HistorySyncRow
		if err := rows.Scan(&r.AccountID, &r.ChatNativeID, &r.State, &r.Requests, &r.EmptyStreak, &r.Added,
			&r.OldestTs, &r.LastRequestAt, &r.DoneAt, &r.LastError); err != nil {
			return nil, fmt.Errorf("almacén: fila de historial ilegible: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveHistorySync crea o reemplaza el avance de un chat.
func (s *AppStore) SaveHistorySync(ctx context.Context, r model.HistorySyncRow) error {
	if r.AccountID == "" || r.ChatNativeID == "" {
		return invalid("el avance del historial necesita cuenta y chat")
	}
	switch r.State {
	case "":
		r.State = model.HistoryPending
	case model.HistoryPending, model.HistoryDone, model.HistoryFailed, model.HistorySkipped:
	default:
		return invalid("estado de historial desconocido " + r.State)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO history_sync(`+historyCols+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, chat_native_id) DO UPDATE SET
  state = excluded.state, requests = excluded.requests, empty_streak = excluded.empty_streak,
  added = excluded.added, oldest_ts = excluded.oldest_ts, last_request_at = excluded.last_request_at,
  done_at = excluded.done_at, last_error = excluded.last_error`,
		string(r.AccountID), r.ChatNativeID, r.State, r.Requests, r.EmptyStreak, r.Added,
		r.OldestTs, r.LastRequestAt, r.DoneAt, r.LastError)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el avance del historial: %w", err)
	}
	return nil
}

// ResetHistorySync borra el avance de una cuenta para volver a recorrer todos sus chats.
func (s *AppStore) ResetHistorySync(ctx context.Context, acc model.AccountID) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM history_sync WHERE account_id = ?`, string(acc)); err != nil {
		return fmt.Errorf("almacén: no se pudo reiniciar el avance del historial: %w", err)
	}
	return nil
}
