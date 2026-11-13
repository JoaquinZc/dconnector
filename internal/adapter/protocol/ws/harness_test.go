package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"dconnector/internal/adapter/store/sqlite"
	"dconnector/internal/core/bus"
	"dconnector/internal/core/fake"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/service"
	"dconnector/internal/paths"
)

// ---------------------------------------------------------------------------------------------
// Entorno: almacén SQLite real en un directorio temporal, adaptador falso y servicios reales.

type env struct {
	t         *testing.T
	app       port.AppStore
	bus       *bus.Bus
	router    *service.Router
	accounts  *service.Accounts
	messaging *service.Messaging
	dsts      *service.Destinations
	mgr       *Manager
	adapters  chan *fake.Adapter
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if err := paths.InitAt(t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	app, err := sqlite.OpenApp(paths.AppDBPath())
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	router := service.NewRouter(app, b)
	adapters := make(chan *fake.Adapter, 8)
	reg := map[model.Channel]port.AdapterFactory{model.ChannelWhatsApp: fake.Factory(adapters)}
	accounts := service.NewAccounts(app, router, b, reg, func(id model.AccountID) (port.AccountStore, *sql.DB, error) {
		return sqlite.OpenAccount(id, paths.AccountDBPath(id))
	})
	messaging := service.NewMessaging(accounts, router, b, app)
	dsts := service.NewDestinations(app)
	mgr := NewManager(dsts, Services{
		Accounts:  accounts,
		Inbox:     service.NewInbox(accounts),
		Messaging: messaging,
		Statuses:  service.NewStatuses(accounts, router, app),
		Contacts:  service.NewContacts(app, accounts),
	}, b, app, nil)
	mgr.backoffMin, mgr.backoffMax = 20*time.Millisecond, 100*time.Millisecond
	mgr.welcomeWait = 300 * time.Millisecond
	router.SetOutbox(mgr)

	e := &env{t: t, app: app, bus: b, router: router, accounts: accounts, messaging: messaging, dsts: dsts, mgr: mgr, adapters: adapters}
	t.Cleanup(func() {
		mgr.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = accounts.StopAll(ctx)
		messaging.Close()
		_ = app.Close()
	})
	return e
}

// newAccount crea una cuenta con adaptador falso y espera a que esté conectada.
func (e *env) newAccount(label string) (model.Account, *fake.Adapter) {
	e.t.Helper()
	acc, err := e.accounts.Create(context.Background(), model.ChannelWhatsApp, label, "", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	var ad *fake.Adapter
	select {
	case ad = <-e.adapters:
	case <-time.After(5 * time.Second):
		e.t.Fatal("el adaptador falso no se creó")
	}
	waitFor(e.t, "cuenta conectada", func() bool {
		return e.accounts.Status(acc.ID).State == model.StateConnected
	})
	return acc, ad
}

// addDest guarda un destino habilitado que apunta al servidor de prueba.
func (e *env) addDest(ts *testServer, allowCommands bool, f model.DestinationFilter) model.Destination {
	e.t.Helper()
	d, err := e.dsts.Save(context.Background(), model.Destination{
		Name: "prueba", URL: ts.wsURL(), Token: ts.token, Enabled: true, AllowCommands: allowCommands, Filter: f,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *env) start() {
	e.mgr.Start(context.Background())
}

func (e *env) pending(id string) int {
	n, err := e.app.OutboxCount(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) stateOf(id string) model.DestinationState {
	return e.mgr.stateOf(context.Background(), id)
}

// allFilter acepta todo lo que el protocolo puede mandar.
var allFilter = model.DestinationFilter{IncludeFromMe: true, IncludeGroups: true}

// ---------------------------------------------------------------------------------------------
// Servidor de prueba

type testServer struct {
	t     *testing.T
	srv   *httptest.Server
	token string

	mu      sync.Mutex
	conns   int
	headers []http.Header
	frames  []recorded
	cur     *websocket.Conn
	reject  int
	welcome any
	ackFn   func(conn int, f frameIn) bool
	custom  func(w http.ResponseWriter, r *http.Request)
}

type recorded struct {
	conn int
	f    frameIn
}

func newTestServer(t *testing.T, token string) *testServer {
	ts := &testServer{t: t, token: token, ackFn: func(int, frameIn) bool { return true }}
	ts.srv = httptest.NewServer(http.HandlerFunc(ts.handle))
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *testServer) wsURL() string { return "ws" + strings.TrimPrefix(ts.srv.URL, "http") + "/" }

func (ts *testServer) setReject(code int) {
	ts.mu.Lock()
	ts.reject = code
	ts.mu.Unlock()
}

func (ts *testServer) setWelcome(data any) {
	ts.mu.Lock()
	ts.welcome = data
	ts.mu.Unlock()
}

func (ts *testServer) setAck(fn func(conn int, f frameIn) bool) {
	ts.mu.Lock()
	ts.ackFn = fn
	ts.mu.Unlock()
}

func (ts *testServer) handle(w http.ResponseWriter, r *http.Request) {
	ts.mu.Lock()
	ts.conns++
	n := ts.conns
	ts.headers = append(ts.headers, r.Header.Clone())
	rej, tok, custom := ts.reject, ts.token, ts.custom
	ts.mu.Unlock()
	if custom != nil {
		custom(w, r)
		return
	}
	if rej != 0 {
		w.WriteHeader(rej)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+tok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(maxFrameSize)
	ts.mu.Lock()
	ts.cur = c
	welcome := ts.welcome
	ts.mu.Unlock()
	ctx := context.Background()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var f frameIn
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		ts.mu.Lock()
		ts.frames = append(ts.frames, recorded{conn: n, f: f})
		ack := ts.ackFn
		ts.mu.Unlock()
		switch {
		case f.Kind == kindHello && welcome != nil:
			_ = ts.writeTo(c, map[string]any{"v": 1, "id": "w1", "kind": "welcome", "data": welcome})
		case f.Kind == kindEvent && f.Name != nameMediaChunk && ack(n, f):
			_ = ts.writeTo(c, map[string]any{"v": 1, "id": "a-" + f.ID, "kind": "ack", "data": map[string]any{"ids": []string{f.ID}}})
		}
	}
}

func (ts *testServer) writeTo(c *websocket.Conn, v any) error {
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Write(ctx, websocket.MessageText, b)
}

// send manda un frame por la conexión más reciente.
func (ts *testServer) send(v any) {
	ts.t.Helper()
	ts.mu.Lock()
	c := ts.cur
	ts.mu.Unlock()
	if c == nil {
		ts.t.Fatal("no hay conexión abierta")
	}
	if err := ts.writeTo(c, v); err != nil {
		ts.t.Fatalf("no se pudo enviar al cliente: %v", err)
	}
}

// command manda un comando y espera su result.
func (ts *testServer) command(id, name, account string, data any) resultData {
	ts.t.Helper()
	f := map[string]any{"v": 1, "id": id, "kind": "command", "name": name, "data": data}
	if account != "" {
		f["account"] = map[string]any{"id": account}
	}
	ts.send(f)
	return ts.waitResult(id)
}

func (ts *testServer) waitResult(id string) resultData {
	ts.t.Helper()
	var got frameIn
	waitFor(ts.t, "result de "+id, func() bool {
		for _, r := range ts.snapshot() {
			if r.f.Kind == kindResult && r.f.ID == id {
				got = r.f
				return true
			}
		}
		return false
	})
	var rd struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *wireError      `json:"error"`
	}
	if err := json.Unmarshal(got.Data, &rd); err != nil {
		ts.t.Fatalf("result ilegible: %v", err)
	}
	return resultData{OK: rd.OK, Result: rd.Result, Error: rd.Error}
}

func (ts *testServer) closeCurrent() {
	ts.mu.Lock()
	c := ts.cur
	ts.mu.Unlock()
	if c != nil {
		_ = c.CloseNow()
	}
}

func (ts *testServer) snapshot() []recorded {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]recorded(nil), ts.frames...)
}

func (ts *testServer) connCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.conns
}

// eventsNamed devuelve los eventos recibidos con ese nombre (todas las conexiones).
func (ts *testServer) eventsNamed(name string) []recorded {
	var out []recorded
	for _, r := range ts.snapshot() {
		if r.f.Kind == kindEvent && r.f.Name == name {
			out = append(out, r)
		}
	}
	return out
}

func (ts *testServer) waitEvent(name string, pred func(frameIn) bool) frameIn {
	ts.t.Helper()
	var got frameIn
	waitFor(ts.t, "evento "+name, func() bool {
		for _, r := range ts.eventsNamed(name) {
			if pred == nil || pred(r.f) {
				got = r.f
				return true
			}
		}
		return false
	})
	return got
}

func (ts *testServer) waitHello() frameIn {
	ts.t.Helper()
	var got frameIn
	waitFor(ts.t, "hello", func() bool {
		for _, r := range ts.snapshot() {
			if r.f.Kind == kindHello {
				got = r.f
				return true
			}
		}
		return false
	})
	return got
}

// ---------------------------------------------------------------------------------------------
// Utilidades

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("tiempo agotado esperando: %s", what)
}

func jsonOf(t *testing.T, raw any) map[string]any {
	t.Helper()
	var b []byte
	switch v := raw.(type) {
	case json.RawMessage:
		b = v
	case []byte:
		b = v
	default:
		var err error
		if b, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("no es un objeto JSON: %s", b)
	}
	return m
}

// resultBytes devuelve el `result` de un resultData recibido.
func resultBytes(rd resultData) json.RawMessage {
	b, _ := rd.Result.(json.RawMessage)
	return b
}

func incoming(chat, id, text string) *model.Message {
	return &model.Message{
		NativeID: id, ChatNativeID: chat, SenderNativeID: chat, Timestamp: time.Now().UnixMilli(),
		Kind: model.KindText, Text: text, Delivery: model.DeliveryDelivered,
	}
}

func directChat(native, title string) *model.Chat {
	return &model.Chat{NativeID: native, Kind: model.ChatDirect, Title: title, PeerNativeID: native}
}
