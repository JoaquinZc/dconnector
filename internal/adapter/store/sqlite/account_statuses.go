package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"dconnector/internal/core/model"
)

// statusMediaChat: valor de media.chat_native_id para los medios de estados.
const statusMediaChat = "status@broadcast"

const statusSelect = `SELECT s.native_id, s.author_native_id, s.author_name, s.from_me, s.posted_at, s.expires_at, s.kind,
 s.text, s.bg_color, s.font, s.media_id, s.viewed, s.deleted_at, s.delivery,
 md.id, md.mime, md.file_name, md.size, md.width, md.height, md.duration, md.thumb, md.handle, md.local_path,
 (SELECT COUNT(*) FROM status_views v WHERE v.status_native_id = s.native_id),
 (SELECT COUNT(*) FROM status_views v WHERE v.status_native_id = s.native_id AND v.reaction <> '')
 FROM statuses s LEFT JOIN media md ON md.id = s.media_id AND s.media_id <> ''`

func (s *AccountStore) scanStatus(sc scanner) (model.Status, error) {
	var (
		st                             model.Status
		kind, delivery, mediaID        string
		fromMe, viewed                 int
		mid, mime, fname, thumb, lpath sql.NullString
		msize, mw, mh                  sql.NullInt64
		mdur                           sql.NullFloat64
		handle                         []byte
	)
	if err := sc.Scan(&st.NativeID, &st.AuthorNativeID, &st.AuthorName, &fromMe, &st.PostedAt, &st.ExpiresAt, &kind,
		&st.Text, &st.BgColor, &st.Font, &mediaID, &viewed, &st.DeletedAt, &delivery,
		&mid, &mime, &fname, &msize, &mw, &mh, &mdur, &thumb, &handle, &lpath,
		&st.ViewCount, &st.ReactionCount); err != nil {
		return model.Status{}, err
	}
	st.Key = string(s.id) + "|" + st.NativeID
	st.AccountID = s.id
	st.Channel = s.channel
	st.FromMe = fromMe != 0
	st.Kind = model.ContentKind(kind)
	st.Viewed = viewed != 0
	st.Delivery = model.DeliveryState(delivery)
	if mid.Valid {
		st.Media = &model.Media{
			ID: mid.String, Mime: mime.String, FileName: fname.String, Size: msize.Int64,
			Width: int(mw.Int64), Height: int(mh.Int64), Duration: mdur.Float64, Thumb: thumb.String,
			LocalPath: lpath.String, URL: mediaURL(s.id, mid.String), Handle: handle,
		}
	}
	return st, nil
}

// nativeStatusID quita el prefijo "<cuenta>|" de la clave de un estado.
func (s *AccountStore) nativeStatusID(key string) string {
	return strings.TrimPrefix(key, string(s.id)+"|")
}

