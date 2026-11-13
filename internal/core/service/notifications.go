package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

// Reglas de Notifications (avisos de mensajes nuevos).
const (
	// notifyMaxAge: un mensaje más viejo que esto no es «en vivo» (historial, reenvío tardío): no avisa.
	notifyMaxAge = 2 * time.Minute
	// notifyChatGap: como mucho una notificación cada tanto por chat.
	notifyChatGap = 1500 * time.Millisecond
	// notifySoundGap: como mucho un sonido cada tanto en total.
	notifySoundGap = 2 * time.Second
	// notifyPermTTL: cuánto se reutiliza la lectura del permiso del sistema (el registro no es gratis).
	notifyPermTTL = 5 * time.Second
	// notifyStoreTimeout: tope de cada lectura del almacén al decidir.
	notifyStoreTimeout = 5 * time.Second
	// notifySeenMax: cuántos mensajes recientes se recuerdan para no avisar dos veces del mismo.
	notifySeenMax = 512
	// notifyHiddenBody: el cuerpo cuando el ajuste de vista previa está apagado.
	notifyHiddenBody = "Mensaje nuevo"
	// notifyTestID / notifyTestBody: la notificación de prueba de Ajustes.
	notifyTestID    = "dconnector-test"
	notifyTestTitle = "dConnector"
	notifyTestBody  = "Así se verán tus notificaciones"
	// notifyBusBuffer: búfer del suscriptor al bus.
	notifyBusBuffer = 2048
)

// NotifySettingsReader: de dónde se leen los ajustes (lo cumple *Settings).
type NotifySettingsReader interface {
	All(ctx context.Context) (AppSettings, error)
}

// NotifyChatReader: de dónde se lee el chat de un mensaje (lo cumple *Inbox).
type NotifyChatReader interface {
	GetChat(ctx context.Context, key model.ChatKey) (model.Chat, error)
}

// NotificationsStatus: lo que Notifications sabe de sí mismo (para la pantalla de Ajustes).
type NotificationsStatus struct {
	Permission port.NotifyPermission `json:"permission"`
	// Listening: el servicio está escuchando el bus.
	Listening bool `json:"listening"`
	// Shown, Sounds y Skipped: contadores desde el arranque (solo números, nunca textos).
	Shown   int64 `json:"shown"`
	Sounds  int64 `json:"sounds"`
	Skipped int64 `json:"skipped"`
	// LastDecision: qué se decidió con el último mensaje entrante y por qué (para entender por qué no sonó).
	LastDecision NotifyDecision `json:"lastDecision"`
}

// NotifyDecision: el motivo de lo último que decidió el servicio, en español, con su hora.
type NotifyDecision struct {
	// Text: «Último mensaje: aviso y sonido», «Último mensaje: no avisó: el chat está silenciado», etc. Vacío si
	// todavía no llegó ningún mensaje.
	Text string `json:"text"`
	// At: hora de la decisión (milisegundos Unix); 0 si no hay.
	At int64 `json:"at"`
}

// NotificationsOption ajusta Notifications al construirlo.
type NotificationsOption func(*Notifications)

// WithNotificationsClock cambia el reloj (pruebas).
func WithNotificationsClock(now func() time.Time) NotificationsOption {
	return func(n *Notifications) {
		if now != nil {
			n.now = now
		}
	}
}

// WithWindowProbe añade una comprobación en vivo de «la ventana está visible y con foco». Se combina con
// lo que informa SetFocus: sin ella solo se confía en SetFocus. Útil porque ocultar la ventana a la
// bandeja no siempre avisa a la interfaz.
func WithWindowProbe(active func() bool) NotificationsOption {
	return func(n *Notifications) { n.windowActive = active }
}

// WithSyncProbe indica si una cuenta está sincronizando historial: lo que llega mientras tanto no avisa.
func WithSyncProbe(syncing func(model.AccountID) bool) NotificationsOption {
	return func(n *Notifications) { n.syncing = syncing }
}

// WithAvatarResolver cambia cómo se halla el archivo de la foto del chat (por defecto, el de la carpeta
// de avatares de la cuenta). Devolver "" = sin foto.
func WithAvatarResolver(f func(model.Chat) string) NotificationsOption {
	return func(n *Notifications) { n.avatar = f }
}

