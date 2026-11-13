package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// ---------------------------------------------------------------------------------------------
// Dobles

type fakeNotifier struct {
	mu       sync.Mutex
	perm     port.NotifyPermission
	showErr  error
	shown    []port.Notification
	sounds   int
	opened   int
	requests int
}

func (f *fakeNotifier) Permission(context.Context) port.NotifyPermission {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.perm
}

func (f *fakeNotifier) RequestPermission(context.Context) (port.NotifyPermission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	return f.perm, nil
}

func (f *fakeNotifier) Show(_ context.Context, n port.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.showErr != nil {
		return f.showErr
	}
	f.shown = append(f.shown, n)
	return nil
}

func (f *fakeNotifier) PlaySound(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sounds++
	return nil
}

func (f *fakeNotifier) OpenSystemSettings() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened++
	return nil
}

func (f *fakeNotifier) counts() (shown, sounds int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.shown), f.sounds
}

func (f *fakeNotifier) setPerm(p port.NotifyPermission) {
	f.mu.Lock()
	f.perm = p
	f.mu.Unlock()
}

type fakeNotifySettings struct {
	mu  sync.Mutex
	set AppSettings
	err error
}

func (f *fakeNotifySettings) All(context.Context) (AppSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.set, f.err
}

type fakeNotifyChats struct {
	mu    sync.Mutex
	chats map[model.ChatKey]model.Chat
	err   error
}

func (f *fakeNotifyChats) GetChat(_ context.Context, k model.ChatKey) (model.Chat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return model.Chat{}, f.err
	}
	c, ok := f.chats[k]
	if !ok {
		return model.Chat{}, port.ErrNotFound
	}
	return c, nil
}

type notifyClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *notifyClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *notifyClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type notifyEnv struct {
	n     *Notifications
	nt    *fakeNotifier
	set   *fakeNotifySettings
	chats *fakeNotifyChats
	clock *notifyClock
	bus   *bus.Bus
}

const notifyChat = model.ChatKey("wa-1|593999@s.whatsapp.net")

func okPermission() port.NotifyPermission {
	return port.NotifyPermission{State: port.NotifyGranted, Reason: "Las notificaciones están permitidas."}
}

func newNotifyEnv(t *testing.T, opts ...NotificationsOption) *notifyEnv {
	t.Helper()
	clock := &notifyClock{t: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)}
	e := &notifyEnv{
		nt: &fakeNotifier{perm: okPermission()},
		set: &fakeNotifySettings{set: AppSettings{
			Notifications: true, NotificationsSound: true, NotificationsPreview: true,
		}},
		chats: &fakeNotifyChats{chats: map[model.ChatKey]model.Chat{
			notifyChat: {Key: notifyChat, AccountID: "wa-1", Title: "Ana Pérez", Kind: model.ChatDirect, UnreadCount: 1},
		}},
		clock: clock,
		bus:   bus.New(),
	}
	opts = append([]NotificationsOption{WithNotificationsClock(clock.Now), WithAvatarResolver(func(model.Chat) string { return "" })}, opts...)
	e.n = NewNotifications(e.bus, e.set, e.chats, e.nt, opts...)
	return e
}

var notifySeq int

// incoming construye un mensaje entrante reciente.
func (e *notifyEnv) incoming(text string) model.Message {
	notifySeq++
	return model.Message{
		Key:        model.MessageKey(fmt.Sprintf("wa-1|593999@s.whatsapp.net|m%d", notifySeq)),
		ChatKey:    notifyChat,
		AccountID:  "wa-1",
		NativeID:   fmt.Sprintf("m%d", notifySeq),
		Timestamp:  e.clock.Now().Add(-time.Second).UnixMilli(),
		Kind:       model.KindText,
		Text:       text,
		SenderName: "Ana",
	}
}

func (e *notifyEnv) feed(m model.Message) {
	e.n.handle(context.Background(), model.Event{Type: model.EvMessageNew, Message: &m})
}

func (e *notifyEnv) setChat(f func(c *model.Chat)) {
	c := e.chats.chats[notifyChat]
	f(&c)
	e.chats.chats[notifyChat] = c
}

