package ws

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/model"
)

// TestStressEnqueueEditAndStop: bajo -race, eventos que entran desde varias goroutines (como los
// Router de varias cuentas), el destino que se edita y se apaga mientras tanto, y Stop con todo
// en vuelo. Stop no puede colgarse ni dejar goroutines vivas.
func TestStressEnqueueEditAndStop(t *testing.T) {
	ts := newTestServer(t, "secreto")
	e := newEnv(t)
	_, ad := e.newAccount("Ventas")
	dst := e.addDest(ts, true, allFilter)
	e.start()
	ts.waitHello()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			chat := fmt.Sprintf("59399900%04d@s.whatsapp.net", g)
			ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat(chat, "Ana")})
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				ad.Inject(model.Event{Type: model.EvMessageNew, Message: incoming(chat, fmt.Sprintf("m%d", n), "hola")})
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; ; n++ {
			select {
			case <-stop:
				return
			default:
			}
			d := dst
			d.Enabled = n%3 != 0
			d.Name = fmt.Sprintf("prueba %d", n)
			if _, err := e.dsts.Save(context.Background(), d); err != nil {
				t.Error(err)
				return
			}
			_ = e.mgr.States()
			time.Sleep(15 * time.Millisecond)
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	done := make(chan struct{})
	go func() {
		e.mgr.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Manager.Stop no terminó")
	}
	// Parar dos veces y volver a arrancar no puede fallar.
	e.mgr.Stop()
	e.mgr.Start(context.Background())
	e.mgr.Stop()
}
