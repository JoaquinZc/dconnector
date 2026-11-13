package e2e

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Primer arranque: crea el administrador, login, rutas protegidas y RPC representativos con el adaptador falso.
func TestFirstRunLoginAndRPC(t *testing.T) {
	adapters := useFake(t, nil)
	s := start(t, baseCfg(t))
	pass := s.adminPassword()
	require.GreaterOrEqual(t, len(pass), 20)

	// /health es público y dice ok.
	h := s.do("GET", "/api/v1/health", "", nil, nil)
	require.Equal(t, 200, h.Code)
	var hb map[string]any
	h.json(t, &hb)
	require.Equal(t, "ok", hb["status"])
	require.Equal(t, false, hb["tls"])
	require.EqualValues(t, 1, hb["apiVersion"])

	// La línea «listening» es lo primero que se imprime tras la clave y trae la dirección real.
	var line struct {
		Event, Addr string
		TLS         bool
		PID         int
	}
	found := false
	for _, l := range strings.Split(s.out.String(), "\n") {
		if strings.HasPrefix(l, "{") && json.Unmarshal([]byte(l), &line) == nil && line.Event == "listening" {
			found = true
		}
	}
	require.True(t, found, s.out.String())
	require.Equal(t, s.addr, line.Addr)

	// Sin sesión, nada responde salvo /health y /auth/login.
	require.Equal(t, 401, s.rpcRaw("", "Accounts", "List").Code)
	require.Equal(t, 401, s.do("GET", "/api/v1/auth/me", "", nil, nil).Code)
	require.Equal(t, 401, s.rpcRaw("basura", "Accounts", "List").Code)

	// Clave mala: 401 con el mensaje genérico; clave buena: sesión.
	bad := s.postJSON("/api/v1/auth/login", "", map[string]string{"username": "admin", "password": "x"})
	require.Equal(t, 401, bad.Code)
	require.Equal(t, "unauthorized", bad.errCode(t))
	tok := s.login("admin", pass)

	var me struct {
		Username, Role string
		Local          bool
	}
	r := s.do("GET", "/api/v1/auth/me", tok, nil, nil)
	require.Equal(t, 200, r.Code)
	r.json(t, &me)
	require.Equal(t, "admin", me.Username)
	require.False(t, me.Local)

	// RPC representativos.
	var views []service.AccountView
	s.rpc(tok, "Accounts", "List", &views)
	require.Empty(t, views)
	acc, ad := newAccount(t, s, tok, adapters)
	s.rpc(tok, "Accounts", "List", &views)
	require.Len(t, views, 1)
	require.Equal(t, acc.ID, views[0].Account.ID)

	require.Empty(t, listChats(s, tok))
	incomingText(ad, "593999", "in-1", "Hola desde el canal")
	require.Eventually(t, func() bool { return len(listChats(s, tok)) == 1 }, 10*time.Second, 20*time.Millisecond)
	chats := listChats(s, tok)
	require.Equal(t, "593999", chats[0].NativeID)

	var sent model.Message
	s.rpc(tok, "Messaging", "Send", &sent, string(chats[0].Key), model.Outgoing{Kind: model.KindText, Text: "Respuesta"})
	require.True(t, sent.FromMe)
	require.Equal(t, "Respuesta", sent.Text)
	got := ad.Sent()
	require.Len(t, got, 1)
	require.Equal(t, "Respuesta", got[0].Text)

	var st service.AppSettings
	s.rpc(tok, "Settings", "Get", &st)
	st.Theme = "dark"
	s.rpc(tok, "Settings", "Save", nil, st)
	var st2 service.AppSettings
	s.rpc(tok, "Settings", "Get", &st2)
	require.Equal(t, "dark", st2.Theme)
	var dir string
	s.rpc(tok, "Settings", "DataDir", &dir)
	require.Equal(t, s.cfg.Data, dir)

	// Métodos del escritorio no existen en el servidor.
	require.Equal(t, "not_available", s.rpcRaw(tok, "Settings", "OpenDataDir").errCode(t))
	// Servicio o método inexistente, argumentos mal formados.
	require.Equal(t, 404, s.rpcRaw(tok, "Nada", "Nada").Code)
	require.Equal(t, 400, s.rpcRaw(tok, "Inbox", "GetChat").Code)

	// Reiniciar con la misma carpeta: el administrador NO se vuelve a crear ni a imprimir y la sesión sigue vigente.
	require.NoError(t, s.stop())
	s2 := start(t, s.cfg)
	require.NotContains(t, s2.out.String(), "Clave:")
	r = s2.do("GET", "/api/v1/auth/me", tok, nil, nil)
	require.Equal(t, 200, r.Code, string(r.Body))
	require.Len(t, listChats(s2, tok), 1) // los datos persisten
}

