// dConnector: bandeja unificada de WhatsApp, Telegram, Messenger e Instagram (docs/02-ARQUITECTURA §4).
// Desde 0.3.0 esta aplicación es un cliente: lanza dConnector-server.exe (modo local) o conecta con uno
// remoto, y la interfaz habla directo con el servidor por HTTP/WebSocket (docs/14-API-SERVIDOR.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"dconnector/frontend"
	"dconnector/internal/adapter/desktop/bridge"
	"dconnector/internal/adapter/desktop/connection"
	"dconnector/internal/adapter/desktop/localserver"
	"dconnector/internal/adapter/desktop/secret"
	"dconnector/internal/adapter/desktop/winnotify"
	uiwails "dconnector/internal/adapter/ui/wails"
	"dconnector/internal/buildinfo"
	"dconnector/internal/core/model"
	"dconnector/internal/logging"
	"dconnector/internal/paths"
)

// evtConnection avisa a la interfaz de que cambió el estado del servidor (arranque, caída, reintento) o el
// modo de conexión: debe volver a pedir DesktopService.ConnectionInfo.
const evtConnection = "desktop:connection"

func main() {
	var (
		showVersion   = flag.Bool("version", false, "imprime la versión y sale")
		minimized     = flag.Bool("minimized", false, "arranca oculto en la bandeja")
		selftest      = flag.Bool("selftest", false, "comprobación sin ventana: carpeta de datos y arranque del servidor local")
		requireServer = flag.Bool("require-server", false, "con --selftest, falla si no se encuentra dConnector-server (paquetes de instalación)")
		serverExe     = flag.String("server-exe", "", "ruta de dConnector-server (desarrollo; también DCONNECTOR_SERVER_EXE)")
		out           = flag.String("out", "", "archivo donde escribir la salida de --version y --selftest")
	)
	flag.Parse()

	if *showVersion || *selftest {
		attachConsole()
		w, closeOut := outputWriter(*out)
		if *showVersion {
			fmt.Fprintln(w, buildinfo.Version)
			closeOut()
			return
		}
		err := runSelftest(w, *serverExe, *requireServer)
		if err != nil {
			fmt.Fprintln(w, "ERROR:", err)
		}
		closeOut()
		if err != nil {
			os.Exit(1)
		}
		return
	}

	if err := run(*minimized, *serverExe); err != nil {
		fmt.Fprintln(os.Stderr, "dConnector:", err)
		os.Exit(1)
	}
}

// outputWriter devuelve el destino de la salida: el archivo de --out o la consola.
func outputWriter(path string) (io.Writer, func()) {
	if path == "" {
		return os.Stdout, func() {}
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no se pudo abrir", path, ":", err)
		return os.Stdout, func() {}
	}
	return f, func() { _ = f.Close() }
}

// newLauncherFactory arma el lanzador del servidor local sobre la carpeta de datos actual.
func newLauncherFactory(serverExe string) func(keep func() bool, onStatus func(localserver.Status)) connection.LocalServer {
	return func(keep func() bool, onStatus func(localserver.Status)) connection.LocalServer {
		return localserver.New(localserver.Options{
			ExePath:     serverExe,
			DataDir:     paths.DataDir(),
			KeepRunning: keep,
			OnStatus:    onStatus,
		})
	}
}

