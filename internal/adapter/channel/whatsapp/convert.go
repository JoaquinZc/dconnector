package whatsapp

// Funciones puras de traducción whatsmeow → modelo único (07-ADAPTADOR-WHATSAPP §5-§7).
// Nada de aquí toca la red ni la base: todo recibe estructuras de la librería y devuelve
// estructuras del modelo, para poder probarlo con protos construidos a mano.

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// statusTTL: vigencia de un estado de WhatsApp.
	statusTTL = 24 * time.Hour
	// textPreviewMaxRunes: tope del texto citado que se manda al responder.
	quotedTextMaxRunes = 200
	// systemTextEllipsis: se usa al recortar textos de sistema.
	maxSystemNameRunes = 60
)

// Tipos del primer byte de Media.Handle (07 §6).
const (
	handleImage    byte = 1
	handleVideo    byte = 2
	handleAudio    byte = 3
	handleDocument byte = 4
	handleSticker  byte = 5
)

// jidResolver devuelve el JID de teléfono de un @lid, o un JID vacío si no se conoce.
type jidResolver func(types.JID) types.JID

// converter reúne lo que las funciones puras necesitan saber de la cuenta.
type converter struct {
	acc     model.AccountID
	self    types.JID              // JID de teléfono propio, sin dispositivo
	selfLID types.JID              // JID @lid propio, sin dispositivo
	resolve jidResolver            // @lid → teléfono (client.Store.LIDs); puede ser nil
	name    func(types.JID) string // nombre para mostrar de una persona; puede ser nil
	now     func() time.Time       // reloj; nil = time.Now
	// unknown se llama con el nombre del tipo de mensaje que no se sabe traducir (para registrarlo
	// una vez por tipo); puede ser nil.
	unknown func(label string)
}

func (c converter) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// ---------------------------------------------------------------------------------------------
// JID ↔ id nativo (07 §5)

// isStatusJID: true para status@broadcast (no es un chat: va a estados).
func isStatusJID(j types.JID) bool {
	return j.User == types.StatusBroadcastJID.User && j.Server == types.BroadcastServer
}

// jidToNative: id nativo de un JID (sin dispositivo). "" si el JID está vacío.
func jidToNative(j types.JID) string {
	if j.IsEmpty() {
		return ""
	}
	return j.ToNonAD().String()
}

var nonDigits = regexp.MustCompile(`[^0-9]`)

// nativeToJID: id nativo → JID. Acepta también un número suelto («+593 99 123 4567»).
func nativeToJID(s string) (types.JID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.EmptyJID, fmt.Errorf("whatsapp: id vacío: %w", port.ErrInvalid)
	}
	if !strings.Contains(s, "@") {
		// Número suelto: solo se admiten dígitos y separadores comunes.
		cleaned := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(s)
		cleaned = strings.TrimPrefix(cleaned, "+")
		if len(cleaned) < 6 || nonDigits.MatchString(cleaned) {
			return types.EmptyJID, fmt.Errorf("whatsapp: id no válido: %w", port.ErrInvalid)
		}
		return types.NewJID(cleaned, types.DefaultUserServer), nil
	}
	j, err := types.ParseJID(s)
	if err != nil || j.IsEmpty() {
		return types.EmptyJID, fmt.Errorf("whatsapp: id no válido: %w", port.ErrInvalid)
	}
	j = j.ToNonAD()
	if j.Server == types.LegacyUserServer {
		j.Server = types.DefaultUserServer
	}
	return j, nil
}

// phoneOf: «+<dígitos>» para JIDs de teléfono; "" para el resto.
func phoneOf(j types.JID) string {
	if j.Server != types.DefaultUserServer || j.User == "" {
		return ""
	}
	return "+" + j.User
}

// chatKindOf: tipo de chat de un JID (07 §5).
func chatKindOf(j types.JID, self, selfLID types.JID) model.ChatKind {
	switch j.Server {
	case types.GroupServer:
		return model.ChatGroup
	case types.NewsletterServer:
		return model.ChatChannel
	}
	if j.User != "" && (j.Server == types.DefaultUserServer || j.Server == types.HiddenUserServer) {
		if (!self.IsEmpty() && j.User == self.User) || (!selfLID.IsEmpty() && j.User == selfLID.User) {
			return model.ChatSelf
		}
	}
	return model.ChatDirect
}

// mediaID: primeros 24 hex de sha1(chatNativeID + "/" + msgID) (07 §5).
func mediaID(chatNativeID, msgID string) string {
	sum := sha1.Sum([]byte(chatNativeID + "/" + msgID))
	return hex.EncodeToString(sum[:])[:24]
}

// shortID: últimos 4 caracteres de un id, para registros (02 §7).
func shortID(s string) string {
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	if len(s) <= 4 {
		return s
	}
	return "…" + s[len(s)-4:]
}

// ---------------------------------------------------------------------------------------------
// Resolución de identidades (@lid → teléfono, decisión C6)

