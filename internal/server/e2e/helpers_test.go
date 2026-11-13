// Package e2e prueba dconnector-server de extremo a extremo: server.Run real (puerto efímero, datos en
// t.TempDir()) con un cliente HTTP/WebSocket real y el adaptador falso en lugar de cuentas reales.
package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"dconnector/internal/adapter/api/tlsconf"
	"dconnector/internal/adapter/channel"
	"dconnector/internal/adapter/store/sqlite"
	"dconnector/internal/config"
	"dconnector/internal/core/fake"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/server"
)

// syncBuf es un bytes.Buffer seguro para varios hilos (el servidor escribe y la prueba lee).
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// useFake registra el adaptador falso como canal Telegram durante la prueba (aditivo: restaura el registro al
// acabar). Devuelve el canal por el que llegan los adaptadores creados y el que bloquea el arranque si se pide.
func useFake(t *testing.T, wrap func(*fake.Adapter) port.ChannelAdapter) chan *fake.Adapter {
	t.Helper()
	out := make(chan *fake.Adapter, 16)
	prev := channel.Registry[model.ChannelTelegram]
	channel.Registry[model.ChannelTelegram] = func(deps port.AdapterDeps) (port.ChannelAdapter, error) {
		a := fake.New(deps.Account)
		select {
		case out <- a:
		default:
		}
		if wrap != nil {
			return wrap(a), nil
		}
		return a, nil
	}
	t.Cleanup(func() { channel.Registry[model.ChannelTelegram] = prev })
	return out
}

// inst es un servidor en marcha dentro del proceso de la prueba.
type inst struct {
	t       *testing.T
	cfg     config.Server
	addr    string
	tls     bool
	out     *syncBuf
	errOut  *syncBuf
	cancel  context.CancelFunc
	done    chan error
	stopped bool
	client  *http.Client
	fp      string // huella impresa al arrancar (con TLS)
}

var (
	adminPassRe = regexp.MustCompile(`Clave: (\S+)`)
	fingerRe    = regexp.MustCompile(`Huella TLS \(SHA-256\): ([0-9A-F:]+)`)
)

// baseCfg: modo estándar de prueba (loopback, sin TLS, administrador propio).
func baseCfg(t *testing.T) config.Server {
	return config.Server{Listen: "127.0.0.1:0", Data: t.TempDir(), NoTLS: true, AdminUser: "admin"}
}

func start(t *testing.T, cfg config.Server) *inst {
	t.Helper()
	i, err := tryStart(t, cfg)
	require.NoError(t, err)
	i.waitReady()
	return i
}

// waitReady espera a que /health diga ok (el núcleo y las cuentas ya arrancaron).
func (i *inst) waitReady() {
	i.t.Helper()
	require.Eventually(i.t, func() bool {
		req, _ := http.NewRequest("GET", i.url("/api/v1/health"), nil)
		r, err := i.client.Do(req)
		if err != nil {
			return false
		}
		defer r.Body.Close()
		return r.StatusCode == 200
	}, 30*time.Second, 20*time.Millisecond, "el servidor no llegó a ok")
}

// tryStart arranca el servidor y espera a que escuche; devuelve el error de Run si termina antes.
func tryStart(t *testing.T, cfg config.Server) (*inst, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	i := &inst{t: t, cfg: cfg, out: &syncBuf{}, errOut: &syncBuf{}, cancel: cancel, done: make(chan error, 1)}
	addrCh := make(chan string, 1)
	go func() {
		i.done <- server.RunHooks(ctx, cfg, i.out, i.errOut, server.Hooks{Listening: func(a string) { addrCh <- a }})
	}()
	select {
	case i.addr = <-addrCh:
	case err := <-i.done:
		cancel()
		i.stopped = true
		if err == nil {
			err = errors.New("el servidor terminó sin escuchar")
		}
		return nil, err
	case <-time.After(40 * time.Second):
		cancel()
		t.Fatal("el servidor no arrancó")
	}
	i.tls = !cfg.NoTLS
	if m := fingerRe.FindStringSubmatch(i.out.String()); m != nil {
		i.fp = m[1]
	}
	i.client = i.newClient()
	t.Cleanup(func() { _ = i.stop() })
	return i, nil
}

// newClient devuelve un cliente HTTP; con TLS fija la huella impresa (confianza al primer uso).
func (i *inst) newClient() *http.Client {
	tr := &http.Transport{DisableKeepAlives: false}
	if i.tls {
		tr.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, // se verifica la huella a mano
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				c, err := x509.ParseCertificate(raw[0])
				if err != nil {
					return err
				}
				if got := tlsconf.Fingerprint(c); got != i.fp {
					return errors.New("la huella del certificado no coincide: " + got)
				}
				return nil
			},
		}
	}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second}
}

