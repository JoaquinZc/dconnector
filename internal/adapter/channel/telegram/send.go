package telegram

// Envío y acciones sobre mensajes (08 §6).

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	maxTextLen      = 4096
	maxCaptionLen   = 1024
	maxPhotoBytes   = 10 << 20
	maxFileBytes    = 2000 << 20
	maxPollOptions  = 10
	maxPollQuestion = 300
)

// ---------------------------------------------------------------------------------------------
// Utilidades puras

// randID: identificador aleatorio de 64 bits para mensajes y historias.
func randID() int64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(b[:]) &^ (1 << 63))
}

// splitName separa «Nombre Apellido» en nombre y apellido.
func splitName(full string) (first, last string) {
	full = strings.TrimSpace(full)
	if i := strings.IndexAny(full, " \t"); i > 0 {
		return full[:i], strings.TrimSpace(full[i+1:])
	}
	return full, ""
}

// detectMime averigua el tipo MIME: el pedido, la extensión o el contenido.
func detectMime(given, path string) string {
	if g := strings.TrimSpace(given); g != "" {
		return g
	}
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); t != "" {
		if mt, _, err := mime.ParseMediaType(t); err == nil {
			return mt
		}
		return t
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return http.DetectContentType(buf[:n])
}

// defaultMime: tipo MIME cuando no se pudo averiguar otro.
func defaultMime(kind model.ContentKind) string {
	switch kind {
	case model.KindImage:
		return "image/jpeg"
	case model.KindVideo:
		return "video/mp4"
	case model.KindAudio:
		return "audio/mpeg"
	case model.KindVoice:
		return "audio/ogg"
	case model.KindSticker:
		return "image/webp"
	}
	return "application/octet-stream"
}

// oggOpusDuration: duración en segundos de un OGG/Opus leyendo la posición de grano de la última
// página (Opus va siempre a 48 kHz). 0 si no se puede saber.
func oggOpusDuration(data []byte) int {
	if len(data) < 27 || string(data[:4]) != "OggS" {
		return 0
	}
	if !strings.Contains(string(data[:min(len(data), 512)]), "OpusHead") {
		return 0
	}
	last := -1
	for i := len(data) - 27; i >= 0; i-- {
		if data[i] == 'O' && string(data[i:i+4]) == "OggS" {
			last = i
			break
		}
	}
	if last < 0 {
		return 0
	}
	granule := binary.LittleEndian.Uint64(data[last+6 : last+14])
	if granule == 0 || granule == ^uint64(0) {
		return 0
	}
	return int((granule + 47999) / 48000)
}

// sentMessageOf busca en la respuesta de un envío el mensaje creado (08 §6). Para
// UpdateShortSentMessage arma un mensaje con lo que trae.
func sentMessageOf(u tg.UpdatesClass, chat string, text string) (*tg.Message, tg.Entities, bool) {
	switch x := u.(type) {
	case *tg.UpdateShortSentMessage:
		peer, ok := peerFromNative(chat)
		if !ok {
			return nil, tg.Entities{}, false
		}
		m := &tg.Message{ID: x.ID, Out: true, PeerID: peer, Date: x.Date, Message: text}
		if x.Media != nil {
			m.Media = x.Media
		}
		if len(x.Entities) > 0 {
			m.Entities = x.Entities
		}
		return m, tg.Entities{}, true
	case *tg.Updates:
		return firstNewMessage(x.Updates, tg.EntitiesFrom(x.Users, x.Chats))
	case *tg.UpdatesCombined:
		return firstNewMessage(x.Updates, tg.EntitiesFrom(x.Users, x.Chats))
	}
	return nil, tg.Entities{}, false
}

