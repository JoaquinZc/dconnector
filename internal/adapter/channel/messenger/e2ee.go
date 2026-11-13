package messenger

// Cifrado de extremo a extremo de Messenger (09 §1.3). mautrix-meta lo hace con whatsmeow en modo Messenger
// (pkg/messagix/e2ee-*.go: registro del dispositivo con ICDC y cliente whatsmeow con MessengerConfig) y
// convierte los mensajes en pkg/msgconv (from-whatsapp.go, to-whatsapp.go). Aquí se usa lo mismo SIN el
// puente de Matrix: messagix para registrar el dispositivo y obtener el cliente, y whatsmeow directamente
// para recibir y enviar. La traducción de mensajes cifrados está en e2ee_convert.go (pura, con pruebas).
//
// Alcance: texto, imagen, vídeo, audio/voz, documento, sticker, ubicación (solo recibir), reacciones,
// ediciones, borrado para todos, leído, «escribiendo» y recibos. El historial cifrado antiguo no se pide
// (mautrix usa un PIN de «secure history», que aquí no se implementa).

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	mtypes "go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
	"go.mau.fi/whatsmeow/proto/waMsgApplication"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// e2eeState: el cliente cifrado de la cuenta.
type e2eeState struct {
	a *Adapter

	connectMu sync.Mutex // serializa conectar / cerrar / borrar (con red); mu solo guarda los campos
	mu        sync.Mutex
	container *sqlstore.Container
	cli       *whatsmeow.Client
	chats     map[string]types.JID // chat → JID cifrado (aprendido de eventos y del KV)
	ready     atomic.Bool
}

func newE2EE(a *Adapter) *e2eeState {
	return &e2eeState{a: a, chats: map[string]types.JID{}}
}

const e2eeKVPrefix = meta.KVPrefix + "e2ee."

func (a *Adapter) e2eeConv() *e2eeConverter {
	return &e2eeConverter{acc: a.Acc.ID, self: a.SelfID(), name: a.conv.Name}
}

// e2eeChat dice si un chat es cifrado y devuelve su JID.
func (a *Adapter) e2eeChat(chat string) (types.JID, bool) {
	e := a.e2ee
	if e == nil || chat == "" {
		return types.JID{}, false
	}
	e.mu.Lock()
	jid, ok := e.chats[chat]
	e.mu.Unlock()
	if ok {
		return jid, true
	}
	if b, found, err := a.KV.Get(context.Background(), e2eeKVPrefix+chat); err == nil && found {
		if j, err := types.ParseJID(string(b)); err == nil {
			e.mu.Lock()
			e.chats[chat] = j
			e.mu.Unlock()
			return j, true
		}
	}
	key, err := meta.ParseID(chat)
	if err != nil {
		return types.JID{}, false
	}
	if known, group, typ := a.conv.ThreadKnown(key); known {
		switch {
		case isEncryptedThread(typ) && group:
			return types.NewJID(chat, types.GroupServer), true
		case isEncryptedThread(typ):
			return types.NewJID(chat, types.MessengerServer), true
		}
	}
	return types.JID{}, false
}

func isEncryptedThread(t table.ThreadType) bool { return t.IsWhatsApp() }

const (
	typeEncryptedDirect = table.ENCRYPTED_OVER_WA_ONE_TO_ONE
	typeEncryptedGroup  = table.ENCRYPTED_OVER_WA_GROUP
)

func (a *Adapter) rememberE2EEChat(chat string, jid types.JID) {
	e := a.e2ee
	e.mu.Lock()
	_, had := e.chats[chat]
	e.chats[chat] = jid.ToNonAD()
	e.mu.Unlock()
	if !had {
		ctx, cancel := context.WithTimeout(context.Background(), meta.OpTimeout)
		defer cancel()
		_ = a.KV.Set(ctx, e2eeKVPrefix+chat, []byte(jid.ToNonAD().String()))
	}
}

// ---------------------------------------------------------------------------------------------
// Conexión

func (a *Adapter) e2eeLogger() *walogger { return newWALogger(a.Log) }

