package telegram

// Entrada (08 §5): manejadores del despachador de gotd que traducen cada actualización a eventos.

import (
	"context"
	"strconv"
	"time"

	"github.com/gotd/td/tg"

	"dconnector/internal/core/model"
)

// typingHold: cuánto dura «escribiendo…» sin nuevas señales (Telegram no manda el «ya no escribe»).
const typingHold = 7 * time.Second

// dispatcher arma el despachador con todos los manejadores.
func (a *Adapter) dispatcher() tg.UpdateDispatcher {
	d := tg.NewUpdateDispatcher()
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		defer a.recoverLoop("mensaje nuevo")
		a.onMessage(ctx, e, u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		defer a.recoverLoop("mensaje nuevo de canal")
		a.onMessage(ctx, e, u.Message)
		return nil
	})
	d.OnEditMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditMessage) error {
		defer a.recoverLoop("mensaje editado")
		a.onEdit(ctx, e, u.Message)
		return nil
	})
	d.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		defer a.recoverLoop("mensaje editado de canal")
		a.onEdit(ctx, e, u.Message)
		return nil
	})
	d.OnDeleteMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteMessages) error {
		defer a.recoverLoop("mensajes borrados")
		a.onDelete(ctx, "", u.Messages)
		return nil
	})
	d.OnDeleteChannelMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		defer a.recoverLoop("mensajes borrados de canal")
		a.onDelete(ctx, channelNative(u.ChannelID), u.Messages)
		return nil
	})
	d.OnReadHistoryOutbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadHistoryOutbox) error {
		defer a.recoverLoop("leído por el otro")
		a.feedEntities(ctx, e)
		a.onReadOutbox(ctx, nativeOf(u.Peer), u.MaxID)
		return nil
	})
	d.OnReadChannelOutbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadChannelOutbox) error {
		defer a.recoverLoop("leído en canal")
		a.feedEntities(ctx, e)
		a.onReadOutbox(ctx, channelNative(u.ChannelID), u.MaxID)
		return nil
	})
	d.OnUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateUserTyping) error {
		defer a.recoverLoop("escribiendo")
		a.onTyping(userNative(u.UserID), userNative(u.UserID), u.Action)
		return nil
	})
	d.OnChatUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateChatUserTyping) error {
		defer a.recoverLoop("escribiendo en grupo")
		a.onTyping(chatNative(u.ChatID), nativeOf(u.FromID), u.Action)
		return nil
	})
	d.OnChannelUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateChannelUserTyping) error {
		defer a.recoverLoop("escribiendo en supergrupo")
		a.onTyping(channelNative(u.ChannelID), nativeOf(u.FromID), u.Action)
		return nil
	})
	d.OnMessageReactions(func(ctx context.Context, e tg.Entities, u *tg.UpdateMessageReactions) error {
		defer a.recoverLoop("reacciones")
		a.feedEntities(ctx, e)
		a.onReactions(e, u)
		return nil
	})
	d.OnMessagePoll(func(ctx context.Context, e tg.Entities, u *tg.UpdateMessagePoll) error {
		defer a.recoverLoop("encuesta")
		a.feedEntities(ctx, e)
		a.onPoll(ctx, e, u)
		return nil
	})
	d.OnStory(func(ctx context.Context, e tg.Entities, u *tg.UpdateStory) error {
		defer a.recoverLoop("historia")
		a.feedEntities(ctx, e)
		a.onStory(e, u)
		return nil
	})
	d.OnUserName(func(ctx context.Context, e tg.Entities, u *tg.UpdateUserName) error {
		defer a.recoverLoop("nombre de usuario")
		a.feedEntities(ctx, e)
		a.onUserName(ctx, u)
		return nil
	})
	d.OnUser(func(ctx context.Context, e tg.Entities, u *tg.UpdateUser) error {
		defer a.recoverLoop("usuario")
		a.feedEntities(ctx, e)
		return nil
	})
	d.OnDialogPinned(func(ctx context.Context, e tg.Entities, u *tg.UpdateDialogPinned) error {
		defer a.recoverLoop("chat fijado")
		if dp, ok := u.Peer.(*tg.DialogPeer); ok {
			a.onMarks(ctx, e, nativeOf(dp.Peer), func(m *chatMarks) { m.Pinned = u.Pinned })
		}
		return nil
	})
	d.OnPinnedDialogs(func(ctx context.Context, e tg.Entities, u *tg.UpdatePinnedDialogs) error {
		defer a.recoverLoop("chats fijados")
		for _, p := range u.Order {
			if dp, ok := p.(*tg.DialogPeer); ok {
				a.onMarks(ctx, e, nativeOf(dp.Peer), func(m *chatMarks) { m.Pinned = true })
			}
		}
		return nil
	})
	d.OnNotifySettings(func(ctx context.Context, e tg.Entities, u *tg.UpdateNotifySettings) error {
		defer a.recoverLoop("silencio")
		if np, ok := u.Peer.(*tg.NotifyPeer); ok {
			until := muteMs(u.NotifySettings.MuteUntil, time.Now())
			a.onMarks(ctx, e, nativeOf(np.Peer), func(m *chatMarks) { m.Muted = until })
		}
		return nil
	})
	d.OnFolderPeers(func(ctx context.Context, e tg.Entities, u *tg.UpdateFolderPeers) error {
		defer a.recoverLoop("archivados")
		for _, fp := range u.FolderPeers {
			archived := fp.FolderID == 1
			a.onMarks(ctx, e, nativeOf(fp.Peer), func(m *chatMarks) { m.Archived = archived })
		}
		return nil
	})
	return d
}

