package meta

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"

	"dconnector/internal/core/model"
)

// Convertidor de las tablas Lightspeed (LS…) de Messenger al modelo único (09 §2).
// Todo lo de este archivo es puro: no toca la red ni la base. Lo que necesita saber de lo ya guardado
// (el chat de un mensaje editado, mis mensajes sin leer) se lo da el adaptador por Lookups.

// Carpetas de Messenger que se muestran como chats (el resto, p. ej. spam, no).
const (
	folderSpam                  = "spam"
	folderArchived              = "archived"
	folderE2EECutoverArchived   = "e2ee_cutover_archived"
	thumbsUpSmallStickerID      = 369239263222822
	thumbsUpMediumStickerID     = 369239343222814
	thumbsUpLargeStickerID      = 369239383222810
	maxExtraAttachmentsPerEvent = 10
)

// Lookups: consultas a lo ya guardado (las pone el adaptador; pueden ser nil en pruebas).
type Lookups struct {
	ChatOfMessage func(ctx context.Context, msgID string) string
	OutgoingUpTo  func(ctx context.Context, chat string, upToMs int64) []string
}

// threadState es lo que se sabe de un hilo (se acumula entre tablas).
type threadState struct {
	key        int64
	typ        table.ThreadType
	kind       model.ChatKind
	folder     string
	title      string
	lastTS     int64
	preview    string
	lastFromMe bool
	unread     int
	muted      int64
	archived   bool
	readOnly   bool
	hidden     bool // chats cifrados cuando no se admiten
	watermark  int64
	known      bool // ya llegó su información completa
}

// Converter traduce tablas LS… a eventos del modelo. Guarda el estado mínimo de hilos y nombres.
type Converter struct {
	acc  model.AccountID
	ch   model.Channel
	self int64
	// ShowEncrypted: mostrar los hilos cifrados de extremo a extremo (solo si el adaptador los sabe leer).
	ShowEncrypted bool
	Lookups       Lookups
	Now           func() time.Time

	mu          sync.Mutex
	threads     map[int64]*threadState
	names       map[int64]string
	avatars     map[string]string
	avatarFiles map[string]string
	hybrid      map[int64]int64 // hilo de Facebook → hilo cifrado (JID) que lo reemplaza
}

// NewConverter crea el convertidor de una cuenta.
func NewConverter(acc model.AccountID, ch model.Channel, self int64) *Converter {
	return &Converter{
		acc: acc, ch: ch, self: self,
		threads: map[int64]*threadState{}, names: map[int64]string{}, avatars: map[string]string{},
	}
}

// SetSelf fija el id propio.
func (c *Converter) SetSelf(id int64) {
	c.mu.Lock()
	c.self = id
	c.mu.Unlock()
}

func (c *Converter) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Result es lo que sale de traducir una tabla, en el orden en que debe entregarse.
type Result struct {
	Persons   []model.Person
	Chats     []model.Chat
	Messages  []model.Message
	Updates   []model.MessageUpdate
	Reactions []model.ReactionEvent
	Receipts  []model.ReceiptEvent
	Typing    []model.TypingEvent
	Removed   []model.ChatKey
	// Avatars: id nativo (persona o chat) → URL remota de la foto (para FetchAvatar).
	Avatars map[string]string
	// Skipped: cuántos avisos no se pudieron traducir (p. ej. una edición de un mensaje que no se conoce).
	Skipped int
}

// Empty dice si la tabla no trajo nada que entregar.
func (r *Result) Empty() bool {
	return len(r.Persons)+len(r.Chats)+len(r.Messages)+len(r.Updates)+len(r.Reactions)+
		len(r.Receipts)+len(r.Typing)+len(r.Removed) == 0
}

// Events convierte el resultado en eventos de cuenta viva, en orden: personas, chats, mensajes,
// cambios, reacciones, recibos, «escribiendo» y chats borrados.
func (r *Result) Events() []model.Event {
	var evs []model.Event
	for i := range r.Persons {
		p := r.Persons[i]
		evs = append(evs, model.Event{Type: model.EvPerson, Person: &p})
	}
	for i := range r.Chats {
		ch := r.Chats[i]
		evs = append(evs, model.Event{Type: model.EvChatUpsert, Chat: &ch, ChatKey: ch.Key})
	}
	for i := range r.Messages {
		m := r.Messages[i]
		evs = append(evs, model.Event{Type: model.EvMessageNew, At: m.Timestamp, Message: &m, ChatKey: m.ChatKey})
	}
	for i := range r.Updates {
		u := r.Updates[i]
		evs = append(evs, model.Event{Type: model.EvMessageUpdate, Update: &u, ChatKey: u.Key.Chat()})
	}
	for i := range r.Reactions {
		re := r.Reactions[i]
		evs = append(evs, model.Event{Type: model.EvReaction, Reaction: &re, ChatKey: re.MessageKey.Chat()})
	}
	for i := range r.Receipts {
		rc := r.Receipts[i]
		evs = append(evs, model.Event{Type: model.EvReceipt, Receipt: &rc, ChatKey: rc.ChatKey})
	}
	for i := range r.Typing {
		t := r.Typing[i]
		evs = append(evs, model.Event{Type: model.EvTyping, Typing: &t, ChatKey: t.ChatKey})
	}
	for _, k := range r.Removed {
		evs = append(evs, model.Event{Type: model.EvChatRemoved, ChatKey: k})
	}
	return evs
}

