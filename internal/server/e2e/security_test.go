package e2e

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// rawGet manda una petición con la línea de petición TAL CUAL (sin que el cliente de Go la normalice) y devuelve
// el estado y el cuerpo. Sirve para recorridos de ruta con %2e%2e, ..%5c, NUL, etc.
func (i *inst) rawReq(method, target string, hdr map[string]string, body string) (int, string) {
	i.t.Helper()
	conn, err := net.DialTimeout("tcp", i.addr, 5*time.Second)
	require.NoError(i.t, err)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n", method, target, i.addr)
	for k, v := range hdr {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	if body != "" {
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	}
	b.WriteString("\r\n" + body)
	_, err = conn.Write(b.Bytes())
	require.NoError(i.t, err)
	r, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return 0, err.Error() // el servidor cortó la conexión: también es una forma de rechazo
	}
	defer r.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	return r.StatusCode, string(data)
}

// Ninguna ruta responde sin autenticación salvo /health y /auth/login.
func TestNoRouteAnswersWithoutAuth(t *testing.T) {
	useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	_ = tok

	routes := []struct{ method, path string }{
		{"POST", "/api/v1/auth/logout"}, {"GET", "/api/v1/auth/me"}, {"POST", "/api/v1/auth/password"},
		{"POST", "/api/v1/auth/ws-ticket"}, {"POST", "/api/v1/auth/media-token"},
		{"POST", "/api/v1/rpc/Accounts/List"}, {"POST", "/api/v1/rpc/Settings/Get"}, {"POST", "/api/v1/rpc/Settings/DataDir"},
		{"POST", "/api/v1/rpc/Inbox/ListChats"}, {"POST", "/api/v1/rpc/Messaging/Send"}, {"POST", "/api/v1/rpc/Contacts/ImportCSV"},
		{"POST", "/api/v1/rpc/Nada/Nada"}, {"GET", "/api/v1/rpc/Accounts/List"}, {"POST", "/api/v1/rpc/"},
		{"POST", "/api/v1/files"}, {"GET", "/api/v1/files/01JZZZZZZZZZZZZZZZZZZZZZZZ"}, {"GET", "/api/v1/files/"},
		{"GET", "/api/v1/media/a/b"}, {"GET", "/api/v1/media/"}, {"GET", "/api/v1/avatar/a/b.jpg"},
		{"HEAD", "/api/v1/media/a/b"}, {"HEAD", "/api/v1/files/01JZZZZZZZZZZZZZZZZZZZZZZZ"},
	}
	for _, rt := range routes {
		code, body := s.rawReq(rt.method, rt.path, nil, "")
		require.Equal(t, 401, code, "%s %s -> %d %s", rt.method, rt.path, code, body)
	}
	// El WebSocket sin ticket es 401 (antes del upgrade).
	code, _ := s.rawReq("GET", "/api/v1/events", map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="}, "")
	require.Equal(t, 401, code)
	// Lo que no es de la API no existe (y no revela nada).
	for _, p := range []string{"/", "/index.html", "/api", "/api/v1", "/api/v1/", "/api/v2/health", "/debug/pprof/", "/metrics", "/.env", "/app.db"} {
		code, body := s.rawReq("GET", p, nil, "")
		require.Equal(t, 404, code, "%s -> %d %s", p, code, body)
	}
	// Los dos públicos sí responden.
	require.Equal(t, 200, s.do("GET", "/api/v1/health", "", nil, nil).Code)
	require.Equal(t, 400, s.postJSON("/api/v1/auth/login", "", map[string]string{}).Code)
	// Un token en la URL no abre /rpc ni /auth/*.
	require.Equal(t, 401, s.do("POST", "/api/v1/rpc/Accounts/List?token="+tok, "", strings.NewReader("[]"), nil).Code)
	require.Equal(t, 401, s.do("GET", "/api/v1/auth/me?token="+tok, "", nil, nil).Code)
	// Tokens absurdos.
	require.Equal(t, 401, s.rpcRaw(strings.Repeat("A", 5000), "Accounts", "List").Code)
	require.Equal(t, 401, s.rpcRaw("Bearer", "Accounts", "List").Code)
}

// OPTIONS y CORS: sin token para la verificación previa, pero solo con cabeceras para orígenes permitidos.
func TestCORSAndOptions(t *testing.T) {
	useFake(t, nil)
	cfg := baseCfg(t)
	cfg.AllowOrigin = "https://panel.ejemplo.com/"
	s := start(t, cfg)
	h := func(origin string) map[string]string {
		m := map[string]string{"Access-Control-Request-Method": "POST"}
		if origin != "" {
			m["Origin"] = origin
		}
		return m
	}
	// Orígenes permitidos: wails.localhost, el mismo host, el extra.
	for _, o := range []string{"http://wails.localhost", "http://" + s.addr, "https://panel.ejemplo.com"} {
		r := s.do("OPTIONS", "/api/v1/rpc/Accounts/List", "", nil, h(o))
		require.Equal(t, 204, r.Code, o)
		require.Equal(t, o, r.Header.Get("Access-Control-Allow-Origin"), o)
		require.NotContains(t, r.Header.Get("Access-Control-Allow-Origin"), "*")
		require.Empty(t, r.Body)
	}
	// Orígenes ajenos: 403 sin cabeceras CORS; «null» tampoco.
	for _, o := range []string{"https://malo.example", "null", "http://wails.localhost.malo.example", "file://", "http://127.0.0.1:1"} {
		r := s.do("OPTIONS", "/api/v1/rpc/Accounts/List", "", nil, h(o))
		require.Equal(t, 403, r.Code, o)
		require.Empty(t, r.Header.Get("Access-Control-Allow-Origin"), o)
	}
	// Una petición normal desde un origen ajeno no recibe Allow-Origin (el navegador no podría leer la respuesta).
	r := s.do("GET", "/api/v1/health", "", nil, map[string]string{"Origin": "https://malo.example"})
	require.Empty(t, r.Header.Get("Access-Control-Allow-Origin"))
	// OPTIONS sin Origin: 204 sin cabeceras CORS ni datos.
	r = s.do("OPTIONS", "/api/v1/rpc/Accounts/List", "", nil, nil)
	require.Equal(t, 204, r.Code)
	require.Empty(t, r.Header.Get("Access-Control-Allow-Origin"))
	// WebSocket desde un origen ajeno: 403 incluso con un ticket válido (el ticket no se gasta de balde).
	tok := s.login("admin", s.adminPassword())
	_, hr, err := s.wsDialOrigin(s.ticket(tok), "https://malo.example")
	require.Error(t, err)
	require.Equal(t, 403, hr.StatusCode)
	w, _, err := s.wsDialOrigin(s.ticket(tok), "http://wails.localhost")
	require.NoError(t, err)
	require.Equal(t, "hello", w.next().Event)
}

func (i *inst) wsDialOrigin(ticket, origin string) (*wsClient, *http.Response, error) {
	i.t.Helper()
	u := "ws://" + i.addr + "/api/v1/events?ticket=" + ticket
	c, hr, err := websocket.Dial(i.t.Context(), u, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
	if err != nil {
		return nil, hr, err
	}
	w := &wsClient{t: i.t, c: c, events: make(chan wsEvent, 16), closed: make(chan websocket.StatusCode, 1)}
	go func() {
		for {
			_, b, err := c.Read(i.t.Context())
			if err != nil {
				w.closed <- websocket.CloseStatus(err)
				return
			}
			var ev wsEvent
			if jsonUnmarshal(b, &ev) == nil {
				w.events <- ev
			}
		}
	}()
	i.t.Cleanup(func() { _ = c.CloseNow() })
	return w, hr, nil
}

// Recorridos de ruta y ids inválidos en /files, /media y /avatar: nunca 200 ni contenido del servidor.
func TestPathTraversalAndBadIDs(t *testing.T) {
	adapters := useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	acc, _ := newAccount(t, s, tok, adapters)
	auth := map[string]string{"Authorization": "Bearer " + tok}
	require.NoError(t, os.WriteFile(filepath.Join(s.cfg.Data, "secreto.txt"), []byte("SECRETO-DEL-SERVIDOR"), 0o600))

	evil := []string{
		"/api/v1/files/%2e%2e%2fapp.db", "/api/v1/files/..%5capp.db", "/api/v1/files/%2e%2e", "/api/v1/files/..", "/api/v1/files/../app.db",
		"/api/v1/files/%252e%252e%252fapp.db", "/api/v1/files/01JZZZZZZZZZZZZZZZZZZZZZZZ/../../app.db", "/api/v1/files/%00",
		"/api/v1/files/01JZZZZZZZZZZZZZZZZZZZZZZZ%00.txt", "/api/v1/files/01jzzzzzzzzzzzzzzzzzzzzzzz", "/api/v1/files/" + strings.Repeat("A", 4000),
		"/api/v1/media/" + string(acc.ID) + "/%2e%2e%2f..%2fsecreto.txt", "/api/v1/media/" + string(acc.ID) + "/..%5c..%5csecreto.txt",
		"/api/v1/media/..%2f/secreto.txt", "/api/v1/media/%2e%2e/%2e%2e", "/api/v1/media/" + string(acc.ID) + "/%00", "/api/v1/media/%00/x",
		"/api/v1/media/" + string(acc.ID) + "/..", "/api/v1/media//x",
		"/api/v1/avatar/" + string(acc.ID) + "/..%2f..%2fsecreto.txt", "/api/v1/avatar/" + string(acc.ID) + "/%2e%2e%5csecreto.txt",
		"/api/v1/avatar/..%2f/x", "/api/v1/avatar/" + string(acc.ID) + "/%00", "/api/v1/avatar/%2e%2e/%2e%2e/secreto.txt",
		"/api/v1/rpc/Accounts/%2e%2e%2f..%2fauth/me", "/api/v1/rpc/..%2f/Accounts/List", "/api/v1/rpc/Accounts/List/extra",
	}
	for _, p := range evil {
		code, body := s.rawReq("GET", p, auth, "")
		require.NotEqual(t, 200, code, "%s -> %d", p, code)
		require.NotContains(t, body, "SECRETO-DEL-SERVIDOR", p)
		require.NotContains(t, body, "SQLite format", p)
		require.NotContains(t, body, s.cfg.Data, "no revela rutas del servidor: "+p)
		if code != 0 {
			require.True(t, code >= 400 && code < 500, "%s -> %d %s", p, code, body)
		}
	}
}

// Cuerpos enormes y cabeceras enormes.
func TestHugeBodiesAndHeaders(t *testing.T) {
	useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())

	// RPC > 1 MiB: 413 too_large.
	big := `["` + strings.Repeat("a", 2<<20) + `"]`
	r := s.do("POST", "/api/v1/rpc/Inbox/GetChat", tok, strings.NewReader(big), map[string]string{"Content-Type": "application/json"})
	require.Equal(t, 413, r.Code)
	require.Equal(t, "too_large", r.errCode(t))
	// Login con un cuerpo gigante: se corta (413 según el contrato).
	r = s.do("POST", "/api/v1/auth/login", "", strings.NewReader(`{"username":"`+strings.Repeat("a", 1<<20)+`"}`), map[string]string{"Content-Type": "application/json"})
	require.Equal(t, 413, r.Code, string(r.Body))
	require.Equal(t, "too_large", r.errCode(t))
	// Cabeceras de 200 KB: se rechazan (431) o se corta la conexión; nunca se atienden.
	code, _ := s.rawReq("GET", "/api/v1/health", map[string]string{"X-Basura": strings.Repeat("a", 200<<10)}, "")
	require.True(t, code == 431 || code == 0 || code == 400, "código %d", code)
	// JSON profundamente anidado / mal formado: 400, sin 500.
	r = s.do("POST", "/api/v1/rpc/Inbox/GetChat", tok, strings.NewReader(strings.Repeat("[", 100000)), nil)
	require.Equal(t, 400, r.Code)
	r = s.do("POST", "/api/v1/rpc/Messaging/Send", tok, strings.NewReader(`["x",{"kind":{"a":1}}]`), nil)
	require.Equal(t, 400, r.Code, string(r.Body))
	// Subida con multipart roto.
	r = s.do("POST", "/api/v1/files", tok, strings.NewReader("--x\r\nbasura"), map[string]string{"Content-Type": "multipart/form-data; boundary=x"})
	require.GreaterOrEqual(t, r.Code, 400)
	require.Less(t, r.Code, 500, string(r.Body))
}

// Slowloris: una conexión que gotea la cabecera se corta por ReadHeaderTimeout (10 s).
func TestSlowlorisHeaderTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("tarda ~11 s")
	}
	useFake(t, nil)
	s := start(t, baseCfg(t))
	conn, err := net.Dial("tcp", s.addr)
	require.NoError(t, err)
	defer conn.Close()
	_, _ = conn.Write([]byte("GET /api/v1/health HTTP/1.1\r\nHost: x\r\nX-Lento: "))
	t0 := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	// El servidor responde 408 o cierra: lo importante es que la conexión no queda abierta indefinidamente.
	_, err = io.ReadAll(conn)
	el := time.Since(t0)
	_ = err
	require.Less(t, el, 15*time.Second, "la conexión lenta debe cortarse")
	require.Greater(t, el, 5*time.Second, "pero no antes de lo previsto")
	// El servidor sigue atendiendo.
	require.Equal(t, 200, s.do("GET", "/api/v1/health", "", nil, nil).Code)
}

