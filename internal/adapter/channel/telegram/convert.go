package telegram

// Traducción pura de los tipos de gotd (tg.*) al modelo único (08 §4 y §5). Nada de aquí toca la
// red ni la base de datos: todo lo que necesita (nombres de pares, mensajes citados) entra por
// peerView y por el gancho opcional `quote`.

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/gotd/td/telegram/thumbnail"
	"github.com/gotd/td/tg"

	"dconnector/internal/core/model"
)

// converter arma el modelo con lo que sabe el adaptador en ese momento.
type converter struct {
	acc    model.AccountID
	selfID int64
	view   peerView
	// quote busca la vista previa y el emisor de un mensaje citado ya conocido (opcional).
	quote func(chatNative string, msgID int) (preview, sender string, ok bool)
}

func (c converter) v() peerView {
	if c.view == nil {
		return emptyView{}
	}
	return c.view
}

// usernameFinder lo implementan las vistas que pueden buscar un usuario por su @nombre.
type usernameFinder interface {
	UserByUsername(username string) (*tg.User, bool)
}

func (v entitiesView) UserByUsername(username string) (*tg.User, bool) {
	for _, u := range v.e.Users {
		if u != nil && strings.EqualFold(usernameOf(u), username) {
			return u, true
		}
	}
	return nil, false
}

func (c chainView) UserByUsername(username string) (*tg.User, bool) {
	for _, v := range c {
		if f, ok := v.(usernameFinder); ok {
			if u, ok := f.UserByUsername(username); ok {
				return u, true
			}
		}
	}
	return nil, false
}

func (p *peerCache) UserByUsername(username string) (*tg.User, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, u := range p.users {
		if u != nil && strings.EqualFold(usernameOf(u), username) {
			return u, true
		}
	}
	return nil, false
}

// mediaID: primeros 24 hex de sha1(chatNativeID + "/" + msgID), igual que en WhatsApp.
func mediaID(chatNativeID, msgID string) string {
	sum := sha1.Sum([]byte(chatNativeID + "/" + msgID))
	return hex.EncodeToString(sum[:])[:24]
}

// ---------------------------------------------------------------------------------------------
// Mensajes

// senderOf devuelve el emisor de un mensaje (08 §4): FromID; si falta, en privados el otro usuario o
// yo según Out; en canales de difusión, el propio canal.
func (c converter) senderOf(from, peer tg.PeerClass, out bool) string {
	if from != nil {
		if n := nativeOf(from); n != "" {
			return n
		}
	}
	if out && c.selfID != 0 {
		return userNative(c.selfID)
	}
	return nativeOf(peer)
}

func (c converter) senderNameOf(native string) string {
	if kind, id, ok := parseNative(native); ok && kind == kindUser && c.selfID != 0 && id == c.selfID {
		if n := nameOfNative(c.v(), native); n != "" {
			return n
		}
		return ""
	}
	return nameOfNative(c.v(), native)
}

// message traduce un *tg.Message al modelo (08 §5).
func (c converter) message(m *tg.Message) model.Message {
	chat := nativeOf(m.PeerID)
	id := strconv.Itoa(m.ID)
	sender := c.senderOf(m.FromID, m.PeerID, m.Out)
	out := model.Message{
		Key:            model.NewMessageKey(c.acc, chat, id),
		ChatKey:        model.NewChatKey(c.acc, chat),
		AccountID:      c.acc,
		Channel:        model.ChannelTelegram,
		NativeID:       id,
		ChatNativeID:   chat,
		SenderNativeID: sender,
		SenderName:     c.senderNameOf(sender),
		FromMe:         m.Out || (c.selfID != 0 && sender == userNative(c.selfID)),
		Timestamp:      int64(m.Date) * 1000,
		Kind:           model.KindText,
		Text:           m.Message,
		Delivery:       model.DeliverySent,
	}
	if m.EditDate > 0 && !m.EditHide {
		out.EditedAt = int64(m.EditDate) * 1000
	}
	out.Mentions = c.mentions(m.Message, m.Entities)
	c.applyReply(&out, chat, m.ReplyTo)
	if !m.FwdFrom.Zero() {
		out.Forwarded = true
	}
	c.applyMedia(&out, m)
	out.Reactions = c.reactions(m.Reactions)
	return out
}

