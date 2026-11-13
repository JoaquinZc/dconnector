package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

func hasCall(ad *testAdapter, call string) bool {
	for _, c := range ad.Calls() {
		if c == call {
			return true
		}
	}
	return false
}

func setSelf(t *testing.T, e *testEnv, id model.AccountID) {
	t.Helper()
	acc, err := e.app.GetAccount(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	acc.SelfID, acc.SelfName = "me", "Yo"
	if err := e.app.SaveAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
}

func TestMessagingSend(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	key := model.NewChatKey(acc.ID, "593999")

	from := e.mark()
	msg, err := e.msg.Send(ctx, key, model.Outgoing{Kind: model.KindText, Text: "hola", ClientRef: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Key != model.NewMessageKey(acc.ID, "593999", msg.NativeID) || msg.ChatKey != key || !msg.FromMe || msg.AccountID != acc.ID {
		t.Fatalf("mensaje devuelto: %+v", msg)
	}
	if sent := e.adapter(acc.ID).Sent(); len(sent) != 1 || sent[0].Text != "hola" || sent[0].ClientRef != "r1" {
		t.Fatalf("el adaptador recibió: %+v", sent)
	}
	ev := e.waitEvent(from, "message.sent", ofType(model.EvMessageSent))
	if ev.Message.Key != msg.Key {
		t.Fatalf("evento: %+v", ev.Message)
	}
	if got, ok := e.store(acc.ID).message("593999", msg.NativeID); !ok || got.Text != "hola" {
		t.Fatalf("no se guardó: %+v", got)
	}
	// el chat no existía: se crea con la última actividad
	if c, ok := e.store(acc.ID).chat("593999"); !ok || c.LastMessageAt == 0 || c.Kind != model.ChatDirect || c.LastPreview != "hola" {
		t.Fatalf("chat nuevo: %+v %v", c, ok)
	}

	if _, err := e.msg.Send(ctx, model.NewChatKey("wa-nope", "x"), model.Outgoing{Kind: model.KindText, Text: "x"}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
}

func TestMessagingSendKeepsExistingChat(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 5)
	from := e.mark()
	if _, err := e.msg.Send(context.Background(), model.NewChatKey(acc.ID, "c1"), model.Outgoing{Kind: model.KindText, Text: "hola"}); err != nil {
		t.Fatal(err)
	}
	e.waitEvent(from, "message.sent", ofType(model.EvMessageSent))
	if c, _ := e.store(acc.ID).chat("c1"); c.Title != "Ana" {
		t.Fatalf("no debe pisar el chat existente: %+v", c)
	}
}

func TestMessagingSendValidation(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 1)
	_ = e.store(acc.ID).UpsertChat(ctx, model.Chat{NativeID: "ro", Kind: model.ChatChannel, ReadOnly: true})
	key := model.NewChatKey(acc.ID, "c1")

	small := filepath.Join(t.TempDir(), "foto.jpg")
	if err := os.WriteFile(small, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	caps := ad.Capabilities(nil)
	caps.MaxTextLen, caps.MaxImageSize, caps.MaxFileSize, caps.MaxVideoSize = 10, 1024, 1024, 1024
	caps.SendSticker, caps.Reply = false, false
	ad.SetCapabilities(caps)

	cases := []struct {
		name string
		key  model.ChatKey
		out  model.Outgoing
		want error
	}{
		{"texto vacío", key, model.Outgoing{Kind: model.KindText, Text: "   "}, port.ErrInvalid},
		{"texto largo", key, model.Outgoing{Kind: model.KindText, Text: "12345678901"}, port.ErrInvalid},
		{"pie largo", key, model.Outgoing{Kind: model.KindImage, Text: "12345678901", FilePath: small}, port.ErrInvalid},
		{"imagen grande", key, model.Outgoing{Kind: model.KindImage, FilePath: small}, port.ErrInvalid},
		{"documento grande", key, model.Outgoing{Kind: model.KindDocument, FilePath: small}, port.ErrInvalid},
		{"video grande", key, model.Outgoing{Kind: model.KindVideo, FilePath: small}, port.ErrInvalid},
		{"sin archivo", key, model.Outgoing{Kind: model.KindImage}, port.ErrInvalid},
		{"archivo inexistente", key, model.Outgoing{Kind: model.KindImage, FilePath: filepath.Join(t.TempDir(), "no.jpg")}, port.ErrInvalid},
		{"sticker no admitido", key, model.Outgoing{Kind: model.KindSticker, FilePath: small}, port.ErrUnsupported},
		{"responder no admitido", key, model.Outgoing{Kind: model.KindText, Text: "hola", ReplyTo: &model.MessageRef{NativeID: "m"}}, port.ErrUnsupported},
		{"chat de solo lectura", model.NewChatKey(acc.ID, "ro"), model.Outgoing{Kind: model.KindText, Text: "hola"}, port.ErrUnsupported},
		{"ubicación sin datos", key, model.Outgoing{Kind: model.KindLocation}, port.ErrInvalid},
		{"contacto sin datos", key, model.Outgoing{Kind: model.KindContact}, port.ErrInvalid},
		{"encuesta de una opción", key, model.Outgoing{Kind: model.KindPoll, Poll: &model.Poll{Question: "¿?", Options: []model.PollOption{{Name: "a"}}}}, port.ErrInvalid},
		{"tipo desconocido", key, model.Outgoing{Kind: model.ContentKind("hologram")}, port.ErrInvalid},
	}
	for _, c := range cases {
		if _, err := e.msg.Send(ctx, c.key, c.out); !errors.Is(err, c.want) {
			t.Errorf("%s: error %v, quería %v", c.name, err, c.want)
		}
	}
	if sent := ad.Sent(); len(sent) != 0 {
		t.Fatalf("nada de eso debe llegar al adaptador: %+v", sent)
	}

	// capacidad apagada de texto
	caps.SendText = false
	ad.SetCapabilities(caps)
	if _, err := e.msg.Send(ctx, key, model.Outgoing{Kind: model.KindText, Text: "hola"}); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("SendText apagado: %v", err)
	}

	// lo válido pasa (archivo justo en el límite, ubicación, encuesta)
	caps = ad.Capabilities(nil)
	caps.SendText, caps.MaxImageSize, caps.MaxTextLen = true, 2048, 0
	ad.SetCapabilities(caps)
	for _, out := range []model.Outgoing{
		{Kind: model.KindImage, FilePath: small, Text: "pie", Mime: "image/jpeg"},
		{Kind: model.KindLocation, Location: &model.Location{Lat: 1, Lng: 2}},
		{Kind: model.KindPoll, Poll: &model.Poll{Question: "¿?", Options: []model.PollOption{{Name: "a"}, {Name: "b"}}}},
		{Kind: model.KindText, Text: "hola", Mentions: []string{"x"}},
	} {
		if _, err := e.msg.Send(ctx, key, out); err != nil {
			t.Errorf("%s debía enviarse: %v", out.Kind, err)
		}
	}
}

func TestMessagingSendAdapterError(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.adapter(acc.ID).SendErr = port.ErrRateLimited
	from := e.mark()
	_, err := e.msg.Send(context.Background(), model.NewChatKey(acc.ID, "c1"), model.Outgoing{Kind: model.KindText, Text: "hola"})
	if !errors.Is(err, port.ErrRateLimited) {
		t.Fatalf("debe llegar el error del adaptador: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	for _, ev := range e.eventsSince(from) {
		if ev.Type == model.EvMessageSent || ev.Type == model.EvChatUpsert {
			t.Fatalf("si el envío falla no se emite nada: %+v", ev)
		}
	}
	if _, ok := e.store(acc.ID).chat("c1"); ok {
		t.Fatal("no debe crearse el chat")
	}
}

func TestMessagingReact(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 1)
	m := e.seedMessage(acc.ID, "c1", "m1", 100, false, "hola")
	ad := e.adapter(acc.ID)

	// sin SelfID conocido solo se llama al adaptador
	if err := e.msg.React(ctx, m.Key, "👍"); err != nil {
		t.Fatal(err)
	}
	if !hasCall(ad, "React:m1:👍") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}

	setSelf(t, e, acc.ID)
	from := e.mark()
	if err := e.msg.React(ctx, m.Key, "❤️"); err != nil {
		t.Fatal(err)
	}
	ev := e.waitEvent(from, "reaction", ofType(model.EvReaction))
	if len(ev.Message.Reactions) != 1 || ev.Message.Reactions[0].Emoji != "❤️" || ev.Message.Reactions[0].SenderNativeID != "me" {
		t.Fatalf("reacciones: %+v", ev.Message.Reactions)
	}
	from = e.mark()
	if err := e.msg.React(ctx, m.Key, ""); err != nil {
		t.Fatal(err)
	}
	ev = e.waitEvent(from, "reacción quitada", ofType(model.EvReaction))
	if len(ev.Message.Reactions) != 0 {
		t.Fatalf("debe quitarse: %+v", ev.Message.Reactions)
	}

	caps := ad.Capabilities(nil)
	caps.React = false
	ad.SetCapabilities(caps)
	if err := e.msg.React(ctx, m.Key, "👍"); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("React apagado: %v", err)
	}
	if err := e.msg.React(ctx, model.NewMessageKey(acc.ID, "c1", "nope"), "👍"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("mensaje desconocido: %v", err)
	}
	if err := e.msg.React(ctx, "mal", "👍"); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("clave mal formada: %v", err)
	}
}

