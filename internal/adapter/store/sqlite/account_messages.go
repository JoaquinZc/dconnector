package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	defaultMessageLimit = 50
	defaultSearchLimit  = 50
)

// msgExtra: lo que va en messages.extra_json.
type msgExtra struct {
	Location       *model.Location     `json:"location,omitempty"`
	Contacts       []model.ContactCard `json:"contacts,omitempty"`
	Poll           *model.Poll         `json:"poll,omitempty"`
	Mentions       []string            `json:"mentions,omitempty"`
	QuotedNativeID string              `json:"quotedNativeId,omitempty"`
	QuotedPreview  string              `json:"quotedPreview,omitempty"`
	QuotedSender   string              `json:"quotedSender,omitempty"`
	FromStatusID   string              `json:"fromStatusId,omitempty"`
}

const msgSelect = `SELECT m.chat_native_id, m.native_id, m.sender_native_id, m.sender_name, m.from_me, m.ts, m.kind,
 m.text, m.media_id, m.extra_json, m.forwarded, m.edited_at, m.deleted_at, m.delivery,
 md.id, md.mime, md.file_name, md.size, md.width, md.height, md.duration, md.thumb, md.handle, md.local_path
 FROM messages m LEFT JOIN media md ON md.id = m.media_id AND m.media_id <> ''`

func (s *AccountStore) scanMessage(sc scanner) (model.Message, error) {
	var (
		m                              model.Message
		kind, delivery, mediaID, extra string
		fromMe, forwarded              int
		mid, mime, fname, thumb, lpath sql.NullString
		msize, mw, mh                  sql.NullInt64
		mdur                           sql.NullFloat64
		handle                         []byte
	)
	if err := sc.Scan(&m.ChatNativeID, &m.NativeID, &m.SenderNativeID, &m.SenderName, &fromMe, &m.Timestamp, &kind,
		&m.Text, &mediaID, &extra, &forwarded, &m.EditedAt, &m.DeletedAt, &delivery,
		&mid, &mime, &fname, &msize, &mw, &mh, &mdur, &thumb, &handle, &lpath); err != nil {
		return model.Message{}, err
	}
	m.Key = model.NewMessageKey(s.id, m.ChatNativeID, m.NativeID)
	m.ChatKey = model.NewChatKey(s.id, m.ChatNativeID)
	m.AccountID = s.id
	m.Channel = s.channel
	m.FromMe = fromMe != 0
	m.Kind = model.ContentKind(kind)
	m.Forwarded = forwarded != 0
	m.Delivery = model.DeliveryState(delivery)
	var ex msgExtra
	if err := decodeJSON(extra, &ex); err != nil {
		return model.Message{}, err
	}
	m.Location = ex.Location
	m.Contacts = ex.Contacts
	m.Poll = ex.Poll
	m.Mentions = ex.Mentions
	m.QuotedNativeID = ex.QuotedNativeID
	m.QuotedPreview = ex.QuotedPreview
	m.QuotedSender = ex.QuotedSender
	m.FromStatusID = ex.FromStatusID
	if mid.Valid {
		m.Media = &model.Media{
			ID: mid.String, Mime: mime.String, FileName: fname.String, Size: msize.Int64,
			Width: int(mw.Int64), Height: int(mh.Int64), Duration: mdur.Float64, Thumb: thumb.String,
			LocalPath: lpath.String, URL: mediaURL(s.id, mid.String), Handle: handle,
		}
	}
	return m, nil
}

// mediaIDFor devuelve el id del medio; si el adaptador no lo puso, uno estable a partir del mensaje.
func mediaIDFor(chat, native string, md *model.Media) string {
	if md.ID != "" {
		return md.ID
	}
	sum := sha256.Sum256([]byte("m|" + chat + "|" + native))
	return hex.EncodeToString(sum[:12])
}