// applyMedia rellena Kind, Media, Location, Contacts y Poll según m.Media.
func (c converter) applyMedia(out *model.Message, m *tg.Message) {
	switch md := m.Media.(type) {
	case nil, *tg.MessageMediaEmpty, *tg.MessageMediaWebPage:
		// Texto normal (la vista previa de enlaces no se traduce).
	case *tg.MessageMediaPhoto:
		if ph, ok := md.Photo.(*tg.Photo); ok {
			out.Kind = model.KindImage
			out.Media = c.photoMedia(out.ChatNativeID, m.ID, ph)
		} else {
			out.Kind = model.KindUnsupported // foto caducada o no disponible
		}
	case *tg.MessageMediaDocument:
		if doc, ok := md.Document.(*tg.Document); ok {
			kind, media := c.documentMedia(out.ChatNativeID, m.ID, doc, md)
			out.Kind, out.Media = kind, media
		} else {
			out.Kind = model.KindUnsupported
		}
	case *tg.MessageMediaGeo:
		if loc, ok := geoOf(md.Geo); ok {
			out.Kind, out.Location = model.KindLocation, &loc
		} else {
			out.Kind = model.KindUnsupported
		}
	case *tg.MessageMediaGeoLive:
		if loc, ok := geoOf(md.Geo); ok {
			loc.Live = true
			out.Kind, out.Location = model.KindLocation, &loc
		} else {
			out.Kind = model.KindUnsupported
		}
	case *tg.MessageMediaVenue:
		if loc, ok := geoOf(md.Geo); ok {
			loc.Name, loc.Address = md.Title, md.Address
			out.Kind, out.Location = model.KindLocation, &loc
		} else {
			out.Kind = model.KindUnsupported
		}
	case *tg.MessageMediaContact:
		out.Kind = model.KindContact
		out.Contacts = []model.ContactCard{contactCardOf(md)}
	case *tg.MessageMediaPoll:
		out.Kind = model.KindPoll
		out.Poll = c.pollOf(md.Poll, md.Results)
	default:
		out.Kind = model.KindUnsupported
	}
}

// geoOf: coordenadas de un punto; false si es un punto vacío.
func geoOf(g tg.GeoPointClass) (model.Location, bool) {
	p, ok := g.(*tg.GeoPoint)
	if !ok || p == nil {
		return model.Location{}, false
	}
	return model.Location{Lat: p.Lat, Lng: p.Long}, true
}

func contactCardOf(md *tg.MessageMediaContact) model.ContactCard {
	name := strings.TrimSpace(strings.TrimSpace(md.FirstName) + " " + strings.TrimSpace(md.LastName))
	phone := strings.TrimSpace(md.PhoneNumber)
	if phone != "" && !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	return model.ContactCard{Name: name, Phone: phone, VCard: md.Vcard}
}

// ---------------------------------------------------------------------------------------------
// Citas, menciones y entidades

