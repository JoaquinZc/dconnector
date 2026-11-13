package connection

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdatePrefsDefaultsAndPersist(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := NewUpdatePrefs(st)
	if !p.AutoCheck() || !p.LastCheck().IsZero() {
		t.Fatal("por defecto busca al abrir y nunca comprobó")
	}
	when := time.UnixMilli(1_700_000_000_123)
	if err := p.SetAutoCheck(false); err != nil {
		t.Fatal(err)
	}
	if err := p.SetLastCheck(when); err != nil {
		t.Fatal(err)
	}
	st2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p2 := NewUpdatePrefs(st2)
	if p2.AutoCheck() || !p2.LastCheck().Equal(when) {
		t.Fatalf("no se guardó: %v %v", p2.AutoCheck(), p2.LastCheck())
	}
}

func TestOldDesktopJSONStillLoads(t *testing.T) {
	dir := t.TempDir()
	old := `{"mode":"remote","keepRunningInBackground":true,"remote":{"url":"https://x:1","user":"u"},"notificationsAsked":true}`
	if err := os.WriteFile(filepath.Join(dir, "desktop.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := st.Get()
	if c.Mode != ModeRemote || !c.KeepRunning || c.Remote.User != "u" || !NewUpdatePrefs(st).AutoCheck() {
		t.Fatalf("%+v", c)
	}
	// Guardar un cambio de actualizaciones no pierde los campos de siempre.
	_ = NewUpdatePrefs(st).SetAutoCheck(false)
	c = st.Get()
	if c.Mode != ModeRemote || c.Remote.URL != "https://x:1" || !c.NotificationsAsked {
		t.Fatalf("%+v", c)
	}
}
