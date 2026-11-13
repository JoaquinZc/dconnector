package whatsapp

// Envío y acciones sobre mensajes (07 §9).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	maxPollOptions = 12
	voiceMime      = "audio/ogg; codecs=opus"
)

// ---------------------------------------------------------------------------------------------
// Archivos

// preparedFile: un archivo leído, medido y subido a los servidores de WhatsApp.
type preparedFile struct {
	data []byte
	mime string
	name string
	up   whatsmeow.UploadResponse
}

func mediaTypeFor(kind model.ContentKind) (whatsmeow.MediaType, bool) {
	switch kind {
	case model.KindImage, model.KindSticker:
		return whatsmeow.MediaImage, true
	case model.KindVideo:
		return whatsmeow.MediaVideo, true
	case model.KindAudio, model.KindVoice:
		return whatsmeow.MediaAudio, true
	case model.KindDocument:
		return whatsmeow.MediaDocument, true
	}
	return "", false
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
		return voiceMime
	case model.KindSticker:
		return "image/webp"
	}
	return "application/octet-stream"
}

// readFile lee el archivo a enviar y valida tamaño y formato.
func readFile(kind model.ContentKind, out model.Outgoing) (preparedFile, error) {
	if strings.TrimSpace(out.FilePath) == "" {
		return preparedFile{}, fmt.Errorf("whatsapp: falta el archivo a enviar: %w", port.ErrInvalid)
	}
	if kind == model.KindSticker {
		ext := strings.ToLower(filepath.Ext(out.FilePath))
		mt := strings.ToLower(out.Mime)
		if ext != ".webp" && mt != "image/webp" {
			return preparedFile{}, fmt.Errorf("whatsapp: El sticker debe ser .webp: %w", port.ErrInvalid)
		}
	}
	fi, err := os.Stat(out.FilePath)
	if err != nil {
		return preparedFile{}, fmt.Errorf("whatsapp: no se pudo abrir el archivo: %w", port.ErrNotFound)
	}
	if err := checkSize(kind, fi.Size()); err != nil {
		return preparedFile{}, err
	}
	data, err := os.ReadFile(out.FilePath)
	if err != nil {
		return preparedFile{}, fmt.Errorf("whatsapp: no se pudo leer el archivo: %w", port.ErrInvalid)
	}
	mimeType := detectMime(out.Mime, out.FilePath, data)
	if mimeType == "application/octet-stream" || mimeType == "" {
		mimeType = defaultMime(kind)
	}
	if kind == model.KindVoice {
		mimeType = voiceMime // la interfaz ya manda OGG/Opus
	}
	if kind == model.KindSticker {
		mimeType = "image/webp"
	}
	name := out.FileName
	if name == "" {
		name = filepath.Base(out.FilePath)
	}
	return preparedFile{data: data, mime: mimeType, name: name}, nil
}

// buildMediaMessage arma el submensaje de medio con lo subido (07 §9). Función pura.
func buildMediaMessage(kind model.ContentKind, pf preparedFile, caption string, ci *waE2E.ContextInfo) (*waE2E.Message, error) {
	up := pf.up
	length := up.FileLength
	if length == 0 {
		length = uint64(len(pf.data))
	}
	switch kind {
	case model.KindImage:
		w, h, thumb := imageInfo(pf.data)
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(length),
			Mimetype: proto.String(pf.mime), Caption: optString(caption),
			Width: proto.Uint32(uint32(w)), Height: proto.Uint32(uint32(h)), JPEGThumbnail: thumb, ContextInfo: ci,
		}}, nil
	case model.KindVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(length),
			Mimetype: proto.String(pf.mime), Caption: optString(caption), ContextInfo: ci,
		}}, nil
	case model.KindAudio, model.KindVoice:
		au := &waE2E.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(length),
			Mimetype: proto.String(pf.mime), ContextInfo: ci,
		}
		if kind == model.KindVoice {
			au.PTT = proto.Bool(true)
			au.Mimetype = proto.String(voiceMime)
			au.Seconds = proto.Uint32(uint32(oggOpusSeconds(pf.data)))
		}
		return &waE2E.Message{AudioMessage: au}, nil
	case model.KindDocument:
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(length),
			Mimetype: proto.String(pf.mime), FileName: proto.String(pf.name), Title: proto.String(pf.name),
			Caption: optString(caption), ContextInfo: ci,
		}}, nil
	case model.KindSticker:
		w, h := webpSize(pf.data)
		return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(length),
			Mimetype: proto.String("image/webp"), Width: proto.Uint32(uint32(w)), Height: proto.Uint32(uint32(h)),
			ContextInfo: ci,
		}}, nil
	}
	return nil, port.ErrUnsupported
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return proto.String(s)
}