// applyReply: cita (ReplyTo.ReplyToMsgID) o respuesta a una historia.
func (c converter) applyReply(out *model.Message, chat string, rt tg.MessageReplyHeaderClass) {
	switch rh := rt.(type) {
	case *tg.MessageReplyHeader:
		if rh.ReplyToMsgID == 0 || (rh.ForumTopic && rh.ReplyToTopID == 0) {
			return
		}
		out.QuotedNativeID = strconv.Itoa(rh.ReplyToMsgID)
		if rh.QuoteText != "" {
			out.QuotedPreview = previewText(rh.QuoteText)
		} else if rh.ReplyMedia != nil {
			var tmp model.Message
			c.applyMedia(&tmp, &tg.Message{Media: rh.ReplyMedia})
			out.QuotedPreview = model.Preview(tmp)
		}
		if !rh.ReplyFrom.Zero() {
			if rh.ReplyFrom.FromName != "" {
				out.QuotedSender = rh.ReplyFrom.FromName
			} else if rh.ReplyFrom.FromID != nil {
				out.QuotedSender = nameOfNative(c.v(), nativeOf(rh.ReplyFrom.FromID))
			}
		}
		if (out.QuotedPreview == "" || out.QuotedSender == "") && c.quote != nil {
			qchat := chat
			if rh.ReplyToPeerID != nil {
				if n := nativeOf(rh.ReplyToPeerID); n != "" {
					qchat = n
				}
			}
			if pv, snd, ok := c.quote(qchat, rh.ReplyToMsgID); ok {
				if out.QuotedPreview == "" {
					out.QuotedPreview = pv
				}
				if out.QuotedSender == "" {
					out.QuotedSender = snd
				}
			}
		}
	case *tg.MessageReplyStoryHeader:
		if n := nativeOf(rh.Peer); n != "" {
			out.FromStatusID = storyNative(n, rh.StoryID)
		}
	}
}

// previewText colapsa espacios y recorta, como model.Preview.
func previewText(s string) string {
	return model.Preview(model.Message{Text: s})
}

// mentions devuelve los ids nativos de las personas mencionadas. Las @menciones por nombre de
// usuario se resuelven con los usuarios conocidos; las que no se conocen se omiten.
func (c converter) mentions(text string, ents []tg.MessageEntityClass) []string {
	var out []string
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, e := range ents {
		switch x := e.(type) {
		case *tg.MessageEntityMentionName:
			add(userNative(x.UserID))
		case *tg.MessageEntityMention:
			un := strings.TrimPrefix(utf16Slice(text, x.Offset, x.Length), "@")
			if f, ok := c.v().(usernameFinder); ok && un != "" {
				if u, ok := f.UserByUsername(un); ok {
					add(userNative(u.ID))
				}
			}
		}
	}
	return out
}

// utf16Slice corta un texto con desplazamientos en unidades UTF-16 (las entidades de Telegram).
func utf16Slice(s string, off, length int) string {
	u := utf16.Encode([]rune(s))
	if off < 0 || length < 0 || off > len(u) {
		return ""
	}
	end := off + length
	if end > len(u) {
		end = len(u)
	}
	return string(utf16.Decode(u[off:end]))
}

// ---------------------------------------------------------------------------------------------
// Medios

// mediaHandle: dato opaco para descargar después (08 §5). Se guarda en Media.Handle como JSON.
type mediaHandle struct {
	T  string `json:"t"`            // "photo" | "doc"
	ID int64  `json:"id"`           // id de la foto o el documento
	AH int64  `json:"ah"`           // access_hash
	FR string `json:"fr"`           // file_reference en base64
	TS string `json:"ts,omitempty"` // tamaño de miniatura de la foto ("y", "x"…)
	DC int    `json:"dc,omitempty"` // centro de datos
	// Para renovar la referencia caducada:
	C  string `json:"c,omitempty"` // chat nativo del mensaje (o autor de la historia)
	M  int    `json:"m,omitempty"` // id del mensaje
	S  int    `json:"s,omitempty"` // id de la historia (si el medio es de una historia)
	Sz int64  `json:"sz,omitempty"`
}

func (h mediaHandle) encode() []byte {
	b, _ := json.Marshal(h)
	return b
}

func parseHandle(b []byte) (mediaHandle, bool) {
	var h mediaHandle
	if len(b) == 0 || json.Unmarshal(b, &h) != nil || h.ID == 0 || (h.T != "photo" && h.T != "doc") {
		return mediaHandle{}, false
	}
	return h, true
}

func (h mediaHandle) fileRef() []byte {
	b, _ := base64.StdEncoding.DecodeString(h.FR)
	return b
}