// History convierte el resultado en un lote de historial (history.batch).
func (r *Result) History(progress float64) model.HistoryBatch {
	return model.HistoryBatch{Chats: r.Chats, Messages: r.Messages, Persons: r.Persons, Progress: progress}
}

// ---------------------------------------------------------------------------------------------

// Convert traduce una tabla. Es seguro llamarlo desde varias goroutines (se serializa).
func (c *Converter) Convert(ctx context.Context, tbl *table.LSTable) *Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := &Result{Avatars: map[string]string{}}
	if tbl == nil {
		return r
	}
	dirty := map[int64]bool{} // hilos cuyo chat hay que anunciar

	c.hybridThreads(tbl, r)
	c.persons(tbl, r, dirty)
	c.threadsInfo(tbl, dirty)
	others := othersRead(tbl, c.self)

	// Hilos que aparecen en esta tabla: un borrado simultáneo es en realidad «borrar e insertar».
	active := map[int64]bool{}
	for _, t := range tbl.LSVerifyThreadExists {
		active[t.ThreadKey] = true
	}
	for _, t := range tbl.LSUpdateOrInsertThread {
		active[t.ThreadKey] = true
	}
	for _, t := range tbl.LSDeleteThenInsertThread {
		active[t.ThreadKey] = true
	}
	for _, t := range tbl.LSInsertNewMessageRange {
		active[t.ThreadKey] = true
	}

	c.messages(ctx, tbl, r, dirty, others)
	c.edits(ctx, tbl, r)
	c.deletes(ctx, tbl, r)
	c.reactions(ctx, tbl, r)
	c.readState(ctx, tbl, r, dirty)
	c.typing(tbl, r)

	// Borrados de hilos (después de todo lo demás: un hilo borrado cancela lo que le siguiera).
	for _, d := range tbl.LSDeleteThread {
		c.removeThread(d.ThreadKey, active, r, dirty)
	}
	for _, d := range tbl.LSDeletePartialThread {
		c.removeThread(d.ThreadKey, active, r, dirty)
	}
	for _, d := range tbl.LSDeleteMessageRequest {
		c.removeThread(d.ThreadKey, active, r, dirty)
	}

	// Chats a anunciar, en orden estable (por clave).
	keys := make([]int64, 0, len(dirty))
	for k, ok := range dirty {
		if ok {
			keys = append(keys, k)
		}
	}
	sortInt64(keys)
	for _, k := range keys {
		if st := c.threads[k]; st != nil && !st.hidden {
			r.Chats = append(r.Chats, c.chatOf(st))
			st.unread = -1 // el valor real se entrega una sola vez; después el almacén conserva el suyo
		}
	}
	for id, u := range c.avatars {
		r.Avatars[id] = u
	}
	return r
}

func sortInt64(a []int64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func (c *Converter) removeThread(key int64, active map[int64]bool, r *Result, dirty map[int64]bool) {
	if active[key] {
		return
	}
	delete(dirty, key)
	delete(c.threads, key)
	id := NativeUser(key)
	r.Removed = append(r.Removed, model.NewChatKey(c.acc, id))
}

// ---------------------------------------------------------------------------------------------
// Personas

func (c *Converter) persons(tbl *table.LSTable, r *Result, dirty map[int64]bool) {
	add := func(id int64, name, username, avatar string, phone int64) {
		if id == 0 {
			return
		}
		p := model.Person{AccountID: c.acc, NativeID: NativeUser(id), PushName: name, Username: username, AvatarURL: c.avatarPath(NativeUser(id))}
		if phone > 0 {
			p.Phone = "+" + strconv.FormatInt(phone, 10)
		}
		if name != "" {
			if c.names[id] != name {
				c.names[id] = name
				if st := c.threads[id]; st != nil && st.kind != model.ChatGroup && st.title == "" {
					dirty[id] = true
				}
			}
		}
		if avatar != "" {
			c.avatars[p.NativeID] = avatar
		}
		r.Persons = append(r.Persons, p)
	}
	for _, ct := range tbl.LSDeleteThenInsertContact {
		add(ct.Id, ct.Name, ct.Username, ct.GetAvatarURL(), ct.PhoneNumber)
	}
	for _, ct := range tbl.LSVerifyContactRowExists {
		add(ct.ContactId, ct.Name, ct.SecondaryName, ct.ProfilePictureUrl, 0)
	}
}

// ---------------------------------------------------------------------------------------------
// Hilos

func isEncryptedType(t table.ThreadType) bool {
	return t == table.ENCRYPTED_OVER_WA_ONE_TO_ONE || t == table.ENCRYPTED_OVER_WA_GROUP
}

func skippableType(t table.ThreadType) bool {
	switch t {
	case table.MONTAGE, table.FOLDER, table.COMMUNITY_FOLDER, table.COMMUNITY_CHANNEL_CATEGORY:
		return true
	}
	return false
}

func (c *Converter) state(key int64) *threadState {
	st := c.threads[key]
	if st == nil {
		st = &threadState{key: key, kind: model.ChatDirect, unread: -1}
		c.threads[key] = st
	}
	return st
}

func (c *Converter) kindOf(key int64, t table.ThreadType) model.ChatKind {
	switch {
	case t.IsOneToOne() || t == table.UNKNOWN_THREAD_TYPE:
		if key == c.self && c.self != 0 {
			return model.ChatSelf
		}
		return model.ChatDirect
	default:
		return model.ChatGroup
	}
}

// reason: CannotReplyReason llega como cualquier tipo; un valor distinto de vacío/0 deja el chat de solo lectura.
func cannotReply(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != "" && t != "0"
	case float64:
		return t != 0
	case int64:
		return t != 0
	case bool:
		return t
	}
	return false
}

