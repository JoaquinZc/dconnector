//go:build windows

package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"dconnector/internal/config"
)

// IsWindowsService dice si el proceso lo lanzó el Administrador de servicios (SCM).
func IsWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// RunAsService ejecuta el servidor bajo el SCM hasta que lo paren. Los parámetros (--data, --listen, TLS…)
// vienen de la línea de comandos guardada al instalar.
func RunAsService(cfg config.Server) error {
	var elog *eventlog.Log
	if l, err := eventlog.Open(ServiceName); err == nil {
		elog = l
		defer elog.Close()
	}
	return svc.Run(ServiceName, &handler{cfg: cfg, elog: elog})
}

type handler struct {
	cfg  config.Server
	elog *eventlog.Log
}

func (h *handler) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending, WaitHint: 60000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		done <- RunHooks(ctx, h.cfg, io.Discard, io.Discard, Hooks{Listening: func(string) { once.Do(func() { close(ready) }) }})
	}()
	select {
	case <-ready:
	case err := <-done:
		h.log(false, "El servidor no pudo arrancar: %v", err)
		return false, 1
	case <-time.After(60 * time.Second):
		h.log(false, "El servidor no arrancó en 60 s")
		return false, 1
	}
	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	h.log(true, "dConnector Servidor en marcha")
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending, WaitHint: 20000}
				cancel()
				select {
				case err := <-done:
					if err != nil {
						h.log(false, "Apagado con error: %v", err)
					}
				case <-time.After(ShutdownTimeout + 5*time.Second):
					h.log(false, "El apagado tardó más de lo permitido")
				}
				return false, 0
			}
		case err := <-done:
			if err != nil {
				h.log(false, "El servidor se detuvo: %v", err)
				return false, 1
			}
			return false, 0
		}
	}
}

func (h *handler) log(info bool, format string, a ...any) {
	if h.elog == nil {
		return
	}
	if info {
		_ = h.elog.Info(1, fmt.Sprintf(format, a...))
	} else {
		_ = h.elog.Error(1, fmt.Sprintf(format, a...))
	}
}

// ControlService hace install, uninstall, start o stop del servicio. Pide permisos de administrador.
func ControlService(action string, cfg config.Server, w io.Writer) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("no se pudo conectar con el administrador de servicios (¿ejecuta como administrador?): %w", err)
	}
	defer m.Disconnect()
	switch action {
	case "install":
		return installService(m, cfg, w)
	case "uninstall":
		return uninstallService(m, w)
	case "start":
		s, err := m.OpenService(ServiceName)
		if err != nil {
			return fmt.Errorf("el servicio %s no está instalado: %w", ServiceName, err)
		}
		defer s.Close()
		if err := s.Start(); err != nil {
			return fmt.Errorf("no se pudo iniciar el servicio: %w", err)
		}
		fmt.Fprintln(w, "Servicio iniciado.")
		return nil
	case "stop":
		s, err := m.OpenService(ServiceName)
		if err != nil {
			return fmt.Errorf("el servicio %s no está instalado: %w", ServiceName, err)
		}
		defer s.Close()
		if err := stopAndWait(s, 30*time.Second); err != nil {
			return err
		}
		fmt.Fprintln(w, "Servicio detenido.")
		return nil
	}
	return fmt.Errorf("acción de servicio desconocida %q", action)
}

func installService(m *mgr.Mgr, cfg config.Server, w io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if s, err := m.OpenService(ServiceName); err == nil {
		s.Close()
		return fmt.Errorf("el servicio %s ya está instalado; desinstálelo primero (--service uninstall)", ServiceName)
	}
	args := ServiceInstallArgs(cfg, os.Getenv("ProgramData"))
	s, err := m.CreateService(ServiceName, exe, mgr.Config{
		DisplayName: ServiceDisplayName, Description: ServiceDescription,
		StartType: mgr.StartAutomatic, DelayedAutoStart: true,
	}, args...)
	if err != nil {
		return fmt.Errorf("no se pudo instalar el servicio: %w", err)
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 86400)
	if err := eventlog.InstallAsEventCreate(ServiceName, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		fmt.Fprintln(w, "Aviso: no se pudo registrar el origen del registro de eventos:", err)
	}
	fmt.Fprintf(w, "Servicio %s instalado (inicio automático). Parámetros guardados: %v\n", ServiceName, args)
	fmt.Fprintln(w, "Antes de iniciarlo cree el administrador con la misma carpeta de datos, p. ej.:")
	fmt.Fprintln(w, "  dconnector-server --data <carpeta> user add admin")
	fmt.Fprintln(w, "Luego: dconnector-server --service start")
	return nil
}

func uninstallService(m *mgr.Mgr, w io.Writer) error {
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("el servicio %s no está instalado: %w", ServiceName, err)
	}
	defer s.Close()
	_ = stopAndWait(s, 30*time.Second) // si no estaba en marcha, no importa
	if err := s.Delete(); err != nil {
		return fmt.Errorf("no se pudo desinstalar el servicio: %w", err)
	}
	_ = eventlog.Remove(ServiceName)
	fmt.Fprintln(w, "Servicio desinstalado. Los datos no se tocaron.")
	return nil
}

func stopAndWait(s *mgr.Service, within time.Duration) error {
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Stopped {
		return nil
	}
	if st, err = s.Control(svc.Stop); err != nil {
		return fmt.Errorf("no se pudo detener el servicio: %w", err)
	}
	deadline := time.Now().Add(within)
	for st.State != svc.Stopped {
		if time.Now().After(deadline) {
			return fmt.Errorf("el servicio no se detuvo en %s", within)
		}
		time.Sleep(300 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			return err
		}
	}
	return nil
}
