// Package bridge conecta el flujo de eventos del servidor con lo que solo puede hacer el escritorio de
// Windows: el aviso del sistema y su sonido (winnotify), el contador de la barra de tareas, la bandeja y el
// arranque con Windows. El servidor ya decidió si hay que avisar (evento notify); aquí solo se ejecuta.
package bridge

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"dconnector/internal/adapter/desktop/client"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// AccountLine es una cuenta tal como la muestra el menú de la bandeja.
type AccountLine struct {
	Label     string
	Channel   string
	State     string
	Connected bool
}

// Settings: los ajustes del servidor que importan al escritorio.
type Settings struct {
	CloseToTray        bool `json:"closeToTray"`
	StartWithWindows   bool `json:"startWithWindows"`
	StartMinimized     bool `json:"startMinimized"`
	NotificationsSound bool `json:"notificationsSound"`
}

// Deps: lo que el puente necesita del escritorio. Todo es opcional (nil = no hace nada).
type Deps struct {
	// Notifier muestra el aviso de Windows y suena (winnotify).
	Notifier port.Notifier
	// OnUnread recibe el total de no leídos (insignia de la barra de tareas y tooltip de la bandeja).
	OnUnread func(total int)
	// OnAccounts recibe las cuentas con su estado (menú de la bandeja).
	OnAccounts func([]AccountLine)
	// OnConnection avisa de que el flujo de eventos se conectó o se perdió (err en español).
	OnConnection func(connected bool, err string)
	// OnSettings recibe los ajustes del servidor cuando cambian.
	OnSettings func(Settings)
	// OpenChat muestra la ventana y le pide abrir el chat (evento ui:open-chat).
	OpenChat func(chatKey string)
	// Now: reloj (nil = time.Now).
	Now func() time.Time
}

// Bridge recibe los eventos del servidor.
type Bridge struct {
	d Deps

	mu         sync.Mutex
	cl         *client.Client
	lastSet    *Settings
	decision   string
	decisionAt int64
	accTimer   *time.Timer
	listening  atomic.Bool
	soundOn    atomic.Bool
	kick       chan struct{}
}

// New crea el puente.
func New(d Deps) *Bridge {
	if d.Now == nil {
		d.Now = time.Now
	}
	b := &Bridge{d: d, kick: make(chan struct{}, 4)}
	b.soundOn.Store(true)
	return b
}

// Listening dice si el flujo de eventos del servidor está conectado.
func (b *Bridge) Listening() bool { return b.listening.Load() }

// SoundEnabled: el ajuste «sonido» del servidor (por defecto encendido hasta saber otra cosa).
func (b *Bridge) SoundEnabled() bool { return b.soundOn.Load() }

// LastDecision: qué decidió el servidor con el último mensaje y cuándo (ms Unix).
func (b *Bridge) LastDecision() (string, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.decision, b.decisionAt
}

// Serve mantiene el flujo de eventos y las sincronizaciones periódicas con cl hasta que ctx termine.
func (b *Bridge) Serve(ctx context.Context, cl *client.Client) {
	b.mu.Lock()
	b.cl = cl
	b.mu.Unlock()
	sctx, cancel := context.WithCancel(ctx)
	eventsDone := make(chan struct{})
	defer func() {
		cancel()
		// Se espera a que el flujo de eventos termine de verdad: si siguiera vivo, sus avisos tardíos
		// (listening, OnConnection, Handle) se mezclarían con los del Serve siguiente.
		<-eventsDone
		b.mu.Lock()
		if b.cl == cl {
			b.cl = nil
		}
		if b.accTimer != nil {
			b.accTimer.Stop()
		}
		b.mu.Unlock()
		b.listening.Store(false)
	}()
	go func() { defer close(eventsDone); cl.RunEvents(sctx, b.Handle, b.onStream) }()

	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-sctx.Done():
			return
		case <-b.kick:
			b.refresh(sctx, cl, true)
		case <-tick.C:
			b.refresh(sctx, cl, false)
		}
	}
}

func (b *Bridge) trigger() {
	select {
	case b.kick <- struct{}{}:
	default:
	}
}

// RefreshSettings pide releer ya los ajustes del servidor (p. ej. al enfocar la ventana).
func (b *Bridge) RefreshSettings() { b.trigger() }

func (b *Bridge) onStream(s client.StreamState) {
	b.listening.Store(s.Connected)
	if s.Connected {
		b.trigger()
		if b.d.OnConnection != nil {
			b.d.OnConnection(true, "")
		}
		return
	}
	if b.d.OnConnection != nil {
		msg := "Sin conexión con el servidor."
		if s.Err != nil {
			msg = s.Err.Error()
		}
		b.d.OnConnection(false, msg)
	}
}

// Handle procesa un evento del servidor.
func (b *Bridge) Handle(ev client.Event) {
	switch ev.Name {
	case client.EvtUnreadTotal:
		if u, err := client.Decode[client.UnreadTotal](ev); err == nil && b.d.OnUnread != nil {
			b.d.OnUnread(u.Total)
		}
	case client.EvtAccountStatus:
		b.scheduleAccounts()
	case client.EvtNotify:
		if n, err := client.Decode[client.Notify](ev); err == nil {
			b.onNotify(n)
		}
	case client.EvtNotifyDecision:
		if d, err := client.Decode[client.NotifyDecision](ev); err == nil {
			b.setDecision(d.Reason)
		}
	case "settings:changed":
		b.trigger()
	}
}

