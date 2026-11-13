package localserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain: si DC_FAKE_SERVER está en el entorno, este binario hace de dConnector-server falso (§8 de docs/14).
func TestMain(m *testing.M) {
	if os.Getenv("DC_FAKE_SERVER") == "1" {
		fakeServer()
		return
	}
	if os.Getenv("DC_SLEEPER") == "1" {
		time.Sleep(2 * time.Minute)
		return
	}
	if d := os.Getenv("DC_FAKE_DESKTOP"); d != "" {
		fakeDesktop(d)
		return
	}
	os.Exit(m.Run())
}

func fakeServer() {
	var data string
	for i, a := range os.Args {
		if a == "--data" && i+1 < len(os.Args) {
			data = os.Args[i+1]
		}
	}
	token := os.Getenv(EnvToken)
	mode := os.Getenv("DC_FAKE_MODE")
	info, _ := json.Marshal(map[string]any{"args": os.Args[1:], "token": token, "pid": os.Getpid()})
	_ = os.WriteFile(filepath.Join(data, "fake-info.json"), info, 0o600)

	sig := make(chan os.Signal, 4)
	signal.Notify(sig, os.Interrupt)
	if mode == "nolisten" {
		time.Sleep(time.Minute)
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","version":"9.9.9","apiVersion":1,"protocol":1,"tls":false}`))
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"username":"local"}`))
	})
	go func() { _ = http.Serve(ln, mux) }()
	fmt.Println("Clave del primer arranque: SECRETA-NO-LOGUEAR") // línea que el lanzador debe ignorar
	fmt.Printf(`{"event":"listening","addr":%q,"tls":false,"version":"9.9.9","pid":%d}`+"\n", ln.Addr().String(), os.Getpid())

	if mode == "crash-once" {
		marker := filepath.Join(data, "crashed")
		if _, err := os.Stat(marker); err != nil {
			_ = os.WriteFile(marker, nil, 0o600)
			time.Sleep(300 * time.Millisecond)
			os.Exit(3)
		}
	}
	for s := range sig {
		_ = s
		if mode == "ignore" {
			continue
		}
		_ = os.WriteFile(filepath.Join(data, "graceful"), nil, 0o600)
		os.Exit(0)
	}
}

func newLauncher(t *testing.T, mode string, mod func(*Options)) (*Launcher, string) {
	t.Helper()
	dir := t.TempDir()
	o := Options{
		ExePath:      os.Args[0],
		DataDir:      dir,
		Env:          append(os.Environ(), "DC_FAKE_SERVER=1", "DC_FAKE_MODE="+mode),
		StartTimeout: 10 * time.Second,
		StopTimeout:  10 * time.Second,
		Backoff:      func(int) time.Duration { return 50 * time.Millisecond },
	}
	if mod != nil {
		mod(&o)
	}
	l := New(o)
	t.Cleanup(func() {
		l.Stop()
		if r, ok := readRecord(dir); ok {
			killTree(r.PID)
		}
	})
	return l, dir
}

func waitCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

func readFake(t *testing.T, dir string) (args []string, token string, pid int) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "fake-info.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Args  []string `json:"args"`
		Token string   `json:"token"`
		PID   int      `json:"pid"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v.Args, v.Token, v.PID
}