func TestMessagingEdit(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 1)
	now := time.Now()
	e.msg.now = func() time.Time { return now }
	recent := e.seedMessage(acc.ID, "c1", "m1", now.Add(-5*time.Minute).UnixMilli(), true, "viejo")
	old := e.seedMessage(acc.ID, "c1", "m2", now.Add(-20*time.Minute).UnixMilli(), true, "muy viejo")
	theirs := e.seedMessage(acc.ID, "c1", "m3", now.UnixMilli(), false, "ajeno")

	from := e.mark()
	if err := e.msg.Edit(ctx, recent.Key, "nuevo"); err != nil {
		t.Fatal(err)
	}
	if !hasCall(ad, "Edit:m1:nuevo") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}
	ev := e.waitEvent(from, "message.updated", ofType(model.EvMessageUpdate))
	if ev.Message.Text != "nuevo" || ev.Message.EditedAt != now.UnixMilli() {
		t.Fatalf("mensaje editado: %+v", ev.Message)
	}
	if err := e.msg.Edit(ctx, old.Key, "x"); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("fuera de plazo: %v", err)
	}
	if err := e.msg.Edit(ctx, theirs.Key, "x"); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("mensaje ajeno: %v", err)
	}
	if err := e.msg.Edit(ctx, recent.Key, "  "); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("texto vacío: %v", err)
	}
	caps := ad.Capabilities(nil)
	caps.EditWindow = 0 // sin límite: el viejo ya se puede
	ad.SetCapabilities(caps)
	if err := e.msg.Edit(ctx, old.Key, "ok"); err != nil {
		t.Fatalf("sin ventana: %v", err)
	}
	caps.MaxTextLen = 3
	ad.SetCapabilities(caps)
	if err := e.msg.Edit(ctx, old.Key, "demasiado largo"); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("texto largo: %v", err)
	}
	caps.Edit = false
	ad.SetCapabilities(caps)
	if err := e.msg.Edit(ctx, old.Key, "x"); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("Edit apagado: %v", err)
	}
}