// Notifications escucha el bus y avisa de los mensajes entrantes nuevos que nadie ha visto.
//
// Por cada mensaje decide, en este orden: (1) ¿es un mensaje en vivo y ajeno (o uno propio escrito en el
// chat «Tú» desde otro dispositivo)?; (2) ¿su chat está silenciado o archivado? → nada; (3) ¿ya se vio (chat
// abierto con la ventana activa) o ya consta como leído? → nada; (4) con «Notificaciones» encendidas y el
// permiso del sistema concedido → notificación, SIEMPRE sin sonido propio; (5) con el ajuste de sonido
// encendido, el sonido propio de dConnector suena SIEMPRE, aunque la notificación esté apagada, bloqueada o
// la oculte «No molestar» de Windows (que silencia los toasts). Las ráfagas se agrupan: una notificación
// cada 1,5 s por chat y un sonido cada 2 s en total. Cada decisión queda en LastDecision.
type Notifications struct {
	b        *bus.Bus
	settings NotifySettingsReader
	chats    NotifyChatReader
	notifier port.Notifier

	now          func() time.Time
	windowActive func() bool
	syncing      func(model.AccountID) bool
	avatar       func(model.Chat) string

	mu        sync.Mutex
	focusChat model.ChatKey
	focusWin  bool
	lastToast map[model.ChatKey]time.Time
	lastSound time.Time
	seen      map[string]struct{}
	seenOrder []string
	readKeys  map[model.MessageKey]struct{}
	readOrder []model.MessageKey
	perm      port.NotifyPermission
	permAt    time.Time
	last      NotifyDecision

	lifeMu    sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	listening atomic.Bool

	shown, sounds, skipped atomic.Int64
}

// NewNotifications crea el servicio. Hasta llamar a Start no escucha nada.
func NewNotifications(b *bus.Bus, set NotifySettingsReader, chats NotifyChatReader, nt port.Notifier, opts ...NotificationsOption) *Notifications {
	n := &Notifications{
		b: b, settings: set, chats: chats, notifier: nt,
		now:       time.Now,
		lastToast: map[model.ChatKey]time.Time{},
		seen:      map[string]struct{}{},
		readKeys:  map[model.MessageKey]struct{}{},
	}
	n.avatar = defaultNotifyAvatar
	for _, o := range opts {
		o(n)
	}
	return n
}

// Start empieza a escuchar el bus. Es seguro llamarlo más de una vez.
func (n *Notifications) Start(ctx context.Context) {
	n.lifeMu.Lock()
	defer n.lifeMu.Unlock()
	if n.cancel != nil || n.b == nil {
		return
	}
	ch, unsub := n.b.Subscribe("notifications", notifyBusBuffer)
	cctx, cancel := context.WithCancel(ctx)
	n.cancel = cancel
	n.done = make(chan struct{})
	n.listening.Store(true)
	go func(done chan struct{}) {
		defer close(done)
		defer n.listening.Store(false)
		defer unsub()
		for {
			select {
			case <-cctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				n.safeHandle(cctx, ev)
			}
		}
	}(n.done)
}

// Stop deja de escuchar y espera a que termine lo que estaba haciendo.
func (n *Notifications) Stop() {
	n.lifeMu.Lock()
	cancel, done := n.cancel, n.done
	n.cancel, n.done = nil, nil
	n.lifeMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// Listening dice si el servicio está escuchando el bus.
func (n *Notifications) Listening() bool { return n.listening.Load() }

// SetFocus lo llama la interfaz: qué chat tiene abierto y si la ventana tiene el foco. Un mensaje de
// ese chat, con la ventana activa, ya se está viendo y no avisa. chatKey vacío = ningún chat abierto.
func (n *Notifications) SetFocus(chatKey model.ChatKey, windowFocused bool) {
	n.mu.Lock()
	n.focusChat, n.focusWin = chatKey, windowFocused
	n.mu.Unlock()
}

func (n *Notifications) safeHandle(ctx context.Context, ev model.Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("pánico recuperado", "where", "notificaciones", "panic", r)
		}
	}()
	n.handle(ctx, ev)
}

func (n *Notifications) handle(ctx context.Context, ev model.Event) {
	switch ev.Type {
	case model.EvMessageNew:
		if ev.Message != nil {
			n.onMessage(ctx, *ev.Message)
		}
	case model.EvReceipt:
		if ev.Receipt != nil {
			n.onReceipt(*ev.Receipt)
		}
	}
}

