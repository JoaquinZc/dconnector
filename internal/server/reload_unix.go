//go:build !windows

package server

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"dconnector/internal/adapter/api/tlsconf"
)

// watchReload recarga el certificado TLS externo al recibir SIGHUP (docs/14 §8).
func watchReload(ctx context.Context) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				if err := tlsconf.Reload(); err != nil {
					slog.Error("no se pudo recargar el certificado TLS; se conserva el anterior", "error", err)
				} else {
					slog.Info("certificado TLS recargado")
				}
			}
		}
	}()
}