func TestMessagingDelete(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 1)
	now := time.Now()
	e.msg.now = func() time.Time { return now }
	mine := e.seedMessage(acc.ID, "c1", "m1", now.Add(-time.Hour).UnixMilli(), true, "mío")
	ancient := e.seedMessage(acc.ID, "c1", "m2", now.Add(-72*time.Hour).UnixMilli(), true, "antiguo")
	theirs := e.seedMessage(acc.ID, "c1", "m3", now.UnixMilli(), false, "ajeno")

	from := e.mark()
	if err := e.msg.Delete(ctx, mine.Key, true); err != nil {
		t.Fatal(err)
	}
	if !hasCall(ad, "Delete:m1:true") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}
	ev := e.waitEvent(from, "message.updated", ofType(model.EvMessageUpdate))
	if ev.Message.DeletedAt == 0 {
		t.Fatalf("debe quedar borrado: %+v", ev.Message)
	}
	if err := e.msg.Delete(ctx, ancient.Key, true); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("fuera de plazo: %v", err)
	}
	if err := e.msg.Delete(ctx, theirs.Key, true); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("borrar para todos un mensaje ajeno: %v", err)
	}
	// borrar solo para mí no tiene plazo ni exige ser el autor
	if err := e.msg.Delete(ctx, ancient.Key, false); err != nil {
		t.Fatalf("borrar para mí: %v", err)
	}
	if err := e.msg.Delete(ctx, theirs.Key, false); err != nil {
		t.Fatalf("borrar para mí lo ajeno: %v", err)
	}
	caps := ad.Capabilities(nil)
	caps.DeleteForEveryone = false
	ad.SetCapabilities(caps)
	if err := e.msg.Delete(ctx, mine.Key, true); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("DeleteForEveryone apagado: %v", err)
	}
	if err := e.msg.Delete(ctx, mine.Key, false); err != nil {
		t.Fatalf("para mí sigue permitido: %v", err)
	}
}

