package auth

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoginMeLogout(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)

	r := e.login("ana", goodPass, "")
	require.Equal(t, 200, r.Code, r.Body.String())
	j := r.json()
	tok := j["token"].(string)
	require.Len(t, tok, 43) // 32 bytes en base64url sin relleno
	require.InDelta(t, e.clock.Add(24*time.Hour).UnixMilli(), j["expiresAt"], 1)
	u := j["user"].(map[string]any)
	require.Equal(t, "ana", u["username"])
	require.Equal(t, "admin", u["role"])
	require.NotEmpty(t, u["id"])

	// El usuario se busca sin distinguir mayúsculas.
	require.Equal(t, 200, e.login("ANA", goodPass, "").Code)

	me := e.do("GET", "/api/v1/auth/me", tok, "", nil)
	require.Equal(t, 200, me.Code)
	require.Equal(t, "ana", me.json()["username"])
	require.Equal(t, false, me.json()["local"])

	require.Equal(t, 200, e.do("POST", "/api/v1/auth/logout", tok, "", nil).Code)
	r = e.do("GET", "/api/v1/auth/me", tok, "", nil)
	require.Equal(t, 401, r.Code)
	require.Equal(t, "unauthorized", r.errCode())
}

func TestTokenSoloSeGuardaComoHash(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	tok := e.token("ana", goodPass)
	var n int
	require.NoError(t, e.db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE token_hash = ?`, tok).Scan(&n))
	require.Zero(t, n)
	require.NoError(t, e.db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE token_hash = ?`, hashToken(tok)).Scan(&n))
	require.Equal(t, 1, n)
}

func TestLoginFallidoIgualParaUsuarioInexistente(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	a := e.login("ana", "otra-clave-larga-1", "")
	b := e.login("nadie", "otra-clave-larga-1", "")
	require.Equal(t, 401, a.Code)
	require.Equal(t, 401, b.Code)
	require.Equal(t, a.Body.String(), b.Body.String())
	require.Equal(t, "Usuario o clave incorrectos.", a.json()["error"].(map[string]any)["message"])
	// Usuario desactivado: mismo mensaje.
	u, _ := getUser(t.Context(), e.db, "ana")
	require.NoError(t, setDisabled(t.Context(), e.db, u.ID, true, e.s.now()))
	c := e.login("ana", goodPass, "")
	require.Equal(t, a.Body.String(), c.Body.String())
}

func TestFuerzaBrutaPorIPConBloqueoCreciente(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	ip := "198.51.100.7:1000"
	for i := 0; i < 5; i++ {
		require.Equal(t, 401, e.login(fmt.Sprintf("u%d", i), "mala-clave-larga-1", ip).Code)
	}
	r := e.login("ana", goodPass, ip) // aunque la clave sea buena, está bloqueada
	require.Equal(t, 429, r.Code)
	require.Equal(t, "rate_limited", r.errCode())
	require.Equal(t, "60", r.Header().Get("Retry-After"))

	// Otra IP no queda bloqueada.
	require.Equal(t, 200, e.login("ana", goodPass, "198.51.100.8:1").Code)

	// Pasado el primer bloqueo, otra tanda de fallos duplica el siguiente.
	e.advance(61 * time.Second)
	for i := 0; i < 5; i++ {
		require.Equal(t, 401, e.login("ana", "mala-clave-larga-1", ip).Code)
	}
	r = e.login("ana", goodPass, ip)
	require.Equal(t, 429, r.Code)
	require.Equal(t, "120", r.Header().Get("Retry-After"))

	e.advance(121 * time.Second)
	require.Equal(t, 200, e.login("ana", goodPass, ip).Code, "tras el bloqueo vuelve a funcionar")
}

