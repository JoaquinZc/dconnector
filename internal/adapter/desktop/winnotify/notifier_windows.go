//go:build windows

package winnotify

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// New crea el adaptador de Windows. appID es el AppUserModelID con que el servicio de notificaciones de
// Wails registra la aplicación: es el Name de application.Options («dConnector»). Ese servicio, al
// arrancar, crea HKCU\Software\Classes\AppUserModelId\<appID> (nombre, icono y activador del toast), por
// eso este adaptador no vuelve a registrarla: solo comprueba que exista.
func New(appID string, svc *notifications.NotificationService) *Notifier {
	d := Deps{
		AppID:     appID,
		Registry:  hkcu{},
		Beep:      playMessageSound,
		Quiet:     winQuiet{},
		Open:      shellOpen,
		Supported: true,
	}
	if svc != nil {
		d.Toaster = wailsToaster{svc: svc}
	}
	return NewWithDeps(d)
}

// hkcu lee de HKEY_CURRENT_USER.
type hkcu struct{}

func (hkcu) DWord(path, name string) (uint32, bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, err
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue(name)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return uint32(v), true, nil
}

func (hkcu) String(path, name string) (string, bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

// wailsToaster adapta el servicio de notificaciones de Wails a Toaster.
type wailsToaster struct {
	svc *notifications.NotificationService
}

func (w wailsToaster) Push(t Toast) error {
	opts := notifications.NotificationOptions{
		ID:    t.ID,
		Title: t.Title,
		Body:  t.Body,
		Data:  map[string]any{"chatKey": t.ChatKey},
	}
	if t.Silent {
		opts.Sound = &notifications.NotificationSound{Silent: true}
	} // si no, el sonido propio de Windows para notificaciones
	if t.AvatarPath != "" {
		opts.Attachments = []notifications.NotificationAttachment{{ID: "avatar", Path: t.AvatarPath, Type: "appLogoOverride"}}
	}
	return w.svc.SendNotification(opts)
}

func (w wailsToaster) OnActivate(handler func(chatKey string)) {
	w.svc.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			return
		}
		handler(ChatKeyFromActivation(r.Response.UserInfo, r.Response.ID))
	})
}

var (
	winmm    = windows.NewLazySystemDLL("winmm.dll")
	procPlay = winmm.NewProc("PlaySoundW")
	user32   = windows.NewLazySystemDLL("user32.dll")
	procBeep = user32.NewProc("MessageBeep")
	// SND_MEMORY | SND_ASYNC | SND_NODEFAULT: suena el WAV de memoria sin bloquear y sin el sonido «por defecto»
	// si falla. A diferencia de un alias del esquema de sonidos, no depende de que el usuario tenga sonidos.
	soundFlags     = uintptr(0x0004 | 0x0001 | 0x0002)
	mbIconAsterisk = uintptr(0x00000040)

	// soundMu serializa las llamadas a PlaySound (una nueva interrumpe la anterior: se evita que dos hilos se pisen).
	soundMu sync.Mutex
)

// playMessageSound reproduce el sonido propio de dConnector (WAV en memoria, asíncrono); si PlaySound falla,
// el pitido de «asterisco» del sistema.
func playMessageSound() error {
	wav := messageSound()
	soundMu.Lock()
	defer soundMu.Unlock()
	if err := procPlay.Find(); err == nil {
		r, _, _ := procPlay.Call(uintptr(unsafe.Pointer(&wav[0])), 0, soundFlags)
		runtime.KeepAlive(wav) // el búfer es global y sigue vivo; esto solo deja claro que PlaySound lo lee
		if r != 0 {
			return nil
		}
	}
	if err := procBeep.Find(); err != nil {
		return err
	}
	if r, _, err := procBeep.Call(mbIconAsterisk); r == 0 {
		return fmt.Errorf("MessageBeep: %w", err)
	}
	return nil
}

// shellOpen abre una dirección (p. ej. ms-settings:notifications) con el programa asociado.
func shellOpen(uri string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(uri)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
