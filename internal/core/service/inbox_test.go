package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func chatKeys(cs []model.Chat) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, c.Title)
	}
	return strings.Join(parts, ",")
}

func TestInboxListChatsMergesAccounts(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	wa := e.newAccount(model.ChannelWhatsApp, "WA")
	tg := e.newAccount(model.ChannelTelegram, "TG")
	e.seedChat(wa.ID, "a", "wa-100", 100)
	e.seedChat(wa.ID, "b", "wa-300", 300)
	e.seedChat(tg.ID, "c", "tg-200", 200)
	e.seedChat(tg.ID, "d", "tg-400", 400)
	_ = e.store(wa.ID).UpsertChat(ctx, model.Chat{NativeID: "p", Kind: model.ChatDirect, Title: "wa-pinned", LastMessageAt: 50, Pinned: true})
	_ = e.store(tg.ID).UpsertChat(ctx, model.Chat{NativeID: "g", Kind: model.ChatGroup, Title: "tg-group", LastMessageAt: 10})
	_ = e.store(tg.ID).UpsertChat(ctx, model.Chat{NativeID: "x", Kind: model.ChatDirect, Title: "tg-arch", LastMessageAt: 999, Archived: true, UnreadCount: 2})

	got, err := e.inbox.ListChats(ctx, model.ChatFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if s := chatKeys(got); s != "wa-pinned,tg-400,wa-300,tg-200,wa-100,tg-group" {
		t.Fatalf("orden: %s", s)
	}

	// límite: se corta tras mezclar
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{Limit: 3})
	if s := chatKeys(got); s != "wa-pinned,tg-400,wa-300" {
		t.Fatalf("con límite: %s", s)
	}
	// paginación por LastMessageAt
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{Before: 300, Limit: 2})
	if s := chatKeys(got); s != "wa-pinned,tg-200" {
		// los fijados se mantienen como en el almacén (pinned primero)
		t.Fatalf("página siguiente: %s", s)
	}
	// filtros
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{AccountIDs: []model.AccountID{tg.ID}})
	if s := chatKeys(got); s != "tg-400,tg-200,tg-group" {
		t.Fatalf("por cuenta: %s", s)
	}
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{Channels: []model.Channel{model.ChannelWhatsApp}})
	if s := chatKeys(got); s != "wa-pinned,wa-300,wa-100" {
		t.Fatalf("por canal: %s", s)
	}
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{Tab: "groups"})
	if s := chatKeys(got); s != "tg-group" {
		t.Fatalf("grupos: %s", s)
	}
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{Tab: "archived"})
	if s := chatKeys(got); s != "tg-arch" {
		t.Fatalf("archivados: %s", s)
	}
	got, _ = e.inbox.ListChats(ctx, model.ChatFilter{Query: "pinned"})
	if s := chatKeys(got); s != "wa-pinned" {
		t.Fatalf("búsqueda: %s", s)
	}
	if got, err := e.inbox.ListChats(ctx, model.ChatFilter{Query: "zzz"}); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("sin resultados debe ser lista vacía: %v %v", got, err)
	}
}

func TestInboxGetChatAndMessages(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 100)
	for i := int64(1); i <= 5; i++ {
		e.seedMessage(acc.ID, "c1", "m"+string(rune('0'+i)), i*10, i%2 == 0, "t")
	}
	key := model.NewChatKey(acc.ID, "c1")

	c, err := e.inbox.GetChat(ctx, key)
	if err != nil || c.Title != "Ana" || c.Key != key || c.Channel != model.ChannelWhatsApp {
		t.Fatalf("GetChat: %+v %v", c, err)
	}
	if _, err := e.inbox.GetChat(ctx, model.NewChatKey(acc.ID, "nope")); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("chat desconocido: %v", err)
	}
	if _, err := e.inbox.GetChat(ctx, model.NewChatKey("wa-nope", "c1")); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
	if _, err := e.inbox.GetChat(ctx, "sin-barra"); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("clave mal formada: %v", err)
	}

	page, err := e.inbox.Messages(ctx, key, 0, 3)
	if err != nil || len(page.Messages) != 3 || !page.HasMore || page.Messages[0].Timestamp != 30 {
		t.Fatalf("página: %+v %v", page, err)
	}
	page, _ = e.inbox.Messages(ctx, key, 30, 3)
	if len(page.Messages) != 2 || page.HasMore {
		t.Fatalf("página anterior: %+v", page)
	}
}

func TestInboxMessagesAsksHistoryInBackground(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 100)
	e.seedMessage(acc.ID, "c1", "m1", 10, false, "a")
	e.seedMessage(acc.ID, "c1", "m2", 20, false, "b")
	key := model.NewChatKey(acc.ID, "c1")
	ad := e.adapter(acc.ID)
	hist := func() []string {
		var out []string
		for _, c := range ad.Calls() {
			if strings.HasPrefix(c, "RequestHistory") {
				out = append(out, c)
			}
		}
		return out
	}

	// página llena: no se pide nada
	if _, err := e.inbox.Messages(ctx, key, 0, 2); err != nil {
		t.Fatal(err)
	}
	e.inbox.bgWG.Wait()
	if len(hist()) != 0 {
		t.Fatalf("la página estaba llena: %v", hist())
	}

	// página corta: se devuelve lo que hay y se pide historial una sola vez (límite de frecuencia)
	page, _ := e.inbox.Messages(ctx, key, 0, 50)
	if len(page.Messages) != 2 {
		t.Fatalf("debe devolver lo local: %+v", page)
	}
	_, _ = e.inbox.Messages(ctx, key, 0, 50)
	e.inbox.bgWG.Wait()
	if h := hist(); len(h) != 1 || h[0] != "RequestHistory:c1:50" {
		t.Fatalf("peticiones de historial: %v", h)
	}
	// pasado el tiempo mínimo se vuelve a pedir
	e.inbox.now = func() time.Time { return time.Now().Add(time.Minute) }
	_, _ = e.inbox.Messages(ctx, key, 0, 50)
	e.inbox.bgWG.Wait()
	if h := hist(); len(h) != 2 {
		t.Fatalf("segunda petición: %v", h)
	}

	// sin HistoryOnDemand no se pide
	caps := e.adapter(acc.ID).Capabilities(nil)
	caps.HistoryOnDemand = false
	ad.SetCapabilities(caps)
	e.inbox.now = func() time.Time { return time.Now().Add(time.Hour) }
	_, _ = e.inbox.Messages(ctx, key, 0, 50)
	e.inbox.bgWG.Wait()
	if h := hist(); len(h) != 2 {
		t.Fatalf("sin HistoryOnDemand: %v", h)
	}
}