// location arma la ubicación de descarga (08 §8).
func (h mediaHandle) location() tg.InputFileLocationClass {
	if h.T == "photo" {
		return &tg.InputPhotoFileLocation{ID: h.ID, AccessHash: h.AH, FileReference: h.fileRef(), ThumbSize: h.TS}
	}
	return &tg.InputDocumentFileLocation{ID: h.ID, AccessHash: h.AH, FileReference: h.fileRef(), ThumbSize: h.TS}
}

// bestPhotoSize elige el tamaño mayor de una foto (08 §5): letra, ancho, alto y bytes.
func bestPhotoSize(sizes []tg.PhotoSizeClass) (typ string, w, h int, size int64) {
	bestArea := -1
	for _, s := range sizes {
		var t string
		var sw, sh int
		var sz int64
		switch x := s.(type) {
		case *tg.PhotoSize:
			t, sw, sh, sz = x.Type, x.W, x.H, int64(x.Size)
		case *tg.PhotoSizeProgressive:
			t, sw, sh = x.Type, x.W, x.H
			if n := len(x.Sizes); n > 0 {
				sz = int64(x.Sizes[n-1])
			}
		case *tg.PhotoCachedSize:
			t, sw, sh, sz = x.Type, x.W, x.H, int64(len(x.Bytes))
		default:
			continue
		}
		if area := sw * sh; area > bestArea {
			bestArea, typ, w, h, size = area, t, sw, sh, sz
		}
	}
	return
}

// thumbDataURL devuelve la miniatura JPEG como data URL (de una miniatura recortada o en caché).
func thumbDataURL(sizes []tg.PhotoSizeClass) string {
	var cached []byte
	for _, s := range sizes {
		switch x := s.(type) {
		case *tg.PhotoStrippedSize:
			if jpg, err := thumbnail.Expand(x.Bytes); err == nil {
				return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg)
			}
		case *tg.PhotoCachedSize:
			if cached == nil {
				cached = x.Bytes
			}
		}
	}
	if len(cached) > 0 {
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(cached)
	}
	return ""
}

func (c converter) photoMedia(chat string, msgID int, p *tg.Photo) *model.Media {
	typ, w, h, size := bestPhotoSize(p.Sizes)
	hd := mediaHandle{
		T: "photo", ID: p.ID, AH: p.AccessHash, FR: base64.StdEncoding.EncodeToString(p.FileReference),
		TS: typ, DC: p.DCID, C: chat, M: msgID, Sz: size,
	}
	return &model.Media{
		ID: mediaID(chat, strconv.Itoa(msgID)), Mime: "image/jpeg", Size: size, Width: w, Height: h,
		Thumb: thumbDataURL(p.Sizes), Handle: hd.encode(),
	}
}

