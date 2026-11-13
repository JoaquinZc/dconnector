package messenger

import (
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/encoding/protojson"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
)

// Traducción de los mensajes cifrados de extremo a extremo (whatsmeow en modo Messenger) al modelo único.
// Todo es puro: no toca la red ni la base. Las ids de chat son la parte de usuario del JID del chat
// (para un privado, el id de la otra persona; para un grupo, el id del grupo).

// e2eeConverter traduce eventos FBMessage.
type e2eeConverter struct {
	acc  model.AccountID
	self string
	name func(id int64) string
}

// e2eeOut es lo que sale de un mensaje cifrado: uno de Message, Update o Reaction.
type e2eeOut struct {
	Chat     string
	Group    bool
	Message  *model.Message
	Update   *model.MessageUpdate
	Reaction *model.ReactionEvent
}

func msgText(t *waCommon.MessageText) string { return t.GetText() }

// chatOf devuelve el id nativo del chat de un mensaje cifrado.
func e2eeChatID(info types.MessageInfo) string { return info.Chat.ToNonAD().User }

func (c *e2eeConverter) senderName(id string) string {
	if c.name == nil {
		return ""
	}
	var n int64
	for _, ch := range id {
		if ch < '0' || ch > '9' {
			return ""
		}
		n = n*10 + int64(ch-'0')
	}
	return c.name(n)
}

// convert traduce un mensaje cifrado; ok=false si no hay nada que entregar (señales internas).
func (c *e2eeConverter) convert(evt *events.FBMessage) (e2eeOut, bool) {
	info := evt.Info
	chat := e2eeChatID(info)
	out := e2eeOut{Chat: chat, Group: info.IsGroup || info.Chat.Server == types.GroupServer}
	sender := info.Sender.ToNonAD().User
	if info.IsFromMe && sender == "" {
		sender = c.self
	}
	fromMe := info.IsFromMe || (sender != "" && sender == c.self)
	ts := info.Timestamp.UnixMilli()

	app := evt.GetConsumerApplication()
	if app == nil {
		if evt.GetArmadillo() == nil {
			return out, false // sin contenido (p. ej. un cambio de mensajes temporales)
		}
		// Contenidos «Armadillo» (compartidos, ubicaciones, enlaces enriquecidos…): no se muestran.
		m := c.base(info, chat, sender, fromMe, ts)
		m.Kind = model.KindUnsupported
		out.Message = &m
		return out, true
	}
	payload := app.GetPayload()
	if rv := payload.GetApplicationData().GetRevoke(); rv != nil {
		out.Update = &model.MessageUpdate{Key: model.NewMessageKey(c.acc, chat, rv.GetKey().GetID()), Deleted: true}
		return out, rv.GetKey().GetID() != ""
	}
	content := payload.GetContent()
	if content == nil {
		return out, false
	}
	if ed := content.GetEditMessage(); ed != nil {
		text := msgText(ed.GetMessage())
		editedAt := ed.GetTimestampMS()
		if editedAt == 0 {
			editedAt = ts
		}
		out.Update = &model.MessageUpdate{Key: model.NewMessageKey(c.acc, chat, ed.GetKey().GetID()), Text: &text, EditedAt: editedAt}
		return out, ed.GetKey().GetID() != ""
	}
	if re := content.GetReactionMessage(); re != nil {
		at := re.GetSenderTimestampMS()
		if at == 0 {
			at = ts
		}
		out.Reaction = &model.ReactionEvent{
			MessageKey: model.NewMessageKey(c.acc, chat, re.GetKey().GetID()),
			Reaction:   model.Reaction{SenderNativeID: sender, SenderName: c.senderName(sender), Emoji: re.GetText(), At: at},
		}
		return out, re.GetKey().GetID() != ""
	}

	m := c.base(info, chat, sender, fromMe, ts)
	if q := evt.FBApplication.GetMetadata().GetQuotedMessage(); q != nil && q.GetStanzaID() != "" {
		m.QuotedNativeID = q.GetStanzaID()
		if p := q.GetParticipant(); p != "" {
			if jid, err := types.ParseJID(p); err == nil {
				m.QuotedSender = firstNonEmpty(c.senderName(jid.User), jid.User)
			}
		}
	}
	switch {
	case content.GetMessageText() != nil:
		m.Kind, m.Text = model.KindText, msgText(content.GetMessageText())
	case content.GetExtendedTextMessage() != nil:
		m.Kind, m.Text = model.KindText, msgText(content.GetExtendedTextMessage().GetText())
	case content.GetImageMessage() != nil:
		t, err := content.GetImageMessage().Decode()
		if err != nil {
			m.Kind = model.KindUnsupported
			break
		}
		c.media(&m, model.KindImage, t.GetIntegral().GetTransport(), whatsmeow.MediaImage,
			int(t.GetAncillary().GetWidth()), int(t.GetAncillary().GetHeight()), 0, "")
		m.Text = msgText(content.GetImageMessage().GetCaption())
	case content.GetVideoMessage() != nil:
		t, err := content.GetVideoMessage().Decode()
		if err != nil {
			m.Kind = model.KindUnsupported
			break
		}
		c.media(&m, model.KindVideo, t.GetIntegral().GetTransport(), whatsmeow.MediaVideo,
			int(t.GetAncillary().GetWidth()), int(t.GetAncillary().GetHeight()), float64(t.GetAncillary().GetSeconds()), "")
		m.Text = msgText(content.GetVideoMessage().GetCaption())
		if m.Media != nil && strings.HasPrefix(m.Media.Mime, "image/") {
			m.Kind = model.KindImage // Messenger manda imágenes animadas dentro de un contenedor de vídeo
		}
	case content.GetAudioMessage() != nil:
		t, err := content.GetAudioMessage().Decode()
		if err != nil {
			m.Kind = model.KindUnsupported
			break
		}
		// Las notas de voz de Messenger no siempre traen la marca de «voz»: todo audio se trata como voz.
		c.media(&m, model.KindVoice, t.GetIntegral().GetTransport(), whatsmeow.MediaAudio, 0, 0, float64(t.GetAncillary().GetSeconds()), "")
	case content.GetDocumentMessage() != nil:
		t, err := content.GetDocumentMessage().Decode()
		if err != nil {
			m.Kind = model.KindUnsupported
			break
		}
		c.media(&m, model.KindDocument, t.GetIntegral().GetTransport(), whatsmeow.MediaDocument, 0, 0, 0, content.GetDocumentMessage().GetFileName())
	case content.GetStickerMessage() != nil:
		t, err := content.GetStickerMessage().Decode()
		if err != nil || t.GetAncillary().GetReceiverFetchID() != "" {
			m.Kind = model.KindUnsupported // pegatinas que se piden aparte: no se muestran
			break
		}
		c.media(&m, model.KindSticker, t.GetIntegral().GetTransport(), whatsmeow.MediaImage,
			int(t.GetAncillary().GetWidth()), int(t.GetAncillary().GetHeight()), 0, "")
	case content.GetLocationMessage() != nil:
		loc := content.GetLocationMessage()
		m.Kind = model.KindLocation
		m.Location = &model.Location{
			Lat: loc.GetLocation().GetDegreesLatitude(), Lng: loc.GetLocation().GetDegreesLongitude(),
			Name: loc.GetLocation().GetName(), Address: loc.GetAddress(),
		}
	default:
		m.Kind = model.KindUnsupported // contactos, encuestas, ver una vez…
	}
	if m.Kind == model.KindText && strings.TrimSpace(m.Text) == "" {
		return out, false
	}
	out.Message = &m
	return out, true
}