// feedEntities aprende los pares de unas entidades y anuncia las personas nuevas o cambiadas.
func (a *Adapter) feedEntities(ctx context.Context, e tg.Entities) {
	for _, u := range a.peers.feed(ctx, e) {
		if u.Bot && u.Deleted {
			continue
		}
		if a.local.noteUserSig(u) {
			p := personOf(a.acc.ID, u)
			a.emit(model.Event{Type: model.EvPerson, Person: &p})
		}
	}
}

// muteMs: MuteUntil de Telegram (segundos Unix; un valor enorme es «siempre») → ms del modelo.
func muteMs(until int, now time.Time) int64 {
	if until <= 0 {
		return 0
	}
	const forever = 2147483647
	if until >= forever-86400 || int64(until) > now.Add(10*365*24*time.Hour).Unix() {
		return -1
	}
	ms := int64(until) * 1000
	if ms <= now.UnixMilli() {
		return 0
	}
	return ms
}

// ---------------------------------------------------------------------------------------------
// Mensajes

// onMessage traduce un mensaje nuevo (08 §5): message.received o system.
func (a *Adapter) onMessage(ctx context.Context, e tg.Entities, mc tg.MessageClass) {
	a.feedEntities(ctx, e)
	c := a.conv(&e)
	var msg model.Message
	switch m := mc.(type) {
	case *tg.Message:
		msg = c.message(m)
		a.rememberPoll(ctx, msg.ChatNativeID, m)
	case *tg.MessageService:
		sm, ok := c.service(m)
		if !ok {
			return
		}
		msg = sm
	default:
		return
	}
	if msg.ChatNativeID == "" {
		return
	}
	a.local.noteQuote(msg)
	a.local.indexMsgChats(ctx, []model.Message{msg})
	a.announceChat(ctx, c, msg)
	a.emit(model.Event{Type: model.EvMessageNew, At: msg.Timestamp, ChatKey: msg.ChatKey, Message: &msg})
}

// rememberPoll guarda las opciones de una encuesta recién vista.
func (a *Adapter) rememberPoll(ctx context.Context, chat string, m *tg.Message) {
	if mp, ok := m.Media.(*tg.MessageMediaPoll); ok {
		a.local.putPoll(ctx, chat, m.ID, mp.Poll.ID, recordOfPoll(mp.Poll))
	}
}

// onEdit traduce un mensaje editado: message.updated con el texto nuevo.
func (a *Adapter) onEdit(ctx context.Context, e tg.Entities, mc tg.MessageClass) {
	m, ok := mc.(*tg.Message)
	if !ok {
		return
	}
	a.feedEntities(ctx, e)
	msg := a.conv(&e).message(m)
	if msg.ChatNativeID == "" {
		return
	}
	text := msg.Text
	editedAt := msg.EditedAt
	if editedAt == 0 {
		editedAt = time.Now().UnixMilli()
	}
	a.local.noteQuote(msg)
	a.emit(model.Event{
		Type: model.EvMessageUpdate, ChatKey: msg.ChatKey,
		Update: &model.MessageUpdate{Key: msg.Key, Text: &text, EditedAt: editedAt},
	})
}