// WebSocket: ticket de un solo uso, hello, seq creciente, eventos de un mensaje recibido, dos clientes a la vez.
func TestWebSocketEventsAndTwoClients(t *testing.T) {
	adapters := useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	_, ad := newAccount(t, s, tok, adapters)

	// Sin ticket, con ticket falso o sin Upgrade: no hay WebSocket.
	_, hr, err := s.wsDial("")
	require.Error(t, err)
	require.Equal(t, 401, hr.StatusCode)
	_, hr, err = s.wsDial("inventado")
	require.Error(t, err)
	require.Equal(t, 401, hr.StatusCode)

	// Un ticket sirve una sola vez.
	tk := s.ticket(tok)
	w1, _, err := s.wsDial(tk)
	require.NoError(t, err)
	_, hr, err = s.wsDial(tk)
	require.Error(t, err)
	require.Equal(t, 401, hr.StatusCode)

	w2 := s.ws(tok)
	for _, w := range []*wsClient{w1, w2} {
		hello := w.next()
		require.Equal(t, "hello", hello.Event)
		require.EqualValues(t, 0, hello.Seq)
		var hd struct {
			Version, User, ConnID string
			APIVersion, Protocol  int
			ServerTime            int64
		}
		require.NoError(t, json.Unmarshal(hello.Data, &hd))
		require.Equal(t, "admin", hd.User)
		require.NotEmpty(t, hd.ConnID)
		require.Equal(t, 1, hd.APIVersion)
	}

	// Un mensaje recibido llega a AMBOS clientes, con seq creciente.
	incomingText(ad, "593111", "ws-1", "Primero")
	incomingText(ad, "593111", "ws-2", "Segundo")
	for _, w := range []*wsClient{w1, w2} {
		var last int64
		seen := map[string]bool{}
		deadline := time.After(10 * time.Second)
		for !(seen["message:upsert"] && seen["chat:upsert"]) {
			select {
			case ev := <-w.events:
				require.Greater(t, ev.Seq, last, "seq debe crecer (%s)", ev.Event)
				last = ev.Seq
				seen[ev.Event] = true
			case <-deadline:
				t.Fatalf("faltan eventos: %v", seen)
			}
		}
	}
}

// Máximo de 8 conexiones por usuario; la 9.ª recibe 429.
func TestWebSocketLimitPerUser(t *testing.T) {
	useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	var ws []*wsClient
	for n := 0; n < 8; n++ {
		w := s.ws(tok)
		require.Equal(t, "hello", w.next().Event)
		ws = append(ws, w)
	}
	_, hr, err := s.wsDial(s.ticket(tok))
	require.Error(t, err)
	require.Equal(t, 429, hr.StatusCode)
	require.NotEmpty(t, hr.Header.Get("Retry-After"))
	// Al cerrar una, se puede abrir otra.
	_ = ws[0].c.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool {
		w, _, err := s.wsDial(s.ticket(tok))
		if err != nil {
			return false
		}
		_ = w.c.CloseNow()
		return true
	}, 5*time.Second, 50*time.Millisecond)
}

// Reconexión: se corta el servidor, se vuelve a arrancar sobre la misma carpeta y el cliente reconecta con un
// ticket nuevo (la sesión sigue vigente); el seq de la conexión nueva vuelve a empezar y no hay reenvío.
func TestWebSocketReconnectAfterServerRestart(t *testing.T) {
	adapters := useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	_, ad := newAccount(t, s, tok, adapters)
	w := s.ws(tok)
	require.Equal(t, "hello", w.next().Event)
	incomingText(ad, "593222", "r-1", "antes del corte")
	w.until("message:upsert")

	require.NoError(t, s.stop())
	require.EqualValues(t, websocket.StatusGoingAway, w.waitClose(), "el apagado cierra con 1001")

	s2 := start(t, s.cfg)
	w2 := s2.ws(tok)
	hello := w2.next()
	require.Equal(t, "hello", hello.Event)
	require.EqualValues(t, 0, hello.Seq)
	// Tras reconectar, el cliente recarga su estado por RPC (el servidor es la fuente de verdad).
	require.Len(t, listChats(s2, tok), 1)
}

