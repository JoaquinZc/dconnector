package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHub simula la API de GitHub y los archivos de la Release en un servidor TLS local.
type fakeGitHub struct {
	t        *testing.T
	srv      *httptest.Server
	tag      string
	draft    bool
	pre      bool
	payload  []byte // contenido del instalador
	sumHash  string // hash que publica SHA256SUMS ("" = el correcto)
	noSums   bool
	noSumRow bool
	assets   map[string]string // nombre -> url (sobrescribe)
	dlHits   atomic.Int32
	sumsHits atomic.Int32
	redirect string // si no vacío, la descarga redirige a esa URL
	chunked  bool
	failOnce atomic.Bool // corta la primera descarga a la mitad
	lastUA   atomic.Value
}

func newFakeGitHub(t *testing.T, tag string, payload []byte) *fakeGitHub {
	f := &fakeGitHub{t: t, tag: tag, payload: payload}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) sha() string {
	if f.sumHash != "" {
		return f.sumHash
	}
	h := sha256.Sum256(f.payload)
	return hex.EncodeToString(h[:])
}

func (f *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	f.lastUA.Store(r.Header.Get("User-Agent"))
	switch {
	case strings.HasSuffix(r.URL.Path, "/releases/latest"):
		ver := strings.TrimPrefix(f.tag, "v")
		var assets []map[string]any
		add := func(name string) {
			u := f.srv.URL + "/dl/" + name
			if o, ok := f.assets[name]; ok {
				u = o
			}
			assets = append(assets, map[string]any{"name": name, "size": len(f.payload), "browser_download_url": u})
		}
		add(fmt.Sprintf("dConnector-%s-x64.msi", ver))
		add(fmt.Sprintf("dConnector-%s-arm64.msi", ver))
		add(fmt.Sprintf("dConnector-%s-portable-x64.zip", ver))
		add(fmt.Sprintf("dConnector-%s-portable-arm64.zip", ver))
		add(`..\evil.msi`)
		if !f.noSums {
			add(SumsName)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": f.tag, "body": "Novedades\x00 de la versión\n- una\n- dos", "html_url": f.srv.URL + "/release",
			"draft": f.draft, "prerelease": f.pre, "assets": assets,
		})
	case r.URL.Path == "/dl/"+SumsName:
		f.sumsHits.Add(1)
		var b strings.Builder
		b.WriteString("deadbeef  otro.zip\n")
		if !f.noSumRow {
			ver := strings.TrimPrefix(f.tag, "v")
			for _, n := range []string{fmt.Sprintf("dConnector-%s-x64.msi", ver), fmt.Sprintf("dConnector-%s-portable-x64.zip", ver)} {
				fmt.Fprintf(&b, "%s  %s\n", f.sha(), n)
			}
		}
		_, _ = w.Write([]byte(b.String()))
	case strings.HasPrefix(r.URL.Path, "/dl/"):
		f.dlHits.Add(1)
		if f.redirect != "" {
			http.Redirect(w, r, f.redirect, http.StatusFound)
			return
		}
		if f.chunked {
			fl := w.(http.Flusher)
			for i := 0; i < 50; i++ {
				_, _ = w.Write(make([]byte, 1024))
				fl.Flush()
			}
			return
		}
		if f.failOnce.CompareAndSwap(true, false) {
			// Anuncia el tamaño completo y corta a la mitad.
			w.Header().Set("Content-Length", fmt.Sprint(len(f.payload)))
			w.WriteHeader(200)
			_, _ = w.Write(f.payload[:len(f.payload)/2])
			if hj, ok := w.(http.Hijacker); ok {
				c, _, _ := hj.Hijack()
				_ = c.Close()
			}
			return
		}
		http.ServeContent(w, r, "x", time.Time{}, strings.NewReader(string(f.payload)))
	default:
		http.NotFound(w, r)
	}
}

type memPrefs struct {
	mu   sync.Mutex
	auto bool
	last time.Time
}

