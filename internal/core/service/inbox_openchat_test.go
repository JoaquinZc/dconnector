package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Solo con actividad (H.1) y «Nuevo chat» (H.2).

func titlesOf(cs []model.Chat) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Title)
	}
	sort.Strings(out)
	return out
}

func TestInboxListChatsOnlyWithActivity(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "WA")
	st := e.store(acc.ID)
	for _, c := range []model.Chat{
		{NativeID: "d1", Kind: model.ChatDirect, Title: "directo-activo", LastMessageAt: 100},
		{NativeID: "d2", Kind: model.ChatDirect, Title: "directo-vacio"},
		{NativeID: "d3", Kind: model.ChatDirect, Title: "directo-vacio-fijado", Pinned: true},
		{NativeID: "g1", Kind: model.ChatGroup, Title: "grupo-vacio"},
		{NativeID: "c1", Kind: model.ChatChannel, Title: "canal-vacio"},
		{NativeID: "s1", Kind: model.ChatSelf, Title: "self-vacio"},
		{NativeID: "d4", Kind: model.ChatDirect, Title: "directo-vacio-no-leidos", UnreadCount: 2},
	} {
		if err := st.UpsertChat(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.inbox.ListChats(ctx, model.ChatFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"canal-vacio", "directo-activo", "directo-vacio-fijado", "grupo-vacio"}
	if g := titlesOf(got); !reflect.DeepEqual(g, want) {
		t.Fatalf("por defecto: %v, quería %v", g, want)
	}
	// Con IncludeEmpty salen todos (también el self vacío).
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{IncludeEmpty: true})
	if len(got) != 7 {
		t.Fatalf("IncludeEmpty: %v", titlesOf(got))
	}
	// Buscar por título a alguien sin actividad no lo muestra en la lista de chats.
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{Query: "directo-vacio"})
	if g := titlesOf(got); !reflect.DeepEqual(g, []string{"directo-vacio-fijado"}) {
		t.Fatalf("búsqueda: %v", g)
	}
	// El self con actividad sí sale.
	_ = st.UpsertChat(ctx, model.Chat{NativeID: "s1", Kind: model.ChatSelf, Title: "self-vacio", LastMessageAt: 50})
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{Tab: "all"})
	if !strings.Contains(chatKeys(got), "self-vacio") {
		t.Fatalf("self con actividad: %s", chatKeys(got))
	}
	// Los totales de no leídos no cambian: cuentan también el chat vacío con no leídos.
	un, _ := e.inbox.UnreadTotals(ctx)
	if un[acc.ID] != 2 {
		t.Fatalf("no leídos: %v", un)
	}
}

func TestInboxOpenChatCreatesEmptyChat(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "WA")
	_ = e.app.SaveContact(ctx, model.Contact{ID: "ct1", Name: "Ana Pérez", Identities: []model.Identity{
		{AccountID: acc.ID, Channel: model.ChannelWhatsApp, PeerNativeID: "593999@s.whatsapp.net"},
	}})
	before := len(e.adapter(acc.ID).Sent())

	c, err := e.inbox.OpenChat(ctx, acc.ID, " 593999@s.whatsapp.net ")
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != model.NewChatKey(acc.ID, "593999@s.whatsapp.net") || c.Kind != model.ChatDirect || c.Title != "Ana Pérez" ||
		c.LastMessageAt != 0 || c.ContactID != "ct1" || c.PeerNativeID != "593999@s.whatsapp.net" || c.Channel != model.ChannelWhatsApp {
		t.Fatalf("chat creado: %+v", c)
	}
	if len(e.adapter(acc.ID).Sent()) != before {
		t.Fatal("OpenChat no debe enviar nada")
	}
	// Guardado: se puede leer, pero no sale en la lista de chats.
	if g, err := e.inbox.GetChat(ctx, c.Key); err != nil || g.Title != "Ana Pérez" {
		t.Fatalf("GetChat: %+v %v", g, err)
	}
	if l, _ := e.inbox.ListChats(ctx, model.ChatFilter{}); len(l) != 0 {
		t.Fatalf("un chat vacío no sale en la lista: %v", chatKeys(l))
	}
	if l, _ := e.inbox.ListChats(ctx, model.ChatFilter{IncludeEmpty: true}); len(l) != 1 {
		t.Fatalf("con IncludeEmpty sí: %v", chatKeys(l))
	}
	// Abrirlo otra vez devuelve el mismo, sin duplicar.
	again, err := e.inbox.OpenChat(ctx, acc.ID, "593999@s.whatsapp.net")
	if err != nil || again.Key != c.Key || again.ContactID != "ct1" {
		t.Fatalf("segunda apertura: %+v %v", again, err)
	}

	// Al enviar el primer mensaje el chat pasa a tener actividad y aparece (chat.updated).
	from := e.mark()
	if _, err := e.msg.Send(ctx, c.Key, model.Outgoing{Kind: model.KindText, Text: "hola Ana"}); err != nil {
		t.Fatal(err)
	}
	ev := e.waitEvent(from, "chat.updated con actividad", func(ev model.Event) bool {
		return ev.Type == model.EvChatUpsert && ev.Chat != nil && ev.Chat.NativeID == "593999@s.whatsapp.net" && ev.Chat.LastMessageAt > 0
	})
	if ev.Chat.Title != "Ana Pérez" {
		t.Fatalf("el chat conserva el título del contacto: %+v", ev.Chat)
	}
	if l, _ := e.inbox.ListChats(ctx, model.ChatFilter{}); len(l) != 1 || l[0].Title != "Ana Pérez" {
		t.Fatalf("tras enviar sale en la lista: %v", chatKeys(l))
	}
}

