package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/service"
	"dconnector/internal/paths"
)

func TestHealth(t *testing.T) {
	e := newEnv(t)
	code, body, h := e.raw("GET", "/api/v1/health", "", nil, nil)
	require.Equal(t, 200, code)
	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m))
	require.Equal(t, map[string]any{"status": "ok", "version": "0.3.0-test", "apiVersion": 1.0, "protocol": 1.0, "tls": false}, m)
	require.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	require.Equal(t, "no-referrer", h.Get("Referrer-Policy"))
	require.Equal(t, "no-store", h.Get("Cache-Control"))
	require.Empty(t, h.Get("Strict-Transport-Security"))

	e.ready = func() string { return "starting" }
	code, body, _ = e.raw("GET", "/api/v1/health", "", nil, nil)
	require.Equal(t, 503, code)
	require.Contains(t, string(body), `"starting"`)
	e.ready = func() string { return "stopping" }
	code, _, _ = e.raw("GET", "/api/v1/health", "", nil, nil)
	require.Equal(t, 503, code)

	code, _, _ = e.raw("POST", "/api/v1/health", "", nil, nil)
	require.Equal(t, 405, code)
}

func TestHSTSOnlyWithTLS(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.TLS = true })
	_, _, h := e.raw("GET", "/api/v1/health", "", nil, nil)
	require.Equal(t, "max-age=31536000", h.Get("Strict-Transport-Security"))
}

func TestRPCRequiresSession(t *testing.T) {
	e := newEnv(t)
	code, body, _ := e.call("", "Settings", "Version")
	require.Equal(t, 401, code)
	c, _ := errCode(t, body)
	require.Equal(t, "unauthorized", c)
	code, _, _ = e.call("malo", "Settings", "Version")
	require.Equal(t, 401, code)
	// Ni las rutas de medios ni /files sin sesión.
	for _, p := range []string{"/api/v1/media/a/b", "/api/v1/avatar/a/b.jpg", "/api/v1/files/01JC0000000000000000000000"} {
		code, _, _ = e.raw("GET", p, "", nil, nil)
		require.Equal(t, 401, code, p)
	}
	code, _, _ = e.raw("POST", "/api/v1/files", "", nil, nil)
	require.Equal(t, 401, code)
	// El media-token solo vale en GET de medios, avatares y archivos, nunca en /rpc.
	code, _, _ = e.raw("POST", "/api/v1/rpc/Settings/Version?token="+tokMedia, "", nil, strings.NewReader("[]"))
	require.Equal(t, 401, code)
}

func TestRPCBasicsAndShapes(t *testing.T) {
	e := newEnv(t)

	var ver string
	e.ok(&ver, "Settings", "Version")
	require.NotEmpty(t, ver)

	var dir string
	e.ok(&dir, "Settings", "DataDir")
	require.Equal(t, paths_DataDir(), dir)

	// Lista vacía: llega como null o [] (igual que en Wails).
	code, body, h := e.call(tokUser, "Accounts", "List")
	require.Equal(t, 200, code, string(body))
	require.Equal(t, "application/json; charset=utf-8", h.Get("Content-Type"))
	require.Contains(t, []string{"null", "[]"}, strings.TrimSpace(string(body)))

	// Sin cuerpo = sin argumentos.
	code, _, _ = e.raw("POST", "/api/v1/rpc/Settings/Version", tokUser, nil, nil)
	require.Equal(t, 200, code)

	// Método sin valor: null.
	acc, _ := e.newAccount()
	code, body, _ = e.call(tokUser, "Accounts", "Update", string(acc.ID), "Nuevo nombre", "#111111", true)
	require.Equal(t, 200, code, string(body))
	require.Equal(t, "null", strings.TrimSpace(string(body)))

	var views []service.AccountView
	e.ok(&views, "Accounts", "List")
	require.Len(t, views, 1)
	require.Equal(t, "Nuevo nombre", views[0].Account.Label)

	// Struct de retorno y argumento struct con etiquetas JSON.
	var st service.AppSettings
	e.ok(&st, "Settings", "Get")
	e.ok(nil, "Settings", "Save", st)
	select {
	case got := <-e.saved:
		require.Equal(t, st, got)
	case <-time.After(2 * time.Second):
		t.Fatal("OnSettingsSaved no se llamó")
	}

	// Métodos sin contexto ni error: States, ProtocolDoc, StatusAll.
	var states []model.DestinationState
	e.ok(&states, "Destinations", "States")
	var doc string
	e.ok(&doc, "Destinations", "ProtocolDoc")
	require.NotEmpty(t, doc)
	var hs []service.HistoryStatus
	e.ok(&hs, "History", "StatusAll")
	var page struct {
		Contacts []model.Contact `json:"contacts"`
		Total    int             `json:"total"`
	}
	e.ok(&page, "Contacts", "List", model.ContactFilter{})

	// Los campos desconocidos se ignoran.
	code, body, _ = e.raw("POST", "/api/v1/rpc/Contacts/List", tokUser, nil, strings.NewReader(`[{"limit":5,"campoNuevo":1}]`))
	require.Equal(t, 200, code, string(body))

	// PreviewRecurrence: sin contexto y con (T, error).
	code, body, _ = e.call(tokUser, "Scheduled", "PreviewRecurrence", model.Recurrence{}, time.Now().Add(time.Hour).UnixMilli(), 3)
	require.NotEqual(t, 500, code, string(body))
}

