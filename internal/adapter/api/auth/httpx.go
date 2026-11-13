package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WriteError escribe un error en el formato de docs/14 §2. Reutilizable por el paquete api.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// WriteRateLimited escribe 429 con Retry-After (segundos, redondeado hacia arriba).
func WriteRateLimited(w http.ResponseWriter, wait time.Duration, message string) {
	secs := int((wait + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	WriteError(w, http.StatusTooManyRequests, "rate_limited", message)
}

// WriteJSON escribe un cuerpo JSON con el código dado.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ClientIP devuelve la IP del extremo de la conexión (RemoteAddr). No se confía en X-Forwarded-For:
// detrás de un proxy inverso todos los clientes compartirían el límite de la IP del proxy, pero nadie
// puede falsear su IP para esquivar el bloqueo.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

// RequestLabel devuelve «MÉTODO ruta» sin cadena de consulta, para registros de acceso: nunca debe
// registrarse r.URL.RawQuery (puede llevar ?token= o ?ticket=).
func RequestLabel(r *http.Request) string {
	return r.Method + " " + r.URL.Path
}

// RedactURL devuelve la URL sin consulta ni fragmento (para mensajes de error y registros).
func RedactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Path
}

// SecurityHeaders pone las cabeceras de docs/14 §8 en toda respuesta: nosniff, Referrer-Policy,
// Cache-Control: no-store (los manejadores de medios pueden sustituirlo después) y, con TLS, HSTS.
func SecurityHeaders(tls bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if tls {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// SandboxHeaders añade «Content-Security-Policy: sandbox» (medios y archivos, docs/14 §8).
func SandboxHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "sandbox")
		next.ServeHTTP(w, r)
	})
}

func isLoopbackConn(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
