package wails

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"dconnector/internal/adapter/desktop/bridge"
	"dconnector/internal/adapter/desktop/taskbarbadge"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

//go:embed icon.ico
var trayIcon []byte

// Desktop reúne el comportamiento de escritorio de la ventana (11 §4): bandeja del sistema, cerrar a la
// bandeja y salida ordenada. Se alimenta del flujo de eventos del servidor (SetUnread, SetAccounts,
// SetConnection); los avisos de mensajes los ejecuta el puente (bridge) y Desktop solo usa el notificador
// para el aviso «sigue abierto en la bandeja» y para abrir un chat cuando se pulsa una notificación.
type Desktop struct {
	app      *application.App
	win      *application.WebviewWindow
	notifier port.Notifier
	onQuit   func()

	tray *application.SystemTray
	// badge: el contador de sin leer sobre el icono de la barra de tareas (círculo rojo con el número).
	badge *taskbarbadge.Badge

	closeToTray atomic.Bool
	quitting    atomic.Bool
	hintShown   atomic.Bool
	unread      atomic.Int64
	offline     atomic.Bool

	mu       sync.Mutex
	accounts []bridge.AccountLine
}

// NewDesktop crea el módulo. notifier puede ser nil (sin avisos de escritorio). onQuit se llama
// una vez antes de cerrar la aplicación al elegir «Salir».
func NewDesktop(app *application.App, win *application.WebviewWindow, notifier port.Notifier, onQuit func()) *Desktop {
	dk := &Desktop{app: app, win: win, notifier: notifier, onQuit: onQuit}
	dk.closeToTray.Store(true)
	// El HWND es el de la ventana principal (no el de «la ventana con foco»: en segundo plano o en la bandeja no
	// hay ventana actual y el contador no aparecería) y toda llamada COM va por el hilo de interfaz de Wails.
	dk.badge = taskbarbadge.New(taskbarbadge.NewOverlay(
		func() uintptr {
			if win == nil {
				return 0
			}
			return uintptr(win.NativeWindow())
		},
		application.InvokeSyncWithError,
	))
	return dk
}

// SetCloseToTray aplica el ajuste «cerrar a la bandeja».
func (d *Desktop) SetCloseToTray(v bool) { d.closeToTray.Store(v) }

// Setup crea el icono de la bandeja y engancha el cierre de la ventana.
func (d *Desktop) Setup() {
	d.tray = d.app.SystemTray.New()
	d.tray.SetIcon(trayIcon)
	d.tray.SetTooltip("dConnector")
	d.tray.OnClick(d.ShowWindow)
	d.tray.OnDoubleClick(d.ShowWindow)
	d.rebuildMenu()

	// Explorer pierde la insignia cuando recrea el botón de la barra de tareas y Wails no deja enganchar el
	// mensaje «TaskbarButtonCreated»: se vuelve a poner al mostrar o enfocar la ventana (y cada 30 s mientras
	// haya sin leer, ver taskbarbadge).
	for _, ev := range []events.WindowEventType{events.Common.WindowShow, events.Common.WindowRestore, events.Common.WindowFocus} {
		d.win.OnWindowEvent(ev, func(*application.WindowEvent) { d.badge.Reapply() })
	}

	d.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if d.quitting.Load() || !d.closeToTray.Load() {
			return
		}
		e.Cancel()
		d.win.Hide()
		if d.hintShown.CompareAndSwap(false, true) {
			go d.sendHint("dConnector sigue abierto en la bandeja")
		}
	})
}

// OnWindowFocus registra qué hacer cuando la ventana recibe el foco (p. ej. releer los ajustes del servidor).
func (d *Desktop) OnWindowFocus(f func()) {
	d.win.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) { f() })
}

// OpenChat lo llama el puente al pulsar una notificación: muestra la ventana y, si la notificación llevaba
// un chat, pide a la interfaz que lo abra (evento ui:open-chat).
func (d *Desktop) OpenChat(key model.ChatKey) {
	d.ShowWindow()
	if key != "" {
		d.app.Event.Emit(EvtOpenChat, string(key))
	}
}

