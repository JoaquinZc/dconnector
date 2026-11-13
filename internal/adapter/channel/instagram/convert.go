package instagram

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
)

// Traducción de los mensajes y avisos de Instagram (slidetypes) al modelo único (09 §2, adaptado).
//
// Decisión de la librería: messagix ya NO sirve Instagram (desde v26.08 mautrix-meta sacó Instagram de
// messagix); el cliente de Instagram es pkg/instameow, que habla otro protocolo (mailbox + deltas «slide»
// en vez de tablas LS…). Por eso el adaptador de Instagram traduce estas estructuras y no las de meta/convert.go.
//
// Ids: el chat es el id «thread_fbid» (ThreadInfo.ID, el mismo de los deltas); las personas son su id de mensajería
// (interop_messaging_user_fbid); el id largo del hilo (thread_igid) se guarda aparte para borrar y marcar leído.

// Constantes de tipos de adjunto de Instagram (los mismos números que table.AttachmentType).
const (
	attImage         = 2
	attAnimatedImage = 3
	attVideo         = 4
)

type threadState struct {
	id        string // thread_fbid (id nativo del chat)
	longID    string // thread_igid
	group     bool
	title     string
	lastTS    int64
	preview   string
	muted     int64
	pinned    bool
	archived  bool
	request   bool
	unread    int
	users     []int64
	known     bool
	watermark int64
}

// Converter guarda el estado mínimo y traduce.
type Converter struct {
	acc  model.AccountID
	self int64
	Now  func() time.Time

	mu          sync.Mutex
	threads     map[string]*threadState
	long2short  map[string]string
	names       map[int64]string
	handles     map[int64]string
	avatars     map[string]string
	avatarFiles map[string]string
	reactLog    map[string]reactRef
	reactOrder  []string
}

type reactRef struct {
	msg    string
	sender int64
	emoji  string
}

// NewConverter crea el traductor de una cuenta.
func NewConverter(acc model.AccountID, self int64) *Converter {
	return &Converter{
		acc: acc, self: self,
		threads: map[string]*threadState{}, long2short: map[string]string{},
		names: map[int64]string{}, handles: map[int64]string{}, avatars: map[string]string{},
		avatarFiles: map[string]string{}, reactLog: map[string]reactRef{},
	}
}

func (c *Converter) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// SetSelf fija el id propio (id de mensajería).
func (c *Converter) SetSelf(id int64) {
	c.mu.Lock()
	c.self = id
	c.mu.Unlock()
}

// Self devuelve el id propio.
func (c *Converter) Self() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.self
}

// SetAvatarFile anota el archivo local de la foto de una persona o grupo.
func (c *Converter) SetAvatarFile(id, file string) {
	c.mu.Lock()
	c.avatarFiles[id] = file
	c.mu.Unlock()
}

func (c *Converter) avatarPath(id string) string {
	if f := c.avatarFiles[id]; f != "" {
		return "/avatar/" + string(c.acc) + "/" + f
	}
	return ""
}

// AvatarURL devuelve la URL remota conocida de la foto de una persona o grupo.
func (c *Converter) AvatarURL(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.avatars[id]
}

// Name devuelve el nombre conocido de una persona.
func (c *Converter) Name(id int64) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.names[id]
}

// LongID devuelve el id largo (thread_igid) de un chat, si se conoce.
func (c *Converter) LongID(chat string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st := c.threads[chat]; st != nil {
		return st.longID
	}
	return ""
}

// ShortOfLong devuelve el chat de un id largo de hilo (para los avisos de «escribiendo»).
func (c *Converter) ShortOfLong(long string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.long2short[long]
}

// Known dice si se conoce el chat.
func (c *Converter) Known(chat string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.threads[chat]
	return st != nil && st.known
}

// Result: lo que sale de traducir un aviso, en el orden de entrega.
type Result struct {
	Persons   []model.Person
	Chats     []model.Chat
	Messages  []model.Message
	Updates   []model.MessageUpdate
	Reactions []model.ReactionEvent
	Receipts  []model.ReceiptEvent
	Removed   []model.ChatKey
	Avatars   map[string]string
}

// Empty dice si no hay nada que entregar.
func (r *Result) Empty() bool {
	return len(r.Persons)+len(r.Chats)+len(r.Messages)+len(r.Updates)+len(r.Reactions)+len(r.Receipts)+len(r.Removed) == 0
}

