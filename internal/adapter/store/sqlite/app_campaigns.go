package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"dconnector/internal/core/model"
)

const campaignCols = `id, name, account_id, state, text, variants_json, file_path, list_ids_json, contact_ids_json,
 start_at, window_json, pacing_json, optout_footer, ab_enabled, brake_reason, created_at, updated_at`

func scanCampaign(sc scanner) (model.Campaign, error) {
	var (
		c                         model.Campaign
		state                     string
		variants, lists, contacts string
		window, pacing            string
		optFooter, abEnabled      int
	)
	if err := sc.Scan(&c.ID, &c.Name, &c.AccountID, &state, &c.Text, &variants, &c.FilePath, &lists, &contacts,
		&c.StartAt, &window, &pacing, &optFooter, &abEnabled, &c.BrakeReason, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return model.Campaign{}, err
	}
	c.State = model.CampaignState(state)
	c.Variants = decodeStrings(variants)
	c.ListIDs = decodeStrings(lists)
	c.ContactIDs = decodeStrings(contacts)
	c.OptOutFooter = optFooter != 0
	c.ABEnabled = abEnabled != 0
	if err := decodeJSON(window, &c.Window); err != nil {
		return model.Campaign{}, err
	}
	if c.Window.Weekdays == nil {
		c.Window.Weekdays = []int{}
	}
	if err := decodeJSON(pacing, &c.Pacing); err != nil {
		return model.Campaign{}, err
	}
	return c, nil
}

// fillTotals calcula los totales de cada campaña a partir de su cola: Queued = pendientes +
// retenidas (held); Sent, Failed y Skipped son los de ese estado. Replied y OptOut no se guardan
// aquí: los calcula el servicio de campañas.
func (s *AppStore) fillTotals(ctx context.Context, cs []model.Campaign) error {
	if len(cs) == 0 {
		return nil
	}
	idx := make(map[string]int, len(cs))
	args := make([]any, 0, len(cs))
	for i, c := range cs {
		idx[c.ID] = i
		args = append(args, c.ID)
	}
	for _, part := range chunks(args, 400) {
		rows, err := s.db.QueryContext(ctx,
			`SELECT campaign_id, state, COUNT(*) FROM campaign_queue
			  WHERE campaign_id IN (`+placeholders(len(part))+`) GROUP BY campaign_id, state`, part...)
		if err != nil {
			return fmt.Errorf("almacén: no se pudieron calcular los totales: %w", err)
		}
		for rows.Next() {
			var (
				cid, state string
				n          int
			)
			if err := rows.Scan(&cid, &state, &n); err != nil {
				rows.Close()
				return err
			}
			t := &cs[idx[cid]].Totals
			switch state {
			case "pending", "held":
				t.Queued += n
			case "sent":
				t.Sent += n
			case "failed":
				t.Failed += n
			case "skipped":
				t.Skipped += n
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

// ListCampaigns devuelve las campañas, las más recientes primero, con sus totales.
func (s *AppStore) ListCampaigns(ctx context.Context) ([]model.Campaign, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+campaignCols+` FROM campaigns ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer las campañas: %w", err)
	}
	out := []model.Campaign{}
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("almacén: campaña ilegible: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.fillTotals(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCampaign devuelve una campaña con sus totales (ErrNotFound si no existe).
func (s *AppStore) GetCampaign(ctx context.Context, id string) (model.Campaign, error) {
	c, err := scanCampaign(s.db.QueryRowContext(ctx, `SELECT `+campaignCols+` FROM campaigns WHERE id = ?`, id))
	if isNoRows(err) {
		return model.Campaign{}, notFound("campaña " + id)
	}
	if err != nil {
		return model.Campaign{}, fmt.Errorf("almacén: no se pudo leer la campaña: %w", err)
	}
	cs := []model.Campaign{c}
	if err := s.fillTotals(ctx, cs); err != nil {
		return model.Campaign{}, err
	}
	return cs[0], nil
}

// SaveCampaign crea o actualiza una campaña (los totales no se guardan: salen de la cola).
func (s *AppStore) SaveCampaign(ctx context.Context, c model.Campaign) error {
	if c.ID == "" {
		return invalid("la campaña no tiene id")
	}
	window, err := encodeJSON(c.Window)
	if err != nil {
		return err
	}
	pacing, err := encodeJSON(c.Pacing)
	if err != nil {
		return err
	}
	now := nowMs()
	created := c.CreatedAt
	if created == 0 {
		created = now
	}
	updated := c.UpdatedAt
	if updated == 0 {
		updated = now
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO campaigns(id, name, account_id, state, text, variants_json, file_path, list_ids_json, contact_ids_json,
  start_at, window_json, pacing_json, optout_footer, ab_enabled, brake_reason, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name, account_id = excluded.account_id, state = excluded.state, text = excluded.text,
  variants_json = excluded.variants_json, file_path = excluded.file_path, list_ids_json = excluded.list_ids_json,
  contact_ids_json = excluded.contact_ids_json, start_at = excluded.start_at, window_json = excluded.window_json,
  pacing_json = excluded.pacing_json, optout_footer = excluded.optout_footer, ab_enabled = excluded.ab_enabled,
  brake_reason = excluded.brake_reason, updated_at = excluded.updated_at`,
		c.ID, c.Name, string(c.AccountID), string(c.State), c.Text, encodeStrings(c.Variants), c.FilePath,
		encodeStrings(c.ListIDs), encodeStrings(c.ContactIDs), c.StartAt, window, pacing,
		b2i(c.OptOutFooter), b2i(c.ABEnabled), c.BrakeReason, created, updated)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar la campaña: %w", err)
	}
	return nil
}

// DeleteCampaign borra la campaña y su cola.
func (s *AppStore) DeleteCampaign(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM campaigns WHERE id = ?`, id); err != nil {
		return fmt.Errorf("almacén: no se pudo borrar la campaña: %w", err)
	}
	return nil
}

// ---- Cola ----

const queueCols = `id, campaign_id, contact_id, chat_native_id, risk, variant, ab_sample, text, state,
 send_after, sent_at, message_key, error`

func scanQueueItem(sc scanner) (model.QueueItem, error) {
	var (
		it     model.QueueItem
		ab     int
		msgKey string
	)
	if err := sc.Scan(&it.ID, &it.CampaignID, &it.ContactID, &it.ChatNativeID, &it.Risk, &it.Variant, &ab, &it.Text,
		&it.State, &it.SendAfter, &it.SentAt, &msgKey, &it.Error); err != nil {
		return model.QueueItem{}, err
	}
	it.ABSample = ab != 0
	it.MessageKey = model.MessageKey(msgKey)
	return it, nil
}

// ReplaceQueue reemplaza toda la cola de la campaña (conserva el orden dado). La cuenta de cada
// item es la de la campaña.
func (s *AppStore) ReplaceQueue(ctx context.Context, campaignID string, items []model.QueueItem) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		var acc string
		if err := tx.QueryRowContext(ctx, `SELECT account_id FROM campaigns WHERE id = ?`, campaignID).Scan(&acc); err != nil {
			if isNoRows(err) {
				return notFound("campaña " + campaignID)
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM campaign_queue WHERE campaign_id = ?`, campaignID); err != nil {
			return fmt.Errorf("almacén: no se pudo vaciar la cola: %w", err)
		}
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO campaign_queue(id, campaign_id, account_id, contact_id, chat_native_id, risk, variant, ab_sample, text,
  state, send_after, sent_at, message_key, error, sort)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for i, it := range items {
			id := it.ID
			if id == "" {
				id = model.NewID()
			}
			if _, err := stmt.ExecContext(ctx, id, campaignID, acc, it.ContactID, it.ChatNativeID, it.Risk, it.Variant,
				b2i(it.ABSample), it.Text, it.State, it.SendAfter, it.SentAt, string(it.MessageKey), it.Error, i); err != nil {
				return fmt.Errorf("almacén: no se pudo guardar un item de la cola: %w", err)
			}
		}
		return nil
	})
}

