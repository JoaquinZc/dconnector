//go:build windows

package winnotify

import "testing"

// Lectura real (solo lectura) del estado de «No molestar» y de SHQueryUserNotificationState en este equipo.
// Con «No molestar» activado en Windows, el perfil WNF es distinto de 0 y Permission() informa «activo»,
// aunque SHQueryUserNotificationState diga «acepta avisos» (5).
func TestRealQuietReads(t *testing.T) {
	q := winQuiet{}
	profile, derr := q.DoNotDisturb()
	state, uerr := q.UserState()
	if derr != nil || uerr != nil {
		t.Fatalf("lecturas reales: WNF=%d (%v) SHQUNS=%d (%v)", profile, derr, state, uerr)
	}
	st, cause, reason := quietStatus(q)
	t.Logf("equipo: perfil WNF de «No molestar»=%d, SHQueryUserNotificationState=%d → %s (%s) %q", profile, state, st, cause, reason)
	if state < 1 || state > 7 {
		t.Fatalf("SHQueryUserNotificationState fuera de rango: %d", state)
	}
	if profile != 0 && st != "active" {
		t.Fatalf("con el perfil %d debe estar activo: %s", profile, st)
	}
	n := New("dConnector", nil)
	if p := n.Permission(t.Context()); p.Quiet == "" {
		t.Fatalf("Permission debe informar el estado de supresión: %+v", p)
	}
}