// lidToPN devuelve el JID de teléfono de un @lid si se conoce; si no, el mismo JID sin dispositivo.
func (c converter) lidToPN(j types.JID) types.JID {
	j = j.ToNonAD()
	if j.Server != types.HiddenUserServer {
		return j
	}
	if !c.selfLID.IsEmpty() && j.User == c.selfLID.User && !c.self.IsEmpty() {
		return c.self.ToNonAD()
	}
	if c.resolve != nil {
		if pn := c.resolve(j); !pn.IsEmpty() && pn.Server == types.DefaultUserServer {
			return pn.ToNonAD()
		}
	}
	return j
}

// norm resuelve un JID usando, si hace falta, el JID alternativo que trae el mensaje.
func (c converter) norm(j types.JID, alt types.JID) types.JID {
	r := c.lidToPN(j)
	if r.Server == types.HiddenUserServer && alt.Server == types.DefaultUserServer && alt.User != "" {
		return alt.ToNonAD()
	}
	return r
}

// chatJID: JID del chat de un mensaje, con las reglas de 07 §5.
func (c converter) chatJID(info types.MessageInfo) types.JID {
	chat := info.Chat.ToNonAD()
	// Difusión recibida: el mensaje aparece en el chat directo con quien la envió.
	if chat.IsBroadcastList() && !isStatusJID(chat) && !info.IsFromMe {
		return c.senderJID(info)
	}
	alt := info.SenderAlt
	if info.IsFromMe {
		alt = info.RecipientAlt
	}
	if chat.Server == types.HiddenUserServer {
		return c.norm(chat, alt)
	}
	return chat
}

// senderJID: JID de quien envió, resuelto de LID.
func (c converter) senderJID(info types.MessageInfo) types.JID {
	if info.IsFromMe {
		if !c.self.IsEmpty() {
			return c.self.ToNonAD()
		}
		return c.lidToPN(info.Sender)
	}
	return c.norm(info.Sender, info.SenderAlt)
}

func (c converter) nameOf(j types.JID) string {
	if c.name == nil {
		return ""
	}
	return c.name(j)
}

// ---------------------------------------------------------------------------------------------
// Envoltorios

// unwrap desenvuelve los envoltorios que whatsmeow aún deje (07 §6): efímero, ver una vez,
// documento con pie, mensaje editado, enviado desde otro dispositivo propio, etc.
func unwrap(m *waE2E.Message) *waE2E.Message {
	for i := 0; i < 8 && m != nil; i++ {
		var next *waE2E.Message
		switch {
		case m.GetDeviceSentMessage().GetMessage() != nil:
			next = m.GetDeviceSentMessage().GetMessage()
		case m.GetBotInvokeMessage().GetMessage() != nil:
			next = m.GetBotInvokeMessage().GetMessage()
		case m.GetEphemeralMessage().GetMessage() != nil:
			next = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage().GetMessage() != nil:
			next = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2().GetMessage() != nil:
			next = m.GetViewOnceMessageV2().GetMessage()
		case m.GetViewOnceMessageV2Extension().GetMessage() != nil:
			next = m.GetViewOnceMessageV2Extension().GetMessage()
		case m.GetLottieStickerMessage().GetMessage() != nil:
			next = m.GetLottieStickerMessage().GetMessage()
		case m.GetDocumentWithCaptionMessage().GetMessage() != nil:
			next = m.GetDocumentWithCaptionMessage().GetMessage()
		case m.GetEditedMessage().GetMessage() != nil:
			next = m.GetEditedMessage().GetMessage()
		case m.GetGroupMentionedMessage().GetMessage() != nil:
			next = m.GetGroupMentionedMessage().GetMessage()
		case m.GetAssociatedChildMessage().GetMessage() != nil: // hijos de un álbum
			next = m.GetAssociatedChildMessage().GetMessage()
		case m.GetAudioStickerMessage().GetMessage() != nil:
			next = m.GetAudioStickerMessage().GetMessage()
		case m.GetSpoilerMessage().GetMessage() != nil:
			next = m.GetSpoilerMessage().GetMessage()
		case m.GetGroupStatusMessage().GetMessage() != nil:
			next = m.GetGroupStatusMessage().GetMessage()
		case m.GetGroupStatusMessageV2().GetMessage() != nil:
			next = m.GetGroupStatusMessageV2().GetMessage()
		case m.GetStatusAddYours().GetMessage() != nil:
			next = m.GetStatusAddYours().GetMessage()
		case m.GetBotForwardedMessage().GetMessage() != nil:
			next = m.GetBotForwardedMessage().GetMessage()
		}
		if next == nil {
			break
		}
		m = next
	}
	return m
}

// pollOf devuelve la creación de encuesta de cualquiera de sus versiones.
func pollOf(m *waE2E.Message) *waE2E.PollCreationMessage {
	for _, p := range []*waE2E.PollCreationMessage{
		m.GetPollCreationMessage(), m.GetPollCreationMessageV2(), m.GetPollCreationMessageV3(),
		m.GetPollCreationMessageV5(), m.GetPollCreationMessageV6(),
	} {
		if p != nil {
			return p
		}
	}
	return nil
}

