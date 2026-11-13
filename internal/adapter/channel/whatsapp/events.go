package whatsapp

// Manejador de eventos de whatsmeow (07 §3 y §7): cada evento se traduce con las funciones puras
// de convert.go y se entrega al sink.

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"dconnector/internal/core/model"
)

// handle recibe todos los eventos del cliente. Nunca debe tumbar el proceso.
func (a *Adapter) handle(raw any) {
	defer a.recoverLoop("handle")
	switch evt := raw.(type) {
	case *events.Connected:
		a.onConnected()
	case *events.Disconnected:
		a.onDisconnected()
	case *events.PairSuccess:
		a.log.Info("vinculación completada")
	case *events.LoggedOut:
		a.onLoggedOut(evt)
	case *events.StreamReplaced:
		a.onSessionLost("WhatsApp se abrió en otro lugar con esta misma sesión. Vuelve a conectar la cuenta aquí para usarla.")
	case *events.ClientOutdated:
		a.onSessionLost("WhatsApp rechazó esta versión de dConnector por estar desactualizada. Actualiza la aplicación y vuelve a conectar.")
	case *events.TemporaryBan:
		a.setStatus(model.AccountStatus{State: model.StateError, Detail: banDetail(evt, time.Now())})
	case *events.ConnectFailure:
		a.log.Warn("WhatsApp rechazó la conexión", "reason", int(evt.Reason))
		a.setStatus(model.AccountStatus{
			State: model.StateError, Detail: fmt.Sprintf("WhatsApp rechazó la conexión (código %d).", int(evt.Reason)),
		})
	case *events.Message:
		a.onMessage(evt)
	case *events.UndecryptableMessage:
		a.onUndecryptable(evt)
	case *events.Receipt:
		for _, ev := range a.conv().receiptEvents(evt) {
			a.emit(ev)
		}
	case *events.ChatPresence:
		if ev, ok := a.conv().presenceEvent(evt); ok {
			a.emit(ev)
		}
	case *events.PushName:
		a.onPushName(evt)
	case *events.Contact:
		a.onContact(evt)
	case *events.Picture:
		go a.onPicture(evt)
	case *events.GroupInfo:
		a.onGroupInfo(evt)
	case *events.JoinedGroup:
		a.onJoinedGroup(evt)
	case *events.Archive:
		a.onChatFlag(evt.JID, evt.FromFullSync, func(ch *model.Chat) { ch.Archived = evt.Action.GetArchived() })
	case *events.Pin:
		a.onChatFlag(evt.JID, evt.FromFullSync, func(ch *model.Chat) { ch.Pinned = evt.Action.GetPinned() })
	case *events.Mute:
		a.onChatFlag(evt.JID, evt.FromFullSync, func(ch *model.Chat) {
			ch.MutedUntil = muteUntil(evt.Action.GetMuted(), evt.Action.GetMuteEndTimestamp(), time.Now())
		})
	case *events.MarkChatAsRead:
		a.onChatFlag(evt.JID, evt.FromFullSync, func(ch *model.Chat) {
			if evt.Action.GetRead() {
				ch.UnreadCount = 0 // leído desde el teléfono
			} else {
				ch.UnreadCount = 1
			}
		})
	case *events.DeleteChat:
		a.onDeleteChat(evt)
	case *events.HistorySync:
		go a.onHistorySync(evt)
	case *events.MediaRetry:
		// Respuesta del teléfono a «vuelve a subir este archivo» (media_retry.go).
		if !a.retry.deliver(evt) {
			a.log.Debug("respuesta de archivo que nadie esperaba")
		}
	}
}

// noteUnknownType registra, una sola vez por tipo y por sesión, que llegó un mensaje de un tipo
// que no se sabe traducir. Solo lleva el nombre del campo del protocolo: nunca contenido ni
// teléfonos.
func (a *Adapter) noteUnknownType(label string) {
	if label == "" {
		return
	}
	if _, seen := a.unknownSeen.LoadOrStore(label, struct{}{}); seen {
		return
	}
	a.log.Info("tipo de mensaje de WhatsApp no soportado: " + label)
}

// muteUntil: fin del silencio en ms (0 = no silenciado, -1 = siempre).
func muteUntil(muted bool, endMs int64, now time.Time) int64 {
	if !muted {
		return 0
	}
	if endMs <= 0 {
		return -1
	}
	if endMs <= now.UnixMilli() {
		return 0
	}
	return endMs
}

