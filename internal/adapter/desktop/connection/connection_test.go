package connection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/adapter/desktop/localserver"
)

// fakeProtector: «cifra» con un prefijo para comprobar que la clave no queda en claro.
type fakeProtector struct{}

func (fakeProtector) Protect(b []byte) ([]byte, error) {
	out := []byte("ENC:")
	for _, c := range b {
		out = append(out, c^0x5A)
	}
	return out, nil
}

func (fakeProtector) Unprotect(b []byte) ([]byte, error) {
	if !strings.HasPrefix(string(b), "ENC:") {
		return nil, errors.New("bloque inválido")
	}
	var out []byte
	for _, c := range b[4:] {
		out = append(out, c^0x5A)
	}
	return out, nil
}

type fakeLocal struct {
	mu       sync.Mutex
	started  bool
	stopped  bool
	startErr error
	url, tok string
}

func (f *fakeLocal) Start() error                        { f.mu.Lock(); f.started = true; f.mu.Unlock(); return f.startErr }
func (f *fakeLocal) Stop()                               { f.mu.Lock(); f.stopped = true; f.mu.Unlock() }
func (f *fakeLocal) WaitReady(ctx context.Context) error { return nil }
func (f *fakeLocal) BaseURL() string                     { return f.url }
func (f *fakeLocal) Token() string                       { return f.tok }
func (f *fakeLocal) Version() string                     { return "0.3.0" }

// apiServer simula health y login/me del servidor (docs/14).
type apiServer struct {
	mu      sync.Mutex
	valid   map[string]bool
	logins  int
	version int
}

func newAPI() (*apiServer, http.Handler) {
	a := &apiServer{valid: map[string]bool{}, version: 1}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		v := a.version
		a.mu.Unlock()
		_, _ = io.WriteString(w, `{"status":"ok","version":"0.3.0","apiVersion":`+string(rune('0'+v))+`,"protocol":1,"tls":true}`)
	})
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "clave-larga-correcta") {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"Usuario o clave incorrectos."}}`)
			return
		}
		a.mu.Lock()
		a.logins++
		tok := "tok" + string(rune('0'+a.logins))
		a.valid[tok] = true
		a.mu.Unlock()
		_, _ = io.WriteString(w, `{"token":"`+tok+`","expiresAt":1,"user":{"id":"u","username":"admin","role":"admin"}}`)
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		ok := a.valid[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		a.mu.Unlock()
		if !ok {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"La sesión caducó."}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"u","username":"admin","role":"admin"}`)
	})
	return a, mux
}

func newManager(t *testing.T) (*Manager, *Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	fl := &fakeLocal{url: "http://127.0.0.1:1234", tok: "tok-local"}
	m := New(Options{Store: st, Protector: fakeProtector{}, LocalWait: time.Second,
		NewLocal: func(keep func() bool, on func(localserver.Status)) LocalServer { return fl }})
	t.Cleanup(m.Shutdown)
	return m, st, dir
}

func TestNormalizeURL(t *testing.T) {
	ok := map[string]string{
		"srv.example.com":                 "https://srv.example.com",
		" https://srv.example.com:8443/ ": "https://srv.example.com:8443",
		"http://127.0.0.1:8443":           "http://127.0.0.1:8443",
		"http://localhost:8443/x":         "http://localhost:8443",
	}
	for in, want := range ok {
		if got, err := NormalizeURL(in); err != nil || got != want {
			t.Errorf("%q → %q %v (quería %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "   ", "http://srv.example.com", "ftp://x", "https://u:p@x.com", "https://x.com?a=1", "https://"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Errorf("%q debía rechazarse", bad)
		}
	}
	if HostPort("https://Srv.Example.com") != "srv.example.com:443" || HostPort("http://127.0.0.1:80") != "127.0.0.1:80" {
		t.Fatal("HostPort")
	}
}

func TestFingerprintHelpers(t *testing.T) {
	sum := sha256.Sum256([]byte("x"))
	h := hex.EncodeToString(sum[:])
	f := FormatFingerprint(h)
	if len(f) != 95 || f != strings.ToUpper(f) || strings.Count(f, ":") != 31 {
		t.Fatalf("formato: %s", f)
	}
	for _, in := range []string{f, strings.ToLower(f), h, strings.ReplaceAll(f, ":", " ")} {
		if got, err := NormalizeFingerprint(in); err != nil || got != h {
			t.Errorf("normalizar %q: %v", in, err)
		}
	}
	for _, bad := range []string{"", "abc", strings.Repeat("zz", 32)} {
		if _, err := NormalizeFingerprint(bad); err == nil {
			t.Errorf("%q debía fallar", bad)
		}
	}
}

func TestStoreRoundTripAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	st, _ := OpenStore(dir)
	if st.Get().Mode != ModeLocal {
		t.Fatal("el modo por defecto es local")
	}
	if err := st.Update(func(c *Config) { c.Mode = ModeRemote; c.KeepRunning = true; c.TLSPins = map[string]string{"a:1": "ff"} }); err != nil {
		t.Fatal(err)
	}
	st2, _ := OpenStore(dir)
	if c := st2.Get(); c.Mode != ModeRemote || !c.KeepRunning || c.TLSPins["a:1"] != "ff" {
		t.Fatalf("no se guardó: %+v", c)
	}
	_ = os.WriteFile(filepath.Join(dir, "desktop.json"), []byte("{no es json"), 0o600)
	st3, err := OpenStore(dir)
	if err != nil || st3.Get().Mode != ModeLocal {
		t.Fatalf("un archivo dañado debe dar el modo local: %v", err)
	}
}

