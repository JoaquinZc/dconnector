package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/service"
)

type frame struct {
	Event string          `json:"event"`
	Seq   int64           `json:"seq"`
	Data  json.RawMessage `json:"data"`
}

func (e *env) wsURL(ticket string) string {
	return "ws" + strings.TrimPrefix(e.ts.URL, "http") + "/api/v1/events?ticket=" + ticket
}

func (e *env) dial(user string, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, e.wsURL(e.auth.ticket(user)), &websocket.DialOptions{HTTPHeader: hdr})
}

func (e *env) connect() (*websocket.Conn, string) {
	e.t.Helper()
	c, _, err := e.dial("tester", nil)
	require.NoError(e.t, err)
	e.t.Cleanup(func() { _ = c.CloseNow() })
	f := readFrame(e.t, c, 5*time.Second)
	require.Equal(e.t, "hello", f.Event)
	var h struct {
		ConnID string `json:"connId"`
	}
	require.NoError(e.t, json.Unmarshal(f.Data, &h))
	return c, h.ConnID
}

func readFrame(t *testing.T, c *websocket.Conn, d time.Duration) frame {
	t.Helper()
	f, err := tryFrame(c, d)
	require.NoError(t, err)
	return f
}

func tryFrame(c *websocket.Conn, d time.Duration) (frame, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		return frame{}, err
	}
	var f frame
	return f, json.Unmarshal(b, &f)
}

// waitFrame lee hasta hallar un evento con ese nombre (devuelve los vistos, para comprobar seq).
func waitFrame(t *testing.T, c *websocket.Conn, event string) frame {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		f, err := tryFrame(c, time.Until(deadline))
		require.NoError(t, err, "esperando el evento "+event)
		if f.Event == event {
			return f
		}
	}
	t.Fatalf("no llegó el evento %s", event)
	return frame{}
}

func TestEventsHelloAndSeq(t *testing.T) {
	e := newEnv(t)
	c, _, err := e.dial("tester", nil)
	require.NoError(t, err)
	defer c.CloseNow()
	f := readFrame(t, c, 5*time.Second)
	require.Equal(t, "hello", f.Event)
	require.EqualValues(t, 0, f.Seq)
	var h map[string]any
	require.NoError(t, json.Unmarshal(f.Data, &h))
	require.Equal(t, "0.3.0-test", h["version"])
	require.EqualValues(t, 1, h["apiVersion"])
	require.EqualValues(t, 1, h["protocol"])
	require.Equal(t, "tester", h["user"])
	require.NotEmpty(t, h["connId"])
	require.InDelta(t, time.Now().UnixMilli(), h["serverTime"], 10000)

	// Eventos del núcleo: llegan con seq creciente (1, 2, …).
	_, ad := e.newAccount()
	e.tick()
	incoming(ad, "593999", "m1", "hola")
	var last int64
	seen := map[string]frame{}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && (seen["chat:upsert"].Event == "" || seen["message:upsert"].Event == "" || seen["unread:total"].Event == "") {
		f, err := tryFrame(c, time.Until(deadline))
		require.NoError(t, err)
		require.Greater(t, f.Seq, last, "seq crece de uno en uno")
		require.EqualValues(t, last+1, f.Seq)
		last = f.Seq
		seen[f.Event] = f
	}
	require.NotEmpty(t, seen["account:status"].Event)
	var chats []model.Chat
	require.NoError(t, json.Unmarshal(seen["chat:upsert"].Data, &chats))
	require.Len(t, chats, 1)
	var msgs []model.Message
	require.NoError(t, json.Unmarshal(seen["message:upsert"].Data, &msgs))
	require.Len(t, msgs, 1)
	require.Equal(t, "hola", msgs[0].Text)
	var un struct {
		Total     int            `json:"total"`
		ByAccount map[string]int `json:"byAccount"`
	}
	require.NoError(t, json.Unmarshal(seen["unread:total"].Data, &un))
	require.Equal(t, 1, un.Total)
}

