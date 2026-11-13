package meta

import (
	"go.mau.fi/mautrix-meta/pkg/messagix/table"

	"dconnector/internal/core/model"
)

// Hilos cifrados (E2EE): Messenger los pasa de «hilo de Facebook» a «hilo cifrado» (JID de WhatsApp) y las
// tablas LS… siguen hablando del hilo de Facebook. El mapa hilo de Facebook → JID se arma con
// LSVerifyHybridThreadExists y LSUpdateThreadAuthorityAndMappingWithOTIDFromJID, y todo lo que llegue
// con la clave vieja se reescribe a la nueva para que cada chat tenga un solo id (el del JID).

func (c *Converter) mapKey(k int64) int64 {
	if to, ok := c.hybrid[k]; ok && to != 0 {
		return to
	}
	return k
}

// hybridThreads aprende el mapa y reescribe las claves de hilo de la tabla (se modifica en sitio).
func (c *Converter) hybridThreads(tbl *table.LSTable, r *Result) {
	if c.hybrid == nil {
		c.hybrid = map[int64]int64{}
	}
	for _, m := range tbl.LSUpdateThreadAuthorityAndMappingWithOTIDFromJID {
		if m.ThreadKey != 0 && m.ThreadJID != 0 {
			c.hybrid[m.ThreadKey] = m.ThreadJID
		}
	}
	for _, m := range tbl.LSVerifyHybridThreadExists {
		if m.ThreadKey == 0 || m.ThreadJID == 0 {
			continue
		}
		c.hybrid[m.ThreadKey] = m.ThreadJID
		st := c.state(m.ThreadJID)
		if m.ThreadType != table.UNKNOWN_THREAD_TYPE {
			st.typ = m.ThreadType
		}
		st.kind = c.kindOf(m.ThreadJID, st.typ)
		if m.IsGroupThread {
			st.kind = model.ChatGroup
		}
		st.hidden = isEncryptedType(st.typ) && !c.ShowEncrypted
		if m.ThreadKey != m.ThreadJID {
			// el hilo de Facebook que quedó atrás ya no es un chat
			if old := c.threads[m.ThreadKey]; old != nil {
				delete(c.threads, m.ThreadKey)
			}
			r.Removed = append(r.Removed, model.NewChatKey(c.acc, NativeUser(m.ThreadKey)))
		}
	}
	if len(c.hybrid) == 0 {
		return
	}
	for _, t := range tbl.LSDeleteThenInsertThread {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSUpdateOrInsertThread {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSVerifyThreadExists {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSSyncUpdateThreadName {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSSetThreadImageURL {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSUpdateThreadMuteSetting {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSUpdateThreadSnippet {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSMarkThreadRead {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSMarkThreadReadV2 {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSMoveThreadToE2EECutoverFolder {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSInsertMessage {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSUpsertMessage {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSUpsertReaction {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSDeleteReaction {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSDeleteMessage {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSDeleteThenInsertMessage {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSUpdateReadReceipt {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSUpdateTypingIndicator {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSDeleteThread {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSDeletePartialThread {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSDeleteMessageRequest {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSAddParticipantIdToGroupThread {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
	for _, t := range tbl.LSInsertNewMessageRange {
		t.ThreadKey = c.mapKey(t.ThreadKey)
	}
}

// EnsureThread registra un hilo que solo se conoce por haber recibido un mensaje (p. ej. un chat cifrado) y
// devuelve su chat para anunciarlo; known=true si ya se conocía (en ese caso el chat no hace falta).
func (c *Converter) EnsureThread(key int64, group bool, typ table.ThreadType) (chat model.Chat, known bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st := c.threads[key]; st != nil && st.known {
		return model.Chat{}, true
	}
	st := c.state(key)
	st.known = true
	if typ != table.UNKNOWN_THREAD_TYPE {
		st.typ = typ
	}
	st.kind = c.kindOf(key, st.typ)
	if group {
		st.kind = model.ChatGroup
	}
	return c.chatOf(st), false
}
