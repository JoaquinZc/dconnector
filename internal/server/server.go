// Package server arranca y apaga dconnector-server: núcleo, canales, API HTTP(S), seguridad y TLS
// (docs/14-API-SERVIDOR.md §8). El orden de arranque y de apagado es el del escritorio (cmd/dconnector).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"dconnector/internal/adapter/api"
	"dconnector/internal/adapter/api/auth"
	"dconnector/internal/adapter/api/tlsconf"
	"dconnector/internal/adapter/store/sqlite"
	dcapp "dconnector/internal/app"
	"dconnector/internal/buildinfo"
	"dconnector/internal/config"
	"dconnector/internal/core/service"
	"dconnector/internal/logging"
	"dconnector/internal/paths"
)

// Estados de /health.
const (
	stateStarting int32 = iota
	stateOK
	stateStopping
)

// ShutdownTimeout es el máximo que tarda el apagado ordenado (docs/14 §8).
const ShutdownTimeout = 10 * time.Second

// Hooks son avisos opcionales del arranque (los usa el servicio de Windows y las pruebas).
type Hooks struct {
	// Listening se llama cuando el servidor ya acepta conexiones (addr es la dirección real, con el puerto efímero resuelto).
	Listening func(addr string)
}

// Listening es la línea JSON que se escribe en la salida estándar al estar listo (docs/14 §8).
type listeningLine struct {
	Event   string `json:"event"`
	Addr    string `json:"addr"`
	TLS     bool   `json:"tls"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

// Run arranca el servidor y bloquea hasta que ctx se cancele (o falle); entonces apaga en orden y devuelve nil.
func Run(ctx context.Context, cfg config.Server, stdout, stderr io.Writer) error {
	return RunHooks(ctx, cfg, stdout, stderr, Hooks{})
}

// RunHooks es Run con avisos.
func RunHooks(ctx context.Context, cfg config.Server, stdout, stderr io.Writer, hooks Hooks) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if cfg.Listen == "" {
		cfg.Listen = config.DefaultListen
	}
	tlsOpts := tlsconf.Options{Cert: cfg.TLSCert, Key: cfg.TLSKey, NoTLS: cfg.NoTLS, Insecure: cfg.Insecure}

	// 1-2. Carpeta de datos y registro.
	if cfg.Data != "" {
		paths.SetOverride(cfg.Data)
	}
	// Un solo servidor por carpeta de datos: el candado se toma ANTES de que Init vacíe tmp/ (un segundo
	// servidor no debe borrar los temporales del primero) y se suelta al final de todo.
	dataDir, _, err := paths.Resolve()
	if err != nil {
		return err
	}
	unlockData, err := lockDataDir(dataDir)
	if err != nil {
		return err
	}
	defer unlockData()
	if err := paths.Init(); err != nil {
		return err
	}
	tlsOpts.DataDir = paths.DataDir()
	closeLog, err := logging.Init(paths.LogsDir())
	if err != nil {
		return err
	}
	defer func() { _ = closeLog() }()
	// El registro va al archivo del día y, además, a la salida de errores (docker, systemd, consola).
	prevLog := slog.Default()
	slog.SetDefault(slog.New(fanout{prevLog.Handler(), logging.New(stderr).Handler()}))
	defer slog.SetDefault(prevLog) // varias ejecuciones en un mismo proceso (pruebas) no anidan manejadores
	if cfg.Verbose {
		logging.SetVerbose(true)
	}
	slog.Info("dconnector-server arranca", "version", buildinfo.Version, "data", paths.DataDir(), "listen", cfg.Listen)

	// La dirección de escucha se valida antes de tocar nada más: fuera de loopback exige TLS.
	if err := tlsconf.CheckListen(cfg.Listen, tlsOpts); err != nil {
		return err
	}

	// 3. Almacén común (y un segundo manejador de app.db para la seguridad: WAL admite varios).
	appStore, err := sqlite.OpenApp(paths.AppDBPath())
	if err != nil {
		return fmt.Errorf("no se pudo abrir la base de datos: %w", err)
	}
	defer appStore.Close()
	authDB, err := sqlite.Open(paths.AppDBPath())
	if err != nil {
		return fmt.Errorf("no se pudo abrir la base de datos de usuarios: %w", err)
	}
	defer authDB.Close()

	// 4-6. Núcleo.
	core := dcapp.Build(appStore, dcapp.OpenAccountStore)
	// Build deja una goroutina de descarga automática suscrita al bus: si el arranque falla más adelante (puerto
	// ocupado, TLS), debe cerrarse igual. Close es idempotente; el apagado normal la cierra en su orden.
	defer core.Messaging.Close()
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	if err := core.Settings.EnsureDefaults(rootCtx); err != nil {
		return fmt.Errorf("ajustes iniciales: %w", err)
	}
	if cur, err := core.Settings.All(rootCtx); err == nil && !cfg.Verbose {
		logging.SetVerbose(cur.VerboseLog)
	}

	// Seguridad: usuarios y primer arranque. El modo local (token del escritorio) no crea usuarios.
	authSvc, err := auth.NewService(auth.Options{DB: authDB, Log: slog.Default(), LocalToken: cfg.LocalToken, TLS: !cfg.NoTLS,
		ListenNonLoopback: !tlsconf.IsLoopback(cfg.Listen)})
	if err != nil {
		return fmt.Errorf("seguridad: %w", err)
	}
	defer authSvc.Close()
	if cfg.LocalToken == "" {
		gen, created, err := authSvc.EnsureAdmin(ctx, cfg.AdminUser)
		if err != nil {
			return fmt.Errorf("administrador inicial: %w", err)
		}
		if created {
			slog.Info("administrador creado", "user", cfg.AdminUser)
			if gen != "" {
				// La clave generada sale UNA vez por la salida estándar y nunca por el registro.
				fmt.Fprintf(stdout, "Administrador creado. Usuario: %s  Clave: %s\nGuarde esta clave ahora: no se volverá a mostrar.\n", cfg.AdminUser, gen)
			}
		}
	}

	// TLS.
	tlsCfg, fingerprint, err := tlsconf.Load(tlsOpts)
	if err != nil {
		return fmt.Errorf("TLS: %w", err)
	}
	useTLS := tlsCfg != nil
	if useTLS {
		slog.Info("huella TLS", "sha256", fingerprint)
		fmt.Fprintf(stdout, "Huella TLS (SHA-256): %s\n", fingerprint)
	}

	// API.
	var state atomic.Int32 // stateStarting
	apiServer, err := api.New(api.Config{
		Core: core, Auth: authSvc, AuthRoutes: authSvc.Handler(), AllowOrigins: cfg.Origins(),
		Version: buildinfo.Version, TLS: useTLS,
		Ready: func() string {
			switch state.Load() {
			case stateOK:
				return "ok"
			case stateStopping:
				return "stopping"
			}
			return "starting"
		},
	})
	if err != nil {
		return fmt.Errorf("API: %w", err)
	}
	defer apiServer.Close()

	// Avisos: el servicio del núcleo decide y la API los reparte como eventos `notify`.
	notifySvc := service.NewNotifications(core.Bus, core.Settings, core.Inbox, apiServer.Notifier())
	apiServer.SetNotifications(notifySvc) // foco por conexión y eventos notify:decision

	// HTTP(S).
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("no se pudo escuchar en %s: %w", cfg.Listen, err)
	}
	httpSrv := &http.Server{
		Handler:           apiServer.Handler(),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    64 << 10,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}
	serveErr := make(chan error, 1)
	go func() {
		var e error
		if useTLS {
			e = httpSrv.ServeTLS(ln, "", "")
		} else {
			e = httpSrv.Serve(ln)
		}
		if !errors.Is(e, http.ErrServerClosed) {
			serveErr <- e
		}
		close(serveErr)
	}()
	watchReload(rootCtx)

	addr := ln.Addr().String()
	if cfg.Insecure && !tlsconf.IsLoopback(cfg.Listen) {
		go insecureNag(rootCtx, addr)
	}
	line, _ := json.Marshal(listeningLine{Event: "listening", Addr: addr, TLS: useTLS, Version: buildinfo.Version, PID: os.Getpid()})
	fmt.Fprintf(stdout, "%s\n", line)
	slog.Info("escuchando", "addr", addr, "tls", useTLS)
	if hooks.Listening != nil {
		hooks.Listening(addr)
	}

	// 9. Arranque de lo demás, en el orden del escritorio.
	startDone := make(chan struct{})
	go func() {
		defer close(startDone)
		if notifySvc != nil {
			notifySvc.Start(rootCtx)
		}
		if cfg.NoAccounts {
			// Solo se sirven los datos existentes: no se conecta ninguna cuenta ni arranca ningún servicio de fondo.
			slog.Warn("modo --no-accounts: no se inician cuentas, historial, programados, campañas, estados ni protocolo")
			if err := core.Accounts.LoadAll(rootCtx); err != nil { // abre sus bases para poder leer los datos, sin conectarlas
				slog.Warn("no se pudieron abrir las cuentas", "error", err)
			}
			if rootCtx.Err() == nil {
				state.Store(stateOK)
				slog.Info("servidor listo")
			}
			return
		}
		if err := core.Accounts.StartAll(rootCtx); err != nil {
			slog.Warn("no se pudieron arrancar las cuentas", "error", err)
		}
		if n, err := core.Contacts.SyncAll(rootCtx); err != nil {
			slog.Warn("no se pudieron sincronizar los contactos", "error", err)
		} else if n > 0 {
			slog.Info("contactos agregados automáticamente", "n", n)
		}
		core.History.AutoRun(rootCtx)
		core.Scheduler.Start(rootCtx)
		core.Campaigns.Start(rootCtx)
		core.Statuses.Start(rootCtx)
		core.Protocol.Start(rootCtx)
		if rootCtx.Err() == nil {
			state.Store(stateOK)
			slog.Info("servidor listo")
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-serveErr:
		if runErr != nil {
			runErr = fmt.Errorf("el servidor HTTP se detuvo: %w", runErr)
		}
	}

	// 10. Apagado ordenado, máximo 10 s en total: se deja de aceptar, se cierran los WebSocket (1001),
	// y se apaga el núcleo y las cuentas en el orden del escritorio.
	slog.Info("apagando")
	state.Store(stateStopping)
	cancelRoot()
	sctx, scancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer scancel()
	apiServer.Close()
	if err := httpSrv.Shutdown(sctx); err != nil {
		_ = httpSrv.Close()
	}
	select { // espera a que termine el arranque, si seguía en curso
	case <-startDone:
	case <-sctx.Done():
	}
	if notifySvc != nil {
		notifySvc.Stop()
	}
	core.Scheduler.Stop()
	core.Protocol.Stop()
	core.History.StopAll()
	core.Statuses.Stop()
	core.Campaigns.Stop()
	_ = core.Accounts.StopAll(sctx)
	core.Messaging.Close()
	slog.Info("apagado completo")
	return runErr
}

// insecureNag avisa cada minuto de que se escucha fuera de loopback sin protección (--insecure).
func insecureNag(ctx context.Context, addr string) {
	warn := func() {
		slog.Warn("AVISO: el servidor escucha en una interfaz no local SIN TLS (--insecure). Cualquiera en la red puede leer el tráfico, incluidas las claves. Use TLS o un proxy inverso de confianza.", "addr", addr)
	}
	warn()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			warn()
		}
	}
}

// fanout reparte cada registro a varios manejadores de slog.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var first error
	for _, h := range f {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (f fanout) WithAttrs(a []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(a)
	}
	return out
}

func (f fanout) WithGroup(n string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(n)
	}
	return out
}
