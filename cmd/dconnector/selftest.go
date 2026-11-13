package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dconnector/internal/adapter/desktop/localserver"
	"dconnector/internal/buildinfo"
	"dconnector/internal/paths"
)

// runSelftest es la comprobación sin ventana de los paquetes (portable y MSI): 1) la carpeta de datos se
// resuelve y se puede escribir; 2) se localiza dConnector-server; 3) si existe, se prueba con --selftest y
// se arranca de verdad en una carpeta temporal (escucha en loopback, responde /health con el token propio y
// se apaga). Si el servidor no existe se dice con claridad: en desarrollo es un AVISO (código 0); con
// --require-server (paquetes) es un fallo.
func runSelftest(w io.Writer, serverExe string, requireServer bool) error {
	fmt.Fprintln(w, "dConnector", buildinfo.Version)
	if err := paths.InitKeepTmp(); err != nil { // sin vaciar tmp/: puede haber un servidor vivo usándola
		return fmt.Errorf("carpeta de datos: %w", err)
	}
	fmt.Fprintln(w, "datos:", paths.DataDir(), "portable:", paths.IsPortable())
	probe := filepath.Join(paths.TmpDir(), "selftest-"+fmt.Sprint(os.Getpid()))
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("la carpeta de datos no se puede escribir: %w", err)
	}
	_ = os.Remove(probe)
	fmt.Fprintln(w, "carpeta de datos: escribible")

	exe, err := localserver.FindServerExe(serverExe)
	if err != nil {
		if requireServer {
			return fmt.Errorf("servidor: %w", err)
		}
		fmt.Fprintln(w, "AVISO: no se encontró", localserver.ServerExeName(), "junto a la aplicación (normal en desarrollo; use --server-exe o DCONNECTOR_SERVER_EXE).")
		fmt.Fprintln(w, "OK (sin servidor)")
		return nil
	}
	fmt.Fprintln(w, "servidor:", exe)

	// Todo lo del servidor va en una carpeta temporal: nunca se toca la de datos reales.
	tmp, err := os.MkdirTemp("", "dconnector-selftest-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--selftest", "--data", tmp)
	if o, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("el servidor falló su --selftest: %w: %s", err, tail(string(o)))
	}
	fmt.Fprintln(w, "servidor --selftest: OK")

	l := localserver.New(localserver.Options{ExePath: exe, DataDir: tmp, StartTimeout: 30 * time.Second, StopTimeout: 10 * time.Second})
	if err := l.Start(); err != nil {
		return err
	}
	defer l.Stop()
	if err := waitServer(ctx, l); err != nil {
		return errors.New("el servidor no arrancó: " + err.Error())
	}
	fmt.Fprintln(w, "servidor local: escucha en", l.BaseURL(), "versión", l.Version(), "(health y token propio OK)")
	l.Stop()
	fmt.Fprintln(w, "OK")
	return nil
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		s = "…" + s[len(s)-400:]
	}
	return s
}

// waitServer espera a que el servidor local esté listo; falla en cuanto el primer arranque se cae (no
// espera a los reintentos) o a los 40 s.
func waitServer(ctx context.Context, l *localserver.Launcher) error {
	deadline := time.Now().Add(40 * time.Second)
	for {
		st := l.Status()
		switch {
		case st.Phase == localserver.PhaseReady:
			return nil
		case st.Phase == localserver.PhaseRestarting && st.Err != "":
			return errors.New(st.Err)
		case time.Now().After(deadline):
			return errors.New("se agotó la espera")
		case ctx.Err() != nil:
			return ctx.Err()
		}
		time.Sleep(100 * time.Millisecond)
	}
}
