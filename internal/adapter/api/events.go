package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"dconnector/internal/core/model"
)

const (
	maxConnsPerUser = 8
	apiVersion      = 1
	protocolVersion = 1
)

// connQueueSize: eventos en cola por conexión antes de cerrarla por lenta (variable para las pruebas).
var connQueueSize = 1024

// queued: un evento ya serializado; el número de secuencia lo pone el escritor de cada conexión.
type queued struct {
	event string
	data  json.RawMessage
}

type wsConn struct {
	id, user string
	c        *websocket.Conn
	q        chan queued
	cancel   context.CancelFunc
}

// hub: las conexiones WebSocket abiertas y el reparto de eventos.
type hub struct {
	log       *slog.Logger
	version   string
	ctx       context.Context
	pingEvery time.Duration
	pongWait  time.Duration
	onClosed  func(connID string)
	// active (opcional): dice si el usuario sigue activo. Se consulta en cada latido y, si ya no lo está
	// (desactivado o borrado con la CLI, otro proceso que no puede avisar), se cierra su conexión con 4401.
	active func(user string) bool

	mu     sync.Mutex
	conns  map[*wsConn]struct{}
	byUser map[string]int
	closed bool
	wg     sync.WaitGroup
}

func newHub(ctx context.Context, log *slog.Logger, version string, onClosed func(string)) *hub {
	return &hub{log: log, version: version, ctx: ctx, pingEvery: 25 * time.Second, pongWait: 60 * time.Second,
		onClosed: onClosed, conns: map[*wsConn]struct{}{}, byUser: map[string]int{}}
}

// broadcast manda un evento a todas las conexiones. Una conexión que no da abasto (cola llena) se cierra:
// no hay reenvío, el cliente recarga su estado al reconectar.
func (h *hub) broadcast(event string, data any) {
	h.mu.Lock()
	n := len(h.conns)
	h.mu.Unlock()
	if n == 0 {
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		h.log.Error("no se pudo serializar un evento", "event", event, "error", err)
		return
	}
	h.mu.Lock()
	var slow []*wsConn
	for c := range h.conns {
		select {
		case c.q <- queued{event, raw}:
		default:
			slow = append(slow, c)
			delete(h.conns, c) // deja de recibir ya; release lo contará al terminar su manejador
		}
	}
	h.mu.Unlock()
	for _, c := range slow {
		h.log.Warn("conexión de eventos demasiado lenta: se cierra", "conn", c.id)
		c.cancel() // aborta la escritura bloqueada; el manejador cierra el socket
	}
}

// reserve cuenta una conexión del usuario; false si ya tiene el máximo o el hub se cerró.
func (h *hub) reserve(user string) (ok, closed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false, true
	}
	if h.byUser[user] >= maxConnsPerUser {
		return false, false
	}
	h.byUser[user]++
	h.wg.Add(1)
	return true, false
}

func (h *hub) release(user string, c *wsConn) {
	h.mu.Lock()
	if c != nil {
		delete(h.conns, c)
	}
	if h.byUser[user]--; h.byUser[user] <= 0 {
		delete(h.byUser, user)
	}
	h.mu.Unlock()
	h.wg.Done()
}

