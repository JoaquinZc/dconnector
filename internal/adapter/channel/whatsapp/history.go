package whatsapp

// Historial y chats (07 §8).

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// historyBatchSize: máximo de mensajes por history.batch (07 §8).
	historyBatchSize = 500
	// historyRequestDefault y historyRequestMax: cuántos mensajes pedir al teléfono por petición. whatsmeow
	// recomienda 50 y el teléfono puede rechazar más.
	historyRequestDefault = 50
	historyRequestMax     = 50
	// anchorIDPrefix: prefijo de los ids de mensaje que genera WhatsApp (el ancla sintética lo imita).
	anchorIDPrefix = "3EB0"
)

// Valores de la sincronización completa que se piden al vincular una cuenta NUEVA (ver historyDeviceProps).
const (
	fullSyncDaysLimit   = 3650 // ~10 años
	fullSyncSizeMbLimit = 4096
	storageQuotaMb      = 8192
)

// errNoStoredMessages: el chat no tiene ningún mensaje guardado de dónde partir.
var errNoStoredMessages = fmt.Errorf("whatsapp: este chat todavía no tiene mensajes de dónde partir: %w", port.ErrInvalid)

// historyDeviceProps pide a WhatsApp, al VINCULAR un dispositivo nuevo, todo el historial que quiera
// mandar (sincronización completa, hasta ~10 años) en vez de solo los últimos meses.
//
// store.DeviceProps es global y solo se lee al registrar un dispositivo nuevo: las cuentas YA vinculadas
// no reciben esa sincronización completa (dependen de las peticiones por chat de RequestHistory) y no
// se puede forzar sin volver a vincularlas. Los campos que no se mencionan aquí se dejan como están.
func historyDeviceProps(p *waCompanionReg.DeviceProps) {
	if p == nil {
		return
	}
	p.RequireFullSync = new(true)
	if p.HistorySyncConfig == nil {
		p.HistorySyncConfig = &waCompanionReg.DeviceProps_HistorySyncConfig{}
	}
	c := p.HistorySyncConfig
	c.FullSyncDaysLimit = new(uint32(fullSyncDaysLimit))
	c.FullSyncSizeMbLimit = new(uint32(fullSyncSizeMbLimit))
	c.StorageQuotaMb = new(uint32(storageQuotaMb))
	c.SupportCallLogHistory = new(true)
	c.OnDemandReady = new(true)
	c.CompleteOnDemandReady = new(true)
}

// webParser: client.ParseWebMessage (se inyecta para poder probar sin cliente).
type webParser func(chat types.JID, w *waWeb.WebMessageInfo) (*events.Message, error)

// historyMute: MuteEndTime del historial (segundos, o un valor enorme para «siempre») → ms del modelo.
func historyMute(v uint64, now time.Time) int64 {
	if v == 0 {
		return 0
	}
	const farFuture = 4102444800 // año 2100, en segundos
	if int64(v) < 0 || v >= farFuture {
		return -1
	}
	ms := int64(v) * 1000
	if ms <= now.UnixMilli() {
		return 0
	}
	return ms
}

// badUserJID: un JID que no es de un teléfono, @lid, grupo ni canal, o cuyo usuario no es válido
// (los de teléfono y @lid son solo dígitos), no es de fiar en el historial.
func badUserJID(j types.JID) bool {
	switch j.Server {
	case types.DefaultUserServer, types.HiddenUserServer:
		return j.User == "" || nonDigits.MatchString(j.User)
	case types.GroupServer, types.NewsletterServer:
		return j.User == ""
	case types.BroadcastServer:
		return false // las difusiones y status@broadcast se descartan aparte
	}
	return true
}