func TestBloqueoTieneTopeDeUnaHora(t *testing.T) {
	l := newAttemptLimiter(1, time.Minute, time.Now)
	for i := 0; i < 20; i++ {
		l.fail("k")
		rem, b := l.blocked("k")
		require.True(t, b)
		require.LessOrEqual(t, rem, time.Hour)
		l.m["k"].lockUntil = time.Time{} // simula que pasó el bloqueo
	}
	l.fail("k")
	rem, _ := l.blocked("k")
	require.Equal(t, time.Hour, rem.Round(time.Minute))
}

func TestFuerzaBrutaPorUsuarioDesdeVariasIP(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	for i := 0; i < 10; i++ {
		ip := fmt.Sprintf("203.0.113.%d:9", i+1)
		require.Equal(t, 401, e.login("Ana", "mala-clave-larga-1", ip).Code)
	}
	r := e.login("ana", goodPass, "203.0.113.200:9")
	require.Equal(t, 429, r.Code)
	require.NotEmpty(t, r.Header().Get("Retry-After"))
	// Un usuario inexistente se limita igual (no revela si existe).
	for i := 0; i < 10; i++ {
		e.login("fantasma", "mala-clave-larga-1", fmt.Sprintf("203.0.113.%d:9", 100+i))
	}
	require.Equal(t, 429, e.login("fantasma", "x", "203.0.113.250:9").Code)
}

func TestExitosNoCuentanParaElLimite(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	for i := 0; i < 30; i++ {
		require.Equal(t, 200, e.login("ana", goodPass, "198.51.100.9:1").Code)
	}
}

func TestTokenCaducadoPorInactividadYDeslizante(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	tok := e.token("ana", goodPass)

	// Usarlo cada 20 h lo mantiene vivo (ventana deslizante).
	for i := 0; i < 3; i++ {
		e.advance(20 * time.Hour)
		require.Equal(t, 200, e.do("POST", "/api/v1/rpc/x/y", tok, "", nil).Code, "vuelta %d", i)
	}
	// 25 h sin usar: caduca.
	e.advance(25 * time.Hour)
	require.Equal(t, 401, e.do("POST", "/api/v1/rpc/x/y", tok, "", nil).Code)
}

func TestTokenCaducaALos30DiasAunqueSeUse(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	tok := e.token("ana", goodPass)
	for i := 0; i < 29; i++ {
		e.advance(23 * time.Hour)
		r := e.do("GET", "/api/v1/auth/me", tok, "", nil)
		if r.Code != 200 { // 29*23h = 27,8 días: aún vive
			t.Fatalf("vuelta %d: %d", i, r.Code)
		}
	}
	e.advance(23 * time.Hour)
	e.advance(23 * time.Hour)
	require.Equal(t, 401, e.do("GET", "/api/v1/auth/me", tok, "", nil).Code)
}

func TestTokenManipuladoOAjeno(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	e.addUser("beto", goodPass)
	ta, tb := e.token("ana", goodPass), e.token("beto", goodPass)

	for _, bad := range []string{
		ta[:len(ta)-1] + "x", "x" + ta[1:], ta + "a", ta[:20], strings.ToUpper(ta), "Bearer", "", "null", strings.Repeat("a", 5000),
	} {
		if bad == ta {
			continue
		}
		require.Equal(t, 401, e.do("GET", "/api/v1/auth/me", bad, "", nil).Code, "token %.10q", bad)
	}
	// Cada token pertenece a su usuario.
	require.Equal(t, "ana", e.do("GET", "/api/v1/auth/me", ta, "", nil).json()["username"])
	require.Equal(t, "beto", e.do("GET", "/api/v1/auth/me", tb, "", nil).json()["username"])
	// Cerrar la sesión de ana no toca la de beto.
	e.do("POST", "/api/v1/auth/logout", ta, "", nil)
	require.Equal(t, 200, e.do("GET", "/api/v1/auth/me", tb, "", nil).Code)
}