func TestEventsBatchedAndUnreadThrottled(t *testing.T) {
	e := newEnv(t)
	c, _ := e.connect()
	_, ad := e.newAccount()
	for i := 0; i < 20; i++ {
		incoming(ad, "593999", "m"+string(rune('a'+i)), "x")
	}
	// Los 20 mensajes llegan agrupados en pocos lotes (100 ms) y el total de no leídos sale como mucho 1/s.
	total, batches, unreads := 0, 0, 0
	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		f, err := tryFrame(c, time.Until(deadline))
		if err != nil {
			break
		}
		switch f.Event {
		case "message:upsert":
			var ms []model.Message
			require.NoError(t, json.Unmarshal(f.Data, &ms))
			total += len(ms)
			batches++
		case "unread:total":
			unreads++
		}
	}
	require.Equal(t, 20, total)
	require.Less(t, batches, 20)
	require.LessOrEqual(t, unreads, 3)
	require.GreaterOrEqual(t, unreads, 1)
}

func TestEventsAuth(t *testing.T) {
	e := newEnv(t)
	url := func(q string) string { return e.ts.URL + "/api/v1/events" + q }
	do := func(method, u string, hdr map[string]string) (int, string) {
		req, _ := http.NewRequest(method, u, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		b := make([]byte, 512)
		n, _ := resp.Body.Read(b)
		return resp.StatusCode, string(b[:n])
	}
	up := map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="}

	// Sin ticket o con ticket falso: 401 antes del upgrade.
	code, body := do("GET", url(""), up)
	require.Equal(t, 401, code)
	require.Contains(t, body, `"unauthorized"`)
	code, _ = do("GET", url("?ticket=falso"), up)
	require.Equal(t, 401, code)
	// El token de sesión no vale como ticket.
	code, _ = do("GET", url("?ticket="+tokUser), up)
	require.Equal(t, 401, code)
	// POST: 405. Sin Upgrade: 400.
	code, _ = do("POST", url("?ticket=x"), nil)
	require.Equal(t, 405, code)
	code, _ = do("GET", url("?ticket="+e.auth.ticket("tester")), nil)
	require.Equal(t, 400, code)

	// Un ticket vale una sola vez.
	tk := e.auth.ticket("tester")
	c, _, err := websocket.Dial(context.Background(), e.wsURL(tk), nil)
	require.NoError(t, err)
	defer c.CloseNow()
	_, resp, err := websocket.Dial(context.Background(), e.wsURL(tk), nil)
	require.Error(t, err)
	require.Equal(t, 401, resp.StatusCode)
}

func TestEventsOrigin(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AllowOrigins = []string{"https://panel.ejemplo.com"} })
	host := strings.TrimPrefix(e.ts.URL, "http://")
	for origin, wantOK := range map[string]bool{
		"":                          true, // clientes nativos
		"http://" + host:            true, // el mismo host
		"http://wails.localhost":    true,
		"https://wails.localhost":   true,
		"https://panel.ejemplo.com": true, // --allow-origin
		"https://otro.com":          false,
		"http://localhost:5173":     false,
		"null":                      false,
	} {
		hdr := http.Header{}
		if origin != "" {
			hdr.Set("Origin", origin)
		}
		c, resp, err := e.dial("tester", hdr)
		if wantOK {
			require.NoError(t, err, origin)
			_ = c.CloseNow()
		} else {
			require.Error(t, err, origin)
			require.Equal(t, 403, resp.StatusCode, origin)
		}
	}
}

func TestEventsMaxEightPerUser(t *testing.T) {
	e := newEnv(t)
	var cs []*websocket.Conn
	for i := 0; i < 8; i++ {
		c, _, err := e.dial("tester", nil)
		require.NoError(t, err)
		cs = append(cs, c)
	}
	_, resp, err := e.dial("tester", nil)
	require.Error(t, err)
	require.Equal(t, 429, resp.StatusCode)
	require.NotEmpty(t, resp.Header.Get("Retry-After"))
	// Otro usuario no se ve afectado.
	c, _, err := e.dial("otro", nil)
	require.NoError(t, err)
	_ = c.CloseNow()
	// Al cerrar una, vuelve a haber hueco.
	_ = cs[0].CloseNow()
	require.Eventually(t, func() bool {
		c, _, err := e.dial("tester", nil)
		if err == nil {
			_ = c.CloseNow()
		}
		return err == nil
	}, 5*time.Second, 50*time.Millisecond)
	for _, c := range cs[1:] {
		_ = c.CloseNow()
	}
}