func TestInboxLoadOlderUsesOldestAsAnchor(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 100)
	if err := e.inbox.LoadOlder(ctx, model.NewChatKey(acc.ID, "c1")); err != nil {
		t.Fatal(err)
	}
	// con 450 mensajes hay que recorrer varias páginas locales hasta el más antiguo
	for i := int64(1); i <= 450; i++ {
		e.seedMessage(acc.ID, "c1", "m"+itoa(i), i, false, "x")
	}
	st := e.store(acc.ID)
	oldest, ok := oldestMessage(ctx, st, "c1")
	if !ok || oldest.NativeID != "m1" {
		t.Fatalf("el más antiguo: %+v %v", oldest, ok)
	}
	if err := e.inbox.LoadOlder(ctx, model.NewChatKey(acc.ID, "c1")); err != nil {
		t.Fatal(err)
	}
	if _, ok := oldestMessage(ctx, st, "vacio"); ok {
		t.Fatal("un chat vacío no tiene mensaje más antiguo")
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestInboxSearchAndUnread(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	wa := e.newAccount(model.ChannelWhatsApp, "")
	tg := e.newAccount(model.ChannelTelegram, "")
	e.seedChat(wa.ID, "c1", "A", 1)
	e.seedChat(tg.ID, "c2", "B", 1)
	e.seedMessage(wa.ID, "c1", "m1", 10, false, "precio del cemento")
	e.seedMessage(tg.ID, "c2", "m2", 30, false, "cemento gris")
	e.seedMessage(tg.ID, "c2", "m3", 20, false, "otra cosa")

	got, err := e.inbox.Search(ctx, "CEMENTO", nil, 0)
	if err != nil || len(got) != 2 || got[0].NativeID != "m2" {
		t.Fatalf("búsqueda global: %+v %v", got, err)
	}
	got, _ = e.inbox.Search(ctx, "cemento", []model.AccountID{wa.ID}, 10)
	if len(got) != 1 || got[0].NativeID != "m1" {
		t.Fatalf("búsqueda por cuenta: %+v", got)
	}
	got, _ = e.inbox.Search(ctx, "cemento", nil, 1)
	if len(got) != 1 {
		t.Fatalf("límite: %+v", got)
	}

	totals, err := e.inbox.UnreadTotals(ctx)
	if err != nil || totals[wa.ID] != 1 || totals[tg.ID] != 2 {
		t.Fatalf("no leídos: %+v %v", totals, err)
	}
}

func TestInboxArchivePinMute(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 1)
	key := model.NewChatKey(acc.ID, "c1")
	ad := e.adapter(acc.ID)

	from := e.mark()
	if err := e.inbox.Archive(ctx, key, true); err != nil {
		t.Fatal(err)
	}
	ev := e.waitEvent(from, "chat.updated", ofType(model.EvChatUpsert))
	if ev.Chat == nil || !ev.Chat.Archived || ev.Chat.Key != key || ev.AccountID != acc.ID {
		t.Fatalf("evento: %+v", ev)
	}
	if c, _ := e.store(acc.ID).chat("c1"); !c.Archived {
		t.Fatal("debe quedar archivado en local")
	}
	if err := e.inbox.Pin(ctx, key, true); err != nil {
		t.Fatal(err)
	}
	if err := e.inbox.Mute(ctx, key, -1); err != nil {
		t.Fatal(err)
	}
	c, _ := e.store(acc.ID).chat("c1")
	if !c.Pinned || c.MutedUntil != -1 || !c.Archived {
		t.Fatalf("estado local: %+v", c)
	}
	calls := strings.Join(ad.Calls(), " ")
	for _, want := range []string{"ArchiveChat:c1:true", "PinChat:c1:true", "MuteChat:c1:-1"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("falta la llamada %s en %s", want, calls)
		}
	}

	// el canal no lo admite: solo local, sin error, y se publica igual
	ad.archiveErr = port.ErrUnsupported
	from = e.mark()
	if err := e.inbox.Pin(ctx, key, false); err != nil {
		t.Fatalf("ErrUnsupported no debe ser error: %v", err)
	}
	e.waitEvent(from, "chat.updated", ofType(model.EvChatUpsert))
	if c, _ := e.store(acc.ID).chat("c1"); c.Pinned {
		t.Fatal("debe desfijarse en local")
	}

	// otros errores sí se devuelven y no se aplica nada
	ad.archiveErr = port.ErrNotConnected
	if err := e.inbox.Archive(ctx, key, false); !errors.Is(err, port.ErrNotConnected) {
		t.Fatalf("error del adaptador: %v", err)
	}
	if c, _ := e.store(acc.ID).chat("c1"); !c.Archived {
		t.Fatal("si falla el adaptador no se cambia lo local")
	}
	if err := e.inbox.Archive(ctx, model.NewChatKey(acc.ID, "nope"), true); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("chat desconocido: %v", err)
	}
}