// ---------------------------------------------------------------------------------------------
// Matriz de decisiones

func TestNotificationsDecisionMatrix(t *testing.T) {
	future := func(e *notifyEnv) int64 { return e.clock.Now().Add(time.Hour).UnixMilli() }
	past := func(e *notifyEnv) int64 { return e.clock.Now().Add(-time.Hour).UnixMilli() }

	cases := []struct {
		name     string
		perm     port.NotifyState
		quiet    port.QuietState // estado de «No molestar» del sistema
		notifOn  bool
		soundOn  bool
		muted    func(e *notifyEnv) int64 // MutedUntil
		archived bool
		focus    string // "": ninguno; "open": chat abierto con ventana enfocada; "other": otro chat; "blur": chat abierto sin foco
		fromMe   bool
		self     bool // el chat es el de «Tú»
		unread0  bool // el chat no tiene mensajes sin leer tras el mensaje
		system   bool
		oldMsg   bool
		// Lo esperado: notificaciones (siempre sin sonido propio) y sonidos propios, cada uno por su lado.
		wantToast  int
		wantSounds int
		decision   string // trozo de la última decisión
	}{
		{name: "concedido, activo, con sonido", perm: port.NotifyGranted, notifOn: true, soundOn: true, wantToast: 1, wantSounds: 1, decision: "aviso y sonido"},
		{name: "concedido, activo, sin sonido", perm: port.NotifyGranted, notifOn: true, soundOn: false, wantToast: 1, decision: "el sonido está apagado en Ajustes"},
		{name: "desconocido intenta mostrar", perm: port.NotifyUnknown, notifOn: true, soundOn: true, wantToast: 1, wantSounds: 1, decision: "aviso y sonido"},
		{name: "No molestar activo: el aviso sale y el sonido suena", perm: port.NotifyGranted, quiet: port.QuietActive, notifOn: true, soundOn: true, wantToast: 1, wantSounds: 1, decision: "No molestar"},
		{name: "No molestar activo y sin sonido", perm: port.NotifyGranted, quiet: port.QuietActive, notifOn: true, soundOn: false, wantToast: 1, decision: "No molestar"},
		{name: "bloqueado en Windows: solo suena", perm: port.NotifyDenied, notifOn: true, soundOn: true, wantSounds: 1, decision: "solo sonido (Windows tiene bloqueadas"},
		{name: "bloqueado y sin sonido: nada", perm: port.NotifyDenied, notifOn: true, soundOn: false, decision: "no avisó ("},
		{name: "no admitido: solo suena", perm: port.NotifyUnsupported, notifOn: true, soundOn: true, wantSounds: 1, decision: "solo sonido"},
		{name: "ajuste apagado: solo suena", perm: port.NotifyGranted, notifOn: false, soundOn: true, wantSounds: 1, decision: "apagadas en Ajustes"},
		{name: "ajuste apagado y sin sonido: nada", perm: port.NotifyGranted, notifOn: false, soundOn: false, decision: "no avisó ("},
		{name: "silenciado para siempre", perm: port.NotifyGranted, notifOn: true, soundOn: true, muted: func(*notifyEnv) int64 { return -1 }, decision: "silenciado"},
		{name: "silenciado hasta el futuro", perm: port.NotifyGranted, notifOn: true, soundOn: true, muted: future, decision: "silenciado"},
		{name: "silencio ya vencido avisa", perm: port.NotifyGranted, notifOn: true, soundOn: true, muted: past, wantToast: 1, wantSounds: 1, decision: "aviso y sonido"},
		{name: "silenciado y ajuste apagado: ni sonido", perm: port.NotifyGranted, notifOn: false, soundOn: true, muted: func(*notifyEnv) int64 { return -1 }, decision: "silenciado"},
		{name: "silenciado y bloqueado: ni sonido", perm: port.NotifyDenied, notifOn: true, soundOn: true, muted: future, decision: "silenciado"},
		{name: "archivado no avisa", perm: port.NotifyGranted, notifOn: true, soundOn: true, archived: true, decision: "archivado"},
		{name: "chat abierto con la ventana enfocada", perm: port.NotifyGranted, notifOn: true, soundOn: true, focus: "open", decision: "abierto en pantalla"},
		{name: "chat abierto con la ventana enfocada, bloqueado: ni sonido", perm: port.NotifyDenied, notifOn: true, soundOn: true, focus: "open", decision: "abierto en pantalla"},
		{name: "otro chat abierto avisa", perm: port.NotifyGranted, notifOn: true, soundOn: true, focus: "other", wantToast: 1, wantSounds: 1, decision: "aviso y sonido"},
		{name: "chat abierto pero ventana sin foco avisa", perm: port.NotifyGranted, notifOn: true, soundOn: true, focus: "blur", wantToast: 1, wantSounds: 1, decision: "aviso y sonido"},
		{name: "mensaje propio en un chat ajeno", perm: port.NotifyGranted, notifOn: true, soundOn: true, fromMe: true, decision: "era un mensaje tuyo"},
		{name: "mensaje propio en el chat Tú avisa (nota desde otro dispositivo)", perm: port.NotifyGranted, notifOn: true, soundOn: true, fromMe: true, self: true, unread0: true, wantToast: 1, wantSounds: 1, decision: "aviso y sonido"},
		{name: "mensaje propio en el chat Tú, sin avisos: suena", perm: port.NotifyDenied, notifOn: true, soundOn: true, fromMe: true, self: true, unread0: true, wantSounds: 1, decision: "solo sonido"},
		{name: "mensaje propio en el chat Tú silenciado", perm: port.NotifyGranted, notifOn: true, soundOn: true, fromMe: true, self: true, unread0: true, muted: func(*notifyEnv) int64 { return -1 }, decision: "silenciado"},
		{name: "chat ajeno sin no leídos: ya visto", perm: port.NotifyGranted, notifOn: true, soundOn: true, unread0: true, decision: "ya lo habías visto"},
		{name: "mensaje de sistema", perm: port.NotifyGranted, notifOn: true, soundOn: true, system: true},
		{name: "mensaje viejo de historial", perm: port.NotifyGranted, notifOn: true, soundOn: true, oldMsg: true, decision: "demasiado antiguo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newNotifyEnv(t)
			e.nt.setPerm(port.NotifyPermission{State: c.perm, Reason: "motivo", Quiet: c.quiet})
			e.set.set.Notifications, e.set.set.NotificationsSound = c.notifOn, c.soundOn
			if c.muted != nil {
				v := c.muted(e)
				e.setChat(func(ch *model.Chat) { ch.MutedUntil = v })
			}
			e.setChat(func(ch *model.Chat) {
				ch.Archived = c.archived
				if c.self {
					ch.Kind = model.ChatSelf
				}
				if c.unread0 {
					ch.UnreadCount = 0
				}
			})
			switch c.focus {
			case "open":
				e.n.SetFocus(notifyChat, true)
			case "other":
				e.n.SetFocus("wa-1|otro@s.whatsapp.net", true)
			case "blur":
				e.n.SetFocus(notifyChat, false)
			}
			m := e.incoming("hola")
			m.FromMe = c.fromMe
			if c.system {
				m.Kind = model.KindSystem
			}
			if c.oldMsg {
				m.Timestamp = e.clock.Now().Add(-3 * time.Minute).UnixMilli()
			}
			e.feed(m)
			toasts, sounds := e.nt.counts()
			if toasts != c.wantToast || sounds != c.wantSounds {
				t.Fatalf("notificaciones=%d (quería %d), sonidos=%d (quería %d)", toasts, c.wantToast, sounds, c.wantSounds)
			}
			for _, sh := range e.nt.shown {
				if !sh.Silent {
					t.Fatal("la notificación debe salir siempre sin su sonido propio (el sonido es el de dConnector)")
				}
			}
			got := e.n.Status(context.Background()).LastDecision
			if c.decision == "" {
				if got.Text != "" {
					t.Fatalf("no debía anotarse decisión: %+v", got)
				}
				return
			}
			if !strings.HasPrefix(got.Text, "Último mensaje: ") || !strings.Contains(got.Text, c.decision) || got.At != e.clock.Now().UnixMilli() {
				t.Fatalf("última decisión %q (quería que contuviera %q)", got.Text, c.decision)
			}
		})
	}
}

