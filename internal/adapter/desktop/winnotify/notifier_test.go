package winnotify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// ---------------------------------------------------------------------------------------------
// Dobles

type fakeReg struct {
	dwords  map[string]uint32
	strs    map[string]string
	dwErr   map[string]error
	strErr  map[string]error
	lookups []string
}

func newFakeReg() *fakeReg {
	return &fakeReg{dwords: map[string]uint32{}, strs: map[string]string{}, dwErr: map[string]error{}, strErr: map[string]error{}}
}

func (r *fakeReg) DWord(path, name string) (uint32, bool, error) {
	k := path + "|" + name
	r.lookups = append(r.lookups, k)
	if err := r.dwErr[k]; err != nil {
		return 0, false, err
	}
	v, ok := r.dwords[k]
	return v, ok, nil
}

func (r *fakeReg) String(path, name string) (string, bool, error) {
	k := path + "|" + name
	r.lookups = append(r.lookups, k)
	if err := r.strErr[k]; err != nil {
		return "", false, err
	}
	v, ok := r.strs[k]
	return v, ok, nil
}

type fakeToaster struct {
	pushed   []Toast
	pushErr  error
	handler  func(string)
	handlers int
}

func (f *fakeToaster) Push(t Toast) error {
	if f.pushErr != nil {
		return f.pushErr
	}
	f.pushed = append(f.pushed, t)
	return nil
}

func (f *fakeToaster) OnActivate(h func(string)) { f.handler = h; f.handlers++ }

type env struct {
	n     *Notifier
	reg   *fakeReg
	toast *fakeToaster
	beeps int
	opens []string
	beepE error
	openE error
}

const appID = "dConnector"

func newEnv(supported bool) *env {
	e := &env{reg: newFakeReg(), toast: &fakeToaster{}}
	// La aplicación está registrada por el servicio de Wails.
	e.reg.strs[appRegKey+appID+"|"+appDisplayName] = appID
	e.n = NewWithDeps(Deps{
		AppID: appID, Registry: e.reg, Toaster: e.toast, Supported: supported,
		Beep: func() error { e.beeps++; return e.beepE },
		Open: func(u string) error { e.opens = append(e.opens, u); return e.openE },
	})
	return e
}

// ---------------------------------------------------------------------------------------------
// Permiso

func TestPermissionGrantedByDefaultWhenKeysAreAbsent(t *testing.T) {
	e := newEnv(true)
	p := e.n.Permission(context.Background())
	if p.State != port.NotifyGranted || !strings.Contains(p.Reason, "permite") {
		t.Fatalf("permiso: %+v", p)
	}
}

func TestPermissionReadsTheRightRegistryKeys(t *testing.T) {
	e := newEnv(true)
	e.n.Permission(context.Background())
	want := []string{
		`Software\Microsoft\Windows\CurrentVersion\PushNotifications|ToastEnabled`,
		`Software\Microsoft\Windows\CurrentVersion\Notifications\Settings\dConnector|Enabled`,
		`Software\Classes\AppUserModelId\dConnector|DisplayName`,
	}
	if strings.Join(e.reg.lookups, "\n") != strings.Join(want, "\n") {
		t.Fatalf("claves leídas:\n%s", strings.Join(e.reg.lookups, "\n"))
	}
}