func TestSchemeBearerSinDistinguirMayusculasYQueryNoSirve(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	tok := e.token("ana", goodPass)
	r := httpReq(e, "GET", "/api/v1/rpc/a/b", map[string]string{"Authorization": "bearer " + tok})
	require.Equal(t, 200, r.Code)
	// ?token= con un token de sesión: ni en /rpc ni en medios.
	require.Equal(t, 401, e.do("POST", "/api/v1/rpc/a/b?token="+tok, "", "", nil).Code)
	require.Equal(t, 401, e.do("GET", "/api/v1/media/c/m?token="+tok, "", "", nil).Code)
}

func httpReq(e *env, method, target string, hdr map[string]string) resp {
	r := newReq(method, target)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := newRec()
	e.mux.ServeHTTP(w, r)
	return resp{w}
}

func TestTokensInvalidosTrasCambioDeClave(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	t1, t2 := e.token("ana", goodPass), e.token("ana", goodPass)
	media := e.do("POST", "/api/v1/auth/media-token", t1, "", nil).json()["token"].(string)
	require.Equal(t, 200, e.do("GET", "/api/v1/media/a/b", media, "", nil).Code)

	// Clave actual incorrecta, clave corta e igual: se rechazan sin cambiar nada.
	r := e.do("POST", "/api/v1/auth/password", t1, "", map[string]string{"currentPassword": "no-es-la-clave-1", "newPassword": "nueva-clave-larga-1"})
	require.Equal(t, 401, r.Code)
	r = e.do("POST", "/api/v1/auth/password", t1, "", map[string]string{"currentPassword": goodPass, "newPassword": "corta"})
	require.Equal(t, 400, r.Code)
	require.Equal(t, "invalid", r.errCode())
	r = e.do("POST", "/api/v1/auth/password", t1, "", map[string]string{"currentPassword": goodPass, "newPassword": goodPass})
	require.Equal(t, 400, r.Code)
	require.Equal(t, 200, e.do("GET", "/api/v1/auth/me", t2, "", nil).Code)

	r = e.do("POST", "/api/v1/auth/password", t1, "", map[string]string{"currentPassword": goodPass, "newPassword": "nueva-clave-larga-1"})
	require.Equal(t, 200, r.Code, r.Body.String())
	for _, old := range []string{t1, t2} {
		require.Equal(t, 401, e.do("GET", "/api/v1/auth/me", old, "", nil).Code)
	}
	require.Equal(t, 401, e.do("GET", "/api/v1/media/a/b", media, "", nil).Code, "el media-token también muere")
	// Se devuelve una sesión nueva válida; la clave vieja ya no entra y la nueva sí.
	nt := r.json()["token"].(string)
	require.Equal(t, 200, e.do("GET", "/api/v1/auth/me", nt, "", nil).Code)
	require.Equal(t, 401, e.login("ana", goodPass, "").Code)
	require.Equal(t, 200, e.login("ana", "nueva-clave-larga-1", "").Code)
}

func TestLogoutInvalidaTodosLosTokensDelUsuario(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	t1, t2 := e.token("ana", goodPass), e.token("ana", goodPass)
	media := e.do("POST", "/api/v1/auth/media-token", t1, "", nil).json()["token"].(string)
	e.do("POST", "/api/v1/auth/logout", t1, "", nil)
	require.Equal(t, 401, e.do("GET", "/api/v1/auth/me", t2, "", nil).Code)
	require.Equal(t, 401, e.do("GET", "/api/v1/media/a/b", media, "", nil).Code)
}

