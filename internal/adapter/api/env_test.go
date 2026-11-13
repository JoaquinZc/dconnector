package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dconnector/internal/adapter/protocol/ws"
	"dconnector/internal/adapter/store/sqlite"
	"dconnector/internal/app"
	"dconnector/internal/core/bus"
	"dconnector/internal/core/fake"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/service"
	"dconnector/internal/paths"
)

const (
	tokUser  = "tok"   // sesión remota
	tokLocal = "local" // token local (modo local)
	tokMedia = "media" // media-token (solo ?token= en GET de medios)
)

// fakeAuth: la Auth de prueba (la real es del agente B y no se importa aquí).
type fakeAuth struct {
	mu      sync.Mutex
	tickets map[string]string
}

func newFakeAuth() *fakeAuth { return &fakeAuth{tickets: map[string]string{}} }

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return t
	}
	return ""
}

func (a *fakeAuth) deny(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, codeUnauthorized, "Falta la sesión.")
}

func (a *fakeAuth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := bearer(r); t != tokUser && t != tokLocal {
			a.deny(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *fakeAuth) RequireSessionOrMedia(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := bearer(r)
		if t == tokUser || t == tokLocal || (t == "" && r.URL.Query().Get("token") == tokMedia && r.Method == http.MethodGet) {
			next.ServeHTTP(w, r)
			return
		}
		a.deny(w)
	})
}

func (a *fakeAuth) RedeemWSTicket(ticket string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.tickets[ticket]
	delete(a.tickets, ticket)
	return u, ok
}

func (a *fakeAuth) Username(r *http.Request) string {
	if bearer(r) == tokLocal {
		return "local"
	}
	return "tester"
}

func (a *fakeAuth) IsLocal(r *http.Request) bool { return bearer(r) == tokLocal }

func (a *fakeAuth) ticket(user string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := "t-" + model.NewID()
	a.tickets[t] = user
	return t
}

// env: núcleo real sobre SQLite con el adaptador falso, y la API en un servidor HTTP de prueba.
type env struct {
	t        *testing.T
	core     *app.Core
	srv      *Server
	ts       *httptest.Server
	auth     *fakeAuth
	adapters chan *fake.Adapter
	saved    chan service.AppSettings
	ready    func() string
	data     string
}

type envOpt func(*Config)

func newEnv(t *testing.T, opts ...envOpt) *env {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, paths.InitAt(dir, false))
	e := &env{t: t, auth: newFakeAuth(), adapters: make(chan *fake.Adapter, 8), saved: make(chan service.AppSettings, 4), data: dir}
	e.ready = func() string { return "ok" }

	appStore, err := sqlite.OpenApp(paths.AppDBPath())
	require.NoError(t, err)
	reg := map[model.Channel]port.AdapterFactory{}
	for _, ch := range model.AllChannels {
		reg[ch] = fake.Factory(e.adapters)
	}
	b := bus.New()
	r := service.NewRouter(appStore, b)
	acc := service.NewAccounts(appStore, r, b, reg, app.OpenAccountStore)
	msg := service.NewMessaging(acc, r, b, appStore)
	st := service.NewStatuses(acc, r, appStore)
	camp := service.NewCampaigns(appStore, acc, msg, b)
	r.AddObserver(camp)
	inbox := service.NewInbox(acc)
	contacts := service.NewContacts(appStore, acc)
	r.SetContactSync(contacts)
	dest := service.NewDestinations(appStore)
	mgr := ws.NewManager(dest, ws.Services{Accounts: acc, Inbox: inbox, Messaging: msg, Statuses: st, Contacts: contacts}, b, appStore, slog.Default())
	r.SetOutbox(mgr)
	e.core = &app.Core{
		App: appStore, Bus: b, Router: r, Accounts: acc, Inbox: inbox, Messaging: msg, Statuses: st,
		Contacts: contacts, Campaigns: camp, Scheduler: service.NewScheduler(appStore, msg, st, acc),
		Stats: service.NewStats(acc), Destinations: dest, Settings: service.NewSettings(appStore),
		Protocol: mgr, History: service.NewHistory(appStore, acc, b),
	}
	require.NoError(t, e.core.Settings.EnsureDefaults(context.Background()))

	cfg := Config{
		Core: e.core, Auth: e.auth, Version: "0.3.0-test",
		Ready:           func() string { return e.ready() },
		OnSettingsSaved: func(s service.AppSettings) { e.saved <- s },
		AuthRoutes: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]string{"auth": r.URL.Path})
		}),
	}
	for _, o := range opts {
		o(&cfg)
	}
	srv, err := New(cfg)
	require.NoError(t, err)
	e.srv = srv
	e.ts = httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		e.ts.Close()
		srv.Close()
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = acc.StopAll(ctx)
		msg.Close()
		_ = appStore.Close()
	})
	return e
}

// call hace POST /rpc/{svc}/{method} con el token remoto y devuelve el estado y el cuerpo.
func (e *env) call(token, svc, method string, args ...any) (int, []byte, http.Header) {
	e.t.Helper()
	return e.callH(token, nil, svc, method, args...)
}

func (e *env) callH(token string, hdr map[string]string, svc, method string, args ...any) (int, []byte, http.Header) {
	e.t.Helper()
	if args == nil {
		args = []any{}
	}
	body, err := json.Marshal(args)
	require.NoError(e.t, err)
	return e.raw(http.MethodPost, "/api/v1/rpc/"+svc+"/"+method, token, hdr, bytes.NewReader(body))
}

func (e *env) raw(method, path, token string, hdr map[string]string, body io.Reader) (int, []byte, http.Header) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.ts.URL+path, body)
	require.NoError(e.t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(e.t, err)
	return resp.StatusCode, b, resp.Header
}

// ok llama a un método y decodifica la respuesta 200 en out.
func (e *env) ok(out any, svc, method string, args ...any) {
	e.t.Helper()
	code, body, _ := e.call(tokUser, svc, method, args...)
	require.Equal(e.t, 200, code, string(body))
	if out != nil {
		require.NoError(e.t, json.Unmarshal(body, out), string(body))
	}
}

// errCode devuelve el código de error de una respuesta de error.
func errCode(t *testing.T, body []byte) (code, msg string) {
	t.Helper()
	var eb errorBody
	require.NoError(t, json.Unmarshal(body, &eb), string(body))
	return eb.Error.Code, eb.Error.Message
}

// newAccount crea una cuenta (por RPC) y espera su adaptador conectado.
func (e *env) newAccount() (model.Account, *fake.Adapter) {
	e.t.Helper()
	var acc model.Account
	e.ok(&acc, "Accounts", "Create", "whatsapp", "Cuenta de prueba", "#25D366", map[string]string{})
	var ad *fake.Adapter
	select {
	case ad = <-e.adapters:
	case <-time.After(5 * time.Second):
		e.t.Fatal("el adaptador falso no se creó")
	}
	require.Eventually(e.t, func() bool { return e.core.Accounts.Status(acc.ID).State == model.StateConnected }, 5*time.Second, 10*time.Millisecond)
	return acc, ad
}

// incoming inyecta un mensaje entrante de texto en el chat nativo dado.
func incoming(ad *fake.Adapter, chat, id, text string) {
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: &model.Message{
		NativeID: id, ChatNativeID: chat, SenderNativeID: chat, SenderName: "Ana",
		Timestamp: time.Now().UnixMilli(), Kind: model.KindText, Text: text,
	}, Chat: &model.Chat{NativeID: chat, Kind: model.ChatDirect, Title: "Ana", PeerNativeID: chat}})
}

func slogDiscard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