// ---------------------------------------------------------------------------------------------
// Construcción del mensaje saliente

// quotedTextOf: texto citado que se manda al responder (el texto completo si es texto; si no,
// la vista previa).
func (c converter) quotedTextOf(chatID, msgID string, m *waE2E.Message) string {
	m = unwrap(m)
	switch {
	case m == nil:
		return ""
	case m.Conversation != nil:
		return truncate(m.GetConversation(), quotedTextMaxRunes)
	case m.GetExtendedTextMessage() != nil:
		return truncate(m.GetExtendedTextMessage().GetText(), quotedTextMaxRunes)
	}
	return c.previewOfProto(chatID, msgID, m)
}

// buildContext arma el ContextInfo de una respuesta o con menciones. nil si no hace falta.
func (a *Adapter) buildContext(ctx context.Context, c converter, chat types.JID, out model.Outgoing) *waE2E.ContextInfo {
	ci := &waE2E.ContextInfo{}
	used := false
	if r := out.ReplyTo; r != nil && r.NativeID != "" {
		ci.StanzaID = proto.String(r.NativeID)
		var participant types.JID
		switch {
		case r.FromMe:
			participant = c.self
		case r.SenderNativeID != "":
			if j, err := nativeToJID(r.SenderNativeID); err == nil {
				participant = a.wireJID(j)
			}
		}
		if participant.IsEmpty() {
			participant = chat
		}
		ci.Participant = proto.String(participant.ToNonAD().String())
		if m, ok := a.local.getMsg(ctx, jidToNative(chat), r.NativeID); ok {
			ci.QuotedMessage = &waE2E.Message{Conversation: proto.String(c.quotedTextOf(jidToNative(chat), r.NativeID, m))}
		} else {
			ci.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		used = true
	}
	for _, m := range out.Mentions {
		if j, err := nativeToJID(m); err == nil {
			ci.MentionedJID = append(ci.MentionedJID, a.wireJID(j).String())
			used = true
		}
	}
	if !used {
		return nil
	}
	return ci
}

// buildOutgoing arma el mensaje de whatsmeow para un Outgoing, subiendo el archivo si hace falta.
func (a *Adapter) buildOutgoing(ctx context.Context, cli *whatsmeow.Client, c converter, chat types.JID, out model.Outgoing) (*waE2E.Message, error) {
	ci := a.buildContext(ctx, c, chat, out)
	switch out.Kind {
	case model.KindText:
		if strings.TrimSpace(out.Text) == "" {
			return nil, fmt.Errorf("whatsapp: el mensaje está vacío: %w", port.ErrInvalid)
		}
		if utf8.RuneCountInString(out.Text) > baseCapabilities().MaxTextLen {
			return nil, fmt.Errorf("whatsapp: el texto es demasiado largo: %w", port.ErrInvalid)
		}
		if ci == nil {
			return &waE2E.Message{Conversation: proto.String(out.Text)}, nil
		}
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(out.Text), ContextInfo: ci}}, nil

	case model.KindImage, model.KindVideo, model.KindAudio, model.KindVoice, model.KindDocument, model.KindSticker:
		pf, err := readFile(out.Kind, out)
		if err != nil {
			return nil, err
		}
		mt, _ := mediaTypeFor(out.Kind)
		up, err := cli.Upload(ctx, pf.data, mt)
		if err != nil {
			return nil, a.mapErr("subir el archivo", err)
		}
		pf.up = up
		caption := out.Text
		if out.Kind == model.KindAudio || out.Kind == model.KindVoice || out.Kind == model.KindSticker {
			caption = ""
		}
		return buildMediaMessage(out.Kind, pf, caption, ci)

	case model.KindLocation:
		l := out.Location
		if l == nil || l.Lat < -90 || l.Lat > 90 || l.Lng < -180 || l.Lng > 180 {
			return nil, fmt.Errorf("whatsapp: la ubicación no es válida: %w", port.ErrInvalid)
		}
		return &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
			DegreesLatitude: proto.Float64(l.Lat), DegreesLongitude: proto.Float64(l.Lng),
			Name: optString(l.Name), Address: optString(l.Address), ContextInfo: ci,
		}}, nil

	case model.KindContact:
		ct := out.Contact
		if ct == nil || (strings.TrimSpace(ct.VCard) == "" && strings.TrimSpace(ct.Phone) == "") {
			return nil, fmt.Errorf("whatsapp: el contacto necesita un teléfono o una vCard: %w", port.ErrInvalid)
		}
		name := strings.TrimSpace(ct.Name)
		if name == "" {
			name = ct.Phone
		}
		vcard := ct.VCard
		if strings.TrimSpace(vcard) == "" {
			vcard = buildVCard(name, ct.Phone)
		}
		return &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
			DisplayName: proto.String(name), Vcard: proto.String(vcard), ContextInfo: ci,
		}}, nil

	case model.KindPoll:
		p := out.Poll
		if p == nil || strings.TrimSpace(p.Question) == "" {
			return nil, fmt.Errorf("whatsapp: la encuesta necesita una pregunta: %w", port.ErrInvalid)
		}
		names := make([]string, 0, len(p.Options))
		seen := map[string]bool{}
		for _, o := range p.Options {
			n := strings.TrimSpace(o.Name)
			if n == "" || seen[n] {
				return nil, fmt.Errorf("whatsapp: las opciones de la encuesta no pueden estar vacías ni repetidas: %w", port.ErrInvalid)
			}
			seen[n] = true
			names = append(names, n)
		}
		if len(names) < 2 || len(names) > maxPollOptions {
			return nil, fmt.Errorf("whatsapp: la encuesta necesita entre 2 y %d opciones: %w", maxPollOptions, port.ErrInvalid)
		}
		selectable := 1
		if p.Multi {
			selectable = len(names)
		}
		msg := cli.BuildPollCreation(strings.TrimSpace(p.Question), names, selectable)
		if ci != nil {
			msg.PollCreationMessage.ContextInfo = ci
		}
		return msg, nil
	}
	return nil, fmt.Errorf("whatsapp: tipo de mensaje %q no admitido: %w", out.Kind, port.ErrUnsupported)
}