func firstNewMessage(ups []tg.UpdateClass, ents tg.Entities) (*tg.Message, tg.Entities, bool) {
	for _, up := range ups {
		var mc tg.MessageClass
		switch v := up.(type) {
		case *tg.UpdateNewMessage:
			mc = v.Message
		case *tg.UpdateNewChannelMessage:
			mc = v.Message
		default:
			continue
		}
		if m, ok := mc.(*tg.Message); ok {
			return m, ents, true
		}
	}
	return nil, ents, false
}

// ---------------------------------------------------------------------------------------------
// Envío

// sendTarget valida la conexión y resuelve el par de un chat.
func (a *Adapter) sendTarget(ctx context.Context, chatNativeID string) (*tg.Client, tg.InputPeerClass, error) {
	api, err := a.liveAPI()
	if err != nil {
		return nil, nil, err
	}
	peer, err := a.peers.input(ctx, chatNativeID)
	if err != nil {
		return nil, nil, err
	}
	return api, peer, nil
}

func caption(text string) []message.StyledTextOption {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []message.StyledTextOption{styling.Plain(text)}
}

// checkOutgoing valida lo que se pide enviar antes de tocar la red.
func checkOutgoing(out model.Outgoing) error {
	switch out.Kind {
	case model.KindText, "":
		t := strings.TrimSpace(out.Text)
		if t == "" {
			return fmt.Errorf("telegram: el mensaje está vacío: %w", port.ErrInvalid)
		}
		if utf8.RuneCountInString(out.Text) > maxTextLen {
			return fmt.Errorf("telegram: el mensaje supera los %d caracteres que admite Telegram: %w", maxTextLen, port.ErrInvalid)
		}
	case model.KindImage, model.KindVideo, model.KindAudio, model.KindVoice, model.KindDocument, model.KindSticker:
		if strings.TrimSpace(out.FilePath) == "" {
			return fmt.Errorf("telegram: falta el archivo a enviar: %w", port.ErrInvalid)
		}
		if utf8.RuneCountInString(out.Text) > maxCaptionLen {
			return fmt.Errorf("telegram: el pie del archivo supera los %d caracteres que admite Telegram: %w", maxCaptionLen, port.ErrInvalid)
		}
		if out.Kind == model.KindSticker {
			ext := strings.ToLower(filepath.Ext(out.FilePath))
			if ext != ".webp" && strings.ToLower(out.Mime) != "image/webp" {
				return fmt.Errorf("telegram: el sticker debe ser .webp: %w", port.ErrInvalid)
			}
		}
	case model.KindLocation:
		if out.Location == nil {
			return fmt.Errorf("telegram: falta la ubicación: %w", port.ErrInvalid)
		}
		if out.Location.Lat < -90 || out.Location.Lat > 90 || out.Location.Lng < -180 || out.Location.Lng > 180 {
			return fmt.Errorf("telegram: las coordenadas no son válidas: %w", port.ErrInvalid)
		}
	case model.KindContact:
		if out.Contact == nil || (strings.TrimSpace(out.Contact.Phone) == "" && strings.TrimSpace(out.Contact.VCard) == "") {
			return fmt.Errorf("telegram: falta el teléfono del contacto: %w", port.ErrInvalid)
		}
	case model.KindPoll:
		p := out.Poll
		if p == nil || strings.TrimSpace(p.Question) == "" {
			return fmt.Errorf("telegram: falta la pregunta de la encuesta: %w", port.ErrInvalid)
		}
		if utf8.RuneCountInString(p.Question) > maxPollQuestion {
			return fmt.Errorf("telegram: la pregunta de la encuesta es demasiado larga: %w", port.ErrInvalid)
		}
		if len(p.Options) < 2 || len(p.Options) > maxPollOptions {
			return fmt.Errorf("telegram: la encuesta necesita entre 2 y %d opciones: %w", maxPollOptions, port.ErrInvalid)
		}
		for _, o := range p.Options {
			if strings.TrimSpace(o.Name) == "" {
				return fmt.Errorf("telegram: hay una opción de la encuesta vacía: %w", port.ErrInvalid)
			}
		}
	default:
		return fmt.Errorf("telegram: no se puede enviar ese tipo de mensaje: %w", port.ErrUnsupported)
	}
	return nil
}