// buildHistory traduce un HistorySync a lotes del modelo (07 §8). Función pura salvo por `parse`.
func (c converter) buildHistory(data *waHistorySync.HistorySync, parse webParser) []model.HistoryBatch {
	if data == nil {
		return nil
	}
	now := c.clock()
	progress := -1.0
	if data.Progress != nil {
		progress = float64(data.GetProgress()) / 100
		if progress > 1 {
			progress = 1
		}
	}

	var persons []model.Person
	for _, pn := range data.GetPushnames() {
		jid, err := types.ParseJID(pn.GetID())
		if err != nil || pn.GetPushname() == "" || badUserJID(jid) {
			continue
		}
		jid = c.lidToPN(jid)
		persons = append(persons, model.Person{
			AccountID: c.acc, NativeID: jidToNative(jid), PushName: pn.GetPushname(), Phone: phoneOf(jid),
		})
	}

	var chats []model.Chat
	var msgs []model.Message
	for _, conv := range data.GetConversations() {
		orig, err := types.ParseJID(conv.GetID())
		if err != nil || orig.IsEmpty() {
			continue
		}
		jid := orig.ToNonAD()
		if jid.Server == types.HiddenUserServer && conv.GetPnJID() != "" {
			if pn, perr := types.ParseJID(conv.GetPnJID()); perr == nil && pn.Server == types.DefaultUserServer {
				jid = pn.ToNonAD()
			}
		}
		jid = c.lidToPN(jid)
		if isStatusJID(jid) || jid.IsBroadcastList() || badUserJID(jid) {
			continue
		}
		chatID := jidToNative(jid)
		kind := chatKindOf(jid, c.self, c.selfLID)
		title := conv.GetDisplayName()
		if title == "" {
			title = conv.GetName()
		}
		if title == "" && (kind == model.ChatDirect) {
			title = c.nameOf(jid)
			if title == "" {
				title = phoneOf(jid)
			}
		}
		ch := model.Chat{
			Key: model.NewChatKey(c.acc, chatID), AccountID: c.acc, Channel: model.ChannelWhatsApp,
			NativeID: chatID, Kind: kind, Title: title, UnreadCount: int(conv.GetUnreadCount()),
			Archived: conv.GetArchived(), Pinned: conv.GetPinned() > 0,
			MutedUntil: historyMute(conv.GetMuteEndTime(), now), ReadOnly: conv.GetReadOnly(),
			LastMessageAt: int64(conv.GetConversationTimestamp()) * 1000,
		}
		if ch.UnreadCount == 0 && conv.GetMarkedAsUnread() {
			ch.UnreadCount = 1
		}
		if kind == model.ChatDirect || kind == model.ChatSelf {
			ch.PeerNativeID = chatID
		}

		var newest *model.Message
		var convMsgs []model.Message
		for _, hm := range conv.GetMessages() {
			w := hm.GetMessage()
			if w == nil {
				continue
			}
			evt, perr := parse(orig, w)
			if perr != nil || evt == nil {
				continue
			}
			// Un chat resuelto de @lid a teléfono: el mensaje se guarda en el chat resuelto.
			if jid != orig.ToNonAD() {
				evt.Info.Chat = jid
				if !evt.Info.IsGroup && !evt.Info.IsFromMe {
					evt.Info.Sender = jid
				}
			}
			in := c.classify(evt)
			if in.Kind != inMessage {
				continue
			}
			m := in.Message
			if evt.Info.IsFromMe && w.Status != nil {
				m.Delivery = deliveryFromWeb(int32(w.GetStatus()))
			}
			convMsgs = append(convMsgs, m)
		}
		for i := range convMsgs {
			if newest == nil || convMsgs[i].Timestamp > newest.Timestamp {
				newest = &convMsgs[i]
			}
		}
		if newest != nil && newest.Kind != model.KindSystem {
			if newest.Timestamp >= ch.LastMessageAt {
				ch.LastMessageAt = newest.Timestamp
				ch.LastPreview = model.Preview(*newest)
				ch.LastFromMe = newest.FromMe
			}
		}
		chats = append(chats, ch)
		msgs = append(msgs, convMsgs...)
	}

	if len(chats) == 0 && len(msgs) == 0 && len(persons) == 0 {
		return nil
	}
	var batches []model.HistoryBatch
	first := true
	for len(msgs) > 0 || first {
		n := min(len(msgs), historyBatchSize)
		b := model.HistoryBatch{Messages: msgs[:n], Progress: progress}
		if first {
			b.Chats, b.Persons = chats, persons
			first = false
		}
		batches = append(batches, b)
		msgs = msgs[n:]
	}
	return batches
}

