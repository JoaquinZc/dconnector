//go:build !windows

package localserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func hideAndGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func gracefulSignal(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func killTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errLocked
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

func restrictDir(dir string) error { return os.Chmod(dir, 0o700) }

// bindLifetime: fuera de Windows el escritorio no existe como cliente; no hace nada.
func bindLifetime(int) func(keepAlive bool) { return func(bool) {} }

// isServerProcess compara el ejecutable del pid con el del servidor (Linux: /proc); si no se puede saber,
// basta con que el proceso exista.
func isServerProcess(pid int, exe string) bool {
	if !processAlive(pid) {
		return false
	}
	if exe == "" {
		return true
	}
	p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return true
	}
	return filepath.Base(strings.TrimSuffix(p, " (deleted)")) == filepath.Base(exe)
}