// ShowWindow muestra y enfoca la ventana principal.
func (d *Desktop) ShowWindow() {
	d.win.Show()
	d.win.UnMinimise()
	d.win.Focus()
	d.badge.Reapply()
}

// Quit apaga la aplicación de forma ordenada (la ventana no se oculta al cerrarse).
func (d *Desktop) Quit() {
	if !d.quitting.CompareAndSwap(false, true) {
		return
	}
	if d.onQuit != nil {
		d.onQuit()
	}
	d.badge.Close()
	d.app.Quit()
}

// SetUnread actualiza el tooltip de la bandeja y el contador de la barra de tareas (insignia 1…9, 9+). total
// es el de unread:total del servidor.
func (d *Desktop) SetUnread(total int) {
	d.unread.Store(int64(total))
	d.badge.Set(total)
	d.refreshTooltip()
}

// SetConnection indica si el flujo de eventos del servidor está conectado (tooltip «sin conexión»).
func (d *Desktop) SetConnection(connected bool) {
	d.offline.Store(!connected)
	d.refreshTooltip()
}

func (d *Desktop) refreshTooltip() {
	if d.tray == nil {
		return
	}
	switch n := d.unread.Load(); {
	case d.offline.Load():
		d.tray.SetTooltip("dConnector · sin conexión")
	case n > 0:
		d.tray.SetTooltip(fmt.Sprintf("dConnector · %d sin leer", n))
	default:
		d.tray.SetTooltip("dConnector")
	}
}

// SetAccounts guarda las cuentas con su estado y reconstruye el menú de la bandeja.
func (d *Desktop) SetAccounts(lines []bridge.AccountLine) {
	d.mu.Lock()
	d.accounts = append([]bridge.AccountLine(nil), lines...)
	d.mu.Unlock()
	d.rebuildMenu()
}

func (d *Desktop) rebuildMenu() {
	if d.tray == nil {
		return
	}
	d.mu.Lock()
	lines := append([]bridge.AccountLine(nil), d.accounts...)
	d.mu.Unlock()

	menu := application.NewMenu()
	menu.Add("Abrir dConnector").OnClick(func(*application.Context) { d.ShowWindow() })
	menu.AddSeparator()
	for _, l := range lines {
		menu.Add(accountMenuLabel(l)).SetEnabled(false)
	}
	if len(lines) > 0 {
		menu.AddSeparator()
	}
	menu.Add("Salir").OnClick(func(*application.Context) { d.Quit() })
	d.tray.SetMenu(menu)
}

func channelName(c string) string {
	switch model.Channel(c) {
	case model.ChannelWhatsApp:
		return "WhatsApp"
	case model.ChannelTelegram:
		return "Telegram"
	case model.ChannelMessenger:
		return "Messenger"
	case model.ChannelInstagram:
		return "Instagram"
	}
	return c
}

func stateName(s string) string {
	switch model.ConnState(s) {
	case model.StateConnected:
		return "Conectada"
	case model.StateConnecting:
		return "Conectando…"
	case model.StatePairing:
		return "Esperando vinculación"
	case model.StateLoggedOut:
		return "Sesión cerrada"
	case model.StateError:
		return "Con error"
	}
	return "Desconectada"
}

// accountMenuLabel: «● Ventas (WhatsApp) — Conectada».
func accountMenuLabel(l bridge.AccountLine) string {
	dot := "○"
	if l.Connected {
		dot = "●"
	}
	return fmt.Sprintf("%s %s (%s) — %s", dot, l.Label, channelName(l.Channel), stateName(l.State))
}

// sendHint muestra, sin sonido, un aviso corto que no es de un mensaje (el de «sigue abierto en la
// bandeja»). Si el sistema tiene las notificaciones bloqueadas, no se muestra.
func (d *Desktop) sendHint(body string) {
	if d.notifier == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p := d.notifier.Permission(ctx); p.State == port.NotifyDenied || p.State == port.NotifyUnsupported {
		return
	}
	if err := d.notifier.Show(ctx, port.Notification{ID: "dconnector-hint", Title: "dConnector", Body: body, Silent: true}); err != nil {
		slog.Debug("no se pudo mostrar el aviso de la bandeja", "error", err)
	}
}
