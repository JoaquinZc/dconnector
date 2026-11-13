package ws

import (
	"context"
	"testing"

	"dconnector/internal/core/model"
)

// Prueba de integración: servidor WebSocket de prueba + almacén SQLite real + adaptador falso +
// servicios reales del núcleo (Router, Accounts, Messaging, Inbox…).
func TestIntegration(t *testing.T) {
	ts := newTestServer(t, "secreto")
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	dst := e.addDest(ts, true, allFilter)
	e.start()

	// Saludo.
	hello := ts.waitHello()
	h := jsonOf(t, hello.Data)
	if h["protocol"].(float64) != 1 || h["instanceId"].(string) == "" || h["allowCommands"] != true {
		t.Fatalf("hello: %v", h)
	}
	accounts := h["accounts"].([]any)
	if len(accounts) != 1 || accounts[0].(map[string]any)["id"] != string(acc.ID) {
		t.Fatalf("cuentas del hello: %v", accounts)
	}
	waitFor(t, "destino en línea", func() bool { return e.stateOf(dst.ID).State == "online" })

	// Llega un mensaje por el adaptador falso → el servidor lo recibe → responde ack → outbox vacío.
	const chat = "593999000111@s.whatsapp.net"
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat(chat, "Ana")})
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incoming(chat, "m1", "hola")})
	ev := ts.waitEvent("message.received", nil)
	if ev.Account == nil || ev.Account.ID != string(acc.ID) || ev.Account.Channel != "whatsapp" || ev.Account.Label != "Ventas" {
		t.Fatalf("account del evento: %+v", ev.Account)
	}
	if ev.ID == "" || ev.V != 1 || ev.TS == 0 {
		t.Fatalf("sobre del evento: %+v", ev)
	}
	data := jsonOf(t, ev.Data)
	if data["text"] != "hola" || data["chatNativeId"] != chat || data["chatKind"] != "direct" || data["fromMe"] != false {
		t.Fatalf("data del evento: %v", data)
	}
	waitFor(t, "outbox vacío tras el ack", func() bool { return e.pending(dst.ID) == 0 })

	// Un comando message.send del servidor hace que el adaptador falso envíe.
	rd := ts.command("cmd-1", "message.send", string(acc.ID), map[string]any{
		"chatId": chat, "message": map[string]any{"kind": "text", "text": "Hola desde el servidor"}, "clientRef": "abc",
	})
	if !rd.OK {
		t.Fatalf("message.send falló: %+v", rd.Error)
	}
	sent := ad.Sent()
	if len(sent) != 1 || sent[0].Text != "Hola desde el servidor" || sent[0].Kind != model.KindText {
		t.Fatalf("el adaptador debía enviar el texto: %+v", sent)
	}
	res := jsonOf(t, resultBytes(rd))
	if res["fromMe"] != true || res["text"] != "Hola desde el servidor" {
		t.Fatalf("result de message.send: %v", res)
	}

	// El evento message.sent lleva el clientRef del comando.
	sentEv := ts.waitEvent("message.sent", nil)
	if jsonOf(t, sentEv.Data)["clientRef"] != "abc" {
		t.Fatalf("message.sent sin clientRef: %s", sentEv.Data)
	}
	waitFor(t, "outbox vacío al final", func() bool { return e.pending(dst.ID) == 0 })

	// El registro de estados refleja el destino.
	states := e.mgr.States()
	if len(states) != 1 || states[0].DestinationID != dst.ID || states[0].State != "online" || states[0].Pending != 0 {
		t.Fatalf("estados: %+v", states)
	}
	_ = context.Background()
}