func TestEventsCloseOnShutdownIs1001(t *testing.T) {
	e := newEnv(t)
	c, _ := e.connect()
	go e.srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			require.Equal(t, websocket.StatusGoingAway, websocket.CloseStatus(err), err.Error())
			break
		}
	}
	// Tras apagar, no se aceptan conexiones nuevas.
	code, _, _ := e.raw("GET", "/api/v1/events?ticket="+e.auth.ticket("tester"), "", map[string]string{"Upgrade": "websocket"}, nil)
	require.Equal(t, 503, code)
}

func TestEventsPingKeepsAliveAndDropsDeadPeers(t *testing.T) {
	e := newEnv(t)
	e.srv.hub.pingEvery, e.srv.hub.pongWait = 50*time.Millisecond, 200*time.Millisecond
	c, _ := e.connect()
	// Mientras el cliente lee contesta los ping (la biblioteca lo hace sola) y la conexión sigue viva.
	frames := make(chan frame, 16)
	go func() {
		for {
			f, err := tryFrame(c, 10*time.Second)
			if err != nil {
				close(frames)
				return
			}
			frames <- f
		}
	}()
	time.Sleep(500 * time.Millisecond)
	e.srv.hub.broadcast("typing", map[string]any{"x": 1})
	select {
	case f, ok := <-frames:
		require.True(t, ok, "la conexión viva no debe cerrarse")
		require.Equal(t, "typing", f.Event)
	case <-time.After(3 * time.Second):
		t.Fatal("no llegó el evento")
	}

	// Una conexión que no lee no contesta los ping: el servidor la cierra al pasar el plazo.
	c2, _ := e.connect()
	_ = c2
	require.Eventually(t, func() bool {
		e.srv.hub.mu.Lock()
		defer e.srv.hub.mu.Unlock()
		return len(e.srv.hub.conns) == 1
	}, 5*time.Second, 50*time.Millisecond)
}

func TestEventsSlowClientDropped(t *testing.T) {
	old := connQueueSize
	connQueueSize = 4
	t.Cleanup(func() { connQueueSize = old })
	e := newEnv(t)
	c, _, err := e.dial("tester", nil) // no lee: su cola se llena
	require.NoError(t, err)
	defer c.CloseNow()
	big := strings.Repeat("x", 512<<10)
	require.Eventually(t, func() bool {
		e.srv.hub.broadcast("typing", map[string]any{"pad": big})
		e.srv.hub.mu.Lock()
		defer e.srv.hub.mu.Unlock()
		return len(e.srv.hub.conns) == 0
	}, 20*time.Second, 20*time.Millisecond)
}

func TestDestinationStateEvent(t *testing.T) {
	e := newEnv(t)
	c, _ := e.connect()
	e.srv.hub.broadcast(EvtDestinationState, model.DestinationState{DestinationID: "d1"})
	f := waitFrame(t, c, "destination:state")
	require.Contains(t, string(f.Data), "d1")
}

// ---------------------------------------------------------------------------------------------
// notify y foco por conexión

// clockSkew adelanta el reloj del servicio de avisos (el núcleo agrupa los avisos de un chat que llegan en 1,5 s).
var clockSkew atomic.Int64

func (e *env) tick() { clockSkew.Add(int64(5 * time.Second)) }

func (e *env) withNotifications() *service.Notifications {
	e.t.Helper()
	clockSkew.Store(0)
	n := service.NewNotifications(e.core.Bus, e.core.Settings, e.core.Inbox, e.srv.Notifier(),
		service.WithNotificationsClock(func() time.Time { return time.Now().Add(time.Duration(clockSkew.Load())) }))
	e.srv.SetNotifications(n)
	n.Start(context.Background())
	e.t.Cleanup(n.Stop)
	return n
}