func run(startMinimized bool, serverExe string) error {
	// 1-2. Carpeta de datos y registro (el del escritorio va aparte del del servidor: dos procesos no
	// comparten archivo de registro).
	// No se vacía tmp/ al abrir: con «seguir en segundo plano» puede haber un servidor vivo con subidas en curso.
	// Los temporales del propio escritorio se podan por antigüedad.
	if err := paths.InitKeepTmp(); err != nil {
		return err
	}
	pruneTmp(paths.TmpDir(), 24*time.Hour)
	closeLog, err := logging.Init(filepath.Join(paths.LogsDir(), "desktop"))
	if err != nil {
		return err
	}
	defer closeLog()
	slog.Info("dConnector arranca", "version", buildinfo.Version, "portable", paths.IsPortable(), "data", paths.DataDir())

	store, err := connection.OpenStore(paths.DataDir())
	if err != nil {
		return err
	}
	exe, _ := os.Executable()

	var (
		app        *application.App
		mainWindow *application.WebviewWindow
		desktop    *uiwails.Desktop
		mgr        *connection.Manager
	)
	emit := func(payload map[string]any) {
		if app != nil {
			app.Event.Emit(evtConnection, payload)
		}
	}
	mgr = connection.New(connection.Options{
		Store:      store,
		Protector:  secret.Default(),
		NewLocal:   newLauncherFactory(serverExe),
		ClientName: "dConnector-desktop/" + buildinfo.Version,
		OnState: func(s connection.State) {
			emit(map[string]any{"mode": s.Mode, "state": string(s.Phase), "error": s.Err, "retryInMs": s.RetryIn.Milliseconds()})
		},
	})

	// Avisos de Windows, contador de la barra de tareas y bandeja: se alimentan de los eventos del servidor.
	notifier := notifications.New()
	winNotifier := winnotify.New("dConnector", notifier)
	br := bridge.New(bridge.Deps{
		Notifier: winNotifier,
		OnUnread: func(n int) {
			if desktop != nil {
				desktop.SetUnread(n)
			}
		},
		OnAccounts: func(l []bridge.AccountLine) {
			if desktop != nil {
				desktop.SetAccounts(l)
			}
		},
		OnConnection: func(connected bool, msg string) {
			if desktop != nil {
				desktop.SetConnection(connected)
			}
			state := "ready"
			if !connected {
				state = "disconnected"
			}
			emit(map[string]any{"mode": mgr.Mode(), "state": state, "error": msg})
		},
		OnSettings: func(s bridge.Settings) {
			// Cerrar a la bandeja y arrancar con Windows son ajustes de ESTA máquina: solo se toman del
			// servidor cuando es el local.
			if mgr.Mode() != connection.ModeLocal {
				return
			}
			if desktop != nil {
				desktop.SetCloseToTray(s.CloseToTray)
			}
			if uiwails.IsAutostart(exe, s.StartMinimized) != s.StartWithWindows {
				if err := uiwails.SetAutostart(s.StartWithWindows, exe, s.StartMinimized); err != nil {
					slog.Warn("arranque con Windows", "error", err)
				}
			}
		},
		OpenChat: func(key string) {
			if desktop != nil {
				desktop.OpenChat(model.ChatKey(key))
			}
		},
	})
	winNotifier.OnActivate(func(key model.ChatKey) { br.OnToastActivate(string(key)) })

	services := []application.Service{
		application.NewService(uiwails.NewDesktopService(mgr)),
		application.NewService(uiwails.NewNotificationsService(winNotifier, br, mgr)),
		application.NewService(&uiwails.SettingsService{}),
		application.NewService(uiwails.NewFilesService(mgr)),
		application.NewService(notifier),
	}

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdown := func() {
		// Apagado ordenado: el servidor local (máximo 10 s) salvo «seguir en segundo plano».
		cancel()
		mgr.Shutdown()
	}

	app = application.New(application.Options{
		Name:        "dConnector",
		Description: "Bandeja unificada de WhatsApp, Telegram, Messenger e Instagram",
		Services:    services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend.Assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.darkmelon.dconnector",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if desktop != nil {
					desktop.ShowWindow()
				}
			},
		},
		OnShutdown: shutdown,
	})

	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "dConnector",
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        600,
		Hidden:           startMinimized,
		BackgroundColour: application.NewRGB(240, 242, 245),
		URL:              "/",
	})

	desktop = uiwails.NewDesktop(app, mainWindow, winNotifier, nil)
	desktop.Setup()
	desktop.OnWindowFocus(br.RefreshSettings)

	// El servidor local arranca ya (mientras carga la ventana), si el modo es local.
	mgr.Start()

	// Al estar la aplicación lista, el seguidor mantiene el flujo de eventos con el servidor vigente.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go follow(rootCtx, mgr, br, emit)
	})

	// Ctrl+C en consola apaga ordenadamente.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		desktop.Quit()
	}()

	return app.Run()
}

// follow mantiene el puente conectado al servidor vigente: espera a que haya uno (el local listo o el remoto
// con sesión), sirve los eventos y, si cambia el destino (otro modo, servidor local reiniciado), vuelve a
// empezar. Si el servidor no está disponible, reintenta cada 5 s sin dejar la interfaz en blanco (la propia
// interfaz muestra «Sin conexión»).
func follow(ctx context.Context, mgr *connection.Manager, br *bridge.Bridge, emit func(map[string]any)) {
	for ctx.Err() == nil {
		changed := mgr.Changed()
		cl, _, err := mgr.Client(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("sin servidor para los avisos del escritorio", "error", err)
			emit(map[string]any{"mode": mgr.Mode(), "state": "unavailable", "error": err.Error()})
			select {
			case <-changed:
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		sctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { br.Serve(sctx, cl); close(done) }()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		stop()
		<-done
	}
}

// pruneTmp borra de tmp/ lo que lleva más de maxAge sin tocarse (descargas ya abiertas, restos de otra sesión).
func pruneTmp(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}
