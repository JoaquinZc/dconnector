package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

const (
	// maxAutoDownloads: descargas automáticas simultáneas.
	maxAutoDownloads = 3
	// markReadMax: mensajes entrantes que se pasan al adaptador al marcar un chat como leído.
	markReadMax = 50
	// autoDownloadTimeout: tope de una descarga automática.
	autoDownloadTimeout = 3 * time.Minute
)

// Messaging: enviar, responder, reaccionar, editar, borrar, leído, medios (06 §5).
type Messaging struct {
	acc    *Accounts
	router *Router
	bus    *bus.Bus
	app    port.AppStore
	now    func() time.Time

	flightMu sync.Mutex
	flights  map[string]*mediaFlight

	autoSem    chan struct{}
	autoCancel func()
	autoDone   chan struct{}
	autoWG     sync.WaitGroup
}

type mediaFlight struct {
	done chan struct{}
	path string
	err  error
}

// NewMessaging crea el servicio y se suscribe al bus (`automedia`) para la descarga automática.
// Close() cancela esa suscripción.
func NewMessaging(acc *Accounts, r *Router, b *bus.Bus, app port.AppStore) *Messaging {
	m := &Messaging{
		acc: acc, router: r, bus: b, app: app, now: time.Now,
		flights: map[string]*mediaFlight{},
		autoSem: make(chan struct{}, maxAutoDownloads),
	}
	ch, cancel := b.Subscribe("automedia", bus.DefaultBuffer)
	m.autoCancel = cancel
	m.autoDone = make(chan struct{})
	go func() {
		defer close(m.autoDone)
		for ev := range ch {
			safeCall("descarga automática", func() { m.maybeAutoDownload(ev) })
		}
	}()
	return m
}

// Close cancela la suscripción de descarga automática y espera a las descargas en curso.
func (m *Messaging) Close() {
	if m.autoCancel != nil {
		m.autoCancel()
		<-m.autoDone
	}
	m.autoWG.Wait()
}

// ---------------------------------------------------------------------------------------------
// Resolución y validación

type msgTarget struct {
	inst   *instance
	ad     port.ChannelAdapter
	native string
	key    model.ChatKey
	chat   model.Chat
	known  bool // el chat existe en el almacén
	caps   capability.Set
}

func (m *Messaging) target(ctx context.Context, key model.ChatKey) (*msgTarget, error) {
	inst, native, err := m.acc.chatTarget(key)
	if err != nil {
		return nil, err
	}
	t := &msgTarget{inst: inst, ad: inst.getAdapter(), native: native, key: key}
	chat, err := inst.store.GetChat(ctx, native)
	switch {
	case err == nil:
		t.chat, t.known = chat, true
	case errors.Is(err, port.ErrNotFound):
		t.chat = model.Chat{NativeID: native, Kind: model.ChatDirect, PeerNativeID: native}
	default:
		return nil, err
	}
	t.chat.Key, t.chat.AccountID, t.chat.Channel = key, inst.acc.ID, inst.acc.Channel
	t.caps = t.ad.Capabilities(&t.chat)
	return t, nil
}

func msgUnsupported(what string) error {
	return fmt.Errorf("%w: %s", port.ErrUnsupported, what)
}

func msgInvalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", port.ErrInvalid, fmt.Sprintf(format, a...))
}

func checkTextLen(text string, c capability.Set) error {
	if c.MaxTextLen > 0 && utf8.RuneCountInString(text) > c.MaxTextLen {
		return msgInvalid("el texto supera el máximo de %d caracteres", c.MaxTextLen)
	}
	return nil
}