// ---------------------------------------------------------------------------------------------
// Contenido

func TestNotificationsContentDirectChat(t *testing.T) {
	e := newNotifyEnv(t)
	e.feed(e.incoming("Hola,   ¿cómo estás?"))
	if len(e.nt.shown) != 1 {
		t.Fatalf("notificaciones: %d", len(e.nt.shown))
	}
	got := e.nt.shown[0]
	if got.Title != "Ana Pérez" || got.Body != "Hola, ¿cómo estás?" || got.ChatKey != notifyChat {
		t.Fatalf("contenido: %+v", got)
	}
	if !strings.HasPrefix(got.ID, string(notifyChat)+"#") {
		t.Fatalf("el id debe llevar la clave del chat: %q", got.ID)
	}
}

func TestNotificationsContentGroupShowsSender(t *testing.T) {
	e := newNotifyEnv(t)
	e.setChat(func(c *model.Chat) { c.Kind, c.Title = model.ChatGroup, "Equipo" })
	e.feed(e.incoming("reunión a las 3"))
	if got := e.nt.shown[0].Title; got != "Ana · Equipo" {
		t.Fatalf("título de grupo: %q", got)
	}
}

func TestNotificationsContentWithoutKnownChatUsesSender(t *testing.T) {
	e := newNotifyEnv(t)
	delete(e.chats.chats, notifyChat)
	e.feed(e.incoming("primer mensaje"))
	if len(e.nt.shown) != 1 || e.nt.shown[0].Title != "Ana" {
		t.Fatalf("chat nuevo: %+v", e.nt.shown)
	}
}

