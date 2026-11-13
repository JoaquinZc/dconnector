package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"

	"dconnector/internal/core/model"
)

// Nombres de los eventos que usa el escritorio (docs/14 §5).
const (
	EvtHello          = "hello"
	EvtUnreadTotal    = "unread:total"
	EvtAccountStatus  = "account:status"
	EvtNotify         = "notify"
	EvtNotifyDecision = "notify:decision"
	EvtChatUpsert     = "chat:upsert"
	EvtChatRemoved    = "chat:removed"
	EvtMessageUpsert  = "message:upsert"
)

// Event es un marco del WebSocket: {"event":…, "seq":…, "data":…}.
type Event struct {
	Name string          `json:"event"`
	Seq  int64           `json:"seq"`
	Data json.RawMessage `json:"data"`
}

// Decode convierte la carga de un evento en el tipo T.
func Decode[T any](ev Event) (T, error) {
	var v T
	if len(ev.Data) == 0 {
		return v, nil
	}
	err := json.Unmarshal(ev.Data, &v)
	return v, err
}

// Hello: primer mensaje de la conexión.
type Hello struct {
	Version    string `json:"version"`
	APIVersion int    `json:"apiVersion"`
	Protocol   int    `json:"protocol"`
	ServerTime int64  `json:"serverTime"`
	User       string `json:"user"`
	ConnID     string `json:"connId"`
}

// UnreadTotal: carga de unread:total.
type UnreadTotal struct {
	Total     int            `json:"total"`
	ByAccount map[string]int `json:"byAccount"`
}

// AccountStatus: carga de account:status.
type AccountStatus = model.AccountStatus

// Notify: carga de notify. El servidor ya decidió que hay que avisar; Silent + Sound = «solo sonido».
type Notify struct {
	ChatKey   string `json:"chatKey"`
	AccountID string `json:"accountId"`
	Channel   string `json:"channel"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Preview   string `json:"preview"`
	Silent    bool   `json:"silent"`
	Sound     bool   `json:"sound"`
	Reason    string `json:"reason"`
}

// NotifyDecision: carga de notify:decision (por qué un mensaje no avisó).
type NotifyDecision struct {
	ChatKey string `json:"chatKey"`
	Shown   bool   `json:"shown"`
	Reason  string `json:"reason"`
}

// StreamState informa de los cambios de la conexión de eventos.
type StreamState struct {
	// Connected: el WebSocket está abierto y llegó «hello».
	Connected bool
	// Err: por qué se perdió o no se pudo abrir (nil al conectar).
	Err error
	// Attempt: número de intento desde la última conexión buena (0 al conectar).
	Attempt int
	// Hello: datos de la conexión (solo con Connected).
	Hello Hello
}

// ReconnectDelay: 1, 2, 4, 8, 16 y 30 s (tope) con ±20 % de azar (docs/14 §5).
func ReconnectDelay(attempt int) time.Duration {
	steps := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(steps) {
		attempt = len(steps)
	}
	base := steps[attempt-1]
	jitter := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(base) * jitter)
}

// RunEvents mantiene abierta la conexión de eventos hasta que ctx termine: pide un ticket, abre el
// WebSocket, entrega cada evento a handler (en orden, desde una sola goroutina) y, si se cae, reintenta
// con espera creciente. Un 401 al pedir el ticket (o el cierre 4401) llama a Reauth. Bloquea.
func (c *Client) RunEvents(ctx context.Context, handler func(Event), onState func(StreamState)) {
	if onState == nil {
		onState = func(StreamState) {}
	}
	attempt := 0
	for ctx.Err() == nil {
		connected, err := c.session(ctx, handler, onState, attempt)
		if ctx.Err() != nil {
			return
		}
		if connected {
			attempt = 0
		}
		attempt++
		onState(StreamState{Err: err, Attempt: attempt})
		var ra time.Duration
		var ae *APIError
		if errors.As(err, &ae) {
			ra = ae.RetryAfter
		}
		delay := max(c.cfg.Delay(attempt), ra)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}

// session abre una conexión y la atiende hasta que se cae. connected: llegó a recibir «hello».
func (c *Client) session(ctx context.Context, handler func(Event), onState func(StreamState), attempt int) (connected bool, err error) {
	defer c.setConnID("")
	ticket, err := c.WSTicket(ctx)
	if IsUnauthorized(err) && c.cfg.Reauth != nil {
		if rerr := c.cfg.Reauth(ctx); rerr != nil {
			return false, rerr
		}
		ticket, err = c.WSTicket(ctx)
	}
	if err != nil {
		return false, err
	}
	u, err := url.Parse(c.cfg.BaseURL)
	if err != nil {
		return false, fmt.Errorf("dirección del servidor no válida: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/events"
	u.RawQuery = url.Values{"ticket": {ticket}}.Encode()

	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dctx, u.String(), &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: c.cfg.Transport},
		HTTPHeader: http.Header{"X-DConnector-Client": {c.cfg.ClientName}},
	})
	cancel()
	if err != nil {
		return false, connError(stripURL(err))
	}
	conn.SetReadLimit(8 << 20)
	defer conn.CloseNow()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == 4401 {
				if c.cfg.Reauth != nil {
					_ = c.cfg.Reauth(ctx)
				}
				return connected, errors.New("la sesión del servidor caducó")
			}
			if ctx.Err() != nil {
				return connected, ctx.Err()
			}
			return connected, fmt.Errorf("se perdió la conexión de eventos: %w", stripURL(err))
		}
		var ev Event
		if json.Unmarshal(data, &ev) != nil || ev.Name == "" {
			continue
		}
		if ev.Name == EvtHello {
			h, _ := Decode[Hello](ev)
			c.setConnID(h.ConnID)
			connected = true
			onState(StreamState{Connected: true, Hello: h})
		}
		handler(ev)
	}
}

var ticketRe = regexp.MustCompile(`ticket=[^&\s"']+`)

// stripURL oculta el ticket (va en la consulta de la URL) en el texto de un error.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if msg := err.Error(); ticketRe.MatchString(msg) {
		return errors.New(ticketRe.ReplaceAllString(msg, "ticket=…"))
	}
	return err
}