// Cambiar la clave invalida los tokens (sesión y media) y cierra los WebSocket con 4401; logout igual.
func TestPasswordChangeAndLogoutRevoke(t *testing.T) {
	useFake(t, nil)
	s := start(t, baseCfg(t))
	pass := s.adminPassword()
	tok := s.login("admin", pass)
	tok2 := s.login("admin", pass)
	media := s.mediaToken(tok)
	w := s.ws(tok2)
	require.Equal(t, "hello", w.next().Event)
	pending := s.ticket(tok2) // un ticket pendiente tampoco sobrevive

	newPass := "otra-clave-bien-larga-1"
	r := s.postJSON("/api/v1/auth/password", tok, map[string]string{"currentPassword": pass, "newPassword": newPass})
	require.Equal(t, 200, r.Code, string(r.Body))
	var nt struct{ Token string }
	r.json(t, &nt)

	require.EqualValues(t, 4401, w.waitClose(), "token revocado: cierre 4401")
	require.Equal(t, 401, s.rpcRaw(tok, "Accounts", "List").Code)
	require.Equal(t, 401, s.rpcRaw(tok2, "Accounts", "List").Code)
	require.Equal(t, 401, s.do("GET", "/api/v1/files/01JZZZZZZZZZZZZZZZZZZZZZZZ?token="+media, "", nil, nil).Code)
	_, hr, err := s.wsDial(pending)
	require.Error(t, err)
	require.Equal(t, 401, hr.StatusCode)

	// La clave vieja ya no entra; la nueva sí; el token que devolvió el cambio sirve.
	old := s.postJSON("/api/v1/auth/login", "", map[string]string{"username": "admin", "password": pass})
	require.Equal(t, 401, old.Code)
	require.Equal(t, 200, s.rpcRaw(nt.Token, "Accounts", "List").Code)
	tok3 := s.login("admin", newPass)

	// logout invalida todos los tokens y cierra los WebSocket.
	w3 := s.ws(tok3)
	require.Equal(t, "hello", w3.next().Event)
	require.Equal(t, 200, s.postJSON("/api/v1/auth/logout", tok3, nil).Code)
	require.EqualValues(t, 4401, w3.waitClose())
	require.Equal(t, 401, s.rpcRaw(tok3, "Accounts", "List").Code)
	require.Equal(t, 401, s.rpcRaw(nt.Token, "Accounts", "List").Code)
}

// Subida a /files, descarga, y upload:// en un envío; las rutas locales se rechazan a un cliente remoto.
func TestUploadsAndUploadScheme(t *testing.T) {
	adapters := useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	_, ad := newAccount(t, s, tok, adapters)
	incomingText(ad, "593333", "u-1", "hola")
	require.Eventually(t, func() bool { return len(listChats(s, tok)) == 1 }, 10*time.Second, 20*time.Millisecond)
	chatKey := string(listChats(s, tok)[0].Key)

	content := []byte("%PDF-1.4 contenido de prueba")
	up := func(name string, data []byte, token string) resp {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, err := mw.CreateFormFile("file", name)
		require.NoError(t, err)
		_, _ = fw.Write(data)
		require.NoError(t, mw.Close())
		return s.do("POST", "/api/v1/files", token, &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
	}
	require.Equal(t, 401, up("a.pdf", content, "").Code)
	r := up("../../informe final.pdf", content, tok)
	require.Equal(t, 200, r.Code, string(r.Body))
	var info struct {
		ID, Name, Mime string
		Size           int64
	}
	r.json(t, &info)
	require.Equal(t, "informe final.pdf", info.Name)
	require.Equal(t, "application/pdf", info.Mime)
	require.EqualValues(t, len(content), info.Size)
	// El archivo vive bajo <data>/uploads/<id>/ (el nombre del cliente nunca es una ruta).
	ents, err := os.ReadDir(filepath.Join(s.cfg.Data, "uploads", info.ID))
	require.NoError(t, err)
	require.NotEmpty(t, ents)

	d := s.do("GET", "/api/v1/files/"+info.ID, tok, nil, nil)
	require.Equal(t, 200, d.Code)
	require.Equal(t, content, d.Body)
	require.Equal(t, "sandbox", d.Header.Get("Content-Security-Policy"))
	require.Equal(t, "nosniff", d.Header.Get("X-Content-Type-Options"))
	require.Equal(t, 400, s.do("GET", "/api/v1/files/no-es-un-ulid", tok, nil, nil).Code)

	// upload:// en un envío: el adaptador recibe la ruta real bajo uploads con el nombre y el tipo de la ficha.
	var sent model.Message
	s.rpc(tok, "Messaging", "Send", &sent, chatKey, model.Outgoing{Kind: model.KindDocument, FilePath: "upload://" + info.ID})
	got := ad.Sent()
	require.Len(t, got, 1)
	require.True(t, strings.HasPrefix(got[0].FilePath, filepath.Join(s.cfg.Data, "uploads")), got[0].FilePath)
	require.Equal(t, "informe final.pdf", got[0].FileName)
	require.Equal(t, "application/pdf", got[0].Mime)

	// Un cliente remoto no puede mandar rutas del servidor ni ids inventados.
	for _, p := range []string{
		filepath.Join(s.cfg.Data, "app.db"), `C:\Windows\win.ini`, "/etc/passwd", "../../app.db",
		"upload://../../app.db", "upload://01JZZZZZZZZZZZZZZZZZZZZZZZ", "upload://" + info.ID + "/../..", "upload://\x00",
	} {
		r := s.rpcRaw(tok, "Messaging", "Send", chatKey, model.Outgoing{Kind: model.KindDocument, FilePath: p})
		require.Equal(t, 400, r.Code, "%q: %s", p, r.Body)
		require.Equal(t, "invalid", r.errCode(t))
	}
	require.Len(t, ad.Sent(), 1, "ninguna de las rutas rechazadas llegó al canal")

	// Archivo vacío o campo equivocado: 400.
	require.Equal(t, 400, s.do("POST", "/api/v1/files", tok, strings.NewReader("x"), map[string]string{"Content-Type": "application/octet-stream"}).Code)
}

// Medios: media-token en ?token= (solo lectura), Range, y el token de sesión no sirve en la URL.
func TestMediaWithMediaTokenAndRange(t *testing.T) {
	adapters := useFake(t, nil)
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	acc, ad := newAccount(t, s, tok, adapters)
	ad.SetMedia("m1", []byte("0123456789"))
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: &model.Message{
		NativeID: "m1", ChatNativeID: "593444", SenderNativeID: "593444", Timestamp: time.Now().UnixMilli(),
		Kind: model.KindAudio, Media: &model.Media{ID: "m1", Mime: "audio/ogg", FileName: "n.ogg"},
	}, Chat: &model.Chat{NativeID: "593444", Kind: model.ChatDirect, Title: "Ana"}})
	require.Eventually(t, func() bool { return len(listChats(s, tok)) == 1 }, 10*time.Second, 20*time.Millisecond)

	mt := s.mediaToken(tok)
	base := "/api/v1/media/" + string(acc.ID) + "/m1"
	r := s.do("GET", base+"?token="+mt, "", nil, nil)
	require.Equal(t, 200, r.Code, string(r.Body))
	require.Equal(t, "0123456789", string(r.Body))
	require.Equal(t, "private, max-age=3600", r.Header.Get("Cache-Control"))
	r = s.do("GET", base+"?token="+mt, "", nil, map[string]string{"Range": "bytes=2-4"})
	require.Equal(t, http.StatusPartialContent, r.Code)
	require.Equal(t, "234", string(r.Body))
	require.Equal(t, "bytes 2-4/10", r.Header.Get("Content-Range"))

	// El media-token no sirve fuera de la lectura de medios.
	require.Equal(t, 403, s.rpcRaw(mt, "Accounts", "List").Code)
	require.Equal(t, 403, s.postJSON("/api/v1/auth/media-token", mt, nil).Code)
	require.Equal(t, 403, s.postJSON("/api/v1/auth/ws-ticket", mt, nil).Code)
	// El token de sesión en la URL no vale (solo en la cabecera).
	require.Equal(t, 401, s.do("GET", base+"?token="+tok, "", nil, nil).Code)
	require.Equal(t, 200, s.do("GET", base, tok, nil, nil).Code)
	// Sin ningún token.
	require.Equal(t, 401, s.do("GET", base, "", nil, nil).Code)
}