func paths_DataDir() string { return paths.DataDir() }

func TestRPCMessagingFlow(t *testing.T) {
	e := newEnv(t)
	acc, ad := e.newAccount()
	incoming(ad, "593999", "m1", "hola")

	var chats []model.Chat
	require.Eventually(t, func() bool {
		e.ok(&chats, "Inbox", "ListChats", model.ChatFilter{})
		return len(chats) == 1
	}, 5*time.Second, 20*time.Millisecond)
	key := string(chats[0].Key)

	var chat model.Chat
	e.ok(&chat, "Inbox", "GetChat", key)
	require.Equal(t, chats[0].Key, chat.Key)

	var pg model.MessagePage
	e.ok(&pg, "Inbox", "Messages", key, 0, 50)
	require.Len(t, pg.Messages, 1)

	var totals map[string]int
	e.ok(&totals, "Inbox", "UnreadTotals")
	require.Equal(t, 1, totals[string(acc.ID)])

	var sent model.Message
	e.ok(&sent, "Messaging", "Send", key, model.Outgoing{Kind: model.KindText, Text: "respuesta", ClientRef: "r1"})
	require.True(t, sent.FromMe)
	require.Equal(t, "respuesta", sent.Text)
	require.Len(t, ad.Sent(), 1)
	require.Equal(t, "respuesta", ad.Sent()[0].Text)

	e.ok(nil, "Messaging", "MarkRead", key)
	e.ok(nil, "Inbox", "Pin", key, true)

	var found []model.Message
	e.ok(&found, "Inbox", "Search", "respuesta", []string{string(acc.ID)}, 10)
}

func TestRPCContactsCampaignsScheduledStats(t *testing.T) {
	e := newEnv(t)
	var ct model.Contact
	e.ok(&ct, "Contacts", "Save", model.Contact{Name: "Pedro"})
	require.NotEmpty(t, ct.ID)
	var got model.Contact
	e.ok(&got, "Contacts", "Get", ct.ID)
	require.Equal(t, "Pedro", got.Name)
	e.ok(nil, "Contacts", "Delete", ct.ID)
	code, body, _ := e.call(tokUser, "Contacts", "Get", ct.ID)
	require.Equal(t, 404, code, string(body))

	var lists []model.ContactList
	e.ok(&lists, "Contacts", "Lists")
	var camps []model.Campaign
	e.ok(&camps, "Campaigns", "List")
	var sch []model.Scheduled
	e.ok(&sch, "Scheduled", "List", 0, time.Now().Add(24*time.Hour).UnixMilli())

	acc, _ := e.newAccount()
	var pacing model.PacingSettings
	e.ok(&pacing, "Campaigns", "GetPacing", string(acc.ID))
	var authors []model.StatusAuthor
	e.ok(&authors, "Statuses", "Authors", []string{string(acc.ID)})
	code, body, _ = e.call(tokUser, "Stats", "Conversations", []string{string(acc.ID)}, map[string]any{})
	require.Equal(t, 200, code, string(body))
}

