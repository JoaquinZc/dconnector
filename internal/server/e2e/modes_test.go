package e2e

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"dconnector/internal/adapter/api/tlsconf"
	"dconnector/internal/core/fake"
	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
	"dconnector/internal/server"
)

// /health: starting (503) hasta que el núcleo arranca, ok (200) y stopping (503) al apagar.
func TestHealthStartingOkStopping(t *testing.T) {
	useFake(t, nil)
	cfg := baseCfg(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type snap struct {
		health, rpc int
		status      string
	}
	stdout := &syncBuf{}
	first := make(chan snap, 1)
	done := make(chan error, 1)
	addrCh := make(chan string, 1)
	go func() {
		done <- server.RunHooks(ctx, cfg, stdout, &syncBuf{}, server.Hooks{Listening: func(a string) {
			// El gancho se llama antes de arrancar el núcleo: el estado es «starting» sin carrera posible.
			var s snap
			r, err := http.Get("http://" + a + "/api/v1/health")
			if err == nil {
				s.health = r.StatusCode
				var b [200]byte
				n, _ := r.Body.Read(b[:])
				s.status = string(b[:n])
				r.Body.Close()
			}
			req, _ := http.NewRequest("POST", "http://"+a+"/api/v1/rpc/Accounts/List", strings.NewReader("[]"))
			req.Header.Set("Authorization", "Bearer inventado")
			if r, err := http.DefaultClient.Do(req); err == nil {
				s.rpc = r.StatusCode
				r.Body.Close()
			}
			first <- s
			addrCh <- a
		}})
	}()
	var addr string
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(30 * time.Second):
		t.Fatal("no arrancó")
	}
	s := <-first
	require.Equal(t, 503, s.health)
	require.Contains(t, s.status, `"starting"`)
	require.Equal(t, 401, s.rpc, "sin sesión se responde 401 también mientras arranca (la autenticación va antes)")

	i := &inst{t: t, addr: addr, out: stdout, client: &http.Client{Timeout: 10 * time.Second}}
	i.waitReady()

	// Apagado: un WebSocket que no atiende el cierre (nadie lee su socket) retiene el apagado unos segundos con el
	// puerto todavía abierto; en ese lapso /health dice stopping.
	tok := i.login("admin", i.adminPassword())
	idle, _, err := websocket.Dial(t.Context(), "ws://"+addr+"/api/v1/events?ticket="+i.ticket(tok), nil) // nadie lee este socket: no atiende el cierre
	defer idle.CloseNow()
	require.NoError(t, err)
	cancel()
	var last resp
	require.Eventually(t, func() bool {
		r, err := http.Get("http://" + addr + "/api/v1/health")
		if err != nil {
			return false
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		last = resp{Code: r.StatusCode, Body: b}
		return r.StatusCode == 503 && strings.Contains(string(b), "stopping")
	}, 8*time.Second, 25*time.Millisecond)
	require.Equal(t, 503, last.Code)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("el apagado tardó demasiado")
	}
}

// TLS con certificado propio: huella impresa == huella servida, el SAN cubre loopback, la huella se mantiene al
// reiniciar, HSTS, WebSocket por wss y un cliente sin la huella fijada es rechazado.
func TestTLSSelfSignedFingerprint(t *testing.T) {
	useFake(t, nil)
	cfg := baseCfg(t)
	cfg.NoTLS = false
	s := start(t, cfg)
	require.NotEmpty(t, s.fp, s.out.String())
	require.True(t, s.tls)

	// La línea «listening» dice tls:true y /health también.
	require.Contains(t, s.out.String(), `"tls":true`)
	h := s.do("GET", "/api/v1/health", "", nil, nil)
	require.Equal(t, 200, h.Code)
	require.Contains(t, string(h.Body), `"tls":true`)
	require.Equal(t, "max-age=31536000", h.Header.Get("Strict-Transport-Security"))

	// Un cliente que verifica con las raíces del sistema rechaza el certificado propio.
	_, err := (&http.Client{Timeout: 5 * time.Second}).Get(s.url("/api/v1/health"))
	require.Error(t, err)

	// La huella servida es la impresa, y el certificado es válido para localhost y 127.0.0.1 (verificación real
	// de nombre y vigencia usando el propio certificado como raíz).
	conn, err := tls.Dial("tcp", s.addr, &tls.Config{InsecureSkipVerify: true})
	require.NoError(t, err)
	leaf := conn.ConnectionState().PeerCertificates[0]
	_ = conn.Close()
	require.Equal(t, s.fp, tlsconf.Fingerprint(leaf))
	require.GreaterOrEqual(t, int(conn.ConnectionState().Version), tls.VersionTLS12)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	for _, name := range []string{"localhost", "127.0.0.1"} {
		_, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: name})
		require.NoError(t, err, name)
	}
	require.Greater(t, time.Until(leaf.NotAfter), 300*24*time.Hour)
	_, statErr := os.Stat(filepath.Join(s.cfg.Data, "tls"))
	require.NoError(t, statErr)

	// Login y WebSocket por TLS con la huella fijada.
	tok := s.login("admin", s.adminPassword())
	w := s.ws(tok)
	require.Equal(t, "hello", w.next().Event)

	// Reinicio: mismo certificado, misma huella.
	fp := s.fp
	require.NoError(t, s.stop())
	s2 := start(t, s.cfg)
	require.Equal(t, fp, s2.fp)
}