func TestMessagingForward(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	other := e.newAccount(model.ChannelTelegram, "")
	e.seedChat(acc.ID, "c1", "Ana", 1)
	e.seedChat(acc.ID, "c2", "Beto", 1)
	e.seedChat(other.ID, "x1", "Otra", 1)
	m := e.seedMessage(acc.ID, "c1", "m1", 10, false, "hola")

	from := e.mark()
	got, err := e.msg.Forward(ctx, m.Key, model.NewChatKey(acc.ID, "c2"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatNativeID != "c2" || !got.Forwarded || got.Key == "" {
		t.Fatalf("reenviado: %+v", got)
	}
	e.waitEvent(from, "message.sent", ofType(model.EvMessageSent))
	if _, ok := e.store(acc.ID).message("c2", got.NativeID); !ok {
		t.Fatal("el reenviado debe guardarse en el chat destino")
	}
	if !hasCall(e.adapter(acc.ID), "Forward:m1:c2") {
		t.Fatalf("llamadas: %v", e.adapter(acc.ID).Calls())
	}
	if _, err := e.msg.Forward(ctx, m.Key, model.NewChatKey(other.ID, "x1")); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("a otra cuenta: %v", err)
	}
	caps := e.adapter(acc.ID).Capabilities(nil)
	caps.Forward = false
	e.adapter(acc.ID).SetCapabilities(caps)
	if _, err := e.msg.Forward(ctx, m.Key, model.NewChatKey(acc.ID, "c2")); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("Forward apagado: %v", err)
	}
}

func TestMessagingVotePoll(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 1)
	single := e.seedMessage(acc.ID, "c1", "p1", 10, false, "")
	multi := e.seedMessage(acc.ID, "c1", "p2", 11, false, "")
	plain := e.seedMessage(acc.ID, "c1", "t1", 12, false, "hola")
	for _, p := range []struct {
		key   string
		multi bool
	}{{"p1", false}, {"p2", true}} {
		st := e.store(acc.ID)
		mm, _ := st.message("c1", p.key)
		mm.Kind = model.KindPoll
		mm.Poll = &model.Poll{Question: "¿?", Multi: p.multi, Options: []model.PollOption{{Name: "a"}, {Name: "b"}}}
		_, _ = st.UpsertMessage(ctx, mm)
	}

	if err := e.msg.VotePoll(ctx, single.Key, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if !hasCall(ad, "VotePoll:p1:1") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}
	if err := e.msg.VotePoll(ctx, single.Key, []string{"a", "b"}); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("varias opciones en encuesta simple: %v", err)
	}
	if err := e.msg.VotePoll(ctx, multi.Key, []string{"a", "b"}); err != nil {
		t.Fatalf("encuesta múltiple: %v", err)
	}
	if err := e.msg.VotePoll(ctx, multi.Key, []string{"zzz"}); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("opción inexistente: %v", err)
	}
	if err := e.msg.VotePoll(ctx, plain.Key, []string{"a"}); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("no es encuesta: %v", err)
	}
	caps := ad.Capabilities(nil)
	caps.VotePoll = false
	ad.SetCapabilities(caps)
	if err := e.msg.VotePoll(ctx, single.Key, []string{"a"}); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("VotePoll apagado: %v", err)
	}
}

