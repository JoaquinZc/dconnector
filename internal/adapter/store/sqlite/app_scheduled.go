package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"dconnector/internal/core/model"
)

const scheduledCols = `id, account_id, kind, chat_native_id, chat_title, payload_json, send_at, state, sent_at, error, created_at, recurrence_json, series_id`

// Orden de los programados: hora de envío, luego creación y, si empatan en el milisegundo, el
// orden de inserción (rowid), para que varios mensajes a la misma hora salgan como se crearon.
const scheduledOrder = ` ORDER BY send_at, created_at, rowid`

func scanScheduled(sc scanner) (model.Scheduled, error) {
	var (
		it      model.Scheduled
		payload string
		recur   string
	)
	if err := sc.Scan(&it.ID, &it.AccountID, &it.Kind, &it.ChatNativeID, &it.ChatTitle, &payload, &it.SendAt,
		&it.State, &it.SentAt, &it.Error, &it.CreatedAt, &recur, &it.SeriesID); err != nil {
		return model.Scheduled{}, err
	}
	if recur != "" {
		var r model.Recurrence
		if err := decodeJSON(recur, &r); err != nil {
			return model.Scheduled{}, err
		}
		it.Recurrence = &r
	}
	if it.Kind == "status" {
		var st model.OutgoingStatus
		if err := decodeJSON(payload, &st); err != nil {
			return model.Scheduled{}, err
		}
		it.Status = &st
	} else {
		var m model.Outgoing
		if err := decodeJSON(payload, &m); err != nil {
			return model.Scheduled{}, err
		}
		it.Message = &m
	}
	return it, nil
}

func (s *AppStore) queryScheduled(ctx context.Context, query string, args ...any) ([]model.Scheduled, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer los programados: %w", err)
	}
	defer rows.Close()
	out := []model.Scheduled{}
	for rows.Next() {
		it, err := scanScheduled(rows)
		if err != nil {
			return nil, fmt.Errorf("almacén: programado ilegible: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListScheduled devuelve los programados con send_at entre from y to (ambos incluidos; un valor
// <= 0 deja ese extremo abierto) y en alguno de los estados dados (vacío = todos), por fecha de envío.
func (s *AppStore) ListScheduled(ctx context.Context, from, to int64, states []string) ([]model.Scheduled, error) {
	where := []string{"1 = 1"}
	var args []any
	if from > 0 {
		where = append(where, "send_at >= ?")
		args = append(args, from)
	}
	if to > 0 {
		where = append(where, "send_at <= ?")
		args = append(args, to)
	}
	if len(states) > 0 {
		where = append(where, "state IN ("+placeholders(len(states))+")")
		for _, st := range states {
			args = append(args, st)
		}
	}
	return s.queryScheduled(ctx,
		`SELECT `+scheduledCols+` FROM scheduled WHERE `+strings.Join(where, " AND ")+scheduledOrder, args...)
}

// DueScheduled devuelve los pendientes cuya hora ya llegó (send_at <= now), el más antiguo primero.
func (s *AppStore) DueScheduled(ctx context.Context, now int64) ([]model.Scheduled, error) {
	return s.queryScheduled(ctx,
		`SELECT `+scheduledCols+` FROM scheduled WHERE state = 'pending' AND send_at <= ?`+scheduledOrder, now)
}

// scheduledRow serializa un programado para guardarlo.
func scheduledRow(it model.Scheduled) (payload, recur string, created int64, err error) {
	if it.ID == "" {
		return "", "", 0, invalid("el programado no tiene id")
	}
	if it.Kind == "status" {
		if it.Status == nil {
			return "", "", 0, invalid("el programado de estado no tiene contenido")
		}
		payload, err = encodeJSON(it.Status)
	} else {
		if it.Message == nil {
			return "", "", 0, invalid("el programado de mensaje no tiene contenido")
		}
		payload, err = encodeJSON(it.Message)
	}
	if err != nil {
		return "", "", 0, err
	}
	if it.Recurrence != nil {
		if recur, err = encodeJSON(it.Recurrence); err != nil {
			return "", "", 0, err
		}
	}
	created = it.CreatedAt
	if created == 0 {
		created = nowMs()
	}
	return payload, recur, created, nil
}

func saveScheduledTo(ctx context.Context, q dbtx, it model.Scheduled) error {
	payload, recur, created, err := scheduledRow(it)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `
INSERT INTO scheduled(id, account_id, kind, chat_native_id, chat_title, payload_json, send_at, state, sent_at, error, created_at, recurrence_json, series_id)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  account_id = excluded.account_id, kind = excluded.kind, chat_native_id = excluded.chat_native_id,
  chat_title = excluded.chat_title, payload_json = excluded.payload_json, send_at = excluded.send_at,
  state = excluded.state, sent_at = excluded.sent_at, error = excluded.error,
  recurrence_json = excluded.recurrence_json, series_id = excluded.series_id`,
		it.ID, string(it.AccountID), it.Kind, it.ChatNativeID, it.ChatTitle, payload, it.SendAt,
		it.State, it.SentAt, it.Error, created, recur, it.SeriesID)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el programado: %w", err)
	}
	return nil
}

// SaveScheduled crea o actualiza un programado.
func (s *AppStore) SaveScheduled(ctx context.Context, it model.Scheduled) error {
	return saveScheduledTo(ctx, s.db, it)
}

// DeleteScheduled borra un programado.
func (s *AppStore) DeleteScheduled(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM scheduled WHERE id = ?`, id); err != nil {
		return fmt.Errorf("almacén: no se pudo borrar el programado: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Extras (no están en port.AppStore): el servicio los usa por aserción de tipo.

// GetScheduled devuelve un programado por id (ErrNotFound si no existe).
func (s *AppStore) GetScheduled(ctx context.Context, id string) (model.Scheduled, error) {
	it, err := scanScheduled(s.db.QueryRowContext(ctx, `SELECT `+scheduledCols+` FROM scheduled WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Scheduled{}, notFound("programado " + id)
	}
	if err != nil {
		return model.Scheduled{}, fmt.Errorf("almacén: no se pudo leer el programado: %w", err)
	}
	return it, nil
}