func (p *memPrefs) AutoCheck() bool           { p.mu.Lock(); defer p.mu.Unlock(); return p.auto }
func (p *memPrefs) SetAutoCheck(v bool) error { p.mu.Lock(); p.auto = v; p.mu.Unlock(); return nil }
func (p *memPrefs) LastCheck() time.Time      { p.mu.Lock(); defer p.mu.Unlock(); return p.last }
func (p *memPrefs) SetLastCheck(t time.Time) error {
	p.mu.Lock()
	p.last = t
	p.mu.Unlock()
	return nil
}

type harness struct {
	u      *Updater
	f      *fakeGitHub
	data   string
	exe    string
	prefs  *memPrefs
	events []string
	mu     sync.Mutex
	helper [][]string
	quit   atomic.Int32
}

func newHarness(t *testing.T, f *fakeGitHub, current string, kind Kind, mod func(*Options)) *harness {
	t.Helper()
	h := &harness{f: f, data: t.TempDir(), prefs: &memPrefs{auto: true}}
	h.exe = filepath.Join(t.TempDir(), "dConnector.exe")
	_ = os.WriteFile(h.exe, []byte(fakeExe("cur")), 0o755)
	o := Options{
		CurrentVersion: current, DataDir: h.data, Exe: h.exe, Kind: kind, GOARCH: "amd64",
		API: f.srv.URL, Client: f.srv.Client(), Hosts: []string{"127.0.0.1"}, Retries: 3, RetryDelay: time.Millisecond,
		Prefs: h.prefs,
		Emit:  func(ev string, _ any) { h.mu.Lock(); h.events = append(h.events, ev); h.mu.Unlock() },
		Quit:  func() { h.quit.Add(1) },
		StartHelper: func(exe string, args []string) error {
			h.mu.Lock()
			h.helper = append(h.helper, append([]string{exe}, args...))
			h.mu.Unlock()
			return nil
		},
	}
	if mod != nil {
		mod(&o)
	}
	h.u = New(o)
	return h
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func TestCheckFindsNewVersionAndSendsOnlyUserAgent(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(4096))
	h := newHarness(t, f, "0.3.1", KindMSI, nil)
	st, err := h.u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateAvailable || st.Latest == nil || st.Latest.Version != "0.4.0" {
		t.Fatalf("%+v", st)
	}
	if st.Latest.AssetName != "dConnector-0.4.0-x64.msi" {
		t.Fatalf("asset: %s", st.Latest.AssetName)
	}
	if strings.ContainsRune(st.Latest.Notes, 0) || !strings.Contains(st.Latest.Notes, "- dos") {
		t.Fatalf("notas: %q", st.Latest.Notes)
	}
	if ua, _ := f.lastUA.Load().(string); ua != "dConnector/0.3.1" {
		t.Fatalf("User-Agent: %q", ua)
	}
	if h.prefs.LastCheck().IsZero() || st.LastCheck == 0 {
		t.Fatal("no anotó la hora de la comprobación")
	}
	if !st.CanInstall || st.InstallKind != "msi" {
		t.Fatalf("%+v", st)
	}
}

func TestCheckSelectsAssetByKindAndArch(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(100))
	for _, c := range []struct {
		kind Kind
		arch string
		want string
	}{
		{KindMSI, "amd64", "dConnector-0.4.0-x64.msi"},
		{KindMSI, "arm64", "dConnector-0.4.0-arm64.msi"},
		{KindPortable, "amd64", "dConnector-0.4.0-portable-x64.zip"},
		{KindPortable, "arm64", "dConnector-0.4.0-portable-arm64.zip"},
		{KindOther, "amd64", ""},
	} {
		h := newHarness(t, f, "0.3.1", c.kind, func(o *Options) { o.GOARCH = c.arch })
		st, err := h.u.Check(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if st.Latest == nil || st.Latest.AssetName != c.want {
			t.Errorf("%s/%s: %+v", c.kind, c.arch, st.Latest)
		}
		if c.kind == KindOther && st.CanInstall {
			t.Error("other no instala")
		}
	}
	// 386: no se ofrece.
	h := newHarness(t, f, "0.3.1", KindMSI, func(o *Options) { o.GOARCH = "386" })
	if h.u.Status().CanInstall || h.u.CanApply() == "" {
		t.Error("386 no debe poder instalar")
	}
}

