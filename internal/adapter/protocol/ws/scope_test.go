package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dconnector/internal/core/model"
)

// scopeFixture: cuenta con listas, contactos y chats para probar el alcance de contactos.
//
//	listas:    fam (Familia), ami (Amigos), vip (VIP), vieja (se borra después)
//	contactos: ana → fam · beto → ami · carla → fam+vip · dani → sin listas · eva → vip+vieja
//	chats:     c-ana, c-beto, c-carla, c-dani, c-eva (directos, por identidad)
//	           c-zeta (directo sin contacto) · g1 (grupo) · canal1 (canal) · yo (self)
type scopeFixture struct {
	e   *env
	acc model.Account
	ctx context.Context
}

func newScopeFixture(t *testing.T) (*scopeFixture, *testServer) {
	t.Helper()
	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, _ := e.newAccount("Ventas")
	ctx := context.Background()
	for _, l := range []model.ContactList{{ID: "fam", Name: "Familia"}, {ID: "ami", Name: "Amigos"}, {ID: "vip", Name: "VIP"}, {ID: "vieja", Name: "Vieja"}} {
		if err := e.app.SaveList(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	ident := func(peer string) []model.Identity {
		return []model.Identity{{AccountID: acc.ID, Channel: model.ChannelWhatsApp, PeerNativeID: peer}}
	}
	for _, c := range []model.Contact{
		{ID: "ana", Name: "Ana", Identities: ident("c-ana"), ListIDs: []string{"fam"}},
		{ID: "beto", Name: "Beto", Identities: ident("c-beto"), ListIDs: []string{"ami"}},
		{ID: "carla", Name: "Carla", Identities: ident("c-carla"), ListIDs: []string{"fam", "vip"}},
		{ID: "dani", Name: "Dani", Identities: ident("c-dani")},
		{ID: "eva", Name: "Eva", Identities: ident("c-eva"), ListIDs: []string{"vip", "vieja"}},
	} {
		if err := e.app.SaveContact(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	st, err := e.accounts.Store(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []model.Chat{
		{NativeID: "c-ana", Kind: model.ChatDirect, Title: "Ana", PeerNativeID: "c-ana", LastMessageAt: 1000},
		{NativeID: "c-beto", Kind: model.ChatDirect, Title: "Beto", PeerNativeID: "c-beto", LastMessageAt: 1000},
		{NativeID: "c-carla", Kind: model.ChatDirect, Title: "Carla", PeerNativeID: "c-carla", LastMessageAt: 1000},
		{NativeID: "c-dani", Kind: model.ChatDirect, Title: "Dani", PeerNativeID: "c-dani", LastMessageAt: 1000},
		{NativeID: "c-eva", Kind: model.ChatDirect, Title: "Eva", PeerNativeID: "c-eva", LastMessageAt: 1000},
		{NativeID: "c-zeta", Kind: model.ChatDirect, Title: "Zeta", PeerNativeID: "c-zeta", LastMessageAt: 1000},
		{NativeID: "g1", Kind: model.ChatGroup, Title: "Grupo", LastMessageAt: 1000},
		{NativeID: "canal1", Kind: model.ChatChannel, Title: "Canal", LastMessageAt: 1000},
		{NativeID: "yo", Kind: model.ChatSelf, Title: "Yo", LastMessageAt: 1000},
	} {
		if err := st.UpsertChat(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	return &scopeFixture{e: e, acc: acc, ctx: ctx}, ts
}

func (x *scopeFixture) msgEvent(chat string) model.Event {
	return model.Event{
		Type: model.EvMessageNew, AccountID: x.acc.ID, Channel: model.ChannelWhatsApp,
		Message: &model.Message{ChatNativeID: chat, NativeID: "m-" + chat},
	}
}

func scope(mode string, include, exclude []string) model.DestinationFilter {
	return model.DestinationFilter{
		IncludeFromMe: true, IncludeGroups: true,
		ContactScope: model.ContactScope{Mode: mode, IncludeListIDs: include, ExcludeListIDs: exclude},
	}
}

func TestAcceptsContactScopeMatrix(t *testing.T) {
	x, _ := newScopeFixture(t)
	chats := []string{"c-ana", "c-beto", "c-carla", "c-dani", "c-eva", "c-zeta", "g1", "canal1", "yo", "nuevo-sin-guardar"}
	cases := []struct {
		name string
		f    model.DestinationFilter
		want map[string]bool // lo que no figura = false
	}{
		{"destino antiguo (sin el campo): todos", model.DestinationFilter{IncludeFromMe: true, IncludeGroups: true},
			map[string]bool{"c-ana": true, "c-beto": true, "c-carla": true, "c-dani": true, "c-eva": true, "c-zeta": true, "g1": true, "canal1": true, "yo": true, "nuevo-sin-guardar": true}},
		{"todos (modo explícito)", scope("all", nil, nil),
			map[string]bool{"c-ana": true, "c-beto": true, "c-carla": true, "c-dani": true, "c-eva": true, "c-zeta": true, "g1": true, "canal1": true, "yo": true, "nuevo-sin-guardar": true}},
		{"solo la lista Amigos", scope("lists", []string{"ami"}, nil),
			map[string]bool{"c-beto": true}},
		{"solo VIP: Carla y Eva", scope("lists", []string{"vip"}, nil),
			map[string]bool{"c-carla": true, "c-eva": true}},
		{"solo Familia y Amigos", scope("lists", []string{"fam", "ami"}, nil),
			map[string]bool{"c-ana": true, "c-beto": true, "c-carla": true}},
		{"todos menos Familia: pasan sin contacto, grupos y canales", scope("all", nil, []string{"fam"}),
			map[string]bool{"c-beto": true, "c-dani": true, "c-eva": true, "c-zeta": true, "g1": true, "canal1": true, "yo": true, "nuevo-sin-guardar": true}},
		{"todos menos Familia y Amigos", scope("all", nil, []string{"fam", "ami"}),
			map[string]bool{"c-dani": true, "c-eva": true, "c-zeta": true, "g1": true, "canal1": true, "yo": true, "nuevo-sin-guardar": true}},
		{"VIP menos Familia: Carla está en las dos y no pasa", scope("lists", []string{"vip"}, []string{"fam"}),
			map[string]bool{"c-eva": true}},
		{"Familia menos VIP: Carla fuera, Ana dentro", scope("lists", []string{"fam"}, []string{"vip"}),
			map[string]bool{"c-ana": true}},
		{"solo listas sin ninguna incluida: nadie", scope("lists", nil, nil), map[string]bool{}},
		{"lista incluida que ya no existe: nadie", scope("lists", []string{"borrada"}, nil), map[string]bool{}},
		{"lista excluida que ya no existe: se ignora", scope("all", nil, []string{"borrada"}),
			map[string]bool{"c-ana": true, "c-beto": true, "c-carla": true, "c-dani": true, "c-eva": true, "c-zeta": true, "g1": true, "canal1": true, "yo": true, "nuevo-sin-guardar": true}},
		{"modo desconocido: no abre", scope("raro", nil, nil), map[string]bool{}},
	}
	for _, c := range cases {
		for _, chat := range chats {
			lk := &lookups{m: x.e.mgr, ctx: x.ctx}
			ev := x.msgEvent(chat)
			if got := accepts(c.f, "message.received", ev, lk); got != c.want[chat] {
				t.Errorf("%s · %s: accepts = %v, quería %v", c.name, chat, got, c.want[chat])
			}
		}
	}
}

func TestAcceptsContactScopeOtherEvents(t *testing.T) {
	x, _ := newScopeFixture(t)
	f := scope("lists", []string{"fam"}, nil)
	acc, ch := x.acc.ID, model.ChannelWhatsApp
	evs := map[string]struct {
		ev   model.Event
		want bool
	}{
		"estado de la cuenta no lleva contactos": {model.Event{Type: model.EvAccountStatus, AccountID: acc, Channel: ch}, true},
		"capacidades de la cuenta":               {model.Event{Type: model.EvCapabilities, AccountID: acc, Channel: ch}, true},
		"chat.updated de Ana (el chat trae su ContactID)": {model.Event{Type: model.EvChatUpsert, AccountID: acc, Channel: ch,
			Chat: &model.Chat{NativeID: "x", Kind: model.ChatDirect, ContactID: "ana"}}, true},
		"chat.updated de Beto":              {model.Event{Type: model.EvChatUpsert, AccountID: acc, Channel: ch, Chat: &model.Chat{NativeID: "c-beto", Kind: model.ChatDirect, PeerNativeID: "c-beto"}}, false},
		"chat.updated de un grupo":          {model.Event{Type: model.EvChatUpsert, AccountID: acc, Channel: ch, Chat: &model.Chat{NativeID: "g1", Kind: model.ChatGroup}}, false},
		"reacción en el chat de Ana":        {model.Event{Type: model.EvReaction, AccountID: acc, Channel: ch, Reaction: &model.ReactionEvent{MessageKey: model.NewMessageKey(acc, "c-ana", "m")}}, true},
		"acuse en el chat de Beto":          {model.Event{Type: model.EvReceipt, AccountID: acc, Channel: ch, Receipt: &model.ReceiptEvent{ChatKey: model.NewChatKey(acc, "c-beto")}}, false},
		"person.updated de Ana":             {model.Event{Type: model.EvPerson, AccountID: acc, Channel: ch, Person: &model.Person{NativeID: "c-ana"}}, true},
		"person.updated de Beto":            {model.Event{Type: model.EvPerson, AccountID: acc, Channel: ch, Person: &model.Person{NativeID: "c-beto"}}, false},
		"estado publicado por Carla":        {model.Event{Type: model.EvStatusUpsert, AccountID: acc, Channel: ch, Status: &model.Status{AuthorNativeID: "c-carla"}}, true},
		"estado publicado por Beto":         {model.Event{Type: model.EvStatusUpsert, AccountID: acc, Channel: ch, Status: &model.Status{AuthorNativeID: "c-beto"}}, false},
		"estado visto por Ana":              {model.Event{Type: model.EvStatusView, AccountID: acc, Channel: ch, StatusView: &model.StatusView{ViewerID: "c-ana"}}, true},
		"evento de chat sin carga":          {model.Event{Type: model.EvMessageNew, AccountID: acc, Channel: ch}, false},
		"estado visto sin carga":            {model.Event{Type: model.EvStatusView, AccountID: acc, Channel: ch}, false},
		"mensaje propio en el chat de Ana":  {model.Event{Type: model.EvMessageSent, AccountID: acc, Channel: ch, Message: &model.Message{ChatNativeID: "c-ana", FromMe: true}}, true},
		"mensaje propio en el chat de Beto": {model.Event{Type: model.EvMessageSent, AccountID: acc, Channel: ch, Message: &model.Message{ChatNativeID: "c-beto", FromMe: true}}, false},
	}
	for name, c := range evs {
		lk := &lookups{m: x.e.mgr, ctx: x.ctx}
		if got := accepts(f, string(c.ev.Type), c.ev, lk); got != c.want {
			t.Errorf("%s: accepts = %v, quería %v", name, got, c.want)
		}
	}
}

func TestContactScopeCache(t *testing.T) {
	x, _ := newScopeFixture(t)
	now := time.Now()
	x.e.mgr.scope.now = func() time.Time { return now }
	f := scope("lists", []string{"fam"}, nil)
	allowed := func(chat string) bool {
		return accepts(f, "message.received", x.msgEvent(chat), &lookups{m: x.e.mgr, ctx: x.ctx})
	}
	if allowed("c-beto") {
		t.Fatal("Beto no está en Familia")
	}
	// Beto pasa a Familia: mientras la caché viva, todavía no se nota…
	if err := x.e.app.SetListMembers(x.ctx, "fam", []string{"beto"}, nil); err != nil {
		t.Fatal(err)
	}
	now = now.Add(scopeCacheTTL - time.Second)
	if allowed("c-beto") {
		t.Fatal("dentro del TTL se usa lo recordado")
	}
	// …pasado el TTL, sí.
	now = now.Add(2 * time.Second)
	if !allowed("c-beto") {
		t.Fatal("pasado el TTL debe ver la lista nueva")
	}
	// Y el reinicio explícito lo nota al instante.
	if err := x.e.app.SetListMembers(x.ctx, "fam", nil, []string{"beto"}); err != nil {
		t.Fatal(err)
	}
	if !allowed("c-beto") {
		t.Fatal("la caché sigue viva")
	}
	x.e.mgr.InvalidateContactScope()
	if allowed("c-beto") {
		t.Fatal("tras reiniciar la caché Beto ya no está en Familia")
	}
}

func TestContactScopeCacheAvoidsDatabaseHits(t *testing.T) {
	x, _ := newScopeFixture(t)
	f := scope("all", nil, []string{"fam"})
	for i := 0; i < 50; i++ {
		accepts(f, "message.received", x.msgEvent("c-ana"), &lookups{m: x.e.mgr, ctx: x.ctx})
	}
	x.e.mgr.scope.mu.Lock()
	defer x.e.mgr.scope.mu.Unlock()
	if n := len(x.e.mgr.scope.peers); n != 1 {
		t.Fatalf("50 eventos de la misma persona deben dejar 1 entrada en la caché, hay %d", n)
	}
}

func TestContactScopeDeletedListsAreIgnored(t *testing.T) {
	x, _ := newScopeFixture(t)
	// Eva está en vip y en vieja. Se borra vieja: un destino que solo incluía vieja no deja pasar a nadie.
	if err := x.e.app.DeleteList(x.ctx, "vieja"); err != nil {
		t.Fatal(err)
	}
	x.e.mgr.InvalidateContactScope()
	f := scope("lists", []string{"vieja"}, nil)
	if accepts(f, "message.received", x.msgEvent("c-eva"), &lookups{m: x.e.mgr, ctx: x.ctx}) {
		t.Fatal("una lista borrada no puede dejar pasar a nadie")
	}
	f = scope("all", nil, []string{"vieja"})
	if !accepts(f, "message.received", x.msgEvent("c-eva"), &lookups{m: x.e.mgr, ctx: x.ctx}) {
		t.Fatal("una lista excluida que ya no existe se ignora")
	}
}

func TestEnqueueRespectsContactScope(t *testing.T) {
	ts := newTestServer(t, "t")
	ts.srv.Close() // el outbox no necesita la red
	x, _ := newScopeFixture(t)
	e := x.e
	all := e.addDest(ts, false, scope("all", nil, nil))
	noFam := e.addDest(ts, false, scope("all", nil, []string{"fam"}))
	onlyVip := e.addDest(ts, false, scope("lists", []string{"vip"}, nil))
	e.start()
	for _, chat := range []string{"c-ana", "c-beto", "c-dani", "c-eva"} {
		e.mgr.Enqueue(x.ctx, x.msgEvent(chat))
	}
	if n := e.pending(all.ID); n != 4 {
		t.Fatalf("todos: %d eventos, quería 4", n)
	}
	if n := e.pending(noFam.ID); n != 3 {
		t.Fatalf("todos menos Familia: %d eventos, quería 3", n)
	}
	if n := e.pending(onlyVip.ID); n != 1 {
		t.Fatalf("solo VIP: %d eventos, quería 1 (Eva)", n)
	}
}

func TestHelloCarriesContactScope(t *testing.T) {
	ts := newTestServer(t, "t")
	x, _ := newScopeFixture(t)
	x.e.addDest(ts, false, scope("lists", []string{"vip", "fam"}, []string{"ami"}))
	x.e.start()
	hello := ts.waitHello()
	var h struct {
		Filter struct {
			ContactScope struct {
				Mode           string   `json:"mode"`
				IncludeListIDs []string `json:"includeListIds"`
				ExcludeListIDs []string `json:"excludeListIds"`
			} `json:"contactScope"`
		} `json:"filter"`
	}
	if err := json.Unmarshal(hello.Data, &h); err != nil {
		t.Fatal(err)
	}
	cs := h.Filter.ContactScope
	if cs.Mode != "lists" || len(cs.IncludeListIDs) != 2 || len(cs.ExcludeListIDs) != 1 || cs.ExcludeListIDs[0] != "ami" {
		t.Fatalf("hello.filter.contactScope: %+v", cs)
	}
}

func TestHelloContactScopeOfOldDestination(t *testing.T) {
	ts := newTestServer(t, "t")
	x, _ := newScopeFixture(t)
	// Un destino guardado antes de existir el campo.
	if err := x.e.app.SaveDestination(x.ctx, model.Destination{
		ID: "viejo", Name: "viejo", URL: ts.wsURL(), Token: ts.token, Enabled: true,
		Filter: model.DestinationFilter{IncludeFromMe: true},
	}); err != nil {
		t.Fatal(err)
	}
	x.e.start()
	hello := ts.waitHello()
	var raw struct {
		Filter map[string]json.RawMessage `json:"filter"`
	}
	if err := json.Unmarshal(hello.Data, &raw); err != nil {
		t.Fatal(err)
	}
	if got := string(raw.Filter["contactScope"]); got != `{"mode":"all","includeListIds":[],"excludeListIds":[]}` {
		t.Fatalf("contactScope de un destino antiguo: %s", got)
	}
}

// ---------------------------------------------------------------------------------------------
// Comandos

func listChatIDs(t *testing.T, rd resultData) map[string]bool {
	t.Helper()
	var chats []struct {
		NativeID string `json:"nativeId"`
	}
	mustUnmarshal(t, resultBytes(rd), &chats)
	out := map[string]bool{}
	for _, c := range chats {
		out[c.NativeID] = true
	}
	return out
}

func TestCommandsRespectContactScope(t *testing.T) {
	x, ts := newScopeFixture(t)
	e := x.e
	// Todos menos Familia (incluye grupos, que no son contactos).
	e.addDest(ts, true, scope("all", nil, []string{"fam"}))
	e.start()
	ts.waitHello()
	account := string(x.acc.ID)

	rd := ts.command("cl", "chats.list", account, map[string]any{"limit": 100})
	if !rd.OK {
		t.Fatalf("chats.list: %+v", rd.Error)
	}
	got := listChatIDs(t, rd)
	for _, chat := range []string{"c-ana", "c-carla"} {
		if got[chat] {
			t.Errorf("chats.list no debe traer %s (de Familia)", chat)
		}
	}
	for _, chat := range []string{"c-beto", "c-dani", "c-eva", "c-zeta", "g1", "canal1", "yo"} {
		if !got[chat] {
			t.Errorf("chats.list debe traer %s", chat)
		}
	}

	// Los comandos sobre un chat de Familia reciben forbidden; sobre otro, funcionan.
	for i, name := range []string{"messages.list", "chat.mark_read", "chat.typing"} {
		rd := ts.command("f"+string(rune('a'+i)), name, account, map[string]any{"chatId": "c-ana", "typing": true})
		if rd.OK || rd.Error == nil || rd.Error.Code != "forbidden" {
			t.Fatalf("%s en un chat de Familia: %+v", name, rd)
		}
	}
	rd = ts.command("send-ana", "message.send", account, map[string]any{"chatId": "c-carla", "message": map[string]any{"kind": "text", "text": "no"}})
	if rd.OK || rd.Error.Code != "forbidden" {
		t.Fatalf("message.send a un contacto de Familia: %+v", rd)
	}
	rd = ts.command("send-beto", "message.send", account, map[string]any{"chatId": "c-beto", "message": map[string]any{"kind": "text", "text": "sí"}})
	if !rd.OK {
		t.Fatalf("message.send a Beto: %+v", rd.Error)
	}
	rd = ts.command("ml-beto", "messages.list", account, map[string]any{"chatId": "c-beto"})
	if !rd.OK {
		t.Fatalf("messages.list de Beto: %+v", rd.Error)
	}
	// Reenviar hacia un chat de Familia también es forbidden.
	rd = ts.command("fw", "message.forward", account, map[string]any{"chatId": "c-beto", "messageId": "m1", "toChatId": "c-ana"})
	if rd.OK || rd.Error.Code != "forbidden" {
		t.Fatalf("message.forward hacia Familia: %+v", rd)
	}

	// contacts.find: solo los contactos que pasan.
	rd = ts.command("cf", "contacts.find", "", map[string]any{"limit": 50})
	if !rd.OK {
		t.Fatalf("contacts.find: %+v", rd.Error)
	}
	var contacts []model.Contact
	mustUnmarshal(t, resultBytes(rd), &contacts)
	names := map[string]bool{}
	for _, c := range contacts {
		names[c.Name] = true
	}
	if names["Ana"] || names["Carla"] || !names["Beto"] || !names["Dani"] || !names["Eva"] {
		t.Fatalf("contacts.find: %v", names)
	}
}

func TestCommandsRespectOnlyListsScope(t *testing.T) {
	x, ts := newScopeFixture(t)
	x.e.addDest(ts, true, scope("lists", []string{"ami"}, nil))
	x.e.start()
	ts.waitHello()
	account := string(x.acc.ID)

	rd := ts.command("cl", "chats.list", account, map[string]any{"limit": 100})
	if !rd.OK {
		t.Fatalf("chats.list: %+v", rd.Error)
	}
	got := listChatIDs(t, rd)
	if len(got) != 1 || !got["c-beto"] {
		t.Fatalf("solo Amigos: chats.list = %v", got)
	}
	// Un grupo, un chat sin contacto y uno de otra lista: forbidden.
	for i, chat := range []string{"g1", "c-zeta", "c-ana", "chat-nuevo"} {
		rd := ts.command("m"+string(rune('a'+i)), "messages.list", account, map[string]any{"chatId": chat})
		if rd.OK || rd.Error.Code != "forbidden" {
			t.Fatalf("messages.list de %s: %+v", chat, rd)
		}
	}
	rd = ts.command("ok", "messages.list", account, map[string]any{"chatId": "c-beto"})
	if !rd.OK {
		t.Fatalf("messages.list de Beto: %+v", rd.Error)
	}
	// Con la clave "<cuenta>|<chat>" pasa por el mismo filtro.
	rd = ts.command("key", "messages.list", "", map[string]any{"chatId": account + "|c-ana"})
	if rd.OK || rd.Error.Code != "forbidden" {
		t.Fatalf("messages.list por clave: %+v", rd)
	}
}

func TestCommandsWithoutScopeAreUnchanged(t *testing.T) {
	x, ts := newScopeFixture(t)
	x.e.addDest(ts, true, model.DestinationFilter{IncludeFromMe: true, IncludeGroups: true}) // destino antiguo
	x.e.start()
	ts.waitHello()
	rd := ts.command("cl", "chats.list", string(x.acc.ID), map[string]any{"limit": 100})
	if !rd.OK || len(listChatIDs(t, rd)) != 9 {
		t.Fatalf("sin alcance: chats.list = %v (%+v)", listChatIDs(t, rd), rd.Error)
	}
}
