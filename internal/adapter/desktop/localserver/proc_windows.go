//go:build windows

package localserver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createNoWindow      = 0x08000000
	createNewProcessGrp = 0x00000200
	ctrlBreakEvent      = 1
	processQueryLimited = 0x1000
	stillActive         = 259
)

// consoleMu serializa el truco de consola de gracefulSignal (AttachConsole afecta a todo el proceso).
var consoleMu sync.Mutex

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procAttach       = kernel32.NewProc("AttachConsole")
	procFree         = kernel32.NewProc("FreeConsole")
	procSetCtrlHdlr  = kernel32.NewProc("SetConsoleCtrlHandler")
	procGenerateCtrl = kernel32.NewProc("GenerateConsoleCtrlEvent")
)

// hideAndGroup deja el proceso sin ventana de consola y en su propio grupo (para poder pedirle que se apague).
func hideAndGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGrp}
}

// gracefulSignal pide a un proceso lanzado con hideAndGroup que se apague (CTRL_BREAK, que Go entrega como
// os.Interrupt). Windows no tiene SIGTERM: se entra un instante en la consola oculta del servidor y se envía
// el evento solo a su grupo, ignorándolo en este proceso.
func gracefulSignal(pid int) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	_, _, _ = procFree.Call()
	if r, _, err := procAttach.Call(uintptr(pid)); r == 0 {
		return fmt.Errorf("no se pudo entrar en la consola del servidor: %w", err)
	}
	defer procFree.Call()
	_, _, _ = procSetCtrlHdlr.Call(0, 1) // este proceso ignora Ctrl+C mientras dura el envío
	defer procSetCtrlHdlr.Call(0, 0)
	if r, _, err := procGenerateCtrl.Call(ctrlBreakEvent, uintptr(pid)); r == 0 {
		return fmt.Errorf("no se pudo enviar la señal de apagado: %w", err)
	}
	return nil
}

// killTree mata al proceso y a sus hijos.
func killTree(pid int) {
	c := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid))
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := c.Run(); err != nil {
		if p, e := os.FindProcess(pid); e == nil {
			_ = p.Kill()
		}
	}
}

// processAlive dice si hay un proceso vivo con ese pid.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// lockFile toma un candado exclusivo (sin esperar) sobre el archivo; unlock lo suelta.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol); err != nil {
		_ = f.Close()
		return nil, errLocked
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
		_ = f.Close()
	}, nil
}

// restrictDir deja la carpeta accesible solo al usuario actual (DACL protegida, sin herencia).
func restrictDir(dir string) error {
	tok := windows.GetCurrentProcessToken()
	user, err := tok.GetTokenUser()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// bindLifetime ata la vida del proceso hijo a la de este proceso con un objeto de trabajo (KILL_ON_JOB_CLOSE):
// si el escritorio muere a mala fuerza (Administrador de tareas, fallo, corte de luz del proceso), Windows
// termina al servidor local en vez de dejarlo huérfano. La función devuelta se llama al soltar el proceso:
// con keepAlive=true quita la marca para que el servidor sobreviva al cierre ordenado («seguir en segundo
// plano»); con false lo deja en manos de Windows (cierra el objeto, que mata lo que siga vivo).
// Si no se puede crear el objeto devuelve una función que no hace nada (se pierde la red de seguridad, no el servicio).
func bindLifetime(pid int) func(keepAlive bool) {
	nop := func(bool) {}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nop
	}
	set := func(flags uint32) bool {
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		info.BasicLimitInformation.LimitFlags = flags
		_, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
		return err == nil
	}
	if !set(windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE) {
		_ = windows.CloseHandle(job)
		return nop
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nop
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return nop
	}
	var once sync.Once
	return func(keepAlive bool) {
		once.Do(func() {
			if keepAlive {
				set(0)
			}
			_ = windows.CloseHandle(job)
		})
	}
}

// isServerProcess dice si el pid sigue siendo el servidor que lanzamos y no otro proceso que reutilizó el
// número: compara el nombre del ejecutable. Si no se puede averiguar (permisos), se da por bueno solo si
// el proceso existe (la comprobación con token ya lo confirmó al adoptarlo).
func isServerProcess(pid int, exe string) bool {
	if pid <= 0 {
		return false
	}
	if exe == "" {
		return processAlive(pid)
	}
	h, err := windows.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return processAlive(pid)
	}
	return strings.EqualFold(filepath.Base(windows.UTF16ToString(buf[:n])), filepath.Base(exe))
}
