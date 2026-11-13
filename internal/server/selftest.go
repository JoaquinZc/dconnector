package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"dconnector/internal/adapter/api/auth"
	"dconnector/internal/adapter/api/tlsconf"
	"dconnector/internal/adapter/store/sqlite"
	dcapp "dconnector/internal/app"
	"dconnector/internal/config"
)

// Selftest es la comprobación sin red externa de --selftest: la del núcleo (dcapp.RunSelftest) más lo propio
// del servidor, todo sobre carpetas temporales: abrir app.db, crear el servicio de seguridad, cargar el TLS propio
// y levantar el servidor completo en 127.0.0.1:0 para hacer un GET /api/v1/health.
func Selftest(w io.Writer, cfg config.Server) error {
	if err := dcapp.RunSelftest(dropOK{w}); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "dconnector-selftest-*")
	if err != nil {
		return fmt.Errorf("carpeta temporal: %w", err)
	}
	defer os.RemoveAll(tmp)

	// Seguridad sobre una app.db temporal.
	authPath := filepath.Join(tmp, "auth.db")
	mig, err := sqlite.OpenApp(authPath) // aplica las migraciones (tabla users)
	if err != nil {
		return fmt.Errorf("abrir la base de usuarios: %w", err)
	}
	_ = mig.Close()
	db, err := sqlite.Open(authPath)
	if err != nil {
		return fmt.Errorf("abrir la base de usuarios: %w", err)
	}
	svc, err := auth.NewService(auth.Options{DB: db, TLS: true})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("servicio de seguridad: %w", err)
	}
	svc.Close()
	_ = db.Close()
	fmt.Fprintln(w, "seguridad: servicio creado")

	// TLS propio sobre una carpeta temporal.
	_, fp, err := tlsconf.Load(tlsconf.Options{DataDir: filepath.Join(tmp, "tlsdata")})
	if err != nil {
		return fmt.Errorf("TLS propio: %w", err)
	}
	if fp == "" {
		return fmt.Errorf("TLS propio: no devolvió la huella del certificado")
	}
	fmt.Fprintln(w, "tls: certificado propio generado, huella", fp)

	// Servidor completo en loopback, sin TLS y con token local (modo local: no crea usuarios).
	var tok [16]byte
	if _, err := rand.Read(tok[:]); err != nil {
		return err
	}
	sc := config.Server{
		Listen: "127.0.0.1:0", Data: filepath.Join(tmp, "srv"), NoTLS: true, AdminUser: "admin",
		LocalToken: hex.EncodeToString(tok[:]),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunHooks(ctx, sc, io.Discard, io.Discard, Hooks{Listening: func(a string) { addrCh <- a }})
	}()
	var addr string
	select {
	case addr = <-addrCh:
	case err := <-done:
		return fmt.Errorf("el servidor no arrancó: %w", err)
	case <-time.After(30 * time.Second):
		return fmt.Errorf("el servidor no arrancó en 30 s")
	}
	status, err := waitHealth("http://"+addr+"/api/v1/health", "ok", 15*time.Second)
	cancel()
	if rerr := <-done; rerr != nil && err == nil {
		err = fmt.Errorf("apagado del servidor: %w", rerr)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "servidor: /api/v1/health =", status, "y apagado ordenado")
	fmt.Fprintln(w, "OK")
	return nil
}

// waitHealth consulta la URL hasta que "status" sea want.
func waitHealth(url, want string, within time.Duration) (string, error) {
	deadline := time.Now().Add(within)
	cl := &http.Client{Timeout: 3 * time.Second}
	last := ""
	for {
		resp, err := cl.Get(url)
		if err == nil {
			var body struct {
				Status string `json:"status"`
			}
			derr := json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if derr == nil {
				last = body.Status
				if body.Status == want && resp.StatusCode == http.StatusOK {
					return last, nil
				}
			}
		} else {
			last = err.Error()
		}
		if time.Now().After(deadline) {
			return last, fmt.Errorf("/api/v1/health no llegó a %q (último resultado: %s)", want, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// dropOK descarta la línea «OK» final de dcapp.RunSelftest: el OK del servidor es el último.
type dropOK struct{ w io.Writer }

func (d dropOK) Write(p []byte) (int, error) {
	if string(p) == "OK\n" {
		return len(p), nil
	}
	return d.w.Write(p)
}