// ---------------------------------------------------------------------------------------------
// Conexión

func (a *Adapter) isRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runCtx != nil
}

func (a *Adapter) onConnected() {
	a.setStatus(model.AccountStatus{State: model.StateConnected})
	go func() {
		defer a.recoverLoop("connected")
		cli := a.getClient()
		if cli == nil {
			return
		}
		ctx, cancel := context.WithTimeout(a.runContext(), opTimeout)
		// «No disponible»: así el teléfono sigue recibiendo los avisos. Sin nombre propio todavía
		// no se puede enviar presencia; no es un error.
		if err := cli.SendPresence(ctx, types.PresenceUnavailable); err != nil {
			a.log.Debug("presencia no enviada", "err", redact(err.Error()))
		}
		cancel()
		if jid := cli.Store.GetJID(); !jid.IsEmpty() {
			a.emit(model.Event{Type: model.EvPerson, Person: &model.Person{
				AccountID: a.acc.ID, NativeID: jidToNative(jid), PushName: cli.Store.PushName, Phone: phoneOf(jid.ToNonAD()),
			}})
		}
		if err := a.SyncChats(a.runContext()); err != nil {
			a.log.Debug("no se pudieron sincronizar los chats", "err", redact(err.Error()))
		}
	}()
}

func (a *Adapter) onDisconnected() {
	if !a.isRunning() {
		return
	}
	a.mu.Lock()
	state := a.lastStatus.State
	a.mu.Unlock()
	switch state {
	case model.StateLoggedOut, model.StateError, model.StatePairing:
		return
	}
	a.setStatus(model.AccountStatus{State: model.StateConnecting})
}

// onLoggedOut: la sesión ya no vale; se borra la sesión local (07 §3).
func (a *Adapter) onLoggedOut(evt *events.LoggedOut) {
	detail := "WhatsApp cerró la sesión de este dispositivo. Vuelve a vincular la cuenta."
	if evt.OnConnect {
		switch evt.Reason {
		case events.ConnectFailureMainDeviceGone:
			detail = "El teléfono principal desvinculó este dispositivo. Vuelve a vincular la cuenta."
		case events.ConnectFailureUnknownLogout:
			detail = "WhatsApp cerró la sesión de esta cuenta por un motivo desconocido. Vuelve a vincular la cuenta."
		}
	}
	a.log.Warn("sesión cerrada por WhatsApp", "onConnect", evt.OnConnect, "reason", int(evt.Reason))
	go func() {
		defer a.recoverLoop("loggedOut")
		a.dropClient(context.Background(), true)
		a.setStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: detail})
	}()
}

// onSessionLost: otra sesión tomó la cuenta o la versión quedó vieja. Se informa como logged_out
// (para que no se reintente en bucle), pero la sesión guardada se conserva: BeginPairing la reutiliza.
func (a *Adapter) onSessionLost(detail string) {
	a.log.Warn("sesión interrumpida", "detail", "sesión reemplazada o cliente desactualizado")
	a.setStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: detail})
}

// ---------------------------------------------------------------------------------------------
// Mensajes

func (a *Adapter) onMessage(evt *events.Message) {
	c := a.conv()
	in := c.classify(evt)
	if isStatusJID(evt.Info.Chat) && !evt.Info.IsFromMe {
		// Diagnóstico de estados ajenos: dice si llegaron y por qué se descartó alguno (sin texto ni números).
		a.log.Info("estado recibido", "contenido", messageKindName(evt.Message), "aceptado", in.Kind == inStatus || in.Kind == inStatusRevoke || in.Kind == inStatusReaction)
	}
	if in.Kind == inIgnore {
		return
	}
	a.noteWire(evt.Info.Chat, c.chatJID(evt.Info))
	a.noteWire(evt.Info.Sender, c.senderJID(evt.Info))
	ctx, cancel := context.WithTimeout(a.runContext(), opTimeout)
	defer cancel()

	switch in.Kind {
	case inMessage:
		a.deliverMessage(ctx, c, in.Message, evt, true)
	case inStatus, inStatusRevoke:
		a.emit(model.Event{Type: model.EvStatusUpsert, At: in.Status.PostedAt, Status: &in.Status})
	case inStatusReaction:
		a.emit(model.Event{Type: model.EvStatusView, At: in.StatusView.At, StatusView: in.StatusView})
	case inReaction:
		a.emit(model.Event{Type: model.EvReaction, At: in.Reaction.Reaction.At, ChatKey: model.NewChatKey(a.acc.ID, in.ChatID), Reaction: in.Reaction})
	case inRevoke, inEdit:
		a.emit(model.Event{Type: model.EvMessageUpdate, ChatKey: model.NewChatKey(a.acc.ID, in.ChatID), Update: in.Update})
	case inPollVote:
		a.onPollVote(ctx, c, evt, in)
	}
}