// sentMessage arma el model.Message de lo que se acaba de enviar, con la misma traducción que
// se usa al recibir (así el resultado es coherente con lo que luego llegue por eventos).
func (a *Adapter) sentMessage(ctx context.Context, c converter, chat types.JID, resp whatsmeow.SendResponse, msg *waE2E.Message) model.Message {
	ts := resp.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	info := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat: chat, Sender: c.self, IsFromMe: true, IsGroup: chat.Server == types.GroupServer,
		},
		ID: resp.ID, Timestamp: ts,
	}
	m := c.convertMessage(info, msg)
	if m.Poll != nil {
		m.Poll = a.polls.register(ctx, m.ChatNativeID, m.NativeID, m.Poll)
	}
	a.local.putMsg(ctx, m.ChatNativeID, m.NativeID, m.Timestamp, msg)
	return m
}

// ---------------------------------------------------------------------------------------------
// Puerto: mensajes

func (a *Adapter) Send(ctx context.Context, chatNativeID string, out model.Outgoing) (model.Message, error) {
	cli, err := a.live()
	if err != nil {
		return model.Message{}, err
	}
	chat, err := nativeToJID(chatNativeID)
	if err != nil {
		return model.Message{}, err
	}
	if isStatusJID(chat) {
		return model.Message{}, fmt.Errorf("whatsapp: los estados se publican desde Estados: %w", port.ErrInvalid)
	}
	c := a.conv()
	msg, err := a.buildOutgoing(ctx, cli, c, chat, out)
	if err != nil {
		return model.Message{}, err
	}
	resp, err := cli.SendMessage(ctx, chat, msg)
	if err != nil {
		return model.Message{}, a.mapErr("enviar el mensaje", err)
	}
	m := a.sentMessage(ctx, c, chat, resp, msg)
	if m.Media != nil && out.FilePath != "" {
		m.Media.LocalPath = out.FilePath // ya está en disco
	}
	a.log.Debug("mensaje enviado", "chat", shortID(chatNativeID), "kind", string(out.Kind))
	return m, nil
}

