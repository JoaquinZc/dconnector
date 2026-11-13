// Package winnotify: adaptador de escritorio de Windows para el puerto port.Notifier. Muestra los avisos
// con el servicio de notificaciones de Wails (toasts), lee del registro de Windows si el usuario las dejó
// pasar, y suena con el sonido de mensaje del sistema. Todo lo que toca el sistema (registro, sonido,
// abrir Configuración, el toast real) entra por interfaces pequeñas, así la lógica se prueba en cualquier
// sistema. Fuera de Windows el adaptador responde «no admitido» y no hace nada.
package winnotify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Rutas del registro (todas bajo HKEY_CURRENT_USER).
const (
	// pushKey\ToastEnabled: interruptor general «Notificaciones» de Windows (0 = apagado; ausente = encendido).
	pushKey      = `Software\Microsoft\Windows\CurrentVersion\PushNotifications`
	toastEnabled = "ToastEnabled"
	// settingsKey\<AppUserModelID>\Enabled: interruptor de esta aplicación (0 = apagado; ausente = encendido).
	settingsKey = `Software\Microsoft\Windows\CurrentVersion\Notifications\Settings\`
	appEnabled  = "Enabled"
	// appRegKey\<AppUserModelID>\DisplayName: el registro de la aplicación que hace el servicio de
	// notificaciones de Wails al arrancar (nombre, icono y activador). Sin él, Windows no muestra toasts.
	appRegKey      = `Software\Classes\AppUserModelId\`
	appDisplayName = "DisplayName"
)

// SettingsURI: la pantalla «Notificaciones» de Configuración de Windows.
const SettingsURI = "ms-settings:notifications"

// Registry lee valores del registro de Windows (HKEY_CURRENT_USER).
type Registry interface {
	// DWord lee un valor numérico; found es false si la clave o el valor no existen.
	DWord(path, name string) (value uint32, found bool, err error)
	// String lee un valor de texto; found es false si la clave o el valor no existen.
	String(path, name string) (value string, found bool, err error)
}

// Toast: lo que se pide mostrar al servicio de notificaciones.
type Toast struct {
	ID         string
	Title      string
	Body       string
	ChatKey    string
	AvatarPath string
	// Silent: sin el sonido propio de la notificación.
	Silent bool
}

// Toaster muestra toasts y avisa al hacer clic en uno (lo implementa el servicio de Wails).
type Toaster interface {
	Push(t Toast) error
	// OnActivate registra qué hacer al pulsar un toast; recibe la clave del chat ("" si no la lleva).
	OnActivate(handler func(chatKey string))
}

// Deps: todo lo que el adaptador necesita del sistema.
type Deps struct {
	// AppID: el AppUserModelID con que Wails registra la aplicación (el Name de la aplicación: «dConnector»).
	AppID    string
	Registry Registry
	Toaster  Toaster
	// Beep reproduce el sonido de mensaje de dConnector sin bloquear.
	Beep func() error
	// Quiet lee si Windows está suprimiendo los avisos («No molestar», pantalla completa, presentación).
	// nil = no se sabe (el estado queda «desconocido»).
	Quiet QuietReader
	// Open abre una dirección o archivo con el programa del sistema (aquí, ms-settings:).
	Open func(uri string) error
	// Supported: false fuera de Windows.
	Supported bool
}

// Notifier implementa port.Notifier.
type Notifier struct {
	d Deps

	mu       sync.Mutex
	activate func(chatKey model.ChatKey)
	wired    bool
}

var _ port.Notifier = (*Notifier)(nil)

// NewWithDeps crea el adaptador con sus dependencias (lo usan New y las pruebas).
func NewWithDeps(d Deps) *Notifier { return &Notifier{d: d} }

// OnActivate registra qué hacer cuando la persona pulsa una notificación: se llama con la clave del
// chat ("" si la notificación no lleva chat, p. ej. la de prueba).
func (n *Notifier) OnActivate(handler func(chatKey model.ChatKey)) {
	n.mu.Lock()
	n.activate = handler
	wire := !n.wired && n.d.Toaster != nil
	n.wired = n.wired || wire
	n.mu.Unlock()
	if wire {
		n.d.Toaster.OnActivate(func(key string) {
			n.mu.Lock()
			h := n.activate
			n.mu.Unlock()
			if h != nil {
				h(model.ChatKey(key))
			}
		})
	}
}

// Permission lee del registro si Windows deja mostrar las notificaciones de la aplicación y, aparte, si
// el sistema las está suprimiendo ahora mismo («No molestar», pantalla completa, presentación: ver quiet.go).
// Con «No molestar» activo el permiso sigue «concedido» (los avisos llegan al centro de notificaciones),
// por eso el estado de supresión va en sus propios campos.
func (n *Notifier) Permission(ctx context.Context) port.NotifyPermission {
	p := n.permission(ctx)
	if !n.d.Supported {
		return p
	}
	p.Quiet, p.QuietCause, p.QuietReason = quietStatus(n.d.Quiet)
	return p
}

// permission lee solo los interruptores de Windows (registro).
func (n *Notifier) permission(ctx context.Context) port.NotifyPermission {
	if !n.d.Supported {
		return port.NotifyPermission{State: port.NotifyUnsupported,
			Reason: "Las notificaciones de escritorio solo están disponibles en Windows."}
	}
	if n.d.Registry == nil {
		return unknown("No se pudo consultar la configuración de notificaciones de Windows.")
	}
	if v, found, err := n.d.Registry.DWord(pushKey, toastEnabled); err != nil {
		slog.Debug("notificaciones: no se pudo leer el interruptor general", "error", err)
		return unknown("No se pudo leer si las notificaciones de Windows están activadas.")
	} else if found && v == 0 {
		return denied("Las notificaciones están desactivadas en Windows. Actívalas en Configuración › Sistema › Notificaciones.")
	}
	if v, found, err := n.d.Registry.DWord(settingsKey+n.d.AppID, appEnabled); err != nil {
		slog.Debug("notificaciones: no se pudo leer el interruptor de la aplicación", "error", err)
		return unknown("No se pudo leer si Windows permite las notificaciones de " + n.d.AppID + ".")
	} else if found && v == 0 {
		return denied("Las notificaciones de " + n.d.AppID + " están bloqueadas en Windows. Actívalas en Configuración › Sistema › Notificaciones.")
	}
	if name, found, err := n.d.Registry.String(appRegKey+n.d.AppID, appDisplayName); err != nil {
		slog.Debug("notificaciones: no se pudo leer el registro de la aplicación", "error", err)
		return unknown("No se pudo comprobar que " + n.d.AppID + " esté registrada en las notificaciones de Windows.")
	} else if !found || strings.TrimSpace(name) == "" {
		return unknown(n.d.AppID + " todavía no está registrada en las notificaciones de Windows; se registra al arrancar.")
	}
	return port.NotifyPermission{State: port.NotifyGranted,
		Reason: "Windows permite las notificaciones de " + n.d.AppID + "."}
}

func unknown(reason string) port.NotifyPermission {
	return port.NotifyPermission{State: port.NotifyUnknown, Reason: reason}
}

func denied(reason string) port.NotifyPermission {
	return port.NotifyPermission{State: port.NotifyDenied, Reason: reason}
}

// RequestPermission: Windows no tiene un diálogo de permiso. Si está bloqueado, abre la pantalla de
// notificaciones de Configuración para que la persona las encienda; si no, confirma el estado.
func (n *Notifier) RequestPermission(ctx context.Context) (port.NotifyPermission, error) {
	p := n.Permission(ctx)
	if p.State != port.NotifyDenied {
		return p, nil
	}
	if err := n.OpenSystemSettings(); err != nil {
		return p, err
	}
	return p, nil
}

// Show muestra una notificación (toast).
func (n *Notifier) Show(ctx context.Context, note port.Notification) error {
	if !n.d.Supported || n.d.Toaster == nil {
		return errors.New("las notificaciones de escritorio no están disponibles en este sistema")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(note.Title) == "" {
		return errors.New("la notificación no tiene título")
	}
	id := note.ID
	if id == "" {
		id = "dconnector"
	}
	if err := n.d.Toaster.Push(Toast{
		ID: id, Title: note.Title, Body: note.Body, ChatKey: string(note.ChatKey),
		AvatarPath: note.AvatarPath, Silent: note.Silent,
	}); err != nil {
		return fmt.Errorf("no se pudo mostrar la notificación: %w", err)
	}
	return nil
}

// PlaySound reproduce el sonido de mensaje de dConnector (generado en memoria: no depende del esquema de
// sonidos de Windows ni de que el aviso sea visible).
func (n *Notifier) PlaySound(ctx context.Context) error {
	if !n.d.Supported || n.d.Beep == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := n.d.Beep(); err != nil {
		return fmt.Errorf("no se pudo reproducir el sonido: %w", err)
	}
	return nil
}

// OpenSystemSettings abre Configuración › Sistema › Notificaciones.
func (n *Notifier) OpenSystemSettings() error {
	if !n.d.Supported || n.d.Open == nil {
		return errors.New("la configuración de notificaciones solo se puede abrir en Windows")
	}
	if err := n.d.Open(SettingsURI); err != nil {
		return fmt.Errorf("no se pudo abrir la configuración de notificaciones de Windows: %w", err)
	}
	return nil
}

// ChatKeyFromActivation saca la clave del chat de lo que devuelve el servicio de Wails al pulsar un
// toast: primero los datos adjuntos (chatKey) y, si faltan, el id de la notificación («<chatKey>#<id>»).
func ChatKeyFromActivation(data map[string]any, id string) string {
	if v, ok := data["chatKey"].(string); ok && v != "" {
		return v
	}
	if i := strings.LastIndex(id, "#"); i > 0 {
		return id[:i]
	}
	return ""
}