func TestMessagingMarkRead(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 1)
	for i := int64(1); i <= 3; i++ {
		e.seedMessage(acc.ID, "c1", "in"+itoa(i), i, false, "x")
	}
	e.seedMessage(acc.ID, "c1", "out", 4, true, "mío")
	key := model.NewChatKey(acc.ID, "c1")
	if c, _ := e.store(acc.ID).chat("c1"); c.UnreadCount != 3 {
		t.Fatalf("preparación: %+v", c)
	}

	from := e.mark()
	if err := e.msg.MarkRead(ctx, key); err != nil {
		t.Fatal(err)
	}
	if !hasCall(ad, "MarkRead:c1:3") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}
	ev := e.waitEvent(from, "chat.updated", ofType(model.EvChatUpsert))
	if ev.Chat.UnreadCount != 0 || ev.Chat.Key != key {
		t.Fatalf("chat publicado: %+v", ev.Chat)
	}
	if c, _ := e.store(acc.ID).chat("c1"); c.UnreadCount != 0 {
		t.Fatalf("unreadCount = %d", c.UnreadCount)
	}
	if m, _ := e.store(acc.ID).message("c1", "in1"); m.Delivery != model.DeliveryRead {
		t.Fatalf("el entrante debe quedar leído: %+v", m)
	}
	// ya no hay nada que leer: no vuelve a llamar al adaptador
	if err := e.msg.MarkRead(ctx, key); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range ad.Calls() {
		if strings.HasPrefix(c, "MarkRead") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("MarkRead al adaptador %d veces, quería 1", n)
	}
	if err := e.msg.MarkRead(ctx, model.NewChatKey(acc.ID, "nope")); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("chat desconocido: %v", err)
	}
}

func TestMessagingMarkReadLimitAndCaps(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelTelegram, "")
	ad := e.adapter(acc.ID)
	e.seedChat(acc.ID, "c1", "Ana", 1)
	for i := int64(1); i <= 60; i++ {
		e.seedMessage(acc.ID, "c1", "in"+itoa(i), i, false, "x")
	}
	key := model.NewChatKey(acc.ID, "c1")
	if err := e.msg.MarkRead(ctx, key); err != nil {
		t.Fatal(err)
	}
	if !hasCall(ad, "MarkRead:c1:50") {
		t.Fatalf("máximo 50 recientes: %v", ad.Calls())
	}

	// canal sin acuses de lectura: no se llama al adaptador, pero se limpia lo local
	e.seedChat(acc.ID, "c2", "Beto", 1)
	_ = e.store(acc.ID).PatchChat(ctx, model.Chat{NativeID: "c2", UnreadCount: 4}, []string{"unreadCount"})
	caps := ad.Capabilities(nil)
	caps.ReadReceipts = false
	ad.SetCapabilities(caps)
	if err := e.msg.MarkRead(ctx, model.NewChatKey(acc.ID, "c2")); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.store(acc.ID).chat("c2"); c.UnreadCount != 0 {
		t.Fatalf("unreadCount = %d", c.UnreadCount)
	}
}

func TestMessagingSetTyping(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	key := model.NewChatKey(acc.ID, "c1")
	if err := e.msg.SetTyping(ctx, key, true); err != nil {
		t.Fatal(err)
	}
	if !hasCall(ad, "SetTyping:c1:true") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}
	caps := ad.Capabilities(nil)
	caps.Typing = false
	ad.SetCapabilities(caps)
	if err := e.msg.SetTyping(ctx, key, true); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("Typing apagado: %v", err)
	}
}