func (i *inst) scheme() string {
	if i.tls {
		return "https"
	}
	return "http"
}

func (i *inst) url(path string) string { return i.scheme() + "://" + i.addr + path }

// stop apaga el servidor (cancela el contexto) y devuelve el error de Run; es idempotente.
func (i *inst) stop() error {
	if i.stopped {
		return nil
	}
	i.stopped = true
	i.cancel()
	select {
	case err := <-i.done:
		if i.client != nil {
			i.client.CloseIdleConnections()
		}
		return err
	case <-time.After(server.ShutdownTimeout + 10*time.Second):
		i.t.Error("el apagado tardó demasiado")
		return errors.New("apagado demasiado lento")
	}
}

// adminPassword extrae la clave del administrador impresa en el primer arranque.
func (i *inst) adminPassword() string {
	m := adminPassRe.FindStringSubmatch(i.out.String())
	if m == nil {
		i.t.Fatalf("no se imprimió la clave del administrador: %q", i.out.String())
	}
	return m[1]
}

// resp es una respuesta HTTP ya leída.
type resp struct {
	Code   int
	Body   []byte
	Header http.Header
}

func (r resp) json(t *testing.T, out any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.Body, out), string(r.Body))
}

func (r resp) errCode(t *testing.T) string {
	t.Helper()
	var e struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	require.NoError(t, json.Unmarshal(r.Body, &e), string(r.Body))
	return e.Error.Code
}

// do hace una petición con token Bearer opcional.
func (i *inst) do(method, path, token string, body io.Reader, hdr map[string]string) resp {
	i.t.Helper()
	req, err := http.NewRequest(method, i.url(path), body)
	require.NoError(i.t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	r, err := i.client.Do(req)
	require.NoError(i.t, err)
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	require.NoError(i.t, err)
	return resp{Code: r.StatusCode, Body: b, Header: r.Header}
}

func (i *inst) postJSON(path, token string, v any) resp {
	i.t.Helper()
	b, err := json.Marshal(v)
	require.NoError(i.t, err)
	return i.do("POST", path, token, bytes.NewReader(b), map[string]string{"Content-Type": "application/json"})
}

// login inicia sesión y devuelve el token.
func (i *inst) login(user, pass string) string {
	i.t.Helper()
	r := i.postJSON("/api/v1/auth/login", "", map[string]string{"username": user, "password": pass})
	require.Equal(i.t, 200, r.Code, string(r.Body))
	var out struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	r.json(i.t, &out)
	require.NotEmpty(i.t, out.Token)
	return out.Token
}

// rpc llama a un método y exige 200; decodifica en out si no es nil.
func (i *inst) rpc(token, svc, method string, out any, args ...any) {
	i.t.Helper()
	r := i.rpcRaw(token, svc, method, args...)
	require.Equal(i.t, 200, r.Code, string(r.Body))
	if out != nil {
		r.json(i.t, out)
	}
}

func (i *inst) rpcRaw(token, svc, method string, args ...any) resp {
	i.t.Helper()
	if args == nil {
		args = []any{}
	}
	return i.postJSON("/api/v1/rpc/"+svc+"/"+method, token, args)
}

// ticket pide un ticket de WebSocket.
func (i *inst) ticket(token string) string {
	i.t.Helper()
	r := i.postJSON("/api/v1/auth/ws-ticket", token, nil)
	require.Equal(i.t, 200, r.Code, string(r.Body))
	var out struct {
		Ticket string `json:"ticket"`
	}
	r.json(i.t, &out)
	return out.Ticket
}

func (i *inst) mediaToken(token string) string {
	i.t.Helper()
	r := i.postJSON("/api/v1/auth/media-token", token, nil)
	require.Equal(i.t, 200, r.Code, string(r.Body))
	var out struct {
		Token string `json:"token"`
	}
	r.json(i.t, &out)
	return out.Token
}

// wsEvent es un marco del WebSocket.
type wsEvent struct {
	Event string          `json:"event"`
	Seq   int64           `json:"seq"`
	Data  json.RawMessage `json:"data"`
}

// wsClient es un cliente WebSocket que lee en segundo plano.
type wsClient struct {
	t      *testing.T
	c      *websocket.Conn
	events chan wsEvent
	closed chan websocket.StatusCode // código de cierre (-1 si no fue un cierre limpio)
}

func (i *inst) wsDial(ticket string) (*wsClient, *http.Response, error) {
	i.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(i.scheme(), "http") + "://" + i.addr + "/api/v1/events?ticket=" + ticket
	c, hr, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: i.client})
	if err != nil {
		return nil, hr, err
	}
	w := &wsClient{t: i.t, c: c, events: make(chan wsEvent, 256), closed: make(chan websocket.StatusCode, 1)}
	go func() {
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				code := websocket.CloseStatus(err)
				w.closed <- code
				return
			}
			var ev wsEvent
			if json.Unmarshal(b, &ev) == nil {
				w.events <- ev
			}
		}
	}()
	i.t.Cleanup(func() { _ = c.CloseNow() })
	return w, hr, nil
}