func TestRPCErrorCodes(t *testing.T) {
	e := newEnv(t)

	// 404 no_found: un recurso que no existe (port.ErrNotFound).
	code, body, _ := e.call(tokUser, "Inbox", "GetChat", "cuenta|nada")
	require.Equal(t, 404, code, string(body))
	c, msg := errCode(t, body)
	require.Equal(t, "not_found", c)
	require.NotEmpty(t, msg)

	// 400 invalid: número de argumentos, tipos, JSON y estructura.
	for name, raw := range map[string]string{
		"pocos":    `[]`,
		"muchos":   `["a","b"]`,
		"tipo":     `[5]`,
		"no array": `{"a":1}`,
		"roto":     `[`,
		"basura":   `["a"] x`,
	} {
		code, body, _ = e.raw("POST", "/api/v1/rpc/Inbox/GetChat", tokUser, nil, strings.NewReader(raw))
		require.Equal(t, 400, code, name+": "+string(body))
		c, _ = errCode(t, body)
		require.Equal(t, "invalid", c, name)
	}

	// 404 para servicio o método inexistente; ruta mal formada.
	for _, p := range []string{"/rpc/Nada/Hace", "/rpc/Inbox/Nada", "/rpc/Inbox", "/rpc/Inbox/GetChat/extra", "/rpc/inbox/GetChat", "/rpc//GetChat"} {
		code, body, _ = e.raw("POST", "/api/v1"+p, tokUser, nil, strings.NewReader("[]"))
		require.Equal(t, 404, code, p)
		c, _ = errCode(t, body)
		require.Equal(t, "not_found", c, p)
	}
	// Los métodos no exportados o de ayuda (ConnClosed, Services) no existen.
	for _, m := range [][2]string{{"Notifications", "dropConn"}, {"Notifications", "ConnClosed"}, {"Facade", "Services"}} {
		code, _, _ = e.call(tokUser, m[0], m[1])
		require.Equal(t, 404, code, m[0]+"/"+m[1])
	}

	// 405: método distinto de POST.
	code, _, h := e.raw("GET", "/api/v1/rpc/Settings/Version", tokUser, nil, nil)
	require.Equal(t, 405, code)
	require.Equal(t, "POST", h.Get("Allow"))

	// 404 not_available: métodos de escritorio (§4.3).
	for _, m := range [][2]string{
		{"Files", "PickFiles"}, {"Files", "SaveTemp"}, {"Files", "Info"}, {"Messaging", "SaveMediaAs"},
		{"Settings", "OpenDataDir"}, {"Settings", "OpenLogsDir"}, {"Notifications", "Status"},
		{"Notifications", "RequestPermission"}, {"Notifications", "SendTest"},
		{"Notifications", "OpenSystemSettings"}, {"Notifications", "MarkAsked"},
	} {
		code, body, _ = e.call(tokUser, m[0], m[1])
		require.Equal(t, 404, code, m[0]+"/"+m[1])
		c, _ = errCode(t, body)
		require.Equal(t, "not_available", c, m[0]+"/"+m[1])
	}

	// 413: cuerpo de más de 1 MiB.
	big := `["` + strings.Repeat("a", 1<<20) + `"]`
	code, body, _ = e.raw("POST", "/api/v1/rpc/Inbox/GetChat", tokUser, nil, strings.NewReader(big))
	require.Equal(t, 413, code, string(body))
	c, _ = errCode(t, body)
	require.Equal(t, "too_large", c)
}

func TestRPCNotConnectedAndUnsupported(t *testing.T) {
	e := newEnv(t)
	acc, _ := e.newAccount()
	var chat model.Chat
	e.ok(&chat, "Inbox", "OpenChat", string(acc.ID), "593111")
	e.ok(nil, "Accounts", "Disconnect", string(acc.ID))
	require.Eventually(t, func() bool { return e.core.Accounts.Status(acc.ID).State != model.StateConnected }, 5*time.Second, 10*time.Millisecond)
	code, body, _ := e.call(tokUser, "Messaging", "Send", string(chat.Key), model.Outgoing{Kind: model.KindText, Text: "x"})
	require.Equal(t, 409, code, string(body))
	c, msg := errCode(t, body)
	require.Equal(t, "not_connected", c)
	require.NotEmpty(t, msg)
}