// deliverMessage anuncia el chat si es nuevo, guarda lo necesario para citar y reenviar, y emite
// message.received.
func (a *Adapter) deliverMessage(ctx context.Context, c converter, msg model.Message, evt *events.Message, live bool) {
	if msg.Poll != nil {
		msg.Poll = a.polls.register(ctx, msg.ChatNativeID, msg.NativeID, msg.Poll)
	}
	if live {
		a.announceChat(ctx, c, msg)
		if evt != nil && msg.Kind != model.KindUnsupported {
			a.local.putMsg(ctx, msg.ChatNativeID, msg.NativeID, msg.Timestamp, evt.Message)
		}
	}
	a.emit(model.Event{
		Type: model.EvMessageNew, At: msg.Timestamp, ChatKey: msg.ChatKey, Message: &msg,
	})
}

// announceChat emite chat.updated la primera vez que el adaptador ve un chat (con título, tipo y
// vista previa del primer mensaje), y pide su avatar en segundo plano.
func (a *Adapter) announceChat(ctx context.Context, c converter, msg model.Message) {
	if a.local.chatKnown(ctx, msg.ChatNativeID) {
		return
	}
	jid, err := nativeToJID(msg.ChatNativeID)
	if err != nil {
		return
	}
	ch := a.chatShell(ctx, c, jid, true)
	ch.LastMessageAt = msg.Timestamp
	ch.LastPreview = model.Preview(msg)
	ch.LastFromMe = msg.FromMe
	if ch.Kind == model.ChatDirect && ch.Title == "" {
		ch.Title = msg.SenderName
	}
	a.local.markChat(ctx, msg.ChatNativeID)
	a.emit(model.Event{Type: model.EvChatUpsert, At: msg.Timestamp, Chat: &ch})
	go a.refreshAvatar(jid)
}

// chatShell arma un Chat con lo que el adaptador sabe de él (sin contadores ni último mensaje).
// withNetwork permite consultar el grupo al servidor si no se conoce su título.
func (a *Adapter) chatShell(ctx context.Context, c converter, jid types.JID, withNetwork bool) model.Chat {
	id := jidToNative(jid)
	ch := model.Chat{
		Key: model.NewChatKey(a.acc.ID, id), AccountID: a.acc.ID, Channel: model.ChannelWhatsApp,
		NativeID: id, Kind: chatKindOf(jid, c.self, c.selfLID),
		// -1 = «no sé cuántos no leídos hay»: el almacén conserva el contador que ya tenía.
		UnreadCount: -1,
	}
	switch ch.Kind {
	case model.ChatGroup:
		ch.Title = a.groupTitle(ctx, jid, withNetwork)
	case model.ChatChannel:
		a.mu.Lock()
		ch.Title = a.titles[id]
		a.mu.Unlock()
	case model.ChatSelf:
		ch.PeerNativeID = id
		ch.Title = "Tú"
	default:
		ch.PeerNativeID = id
		ch.Title = a.contactName(jid)
		if ch.Title == "" {
			ch.Title = phoneOf(jid)
		}
	}
	ch.ReadOnly = a.isReadOnly(id)
	f := a.local.chatFlagsOf(ctx, id)
	ch.Archived, ch.Pinned, ch.MutedUntil = f.Archived, f.Pinned, f.Muted
	return ch
}

func (a *Adapter) onUndecryptable(evt *events.UndecryptableMessage) {
	if isStatusJID(evt.Info.Chat) {
		// Un estado que no se pudo descifrar no se muestra, pero se deja constancia: es una causa posible de «no veo estados».
		a.log.Warn("no se pudo descifrar un estado", "tipo", string(evt.UnavailableType), "oculto", evt.DecryptFailMode == events.DecryptFailHide)
		return
	}
	if evt.DecryptFailMode == events.DecryptFailHide {
		return
	}
	c := a.conv()
	msg := c.convertMessage(evt.Info, nil)
	msg.Kind, msg.Text = model.KindUnsupported, "Esperando este mensaje…"
	a.noteWire(evt.Info.Chat, c.chatJID(evt.Info))
	ctx, cancel := context.WithTimeout(a.runContext(), opTimeout)
	defer cancel()
	a.deliverMessage(ctx, c, msg, nil, true)
}