// queryStatuses ejecuta una consulta de statusSelect y completa el número de respuestas.
func (s *AccountStore) queryStatuses(ctx context.Context, q string, args ...any) ([]model.Status, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer los estados: %w", err)
	}
	out := []model.Status{}
	for rows.Next() {
		st, err := s.scanStatus(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("almacén: estado ilegible: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.fillReplyCounts(ctx, out)
}

// fillReplyCounts cuenta los mensajes que responden a cada estado (extra_json.fromStatusId, con el id
// nativo o con la clave). Solo mira mensajes desde que se publicó el estado más antiguo.
func (s *AccountStore) fillReplyCounts(ctx context.Context, sts []model.Status) error {
	if len(sts) == 0 {
		return nil
	}
	minAt := sts[0].PostedAt
	for _, st := range sts {
		if st.PostedAt < minAt {
			minAt = st.PostedAt
		}
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT json_extract(extra_json, '$.fromStatusId'), COUNT(*) FROM messages
 WHERE ts >= ? AND extra_json LIKE '%"fromStatusId"%' GROUP BY 1`, minAt)
	if err != nil {
		return fmt.Errorf("almacén: no se pudieron contar las respuestas a estados: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var (
			id sql.NullString
			n  int
		)
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		if id.Valid {
			counts[id.String] += n
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range sts {
		sts[i].ReplyCount = counts[sts[i].NativeID] + counts[sts[i].Key]
	}
	return nil
}

// UpsertStatus crea o actualiza un estado. No quita lo visto, ni lo borrado, ni baja la entrega.
func (s *AccountStore) UpsertStatus(ctx context.Context, st model.Status) error {
	if st.NativeID == "" && st.Key != "" {
		st.NativeID = s.nativeStatusID(st.Key)
	}
	if st.NativeID == "" {
		return invalid("el estado no tiene id")
	}
	kind := st.Kind
	if kind == "" {
		kind = model.KindText
	}
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		// Aviso parcial de borrado (solo ids y DeletedAt): se fusiona, no reemplaza la fila.
		if st.DeletedAt > 0 && st.PostedAt == 0 {
			res, err := tx.ExecContext(ctx, `UPDATE statuses SET deleted_at = MAX(deleted_at, ?) WHERE native_id = ?`, st.DeletedAt, st.NativeID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				return nil
			}
		}
		mediaID := ""
		if st.Media != nil {
			md := *st.Media
			if md.ID == "" {
				sum := sha256.Sum256([]byte("s|" + st.NativeID))
				md.ID = hex.EncodeToString(sum[:12])
			}
			mediaID = md.ID
			if err := upsertMedia(ctx, tx, &md, mediaID, statusMediaChat, st.NativeID); err != nil {
				return err
			}
		}
		var curDelivery string
		err := tx.QueryRowContext(ctx, `SELECT delivery FROM statuses WHERE native_id = ?`, st.NativeID).Scan(&curDelivery)
		if err != nil && !isNoRows(err) {
			return err
		}
		delivery := mergeDelivery(model.DeliveryState(curDelivery), st.Delivery)
		if delivery == "" {
			delivery = model.DeliverySent
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO statuses(native_id, author_native_id, author_name, from_me, posted_at, expires_at, kind, text, bg_color,
  font, media_id, viewed, deleted_at, delivery)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(native_id) DO UPDATE SET
  author_native_id = excluded.author_native_id,
  author_name = CASE WHEN excluded.author_name <> '' THEN excluded.author_name ELSE statuses.author_name END,
  from_me = excluded.from_me, posted_at = excluded.posted_at, expires_at = excluded.expires_at,
  kind = excluded.kind, text = excluded.text, bg_color = excluded.bg_color, font = excluded.font,
  media_id = CASE WHEN excluded.media_id <> '' THEN excluded.media_id ELSE statuses.media_id END,
  viewed = MAX(statuses.viewed, excluded.viewed),
  deleted_at = MAX(statuses.deleted_at, excluded.deleted_at),
  delivery = excluded.delivery`,
			st.NativeID, st.AuthorNativeID, st.AuthorName, b2i(st.FromMe), st.PostedAt, st.ExpiresAt, string(kind), st.Text,
			st.BgColor, st.Font, mediaID, b2i(st.Viewed), st.DeletedAt, string(delivery))
		if err != nil {
			return fmt.Errorf("almacén: no se pudo guardar el estado: %w", err)
		}
		return nil
	})
}

