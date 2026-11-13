//go:build !windows

package e2e

import (
	"os/exec"
	"syscall"
)

func hideAndGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func gracefulSignal(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func killTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