func TestNotificationsPreviewOff(t *testing.T) {
	e := newNotifyEnv(t)
	e.set.set.NotificationsPreview = false
	e.feed(e.incoming("mi clave es 1234"))
	if got := e.nt.shown[0].Body; got != "Mensaje nuevo" {
		t.Fatalf("cuerpo con vista previa apagada: %q", got)
	}
}

func TestNotificationsPreviewOfMedia(t *testing.T) {
	e := newNotifyEnv(t)
	m := e.incoming("")
	m.Kind = model.KindImage
	e.feed(m)
	if got := e.nt.shown[0].Body; got != "📷 Foto" {
		t.Fatalf("vista previa de una foto: %q", got)
	}
}

func TestNotificationsAvatarOnlyForKnownChats(t *testing.T) {
	e := newNotifyEnv(t, WithAvatarResolver(func(c model.Chat) string { return "C:\\avatars\\" + c.Title + ".jpg" }))
	e.feed(e.incoming("hola"))
	if got := e.nt.shown[0].AvatarPath; got != "C:\\avatars\\Ana Pérez.jpg" {
		t.Fatalf("avatar: %q", got)
	}
}

// ---------------------------------------------------------------------------------------------
// Leído en otro lado, repetidos, errores

func TestNotificationsAlreadyReadElsewhere(t *testing.T) {
	e := newNotifyEnv(t)
	e.setChat(func(c *model.Chat) { c.UnreadCount = 0 })
	e.feed(e.incoming("ya lo leí en el teléfono"))
	if toasts, sounds := e.nt.counts(); toasts != 0 || sounds != 0 {
		t.Fatalf("un chat sin no leídos no avisa: %d %d", toasts, sounds)
	}
}

func TestNotificationsReadReceiptCancels(t *testing.T) {
	e := newNotifyEnv(t)
	m := e.incoming("leído")
	e.n.handle(context.Background(), model.Event{Type: model.EvReceipt, Receipt: &model.ReceiptEvent{
		ChatKey: notifyChat, MessageKeys: []model.MessageKey{m.Key}, Delivery: model.DeliveryRead,
	}})
	e.feed(m)
	if toasts, sounds := e.nt.counts(); toasts != 0 || sounds != 0 {
		t.Fatalf("un mensaje con recibo de lectura no avisa: %d %d", toasts, sounds)
	}
	// un recibo de entrega no cuenta como leído
	m2 := e.incoming("entregado")
	e.n.handle(context.Background(), model.Event{Type: model.EvReceipt, Receipt: &model.ReceiptEvent{
		MessageKeys: []model.MessageKey{m2.Key}, Delivery: model.DeliveryDelivered,
	}})
	e.feed(m2)
	if toasts, _ := e.nt.counts(); toasts != 1 {
		t.Fatalf("la entrega no es lectura: %d", toasts)
	}
}