func seedMedia(t *testing.T, e *testEnv, acc model.AccountID, chat, msgID, mediaID, mime string, size int64, kind model.ContentKind) model.Message {
	t.Helper()
	m := model.Message{
		Key: model.NewMessageKey(acc, chat, msgID), ChatKey: model.NewChatKey(acc, chat), AccountID: acc,
		NativeID: msgID, ChatNativeID: chat, Timestamp: 1, Kind: kind,
		Media: &model.Media{ID: mediaID, Mime: mime, Size: size},
	}
	if _, err := e.store(acc).UpsertMessage(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMessagingMediaPath(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	seedMedia(t, e, acc.ID, "c1", "med1", "med1", "image/jpeg", 3, model.KindImage)
	ad.SetMedia("med1", []byte("abc"))

	from := e.mark()
	path, err := e.msg.MediaPath(ctx, acc.ID, "med1")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(paths.MediaDir(acc.ID), "med1.jpg") {
		t.Fatalf("ruta: %s", path)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "abc" {
		t.Fatalf("contenido: %q %v", b, err)
	}
	md, _, _ := e.store(acc.ID).GetMedia(ctx, "med1")
	if md.LocalPath != path {
		t.Fatalf("SetMediaPath: %q", md.LocalPath)
	}
	ev := e.waitEvent(from, "message.updated con el medio", ofType(model.EvMessageUpdate))
	if ev.Message.Media == nil || ev.Message.Media.LocalPath != path {
		t.Fatalf("evento: %+v", ev.Message)
	}
	if entries, _ := os.ReadDir(paths.MediaDir(acc.ID)); len(entries) != 1 {
		t.Fatalf("no deben quedar temporales: %v", entries)
	}

	// ya descargado: no vuelve a bajar
	if p2, err := e.msg.MediaPath(ctx, acc.ID, "med1"); err != nil || p2 != path {
		t.Fatalf("segunda llamada: %q %v", p2, err)
	}
	if calls, _ := ad.downloads(); calls != 1 {
		t.Fatalf("descargas = %d, quería 1", calls)
	}
	// si el archivo desapareció, se baja de nuevo
	_ = os.Remove(path)
	if _, err := e.msg.MediaPath(ctx, acc.ID, "med1"); err != nil {
		t.Fatal(err)
	}
	if calls, _ := ad.downloads(); calls != 2 {
		t.Fatalf("descargas = %d, quería 2", calls)
	}

	if _, err := e.msg.MediaPath(ctx, acc.ID, "nope"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("medio desconocido: %v", err)
	}
	seedMedia(t, e, acc.ID, "c1", "med2", "med2", "image/png", 1, model.KindImage) // el adaptador no lo tiene
	if _, err := e.msg.MediaPath(ctx, acc.ID, "med2"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("el adaptador no lo tiene: %v", err)
	}
	if _, err := e.msg.MediaPath(ctx, "wa-nope", "med1"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
}

func TestMessagingMediaPathSingleFlight(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	seedMedia(t, e, acc.ID, "c1", "med1", "med1", "audio/ogg; codecs=opus", 3, model.KindAudio)
	ad.SetMedia("med1", []byte("abc"))
	ad.dlGate = make(chan struct{})

	var wg sync.WaitGroup
	results := make([]string, 6)
	errs := make([]error, 6)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = e.msg.MediaPath(ctx, acc.ID, "med1")
		}()
	}
	eventually(t, "una descarga en curso", func() bool { c, _ := ad.downloads(); return c == 1 })
	time.Sleep(30 * time.Millisecond) // las demás llamadas ya están esperando
	close(ad.dlGate)
	wg.Wait()
	if calls, _ := ad.downloads(); calls != 1 {
		t.Fatalf("descargas = %d, quería 1", calls)
	}
	for i := range results {
		if errs[i] != nil || results[i] != filepath.Join(paths.MediaDir(acc.ID), "med1.ogg") {
			t.Fatalf("resultado %d: %q %v", i, results[i], errs[i])
		}
	}
}

func TestMediaFileName(t *testing.T) {
	cases := []struct {
		m    model.Media
		want string
	}{
		{model.Media{ID: "abc", Mime: "image/jpeg"}, "abc.jpg"},
		{model.Media{ID: "abc", Mime: "audio/ogg; codecs=opus"}, "abc.ogg"},
		{model.Media{ID: "abc", Mime: "application/octet-stream", FileName: "Informe.PDF"}, "abc.pdf"},
		{model.Media{ID: "a/b:c", Mime: "video/mp4"}, "a_b_c.mp4"},
		{model.Media{ID: "x", Mime: "algo/raro"}, "x.bin"},
		{model.Media{ID: "..", Mime: ""}, "media.bin"},
	}
	for _, c := range cases {
		if got := mediaFileName(c.m); got != c.want {
			t.Errorf("%+v: %q, quería %q", c.m, got, c.want)
		}
	}
}

func injectMedia(ad *testAdapter, chat, id string, kind model.ContentKind, size int64, fromMe bool) {
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: &model.Message{
		NativeID: id, ChatNativeID: chat, Timestamp: 1, Kind: kind, FromMe: fromMe,
		Media: &model.Media{ID: id, Mime: "image/jpeg", Size: size},
	}})
}

