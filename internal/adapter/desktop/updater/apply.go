package updater

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Códigos de salida del ayudante.
const (
	ExitOK     = 0
	ExitFailed = 1
	ExitUsage  = 2
)

// hooks son las acciones del sistema que usa el ayudante (sustituibles en pruebas).
type hooks struct {
	waitParent  func(pid int, timeout time.Duration) error
	waitServers func(dir string, timeout time.Duration) error
	runMSI      func(msi, logPath string) (int, error)
	relaunch    func(exe string, args []string) error
	message     func(title, text string)
	openFolder  func(path string)
	now         func() time.Time
}

func systemHooks() hooks {
	return hooks{
		waitParent: waitParent, waitServers: waitServers, runMSI: runMSI, relaunch: relaunch,
		message: showMessage, openFolder: openFolder, now: time.Now,
	}
}

const (
	parentWait = 60 * time.Second
	serverWait = 20 * time.Second
	msgTitle   = "dConnector · Actualización"
)

// RunApply es el modo ayudante (dConnector.exe --apply-update …). Devuelve el código de salida del proceso.
func RunApply(args []string) int { return runApply(args, systemHooks()) }

func runApply(args []string, h hooks) int {
	a, err := ParseApplyArgs(args)
	if err != nil {
		// Sin datos de dónde escribir el registro: solo se avisa.
		h.message(msgTitle, "No se pudo iniciar la actualización: "+err.Error())
		return ExitUsage
	}
	logDir := filepath.Join(a.ParentData, "logs", "desktop")
	_ = os.MkdirAll(logDir, 0o755)
	stamp := h.now().Format("20060102-150405")
	logPath := filepath.Join(logDir, "update-"+stamp+".log")
	lw := io.Discard
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		defer f.Close()
		lw = f
	}
	log := slog.New(slog.NewTextHandler(lw, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log.Info("ayudante de actualización", "tipo", a.Kind, "archivo", filepath.Base(a.File), "pid", a.ParentPID)

	folder := filepath.Dir(a.File)
	fail := func(cause string, relaunchOld bool) int {
		log.Error("actualización fallida", "causa", cause)
		h.message(msgTitle, cause+"\n\nEl instalador descargado está en:\n"+a.File+"\n\nRegistro:\n"+logPath+"\n\nSe abrirá la carpeta del instalador.")
		h.openFolder(folder)
		if relaunchOld {
			if err := h.relaunch(a.Relaunch, relaunchArgs(a)); err != nil {
				log.Warn("no se pudo volver a abrir la versión anterior", "error", err)
			}
		}
		return ExitFailed
	}

	if err := h.waitParent(a.ParentPID, parentWait); err != nil {
		return fail("dConnector no se cerró a tiempo, así que no se instaló nada: "+err.Error(), false)
	}
	installDir := filepath.Dir(a.Relaunch)
	if a.Kind == string(KindPortable) {
		installDir = a.Target
	}
	if err := h.waitServers(installDir, serverWait); err != nil {
		log.Warn("el servidor local no terminó", "error", err)
		return fail("El servidor local de dConnector no se cerró a tiempo: "+err.Error(), true)
	}

	// Última comprobación del hash: lo que se ejecuta o extrae es lo que se verificó.
	sum, err := SHA256File(a.File)
	if err != nil {
		return fail("No se pudo leer el instalador descargado: "+err.Error(), true)
	}
	if !strings.EqualFold(sum, a.SHA256) {
		_ = os.Remove(a.File)
		return fail("El instalador descargado ya no coincide con su suma de verificación (SHA-256); se borró y no se instaló nada.", true)
	}

	switch a.Kind {
	case string(KindMSI):
		msiLog := filepath.Join(logDir, "update-"+stamp+"-msi.log")
		code, err := h.runMSI(a.File, msiLog)
		log.Info("msiexec terminó", "codigo", code, "error", fmt.Sprint(err), "registro", msiLog)
		if err != nil {
			return fail("No se pudo ejecutar el instalador de Windows: "+err.Error()+blockHint(err), true)
		}
		if code != 0 && code != 3010 && code != 1641 {
			return fail(msiCodeText(code), true)
		}
	case string(KindPortable):
		if err := ReplacePortable(a.File, a.Target); err != nil {
			return fail("No se pudo actualizar la carpeta portable (se dejó como estaba): "+err.Error(), true)
		}
		log.Info("portable actualizado", "destino", a.Target)
	}

	if err := h.relaunch(a.Relaunch, relaunchArgs(a)); err != nil {
		log.Error("no se pudo volver a abrir", "error", err)
		h.message(msgTitle, "dConnector se actualizó, pero no se pudo volver a abrir: "+err.Error()+blockHint(err)+"\n\nÁbrelo desde el menú Inicio o desde:\n"+a.Relaunch+"\n\nRegistro:\n"+logPath)
		h.openFolder(filepath.Dir(a.Relaunch))
		return ExitFailed
	}
	log.Info("actualización terminada")
	return ExitOK
}

func relaunchArgs(a ApplyArgs) []string {
	if a.Minimized {
		return []string{"--minimized"}
	}
	return nil
}

// blockHint añade la causa probable cuando Windows parece haber bloqueado el programa.
func blockHint(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "operation did not complete successfully because the file contains a virus") ||
		strings.Contains(s, "acceso denegado") || strings.Contains(s, "access is denied") ||
		strings.Contains(s, "blocked") || strings.Contains(s, "bloque") || strings.Contains(s, "virus") ||
		strings.Contains(s, "application control") || strings.Contains(s, "smart app") {
		return "\n\nCausa probable: Windows (Smart App Control, SmartScreen) o tu antivirus bloqueó el programa porque no está firmado. Ábrelo manualmente y elige «Más información» > «Ejecutar de todas formas»."
	}
	return "\n\nSi Windows muestra un aviso de seguridad (Smart App Control, SmartScreen) o tu antivirus lo bloqueó, es porque los ejecutables no están firmados."
}

func msiCodeText(code int) string {
	switch code {
	case 1602:
		return "La instalación se canceló."
	case 1618:
		return "Ya hay otra instalación de Windows en curso. Espera a que termine y vuelve a actualizar."
	case 1638:
		return "Ya hay instalada una versión igual o más nueva de dConnector."
	case 1625:
		return "Una directiva de Windows impide instalar este paquete."
	}
	return fmt.Sprintf("El instalador de Windows terminó con el código %d. Si Windows mostró un aviso de seguridad (Smart App Control, SmartScreen) o tu antivirus lo bloqueó, es porque los instaladores no están firmados.", code)
}

var errTimeout = errors.New("tiempo de espera agotado")

// pruneHelperTemp borra carpetas de ayudantes antiguos de %TEMP%.
func pruneHelperTemp(maxAge time.Duration) {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), helperDirPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			_ = os.RemoveAll(filepath.Join(os.TempDir(), e.Name()))
		}
	}
}

const helperDirPrefix = "dConnector-update-"

// defaultStartHelper copia el ejecutable a una carpeta temporal (para no bloquear los archivos que se
// reemplazan) y lo lanza ahí con los argumentos del ayudante, sin esperar.
func defaultStartHelper(exe string, args []string) error {
	dir, err := os.MkdirTemp("", helperDirPrefix)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, "dConnector-update.exe")
	if err := copyFile(exe, dst); err != nil {
		_ = os.RemoveAll(dir)
		return fmt.Errorf("no se pudo copiar el ejecutable a una carpeta temporal: %w", err)
	}
	return startDetached(dst, args)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
