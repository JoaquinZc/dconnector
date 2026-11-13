package service

import (
	"context"
	"testing"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/model"
)

func incomingMsg(chat, id, text string, ts int64) *model.Message {
	return &model.Message{
		NativeID: id, ChatNativeID: chat, SenderNativeID: chat, Timestamp: ts,
		Kind: model.KindText, Text: text, Delivery: model.DeliveryDelivered,
	}
}

func directChat(native, title string) *model.Chat {
	return &model.Chat{NativeID: native, Kind: model.ChatDirect, Title: title, PeerNativeID: native}
}

func TestRouterReceivedMessage(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "Ventas")
	ad := e.adapter(acc.ID)

	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "Ana")})
	e.waitEvent(0, "chat.updated", ofType(model.EvChatUpsert))

	from := e.mark()
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("c1", "m1", "hola", 1000)})
	ev := e.waitEvent(from, "message.received", ofType(model.EvMessageNew))

	if ev.ID == "" || ev.AccountID != acc.ID || ev.Channel != model.ChannelWhatsApp || ev.At == 0 {
		t.Fatalf("faltan campos comunes: %+v", ev)
	}
	m := ev.Message
	if m.Key != model.NewMessageKey(acc.ID, "c1", "m1") || m.ChatKey != model.NewChatKey(acc.ID, "c1") ||
		m.AccountID != acc.ID || m.Channel != model.ChannelWhatsApp {
		t.Fatalf("claves mal puestas: %+v", m)
	}
	if got, ok := e.store(acc.ID).message("c1", "m1"); !ok || got.Text != "hola" {
		t.Fatalf("el mensaje no se guardó: %+v %v", got, ok)
	}
	// El chat cambió: se publica un chat.updated derivado con el no leído.
	snap := e.waitEvent(from, "chat.updated derivado", func(ev model.Event) bool {
		return ev.Type == model.EvChatUpsert && ev.Chat != nil && ev.Chat.UnreadCount == 1
	})
	if snap.Chat.Key != model.NewChatKey(acc.ID, "c1") || snap.Chat.LastPreview != "hola" {
		t.Fatalf("instantánea del chat: %+v", snap.Chat)
	}
}

func TestRouterDuplicateBecomesUpdate(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 0)

	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("c1", "m1", "hola", 1000)})
	e.waitEvent(0, "primera", ofType(model.EvMessageNew))

	from := e.mark()
	again := incomingMsg("c1", "m1", "hola editado", 1000)
	again.EditedAt = 2000
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: again})
	ev := e.waitEvent(from, "message.updated", ofType(model.EvMessageUpdate))
	if ev.Update == nil || ev.Update.Text == nil || *ev.Update.Text != "hola editado" || ev.Update.EditedAt != 2000 {
		t.Fatalf("update mal formado: %+v", ev.Update)
	}
	if ev.Message == nil || ev.Message.Text != "hola editado" {
		t.Fatalf("el evento debe llevar el mensaje completo: %+v", ev.Message)
	}
	for _, x := range e.eventsSince(from) {
		if x.Type == model.EvMessageNew {
			t.Fatal("el duplicado no debe publicarse como message.received")
		}
	}
	// El chat no suma otro no leído.
	if c, _ := e.store(acc.ID).chat("c1"); c.UnreadCount != 1 {
		t.Fatalf("no leídos = %d, quería 1", c.UnreadCount)
	}
}

func TestRouterExistingButNeverPublishedStaysReceived(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	// El mensaje ya estaba en la base (p. ej. por historial) pero nunca se publicó como message.received.
	e.seedMessage(acc.ID, "c1", "m1", 1000, false, "viejo")
	from := e.mark()
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("c1", "m1", "viejo", 1000)})
	e.waitEvent(from, "message.received", ofType(model.EvMessageNew))
}