func (c *e2eeConverter) base(info types.MessageInfo, chat, sender string, fromMe bool, ts int64) model.Message {
	m := model.Message{
		AccountID: c.acc, Channel: model.ChannelMessenger, ChatKey: model.NewChatKey(c.acc, chat),
		NativeID: info.ID, ChatNativeID: chat, SenderNativeID: sender, SenderName: c.senderName(sender),
		FromMe: fromMe, Timestamp: ts, Delivery: model.DeliverySent,
	}
	if fromMe && m.SenderName == "" {
		m.SenderName = "Tú"
	}
	m.Key = model.NewMessageKey(c.acc, chat, info.ID)
	return m
}

// media rellena el medio de un mensaje cifrado: guarda el transporte (llaves de descarga) en Media.Handle.
func (c *e2eeConverter) media(m *model.Message, kind model.ContentKind, tr *waMediaTransport.WAMediaTransport, mt whatsmeow.MediaType,
	w, h int, dur float64, fileName string) {
	if tr == nil || tr.GetIntegral() == nil {
		m.Kind = model.KindUnsupported
		return
	}
	integral, err := protojson.Marshal(tr.GetIntegral())
	if err != nil {
		m.Kind = model.KindUnsupported
		return
	}
	mimeType := tr.GetAncillary().GetMimetype()
	m.Kind = kind
	med := &model.Media{
		ID: meta.MediaID(m.ChatNativeID, m.NativeID, 0), Mime: mimeType, FileName: fileName,
		Size: int64(tr.GetAncillary().GetFileLength()), Width: w, Height: h, Duration: dur,
		Handle: meta.MakeHandle(meta.Handle{E2EE: integral, Type: string(mt), Mime: mimeType}),
	}
	if med.FileName == "" {
		med.FileName = meta.FileNameFor(kind, mimeType)
	}
	m.Media = med
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// receiptEvent traduce un recibo cifrado de mis mensajes (entregado / leído).
func (c *e2eeConverter) receiptEvent(evt *events.Receipt) (model.ReceiptEvent, bool) {
	var delivery model.DeliveryState
	switch evt.Type {
	case types.ReceiptTypeRead:
		delivery = model.DeliveryRead
	case types.ReceiptTypeDelivered:
		delivery = model.DeliveryDelivered
	default:
		return model.ReceiptEvent{}, false
	}
	chat := evt.Chat.ToNonAD().User
	if len(evt.MessageIDs) == 0 || chat == "" {
		return model.ReceiptEvent{}, false
	}
	keys := make([]model.MessageKey, len(evt.MessageIDs))
	for i, id := range evt.MessageIDs {
		keys[i] = model.NewMessageKey(c.acc, chat, id)
	}
	at := evt.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	return model.ReceiptEvent{
		ChatKey: model.NewChatKey(c.acc, chat), MessageKeys: keys, Delivery: delivery,
		ReaderID: evt.Sender.ToNonAD().User, At: at.UnixMilli(),
	}, true
}

// typingEvent traduce un «escribiendo…» cifrado.
func (c *e2eeConverter) typingEvent(evt *events.ChatPresence) (model.TypingEvent, bool) {
	chat := evt.Chat.ToNonAD().User
	sender := evt.Sender.ToNonAD().User
	if chat == "" || sender == c.self {
		return model.TypingEvent{}, false
	}
	return model.TypingEvent{
		ChatKey: model.NewChatKey(c.acc, chat), PeerID: sender,
		Typing:    evt.State == types.ChatPresenceComposing,
		Recording: evt.State == types.ChatPresenceComposing && evt.Media == types.ChatPresenceMediaAudio,
	}, true
}
