package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const defaultChatLimit = 60

// AccountStore implementa port.AccountStore sobre account.db de una cuenta. Los mensajes están en
// account_messages.go y los estados en account_statuses.go.
type AccountStore struct {
	id      model.AccountID
	channel model.Channel // deducido del prefijo del id de cuenta
	db      *sql.DB
}

var _ port.AccountStore = (*AccountStore)(nil)

func newAccountStore(id model.AccountID, db *sql.DB) *AccountStore {
	return &AccountStore{id: id, channel: channelOf(id), db: db}
}

// AccountID devuelve el id de la cuenta dueña de esta base.
func (s *AccountStore) AccountID() model.AccountID { return s.id }

// Close cierra la base (la misma que usa la librería del canal).
func (s *AccountStore) Close() error { return s.db.Close() }

// ---- Chats ----

const chatCols = `native_id, kind, title, avatar_file, unread_count, last_message_at, last_preview, last_from_me,
 archived, pinned, muted_until, read_only, peer_native_id`

func (s *AccountStore) scanChat(sc scanner) (model.Chat, error) {
	var (
		c                                      model.Chat
		kind                                   string
		avatar                                 string
		lastFromMe, archived, pinned, readOnly int
	)
	if err := sc.Scan(&c.NativeID, &kind, &c.Title, &avatar, &c.UnreadCount, &c.LastMessageAt, &c.LastPreview,
		&lastFromMe, &archived, &pinned, &c.MutedUntil, &readOnly, &c.PeerNativeID); err != nil {
		return model.Chat{}, err
	}
	c.Key = model.NewChatKey(s.id, c.NativeID)
	c.AccountID = s.id
	c.Channel = s.channel
	c.Kind = model.ChatKind(kind)
	c.AvatarURL = avatarURL(s.id, avatar)
	c.LastFromMe = lastFromMe != 0
	c.Archived = archived != 0
	c.Pinned = pinned != 0
	c.ReadOnly = readOnly != 0
	return c, nil
}