// onHistorySync procesa un HistorySync de whatsmeow y emite history.batch por lotes.
func (a *Adapter) onHistorySync(evt *events.HistorySync) {
	defer a.recoverLoop("history")
	if evt == nil || evt.Data == nil {
		return
	}
	cli := a.getClient()
	if cli == nil {
		return
	}
	a.histMu.Lock() // mantiene en orden los lotes
	defer a.histMu.Unlock()
	c := a.conv()
	batches := c.buildHistory(evt.Data, cli.ParseWebMessage)
	ctx, cancel := context.WithTimeout(a.runContext(), opTimeout)
	defer cancel()
	for i := range batches {
		b := batches[i]
		for j := range b.Messages {
			if b.Messages[j].Poll != nil {
				b.Messages[j].Poll = a.polls.register(ctx, b.Messages[j].ChatNativeID, b.Messages[j].NativeID, b.Messages[j].Poll)
			}
		}
		for _, ch := range b.Chats {
			a.local.saveChatFlags(ctx, ch.NativeID, chatFlags{Archived: ch.Archived, Pinned: ch.Pinned, Muted: ch.MutedUntil})
			if ch.ReadOnly {
				a.setReadOnly(ch.NativeID, true)
			}
		}
		a.emit(model.Event{Type: model.EvHistoryBatch, History: &b})
	}
	// La respuesta a una petición por chat que no trae nada (no hay mensajes más antiguos) se avisa con un
	// lote vacío: así el servicio de historial sabe que el teléfono contestó y que no hay más.
	if len(batches) == 0 && evt.Data.GetSyncType() == waHistorySync.HistorySync_ON_DEMAND {
		a.emit(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{Progress: -1}})
	}
	// Estados que ya existían al vincular (INITIAL_STATUS_V3): no vienen como mensajes sueltos.
	statuses := c.statusesFromHistory(evt.Data, cli.ParseWebMessage)
	for i := range statuses {
		st := statuses[i]
		a.emit(model.Event{Type: model.EvStatusUpsert, At: st.PostedAt, Status: &st})
	}
	if n := len(evt.Data.GetStatusV3Messages()); n > 0 || len(statuses) > 0 {
		a.log.Info("estados del historial", "recibidos", n, "vigentes", len(statuses))
	}
	a.log.Debug("historial recibido", "tipo", evt.Data.GetSyncType().String(), "lotes", len(batches))
}

// RequestHistory pide al teléfono mensajes anteriores a `before` (o al más antiguo que haya en la
// base; si el chat no tiene ninguno, a un ancla sintética). La respuesta llega como HistorySync de tipo
// ON_DEMAND y pasa por onHistorySync. Necesita el teléfono en línea y devuelve en cuanto sale la petición.
func (a *Adapter) RequestHistory(ctx context.Context, chatNativeID string, before *model.MessageRef, count int) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	info, count, err := a.historyRequestInfo(ctx, chatNativeID, before, count)
	if err != nil {
		return err
	}
	if _, err := cli.SendPeerMessage(ctx, cli.BuildHistorySyncRequest(info, count)); err != nil {
		return a.mapErr("pedir mensajes anteriores", err)
	}
	return nil
}

// historyRequestInfo arma el mensaje de partida de la petición y acota la cantidad. Es la parte de
// RequestHistory que no necesita conexión.
func (a *Adapter) historyRequestInfo(ctx context.Context, chatNativeID string, before *model.MessageRef, count int) (*types.MessageInfo, int, error) {
	chat, err := nativeToJID(chatNativeID)
	if err != nil {
		return nil, 0, err
	}
	if isNewsletter(chat) || isStatusJID(chat) {
		return nil, 0, port.ErrUnsupported
	}
	if count <= 0 {
		count = historyRequestDefault
	}
	count = min(count, historyRequestMax)
	ref := model.MessageRef{ChatNativeID: chatNativeID}
	if before != nil && before.NativeID != "" {
		ref = *before
	} else {
		ref, err = a.oldestMessage(ctx, chatNativeID)
		if errors.Is(err, errNoStoredMessages) {
			ref, err = a.syntheticAnchor(ctx, chatNativeID)
		}
		if err != nil {
			return nil, 0, err
		}
	}
	ts := time.UnixMilli(ref.Timestamp)
	if ref.Timestamp <= 0 {
		ts = time.Now()
	}
	return &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: a.wireJID(chat), IsFromMe: ref.FromMe},
		ID:            ref.NativeID, Timestamp: ts,
	}, count, nil
}