// contextInfoOf: el ContextInfo del submensaje que tenga el mensaje (cita, menciones, reenviado).
func contextInfoOf(m *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case m == nil:
		return nil
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		return m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		return m.GetStickerMessage().GetContextInfo()
	case m.GetLocationMessage() != nil:
		return m.GetLocationMessage().GetContextInfo()
	case m.GetLiveLocationMessage() != nil:
		return m.GetLiveLocationMessage().GetContextInfo()
	case m.GetContactMessage() != nil:
		return m.GetContactMessage().GetContextInfo()
	case m.GetContactsArrayMessage() != nil:
		return m.GetContactsArrayMessage().GetContextInfo()
	case m.GetPtvMessage() != nil:
		return m.GetPtvMessage().GetContextInfo()
	case m.GetInstantImageMessage() != nil:
		return m.GetInstantImageMessage().GetContextInfo()
	case m.GetButtonsMessage() != nil:
		return m.GetButtonsMessage().GetContextInfo()
	case m.GetListMessage() != nil:
		return m.GetListMessage().GetContextInfo()
	case m.GetTemplateMessage() != nil:
		return m.GetTemplateMessage().GetContextInfo()
	case m.GetInteractiveMessage() != nil:
		return m.GetInteractiveMessage().GetContextInfo()
	case m.GetButtonsResponseMessage() != nil:
		return m.GetButtonsResponseMessage().GetContextInfo()
	case m.GetListResponseMessage() != nil:
		return m.GetListResponseMessage().GetContextInfo()
	case m.GetTemplateButtonReplyMessage() != nil:
		return m.GetTemplateButtonReplyMessage().GetContextInfo()
	case m.GetInteractiveResponseMessage() != nil:
		return m.GetInteractiveResponseMessage().GetContextInfo()
	case m.GetGroupInviteMessage() != nil:
		return m.GetGroupInviteMessage().GetContextInfo()
	case m.GetEventMessage() != nil:
		return m.GetEventMessage().GetContextInfo()
	case m.GetOrderMessage() != nil:
		return m.GetOrderMessage().GetContextInfo()
	case m.GetProductMessage() != nil:
		return m.GetProductMessage().GetContextInfo()
	}
	if p := pollOf(m); p != nil {
		return p.GetContextInfo()
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Contenido (07 §6)

// content: el contenido de un mensaje ya desenvuelto, sin datos de remitente ni de chat.
type content struct {
	Kind     model.ContentKind
	Text     string
	Media    *model.Media
	Location *model.Location
	Contacts []model.ContactCard
	Poll     *model.Poll
	Ctx      *waE2E.ContextInfo
}

func (c converter) content(chatID, msgID string, m *waE2E.Message) content {
	if m == nil {
		return content{Kind: model.KindUnsupported}
	}
	ct := content{Kind: model.KindUnsupported, Ctx: contextInfoOf(m)}
	switch {
	case m.Conversation != nil:
		ct.Kind, ct.Text = model.KindText, m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		ct.Kind, ct.Text = model.KindText, m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil || m.GetInstantImageMessage() != nil:
		im := m.GetImageMessage()
		if im == nil {
			im = m.GetInstantImageMessage()
		}
		ct.Kind, ct.Text = model.KindImage, im.GetCaption()
		ct.Media = &model.Media{
			ID: mediaID(chatID, msgID), Mime: im.GetMimetype(), Size: int64(im.GetFileLength()),
			Width: int(im.GetWidth()), Height: int(im.GetHeight()),
			Thumb: thumbDataURL(im.GetJPEGThumbnail(), "image/jpeg"), Handle: makeHandle(handleImage, im),
		}
	case m.GetVideoMessage() != nil || m.GetPtvMessage() != nil: // PTV: el vídeo redondo
		vi := m.GetVideoMessage()
		if vi == nil {
			vi = m.GetPtvMessage()
		}
		ct.Kind, ct.Text = model.KindVideo, vi.GetCaption()
		ct.Media = &model.Media{
			ID: mediaID(chatID, msgID), Mime: vi.GetMimetype(), Size: int64(vi.GetFileLength()),
			Width: int(vi.GetWidth()), Height: int(vi.GetHeight()), Duration: float64(vi.GetSeconds()),
			Thumb: thumbDataURL(vi.GetJPEGThumbnail(), "image/jpeg"), Handle: makeHandle(handleVideo, vi),
		}
	case m.GetAudioMessage() != nil:
		au := m.GetAudioMessage()
		ct.Kind = model.KindAudio
		if au.GetPTT() {
			ct.Kind = model.KindVoice
		}
		ct.Media = &model.Media{
			ID: mediaID(chatID, msgID), Mime: au.GetMimetype(), Size: int64(au.GetFileLength()),
			Duration: float64(au.GetSeconds()), Handle: makeHandle(handleAudio, au),
		}
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		name := d.GetFileName()
		if name == "" {
			name = d.GetTitle()
		}
		ct.Kind, ct.Text = model.KindDocument, d.GetCaption()
		ct.Media = &model.Media{
			ID: mediaID(chatID, msgID), Mime: d.GetMimetype(), FileName: name, Size: int64(d.GetFileLength()),
			Thumb: thumbDataURL(d.GetJPEGThumbnail(), "image/jpeg"), Handle: makeHandle(handleDocument, d),
		}
	case m.GetStickerMessage() != nil:
		s := m.GetStickerMessage()
		mime := s.GetMimetype()
		if mime == "" {
			mime = "image/webp"
		}
		ct.Kind = model.KindSticker
		ct.Media = &model.Media{
			ID: mediaID(chatID, msgID), Mime: mime, Size: int64(s.GetFileLength()),
			Width: int(s.GetWidth()), Height: int(s.GetHeight()),
			Thumb: thumbDataURL(s.GetPngThumbnail(), "image/png"), Handle: makeHandle(handleSticker, s),
		}
	case m.GetLocationMessage() != nil:
		l := m.GetLocationMessage()
		ct.Kind, ct.Text = model.KindLocation, l.GetComment()
		ct.Location = &model.Location{
			Lat: l.GetDegreesLatitude(), Lng: l.GetDegreesLongitude(),
			Name: l.GetName(), Address: l.GetAddress(), Live: l.GetIsLive(),
		}
	case m.GetLiveLocationMessage() != nil:
		l := m.GetLiveLocationMessage()
		ct.Kind, ct.Text = model.KindLocation, l.GetCaption()
		ct.Location = &model.Location{Lat: l.GetDegreesLatitude(), Lng: l.GetDegreesLongitude(), Live: true}
	case m.GetContactMessage() != nil:
		ct.Kind = model.KindContact
		ct.Contacts = []model.ContactCard{contactCard(m.GetContactMessage())}
	case m.GetContactsArrayMessage() != nil:
		ct.Kind = model.KindContact
		for _, cm := range m.GetContactsArrayMessage().GetContacts() {
			ct.Contacts = append(ct.Contacts, contactCard(cm))
		}
	default:
		if p := pollOf(m); p != nil {
			ct.Kind = model.KindPoll
			ct.Poll = pollModel(p)
		} else if k, text, ok := richContent(m); ok {
			ct.Kind, ct.Text = k, text
		}
	}
	return ct
}

func pollModel(p *waE2E.PollCreationMessage) *model.Poll {
	poll := &model.Poll{Question: p.GetName(), Multi: p.GetSelectableOptionsCount() != 1}
	for _, o := range p.GetOptions() {
		poll.Options = append(poll.Options, model.PollOption{Name: o.GetOptionName(), Voters: []string{}})
	}
	return poll
}

func contactCard(cm *waE2E.ContactMessage) model.ContactCard {
	return model.ContactCard{Name: cm.GetDisplayName(), Phone: phoneFromVCard(cm.GetVcard()), VCard: cm.GetVcard()}
}

// phoneFromVCard: teléfono de la primera línea TEL de una vCard, como «+<dígitos>».
func phoneFromVCard(vcard string) string {
	for _, line := range strings.Split(vcard, "\n") {
		line = strings.TrimSpace(line)
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		head, value := strings.ToUpper(line[:colon]), line[colon+1:]
		// «TEL;…», «item1.TEL;…»
		if dot := strings.IndexByte(head, '.'); dot >= 0 {
			head = head[dot+1:]
		}
		if !strings.HasPrefix(head, "TEL") {
			continue
		}
		if i := strings.Index(head, "WAID="); i >= 0 {
			rest := head[i+len("WAID="):]
			if j := strings.IndexByte(rest, ';'); j >= 0 {
				rest = rest[:j]
			}
			if d := nonDigits.ReplaceAllString(rest, ""); d != "" {
				return "+" + d
			}
		}
		if d := nonDigits.ReplaceAllString(value, ""); d != "" {
			return "+" + d
		}
	}
	return ""
}

// buildVCard: vCard mínima para enviar un contacto que no trae la suya (07 §9).
func buildVCard(name, phone string) string {
	digits := nonDigits.ReplaceAllString(phone, "")
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\nVERSION:3.0\n")
	fmt.Fprintf(&b, "N:;%s;;;\nFN:%s\n", name, name)
	if digits != "" {
		fmt.Fprintf(&b, "TEL;type=CELL;waid=%s:+%s\n", digits, digits)
	}
	b.WriteString("END:VCARD")
	return b.String()
}

func thumbDataURL(b []byte, mime string) string {
	if len(b) == 0 {
		return ""
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// ---------------------------------------------------------------------------------------------
// Mensaje completo

// convertMessage traduce un mensaje de whatsmeow al modelo (07 §6). No decide si es un
// mensaje «real»: eso lo hace classify.
func (c converter) convertMessage(info types.MessageInfo, msg *waE2E.Message) model.Message {
	return c.convertWith(info, msg, hasViewOnceWrapper(msg))
}

// convertWith es convertMessage sabiendo ya si el mensaje era de «ver una vez» (whatsmeow quita
// el envoltorio y lo deja en evt.IsViewOnce). Una foto, vídeo o audio de ese tipo no se muestra:
// sale un aviso para abrirlo en el teléfono, sin miniatura ni dato de descarga.
func (c converter) convertWith(info types.MessageInfo, msg *waE2E.Message, viewOnce bool) model.Message {
	m := unwrap(msg)
	chat := c.chatJID(info)
	chatID := jidToNative(chat)
	sender := c.senderJID(info)
	ts := info.Timestamp
	if ts.IsZero() {
		ts = c.clock()
	}
	out := model.Message{
		AccountID: c.acc, Channel: model.ChannelWhatsApp,
		Key:      model.NewMessageKey(c.acc, chatID, info.ID),
		ChatKey:  model.NewChatKey(c.acc, chatID),
		NativeID: info.ID, ChatNativeID: chatID,
		SenderNativeID: jidToNative(sender), FromMe: info.IsFromMe,
		Timestamp: ts.UnixMilli(), Delivery: model.DeliverySent,
	}
	if !info.IsFromMe {
		out.SenderName = info.PushName
	}
	ct := c.content(chatID, info.ID, m)
	out.Kind, out.Text, out.Media, out.Location, out.Contacts, out.Poll = ct.Kind, ct.Text, ct.Media, ct.Location, ct.Contacts, ct.Poll
	switch {
	case viewOnce && isMediaKind(ct.Kind):
		out.Kind, out.Text, out.Media = model.KindUnsupported, viewOnceText, nil
	case ct.Kind == model.KindUnsupported && m != nil && c.unknown != nil:
		if label := unknownTypeLabel(m); label != "" {
			c.unknown(label)
		}
	}
	if ci := ct.Ctx; ci != nil {
		if ci.GetStanzaID() != "" {
			if ci.GetRemoteJID() == types.StatusBroadcastJID.String() {
				out.FromStatusID = ci.GetStanzaID()
			} else {
				out.QuotedNativeID = ci.GetStanzaID()
				out.QuotedPreview = c.quotedPreview(chatID, ci)
				if p := ci.GetParticipant(); p != "" {
					if pj, err := types.ParseJID(p); err == nil {
						out.QuotedSender = jidToNative(c.lidToPN(pj))
					}
				}
			}
		}
		out.Forwarded = ci.GetIsForwarded()
		for _, s := range ci.GetMentionedJID() {
			if j, err := types.ParseJID(s); err == nil {
				out.Mentions = append(out.Mentions, jidToNative(c.lidToPN(j)))
			}
		}
	}
	return out
}

// quotedPreview: texto corto del mensaje citado.
func (c converter) quotedPreview(chatID string, ci *waE2E.ContextInfo) string {
	q := unwrap(ci.GetQuotedMessage())
	if q == nil {
		return ""
	}
	qc := c.content(chatID, ci.GetStanzaID(), q)
	return model.Preview(model.Message{Kind: qc.Kind, Text: qc.Text, Media: qc.Media, Contacts: qc.Contacts, Poll: qc.Poll})
}

// previewOfProto: vista previa de un mensaje guardado en la caché de reenvío y citas.
func (c converter) previewOfProto(chatID, msgID string, m *waE2E.Message) string {
	m = unwrap(m)
	if m == nil {
		return ""
	}
	ct := c.content(chatID, msgID, m)
	return model.Preview(model.Message{Kind: ct.Kind, Text: ct.Text, Media: ct.Media, Contacts: ct.Contacts, Poll: ct.Poll})
}

// ---------------------------------------------------------------------------------------------
// Clasificación de *events.Message (07 §6 y §7)

type incomingKind int

const (
	inIgnore incomingKind = iota
	inMessage
	inStatus
	inStatusRevoke
	inStatusReaction
	inReaction
	inRevoke
	inEdit
	inPollVote
)

// incoming: lo que un *events.Message significa para el modelo.
type incoming struct {
	Kind       incomingKind
	Message    model.Message        // inMessage
	Status     model.Status         // inStatus, inStatusRevoke
	Reaction   *model.ReactionEvent // inReaction
	Update     *model.MessageUpdate // inRevoke, inEdit
	StatusView *model.StatusView    // inStatusReaction
	PollTarget string               // inPollVote: id de la encuesta votada
	ChatID     string               // chat (resuelto) del evento
}

// onlySessionKeys: true si el mensaje solo trae claves de sesión (nada que mostrar).
func onlySessionKeys(m *waE2E.Message) bool {
	if m == nil {
		return true
	}
	cl := proto.Clone(m).(*waE2E.Message)
	cl.SenderKeyDistributionMessage = nil
	cl.FastRatchetKeySenderKeyDistributionMessage = nil
	cl.MessageContextInfo = nil
	return proto.Size(cl) == 0
}

// editedText: texto (o pie) de un mensaje editado.
func editedText(m *waE2E.Message) string {
	m = unwrap(m)
	switch {
	case m == nil:
		return ""
	case m.Conversation != nil:
		return m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetCaption()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetCaption()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetCaption()
	}
	return ""
}

// classify traduce un evento de mensaje. Nunca devuelve un error: lo que no entiende es inIgnore.
func (c converter) classify(evt *events.Message) incoming {
	if evt == nil {
		return incoming{}
	}
	info := evt.Info
	chat := c.chatJID(info)
	chatID := jidToNative(chat)
	m := unwrap(evt.Message)
	if m == nil {
		return incoming{}
	}
	ts := info.Timestamp
	if ts.IsZero() {
		ts = c.clock()
	}
	sender := c.senderJID(info)
	inStatusChat := isStatusJID(info.Chat)

	// Difusión propia a una lista: no es un chat que se muestre.
	if info.Chat.IsBroadcastList() && !inStatusChat && info.IsFromMe {
		return incoming{}
	}

	// Protocolo: borrado y edición; el resto se ignora.
	if pm := m.GetProtocolMessage(); pm != nil {
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			target := pm.GetKey().GetID()
			if target == "" {
				return incoming{}
			}
			if inStatusChat {
				st := model.Status{
					Key: statusKey(c.acc, target), AccountID: c.acc, Channel: model.ChannelWhatsApp, NativeID: target,
					AuthorNativeID: jidToNative(sender), FromMe: info.IsFromMe,
					DeletedAt: ts.UnixMilli(), Delivery: model.DeliverySent,
				}
				return incoming{Kind: inStatusRevoke, Status: st, ChatID: chatID}
			}
			return incoming{Kind: inRevoke, ChatID: chatID, Update: &model.MessageUpdate{
				Key: model.NewMessageKey(c.acc, chatID, target), Deleted: true,
			}}
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			target := pm.GetKey().GetID()
			if target == "" || inStatusChat {
				return incoming{}
			}
			text := editedText(pm.GetEditedMessage())
			at := ts.UnixMilli()
			if pm.GetTimestampMS() > 0 {
				at = pm.GetTimestampMS()
			}
			return incoming{Kind: inEdit, ChatID: chatID, Update: &model.MessageUpdate{
				Key: model.NewMessageKey(c.acc, chatID, target), Text: &text, EditedAt: at,
			}}
		default:
			return incoming{}
		}
	}

	// Reacción.
	if r := m.GetReactionMessage(); r != nil {
		target := r.GetKey().GetID()
		if target == "" {
			return incoming{}
		}
		at := ts.UnixMilli()
		if r.GetSenderTimestampMS() > 0 {
			at = r.GetSenderTimestampMS()
		}
		if inStatusChat {
			v := model.StatusView{
				StatusKey: statusKey(c.acc, target), ViewerID: jidToNative(sender),
				ViewerName: info.PushName, At: at, Reaction: r.GetText(),
			}
			return incoming{Kind: inStatusReaction, StatusView: &v, ChatID: chatID}
		}
		return incoming{Kind: inReaction, ChatID: chatID, Reaction: &model.ReactionEvent{
			MessageKey: model.NewMessageKey(c.acc, chatID, target),
			Reaction: model.Reaction{
				SenderNativeID: jidToNative(sender), SenderName: info.PushName, Emoji: r.GetText(), At: at,
			},
		}}
	}

	// Voto de encuesta: hay que descifrarlo con el cliente (lo hace el adaptador).
	if pu := m.GetPollUpdateMessage(); pu != nil {
		target := pu.GetPollCreationMessageKey().GetID()
		if target == "" {
			return incoming{}
		}
		return incoming{Kind: inPollVote, PollTarget: target, ChatID: chatID}
	}

	if onlySessionKeys(m) || isMetadataOnly(m) {
		return incoming{}
	}

	// Estados.
	if inStatusChat {
		if st, ok := c.status(info, m); ok {
			return incoming{Kind: inStatus, Status: st, ChatID: chatID}
		}
		return incoming{}
	}

	msg := c.convertWith(info, m, evt.IsViewOnce || hasViewOnceWrapper(evt.Message))
	// Edición recibida ya desenvuelta por whatsmeow en mensajes del historial: Info.ID es el original.
	if evt.IsEdit {
		text := editedText(m)
		return incoming{Kind: inEdit, ChatID: chatID, Update: &model.MessageUpdate{
			Key: model.NewMessageKey(c.acc, chatID, info.ID), Text: &text, EditedAt: ts.UnixMilli(),
		}}
	}
	return incoming{Kind: inMessage, Message: msg, ChatID: chatID}
}

func statusKey(acc model.AccountID, id string) string { return string(acc) + "|" + id }

// ---------------------------------------------------------------------------------------------
// Estados (07 §10)

// status traduce un mensaje de status@broadcast. ok=false si no es texto, foto ni vídeo.
func (c converter) status(info types.MessageInfo, msg *waE2E.Message) (model.Status, bool) {
	m := unwrap(msg)
	author := c.senderJID(info)
	ts := info.Timestamp
	if ts.IsZero() {
		ts = c.clock()
	}
	st := model.Status{
		Key: statusKey(c.acc, info.ID), AccountID: c.acc, Channel: model.ChannelWhatsApp, NativeID: info.ID,
		AuthorNativeID: jidToNative(author), FromMe: info.IsFromMe,
		PostedAt: ts.UnixMilli(), ExpiresAt: ts.Add(statusTTL).UnixMilli(), Delivery: model.DeliverySent,
	}
	if !info.IsFromMe {
		st.AuthorName = info.PushName
	}
	chatID := jidToNative(types.StatusBroadcastJID)
	switch {
	case m.GetExtendedTextMessage() != nil:
		et := m.GetExtendedTextMessage()
		st.Kind, st.Text = model.KindText, et.GetText()
		if et.BackgroundArgb != nil {
			st.BgColor = argbToHex(et.GetBackgroundArgb())
		}
		st.Font = int(et.GetFont())
	case m.Conversation != nil:
		st.Kind, st.Text = model.KindText, m.GetConversation()
	case m.GetImageMessage() != nil:
		ct := c.content(chatID, info.ID, m)
		st.Kind, st.Text, st.Media = model.KindImage, ct.Text, ct.Media
	case m.GetVideoMessage() != nil:
		ct := c.content(chatID, info.ID, m)
		st.Kind, st.Text, st.Media = model.KindVideo, ct.Text, ct.Media
	default:
		return st, false
	}
	return st, true
}

// argbToHex: 0xAARRGGBB → «#RRGGBB».
func argbToHex(argb uint32) string { return fmt.Sprintf("#%06X", argb&0xFFFFFF) }

// hexToArgb: «#RRGGBB» → 0xFFRRGGBB.
func hexToArgb(s string) (uint32, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 0, false
	}
	var v uint32
	if _, err := fmt.Sscanf(s, "%06x", &v); err != nil {
		return 0, false
	}
	return 0xFF000000 | v, true
}

// ---------------------------------------------------------------------------------------------
// Recibos, escribiendo, personas (07 §7)

// receiptEvents traduce un *events.Receipt. En status@broadcast, leído/reproducido son vistas.
func (c converter) receiptEvents(evt *events.Receipt) []model.Event {
	if evt == nil || len(evt.MessageIDs) == 0 {
		return nil
	}
	at := evt.Timestamp.UnixMilli()
	if evt.Timestamp.IsZero() {
		at = c.clock().UnixMilli()
	}
	if isStatusJID(evt.Chat) {
		if evt.Type != types.ReceiptTypeRead && evt.Type != types.ReceiptTypePlayed {
			return nil
		}
		if evt.IsFromMe {
			return nil
		}
		viewer := c.norm(evt.Sender, evt.SenderAlt)
		var out []model.Event
		for _, id := range evt.MessageIDs {
			out = append(out, model.Event{
				Type: model.EvStatusView, AccountID: c.acc, Channel: model.ChannelWhatsApp, At: at,
				StatusView: &model.StatusView{
					StatusKey: statusKey(c.acc, id), ViewerID: jidToNative(viewer),
					ViewerName: c.nameOf(viewer), At: at,
				},
			})
		}
		return out
	}
	var delivery model.DeliveryState
	switch evt.Type {
	case types.ReceiptTypeDelivered:
		delivery = model.DeliveryDelivered
	case types.ReceiptTypeRead:
		delivery = model.DeliveryRead
	case types.ReceiptTypePlayed:
		delivery = model.DeliveryPlayed
	default:
		return nil
	}
	if evt.IsFromMe {
		return nil // lo leí yo desde otro dispositivo
	}
	alt := evt.SenderAlt
	chat := evt.Chat.ToNonAD()
	if chat.Server == types.HiddenUserServer {
		chat = c.norm(chat, alt)
	}
	chatID := jidToNative(chat)
	reader := c.norm(evt.Sender, evt.SenderAlt)
	keys := make([]model.MessageKey, 0, len(evt.MessageIDs))
	for _, id := range evt.MessageIDs {
		keys = append(keys, model.NewMessageKey(c.acc, chatID, id))
	}
	return []model.Event{{
		Type: model.EvReceipt, AccountID: c.acc, Channel: model.ChannelWhatsApp, At: at,
		Receipt: &model.ReceiptEvent{
			ChatKey: model.NewChatKey(c.acc, chatID), MessageKeys: keys, Delivery: delivery,
			ReaderID: jidToNative(reader), At: at,
		},
	}}
}

// presenceEvent traduce *events.ChatPresence a typing (composing = escribiendo; audio = grabando).
func (c converter) presenceEvent(evt *events.ChatPresence) (model.Event, bool) {
	if evt == nil || evt.IsFromMe || isStatusJID(evt.Chat) {
		return model.Event{}, false
	}
	chat := evt.Chat.ToNonAD()
	if chat.Server == types.HiddenUserServer {
		chat = c.norm(chat, evt.SenderAlt)
	}
	peer := c.norm(evt.Sender, evt.SenderAlt)
	composing := evt.State == types.ChatPresenceComposing
	return model.Event{
		Type: model.EvTyping, AccountID: c.acc, Channel: model.ChannelWhatsApp, At: c.clock().UnixMilli(),
		Typing: &model.TypingEvent{
			ChatKey: model.NewChatKey(c.acc, jidToNative(chat)), PeerID: jidToNative(peer),
			Typing: composing, Recording: composing && evt.Media == types.ChatPresenceMediaAudio,
		},
	}, true
}

// personFrom arma la persona de un contacto de whatsmeow.
func personFrom(acc model.AccountID, j types.JID, ci types.ContactInfo, avatarURL string) model.Person {
	name := ci.FullName
	if name == "" {
		name = ci.FirstName
	}
	push := ci.PushName
	if push == "" {
		push = ci.BusinessName
	}
	return model.Person{
		AccountID: acc, NativeID: jidToNative(j), Name: name, PushName: push,
		Phone: phoneOf(j.ToNonAD()), AvatarURL: avatarURL,
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---------------------------------------------------------------------------------------------
// Grupos (07 §7): mensajes de sistema

// groupSystemMessages arma los mensajes de tipo system de un cambio de grupo.
func (c converter) groupSystemMessages(evt *events.GroupInfo) []model.Message {
	if evt == nil {
		return nil
	}
	chatID := jidToNative(evt.JID)
	ts := evt.Timestamp
	if ts.IsZero() {
		ts = c.clock()
	}
	var actorJID types.JID
	if evt.SenderPN != nil && !evt.SenderPN.IsEmpty() {
		actorJID = evt.SenderPN.ToNonAD()
	} else if evt.Sender != nil {
		actorJID = c.lidToPN(*evt.Sender)
	}
	actor := ""
	if !actorJID.IsEmpty() {
		actor = c.label(actorJID)
	}
	var texts []string
	add := func(format string, args ...any) { texts = append(texts, fmt.Sprintf(format, args...)) }

	if evt.Name != nil {
		if actor != "" {
			add("%s cambió el asunto del grupo a «%s»", actor, truncate(evt.Name.Name, 100))
		} else {
			add("Se cambió el asunto del grupo a «%s»", truncate(evt.Name.Name, 100))
		}
	}
	if evt.Topic != nil {
		if actor != "" {
			add("%s cambió la descripción del grupo", actor)
		} else {
			add("Se cambió la descripción del grupo")
		}
	}
	for _, j := range evt.Join {
		member := c.lidToPN(j)
		switch {
		case actorJID.IsEmpty() || actorJID.User == member.User:
			add("%s se unió", c.label(member))
		default:
			add("%s añadió a %s", actor, c.label(member))
		}
	}
	for _, j := range evt.Leave {
		member := c.lidToPN(j)
		switch {
		case actorJID.IsEmpty() || actorJID.User == member.User:
			add("%s salió", c.label(member))
		default:
			add("%s eliminó a %s", actor, c.label(member))
		}
	}
	for _, j := range evt.Promote {
		add("%s ahora es admin", c.label(c.lidToPN(j)))
	}
	for _, j := range evt.Demote {
		add("%s ya no es admin", c.label(c.lidToPN(j)))
	}
	if evt.Announce != nil {
		if evt.Announce.IsAnnounce {
			add("Solo los admins pueden enviar mensajes")
		} else {
			add("Todos los participantes pueden enviar mensajes")
		}
	}
	if evt.Locked != nil {
		if evt.Locked.IsLocked {
			add("Solo los admins pueden editar los datos del grupo")
		} else {
			add("Todos los participantes pueden editar los datos del grupo")
		}
	}

	out := make([]model.Message, 0, len(texts))
	for i, t := range texts {
		sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%s", chatID, ts.UnixMilli(), i, t)))
		id := "sys-" + hex.EncodeToString(sum[:])[:16]
		out = append(out, model.Message{
			Key: model.NewMessageKey(c.acc, chatID, id), ChatKey: model.NewChatKey(c.acc, chatID),
			AccountID: c.acc, Channel: model.ChannelWhatsApp, NativeID: id, ChatNativeID: chatID,
			SenderNativeID: jidToNative(actorJID), Timestamp: ts.UnixMilli() + int64(i),
			Kind: model.KindSystem, Text: t, Delivery: model.DeliverySent,
		})
	}
	return out
}

// label: nombre para mostrar de una persona; si no se sabe, su teléfono.
func (c converter) label(j types.JID) string {
	if n := c.nameOf(j); n != "" {
		return truncate(n, maxSystemNameRunes)
	}
	if p := phoneOf(j); p != "" {
		return p
	}
	return "Alguien"
}

// ---------------------------------------------------------------------------------------------
// Entrega de mensajes del historial

// deliveryFromWeb: estado de entrega de un mensaje propio del historial.
func deliveryFromWeb(status int32) model.DeliveryState {
	switch status {
	case 0:
		return model.DeliveryFailed
	case 1:
		return model.DeliveryPending
	case 2:
		return model.DeliverySent
	case 3:
		return model.DeliveryDelivered
	case 4:
		return model.DeliveryRead
	case 5:
		return model.DeliveryPlayed
	}
	return model.DeliverySent
}

// tempBanReason: motivo de un bloqueo temporal, en español.
func tempBanReason(code events.TempBanReason) string {
	switch code {
	case events.TempBanSentToTooManyPeople:
		return "enviaste demasiados mensajes a personas que no te tienen en su agenda"
	case events.TempBanBlockedByUsers:
		return "demasiadas personas te bloquearon"
	case events.TempBanCreatedTooManyGroups:
		return "creaste demasiados grupos con personas que no te tienen en su agenda"
	case events.TempBanSentTooManySameMessage:
		return "enviaste el mismo mensaje a demasiadas personas"
	case events.TempBanBroadcastList:
		return "enviaste demasiados mensajes a una lista de difusión"
	}
	return fmt.Sprintf("código %d", int(code))
}