// UpsertChat crea o actualiza un chat. Un título o avatar vacío no borra el que ya hay; la marca de
// tiempo y la vista previa solo cambian si el chat nuevo no es más antiguo que el guardado (se
// conserva la mayor), y una vista previa vacía nunca borra la que ya hay.
func (s *AccountStore) UpsertChat(ctx context.Context, c model.Chat) error {
	if c.NativeID == "" {
		return invalid("el chat no tiene id")
	}
	kind := c.Kind
	if kind == "" {
		kind = model.ChatDirect
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO chats(native_id, kind, title, avatar_file, unread_count, last_message_at, last_preview, last_from_me,
  archived, pinned, muted_until, read_only, peer_native_id)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(native_id) DO UPDATE SET
  kind = excluded.kind,
  title = CASE WHEN excluded.title <> '' THEN excluded.title ELSE chats.title END,
  avatar_file = CASE WHEN excluded.avatar_file <> '' THEN excluded.avatar_file ELSE chats.avatar_file END,
  unread_count = CASE WHEN ? < 0 THEN chats.unread_count ELSE ? END,
  last_preview = CASE WHEN excluded.last_preview <> ''
                       AND ((excluded.last_message_at > 0 AND excluded.last_message_at >= chats.last_message_at)
                            OR chats.last_preview = '')
                      THEN excluded.last_preview ELSE chats.last_preview END,
  last_from_me = CASE WHEN excluded.last_preview <> ''
                       AND ((excluded.last_message_at > 0 AND excluded.last_message_at >= chats.last_message_at)
                            OR chats.last_preview = '')
                      THEN excluded.last_from_me ELSE chats.last_from_me END,
  last_message_at = CASE WHEN excluded.last_message_at > 0 AND excluded.last_message_at >= chats.last_message_at
                         THEN excluded.last_message_at ELSE chats.last_message_at END,
  archived = excluded.archived, pinned = excluded.pinned, muted_until = excluded.muted_until,
  read_only = excluded.read_only,
  peer_native_id = CASE WHEN excluded.peer_native_id <> '' THEN excluded.peer_native_id ELSE chats.peer_native_id END`,
		// UnreadCount < 0 significa «sin cambio»: en un chat nuevo se guarda 0.
		c.NativeID, string(kind), c.Title, avatarFile(s.id, c.AvatarURL), max(c.UnreadCount, 0), c.LastMessageAt, c.LastPreview,
		b2i(c.LastFromMe), b2i(c.Archived), b2i(c.Pinned), c.MutedUntil, b2i(c.ReadOnly), c.PeerNativeID,
		c.UnreadCount, c.UnreadCount) // los dos ? de unread_count dentro de ON CONFLICT
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el chat: %w", err)
	}
	return nil
}

// patchColumns: nombre JSON del campo → columna y cómo sacar el valor del chat.
var patchColumns = map[string]struct {
	col string
	val func(s *AccountStore, c model.Chat) any
}{
	"kind":          {"kind", func(_ *AccountStore, c model.Chat) any { return string(c.Kind) }},
	"title":         {"title", func(_ *AccountStore, c model.Chat) any { return c.Title }},
	"avatarUrl":     {"avatar_file", func(s *AccountStore, c model.Chat) any { return avatarFile(s.id, c.AvatarURL) }},
	"unreadCount":   {"unread_count", func(_ *AccountStore, c model.Chat) any { return c.UnreadCount }},
	"lastMessageAt": {"last_message_at", func(_ *AccountStore, c model.Chat) any { return c.LastMessageAt }},
	"lastPreview":   {"last_preview", func(_ *AccountStore, c model.Chat) any { return c.LastPreview }},
	"lastFromMe":    {"last_from_me", func(_ *AccountStore, c model.Chat) any { return b2i(c.LastFromMe) }},
	"archived":      {"archived", func(_ *AccountStore, c model.Chat) any { return b2i(c.Archived) }},
	"pinned":        {"pinned", func(_ *AccountStore, c model.Chat) any { return b2i(c.Pinned) }},
	"mutedUntil":    {"muted_until", func(_ *AccountStore, c model.Chat) any { return c.MutedUntil }},
	"readOnly":      {"read_only", func(_ *AccountStore, c model.Chat) any { return b2i(c.ReadOnly) }},
	"peerNativeId":  {"peer_native_id", func(_ *AccountStore, c model.Chat) any { return c.PeerNativeID }},
}

// PatchChat aplica solo los campos de `fields` (nombres JSON del modelo). Un nombre desconocido es
// ErrInvalid; si el chat no existe, ErrNotFound.
func (s *AccountStore) PatchChat(ctx context.Context, c model.Chat, fields []string) error {
	if c.NativeID == "" {
		return invalid("el chat no tiene id")
	}
	if len(fields) == 0 {
		return nil
	}
	sets := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	seen := map[string]bool{}
	for _, f := range fields {
		p, ok := patchColumns[f]
		if !ok {
			return invalid("campo de chat desconocido: " + f)
		}
		if seen[p.col] {
			continue
		}
		seen[p.col] = true
		if f == "unreadCount" && c.UnreadCount < 0 {
			continue // «sin cambio»: un valor negativo nunca se guarda
		}
		sets = append(sets, p.col+" = ?")
		args = append(args, p.val(s, c))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, c.NativeID)
	res, err := s.db.ExecContext(ctx, `UPDATE chats SET `+strings.Join(sets, ", ")+` WHERE native_id = ?`, args...)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo modificar el chat: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("chat " + c.NativeID)
	}
	return nil
}

// GetChat devuelve un chat (ErrNotFound si no existe).
func (s *AccountStore) GetChat(ctx context.Context, nativeID string) (model.Chat, error) {
	c, err := s.scanChat(s.db.QueryRowContext(ctx, `SELECT `+chatCols+` FROM chats WHERE native_id = ?`, nativeID))
	if isNoRows(err) {
		return model.Chat{}, notFound("chat " + nativeID)
	}
	if err != nil {
		return model.Chat{}, fmt.Errorf("almacén: no se pudo leer el chat: %w", err)
	}
	return c, nil
}

// ListChats lista los chats según el filtro (docs/05 §3): pestaña unread → unread_count>0,
// groups → kind='group', archived → archived=1, all → archived=0; texto → título contiene (sin
// distinguir mayúsculas). Orden: fijados primero y luego el más reciente.
func (s *AccountStore) ListChats(ctx context.Context, f model.ChatFilter) ([]model.Chat, error) {
	if len(f.AccountIDs) > 0 {
		ok := false
		for _, id := range f.AccountIDs {
			if id == s.id {
				ok = true
			}
		}
		if !ok {
			return []model.Chat{}, nil
		}
	}
	if len(f.Channels) > 0 {
		ok := false
		for _, ch := range f.Channels {
			if ch == s.channel {
				ok = true
			}
		}
		if !ok {
			return []model.Chat{}, nil
		}
	}
	var where []string
	var args []any
	switch f.Tab {
	case "unread":
		where = append(where, "unread_count > 0")
	case "groups":
		where = append(where, "kind = 'group'")
	case "archived":
		where = append(where, "archived = 1")
	default: // "all" o vacío
		where = append(where, "archived = 0")
	}
	if !f.IncludeEmpty {
		// Una lista de chats solo muestra conversaciones con actividad: un directo sin ningún mensaje es
		// un contacto de la libreta, no una conversación. Grupos y canales se muestran siempre, un chat
		// fijado también, y el chat «self» («Tú») solo cuando tiene actividad.
		where = append(where, "NOT (kind IN ('direct', 'self') AND last_message_at = 0 AND pinned = 0)")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, `title LIKE ? ESCAPE '\'`)
		args = append(args, likePattern(q))
	}
	if f.Before > 0 {
		where = append(where, "last_message_at < ?")
		args = append(args, f.Before)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultChatLimit
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+chatCols+` FROM chats WHERE `+strings.Join(where, " AND ")+
			` ORDER BY pinned DESC, last_message_at DESC, native_id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer los chats: %w", err)
	}
	defer rows.Close()
	out := []model.Chat{}
	for rows.Next() {
		c, err := s.scanChat(rows)
		if err != nil {
			return nil, fmt.Errorf("almacén: chat ilegible: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteChat borra el chat con sus mensajes, medios, reacciones y entradas de búsqueda.
func (s *AccountStore) DeleteChat(ctx context.Context, nativeID string) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		stmts := []string{
			`DELETE FROM messages_fts WHERE rowid IN (SELECT rowid FROM messages WHERE chat_native_id = ?)`,
			`DELETE FROM reactions WHERE chat_native_id = ?`,
			`DELETE FROM media WHERE chat_native_id = ?`,
			`DELETE FROM messages WHERE chat_native_id = ?`,
			`DELETE FROM chats WHERE native_id = ?`,
		}
		for _, q := range stmts {
			if _, err := tx.ExecContext(ctx, q, nativeID); err != nil {
				return fmt.Errorf("almacén: no se pudo borrar el chat: %w", err)
			}
		}
		return nil
	})
}

// ---- Personas ----

const personCols = `native_id, name, push_name, phone, username, avatar_file`

func (s *AccountStore) scanPerson(sc scanner) (model.Person, error) {
	var (
		p      model.Person
		avatar string
	)
	if err := sc.Scan(&p.NativeID, &p.Name, &p.PushName, &p.Phone, &p.Username, &avatar); err != nil {
		return model.Person{}, err
	}
	p.AccountID = s.id
	p.AvatarURL = avatarURL(s.id, avatar)
	return p, nil
}

// UpsertPerson crea o completa una persona: un campo vacío no borra el que ya hay.
func (s *AccountStore) UpsertPerson(ctx context.Context, p model.Person) error {
	if p.NativeID == "" {
		return invalid("la persona no tiene id")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO persons(native_id, name, push_name, phone, username, avatar_file) VALUES(?, ?, ?, ?, ?, ?)
ON CONFLICT(native_id) DO UPDATE SET
  name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE persons.name END,
  push_name = CASE WHEN excluded.push_name <> '' THEN excluded.push_name ELSE persons.push_name END,
  phone = CASE WHEN excluded.phone <> '' THEN excluded.phone ELSE persons.phone END,
  username = CASE WHEN excluded.username <> '' THEN excluded.username ELSE persons.username END,
  avatar_file = CASE WHEN excluded.avatar_file <> '' THEN excluded.avatar_file ELSE persons.avatar_file END`,
		p.NativeID, p.Name, p.PushName, p.Phone, p.Username, avatarFile(s.id, p.AvatarURL))
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar la persona: %w", err)
	}
	return nil
}

