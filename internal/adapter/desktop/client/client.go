// Package client es el cliente Go del servidor de dConnector (docs/14-API-SERVIDOR.md) que usa la app de
// escritorio: RPC, login y el flujo de eventos por WebSocket con reconexión. Los tipos de evento son solo los
// que necesita el escritorio (contador de no leídos, estado de cuentas, avisos).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config configura el cliente.
type Config struct {
	// BaseURL: «http://127.0.0.1:51234» o «https://servidor:8443» (sin barra final ni /api/v1).
	BaseURL string
	// Token devuelve el token de sesión (o el token local) vigente.
	Token func() string
	// Reauth se llama ante un 401 (token caducado) y debe dejar un token nuevo disponible en Token.
	// nil: el 401 se devuelve tal cual.
	Reauth func(ctx context.Context) error
	// Transport permite fijar el certificado del servidor (nil: el de http por defecto).
	Transport http.RoundTripper
	// ClientName va en X-DConnector-Client («dConnector-desktop/0.3.0»).
	ClientName string
	// Delay da la espera antes del intento n de reconexión (nil: ReconnectDelay).
	Delay func(attempt int) time.Duration
}

// Client habla con el servidor. Es seguro para uso concurrente.
type Client struct {
	cfg  Config
	http *http.Client

	mu     sync.Mutex
	connID string
}

// New crea el cliente.
func New(cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Token == nil {
		cfg.Token = func() string { return "" }
	}
	if cfg.ClientName == "" {
		cfg.ClientName = "dConnector-desktop"
	}
	if cfg.Delay == nil {
		cfg.Delay = ReconnectDelay
	}
	return &Client{cfg: cfg, http: &http.Client{Transport: cfg.Transport}}
}

// BaseURL de este cliente.
func (c *Client) BaseURL() string { return c.cfg.BaseURL }

// APIError: error devuelto por el servidor (§2). Message está en español y se puede mostrar tal cual.
type APIError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("el servidor respondió %d", e.Status)
}

// IsUnauthorized dice si err es un 401 del servidor.
func IsUnauthorized(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}

// Health es la respuesta de GET /api/v1/health.
type Health struct {
	Status     string `json:"status"`
	Version    string `json:"version"`
	APIVersion int    `json:"apiVersion"`
	Protocol   int    `json:"protocol"`
	TLS        bool   `json:"tls"`
}

// OK: el servidor está listo.
func (h Health) OK() bool { return h.Status == "ok" }

// SessionUser es el usuario de una sesión.
type SessionUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Session es la respuesta del login.
type Session struct {
	Token     string      `json:"token"`
	ExpiresAt int64       `json:"expiresAt"`
	User      SessionUser `json:"user"`
}

// Health consulta el estado del servidor (sin autenticación). Un 503 («starting»/«stopping») no es error:
// se devuelve su estado.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/health", nil, false, nil)
	if err != nil {
		return h, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err := json.Unmarshal(body, &h); err != nil || h.Status == "" {
		if resp.StatusCode >= 400 {
			return h, apiError(resp, body)
		}
		return h, errors.New("la respuesta de /health no es válida: ¿es un servidor de dConnector?")
	}
	return h, nil
}

// Login inicia sesión con usuario y clave.
func (c *Client) Login(ctx context.Context, user, password string) (Session, error) {
	var s Session
	body, _ := json.Marshal(map[string]string{"username": user, "password": password})
	err := c.jsonCall(ctx, http.MethodPost, "/api/v1/auth/login", body, false, &s)
	if err == nil && s.Token == "" {
		err = errors.New("el servidor no entregó un token de sesión")
	}
	return s, err
}

// Me devuelve el usuario de la sesión actual.
func (c *Client) Me(ctx context.Context) (SessionUser, error) {
	var u SessionUser
	err := c.jsonCall(ctx, http.MethodGet, "/api/v1/auth/me", nil, true, &u)
	return u, err
}

// Logout invalida el token actual.
func (c *Client) Logout(ctx context.Context) error {
	return c.jsonCall(ctx, http.MethodPost, "/api/v1/auth/logout", []byte("{}"), true, nil)
}

// Call llama a un método del núcleo (POST /api/v1/rpc/{servicio}/{método}). args son los argumentos de la
// firma Go sin el context. Devuelve el JSON de la respuesta ("null" si no hay valor).
func (c *Client) Call(ctx context.Context, service, method string, args ...any) (json.RawMessage, error) {
	if args == nil {
		args = []any{}
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("argumentos no válidos para %s.%s: %w", service, method, err)
	}
	var raw json.RawMessage
	path := "/api/v1/rpc/" + url.PathEscape(service) + "/" + url.PathEscape(method)
	if err := c.jsonCall(ctx, http.MethodPost, path, body, true, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// CallInto es Call y decodifica el resultado en out (nil: se descarta).
func (c *Client) CallInto(ctx context.Context, out any, service, method string, args ...any) error {
	raw, err := c.Call(ctx, service, method, args...)
	if err != nil || out == nil || len(raw) == 0 || string(raw) == "null" {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("respuesta inesperada de %s.%s: %w", service, method, err)
	}
	return nil
}

// WSTicket pide un ticket de un solo uso para abrir el WebSocket.
func (c *Client) WSTicket(ctx context.Context) (string, error) {
	var r struct {
		Ticket string `json:"ticket"`
	}
	if err := c.jsonCall(ctx, http.MethodPost, "/api/v1/auth/ws-ticket", []byte("{}"), true, &r); err != nil {
		return "", err
	}
	if r.Ticket == "" {
		return "", errors.New("el servidor no entregó un ticket de eventos")
	}
	return r.Ticket, nil
}

// ConnID es el identificador de conexión que anunció el servidor en «hello» ("" sin conexión de eventos).
func (c *Client) ConnID() string { c.mu.Lock(); defer c.mu.Unlock(); return c.connID }

func (c *Client) setConnID(id string) { c.mu.Lock(); c.connID = id; c.mu.Unlock() }

// jsonCall hace la petición, reintenta una vez tras Reauth si da 401 y decodifica la respuesta en out.
func (c *Client) jsonCall(ctx context.Context, method, path string, body []byte, auth bool, out any) error {
	for attempt := 0; ; attempt++ {
		resp, err := c.do(ctx, method, path, body, auth, nil)
		if err != nil {
			return err
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized && auth && attempt == 0 && c.cfg.Reauth != nil {
			if err := c.cfg.Reauth(ctx); err == nil {
				continue
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return apiError(resp, data)
		}
		if rerr != nil {
			return fmt.Errorf("se cortó la respuesta del servidor: %w", rerr)
		}
		if out == nil {
			return nil
		}
		if raw, ok := out.(*json.RawMessage); ok {
			*raw = data
			return nil
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("respuesta inesperada del servidor: %w", err)
		}
		return nil
	}
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, auth bool, extra http.Header) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, rd)
	if err != nil {
		return nil, fmt.Errorf("dirección del servidor no válida: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-DConnector-Client", c.cfg.ClientName)
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token())
		if id := c.ConnID(); id != "" {
			req.Header.Set("X-DConnector-Conn", id)
		}
	}
	for k, v := range extra {
		req.Header[k] = v
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, connError(err)
	}
	return resp, nil
}

func connError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("no se pudo conectar con el servidor: %w", err)
}

func apiError(resp *http.Response, body []byte) *APIError {
	e := &APIError{Status: resp.StatusCode}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil {
		e.Code, e.Message = env.Error.Code, env.Error.Message
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			e.RetryAfter = time.Duration(n) * time.Second
		}
	}
	return e
}