// closeAll cierra todas las conexiones con 1001 (apagado) y espera a que terminen sus manejadores.
func (h *hub) closeAll() {
	h.mu.Lock()
	h.closed = true
	cs := make([]*wsConn, 0, len(h.conns))
	for c := range h.conns {
		cs = append(cs, c)
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range cs {
		wg.Add(1)
		go func(c *wsConn) {
			defer wg.Done()
			_ = c.c.Close(websocket.StatusGoingAway, "El servidor se está apagando.")
		}(c)
	}
	wg.Wait()
	h.wg.Wait()
}

// serve atiende una conexión ya autorizada (el usuario viene del ticket).
func (h *hub) serve(w http.ResponseWriter, r *http.Request, user string) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // el origen ya se verificó (docs/14 §5)
	})
	if err != nil {
		h.release(user, nil)
		return
	}
	c.SetReadLimit(1 << 16)
	ctx, cancel := context.WithCancel(h.ctx)
	conn := &wsConn{id: model.NewID(), user: user, c: c, q: make(chan queued, connQueueSize), cancel: cancel}

	h.mu.Lock()
	h.conns[conn] = struct{}{}
	h.mu.Unlock()
	defer func() {
		cancel()
		_ = c.CloseNow()
		h.release(user, conn)
		if h.onClosed != nil {
			h.onClosed(conn.id)
		}
	}()

	hello := map[string]any{
		"version": h.version, "apiVersion": apiVersion, "protocol": protocolVersion,
		"serverTime": time.Now().UnixMilli(), "user": user, "connId": conn.id,
	}
	if !conn.write(ctx, EvtHello, 0, hello) {
		return
	}

	// Lector: el cliente no manda datos (si lo hace, se ignora), pero hace falta leer para atender ping/pong y cierre.
	go func() {
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()
	// Latido: ping cada pingEvery; sin pong en pongWait se cierra.
	go func() {
		t := time.NewTicker(h.pingEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, pc := context.WithTimeout(ctx, h.pongWait)
				err := c.Ping(pctx)
				pc()
				if err != nil { // sin pong a tiempo: se corta (cancel hace que el manejador cierre el socket)
					cancel()
					return
				}
				if h.active != nil && !h.active(user) {
					_ = c.Close(closeRevoked, "La sesión ya no es válida.")
					cancel()
					return
				}
			}
		}
	}()

	var seq int64
	for {
		select {
		case <-ctx.Done():
			return
		case q := <-conn.q:
			seq++
			if !conn.write(ctx, q.event, seq, q.data) {
				return
			}
		}
	}
}

func (c *wsConn) write(ctx context.Context, event string, seq int64, data any) bool {
	b, err := json.Marshal(struct {
		Event string `json:"event"`
		Seq   int64  `json:"seq"`
		Data  any    `json:"data"`
	}{event, seq, data})
	if err != nil {
		return true // un evento que no se serializa se omite; la conexión sigue
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.c.Write(wctx, websocket.MessageText, b) == nil
}

// handleEvents atiende GET /api/v1/events?ticket=…
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, codeMethod, "Usa GET con una conexión WebSocket.")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(r, origin) {
		writeError(w, http.StatusForbidden, codeForbidden, "El origen de la conexión no está permitido.")
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		writeError(w, http.StatusBadRequest, codeInvalid, "Esta ruta es un WebSocket: abre la conexión con «Upgrade: websocket».")
		return
	}
	ticket := r.URL.Query().Get("ticket")
	user, ok := "", false
	if ticket != "" {
		user, ok = s.cfg.Auth.RedeemWSTicket(ticket)
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "El ticket no es válido o ya caducó.")
		return
	}
	if !s.gateReady(w) {
		return
	}
	reserved, closed := s.hub.reserve(user)
	if closed {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, "El servidor se está apagando.")
		return
	}
	if !reserved {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, codeRateLimited, "Hay demasiadas conexiones abiertas para este usuario (máximo 8).")
		return
	}
	s.hub.serve(w, r, user)
}

// closeRevoked es el código de cierre del WebSocket cuando se revoca el token del usuario (docs/14 §5).
const closeRevoked = websocket.StatusCode(4401)

// revokeUser cierra con 4401 todas las conexiones del usuario (sus sesiones se acaban de invalidar).
func (h *hub) revokeUser(user string) {
	h.mu.Lock()
	var cs []*wsConn
	for c := range h.conns {
		if c.user == user {
			cs = append(cs, c)
		}
	}
	h.mu.Unlock()
	for _, c := range cs {
		go func(c *wsConn) { _ = c.c.Close(closeRevoked, "La sesión ya no es válida.") }(c)
	}
}
