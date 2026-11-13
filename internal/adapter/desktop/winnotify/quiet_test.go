package winnotify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dconnector/internal/core/port"
)

// ---------------------------------------------------------------------------------------------
// «No molestar», pantalla completa, presentación

type fakeQuiet struct {
	profile uint32
	dndErr  error
	state   int
	userErr error
}

func (f fakeQuiet) DoNotDisturb() (uint32, error) { return f.profile, f.dndErr }
func (f fakeQuiet) UserState() (int, error)       { return f.state, f.userErr }

func TestQuietStatusMatrix(t *testing.T) {
	boom := errors.New("sin acceso")
	cases := []struct {
		name   string
		q      QuietReader
		state  port.QuietState
		cause  port.QuietCause
		reason string
	}{
		{"sin lector: desconocido", nil, port.QuietUnknown, port.QuietCauseNone, ""},
		{"todo libre", fakeQuiet{state: UserAcceptsNotifications}, port.QuietInactive, port.QuietCauseNone, ""},
		{"No molestar con perfil 1", fakeQuiet{profile: 1, state: UserAcceptsNotifications}, port.QuietActive, port.QuietCauseDND, "«No molestar»"},
		{"No molestar con perfil 2", fakeQuiet{profile: 2, state: UserAcceptsNotifications}, port.QuietActive, port.QuietCauseDND, "«No molestar»"},
		{"No molestar gana a pantalla completa", fakeQuiet{profile: 1, state: UserBusy}, port.QuietActive, port.QuietCauseDND, "«No molestar»"},
		{"pantalla completa", fakeQuiet{state: UserBusy}, port.QuietActive, port.QuietCauseFullscreen, "pantalla completa"},
		{"juego a pantalla completa", fakeQuiet{state: UserD3DFullScreen}, port.QuietActive, port.QuietCauseFullscreen, "pantalla completa"},
		{"aplicación de la Tienda a pantalla completa", fakeQuiet{state: UserApp}, port.QuietActive, port.QuietCauseFullscreen, "pantalla completa"},
		{"presentación", fakeQuiet{state: UserPresentationMode}, port.QuietActive, port.QuietCausePresentation, "modo presentación"},
		{"horas de silencio", fakeQuiet{state: UserQuietTime}, port.QuietActive, port.QuietCauseDND, "«No molestar»"},
		{"equipo bloqueado no cuenta", fakeQuiet{state: UserNotPresent}, port.QuietInactive, port.QuietCauseNone, ""},
		{"falla el WNF y no hay pantalla completa: desconocido", fakeQuiet{dndErr: boom, state: UserAcceptsNotifications}, port.QuietUnknown, port.QuietCauseNone, ""},
		{"falla SHQUNS y el WNF dice libre: desconocido", fakeQuiet{userErr: boom}, port.QuietUnknown, port.QuietCauseNone, ""},
		{"falla SHQUNS pero el WNF dice activo: activo", fakeQuiet{profile: 1, userErr: boom}, port.QuietActive, port.QuietCauseDND, "«No molestar»"},
		{"falla el WNF pero hay pantalla completa: activo", fakeQuiet{dndErr: boom, state: UserBusy}, port.QuietActive, port.QuietCauseFullscreen, "pantalla completa"},
		{"fallan los dos: desconocido", fakeQuiet{dndErr: boom, userErr: boom}, port.QuietUnknown, port.QuietCauseNone, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, cause, reason := quietStatus(c.q)
			if st != c.state || cause != c.cause || !strings.Contains(reason, c.reason) || (c.reason == "" && reason != "") {
				t.Fatalf("estado=%s causa=%s motivo=%q", st, cause, reason)
			}
			if st == port.QuietActive && !strings.Contains(reason, "siguen sonando") {
				t.Fatalf("el motivo debe aclarar que el sonido sigue: %q", reason)
			}
		})
	}
}

func TestPermissionCarriesQuietStatus(t *testing.T) {
	e := newEnv(true)
	e.n.d.Quiet = fakeQuiet{profile: 1, state: UserAcceptsNotifications}
	p := e.n.Permission(context.Background())
	if p.State != port.NotifyGranted {
		t.Fatalf("con «No molestar» el permiso sigue concedido: %+v", p)
	}
	if p.Quiet != port.QuietActive || p.QuietCause != port.QuietCauseDND || !strings.Contains(p.QuietReason, "No molestar") || !p.Hidden() {
		t.Fatalf("supresión: %+v", p)
	}
	e.n.d.Quiet = fakeQuiet{state: UserAcceptsNotifications}
	if p := e.n.Permission(context.Background()); p.Quiet != port.QuietInactive || p.Hidden() {
		t.Fatalf("sin supresión: %+v", p)
	}
	e.n.d.Quiet = nil
	if p := e.n.Permission(context.Background()); p.Quiet != port.QuietUnknown {
		t.Fatalf("sin lector: %+v", p)
	}
	// fuera de Windows no se informa nada
	if p := newEnv(false).n.Permission(context.Background()); p.Quiet != "" {
		t.Fatalf("no admitido: %+v", p)
	}
}