// Events convierte el resultado en eventos (personas, chats, mensajes, cambios, reacciones, recibos, borrados).
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
	for _, k := range r.Removed {
		evs = append(evs, model.Event{Type: model.EvChatRemoved, ChatKey: k})
	}
	return evs
}

// History convierte el resultado en un lote de historial.
func (r *Result) History(progress float64) model.HistoryBatch {
	return model.HistoryBatch{Chats: r.Chats, Messages: r.Messages, Persons: r.Persons, Progress: progress}
}

// ---------------------------------------------------------------------------------------------
// Hilos y personas

func (c *Converter) state(id string) *threadState {
	st := c.threads[id]
	if st == nil {
		st = &threadState{id: id, unread: -1}
		c.threads[id] = st
	}
	return st
}

func (c *Converter) person(u *slidetypes.User, r *Result) {
	if u == nil || u.InteropMessagingUserFBID == 0 {
		return
	}
	id := u.InteropMessagingUserFBID
	nat := meta.NativeUser(id)
	name := strings.TrimSpace(u.FullName)
	if name == "" {
		name = u.Username
	}
	c.names[id] = name
	c.handles[id] = u.Username
	if u.ProfilePicURL != "" {
		c.avatars[nat] = u.ProfilePicURL
	}
	r.Persons = append(r.Persons, model.Person{
		AccountID: c.acc, NativeID: nat, PushName: u.FullName, Username: u.Username, AvatarURL: c.avatarPath(nat),
	})
}

// Thread traduce la ficha de un hilo (bandeja o detalle) y recuerda su estado. Devuelve su chat y sus personas.
func (c *Converter) Thread(info *slidetypes.ThreadInfo) *Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := &Result{Avatars: map[string]string{}}
	if info == nil || info.ID == "" {
		return r
	}
	if c.self == 0 && info.Viewer != nil && info.Viewer.InteropMessagingUserFBID != 0 {
		c.self = info.Viewer.InteropMessagingUserFBID
	}
	for _, u := range info.Users {
		c.person(u, r)
	}
	st := c.state(info.ID)
	st.known = true
	if info.ThreadID != "" {
		st.longID = info.ThreadID
		c.long2short[info.ThreadID] = info.ID
	}
	st.group = info.IsGroup || info.ThreadSubtype == slidetypes.ThreadSubtypeGroup || len(info.Users) > 1
	st.users = st.users[:0]
	for _, u := range info.Users {
		if u != nil {
			st.users = append(st.users, u.InteropMessagingUserFBID)
		}
	}
	if st.group {
		st.title = info.ThreadTitle
		if info.ThreadImageURL != "" {
			c.avatars[info.ID] = info.ThreadImageURL
		}
	} else if len(info.Users) == 1 {
		st.title = firstNonEmpty(info.Users[0].FullName, info.Users[0].Username)
		if info.Users[0].ProfilePicURL != "" {
			c.avatars[info.ID] = info.Users[0].ProfilePicURL
		}
	}
	if ts := info.LastActivityTimestampMS.UnixMilli(); ts > 0 {
		st.lastTS = ts
	}
	if info.IsMuted != nil {
		if *info.IsMuted {
			st.muted = -1
		} else {
			st.muted = 0
		}
	}
	if info.IsPin != nil {
		st.pinned = *info.IsPin
	}
	st.request = info.SystemFolder == "PENDING" || info.SystemFolder == "SPAM"
	// Último mensaje: solo sirve para la vista previa (en la bandeja llega «a medias»).
	if info.SlideMessages != nil {
		var lastTS int64
		for _, e := range info.SlideMessages.Edges {
			if e.Node == nil {
				continue
			}
			if ts := e.Node.TimestampMS.UnixMilli(); ts >= lastTS {
				lastTS = ts
				if p := strings.TrimSpace(e.Node.IGDSnippet); p != "" {
					st.preview = p
				}
			}
		}
	}
	// Lectura: si mi marca alcanza la última actividad, el chat está leído.
	for _, rr := range info.SlideReadReceipts {
		if rr.ParticipantFBID == c.self && c.self != 0 {
			st.watermark = rr.WatermarkTimestampMS.UnixMilli()
			if st.lastTS > 0 && st.watermark >= st.lastTS && !info.MarkedAsUnread {
				st.unread = 0
			}
		}
	}
	if info.MarkedAsUnread {
		st.unread = 1
	}
	r.Chats = append(r.Chats, c.chatOf(st))
	st.unread = -1
	for id, u := range c.avatars {
		r.Avatars[id] = u
	}
	return r
}

