package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/model"
)

// TestStressLifecycleAndEvents junta, bajo -race, lo que en producción pasa a la vez: adaptadores que
// emiten eventos sin parar, la interfaz que conecta, desconecta y edita cuentas, lecturas de la
// bandeja y de los medios, y el apagado final (StopAll) con eventos todavía en vuelo.
func TestStressLifecycleAndEvents(t *testing.T) {
	e := newTestEnv(t)
	var ids []model.AccountID
	for i := 0; i < 3; i++ {
		acc := e.newAccount(model.ChannelWhatsApp, fmt.Sprintf("cuenta %d", i))
		ids = append(ids, acc.ID)
		e.seedChat(acc.ID, "c1", "Ana", 0)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Adaptadores emitiendo.
	for _, id := range ids {
		ad := e.adapter(id)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				ad.Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("c1", fmt.Sprintf("m%d", n), "hola", int64(1000+n))})
				ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "Ana")})
				ad.Inject(model.Event{Type: model.EvReceipt, Receipt: &model.ReceiptEvent{
					MessageKeys: []model.MessageKey{model.NewMessageKey(id, "c1", fmt.Sprintf("m%d", n))}, Delivery: model.DeliveryRead,
				}})
				time.Sleep(time.Millisecond)
			}
		}()
	}
	// La interfaz: ciclo de vida y lecturas.
	ctx := context.Background()
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = e.accounts.Disconnect(ctx, id)
				_ = e.accounts.Connect(ctx, id)
				_ = e.accounts.Update(ctx, id, fmt.Sprintf("cuenta %d", n), "", true)
				_, _ = e.accounts.List(ctx)
				_ = e.accounts.SetConfig(ctx, id, map[string]string{"k": fmt.Sprint(n)})
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = e.inbox.ListChats(ctx, model.ChatFilter{})
			_, _ = e.inbox.UnreadTotals(ctx)
			for _, id := range ids {
				_ = e.msg.MarkRead(ctx, model.NewChatKey(id, "c1"))
				_, _ = e.msg.Send(ctx, model.NewChatKey(id, "c1"), model.Outgoing{Kind: model.KindText, Text: "x"})
			}
			time.Sleep(time.Millisecond)
		}
	}()

	time.Sleep(600 * time.Millisecond)
	close(stop)
	wg.Wait()

	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = e.accounts.StopAll(sctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("StopAll no terminó")
	}
	// Detener dos veces no debe colgarse ni entrar en pánico.
	_ = e.accounts.StopAll(sctx)
}