func TestMessagingAutoDownload(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	for _, id := range []string{"img", "vid", "big", "mine"} {
		ad.SetMedia(id, []byte("data"))
	}

	injectMedia(ad, "c1", "vid", model.KindVideo, 10, false)     // video: apagado por defecto
	injectMedia(ad, "c1", "big", model.KindImage, 25<<20, false) // imagen > 20 MB
	injectMedia(ad, "c1", "mine", model.KindImage, 10, true)     // saliente
	injectMedia(ad, "c1", "img", model.KindImage, 10, false)     // sí
	eventually(t, "descarga automática de la imagen", func() bool {
		md, _, err := e.store(acc.ID).GetMedia(context.Background(), "img")
		return err == nil && md.LocalPath != ""
	})
	time.Sleep(50 * time.Millisecond)
	if calls, _ := ad.downloads(); calls != 1 {
		t.Fatalf("descargas = %d, quería solo la de la imagen", calls)
	}

	// cambiar el ajuste: ahora el video sí
	ctx := context.Background()
	_ = e.app.SetSetting(ctx, SettingAutoDownload, `{"video":{"enabled":true,"maxMB":50}}`)
	ad.SetMedia("vid2", []byte("data"))
	injectMedia(ad, "c1", "vid2", model.KindVideo, 10, false)
	eventually(t, "descarga automática del video", func() bool {
		md, _, err := e.store(acc.ID).GetMedia(ctx, "vid2")
		return err == nil && md.LocalPath != ""
	})
}

func TestMessagingAutoDownloadMaxThree(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	ad.dlGate = make(chan struct{})
	ids := []string{"a", "b", "c", "d", "e", "f"}
	for _, id := range ids {
		ad.SetMedia(id, []byte("x"))
		injectMedia(ad, "c1", id, model.KindImage, 1, false)
	}
	eventually(t, "tres descargas en paralelo", func() bool { _, mx := ad.downloads(); return mx == 3 })
	time.Sleep(50 * time.Millisecond)
	if calls, mx := ad.downloads(); mx != 3 || calls != 3 {
		t.Fatalf("llamadas=%d máximo simultáneo=%d, quería 3 y 3", calls, mx)
	}
	close(ad.dlGate)
	eventually(t, "las seis terminan", func() bool {
		for _, id := range ids {
			md, _, err := e.store(acc.ID).GetMedia(context.Background(), id)
			if err != nil || md.LocalPath == "" {
				return false
			}
		}
		return true
	})
	if _, mx := ad.downloads(); mx != 3 {
		t.Fatalf("máximo simultáneo = %d", mx)
	}
}