func (c *Converter) chatOf(st *threadState) model.Chat {
	kind := model.ChatDirect
	if st.group {
		kind = model.ChatGroup
	}
	ch := model.Chat{
		Key: model.NewChatKey(c.acc, st.id), AccountID: c.acc, Channel: model.ChannelInstagram, NativeID: st.id,
		Kind: kind, Title: st.title, AvatarURL: c.avatarPath(st.id), UnreadCount: st.unread,
		LastMessageAt: st.lastTS, LastPreview: st.preview, Archived: st.archived, Pinned: st.pinned, MutedUntil: st.muted,
	}
	if kind == model.ChatDirect && len(st.users) == 1 {
		ch.PeerNativeID = meta.NativeUser(st.users[0])
	}
	if kind == model.ChatDirect && len(st.users) == 0 && c.self != 0 {
		ch.Kind, ch.Title = model.ChatSelf, "Tú"
	}
	return ch
}

// ChatSnapshot devuelve el chat como se conoce ahora.
func (c *Converter) ChatSnapshot(id string) (model.Chat, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.threads[id]
	if st == nil || !st.known {
		return model.Chat{}, false
	}
	return c.chatOf(st), true
}

// AllChats devuelve todos los chats conocidos, de más reciente a más antiguo.
func (c *Converter) AllChats() []model.Chat {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.Chat, 0, len(c.threads))
	for _, st := range c.threads {
		if st.known {
			out = append(out, c.chatOf(st))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastMessageAt > out[j].LastMessageAt })
	return out
}

// PersonSnapshot devuelve la persona como se conoce ahora.
func (c *Converter) PersonSnapshot(id int64) model.Person {
	c.mu.Lock()
	defer c.mu.Unlock()
	nat := meta.NativeUser(id)
	return model.Person{AccountID: c.acc, NativeID: nat, PushName: c.names[id], Username: c.handles[id], AvatarURL: c.avatarPath(nat)}
}

// ---------------------------------------------------------------------------------------------
// Mensajes

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func msgID(m *slidetypes.Message) string { return firstNonEmpty(m.ID, m.MessageID) }

// Messages traduce un mensaje de Instagram a uno o más del modelo (un adjunto adicional = un mensaje más,
// con id «<id>#2»). chat es el id del chat (thread_fbid). Un mensaje sin contenido útil devuelve nil.
func (c *Converter) Messages(m *slidetypes.Message, chat string) []model.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.messages(m, chat, &Result{Avatars: map[string]string{}})
}