// onDelete marca mensajes como borrados. En privados y grupos el aviso no trae el chat: se busca
// con el índice tg.msgchat.<id> (o en la tabla messages).
func (a *Adapter) onDelete(ctx context.Context, chat string, ids []int) {
	for _, id := range ids {
		c := chat
		if c == "" {
			c = a.local.chatOfMessage(ctx, id)
		}
		if c == "" {
			a.log.Debug("mensaje borrado de un chat desconocido", "id", id)
			continue
		}
		key := model.NewMessageKey(a.acc.ID, c, strconv.Itoa(id))
		a.emit(model.Event{
			Type: model.EvMessageUpdate, ChatKey: model.NewChatKey(a.acc.ID, c),
			Update: &model.MessageUpdate{Key: key, Deleted: true},
		})
	}
}

// onReadOutbox: el otro leyó mis mensajes hasta maxID.
func (a *Adapter) onReadOutbox(ctx context.Context, chat string, maxID int) {
	if chat == "" || maxID <= 0 {
		return
	}
	ids := a.local.outgoingUpTo(ctx, chat, maxID)
	if len(ids) == 0 {
		return
	}
	keys := make([]model.MessageKey, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, model.NewMessageKey(a.acc.ID, chat, id))
	}
	now := time.Now().UnixMilli()
	a.emit(model.Event{
		Type: model.EvReceipt, At: now, ChatKey: model.NewChatKey(a.acc.ID, chat),
		Receipt: &model.ReceiptEvent{
			ChatKey: model.NewChatKey(a.acc.ID, chat), MessageKeys: keys, Delivery: model.DeliveryRead, ReaderID: chat, At: now,
		},
	})
}

// ---------------------------------------------------------------------------------------------
// Escribiendo, reacciones y encuestas

// onTyping emite «escribiendo…» y programa su fin (Telegram no avisa cuando deja de escribir).
func (a *Adapter) onTyping(chat, peer string, action tg.SendMessageActionClass) {
	if chat == "" || peer == "" || peer == userNative(a.selfID.Load()) {
		return
	}
	typing, recording := typingFromAction(action)
	key := chat + "|" + peer
	stop := func() {
		a.typingMu.Lock()
		delete(a.typingTimers, key)
		a.typingMu.Unlock()
		a.emit(model.Event{Type: model.EvTyping, ChatKey: model.NewChatKey(a.acc.ID, chat),
			Typing: &model.TypingEvent{ChatKey: model.NewChatKey(a.acc.ID, chat), PeerID: peer}})
	}
	a.typingMu.Lock()
	if t, ok := a.typingTimers[key]; ok {
		t.Stop()
		delete(a.typingTimers, key)
	}
	if typing || recording {
		if a.typingTimers == nil {
			a.typingTimers = map[string]*time.Timer{}
		}
		a.typingTimers[key] = time.AfterFunc(typingHold, stop)
	}
	a.typingMu.Unlock()
	a.emit(model.Event{Type: model.EvTyping, ChatKey: model.NewChatKey(a.acc.ID, chat),
		Typing: &model.TypingEvent{ChatKey: model.NewChatKey(a.acc.ID, chat), PeerID: peer, Typing: typing, Recording: recording}})
}

// onReactions emite una reacción por cada persona de la lista reciente.
func (a *Adapter) onReactions(e tg.Entities, u *tg.UpdateMessageReactions) {
	chat := nativeOf(u.Peer)
	if chat == "" {
		return
	}
	c := a.conv(&e)
	key := model.NewMessageKey(a.acc.ID, chat, strconv.Itoa(u.MsgID))
	for _, r := range c.reactions(u.Reactions) {
		r := r
		a.emit(model.Event{
			Type: model.EvReaction, At: r.At, ChatKey: model.NewChatKey(a.acc.ID, chat),
			Reaction: &model.ReactionEvent{MessageKey: key, Reaction: r},
		})
	}
}

// onPoll emite los recuentos nuevos de una encuesta.
func (a *Adapter) onPoll(ctx context.Context, e tg.Entities, u *tg.UpdateMessagePoll) {
	chat, msgID := nativeOf(u.Peer), u.MsgID
	if chat == "" || msgID == 0 {
		var ok bool
		if chat, msgID, ok = a.local.pollRef(ctx, u.PollID); !ok {
			return
		}
	}
	rec, ok := a.local.getPoll(ctx, chat, msgID)
	if !u.Poll.Zero() {
		if nr := recordOfPoll(u.Poll); len(nr.Options) > 0 {
			rec, ok = nr, true
			a.local.putPoll(ctx, chat, msgID, u.PollID, nr)
		}
	}
	if !ok {
		return
	}
	poll := a.conv(&e).pollFromRecord(rec, u.Results)
	a.emit(model.Event{
		Type: model.EvMessageUpdate, ChatKey: model.NewChatKey(a.acc.ID, chat),
		Update: &model.MessageUpdate{Key: model.NewMessageKey(a.acc.ID, chat, strconv.Itoa(msgID)), Poll: poll},
	})
}