func TestRouterHistoryBatch(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelTelegram, "")
	ad := e.adapter(acc.ID)
	out := &outboxRec{}
	e.router.SetOutbox(out)

	ad.Inject(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{
		Chats:    []model.Chat{*directChat("c1", "Ana"), *directChat("c2", "Beto")},
		Messages: []model.Message{*incomingMsg("c1", "m1", "uno", 10), *incomingMsg("c1", "m2", "dos", 20)},
		Persons:  []model.Person{{NativeID: "c1", Name: "Ana"}},
		Progress: 0.5,
	}})
	ev := e.waitEvent(0, "history.batch", ofType(model.EvHistoryBatch))
	if ev.History.Chats[0].Key != model.NewChatKey(acc.ID, "c1") || ev.History.Messages[1].Key != model.NewMessageKey(acc.ID, "c1", "m2") {
		t.Fatalf("claves del lote: %+v", ev.History)
	}
	st := e.store(acc.ID)
	if st.batches() != 1 {
		t.Fatalf("UpsertMessages llamado %d veces, quería 1", st.batches())
	}
	if _, ok := st.chat("c2"); !ok {
		t.Fatal("falta el chat c2")
	}
	if _, ok := st.person("c1"); !ok {
		t.Fatal("falta la persona c1")
	}
	if c, _ := st.chat("c1"); c.UnreadCount != 0 {
		t.Fatalf("el historial no suma no leídos, hay %d", c.UnreadCount)
	}
	for _, x := range out.list() {
		if x.Type == model.EvHistoryBatch {
			t.Fatal("history.batch no va al outbox")
		}
	}
	// y no dispara chat.updated derivados ni observadores
	for _, x := range e.eventsSince(0) {
		if x.Type == model.EvChatUpsert {
			t.Fatalf("el lote no debe generar chat.updated: %+v", x)
		}
	}
}

func TestRouterOutbox(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	out := &outboxRec{}
	e.router.SetOutbox(out)

	e.seedChat(acc.ID, "c1", "Ana", 0)
	from := e.mark()
	ad.Inject(model.Event{Type: model.EvTyping, ChatKey: model.NewChatKey(acc.ID, "c1"), Typing: &model.TypingEvent{PeerID: "c1", Typing: true}})
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("c1", "m1", "hola", 1000)})
	ad.Inject(model.Event{Type: model.EvPerson, Person: &model.Person{NativeID: "c1", Name: "Ana"}})
	e.waitEvent(from, "person.updated", ofType(model.EvPerson))

	var types []model.EventType
	for _, x := range out.list() {
		if x.Type == model.EvAccountStatus {
			continue
		}
		if x.ID == "" {
			t.Fatal("el evento del outbox debe llevar ID")
		}
		types = append(types, x.Type)
	}
	want := []model.EventType{model.EvMessageNew, model.EvPerson}
	if len(types) != len(want) || types[0] != want[0] || types[1] != want[1] {
		t.Fatalf("outbox = %v, quería %v (sin typing ni derivados)", types, want)
	}
	// El typing sí se publica en el bus.
	e.waitEvent(from, "typing", ofType(model.EvTyping))
}

func TestRouterNoOutboxNoObserversNoPanic(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 0)
	from := e.mark()
	e.adapter(acc.ID).Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("c1", "m1", "baja", 1000)})
	e.waitEvent(from, "message.received", ofType(model.EvMessageNew))
}

func TestRouterUpdateReactionReceipt(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 0)
	key := model.NewMessageKey(acc.ID, "c1", "m1")
	e.seedMessage(acc.ID, "c1", "m1", 1000, true, "hola")

	from := e.mark()
	txt := "hola!"
	ad.Inject(model.Event{Type: model.EvMessageUpdate, Update: &model.MessageUpdate{Key: key, Text: &txt, EditedAt: 5}})
	ev := e.waitEvent(from, "message.updated", ofType(model.EvMessageUpdate))
	if ev.Message == nil || ev.Message.Text != "hola!" || ev.Message.EditedAt != 5 {
		t.Fatalf("el update debe llevar el mensaje resultante: %+v", ev.Message)
	}

	from = e.mark()
	ad.Inject(model.Event{Type: model.EvReaction, Reaction: &model.ReactionEvent{
		MessageKey: key, Reaction: model.Reaction{SenderNativeID: "c1", Emoji: "👍", At: 7},
	}})
	ev = e.waitEvent(from, "reaction", ofType(model.EvReaction))
	if ev.Message == nil || len(ev.Message.Reactions) != 1 {
		t.Fatalf("la reacción debe llevar el mensaje completo: %+v", ev.Message)
	}

	from = e.mark()
	ad.Inject(model.Event{Type: model.EvReceipt, Receipt: &model.ReceiptEvent{
		ChatKey: model.NewChatKey(acc.ID, "c1"), MessageKeys: []model.MessageKey{key}, Delivery: model.DeliveryRead,
	}})
	e.waitEvent(from, "receipt", ofType(model.EvReceipt))
	if m, _ := e.store(acc.ID).message("c1", "m1"); m.Delivery != model.DeliveryRead {
		t.Fatalf("entrega = %q", m.Delivery)
	}

	// Un update de un mensaje que no existe se descarta sin publicar.
	from = e.mark()
	ad.Inject(model.Event{Type: model.EvMessageUpdate, Update: &model.MessageUpdate{Key: model.NewMessageKey(acc.ID, "c1", "nope"), Text: &txt}})
	ad.Inject(model.Event{Type: model.EvPerson, Person: &model.Person{NativeID: "x"}}) // marcador
	e.waitEvent(from, "marcador", ofType(model.EvPerson))
	for _, x := range e.eventsSince(from) {
		if x.Type == model.EvMessageUpdate {
			t.Fatal("no debe publicarse el update de un mensaje inexistente")
		}
	}
}