func TestCheckUpToDateAndDev(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(100))
	for _, cur := range []string{"0.4.0", "0.5.0", "dev"} {
		h := newHarness(t, f, cur, KindMSI, nil)
		st, err := h.u.Check(context.Background())
		if err != nil || st.State != StateUpToDate || st.Latest != nil {
			t.Errorf("%s: %+v %v", cur, st, err)
		}
	}
	// La versión de prueba instalada es más antigua que la final.
	h := newHarness(t, f, "0.4.0-prueba", KindMSI, nil)
	if st, _ := h.u.Check(context.Background()); st.State != StateAvailable {
		t.Errorf("0.4.0-prueba debe ver 0.4.0: %+v", st)
	}
	if h := newHarness(t, f, "dev", KindMSI, nil); h.u.Status().CanInstall {
		t.Error("dev no instala")
	}
}

func TestCheckIgnoresDraftAndPrerelease(t *testing.T) {
	for _, mod := range []func(*fakeGitHub){func(f *fakeGitHub) { f.draft = true }, func(f *fakeGitHub) { f.pre = true }} {
		f := newFakeGitHub(t, "v9.0.0", payload(10))
		mod(f)
		h := newHarness(t, f, "0.3.1", KindMSI, nil)
		st, err := h.u.Check(context.Background())
		if err != nil || st.State != StateUpToDate {
			t.Fatalf("%+v %v", st, err)
		}
	}
}

func TestCheckRejectsBadTag(t *testing.T) {
	f := newFakeGitHub(t, `v1.0.0/../../x`, payload(10))
	h := newHarness(t, f, "0.3.1", KindMSI, nil)
	st, err := h.u.Check(context.Background())
	if err == nil || st.State != StateError {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestApplyHappyPathMSI(t *testing.T) {
	pl := payload(300_000)
	f := newFakeGitHub(t, "v0.4.0", pl)
	h := newHarness(t, f, "0.3.1", KindMSI, func(o *Options) { o.Minimized = func() bool { return true } })
	if _, err := h.u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h.data, "updates", "0.4.0", "dConnector-0.4.0-x64.msi")
	if got, _ := os.ReadFile(dest); string(got) != string(pl) {
		t.Fatal("el archivo descargado no es el publicado")
	}
	if h.quit.Load() != 1 || len(h.helper) != 1 {
		t.Fatalf("quit=%d helper=%v", h.quit.Load(), h.helper)
	}
	a, err := ParseApplyArgs(h.helper[0][1:])
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != "msi" || a.File != dest || a.SHA256 != f.sha() || a.Relaunch != h.exe || !a.Minimized || a.ParentData != h.data || a.ParentPID != os.Getpid() {
		t.Fatalf("%+v", a)
	}
	if st := h.u.Status(); st.State != StateInstalling {
		t.Fatalf("estado: %s", st.State)
	}
}

func TestApplyPortablePassesTarget(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(5000))
	h := newHarness(t, f, "0.3.1", KindPortable, nil)
	_, _ = h.u.Check(context.Background())
	if err := h.u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, err := ParseApplyArgs(h.helper[0][1:])
	if err != nil || a.Kind != "portable" || a.Target != filepath.Dir(h.exe) || !strings.HasSuffix(a.File, "portable-x64.zip") {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestApplyRejectsWrongHashAndDeletesFile(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(5000))
	f.sumHash = strings.Repeat("ab", 32)
	h := newHarness(t, f, "0.3.1", KindMSI, nil)
	_, _ = h.u.Check(context.Background())
	err := h.u.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("err=%v", err)
	}
	if _, e := os.Stat(filepath.Join(h.data, "updates", "0.4.0", "dConnector-0.4.0-x64.msi")); e == nil {
		t.Fatal("el archivo con hash malo no se borró")
	}
	if len(h.helper) != 0 || h.quit.Load() != 0 {
		t.Fatal("no debe lanzar el ayudante")
	}
	if st := h.u.Status(); st.State != StateError || st.Error == "" {
		t.Fatalf("%+v", st)
	}
	// Tras el fallo se puede reintentar (ya no está ocupado).
	f.sumHash = ""
	if err := h.u.Apply(context.Background()); err != nil {
		t.Fatalf("reintento: %v", err)
	}
}