// upsertMedia guarda el medio sin borrar lo que ya se sabe (ruta local, miniatura, asa de descarga)
// cuando el valor nuevo viene vacío.
func upsertMedia(ctx context.Context, q dbtx, md *model.Media, id, chat, native string) error {
	var handle any
	if len(md.Handle) > 0 {
		handle = md.Handle
	}
	_, err := q.ExecContext(ctx, `
INSERT INTO media(id, chat_native_id, message_native_id, mime, file_name, size, width, height, duration, thumb, handle, local_path)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  chat_native_id = excluded.chat_native_id, message_native_id = excluded.message_native_id,
  mime = CASE WHEN excluded.mime <> '' THEN excluded.mime ELSE media.mime END,
  file_name = CASE WHEN excluded.file_name <> '' THEN excluded.file_name ELSE media.file_name END,
  size = CASE WHEN excluded.size <> 0 THEN excluded.size ELSE media.size END,
  width = CASE WHEN excluded.width <> 0 THEN excluded.width ELSE media.width END,
  height = CASE WHEN excluded.height <> 0 THEN excluded.height ELSE media.height END,
  duration = CASE WHEN excluded.duration <> 0 THEN excluded.duration ELSE media.duration END,
  thumb = CASE WHEN excluded.thumb <> '' THEN excluded.thumb ELSE media.thumb END,
  handle = COALESCE(excluded.handle, media.handle),
  local_path = CASE WHEN excluded.local_path <> '' THEN excluded.local_path ELSE media.local_path END`,
		id, chat, native, md.Mime, md.FileName, md.Size, md.Width, md.Height, md.Duration, md.Thumb, handle, md.LocalPath)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el medio: %w", err)
	}
	return nil
}

// loadMessage lee un mensaje sin reacciones (ErrNotFound si no existe).
func (s *AccountStore) loadMessage(ctx context.Context, q dbtx, chat, native string) (model.Message, error) {
	m, err := s.scanMessage(q.QueryRowContext(ctx, msgSelect+` WHERE m.chat_native_id = ? AND m.native_id = ?`, chat, native))
	if isNoRows(err) {
		return model.Message{}, notFound("mensaje " + native)
	}
	if err != nil {
		return model.Message{}, fmt.Errorf("almacén: no se pudo leer el mensaje: %w", err)
	}
	return m, nil
}

// writeMessage inserta o reemplaza la fila del mensaje (ya fusionado) y mantiene el índice FTS.
func (s *AccountStore) writeMessage(ctx context.Context, q dbtx, m model.Message, mediaID string) error {
	extra, err := encodeJSON(msgExtra{
		Location: m.Location, Contacts: m.Contacts, Poll: m.Poll, Mentions: m.Mentions,
		QuotedNativeID: m.QuotedNativeID, QuotedPreview: m.QuotedPreview, QuotedSender: m.QuotedSender,
		FromStatusID: m.FromStatusID,
	})
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `
INSERT INTO messages(chat_native_id, native_id, sender_native_id, sender_name, from_me, ts, kind, text, media_id,
  extra_json, forwarded, edited_at, deleted_at, delivery)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(chat_native_id, native_id) DO UPDATE SET
  sender_native_id = excluded.sender_native_id, sender_name = excluded.sender_name, from_me = excluded.from_me,
  ts = excluded.ts, kind = excluded.kind, text = excluded.text, media_id = excluded.media_id,
  extra_json = excluded.extra_json, forwarded = excluded.forwarded, edited_at = excluded.edited_at,
  deleted_at = excluded.deleted_at, delivery = excluded.delivery`,
		m.ChatNativeID, m.NativeID, m.SenderNativeID, m.SenderName, b2i(m.FromMe), m.Timestamp, string(m.Kind), m.Text,
		mediaID, extra, b2i(m.Forwarded), m.EditedAt, m.DeletedAt, string(m.Delivery))
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el mensaje: %w", err)
	}
	return syncFTS(ctx, q, m.ChatNativeID, m.NativeID, m.Text, m.DeletedAt > 0)
}

