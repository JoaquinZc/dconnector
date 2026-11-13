//go:build windows

package winnotify

import "testing"

// El lector real del registro: una clave que no existe es «no encontrada», no un error.
func TestHKCUMissingKeyIsNotFound(t *testing.T) {
	r := hkcu{}
	if v, found, err := r.DWord(`Software\dConnectorPruebaQueNoExiste\Nada`, "Enabled"); err != nil || found || v != 0 {
		t.Fatalf("DWord: %d %v %v", v, found, err)
	}
	if v, found, err := r.String(`Software\dConnectorPruebaQueNoExiste\Nada`, "DisplayName"); err != nil || found || v != "" {
		t.Fatalf("String: %q %v %v", v, found, err)
	}
}

// Lectura real (solo lectura) del interruptor general: el valor puede no existir, pero no debe fallar.
func TestHKCUReadsPushNotifications(t *testing.T) {
	if _, _, err := (hkcu{}).DWord(pushKey, toastEnabled); err != nil {
		t.Fatalf("leer %s: %v", toastEnabled, err)
	}
}

// El permiso real de este equipo siempre es uno de los cuatro estados, con motivo.
func TestRealPermissionHasReason(t *testing.T) {
	n := New("dConnector", nil)
	p := n.Permission(t.Context())
	if p.State == "" || p.Reason == "" {
		t.Fatalf("permiso real: %+v", p)
	}
	t.Logf("permiso real en este equipo: %s — %s", p.State, p.Reason)
}