// refParts devuelve el chat (con el direccionamiento original de WhatsApp) y el emisor de un
// mensaje referido. El emisor queda vacío si el mensaje es mío.
func (a *Adapter) refParts(ref model.MessageRef) (chat, sender types.JID, err error) {
	chat, err = nativeToJID(ref.ChatNativeID)
	if err != nil {
		return
	}
	if ref.NativeID == "" {
		err = fmt.Errorf("whatsapp: falta el id del mensaje: %w", port.ErrInvalid)
		return
	}
	chat = a.wireJID(chat)
	if ref.FromMe {
		return
	}
	if ref.SenderNativeID != "" {
		if j, e := nativeToJID(ref.SenderNativeID); e == nil {
			sender = a.wireJID(j)
		}
	}
	if sender.IsEmpty() {
		sender = chat
	}
	return
}

func isNewsletter(j types.JID) bool { return j.Server == types.NewsletterServer }

func (a *Adapter) React(ctx context.Context, msg model.MessageRef, emoji string) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	chat, sender, err := a.refParts(msg)
	if err != nil {
		return err
	}
	if isNewsletter(chat) {
		return port.ErrUnsupported
	}
	if _, err := cli.SendMessage(ctx, chat, cli.BuildReaction(chat, sender, msg.NativeID, emoji)); err != nil {
		return a.mapErr("reaccionar", err)
	}
	return nil
}

func (a *Adapter) Edit(ctx context.Context, msg model.MessageRef, newText string) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	chat, _, err := a.refParts(msg)
	if err != nil {
		return err
	}
	if isNewsletter(chat) {
		return port.ErrUnsupported
	}
	if !msg.FromMe {
		return fmt.Errorf("whatsapp: solo puedes editar tus propios mensajes: %w", port.ErrInvalid)
	}
	if strings.TrimSpace(newText) == "" || utf8.RuneCountInString(newText) > baseCapabilities().MaxTextLen {
		return fmt.Errorf("whatsapp: el texto editado no es válido: %w", port.ErrInvalid)
	}
	edit := cli.BuildEdit(chat, msg.NativeID, &waE2E.Message{Conversation: proto.String(newText)})
	if _, err := cli.SendMessage(ctx, chat, edit); err != nil {
		return a.mapErr("editar el mensaje", err)
	}
	return nil
}

// Delete: para todos envía una revocación; solo para mí no llama a WhatsApp (el servicio lo marca).
func (a *Adapter) Delete(ctx context.Context, msg model.MessageRef, forEveryone bool) error {
	if !forEveryone {
		return nil
	}
	cli, err := a.live()
	if err != nil {
		return err
	}
	chat, sender, err := a.refParts(msg)
	if err != nil {
		return err
	}
	if isNewsletter(chat) {
		return port.ErrUnsupported
	}
	if _, err := cli.SendMessage(ctx, chat, cli.BuildRevoke(chat, sender, msg.NativeID)); err != nil {
		return a.mapErr("borrar el mensaje", err)
	}
	return nil
}

// forwardedCopy: copia del mensaje guardado con la marca de reenviado. nil si no se puede reenviar.
func forwardedCopy(m *waE2E.Message) *waE2E.Message {
	m = unwrap(m)
	if m == nil || pollOf(m) != nil {
		return nil
	}
	return withContext(m, &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(1)})
}