// ListQueue devuelve los items de la campaña en su orden; `states` vacío = todos; limit <= 0 = sin límite.
func (s *AppStore) ListQueue(ctx context.Context, campaignID string, states []string, limit int) ([]model.QueueItem, error) {
	q := `SELECT ` + queueCols + ` FROM campaign_queue WHERE campaign_id = ?`
	args := []any{campaignID}
	if len(states) > 0 {
		q += ` AND state IN (` + placeholders(len(states)) + `)`
		for _, st := range states {
			args = append(args, st)
		}
	}
	q += ` ORDER BY sort`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudo leer la cola: %w", err)
	}
	defer rows.Close()
	out := []model.QueueItem{}
	for rows.Next() {
		it, err := scanQueueItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// UpdateQueueItem actualiza los campos de un item (ErrNotFound si no existe).
func (s *AppStore) UpdateQueueItem(ctx context.Context, it model.QueueItem) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE campaign_queue SET contact_id = ?, chat_native_id = ?, risk = ?, variant = ?, ab_sample = ?, text = ?,
  state = ?, send_after = ?, sent_at = ?, message_key = ?, error = ?
WHERE id = ?`,
		it.ContactID, it.ChatNativeID, it.Risk, it.Variant, b2i(it.ABSample), it.Text,
		it.State, it.SendAfter, it.SentAt, string(it.MessageKey), it.Error, it.ID)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo actualizar el item de la cola: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("item de cola " + it.ID)
	}
	return nil
}

// CountSentSince cuenta los envíos de campaña de la cuenta con sent_at >= since.
func (s *AppStore) CountSentSince(ctx context.Context, acc model.AccountID, since int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM campaign_queue WHERE account_id = ? AND state = 'sent' AND sent_at >= ?`,
		string(acc), since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("almacén: no se pudieron contar los envíos: %w", err)
	}
	return n, nil
}

// QueueItemByMessage devuelve el item de la cola que envió ese mensaje (ErrNotFound si ninguno).
func (s *AppStore) QueueItemByMessage(ctx context.Context, key model.MessageKey) (model.QueueItem, error) {
	if strings.TrimSpace(string(key)) == "" {
		return model.QueueItem{}, notFound("item de cola sin mensaje")
	}
	it, err := scanQueueItem(s.db.QueryRowContext(ctx,
		`SELECT `+queueCols+` FROM campaign_queue WHERE message_key = ? LIMIT 1`, string(key)))
	if isNoRows(err) {
		return model.QueueItem{}, notFound("item de cola del mensaje " + string(key))
	}
	if err != nil {
		return model.QueueItem{}, fmt.Errorf("almacén: no se pudo leer el item de la cola: %w", err)
	}
	return it, nil
}