// Send envía un mensaje de cualquiera de los tipos que admite Telegram.
func (a *Adapter) Send(ctx context.Context, chatNativeID string, out model.Outgoing) (model.Message, error) {
	if err := checkOutgoing(out); err != nil {
		return model.Message{}, err
	}
	api, peer, err := a.sendTarget(ctx, chatNativeID)
	if err != nil {
		return model.Message{}, err
	}
	b := message.NewSender(api).To(peer).CloneBuilder()
	if out.ReplyTo != nil {
		if id, e := strconv.Atoi(out.ReplyTo.NativeID); e == nil && id > 0 {
			b = b.Reply(id)
		}
	}
	var upd tg.UpdatesClass
	switch out.Kind {
	case model.KindText, "":
		upd, err = b.Text(ctx, out.Text)
	case model.KindLocation:
		upd, err = a.sendLocation(ctx, b, out.Location)
	case model.KindContact:
		first, last := splitName(out.Contact.Name)
		if first == "" {
			first = out.Contact.Phone
		}
		upd, err = b.Media(ctx, message.Contact(tg.InputMediaContact{
			PhoneNumber: strings.TrimSpace(out.Contact.Phone), FirstName: first, LastName: last, Vcard: out.Contact.VCard,
		}))
	case model.KindPoll:
		upd, err = a.sendPoll(ctx, b, out.Poll)
	default:
		upd, err = a.sendFile(ctx, api, b, out)
	}
	if err != nil {
		return model.Message{}, a.mapErr("enviar el mensaje", err)
	}
	m, ents, ok := sentMessageOf(upd, chatNativeID, out.Text)
	if !ok {
		return model.Message{}, fmt.Errorf("telegram: Telegram no devolvió el mensaje enviado: %w", port.ErrNotFound)
	}
	c := a.conv(&ents)
	msg := c.message(m)
	msg.FromMe, msg.Delivery = true, model.DeliverySent
	if msg.SenderNativeID == "" || msg.SenderNativeID == msg.ChatNativeID {
		if id := a.selfID.Load(); id != 0 {
			msg.SenderNativeID = userNative(id)
		}
	}
	if msg.Media != nil && out.FilePath != "" {
		msg.Media.LocalPath = out.FilePath // ya está en disco
	}
	if msg.ChatNativeID == "" {
		msg.ChatNativeID, msg.ChatKey = chatNativeID, model.NewChatKey(a.acc.ID, chatNativeID)
		msg.Key = model.NewMessageKey(a.acc.ID, chatNativeID, msg.NativeID)
	}
	a.rememberPoll(ctx, msg.ChatNativeID, m)
	a.local.noteQuote(msg)
	a.local.indexMsgChats(ctx, []model.Message{msg})
	a.log.Debug("mensaje enviado", "chat", shortID(chatNativeID), "kind", string(out.Kind))
	return msg, nil
}

func (a *Adapter) sendLocation(ctx context.Context, b *message.Builder, l *model.Location) (tg.UpdatesClass, error) {
	if l.Name != "" || l.Address != "" {
		return b.Media(ctx, message.Venue(l.Lat, l.Lng, 0, l.Name, l.Address))
	}
	return b.Media(ctx, message.GeoPoint(l.Lat, l.Lng, 0))
}

// sendPoll envía una encuesta. Las opciones de la respuesta (con sus bytes) las guarda Send con
// rememberPoll al conocer el id del mensaje.
func (a *Adapter) sendPoll(ctx context.Context, b *message.Builder, p *model.Poll) (tg.UpdatesClass, error) {
	opts := make([]message.PollAnswerOption, 0, len(p.Options))
	for _, o := range p.Options {
		opts = append(opts, message.PollAnswer(o.Name))
	}
	return b.Media(ctx, message.Poll(p.Question, opts[0], opts[1], opts[2:]...).MultipleChoice(p.Multi))
}