func TestMediaTokenAlcance(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	tok := e.token("ana", goodPass)
	r := e.do("POST", "/api/v1/auth/media-token", tok, "", nil)
	require.Equal(t, 200, r.Code)
	mt := r.json()["token"].(string)
	require.InDelta(t, e.clock.Add(15*time.Minute).UnixMilli(), r.json()["expiresAt"], 1)

	// Por URL en las tres rutas de lectura.
	for _, p := range []string{"/api/v1/media/c/m", "/api/v1/avatar/c/a.jpg", "/api/v1/files/abc"} {
		ok := e.do("GET", p+"?token="+mt, "", "", nil)
		require.Equal(t, 200, ok.Code, p)
		require.NotContains(t, ok.Body.String(), mt, "?token= se quita de la URL antes de llegar al manejador")
		require.Equal(t, 200, e.do("GET", p, mt, "", nil).Code, "también por Bearer en "+p)
	}
	// Fuera de alcance: RPC, auth, escritura, otras rutas.
	require.Equal(t, 403, e.do("POST", "/api/v1/rpc/a/b", mt, "", nil).Code)
	require.Equal(t, "forbidden", e.do("POST", "/api/v1/rpc/a/b", mt, "", nil).errCode())
	require.Equal(t, 403, e.do("GET", "/api/v1/auth/me", mt, "", nil).Code)
	require.Equal(t, 403, e.do("POST", "/api/v1/auth/media-token", mt, "", nil).Code)
	require.Equal(t, 403, e.do("POST", "/api/v1/auth/ws-ticket", mt, "", nil).Code)
	require.Equal(t, 403, e.do("POST", "/api/v1/auth/logout", mt, "", nil).Code)
	require.Equal(t, 403, e.do("POST", "/api/v1/files?token="+mt, "", "", nil).Code, "subir no es lectura")
	require.Equal(t, 403, e.do("DELETE", "/api/v1/files/abc?token="+mt, "", "", nil).Code)
	// Mismo token en un mux que lo aplique a una ruta no listada.
	e.mux.Handle("/api/v1/otra/", e.s.RequireSessionOrMedia(http.NotFoundHandler()))
	require.Equal(t, 403, e.do("GET", "/api/v1/otra/x?token="+mt, "", "", nil).Code)

	// Caduca a los 15 minutos.
	e.advance(16 * time.Minute)
	require.Equal(t, 401, e.do("GET", "/api/v1/media/c/m?token="+mt, "", "", nil).Code)
	// La sesión sigue (no pasó 24 h) y puede pedir otro.
	require.Equal(t, 200, e.do("POST", "/api/v1/auth/media-token", tok, "", nil).Code)
}

func TestTicketDeUnSoloUsoYCaducidad(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	tok := e.token("ana", goodPass)
	r := e.do("POST", "/api/v1/auth/ws-ticket", tok, "", nil)
	require.Equal(t, 200, r.Code)
	tk := r.json()["ticket"].(string)
	require.InDelta(t, e.clock.Add(30*time.Second).UnixMilli(), r.json()["expiresAt"], 1)

	u, ok := e.s.RedeemWSTicket(tk)
	require.True(t, ok)
	require.Equal(t, "ana", u)
	_, ok = e.s.RedeemWSTicket(tk)
	require.False(t, ok, "un ticket no se reutiliza")

	tk2 := e.do("POST", "/api/v1/auth/ws-ticket", tok, "", nil).json()["ticket"].(string)
	e.advance(31 * time.Second)
	_, ok = e.s.RedeemWSTicket(tk2)
	require.False(t, ok, "caducado")

	for _, bad := range []string{"", "inventado", tok, strings.Repeat("z", 1000)} {
		_, ok = e.s.RedeemWSTicket(bad)
		require.False(t, ok)
	}
	// El token de sesión no sirve como ticket y el ticket no sirve como sesión.
	tk3 := e.do("POST", "/api/v1/auth/ws-ticket", tok, "", nil).json()["ticket"].(string)
	require.Equal(t, 401, e.do("GET", "/api/v1/auth/me", tk3, "", nil).Code)
}