// GetPerson devuelve una persona (ErrNotFound si no existe).
func (s *AccountStore) GetPerson(ctx context.Context, nativeID string) (model.Person, error) {
	p, err := s.scanPerson(s.db.QueryRowContext(ctx, `SELECT `+personCols+` FROM persons WHERE native_id = ?`, nativeID))
	if isNoRows(err) {
		return model.Person{}, notFound("persona " + nativeID)
	}
	if err != nil {
		return model.Person{}, fmt.Errorf("almacén: no se pudo leer la persona: %w", err)
	}
	return p, nil
}

// ListPersons lista personas cuyo nombre, alias, teléfono, usuario o id contienen `query` (vacío = todas),
// por nombre. limit <= 0 = sin límite.
func (s *AccountStore) ListPersons(ctx context.Context, query string, limit int) ([]model.Person, error) {
	q := `SELECT ` + personCols + ` FROM persons`
	var args []any
	if t := strings.TrimSpace(query); t != "" {
		p := likePattern(t)
		q += ` WHERE name LIKE ? ESCAPE '\' OR push_name LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\'
		        OR username LIKE ? ESCAPE '\' OR native_id LIKE ? ESCAPE '\'`
		args = append(args, p, p, p, p, p)
	}
	q += ` ORDER BY COALESCE(NULLIF(name, ''), NULLIF(push_name, ''), NULLIF(username, ''), NULLIF(phone, ''), native_id) COLLATE NOCASE, native_id`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer las personas: %w", err)
	}
	defer rows.Close()
	out := []model.Person{}
	for rows.Next() {
		p, err := s.scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- KV ----

// KVGet devuelve el valor de la clave; nil, nil si no existe.
func (s *AccountStore) KVGet(ctx context.Context, key string) ([]byte, error) {
	var v []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudo leer el valor: %w", err)
	}
	if v == nil {
		v = []byte{}
	}
	return v, nil
}

// KVSet guarda el valor de la clave.
func (s *AccountStore) KVSet(ctx context.Context, key string, value []byte) error {
	if key == "" {
		return invalid("la clave está vacía")
	}
	if value == nil {
		value = []byte{}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el valor: %w", err)
	}
	return nil
}