type threadInfoIn struct {
	key        int64
	typ        table.ThreadType
	folder     string
	name       string
	picture    string
	lastTS     int64
	watermark  int64
	snippet    string
	snippetBy  int64
	mute       int64
	cannotRep  any
	unreadMsgs int64
	hasUnread  bool
}

func (c *Converter) applyThread(in threadInfoIn, dirty map[int64]bool) {
	if in.key == 0 || skippableType(in.typ) {
		return
	}
	st := c.state(in.key)
	if in.typ != table.UNKNOWN_THREAD_TYPE {
		st.typ = in.typ
	}
	st.kind = c.kindOf(in.key, st.typ)
	st.folder = in.folder
	st.known = true
	st.hidden = (isEncryptedType(st.typ) && !c.ShowEncrypted) || in.folder == folderSpam
	st.archived = in.folder == folderArchived || in.folder == folderE2EECutoverArchived
	if in.name != "" {
		st.title = in.name
	}
	if in.picture != "" {
		c.avatars[NativeUser(in.key)] = in.picture
	}
	if in.lastTS > 0 {
		st.lastTS = in.lastTS
	}
	st.watermark = in.watermark
	if in.snippet != "" {
		st.preview = in.snippet
	}
	st.lastFromMe = in.snippetBy != 0 && in.snippetBy == c.self
	st.readOnly = cannotReply(in.cannotRep)
	st.muted = muteValue(in.mute, c.now())
	switch {
	case in.lastTS > 0 && in.watermark >= in.lastTS:
		st.unread = 0 // leído hasta la última actividad
	case in.hasUnread && in.unreadMsgs > 0:
		st.unread = int(in.unreadMsgs)
	default:
		st.unread = -1
	}
	dirty[in.key] = true
}

// muteValue: 0 = no silenciado; -1 = siempre; si el silencio ya venció, 0.
func muteValue(ms int64, now time.Time) int64 {
	switch {
	case ms < 0:
		return -1
	case ms == 0:
		return 0
	case ms <= now.UnixMilli():
		return 0
	case ms > now.Add(10*365*24*time.Hour).UnixMilli():
		return -1
	}
	return ms
}