// e2eeConnect registra (la primera vez) el dispositivo cifrado y conecta el cliente de whatsmeow.
func (a *Adapter) e2eeConnect(ctx context.Context) {
	e := a.e2ee
	if e == nil {
		return
	}
	e.connectMu.Lock()
	defer e.connectMu.Unlock()
	e.mu.Lock()
	already := e.cli != nil
	e.mu.Unlock()
	if already {
		return
	}
	a.mu.Lock()
	mc := a.client
	a.mu.Unlock()
	if mc == nil {
		return
	}
	fail := func(what string, err error) {
		a.Log.Warn("los chats cifrados no están disponibles", "paso", what, "err", meta.RedactErr(err))
	}
	container := a.e2eeContainer()
	if err := container.Upgrade(ctx); err != nil {
		fail("almacén", err)
		return
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		fail("dispositivo", err)
		return
	}
	isNew := device.ID == nil
	if isNew && a.platformOf(ctx) != mtypes.Messenger {
		// Las sesiones de «Messenger Lite» (inicio con contraseña) no registran dispositivo cifrado aquí.
		a.Log.Info("la sesión de Messenger Lite no registra chats cifrados")
		return
	}
	mc.SetDevice(device)
	if isNew {
		rctx, cancel := context.WithTimeout(ctx, meta.OpTimeout)
		err := mc.RegisterE2EE(rctx, selfInt(a.SelfID()))
		cancel()
		if err != nil {
			fail("registro del dispositivo", err)
			return
		}
		if err := device.Save(ctx); err != nil {
			fail("guardar el dispositivo", err)
			return
		}
		a.Log.Info("dispositivo cifrado registrado")
	}
	cli, err := mc.PrepareE2EEClient()
	if err != nil {
		fail("preparar el cliente", err)
		return
	}
	cli.SynchronousAck = true
	cli.EnableDecryptedEventBuffer = true
	cli.AddEventHandlerWithSuccessStatus(a.e2eeHandle)
	if err := cli.Connect(); err != nil {
		fail("conectar", err)
		return
	}
	e.mu.Lock()
	e.cli = cli
	e.mu.Unlock()
}

// e2eeContainer devuelve el almacén de dispositivos de whatsmeow (tablas whatsmeow_* del account.db).
func (a *Adapter) e2eeContainer() *sqlstore.Container {
	e := a.e2ee
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.container == nil {
		e.container = sqlstore.NewWithDB(a.deps.DB, "sqlite", a.e2eeLogger())
	}
	return e.container
}

func (a *Adapter) e2eeStop() {
	e := a.e2ee
	if e == nil {
		return
	}
	e.mu.Lock()
	cli := e.cli
	e.cli = nil
	e.mu.Unlock()
	e.ready.Store(false)
	if cli != nil {
		cli.Disconnect()
	}
}

// e2eeReset borra el dispositivo cifrado (al cerrar la sesión): un nuevo inicio registra uno nuevo.
func (a *Adapter) e2eeReset(ctx context.Context) {
	e := a.e2ee
	if e == nil {
		return
	}
	e.connectMu.Lock()
	defer e.connectMu.Unlock()
	e.mu.Lock()
	e.chats = map[string]types.JID{}
	e.mu.Unlock()
	container := a.e2eeContainer()
	if err := container.Upgrade(ctx); err != nil {
		return
	}
	devs, err := container.GetAllDevices(ctx)
	if err != nil {
		return
	}
	for _, d := range devs {
		if err := d.Delete(ctx); err != nil {
			a.Log.Warn("no se pudo borrar el dispositivo cifrado", "err", meta.RedactErr(err))
		}
	}
}

func (a *Adapter) e2eeClient() (*whatsmeow.Client, error) {
	e := a.e2ee
	if e == nil {
		return nil, fmt.Errorf("messenger: %w", port.ErrUnsupported)
	}
	e.mu.Lock()
	cli := e.cli
	e.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return nil, fmt.Errorf("messenger: la conexión de los chats cifrados no está lista: %w", port.ErrNotConnected)
	}
	return cli, nil
}

// ---------------------------------------------------------------------------------------------
// Recibir