func TestWriteServiceErrorMapping(t *testing.T) {
	type tc struct {
		err    error
		status int
		code   string
	}
	cases := []tc{
		{fmt.Errorf("el número no es válido: %w", port.ErrInvalid), 400, "invalid"},
		{fmt.Errorf("x: %w", port.ErrNotFound), 404, "not_found"},
		{port.ErrNotConnected, 409, "not_connected"},
		{fmt.Errorf("x: %w", port.ErrUnsupported), 422, "unsupported"},
		{port.ErrRateLimited, 429, "rate_limited"},
		{errors.New("pq: password=hunter2 at C:\\secret\\ruta"), 500, "internal"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeServiceError(rec, slogDiscard(), "t", c.err)
		require.Equal(t, c.status, rec.Code)
		code, msg := errCode(t, rec.Body.Bytes())
		require.Equal(t, c.code, code)
		if c.code == "internal" {
			require.NotContains(t, msg, "hunter2")
			require.NotContains(t, msg, "secret")
		}
	}
	// Mensaje conservado.
	rec := httptest.NewRecorder()
	writeServiceError(rec, slogDiscard(), "t", fmt.Errorf("El número no es válido: %w", port.ErrInvalid))
	_, msg := errCode(t, rec.Body.Bytes())
	require.Contains(t, msg, "El número no es válido")

	// Retry-After por defecto y el que trae el error.
	rec = httptest.NewRecorder()
	writeServiceError(rec, slogDiscard(), "t", port.ErrRateLimited)
	require.Equal(t, "5", rec.Header().Get("Retry-After"))
	rec = httptest.NewRecorder()
	writeServiceError(rec, slogDiscard(), "t", waitErr{d: 90 * time.Second})
	require.Equal(t, "90", rec.Header().Get("Retry-After"))
	require.Equal(t, 429, rec.Code)
}

type waitErr struct{ d time.Duration }

func (w waitErr) Error() string             { return "espera" }
func (w waitErr) Unwrap() error             { return port.ErrRateLimited }
func (w waitErr) RetryAfter() time.Duration { return w.d }

type tupleSvc struct{}

func (tupleSvc) Pair(x int) (int, string, error)               { return x, "dos", nil }
func (tupleSvc) Nothing()                                      {}
func (tupleSvc) Boom() (string, error)                         { return "", errors.New("detalle interno") }
func (tupleSvc) Panic()                                        { panic("ay") }
func (tupleSvc) Only() error                                   { return nil }
func (tupleSvc) Two(a string, b []int) (map[string]int, error) { return map[string]int{a: len(b)}, nil }

func TestReflectionReturnShapes(t *testing.T) {
	rpc := buildRPC(map[string]any{"T": tupleSvc{}})
	m := rpc["T"]
	res, err := m["Pair"].call(context.Background(), []reflect.Value{reflect.ValueOf(7)})
	require.NoError(t, err)
	b, _ := json.Marshal(res)
	require.JSONEq(t, `[7,"dos"]`, string(b))
	res, err = m["Nothing"].call(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, res)
	res, err = m["Only"].call(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, res)
	_, err = m["Boom"].call(context.Background(), nil)
	require.Error(t, err)
	_, err = m["Panic"].call(context.Background(), nil)
	require.ErrorContains(t, err, "pánico")

	args, err := decodeArgs(m["Two"], []byte(`["k",[1,2,3]]`))
	require.NoError(t, err)
	res, err = m["Two"].call(context.Background(), args)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"k": 3}, res)
}

// El catálogo de servicios y métodos es el de las fachadas de Wails menos los de escritorio (docs/14 §4).
func TestCatalog(t *testing.T) {
	e := newEnv(t)
	got := map[string][]string{}
	for svc, ms := range e.srv.rpc {
		for name := range ms {
			got[svc] = append(got[svc], name)
		}
		sort.Strings(got[svc])
	}
	want := map[string][]string{
		"Accounts":      {"BeginPairing", "Capabilities", "Connect", "Create", "Delete", "Disconnect", "List", "Logout", "Reorder", "SetConfig", "SubmitPairing", "Update"},
		"Inbox":         {"Archive", "GetChat", "ListChats", "LoadOlder", "Messages", "Mute", "OpenChat", "Pin", "Search", "UnreadTotals"},
		"Messaging":     {"Delete", "Edit", "Forward", "MarkRead", "React", "Send", "SetTyping", "VotePoll"},
		"Statuses":      {"Authors", "ByAuthor", "Delete", "MarkViewed", "Mine", "Post", "React", "Stats", "Viewers"},
		"Contacts":      {"Delete", "DeleteList", "FromChat", "Get", "ImportCSV", "ImportFromAccount", "List", "Lists", "Merge", "Save", "SaveList", "SetListMembers", "SplitIdentity", "SuggestMerges", "SyncAll"},
		"Campaigns":     {"Cancel", "Delete", "Get", "GetPacing", "Launch", "List", "Pause", "Queue", "Resume", "Review", "Save", "SetPacing"},
		"Scheduled":     {"Cancel", "CancelSeries", "Delete", "DeleteSeries", "List", "ListForChat", "PreviewRecurrence", "Save", "SendNow", "Update", "UpdateSeries"},
		"Stats":         {"Chat", "Conversations", "Groups"},
		"Destinations":  {"Delete", "List", "ProtocolDoc", "Save", "States", "Test"},
		"Settings":      {"DataDir", "Get", "Save", "Version"},
		"History":       {"LoadMore", "Reset", "Start", "Status", "StatusAll", "Stop"},
		"Notifications": {"SetFocus"},
	}
	require.Equal(t, want, got)
}

// Un argumento no puede colar rutas del servidor en un error visible.
func TestErrorMessagesHideDataDir(t *testing.T) {
	e := newEnv(t)
	rec := httptest.NewRecorder()
	writeServiceError(rec, slogDiscard(), "t", fmt.Errorf("no se pudo abrir %s/x: %w", e.data, port.ErrNotFound))
	_, msg := errCode(t, rec.Body.Bytes())
	require.NotContains(t, msg, e.data)
}

var _ = bytes.NewReader
var _ = http.StatusOK
