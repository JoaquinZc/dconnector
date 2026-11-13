package connection

import (
	"os"
	"path/filepath"
	"testing"
)

// Un desktop.json dañado arranca en modo local pero NO se pierde: puede llevar la clave remota cifrada y las
// huellas fijadas, y el siguiente Update lo sobrescribiría.
func TestCorruptDesktopJSONIsKeptAsBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desktop.json")
	if err := os.WriteFile(path, []byte(`{"mode":"remote","remote":{"password":"AAAA"`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := OpenStore(dir)
	if err != nil || st.Get().Mode != ModeLocal {
		t.Fatalf("modo local esperado: %v", err)
	}
	if err := st.Update(func(c *Config) { c.KeepRunning = true }); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path + ".dañado")
	if err != nil || len(b) == 0 {
		t.Fatalf("el archivo dañado debía conservarse: %v", err)
	}
}