// documentMedia clasifica un documento por sus atributos (08 §5): sticker, voz, audio, vídeo
// (también redondo y GIF) o documento.
func (c converter) documentMedia(chat string, msgID int, d *tg.Document, md *tg.MessageMediaDocument) (model.ContentKind, *model.Media) {
	var (
		isSticker, isVoice, isAudio, isVideo, isAnim bool
		name, title, performer                       string
		w, h                                         int
		dur                                          float64
	)
	for _, a := range d.Attributes {
		switch x := a.(type) {
		case *tg.DocumentAttributeSticker:
			isSticker = true
		case *tg.DocumentAttributeAudio:
			if x.Voice {
				isVoice = true
			} else {
				isAudio = true
			}
			dur = float64(x.Duration)
			title, performer = x.Title, x.Performer
		case *tg.DocumentAttributeVideo:
			isVideo = true
			w, h, dur = x.W, x.H, x.Duration
		case *tg.DocumentAttributeAnimated:
			isAnim = true
		case *tg.DocumentAttributeFilename:
			name = x.FileName
		case *tg.DocumentAttributeImageSize:
			if w == 0 {
				w, h = x.W, x.H
			}
		}
	}
	// Indicadores del propio medio cuando faltan atributos.
	if !isSticker && !isVoice && !isAudio && !isVideo && !isAnim {
		switch {
		case md.Voice:
			isVoice = true
		case md.Round || md.Video:
			isVideo = true
		}
	}
	kind := model.KindDocument
	switch {
	case isSticker:
		kind = model.KindSticker
	case isVoice:
		kind = model.KindVoice
	case isAudio:
		kind = model.KindAudio
	case isVideo, isAnim:
		kind = model.KindVideo
	}
	if name == "" && kind == model.KindAudio && title != "" {
		name = title
		if performer != "" {
			name = performer + " - " + title
		}
	}
	hd := mediaHandle{
		T: "doc", ID: d.ID, AH: d.AccessHash, FR: base64.StdEncoding.EncodeToString(d.FileReference),
		DC: d.DCID, C: chat, M: msgID, Sz: d.Size,
	}
	return kind, &model.Media{
		ID: mediaID(chat, strconv.Itoa(msgID)), Mime: d.MimeType, FileName: name, Size: d.Size,
		Width: w, Height: h, Duration: dur, Thumb: thumbDataURL(d.Thumbs), Handle: hd.encode(),
	}
}

// ---------------------------------------------------------------------------------------------
// Encuestas

// pollRecord: lo que se guarda de una encuesta para poder votar y acumular recuentos
// (kv["tg.poll.<chat>.<mensaje>"]).
type pollRecord struct {
	Question string       `json:"q"`
	Multi    bool         `json:"mc,omitempty"`
	Options  []pollOption `json:"o"`
}

type pollOption struct {
	Text string `json:"t"`
	Opt  []byte `json:"b"` // bytes de la opción (se codifican en base64 en el JSON)
}

// recordOfPoll saca el registro de una encuesta de gotd.
func recordOfPoll(p tg.Poll) pollRecord {
	rec := pollRecord{Question: p.Question.Text, Multi: p.MultipleChoice}
	for _, a := range p.Answers {
		if pa, ok := a.(*tg.PollAnswer); ok {
			rec.Options = append(rec.Options, pollOption{Text: pa.Text.Text, Opt: pa.Option})
		}
	}
	return rec
}

// pollOf traduce una encuesta con sus recuentos. En encuestas anónimas solo se conoce cuántas
// personas votaron cada opción: Voters se completa con marcadores "anon" hasta ese número.
func (c converter) pollOf(p tg.Poll, res tg.PollResults) *model.Poll {
	return c.pollFromRecord(recordOfPoll(p), res)
}

func (c converter) pollFromRecord(rec pollRecord, res tg.PollResults) *model.Poll {
	byOpt := map[string]tg.PollAnswerVoters{}
	for _, r := range res.Results {
		byOpt[string(r.Option)] = r
	}
	out := &model.Poll{Question: rec.Question, Multi: rec.Multi}
	for _, o := range rec.Options {
		po := model.PollOption{Name: o.Text, Voters: []string{}}
		if r, ok := byOpt[string(o.Opt)]; ok {
			seen := map[string]bool{}
			for _, pr := range r.RecentVoters {
				if n := nativeOf(pr); n != "" && !seen[n] {
					seen[n] = true
					po.Voters = append(po.Voters, n)
				}
			}
			if r.Chosen && c.selfID != 0 {
				if n := userNative(c.selfID); !seen[n] {
					seen[n] = true
					po.Voters = append(po.Voters, n)
				}
			}
			for len(po.Voters) < r.Voters {
				po.Voters = append(po.Voters, "anon")
			}
		}
		out.Options = append(out.Options, po)
	}
	return out
}

// ---------------------------------------------------------------------------------------------
// Reacciones

// emojiOf devuelve el emoji de una reacción; "" si es personalizada o de pago.
func emojiOf(r tg.ReactionClass) string {
	if e, ok := r.(*tg.ReactionEmoji); ok {
		return e.Emoticon
	}
	return ""
}