// ListScheduledByChat devuelve los programados de mensaje de un chat en los estados dados
// (vacío = todos) con send_at >= since (since <= 0 = sin límite). Se resuelve con el índice de
// estado más la comparación por chat: la tabla es pequeña y no hace falta otro índice.
func (s *AppStore) ListScheduledByChat(ctx context.Context, accountID model.AccountID, chatNativeID string, states []string, since int64) ([]model.Scheduled, error) {
	where := []string{"account_id = ?", "kind = 'message'", "chat_native_id = ?"}
	args := []any{string(accountID), chatNativeID}
	if since > 0 {
		where = append(where, "send_at >= ?")
		args = append(args, since)
	}
	if len(states) > 0 {
		where = append(where, "state IN ("+placeholders(len(states))+")")
		for _, st := range states {
			args = append(args, st)
		}
	}
	return s.queryScheduled(ctx,
		`SELECT `+scheduledCols+` FROM scheduled WHERE `+strings.Join(where, " AND ")+scheduledOrder, args...)
}

// ListScheduledBySeries devuelve las ocurrencias de una serie en los estados dados (vacío = todas).
func (s *AppStore) ListScheduledBySeries(ctx context.Context, seriesID string, states []string) ([]model.Scheduled, error) {
	if seriesID == "" {
		return []model.Scheduled{}, nil
	}
	where := []string{"series_id = ?"}
	args := []any{seriesID}
	if len(states) > 0 {
		where = append(where, "state IN ("+placeholders(len(states))+")")
		for _, st := range states {
			args = append(args, st)
		}
	}
	return s.queryScheduled(ctx,
		`SELECT `+scheduledCols+` FROM scheduled WHERE `+strings.Join(where, " AND ")+scheduledOrder, args...)
}

// SaveScheduledWithNext guarda cur y, en la misma transacción, inserta next (la siguiente
// ocurrencia de la serie) salvo que la serie ya tenga otra pendiente: así una serie no se
// duplica ni se pierde aunque la app se cierre a mitad. next puede ser nil.
func (s *AppStore) SaveScheduledWithNext(ctx context.Context, cur model.Scheduled, next *model.Scheduled) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := saveScheduledTo(ctx, tx, cur); err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		if next.SeriesID != "" {
			var n int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM scheduled WHERE series_id = ? AND state = 'pending' AND id <> ?`,
				next.SeriesID, next.ID).Scan(&n); err != nil {
				return fmt.Errorf("almacén: no se pudo revisar la serie: %w", err)
			}
			if n > 0 {
				return nil // ya hay una pendiente: no se duplica
			}
		}
		return saveScheduledTo(ctx, tx, *next)
	})
}
