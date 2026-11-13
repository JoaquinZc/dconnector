package telegram

// Estado propio del adaptador: caché de citas, marcas de chats, índices en kv (encuestas y chat de
// cada mensaje) y lecturas de solo lectura de la tabla messages del almacén.

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/gotd/td/tg"

	"dconnector/internal/core/model"
)

const (
	prefMsgChat = "tg.msgchat."
	prefPoll    = "tg.poll."
	prefPollID  = "tg.pollid."
	// quoteCacheMax: cuántos mensajes recientes se recuerdan para armar citas.
	quoteCacheMax = 2000
)

type quoteEntry struct{ preview, sender string }

// chatMarks: marcas de un chat que el adaptador refleja en cada chat.updated (el almacén las
// reemplaza en cada UpsertChat, así que siempre deben ir completas).
type chatMarks struct {
	Archived bool
	Pinned   bool
	Muted    int64 // 0 = no; -1 = siempre; si no, ms de fin
}

type localState struct {
	db *sql.DB
	kv *kvStore

	mu         sync.Mutex
	quotes     map[string]quoteEntry
	quoteOrder []string
	marks      map[string]chatMarks
	synced     bool
	userSigs   map[int64]string
}

func newLocalState(db *sql.DB, kv *kvStore) *localState {
	return &localState{db: db, kv: kv, quotes: map[string]quoteEntry{}, marks: map[string]chatMarks{}, userSigs: map[int64]string{}}
}

// reset olvida todo lo recordado en memoria (al cerrar la sesión).
func (l *localState) reset() {
	l.mu.Lock()
	l.quotes = map[string]quoteEntry{}
	l.quoteOrder = nil
	l.marks = map[string]chatMarks{}
	l.synced = false
	l.userSigs = map[int64]string{}
	l.mu.Unlock()
}

// ---------------------------------------------------------------------------------------------
// Citas

func quoteKey(chat string, id int) string { return chat + "/" + strconv.Itoa(id) }

// noteQuote recuerda la vista previa y el emisor de un mensaje para citarlo después.
func (l *localState) noteQuote(m model.Message) {
	if m.ChatNativeID == "" || m.NativeID == "" {
		return
	}
	id, err := strconv.Atoi(m.NativeID)
	if err != nil {
		return
	}
	k := quoteKey(m.ChatNativeID, id)
	e := quoteEntry{preview: model.Preview(m), sender: m.SenderName}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.quotes[k]; !ok {
		l.quoteOrder = append(l.quoteOrder, k)
		if len(l.quoteOrder) > quoteCacheMax {
			old := l.quoteOrder[0]
			l.quoteOrder = l.quoteOrder[1:]
			delete(l.quotes, old)
		}
	}
	l.quotes[k] = e
}

// quoteLookup busca un mensaje citado: primero en memoria y luego en la tabla messages del almacén.
func (l *localState) quoteLookup(chat string, id int) (string, string, bool) {
	l.mu.Lock()
	e, ok := l.quotes[quoteKey(chat, id)]
	l.mu.Unlock()
	if ok {
		return e.preview, e.sender, true
	}
	var sender, kind, text string
	err := l.db.QueryRow(
		"SELECT sender_name, kind, text FROM messages WHERE chat_native_id = ? AND native_id = ?",
		chat, strconv.Itoa(id)).Scan(&sender, &kind, &text)
	if err != nil {
		return "", "", false
	}
	return model.Preview(model.Message{Kind: model.ContentKind(kind), Text: text}), sender, true
}

// ---------------------------------------------------------------------------------------------
// Marcas de chats

func (l *localState) marksOf(native string) (chatMarks, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, ok := l.marks[native]
	return m, ok
}

func (l *localState) setMarks(native string, m chatMarks) {
	l.mu.Lock()
	l.marks[native] = m
	l.mu.Unlock()
}

// updateMarks cambia las marcas de un chat conocido; devuelve las nuevas y si el chat era conocido.
func (l *localState) updateMarks(native string, f func(*chatMarks)) (chatMarks, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, ok := l.marks[native]
	if !ok {
		return chatMarks{}, false
	}
	f(&m)
	l.marks[native] = m
	return m, true
}