// reactions traduce las reacciones recientes de un mensaje (las que muestran quién reaccionó).
func (c converter) reactions(r tg.MessageReactions) []model.Reaction {
	var out []model.Reaction
	for _, rx := range r.RecentReactions {
		emoji := emojiOf(rx.Reaction)
		if emoji == "" {
			continue
		}
		sender := nativeOf(rx.PeerID)
		if rx.My && c.selfID != 0 {
			sender = userNative(c.selfID)
		}
		out = append(out, model.Reaction{
			SenderNativeID: sender, SenderName: c.senderNameOf(sender), Emoji: emoji, At: int64(rx.Date) * 1000,
		})
	}
	return out
}

// ---------------------------------------------------------------------------------------------
// Mensajes de servicio

// service traduce un *tg.MessageService a un mensaje de sistema (altas, bajas, título…). El
// segundo valor es false para acciones que no se muestran.
func (c converter) service(m *tg.MessageService) (model.Message, bool) {
	chat := nativeOf(m.PeerID)
	id := strconv.Itoa(m.ID)
	sender := c.senderOf(m.FromID, m.PeerID, m.Out)
	text := c.actionText(m.Action, sender)
	if text == "" || chat == "" {
		return model.Message{}, false
	}
	return model.Message{
		Key:            model.NewMessageKey(c.acc, chat, id),
		ChatKey:        model.NewChatKey(c.acc, chat),
		AccountID:      c.acc,
		Channel:        model.ChannelTelegram,
		NativeID:       id,
		ChatNativeID:   chat,
		SenderNativeID: sender,
		SenderName:     c.senderNameOf(sender),
		FromMe:         m.Out,
		Timestamp:      int64(m.Date) * 1000,
		Kind:           model.KindSystem,
		Text:           text,
		Delivery:       model.DeliverySent,
	}, true
}

func (c converter) nameOr(native, fallback string) string {
	if n := nameOfNative(c.v(), native); n != "" {
		return n
	}
	return fallback
}

func (c converter) actionText(a tg.MessageActionClass, actorNative string) string {
	actor := c.nameOr(actorNative, "Alguien")
	userNames := func(ids []int64) string {
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			names = append(names, c.nameOr(userNative(id), "alguien"))
		}
		return strings.Join(names, ", ")
	}
	switch x := a.(type) {
	case *tg.MessageActionChatCreate:
		return actor + " creó el grupo «" + x.Title + "»"
	case *tg.MessageActionChatEditTitle:
		return actor + " cambió el nombre del grupo a «" + x.Title + "»"
	case *tg.MessageActionChatAddUser:
		if len(x.Users) == 1 && userNative(x.Users[0]) == actorNative {
			return actor + " se unió al grupo"
		}
		return actor + " añadió a " + userNames(x.Users)
	case *tg.MessageActionChatDeleteUser:
		if userNative(x.UserID) == actorNative {
			return actor + " salió del grupo"
		}
		return actor + " eliminó a " + userNames([]int64{x.UserID})
	case *tg.MessageActionChatJoinedByLink:
		return actor + " se unió con un enlace de invitación"
	case *tg.MessageActionChatJoinedByRequest:
		return actor + " se unió al grupo"
	case *tg.MessageActionChannelCreate:
		return "Se creó el canal «" + x.Title + "»"
	case *tg.MessageActionChatEditPhoto:
		return actor + " cambió la foto del grupo"
	case *tg.MessageActionChatDeletePhoto:
		return actor + " eliminó la foto del grupo"
	case *tg.MessageActionPinMessage:
		return actor + " fijó un mensaje"
	case *tg.MessageActionChatMigrateTo:
		return "El grupo se convirtió en supergrupo"
	case *tg.MessageActionChannelMigrateFrom:
		return "Este supergrupo viene de un grupo"
	case *tg.MessageActionContactSignUp:
		return actor + " se unió a Telegram"
	}
	return ""
}