func TestPermissionMatrix(t *testing.T) {
	const (
		global = `Software\Microsoft\Windows\CurrentVersion\PushNotifications|ToastEnabled`
		app    = `Software\Microsoft\Windows\CurrentVersion\Notifications\Settings\dConnector|Enabled`
		reg    = `Software\Classes\AppUserModelId\dConnector|DisplayName`
	)
	boom := errors.New("acceso denegado")
	cases := []struct {
		name   string
		setup  func(r *fakeReg)
		state  port.NotifyState
		reason string
	}{
		{"general apagado", func(r *fakeReg) { r.dwords[global] = 0 }, port.NotifyDenied, "desactivadas en Windows"},
		{"general encendido", func(r *fakeReg) { r.dwords[global] = 1 }, port.NotifyGranted, ""},
		{"app apagada", func(r *fakeReg) { r.dwords[app] = 0 }, port.NotifyDenied, "bloqueadas en Windows"},
		{"app encendida", func(r *fakeReg) { r.dwords[app] = 1 }, port.NotifyGranted, ""},
		{"general apagado gana a app encendida", func(r *fakeReg) { r.dwords[global] = 0; r.dwords[app] = 1 }, port.NotifyDenied, "desactivadas en Windows"},
		{"app apagada con general encendido", func(r *fakeReg) { r.dwords[global] = 1; r.dwords[app] = 0 }, port.NotifyDenied, "bloqueadas"},
		{"error al leer el general", func(r *fakeReg) { r.dwErr[global] = boom }, port.NotifyUnknown, "No se pudo leer"},
		{"error al leer el de la app", func(r *fakeReg) { r.dwErr[app] = boom }, port.NotifyUnknown, "No se pudo leer"},
		{"error al leer el registro de la app", func(r *fakeReg) { r.strErr[reg] = boom }, port.NotifyUnknown, "No se pudo comprobar"},
		{"app sin registrar", func(r *fakeReg) { delete(r.strs, reg) }, port.NotifyUnknown, "todavía no está registrada"},
		{"nombre vacío = sin registrar", func(r *fakeReg) { r.strs[reg] = "  " }, port.NotifyUnknown, "todavía no está registrada"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(true)
			c.setup(e.reg)
			p := e.n.Permission(context.Background())
			if p.State != c.state || !strings.Contains(p.Reason, c.reason) || p.Reason == "" {
				t.Fatalf("permiso: %+v (quería %s y %q)", p, c.state, c.reason)
			}
		})
	}
}

func TestPermissionUnsupportedOutsideWindows(t *testing.T) {
	e := newEnv(false)
	p := e.n.Permission(context.Background())
	if p.State != port.NotifyUnsupported || !strings.Contains(p.Reason, "Windows") {
		t.Fatalf("permiso: %+v", p)
	}
	if len(e.reg.lookups) != 0 {
		t.Fatalf("no debe tocar el registro: %v", e.reg.lookups)
	}
	if err := e.n.Show(context.Background(), port.Notification{Title: "x"}); err == nil {
		t.Fatal("Show debe fallar donde no se admite")
	}
	if err := e.n.PlaySound(context.Background()); err != nil || e.beeps != 0 {
		t.Fatalf("PlaySound no hace nada: %v %d", err, e.beeps)
	}
	if err := e.n.OpenSystemSettings(); err == nil {
		t.Fatal("abrir Configuración debe fallar donde no se admite")
	}
}

func TestPermissionWithoutRegistryIsUnknown(t *testing.T) {
	n := NewWithDeps(Deps{AppID: appID, Supported: true})
	if p := n.Permission(context.Background()); p.State != port.NotifyUnknown {
		t.Fatalf("sin lector del registro: %+v", p)
	}
}

// ---------------------------------------------------------------------------------------------
// Pedir permiso

func TestRequestPermissionDeniedOpensSettings(t *testing.T) {
	e := newEnv(true)
	e.reg.dwords[`Software\Microsoft\Windows\CurrentVersion\Notifications\Settings\dConnector|Enabled`] = 0
	p, err := e.n.RequestPermission(context.Background())
	if err != nil || p.State != port.NotifyDenied {
		t.Fatalf("pedir permiso: %+v %v", p, err)
	}
	if len(e.opens) != 1 || e.opens[0] != "ms-settings:notifications" {
		t.Fatalf("debía abrir Configuración › Notificaciones: %v", e.opens)
	}
}

func TestRequestPermissionGrantedOnlyConfirms(t *testing.T) {
	e := newEnv(true)
	p, err := e.n.RequestPermission(context.Background())
	if err != nil || p.State != port.NotifyGranted || len(e.opens) != 0 {
		t.Fatalf("concedido: %+v %v %v", p, err, e.opens)
	}
}

func TestRequestPermissionOpenFailureReturnsStateAndError(t *testing.T) {
	e := newEnv(true)
	e.reg.dwords[`Software\Microsoft\Windows\CurrentVersion\PushNotifications|ToastEnabled`] = 0
	e.openE = errors.New("sin shell")
	p, err := e.n.RequestPermission(context.Background())
	if err == nil || !strings.Contains(err.Error(), "No se pudo abrir") && !strings.Contains(err.Error(), "no se pudo abrir") {
		t.Fatalf("debe devolver el error: %v", err)
	}
	if p.State != port.NotifyDenied {
		t.Fatalf("y el estado: %+v", p)
	}
}

// ---------------------------------------------------------------------------------------------
// Mostrar, sonar, abrir