func (a *Adapter) e2eeHandle(raw any) bool {
	defer a.RecoverLoop("cifrado")
	switch evt := raw.(type) {
	case *events.FBMessage:
		a.e2eeOnMessage(evt)
	case *events.ChatPresence:
		if t, ok := a.e2eeConv().typingEvent(evt); ok {
			a.emitTyping(t)
		}
	case *events.Receipt:
		switch evt.Type {
		case types.ReceiptTypeReadSelf:
			a.e2eeSelfRead(evt)
		default:
			if rc, ok := a.e2eeConv().receiptEvent(evt); ok {
				a.Emit(model.Event{Type: model.EvReceipt, Receipt: &rc, ChatKey: rc.ChatKey})
			}
		}
	case *events.Connected:
		a.e2ee.ready.Store(true)
		a.Log.Debug("conectado al servidor de chats cifrados")
	case *events.Disconnected:
		a.e2ee.ready.Store(false)
	case *events.CATRefreshError:
		a.Log.Warn("no se pudo renovar el permiso de los chats cifrados", "err", meta.RedactErr(evt.Error))
	case events.PermanentDisconnect:
		a.e2ee.ready.Store(false)
		a.Log.Warn("los chats cifrados se desconectaron", "motivo", evt.PermanentDisconnectDescription())
	}
	return true
}

func (a *Adapter) e2eeOnMessage(evt *events.FBMessage) {
	out, ok := a.e2eeConv().convert(evt)
	if !ok {
		return
	}
	a.rememberE2EEChat(out.Chat, evt.Info.Chat)
	if key, err := meta.ParseID(out.Chat); err == nil {
		typ := typeEncryptedDirect
		if out.Group {
			typ = typeEncryptedGroup
		}
		if ch, known := a.conv.EnsureThread(key, out.Group, typ); !known {
			ch.UnreadCount = -1
			a.Emit(model.Event{Type: model.EvChatUpsert, Chat: &ch, ChatKey: ch.Key})
		}
	}
	switch {
	case out.Message != nil:
		m := *out.Message
		if m.QuotedNativeID != "" && m.QuotedPreview == "" {
			m.QuotedPreview, _ = a.local.Quote(context.Background(), m.ChatNativeID, m.QuotedNativeID)
		}
		a.local.Remember(m.ChatNativeID, m.NativeID)
		a.Emit(model.Event{Type: model.EvMessageNew, At: m.Timestamp, Message: &m, ChatKey: m.ChatKey})
	case out.Update != nil:
		u := *out.Update
		a.Emit(model.Event{Type: model.EvMessageUpdate, Update: &u, ChatKey: u.Key.Chat()})
	case out.Reaction != nil:
		r := *out.Reaction
		a.Emit(model.Event{Type: model.EvReaction, Reaction: &r, ChatKey: r.MessageKey.Chat()})
	}
}

// e2eeSelfRead: leí el chat desde otro dispositivo.
func (a *Adapter) e2eeSelfRead(evt *events.Receipt) {
	key, err := meta.ParseID(evt.Chat.ToNonAD().User)
	if err != nil {
		return
	}
	if ch, ok := a.conv.ChatSnapshot(key); ok {
		ch.UnreadCount = 0
		a.Emit(model.Event{Type: model.EvChatUpsert, Chat: &ch, ChatKey: ch.Key})
	}
}

// ---------------------------------------------------------------------------------------------
// Enviar

func waKey(jid types.JID, msg model.MessageRef, self string) *waCommon.MessageKey {
	k := &waCommon.MessageKey{RemoteJID: proto.String(jid.ToNonAD().String()), ID: proto.String(msg.NativeID)}
	if msg.FromMe {
		k.FromMe = proto.Bool(true)
	}
	if jid.Server == types.GroupServer {
		sender := msg.SenderNativeID
		if msg.FromMe && sender == "" {
			sender = self
		}
		if sender != "" {
			k.Participant = proto.String(types.NewJID(sender, types.MessengerServer).String())
		}
	}
	return k
}

func wrapContent(c *waConsumerApplication.ConsumerApplication_Content) *waConsumerApplication.ConsumerApplication {
	return &waConsumerApplication.ConsumerApplication{
		Payload: &waConsumerApplication.ConsumerApplication_Payload{
			Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{Content: c},
		},
	}
}

func textMessage(text string) *waCommon.MessageText { return &waCommon.MessageText{Text: &text} }