// ---------------------------------------------------------------------------------------------
// Escribiendo

// typingFromAction: (escribiendo, grabando) según la acción de chat de Telegram.
func typingFromAction(a tg.SendMessageActionClass) (typing, recording bool) {
	switch a.(type) {
	case *tg.SendMessageCancelAction:
		return false, false
	case *tg.SendMessageRecordAudioAction, *tg.SendMessageRecordVideoAction, *tg.SendMessageRecordRoundAction:
		return false, true
	case *tg.SpeakingInGroupCallAction, *tg.SendMessageHistoryImportAction:
		return false, false
	}
	return true, false
}

// ---------------------------------------------------------------------------------------------
// Personas y chats

// personOf traduce un usuario a Person. Con User.Contact el nombre es de agenda (Name); si no,
// es el que la persona se puso (PushName).
func personOf(acc model.AccountID, u *tg.User) model.Person {
	p := model.Person{AccountID: acc, NativeID: userNative(u.ID), Username: usernameOf(u)}
	full := userDisplay(u)
	if u.Contact {
		p.Name = full
	} else {
		p.PushName = full
	}
	if u.Phone != "" {
		p.Phone = "+" + strings.TrimPrefix(u.Phone, "+")
	}
	return p
}

// ---------------------------------------------------------------------------------------------
// Historias

// storyNative: id nativo de una historia, «<autor>/<id>» (no lleva «|» porque es el separador de
// las claves del modelo).
func storyNative(author string, storyID int) string {
	return author + "/" + strconv.Itoa(storyID)
}

// parseStoryNative separa «<autor>/<id>».
func parseStoryNative(s string) (author string, id int, ok bool) {
	i := strings.LastIndexByte(s, '/')
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return s[:i], n, true
}

// storyOf traduce una historia vigente. selfID sirve para marcar las propias.
func (c converter) storyOf(peer tg.PeerClass, it *tg.StoryItem) model.Status {
	author := nativeOf(peer)
	nid := storyNative(author, it.ID)
	st := model.Status{
		Key:            string(c.acc) + "|" + nid,
		AccountID:      c.acc,
		Channel:        model.ChannelTelegram,
		NativeID:       nid,
		AuthorNativeID: author,
		AuthorName:     c.nameOr(author, ""),
		FromMe:         it.Out || (c.selfID != 0 && author == userNative(c.selfID)),
		PostedAt:       int64(it.Date) * 1000,
		ExpiresAt:      int64(it.ExpireDate) * 1000,
		Kind:           model.KindImage,
		Text:           it.Caption,
		Delivery:       model.DeliverySent,
		ViewCount:      it.Views.ViewsCount,
		ReactionCount:  it.Views.ReactionsCount,
	}
	switch md := it.Media.(type) {
	case *tg.MessageMediaPhoto:
		if ph, ok := md.Photo.(*tg.Photo); ok {
			st.Media = c.photoMedia(author, -it.ID, ph)
			st.Media.ID = mediaID(author, "story"+strconv.Itoa(it.ID))
			setHandleStory(st.Media, author, it.ID)
		}
	case *tg.MessageMediaDocument:
		if doc, ok := md.Document.(*tg.Document); ok {
			_, media := c.documentMedia(author, -it.ID, doc, md)
			media.ID = mediaID(author, "story"+strconv.Itoa(it.ID))
			setHandleStory(media, author, it.ID)
			st.Media = media
			st.Kind = model.KindVideo
			if strings.HasPrefix(doc.MimeType, "image/") {
				st.Kind = model.KindImage
			}
		}
	}
	return st
}

// setHandleStory marca el handle como de una historia (para renovar la referencia).
func setHandleStory(m *model.Media, author string, storyID int) {
	if h, ok := parseHandle(m.Handle); ok {
		h.C, h.M, h.S = author, 0, storyID
		m.Handle = h.encode()
	}
}