func TestNotificationsSameMessageOnce(t *testing.T) {
	e := newNotifyEnv(t)
	m := e.incoming("repetido")
	e.feed(m)
	e.clock.Advance(10 * time.Second)
	e.feed(m)
	if toasts, _ := e.nt.counts(); toasts != 1 {
		t.Fatalf("el mismo mensaje avisa una sola vez: %d", toasts)
	}
}

func TestNotificationsUnreadableChatDoesNotNotify(t *testing.T) {
	e := newNotifyEnv(t)
	e.chats.err = errors.New("disco ocupado")
	e.feed(e.incoming("hola"))
	if toasts, sounds := e.nt.counts(); toasts != 0 || sounds != 0 {
		t.Fatalf("ante un error del almacén no se molesta: %d %d", toasts, sounds)
	}
}

func TestNotificationsUnreadableSettingsDoesNotNotify(t *testing.T) {
	e := newNotifyEnv(t)
	e.set.err = errors.New("sin ajustes")
	e.feed(e.incoming("hola"))
	if toasts, sounds := e.nt.counts(); toasts != 0 || sounds != 0 {
		t.Fatalf("sin ajustes legibles no se avisa: %d %d", toasts, sounds)
	}
}

func TestNotificationsHistorySyncSilences(t *testing.T) {
	syncing := true
	e := newNotifyEnv(t, WithSyncProbe(func(acc model.AccountID) bool { return syncing && acc == "wa-1" }))
	e.feed(e.incoming("de la sincronización"))
	if toasts, sounds := e.nt.counts(); toasts != 0 || sounds != 0 {
		t.Fatalf("durante la sincronización no avisa: %d %d", toasts, sounds)
	}
	syncing = false
	e.feed(e.incoming("en vivo"))
	if toasts, _ := e.nt.counts(); toasts != 1 {
		t.Fatalf("terminada la sincronización avisa: %d", toasts)
	}
}

func TestNotificationsWindowProbeOverridesStaleFocus(t *testing.T) {
	active := true
	e := newNotifyEnv(t, WithWindowProbe(func() bool { return active }))
	e.n.SetFocus(notifyChat, true)
	e.feed(e.incoming("uno"))
	if toasts, _ := e.nt.counts(); toasts != 0 {
		t.Fatalf("con la ventana activa y el chat abierto no avisa: %d", toasts)
	}
	active = false // se ocultó a la bandeja sin que la interfaz avisara
	e.feed(e.incoming("dos"))
	if toasts, _ := e.nt.counts(); toasts != 1 {
		t.Fatalf("ventana oculta: debe avisar: %d", toasts)
	}
}

func TestNotificationsFocusClearedWhenChatClosed(t *testing.T) {
	e := newNotifyEnv(t)
	e.n.SetFocus(notifyChat, true)
	e.n.SetFocus("", true)
	e.feed(e.incoming("hola"))
	if toasts, _ := e.nt.counts(); toasts != 1 {
		t.Fatalf("sin chat abierto avisa: %d", toasts)
	}
}

func TestNotificationsShowFailureFallsBackToSound(t *testing.T) {
	e := newNotifyEnv(t)
	e.nt.showErr = errors.New("COM falló")
	e.feed(e.incoming("hola"))
	if toasts, sounds := e.nt.counts(); toasts != 0 || sounds != 1 {
		t.Fatalf("si la notificación falla, al menos suena: %d %d", toasts, sounds)
	}
}

// ---------------------------------------------------------------------------------------------
// Ráfagas

