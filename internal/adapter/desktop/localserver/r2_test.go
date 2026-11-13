package localserver

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// fakeDesktop hace de «escritorio»: lanza el servidor falso con un Launcher y se queda esperando hasta que lo
// maten a mala fuerza (la prueba lo mata sin darle opción a apagar nada).
func fakeDesktop(dir string) {
	l := New(Options{
		ExePath: os.Args[0], DataDir: dir, StartTimeout: 20 * time.Second, StopTimeout: 10 * time.Second,
		Env: append(os.Environ(), "DC_FAKE_SERVER=1", "DC_FAKE_DESKTOP="),
	})
	if err := l.Start(); err != nil {
		fmt.Println("error:", err)
		os.Exit(2)
	}
	for i := 0; i < 200 && !l.Ready(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("pid", l.PID())
	time.Sleep(10 * time.Minute)
}

// Matar el escritorio a mala fuerza no debe dejar el servidor local huérfano (en Windows, objeto de trabajo).
func TestHardKilledDesktopDoesNotOrphanServer(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("la red de seguridad del objeto de trabajo es de Windows")
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	cmd.Env = append(os.Environ(), "DC_FAKE_DESKTOP="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	if _, err := fmt.Fscanf(out, "pid %d\n", &pid); err != nil || pid <= 0 {
		_ = cmd.Process.Kill()
		t.Fatalf("el escritorio falso no llegó a lanzar el servidor: %v", err)
	}
	t.Cleanup(func() { killTree(pid) })
	if !processAlive(pid) {
		t.Fatal("el servidor debía estar vivo")
	}
	_ = cmd.Process.Kill() // sin apagado ordenado
	_, _ = cmd.Process.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatal("el servidor quedó huérfano tras matar el escritorio a mala fuerza")
	}
}

// Un pid reutilizado por otro proceso nunca se señala ni se mata.
func TestStopPIDRefusesForeignProcess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	cmd.Env = append(os.Environ(), "DC_SLEEPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	pid := cmd.Process.Pid

	if isServerProcess(pid, "dConnector-server-que-no-es") {
		t.Fatal("un proceso con otro nombre no es nuestro servidor")
	}
	if !isServerProcess(pid, os.Args[0]) {
		t.Fatal("el proceso con el mismo ejecutable sí debía reconocerse")
	}
	l := New(Options{DataDir: t.TempDir(), StopTimeout: 300 * time.Millisecond})
	l.exePath = "dConnector-server-que-no-es"
	l.stopPID(pid)
	time.Sleep(200 * time.Millisecond)
	if !processAlive(pid) {
		t.Fatal("stopPID mató a un proceso ajeno que reutilizó el pid")
	}
}

// Stop justo cuando el servidor acaba de quedar listo no debe dejarlo vivo si no se pidió «seguir en segundo plano».
func TestStopRacingWithReadyNeverLeaksServer(t *testing.T) {
	for i := 0; i < 12; i++ {
		l, dir := newLauncher(t, "", nil)
		if err := l.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(i) * 90 * time.Millisecond)
		l.Stop()
		time.Sleep(100 * time.Millisecond)
		if _, _, pid := func() (a []string, s string, p int) {
			defer func() { _ = recover() }()
			b, err := os.ReadFile(dir + string(os.PathSeparator) + "fake-info.json")
			if err != nil {
				return
			}
			_, _ = fmt.Sscanf(extractPID(string(b)), "%d", &p)
			return
		}(); pid > 0 && processAlive(pid) {
			killTree(pid)
			t.Fatalf("iteración %d: el servidor sobrevivió a Stop sin pedir segundo plano (pid %d)", i, pid)
		}
		if _, ok := readRecord(dir); ok {
			t.Fatalf("iteración %d: localserver.json quedó tras Stop", i)
		}
	}
}

func extractPID(js string) string {
	const k = `"pid":`
	for i := 0; i+len(k) < len(js); i++ {
		if js[i:i+len(k)] == k {
			j := i + len(k)
			e := j
			for e < len(js) && js[e] >= '0' && js[e] <= '9' {
				e++
			}
			return js[j:e]
		}
	}
	return ""
}