func (c *Converter) threadsInfo(tbl *table.LSTable, dirty map[int64]bool) {
	for _, t := range tbl.LSDeleteThenInsertThread {
		c.applyThread(threadInfoIn{
			key: t.ThreadKey, typ: t.ThreadType, folder: t.FolderName, name: t.ThreadName, picture: t.ThreadPictureUrl,
			lastTS: t.LastActivityTimestampMs, watermark: t.LastReadWatermarkTimestampMs, snippet: t.Snippet,
			snippetBy: t.SnippetSenderContactId, mute: t.MuteExpireTimeMs, cannotRep: t.CannotReplyReason,
			unreadMsgs: t.UnreadMessageCount, hasUnread: true,
		}, dirty)
	}
	for _, t := range tbl.LSUpdateOrInsertThread {
		c.applyThread(threadInfoIn{
			key: t.ThreadKey, typ: t.ThreadType, folder: t.FolderName, name: t.ThreadName, picture: t.ThreadPictureUrl,
			lastTS: t.LastActivityTimestampMs, watermark: t.LastReadWatermarkTimestampMs, snippet: t.Snippet,
			snippetBy: t.SnippetSenderContactId, mute: t.MuteExpireTimeMs, cannotRep: t.CannotReplyReason,
		}, dirty)
	}
	for _, t := range tbl.LSVerifyThreadExists {
		if skippableType(t.ThreadType) {
			continue
		}
		st := c.state(t.ThreadKey)
		if t.ThreadType != table.UNKNOWN_THREAD_TYPE {
			st.typ = t.ThreadType
			st.kind = c.kindOf(t.ThreadKey, t.ThreadType)
		}
		if t.FolderName != "" {
			st.folder = t.FolderName
			st.hidden = (isEncryptedType(st.typ) && !c.ShowEncrypted) || t.FolderName == folderSpam
		}
		if !st.known {
			dirty[t.ThreadKey] = true
		}
	}
	for _, t := range tbl.LSSyncUpdateThreadName {
		if st := c.threads[t.ThreadKey]; st != nil && t.ThreadName != "" && st.kind == model.ChatGroup {
			st.title = t.ThreadName
			dirty[t.ThreadKey] = true
		}
	}
	for _, t := range tbl.LSSetThreadImageURL {
		if t.ImageURL != "" {
			c.avatars[NativeUser(t.ThreadKey)] = t.ImageURL
		}
	}
	for _, t := range tbl.LSUpdateThreadMuteSetting {
		if st := c.threads[t.ThreadKey]; st != nil {
			st.muted = muteValue(t.MuteExpireTimeMS, c.now())
			dirty[t.ThreadKey] = true
		}
	}
	for _, t := range tbl.LSUpdateThreadSnippet {
		if st := c.threads[t.ThreadKey]; st != nil && t.Snippet != "" {
			st.preview = t.Snippet
			st.lastFromMe = t.SnippetSenderContactId != 0 && t.SnippetSenderContactId == c.self
			dirty[t.ThreadKey] = true
		}
	}
	for _, t := range tbl.LSMoveThreadToE2EECutoverFolder {
		if st := c.threads[t.ThreadKey]; st != nil {
			if st.typ == table.ONE_TO_ONE {
				st.typ = table.ENCRYPTED_OVER_WA_ONE_TO_ONE
			}
			if isEncryptedType(st.typ) && !c.ShowEncrypted {
				st.hidden = true
			}
		}
	}
}

// othersRead devuelve, por hilo, la marca de lectura más alta de OTRAS personas que trae la tabla
// (sirve para marcar como leídos mis mensajes del historial).
func othersRead(tbl *table.LSTable, self int64) map[int64]int64 {
	out := map[int64]int64{}
	for _, p := range tbl.LSAddParticipantIdToGroupThread {
		if p.ContactId != 0 && p.ContactId != self && p.ReadWatermarkTimestampMs > out[p.ThreadKey] {
			out[p.ThreadKey] = p.ReadWatermarkTimestampMs
		}
	}
	for _, p := range tbl.LSUpdateReadReceipt {
		if p.ContactId != 0 && p.ContactId != self && p.ReadWatermarkTimestampMs > out[p.ThreadKey] {
			out[p.ThreadKey] = p.ReadWatermarkTimestampMs
		}
	}
	return out
}

func (c *Converter) chatOf(st *threadState) model.Chat {
	id := NativeUser(st.key)
	title := st.title
	if title == "" && st.kind != model.ChatGroup {
		title = c.names[st.key]
	}
	ch := model.Chat{
		Key: model.NewChatKey(c.acc, id), AccountID: c.acc, Channel: c.ch, NativeID: id, Kind: st.kind,
		Title: title, UnreadCount: st.unread, LastMessageAt: st.lastTS, LastPreview: st.preview,
		LastFromMe: st.lastFromMe, Archived: st.archived, MutedUntil: st.muted, ReadOnly: st.readOnly,
	}
	ch.AvatarURL = c.avatarPath(id)
	if st.kind == model.ChatSelf {
		ch.Title = "Tú"
	}
	if st.kind == model.ChatDirect {
		ch.PeerNativeID = id
	}
	return ch
}

// ThreadKnown dice si se conoce el hilo y si es un grupo.
func (c *Converter) ThreadKnown(key int64) (known, group bool, typ table.ThreadType) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.threads[key]
	if st == nil {
		return false, false, table.UNKNOWN_THREAD_TYPE
	}
	return true, st.kind == model.ChatGroup, st.typ
}

// Name devuelve el nombre conocido de una persona ("" si no se conoce).
func (c *Converter) Name(id int64) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.names[id]
}

// AvatarURL devuelve la URL remota conocida de la foto de una persona o de un grupo.
func (c *Converter) AvatarURL(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.avatars[id]
}

// ChatSnapshot devuelve el chat completo tal como se conoce ahora (false si no se conoce).
func (c *Converter) ChatSnapshot(key int64) (model.Chat, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.threads[key]
	if st == nil || st.hidden {
		return model.Chat{}, false
	}
	return c.chatOf(st), true
}

// ---------------------------------------------------------------------------------------------
// Mensajes

type joined struct {
	msg   *table.LSInsertMessage
	blobs []*table.LSInsertBlobAttachment
	atts  []*table.LSInsertAttachment
	xmas  []*table.LSInsertXmaAttachment
	stks  []*table.LSInsertStickerAttachment
}

// attachment es un adjunto ya normalizado.
type attachment struct {
	kind    model.ContentKind
	mime    string
	name    string
	size    int64
	w, h    int
	dur     float64
	url     string
	expires int64
	fbid    string
	ephem   bool
}

