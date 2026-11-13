package whatsapp

// Tablas propias del adaptador, con prefijo wa_, en el mismo account.db (el adaptador no recibe
// el AccountStore). Guardan lo mínimo:
//   - wa_kv:    marcas de tiempo (p. ej. la última consulta de avatar de cada JID).
//   - wa_chats: chats que el adaptador ya anunció con chat.updated (para anunciar solo el primero)
//     y sus marcas de archivado, fijado y silencio (para que cada chat.updated salga completo).
//   - wa_msgs:  los últimos mensajes vistos en vivo (proto sin cita), para citar y reenviar.
//   - wa_polls: encuestas con sus opciones y votos acumulados.

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS wa_kv (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS wa_chats (
  chat TEXT PRIMARY KEY, archived INTEGER NOT NULL DEFAULT 0, pinned INTEGER NOT NULL DEFAULT 0,
  muted INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS wa_msgs (
  chat TEXT NOT NULL, id TEXT NOT NULL, ts INTEGER NOT NULL, payload BLOB NOT NULL,
  PRIMARY KEY (chat, id)
);
CREATE INDEX IF NOT EXISTS idx_wa_msgs_ts ON wa_msgs(ts);
CREATE TABLE IF NOT EXISTS wa_polls (
  chat TEXT NOT NULL, id TEXT NOT NULL, question TEXT NOT NULL, options TEXT NOT NULL,
  multi INTEGER NOT NULL, votes TEXT NOT NULL DEFAULT '{}',
  PRIMARY KEY (chat, id)
);
`

// Cuántos mensajes se conservan en wa_msgs y cada cuántas escrituras se poda.
const (
	msgCacheKeep       = 3000
	msgCachePruneEvery = 200
)

// chatFlags: marcas de un chat que el adaptador refleja en sus chat.updated.
type chatFlags struct {
	Archived bool
	Pinned   bool
	Muted    int64 // 0 = no; -1 = siempre; si no, ms de fin
}

type localDB struct {
	db *sql.DB

	mu    sync.Mutex
	puts  int
	known map[string]bool
	flags map[string]chatFlags
}

func newLocalDB(db *sql.DB) *localDB {
	return &localDB{db: db, known: map[string]bool{}, flags: map[string]chatFlags{}}
}

func (l *localDB) ensure(ctx context.Context) error {
	if _, err := l.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("crear tablas wa_: %w", err)
	}
	return nil
}

// wipe borra todo lo que guarda el adaptador (al cerrar la sesión).
func (l *localDB) wipe(ctx context.Context) {
	for _, t := range []string{"wa_kv", "wa_chats", "wa_msgs", "wa_polls"} {
		_, _ = l.db.ExecContext(ctx, "DELETE FROM "+t)
	}
	l.mu.Lock()
	l.known = map[string]bool{}
	l.flags = map[string]chatFlags{}
	l.mu.Unlock()
}

func (l *localDB) kvGet(ctx context.Context, key string) (string, bool) {
	var v string
	if err := l.db.QueryRowContext(ctx, "SELECT value FROM wa_kv WHERE key = ?", key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (l *localDB) kvSet(ctx context.Context, key, value string) {
	_, _ = l.db.ExecContext(ctx,
		"INSERT INTO wa_kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
}

// chatKnown dice si el chat ya se anunció; markChat lo anota.
func (l *localDB) chatKnown(ctx context.Context, chat string) bool {
	l.mu.Lock()
	if l.known[chat] {
		l.mu.Unlock()
		return true
	}
	l.mu.Unlock()
	var one int
	if err := l.db.QueryRowContext(ctx, "SELECT 1 FROM wa_chats WHERE chat = ?", chat).Scan(&one); err == nil {
		l.mu.Lock()
		l.known[chat] = true
		l.mu.Unlock()
		return true
	}
	return false
}

func (l *localDB) markChat(ctx context.Context, chat string) {
	l.mu.Lock()
	l.known[chat] = true
	l.mu.Unlock()
	_, _ = l.db.ExecContext(ctx, "INSERT OR IGNORE INTO wa_chats(chat) VALUES(?)", chat)
}

// chatFlagsOf devuelve las marcas guardadas de un chat (todo en falso si no hay).
func (l *localDB) chatFlagsOf(ctx context.Context, chat string) chatFlags {
	l.mu.Lock()
	if f, ok := l.flags[chat]; ok {
		l.mu.Unlock()
		return f
	}
	l.mu.Unlock()
	var f chatFlags
	var arch, pin int
	if err := l.db.QueryRowContext(ctx, "SELECT archived, pinned, muted FROM wa_chats WHERE chat = ?", chat).Scan(&arch, &pin, &f.Muted); err != nil {
		return chatFlags{}
	}
	f.Archived, f.Pinned = arch != 0, pin != 0
	l.mu.Lock()
	l.flags[chat] = f
	l.known[chat] = true
	l.mu.Unlock()
	return f
}

// saveChatFlags guarda las marcas de un chat (y lo da por anunciado).
func (l *localDB) saveChatFlags(ctx context.Context, chat string, f chatFlags) {
	l.mu.Lock()
	l.flags[chat] = f
	l.known[chat] = true
	l.mu.Unlock()
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	_, _ = l.db.ExecContext(ctx,
		`INSERT INTO wa_chats(chat, archived, pinned, muted) VALUES(?, ?, ?, ?)
		 ON CONFLICT(chat) DO UPDATE SET archived = excluded.archived, pinned = excluded.pinned, muted = excluded.muted`,
		chat, b2i(f.Archived), b2i(f.Pinned), f.Muted)
}

// putMsg guarda el proto de un mensaje visto en vivo (sin ContextInfo: pesa menos y al reenviar
// se pone uno nuevo).
func (l *localDB) putMsg(ctx context.Context, chat, id string, ts int64, m *waE2E.Message) {
	if m == nil || chat == "" || id == "" {
		return
	}
	stripped := withContext(m, nil)
	if stripped == nil {
		stripped = m
	}
	b, err := proto.Marshal(stripped)
	if err != nil || len(b) == 0 {
		return
	}
	_, _ = l.db.ExecContext(ctx,
		`INSERT INTO wa_msgs(chat, id, ts, payload) VALUES(?, ?, ?, ?)
		 ON CONFLICT(chat, id) DO UPDATE SET payload = excluded.payload, ts = excluded.ts`, chat, id, ts, b)
	l.mu.Lock()
	l.puts++
	prune := l.puts%msgCachePruneEvery == 0
	l.mu.Unlock()
	if prune {
		_, _ = l.db.ExecContext(ctx,
			`DELETE FROM wa_msgs WHERE rowid NOT IN (SELECT rowid FROM wa_msgs ORDER BY ts DESC LIMIT ?)`, msgCacheKeep)
	}
}

func (l *localDB) getMsg(ctx context.Context, chat, id string) (*waE2E.Message, bool) {
	var b []byte
	if err := l.db.QueryRowContext(ctx, "SELECT payload FROM wa_msgs WHERE chat = ? AND id = ?", chat, id).Scan(&b); err != nil {
		return nil, false
	}
	var m waE2E.Message
	if err := proto.Unmarshal(b, &m); err != nil {
		return nil, false
	}
	return &m, true
}

// withContext devuelve una copia del mensaje con el ContextInfo dado en su submensaje (nil lo quita).
// Un Conversation se convierte en ExtendedTextMessage si hace falta contexto. Devuelve nil si el
// tipo de mensaje no admite contexto (reacciones, protocolo…).
func withContext(m *waE2E.Message, ci *waE2E.ContextInfo) *waE2E.Message {
	m = unwrap(m)
	if m == nil {
		return nil
	}
	cl := proto.Clone(m).(*waE2E.Message)
	cl.MessageContextInfo = nil
	switch {
	case cl.Conversation != nil:
		if ci == nil {
			return cl
		}
		text := cl.GetConversation()
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &text, ContextInfo: ci}}
	case cl.ExtendedTextMessage != nil:
		cl.ExtendedTextMessage.ContextInfo = ci
	case cl.ImageMessage != nil:
		cl.ImageMessage.ContextInfo = ci
	case cl.VideoMessage != nil:
		cl.VideoMessage.ContextInfo = ci
	case cl.AudioMessage != nil:
		cl.AudioMessage.ContextInfo = ci
	case cl.DocumentMessage != nil:
		cl.DocumentMessage.ContextInfo = ci
	case cl.StickerMessage != nil:
		cl.StickerMessage.ContextInfo = ci
	case cl.LocationMessage != nil:
		cl.LocationMessage.ContextInfo = ci
	case cl.LiveLocationMessage != nil:
		cl.LiveLocationMessage.ContextInfo = ci
	case cl.ContactMessage != nil:
		cl.ContactMessage.ContextInfo = ci
	case cl.ContactsArrayMessage != nil:
		cl.ContactsArrayMessage.ContextInfo = ci
	default:
		if p := pollOf(cl); p != nil {
			p.ContextInfo = ci
			return cl
		}
		return nil
	}
	return cl
}