// Los secretos de las URL (?token=, ?ticket=) y las claves no llegan a ningún registro.
func TestNoSecretsInLogs(t *testing.T) {
	useFake(t, nil)
	cfg := baseCfg(t)
	cfg.Verbose = true
	s := start(t, cfg)
	pass := s.adminPassword()
	tok := s.login("admin", pass)
	media := s.mediaToken(tok)
	tk := s.ticket(tok)
	const canary = "CANARIO-SECRETO-12345"

	s.do("GET", "/api/v1/media/a/b?token="+media+"&x="+canary, "", nil, nil)
	s.do("GET", "/api/v1/media/a/b?token="+canary, "", nil, nil)
	s.do("GET", "/api/v1/files/01JZZZZZZZZZZZZZZZZZZZZZZZ?token="+tok, "", nil, nil)
	s.do("POST", "/api/v1/rpc/Accounts/List?token="+canary+"&ticket="+canary, "", strings.NewReader("[]"), nil)
	s.rawReq("GET", "/api/v1/events?ticket="+canary, map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}, "")
	s.rawReq("GET", "/api/v1/media/%00?token="+media, nil, "")
	s.rawReq("BASURA", "/api/v1/media/a/b?token="+media, nil, "")
	s.postJSON("/api/v1/auth/login", "", map[string]string{"username": "admin", "password": canary + "-clave"})
	s.postJSON("/api/v1/auth/password", tok, map[string]string{"currentPassword": canary + "-vieja", "newPassword": canary + "-nueva-larga"})
	w, _, err := s.wsDial(tk)
	require.NoError(t, err)
	_ = w
	s.do("GET", "/api/v1/auth/me", tok, nil, nil)
	require.NoError(t, s.stop())

	logs := s.errOut.String() + s.out.String()
	files, _ := filepath.Glob(filepath.Join(s.cfg.Data, "logs", "*"))
	require.NotEmpty(t, files)
	for _, f := range files {
		b, _ := os.ReadFile(f)
		logs += string(b)
	}
	// La clave del administrador se imprime UNA vez por la salida estándar (docs/14 §3), nunca en el registro.
	logsSinSalida := s.errOut.String()
	for _, f := range files {
		b, _ := os.ReadFile(f)
		logsSinSalida += string(b)
	}
	for _, secret := range []string{tok, media, tk, canary, pass} {
		require.NotContains(t, logsSinSalida, secret, "un secreto llegó al registro")
	}
	_ = logs
}