func TestSinSesionEnTodasLasRutas(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	cases := [][2]string{
		{"POST", "/api/v1/auth/logout"}, {"GET", "/api/v1/auth/me"}, {"POST", "/api/v1/auth/password"},
		{"POST", "/api/v1/auth/ws-ticket"}, {"POST", "/api/v1/auth/media-token"},
		{"POST", "/api/v1/rpc/chat/list"}, {"GET", "/api/v1/media/c/m"}, {"GET", "/api/v1/avatar/c/a"},
		{"GET", "/api/v1/files/x"}, {"POST", "/api/v1/files"},
	}
	for _, c := range cases {
		r := e.do(c[0], c[1], "", "", nil)
		require.Equal(t, 401, r.Code, c[1])
		require.Equal(t, "unauthorized", r.errCode(), c[1])
	}
	require.Zero(t, e.hits)
	// Rutas inexistentes y métodos equivocados.
	require.Equal(t, 404, e.do("POST", "/api/v1/auth/nada", "", "", nil).Code)
	r := e.do("GET", "/api/v1/auth/login", "", "", nil)
	require.Equal(t, 405, r.Code)
	require.Equal(t, "POST", r.Header().Get("Allow"))
}

func TestUsuarioDesactivadoPierdeLasSesiones(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	tok := e.token("ana", goodPass)
	u, _ := getUser(t.Context(), e.db, "ana")
	require.NoError(t, setDisabled(t.Context(), e.db, u.ID, true, e.s.now()))
	require.Equal(t, 401, e.do("GET", "/api/v1/auth/me", tok, "", nil).Code)
	require.Equal(t, 401, e.login("ana", goodPass, "").Code)
	require.NoError(t, setDisabled(t.Context(), e.db, u.ID, false, e.s.now()))
	require.Equal(t, 200, e.login("ana", goodPass, "").Code)
}

func TestLimitePorMinutoPorToken(t *testing.T) {
	e := newEnv(t, Options{RatePerMinute: 600})
	e.addUser("ana", goodPass)
	e.addUser("beto", goodPass)
	ta, tb := e.token("ana", goodPass), e.token("beto", goodPass)
	for i := 0; i < 600; i++ {
		if c := e.do("POST", "/api/v1/rpc/a/b", ta, "", nil).Code; c != 200 {
			t.Fatalf("petición %d: %d", i, c)
		}
	}
	r := e.do("POST", "/api/v1/rpc/a/b", ta, "", nil)
	require.Equal(t, 429, r.Code)
	require.Equal(t, "rate_limited", r.errCode())
	require.NotEmpty(t, r.Header().Get("Retry-After"))
	require.Equal(t, 200, e.do("POST", "/api/v1/rpc/a/b", tb, "", nil).Code, "otro token no se ve afectado")
	e.advance(61 * time.Second)
	require.Equal(t, 200, e.do("POST", "/api/v1/rpc/a/b", ta, "", nil).Code)
}

func TestLimiteGeneralPorDefectoEs3000(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	ta := e.token("ana", goodPass)
	for i := 0; i < 3000; i++ {
		if c := e.do("POST", "/api/v1/rpc/a/b", ta, "", nil).Code; c != 200 {
			t.Fatalf("petición %d: %d", i, c)
		}
	}
	require.Equal(t, 429, e.do("POST", "/api/v1/rpc/a/b", ta, "", nil).Code)
}

func TestElTokenLocalNoSeLimita(t *testing.T) {
	const lt = "token-local-de-prueba-0123456789"
	e := newEnv(t, Options{LocalToken: lt, RatePerMinute: 50})
	for i := 0; i < 400; i++ {
		if c := e.do("POST", "/api/v1/rpc/a/b", lt, "127.0.0.1:4000", nil).Code; c != 200 {
			t.Fatalf("petición local %d: %d", i, c)
		}
	}
}

func TestAlAlcanzarElLimiteSeRegistranLasRutasMasLlamadas(t *testing.T) {
	e := newEnv(t, Options{RatePerMinute: 20})
	e.addUser("ana", goodPass)
	ta := e.token("ana", goodPass)
	for i := 0; i < 15; i++ {
		e.do("POST", "/api/v1/rpc/Inbox/Messages", ta, "", nil)
	}
	for i := 0; i < 10; i++ {
		e.do("POST", "/api/v1/rpc/Inbox/ListChats", ta, "", nil)
	}
	out := e.logs.String()
	require.Contains(t, out, "límite de peticiones alcanzado")
	require.Contains(t, out, "/api/v1/rpc/Inbox/Messages=15")
	require.Equal(t, 1, strings.Count(out, "límite de peticiones alcanzado"), "se avisa una sola vez por ventana")
	require.NotContains(t, out, goodPass)
}