// Fuera de loopback sin TLS el servidor se niega a arrancar (en español); con TLS o con --insecure sí arranca.
func TestPublicListenRules(t *testing.T) {
	useFake(t, nil)

	cfg := baseCfg(t)
	cfg.Listen = "0.0.0.0:0"
	_, err := tryStart(t, cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "loopback")
	require.Contains(t, err.Error(), "--insecure")
	// No dejó nada a medias: ni usuarios ni socket (el siguiente arranque en la misma carpeta funciona).
	cfg.NoTLS = false // con TLS propio, escuchar fuera de loopback es válido
	s := start(t, cfg)
	require.True(t, s.tls)
	require.NoError(t, s.stop())

	// --insecure: arranca sin TLS y avisa con ruido.
	cfg = baseCfg(t)
	cfg.Listen = "0.0.0.0:0"
	cfg.Insecure = true
	s = start(t, cfg)
	require.False(t, s.tls)
	require.Eventually(t, func() bool { return strings.Contains(s.errOut.String(), "SIN TLS") }, 5*time.Second, 20*time.Millisecond)
	_, port, _ := net.SplitHostPort(s.addr)
	s.addr = "127.0.0.1:" + port
	require.Equal(t, 200, s.do("GET", "/api/v1/health", "", nil, nil).Code)
}

// --no-tls en una dirección pública con --insecure: el token local NO vale (solo loopback, docs/14 §3).
func TestLocalTokenDisabledOffLoopback(t *testing.T) {
	useFake(t, nil)
	cfg := baseCfg(t)
	cfg.Listen = "0.0.0.0:0"
	cfg.Insecure = true
	cfg.LocalToken = "token-local-de-prueba-0123456789"
	s := start(t, cfg)
	_, port, _ := net.SplitHostPort(s.addr)
	s.addr = "127.0.0.1:" + port
	require.Equal(t, 401, s.rpcRaw(cfg.LocalToken, "Accounts", "List").Code)
	// Con LocalToken no se crea administrador: nadie puede entrar. (Sin usuarios el servidor queda cerrado.)
	require.NotContains(t, s.out.String(), "Clave:")
}

// Modo local (token del escritorio): rutas absolutas solo con él; no crea usuarios; no vale por proxy.
func TestLocalMode(t *testing.T) {
	adapters := useFake(t, nil)
	cfg := baseCfg(t)
	cfg.LocalToken = "token-local-de-prueba-0123456789"
	s := start(t, cfg)
	require.NotContains(t, s.out.String(), "Clave:", "el modo local no crea usuarios")
	lt := cfg.LocalToken

	var me struct {
		Username string
		Local    bool
	}
	r := s.do("GET", "/api/v1/auth/me", lt, nil, nil)
	require.Equal(t, 200, r.Code, string(r.Body))
	r.json(t, &me)
	require.True(t, me.Local)
	require.Equal(t, "local", me.Username)

	// Un proxy (cabeceras de reenvío) invalida el token local; el token mal escrito tampoco vale.
	require.Equal(t, 401, s.do("GET", "/api/v1/auth/me", lt, nil, map[string]string{"X-Forwarded-For": "10.0.0.9"}).Code)
	require.Equal(t, 401, s.do("GET", "/api/v1/auth/me", lt+"x", nil, nil).Code)
	// El token local nunca va en la URL.
	require.Equal(t, 401, s.do("GET", "/api/v1/files/01JZZZZZZZZZZZZZZZZZZZZZZZ?token="+lt, "", nil, nil).Code)
	// Sin usuarios no hay login posible.
	require.Equal(t, 401, s.postJSON("/api/v1/auth/login", "", map[string]string{"username": "admin", "password": "cualquiera-larga-1"}).Code)

	acc, ad := newAccount(t, s, lt, adapters)
	_ = acc
	incomingText(ad, "593555", "l-1", "hola")
	require.Eventually(t, func() bool { return len(listChats(s, lt)) == 1 }, 10*time.Second, 20*time.Millisecond)
	chat := string(listChats(s, lt)[0].Key)

	// Ruta absoluta: permitida con el token local.
	f := filepath.Join(t.TempDir(), "doc.txt")
	require.NoError(t, os.WriteFile(f, []byte("hola"), 0o600))
	var sent model.Message
	s.rpc(lt, "Messaging", "Send", &sent, chat, model.Outgoing{Kind: model.KindDocument, FilePath: f, FileName: "doc.txt", Mime: "text/plain"})
	require.Len(t, ad.Sent(), 1)
	require.Equal(t, f, ad.Sent()[0].FilePath)
	// Una ruta relativa o con NUL se rechaza incluso en modo local.
	require.Equal(t, 400, s.rpcRaw(lt, "Messaging", "Send", chat, model.Outgoing{Kind: model.KindDocument, FilePath: "relativa/doc.txt"}).Code)
	require.Equal(t, 400, s.rpcRaw(lt, "Messaging", "Send", chat, model.Outgoing{Kind: model.KindDocument, FilePath: f + "\x00.png"}).Code)

	// El media-token del modo local sirve para leer medios y para nada más; el ticket y el WebSocket funcionan.
	mt := s.mediaToken(lt)
	require.Equal(t, 403, s.rpcRaw(mt, "Accounts", "List").Code)
	w := s.ws(lt)
	require.Equal(t, "hello", w.next().Event)
	// logout del usuario local no rompe nada.
	require.Equal(t, 200, s.postJSON("/api/v1/auth/logout", lt, nil).Code)
	require.Equal(t, 200, s.do("GET", "/api/v1/auth/me", lt, nil, nil).Code)
	// Cambiar la clave del usuario local no tiene sentido.
	require.Equal(t, 403, s.postJSON("/api/v1/auth/password", lt, map[string]string{"currentPassword": "x", "newPassword": "otra-clave-larga-12"}).Code)
}

