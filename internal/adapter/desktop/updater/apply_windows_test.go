//go:build windows

package updater

import (
	"os/exec"
	"testing"
	"time"
)

func TestWaitParentWindows(t *testing.T) {
	cmd := exec.Command("ping.exe", "-n", "4", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	if err := waitParent(cmd.Process.Pid, 150*time.Millisecond); err != errTimeout {
		t.Fatalf("debía agotar el tiempo con el proceso vivo: %v", err)
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	if err := waitParent(cmd.Process.Pid, 2*time.Second); err != nil {
		t.Fatalf("un proceso terminado no se espera: %v", err)
	}
}

func TestServerPIDsEmptyForUnknownDir(t *testing.T) {
	if p := serverPIDs(t.TempDir()); len(p) != 0 {
		t.Fatalf("%v", p)
	}
	if err := waitServers(t.TempDir(), time.Second); err != nil {
		t.Fatal(err)
	}
}