func TestInboxOpenChatTitleFromPersonAndExisting(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelTelegram, "TG")
	_ = e.store(acc.ID).UpsertPerson(ctx, model.Person{NativeID: "77", PushName: "Luis T", Username: "luis"})
	c, err := e.inbox.OpenChat(ctx, acc.ID, "77")
	if err != nil || c.Title != "Luis T" {
		t.Fatalf("título de la persona: %+v %v", c, err)
	}
	// Sin contacto ni persona: el título es el id.
	c, err = e.inbox.OpenChat(ctx, acc.ID, "88")
	if err != nil || c.Title != "88" || c.ContactID != "" {
		t.Fatalf("sin datos: %+v %v", c, err)
	}
	// Un chat que ya existe con actividad se devuelve tal cual aunque el canal no permita iniciar.
	e.adapter(acc.ID).SetCapabilities(capability.Set{SendText: true})
	e.seedChat(acc.ID, "99", "Con historial", 500)
	c, err = e.inbox.OpenChat(ctx, acc.ID, "99")
	if err != nil || c.Title != "Con historial" || c.LastMessageAt != 500 {
		t.Fatalf("existente: %+v %v", c, err)
	}
}

func TestInboxOpenChatErrors(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "WA")

	if _, err := e.inbox.OpenChat(ctx, acc.ID, "  "); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("destino vacío: %v", err)
	}
	if _, err := e.inbox.OpenChat(ctx, "wa-nope", "593"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta inexistente: %v", err)
	}

	// Canal que no permite iniciar conversaciones.
	ig := e.newAccount(model.ChannelInstagram, "IG")
	e.adapter(ig.ID).SetCapabilities(capability.Set{SendText: true})
	_, err := e.inbox.OpenChat(ctx, ig.ID, "555")
	if !errors.Is(err, port.ErrUnsupported) || !strings.Contains(err.Error(), "Instagram") {
		t.Fatalf("canal sin StartChat: %v", err)
	}
	if _, gerr := e.inbox.GetChat(ctx, model.NewChatKey(ig.ID, "555")); !errors.Is(gerr, port.ErrNotFound) {
		t.Fatalf("no debe crearse el chat: %v", gerr)
	}

	// Cuenta desconectada: no se puede crear uno nuevo.
	if err := e.accounts.Disconnect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateDisconnected)
	if _, err := e.inbox.OpenChat(ctx, acc.ID, "593"); !errors.Is(err, port.ErrNotConnected) {
		t.Fatalf("cuenta desconectada: %v", err)
	}

	// Cuenta deshabilitada.
	if err := e.accounts.Update(ctx, acc.ID, "WA", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.inbox.OpenChat(ctx, acc.ID, "593"); !errors.Is(err, port.ErrInvalid) || !strings.Contains(err.Error(), "deshabilitada") {
		t.Fatalf("cuenta deshabilitada: %v", err)
	}
}

func TestInboxOpenChatResolvesNumbers(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "WA")
	ad := e.adapter(acc.ID)
	ad.mu.Lock()
	ad.resolve = func(_ context.Context, in string) (string, string, error) {
		if in == "+593 99 111 2222" {
			return "593991112222@s.whatsapp.net", "+593991112222", nil
		}
		return "", "", fmt.Errorf("%w: el número no está en WhatsApp", port.ErrNotFound)
	}
	ad.mu.Unlock()

	c, err := e.inbox.OpenChat(ctx, acc.ID, "+593 99 111 2222")
	if err != nil || c.NativeID != "593991112222@s.whatsapp.net" || c.Title != "+593991112222" || c.Kind != model.ChatDirect {
		t.Fatalf("número resuelto: %+v %v", c, err)
	}
	// El mismo número otra vez: devuelve el chat ya creado.
	again, err := e.inbox.OpenChat(ctx, acc.ID, "+593 99 111 2222")
	if err != nil || again.Key != c.Key {
		t.Fatalf("repetido: %+v %v", again, err)
	}
	// Un número que no está en el canal: el error del adaptador y nada guardado.
	if _, err := e.inbox.OpenChat(ctx, acc.ID, "+593 99 000 0000"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("número inexistente: %v", err)
	}
	if l, _ := e.store(acc.ID).ListChats(ctx, model.ChatFilter{IncludeEmpty: true}); len(l) != 1 {
		t.Fatalf("solo el chat válido: %v", chatKeys(l))
	}
}