func TestRouterAccountStateOnlyPublished(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	from := e.mark()
	e.adapter(acc.ID).Inject(model.Event{Type: model.EvAccountStatus, AccountStatus: &model.AccountStatus{State: model.StatePairing, Need: model.NeedScanQR, QRCode: "qr"}})
	ev := e.waitEvent(from, "account.state", func(ev model.Event) bool {
		return ev.Type == model.EvAccountStatus && ev.AccountStatus.State == model.StatePairing
	})
	if ev.AccountStatus.AccountID != acc.ID {
		t.Fatalf("falta el AccountID en el estado: %+v", ev.AccountStatus)
	}
}

func TestRouterSelfPerson(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)

	ad.Inject(model.Event{Type: model.EvPerson, Person: &model.Person{NativeID: "me@wa", PushName: "Ana", Phone: "+593999"}})
	e.waitEvent(0, "persona propia", ofType(model.EvPerson))
	got, _ := e.app.GetAccount(context.Background(), acc.ID)
	if got.SelfID != "me@wa" || got.SelfName != "Ana" || got.SelfHandle != "+593999" {
		t.Fatalf("datos propios: %+v", got)
	}

	// Otra persona no pisa los datos propios.
	from := e.mark()
	ad.Inject(model.Event{Type: model.EvPerson, Person: &model.Person{NativeID: "other", Name: "Otro", Phone: "+1"}})
	e.waitEvent(from, "otra persona", ofType(model.EvPerson))
	got, _ = e.app.GetAccount(context.Background(), acc.ID)
	if got.SelfID != "me@wa" || got.SelfName != "Ana" {
		t.Fatalf("otra persona pisó los datos propios: %+v", got)
	}

	// Con SelfID conocido, la propia persona se actualiza (nombre nuevo).
	from = e.mark()
	ad.Inject(model.Event{Type: model.EvPerson, Person: &model.Person{NativeID: "me@wa", PushName: "Ana Z", Username: "az"}})
	e.waitEvent(from, "propia otra vez", ofType(model.EvPerson))
	got, _ = e.app.GetAccount(context.Background(), acc.ID)
	if got.SelfName != "Ana Z" || got.SelfHandle != "@az" {
		t.Fatalf("actualización propia: %+v", got)
	}
}

func TestRouterContactID(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	_ = e.app.SaveContact(context.Background(), model.Contact{
		ID: "ct1", Name: "Ana", Identities: []model.Identity{{AccountID: acc.ID, PeerNativeID: "c1"}},
	})
	ad := e.adapter(acc.ID)
	group := &model.Chat{NativeID: "g1", Kind: model.ChatGroup, Title: "Grupo"}
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "Ana")})
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c2", "Sin contacto")})
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: group})
	eventually(t, "chats guardados", func() bool { _, ok := e.store(acc.ID).chat("g1"); return ok })
	if c, _ := e.store(acc.ID).chat("c1"); c.ContactID != "ct1" {
		t.Fatalf("ContactID = %q", c.ContactID)
	}
	if c, _ := e.store(acc.ID).chat("c2"); c.ContactID != "" {
		t.Fatalf("c2 no tiene contacto, ContactID = %q", c.ContactID)
	}
}

