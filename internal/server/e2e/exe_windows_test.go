//go:build windows

package e2e

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

// Mismo mecanismo que usa el escritorio (internal/adapter/desktop/localserver/proc_windows.go): el proceso se
// lanza sin ventana y en su propio grupo, y se le pide el apagado con CTRL_BREAK (Go lo entrega como os.Interrupt).
const (
	createNoWindow      = 0x08000000
	createNewProcessGrp = 0x00000200
	ctrlBreakEvent      = 1
)

var (
	consoleMu        sync.Mutex
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procAttach       = kernel32.NewProc("AttachConsole")
	procFree         = kernel32.NewProc("FreeConsole")
	procSetCtrlHdlr  = kernel32.NewProc("SetConsoleCtrlHandler")
	procGenerateCtrl = kernel32.NewProc("GenerateConsoleCtrlEvent")
)

func hideAndGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGrp}
}

func gracefulSignal(pid int) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	_, _, _ = procFree.Call()
	if r, _, err := procAttach.Call(uintptr(pid)); r == 0 {
		return fmt.Errorf("no se pudo entrar en la consola del servidor: %w", err)
	}
	defer procFree.Call()
	_, _, _ = procSetCtrlHdlr.Call(0, 1)
	defer procSetCtrlHdlr.Call(0, 0)
	if r, _, err := procGenerateCtrl.Call(ctrlBreakEvent, uintptr(pid)); r == 0 {
		return fmt.Errorf("no se pudo enviar la señal de apagado: %w", err)
	}
	return nil
}

func killTree(pid int) {
	c := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid))
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	_ = c.Run()
}