func TestStartReadyTokenOnlyByEnv(t *testing.T) {
	l, dir := newLauncher(t, "", nil)
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l.WaitReady(waitCtx(t, 20*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(l.BaseURL(), "http://127.0.0.1:") || l.Token() == "" || l.Version() != "9.9.9" {
		t.Fatalf("estado inesperado: %q %q %q", l.BaseURL(), l.Token(), l.Version())
	}
	args, envTok, _ := readFake(t, dir)
	if envTok != l.Token() {
		t.Fatal("el servidor no recibió el token por entorno")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, l.Token()) || !strings.Contains(joined, "--no-tls") || !strings.Contains(joined, "127.0.0.1:0") {
		t.Fatalf("argumentos inesperados: %v", args)
	}
	req, _ := http.NewRequest("GET", l.BaseURL()+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+l.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("me: %v %v", resp, err)
	}
	resp.Body.Close()
	// El registro quedó guardado.
	if r, ok := readRecord(dir); !ok || r.Token != l.Token() {
		t.Fatal("falta localserver.json")
	}
}

func TestStopShutsDownAndRemovesRecord(t *testing.T) {
	l, dir := newLauncher(t, "", nil)
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l.WaitReady(waitCtx(t, 20*time.Second)); err != nil {
		t.Fatal(err)
	}
	pid := l.PID()
	l.Stop()
	if processAlive(pid) {
		t.Fatal("el servidor sigue vivo tras Stop")
	}
	if _, ok := readRecord(dir); ok {
		t.Fatal("localserver.json debía borrarse")
	}
	if l.Status().Phase != PhaseStopped || l.BaseURL() != "" {
		t.Fatalf("estado: %+v", l.Status())
	}
	if _, err := os.Stat(filepath.Join(dir, "graceful")); err != nil {
		t.Log("aviso: el apagado no fue ordenado (se usó la muerte forzada)")
	}
}

func TestStopKillsWhenIgnoringSignal(t *testing.T) {
	l, _ := newLauncher(t, "ignore", func(o *Options) { o.StopTimeout = 500 * time.Millisecond })
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l.WaitReady(waitCtx(t, 20*time.Second)); err != nil {
		t.Fatal(err)
	}
	pid := l.PID()
	began := time.Now()
	l.Stop()
	if processAlive(pid) {
		t.Fatal("el servidor sigue vivo: no se mató")
	}
	if time.Since(began) > 8*time.Second {
		t.Fatal("tardó demasiado en terminar")
	}
}

func TestStartTimeoutReportsAndRetries(t *testing.T) {
	var mu sync.Mutex
	var seen []Status
	l, _ := newLauncher(t, "nolisten", func(o *Options) {
		o.StartTimeout = 400 * time.Millisecond
		o.StopTimeout = 300 * time.Millisecond
		o.OnStatus = func(s Status) { mu.Lock(); seen = append(seen, s); mu.Unlock() }
	})
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := 0
		for _, s := range seen {
			if s.Phase == PhaseRestarting && strings.Contains(s.Err, "no avisó") {
				n++
			}
		}
		mu.Unlock()
		if n >= 2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no se informó el fallo con reintentos: %+v", seen)
}

func TestRestartsAfterCrash(t *testing.T) {
	var mu sync.Mutex
	var phases []Phase
	l, _ := newLauncher(t, "crash-once", func(o *Options) {
		o.OnStatus = func(s Status) { mu.Lock(); phases = append(phases, s.Phase); mu.Unlock() }
	})
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l.WaitReady(waitCtx(t, 20*time.Second)); err != nil {
		t.Fatal(err)
	}
	firstPID := l.PID()
	// Espera la caída y el segundo arranque.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if l.Ready() && l.PID() != firstPID {
			mu.Lock()
			defer mu.Unlock()
			sawRestart := false
			for _, p := range phases {
				if p == PhaseRestarting {
					sawRestart = true
				}
			}
			if !sawRestart {
				t.Fatalf("no se informó el reintento: %v", phases)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("el servidor no se reinició tras caer")
}

func TestKeepRunningThenAdopt(t *testing.T) {
	keep := true
	l1, dir := newLauncher(t, "", func(o *Options) { o.KeepRunning = func() bool { return keep } })
	if err := l1.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l1.WaitReady(waitCtx(t, 20*time.Second)); err != nil {
		t.Fatal(err)
	}
	pid, tok, addr := l1.PID(), l1.Token(), l1.BaseURL()
	l1.Stop()
	if !processAlive(pid) {
		t.Fatal("el servidor debía seguir vivo en segundo plano")
	}
	if _, ok := readRecord(dir); !ok {
		t.Fatal("localserver.json debía quedar")
	}

	// «Al volver a abrir»: otro lanzador sobre la misma carpeta reutiliza el servidor.
	l2 := New(Options{ExePath: os.Args[0], DataDir: dir, StartTimeout: 10 * time.Second, StopTimeout: 10 * time.Second,
		Env: append(os.Environ(), "DC_FAKE_SERVER=1")})
	if err := l2.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l2.WaitReady(waitCtx(t, 20*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !l2.Status().Adopted || l2.PID() != pid || l2.Token() != tok || l2.BaseURL() != addr {
		t.Fatalf("no reutilizó el servidor: %+v", l2.Status())
	}
	// Con la opción apagada, al salir lo apaga.
	l2.Stop()
	if processAlive(pid) {
		t.Fatal("el servidor adoptado debía apagarse")
	}
	if _, ok := readRecord(dir); ok {
		t.Fatal("localserver.json debía borrarse")
	}
}

func TestStartLockPreventsSecondServer(t *testing.T) {
	dir := t.TempDir()
	if err := ensureRunDir(dir); err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireStartLock(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireStartLock(dir, 200*time.Millisecond); !errors.Is(err, errLocked) {
		t.Fatalf("el segundo candado debía fallar: %v", err)
	}
	unlock()
	u2, err := acquireStartLock(dir, time.Second)
	if err != nil {
		t.Fatalf("tras soltar debía poder tomarse: %v", err)
	}
	u2()
}

func TestFindServerExe(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindServerExe(filepath.Join(dir, "no-existe.exe")); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("esperaba ErrServerNotFound: %v", err)
	}
	f := filepath.Join(dir, "srv.exe")
	_ = os.WriteFile(f, []byte("x"), 0o700)
	if got, err := FindServerExe(f); err != nil || got != f {
		t.Fatalf("explícito: %q %v", got, err)
	}
	t.Setenv(EnvServerExe, f)
	if got, err := FindServerExe(""); err != nil || got != f {
		t.Fatalf("variable: %q %v", got, err)
	}
	t.Setenv(EnvServerExe, "")
	l := New(Options{ExePath: filepath.Join(dir, "no.exe"), DataDir: dir})
	if err := l.Start(); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("Start sin exe: %v", err)
	}
}

func TestParseListening(t *testing.T) {
	ok := `{"event":"listening","addr":"127.0.0.1:51234","tls":false,"version":"0.3.0","pid":1234}`
	if ev, good := parseListening([]byte(ok)); !good || ev.Addr != "127.0.0.1:51234" || ev.PID != 1234 {
		t.Fatalf("línea válida rechazada: %+v", ev)
	}
	for _, bad := range []string{
		"", "hola", `{"event":"otra"}`,
		`{"event":"listening","addr":"0.0.0.0:80","tls":false}`,
		`{"event":"listening","addr":"192.168.1.5:80","tls":false}`,
		`{"event":"listening","addr":"127.0.0.1:80","tls":true}`,
		`{"event":"listening","addr":"nada"}`,
	} {
		if _, good := parseListening([]byte(bad)); good {
			t.Errorf("debía rechazarse: %q", bad)
		}
	}
}

func TestDefaultBackoff(t *testing.T) {
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30}
	for i, w := range want {
		if got := DefaultBackoff(i + 1); got != w*time.Second {
			t.Errorf("intento %d: %v", i+1, got)
		}
	}
}