func (c *Converter) messages(m *slidetypes.Message, chat string, r *Result) []model.Message {
	if m == nil || chat == "" || msgID(m) == "" {
		return nil
	}
	if m.Sender != nil {
		c.person(&m.Sender.UserDict, r)
	}
	id := msgID(m)
	base := model.Message{
		AccountID: c.acc, Channel: model.ChannelInstagram, ChatKey: model.NewChatKey(c.acc, chat),
		NativeID: id, ChatNativeID: chat, SenderNativeID: meta.NativeUser(m.SenderFBID),
		SenderName: c.names[m.SenderFBID], FromMe: m.SenderFBID == c.self && c.self != 0,
		Timestamp: m.TimestampMS.UnixMilli(), Delivery: model.DeliverySent, Forwarded: m.IGDIsForwarded,
	}
	if base.FromMe && base.SenderName == "" {
		base.SenderName = "Tú"
	}
	base.Key = model.NewMessageKey(c.acc, chat, id)
	if n := len(m.SlideEditHistory); n > 0 {
		base.EditedAt = m.SlideEditHistory[n-1].TimestampMS.UnixMilli()
	}
	if m.RepliedToMessageID != "" {
		base.QuotedNativeID = m.RepliedToMessageID
		if rm := m.RepliedToMessage; rm != nil {
			base.QuotedPreview = firstNonEmpty(rm.TextBody, rm.IGDSnippet)
			base.QuotedSender = firstNonEmpty(c.names[rm.SenderFBID], meta.NativeUser(rm.SenderFBID))
		}
	}
	for _, re := range m.Reactions {
		if re == nil || re.Reaction == "" {
			continue
		}
		base.Reactions = append(base.Reactions, model.Reaction{
			SenderNativeID: meta.NativeUser(re.SenderFBID), SenderName: c.names[re.SenderFBID],
			Emoji: re.Reaction, At: re.ReactionTimestampMS.UnixMilli(),
		})
		c.rememberReaction(re.LogMessageID, reactRef{msg: id, sender: re.SenderFBID, emoji: re.Reaction})
	}
	text := firstNonEmpty(m.TextBody)

	switch ct := m.Content.Content.(type) {
	case *slidetypes.MessageContentText:
		base.Kind, base.Text = model.KindText, firstNonEmpty(text, ct.TextBody)
		if strings.TrimSpace(base.Text) == "" {
			return nil
		}
		return []model.Message{base}
	case *slidetypes.MessageContentAdminText:
		if ct.IsReactionActionLog {
			return nil // «X reaccionó…»: la reacción llega por su propio aviso
		}
		var parts []string
		for _, f := range ct.TextFragments {
			if f.Plaintext != "" {
				parts = append(parts, f.Plaintext)
			}
		}
		t := strings.TrimSpace(strings.Join(parts, ""))
		if t == "" {
			t = strings.TrimSpace(text)
		}
		if t == "" {
			return nil
		}
		base.Kind, base.Text = model.KindSystem, t
		return []model.Message{base}
	case *slidetypes.MessageContentImage:
		return c.media(base, text, attList(ct.Attachments, model.KindImage))
	case *slidetypes.MessageContentVideo:
		return c.media(base, text, attList(ct.Videos, model.KindVideo))
	case *slidetypes.MessageContentMultiMedia:
		return c.media(base, text, attList(ct.Attachments, ""))
	case *slidetypes.MessageContentRavenImage:
		return c.raven(base, text, ct, model.KindImage)
	case *slidetypes.MessageContentRavenVideo:
		rc := slidetypes.MessageContentRavenImage(*ct)
		return c.raven(base, text, &rc, model.KindVideo)
	case *slidetypes.MessageContentAudio:
		var atts []att
		for _, a := range ct.AudioAttachments {
			if a == nil {
				continue
			}
			atts = append(atts, att{kind: model.KindVoice, mime: "audio/mp4", url: a.AttachmentCDNURL,
				dur: float64(a.PlayableDurationMS) / 1000})
		}
		return c.media(base, text, atts)
	case *slidetypes.MessageContentAnimatedMedia:
		var atts []att
		for _, a := range ct.AnimatedMedia {
			if a == nil {
				continue
			}
			x := att{w: a.PreviewWidth, h: a.PreviewHeight}
			switch {
			case a.IsSticker && a.AttachmentWebpURL != "":
				x.kind, x.mime, x.url = model.KindSticker, "image/webp", a.AttachmentWebpURL
			case a.AttachmentMP4URL != "":
				x.kind, x.mime, x.url = model.KindVideo, "video/mp4", a.AttachmentMP4URL
			default:
				x.kind, x.mime, x.url = model.KindImage, "image/webp", firstNonEmpty(a.AttachmentWebpURL, a.PreviewCDNURL)
			}
			atts = append(atts, x)
		}
		return c.media(base, text, atts)
	case *slidetypes.MessageContentSticker:
		return c.media(base, text, []att{{kind: model.KindSticker, mime: "image/webp", url: ct.PreviewURL, w: ct.PreviewWidth, h: ct.PreviewHeight}})
	case *slidetypes.MessageContentXMA:
		t := xmaText(ct, text)
		if t == "" {
			base.Kind = model.KindUnsupported
			return []model.Message{base}
		}
		base.Kind, base.Text = model.KindText, t
		return []model.Message{base}
	}
	// Música, respuestas de IA y contenidos que la librería no reconoce.
	if strings.TrimSpace(text) != "" {
		base.Kind, base.Text = model.KindText, text
		return []model.Message{base}
	}
	base.Kind = model.KindUnsupported
	return []model.Message{base}
}