func TestModoLocal(t *testing.T) {
	const lt = "token-local-de-prueba-0123456789"
	e := newEnv(t, Options{LocalToken: lt})

	r := e.do("POST", "/api/v1/rpc/a/b", lt, "127.0.0.1:4000", nil)
	require.Equal(t, 200, r.Code)
	require.Equal(t, "local", r.json()["user"])
	require.Equal(t, true, r.json()["local"])
	require.Equal(t, 200, e.do("POST", "/api/v1/rpc/a/b", lt, "[::1]:4000", nil).Code)
	me := e.do("GET", "/api/v1/auth/me", lt, "127.0.0.1:1", nil)
	require.Equal(t, "local", me.json()["username"])
	require.Equal(t, true, me.json()["local"])

	// Desde una IP que no es loopback, el token local no vale.
	for _, ip := range []string{"192.0.2.5:4000", "10.0.0.2:1", "[2001:db8::1]:1"} {
		r = e.do("POST", "/api/v1/rpc/a/b", lt, ip, nil)
		require.Equal(t, 401, r.Code, ip)
		require.Equal(t, 401, e.do("GET", "/api/v1/media/c/m", lt, ip, nil).Code, ip)
	}
	// Tampoco detrás de un proxy local.
	req := newReq("POST", "/api/v1/rpc/a/b")
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Authorization", "Bearer "+lt)
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	w := newRec()
	e.mux.ServeHTTP(w, req)
	require.Equal(t, 401, w.Code)

	// Token local parecido: no.
	require.Equal(t, 401, e.do("POST", "/api/v1/rpc/a/b", lt+"x", "127.0.0.1:1", nil).Code)
	require.Equal(t, 401, e.do("POST", "/api/v1/rpc/a/b", lt[:len(lt)-1], "127.0.0.1:1", nil).Code)
	// Nunca por la URL.
	require.Equal(t, 401, e.do("GET", "/api/v1/media/c/m?token="+lt, "", "127.0.0.1:1", nil).Code)

	// ws-ticket y media-token con el token local.
	tk := e.do("POST", "/api/v1/auth/ws-ticket", lt, "127.0.0.1:1", nil).json()["ticket"].(string)
	u, ok := e.s.RedeemWSTicket(tk)
	require.True(t, ok)
	require.Equal(t, "local", u)
	mr := e.do("POST", "/api/v1/auth/media-token", lt, "127.0.0.1:1", nil)
	require.Equal(t, 200, mr.Code, mr.Body.String())
	mt := mr.json()["token"].(string)
	m := e.do("GET", "/api/v1/media/c/m?token="+mt, "", "192.0.2.77:1", nil)
	require.Equal(t, 200, m.Code)
	require.Equal(t, false, m.json()["local"], "el media-token local no da rutas absolutas")
	require.Equal(t, 403, e.do("POST", "/api/v1/rpc/a/b", mt, "127.0.0.1:1", nil).Code)
	// Cambiar la clave del usuario local no existe; logout no falla.
	require.Equal(t, 403, e.do("POST", "/api/v1/auth/password", lt, "127.0.0.1:1", map[string]string{"currentPassword": "a", "newPassword": "b"}).Code)
	require.Equal(t, 200, e.do("POST", "/api/v1/auth/logout", lt, "127.0.0.1:1", nil).Code)
	require.Equal(t, 200, e.do("GET", "/api/v1/auth/me", lt, "127.0.0.1:1", nil).Code)
}