func TestRouterObserversAndOptOut(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	obs := &obsRec{}
	bad := &obsRec{panicky: true}
	e.router.AddObserver(bad) // un observador roto no impide al siguiente
	e.router.AddObserver(obs)
	_ = e.app.SaveContact(context.Background(), model.Contact{
		ID: "ct1", Identities: []model.Identity{{AccountID: acc.ID, PeerNativeID: "c1"}},
	})
	e.seedChat(acc.ID, "c1", "Ana", 0)
	_ = e.store(acc.ID).UpsertChat(context.Background(), model.Chat{NativeID: "g1", Kind: model.ChatGroup})

	// respuesta normal
	from := e.mark()
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("c1", "m1", "gracias", 1000)})
	e.waitEvent(from, "m1", ofType(model.EvMessageNew))
	eventually(t, "OnIncoming", func() bool { n, _ := obs.counts(); return n == 1 })
	if _, o := obs.counts(); o != 0 {
		t.Fatal("«gracias» no es una baja")
	}

	// baja
	from = e.mark()
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("c1", "m2", " BAJA. ", 2000)})
	e.waitEvent(from, "m2", func(ev model.Event) bool { return ev.Message != nil && ev.Message.NativeID == "m2" })
	eventually(t, "OnOptOut", func() bool { _, o := obs.counts(); return o == 1 })
	if obs.optOuts[0] != string(acc.ID)+"|ct1" {
		t.Fatalf("OnOptOut = %v", obs.optOuts)
	}
	ct, _ := e.app.GetContact(context.Background(), "ct1")
	if !ct.OptedOut {
		t.Fatal("el contacto debe quedar OptedOut")
	}

	// mensaje propio y mensaje de grupo no cuentan
	from = e.mark()
	mine := incomingMsg("c1", "m3", "baja", 3000)
	mine.FromMe = true
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: mine})
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incomingMsg("g1", "m4", "baja", 3001)})
	e.waitEvent(from, "m4", func(ev model.Event) bool { return ev.Message != nil && ev.Message.NativeID == "m4" })
	time.Sleep(50 * time.Millisecond)
	if n, o := obs.counts(); n != 2 || o != 1 {
		t.Fatalf("incoming=%d optOut=%d, quería 2 y 1", n, o)
	}
}

func TestRouterEmitOverflowDropsAfterWait(t *testing.T) {
	app := newMemApp()
	b := bus.New()
	r := NewRouter(app, b)
	r.bufSize, r.emitWait = 2, 40*time.Millisecond
	acc := model.Account{ID: "wa-test", Channel: model.ChannelWhatsApp}
	_ = app.SaveAccount(context.Background(), acc)
	st := newMemAccountStore(acc.ID)
	st.gate = make(chan struct{})
	sink := r.Attach(acc, st)

	ev := model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "x")}
	start := time.Now()
	for i := 0; i < 10; i++ {
		sink.Emit(ev) // uno se queda procesando, 2 en cola, el resto espera 40 ms y se descarta
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Emit bloqueó %v", d)
	}
	if d := time.Since(start); d < 40*time.Millisecond {
		t.Fatalf("Emit no esperó antes de descartar (%v)", d)
	}
	close(st.gate)
	r.detach(acc.ID, true)
}

