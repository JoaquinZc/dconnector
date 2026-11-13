package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Una ráfaga de intentos simultáneos no debe poder evaluarse entera: los que esperan turno de argon2 deben
// ver el bloqueo que provocaron los primeros fallos.
func TestRafagaDeLoginsSimultaneosRespetaElLimite(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	// Se ocupan los 4 turnos de argon2: todas las peticiones pasan la primera comprobación antes de que
	// exista un solo fallo contado, y luego se sueltan a la vez.
	for i := 0; i < cap(e.s.sem); i++ {
		e.s.sem <- struct{}{}
	}
	const n = 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.login("ana", "clave-equivocada-"+strings.Repeat("x", 3), "192.0.2.77:1000")
			mu.Lock()
			codes[r.Code]++
			mu.Unlock()
		}()
	}
	time.Sleep(300 * time.Millisecond)
	for i := 0; i < cap(e.s.sem); i++ {
		<-e.s.sem
	}
	wg.Wait()
	// Límite por IP: 5 fallos. Con turnos de 4 en 4 pueden colarse unos pocos más, no decenas.
	require.LessOrEqual(t, codes[401], 12, "intentos evaluados: %v", codes)
	require.GreaterOrEqual(t, codes[429], n-12, "%v", codes)
}

func TestLimitadorIPv6AgrupaPorPrefijo64(t *testing.T) {
	require.Equal(t, limiterIP("2001:db8:1:2:aaaa::1"), limiterIP("2001:db8:1:2:bbbb::9"))
	require.NotEqual(t, limiterIP("2001:db8:1:2::1"), limiterIP("2001:db8:1:3::1"))
	require.Equal(t, "192.0.2.10", limiterIP("192.0.2.10"))
	// Un atacante con un /64 rota de dirección en cada intento y aun así se bloquea.
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	blocked := false
	for i := 0; i < 8; i++ {
		ip := "[2001:db8:1:2::" + string(rune('a'+i)) + "]:1000"
		r := e.login("usuario"+string(rune('a'+i)), "mala-clave-larga-1", ip)
		if r.Code == 429 {
			blocked = true
		}
	}
	require.True(t, blocked, "rotar la dirección dentro de un /64 no debe esquivar el límite por IP")
}

// Con la tabla llena, una clave nueva (el usuario real que se ataca) debe seguir contándose.
func TestLimitadorLlenoNoDejaAdivinarSinLimite(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := newAttemptLimiter(3, time.Hour, func() time.Time { return now })
	l.max = 50
	for i := 0; i < 50; i++ {
		l.fail("user:relleno" + string(rune('A'+i%26)) + string(rune('a'+i/26)))
	}
	require.Len(t, l.m, 50)
	for i := 0; i < 3; i++ {
		l.fail("user:admin")
	}
	_, b := l.blocked("user:admin")
	require.True(t, b, "con la tabla llena el usuario atacado debe bloquearse igual")
	require.LessOrEqual(t, len(l.m), 50)
}

func TestUserActive(t *testing.T) {
	e := newEnv(t, Options{})
	e.addUser("ana", goodPass)
	require.True(t, e.s.UserActive("ana"))
	require.True(t, e.s.UserActive("local"))
	require.False(t, e.s.UserActive("nadie"))
	u, err := getUser(t.Context(), e.db, "ana")
	require.NoError(t, err)
	require.NoError(t, setDisabled(t.Context(), e.db, u.ID, true, e.s.now()))
	require.False(t, e.s.UserActive("ana"))
}

// Un cuerpo de login que se goteaba sin fin mantenía la conexión: ahora hay plazo de lectura.
func TestDecodeBodyPoneUnPlazoDeLectura(t *testing.T) {
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{}`))
	var v map[string]any
	require.True(t, decodeBody(w, r, &v))
	require.False(t, w.deadline.IsZero(), "decodeBody debe fijar SetReadDeadline")
	require.WithinDuration(t, time.Now().Add(15*time.Second), w.deadline, 3*time.Second)
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (d *deadlineRecorder) SetReadDeadline(t time.Time) error { d.deadline = t; return nil }

var _ http.ResponseWriter = (*deadlineRecorder)(nil)