func TestNotifierEvents(t *testing.T) {
	e := newEnv(t)
	c, _ := e.connect()
	nt := e.srv.Notifier()
	perm := nt.Permission(context.Background())
	require.Equal(t, port.NotifyGranted, perm.State)
	require.False(t, perm.Hidden())
	p, err := nt.RequestPermission(context.Background())
	require.NoError(t, err)
	require.Equal(t, port.NotifyGranted, p.State)
	require.NoError(t, nt.OpenSystemSettings())

	require.NoError(t, nt.Show(context.Background(), port.Notification{
		ID: "1", Title: "Ana", Body: "hola", ChatKey: "wa-01abc|593999", Silent: true,
	}))
	f := waitFrame(t, c, "notify")
	var n map[string]any
	require.NoError(t, json.Unmarshal(f.Data, &n))
	require.Equal(t, "wa-01abc|593999", n["chatKey"])
	require.Equal(t, "wa-01abc", n["accountId"])
	require.Equal(t, "whatsapp", n["channel"])
	require.Equal(t, "Ana", n["title"])
	require.Equal(t, "hola", n["body"])
	require.Equal(t, true, n["preview"])
	require.Equal(t, true, n["silent"])
	require.Equal(t, false, n["sound"])
	require.NotEmpty(t, n["reason"])

	require.NoError(t, nt.Show(context.Background(), port.Notification{Title: "Ana", Body: "Mensaje nuevo", ChatKey: "tg-01abc|1"}))
	f = waitFrame(t, c, "notify")
	require.NoError(t, json.Unmarshal(f.Data, &n))
	require.Equal(t, false, n["preview"])
	require.Equal(t, "telegram", n["channel"])

	require.NoError(t, nt.PlaySound(context.Background()))
	f = waitFrame(t, c, "notify")
	require.NoError(t, json.Unmarshal(f.Data, &n))
	require.Equal(t, true, n["sound"])
	require.Equal(t, true, n["silent"])
	require.Equal(t, "", n["title"])
}

func TestNotifyFromIncomingMessageAndDecision(t *testing.T) {
	fastDecisions(t)
	e := newEnv(t)
	e.withNotifications()
	c, _ := e.connect()
	acc, ad := e.newAccount()
	e.tick()
	incoming(ad, "593999", "m1", "hola desde Ana")
	f := waitFrame(t, c, "notify")
	var n map[string]any
	require.NoError(t, json.Unmarshal(f.Data, &n))
	require.Equal(t, string(acc.ID)+"|593999", n["chatKey"])
	require.Equal(t, "whatsapp", n["channel"])
	d := waitFrame(t, c, "notify:decision")
	var dec map[string]any
	require.NoError(t, json.Unmarshal(d.Data, &dec))
	require.Equal(t, true, dec["shown"])
	require.NotEmpty(t, dec["reason"])
}

func TestSetFocusPerConnection(t *testing.T) {
	fastDecisions(t)
	e := newEnv(t)
	e.withNotifications()
	c1, conn1 := e.connect()
	c2, conn2 := e.connect()
	require.NotEqual(t, conn1, conn2)
	acc, ad := e.newAccount()
	key := string(acc.ID) + "|593999"

	setFocus := func(conn, chat string, focused bool) {
		code, body, _ := e.callH(tokUser, map[string]string{connHeader: conn}, "Notifications", "SetFocus", chat, focused)
		require.Equal(t, 200, code, string(body))
		require.Equal(t, "null", strings.TrimSpace(string(body)))
	}
	expectDecision := func(c *websocket.Conn, shown bool) {
		t.Helper()
		d := waitFrame(t, c, "notify:decision")
		var dec struct {
			Shown  bool   `json:"shown"`
			Reason string `json:"reason"`
		}
		require.NoError(t, json.Unmarshal(d.Data, &dec))
		require.Equal(t, shown, dec.Shown, dec.Reason)
	}

	// La conexión 1 tiene ese chat abierto y enfocado: no se avisa.
	setFocus(conn1, key, true)
	e.tick()
	incoming(ad, "593999", "m1", "uno")
	expectDecision(c1, false)

	// La conexión 2 dice que no tiene nada abierto: sigue viéndolo la 1, no se avisa.
	setFocus(conn2, "", true)
	e.tick()
	incoming(ad, "593999", "m2", "dos")
	expectDecision(c1, false)

	// La 1 pierde el foco y la 2 no tiene el chat: ahora sí se avisa.
	setFocus(conn1, key, false)
	e.tick()
	incoming(ad, "593999", "m3", "tres")
	expectDecision(c1, true)

	// La 2 abre el chat con foco: de nuevo se ve.
	setFocus(conn2, key, true)
	e.tick()
	incoming(ad, "593999", "m4", "cuatro")
	expectDecision(c1, false)

	// Si la conexión 2 se cierra, su foco desaparece y se vuelve a avisar.
	_ = c2.CloseNow()
	require.Eventually(t, func() bool { return e.srv.facade.FocusConns() == 1 }, 5*time.Second, 20*time.Millisecond)
	e.tick()
	incoming(ad, "593999", "m5", "cinco")
	expectDecision(c1, true)
}

