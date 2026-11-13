package meta

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"dconnector/internal/core/model"
)

// Local da al adaptador lo que necesita saber de lo ya guardado: el chat de un mensaje, qué mensajes míos
// siguen sin leer y cuál es el más antiguo de un chat. Solo LEE la tabla messages del almacén (05) y tolera
// que no exista (pruebas): ante cualquier error devuelve vacío.
type Local struct {
	db *sql.DB

	mu      sync.Mutex
	msgChat map[string]string
	order   []string
}

const msgChatCap = 20000

// NewLocal crea el acceso a lo guardado.
func NewLocal(db *sql.DB) *Local { return &Local{db: db, msgChat: map[string]string{}} }

// Remember anota en memoria en qué chat está un mensaje.
func (l *Local) Remember(chat, msgID string) {
	if msgID == "" || chat == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.msgChat[msgID]; !ok {
		l.order = append(l.order, msgID)
		if len(l.order) > msgChatCap {
			drop := l.order[:msgChatCap/4]
			for _, id := range drop {
				delete(l.msgChat, id)
			}
			l.order = append([]string(nil), l.order[len(drop):]...)
		}
	}
	l.msgChat[msgID] = chat
}

// ChatOfMessage devuelve el chat de un mensaje por su id; "" si no se conoce.
func (l *Local) ChatOfMessage(ctx context.Context, msgID string) string {
	l.mu.Lock()
	chat, ok := l.msgChat[msgID]
	l.mu.Unlock()
	if ok {
		return chat
	}
	if l.db == nil {
		return ""
	}
	var found string
	if err := l.db.QueryRowContext(ctx, "SELECT chat_native_id FROM messages WHERE native_id = ? LIMIT 1", msgID).Scan(&found); err != nil {
		return ""
	}
	return found
}

// OutgoingUpTo devuelve los ids de mis mensajes del chat hasta upToMs que aún no figuran como leídos.
func (l *Local) OutgoingUpTo(ctx context.Context, chat string, upToMs int64) []string {
	if l.db == nil {
		return nil
	}
	rows, err := l.db.QueryContext(ctx,
		"SELECT native_id FROM messages WHERE chat_native_id = ? AND from_me = 1 AND ts <= ? "+
			"AND delivery NOT IN ('read', 'played') ORDER BY ts DESC LIMIT 500", chat, upToMs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// Oldest devuelve el id y la hora (ms) del mensaje más antiguo guardado de un chat.
func (l *Local) Oldest(ctx context.Context, chat string) (string, int64) {
	if l.db == nil {
		return "", 0
	}
	var id string
	var ts int64
	if err := l.db.QueryRowContext(ctx,
		"SELECT native_id, ts FROM messages WHERE chat_native_id = ? ORDER BY ts ASC LIMIT 1", chat).Scan(&id, &ts); err != nil {
		return "", 0
	}
	return id, ts
}

// ---------------------------------------------------------------------------------------------
// «Escribiendo…» con fin automático

// TypingTimers emite «dejó de escribir» cuando pasa el tiempo sin nuevas señales (Meta no avisa del fin).
type TypingTimers struct {
	mu     sync.Mutex
	timers map[string]*time.Timer
}

// NewTypingTimers crea el conjunto de temporizadores.
func NewTypingTimers() *TypingTimers { return &TypingTimers{timers: map[string]*time.Timer{}} }

// Touch reinicia el temporizador de (chat, persona); al vencer llama a stop. Con typing=false lo cancela.
func (t *TypingTimers) Touch(key string, typing bool, ttl time.Duration, stop func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if old := t.timers[key]; old != nil {
		old.Stop()
		delete(t.timers, key)
	}
	if !typing {
		return
	}
	t.timers[key] = time.AfterFunc(ttl, func() {
		t.mu.Lock()
		delete(t.timers, key)
		t.mu.Unlock()
		stop()
	})
}

// StopAll cancela todos los temporizadores (al detener la cuenta).
func (t *TypingTimers) StopAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, tm := range t.timers {
		tm.Stop()
		delete(t.timers, k)
	}
}

// Quote devuelve el texto corto y el nombre del autor de un mensaje ya guardado (para citarlo); vacíos si no se conoce.
func (l *Local) Quote(ctx context.Context, chat, msgID string) (preview, sender string) {
	if l.db == nil {
		return "", ""
	}
	var kind, text, name string
	if err := l.db.QueryRowContext(ctx,
		"SELECT kind, text, sender_name FROM messages WHERE chat_native_id = ? AND native_id = ?", chat, msgID).Scan(&kind, &text, &name); err != nil {
		return "", ""
	}
	return model.Preview(model.Message{Kind: model.ContentKind(kind), Text: text}), name
}

// Newest devuelve el id y la hora (ms) del mensaje más reciente guardado de un chat ("", 0 si no hay).
func (l *Local) Newest(ctx context.Context, chat string) (string, int64) {
	if l.db == nil {
		return "", 0
	}
	var id string
	var ts int64
	if err := l.db.QueryRowContext(ctx,
		"SELECT native_id, ts FROM messages WHERE chat_native_id = ? ORDER BY ts DESC LIMIT 1", chat).Scan(&id, &ts); err != nil {
		return "", 0
	}
	return id, ts
}

// MyReaction devuelve el emoji con que reaccioné a un mensaje ("" si no consta).
func (l *Local) MyReaction(ctx context.Context, chat, msgID, self string) string {
	if l.db == nil {
		return ""
	}
	var emoji string
	if err := l.db.QueryRowContext(ctx,
		"SELECT emoji FROM reactions WHERE chat_native_id = ? AND message_native_id = ? AND sender_native_id = ? LIMIT 1",
		chat, msgID, self).Scan(&emoji); err != nil {
		return ""
	}
	return emoji
}
