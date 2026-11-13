package bridge

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/adapter/desktop/client"
)

func goroutinesIn(sub string) int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, sub) {
			n++
		}
	}
	return n
}

// Al volver Serve ya no queda ninguna goroutina del flujo de eventos: si quedara viva, sus avisos tardíos se
// mezclarían con los del Serve siguiente (reinicio del servidor local, cambio de modo).
func TestServeReturnsOnlyAfterEventStreamEnded(t *testing.T) {
	srv, _ := serve(t, nil)
	var mu sync.Mutex
	conn := 0
	b := New(Deps{OnConnection: func(bool, string) { mu.Lock(); conn++; mu.Unlock() }})
	for i := 0; i < 15; i++ {
		cl := client.New(client.Config{BaseURL: srv.URL, Token: func() string { return "t" }, Delay: func(int) time.Duration { return 10 * time.Millisecond }})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { b.Serve(ctx, cl); close(done) }()
		// Cancela en momentos distintos: antes del ticket, durante el WebSocket, ya conectado.
		time.Sleep(time.Duration(i*7) * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Serve no volvió tras cancelar")
		}
		if n := goroutinesIn("(*Client).RunEvents"); n != 0 {
			t.Fatalf("ronda %d: %d goroutinas de RunEvents siguen vivas tras Serve", i, n)
		}
	}
}