func (c *Converter) messages(ctx context.Context, tbl *table.LSTable, r *Result, dirty map[int64]bool, others map[int64]int64) {
	byID := map[string]*joined{}
	var order []*joined
	add := func(m *table.LSInsertMessage) {
		if m == nil || m.MessageId == "" || byID[m.MessageId] != nil {
			return
		}
		j := &joined{msg: m}
		byID[m.MessageId] = j
		order = append(order, j)
	}
	for _, m := range tbl.LSInsertMessage {
		add(m)
	}
	for _, m := range tbl.LSUpsertMessage {
		add(m.ToInsert())
	}
	for _, a := range tbl.LSInsertBlobAttachment {
		if j := byID[a.MessageId]; j != nil {
			j.blobs = append(j.blobs, a)
		}
	}
	for _, a := range tbl.LSInsertAttachment {
		if j := byID[a.MessageId]; j != nil {
			j.atts = append(j.atts, a)
		}
	}
	for _, a := range tbl.LSInsertXmaAttachment {
		if j := byID[a.MessageId]; j != nil {
			j.xmas = append(j.xmas, a)
		}
	}
	for _, a := range tbl.LSInsertStickerAttachment {
		if j := byID[a.MessageId]; j != nil {
			j.stks = append(j.stks, a)
		}
	}
	// Reacciones de la misma tabla, para el historial.
	reacts := map[string][]model.Reaction{}
	for _, re := range tbl.LSUpsertReaction {
		if re.Reaction == "" {
			continue
		}
		reacts[re.MessageId] = append(reacts[re.MessageId], model.Reaction{
			SenderNativeID: NativeUser(re.ActorId), SenderName: c.names[re.ActorId], Emoji: re.Reaction, At: re.TimestampMs,
		})
	}
	for _, j := range order {
		if st := c.threads[j.msg.ThreadKey]; st != nil && st.hidden {
			continue
		}
		if skippableType(c.typeOf(j.msg.ThreadKey)) {
			continue
		}
		msgs := c.convertMessage(j)
		for i := range msgs {
			if i == 0 {
				msgs[i].Reactions = reacts[j.msg.MessageId]
			}
			if msgs[i].FromMe && others[j.msg.ThreadKey] >= msgs[i].Timestamp {
				msgs[i].Delivery = model.DeliveryRead
			}
			r.Messages = append(r.Messages, msgs[i])
		}
		// El hilo existe aunque no se haya visto su ficha: anunciarlo con lo poco que se sabe.
		if st := c.threads[j.msg.ThreadKey]; st == nil && len(msgs) > 0 {
			st = c.state(j.msg.ThreadKey)
			st.kind = c.kindOf(j.msg.ThreadKey, table.UNKNOWN_THREAD_TYPE)
			dirty[j.msg.ThreadKey] = true
		}
	}
}

func (c *Converter) typeOf(key int64) table.ThreadType {
	if st := c.threads[key]; st != nil {
		return st.typ
	}
	return table.UNKNOWN_THREAD_TYPE
}