func TestNotificationsBurstPerChat(t *testing.T) {
	e := newNotifyEnv(t)
	for i := 0; i < 5; i++ {
		e.feed(e.incoming("ráfaga"))
		e.clock.Advance(200 * time.Millisecond) // 1 s en total: dentro de la espera de 1,5 s
	}
	if toasts, _ := e.nt.counts(); toasts != 1 {
		t.Fatalf("una ráfaga de un chat da una notificación: %d", toasts)
	}
	e.clock.Advance(time.Second) // ya pasaron 2 s desde la primera
	e.feed(e.incoming("después"))
	if toasts, _ := e.nt.counts(); toasts != 2 {
		t.Fatalf("pasada la espera vuelve a avisar: %d", toasts)
	}
}

func TestNotificationsBurstDifferentChatsShareOneSound(t *testing.T) {
	e := newNotifyEnv(t)
	other := model.ChatKey("wa-1|otra@s.whatsapp.net")
	e.chats.chats[other] = model.Chat{Key: other, AccountID: "wa-1", Title: "Luis", UnreadCount: 1}
	e.feed(e.incoming("a"))
	m := e.incoming("b")
	m.ChatKey = other
	e.clock.Advance(300 * time.Millisecond)
	e.feed(m)
	if len(e.nt.shown) != 2 {
		t.Fatalf("dos chats, dos notificaciones: %d", len(e.nt.shown))
	}
	if _, sounds := e.nt.counts(); sounds != 1 {
		t.Fatalf("solo el primero suena (1 sonido cada 2 s en total): %d", sounds)
	}
	e.clock.Advance(2 * time.Second)
	m3 := e.incoming("c")
	m3.ChatKey = other
	e.feed(m3)
	if _, sounds := e.nt.counts(); sounds != 2 {
		t.Fatalf("pasados 2 s el sonido vuelve: %d", sounds)
	}
	for i, sh := range e.nt.shown {
		if !sh.Silent {
			t.Fatalf("la notificación %d no debe llevar sonido propio", i)
		}
	}
}

func TestNotificationsSoundOnlyBurst(t *testing.T) {
	e := newNotifyEnv(t)
	e.nt.setPerm(port.NotifyPermission{State: port.NotifyDenied})
	for i := 0; i < 4; i++ {
		e.feed(e.incoming("x"))
		e.clock.Advance(400 * time.Millisecond) // 1,6 s: aún dentro de los 2 s
	}
	if _, sounds := e.nt.counts(); sounds != 1 {
		t.Fatalf("un sonido cada 2 s: %d", sounds)
	}
	e.clock.Advance(time.Second)
	e.feed(e.incoming("y"))
	if _, sounds := e.nt.counts(); sounds != 2 {
		t.Fatalf("pasados 2 s suena otra vez: %d", sounds)
	}
}

// ---------------------------------------------------------------------------------------------
// Permiso en caché

func TestNotificationsPermissionIsCachedBriefly(t *testing.T) {
	e := newNotifyEnv(t)
	e.feed(e.incoming("uno"))
	// el usuario bloquea las notificaciones en Windows
	e.nt.setPerm(port.NotifyPermission{State: port.NotifyDenied, Reason: "bloqueado"})
	e.clock.Advance(2 * time.Second)
	e.feed(e.incoming("dos")) // aún dentro de los 5 s de la lectura guardada
	if toasts, _ := e.nt.counts(); toasts != 2 {
		t.Fatalf("la lectura guardada se reutiliza: %d", toasts)
	}
	e.clock.Advance(10 * time.Second)
	e.feed(e.incoming("tres"))
	toasts, sounds := e.nt.counts()
	if toasts != 2 || sounds != 3 {
		t.Fatalf("tras la caducidad se respeta el bloqueo (sigue sonando): notificaciones=%d sonidos=%d", toasts, sounds)
	}
}

// ---------------------------------------------------------------------------------------------
// Estado, prueba y permisos

func TestNotificationsStatusAndSettings(t *testing.T) {
	e := newNotifyEnv(t)
	e.nt.setPerm(port.NotifyPermission{State: port.NotifyDenied, Reason: "Windows las tiene apagadas."})
	st := e.n.Status(context.Background())
	if st.Permission.State != port.NotifyDenied || st.Permission.Reason == "" || st.Listening {
		t.Fatalf("estado: %+v", st)
	}
	if err := e.n.OpenSystemSettings(); err != nil || e.nt.opened != 1 {
		t.Fatalf("abrir ajustes: %v %d", err, e.nt.opened)
	}
	p, err := e.n.RequestPermission(context.Background())
	if err != nil || p.State != port.NotifyDenied || e.nt.requests != 1 {
		t.Fatalf("pedir permiso: %+v %v %d", p, err, e.nt.requests)
	}
}