// Forward reenvía un mensaje que el adaptador vio en vivo (caché wa_msgs): reutiliza el archivo ya
// subido a WhatsApp, sin descargarlo ni volver a subirlo.
func (a *Adapter) Forward(ctx context.Context, msg model.MessageRef, toChatNativeID string) (model.Message, error) {
	cli, err := a.live()
	if err != nil {
		return model.Message{}, err
	}
	to, err := nativeToJID(toChatNativeID)
	if err != nil {
		return model.Message{}, err
	}
	if isStatusJID(to) {
		return model.Message{}, fmt.Errorf("whatsapp: no se puede reenviar a Estados: %w", port.ErrInvalid)
	}
	src, ok := a.local.getMsg(ctx, msg.ChatNativeID, msg.NativeID)
	if !ok {
		return model.Message{}, fmt.Errorf("whatsapp: ese mensaje ya no está disponible para reenviar. Cópialo y envíalo de nuevo: %w", port.ErrNotFound)
	}
	fwd := forwardedCopy(src)
	if fwd == nil {
		return model.Message{}, fmt.Errorf("whatsapp: este tipo de mensaje no se puede reenviar: %w", port.ErrUnsupported)
	}
	resp, err := cli.SendMessage(ctx, to, fwd)
	if err != nil {
		return model.Message{}, a.mapErr("reenviar el mensaje", err)
	}
	return a.sentMessage(ctx, a.conv(), to, resp, fwd), nil
}

func (a *Adapter) VotePoll(ctx context.Context, msg model.MessageRef, options []string) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	chat, sender, err := a.refParts(msg)
	if err != nil {
		return err
	}
	known, ok := a.polls.options(ctx, msg.ChatNativeID, msg.NativeID)
	if !ok {
		return fmt.Errorf("whatsapp: no se conoce esa encuesta: %w", port.ErrNotFound)
	}
	valid := map[string]bool{}
	for _, o := range known {
		valid[o] = true
	}
	for _, o := range options {
		if !valid[o] {
			return fmt.Errorf("whatsapp: la opción %q no existe en la encuesta: %w", o, port.ErrInvalid)
		}
	}
	c := a.conv()
	if msg.FromMe {
		sender = c.self
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsFromMe: msg.FromMe, IsGroup: chat.Server == types.GroupServer},
		ID:            msg.NativeID,
	}
	vote, err := cli.BuildPollVote(ctx, info, options)
	if err != nil {
		return a.mapErr("votar en la encuesta", err)
	}
	if _, err := cli.SendMessage(ctx, chat, vote); err != nil {
		return a.mapErr("votar en la encuesta", err)
	}
	if poll, ok := a.polls.myVote(ctx, msg.ChatNativeID, msg.NativeID, jidToNative(c.self), options); ok {
		a.emit(model.Event{
			Type: model.EvMessageUpdate, ChatKey: model.NewChatKey(a.acc.ID, msg.ChatNativeID),
			Update: &model.MessageUpdate{Key: model.NewMessageKey(a.acc.ID, msg.ChatNativeID, msg.NativeID), Poll: poll},
		})
	}
	return nil
}

// MarkRead marca como leídos los mensajes entrantes; en grupos agrupa por emisor (07 §9).
func (a *Adapter) MarkRead(ctx context.Context, chatNativeID string, upTo []model.MessageRef) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	chat, err := nativeToJID(chatNativeID)
	if err != nil {
		return err
	}
	if isNewsletter(chat) {
		return port.ErrUnsupported
	}
	chat = a.wireJID(chat)
	type group struct {
		sender types.JID
		ids    []string
	}
	var order []string
	groups := map[string]*group{}
	for _, r := range upTo {
		if r.FromMe || r.NativeID == "" {
			continue
		}
		sender := chat
		if chat.Server == types.GroupServer && r.SenderNativeID != "" {
			if j, e := nativeToJID(r.SenderNativeID); e == nil {
				sender = a.wireJID(j)
			}
		}
		k := sender.String()
		g := groups[k]
		if g == nil {
			g = &group{sender: sender}
			groups[k] = g
			order = append(order, k)
		}
		g.ids = append(g.ids, r.NativeID)
	}
	now := time.Now()
	for _, k := range order {
		g := groups[k]
		if err := cli.MarkRead(ctx, g.ids, now, chat, g.sender); err != nil {
			return a.mapErr("marcar como leído", err)
		}
	}
	return nil
}

