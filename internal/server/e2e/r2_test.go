package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"dconnector/internal/server"
)

func serverGoroutines() int {
	buf := make([]byte, 4<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "dconnector/internal/") && !strings.Contains(g, "_test.go") && !strings.Contains(g, "testing.tRunner") {
			n++
		}
	}
	return n
}

// Apagado con conexiones WebSocket abiertas (unas leyendo, otras que NUNCA leen) y una ráfaga de eventos en
// curso: termina en menos de ShutdownTimeout, cierra con 1001 a los que leen y no deja goroutinas del servidor.
func TestShutdownWithBusyAndStalledWebSocketsDoesNotLeak(t *testing.T) {
	adapters := useFake(t, nil)
	base := serverGoroutines()
	s := start(t, baseCfg(t))
	tok := s.login("admin", s.adminPassword())
	_, ad := newAccount(t, s, tok, adapters)

	var readers []*wsClient
	for i := 0; i < 4; i++ {
		w := s.ws(tok)
		require.Equal(t, "hello", w.next().Event)
		readers = append(readers, w)
	}
	// Dos que nunca leen (consumidor lento): el servidor no puede quedarse bloqueado escribiéndoles.
	var stalled []*websocket.Conn
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		u := "ws://" + s.addr + "/api/v1/events?ticket=" + s.ticket(tok)
		c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: s.client})
		cancel()
		require.NoError(t, err)
		stalled = append(stalled, c)
	}
	t.Cleanup(func() {
		for _, c := range stalled {
			_ = c.CloseNow()
		}
	})

	// Ráfaga de mensajes mientras se apaga.
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; !stop.Load() && n < 5000; n++ {
			incomingText(ad, "593111", fmt.Sprintf("m-%d", n), strings.Repeat("x", 512))
			if n%50 == 0 {
				time.Sleep(time.Millisecond)
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)

	began := time.Now()
	require.NoError(t, s.stop())
	took := time.Since(began)
	stop.Store(true)
	wg.Wait()
	require.Less(t, took, server.ShutdownTimeout+3*time.Second, "el apagado excedió el plazo (10 s + margen)")
	for _, w := range readers {
		code := w.waitClose()
		require.Contains(t, []websocket.StatusCode{websocket.StatusGoingAway, -1}, code)
	}
	for _, c := range stalled {
		_ = c.CloseNow()
	}
	require.Eventually(t, func() bool { return serverGoroutines() <= base+1 }, 8*time.Second, 100*time.Millisecond,
		"fuga de goroutinas del servidor tras apagar (antes %d, ahora %d)", base, serverGoroutines())
}

// Reinicios seguidos del servidor sobre la misma carpeta (con el candado de carpeta): cada ciclo suelta todo
// y el siguiente arranca; sin fugas acumuladas.
func TestRepeatedRestartsSameDataDirNoLeak(t *testing.T) {
	useFake(t, nil)
	cfg := baseCfg(t)
	base := serverGoroutines()
	for i := 0; i < 6; i++ {
		s := start(t, cfg)
		if i == 0 {
			_ = s.adminPassword()
		}
		tok := ""
		if i == 0 {
			tok = s.login("admin", s.adminPassword())
		}
		if tok != "" {
			w := s.ws(tok)
			require.Equal(t, "hello", w.next().Event)
		}
		require.NoError(t, s.stop())
	}
	require.Eventually(t, func() bool { return serverGoroutines() <= base+1 }, 8*time.Second, 100*time.Millisecond,
		"fuga acumulada tras reinicios (antes %d, ahora %d)", base, serverGoroutines())
}

// Un segundo servidor sobre la misma carpeta de datos se niega con un mensaje claro y no toca al primero.
func TestSecondServerSameDataDirRefuses(t *testing.T) {
	useFake(t, nil)
	cfg := baseCfg(t)
	s := start(t, cfg)
	_, err := tryStart(t, cfg)
	require.Error(t, err)
	require.ErrorIs(t, err, server.ErrDataDirInUse)
	require.Contains(t, err.Error(), "Ya hay un servidor usando esta carpeta de datos")
	// El primero sigue sano.
	s.waitReady()
}

// Si el arranque falla tras construir el núcleo (puerto ocupado), no quedan goroutinas del servidor vivas.
func TestStartFailureAfterCoreBuildDoesNotLeak(t *testing.T) {
	useFake(t, nil)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	base := serverGoroutines()
	for i := 0; i < 5; i++ {
		cfg := baseCfg(t)
		cfg.Listen = occupied.Addr().String()
		cfg.LocalToken = "token-de-prueba-0123456789"
		err := server.Run(context.Background(), cfg, io.Discard, io.Discard)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no se pudo escuchar")
	}
	require.Eventually(t, func() bool { return serverGoroutines() <= base+1 }, 5*time.Second, 50*time.Millisecond,
		"goroutinas de más tras fallos de arranque (antes %d, ahora %d)", base, serverGoroutines())
}