// sendFile sube el archivo y lo envía según el tipo (08 §6).
func (a *Adapter) sendFile(ctx context.Context, api *tg.Client, b *message.Builder, out model.Outgoing) (tg.UpdatesClass, error) {
	fi, err := os.Stat(out.FilePath)
	if err != nil || fi.IsDir() {
		return nil, fmt.Errorf("telegram: no se pudo abrir el archivo: %w", port.ErrNotFound)
	}
	if fi.Size() > maxFileBytes {
		return nil, fmt.Errorf("telegram: el archivo supera los 2 GB que admite Telegram: %w", port.ErrInvalid)
	}
	mt := detectMime(out.Mime, out.FilePath)
	if mt == "" || mt == "application/octet-stream" {
		mt = defaultMime(out.Kind)
	}
	name := out.FileName
	if name == "" {
		name = filepath.Base(out.FilePath)
	}
	kind := out.Kind
	// Una imagen mayor de 10 MB va como documento.
	if kind == model.KindImage && fi.Size() > maxPhotoBytes {
		kind = model.KindDocument
	}
	if kind == model.KindVoice {
		mt = "audio/ogg"
	}
	if kind == model.KindSticker {
		mt = "image/webp"
	}
	up := uploader.NewUploader(api)
	f, err := up.FromPath(ctx, out.FilePath)
	if err != nil {
		return nil, err
	}
	cap := caption(out.Text)
	switch kind {
	case model.KindImage:
		return b.Media(ctx, message.UploadedPhoto(f, cap...))
	case model.KindVideo:
		return b.Media(ctx, message.UploadedDocument(f, cap...).MIME(mt).Filename(name).Video().SupportsStreaming())
	case model.KindVoice:
		vb := message.UploadedDocument(f, cap...).MIME(mt).Voice()
		if data, rerr := os.ReadFile(out.FilePath); rerr == nil {
			if d := oggOpusDuration(data); d > 0 {
				vb = vb.DurationSeconds(d)
			}
		}
		return b.Media(ctx, vb)
	case model.KindAudio:
		return b.Media(ctx, message.UploadedDocument(f, cap...).MIME(mt).Filename(name).Audio())
	case model.KindSticker:
		return b.Media(ctx, message.UploadedDocument(f, cap...).MIME(mt).Filename(name).UploadedSticker())
	}
	return b.Media(ctx, message.UploadedDocument(f, cap...).MIME(mt).Filename(name).ForceFile(true))
}

// ---------------------------------------------------------------------------------------------
// Acciones sobre mensajes

func msgID(ref model.MessageRef) (int, error) {
	id, err := strconv.Atoi(ref.NativeID)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("telegram: identificador de mensaje no válido: %w", port.ErrInvalid)
	}
	return id, nil
}

// React pone o quita (emoji == "") la reacción propia.
func (a *Adapter) React(ctx context.Context, msg model.MessageRef, emoji string) error {
	api, peer, err := a.sendTarget(ctx, msg.ChatNativeID)
	if err != nil {
		return err
	}
	id, err := msgID(msg)
	if err != nil {
		return err
	}
	req := &tg.MessagesSendReactionRequest{Peer: peer, MsgID: id, Reaction: []tg.ReactionClass{}}
	if emoji != "" {
		req.Reaction = []tg.ReactionClass{&tg.ReactionEmoji{Emoticon: emoji}}
	}
	if _, err := api.MessagesSendReaction(ctx, req); err != nil {
		return a.mapErr("reaccionar", err)
	}
	// La reacción propia no siempre vuelve como actualización con quién reaccionó: se anota aquí.
	if self := a.selfID.Load(); self != 0 {
		now := time.Now().UnixMilli()
		a.emit(model.Event{
			Type: model.EvReaction, At: now, ChatKey: model.NewChatKey(a.acc.ID, msg.ChatNativeID),
			Reaction: &model.ReactionEvent{
				MessageKey: model.NewMessageKey(a.acc.ID, msg.ChatNativeID, msg.NativeID),
				Reaction: model.Reaction{
					SenderNativeID: userNative(self), SenderName: a.conv(nil).senderNameOf(userNative(self)), Emoji: emoji, At: now,
				},
			},
		})
	}
	return nil
}