func TestSaveRemoteHTTPFlow(t *testing.T) {
	api, h := newAPI()
	srv := httptest.NewServer(h)
	defer srv.Close()
	m, st, dir := newManager(t)

	// Clave errónea: no se guarda nada.
	if err := m.SaveRemote(context.Background(), srv.URL, "admin", "otra"); err == nil || err.Error() != "Usuario o clave incorrectos." {
		t.Fatalf("clave mala: %v", err)
	}
	if st.Get().Mode != ModeLocal || st.Get().Remote.URL != "" {
		t.Fatal("no debía guardar con la clave mala")
	}
	if err := m.SaveRemote(context.Background(), srv.URL, "", "x"); err == nil {
		t.Fatal("usuario vacío debe fallar")
	}
	if err := m.SaveRemote(context.Background(), srv.URL, "admin", "clave-larga-correcta"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "desktop.json"))
	if strings.Contains(string(raw), "clave-larga-correcta") {
		t.Fatal("desktop.json lleva la clave en claro")
	}
	cfg := st.Get()
	if cfg.Mode != ModeRemote || cfg.Remote.User != "admin" || cfg.Remote.Password == "" {
		t.Fatalf("config: %+v", cfg)
	}
	info, err := m.Info(context.Background())
	if err != nil || info.Mode != ModeRemote || info.User != "admin" || info.Token != "tok1" || info.ServerVersion != "0.3.0" || info.BaseURL != srv.URL || info.TLSFingerprint != "" {
		t.Fatalf("info: %+v %v", info, err)
	}
	// El token caduca en el servidor: Info vuelve a entrar con la clave guardada.
	api.mu.Lock()
	api.valid = map[string]bool{}
	api.mu.Unlock()
	info, err = m.Info(context.Background())
	if err != nil || info.Token == "tok1" || info.Token == "" {
		t.Fatalf("no renovó la sesión: %+v %v", info, err)
	}
	// Tras reiniciar el escritorio (sin sesión en memoria) también entra solo.
	m2 := New(Options{Store: st, Protector: fakeProtector{}, NewLocal: func(func() bool, func(localserver.Status)) LocalServer { return &fakeLocal{} }})
	if i2, err := m2.Info(context.Background()); err != nil || i2.Token == "" {
		t.Fatalf("tras reinicio: %+v %v", i2, err)
	}
}

func TestSaveRemoteRejectsOtherAPIVersion(t *testing.T) {
	api, h := newAPI()
	api.version = 2
	srv := httptest.NewServer(h)
	defer srv.Close()
	m, _, _ := newManager(t)
	err := m.SaveRemote(context.Background(), srv.URL, "admin", "clave-larga-correcta")
	if err == nil || !strings.Contains(err.Error(), "otra versión de la API") {
		t.Fatalf("esperaba aviso de versión: %v", err)
	}
}

func TestSaveRemoteRejectsNonDConnector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "hola") }))
	defer srv.Close()
	m, _, _ := newManager(t)
	if err := m.SaveRemote(context.Background(), srv.URL, "admin", "x"); err == nil || !strings.Contains(err.Error(), "servidor de dConnector") {
		t.Fatalf("%v", err)
	}
}