func TestModoLocalDesactivadoSiEscuchaFueraDeLoopbackOSinToken(t *testing.T) {
	const lt = "token-local-de-prueba-0123456789"
	e := newEnv(t, Options{LocalToken: lt, ListenNonLoopback: true})
	require.Equal(t, 401, e.do("POST", "/api/v1/rpc/a/b", lt, "127.0.0.1:1", nil).Code)
	e2 := newEnv(t, Options{})
	require.Equal(t, 401, e2.do("POST", "/api/v1/rpc/a/b", "", "127.0.0.1:1", nil).Code)
	require.False(t, e2.s.IsLocal(newReq("GET", "/")))
	require.Equal(t, "", e2.s.Username(newReq("GET", "/")))

	_, err := NewService(Options{DB: e.db, LocalToken: "corto"})
	require.Error(t, err)
}

func TestNingunRegistroContieneSecretos(t *testing.T) {
	const lt = "token-local-de-prueba-0123456789"
	e := newEnv(t, Options{LocalToken: lt})
	e.addUser("ana", goodPass)
	const secretPass = "clave-secreta-distintiva-77"
	e.addUser("beto", secretPass)

	var secrets []string
	secrets = append(secrets, lt, goodPass, secretPass, "nueva-clave-secreta-88", "mala-clave-secreta-99")
	for i := 0; i < 8; i++ {
		e.login("beto", "mala-clave-secreta-99", "198.51.100.1:1") // incluye bloqueo
	}
	e.login("ana", "mala-clave-secreta-99", "198.51.100.2:1")
	e.login("nadie-secreto-xyz", "mala-clave-secreta-99", "198.51.100.3:1")
	e.advance(time.Hour)
	tok := e.token("beto", secretPass)
	secrets = append(secrets, tok)
	mt := e.do("POST", "/api/v1/auth/media-token", tok, "", nil).json()["token"].(string)
	tk := e.do("POST", "/api/v1/auth/ws-ticket", tok, "", nil).json()["ticket"].(string)
	secrets = append(secrets, mt, tk)
	e.do("GET", "/api/v1/media/c/m?token="+mt, "", "", nil)
	e.do("GET", "/api/v1/auth/me", tok+"x", "", nil)
	e.do("POST", "/api/v1/rpc/a/b", lt, "192.0.2.5:1", nil)
	e.s.RedeemWSTicket(tk)
	e.do("POST", "/api/v1/auth/password", tok, "", map[string]string{"currentPassword": secretPass, "newPassword": "nueva-clave-secreta-88"})
	e.do("POST", "/api/v1/auth/password", tok, "", map[string]string{"currentPassword": "mala-clave-secreta-99", "newPassword": "nueva-clave-secreta-88"})
	t.Setenv("DCONNECTOR_ADMIN_PASSWORD", "corta")
	_, _, _ = newEnv(t, Options{}).s.EnsureAdmin(t.Context(), "admin")

	logs := e.logs.String()
	require.NotEmpty(t, logs, "la prueba debe haber capturado algo")
	for _, s := range secrets {
		require.NotContains(t, logs, s)
	}
	require.NotContains(t, logs, "token=")
	require.NotContains(t, logs, "ticket=")
	require.NotContains(t, logs, "nadie-secreto-xyz", "no se registran nombres de intentos fallidos")
	require.NotContains(t, logs, "password")
	require.NotContains(t, logs, "argon2id$")
}

func TestHashArgon2idPHCYMinimo(t *testing.T) {
	h, err := hashPassword(goodPass, defaultParams)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$"), h)
	require.Len(t, strings.Split(h, "$"), 6)
	ok, rehash := verifyPassword(goodPass, h, defaultParams)
	require.True(t, ok)
	require.False(t, rehash)
	ok, _ = verifyPassword(goodPass+"x", h, defaultParams)
	require.False(t, ok)
	// Parámetros más débiles piden rehash.
	weak, _ := hashPassword(goodPass, argonParams{m: 16, t: 1, p: 1, saltLen: 16, keyLen: 32})
	ok, rehash = verifyPassword(goodPass, weak, defaultParams)
	require.True(t, ok)
	require.True(t, rehash)
	// Cadenas basura o con parámetros abusivos no revientan ni gastan memoria.
	for _, bad := range []string{"", "x", "$argon2id$v=19$m=4294967295,t=1,p=1$AAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAA", "$bcrypt$a$b$c$d"} {
		ok, _ = verifyPassword("x", bad, defaultParams)
		require.False(t, ok)
	}
	require.Error(t, validatePassword("onceonceonc"))
	require.NoError(t, validatePassword("docedocedoce"))
	require.Error(t, validatePassword(strings.Repeat("a", 2000)))
}