// Edit cambia el texto de un mensaje propio.
func (a *Adapter) Edit(ctx context.Context, msg model.MessageRef, newText string) error {
	if strings.TrimSpace(newText) == "" {
		return fmt.Errorf("telegram: el mensaje está vacío: %w", port.ErrInvalid)
	}
	if utf8.RuneCountInString(newText) > maxTextLen {
		return fmt.Errorf("telegram: el mensaje supera los %d caracteres que admite Telegram: %w", maxTextLen, port.ErrInvalid)
	}
	api, peer, err := a.sendTarget(ctx, msg.ChatNativeID)
	if err != nil {
		return err
	}
	id, err := msgID(msg)
	if err != nil {
		return err
	}
	if _, err := api.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{Peer: peer, ID: id, Message: newText}); err != nil {
		if isTg(err, "MESSAGE_NOT_MODIFIED") {
			return nil
		}
		return a.mapErr("editar el mensaje", err)
	}
	return nil
}

// Delete borra un mensaje; en canales y supergrupos siempre es para todos.
func (a *Adapter) Delete(ctx context.Context, msg model.MessageRef, forEveryone bool) error {
	api, _, err := a.sendTarget(ctx, msg.ChatNativeID)
	if err != nil {
		return err
	}
	id, err := msgID(msg)
	if err != nil {
		return err
	}
	if kind, _, _ := parseNative(msg.ChatNativeID); kind == kindChannel {
		ch, err := a.peers.inputChannel(ctx, msg.ChatNativeID)
		if err != nil {
			return err
		}
		if _, err := api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{Channel: ch, ID: []int{id}}); err != nil {
			return a.mapErr("borrar el mensaje", err)
		}
		return nil
	}
	if _, err := api.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{ID: []int{id}, Revoke: forEveryone}); err != nil {
		return a.mapErr("borrar el mensaje", err)
	}
	return nil
}

// Forward reenvía un mensaje a otro chat.
func (a *Adapter) Forward(ctx context.Context, msg model.MessageRef, toChatNativeID string) (model.Message, error) {
	api, from, err := a.sendTarget(ctx, msg.ChatNativeID)
	if err != nil {
		return model.Message{}, err
	}
	to, err := a.peers.input(ctx, toChatNativeID)
	if err != nil {
		return model.Message{}, err
	}
	id, err := msgID(msg)
	if err != nil {
		return model.Message{}, err
	}
	upd, err := api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: from, ToPeer: to, ID: []int{id}, RandomID: []int64{randID()},
	})
	if err != nil {
		return model.Message{}, a.mapErr("reenviar el mensaje", err)
	}
	m, ents, ok := sentMessageOf(upd, toChatNativeID, "")
	if !ok {
		return model.Message{}, fmt.Errorf("telegram: Telegram no devolvió el mensaje reenviado: %w", port.ErrNotFound)
	}
	out := a.conv(&ents).message(m)
	out.FromMe, out.Forwarded, out.Delivery = true, true, model.DeliverySent
	a.local.noteQuote(out)
	a.local.indexMsgChats(ctx, []model.Message{out})
	return out, nil
}