// onReceipt recuerda los mensajes que ya constan como leídos (en otro dispositivo).
func (n *Notifications) onReceipt(r model.ReceiptEvent) {
	if r.Delivery != model.DeliveryRead && r.Delivery != model.DeliveryPlayed {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, k := range r.MessageKeys {
		if _, ok := n.readKeys[k]; ok {
			continue
		}
		n.readKeys[k] = struct{}{}
		n.readOrder = append(n.readOrder, k)
	}
	for len(n.readOrder) > notifySeenMax {
		delete(n.readKeys, n.readOrder[0])
		n.readOrder = n.readOrder[1:]
	}
}

// rememberMessage anota un mensaje como ya atendido. Devuelve false si ya estaba anotado (repetido) o
// ya constaba como leído en otro dispositivo; read dice cuál de los dos casos fue.
func (n *Notifications) rememberMessage(m model.Message) (ok, read bool) {
	id := string(m.Key)
	if id == "" {
		id = string(m.ChatKey) + "#" + m.NativeID
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, isRead := n.readKeys[m.Key]; isRead && m.Key != "" {
		return false, true
	}
	if _, dup := n.seen[id]; dup {
		return false, false
	}
	n.seen[id] = struct{}{}
	n.seenOrder = append(n.seenOrder, id)
	for len(n.seenOrder) > notifySeenMax {
		delete(n.seen, n.seenOrder[0])
		n.seenOrder = n.seenOrder[1:]
	}
	return true, false
}

// decision: el resultado de evaluar un mensaje. code es un motivo fijo para el registro (nunca texto del
// mensaje ni teléfonos); text es lo que se muestra en Ajustes.
type decision struct {
	code string
	text string
}

// Decisiones sin aviso ni sonido.
var (
	decOld      = decision{"viejo", "no avisó: era un mensaje demasiado antiguo (del historial)"}
	decSyncing  = decision{"historial", "no avisó: la cuenta estaba sincronizando historial"}
	decOwn      = decision{"propio", "no avisó: era un mensaje tuyo"}
	decRepeat   = decision{"repetido", "no avisó: ese mensaje ya se había atendido"}
	decRead     = decision{"leido", "no avisó: ya lo habías visto (consta como leído)"}
	decMuted    = decision{"silenciado", "no avisó: el chat está silenciado"}
	decArchived = decision{"archivado", "no avisó: el chat está archivado"}
	decSeen     = decision{"visto", "no avisó: el chat está abierto en pantalla"}
	decNoChat   = decision{"chat-ilegible", "no avisó: no se pudo leer el chat"}
	decNoConfig = decision{"ajustes-ilegibles", "no avisó: no se pudieron leer los ajustes"}
)

// record guarda la última decisión y la anota en el registro con su motivo fijo.
func (n *Notifications) record(d decision, now time.Time) {
	n.mu.Lock()
	n.last = NotifyDecision{Text: "Último mensaje: " + d.text, At: now.UnixMilli()}
	n.mu.Unlock()
	slog.Debug("notificaciones: decisión", "motivo", d.code)
}

// skipWith cuenta y anota un mensaje que no avisó ni sonó.
func (n *Notifications) skipWith(d decision) {
	n.skipped.Add(1)
	n.record(d, n.now())
}

// onMessage decide y ejecuta el aviso de un mensaje entrante.
func (n *Notifications) onMessage(ctx context.Context, m model.Message) {
	// 1. Solo mensajes de contenido real y en vivo.
	if m.Kind == model.KindSystem || m.DeletedAt > 0 {
		return
	}
	now := n.now()
	if m.Timestamp > 0 && now.Sub(time.UnixMilli(m.Timestamp)) > notifyMaxAge {
		n.skipWith(decOld)
		return
	}
	if n.syncing != nil && n.syncing(m.AccountID) {
		n.skipWith(decSyncing)
		return
	}

	// Un chat que aún no existe no puede estar silenciado: se avisa. Si el almacén falla, ante la duda no
	// se molesta.
	chat, found, err := n.loadChat(ctx, m.ChatKey)
	if err != nil {
		n.skipWith(decNoChat)
		return
	}
	self := found && chat.Kind == model.ChatSelf
	// Un mensaje propio solo avisa en el chat «Tú» (la persona se escribe notas desde otro dispositivo).
	if m.FromMe && !self {
		n.skipWith(decOwn)
		return
	}
	if ok, read := n.rememberMessage(m); !ok {
		if read {
			n.skipWith(decRead)
		} else {
			n.skipWith(decRepeat)
		}
		return
	}

	// 2. Silenciado o archivado → nada (ni notificación ni sonido).
	if found {
		if chat.MutedUntil == -1 || chat.MutedUntil > now.UnixMilli() {
			n.skipWith(decMuted)
			return
		}
		if chat.Archived {
			// Decisión prudente: un chat archivado no avisa (en WhatsApp el archivado sí puede avisar, pero
			// aquí no se puede saber qué eligió la persona).
			n.skipWith(decArchived)
			return
		}
		// «No visto» = no leído: si tras este mensaje el chat ya está sin leer nada, se leyó en otro lado.
		// En el chat «Tú» no se exige: lo propio nunca suma no leídos.
		if chat.UnreadCount == 0 && !self {
			n.skipWith(decRead)
			return
		}
	}

	// 3. ¿Ya se está viendo?
	if n.isBeingSeen(m.ChatKey) {
		n.skipWith(decSeen)
		return
	}

	// 4 y 5. Notificación y sonido, cada uno por su lado.
	set, err := n.settings.All(ctx)
	if err != nil {
		n.skipWith(decNoConfig)
		return
	}
	n.deliver(ctx, now, m, chat, found, set)
}

func (n *Notifications) loadChat(ctx context.Context, key model.ChatKey) (model.Chat, bool, error) {
	cctx, cancel := context.WithTimeout(ctx, notifyStoreTimeout)
	defer cancel()
	c, err := n.chats.GetChat(cctx, key)
	switch {
	case err == nil:
		return c, true, nil
	case errors.Is(err, port.ErrNotFound):
		return model.Chat{}, false, nil
	}
	return model.Chat{}, false, err
}

func (n *Notifications) isBeingSeen(key model.ChatKey) bool {
	n.mu.Lock()
	seen := key != "" && n.focusWin && n.focusChat == key
	n.mu.Unlock()
	return seen && (n.windowActive == nil || n.windowActive())
}

// deliver ejecuta la notificación y el sonido de un mensaje que sí debe avisar. Son independientes: el
// sonido propio de dConnector suena siempre que el ajuste esté encendido (Windows en «No molestar» silencia
// los toasts, no esto) y la notificación sale siempre sin sonido propio para no duplicarlo.
func (n *Notifications) deliver(ctx context.Context, now time.Time, m model.Message, chat model.Chat, found bool, set AppSettings) {
	perm := port.NotifyPermission{State: port.NotifyDenied}
	if set.Notifications {
		perm = n.permission(ctx, now, false)
	}
	canToast := set.Notifications && (perm.State == port.NotifyGranted || perm.State == port.NotifyUnknown)

	n.mu.Lock()
	chatOK := now.Sub(n.lastToast[m.ChatKey]) >= notifyChatGap
	soundOK := set.NotificationsSound && now.Sub(n.lastSound) >= notifySoundGap
	doToast := canToast && chatOK
	if doToast {
		n.lastToast[m.ChatKey] = now
		n.pruneToastsLocked(now)
	}
	if soundOK {
		n.lastSound = now
	}
	n.mu.Unlock()

	toastWhy, soundWhy := "", ""
	toasted := false
	if doToast {
		toast := port.Notification{
			ID:      string(m.ChatKey) + "#" + m.NativeID,
			Title:   notifyTitle(chat, found, m),
			Body:    notifyBody(m, set.NotificationsPreview),
			ChatKey: m.ChatKey,
			Silent:  true, // el sonido es el propio de dConnector (abajo): No molestar no lo silencia
		}
		if found && n.avatar != nil {
			toast.AvatarPath = n.avatar(chat)
		}
		if err := n.notifier.Show(ctx, toast); err != nil {
			slog.Warn("notificaciones: no se pudo mostrar", "error", err)
			toastWhy = "no se pudo mostrar el aviso"
		} else {
			n.shown.Add(1)
			toasted = true
		}
	} else {
		switch {
		case !set.Notifications:
			toastWhy = "las notificaciones están apagadas en Ajustes"
		case perm.State == port.NotifyUnsupported:
			toastWhy = "este sistema no tiene notificaciones de escritorio"
		case !canToast:
			toastWhy = "Windows tiene bloqueadas las notificaciones de dConnector"
		default:
			toastWhy = "se agrupó con el aviso anterior de ese chat"
		}
	}

	played := false
	switch {
	case soundOK:
		played = n.playSound(ctx)
		if !played {
			soundWhy = "no se pudo reproducir el sonido"
		}
	case !set.NotificationsSound:
		soundWhy = "el sonido está apagado en Ajustes"
	default:
		soundWhy = "otro sonido acababa de sonar"
	}

	n.record(deliveryDecision(toasted, played, toastWhy, soundWhy, perm), now)
	if !toasted && !played {
		n.skipped.Add(1)
	}
}

// deliveryDecision compone la decisión de un mensaje que sí calificaba para avisar.
func deliveryDecision(toasted, sound bool, toastWhy, soundWhy string, perm port.NotifyPermission) decision {
	quiet := ""
	if toasted && perm.Quiet == port.QuietActive {
		quiet = " (Windows está en «No molestar»: el aviso queda en el centro de notificaciones)"
	}
	switch {
	case toasted && sound:
		return decision{"aviso-y-sonido", "aviso y sonido" + quiet}
	case toasted:
		return decision{"aviso", "aviso sin sonido (" + soundWhy + ")" + quiet}
	case sound:
		return decision{"solo-sonido", "solo sonido (" + toastWhy + ")"}
	}
	return decision{"nada", "no avisó (" + toastWhy + "; " + soundWhy + ")"}
}

// playSound reproduce el sonido propio; false si no se pudo.
func (n *Notifications) playSound(ctx context.Context) bool {
	if err := n.notifier.PlaySound(ctx); err != nil {
		slog.Debug("notificaciones: no se pudo sonar", "error", err)
		return false
	}
	n.sounds.Add(1)
	return true
}

// pruneToastsLocked tira las marcas de chats que ya pasaron su espera (mantiene el mapa pequeño).
func (n *Notifications) pruneToastsLocked(now time.Time) {
	if len(n.lastToast) < 256 {
		return
	}
	for k, t := range n.lastToast {
		if now.Sub(t) > time.Minute {
			delete(n.lastToast, k)
		}
	}
}

// notifyTitle: el nombre del chat; en grupos, «Persona · Grupo».
func notifyTitle(chat model.Chat, found bool, m model.Message) string {
	name := ""
	if found {
		name = strings.TrimSpace(chat.Title)
	}
	sender := strings.TrimSpace(m.SenderName)
	switch {
	case found && chat.Kind == model.ChatGroup && sender != "" && name != "" && sender != name:
		return sender + " · " + name
	case name != "":
		return name
	case sender != "":
		return sender
	}
	return "dConnector"
}

// notifyBody: la vista previa del mensaje, o un texto neutro si la privacidad lo pide.
func notifyBody(m model.Message, preview bool) string {
	if !preview {
		return notifyHiddenBody
	}
	if b := model.Preview(m); b != "" {
		return b
	}
	return notifyHiddenBody
}

// defaultNotifyAvatar: «/avatar/<cuenta>/<archivo>» → la ruta del archivo, si existe y es de un formato
// que el sistema pinta en una notificación.
func defaultNotifyAvatar(c model.Chat) string {
	rest, ok := strings.CutPrefix(c.AvatarURL, "/avatar/")
	if !ok {
		return ""
	}
	acc, file, ok := strings.Cut(rest, "/")
	if !ok || acc == "" || file == "" || strings.ContainsAny(file, `/\`) || strings.Contains(file, "..") {
		return ""
	}
	switch strings.ToLower(filepath.Ext(file)) {
	case ".jpg", ".jpeg", ".png":
	default:
		return ""
	}
	p := filepath.Join(paths.AvatarDir(model.AccountID(acc)), file)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ""
	}
	return p
}

// permission devuelve el permiso del sistema, reutilizando la última lectura unos segundos.
func (n *Notifications) permission(ctx context.Context, now time.Time, force bool) port.NotifyPermission {
	n.mu.Lock()
	if !force && n.permAt != (time.Time{}) && now.Sub(n.permAt) < notifyPermTTL {
		p := n.perm
		n.mu.Unlock()
		return p
	}
	n.mu.Unlock()
	p := n.notifier.Permission(ctx)
	n.mu.Lock()
	n.perm, n.permAt = p, now
	n.mu.Unlock()
	return p
}

// Permission lee el permiso actual del sistema (sin usar la lectura guardada).
func (n *Notifications) Permission(ctx context.Context) port.NotifyPermission {
	return n.permission(ctx, n.now(), true)
}

// Status resume el estado del servicio para Ajustes.
func (n *Notifications) Status(ctx context.Context) NotificationsStatus {
	return NotificationsStatus{
		Permission: n.Permission(ctx),
		Listening:  n.Listening(),
		Shown:      n.shown.Load(),
		Sounds:     n.sounds.Load(),
		Skipped:    n.skipped.Load(),

		LastDecision: n.lastDecision(),
	}
}

func (n *Notifications) lastDecision() NotifyDecision {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.last
}

// RequestPermission pide el permiso al sistema (en Windows lleva a Ajustes si está bloqueado).
func (n *Notifications) RequestPermission(ctx context.Context) (port.NotifyPermission, error) {
	p, err := n.notifier.RequestPermission(ctx)
	n.mu.Lock()
	n.perm, n.permAt = p, n.now()
	n.mu.Unlock()
	return p, err
}

// OpenSystemSettings abre la pantalla de notificaciones del sistema.
func (n *Notifications) OpenSystemSettings() error { return n.notifier.OpenSystemSettings() }

// NotifyTestResult: lo que de verdad pasó con la notificación de prueba (no solo «se envió»).
type NotifyTestResult struct {
	// Permission: el permiso del sistema leído en ese momento, con el estado de «No molestar».
	Permission port.NotifyPermission `json:"permission"`
	// Shown: la notificación se entregó a Windows (si no hay supresión, aparece; si no, queda en el centro).
	Shown bool `json:"shown"`
	// Visible: se verá en pantalla (entregada y sin «No molestar», pantalla completa ni presentación).
	Visible bool `json:"visible"`
	// Sound: sonó el aviso propio de dConnector.
	Sound bool `json:"sound"`
	// Message: lo que hay que decirle a la persona, en español.
	Message string `json:"message"`
}

// SendTest envía una notificación de prueba y suena (con el ajuste de sonido encendido), y dice con
// honestidad qué pasó: «Aviso enviado» solo si nada lo suprime; con «No molestar», pantalla completa,
// presentación o el permiso apagado, explica que no se verá y que el sonido sí suena. Ignora el interruptor
// «Notificaciones»: sirve justamente para comprobar si el sistema las deja pasar.
func (n *Notifications) SendTest(ctx context.Context) (NotifyTestResult, error) {
	perm := n.Permission(ctx)
	sound := true
	if set, err := n.settings.All(ctx); err == nil {
		sound = set.NotificationsSound
	}
	res := NotifyTestResult{Permission: perm}

	var showErr error
	if perm.State == port.NotifyGranted || perm.State == port.NotifyUnknown {
		showErr = n.notifier.Show(ctx, port.Notification{
			ID: notifyTestID, Title: notifyTestTitle, Body: notifyTestBody, Silent: true,
		})
		if showErr != nil {
			slog.Warn("notificaciones: la prueba no se pudo mostrar", "error", showErr)
		}
		res.Shown = showErr == nil
	}
	if sound && n.notifier.PlaySound(ctx) == nil {
		res.Sound = true
	}
	res.Visible = res.Shown && perm.Quiet != port.QuietActive
	res.Message = testMessage(perm, res.Shown, showErr, sound, res.Sound)
	return res, nil
}

// testMessage redacta el resultado de la prueba.
func testMessage(perm port.NotifyPermission, shown bool, showErr error, soundOn, played bool) string {
	// «…no verás el aviso en pantalla, pero sonará el aviso» / «…y el sonido está apagado en Ajustes».
	tail := func() string {
		switch {
		case played:
			return ", pero sonará el aviso."
		case soundOn:
			return " y no se pudo reproducir el sonido."
		}
		return " y el sonido está apagado en Ajustes."
	}
	switch {
	case showErr != nil:
		return "No se pudo mostrar la notificación de prueba (" + showErr.Error() + "): no la verás en pantalla" + tail()
	case !shown && perm.State == port.NotifyUnsupported:
		return "Este sistema no tiene notificaciones de escritorio" + tail()
	case !shown:
		return perm.Reason + " No verás el aviso en pantalla" + tail()
	case perm.Quiet == port.QuietActive:
		lead := "Windows está en «No molestar»"
		switch perm.QuietCause {
		case port.QuietCauseFullscreen:
			lead = "Hay una aplicación a pantalla completa"
		case port.QuietCausePresentation:
			lead = "Windows está en modo presentación"
		}
		return lead + ": no verás el aviso en pantalla" + tail() + " Está en el centro de notificaciones."
	case perm.Quiet != port.QuietInactive:
		return "Aviso enviado. No se pudo comprobar si Windows está en «No molestar»: si no lo ves, revisa esa opción."
	}
	return "Aviso enviado"
}