func xmaText(ct *slidetypes.MessageContentXMA, text string) string {
	var parts []string
	if t := strings.TrimSpace(firstNonEmpty(ct.XMATextBody, text)); t != "" {
		parts = append(parts, t)
	}
	if x := ct.XMA; x != nil {
		if t := strings.TrimSpace(firstNonEmpty(x.TitleText, x.HeaderTitleText, x.CaptionBodyText)); t != "" && !contains(parts, t) {
			parts = append(parts, t)
		}
		if u := strings.TrimSpace(x.TargetURL); strings.HasPrefix(u, "http") && !contains(parts, u) {
			parts = append(parts, u)
		}
	}
	return strings.Join(parts, "\n")
}

func contains(parts []string, s string) bool {
	for _, p := range parts {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}

// att es un adjunto ya normalizado.
type att struct {
	kind model.ContentKind
	mime string
	url  string
	w, h int
	dur  float64
}

func attList(in []*slidetypes.Attachment, def model.ContentKind) []att {
	var out []att
	for _, a := range in {
		if a == nil {
			continue
		}
		k := def
		if k == "" {
			switch a.AttachmentType {
			case attVideo:
				k = model.KindVideo
			case attImage, attAnimatedImage:
				k = model.KindImage
			default:
				k = model.KindImage
				if strings.Contains(a.AttachmentCDNURL, ".mp4") {
					k = model.KindVideo
				}
			}
		}
		x := att{kind: k, url: firstNonEmpty(a.AttachmentCDNURL, a.PreviewCDNURL), w: a.PreviewWidth, h: a.PreviewHeight}
		if k == model.KindVideo {
			x.mime = "video/mp4"
		} else {
			x.mime = "image/jpeg"
		}
		out = append(out, x)
	}
	return out
}

func (c *Converter) raven(base model.Message, text string, ct *slidetypes.MessageContentRavenImage, kind model.ContentKind) []model.Message {
	// «Ver una vez» y «permitir repetir» no se pueden mostrar: solo las fotos y vídeos que se quedan en el chat.
	if ct.ViewMode != slidetypes.RavenViewModeKeepInChat || ct.Attachment == nil {
		base.Kind = model.KindUnsupported
		base.Text = text
		return []model.Message{base}
	}
	return c.media(base, text, attList([]*slidetypes.Attachment{ct.Attachment}, kind))
}

func (c *Converter) media(base model.Message, text string, atts []att) []model.Message {
	if len(atts) == 0 {
		if strings.TrimSpace(text) != "" {
			base.Kind, base.Text = model.KindText, text
			return []model.Message{base}
		}
		base.Kind = model.KindUnsupported
		return []model.Message{base}
	}
	if len(atts) > 11 {
		atts = atts[:11]
	}
	var out []model.Message
	for i, a := range atts {
		mm := base
		if i > 0 {
			mm.NativeID = base.NativeID + "#" + strconv.Itoa(i+1)
			mm.Key = model.NewMessageKey(c.acc, base.ChatNativeID, mm.NativeID)
			mm.Reactions, mm.QuotedNativeID, mm.QuotedPreview, mm.QuotedSender = nil, "", "", ""
		} else {
			mm.Text = text
		}
		if a.url == "" {
			mm.Kind = model.KindUnsupported
			out = append(out, mm)
			continue
		}
		mm.Kind = a.kind
		mm.Media = &model.Media{
			ID: meta.MediaID(base.ChatNativeID, mm.NativeID, 0), Mime: a.mime, FileName: meta.FileNameFor(a.kind, a.mime),
			Width: a.w, Height: a.h, Duration: a.dur,
			Handle: meta.MakeHandle(meta.Handle{URL: a.url, Mime: a.mime}),
		}
		out = append(out, mm)
	}
	return out
}

// Page traduce una página de mensajes (historial) y los devuelve de más antiguo a más reciente.
func (c *Converter) Page(chat string, edges []slidetypes.Node[*slidetypes.Message]) *Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := &Result{Avatars: map[string]string{}}
	for _, e := range edges {
		r.Messages = append(r.Messages, c.messages(e.Node, chat, r)...)
	}
	sort.SliceStable(r.Messages, func(i, j int) bool { return r.Messages[i].Timestamp < r.Messages[j].Timestamp })
	for id, u := range c.avatars {
		r.Avatars[id] = u
	}
	return r
}

func (c *Converter) rememberReaction(logID string, ref reactRef) {
	if logID == "" {
		return
	}
	if _, ok := c.reactLog[logID]; !ok {
		c.reactOrder = append(c.reactOrder, logID)
		if len(c.reactOrder) > 5000 {
			for _, id := range c.reactOrder[:1000] {
				delete(c.reactLog, id)
			}
			c.reactOrder = append([]string(nil), c.reactOrder[1000:]...)
		}
	}
	c.reactLog[logID] = ref
}