// VotePoll vota (o retira el voto con una lista vacía) en una encuesta. options son los textos.
func (a *Adapter) VotePoll(ctx context.Context, msg model.MessageRef, options []string) error {
	api, peer, err := a.sendTarget(ctx, msg.ChatNativeID)
	if err != nil {
		return err
	}
	id, err := msgID(msg)
	if err != nil {
		return err
	}
	rec, ok := a.local.getPoll(ctx, msg.ChatNativeID, id)
	if !ok {
		// Mensaje que el adaptador no vio en vivo: se vuelve a pedir para conocer las opciones.
		m, ferr := a.fetchMessage(ctx, api, msg.ChatNativeID, id)
		if ferr != nil {
			return ferr
		}
		mp, isPoll := m.Media.(*tg.MessageMediaPoll)
		if !isPoll {
			return fmt.Errorf("telegram: ese mensaje no es una encuesta: %w", port.ErrInvalid)
		}
		rec = recordOfPoll(mp.Poll)
		a.local.putPoll(ctx, msg.ChatNativeID, id, mp.Poll.ID, rec)
	}
	bytesOf, err := pollOptionBytes(rec, options)
	if err != nil {
		return err
	}
	if _, err := api.MessagesSendVote(ctx, &tg.MessagesSendVoteRequest{Peer: peer, MsgID: id, Options: bytesOf}); err != nil {
		return a.mapErr("votar en la encuesta", err)
	}
	return nil
}

// pollOptionBytes traduce los textos de las opciones elegidas a los bytes que pide Telegram.
func pollOptionBytes(rec pollRecord, options []string) ([][]byte, error) {
	out := make([][]byte, 0, len(options))
	for _, name := range options {
		found := false
		for _, o := range rec.Options {
			if o.Text == name {
				out = append(out, o.Opt)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("telegram: la encuesta no tiene la opción «%s»: %w", name, port.ErrInvalid)
		}
	}
	if len(out) > 1 && !rec.Multi {
		return nil, fmt.Errorf("telegram: esa encuesta solo admite una opción: %w", port.ErrInvalid)
	}
	return out, nil
}

// fetchMessage pide un mensaje suelto (para renovar referencias de archivos o leer encuestas).
func (a *Adapter) fetchMessage(ctx context.Context, api *tg.Client, chat string, id int) (*tg.Message, error) {
	kind, _, ok := parseNative(chat)
	if !ok {
		return nil, fmt.Errorf("telegram: identificador de chat no válido: %w", port.ErrInvalid)
	}
	var res tg.MessagesMessagesClass
	var err error
	if kind == kindChannel {
		var ch tg.InputChannelClass
		if ch, err = a.peers.inputChannel(ctx, chat); err != nil {
			return nil, err
		}
		res, err = api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: ch, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: id}}})
	} else {
		res, err = api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: id}})
	}
	if err != nil {
		return nil, a.mapErr("pedir el mensaje", err)
	}
	mod, ok := res.AsModified()
	if !ok {
		return nil, fmt.Errorf("telegram: el mensaje ya no existe: %w", port.ErrNotFound)
	}
	a.peers.feedLists(ctx, mod.GetUsers(), mod.GetChats())
	for _, mc := range mod.GetMessages() {
		if m, ok := mc.(*tg.Message); ok && m.ID == id {
			return m, nil
		}
	}
	return nil, fmt.Errorf("telegram: el mensaje ya no existe: %w", port.ErrNotFound)
}

// MarkRead marca como leído el chat hasta el mensaje más reciente de upTo.
func (a *Adapter) MarkRead(ctx context.Context, chatNativeID string, upTo []model.MessageRef) error {
	maxID := 0
	for _, r := range upTo {
		if r.FromMe {
			continue
		}
		if id, err := strconv.Atoi(r.NativeID); err == nil && id > maxID {
			maxID = id
		}
	}
	if maxID == 0 {
		return nil
	}
	api, peer, err := a.sendTarget(ctx, chatNativeID)
	if err != nil {
		return err
	}
	if kind, _, _ := parseNative(chatNativeID); kind == kindChannel {
		ch, err := a.peers.inputChannel(ctx, chatNativeID)
		if err != nil {
			return err
		}
		if _, err := api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{Channel: ch, MaxID: maxID}); err != nil {
			return a.mapErr("marcar como leído", err)
		}
		return nil
	}
	if _, err := api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{Peer: peer, MaxID: maxID}); err != nil {
		return a.mapErr("marcar como leído", err)
	}
	return nil
}