func TestRouterDetach(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	sink, ok := e.router.SinkFor(acc.ID)
	if !ok {
		t.Fatal("falta el sink")
	}
	e.router.Detach(acc.ID)
	if _, ok := e.router.SinkFor(acc.ID); ok {
		t.Fatal("el sink debe desaparecer tras Detach")
	}
	done := make(chan struct{})
	go func() { sink.Emit(model.Event{Type: model.EvTyping}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Emit tras Detach no debe bloquear")
	}
}

func TestRouterDetachDrains(t *testing.T) {
	app := newMemApp()
	r := NewRouter(app, bus.New())
	acc := model.Account{ID: "wa-test", Channel: model.ChannelWhatsApp}
	st := newMemAccountStore(acc.ID)
	st.gate = make(chan struct{})
	sink := r.Attach(acc, st)
	for i := 0; i < 3; i++ {
		sink.Emit(model.Event{Type: model.EvChatUpsert, Chat: directChat("c"+string(rune('1'+i)), "x")})
	}
	go func() { time.Sleep(30 * time.Millisecond); close(st.gate) }()
	r.detach(acc.ID, true)
	for _, n := range []string{"c1", "c2", "c3"} {
		if _, ok := st.chat(n); !ok {
			t.Fatalf("con drain debe procesarse lo pendiente (falta %s)", n)
		}
	}
}

func TestIsOptOut(t *testing.T) {
	kw := defaultsOptOutKeywords
	yes := []string{"baja", "BAJA", " Baja. ", "¡STOP!", "no más", "No mas", "NO MÁS.", "cancelar suscripción", "Cancelar  suscripcion", "unsubscribe", "no quiero recibir"}
	no := []string{"", "   ", "hola", "dar de baja mi cuenta", "no quiero", "baja por favor", "stopping", "gracias, baja"}
	for _, s := range yes {
		if !isOptOut(s, kw) {
			t.Errorf("%q debería ser baja", s)
		}
	}
	for _, s := range no {
		if isOptOut(s, kw) {
			t.Errorf("%q no debería ser baja", s)
		}
	}
	if isOptOut("baja", "") {
		t.Error("sin palabras no hay bajas")
	}
	if !isOptOut("chao", "hola, chao ,") {
		t.Error("las palabras se recortan y se ignoran las vacías")
	}
}

func TestOptOutKeywordsCache(t *testing.T) {
	app := newMemApp()
	r := NewRouter(app, bus.New())
	clock := time.Now()
	r.now = func() time.Time { return clock }
	ctx := context.Background()

	if got := r.optOutKeywords(ctx); got != defaultsOptOutKeywords {
		t.Fatalf("por defecto: %q", got)
	}
	_ = app.SetSetting(ctx, SettingOptOutKeywords, "salir")
	if got := r.optOutKeywords(ctx); got != defaultsOptOutKeywords {
		t.Fatalf("el valor debe venir de la caché: %q", got)
	}
	clock = clock.Add(61 * time.Second)
	if got := r.optOutKeywords(ctx); got != "salir" {
		t.Fatalf("pasado 1 min se relee: %q", got)
	}
}

// La URL del medio la calcula el almacén al leer: lo que se publica por eventos debe traerla igual,
// porque la interfaz pinta imágenes y audios con ella (03: Media.URL).
func TestRouterPublishedMediaCarriesURL(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 0)

	m := incomingMsg("c1", "m1", "mira", 1000)
	m.Kind = model.KindImage
	m.Media = &model.Media{ID: "med1", Mime: "image/jpeg"}
	from := e.mark()
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: m})
	ev := e.waitEvent(from, "message.received", ofType(model.EvMessageNew))
	want := "/media/" + string(acc.ID) + "/med1"
	if ev.Message.Media == nil || ev.Message.Media.URL != want {
		t.Fatalf("Media.URL = %+v, quería %q", ev.Message.Media, want)
	}

	st := model.Status{NativeID: "s1", AuthorNativeID: "c1", PostedAt: 1000, ExpiresAt: 99999999999999, Kind: model.KindImage,
		Media: &model.Media{ID: "med2", Mime: "image/jpeg"}}
	from = e.mark()
	ad.Inject(model.Event{Type: model.EvStatusUpsert, Status: &st})
	sev := e.waitEvent(from, "status.posted", ofType(model.EvStatusUpsert))
	if sev.Status.Media == nil || sev.Status.Media.URL != "/media/"+string(acc.ID)+"/med2" {
		t.Fatalf("Status.Media.URL = %+v", sev.Status.Media)
	}
}

// Messenger e Instagram no fijan ni silencian en el servidor: un chat.updated del adaptador (que
// siempre trae esas marcas apagadas) no debe borrar las locales.
func TestRouterKeepsLocalPinOnMetaChannels(t *testing.T) {
	for _, ch := range []model.Channel{model.ChannelMessenger, model.ChannelInstagram} {
		e := newTestEnv(t)
		acc := e.newAccount(ch, "")
		ad := e.adapter(acc.ID)
		e.seedChat(acc.ID, "c1", "Ana", 5)
		if err := e.store(acc.ID).PatchChat(context.Background(), model.Chat{NativeID: "c1", Pinned: true, MutedUntil: -1}, []string{"pinned", "mutedUntil"}); err != nil {
			t.Fatal(err)
		}
		from := e.mark()
		ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "Ana B")})
		ev := e.waitEvent(from, "chat.updated", ofType(model.EvChatUpsert))
		if !ev.Chat.Pinned || ev.Chat.MutedUntil != -1 || ev.Chat.Title != "Ana B" {
			t.Fatalf("%s: se perdieron las marcas locales: %+v", ch, ev.Chat)
		}
		if c, _ := e.store(acc.ID).chat("c1"); !c.Pinned || c.MutedUntil != -1 {
			t.Fatalf("%s: el almacén perdió las marcas: %+v", ch, c)
		}
	}
	// En WhatsApp sí manda el adaptador (fijar desde el teléfono).
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 5)
	_ = e.store(acc.ID).PatchChat(context.Background(), model.Chat{NativeID: "c1", Pinned: true}, []string{"pinned"})
	from := e.mark()
	e.adapter(acc.ID).Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "Ana")})
	ev := e.waitEvent(from, "chat.updated", ofType(model.EvChatUpsert))
	if ev.Chat.Pinned {
		t.Fatal("en WhatsApp el adaptador manda sobre las marcas")
	}
}