func TestCORS(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AllowOrigins = []string{"https://panel.ejemplo.com/"} })
	pre := func(origin string) (int, http.Header) {
		code, _, h := e.raw("OPTIONS", "/api/v1/rpc/Settings/Version", "", map[string]string{
			"Origin": origin, "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization,content-type,x-dconnector-conn",
		}, nil)
		return code, h
	}
	for _, o := range []string{"http://wails.localhost", "https://panel.ejemplo.com", e.ts.URL} {
		code, h := pre(o)
		require.Equal(t, 204, code, o)
		require.Equal(t, o, h.Get("Access-Control-Allow-Origin"))
		require.Contains(t, h.Get("Access-Control-Allow-Headers"), "Authorization")
		require.Contains(t, h.Get("Access-Control-Allow-Headers"), "Content-Type")
		require.Contains(t, h.Get("Access-Control-Allow-Headers"), "X-DConnector-Client")
		require.Contains(t, h.Get("Access-Control-Allow-Headers"), "X-DConnector-Conn")
		require.Contains(t, h.Get("Vary"), "Origin")
	}
	code, h := pre("https://malo.com")
	require.Equal(t, 403, code)
	require.Empty(t, h.Get("Access-Control-Allow-Origin"))
	// Sin Origin (cliente nativo): OPTIONS sin token también responde.
	code, _, _ = e.raw("OPTIONS", "/api/v1/rpc/Settings/Version", "", nil, nil)
	require.Equal(t, 204, code)

	// Una respuesta normal con origen permitido lleva las cabeceras CORS; con origen ajeno no.
	_, _, h = e.raw("POST", "/api/v1/rpc/Settings/Version", tokUser, map[string]string{"Origin": "http://wails.localhost"}, strings.NewReader("[]"))
	require.Equal(t, "http://wails.localhost", h.Get("Access-Control-Allow-Origin"))
	_, _, h = e.raw("POST", "/api/v1/rpc/Settings/Version", tokUser, map[string]string{"Origin": "https://malo.com"}, strings.NewReader("[]"))
	require.Empty(t, h.Get("Access-Control-Allow-Origin"))
}

func TestReadyGate(t *testing.T) {
	e := newEnv(t)
	e.ready = func() string { return "starting" }
	code, body, _ := e.call(tokUser, "Settings", "Version")
	require.Equal(t, 503, code)
	c, _ := errCode(t, body)
	require.Equal(t, "unavailable", c)
	code, _, _ = e.raw("GET", "/api/v1/media/a/b", tokUser, nil, nil)
	require.Equal(t, 503, code)
	// Sin sesión sigue siendo 401 (no revela el estado).
	code, _, _ = e.call("", "Settings", "Version")
	require.Equal(t, 401, code)
}

func TestAuthRoutesDelegatedAndUnknownRoutes(t *testing.T) {
	e := newEnv(t)
	code, body, _ := e.raw("POST", "/api/v1/auth/login", "", nil, strings.NewReader("{}"))
	require.Equal(t, 200, code)
	require.Contains(t, string(body), "/api/v1/auth/login")
	for _, p := range []string{"/api/v1/nada", "/api/v2/health", "/", "/api/v1/rpc", "/api/v1/media", "/api/v1/files/"} {
		code, body, _ = e.raw("GET", p, tokUser, nil, nil)
		require.Contains(t, []int{400, 404}, code, p)
		_, _ = errCode(t, body)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	e := newEnv(t)
	_, err = New(Config{Core: e.core})
	require.Error(t, err)
}

// fastDecisions acelera el vigía de decisiones de avisos (hay que llamarlo antes de newEnv).
func fastDecisions(t *testing.T) {
	old := decisionPoll
	decisionPoll = 20 * time.Millisecond
	t.Cleanup(func() { decisionPoll = old })
}