// SetTyping avisa «escribiendo…» o lo cancela.
func (a *Adapter) SetTyping(ctx context.Context, chatNativeID string, typing bool) error {
	api, peer, err := a.sendTarget(ctx, chatNativeID)
	if err != nil {
		return err
	}
	var action tg.SendMessageActionClass = &tg.SendMessageCancelAction{}
	if typing {
		action = &tg.SendMessageTypingAction{}
	}
	if _, err := api.MessagesSetTyping(ctx, &tg.MessagesSetTypingRequest{Peer: peer, Action: action}); err != nil {
		return a.mapErr("avisar que escribes", err)
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Chats: archivar, fijar y silenciar

// ArchiveChat mueve el chat a la carpeta de archivados (1) o lo devuelve a la principal (0).
func (a *Adapter) ArchiveChat(ctx context.Context, chatNativeID string, archived bool) error {
	api, peer, err := a.sendTarget(ctx, chatNativeID)
	if err != nil {
		return err
	}
	folder := 0
	if archived {
		folder = 1
	}
	if _, err := api.FoldersEditPeerFolders(ctx, []tg.InputFolderPeer{{Peer: peer, FolderID: folder}}); err != nil {
		return a.mapErr("archivar el chat", err)
	}
	a.applyMarks(ctx, chatNativeID, func(m *chatMarks) { m.Archived = archived })
	return nil
}

// PinChat fija o desfija el chat.
func (a *Adapter) PinChat(ctx context.Context, chatNativeID string, pinned bool) error {
	api, peer, err := a.sendTarget(ctx, chatNativeID)
	if err != nil {
		return err
	}
	if _, err := api.MessagesToggleDialogPin(ctx, &tg.MessagesToggleDialogPinRequest{
		Pinned: pinned, Peer: &tg.InputDialogPeer{Peer: peer},
	}); err != nil {
		if isTg(err, "PINNED_DIALOGS_TOO_MUCH") {
			return fmt.Errorf("telegram: ya tienes el máximo de chats fijados: %w", port.ErrInvalid)
		}
		return a.mapErr("fijar el chat", err)
	}
	a.applyMarks(ctx, chatNativeID, func(m *chatMarks) { m.Pinned = pinned })
	return nil
}

// MuteChat silencia el chat hasta `until` (ms Unix; 0 = quitar el silencio; -1 = siempre).
func (a *Adapter) MuteChat(ctx context.Context, chatNativeID string, until int64) error {
	api, peer, err := a.sendTarget(ctx, chatNativeID)
	if err != nil {
		return err
	}
	var settings tg.InputPeerNotifySettings
	switch {
	case until == 0:
		settings.SetMuteUntil(0)
	case until < 0:
		settings.SetMuteUntil(2147483647)
	default:
		settings.SetMuteUntil(int(until / 1000))
	}
	if _, err := api.AccountUpdateNotifySettings(ctx, &tg.AccountUpdateNotifySettingsRequest{
		Peer: &tg.InputNotifyPeer{Peer: peer}, Settings: settings,
	}); err != nil {
		return a.mapErr("silenciar el chat", err)
	}
	a.applyMarks(ctx, chatNativeID, func(m *chatMarks) { m.Muted = until })
	return nil
}

// applyMarks refleja un cambio propio en las marcas y reenvía el chat completo.
func (a *Adapter) applyMarks(ctx context.Context, native string, f func(*chatMarks)) {
	if _, known := a.local.updateMarks(native, f); !known {
		a.local.setMarks(native, func() chatMarks { var m chatMarks; f(&m); return m }())
	}
	if ch, ok := a.chatShell(native, a.peers); ok {
		a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
	}
}