func TestTLSTrustOnFirstUseAndChange(t *testing.T) {
	_, h := newAPI()
	srv := httptest.NewTLSServer(h)
	defer srv.Close()
	m, st, _ := newManager(t)
	ctx := context.Background()
	wantFP := fingerprintOf(srv.Certificate())

	err := m.SaveRemote(ctx, srv.URL, "admin", "clave-larga-correcta")
	var ce *CertError
	if !errors.As(err, &ce) || ce.Fingerprint != wantFP || ce.Changed {
		t.Fatalf("esperaba CertError con la huella: %v", err)
	}
	if !strings.Contains(err.Error(), "Huella SHA-256: "+FormatFingerprint(wantFP)) {
		t.Fatalf("el mensaje debe llevar la huella: %v", err)
	}
	if st.Get().Mode != ModeLocal {
		t.Fatal("no debía guardar con el certificado sin confiar")
	}
	if err := m.TrustTLS(strings.Repeat("ab", 32)); err == nil {
		t.Fatal("una huella distinta a la presentada debe rechazarse")
	}
	if err := m.TrustTLS("zz"); err == nil {
		t.Fatal("una huella mal formada debe rechazarse")
	}
	if err := m.TrustTLS(FormatFingerprint(wantFP)); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveRemote(ctx, srv.URL, "admin", "clave-larga-correcta"); err != nil {
		t.Fatal(err)
	}
	info, err := m.Info(ctx)
	if err != nil || info.TLSFingerprint != FormatFingerprint(wantFP) || !strings.HasPrefix(info.BaseURL, "http://127.0.0.1:") {
		t.Fatalf("info: %+v %v", info, err)
	}
	// La interfaz habla con el reenvío en loopback y llega al servidor con la huella fijada.
	resp, err := http.Get(info.BaseURL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("reenvío: %d %s", resp.StatusCode, body)
	}

	// La huella cambia (certificado renovado o alguien en medio): error claro y nada de reenvío.
	other := strings.Repeat("0", 64)
	_ = st.Update(func(c *Config) { c.TLSPins[HostPort(srv.URL)] = other })
	_, err = m.Info(ctx)
	if !errors.As(err, &ce) || !ce.Changed || !strings.Contains(err.Error(), "cambió") {
		t.Fatalf("esperaba aviso de huella cambiada: %v", err)
	}
	if err := m.TrustTLS(wantFP); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Info(ctx); err != nil {
		t.Fatalf("tras confiar de nuevo: %v", err)
	}
}

func TestLocalInfoAndSwitch(t *testing.T) {
	api, h := newAPI()
	_ = api
	srv := httptest.NewServer(h)
	defer srv.Close()
	m, st, _ := newManager(t)
	m.Start()
	info, err := m.Info(context.Background())
	if err != nil || info.Mode != ModeLocal || info.Token != "tok-local" || info.User != "local" || info.BaseURL != "http://127.0.0.1:1234" || info.ServerVersion != "0.3.0" {
		t.Fatalf("local: %+v %v", info, err)
	}
	if err := m.SaveRemote(context.Background(), srv.URL, "admin", "clave-larga-correcta"); err != nil {
		t.Fatal(err)
	}
	if m.local != nil {
		t.Fatal("al pasar a remoto se debía detener el local")
	}
	if err := m.UseLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.Get().Mode != ModeLocal || m.local == nil {
		t.Fatal("UseLocal debía volver a arrancar el local")
	}
}

func TestLocalServerMissingIsReported(t *testing.T) {
	dir := t.TempDir()
	st, _ := OpenStore(dir)
	m := New(Options{Store: st, Protector: fakeProtector{}, LocalWait: time.Second,
		NewLocal: func(func() bool, func(localserver.Status)) LocalServer {
			return &fakeLocal{startErr: localserver.ErrServerNotFound}
		}})
	defer m.Shutdown()
	m.Start()
	if _, err := m.Info(context.Background()); !errors.Is(err, localserver.ErrServerNotFound) {
		t.Fatalf("esperaba ErrServerNotFound: %v", err)
	}
}

func TestKeepRunningAndAskedPersist(t *testing.T) {
	m, st, _ := newManager(t)
	if m.KeepRunning() {
		t.Fatal("por defecto apagado")
	}
	if err := m.SetKeepRunning(true); err != nil || !st.Get().KeepRunning || !m.KeepRunning() {
		t.Fatal("no se guardó")
	}
	if m.NotificationsAsked() {
		t.Fatal("por defecto sin preguntar")
	}
	_ = m.MarkNotificationsAsked()
	if !m.NotificationsAsked() {
		t.Fatal("no se guardó el aviso")
	}
}

func TestChangedFiresWhenLocalBecomesReady(t *testing.T) {
	m, _, _ := newManager(t)
	ch := m.Changed()
	m.onLocalStatus(localserver.Status{Phase: localserver.PhaseStarting})
	select {
	case <-ch:
		t.Fatal("no debía avisar al arrancar")
	default:
	}
	m.onLocalStatus(localserver.Status{Phase: localserver.PhaseReady})
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("debía avisar al estar listo")
	}
}