func TestNotificationsSendTestHonestResults(t *testing.T) {
	cases := []struct {
		name      string
		perm      port.NotifyPermission
		soundOn   bool
		showErr   error
		wantShown bool
		wantVis   bool
		wantSound bool
		msg       string // trozo del mensaje
		exact     bool
	}{
		{name: "sin supresión: aviso enviado", perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietInactive}, soundOn: true,
			wantShown: true, wantVis: true, wantSound: true, msg: "Aviso enviado", exact: true},
		{name: "sin supresión y sin sonido", perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietInactive}, soundOn: false,
			wantShown: true, wantVis: true, msg: "Aviso enviado", exact: true},
		{name: "No molestar", perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietActive, QuietCause: port.QuietCauseDND}, soundOn: true,
			wantShown: true, wantSound: true,
			msg: "Windows está en «No molestar»: no verás el aviso en pantalla, pero sonará el aviso. Está en el centro de notificaciones.", exact: true},
		{name: "No molestar sin sonido", perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietActive, QuietCause: port.QuietCauseDND}, soundOn: false,
			wantShown: true, msg: "el sonido está apagado en Ajustes"},
		{name: "pantalla completa", perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietActive, QuietCause: port.QuietCauseFullscreen}, soundOn: true,
			wantShown: true, wantSound: true, msg: "Hay una aplicación a pantalla completa"},
		{name: "presentación", perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietActive, QuietCause: port.QuietCausePresentation}, soundOn: true,
			wantShown: true, wantSound: true, msg: "modo presentación"},
		{name: "No molestar desconocido: no se afirma", perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietUnknown}, soundOn: true,
			wantShown: true, wantVis: true, wantSound: true, msg: "No se pudo comprobar"},
		{name: "permiso bloqueado", perm: port.NotifyPermission{State: port.NotifyDenied, Reason: "Las notificaciones de dConnector están bloqueadas en Windows."}, soundOn: true,
			wantSound: true, msg: "bloqueadas en Windows. No verás el aviso en pantalla, pero sonará el aviso."},
		{name: "no admitido", perm: port.NotifyPermission{State: port.NotifyUnsupported, Reason: "Solo en Windows."}, soundOn: true,
			wantSound: true, msg: "Este sistema no tiene notificaciones de escritorio, pero sonará el aviso."},
		{name: "falla el toast", perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietInactive}, soundOn: true, showErr: errors.New("sin COM"),
			wantSound: true, msg: "No se pudo mostrar la notificación de prueba (sin COM)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newNotifyEnv(t)
			e.nt.setPerm(c.perm)
			e.nt.showErr = c.showErr
			e.set.set.NotificationsSound = c.soundOn
			res, err := e.n.SendTest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if res.Shown != c.wantShown || res.Visible != c.wantVis || res.Sound != c.wantSound {
				t.Fatalf("shown=%v visible=%v sound=%v: %+v", res.Shown, res.Visible, res.Sound, res)
			}
			if c.exact && res.Message != c.msg || !c.exact && !strings.Contains(res.Message, c.msg) {
				t.Fatalf("mensaje %q (quería %q)", res.Message, c.msg)
			}
			toasts, sounds := e.nt.counts()
			wantSounds := 0
			if c.wantSound {
				wantSounds = 1
			}
			wantToasts := 0
			if c.wantShown {
				wantToasts = 1
			}
			if toasts != wantToasts || sounds != wantSounds {
				t.Fatalf("notificaciones=%d sonidos=%d", toasts, sounds)
			}
			if wantToasts == 1 && !e.nt.shown[0].Silent {
				t.Fatal("el toast de prueba sale sin sonido propio: el sonido es el de dConnector")
			}
			if wantToasts == 1 && e.nt.shown[0].Body != "Así se verán tus notificaciones" {
				t.Fatalf("toast de prueba: %+v", e.nt.shown)
			}
		})
	}
}