func (a *Adapter) e2eeSend(ctx context.Context, jid types.JID, chat string, out model.Outgoing) (model.Message, error) {
	cli, err := a.e2eeClient()
	if err != nil {
		return model.Message{}, err
	}
	caps := baseCapabilities(true)
	id := strconv.FormatInt(methods.GenerateEpochID(), 10)
	msg := model.Message{
		AccountID: a.Acc.ID, Channel: model.ChannelMessenger, ChatNativeID: chat,
		ChatKey: model.NewChatKey(a.Acc.ID, chat), SenderNativeID: a.SelfID(), SenderName: "Tú",
		FromMe: true, Kind: out.Kind, Text: out.Text, Delivery: model.DeliverySent, NativeID: id,
		Key: model.NewMessageKey(a.Acc.ID, chat, id),
	}
	var content waConsumerApplication.ConsumerApplication_Content
	if out.Kind == model.KindText {
		content.Content = &waConsumerApplication.ConsumerApplication_Content_MessageText{MessageText: textMessage(out.Text)}
	} else {
		file, err := meta.ReadOutgoing(out, caps)
		if err != nil {
			return model.Message{}, fmt.Errorf("messenger: %w", err)
		}
		mt := whatsmeowMediaType(out.Kind, file.Mime)
		uctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		up, err := cli.Upload(uctx, file.Data, mt)
		cancel()
		if err != nil {
			return model.Message{}, a.mapErr("subir el archivo", err)
		}
		w, h := imageSize(file.Data)
		tr := &waMediaTransport.WAMediaTransport{
			Integral: &waMediaTransport.WAMediaTransport_Integral{
				FileSHA256: up.FileSHA256, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256,
				DirectPath: &up.DirectPath, MediaKeyTimestamp: proto.Int64(time.Now().Unix()),
			},
			Ancillary: &waMediaTransport.WAMediaTransport_Ancillary{
				FileLength: proto.Uint64(uint64(len(file.Data))), Mimetype: &file.Mime, ObjectID: &up.ObjectID,
				// Messenger no muestra el medio sin miniatura con tamaño (iOS exige también ancho y alto).
				Thumbnail: &waMediaTransport.WAMediaTransport_Ancillary_Thumbnail{
					ThumbnailWidth: proto.Uint32(uint32(clamp400(w))), ThumbnailHeight: proto.Uint32(uint32(clamp400(h))),
				},
			},
		}
		caption := &waCommon.MessageText{}
		if strings.TrimSpace(out.Text) != "" {
			caption = textMessage(out.Text)
		}
		if err := setMedia(&content, out.Kind, file, tr, caption, w, h); err != nil {
			return model.Message{}, fmt.Errorf("messenger: %w", port.ErrInvalid)
		}
		msg.Media = &model.Media{ID: meta.MediaID(chat, id, 0), FileName: file.Name, Mime: file.Mime, Size: file.Size, LocalPath: out.FilePath}
	}
	app := wrapContent(&content)
	var md waMsgApplication.MessageApplication_Metadata
	if out.ReplyTo != nil && out.ReplyTo.NativeID != "" {
		sender := out.ReplyTo.SenderNativeID
		if out.ReplyTo.FromMe {
			sender = a.SelfID()
		}
		if sender == "" {
			sender = chat
		}
		md.QuotedMessage = &waMsgApplication.MessageApplication_Metadata_QuotedMessage{
			StanzaID: proto.String(out.ReplyTo.NativeID), Participant: proto.String(types.NewJID(sender, types.MessengerServer).String()),
		}
		msg.QuotedNativeID = out.ReplyTo.NativeID
		msg.QuotedPreview, msg.QuotedSender = a.local.Quote(ctx, chat, out.ReplyTo.NativeID)
	}
	resp, err := cli.SendFBMessage(ctx, jid, app, &md, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		return model.Message{}, a.mapErr("enviar el mensaje cifrado", err)
	}
	msg.Timestamp = resp.Timestamp.UnixMilli()
	if msg.Timestamp <= 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}
	a.local.Remember(chat, id)
	return msg, nil
}

func whatsmeowMediaType(kind model.ContentKind, mime string) whatsmeow.MediaType {
	switch kind {
	case model.KindImage:
		if mime == "image/gif" {
			return whatsmeow.MediaVideo // Messenger manda los GIF dentro de un mensaje de vídeo
		}
		return whatsmeow.MediaImage
	case model.KindVideo:
		return whatsmeow.MediaVideo
	case model.KindAudio, model.KindVoice:
		return whatsmeow.MediaAudio
	}
	return whatsmeow.MediaDocument
}

