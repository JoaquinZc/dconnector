package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"dconnector/internal/adapter/desktop/client"
	"dconnector/internal/core/port"
)

type fakeNotifier struct {
	mu       sync.Mutex
	shown    []port.Notification
	sounds   int
	perm     port.NotifyPermission
	showErr  error
	soundErr error
}

func (f *fakeNotifier) Permission(context.Context) port.NotifyPermission { return f.perm }
func (f *fakeNotifier) RequestPermission(context.Context) (port.NotifyPermission, error) {
	return f.perm, nil
}
func (f *fakeNotifier) Show(_ context.Context, n port.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.showErr != nil {
		return f.showErr
	}
	f.shown = append(f.shown, n)
	return nil
}
func (f *fakeNotifier) PlaySound(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.soundErr != nil {
		return f.soundErr
	}
	f.sounds++
	return nil
}
func (f *fakeNotifier) OpenSystemSettings() error { return nil }

func ev(name, data string) client.Event {
	return client.Event{Name: name, Data: json.RawMessage(data)}
}

func TestNotifyShowsToastAndSound(t *testing.T) {
	n := &fakeNotifier{}
	b := New(Deps{Notifier: n})
	b.Handle(ev("notify", `{"chatKey":"wa:a1:x","title":"Ana","body":"hola","sound":true}`))
	if len(n.shown) != 1 || n.sounds != 1 {
		t.Fatalf("aviso %d, sonido %d", len(n.shown), n.sounds)
	}
	got := n.shown[0]
	if got.Title != "Ana" || got.Body != "hola" || string(got.ChatKey) != "wa:a1:x" || !got.Silent || !strings.HasPrefix(got.ID, "wa:a1:x#") {
		t.Fatalf("aviso: %+v", got)
	}
}

func TestNotifySilentMeansSoundOnly(t *testing.T) {
	n := &fakeNotifier{}
	b := New(Deps{Notifier: n})
	b.Handle(ev("notify", `{"chatKey":"k","title":"Ana","body":"hola","silent":true,"sound":true}`))
	if len(n.shown) != 0 || n.sounds != 1 {
		t.Fatalf("solo sonido: aviso %d, sonido %d", len(n.shown), n.sounds)
	}
}

func TestNotifyToastOnlyAndPreviewFallback(t *testing.T) {
	n := &fakeNotifier{}
	b := New(Deps{Notifier: n})
	b.Handle(ev("notify", `{"chatKey":"k","title":"","preview":"vista previa"}`))
	if len(n.shown) != 1 || n.sounds != 0 || n.shown[0].Body != "vista previa" || n.shown[0].Title != "dConnector" {
		t.Fatalf("%+v sonidos=%d", n.shown, n.sounds)
	}
}

func TestNotifyShowFailureFallsBackToSound(t *testing.T) {
	n := &fakeNotifier{showErr: errors.New("bloqueado")}
	b := New(Deps{Notifier: n})
	b.Handle(ev("notify", `{"chatKey":"k","title":"Ana","body":"x"}`))
	if n.sounds != 1 {
		t.Fatal("si el aviso falla y no había sonido, debe sonar")
	}
}

func TestUnreadAndDecision(t *testing.T) {
	var total = -1
	now := time.UnixMilli(5000)
	b := New(Deps{OnUnread: func(n int) { total = n }, Now: func() time.Time { return now }})
	b.Handle(ev("unread:total", `{"total":7,"byAccount":{"a":7}}`))
	if total != 7 {
		t.Fatalf("total %d", total)
	}
	b.Handle(ev("notify:decision", `{"chatKey":"k","shown":false,"reason":"el chat está silenciado"}`))
	if txt, at := b.LastDecision(); txt != "el chat está silenciado" || at != 5000 {
		t.Fatalf("decisión: %q %d", txt, at)
	}
	b.Handle(ev("unread:total", `{basura`)) // no debe romper
}

func TestSendTestMessages(t *testing.T) {
	n := &fakeNotifier{perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietInactive}}
	r := SendTest(context.Background(), n, true)
	if !r.Shown || !r.Visible || !r.Sound || r.Message != "Aviso enviado" {
		t.Fatalf("%+v", r)
	}
	n = &fakeNotifier{perm: port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietActive, QuietCause: port.QuietCauseDND}}
	r = SendTest(context.Background(), n, true)
	if !r.Shown || r.Visible || !strings.Contains(r.Message, "No molestar") || !strings.Contains(r.Message, "sonará") {
		t.Fatalf("%+v", r)
	}
	n = &fakeNotifier{perm: port.NotifyPermission{State: port.NotifyDenied, Reason: "Bloqueadas."}}
	r = SendTest(context.Background(), n, false)
	if r.Shown || r.Sound || !strings.Contains(r.Message, "sonido está apagado") {
		t.Fatalf("%+v", r)
	}
}