func (l *localState) markSynced() {
	l.mu.Lock()
	l.synced = true
	l.mu.Unlock()
}

func (l *localState) isSynced() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.synced
}

// noteUserSig recuerda lo mostrado de un usuario; devuelve true si es nuevo o cambió.
func (l *localState) noteUserSig(u *tg.User) bool {
	sig := userSig(u)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.userSigs[u.ID] == sig {
		return false
	}
	l.userSigs[u.ID] = sig
	return true
}

// ---------------------------------------------------------------------------------------------
// Índices en kv

// indexMsgChats guarda en qué chat está cada mensaje de privados y grupos (los ids son por cuenta;
// en canales son por canal y el borrado ya trae el canal).
func (l *localState) indexMsgChats(ctx context.Context, msgs []model.Message) {
	kv := map[string][]byte{}
	for _, m := range msgs {
		if m.ChatNativeID == "" || m.NativeID == "" || strings.HasPrefix(m.ChatNativeID, nativeChannelPrefix) {
			continue
		}
		kv[prefMsgChat+m.NativeID] = []byte(m.ChatNativeID)
	}
	_ = l.kv.setMany(ctx, kv)
}

// chatOfMessage devuelve el chat de un mensaje de privado o grupo por su id; "" si no se conoce.
func (l *localState) chatOfMessage(ctx context.Context, id int) string {
	if v, ok, err := l.kv.get(ctx, prefMsgChat+strconv.Itoa(id)); err == nil && ok {
		return string(v)
	}
	var chat string
	err := l.db.QueryRowContext(ctx,
		"SELECT chat_native_id FROM messages WHERE native_id = ? AND chat_native_id NOT LIKE 'channel:%' LIMIT 1",
		strconv.Itoa(id)).Scan(&chat)
	if err != nil {
		return ""
	}
	return chat
}

// outgoingUpTo devuelve los ids de mis mensajes del chat hasta maxID que aún no figuran como leídos.
func (l *localState) outgoingUpTo(ctx context.Context, chat string, maxID int) []string {
	rows, err := l.db.QueryContext(ctx,
		"SELECT native_id FROM messages WHERE chat_native_id = ? AND from_me = 1 "+
			"AND CAST(native_id AS INTEGER) <= ? AND delivery NOT IN ('read', 'played') "+
			"ORDER BY CAST(native_id AS INTEGER) DESC LIMIT 500", chat, maxID)
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

// putPoll guarda las opciones de una encuesta (para votar y acumular recuentos).
func (l *localState) putPoll(ctx context.Context, chat string, msgID int, pollID int64, rec pollRecord) {
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	kv := map[string][]byte{prefPoll + chat + "." + strconv.Itoa(msgID): b}
	if pollID != 0 {
		kv[prefPollID+strconv.FormatInt(pollID, 10)] = []byte(chat + "|" + strconv.Itoa(msgID))
	}
	_ = l.kv.setMany(ctx, kv)
}

func (l *localState) getPoll(ctx context.Context, chat string, msgID int) (pollRecord, bool) {
	v, ok, err := l.kv.get(ctx, prefPoll+chat+"."+strconv.Itoa(msgID))
	if err != nil || !ok {
		return pollRecord{}, false
	}
	var rec pollRecord
	if json.Unmarshal(v, &rec) != nil {
		return pollRecord{}, false
	}
	return rec, true
}

// pollRef devuelve el chat y el mensaje de una encuesta por su id de Telegram.
func (l *localState) pollRef(ctx context.Context, pollID int64) (chat string, msgID int, ok bool) {
	v, found, err := l.kv.get(ctx, prefPollID+strconv.FormatInt(pollID, 10))
	if err != nil || !found {
		return "", 0, false
	}
	i := strings.LastIndexByte(string(v), '|')
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(string(v[i+1:]))
	if err != nil {
		return "", 0, false
	}
	return string(v[:i]), n, true
}
