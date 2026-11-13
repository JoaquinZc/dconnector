// echo-server: servidor de ejemplo del protocolo dConnector v1 (docs/PROTOCOLO-v1.md).
//
//	go run ./examples/echo-server -addr 127.0.0.1:8787 -token secreto
//
// Acepta WebSocket en "/" con el token, imprime el hello y cada evento en una línea legible, responde
// `ack` a todo y, a cada `message.received` que no es tuyo en un chat directo, contesta con un
// comando `message.reply` («Recibido ✅») e imprime el `result`. Para que esa respuesta funcione, el
// destino debe tener activado «Permitir comandos».
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// frame: el sobre del protocolo (solo lo que este ejemplo necesita).
type frame struct {
	V       int             `json:"v"`
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Name    string          `json:"name,omitempty"`
	TS      int64           `json:"ts,omitempty"`
	Account *accountRef     `json:"account,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type accountRef struct {
	ID      string `json:"id"`
	Channel string `json:"channel,omitempty"`
	Label   string `json:"label,omitempty"`
}

// message: los campos de un Message que usa el ejemplo (más chatKind, que añade dConnector).
type message struct {
	NativeID       string `json:"nativeId"`
	ChatNativeID   string `json:"chatNativeId"`
	SenderNativeID string `json:"senderNativeId"`
	SenderName     string `json:"senderName"`
	FromMe         bool   `json:"fromMe"`
	Kind           string `json:"kind"`
	Text           string `json:"text"`
	ChatKind       string `json:"chatKind"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "dirección donde escuchar")
	token := flag.String("token", "secreto", "token que debe traer dConnector (Authorization: Bearer …)")
	flag.Parse()

	srv := &server{token: *token}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handle)
	httpSrv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(sctx)
	}()

	log.Printf("servidor de ejemplo escuchando en ws://%s/ (token: %q)", *addr, *token)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type server struct {
	token string
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	want := "Bearer " + s.token
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
		http.Error(w, "token no válido", http.StatusUnauthorized)
		log.Printf("conexión rechazada: token no válido (desde %s)", r.RemoteAddr)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	log.Printf("dConnector conectado: instancia %s, protocolo %s, %s",
		r.Header.Get("X-DConnector-Id"), r.Header.Get("X-DConnector-Protocol"), r.Header.Get("User-Agent"))
	(&conn{c: c, seen: map[string]struct{}{}, pending: map[string]string{}}).serve(r.Context())
	log.Printf("dConnector desconectado")
}

// conn: una conexión de dConnector.
type conn struct {
	c *websocket.Conn

	mu      sync.Mutex
	seen    map[string]struct{} // ids de eventos ya vistos (el servidor debe deduplicar)
	pending map[string]string   // id del comando → nombre
	nextCmd int
}

func (c *conn) serve(ctx context.Context) {
	for {
		typ, data, err := c.c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var f frame
		if err := json.Unmarshal(data, &f); err != nil {
			log.Printf("frame ilegible: %v", err)
			continue
		}
		switch f.Kind {
		case "hello":
			c.onHello(ctx, f)
		case "event":
			c.onEvent(ctx, f)
		case "result":
			c.onResult(f)
		default:
			log.Printf("frame %q ignorado", f.Kind)
		}
	}
}

func (c *conn) write(ctx context.Context, f frame) error {
	f.V = 1
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.c.Write(wctx, websocket.MessageText, b)
}

func (c *conn) onHello(ctx context.Context, f frame) {
	var h struct {
		InstanceID    string `json:"instanceId"`
		Version       string `json:"version"`
		Protocol      int    `json:"protocol"`
		AllowCommands bool   `json:"allowCommands"`
		Accounts      []struct {
			ID      string `json:"id"`
			Channel string `json:"channel"`
			Label   string `json:"label"`
			State   string `json:"state"`
		} `json:"accounts"`
	}
	_ = json.Unmarshal(f.Data, &h)
	fmt.Printf("HELLO  instancia=%s versión=%s protocolo=%d comandos=%v cuentas=%d\n",
		h.InstanceID, h.Version, h.Protocol, h.AllowCommands, len(h.Accounts))
	for _, a := range h.Accounts {
		fmt.Printf("       cuenta %s (%s, «%s») %s\n", a.ID, a.Channel, a.Label, a.State)
	}
	// El welcome es opcional, pero sin él dConnector espera 10 s antes de mandar el outbox.
	data, _ := json.Marshal(map[string]any{"name": "Servidor de ejemplo", "events": []string{}})
	_ = c.write(ctx, frame{ID: newID("welcome"), Kind: "welcome", Data: data})
}

