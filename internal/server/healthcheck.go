package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"dconnector/internal/config"
)

// Healthcheck consulta GET /api/v1/health del propio servidor (HEALTHCHECK de Docker). Prueba HTTPS y luego
// HTTP en la dirección de --listen (si escucha en todas las interfaces, usa loopback). Devuelve error si no
// responde 200 con status "ok". El certificado no se verifica: solo se consulta a uno mismo.
func Healthcheck(cfg config.Server) error {
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return fmt.Errorf("dirección de escucha no válida %q: %w", cfg.Listen, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	base := net.JoinHostPort(host, port)
	cl := &http.Client{
		Timeout:   4 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // solo consulta a sí mismo
	}
	schemes := []string{"https", "http"}
	if cfg.NoTLS {
		schemes = []string{"http"}
	}
	var last error
	for _, sc := range schemes {
		resp, err := cl.Get(sc + "://" + base + "/api/v1/health")
		if err != nil {
			last = err
			continue
		}
		var body struct {
			Status string `json:"status"`
		}
		derr := json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if derr == nil && resp.StatusCode == http.StatusOK && body.Status == "ok" {
			return nil
		}
		last = fmt.Errorf("el servidor respondió %d (estado %q)", resp.StatusCode, body.Status)
	}
	return fmt.Errorf("el servidor no está sano: %w", last)
}