func TestApplyRejectsMissingSums(t *testing.T) {
	for name, mod := range map[string]func(*fakeGitHub){
		"sin asset SHA256SUMS": func(f *fakeGitHub) { f.noSums = true },
		"sin fila del archivo": func(f *fakeGitHub) { f.noSumRow = true },
	} {
		f := newFakeGitHub(t, "v0.4.0", payload(5000))
		mod(f)
		h := newHarness(t, f, "0.3.1", KindMSI, nil)
		_, _ = h.u.Check(context.Background())
		if err := h.u.Apply(context.Background()); err == nil {
			t.Fatalf("%s: debía rechazar", name)
		}
		if f.dlHits.Load() != 0 {
			t.Errorf("%s: no debía descargar el instalador sin poder verificarlo", name)
		}
		if len(h.helper) != 0 {
			t.Errorf("%s: lanzó el ayudante", name)
		}
	}
}

func TestApplyRejectsDisallowedHost(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(5000))
	f.assets = map[string]string{"dConnector-0.4.0-x64.msi": "https://evil.example.com/dConnector-0.4.0-x64.msi"}
	h := newHarness(t, f, "0.3.1", KindMSI, nil)
	_, _ = h.u.Check(context.Background())
	err := h.u.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no permitido") {
		t.Fatalf("err=%v", err)
	}
	// http plano tampoco.
	f.assets = map[string]string{"dConnector-0.4.0-x64.msi": "http://127.0.0.1/x.msi"}
	_, _ = h.u.Check(context.Background())
	if err := h.u.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("http: %v", err)
	}
}

func TestApplyRejectsRedirectToDisallowedHost(t *testing.T) {
	for _, target := range []string{"https://evil.example.com/x.msi", "http://127.0.0.1/x.msi", "https://user:pw@127.0.0.1/x"} {
		f := newFakeGitHub(t, "v0.4.0", payload(5000))
		f.redirect = target
		h := newHarness(t, f, "0.3.1", KindMSI, nil)
		_, _ = h.u.Check(context.Background())
		err := h.u.Apply(context.Background())
		if err == nil {
			t.Fatalf("%s: debía rechazar la redirección", target)
		}
		if len(h.helper) != 0 {
			t.Fatal("lanzó el ayudante")
		}
		if _, e := os.Stat(filepath.Join(h.data, "updates", "0.4.0", "dConnector-0.4.0-x64.msi")); e == nil {
			t.Fatal("quedó un archivo")
		}
	}
}

func TestApplyRejectsOversize(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(300_000))
	h := newHarness(t, f, "0.3.1", KindMSI, func(o *Options) { o.MaxSize = 100_000 })
	_, _ = h.u.Check(context.Background())
	err := h.u.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "demasiado grande") && !strings.Contains(err.Error(), "máximo") {
		t.Fatalf("err=%v", err)
	}
	// Sin Content-Length (troceado): se corta al pasar el tope.
	f2 := newFakeGitHub(t, "v0.4.0", payload(10))
	f2.chunked = true
	h2 := newHarness(t, f2, "0.3.1", KindMSI, func(o *Options) { o.MaxSize = 10_000 })
	_, _ = h2.u.Check(context.Background())
	if err := h2.u.Apply(context.Background()); err == nil {
		t.Fatal("debía cortar la descarga sin tamaño")
	}
	for _, p := range []string{filepath.Join(h.data, "updates", "0.4.0"), filepath.Join(h2.data, "updates", "0.4.0")} {
		entries, _ := os.ReadDir(p)
		if len(entries) != 0 {
			t.Errorf("quedaron archivos en %s: %v", p, entries)
		}
	}
}

