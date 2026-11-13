package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func dial(t *testing.T, ts *httptest.Server, token string) (*websocket.Conn, *http.Response, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http"), &websocket.DialOptions{HTTPHeader: h})
}

func send(t *testing.T, c *websocket.Conn, v any) {
	b, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func recv(t *testing.T, c *websocket.Conn, d time.Duration) (frame, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		return frame{}, false
	}
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f, true
}

func TestEchoServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc((&server{token: "secreto"}).handle))
	defer ts.Close()

	if _, resp, err := dial(t, ts, "malo"); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("un token malo debe dar 401: %v %v", err, resp)
	}

	c, _, err := dial(t, ts, "secreto")
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	send(t, c, map[string]any{"v": 1, "id": "h1", "kind": "hello", "data": map[string]any{"instanceId": "i", "protocol": 1, "accounts": []any{}}})
	if f, ok := recv(t, c, 3*time.Second); !ok || f.Kind != "welcome" {
		t.Fatalf("tras el hello debe llegar un welcome: %+v", f)
	}

	event := func(id string, fromMe bool, kind string) map[string]any {
		return map[string]any{
			"v": 1, "id": id, "kind": "event", "name": "message.received", "account": map[string]any{"id": "wa-1"},
			"data": map[string]any{"nativeId": "m1", "chatNativeId": "59399@s.whatsapp.net", "senderNativeId": "59399@s.whatsapp.net",
				"fromMe": fromMe, "kind": "text", "text": "hola", "chatKind": kind},
		}
	}

	// Un mensaje ajeno en un chat directo: ack y comando message.reply.
	send(t, c, event("e1", false, "direct"))
	ack, _ := recv(t, c, 3*time.Second)
	if ack.Kind != "ack" || !strings.Contains(string(ack.Data), `"e1"`) {
		t.Fatalf("ack: %+v", ack)
	}
	cmd, ok := recv(t, c, 3*time.Second)
	if !ok || cmd.Kind != "command" || cmd.Name != "message.reply" || cmd.Account == nil || cmd.Account.ID != "wa-1" {
		t.Fatalf("comando: %+v", cmd)
	}
	var d struct {
		ChatID  string `json:"chatId"`
		ReplyTo string `json:"replyTo"`
		Message struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"message"`
	}
	if err := json.Unmarshal(cmd.Data, &d); err != nil {
		t.Fatal(err)
	}
	if d.ChatID != "59399@s.whatsapp.net" || d.ReplyTo != "m1" || d.Message.Kind != "text" || d.Message.Text != "Recibido ✅" {
		t.Fatalf("datos del comando: %+v", d)
	}
	send(t, c, map[string]any{"v": 1, "id": cmd.ID, "kind": "result", "name": "message.reply", "data": map[string]any{"ok": true, "result": map[string]any{}}})

	// Repetido, propio y de grupo: solo ack, nunca respuesta.
	for _, ev := range []map[string]any{event("e1", false, "direct"), event("e2", true, "direct"), event("e3", false, "group")} {
		send(t, c, ev)
		if f, ok := recv(t, c, 3*time.Second); !ok || f.Kind != "ack" {
			t.Fatalf("debe llegar el ack de %v: %+v", ev["id"], f)
		}
		// Un evento centinela: si hubiera respuesta, llegaría antes que su ack (leer con tiempo
		// vencido cerraría la conexión, por eso no se espera «a ver si llega algo»).
		sentinel := "s-" + ev["id"].(string)
		send(t, c, map[string]any{"v": 1, "id": sentinel, "kind": "event", "name": "chat.updated", "data": map[string]any{}})
		f, ok := recv(t, c, 3*time.Second)
		if !ok || f.Kind != "ack" || !strings.Contains(string(f.Data), sentinel) {
			t.Fatalf("no debía haber respuesta a %v, y llegó: %+v", ev["id"], f)
		}
	}
}