func imageSize(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func clamp400(v int) int {
	switch {
	case v <= 0:
		return 400
	case v > 400:
		return 400
	}
	return v
}

// setMedia pone en el contenido el submensaje de medio que corresponde.
func setMedia(c *waConsumerApplication.ConsumerApplication_Content, kind model.ContentKind, file meta.OutFile,
	tr *waMediaTransport.WAMediaTransport, caption *waCommon.MessageText, w, h int) error {
	switch {
	case kind == model.KindImage && file.Mime != "image/gif":
		m := &waConsumerApplication.ConsumerApplication_ImageMessage{Caption: caption}
		if err := m.Set(&waMediaTransport.ImageTransport{
			Integral:  &waMediaTransport.ImageTransport_Integral{Transport: tr},
			Ancillary: &waMediaTransport.ImageTransport_Ancillary{Height: proto.Uint32(uint32(h)), Width: proto.Uint32(uint32(w))},
		}); err != nil {
			return err
		}
		c.Content = &waConsumerApplication.ConsumerApplication_Content_ImageMessage{ImageMessage: m}
	case kind == model.KindImage || kind == model.KindVideo:
		m := &waConsumerApplication.ConsumerApplication_VideoMessage{Caption: caption}
		anc := &waMediaTransport.VideoTransport_Ancillary{Height: proto.Uint32(uint32(h)), Width: proto.Uint32(uint32(w))}
		if kind == model.KindImage {
			anc.GifPlayback = proto.Bool(true)
		}
		if err := m.Set(&waMediaTransport.VideoTransport{
			Integral: &waMediaTransport.VideoTransport_Integral{Transport: tr}, Ancillary: anc,
		}); err != nil {
			return err
		}
		c.Content = &waConsumerApplication.ConsumerApplication_Content_VideoMessage{VideoMessage: m}
	case kind == model.KindAudio || kind == model.KindVoice:
		m := &waConsumerApplication.ConsumerApplication_AudioMessage{PTT: proto.Bool(kind == model.KindVoice)}
		if err := m.Set(&waMediaTransport.AudioTransport{
			Integral:  &waMediaTransport.AudioTransport_Integral{Transport: tr},
			Ancillary: &waMediaTransport.AudioTransport_Ancillary{Seconds: proto.Uint32(0)},
		}); err != nil {
			return err
		}
		c.Content = &waConsumerApplication.ConsumerApplication_Content_AudioMessage{AudioMessage: m}
	default:
		m := &waConsumerApplication.ConsumerApplication_DocumentMessage{FileName: proto.String(file.Name)}
		if err := m.Set(&waMediaTransport.DocumentTransport{
			Integral:  &waMediaTransport.DocumentTransport_Integral{Transport: tr},
			Ancillary: &waMediaTransport.DocumentTransport_Ancillary{},
		}); err != nil {
			return err
		}
		c.Content = &waConsumerApplication.ConsumerApplication_Content_DocumentMessage{DocumentMessage: m}
	}
	return nil
}

func (a *Adapter) e2eeReact(ctx context.Context, jid types.JID, msg model.MessageRef, emoji string) error {
	cli, err := a.e2eeClient()
	if err != nil {
		return err
	}
	emoji = stripVariation(strings.TrimSpace(emoji))
	now := time.Now().UnixMilli()
	app := wrapContent(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_ReactionMessage{
			ReactionMessage: &waConsumerApplication.ConsumerApplication_ReactionMessage{
				Key: waKey(jid, msg, a.SelfID()), Text: proto.String(emoji), SenderTimestampMS: proto.Int64(now),
			},
		},
	})
	if _, err := cli.SendFBMessage(ctx, jid, app, nil, whatsmeow.SendRequestExtra{ID: strconv.FormatInt(methods.GenerateEpochID(), 10)}); err != nil {
		return a.mapErr("reaccionar", err)
	}
	ev := model.ReactionEvent{
		MessageKey: model.NewMessageKey(a.Acc.ID, msg.ChatNativeID, msg.NativeID),
		Reaction:   model.Reaction{SenderNativeID: a.SelfID(), SenderName: "Tú", Emoji: emoji, At: now},
	}
	a.Emit(model.Event{Type: model.EvReaction, Reaction: &ev, ChatKey: ev.MessageKey.Chat()})
	return nil
}

func (a *Adapter) e2eeEdit(ctx context.Context, jid types.JID, msg model.MessageRef, text string) error {
	cli, err := a.e2eeClient()
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	app := wrapContent(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_EditMessage{
			EditMessage: &waConsumerApplication.ConsumerApplication_EditMessage{
				Key: waKey(jid, msg, a.SelfID()), Message: textMessage(text), TimestampMS: proto.Int64(now),
			},
		},
	})
	if _, err := cli.SendFBMessage(ctx, jid, app, nil, whatsmeow.SendRequestExtra{ID: strconv.FormatInt(methods.GenerateEpochID(), 10)}); err != nil {
		return a.mapErr("editar el mensaje", err)
	}
	t := text
	u := model.MessageUpdate{Key: model.NewMessageKey(a.Acc.ID, msg.ChatNativeID, msg.NativeID), Text: &t, EditedAt: now}
	a.Emit(model.Event{Type: model.EvMessageUpdate, Update: &u, ChatKey: u.Key.Chat()})
	return nil
}