// Tokens caducados: se fuerza la caducidad en la base (el reloj del servicio no se puede adelantar desde fuera).
func TestExpiredTokens(t *testing.T) {
	useFake(t, nil)
	s := start(t, baseCfg(t))
	pass := s.adminPassword()
	idle := s.login("admin", pass)
	hard := s.login("admin", pass)
	media := s.mediaToken(idle)

	db := openAppDB(t, s.cfg.Data)
	past := time.Now().Add(-time.Minute).UnixMilli()
	exec := func(q string, args ...any) {
		_, err := db.Exec(q, args...)
		require.NoError(t, err)
	}
	// Los tokens guardan solo el SHA-256: se localizan por tipo y orden de creación.
	exec(`UPDATE auth_tokens SET expires_at = ? WHERE kind = 'media'`, past)
	require.Equal(t, 401, s.do("GET", "/api/v1/files/01JZZZZZZZZZZZZZZZZZZZZZZZ?token="+media, "", nil, nil).Code, "media-token caducado")

	exec(`UPDATE auth_tokens SET hard_expires_at = ? WHERE token_hash = ?`, past, tokenHash(hard))
	require.Equal(t, 401, s.rpcRaw(hard, "Accounts", "List").Code, "tope de 30 días")
	require.Equal(t, 200, s.rpcRaw(idle, "Accounts", "List").Code)

	exec(`UPDATE auth_tokens SET expires_at = ? WHERE kind = 'session'`, past)
	r := s.rpcRaw(idle, "Accounts", "List")
	require.Equal(t, 401, r.Code, "inactividad de 24 h")
	require.Equal(t, "unauthorized", r.errCode(t))
	// Entrar de nuevo funciona.
	require.Equal(t, 200, s.rpcRaw(s.login("admin", pass), "Accounts", "List").Code)
}