func TestApplyMaliciousAssetNameNotOffered(t *testing.T) {
	// Solo existe el asset malicioso: no hay instalador utilizable.
	f := newFakeGitHub(t, "v0.4.0", payload(100))
	h := newHarness(t, f, "0.3.1", KindMSI, nil)
	st, _ := h.u.Check(context.Background())
	st.Latest.AssetName = `..\evil.msi`
	h.u.mu.Lock()
	h.u.st.Latest.AssetName = `..\evil.msi`
	h.u.mu.Unlock()
	if err := h.u.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "no es el esperado") {
		t.Fatalf("err=%v", err)
	}
	if validAssetName(`..\evil.msi`) || validAssetName("../x.msi") || validAssetName(`dConnector-1.0.0-x64.msi\..\x`) || validAssetName("C:dConnector-1.0.0-x64.msi") {
		t.Fatal("validAssetName acepta nombres con rutas")
	}
	if !validAssetName("dConnector-0.3.8-prueba-x64.msi") || !validAssetName("dConnector-0.3.8-portable-arm64.zip") {
		t.Fatal("validAssetName rechaza nombres buenos")
	}
	if _, err := AssetName("1.0.0/..", KindMSI, "x64"); err == nil {
		t.Fatal("AssetName debe validar la versión")
	}
}

func TestDownloadResumesAfterCut(t *testing.T) {
	pl := payload(400_000)
	f := newFakeGitHub(t, "v0.4.0", pl)
	f.failOnce.Store(true)
	h := newHarness(t, f, "0.3.1", KindMSI, nil)
	_, _ = h.u.Check(context.Background())
	if err := h.u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h.data, "updates", "0.4.0", "dConnector-0.4.0-x64.msi")
	if got, _ := os.ReadFile(dest); string(got) != string(pl) {
		t.Fatal("el archivo reanudado no coincide")
	}
	if f.dlHits.Load() < 2 {
		t.Fatalf("debía reintentar: %d", f.dlHits.Load())
	}
}

func TestApplyReusesVerifiedFile(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(5000))
	h := newHarness(t, f, "0.3.1", KindMSI, nil)
	_, _ = h.u.Check(context.Background())
	if err := h.u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.u.mu.Lock()
	h.u.busy = false
	h.u.mu.Unlock()
	before := f.dlHits.Load()
	if err := h.u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.dlHits.Load() != before {
		t.Fatal("volvió a descargar un archivo ya verificado")
	}
}

func TestApplyNotAllowedForOtherAndDev(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(100))
	h := newHarness(t, f, "0.3.1", KindOther, nil)
	_, _ = h.u.Check(context.Background())
	if err := h.u.Apply(context.Background()); err == nil {
		t.Fatal("other no instala")
	}
	if h := newHarness(t, f, "dev", KindMSI, nil); h.u.Apply(context.Background()) == nil {
		t.Fatal("dev no instala")
	}
	h3 := newHarness(t, f, "0.3.1", KindMSI, nil)
	if err := h3.u.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "busca") {
		t.Fatalf("sin comprobar: %v", err)
	}
}

