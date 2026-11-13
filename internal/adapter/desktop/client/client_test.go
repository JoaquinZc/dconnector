package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeServer simula lo mínimo de docs/14: health, login, ticket, rpc y eventos.
type fakeServer struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	validTokens map[string]bool
	tickets     map[string]bool
	conns       int
	lastRPCConn string
	rpcBody     string
	wsURLs      []string
	// scripts: lo que envía cada conexión de eventos antes de cerrarse (índice = nº de conexión).
	scripts [][]string
	holdAt  int // la conexión con este índice se queda abierta
	seq     atomic.Int64
}

func newFake(t *testing.T) *fakeServer {
	f := &fakeServer{t: t, validTokens: map[string]bool{"tok1": true}, tickets: map[string]bool{}, holdAt: -1}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok","version":"0.3.0","apiVersion":1,"protocol":1,"tls":false}`)
	})
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Password != "clave-correcta-12" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"Usuario o clave incorrectos."}}`)
			return
		}
		f.mu.Lock()
		f.validTokens["tok2"] = true
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"token":"tok2","expiresAt":1,"user":{"id":"u1","username":"admin","role":"admin"}}`)
	})
	mux.HandleFunc("/api/v1/auth/ws-ticket", f.auth(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		id := "t" + strings.Repeat("x", int(f.seq.Add(1)%7)+1)
		f.tickets[id] = true
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"ticket":"`+id+`","expiresAt":1}`)
	}))
	mux.HandleFunc("/api/v1/rpc/", f.auth(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.lastRPCConn = r.Header.Get("X-DConnector-Conn")
		f.rpcBody = r.URL.Path + " " + string(b)
		f.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/rpc/Accounts/List":
			_, _ = io.WriteString(w, `[{"account":{"id":"a1","label":"Ventas","channel":"whatsapp"}}]`)
		case "/api/v1/rpc/Notifications/SetFocus":
			_, _ = io.WriteString(w, `null`)
		case "/api/v1/rpc/Limits/Hit":
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"code":"rate_limited","message":"Demasiadas peticiones."}}`)
		default:
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"No existe ese método."}}`)
		}
	}))
	mux.HandleFunc("/api/v1/events", f.events)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		ok := f.validTokens[tok]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"La sesión caducó."}}`)
			return
		}
		next(w, r)
	}
}

