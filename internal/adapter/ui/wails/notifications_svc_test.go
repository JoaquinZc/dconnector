package wails

import (
	"context"
	"testing"

	"dconnector/internal/core/port"
)

type svcNotifier struct {
	perm   port.NotifyPermission
	shown  []port.Notification
	sounds int
	opened int
}

func (n *svcNotifier) Permission(context.Context) port.NotifyPermission { return n.perm }
func (n *svcNotifier) RequestPermission(context.Context) (port.NotifyPermission, error) {
	return n.perm, nil
}
func (n *svcNotifier) Show(_ context.Context, x port.Notification) error {
	n.shown = append(n.shown, x)
	return nil
}
func (n *svcNotifier) PlaySound(context.Context) error { n.sounds++; return nil }
func (n *svcNotifier) OpenSystemSettings() error       { n.opened++; return nil }

type svcAsked struct{ v bool }

func (a *svcAsked) NotificationsAsked() bool      { return a.v }
func (a *svcAsked) MarkNotificationsAsked() error { a.v = true; return nil }

type svcStream struct {
	listening bool
	sound     bool
	text      string
	at        int64
}

func (s svcStream) Listening() bool               { return s.listening }
func (s svcStream) SoundEnabled() bool            { return s.sound }
func (s svcStream) LastDecision() (string, int64) { return s.text, s.at }

func TestNotificationsServiceNotWired(t *testing.T) {
	s := NewNotificationsService(nil, nil, nil)
	ctx := context.Background()
	if st := s.Status(ctx); st.State != "unknown" || st.Reason == "" || st.Listening {
		t.Fatalf("sin servicio: %+v", st)
	}
	if _, err := s.RequestPermission(ctx); err == nil {
		t.Fatal("RequestPermission sin conectar debe fallar")
	}
	if _, err := s.SendTest(ctx); err == nil {
		t.Fatal("SendTest sin conectar debe fallar")
	}
	if err := s.OpenSystemSettings(); err == nil {
		t.Fatal("OpenSystemSettings sin conectar debe fallar")
	}
	if err := s.MarkAsked(ctx); err == nil {
		t.Fatal("MarkAsked sin almacén debe fallar")
	}
}

func TestNotificationsServiceStatusAndActions(t *testing.T) {
	nt := &svcNotifier{perm: port.NotifyPermission{State: port.NotifyDenied, Reason: "Bloqueadas en Windows."}}
	asked := &svcAsked{}
	stream := svcStream{listening: true, sound: true, text: "aviso y sonido", at: 42}
	s := NewNotificationsService(nt, stream, asked)
	ctx := context.Background()

	st := s.Status(ctx)
	if st.State != "denied" || st.Reason != "Bloqueadas en Windows." || !st.Listening || st.Asked || st.LastDecision != "aviso y sonido" || st.LastDecisionAt != 42 {
		t.Fatalf("estado: %+v", st)
	}
	if err := s.MarkAsked(ctx); err != nil || !s.Status(ctx).Asked {
		t.Fatalf("MarkAsked: %v", err)
	}
	if got, err := s.RequestPermission(ctx); err != nil || got.State != "denied" {
		t.Fatalf("RequestPermission: %+v %v", got, err)
	}
	// bloqueado: la prueba solo suena y devuelve el motivo
	got, err := s.SendTest(ctx)
	if err != nil || got.State != "denied" || got.Reason == "" || len(nt.shown) != 0 || nt.sounds != 1 {
		t.Fatalf("SendTest bloqueado: %+v %v shown=%d sounds=%d", got, err, len(nt.shown), nt.sounds)
	}
	nt.perm = port.NotifyPermission{State: port.NotifyGranted, Reason: "ok"}
	if got, err := s.SendTest(ctx); err != nil || got.State != "granted" || len(nt.shown) != 1 {
		t.Fatalf("SendTest concedido: %+v %v", got, err)
	}
	if err := s.OpenSystemSettings(); err != nil || nt.opened != 1 {
		t.Fatalf("OpenSystemSettings: %v %d", err, nt.opened)
	}
}

func TestNotificationsServiceQuietAndSoundSetting(t *testing.T) {
	nt := &svcNotifier{perm: port.NotifyPermission{
		State: port.NotifyGranted, Reason: "Windows permite las notificaciones de dConnector.",
		Quiet: port.QuietActive, QuietCause: port.QuietCauseDND, QuietReason: "Windows tiene activado «No molestar».",
	}}
	s := NewNotificationsService(nt, svcStream{sound: true}, &svcAsked{})
	ctx := context.Background()

	st := s.Status(ctx)
	if st.State != "granted" || st.Quiet != "active" || st.QuietCause != "dnd" || !st.Suppressed || st.QuietReason == "" {
		t.Fatalf("con «No molestar» el permiso sigue concedido pero se avisa de la supresión: %+v", st)
	}
	if st.LastDecision != "" || st.LastDecisionAt != 0 {
		t.Fatalf("sin mensajes todavía no hay decisión: %+v", st)
	}
	// La prueba es honesta: no dice «enviado» a ciegas.
	r, err := s.SendTest(ctx)
	if err != nil || !r.TestShown || r.TestVisible || !r.TestSound || nt.sounds != 1 || len(nt.shown) != 1 {
		t.Fatalf("prueba con No molestar: %+v %v shown=%d sounds=%d", r, err, len(nt.shown), nt.sounds)
	}
	if r.TestMessage != "Windows está en «No molestar»: no verás el aviso en pantalla, pero sonará el aviso. Está en el centro de notificaciones." {
		t.Fatalf("mensaje: %q", r.TestMessage)
	}
	// Sin supresión: «Aviso enviado».
	nt.perm = port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietInactive}
	if r, _ := s.SendTest(ctx); !r.TestVisible || r.TestMessage != "Aviso enviado" || r.Suppressed {
		t.Fatalf("prueba sin supresión: %+v", r)
	}
	// Con el sonido apagado en el servidor, la prueba no suena.
	s2 := NewNotificationsService(nt, svcStream{sound: false}, nil)
	before := nt.sounds
	if r, _ := s2.SendTest(ctx); r.TestSound || nt.sounds != before {
		t.Fatalf("sonido apagado: %+v", r)
	}
	// Un estado que el adaptador no informa se ve como desconocido.
	nt.perm = port.NotifyPermission{State: port.NotifyUnsupported, Reason: "Solo en Windows."}
	if st := s.Status(ctx); st.Quiet != "unknown" || !st.Suppressed {
		t.Fatalf("no admitido: %+v", st)
	}
}