func sizeMB(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

// validateOutgoing aplica las capacidades del chat a lo que se pide enviar (06 §5).
func validateOutgoing(out model.Outgoing, c capability.Set) error {
	if out.ReplyTo != nil && !c.Reply {
		return msgUnsupported("este chat no permite responder a un mensaje")
	}
	var (
		ok      bool
		what    string
		file    bool
		maxSize int64
	)
	switch out.Kind {
	case model.KindText:
		ok, what = c.SendText, "enviar texto"
		if strings.TrimSpace(out.Text) == "" {
			return msgInvalid("el mensaje está vacío")
		}
	case model.KindImage:
		ok, what, file, maxSize = c.SendImage, "enviar imágenes", true, c.MaxImageSize
	case model.KindVideo:
		ok, what, file, maxSize = c.SendVideo, "enviar videos", true, c.MaxVideoSize
	case model.KindAudio:
		ok, what, file, maxSize = c.SendAudio, "enviar audios", true, c.MaxFileSize
	case model.KindVoice:
		ok, what, file, maxSize = c.SendVoice, "enviar notas de voz", true, c.MaxFileSize
	case model.KindDocument:
		ok, what, file, maxSize = c.SendDocument, "enviar documentos", true, c.MaxFileSize
	case model.KindSticker:
		ok, what, file, maxSize = c.SendSticker, "enviar stickers", true, c.MaxFileSize
	case model.KindLocation:
		ok, what = c.SendLocation, "enviar ubicaciones"
		if out.Location == nil {
			return msgInvalid("falta la ubicación")
		}
	case model.KindContact:
		ok, what = c.SendContact, "enviar contactos"
		if out.Contact == nil {
			return msgInvalid("falta el contacto")
		}
	case model.KindPoll:
		ok, what = c.SendPoll, "enviar encuestas"
		if out.Poll == nil || strings.TrimSpace(out.Poll.Question) == "" || len(out.Poll.Options) < 2 {
			return msgInvalid("la encuesta necesita una pregunta y al menos dos opciones")
		}
	default:
		return msgInvalid("tipo de mensaje %q no se puede enviar", out.Kind)
	}
	if !ok {
		return msgUnsupported("este chat no permite " + what)
	}
	if err := checkTextLen(out.Text, c); err != nil {
		return err
	}
	if file {
		if out.FilePath == "" {
			return msgInvalid("falta el archivo")
		}
		fi, err := os.Stat(out.FilePath)
		if err != nil || fi.IsDir() {
			return msgInvalid("no se encontró el archivo")
		}
		if maxSize > 0 && fi.Size() > maxSize {
			return msgInvalid("el archivo pesa %s y el máximo es %s", sizeMB(fi.Size()), sizeMB(maxSize))
		}
	}
	return nil
}

// mediaURLFor: dirección con la que la interfaz pide un medio a ui/wails (03: Media.URL). El almacén
// la calcula al leer; lo que llega por eventos viene del adaptador y no la trae.
func mediaURLFor(acc model.AccountID, mediaID string) string {
	if mediaID == "" {
		return ""
	}
	return "/media/" + string(acc) + "/" + mediaID
}

// withMediaURL devuelve una copia del medio con su URL puesta (si tiene id y aún no la trae).
func withMediaURL(acc model.AccountID, md *model.Media) *model.Media {
	if md == nil || md.URL != "" || md.ID == "" {
		return md
	}
	cp := *md
	cp.URL = mediaURLFor(acc, md.ID)
	return &cp
}

func (m *Messaging) emit(acc model.AccountID, ev model.Event) {
	sink, ok := m.router.SinkFor(acc)
	if !ok {
		slog.Warn("no hay enlace de eventos para la cuenta", "accountId", string(acc), "type", string(ev.Type))
		return
	}
	sink.Emit(ev)
}

// emitMessage emite un mensaje saliente por el sink de la cuenta (se persiste y se publica
// como todo lo demás). Si el chat no existía todavía, lo crea antes.
func (m *Messaging) emitSent(t *msgTarget, msg model.Message) model.Message {
	acc := t.inst.acc.ID
	msg.AccountID, msg.Channel = acc, t.inst.acc.Channel
	if msg.ChatNativeID == "" {
		msg.ChatNativeID = t.native
	}
	if msg.Timestamp == 0 {
		msg.Timestamp = m.now().UnixMilli()
	}
	if msg.Delivery == "" {
		msg.Delivery = model.DeliverySent
	}
	msg.FromMe = true
	msg.Media = withMediaURL(acc, msg.Media)
	msg.Key = model.NewMessageKey(acc, msg.ChatNativeID, msg.NativeID)
	msg.ChatKey = model.NewChatKey(acc, msg.ChatNativeID)
	if !t.known && msg.ChatNativeID == t.native {
		chat := t.chat
		chat.Title, chat.LastMessageAt = t.native, msg.Timestamp
		m.emit(acc, model.Event{Type: model.EvChatUpsert, Chat: &chat, ChatKey: chat.Key})
	}
	cp := msg
	m.emit(acc, model.Event{Type: model.EvMessageSent, At: msg.Timestamp, Message: &cp})
	return msg
}

// ---------------------------------------------------------------------------------------------
// Operaciones

// Send valida con las capacidades del chat, envía por el adaptador y emite message.sent.
func (m *Messaging) Send(ctx context.Context, key model.ChatKey, out model.Outgoing) (model.Message, error) {
	t, err := m.target(ctx, key)
	if err != nil {
		return model.Message{}, err
	}
	if err := validateOutgoing(out, t.caps); err != nil {
		return model.Message{}, err
	}
	if !t.caps.Mentions {
		out.Mentions = nil
	}
	msg, err := t.ad.Send(ctx, t.native, out)
	if err != nil {
		return model.Message{}, err
	}
	return m.emitSent(t, msg), nil
}

type msgRef struct {
	*msgTarget
	msg model.Message
}

// loadMessage resuelve una clave de mensaje: instancia, chat (con capacidades) y mensaje guardado.
func (m *Messaging) loadMessage(ctx context.Context, key model.MessageKey) (*msgRef, error) {
	accID, chatNative, msgNative := key.Split()
	if accID == "" || chatNative == "" || msgNative == "" {
		return nil, msgInvalid("clave de mensaje no válida")
	}
	t, err := m.target(ctx, model.NewChatKey(accID, chatNative))
	if err != nil {
		return nil, err
	}
	msg, err := t.inst.store.GetMessage(ctx, chatNative, msgNative)
	if err != nil {
		return nil, err
	}
	return &msgRef{msgTarget: t, msg: msg}, nil
}

func (m *Messaging) selfOf(ctx context.Context, id model.AccountID) model.Account {
	acc, err := m.app.GetAccount(ctx, id)
	if err != nil {
		return model.Account{ID: id}
	}
	return acc
}

func (m *Messaging) React(ctx context.Context, key model.MessageKey, emoji string) error {
	r, err := m.loadMessage(ctx, key)
	if err != nil {
		return err
	}
	if !r.caps.React {
		return msgUnsupported("este chat no permite reaccionar")
	}
	if err := r.ad.React(ctx, r.msg.Ref(), emoji); err != nil {
		return err
	}
	// Se refleja enseguida en local (una reacción por persona); el eco del canal es idempotente.
	if self := m.selfOf(ctx, r.inst.acc.ID); self.SelfID != "" {
		m.emit(self.ID, model.Event{
			Type: model.EvReaction, At: m.now().UnixMilli(),
			Reaction: &model.ReactionEvent{MessageKey: key, Reaction: model.Reaction{
				SenderNativeID: self.SelfID, SenderName: self.SelfName, Emoji: emoji, At: m.now().UnixMilli(),
			}},
		})
	}
	return nil
}

func withinWindow(ts int64, window time.Duration, now time.Time) bool {
	if window <= 0 {
		return true
	}
	return now.Sub(time.UnixMilli(ts)) <= window
}

func (m *Messaging) Edit(ctx context.Context, key model.MessageKey, text string) error {
	r, err := m.loadMessage(ctx, key)
	if err != nil {
		return err
	}
	if !r.caps.Edit {
		return msgUnsupported("este chat no permite editar mensajes")
	}
	if !r.msg.FromMe {
		return msgInvalid("solo se pueden editar los mensajes propios")
	}
	if r.msg.DeletedAt > 0 {
		return msgInvalid("el mensaje ya fue eliminado")
	}
	if strings.TrimSpace(text) == "" {
		return msgInvalid("el texto está vacío")
	}
	if err := checkTextLen(text, r.caps); err != nil {
		return err
	}
	now := m.now()
	if !withinWindow(r.msg.Timestamp, r.caps.EditWindow, now) {
		return msgInvalid("ya pasó el tiempo para editar este mensaje")
	}
	if err := r.ad.Edit(ctx, r.msg.Ref(), text); err != nil {
		return err
	}
	t := text
	m.emit(r.inst.acc.ID, model.Event{
		Type: model.EvMessageUpdate, At: now.UnixMilli(),
		Update: &model.MessageUpdate{Key: key, Text: &t, EditedAt: now.UnixMilli()},
	})
	return nil
}

func (m *Messaging) Delete(ctx context.Context, key model.MessageKey, forEveryone bool) error {
	r, err := m.loadMessage(ctx, key)
	if err != nil {
		return err
	}
	now := m.now()
	if forEveryone {
		if !r.caps.DeleteForEveryone {
			return msgUnsupported("este chat no permite borrar para todos")
		}
		if !r.msg.FromMe {
			return msgInvalid("solo se pueden borrar para todos los mensajes propios")
		}
		if !withinWindow(r.msg.Timestamp, r.caps.DeleteWindow, now) {
			return msgInvalid("ya pasó el tiempo para borrar este mensaje para todos")
		}
	}
	if err := r.ad.Delete(ctx, r.msg.Ref(), forEveryone); err != nil {
		return err
	}
	m.emit(r.inst.acc.ID, model.Event{
		Type: model.EvMessageUpdate, At: now.UnixMilli(),
		Update: &model.MessageUpdate{Key: key, Deleted: true},
	})
	return nil
}

// Forward reenvía un mensaje a otro chat de la misma cuenta.
func (m *Messaging) Forward(ctx context.Context, key model.MessageKey, to model.ChatKey) (model.Message, error) {
	r, err := m.loadMessage(ctx, key)
	if err != nil {
		return model.Message{}, err
	}
	dest, err := m.target(ctx, to)
	if err != nil {
		return model.Message{}, err
	}
	if dest.inst.acc.ID != r.inst.acc.ID {
		return model.Message{}, msgInvalid("solo se puede reenviar a un chat de la misma cuenta")
	}
	if !r.caps.Forward || !dest.caps.Forward || !dest.caps.SendText {
		return model.Message{}, msgUnsupported("no se puede reenviar a este chat")
	}
	msg, err := r.ad.Forward(ctx, r.msg.Ref(), dest.native)
	if err != nil {
		return model.Message{}, err
	}
	if msg.ChatNativeID == "" {
		msg.ChatNativeID = dest.native
	}
	return m.emitSent(dest, msg), nil
}

func (m *Messaging) VotePoll(ctx context.Context, key model.MessageKey, options []string) error {
	r, err := m.loadMessage(ctx, key)
	if err != nil {
		return err
	}
	if !r.caps.VotePoll {
		return msgUnsupported("este chat no permite votar encuestas")
	}
	if r.msg.Poll == nil {
		return msgInvalid("el mensaje no es una encuesta")
	}
	if len(options) > 1 && !r.msg.Poll.Multi {
		return msgInvalid("esta encuesta admite una sola opción")
	}
	valid := map[string]bool{}
	for _, o := range r.msg.Poll.Options {
		valid[o.Name] = true
	}
	for _, o := range options {
		if !valid[o] {
			return msgInvalid("la opción %q no existe en la encuesta", o)
		}
	}
	return r.ad.VotePoll(ctx, r.msg.Ref(), options)
}

// MarkRead pasa al adaptador los mensajes entrantes sin leer (máx. 50, los más recientes),
// deja unreadCount en 0 y publica chat.updated.
func (m *Messaging) MarkRead(ctx context.Context, key model.ChatKey) error {
	t, err := m.target(ctx, key)
	if err != nil {
		return err
	}
	if !t.known {
		return fmt.Errorf("%w: chat %s", port.ErrNotFound, key)
	}
	page, err := t.inst.store.ListMessages(ctx, t.native, 0, markReadMax)
	if err != nil {
		return err
	}
	var refs []model.MessageRef
	var keys []model.MessageKey
	for _, msg := range page.Messages {
		if msg.FromMe || msg.Delivery == model.DeliveryRead || msg.Delivery == model.DeliveryPlayed {
			continue
		}
		refs = append(refs, msg.Ref())
		keys = append(keys, msg.Key)
	}
	if len(refs) > 0 && t.caps.ReadReceipts {
		if err := t.ad.MarkRead(ctx, t.native, refs); err != nil && !errors.Is(err, port.ErrUnsupported) {
			return err
		}
	}
	if len(keys) > 0 {
		self := m.selfOf(ctx, t.inst.acc.ID)
		_ = t.inst.store.ApplyReceipt(ctx, model.ReceiptEvent{
			ChatKey: key, MessageKeys: keys, Delivery: model.DeliveryRead,
			ReaderID: self.SelfID, At: m.now().UnixMilli(),
		})
	}
	if t.chat.UnreadCount == 0 && len(refs) == 0 {
		return nil
	}
	chat := t.chat
	chat.UnreadCount = 0
	if err := t.inst.store.PatchChat(ctx, chat, []string{"unreadCount"}); err != nil {
		return err
	}
	m.router.Publish(model.Event{
		Type: model.EvChatUpsert, AccountID: t.inst.acc.ID, Channel: t.inst.acc.Channel,
		Chat: &chat, ChatKey: key,
	})
	return nil
}

func (m *Messaging) SetTyping(ctx context.Context, key model.ChatKey, typing bool) error {
	t, err := m.target(ctx, key)
	if err != nil {
		return err
	}
	if !t.caps.Typing {
		return msgUnsupported("este chat no muestra «escribiendo»")
	}
	return t.ad.SetTyping(ctx, t.native, typing)
}

// ---------------------------------------------------------------------------------------------
// Medios

var mimeExt = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
	"audio/ogg": ".ogg", "audio/mpeg": ".mp3", "audio/mp4": ".m4a", "audio/aac": ".aac", "audio/webm": ".webm",
	"video/mp4": ".mp4", "video/webm": ".webm", "video/quicktime": ".mov", "video/3gpp": ".3gp",
	"application/pdf": ".pdf", "text/plain": ".txt",
}