func (f *fakeServer) events(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.wsURLs = append(f.wsURLs, r.URL.String())
	ok := f.tickets[r.URL.Query().Get("ticket")]
	delete(f.tickets, r.URL.Query().Get("ticket"))
	idx := f.conns
	f.conns++
	var script []string
	if idx < len(f.scripts) {
		script = f.scripts[idx]
	}
	hold := idx == f.holdAt
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(401)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"event":"hello","seq":0,"data":{"version":"0.3.0","apiVersion":1,"protocol":1,"user":"admin","connId":"conn-`+string(rune('A'+idx))+`"}}`))
	for _, m := range script {
		_ = c.Write(r.Context(), websocket.MessageText, []byte(m))
	}
	if hold {
		<-r.Context().Done()
		return
	}
	time.Sleep(50 * time.Millisecond)
	_ = c.Close(websocket.StatusGoingAway, "apagado")
}

func newClient(f *fakeServer, token *atomic.Value, reauth func(context.Context) error) *Client {
	return New(Config{
		BaseURL: f.srv.URL,
		Token:   func() string { return token.Load().(string) },
		Reauth:  reauth,
		Delay:   func(int) time.Duration { return 20 * time.Millisecond },
	})
}

func tokenOf(s string) *atomic.Value { v := &atomic.Value{}; v.Store(s); return v }

func TestHealthAndLogin(t *testing.T) {
	f := newFake(t)
	c := newClient(f, tokenOf(""), nil)
	h, err := c.Health(context.Background())
	if err != nil || !h.OK() || h.Version != "0.3.0" || h.APIVersion != 1 {
		t.Fatalf("health: %+v %v", h, err)
	}
	if _, err := c.Login(context.Background(), "admin", "mala"); err == nil || err.Error() != "Usuario o clave incorrectos." || !IsUnauthorized(err) {
		t.Fatalf("login malo: %v", err)
	}
	s, err := c.Login(context.Background(), "admin", "clave-correcta-12")
	if err != nil || s.Token != "tok2" || s.User.Username != "admin" {
		t.Fatalf("login: %+v %v", s, err)
	}
}

func TestHealthRejectsNonServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "<html>hola</html>") }))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL})
	if _, err := c.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "servidor de dConnector") {
		t.Fatalf("esperaba aviso de servidor no válido: %v", err)
	}
}

func TestCallArgsResultAndErrors(t *testing.T) {
	f := newFake(t)
	c := newClient(f, tokenOf("tok1"), nil)
	var out []map[string]any
	if err := c.CallInto(context.Background(), &out, "Accounts", "List"); err != nil || len(out) != 1 {
		t.Fatalf("Accounts.List: %v %v", out, err)
	}
	if !strings.HasSuffix(f.rpcBody, " []") {
		t.Fatalf("sin argumentos debe ir []: %q", f.rpcBody)
	}
	if err := c.CallInto(context.Background(), nil, "Notifications", "SetFocus", "wa:1:x", true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(f.rpcBody, ` ["wa:1:x",true]`) {
		t.Fatalf("cuerpo: %q", f.rpcBody)
	}
	err := c.CallInto(context.Background(), nil, "Limits", "Hit")
	ae, ok := err.(*APIError)
	if !ok || ae.Code != "rate_limited" || ae.RetryAfter != 7*time.Second || ae.Error() != "Demasiadas peticiones." {
		t.Fatalf("error de API: %#v", err)
	}
	if err := c.CallInto(context.Background(), nil, "X", "Y"); err == nil || !strings.Contains(err.Error(), "No existe") {
		t.Fatalf("404: %v", err)
	}
}

func TestReauthRetriesOnce(t *testing.T) {
	f := newFake(t)
	tok := tokenOf("caducado")
	var calls int32
	c := newClient(f, tok, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		tok.Store("tok1")
		return nil
	})
	if err := c.CallInto(context.Background(), nil, "Notifications", "SetFocus", "k", false); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("Reauth debía llamarse una vez: %d", calls)
	}
}

func TestEventsHelloConnIDAndReconnect(t *testing.T) {
	f := newFake(t)
	f.scripts = [][]string{
		{`{"event":"unread:total","seq":1,"data":{"total":5,"byAccount":{"a1":5}}}`},
		{`{"event":"notify","seq":1,"data":{"chatKey":"wa:a1:x","title":"Ana","body":"hola","sound":true}}`},
	}
	f.holdAt = 1
	c := newClient(f, tokenOf("tok1"), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var mu sync.Mutex
	var evs []Event
	var states []StreamState
	done := make(chan struct{})
	go func() {
		c.RunEvents(ctx, func(e Event) { mu.Lock(); evs = append(evs, e); mu.Unlock() },
			func(s StreamState) { mu.Lock(); states = append(states, s); mu.Unlock() })
		close(done)
	}()
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range evs {
			if e.Name == EvtNotify {
				return true
			}
		}
		return false
	})
	// Con la segunda conexión abierta, las llamadas RPC llevan su connId.
	if err := c.CallInto(ctx, nil, "Notifications", "SetFocus", "k", true); err != nil {
		t.Fatal(err)
	}
	if f.lastRPCConn != "conn-B" {
		t.Fatalf("X-DConnector-Conn = %q", f.lastRPCConn)
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	var u UnreadTotal
	for _, e := range evs {
		if e.Name == EvtUnreadTotal {
			u, _ = Decode[UnreadTotal](e)
		}
	}
	if u.Total != 5 || u.ByAccount["a1"] != 5 {
		t.Fatalf("unread: %+v", u)
	}
	conn, disc := 0, 0
	for _, s := range states {
		if s.Connected {
			conn++
		} else if s.Err != nil {
			disc++
		}
	}
	if conn != 2 || disc < 1 {
		t.Fatalf("estados: conectó %d, cayó %d (%+v)", conn, disc, states)
	}
	// El ticket va en la URL del WebSocket; el token de sesión, nunca.
	for _, u := range f.wsURLs {
		if strings.Contains(u, "tok1") || !strings.Contains(u, "ticket=") {
			t.Fatalf("URL del WebSocket: %s", u)
		}
	}
}

func TestEventsReauthOnTicket401(t *testing.T) {
	f := newFake(t)
	f.holdAt = 0
	tok := tokenOf("caducado")
	var reauths int32
	c := newClient(f, tok, func(ctx context.Context) error {
		atomic.AddInt32(&reauths, 1)
		tok.Store("tok1")
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connected := make(chan struct{}, 1)
	go c.RunEvents(ctx, func(Event) {}, func(s StreamState) {
		if s.Connected {
			select {
			case connected <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-connected:
	case <-time.After(8 * time.Second):
		t.Fatal("no conectó tras renovar la sesión")
	}
	if atomic.LoadInt32(&reauths) < 1 {
		t.Fatal("no se renovó la sesión")
	}
}

func TestReconnectDelay(t *testing.T) {
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30}
	for i, w := range want {
		base := w * time.Second
		for k := 0; k < 200; k++ {
			d := ReconnectDelay(i + 1)
			if d < time.Duration(float64(base)*0.8)-time.Millisecond || d > time.Duration(float64(base)*1.2)+time.Millisecond {
				t.Fatalf("intento %d: %v fuera de ±20%% de %v", i+1, d, base)
			}
		}
	}
}

func TestStripURLHidesTicket(t *testing.T) {
	err := stripURL(io.ErrUnexpectedEOF)
	if err != io.ErrUnexpectedEOF {
		t.Fatal("no debía cambiar")
	}
	e2 := stripURL(&testErr{"failed to dial ws://x/api/v1/events?ticket=SECRETO&a=1: boom"})
	if strings.Contains(e2.Error(), "SECRETO") {
		t.Fatalf("el ticket se coló: %v", e2)
	}
}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

func waitFor(t *testing.T, cond func() bool) {
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

func TestUploadAndDownload(t *testing.T) {
	var gotName, gotBody, gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/files", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		f, hdr, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(400)
			return
		}
		b, _ := io.ReadAll(f)
		gotName, gotBody = hdr.Filename, string(b)
		_, _ = io.WriteString(w, `{"id":"01JABC","name":"foto.jpg","mime":"image/jpeg","size":5}`)
	})
	mux.HandleFunc("/api/v1/media/a1/m1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tk" {
			w.WriteHeader(401)
			return
		}
		_, _ = io.WriteString(w, "contenido")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: func() string { return "tk" }})

	up, err := c.Upload(context.Background(), `C:\fotos\foto.jpg`, strings.NewReader("hola!"))
	if err != nil || up.ID != "01JABC" || gotName != "foto.jpg" || gotBody != "hola!" || gotAuth != "Bearer tk" {
		t.Fatalf("subida: %+v %v nombre=%q cuerpo=%q auth=%q", up, err, gotName, gotBody, gotAuth)
	}
	var sb strings.Builder
	if n, err := c.Download(context.Background(), "/media/a1/m1", &sb); err != nil || n != 9 || sb.String() != "contenido" {
		t.Fatalf("descarga: %d %v %q", n, err, sb.String())
	}
	for _, bad := range []string{"/media/../x", "https://evil/media/x", "/api/v1/rpc/X/Y", "/media/a?token=x", ""} {
		if _, err := c.Download(context.Background(), bad, io.Discard); err == nil {
			t.Errorf("%q debía rechazarse", bad)
		}
	}
}