func (b *Bridge) setDecision(text string) {
	b.mu.Lock()
	b.decision, b.decisionAt = text, b.d.Now().UnixMilli()
	b.mu.Unlock()
}

// scheduleAccounts agrupa los cambios de estado de cuentas (500 ms) antes de pedir la lista.
func (b *Bridge) scheduleAccounts() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.accTimer != nil {
		b.accTimer.Stop()
	}
	b.accTimer = time.AfterFunc(500*time.Millisecond, b.trigger)
}

// onNotify muestra el aviso que el servidor ya decidió. silent+sound = solo sonido, sin ventana.
func (b *Bridge) onNotify(n client.Notify) {
	nt := b.d.Notifier
	if nt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	played := false
	if n.Sound {
		// El sonido propio de dConnector suena siempre que el servidor lo pida (Windows en «No molestar»
		// silencia los avisos, no esto).
		if err := nt.PlaySound(ctx); err != nil {
			slog.Debug("notificaciones: no se pudo sonar", "error", err)
		} else {
			played = true
		}
	}
	if n.Silent {
		return
	}
	body := n.Body
	if body == "" {
		body = n.Preview
	}
	title := n.Title
	if title == "" {
		title = "dConnector"
	}
	err := nt.Show(ctx, port.Notification{
		ID:      n.ChatKey + "#" + strconv.FormatInt(b.d.Now().UnixMilli(), 10),
		Title:   title,
		Body:    body,
		ChatKey: model.ChatKey(n.ChatKey),
		Silent:  true, // el sonido es el propio de dConnector (arriba)
	})
	if err != nil {
		slog.Warn("notificaciones: no se pudo mostrar el aviso", "error", err)
		if !played && !n.Sound {
			_ = nt.PlaySound(ctx)
		}
	}
}

// OnToastActivate: la persona pulsó un aviso. Muestra la ventana, abre el chat y le dice al servidor que
// ese chat quedó abierto y enfocado (Notifications.SetFocus) para que no vuelva a avisar de él.
func (b *Bridge) OnToastActivate(key string) {
	if b.d.OpenChat != nil {
		b.d.OpenChat(key)
	}
	if key == "" {
		return
	}
	b.mu.Lock()
	cl := b.cl
	b.mu.Unlock()
	if cl == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cl.CallInto(ctx, nil, "Notifications", "SetFocus", key, true); err != nil {
			slog.Debug("no se pudo informar el foco al servidor", "error", err)
		}
	}()
}

// refresh relee del servidor lo que no llega por eventos: no leídos (al conectar), cuentas y ajustes.
func (b *Bridge) refresh(ctx context.Context, cl *client.Client, full bool) {
	if full {
		b.refreshUnread(ctx, cl)
		b.refreshAccounts(ctx, cl)
	}
	b.refreshSettings(ctx, cl)
}

func call(ctx context.Context, cl *client.Client, out any, svc, method string, args ...any) error {
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return cl.CallInto(cctx, out, svc, method, args...)
}

func (b *Bridge) refreshUnread(ctx context.Context, cl *client.Client) {
	if b.d.OnUnread == nil {
		return
	}
	var by map[string]int
	if err := call(ctx, cl, &by, "Inbox", "UnreadTotals"); err != nil {
		slog.Debug("no se pudo leer el total de no leídos", "error", err)
		return
	}
	total := 0
	for _, n := range by {
		total += n
	}
	b.d.OnUnread(total)
}

func (b *Bridge) refreshAccounts(ctx context.Context, cl *client.Client) {
	if b.d.OnAccounts == nil {
		return
	}
	var views []struct {
		Account struct {
			Label   string `json:"label"`
			Channel string `json:"channel"`
		} `json:"account"`
		Status struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := call(ctx, cl, &views, "Accounts", "List"); err != nil {
		slog.Debug("bandeja: no se pudieron listar las cuentas", "error", err)
		return
	}
	lines := make([]AccountLine, 0, len(views))
	for _, v := range views {
		lines = append(lines, AccountLine{Label: v.Account.Label, Channel: v.Account.Channel, State: v.Status.State, Connected: v.Status.State == "connected"})
	}
	b.d.OnAccounts(lines)
}

func (b *Bridge) refreshSettings(ctx context.Context, cl *client.Client) {
	var s Settings
	if err := call(ctx, cl, &s, "Settings", "Get"); err != nil {
		slog.Debug("no se pudieron leer los ajustes del servidor", "error", err)
		return
	}
	b.soundOn.Store(s.NotificationsSound)
	b.mu.Lock()
	changed := b.lastSet == nil || *b.lastSet != s
	cp := s
	b.lastSet = &cp
	b.mu.Unlock()
	if changed && b.d.OnSettings != nil {
		b.d.OnSettings(s)
	}
}