func TestAutoCheckFrequencyAndSwitch(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(100))
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	h := newHarness(t, f, "0.3.1", KindMSI, func(o *Options) { o.Now = func() time.Time { return now } })
	if !h.u.AutoCheckDue() {
		t.Fatal("nunca comprobó: toca")
	}
	h.u.AutoCheck(context.Background())
	if st := h.u.Status(); st.State != StateAvailable {
		t.Fatalf("%+v", st)
	}
	if h.u.AutoCheckDue() {
		t.Fatal("recién comprobado: no toca")
	}
	now = now.Add(23 * time.Hour)
	if h.u.AutoCheckDue() {
		t.Fatal("23 h: no toca")
	}
	now = now.Add(2 * time.Hour)
	if !h.u.AutoCheckDue() {
		t.Fatal("25 h: toca")
	}
	// Desactivada: nunca, aunque haya pasado el tiempo y no se consulte la red.
	if err := h.u.SetAutoCheck(false); err != nil {
		t.Fatal(err)
	}
	hits := f.sumsHits.Load()
	h.u.AutoCheck(context.Background())
	if h.u.AutoCheckDue() || f.sumsHits.Load() != hits {
		t.Fatal("desactivada")
	}
	if h.u.Status().AutoCheck {
		t.Fatal("el estado debe reflejar el interruptor")
	}
}

func TestAutoCheckFailuresAreSilent(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(100))
	h := newHarness(t, f, "0.3.1", KindMSI, func(o *Options) { o.API = "https://127.0.0.1:1" })
	h.u.AutoCheck(context.Background())
	st := h.u.Status()
	if st.State != StateIdle || st.Error != "" {
		t.Fatalf("un fallo automático no debe cambiar el estado: %+v", st)
	}
	if len(h.events) != 0 {
		t.Fatalf("un fallo automático no debe emitir eventos: %v", h.events)
	}
	if !h.prefs.LastCheck().IsZero() {
		t.Fatal("un fallo no debe contar como comprobación")
	}
	// En cambio, la manual sí informa.
	if _, err := h.u.Check(context.Background()); err == nil || h.u.Status().State != StateError {
		t.Fatal("la manual debe informar del error")
	}
}

func TestEnvAPIOnlyLoopback(t *testing.T) {
	t.Setenv(EnvAPI, "https://evil.example.com")
	u := New(Options{CurrentVersion: "0.3.1", Kind: KindMSI})
	if u.api != DefaultAPI {
		t.Fatalf("api=%s", u.api)
	}
	t.Setenv(EnvAPI, "http://127.0.0.1:9999")
	u = New(Options{CurrentVersion: "0.3.1", Kind: KindMSI})
	if u.api != "http://127.0.0.1:9999" {
		t.Fatalf("api=%s", u.api)
	}
}

func TestPolicy(t *testing.T) {
	p := newPolicy(DefaultHosts)
	for u, ok := range map[string]bool{
		"https://api.github.com/x":                       true,
		"https://github.com/a/b":                         true,
		"https://objects.githubusercontent.com/a?sig=1":  true,
		"https://release-assets.githubusercontent.com/a": true,
		"http://github.com/a":                            false,
		"https://github.com.evil.com/a":                  false,
		"https://evilgithub.com/a":                       false,
		"https://raw.githubusercontent.com/a":            false,
		"https://user@github.com/a":                      false,
		"ftp://github.com/a":                             false,
	} {
		pu, _ := parseURL(u)
		if err := p.check(pu); (err == nil) != ok {
			t.Errorf("%s: err=%v", u, err)
		}
	}
}

func TestCleanupRemovesOldDownloads(t *testing.T) {
	f := newFakeGitHub(t, "v0.4.0", payload(100))
	h := newHarness(t, f, "0.4.0", KindMSI, nil)
	for _, v := range []string{"0.3.1", "0.4.0", "0.5.0", "basura"} {
		_ = writeFile(filepath.Join(h.data, "updates", v, "x"), "x")
	}
	h.u.Cleanup()
	for v, keep := range map[string]bool{"0.3.1": false, "0.4.0": false, "basura": false, "0.5.0": true} {
		_, err := os.Stat(filepath.Join(h.data, "updates", v))
		if (err == nil) != keep {
			t.Errorf("%s: existe=%v", v, err == nil)
		}
	}
}