// syncFTS mantiene messages_fts a mano. La fila del índice usa como rowid el de `messages`, para
// borrarla sin recorrer toda la tabla (un DELETE por columnas UNINDEXED es un escaneo completo).
// Por eso no se debe hacer VACUUM sobre account.db (puede renumerar los rowid).
func syncFTS(ctx context.Context, q dbtx, chat, native, text string, deleted bool) error {
	var rowid int64
	if err := q.QueryRowContext(ctx, `SELECT rowid FROM messages WHERE chat_native_id = ? AND native_id = ?`, chat, native).Scan(&rowid); err != nil {
		return fmt.Errorf("almacén: no se pudo indexar el mensaje: %w", err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM messages_fts WHERE rowid = ?`, rowid); err != nil {
		return fmt.Errorf("almacén: no se pudo actualizar la búsqueda: %w", err)
	}
	if text == "" || deleted {
		return nil
	}
	if _, err := q.ExecContext(ctx,
		`INSERT INTO messages_fts(rowid, text, chat_native_id, native_id) VALUES(?, ?, ?, ?)`, rowid, text, chat, native); err != nil {
		return fmt.Errorf("almacén: no se pudo actualizar la búsqueda: %w", err)
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// mergeMessage fusiona el mensaje que llega (in) con el guardado (old): conserva la entrega mayor,
// los campos que lo nuevo trae vacíos y la edición más reciente.
func mergeMessage(old, in model.Message) model.Message {
	r := in
	r.SenderNativeID = firstNonEmpty(in.SenderNativeID, old.SenderNativeID)
	r.SenderName = firstNonEmpty(in.SenderName, old.SenderName)
	r.QuotedNativeID = firstNonEmpty(in.QuotedNativeID, old.QuotedNativeID)
	r.QuotedPreview = firstNonEmpty(in.QuotedPreview, old.QuotedPreview)
	r.QuotedSender = firstNonEmpty(in.QuotedSender, old.QuotedSender)
	r.FromStatusID = firstNonEmpty(in.FromStatusID, old.FromStatusID)
	if r.Kind == "" {
		r.Kind = old.Kind
	}
	if r.Timestamp <= 0 {
		r.Timestamp = old.Timestamp
	}
	if r.Location == nil {
		r.Location = old.Location
	}
	if len(r.Contacts) == 0 {
		r.Contacts = old.Contacts
	}
	if len(r.Mentions) == 0 {
		r.Mentions = old.Mentions
	}
	if old.Poll != nil { // los votos solo llegan por ApplyUpdate
		r.Poll = old.Poll
	}
	r.Forwarded = in.Forwarded || old.Forwarded
	r.DeletedAt = max(in.DeletedAt, old.DeletedAt)
	if old.EditedAt > in.EditedAt {
		r.Text, r.EditedAt = old.Text, old.EditedAt
	}
	r.Delivery = mergeDelivery(old.Delivery, in.Delivery)
	return r
}

// upsertMessageTx guarda un mensaje dentro de una transacción. countUnread = false para el histórico.
// `touched` (no nulo) recoge los chats cuyo mensaje no es de sistema: quien llama les rellena al final
// la vista previa si quedó vacía (fillEmptyPreviews).
func (s *AccountStore) upsertMessageTx(ctx context.Context, tx dbtx, m model.Message, countUnread bool, touched map[string]struct{}) (bool, error) {
	if m.ChatNativeID == "" && m.Key != "" {
		_, m.ChatNativeID, m.NativeID = m.Key.Split()
	}
	if m.ChatNativeID == "" || m.NativeID == "" {
		return false, invalid("el mensaje no tiene chat o id")
	}
	old, err := s.loadMessage(ctx, tx, m.ChatNativeID, m.NativeID)
	found := err == nil
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return false, err
	}
	merged := m
	if found {
		merged = mergeMessage(old, m)
	} else if merged.Timestamp <= 0 {
		merged.Timestamp = nowMs()
	}
	if merged.Kind == "" {
		merged.Kind = model.KindText
	}
	if merged.Delivery == "" {
		merged.Delivery = model.DeliverySent
	}
	mediaID := ""
	switch {
	case m.Media != nil:
		mediaID = mediaIDFor(m.ChatNativeID, m.NativeID, m.Media)
		if err := upsertMedia(ctx, tx, m.Media, mediaID, m.ChatNativeID, m.NativeID); err != nil {
			return false, err
		}
	case found && old.Media != nil:
		mediaID = old.Media.ID
	}
	if err := s.writeMessage(ctx, tx, merged, mediaID); err != nil {
		return false, err
	}

	// Chat: se crea si no existe y se actualiza la vista previa si el mensaje es el más reciente.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO chats(native_id, kind) VALUES(?, 'direct')`, m.ChatNativeID); err != nil {
		return false, fmt.Errorf("almacén: no se pudo crear el chat: %w", err)
	}
	var last int64
	if err := tx.QueryRowContext(ctx, `SELECT last_message_at FROM chats WHERE native_id = ?`, m.ChatNativeID).Scan(&last); err != nil {
		return false, fmt.Errorf("almacén: no se pudo leer el chat: %w", err)
	}
	if touched != nil && merged.Kind != model.KindSystem {
		touched[m.ChatNativeID] = struct{}{}
	}
	created := !found
	newer := merged.Timestamp > last
	if newer || (!created && merged.Timestamp == last) {
		unread := 0
		if newer && created && countUnread && !merged.FromMe {
			unread = 1
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE chats SET last_message_at = ?, last_preview = ?, last_from_me = ?, unread_count = unread_count + ?
			  WHERE native_id = ?`,
			merged.Timestamp, model.Preview(merged), b2i(merged.FromMe), unread, m.ChatNativeID); err != nil {
			return false, fmt.Errorf("almacén: no se pudo actualizar el chat: %w", err)
		}
	}
	return created, nil
}

// UpsertMessage guarda un mensaje nuevo o fusiona uno existente (conserva reacciones, Media.LocalPath
// y la entrega mayor). Si es nuevo, no es mío y es más reciente que el último del chat, suma 1 a los
// no leídos.
func (s *AccountStore) UpsertMessage(ctx context.Context, m model.Message) (bool, error) {
	var created bool
	err := inTx(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		touched := map[string]struct{}{}
		created, err = s.upsertMessageTx(ctx, tx, m, true, touched)
		if err != nil {
			return err
		}
		return s.fillEmptyPreviews(ctx, tx, touched)
	})
	return created, err
}

// fillEmptyPreviews rellena la vista previa de los chats que se quedaron sin ella pese a tener
// mensajes guardados. El historial de WhatsApp trae la marca de tiempo del chat mayor que la de su
// último mensaje, y entonces ningún mensaje «es el más reciente» para upsertMessageTx. Se hace al
// final de la transacción para que, en un lote desordenado, quede el último mensaje de verdad. Un
// chat sin ningún mensaje (salvo de sistema) conserva la vista previa vacía, y una vista previa que
// ya existe no se toca.
func (s *AccountStore) fillEmptyPreviews(ctx context.Context, tx dbtx, chats map[string]struct{}) error {
	for chat := range chats {
		var preview string
		if err := tx.QueryRowContext(ctx, `SELECT last_preview FROM chats WHERE native_id = ?`, chat).Scan(&preview); err != nil {
			if isNoRows(err) {
				continue
			}
			return fmt.Errorf("almacén: no se pudo leer el chat: %w", err)
		}
		if preview != "" {
			continue
		}
		last, err := s.scanMessage(tx.QueryRowContext(ctx,
			msgSelect+` WHERE m.chat_native_id = ? AND m.kind <> 'system' ORDER BY m.ts DESC, m.rowid DESC LIMIT 1`, chat))
		if isNoRows(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("almacén: no se pudo leer el último mensaje: %w", err)
		}
		pv := model.Preview(last)
		if pv == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE chats SET last_preview = ?, last_from_me = ?, last_message_at = MAX(last_message_at, ?)
			  WHERE native_id = ?`, pv, b2i(last.FromMe), last.Timestamp, chat); err != nil {
			return fmt.Errorf("almacén: no se pudo actualizar la vista previa: %w", err)
		}
	}
	return nil
}

// UpsertMessages guarda un lote (histórico) en una transacción. Nunca suma no leídos.
func (s *AccountStore) UpsertMessages(ctx context.Context, ms []model.Message) (int, error) {
	count := 0
	err := inTx(ctx, s.db, func(tx *sql.Tx) error {
		touched := map[string]struct{}{}
		for _, m := range ms {
			created, err := s.upsertMessageTx(ctx, tx, m, false, touched)
			if err != nil {
				return err
			}
			if created {
				count++
			}
		}
		return s.fillEmptyPreviews(ctx, tx, touched)
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// attachReactions completa las reacciones de los mensajes (una consulta por chat y por trozo).
func (s *AccountStore) attachReactions(ctx context.Context, q dbtx, msgs []model.Message) error {
	byChat := map[string][]int{}
	for i, m := range msgs {
		byChat[m.ChatNativeID] = append(byChat[m.ChatNativeID], i)
	}
	for chat, idxs := range byChat {
		pos := map[string]int{}
		ids := make([]string, 0, len(idxs))
		for _, i := range idxs {
			pos[msgs[i].NativeID] = i
			ids = append(ids, msgs[i].NativeID)
		}
		for _, part := range chunks(ids, 400) {
			args := make([]any, 0, len(part)+1)
			args = append(args, chat)
			for _, id := range part {
				args = append(args, id)
			}
			rows, err := q.QueryContext(ctx,
				`SELECT message_native_id, sender_native_id, sender_name, emoji, at FROM reactions
				  WHERE chat_native_id = ? AND message_native_id IN (`+placeholders(len(part))+`)
				  ORDER BY at, sender_native_id`, args...)
			if err != nil {
				return fmt.Errorf("almacén: no se pudieron leer las reacciones: %w", err)
			}
			for rows.Next() {
				var (
					mid string
					r   model.Reaction
				)
				if err := rows.Scan(&mid, &r.SenderNativeID, &r.SenderName, &r.Emoji, &r.At); err != nil {
					rows.Close()
					return err
				}
				i := pos[mid]
				msgs[i].Reactions = append(msgs[i].Reactions, r)
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// queryMessages ejecuta una consulta de msgSelect y completa las reacciones.
func (s *AccountStore) queryMessages(ctx context.Context, q string, args ...any) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer los mensajes: %w", err)
	}
	out := []model.Message{}
	for rows.Next() {
		m, err := s.scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("almacén: mensaje ilegible: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachReactions(ctx, s.db, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMessage devuelve un mensaje con sus reacciones (ErrNotFound si no existe).
func (s *AccountStore) GetMessage(ctx context.Context, chatNativeID, nativeID string) (model.Message, error) {
	m, err := s.loadMessage(ctx, s.db, chatNativeID, nativeID)
	if err != nil {
		return model.Message{}, err
	}
	ms := []model.Message{m}
	if err := s.attachReactions(ctx, s.db, ms); err != nil {
		return model.Message{}, err
	}
	return ms[0], nil
}

// ApplyUpdate aplica una edición, borrado, cambio de entrega, votos o descarga de medio y devuelve
// el mensaje resultante.
func (s *AccountStore) ApplyUpdate(ctx context.Context, u model.MessageUpdate) (model.Message, error) {
	_, chat, native := u.Key.Split()
	if chat == "" || native == "" {
		return model.Message{}, invalid("la clave del mensaje no es válida")
	}
	var out model.Message
	err := inTx(ctx, s.db, func(tx *sql.Tx) error {
		m, err := s.loadMessage(ctx, tx, chat, native)
		if err != nil {
			return err
		}
		if u.Text != nil {
			m.Text = *u.Text
			m.EditedAt = u.EditedAt
			if m.EditedAt == 0 {
				m.EditedAt = nowMs()
			}
		} else if u.EditedAt > m.EditedAt {
			m.EditedAt = u.EditedAt
		}
		if u.Deleted && m.DeletedAt == 0 {
			m.DeletedAt = nowMs()
		}
		if u.Delivery != "" {
			m.Delivery = mergeDelivery(m.Delivery, u.Delivery)
		}
		if u.Poll != nil {
			m.Poll = u.Poll
		}
		mediaID := ""
		if m.Media != nil {
			mediaID = m.Media.ID
		}
		if u.Media != nil {
			md := *u.Media
			if md.ID == "" {
				md.ID = mediaID
			}
			mediaID = mediaIDFor(chat, native, &md)
			if err := upsertMedia(ctx, tx, &md, mediaID, chat, native); err != nil {
				return err
			}
		}
		if err := s.writeMessage(ctx, tx, m, mediaID); err != nil {
			return err
		}
		// Si es el último mensaje del chat, la vista previa debe reflejar el cambio.
		if _, err := tx.ExecContext(ctx,
			`UPDATE chats SET last_preview = ?, last_from_me = ? WHERE native_id = ? AND last_message_at = ?`,
			model.Preview(m), b2i(m.FromMe), chat, m.Timestamp); err != nil {
			return fmt.Errorf("almacén: no se pudo actualizar el chat: %w", err)
		}
		m, err = s.loadMessage(ctx, tx, chat, native)
		if err != nil {
			return err
		}
		ms := []model.Message{m}
		if err := s.attachReactions(ctx, tx, ms); err != nil {
			return err
		}
		out = ms[0]
		return nil
	})
	if err != nil {
		return model.Message{}, err
	}
	return out, nil
}

// ApplyReaction aplica una reacción: cada persona tiene una sola por mensaje (la última reemplaza a la
// anterior) y un emoji vacío la quita. Devuelve el mensaje con sus reacciones.
func (s *AccountStore) ApplyReaction(ctx context.Context, r model.ReactionEvent) (model.Message, error) {
	_, chat, native := r.MessageKey.Split()
	if chat == "" || native == "" {
		return model.Message{}, invalid("la clave del mensaje no es válida")
	}
	var out model.Message
	err := inTx(ctx, s.db, func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM messages WHERE chat_native_id = ? AND native_id = ?`, chat, native).Scan(&one); err != nil {
			if isNoRows(err) {
				return notFound("mensaje " + native)
			}
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM reactions WHERE chat_native_id = ? AND message_native_id = ? AND sender_native_id = ?`,
			chat, native, r.Reaction.SenderNativeID); err != nil {
			return fmt.Errorf("almacén: no se pudo quitar la reacción anterior: %w", err)
		}
		if r.Reaction.Emoji != "" {
			at := r.Reaction.At
			if at == 0 {
				at = nowMs()
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO reactions(chat_native_id, message_native_id, sender_native_id, sender_name, emoji, at)
				 VALUES(?, ?, ?, ?, ?, ?)`,
				chat, native, r.Reaction.SenderNativeID, r.Reaction.SenderName, r.Reaction.Emoji, at); err != nil {
				return fmt.Errorf("almacén: no se pudo guardar la reacción: %w", err)
			}
		}
		m, err := s.loadMessage(ctx, tx, chat, native)
		if err != nil {
			return err
		}
		ms := []model.Message{m}
		if err := s.attachReactions(ctx, tx, ms); err != nil {
			return err
		}
		out = ms[0]
		return nil
	})
	if err != nil {
		return model.Message{}, err
	}
	return out, nil
}

// ApplyReceipt sube el estado de entrega de los mensajes indicados (nunca lo baja). Los mensajes
// que no existen se ignoran.
func (s *AccountStore) ApplyReceipt(ctx context.Context, r model.ReceiptEvent) error {
	if r.Delivery == "" || len(r.MessageKeys) == 0 {
		return nil
	}
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, k := range r.MessageKeys {
			_, chat, native := k.Split()
			if chat == "" || native == "" {
				continue
			}
			var cur string
			err := tx.QueryRowContext(ctx, `SELECT delivery FROM messages WHERE chat_native_id = ? AND native_id = ?`, chat, native).Scan(&cur)
			if isNoRows(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("almacén: no se pudo leer la entrega: %w", err)
			}
			next := mergeDelivery(model.DeliveryState(cur), r.Delivery)
			if string(next) == cur {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE messages SET delivery = ? WHERE chat_native_id = ? AND native_id = ?`,
				string(next), chat, native); err != nil {
				return fmt.Errorf("almacén: no se pudo guardar la entrega: %w", err)
			}
		}
		return nil
	})
}

// ListMessages devuelve hasta `limit` mensajes anteriores a beforeTs (0 = los más recientes), de más
// antiguo a más reciente. HasMore es verdadero si llegaron `limit`.
func (s *AccountStore) ListMessages(ctx context.Context, chatNativeID string, beforeTs int64, limit int) (model.MessagePage, error) {
	if limit <= 0 {
		limit = defaultMessageLimit
	}
	ms, err := s.queryMessages(ctx,
		msgSelect+` WHERE m.chat_native_id = ? AND (? <= 0 OR m.ts < ?) ORDER BY m.ts DESC, m.native_id DESC LIMIT ?`,
		chatNativeID, beforeTs, beforeTs, limit)
	if err != nil {
		return model.MessagePage{}, err
	}
	hasMore := len(ms) == limit
	for i, j := 0, len(ms)-1; i < j; i, j = i+1, j-1 {
		ms[i], ms[j] = ms[j], ms[i]
	}
	return model.MessagePage{Messages: ms, HasMore: hasMore}, nil
}

// ftsQuery convierte el texto del usuario en una consulta FTS5 segura: cada palabra entre comillas
// (Y entre ellas) y la última como prefijo, para buscar mientras se escribe.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	if len(words) == 0 {
		return ""
	}
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	parts[len(parts)-1] += "*"
	return strings.Join(parts, " ")
}

// SearchMessages busca texto en los mensajes con FTS5 (sin distinguir mayúsculas ni tildes), los más
// recientes primero. chatNativeID vacío = todos los chats.
func (s *AccountStore) SearchMessages(ctx context.Context, query string, chatNativeID string, limit int) ([]model.Message, error) {
	match := ftsQuery(query)
	if match == "" {
		return []model.Message{}, nil
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	q := strings.Replace(msgSelect, "FROM messages m", "FROM messages_fts JOIN messages m ON m.chat_native_id = messages_fts.chat_native_id AND m.native_id = messages_fts.native_id", 1)
	q += ` WHERE messages_fts MATCH ?`
	args := []any{match}
	if chatNativeID != "" {
		q += ` AND m.chat_native_id = ?`
		args = append(args, chatNativeID)
	}
	q += ` ORDER BY m.ts DESC, m.native_id DESC LIMIT ?`
	args = append(args, limit)
	return s.queryMessages(ctx, q, args...)
}

// SetMediaPath guarda la ruta local de un medio ya descargado (ErrNotFound si el medio no existe).
func (s *AccountStore) SetMediaPath(ctx context.Context, mediaID, localPath string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE media SET local_path = ? WHERE id = ?`, localPath, mediaID)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar la ruta del medio: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("medio " + mediaID)
	}
	return nil
}

// GetMedia devuelve el medio y el id nativo del chat al que pertenece (ErrNotFound si no existe).
func (s *AccountStore) GetMedia(ctx context.Context, mediaID string) (model.Media, string, error) {
	var (
		md     model.Media
		chat   string
		handle []byte
	)
	err := s.db.QueryRowContext(ctx, `
SELECT id, chat_native_id, mime, file_name, size, width, height, duration, thumb, handle, local_path
  FROM media WHERE id = ?`, mediaID).
		Scan(&md.ID, &chat, &md.Mime, &md.FileName, &md.Size, &md.Width, &md.Height, &md.Duration, &md.Thumb, &handle, &md.LocalPath)
	if isNoRows(err) {
		return model.Media{}, "", notFound("medio " + mediaID)
	}
	if err != nil {
		return model.Media{}, "", fmt.Errorf("almacén: no se pudo leer el medio: %w", err)
	}
	md.Handle = handle
	md.URL = mediaURL(s.id, md.ID)
	return md, chat, nil
}

// MessageStats devuelve una fila por mensaje (sin texto) con from <= ts <= to (to <= 0 = sin tope),
// opcionalmente solo de los chats dados.
func (s *AccountStore) MessageStats(ctx context.Context, from, to int64, chatNativeIDs []string) ([]port.StatRow, error) {
	q := `SELECT m.chat_native_id, COALESCE(c.kind, 'direct'), m.sender_native_id, m.from_me, m.ts, m.kind, length(m.text)
	        FROM messages m LEFT JOIN chats c ON c.native_id = m.chat_native_id
	       WHERE m.ts >= ?`
	args := []any{from}
	if to > 0 {
		q += ` AND m.ts <= ?`
		args = append(args, to)
	}
	if len(chatNativeIDs) > 0 {
		q += ` AND m.chat_native_id IN (` + placeholders(len(chatNativeIDs)) + `)`
		for _, id := range chatNativeIDs {
			args = append(args, id)
		}
	}
	q += ` ORDER BY m.ts`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer las estadísticas: %w", err)
	}
	defer rows.Close()
	out := []port.StatRow{}
	for rows.Next() {
		var (
			r        port.StatRow
			kind, ck string
			fromMe   int
		)
		if err := rows.Scan(&r.ChatNativeID, &ck, &r.SenderID, &fromMe, &r.Timestamp, &kind, &r.TextLen); err != nil {
			return nil, err
		}
		r.ChatKind = model.ChatKind(ck)
		r.Kind = model.ContentKind(kind)
		r.FromMe = fromMe != 0
		out = append(out, r)
	}
	return out, rows.Err()
}