func (a *Adapter) onPollVote(ctx context.Context, c converter, evt *events.Message, in incoming) {
	cli := a.getClient()
	if cli == nil {
		return
	}
	vote, err := cli.DecryptPollVote(ctx, evt)
	if err != nil {
		a.log.Debug("no se pudo descifrar un voto de encuesta", "err", redact(err.Error()))
		return
	}
	voter := jidToNative(c.senderJID(evt.Info))
	poll, ok := a.polls.vote(ctx, in.ChatID, in.PollTarget, voter, vote.GetSelectedOptions())
	if !ok {
		return
	}
	a.emit(model.Event{
		Type: model.EvMessageUpdate, ChatKey: model.NewChatKey(a.acc.ID, in.ChatID),
		Update: &model.MessageUpdate{Key: model.NewMessageKey(a.acc.ID, in.ChatID, in.PollTarget), Poll: poll},
	})
}

// ---------------------------------------------------------------------------------------------
// Personas y avatares

func (a *Adapter) onPushName(evt *events.PushName) {
	jid := a.conv().norm(evt.JID, evt.JIDAlt)
	if isStatusJID(jid) {
		return
	}
	p := a.personFor(jid)
	if p.PushName == "" {
		p.PushName = evt.NewPushName
	}
	a.emit(model.Event{Type: model.EvPerson, Person: &p})
}

func (a *Adapter) onContact(evt *events.Contact) {
	jid := a.conv().lidToPN(evt.JID)
	p := a.personFor(jid)
	if evt.Action != nil {
		name := evt.Action.GetFullName()
		if name == "" {
			name = evt.Action.GetFirstName()
		}
		p.Name = name
	}
	a.emit(model.Event{Type: model.EvPerson, Person: &p})
}

func (a *Adapter) onPicture(evt *events.Picture) {
	defer a.recoverLoop("picture")
	jid := a.conv().lidToPN(evt.JID)
	a.refreshAvatarForce(jid)
}

func (a *Adapter) refreshAvatar(jid types.JID) { a.refreshAvatarWith(jid, false) }

func (a *Adapter) refreshAvatarForce(jid types.JID) { a.refreshAvatarWith(jid, true) }

// refreshAvatarWith descarga el avatar y avisa con chat.updated y, en directos, person.updated.
func (a *Adapter) refreshAvatarWith(jid types.JID, force bool) {
	defer a.recoverLoop("avatar")
	if isStatusJID(jid) {
		return
	}
	ctx, cancel := context.WithTimeout(a.runContext(), 45*time.Second)
	defer cancel()
	file, err := a.fetchAvatar(ctx, jid, force)
	if err != nil {
		a.log.Debug("no se pudo traer el avatar", "chat", shortID(jidToNative(jid)), "err", redact(err.Error()))
		return
	}
	if file == "" && !force {
		return // no hay foto y no había una anterior que quitar
	}
	url := ""
	if file != "" {
		url = "/avatar/" + string(a.acc.ID) + "/" + file
	}
	c := a.conv()
	ch := a.chatShell(ctx, c, jid, false)
	ch.AvatarURL = url
	a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
	if ch.Kind == model.ChatDirect || ch.Kind == model.ChatSelf {
		p := a.personFor(jid)
		p.AvatarURL = url
		a.emit(model.Event{Type: model.EvPerson, Person: &p})
	}
}

// ---------------------------------------------------------------------------------------------
// Chats y grupos

// onChatFlag emite chat.updated con un cambio de archivado, fijado, silencio o leído. Los eventos
// de la sincronización completa inicial se omiten: esos datos ya llegan con el historial.
func (a *Adapter) onChatFlag(jid types.JID, fromFullSync bool, apply func(*model.Chat)) {
	if fromFullSync || isStatusJID(jid) {
		return
	}
	c := a.conv()
	jid = c.lidToPN(jid)
	ctx, cancel := context.WithTimeout(a.runContext(), opTimeout)
	defer cancel()
	ch := a.chatShell(ctx, c, jid, false)
	apply(&ch)
	a.local.saveChatFlags(ctx, ch.NativeID, chatFlags{Archived: ch.Archived, Pinned: ch.Pinned, Muted: ch.MutedUntil})
	a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
}