// ---------------------------------------------------------------------------------------------
// Avisos en vivo (deltas)

// LookupFunc: consultas a lo ya guardado (las pone el adaptador).
type LookupFunc struct {
	OutgoingUpTo func(ctx context.Context, chat string, upToMs int64) []string
}

// Delta traduce un aviso del socket. chat es el id del chat (thread_fbid). Devuelve (resultado, ok): ok=false si
// el aviso es de un hilo que no se conoce y hace falta pedirlo antes (el adaptador lo hace y reintenta).
func (c *Converter) Delta(ctx context.Context, d *slidetypes.Delta, lk LookupFunc) (*Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := &Result{Avatars: map[string]string{}}
	chat := d.ThreadIGID
	if nm, ok := d.Data.(*slidetypes.NewMessageEvent); ok && chat == "" && nm.Message != nil {
		chat = nm.Message.ThreadFBID
	}
	if chat == "" {
		return r, true
	}
	needsThread := false
	switch d.Data.(type) {
	case *slidetypes.NewMessageEvent, *slidetypes.AdminMessageEvent, *slidetypes.PinThreadEvent:
		needsThread = true
	}
	if st := c.threads[chat]; (st == nil || !st.known) && needsThread {
		return r, false
	}
	switch ev := d.Data.(type) {
	case *slidetypes.NewMessageEvent:
		c.newMessage(r, chat, ev.Message)
	case *slidetypes.AdminMessageEvent:
		c.newMessage(r, chat, ev.Message)
	case *slidetypes.EditMessageEvent:
		text := ev.TextBody
		r.Updates = append(r.Updates, model.MessageUpdate{
			Key: model.NewMessageKey(c.acc, chat, ev.MessageID), Text: &text,
			EditedAt: firstPositive(ev.SlideEditHistoryEntry.TimestampMS.UnixMilli(), c.now().UnixMilli()),
		})
	case *slidetypes.DeleteMessageEvent:
		r.Updates = append(r.Updates, model.MessageUpdate{Key: model.NewMessageKey(c.acc, chat, ev.MessageID), Deleted: true})
	case *slidetypes.CreateReactionEvent:
		c.rememberReaction(ev.Reaction.LogMessageID, reactRef{msg: ev.MessageID, sender: ev.Reaction.SenderFBID, emoji: ev.Reaction.Reaction})
		r.Reactions = append(r.Reactions, model.ReactionEvent{
			MessageKey: model.NewMessageKey(c.acc, chat, ev.MessageID),
			Reaction: model.Reaction{SenderNativeID: meta.NativeUser(ev.Reaction.SenderFBID), SenderName: c.names[ev.Reaction.SenderFBID],
				Emoji: ev.Reaction.Reaction, At: firstPositive(ev.Reaction.ReactionTimestampMS.UnixMilli(), c.now().UnixMilli())},
		})
	case *slidetypes.DeleteReactionEvent:
		msg, sender := ev.MessageID, ev.Reaction.SenderFBID
		if ref, ok := c.reactLog[ev.Reaction.LogMessageID]; ok && (msg == "" || sender == 0) {
			msg, sender = ref.msg, ref.sender
		}
		if msg != "" && sender != 0 {
			r.Reactions = append(r.Reactions, model.ReactionEvent{
				MessageKey: model.NewMessageKey(c.acc, chat, msg),
				Reaction:   model.Reaction{SenderNativeID: meta.NativeUser(sender), SenderName: c.names[sender], Emoji: "", At: c.now().UnixMilli()},
			})
		}
	case *slidetypes.MarkReadEvent:
		c.setUnread(r, chat, 0)
	case *slidetypes.MarkUnreadEvent:
		if ev.MarkedAsUnread {
			c.setUnread(r, chat, 1)
		}
	case *slidetypes.ReadReceiptEvent:
		c.readReceipt(ctx, r, chat, ev, lk)
	case *slidetypes.DeleteThreadEvent:
		delete(c.threads, chat)
		r.Removed = append(r.Removed, model.NewChatKey(c.acc, chat))
	case *slidetypes.PinThreadEvent:
		st := c.state(chat)
		st.pinned = ev.IsPinned
		r.Chats = append(r.Chats, c.chatOf(st))
	case *slidetypes.MuteThreadEvent:
		st := c.state(chat)
		st.muted = 0
		if ev.IsMutedNow {
			st.muted = -1
		}
		r.Chats = append(r.Chats, c.chatOf(st))
	case *slidetypes.UpdateThreadNameEvent:
		st := c.state(chat)
		st.title = ev.ThreadName
		r.Chats = append(r.Chats, c.chatOf(st))
		c.newMessage(r, chat, ev.Message)
	case *slidetypes.UpdateThreadImageEvent:
		if ev.ThreadImage.URI != "" {
			c.avatars[chat] = ev.ThreadImage.URI
			r.Avatars[chat] = ev.ThreadImage.URI
		}
		c.newMessage(r, chat, ev.Message)
	case *slidetypes.ParticipantJoinEvent:
		c.newMessage(r, chat, ev.Message)
	case *slidetypes.ParticipantLeaveEvent:
		c.newMessage(r, chat, ev.Message)
	}
	for id, u := range c.avatars {
		r.Avatars[id] = u
	}
	return r, true
}

