package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// ---- Destinos ----

// ListDestinations devuelve los destinos por orden de creación.
func (s *AppStore) ListDestinations(ctx context.Context) ([]model.Destination, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, url, token, enabled, allow_commands, filter_json, created_at
  FROM destinations ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer los destinos: %w", err)
	}
	defer rows.Close()
	out := []model.Destination{}
	for rows.Next() {
		var (
			d              model.Destination
			enabled, allow int
			filter         string
		)
		if err := rows.Scan(&d.ID, &d.Name, &d.URL, &d.Token, &enabled, &allow, &filter, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Enabled = enabled != 0
		d.AllowCommands = allow != 0
		if err := decodeJSON(filter, &d.Filter); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveDestination crea o actualiza un destino.
func (s *AppStore) SaveDestination(ctx context.Context, d model.Destination) error {
	if d.ID == "" {
		return invalid("el destino no tiene id")
	}
	filter, err := encodeJSON(d.Filter)
	if err != nil {
		return err
	}
	created := d.CreatedAt
	if created == 0 {
		created = nowMs()
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO destinations(id, name, url, token, enabled, allow_commands, filter_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name, url = excluded.url, token = excluded.token, enabled = excluded.enabled,
  allow_commands = excluded.allow_commands, filter_json = excluded.filter_json`,
		d.ID, d.Name, d.URL, d.Token, b2i(d.Enabled), b2i(d.AllowCommands), filter, created)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el destino: %w", err)
	}
	return nil
}

// DeleteDestination borra el destino y todo su outbox.
func (s *AppStore) DeleteDestination(ctx context.Context, id string) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE destination_id = ?`, id); err != nil {
			return fmt.Errorf("almacén: no se pudo vaciar el outbox: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM destinations WHERE id = ?`, id); err != nil {
			return fmt.Errorf("almacén: no se pudo borrar el destino: %w", err)
		}
		return nil
	})
}

// ---- Outbox ----

// OutboxAdd encola un frame para el destino. Es idempotente: si (destino, evento) ya está, no
// hace nada.
func (s *AppStore) OutboxAdd(ctx context.Context, destinationID string, eventID string, payload []byte) error {
	if payload == nil {
		payload = []byte{}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO outbox(destination_id, event_id, payload, created_at, attempts) VALUES(?, ?, ?, ?, 0)`,
		destinationID, eventID, payload, nowMs())
	if err != nil {
		return fmt.Errorf("almacén: no se pudo encolar el evento: %w", err)
	}
	return nil
}

// OutboxPending devuelve hasta `limit` filas pendientes del destino, de la más antigua a la más
// nueva (limit <= 0 = todas). Cada lectura cuenta como un intento: suma 1 a `attempts` y devuelve
// el valor ya incrementado.
func (s *AppStore) OutboxPending(ctx context.Context, destinationID string, limit int) ([]port.OutboxRow, error) {
	out := []port.OutboxRow{}
	err := inTx(ctx, s.db, func(tx *sql.Tx) error {
		q := `SELECT event_id, payload, created_at, attempts FROM outbox WHERE destination_id = ? ORDER BY created_at, rowid`
		args := []any{destinationID}
		if limit > 0 {
			q += ` LIMIT ?`
			args = append(args, limit)
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return fmt.Errorf("almacén: no se pudo leer el outbox: %w", err)
		}
		for rows.Next() {
			var r port.OutboxRow
			if err := rows.Scan(&r.EventID, &r.Payload, &r.CreatedAt, &r.Attempts); err != nil {
				rows.Close()
				return err
			}
			r.Attempts++
			out = append(out, r)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, r := range out {
			if _, err := tx.ExecContext(ctx,
				`UPDATE outbox SET attempts = ? WHERE destination_id = ? AND event_id = ?`,
				r.Attempts, destinationID, r.EventID); err != nil {
				return fmt.Errorf("almacén: no se pudo contar el intento: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// OutboxAck borra del outbox los eventos confirmados por el destino.
func (s *AppStore) OutboxAck(ctx context.Context, destinationID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, part := range chunks(eventIDs, 400) {
			args := make([]any, 0, len(part)+1)
			args = append(args, destinationID)
			for _, id := range part {
				args = append(args, id)
			}
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM outbox WHERE destination_id = ? AND event_id IN (`+placeholders(len(part))+`)`, args...); err != nil {
				return fmt.Errorf("almacén: no se pudo confirmar el outbox: %w", err)
			}
		}
		return nil
	})
}

// OutboxCount cuenta los eventos sin confirmar del destino.
func (s *AppStore) OutboxCount(ctx context.Context, destinationID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE destination_id = ?`, destinationID).Scan(&n); err != nil {
		return 0, fmt.Errorf("almacén: no se pudo contar el outbox: %w", err)
	}
	return n, nil
}

// OutboxTrim poda el outbox: borra lo creado antes de `olderThan` (si > 0) y, si `maxRows` > 0,
// deja como máximo `maxRows` filas por destino (las más nuevas). Devuelve cuántas borró.
func (s *AppStore) OutboxTrim(ctx context.Context, olderThan int64, maxRows int) (int, error) {
	total := 0
	err := inTx(ctx, s.db, func(tx *sql.Tx) error {
		if olderThan > 0 {
			res, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE created_at < ?`, olderThan)
			if err != nil {
				return fmt.Errorf("almacén: no se pudo podar el outbox: %w", err)
			}
			n, _ := res.RowsAffected()
			total += int(n)
		}
		if maxRows > 0 {
			res, err := tx.ExecContext(ctx, `
DELETE FROM outbox WHERE rowid IN (
  SELECT rowid FROM (
    SELECT rowid, ROW_NUMBER() OVER (PARTITION BY destination_id ORDER BY created_at DESC, rowid DESC) AS rn
      FROM outbox)
  WHERE rn > ?)`, maxRows)
			if err != nil {
				return fmt.Errorf("almacén: no se pudo podar el outbox: %w", err)
			}
			n, _ := res.RowsAffected()
			total += int(n)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