// mediaFileName: <mediaId><ext>; la extensión sale del nombre original o del mime.
func mediaFileName(md model.Media) string {
	var b strings.Builder
	for _, r := range md.ID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	base := strings.Trim(b.String(), ".")
	if base == "" {
		base = "media"
	}
	ext := ""
	if e := filepath.Ext(md.FileName); e != "" && len(e) <= 8 {
		ext = strings.ToLower(e)
	}
	if ext == "" {
		if mt, _, err := mime.ParseMediaType(md.Mime); err == nil {
			ext = mimeExt[strings.ToLower(mt)]
			if ext == "" {
				if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
					ext = exts[0]
				}
			}
		}
	}
	if ext == "" {
		ext = ".bin"
	}
	return base + ext
}

// MediaPath devuelve el archivo local del medio; si falta, lo descarga con el adaptador,
// lo guarda en MediaDir, lo registra y emite message.updated. Una sola descarga simultánea
// por medio: las demás llamadas esperan y comparten el resultado.
func (m *Messaging) MediaPath(ctx context.Context, acc model.AccountID, mediaID string) (string, error) {
	inst, err := m.acc.mustGet(acc)
	if err != nil {
		return "", err
	}
	md, chatNative, err := inst.store.GetMedia(ctx, mediaID)
	if err != nil {
		return "", err
	}
	if md.LocalPath != "" {
		if _, err := os.Stat(md.LocalPath); err == nil {
			return md.LocalPath, nil
		}
	}
	flightKey := string(acc) + "|" + mediaID
	m.flightMu.Lock()
	if f, ok := m.flights[flightKey]; ok {
		m.flightMu.Unlock()
		select {
		case <-f.done:
			return f.path, f.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	f := &mediaFlight{done: make(chan struct{})}
	m.flights[flightKey] = f
	m.flightMu.Unlock()

	f.path, f.err = m.download(ctx, inst, md, chatNative)
	m.flightMu.Lock()
	delete(m.flights, flightKey)
	m.flightMu.Unlock()
	close(f.done)
	return f.path, f.err
}

func (m *Messaging) download(ctx context.Context, inst *instance, md model.Media, chatNative string) (string, error) {
	rc, err := inst.getAdapter().DownloadMedia(ctx, md)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	dir := paths.MediaDir(inst.acc.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("medios: crear carpeta: %w", err)
	}
	final := filepath.Join(dir, mediaFileName(md))
	tmp := final + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("medios: crear archivo: %w", err)
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("medios: descargar: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("medios: guardar: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("medios: guardar: %w", err)
	}
	if err := inst.store.SetMediaPath(ctx, md.ID, final); err != nil {
		return "", err
	}
	md.LocalPath = final
	// El almacén solo da el chat del mensaje; se asume que el id del medio es el del mensaje.
	// Si no coincide, el evento se descarta sin error (el archivo ya quedó registrado).
	m.emit(inst.acc.ID, model.Event{
		Type: model.EvMessageUpdate,
		Update: &model.MessageUpdate{
			Key:   model.NewMessageKey(inst.acc.ID, chatNative, md.ID),
			Media: &md,
		},
	})
	return final, nil
}

// ---------------------------------------------------------------------------------------------
// Descarga automática

type autoDownloadRule struct {
	Enabled bool    `json:"enabled"`
	MaxMB   float64 `json:"maxMB"`
}

func (m *Messaging) autoDownloadRules(ctx context.Context) map[string]autoDownloadRule {
	rules := map[string]autoDownloadRule{}
	raw, _ := m.app.GetSetting(ctx, SettingAutoDownload)
	if raw == "" {
		raw = defaultsAutoDownload
	}
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		rules = map[string]autoDownloadRule{}
		_ = json.Unmarshal([]byte(defaultsAutoDownload), &rules)
	}
	return rules
}

func (m *Messaging) maybeAutoDownload(ev model.Event) {
	if ev.Type != model.EvMessageNew || ev.Message == nil || ev.Message.Media == nil {
		return
	}
	msg := ev.Message
	if msg.FromMe || msg.Media.LocalPath != "" || msg.Media.ID == "" {
		return
	}
	rule, ok := m.autoDownloadRules(context.Background())[string(msg.Kind)]
	if !ok || !rule.Enabled {
		return
	}
	if rule.MaxMB > 0 && msg.Media.Size > int64(rule.MaxMB*(1<<20)) {
		return
	}
	acc, mediaID := msg.AccountID, msg.Media.ID
	m.autoWG.Add(1)
	go func() {
		defer m.autoWG.Done()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("pánico en la descarga automática", "panic", rec)
			}
		}()
		m.autoSem <- struct{}{}
		defer func() { <-m.autoSem }()
		ctx, cancel := context.WithTimeout(context.Background(), autoDownloadTimeout)
		defer cancel()
		if _, err := m.MediaPath(ctx, acc, mediaID); err != nil {
			slog.Debug("descarga automática fallida", "accountId", string(acc), "err", err)
		}
	}()
}