// mentionIDs lee la lista «id,id,…» de menciones.
func mentionIDs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Converter) convertMessage(j *joined) []model.Message {
	m := j.msg
	if m.IsUnsent {
		return nil // un mensaje borrado para todos no se muestra; el aviso en vivo llega por LSDeleteThenInsertMessage
	}
	chat := NativeUser(m.ThreadKey)
	base := model.Message{
		AccountID: c.acc, Channel: c.ch, ChatKey: model.NewChatKey(c.acc, chat),
		NativeID: m.MessageId, ChatNativeID: chat,
		SenderNativeID: NativeUser(m.SenderId), SenderName: c.names[m.SenderId],
		FromMe:    m.SenderId == c.self && c.self != 0,
		Timestamp: m.TimestampMs, Delivery: model.DeliverySent,
		Forwarded: m.IsForwarded, Mentions: mentionIDs(m.MentionIds),
	}
	if base.FromMe && base.SenderName == "" {
		base.SenderName = "Tú"
	}
	base.Key = model.NewMessageKey(c.acc, chat, m.MessageId)
	if m.ReplySourceId != "" {
		base.QuotedNativeID = m.ReplySourceId
		base.QuotedPreview = firstNonEmpty(m.ReplyMessageText, m.ReplySnippet)
		if m.ReplyToUserId != 0 {
			base.QuotedSender = firstNonEmpty(c.names[m.ReplyToUserId], NativeUser(m.ReplyToUserId))
		}
	}
	text := m.Text

	// La mano arriba de Messenger llega como un sticker: se muestra como el emoji.
	if (m.StickerId == thumbsUpSmallStickerID || m.StickerId == thumbsUpMediumStickerID || m.StickerId == thumbsUpLargeStickerID) && len(j.stks) == 1 {
		base.Kind, base.Text = model.KindText, "👍"
		return []model.Message{base}
	}
	if m.IsAdminMessage {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		base.Kind, base.Text = model.KindSystem, text
		return []model.Message{base}
	}

	var atts []attachment
	for _, a := range j.blobs {
		atts = append(atts, blobAttachment(a))
	}
	for _, a := range j.atts {
		atts = append(atts, legacyAttachment(a))
	}
	for _, a := range j.stks {
		atts = append(atts, stickerAttachment(a))
	}
	// XMA (enlaces, compartidos, encuestas…): se muestra su título y su URL como texto.
	var extra []string
	unsupportedXMA := false
	for _, x := range j.xmas {
		line := xmaText(x, text)
		if line == "" {
			unsupportedXMA = true
			continue
		}
		extra = append(extra, line)
	}
	if len(extra) > 0 {
		if strings.TrimSpace(text) == "" {
			text = strings.Join(extra, "\n")
		} else {
			text = text + "\n" + strings.Join(extra, "\n")
		}
	}

	if len(atts) > maxExtraAttachmentsPerEvent+1 {
		atts = atts[:maxExtraAttachmentsPerEvent+1]
	}
	if len(atts) == 0 {
		switch {
		case strings.TrimSpace(text) != "":
			base.Kind, base.Text = model.KindText, text
		case unsupportedXMA, m.StickerId != 0, len(j.xmas) > 0:
			base.Kind = model.KindUnsupported
		default:
			return nil // mensaje vacío (señales internas)
		}
		return []model.Message{base}
	}
	var out []model.Message
	for i, a := range atts {
		mm := base
		if i > 0 {
			mm.NativeID = fmt.Sprintf("%s#%d", m.MessageId, i+1)
			mm.Key = model.NewMessageKey(c.acc, chat, mm.NativeID)
			mm.Reactions, mm.QuotedNativeID, mm.QuotedPreview, mm.QuotedSender, mm.Forwarded = nil, "", "", "", base.Forwarded
		}
		if a.ephem || a.url == "" {
			mm.Kind = model.KindUnsupported
			if i == 0 {
				mm.Text = text
			}
			out = append(out, mm)
			continue
		}
		mm.Kind = a.kind
		if i == 0 {
			mm.Text = text
		}
		med := &model.Media{
			ID: MediaID(chat, mm.NativeID, 0), Mime: a.mime, FileName: a.name, Size: a.size,
			Width: a.w, Height: a.h, Duration: a.dur,
			Handle: MakeHandle(Handle{URL: a.url, Expires: a.expires, Mime: a.mime}),
		}
		if med.FileName == "" {
			med.FileName = FileNameFor(a.kind, a.mime)
		}
		mm.Media = med
		out = append(out, mm)
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func xmaText(x *table.LSInsertXmaAttachment, current string) string {
	var parts []string
	title := strings.TrimSpace(firstNonEmpty(x.TitleText, x.HeaderTitle))
	if title != "" && !strings.Contains(current, title) {
		parts = append(parts, title)
	}
	if u := strings.TrimSpace(x.ActionUrl); u != "" && strings.HasPrefix(u, "http") && !strings.Contains(current, u) {
		parts = append(parts, u)
	}
	return strings.Join(parts, "\n")
}

func kindOfAttachment(t table.AttachmentType, mimeType, name string, waveform bool) (model.ContentKind, bool) {
	switch t {
	case table.AttachmentTypeSticker, table.AttachmentTypeSelfieSticker, table.AttachmentTypeThirdPartySticker:
		return model.KindSticker, false
	case table.AttachmentTypeImage, table.AttachmentTypeAnimatedImage:
		if strings.HasPrefix(mimeType, "video/") {
			return model.KindVideo, false
		}
		return model.KindImage, false
	case table.AttachmentTypeVideo:
		return model.KindVideo, false
	case table.AttachmentTypeAudio, table.AttachmentTypeSoundBite:
		if waveform || strings.HasPrefix(strings.ToLower(name), "audioclip") {
			return model.KindVoice, false
		}
		return model.KindAudio, false
	case table.AttachmentTypeFile:
		return model.KindDocument, false
	case table.AttachmentTypeEphemeralImage, table.AttachmentTypeEphemeralVideo:
		return model.KindUnsupported, true // «ver una vez»
	}
	if mimeType == "" {
		mimeType = MimeFromName(name)
	}
	return KindFromMime(mimeType), false
}

func blobAttachment(a *table.LSInsertBlobAttachment) attachment {
	url, mimeType, exp := a.PlayableUrl, a.PlayableUrlMimeType, a.PlayableUrlExpirationTimestampMs
	var w, h int64
	if mimeType == "" {
		mimeType = a.AttachmentMimeType
	}
	if url == "" {
		url, mimeType, exp = a.PreviewUrl, a.PreviewUrlMimeType, a.PreviewUrlExpirationTimestampMs
		w, h = a.PreviewWidth, a.PreviewHeight
		if mimeType == "" {
			mimeType = a.AttachmentMimeType
		}
	}
	kind, ephem := kindOfAttachment(a.AttachmentType, mimeType, a.Filename, a.WaveformData != "")
	return attachment{kind: kind, mime: mimeType, name: a.Filename, size: a.Filesize, w: int(w), h: int(h),
		dur: float64(a.PlayableDurationMs) / 1000, url: url, expires: exp, fbid: a.AttachmentFbid, ephem: ephem}
}

func legacyAttachment(a *table.LSInsertAttachment) attachment {
	url, mimeType, exp := a.PlayableUrl, a.PlayableUrlMimeType, a.PlayableUrlExpirationTimestampMs
	var w, h int64
	if mimeType == "" {
		mimeType = a.AttachmentMimeType
	}
	if url == "" {
		url, mimeType, exp = a.PreviewUrl, a.PreviewUrlMimeType, a.PreviewUrlExpirationTimestampMs
		w, h = a.PreviewWidth, a.PreviewHeight
		if mimeType == "" {
			mimeType = a.AttachmentMimeType
		}
	}
	if url == "" {
		url, mimeType, exp = a.ImageUrl, a.ImageUrlMimeType, a.ImageUrlExpirationTimestampMs
		w, h = a.PreviewWidth, a.PreviewHeight
	}
	kind, ephem := kindOfAttachment(a.AttachmentType, mimeType, a.Filename, false)
	return attachment{kind: kind, mime: mimeType, name: a.Filename, size: a.Filesize, w: int(w), h: int(h),
		dur: float64(a.PlayableDurationMs) / 1000, url: url, expires: exp, fbid: a.AttachmentFbid, ephem: ephem}
}

func stickerAttachment(a *table.LSInsertStickerAttachment) attachment {
	url, mimeType, exp := a.PlayableUrl, a.PlayableUrlMimeType, a.PlayableUrlExpirationTimestampMs
	if url == "" {
		url, mimeType, exp = a.PreviewUrl, a.PreviewUrlMimeType, a.PreviewUrlExpirationTimestampMs
	}
	if url == "" {
		url, mimeType, exp = a.ImageUrl, a.ImageUrlMimeType, a.ImageUrlExpirationTimestampMs
	}
	return attachment{kind: model.KindSticker, mime: mimeType, w: int(a.PreviewWidth), h: int(a.PreviewHeight),
		url: url, expires: exp, fbid: a.AttachmentFbid}
}

// ---------------------------------------------------------------------------------------------
// Ediciones, borrados, reacciones

func (c *Converter) chatOfMessage(ctx context.Context, msgID string) string {
	if c.Lookups.ChatOfMessage == nil {
		return ""
	}
	return c.Lookups.ChatOfMessage(ctx, msgID)
}

func (c *Converter) edits(ctx context.Context, tbl *table.LSTable, r *Result) {
	for _, e := range tbl.LSEditMessage {
		chat := c.chatOfMessage(ctx, e.MessageID)
		if chat == "" {
			r.Skipped++
			continue
		}
		text := e.Text
		r.Updates = append(r.Updates, model.MessageUpdate{
			Key: model.NewMessageKey(c.acc, chat, e.MessageID), Text: &text, EditedAt: c.now().UnixMilli(),
		})
	}
}

func (c *Converter) deletes(ctx context.Context, tbl *table.LSTable, r *Result) {
	for _, d := range tbl.LSDeleteMessage {
		r.Updates = append(r.Updates, model.MessageUpdate{
			Key: model.NewMessageKey(c.acc, NativeUser(d.ThreadKey), d.MessageId), Deleted: true,
		})
	}
	for _, d := range tbl.LSDeleteThenInsertMessage {
		if !d.IsUnsent {
			continue // un «borrar e insertar» normal no es un borrado para todos
		}
		r.Updates = append(r.Updates, model.MessageUpdate{
			Key: model.NewMessageKey(c.acc, NativeUser(d.ThreadKey), d.MessageId), Deleted: true,
		})
	}
}

func (c *Converter) reactions(ctx context.Context, tbl *table.LSTable, r *Result) {
	for _, re := range tbl.LSUpsertReaction {
		r.Reactions = append(r.Reactions, model.ReactionEvent{
			MessageKey: model.NewMessageKey(c.acc, NativeUser(re.ThreadKey), re.MessageId),
			Reaction: model.Reaction{SenderNativeID: NativeUser(re.ActorId), SenderName: c.names[re.ActorId],
				Emoji: re.Reaction, At: re.TimestampMs},
		})
	}
	for _, re := range tbl.LSDeleteReaction {
		r.Reactions = append(r.Reactions, model.ReactionEvent{
			MessageKey: model.NewMessageKey(c.acc, NativeUser(re.ThreadKey), re.MessageId),
			Reaction:   model.Reaction{SenderNativeID: NativeUser(re.ActorId), SenderName: c.names[re.ActorId], Emoji: "", At: c.now().UnixMilli()},
		})
	}
}

// ---------------------------------------------------------------------------------------------
// Leído y «escribiendo»

func (c *Converter) readState(ctx context.Context, tbl *table.LSTable, r *Result, dirty map[int64]bool) {
	// Yo leí desde otro dispositivo.
	markSelfRead := func(key, watermark int64) {
		st := c.threads[key]
		if st == nil {
			return
		}
		if watermark > st.watermark {
			st.watermark = watermark
		}
		if st.lastTS == 0 || watermark >= st.lastTS {
			st.unread = 0
			dirty[key] = true
		}
	}
	for _, m := range tbl.LSMarkThreadRead {
		markSelfRead(m.ThreadKey, m.LastReadWatermarkTimestampMs)
	}
	for _, m := range tbl.LSMarkThreadReadV2 {
		markSelfRead(m.ThreadKey, m.LastReadWatermarkTimestampMs)
	}
	// Otra persona leyó mis mensajes.
	for _, rr := range tbl.LSUpdateReadReceipt {
		if rr.ContactId == c.self && c.self != 0 {
			markSelfRead(rr.ThreadKey, rr.ReadWatermarkTimestampMs)
			continue
		}
		if c.Lookups.OutgoingUpTo == nil || rr.ReadWatermarkTimestampMs <= 0 {
			continue
		}
		chat := NativeUser(rr.ThreadKey)
		ids := c.Lookups.OutgoingUpTo(ctx, chat, rr.ReadWatermarkTimestampMs)
		if len(ids) == 0 {
			continue
		}
		keys := make([]model.MessageKey, len(ids))
		for i, id := range ids {
			keys[i] = model.NewMessageKey(c.acc, chat, id)
		}
		at := rr.ReadActionTimestampMs
		if at <= 0 {
			at = c.now().UnixMilli()
		}
		r.Receipts = append(r.Receipts, model.ReceiptEvent{
			ChatKey: model.NewChatKey(c.acc, chat), MessageKeys: keys, Delivery: model.DeliveryRead,
			ReaderID: NativeUser(rr.ContactId), At: at,
		})
	}
}

func (c *Converter) typing(tbl *table.LSTable, r *Result) {
	for _, t := range tbl.LSUpdateTypingIndicator {
		if t.SenderId == c.self && c.self != 0 {
			continue
		}
		r.Typing = append(r.Typing, model.TypingEvent{
			ChatKey: model.NewChatKey(c.acc, NativeUser(t.ThreadKey)), PeerID: NativeUser(t.SenderId), Typing: t.IsTyping,
		})
	}
}

// ---------------------------------------------------------------------------------------------
// Fotos, chats conocidos y lotes de historial

// SetAvatarFile anota el archivo local de la foto de una persona o grupo (nombre dentro de AvatarDir).
func (c *Converter) SetAvatarFile(id, file string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.avatarFiles == nil {
		c.avatarFiles = map[string]string{}
	}
	c.avatarFiles[id] = file
}

func (c *Converter) avatarPath(id string) string {
	if f := c.avatarFiles[id]; f != "" {
		return "/avatar/" + string(c.acc) + "/" + f
	}
	return ""
}

// PersonSnapshot devuelve la persona tal como se conoce ahora (con su foto local si ya se bajó).
func (c *Converter) PersonSnapshot(id int64) model.Person {
	c.mu.Lock()
	defer c.mu.Unlock()
	nat := NativeUser(id)
	return model.Person{AccountID: c.acc, NativeID: nat, PushName: c.names[id], AvatarURL: c.avatarPath(nat)}
}

// AllChats devuelve todos los chats conocidos (para SyncChats), de más reciente a más antiguo.
func (c *Converter) AllChats() []model.Chat {
	c.mu.Lock()
	defer c.mu.Unlock()
	chats := make([]model.Chat, 0, len(c.threads))
	for _, st := range c.threads {
		if st.hidden || !st.known && st.lastTS == 0 && st.preview == "" {
			continue
		}
		chats = append(chats, c.chatOf(st))
	}
	for i := 1; i < len(chats); i++ {
		for j := i; j > 0 && chats[j-1].LastMessageAt < chats[j].LastMessageAt; j-- {
			chats[j-1], chats[j] = chats[j], chats[j-1]
		}
	}
	return chats
}

// HistoryBatches parte el resultado en lotes de historial de hasta size mensajes (progreso 0..1).
// Los chats y las personas van en el primer lote.
func (r *Result) HistoryBatches(size int) []model.HistoryBatch {
	if size <= 0 {
		size = 500
	}
	n := (len(r.Messages) + size - 1) / size
	if n <= 1 {
		return []model.HistoryBatch{r.History(1)}
	}
	out := make([]model.HistoryBatch, 0, n)
	for i := 0; i < n; i++ {
		lo, hi := i*size, (i+1)*size
		if hi > len(r.Messages) {
			hi = len(r.Messages)
		}
		b := model.HistoryBatch{Messages: r.Messages[lo:hi], Progress: float64(i+1) / float64(n)}
		if i == 0 {
			b.Chats, b.Persons = r.Chats, r.Persons
		}
		out = append(out, b)
	}
	return out
}
