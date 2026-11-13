//go:build windows

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dconnector/internal/adapter/desktop/client"
	"dconnector/internal/adapter/desktop/localserver"
)

func pidAlive(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH", "/FO", "CSV").Output()
	return err == nil && strings.Contains(string(out), fmt.Sprintf("\"%d\"", pid))
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("se agotó la espera: %s", what)
}

// matarSiVive: red de seguridad al final de la prueba (solo pids lanzados aquí).
func matarSiVive(t *testing.T, pids ...*int) {
	t.Cleanup(func() {
		for _, p := range pids {
			if *p > 0 && pidAlive(*p) {
				_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(*p)).Run()
			}
		}
	})
}

func TestLauncherLifecycleWithRealServer(t *testing.T) {
	skipShort(t)
	data := t.TempDir()
	keep := false
	var statuses []localserver.Phase
	newLauncher := func() *localserver.Launcher {
		return localserver.New(localserver.Options{
			ExePath: serverExe, DataDir: data, StartTimeout: 40 * time.Second, StopTimeout: 15 * time.Second,
			KeepRunning: func() bool { return keep },
			Backoff:     func(int) time.Duration { return 200 * time.Millisecond },
			OnStatus:    func(s localserver.Status) { statuses = append(statuses, s.Phase) },
		})
	}
	var pid1, pid2 int
	matarSiVive(t, &pid1, &pid2)

	// 1) Arranque: línea «listening», /health y auth/me con el token local.
	l := newLauncher()
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := l.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	pid1 = l.PID()
	if pid1 <= 0 || !pidAlive(pid1) {
		t.Fatalf("pid del servidor no válido: %d", pid1)
	}
	cl := client.New(client.Config{BaseURL: l.BaseURL(), Token: l.Token, ClientName: "integration-test"})
	h, err := cl.Health(ctx)
	if err != nil || !h.OK() || h.APIVersion != 1 || h.TLS {
		t.Fatalf("health: %+v %v", h, err)
	}
	me, err := cl.Me(ctx)
	if err != nil {
		t.Fatalf("auth/me con el token local: %v", err)
	}
	t.Logf("auth/me: %+v (versión %s)", me, l.Version())
	if _, err := os.Stat(filepath.Join(localserver.RunDir(data), "localserver.json")); err != nil {
		t.Fatalf("falta run/localserver.json: %v", err)
	}
	// Con un token ajeno la API debe rechazar.
	bad := client.New(client.Config{BaseURL: l.BaseURL(), Token: func() string { return "no-es-el-token" }})
	if _, err := bad.Me(ctx); !client.IsUnauthorized(err) {
		t.Fatalf("un token ajeno debía dar 401, dio: %v", err)
	}

	// 2) Reinicio automático: se mata SOLO el pid lanzado.
	if out, err := exec.Command("taskkill", "/F", "/PID", fmt.Sprint(pid1)).CombinedOutput(); err != nil {
		t.Fatalf("taskkill: %v %s", err, out)
	}
	waitFor(t, "reinicio con otro pid", 60*time.Second, func() bool {
		p := l.PID()
		return l.Ready() && p != 0 && p != pid1
	})
	pid2 = l.PID()
	cl2 := client.New(client.Config{BaseURL: l.BaseURL(), Token: l.Token})
	if _, err := cl2.Me(ctx); err != nil {
		t.Fatalf("auth/me tras el reinicio: %v", err)
	}
	if pidAlive(pid1) {
		t.Fatalf("el pid viejo %d sigue vivo", pid1)
	}

	// 3) «Seguir en segundo plano»: Stop deja el servidor vivo y el registro en su sitio.
	keep = true
	l.Stop()
	if !pidAlive(pid2) {
		t.Fatalf("con «seguir en segundo plano» el servidor %d debía seguir vivo", pid2)
	}
	if _, err := os.Stat(filepath.Join(localserver.RunDir(data), "localserver.json")); err != nil {
		t.Fatalf("el registro debía quedar: %v", err)
	}

	// 4) Readopción desde run/localserver.json.
	statuses = nil
	l2 := newLauncher()
	if err := l2.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l2.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	if l2.PID() != pid2 || !l2.Status().Adopted {
		t.Fatalf("debía readoptar el pid %d: pid=%d status=%+v", pid2, l2.PID(), l2.Status())
	}
	if _, err := client.New(client.Config{BaseURL: l2.BaseURL(), Token: l2.Token}).Me(ctx); err != nil {
		t.Fatalf("auth/me tras readoptar: %v", err)
	}

	// 5) Apagado ordenado sin «seguir»: el proceso desaparece y no queda registro.
	keep = false
	began := time.Now()
	l2.Stop()
	took := time.Since(began)
	if pidAlive(pid2) {
		t.Fatalf("el servidor %d quedó huérfano", pid2)
	}
	if _, err := os.Stat(filepath.Join(localserver.RunDir(data), "localserver.json")); !os.IsNotExist(err) {
		t.Fatalf("el registro debía borrarse: %v", err)
	}
	t.Logf("apagado de un servidor readoptado en %s", took)
	if _, err := cl2.Health(ctx); err == nil {
		t.Fatal("el servidor aún responde tras apagarlo")
	}

	// 6) Servidor propio, apagado ordenado: ¿fue ordenado (rápido) o hubo que matarlo?
	l3 := newLauncher()
	if err := l3.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l3.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	pid3 := l3.PID()
	matarSiVive(t, &pid3)
	began = time.Now()
	l3.Stop()
	took = time.Since(began)
	if pidAlive(pid3) {
		t.Fatalf("el servidor %d quedó huérfano", pid3)
	}
	t.Logf("apagado de un servidor propio en %s", took)
	if took > 10*time.Second {
		t.Errorf("el apagado tardó %s: no fue ordenado (se tuvo que terminar el proceso)", took)
	}
	if st := l3.Status().Phase; st != localserver.PhaseStopped {
		t.Errorf("fase final %s", st)
	}
}