// ListStatusAuthors devuelve una fila por persona con estados vigentes (no vencidos ni borrados):
// primero quienes tienen estados sin ver y luego por el más reciente.
func (s *AccountStore) ListStatusAuthors(ctx context.Context, now int64) ([]model.StatusAuthor, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.author_native_id, COUNT(*), SUM(CASE WHEN s.viewed = 0 AND s.from_me = 0 THEN 1 ELSE 0 END), MAX(s.posted_at),
       COALESCE((SELECT x.author_name FROM statuses x
                  WHERE x.author_native_id = s.author_native_id AND x.expires_at > ? AND x.deleted_at = 0 AND x.author_name <> ''
                  ORDER BY x.posted_at DESC LIMIT 1), ''),
       COALESCE((SELECT COALESCE(NULLIF(p.name, ''), NULLIF(p.push_name, ''), NULLIF(p.username, ''), NULLIF(p.phone, ''))
                   FROM persons p WHERE p.native_id = s.author_native_id), ''),
       COALESCE((SELECT p.avatar_file FROM persons p WHERE p.native_id = s.author_native_id), '')
  FROM statuses s
 WHERE s.expires_at > ? AND s.deleted_at = 0
 GROUP BY s.author_native_id
 ORDER BY (SUM(CASE WHEN s.viewed = 0 AND s.from_me = 0 THEN 1 ELSE 0 END) > 0) DESC, MAX(s.posted_at) DESC, s.author_native_id`,
		now, now)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer los autores de estados: %w", err)
	}
	defer rows.Close()
	out := []model.StatusAuthor{}
	for rows.Next() {
		var (
			a                      model.StatusAuthor
			statusName, personName string
			avatar                 string
		)
		if err := rows.Scan(&a.AuthorID, &a.Count, &a.Unviewed, &a.LastAt, &statusName, &personName, &avatar); err != nil {
			return nil, err
		}
		a.AccountID = s.id
		a.AuthorName = firstNonEmpty(personName, statusName)
		if a.AuthorName == "" {
			a.AuthorName = a.AuthorID
		}
		a.AvatarURL = avatarURL(s.id, avatar)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListStatuses devuelve los estados vigentes de una persona, del más antiguo al más reciente.
func (s *AccountStore) ListStatuses(ctx context.Context, authorID string, now int64) ([]model.Status, error) {
	return s.queryStatuses(ctx,
		statusSelect+` WHERE s.author_native_id = ? AND s.expires_at > ? AND s.deleted_at = 0 ORDER BY s.posted_at, s.native_id`,
		authorID, now)
}

// ListMyStatuses devuelve mis estados publicados entre from y to (ambos incluidos; <= 0 deja el
// extremo abierto), vencidos o no, sin los borrados; el más reciente primero.
func (s *AccountStore) ListMyStatuses(ctx context.Context, from, to int64) ([]model.Status, error) {
	q := statusSelect + ` WHERE s.from_me = 1 AND s.deleted_at = 0`
	var args []any
	if from > 0 {
		q += ` AND s.posted_at >= ?`
		args = append(args, from)
	}
	if to > 0 {
		q += ` AND s.posted_at <= ?`
		args = append(args, to)
	}
	q += ` ORDER BY s.posted_at DESC, s.native_id`
	return s.queryStatuses(ctx, q, args...)
}

// UpsertStatusView guarda quién vio (o reaccionó a) un estado. Una vista posterior no borra la
// reacción y se conserva la primera hora de vista.
func (s *AccountStore) UpsertStatusView(ctx context.Context, v model.StatusView) error {
	native := s.nativeStatusID(v.StatusKey)
	if native == "" {
		return invalid("la vista no tiene estado")
	}
	at := v.At
	if at == 0 {
		at = nowMs()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO status_views(status_native_id, viewer_id, viewer_name, at, reaction) VALUES(?, ?, ?, ?, ?)
ON CONFLICT(status_native_id, viewer_id) DO UPDATE SET
  viewer_name = CASE WHEN excluded.viewer_name <> '' THEN excluded.viewer_name ELSE status_views.viewer_name END,
  at = MIN(status_views.at, excluded.at),
  reaction = CASE WHEN excluded.reaction <> '' THEN excluded.reaction ELSE status_views.reaction END`,
		native, v.ViewerID, v.ViewerName, at, v.Reaction)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar la vista del estado: %w", err)
	}
	return nil
}

// ListStatusViews devuelve quién vio el estado, la vista más reciente primero.
func (s *AccountStore) ListStatusViews(ctx context.Context, statusKey string) ([]model.StatusView, error) {
	native := s.nativeStatusID(statusKey)
	rows, err := s.db.QueryContext(ctx,
		`SELECT viewer_id, viewer_name, at, reaction FROM status_views WHERE status_native_id = ? ORDER BY at DESC, viewer_id`, native)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer las vistas: %w", err)
	}
	defer rows.Close()
	out := []model.StatusView{}
	key := string(s.id) + "|" + native
	for rows.Next() {
		v := model.StatusView{StatusKey: key}
		if err := rows.Scan(&v.ViewerID, &v.ViewerName, &v.At, &v.Reaction); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