// --no-accounts: se sirven los datos existentes sin conectar ninguna cuenta ni arrancar servicios de fondo.
func TestNoAccountsMode(t *testing.T) {
	adapters := useFake(t, nil)
	cfg := baseCfg(t)
	s := start(t, cfg)
	pass := s.adminPassword()
	tok := s.login("admin", pass)
	acc, ad := newAccount(t, s, tok, adapters)
	incomingText(ad, "593666", "n-1", "dato real")
	require.Eventually(t, func() bool { return len(listChats(s, tok)) == 1 }, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, s.stop())
	for len(adapters) > 0 {
		<-adapters
	}

	cfg.NoAccounts = true
	s2 := start(t, cfg)
	require.NotContains(t, s2.out.String(), "Clave:")
	tok2 := s2.login("admin", pass)
	time.Sleep(500 * time.Millisecond)
	var views []service.AccountView
	s2.rpc(tok2, "Accounts", "List", &views)
	require.Len(t, views, 1)
	require.Equal(t, acc.ID, views[0].Account.ID)
	require.NotEqual(t, model.StateConnected, views[0].Status.State)
	require.Empty(t, adapterCalls(adapters), "ningún adaptador recibió llamadas")
	require.Len(t, listChats(s2, tok2), 1, "los datos copiados se leen igual")
	// Enviar no sale a ningún lado: la cuenta no está conectada.
	chats := listChats(s2, tok2)
	r := s2.rpcRaw(tok2, "Messaging", "Send", string(chats[0].Key), model.Outgoing{Kind: model.KindText, Text: "no debe salir"})
	require.NotEqual(t, 200, r.Code)
	require.NoError(t, s2.stop())
}

// Apagado ordenado con conexiones abiertas: los WebSocket reciben 1001, Run devuelve nil dentro del plazo y no
// quedan goroutines del servidor.
func TestGracefulShutdownNoGoroutineLeaks(t *testing.T) {
	adapters := useFake(t, nil)
	runtime.GC()
	base := runtime.NumGoroutine()

	cfg := baseCfg(t)
	s := start(t, cfg)
	tok := s.login("admin", s.adminPassword())
	_, ad := newAccount(t, s, tok, adapters)
	var ws []*wsClient
	for n := 0; n < 3; n++ {
		w := s.ws(tok)
		require.Equal(t, "hello", w.next().Event)
		ws = append(ws, w)
	}
	incomingText(ad, "593777", "g-1", "hola")
	ws[0].until("message:upsert")
	t0 := time.Now()
	require.NoError(t, s.stop())
	require.Less(t, time.Since(t0), 8*time.Second, "el apagado ordenado es rápido con conexiones normales")
	for _, w := range ws {
		require.EqualValues(t, websocket.StatusGoingAway, w.waitClose())
	}
	s.client.CloseIdleConnections()
	http.DefaultClient.CloseIdleConnections()

	ok := false
	for n := 0; n < 100; n++ {
		runtime.GC()
		if runtime.NumGoroutine() <= base+2 {
			ok = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ok {
		var sb strings.Builder
		_ = pprof.Lookup("goroutine").WriteTo(&sb, 1)
		t.Fatalf("fuga de goroutines: antes %d, después %d\n%s", base, runtime.NumGoroutine(), sb.String())
	}
}

// adapterCalls recoge las llamadas registradas por los adaptadores falsos creados (vacío = no se tocó ninguno).
func adapterCalls(ch chan *fake.Adapter) []string {
	var out []string
	for len(ch) > 0 {
		out = append(out, (<-ch).Calls()...)
	}
	return out
}