func (i *inst) ws(token string) *wsClient {
	i.t.Helper()
	w, _, err := i.wsDial(i.ticket(token))
	require.NoError(i.t, err)
	return w
}

// next devuelve el siguiente evento (con plazo).
func (w *wsClient) next() wsEvent {
	w.t.Helper()
	select {
	case ev := <-w.events:
		return ev
	case <-time.After(10 * time.Second):
		w.t.Fatal("no llegó ningún evento")
	}
	return wsEvent{}
}

// until lee eventos hasta el que tenga ese nombre y comprueba que los seq crecen.
func (w *wsClient) until(name string) wsEvent {
	w.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-w.events:
			if ev.Event == name {
				return ev
			}
		case <-deadline:
			w.t.Fatalf("no llegó el evento %q", name)
		}
	}
}

// waitClose espera el cierre del socket y devuelve su código.
func (w *wsClient) waitClose() websocket.StatusCode {
	w.t.Helper()
	select {
	case c := <-w.closed:
		return c
	case <-time.After(15 * time.Second):
		w.t.Fatal("el WebSocket no se cerró")
	}
	return -1
}

// newAccount crea una cuenta falsa por RPC y espera su adaptador conectado.
func newAccount(t *testing.T, s *inst, token string, adapters chan *fake.Adapter) (model.Account, *fake.Adapter) {
	t.Helper()
	var acc model.Account
	s.rpc(token, "Accounts", "Create", &acc, "telegram", "Cuenta de prueba", "#229ED9", map[string]string{})
	var ad *fake.Adapter
	select {
	case ad = <-adapters:
	case <-time.After(10 * time.Second):
		t.Fatal("el adaptador falso no se creó")
	}
	require.Eventually(t, func() bool {
		var views []struct {
			Account model.Account       `json:"account"`
			Status  model.AccountStatus `json:"status"`
		}
		r := s.rpcRaw(token, "Accounts", "List")
		if r.Code != 200 || json.Unmarshal(r.Body, &views) != nil {
			return false
		}
		for _, v := range views {
			if v.Account.ID == acc.ID && v.Status.State == model.StateConnected {
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond)
	return acc, ad
}

// incomingText inyecta un mensaje entrante de texto.
func incomingText(ad *fake.Adapter, chat, id, text string) {
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: &model.Message{
		NativeID: id, ChatNativeID: chat, SenderNativeID: chat, SenderName: "Ana",
		Timestamp: time.Now().UnixMilli(), Kind: model.KindText, Text: text,
	}, Chat: &model.Chat{NativeID: chat, Kind: model.ChatDirect, Title: "Ana", PeerNativeID: chat}})
}

// listChats devuelve los chats (por RPC).
func listChats(s *inst, token string) []model.Chat {
	var cs []model.Chat
	s.rpc(token, "Inbox", "ListChats", &cs, model.ChatFilter{})
	return cs
}

// openAppDB abre app.db con otro manejador (WAL admite varios) para forzar estados que el servidor no deja
// alcanzar desde fuera (tokens caducados).
func openAppDB(t *testing.T, data string) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(data, "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// tokenHash es el SHA-256 en hexadecimal con el que el servidor guarda un token.
func tokenHash(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// jsonMessage devuelve el texto de error de una respuesta de error.
func jsonMessage(t *testing.T, r resp) string {
	t.Helper()
	var e struct {
		Error struct{ Message string } `json:"error"`
	}
	require.NoError(t, json.Unmarshal(r.Body, &e), string(r.Body))
	return e.Error.Message
}

func (i *inst) postJSONHdr(path, token string, v any, hdr map[string]string) resp {
	i.t.Helper()
	b, err := json.Marshal(v)
	require.NoError(i.t, err)
	h := map[string]string{"Content-Type": "application/json"}
	for k, x := range hdr {
		h[k] = x
	}
	return i.do("POST", path, token, bytes.NewReader(b), h)
}