func (a *Adapter) SetTyping(ctx context.Context, chatNativeID string, typing bool) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	chat, err := nativeToJID(chatNativeID)
	if err != nil {
		return err
	}
	chat = a.wireJID(chat)
	state := types.ChatPresencePaused
	if typing {
		state = types.ChatPresenceComposing
		a.goOnline(cli)
	}
	if err := cli.SendChatPresence(ctx, chat, state, types.ChatPresenceMediaText); err != nil {
		return a.mapErr("avisar que estás escribiendo", err)
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Puerto: archivar, fijar, silenciar (estado de aplicación de WhatsApp)

func (a *Adapter) chatPatchTarget(chatNativeID string) (*whatsmeow.Client, types.JID, error) {
	cli, err := a.live()
	if err != nil {
		return nil, types.EmptyJID, err
	}
	chat, err := nativeToJID(chatNativeID)
	if err != nil {
		return nil, types.EmptyJID, err
	}
	if isNewsletter(chat) || isStatusJID(chat) {
		return nil, types.EmptyJID, port.ErrUnsupported
	}
	return cli, a.wireJID(chat), nil
}

func (a *Adapter) ArchiveChat(ctx context.Context, chatNativeID string, archived bool) error {
	cli, chat, err := a.chatPatchTarget(chatNativeID)
	if err != nil {
		return err
	}
	if err := cli.SendAppState(ctx, appstate.BuildArchive(chat, archived, time.Time{}, nil)); err != nil {
		return a.mapErr("archivar el chat", err)
	}
	a.updateFlags(ctx, chatNativeID, func(f *chatFlags) {
		f.Archived = archived
		if archived {
			f.Pinned = false // archivar desfija
		}
	})
	return nil
}

func (a *Adapter) PinChat(ctx context.Context, chatNativeID string, pinned bool) error {
	cli, chat, err := a.chatPatchTarget(chatNativeID)
	if err != nil {
		return err
	}
	if err := cli.SendAppState(ctx, appstate.BuildPin(chat, pinned)); err != nil {
		return a.mapErr("fijar el chat", err)
	}
	a.updateFlags(ctx, chatNativeID, func(f *chatFlags) { f.Pinned = pinned })
	return nil
}

// MuteChat: until en ms; 0 = quitar el silencio; -1 = siempre.
func (a *Adapter) MuteChat(ctx context.Context, chatNativeID string, until int64) error {
	cli, chat, err := a.chatPatchTarget(chatNativeID)
	if err != nil {
		return err
	}
	var patch appstate.PatchInfo
	switch {
	case until == 0:
		patch = appstate.BuildMuteAbs(chat, false, nil)
	case until < 0:
		patch = appstate.BuildMuteAbs(chat, true, nil)
	default:
		if until <= time.Now().UnixMilli() {
			return fmt.Errorf("whatsapp: la hora de fin del silencio ya pasó: %w", port.ErrInvalid)
		}
		patch = appstate.BuildMuteAbs(chat, true, &until)
	}
	if err := cli.SendAppState(ctx, patch); err != nil {
		return a.mapErr("silenciar el chat", err)
	}
	a.updateFlags(ctx, chatNativeID, func(f *chatFlags) { f.Muted = max(until, -1) })
	return nil
}

// updateFlags actualiza las marcas que el adaptador recuerda de un chat.
func (a *Adapter) updateFlags(ctx context.Context, chatNativeID string, change func(*chatFlags)) {
	f := a.local.chatFlagsOf(ctx, chatNativeID)
	change(&f)
	a.local.saveChatFlags(ctx, chatNativeID, f)
}
