package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// AppStore implementa port.AppStore sobre app.db. El resto de métodos (contactos, campañas,
// programados, destinos) están en app_*.go.
type AppStore struct {
	db *sql.DB
}

var _ port.AppStore = (*AppStore)(nil)

func newAppStore(db *sql.DB) *AppStore { return &AppStore{db: db} }

// Close cierra la base.
func (s *AppStore) Close() error { return s.db.Close() }

// ---- Cuentas ----

const accountCols = `id, channel, label, color, enabled, self_id, self_name, self_handle, config_json, created_at`

func scanAccount(sc scanner) (model.Account, error) {
	var (
		a       model.Account
		channel string
		enabled int
		cfg     string
	)
	if err := sc.Scan(&a.ID, &channel, &a.Label, &a.Color, &enabled, &a.SelfID, &a.SelfName, &a.SelfHandle, &cfg, &a.CreatedAt); err != nil {
		return model.Account{}, err
	}
	a.Channel = model.Channel(channel)
	a.Enabled = enabled != 0
	a.Config = map[string]string{}
	if err := decodeJSON(cfg, &a.Config); err != nil {
		return model.Account{}, err
	}
	if a.Config == nil {
		a.Config = map[string]string{}
	}
	return a, nil
}

// ListAccounts devuelve las cuentas ordenadas por `sort, created_at`.
func (s *AppStore) ListAccounts(ctx context.Context) ([]model.Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountCols+` FROM accounts ORDER BY sort, created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer las cuentas: %w", err)
	}
	defer rows.Close()
	out := []model.Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("almacén: cuenta ilegible: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAccount devuelve una cuenta (ErrNotFound si no existe).
func (s *AppStore) GetAccount(ctx context.Context, id model.AccountID) (model.Account, error) {
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountCols+` FROM accounts WHERE id = ?`, string(id)))
	if isNoRows(err) {
		return model.Account{}, notFound("cuenta " + string(id))
	}
	if err != nil {
		return model.Account{}, fmt.Errorf("almacén: no se pudo leer la cuenta: %w", err)
	}
	return a, nil
}

// SaveAccount crea o actualiza una cuenta. Una cuenta nueva queda al final (max(sort)+1); si ya
// existe, `sort` y `created_at` no cambian.
func (s *AppStore) SaveAccount(ctx context.Context, a model.Account) error {
	if a.ID == "" {
		return invalid("la cuenta no tiene id")
	}
	cfg, err := encodeJSON(a.Config)
	if err != nil {
		return err
	}
	if a.Config == nil {
		cfg = "{}"
	}
	created := a.CreatedAt
	if created == 0 {
		created = nowMs()
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO accounts(id, channel, label, color, enabled, self_id, self_name, self_handle, config_json, sort, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort), 0) + 1 FROM accounts), ?)
ON CONFLICT(id) DO UPDATE SET
  channel = excluded.channel, label = excluded.label, color = excluded.color, enabled = excluded.enabled,
  self_id = excluded.self_id, self_name = excluded.self_name, self_handle = excluded.self_handle,
  config_json = excluded.config_json`,
		string(a.ID), string(a.Channel), a.Label, a.Color, b2i(a.Enabled), a.SelfID, a.SelfName, a.SelfHandle, cfg, created)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar la cuenta: %w", err)
	}
	return nil
}

// DeleteAccount borra la cuenta con sus identidades, campañas, items de cola, programados y los
// ajustes `pacing:<id>` y `warmup_started_at:<id>`.
func (s *AppStore) DeleteAccount(ctx context.Context, id model.AccountID) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM campaign_queue WHERE account_id = ?`, string(id)); err != nil {
			return fmt.Errorf("almacén: no se pudo borrar la cola de la cuenta: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, string(id)); err != nil {
			return fmt.Errorf("almacén: no se pudo borrar la cuenta: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key IN (?, ?)`,
			"pacing:"+string(id), "warmup_started_at:"+string(id)); err != nil {
			return fmt.Errorf("almacén: no se pudieron borrar los ajustes de la cuenta: %w", err)
		}
		return nil
	})
}

// ReorderAccounts fija el orden de las cuentas (no está en el puerto; lo usa Accounts.Reorder).
// Las cuentas que no aparezcan en `ids` quedan después, en su orden actual.
func (s *AppStore) ReorderAccounts(ctx context.Context, ids []model.AccountID) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM accounts ORDER BY sort, created_at, id`)
		if err != nil {
			return fmt.Errorf("almacén: no se pudo leer el orden de cuentas: %w", err)
		}
		var current []model.AccountID
		for rows.Next() {
			var id model.AccountID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			current = append(current, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		exists := map[model.AccountID]bool{}
		for _, id := range current {
			exists[id] = true
		}
		seen := map[model.AccountID]bool{}
		final := make([]model.AccountID, 0, len(current))
		for _, id := range ids {
			if exists[id] && !seen[id] {
				seen[id] = true
				final = append(final, id)
			}
		}
		for _, id := range current {
			if !seen[id] {
				final = append(final, id)
			}
		}
		for i, id := range final {
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET sort = ? WHERE id = ?`, i+1, string(id)); err != nil {
				return fmt.Errorf("almacén: no se pudo reordenar: %w", err)
			}
		}
		return nil
	})
}

// ---- Ajustes ----

// GetSetting devuelve el valor (JSON) de un ajuste, "" si no existe.
func (s *AppStore) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if isNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("almacén: no se pudo leer el ajuste: %w", err)
	}
	return v, nil
}

// SetSetting guarda un ajuste.
func (s *AppStore) SetSetting(ctx context.Context, key, value string) error {
	if key == "" {
		return invalid("el ajuste no tiene clave")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el ajuste: %w", err)
	}
	return nil
}