func (a *Adapter) e2eeDelete(ctx context.Context, jid types.JID, msg model.MessageRef, forEveryone bool) error {
	if !forEveryone {
		return fmt.Errorf("messenger: en los chats cifrados solo se puede borrar para todos: %w", port.ErrUnsupported)
	}
	if !msg.FromMe {
		return fmt.Errorf("messenger: solo puedes borrar para todos tus propios mensajes: %w", port.ErrInvalid)
	}
	cli, err := a.e2eeClient()
	if err != nil {
		return err
	}
	app := &waConsumerApplication.ConsumerApplication{
		Payload: &waConsumerApplication.ConsumerApplication_Payload{
			Payload: &waConsumerApplication.ConsumerApplication_Payload_ApplicationData{
				ApplicationData: &waConsumerApplication.ConsumerApplication_ApplicationData{
					ApplicationContent: &waConsumerApplication.ConsumerApplication_ApplicationData_Revoke{
						Revoke: &waConsumerApplication.ConsumerApplication_RevokeMessage{Key: waKey(jid, msg, a.SelfID())},
					},
				},
			},
		},
	}
	if _, err := cli.SendFBMessage(ctx, jid, app, nil, whatsmeow.SendRequestExtra{ID: strconv.FormatInt(methods.GenerateEpochID(), 10)}); err != nil {
		return a.mapErr("borrar el mensaje", err)
	}
	u := model.MessageUpdate{Key: model.NewMessageKey(a.Acc.ID, msg.ChatNativeID, msg.NativeID), Deleted: true}
	a.Emit(model.Event{Type: model.EvMessageUpdate, Update: &u, ChatKey: u.Key.Chat()})
	return nil
}

func (a *Adapter) e2eeMarkRead(ctx context.Context, jid types.JID, chat string, upTo []model.MessageRef) error {
	cli, err := a.e2eeClient()
	if err != nil {
		return err
	}
	bySender := map[string][]string{}
	for _, m := range upTo {
		if m.FromMe || m.NativeID == "" {
			continue
		}
		key := ""
		if jid.Server == types.GroupServer {
			key = m.SenderNativeID
		}
		bySender[key] = append(bySender[key], m.NativeID)
	}
	for sender, ids := range bySender {
		var from types.JID
		if sender != "" {
			from = types.NewJID(sender, types.MessengerServer)
		}
		if err := cli.MarkRead(ctx, ids, time.Now(), jid, from); err != nil {
			return a.mapErr("marcar como leído", err)
		}
	}
	return nil
}

func (a *Adapter) e2eeTyping(ctx context.Context, jid types.JID, typing bool) error {
	cli, err := a.e2eeClient()
	if err != nil {
		return err
	}
	state := types.ChatPresencePaused
	if typing {
		// Sin estar «disponible» WhatsApp no entrega el «escribiendo…».
		if err := cli.SendPresence(ctx, types.PresenceAvailable); err != nil {
			return a.mapErr("avisar que escribes", err)
		}
		state = types.ChatPresenceComposing
	}
	if err := cli.SendChatPresence(ctx, jid, state, types.ChatPresenceMediaText); err != nil {
		return a.mapErr("avisar que escribes", err)
	}
	return nil
}

// e2eeDownload baja y descifra un adjunto de un chat cifrado.
func (a *Adapter) e2eeDownload(ctx context.Context, h meta.Handle) (io.ReadCloser, error) {
	cli, err := a.e2eeClient()
	if err != nil {
		return nil, err
	}
	var integral waMediaTransport.WAMediaTransport_Integral
	if err := protojson.Unmarshal(h.E2EE, &integral); err != nil {
		return nil, fmt.Errorf("messenger: el archivo cifrado no tiene datos de descarga: %w", port.ErrNotFound)
	}
	data, err := cli.DownloadFB(ctx, &integral, whatsmeow.MediaType(h.Type))
	if err != nil {
		return nil, fmt.Errorf("messenger: %w", meta.ErrMediaGone)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