// serve levanta un servidor falso con los RPC que usa el puente y un WebSocket de eventos.
func serve(t *testing.T, events []string) (*httptest.Server, *struct {
	mu    sync.Mutex
	focus []string
}) {
	rec := &struct {
		mu    sync.Mutex
		focus []string
	}{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/ws-ticket", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ticket":"t1","expiresAt":1}`)
	})
	mux.HandleFunc("/api/v1/rpc/", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/v1/rpc/Inbox/UnreadTotals":
			_, _ = io.WriteString(w, `{"a":2,"b":3}`)
		case "/api/v1/rpc/Accounts/List":
			_, _ = io.WriteString(w, `[{"account":{"label":"Ventas","channel":"whatsapp"},"status":{"state":"connected"}},{"account":{"label":"Soporte","channel":"telegram"},"status":{"state":"error"}}]`)
		case "/api/v1/rpc/Settings/Get":
			_, _ = io.WriteString(w, `{"closeToTray":true,"startWithWindows":true,"startMinimized":false,"notificationsSound":false,"theme":"dark"}`)
		case "/api/v1/rpc/Notifications/SetFocus":
			rec.mu.Lock()
			rec.focus = append(rec.focus, string(b))
			rec.mu.Unlock()
			_, _ = io.WriteString(w, `null`)
		default:
			w.WriteHeader(404)
		}
	})
	mux.HandleFunc("/api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"event":"hello","seq":0,"data":{"connId":"c1"}}`))
		for _, e := range events {
			_ = c.Write(r.Context(), websocket.MessageText, []byte(e))
		}
		<-r.Context().Done()
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s, rec
}

func TestServeSyncsAccountsUnreadSettingsAndFocus(t *testing.T) {
	srv, rec := serve(t, []string{`{"event":"account:status","seq":1,"data":{"accountId":"a","state":"error"}}`})
	var mu sync.Mutex
	var lines []AccountLine
	var unread = -1
	var settings []Settings
	var conn []bool
	opened := make(chan string, 1)
	b := New(Deps{
		OnAccounts:   func(l []AccountLine) { mu.Lock(); lines = l; mu.Unlock() },
		OnUnread:     func(n int) { mu.Lock(); unread = n; mu.Unlock() },
		OnSettings:   func(s Settings) { mu.Lock(); settings = append(settings, s); mu.Unlock() },
		OnConnection: func(c bool, _ string) { mu.Lock(); conn = append(conn, c); mu.Unlock() },
		OpenChat:     func(k string) { opened <- k },
	})
	cl := client.New(client.Config{BaseURL: srv.URL, Token: func() string { return "t" }, Delay: func(int) time.Duration { return 20 * time.Millisecond }})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Serve(ctx, cl); close(done) }()

	wait(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(lines) == 2 && unread == 5 && len(settings) >= 1 && len(conn) >= 1 && conn[0]
	})
	mu.Lock()
	if lines[0].Label != "Ventas" || !lines[0].Connected || lines[1].Connected || lines[1].State != "error" {
		t.Fatalf("cuentas: %+v", lines)
	}
	if !settings[0].CloseToTray || !settings[0].StartWithWindows || settings[0].NotificationsSound {
		t.Fatalf("ajustes: %+v", settings[0])
	}
	mu.Unlock()
	if !b.Listening() || b.SoundEnabled() {
		t.Fatal("estado del puente: escuchando y sonido apagado según el servidor")
	}

	// Pulsar un aviso: abre el chat y avisa del foco al servidor.
	b.OnToastActivate("wa:a:x")
	if k := <-opened; k != "wa:a:x" {
		t.Fatalf("chat abierto: %q", k)
	}
	wait(t, func() bool { rec.mu.Lock(); defer rec.mu.Unlock(); return len(rec.focus) == 1 })
	if rec.focus[0] != `["wa:a:x",true]` {
		t.Fatalf("SetFocus: %s", rec.focus[0])
	}
	cancel()
	<-done
	if b.Listening() {
		t.Fatal("tras parar ya no escucha")
	}
}

func wait(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("se agotó la espera")
}