// syntheticAnchor: ancla para un chat sin ningún mensaje guardado. WhatsApp pide, como punto de partida,
// el mensaje más antiguo que se conoce; si no hay ninguno se inventa uno (id con la forma de los de
// WhatsApp, recibido, con la fecha del último mensaje conocido del chat o ahora).
//
// NO SE PUDO PROBAR CON UN TELÉFONO REAL: no se sabe si el teléfono responde a un id que no conoce. Es el
// único punto que habría que cambiar si no lo hace (p. ej. pedir sin ancla o dejar el chat sin historial).
func (a *Adapter) syntheticAnchor(ctx context.Context, chatNativeID string) (model.MessageRef, error) {
	var last int64
	err := a.deps.DB.QueryRowContext(ctx, `SELECT last_message_at FROM chats WHERE native_id = ?`, chatNativeID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.MessageRef{}, fmt.Errorf("whatsapp: no se pudo leer el chat: %w", err)
	}
	if last <= 0 {
		last = time.Now().UnixMilli()
	}
	id, err := syntheticMessageID()
	if err != nil {
		return model.MessageRef{}, fmt.Errorf("whatsapp: no se pudo crear el ancla del historial: %w", err)
	}
	return model.MessageRef{ChatNativeID: chatNativeID, NativeID: id, FromMe: false, Timestamp: last}, nil
}

// syntheticMessageID devuelve un id con la forma de los de WhatsApp: «3EB0» + 28 hex al azar en mayúsculas.
func syntheticMessageID() (string, error) {
	b := make([]byte, 14)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return anchorIDPrefix + strings.ToUpper(hex.EncodeToString(b)), nil
}

// oldestMessage lee de la base de la cuenta el mensaje más antiguo del chat (tabla messages de 05).
func (a *Adapter) oldestMessage(ctx context.Context, chatNativeID string) (model.MessageRef, error) {
	var ref model.MessageRef
	var fromMe int
	err := a.deps.DB.QueryRowContext(ctx,
		`SELECT native_id, sender_native_id, from_me, ts FROM messages
		 WHERE chat_native_id = ? ORDER BY ts ASC LIMIT 1`, chatNativeID).
		Scan(&ref.NativeID, &ref.SenderNativeID, &fromMe, &ref.Timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return ref, errNoStoredMessages
	}
	if err != nil {
		return ref, fmt.Errorf("whatsapp: no se pudo leer el mensaje más antiguo: %w", err)
	}
	ref.ChatNativeID, ref.FromMe = chatNativeID, fromMe != 0
	return ref, nil
}

// SyncChats emite chat.updated de los grupos y canales que conoce la cuenta (07 §8). Los chats
// directos salen del historial y de los mensajes.
func (a *Adapter) SyncChats(ctx context.Context) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	c := a.conv()
	groups, gerr := cli.GetJoinedGroups(ctx)
	if gerr != nil {
		a.log.Debug("no se pudieron pedir los grupos", "err", redact(gerr.Error()))
	}
	for _, g := range groups {
		a.noteGroup(g)
		ch := a.chatShell(ctx, c, g.JID, false)
		a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
	}
	news, nerr := cli.GetSubscribedNewsletters(ctx)
	if nerr != nil {
		a.log.Debug("no se pudieron pedir los canales", "err", redact(nerr.Error()))
	}
	for _, n := range news {
		if n == nil {
			continue
		}
		id := jidToNative(n.ID)
		writable := n.ViewerMeta != nil && (n.ViewerMeta.Role == types.NewsletterRoleAdmin || n.ViewerMeta.Role == types.NewsletterRoleOwner)
		a.mu.Lock()
		a.titles[id] = n.ThreadMeta.Name.Text
		a.readOnly[id] = !writable
		a.mu.Unlock()
		ch := a.chatShell(ctx, c, n.ID, false)
		a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
	}
	if gerr != nil && nerr != nil {
		return a.mapErr("sincronizar los chats", gerr)
	}
	return nil
}