func TestShowPushesToast(t *testing.T) {
	e := newEnv(true)
	err := e.n.Show(context.Background(), port.Notification{
		ID: "wa-1|123#m1", Title: "Ana", Body: "Hola", ChatKey: "wa-1|123", AvatarPath: `C:\a.jpg`, Silent: true,
	})
	if err != nil || len(e.toast.pushed) != 1 {
		t.Fatalf("Show: %v %+v", err, e.toast.pushed)
	}
	got := e.toast.pushed[0]
	if got.ID != "wa-1|123#m1" || got.Title != "Ana" || got.Body != "Hola" || got.ChatKey != "wa-1|123" || got.AvatarPath != `C:\a.jpg` || !got.Silent {
		t.Fatalf("toast: %+v", got)
	}
}

func TestShowErrors(t *testing.T) {
	e := newEnv(true)
	if err := e.n.Show(context.Background(), port.Notification{Title: "  "}); err == nil {
		t.Fatal("sin título debe fallar")
	}
	e.toast.pushErr = errors.New("WinRT")
	err := e.n.Show(context.Background(), port.Notification{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "no se pudo mostrar") || !errors.Is(err, e.toast.pushErr) {
		t.Fatalf("el error se envuelve en español: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.toast.pushErr = nil
	if err := e.n.Show(ctx, port.Notification{Title: "x"}); err == nil {
		t.Fatal("con el contexto cancelado no se muestra")
	}
	// sin id se pone uno
	if err := e.n.Show(context.Background(), port.Notification{Title: "x"}); err != nil || e.toast.pushed[0].ID == "" {
		t.Fatalf("id por defecto: %v %+v", err, e.toast.pushed)
	}
}

func TestPlaySound(t *testing.T) {
	e := newEnv(true)
	if err := e.n.PlaySound(context.Background()); err != nil || e.beeps != 1 {
		t.Fatalf("sonido: %v %d", err, e.beeps)
	}
	e.beepE = errors.New("sin audio")
	if err := e.n.PlaySound(context.Background()); err == nil || !strings.Contains(err.Error(), "no se pudo reproducir") {
		t.Fatalf("error de sonido: %v", err)
	}
}

func TestOpenSystemSettings(t *testing.T) {
	e := newEnv(true)
	if err := e.n.OpenSystemSettings(); err != nil || len(e.opens) != 1 || e.opens[0] != SettingsURI {
		t.Fatalf("abrir: %v %v", err, e.opens)
	}
	e.openE = errors.New("x")
	if err := e.n.OpenSystemSettings(); err == nil {
		t.Fatal("debe propagar el error")
	}
}

// ---------------------------------------------------------------------------------------------
// Clic en la notificación

func TestOnActivateForwardsChatKey(t *testing.T) {
	e := newEnv(true)
	var got []model.ChatKey
	e.n.OnActivate(func(k model.ChatKey) { got = append(got, k) })
	e.n.OnActivate(func(k model.ChatKey) { got = append(got, k) }) // reemplaza al anterior; el toaster se engancha una sola vez
	if e.toast.handlers != 1 {
		t.Fatalf("el toaster se engancha una vez: %d", e.toast.handlers)
	}
	e.toast.handler("wa-1|123")
	e.toast.handler("")
	if len(got) != 2 || got[0] != "wa-1|123" || got[1] != "" {
		t.Fatalf("claves: %v", got)
	}
}

func TestOnActivateWithoutHandlerIsHarmless(t *testing.T) {
	e := newEnv(true)
	e.n.OnActivate(nil)
	e.toast.handler("wa-1|1") // no debe fallar
}

func TestChatKeyFromActivation(t *testing.T) {
	if got := ChatKeyFromActivation(map[string]any{"chatKey": "wa-1|123"}, "otro"); got != "wa-1|123" {
		t.Fatalf("de los datos: %q", got)
	}
	if got := ChatKeyFromActivation(nil, "wa-1|123#m9"); got != "wa-1|123" {
		t.Fatalf("del id: %q", got)
	}
	if got := ChatKeyFromActivation(map[string]any{"chatKey": ""}, "dconnector-test"); got != "" {
		t.Fatalf("la de prueba no lleva chat: %q", got)
	}
	if got := ChatKeyFromActivation(map[string]any{"chatKey": 5}, ""); got != "" {
		t.Fatalf("tipo raro: %q", got)
	}
}