func firstPositive(vs ...int64) int64 {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

func (c *Converter) newMessage(r *Result, chat string, m *slidetypes.Message) {
	if m == nil {
		return
	}
	msgs := c.messages(m, chat, r)
	r.Messages = append(r.Messages, msgs...)
	if st := c.threads[chat]; st != nil && len(msgs) > 0 {
		if ts := msgs[0].Timestamp; ts > st.lastTS {
			st.lastTS = ts
		}
	}
}

func (c *Converter) setUnread(r *Result, chat string, n int) {
	st := c.state(chat)
	st.unread = n
	r.Chats = append(r.Chats, c.chatOf(st))
	st.unread = -1
}

func (c *Converter) readReceipt(ctx context.Context, r *Result, chat string, ev *slidetypes.ReadReceiptEvent, lk LookupFunc) {
	who := ev.ReadReceipt.ParticipantFBID
	mark := ev.ReadReceipt.WatermarkTimestampMS.UnixMilli()
	if who == c.self && c.self != 0 {
		c.setUnread(r, chat, 0)
		return
	}
	if lk.OutgoingUpTo == nil || mark <= 0 {
		return
	}
	ids := lk.OutgoingUpTo(ctx, chat, mark)
	if len(ids) == 0 {
		return
	}
	keys := make([]model.MessageKey, len(ids))
	for i, id := range ids {
		keys[i] = model.NewMessageKey(c.acc, chat, id)
	}
	r.Receipts = append(r.Receipts, model.ReceiptEvent{
		ChatKey: model.NewChatKey(c.acc, chat), MessageKeys: keys, Delivery: model.DeliveryRead,
		ReaderID: meta.NativeUser(who), At: c.now().UnixMilli(),
	})
}

// Typing traduce un aviso de «escribiendo» (el id de hilo viene en formato largo).
func (c *Converter) Typing(t *slidetypes.TypingNotification) (model.TypingEvent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.SenderID == c.self && c.self != 0 {
		return model.TypingEvent{}, false
	}
	chat := c.long2short[t.ThreadID]
	if chat == "" {
		if _, ok := c.threads[t.ThreadID]; ok {
			chat = t.ThreadID
		}
	}
	if chat == "" {
		return model.TypingEvent{}, false
	}
	return model.TypingEvent{
		ChatKey: model.NewChatKey(c.acc, chat), PeerID: meta.NativeUser(t.SenderID), Typing: t.ActivityStatus != 0,
	}, true
}

// AvatarURLs devuelve una copia de las URL remotas de fotos conocidas (id → URL).
func (c *Converter) AvatarURLs() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.avatars))
	for k, v := range c.avatars {
		out[k] = v
	}
	return out
}

// KnownPersons devuelve las personas que se conocen por los chats (sin la propia).
func (c *Converter) KnownPersons() []model.Person {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.Person, 0, len(c.names))
	for id, name := range c.names {
		if id == c.self {
			continue
		}
		nat := meta.NativeUser(id)
		out = append(out, model.Person{AccountID: c.acc, NativeID: nat, PushName: name, Username: c.handles[id], AvatarURL: c.avatarPath(nat)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NativeID < out[j].NativeID })
	return out
}