// onUserName: cambio de nombre o de @usuario de una persona.
func (a *Adapter) onUserName(ctx context.Context, u *tg.UpdateUserName) {
	cur, ok := a.peers.User(u.UserID)
	if !ok {
		return
	}
	cp := *cur
	cp.FirstName, cp.LastName = u.FirstName, u.LastName
	if len(u.Usernames) > 0 {
		cp.Usernames, cp.Username = u.Usernames, ""
	}
	cp.Min = false
	a.feedEntities(ctx, tg.Entities{Users: map[int64]*tg.User{u.UserID: &cp}})
}

// ---------------------------------------------------------------------------------------------
// Chats

// chatTitle: título visible de un chat; "" si no se conoce.
func (a *Adapter) chatTitle(native string, v peerView) string {
	kind, id, ok := parseNative(native)
	if !ok {
		return ""
	}
	if kind == kindUser && id == a.selfID.Load() {
		return "Mensajes guardados"
	}
	return nameOfNative(v, native)
}

// chatShell arma un Chat con lo que el adaptador sabe (sin contadores ni último mensaje). Incluye
// siempre las marcas (el almacén las reemplaza en cada UpsertChat). false si no se conoce el título.
func (a *Adapter) chatShell(native string, v peerView) (model.Chat, bool) {
	peer, ok := peerFromNative(native)
	if !ok {
		return model.Chat{}, false
	}
	title := a.chatTitle(native, v)
	kind := chatKindOf(peer, v, a.selfID.Load())
	ch := model.Chat{
		Key: model.NewChatKey(a.acc.ID, native), AccountID: a.acc.ID, Channel: model.ChannelTelegram,
		NativeID: native, Kind: kind, Title: title,
		// -1 = «no sé cuántos no leídos hay»: el almacén conserva el contador que ya tenía.
		UnreadCount: -1,
	}
	if kind == model.ChatDirect || kind == model.ChatSelf {
		ch.PeerNativeID = native
	}
	if k, id, _ := parseNative(native); k == kindChannel {
		if c, ok := v.Channel(id); ok {
			ch.ReadOnly = channelReadOnly(c)
		}
	}
	if m, ok := a.local.marksOf(native); ok {
		ch.Archived, ch.Pinned, ch.MutedUntil = m.Archived, m.Pinned, m.Muted
	}
	return ch, title != ""
}

// onMarks aplica un cambio de marcas (fijado, silencio, archivado) y reenvía el chat completo.
func (a *Adapter) onMarks(ctx context.Context, e tg.Entities, native string, f func(*chatMarks)) {
	if native == "" {
		return
	}
	a.feedEntities(ctx, e)
	if _, known := a.local.updateMarks(native, f); !known {
		return // aún no se sincronizó ese chat: la sincronización traerá las marcas
	}
	if ch, ok := a.chatShell(native, chainView{entitiesView{e}, a.peers}); ok {
		a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
	}
}

// announceChat emite chat.updated la primera vez que se ve un chat que la sincronización no trajo
// (con título, tipo y vista previa del primer mensaje) y pide su avatar.
func (a *Adapter) announceChat(ctx context.Context, c converter, msg model.Message) {
	if !a.local.isSynced() {
		return // la sincronización inicial anuncia todos los chats
	}
	if _, known := a.local.marksOf(msg.ChatNativeID); known {
		return
	}
	ch, ok := a.chatShell(msg.ChatNativeID, c.v())
	if !ok {
		if ch.Kind == model.ChatDirect && !msg.FromMe && msg.SenderName != "" {
			ch.Title, ok = msg.SenderName, true
		}
		if !ok {
			return
		}
	}
	ch.LastMessageAt = msg.Timestamp
	ch.LastPreview = model.Preview(msg)
	ch.LastFromMe = msg.FromMe
	a.local.setMarks(msg.ChatNativeID, chatMarks{})
	a.emit(model.Event{Type: model.EvChatUpsert, At: msg.Timestamp, Chat: &ch})
	a.queueAvatar(msg.ChatNativeID)
}