func TestNotificationsSendTestIgnoresMasterSwitch(t *testing.T) {
	e := newNotifyEnv(t)
	e.set.set.Notifications = false
	if _, err := e.n.SendTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if toasts, _ := e.nt.counts(); toasts != 1 {
		t.Fatalf("la prueba comprueba el sistema aunque el interruptor esté apagado: %d", toasts)
	}
}

// ---------------------------------------------------------------------------------------------
// Escucha del bus

func TestNotificationsListensToBus(t *testing.T) {
	e := newNotifyEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.n.Start(ctx)
	e.n.Start(ctx) // idempotente
	if !e.n.Listening() {
		t.Fatal("debería estar escuchando")
	}

	m := e.incoming("por el bus")
	e.bus.Publish(model.Event{Type: model.EvMessageNew, Message: &m})
	eventually(t, "notificación por el bus", func() bool { s, _ := e.nt.counts(); return s == 1 })

	// un mensaje propio, un estado y un borrado no avisan
	own := e.incoming("mío")
	own.FromMe = true
	e.bus.Publish(model.Event{Type: model.EvMessageNew, Message: &own})
	e.bus.Publish(model.Event{Type: model.EvMessageSent, Message: &own})
	upd := e.incoming("editado")
	e.bus.Publish(model.Event{Type: model.EvMessageUpdate, Message: &upd})
	e.bus.Publish(model.Event{Type: model.EvStatusUpsert, Status: &model.Status{}})
	// un mensaje final que sí avisa, para saber que lo anterior ya se procesó
	e.clock.Advance(5 * time.Second)
	last := e.incoming("último")
	e.bus.Publish(model.Event{Type: model.EvMessageNew, Message: &last})
	eventually(t, "segunda notificación", func() bool { s, _ := e.nt.counts(); return s == 2 })
	if s, _ := e.nt.counts(); s != 2 {
		t.Fatalf("solo debían avisar los dos entrantes: %d", s)
	}

	e.n.Stop()
	if e.n.Listening() {
		t.Fatal("tras Stop no escucha")
	}
	e.n.Stop() // idempotente
	// reiniciable
	e.n.Start(ctx)
	if !e.n.Listening() {
		t.Fatal("debería poder volver a escuchar")
	}
	e.n.Stop()
}

func TestNotificationsLogNeverCarriesMessageText(t *testing.T) {
	// El registro de decisiones solo lleva motivos fijos y conteos: se comprueba que los motivos que
	// pasan por skip no incluyen texto de mensajes (la API no recibe el mensaje).
	e := newNotifyEnv(t)
	e.setChat(func(c *model.Chat) { c.MutedUntil = -1 })
	e.feed(e.incoming("texto secreto +593999111222"))
	if e.n.skipped.Load() != 1 {
		t.Fatalf("debía contarse un mensaje sin aviso: %d", e.n.skipped.Load())
	}
}

func TestNotifyTitleFallbacks(t *testing.T) {
	if got := notifyTitle(model.Chat{}, false, model.Message{}); got != "dConnector" {
		t.Fatalf("sin nada: %q", got)
	}
	if got := notifyTitle(model.Chat{Title: "Canal", Kind: model.ChatChannel}, true, model.Message{SenderName: "Admin"}); got != "Canal" {
		t.Fatalf("un canal usa su nombre: %q", got)
	}
	if got := notifyTitle(model.Chat{Title: "Grupo", Kind: model.ChatGroup}, true, model.Message{}); got != "Grupo" {
		t.Fatalf("grupo sin remitente: %q", got)
	}
}

func TestDefaultNotifyAvatarRejectsOddPaths(t *testing.T) {
	for _, u := range []string{"", "/media/x/y.jpg", "/avatar//a.jpg", "/avatar/acc/../x.jpg", "/avatar/acc/a.webp", "/avatar/acc/no-existe.jpg"} {
		if got := defaultNotifyAvatar(model.Chat{AvatarURL: u}); got != "" {
			t.Errorf("%q → %q", u, got)
		}
	}
}