func (c *conn) onEvent(ctx context.Context, f frame) {
	if f.Name == "media.chunk" {
		fmt.Printf("EVENT  media.chunk (trozo de archivo, sin ack)\n")
		return
	}
	// Siempre se confirma, también los repetidos.
	ack, _ := json.Marshal(map[string][]string{"ids": {f.ID}})
	if err := c.write(ctx, frame{ID: newID("ack"), Kind: "ack", Data: ack}); err != nil {
		log.Printf("no se pudo enviar el ack: %v", err)
		return
	}
	c.mu.Lock()
	_, dup := c.seen[f.ID]
	if len(c.seen) > 50000 {
		c.seen = map[string]struct{}{}
	}
	c.seen[f.ID] = struct{}{}
	c.mu.Unlock()
	if dup {
		fmt.Printf("EVENT  %s %s (repetido, se ignora)\n", f.Name, f.ID)
		return
	}
	fmt.Printf("EVENT  %s\n", describe(f))

	if f.Name != "message.received" || f.Account == nil {
		return
	}
	var m message
	if err := json.Unmarshal(f.Data, &m); err != nil || m.FromMe {
		return
	}
	// chatKind lo añade dConnector; si falta, en un chat directo el remitente es el propio chat.
	direct := m.ChatKind == "direct" || (m.ChatKind == "" && m.SenderNativeID == m.ChatNativeID)
	if !direct {
		return
	}
	c.reply(ctx, f.Account.ID, m)
}

// reply contesta con un comando message.reply.
func (c *conn) reply(ctx context.Context, account string, m message) {
	data, _ := json.Marshal(map[string]any{
		"chatId":  m.ChatNativeID,
		"replyTo": m.NativeID,
		"message": map[string]any{"kind": "text", "text": "Recibido ✅"},
	})
	c.mu.Lock()
	c.nextCmd++
	id := fmt.Sprintf("echo-%d", c.nextCmd)
	c.pending[id] = "message.reply"
	c.mu.Unlock()
	err := c.write(ctx, frame{ID: id, Kind: "command", Name: "message.reply", Account: &accountRef{ID: account}, Data: data})
	if err != nil {
		log.Printf("no se pudo enviar el comando: %v", err)
	}
}

func (c *conn) onResult(f frame) {
	var r struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(f.Data, &r)
	c.mu.Lock()
	delete(c.pending, f.ID)
	c.mu.Unlock()
	if r.OK {
		fmt.Printf("RESULT %s %s ok\n", f.ID, f.Name)
		return
	}
	code, msg := "?", ""
	if r.Error != nil {
		code, msg = r.Error.Code, r.Error.Message
	}
	fmt.Printf("RESULT %s %s error=%s «%s»\n", f.ID, f.Name, code, msg)
}

// describe resume un evento en una línea.
func describe(f frame) string {
	acc := ""
	if f.Account != nil {
		acc = fmt.Sprintf(" cuenta=%s", f.Account.ID)
	}
	line := f.Name + acc
	switch f.Name {
	case "message.received", "message.sent":
		var m message
		if json.Unmarshal(f.Data, &m) == nil {
			text := oneLine(m.Text)
			if text == "" {
				text = "(" + m.Kind + ")"
			}
			line += fmt.Sprintf(" chat=%s de=%s fromMe=%v «%s»", m.ChatNativeID, m.SenderNativeID, m.FromMe, text)
		}
	case "account.state":
		var s struct {
			State  string `json:"state"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(f.Data, &s) == nil {
			line += " estado=" + s.State
		}
	}
	return line + "  id=" + f.ID
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

var idCounter struct {
	sync.Mutex
	n int
}

func newID(prefix string) string {
	idCounter.Lock()
	defer idCounter.Unlock()
	idCounter.n++
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), idCounter.n)
}
