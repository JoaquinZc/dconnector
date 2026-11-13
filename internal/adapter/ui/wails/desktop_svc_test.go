package wails

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"dconnector/internal/adapter/desktop/connection"
)

type fakeConn struct {
	info    connection.Info
	err     error
	keep    bool
	trusted string
	saved   [3]string
	usedLoc bool
}

func (f *fakeConn) Info(context.Context) (connection.Info, error) { return f.info, f.err }
func (f *fakeConn) SaveRemote(_ context.Context, u, us, p string) error {
	f.saved = [3]string{u, us, p}
	return f.err
}
func (f *fakeConn) UseLocal(context.Context) error { f.usedLoc = true; return f.err }
func (f *fakeConn) KeepRunning() bool              { return f.keep }
func (f *fakeConn) SetKeepRunning(v bool) error    { f.keep = v; return f.err }
func (f *fakeConn) TrustTLS(fp string) error       { f.trusted = fp; return f.err }

func TestConnectionInfoJSONShape(t *testing.T) {
	s := NewDesktopService(&fakeConn{info: connection.Info{Mode: "local", BaseURL: "http://127.0.0.1:1", Token: "t", User: "local", TLSFingerprint: "", ServerVersion: "0.3.0"}})
	got, err := s.ConnectionInfo()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"mode", "baseUrl", "token", "user", "tlsFingerprint", "serverVersion"} {
		if _, ok := m[k]; !ok {
			t.Errorf("falta el campo JSON %q: %s", k, b)
		}
	}
	if len(m) != 6 || m["mode"] != "local" || m["baseUrl"] != "http://127.0.0.1:1" {
		t.Fatalf("forma inesperada: %s", b)
	}
}

func TestDesktopServiceDelegates(t *testing.T) {
	f := &fakeConn{}
	s := NewDesktopService(f)
	if err := s.SaveRemote("https://x", "admin", "clave"); err != nil || f.saved != [3]string{"https://x", "admin", "clave"} {
		t.Fatalf("SaveRemote: %v %v", err, f.saved)
	}
	if err := s.UseLocal(); err != nil || !f.usedLoc {
		t.Fatal("UseLocal")
	}
	if s.KeepRunningInBackground() {
		t.Fatal("por defecto apagado")
	}
	if err := s.SetKeepRunningInBackground(true); err != nil || !s.KeepRunningInBackground() {
		t.Fatal("SetKeepRunningInBackground")
	}
	if err := s.TrustTLS("AB:CD"); err != nil || f.trusted != "AB:CD" {
		t.Fatal("TrustTLS")
	}
	f.err = errors.New("No se pudo conectar.")
	if _, err := s.ConnectionInfo(); err == nil || err.Error() != "No se pudo conectar." {
		t.Fatalf("el error debe llegar tal cual: %v", err)
	}
}
