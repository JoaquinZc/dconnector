//go:build windows

package integration

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/adapter/desktop/client"
	"dconnector/internal/adapter/desktop/connection"
	"dconnector/internal/adapter/desktop/secret"
)

type remoteServer struct {
	cmd         *exec.Cmd
	pid         int
	addr        string
	user, pass  string
	fingerprint string // hex, minúsculas, sin «:»
}

var (
	passRe = regexp.MustCompile(`Usuario: (\S+)\s+Clave: (\S+)`)
	fpRe   = regexp.MustCompile(`Huella TLS \(SHA-256\): ([0-9A-F:]+)`)
)

// startRemote lanza el servidor real con TLS autofirmado (modo remoto) en addr (puerto fijo para poder
// reiniciarlo con otro certificado en la misma dirección).
func startRemote(t *testing.T, data, addr string) *remoteServer {
	t.Helper()
	cmd := exec.Command(serverExe, "--data", data, "--listen", addr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	var mu sync.Mutex
	cmd.Stderr = writerFunc(func(p []byte) { mu.Lock(); errBuf.Write(p); mu.Unlock() })
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &remoteServer{cmd: cmd, pid: cmd.Process.Pid}
	t.Cleanup(func() { s.stop() })
	ready := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(out)
		closed := false
		for sc.Scan() {
			line := sc.Text()
			if m := passRe.FindStringSubmatch(line); m != nil {
				s.user, s.pass = m[1], m[2]
			}
			if m := fpRe.FindStringSubmatch(line); m != nil {
				s.fingerprint = strings.ToLower(strings.ReplaceAll(m[1], ":", ""))
			}
			if strings.Contains(line, `"event":"listening"`) && !closed {
				closed = true
				s.addr = addr
				close(ready)
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(40 * time.Second):
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("el servidor remoto no avisó «listening»: %s", errBuf.String())
	}
	return s
}

type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

func (s *remoteServer) stop() {
	if s.cmd.ProcessState == nil && pidAlive(s.pid) {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(s.pid)).Run()
	}
	_ = s.cmd.Wait()
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func TestRemoteTLSClientAndFingerprint(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	addr := freeAddr(t)
	srv := startRemote(t, t.TempDir(), addr)
	if srv.user == "" || srv.pass == "" || srv.fingerprint == "" {
		t.Fatalf("faltan usuario/clave/huella en la salida del servidor: %+v", srv)
	}

	store, err := connection.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr := connection.New(connection.Options{Store: store, Protector: secret.Default(), ClientName: "integration-test/0.3.0"})
	defer mgr.Shutdown()
	base := "https://" + addr

	// 1) Sin huella fijada: error de certificado con la huella del servidor.
	err = mgr.SaveRemote(ctx, base, srv.user, srv.pass)
	var ce *connection.CertError
	if !errors.As(err, &ce) || ce.Changed || ce.Fingerprint != srv.fingerprint {
		t.Fatalf("debía pedir confiar en la huella %s, dio: %v", srv.fingerprint, err)
	}
	// 2) Una huella equivocada no se acepta; la correcta sí.
	if err := mgr.TrustTLS(strings.Repeat("ab", 32)); err == nil {
		t.Fatal("TrustTLS aceptó una huella distinta de la presentada")
	}
	if err := mgr.TrustTLS(connection.FormatFingerprint(ce.Fingerprint)); err != nil {
		t.Fatalf("TrustTLS: %v", err)
	}
	// 3) Clave mala: error claro; clave buena: guarda y pasa a remoto.
	if err := mgr.SaveRemote(ctx, base, srv.user, "clave-equivocada"); err == nil {
		t.Fatal("el login con clave mala debía fallar")
	}
	if err := mgr.SaveRemote(ctx, base, srv.user, srv.pass); err != nil {
		t.Fatalf("SaveRemote tras confiar: %v", err)
	}
	info, err := mgr.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode != connection.ModeRemote || info.User != srv.user || info.Token == "" ||
		info.TLSFingerprint != connection.FormatFingerprint(srv.fingerprint) || info.ServerVersion == "" {
		t.Fatalf("info inesperada: %+v", info)
	}
	if !strings.HasPrefix(info.BaseURL, "http://127.0.0.1:") {
		t.Fatalf("con huella fijada la interfaz debía hablar con el reenvío en loopback: %s", info.BaseURL)
	}

	// 4) Cliente Go: RPC y WebSocket (hello y eventos).
	cl, _, err := mgr.Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	me, err := cl.Me(ctx)
	if err != nil || me.Username != srv.user {
		t.Fatalf("me: %+v %v", me, err)
	}
	var ver string
	if err := cl.CallInto(ctx, &ver, "Settings", "Version"); err != nil {
		t.Fatalf("rpc Settings.Version: %v", err)
	}
	t.Logf("Settings.Version = %q", ver)

	evCtx, evCancel := context.WithCancel(ctx)
	hello := make(chan client.Hello, 4)
	var evMu sync.Mutex
	var names []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		cl.RunEvents(evCtx, func(ev client.Event) { evMu.Lock(); names = append(names, ev.Name); evMu.Unlock() },
			func(s client.StreamState) {
				if s.Connected {
					hello <- s.Hello
				}
			})
	}()
	select {
	case h := <-hello:
		if h.User != srv.user || h.ConnID == "" || h.APIVersion != 1 {
			t.Fatalf("hello inesperado: %+v", h)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no llegó «hello» por el WebSocket")
	}
	// El servidor solo emite eventos ante cambios (sin cuentas no hay ninguno que provocar): se comprueba
	// que el connId de «hello» queda en el cliente y que la conexión sigue abierta.
	if cl.ConnID() == "" {
		t.Error("el cliente no guardó el connId de hello")
	}
	time.Sleep(1500 * time.Millisecond)
	evMu.Lock()
	t.Logf("eventos recibidos (incluido hello): %v", names)
	evMu.Unlock()

	// 5) Cambio de huella: el servidor se reinicia en la MISMA dirección con otros datos (otro certificado).
	evCancel()
	<-done
	srv.stop()
	waitFor(t, "puerto libre", 10*time.Second, func() bool {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return false
		}
		l.Close()
		return true
	})
	srv2 := startRemote(t, t.TempDir(), addr)
	if srv2.fingerprint == srv.fingerprint {
		t.Fatal("los dos servidores generaron el mismo certificado")
	}
	_, err = mgr.Info(ctx)
	if !errors.As(err, &ce) || !ce.Changed || ce.Fingerprint != srv2.fingerprint {
		t.Fatalf("debía detectar el cambio de huella a %s, dio: %v", srv2.fingerprint, err)
	}
	if !strings.Contains(err.Error(), "cambió") {
		t.Fatalf("el mensaje no explica el cambio: %v", err)
	}
	// Y tras confiar en la nueva huella se reconecta (relogin con la clave guardada; el usuario no existe
	// en el servidor nuevo, así que debe fallar con un error de credenciales, no de certificado).
	if err := mgr.TrustTLS(ce.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveRemote(ctx, base, srv2.user, srv2.pass); err != nil {
		t.Fatalf("SaveRemote contra el servidor nuevo: %v", err)
	}
	if _, err := mgr.Info(ctx); err != nil {
		t.Fatalf("Info tras reconfigurar: %v", err)
	}
}
