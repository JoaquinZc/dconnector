//go:build windows

package updater

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	detachedProcess     = 0x00000008
	createNewProcessGrp = 0x00000200
	createNoWindow      = 0x08000000
	mbOK                = 0x0
	mbIconError         = 0x10
	mbSetForeground     = 0x10000
	mbTopmost           = 0x40000
	processQueryLimited = 0x1000
	processTerminate    = 0x0001
	processSynchronize  = 0x00100000
)

func startDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGrp}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// waitParent espera a que termine el proceso pid (si ya no existe, vuelve al instante).
func waitParent(pid int, timeout time.Duration) error {
	h, err := windows.OpenProcess(processSynchronize, false, uint32(pid))
	if err != nil {
		return nil // ya terminó (o no se puede abrir: no hay nada que esperar)
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
	if err != nil {
		return err
	}
	if ev == uint32(windows.WAIT_TIMEOUT) {
		return errTimeout
	}
	return nil
}

func processImage(pid uint32) string {
	h, err := windows.OpenProcess(processQueryLimited, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// serverPIDs devuelve los pid de dConnector-server.exe cuya imagen es EXACTAMENTE <dir>\dConnector-server.exe.
func serverPIDs(dir string) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var out []uint32
	want := filepath.Join(dir, exeServer)
	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if !strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), exeServer) {
			continue
		}
		if img := processImage(pe.ProcessID); img != "" && strings.EqualFold(filepath.Clean(img), filepath.Clean(want)) {
			out = append(out, pe.ProcessID)
		}
	}
	return out
}

// waitServers espera a que terminen los servidores locales de esta carpeta; si alguno sigue tras el tiempo,
// lo termina (solo si su imagen es la nuestra).
func waitServers(dir string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(serverPIDs(dir)) == 0 {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	for _, pid := range serverPIDs(dir) {
		h, err := windows.OpenProcess(processTerminate|processSynchronize, false, pid)
		if err != nil {
			return fmt.Errorf("no se pudo cerrar el servidor local (pid %d): %w", pid, err)
		}
		_ = windows.TerminateProcess(h, 1)
		_, _ = windows.WaitForSingleObject(h, 5000)
		windows.CloseHandle(h)
	}
	if len(serverPIDs(dir)) != 0 {
		return errors.New("el servidor local sigue abierto")
	}
	return nil
}

func runMSI(msi, logPath string) (int, error) {
	cmd := exec.Command("msiexec.exe", "/i", msi, "/passive", "/norestart", "/l*v", logPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func relaunch(exe string, args []string) error {
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("no se encuentra %s", exe)
	}
	return startDetached(exe, args)
}

func showMessage(title, text string) {
	t, err1 := windows.UTF16PtrFromString(title)
	m, err2 := windows.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return
	}
	_, _ = windows.MessageBox(0, m, t, mbOK|mbIconError|mbSetForeground|mbTopmost)
}

func openFolder(path string) {
	_ = exec.Command("explorer.exe", path).Start()
}
