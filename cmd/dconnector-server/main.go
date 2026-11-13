// dconnector-server: servidor sin ventana de dConnector (núcleo + canales + API). docs/14-API-SERVIDOR.md.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"dconnector/internal/adapter/api/auth"
	"dconnector/internal/adapter/store/sqlite"
	"dconnector/internal/buildinfo"
	"dconnector/internal/config"
	"dconnector/internal/paths"
	"dconnector/internal/server"
)

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)) }

// run es main con las dependencias a la vista; devuelve el código de salida.
func run(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := config.ParseServer(args, getenv, stderr)
	if err != nil {
		return 2
	}
	if cfg.Data != "" {
		paths.SetOverride(cfg.Data)
	}

	// Subcomandos: `user …` (administración de usuarios) y `healthcheck` (HEALTHCHECK de Docker).
	if len(cfg.Args) > 0 {
		switch cfg.Args[0] {
		case "user":
			return runUser(cfg.Args[1:], stdin, stdout, stderr)
		case "healthcheck":
			if err := server.Healthcheck(cfg); err != nil {
				fmt.Fprintln(stderr, "ERROR:", err)
				return 1
			}
			return 0
		default:
			fmt.Fprintf(stderr, "dconnector-server: subcomando desconocido %q (use user o healthcheck)\n", cfg.Args[0])
			return 2
		}
	}

	if cfg.Version || cfg.Selftest {
		w, closeOut := outputWriter(cfg.Out, stdout, stderr)
		defer closeOut()
		if cfg.Version {
			fmt.Fprintln(w, buildinfo.Version)
			return 0
		}
		if err := server.Selftest(w, cfg); err != nil {
			fmt.Fprintln(w, "ERROR:", err)
			return 1
		}
		return 0
	}

	if cfg.Service != "" {
		if err := server.ControlService(cfg.Service, cfg, stdout); err != nil {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
		return 0
	}
	if server.IsWindowsService() {
		if err := server.RunAsService(cfg); err != nil {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, cfg, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "dconnector-server:", err)
		return 1
	}
	return 0
}

// runUser abre app.db (con sus migraciones) y delega en la CLI de usuarios del agente B.
func runUser(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if err := paths.Init(); err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	st, err := sqlite.OpenApp(paths.AppDBPath()) // aplica las migraciones aditivas
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: no se pudo abrir la base de datos:", err)
		return 1
	}
	defer st.Close()
	db, err := sqlite.Open(paths.AppDBPath())
	if err != nil {
		fmt.Fprintln(stderr, "ERROR: no se pudo abrir la base de datos:", err)
		return 1
	}
	defer db.Close()
	return auth.RunUserCLI(args, db, stdin, stdout, stderr)
}

func outputWriter(path string, stdout, stderr io.Writer) (io.Writer, func()) {
	if path == "" {
		return stdout, func() {}
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(stderr, "no se pudo abrir", path, ":", err)
		return stdout, func() {}
	}
	return f, func() { _ = f.Close() }
}
