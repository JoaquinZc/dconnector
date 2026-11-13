package ws

import (
	"context"
	"strings"
	"testing"
	"time"

	"dconnector/internal/core/model"
)

func TestAcceptsFilter(t *testing.T) {
	const a1, a2 = model.AccountID("wa-1"), model.AccountID("tg-1")
	e := newEnv(t)
	acc, _ := e.newAccount("Ventas")
	// Un grupo y un directo reales en el almacén para que el filtro consulte su tipo.
	st, err := e.accounts.Store(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = st.UpsertChat(ctx, model.Chat{NativeID: "g1", Kind: model.ChatGroup, Title: "Grupo"})
	_ = st.UpsertChat(ctx, model.Chat{NativeID: "d1", Kind: model.ChatDirect, Title: "Ana"})
	lk := &lookups{m: e.mgr, ctx: ctx}

	msg := func(chat string, fromMe bool) *model.Message {
		return &model.Message{ChatNativeID: chat, FromMe: fromMe}
	}
	ev := func(t model.EventType, m *model.Message) model.Event {
		return model.Event{Type: t, AccountID: acc.ID, Channel: model.ChannelWhatsApp, Message: m}
	}
	cases := []struct {
		name string
		f    model.DestinationFilter
		ev   model.Event
		want bool
	}{
		{"todo abierto", model.DestinationFilter{IncludeFromMe: true, IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", false)), true},
		{"cuenta no incluida", model.DestinationFilter{AccountIDs: []model.AccountID{a1, a2}, IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", false)), false},
		{"cuenta incluida", model.DestinationFilter{AccountIDs: []model.AccountID{acc.ID}, IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", false)), true},
		{"canal no incluido", model.DestinationFilter{Channels: []model.Channel{model.ChannelTelegram}, IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", false)), false},
		{"canal incluido", model.DestinationFilter{Channels: []model.Channel{model.ChannelWhatsApp}, IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", false)), true},
		{"evento no incluido", model.DestinationFilter{Events: []string{"reaction"}, IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", false)), false},
		{"evento incluido", model.DestinationFilter{Events: []string{"message.received"}, IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", false)), true},
		{"propio recibido sin includeFromMe", model.DestinationFilter{IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", true)), false},
		{"propio recibido con includeFromMe", model.DestinationFilter{IncludeFromMe: true, IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", true)), true},
		{"ajeno recibido sin includeFromMe", model.DestinationFilter{IncludeGroups: true}, ev(model.EvMessageNew, msg("d1", false)), true},
		{"message.sent sin includeFromMe", model.DestinationFilter{IncludeGroups: true}, ev(model.EvMessageSent, msg("d1", true)), false},
		{"message.sent con includeFromMe", model.DestinationFilter{IncludeFromMe: true, IncludeGroups: true}, ev(model.EvMessageSent, msg("d1", true)), true},
		{"updated de lo propio sin includeFromMe", model.DestinationFilter{IncludeGroups: true}, ev(model.EvMessageUpdate, msg("d1", true)), false},
		{"updated de lo ajeno sin includeFromMe", model.DestinationFilter{IncludeGroups: true}, ev(model.EvMessageUpdate, msg("d1", false)), true},
		{"grupo sin includeGroups", model.DestinationFilter{IncludeFromMe: true}, ev(model.EvMessageNew, msg("g1", false)), false},
		{"grupo con includeGroups", model.DestinationFilter{IncludeFromMe: true, IncludeGroups: true}, ev(model.EvMessageNew, msg("g1", false)), true},
		{"directo sin includeGroups", model.DestinationFilter{IncludeFromMe: true}, ev(model.EvMessageNew, msg("d1", false)), true},
		{"chat de grupo sin includeGroups", model.DestinationFilter{IncludeFromMe: true},
			model.Event{Type: model.EvChatUpsert, AccountID: acc.ID, Channel: model.ChannelWhatsApp, Chat: &model.Chat{NativeID: "g1", Kind: model.ChatGroup}}, false},
		{"reacción en grupo sin includeGroups", model.DestinationFilter{IncludeFromMe: true},
			model.Event{Type: model.EvReaction, AccountID: acc.ID, Channel: model.ChannelWhatsApp,
				Reaction: &model.ReactionEvent{MessageKey: model.NewMessageKey(acc.ID, "g1", "m")}}, false},
		{"estado de cuenta no es de un chat", model.DestinationFilter{},
			model.Event{Type: model.EvAccountStatus, AccountID: acc.ID, Channel: model.ChannelWhatsApp}, true},
		{"chat desconocido se trata como directo", model.DestinationFilter{IncludeFromMe: true}, ev(model.EvMessageNew, msg("nuevo", false)), true},
	}
	for _, c := range cases {
		name := string(c.ev.Type)
		if got := accepts(c.f, name, c.ev, lk); got != c.want {
			t.Errorf("%s: accepts = %v, quería %v", c.name, got, c.want)
		}
	}
}

func TestEnqueueNeverNeedsTheNetwork(t *testing.T) {
	// El destino apunta a un puerto cerrado: el cliente no conecta, pero los eventos se guardan.
	ts := newTestServer(t, "t")
	ts.srv.Close()
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	on := e.addDest(ts, false, allFilter)
	off, err := e.dsts.Save(context.Background(), model.Destination{
		Name: "apagado", URL: ts.wsURL(), Token: "t", Enabled: false, Filter: allFilter,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.start()

	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "Ana")})
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incoming("c1", "m1", "hola")})
	waitFor(t, "eventos en el outbox", func() bool { return e.pending(on.ID) >= 2 })
	if e.pending(off.ID) != 0 {
		t.Fatalf("un destino apagado no acumula eventos: %d", e.pending(off.ID))
	}
	// Los eventos esperan en el outbox mientras no haya conexión.
	waitFor(t, "estado en error", func() bool { return e.stateOf(on.ID).State == "error" })

	// Pending cuenta lo guardado aunque el destino no esté habilitado.
	if err := e.app.OutboxAdd(context.Background(), off.ID, "x1", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	var offState, onState model.DestinationState
	for _, s := range e.mgr.States() {
		switch s.DestinationID {
		case off.ID:
			offState = s
		case on.ID:
			onState = s
		}
	}
	if offState.State != "off" || offState.Pending != 1 {
		t.Fatalf("destino apagado: %+v", offState)
	}
	if onState.State != "error" || onState.Pending < 2 || onState.LastError == "" {
		t.Fatalf("destino habilitado sin conexión: %+v", onState)
	}
	_ = acc
}

func TestEnqueueSkipsEventsThatDoNotGoThroughTheProtocol(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	e.newAccount("Ventas")
	dst := e.addDest(ts, false, allFilter)
	e.start()
	ts.waitHello()
	ctx := context.Background()
	for _, ev := range []model.Event{
		{Type: model.EvHistoryBatch, AccountID: "wa-1", History: &model.HistoryBatch{}},
		{Type: model.EvTyping, AccountID: "wa-1", Typing: &model.TypingEvent{}},
		{Type: model.EvChatRemoved, AccountID: "wa-1", ChatKey: "wa-1|x"},
	} {
		e.mgr.Enqueue(ctx, ev)
	}
	time.Sleep(100 * time.Millisecond)
	if n := e.pending(dst.ID); n != 0 {
		t.Fatalf("history.batch, typing y chat.removed no van al protocolo: %d en el outbox", n)
	}
}

func TestQRAndPairCodeNeverLeave(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, _ := e.newAccount("Ventas")
	dst := e.addDest(ts, false, allFilter)
	e.start()
	ts.waitHello()
	e.mgr.Enqueue(context.Background(), model.Event{
		ID: "ev-qr", Type: model.EvAccountStatus, AccountID: acc.ID, Channel: model.ChannelWhatsApp, At: time.Now().UnixMilli(),
		AccountStatus: &model.AccountStatus{AccountID: acc.ID, State: model.StatePairing, Need: model.NeedScanQR,
			QRCode: "2@SECRETO-QR", PairCode: "ABCD-EFGH", Detail: "Escanea"},
	})
	ev := ts.waitEvent("account.state", func(f frameIn) bool { return f.ID == "ev-qr" })
	raw := string(ev.Data)
	if strings.Contains(raw, "SECRETO-QR") || strings.Contains(raw, "ABCD-EFGH") || strings.Contains(raw, "qrCode") || strings.Contains(raw, "pairCode") {
		t.Fatalf("el QR y el código de vinculación nunca salen por el protocolo: %s", raw)
	}
	d := jsonOf(t, ev.Data)
	if d["state"] != "pairing" || d["need"] != "scan_qr" {
		t.Fatalf("account.state: %v", d)
	}
	waitFor(t, "ack", func() bool { return e.pending(dst.ID) == 0 })
}

func TestEventIDIsStableAcrossDestinations(t *testing.T) {
	ts1, ts2 := newTestServer(t, "t"), newTestServer(t, "t")
	e := newEnv(t)
	_, ad := e.newAccount("Ventas")
	e.addDest(ts1, false, allFilter)
	e.addDest(ts2, false, allFilter)
	e.start()
	ts1.waitHello()
	ts2.waitHello()
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "Ana")})
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incoming("c1", "m1", "hola")})
	a := ts1.waitEvent("message.received", nil)
	b := ts2.waitEvent("message.received", nil)
	if a.ID == "" || a.ID != b.ID {
		t.Fatalf("el id del evento debe ser el mismo en todos los destinos: %q %q", a.ID, b.ID)
	}
}