func TestLoginRehashaParametrosViejos(t *testing.T) {
	e := newEnv(t, Options{})
	old := e.s.params
	e.s.params = argonParams{m: 8, t: 1, p: 1, saltLen: 16, keyLen: 32}
	e.addUser("ana", goodPass)
	e.s.params = old
	require.Equal(t, 200, e.login("ana", goodPass, "").Code)
	u, _ := getUser(t.Context(), e.db, "ana")
	require.Contains(t, u.Hash, "m=16,t=1,p=1")
}

func TestLoginCuerpoInvalido(t *testing.T) {
	e := newEnv(t, Options{})
	r := e.do("POST", "/api/v1/auth/login", "", "", nil)
	require.Equal(t, 400, r.Code)
	require.Equal(t, "invalid", r.errCode())
	r = e.do("POST", "/api/v1/auth/login", "", "", map[string]string{"username": "", "password": ""})
	require.Equal(t, 400, r.Code)
}

func TestEnsureAdmin(t *testing.T) {
	t.Setenv("DCONNECTOR_ADMIN_PASSWORD", "")
	e := newEnv(t, Options{})
	pw, created, err := e.s.EnsureAdmin(t.Context(), "")
	require.NoError(t, err)
	require.True(t, created)
	require.Len(t, pw, 24)
	require.Equal(t, 200, e.login("admin", pw, "").Code)
	// Segunda vez: no hace nada.
	pw2, created, err := e.s.EnsureAdmin(t.Context(), "admin")
	require.NoError(t, err)
	require.False(t, created)
	require.Empty(t, pw2)
	// Aleatoria: dos arranques distintos dan claves distintas.
	e2 := newEnv(t, Options{})
	pw3, _, _ := e2.s.EnsureAdmin(t.Context(), "root")
	require.NotEqual(t, pw, pw3)
	require.NotContains(t, e.logs.String(), pw)
	require.NotContains(t, e2.logs.String(), pw3)

	// Con variable de entorno: se usa y no se devuelve.
	t.Setenv("DCONNECTOR_ADMIN_PASSWORD", "clave-desde-entorno-1")
	e3 := newEnv(t, Options{})
	pw4, created, err := e3.s.EnsureAdmin(t.Context(), "admin")
	require.NoError(t, err)
	require.True(t, created)
	require.Empty(t, pw4)
	require.Equal(t, 200, e3.login("admin", "clave-desde-entorno-1", "").Code)
	// Variable demasiado corta: error sin eco de la clave.
	t.Setenv("DCONNECTOR_ADMIN_PASSWORD", "corta")
	_, _, err = newEnv(t, Options{}).s.EnsureAdmin(t.Context(), "admin")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "corta")
	// Nombre inválido.
	_, _, err = newEnv(t, Options{}).s.EnsureAdmin(t.Context(), "local")
	require.Error(t, err)
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(true, SandboxHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	w := newRec()
	h.ServeHTTP(w, newReq("GET", "/"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "sandbox", w.Header().Get("Content-Security-Policy"))
	require.Equal(t, "max-age=31536000", w.Header().Get("Strict-Transport-Security"))
	w = newRec()
	SecurityHeaders(false, http.NotFoundHandler()).ServeHTTP(w, newReq("GET", "/"))
	require.Empty(t, w.Header().Get("Strict-Transport-Security"))
	require.Equal(t, "GET /x", RequestLabel(newReq("GET", "/x?token=secreto")))
}