func (a *Adapter) onDeleteChat(evt *events.DeleteChat) {
	if evt.FromFullSync || isStatusJID(evt.JID) {
		return
	}
	jid := a.conv().lidToPN(evt.JID)
	a.emit(model.Event{Type: model.EvChatRemoved, ChatKey: model.NewChatKey(a.acc.ID, jidToNative(jid))})
}

func (a *Adapter) onGroupInfo(evt *events.GroupInfo) {
	c := a.conv()
	chatID := jidToNative(evt.JID)
	if evt.Name != nil {
		a.mu.Lock()
		a.titles[chatID] = evt.Name.Name
		a.mu.Unlock()
	}
	if evt.Name != nil || evt.Announce != nil || len(evt.Promote) > 0 || len(evt.Demote) > 0 {
		go a.refreshGroup(evt.JID)
	}
	for _, m := range c.groupSystemMessages(evt) {
		msg := m
		a.emit(model.Event{Type: model.EvMessageNew, At: msg.Timestamp, ChatKey: msg.ChatKey, Message: &msg})
	}
}

func (a *Adapter) onJoinedGroup(evt *events.JoinedGroup) {
	a.noteGroup(&evt.GroupInfo)
	c := a.conv()
	ctx, cancel := context.WithTimeout(a.runContext(), opTimeout)
	defer cancel()
	ch := a.chatShell(ctx, c, evt.JID, false)
	a.local.markChat(ctx, ch.NativeID)
	a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
	ts := evt.GroupCreated
	if ts.IsZero() {
		ts = time.Now()
	}
	text := "Te uniste al grupo"
	id := "sys-join-" + fmt.Sprint(ts.UnixMilli())
	msg := model.Message{
		Key: model.NewMessageKey(a.acc.ID, ch.NativeID, id), ChatKey: ch.Key, AccountID: a.acc.ID,
		Channel: model.ChannelWhatsApp, NativeID: id, ChatNativeID: ch.NativeID, Timestamp: time.Now().UnixMilli(),
		Kind: model.KindSystem, Text: text, Delivery: model.DeliverySent,
	}
	a.emit(model.Event{Type: model.EvMessageNew, At: msg.Timestamp, ChatKey: msg.ChatKey, Message: &msg})
	go a.refreshAvatar(evt.JID)
}

// noteGroup guarda título y permiso de escritura de un grupo.
func (a *Adapter) noteGroup(g *types.GroupInfo) {
	if g == nil {
		return
	}
	id := jidToNative(g.JID)
	c := a.conv()
	readOnly := false
	if g.IsAnnounce {
		readOnly = !isGroupAdmin(g, c.self, c.selfLID)
	}
	a.mu.Lock()
	a.titles[id] = g.Name
	a.readOnly[id] = readOnly
	a.mu.Unlock()
}

// isGroupAdmin: si yo soy administrador del grupo.
func isGroupAdmin(g *types.GroupInfo, self, selfLID types.JID) bool {
	for _, p := range g.Participants {
		mine := (!self.IsEmpty() && (p.JID.User == self.User || p.PhoneNumber.User == self.User)) ||
			(!selfLID.IsEmpty() && (p.JID.User == selfLID.User || p.LID.User == selfLID.User))
		if mine {
			return p.IsAdmin || p.IsSuperAdmin
		}
	}
	return false
}

// refreshGroup vuelve a pedir el grupo y avisa con chat.updated (título y solo lectura).
func (a *Adapter) refreshGroup(jid types.JID) {
	defer a.recoverLoop("refreshGroup")
	cli, err := a.live()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.runContext(), opTimeout)
	defer cancel()
	g, err := cli.GetGroupInfo(ctx, jid)
	if err != nil {
		return
	}
	a.noteGroup(g)
	ch := a.chatShell(ctx, a.conv(), g.JID, false)
	a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
}

// groupTitle: título conocido de un grupo; si no se conoce y se permite, lo pide (máx. 4 s).
func (a *Adapter) groupTitle(ctx context.Context, jid types.JID, withNetwork bool) string {
	id := jidToNative(jid)
	a.mu.Lock()
	t, ok := a.titles[id]
	a.mu.Unlock()
	if ok || !withNetwork {
		return t
	}
	cli, err := a.live()
	if err != nil {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	g, err := cli.GetGroupInfo(cctx, jid)
	if err != nil {
		return ""
	}
	a.noteGroup(g)
	return g.Name
}