// Fuerza bruta: 5 fallos por IP bloquean con 429 + Retry-After, incluso con la clave buena.
func TestLoginBruteForceLockout(t *testing.T) {
	useFake(t, nil)
	s := start(t, baseCfg(t))
	pass := s.adminPassword()
	for n := 0; n < 5; n++ {
		r := s.postJSON("/api/v1/auth/login", "", map[string]string{"username": "admin", "password": fmt.Sprintf("incorrecta-%d", n)})
		require.Equal(t, 401, r.Code)
		require.Equal(t, "Usuario o clave incorrectos.", jsonMessage(t, r))
	}
	r := s.postJSON("/api/v1/auth/login", "", map[string]string{"username": "admin", "password": pass})
	require.Equal(t, 429, r.Code, string(r.Body))
	require.Equal(t, "rate_limited", r.errCode(t))
	require.NotEmpty(t, r.Header.Get("Retry-After"))
	// El usuario que no existe recibe el mismo mensaje y también cuenta.
	r = s.postJSON("/api/v1/auth/login", "", map[string]string{"username": "nadie", "password": "x"})
	require.Equal(t, 429, r.Code)
}

// Notifications.SetFocus por RPC con la cabecera de conexión (el servicio de avisos quedó conectado a la API).
func TestNotificationsSetFocusAndNotifyEvents(t *testing.T) {
	adapters := useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	_, ad := newAccount(t, s, tok, adapters)
	w := s.ws(tok)
	hello := w.next()
	var hd struct{ ConnID string }
	require.NoError(t, jsonUnmarshal(hello.Data, &hd))
	r := s.postJSONHdr("/api/v1/rpc/Notifications/SetFocus", tok, []any{"", false}, map[string]string{"X-DConnector-Conn": hd.ConnID})
	require.Equal(t, 200, r.Code, string(r.Body))
	// Un mensaje nuevo con la ventana sin foco: el servidor decide avisar y manda `notify`.
	incomingText(ad, "593888", "nt-1", "¿Estás ahí?")
	ev := w.until("notify")
	require.Contains(t, string(ev.Data), "593888")
}
