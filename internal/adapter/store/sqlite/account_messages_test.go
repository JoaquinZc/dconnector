package sqlite

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func mkMsg(chat, native, text string, ts int64, fromMe bool) model.Message {
	return model.Message{
		ChatNativeID: chat, NativeID: native, SenderNativeID: "s-" + native, Timestamp: ts, FromMe: fromMe,
		Kind: model.KindText, Text: text, Delivery: model.DeliverySent,
	}
}

func mkey(chat, native string) model.MessageKey { return model.NewMessageKey(testAcc, chat, native) }

func unread(t *testing.T, s *AccountStore, chat string) int {
	t.Helper()
	c, err := s.GetChat(bg, chat)
	require.NoError(t, err)
	return c.UnreadCount
}

func ftsCount(t *testing.T, s *AccountStore) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM messages_fts`).Scan(&n))
	return n
}

func TestUpsertMessageIdempotent(t *testing.T) {
	s := newTestAccount(t)
	m := mkMsg("c1", "m1", "Hola mundo", 1000, false)
	m.Location = &model.Location{Lat: -0.18, Lng: -78.46, Name: "Quito"}
	m.QuotedNativeID, m.QuotedPreview, m.QuotedSender = "q1", "cita", "Beto"
	m.Mentions = []string{"a@s"}
	m.Forwarded = true
	m.Contacts = []model.ContactCard{{Name: "Ana", Phone: "+593"}}
	m.FromStatusID = "st1"

	created, err := s.UpsertMessage(bg, m)
	require.NoError(t, err)
	require.True(t, created)
	created, err = s.UpsertMessage(bg, m)
	require.NoError(t, err)
	require.False(t, created)

	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n))
	require.Equal(t, 1, n)
	require.Equal(t, 1, ftsCount(t, s))
	require.Equal(t, 1, unread(t, s, "c1")) // la repetición no suma

	got, err := s.GetMessage(bg, "c1", "m1")
	require.NoError(t, err)
	m.Key = mkey("c1", "m1")
	m.ChatKey = model.NewChatKey(testAcc, "c1")
	m.AccountID = testAcc
	m.Channel = model.ChannelWhatsApp
	require.Equal(t, m, got)

	// Sin clave de chat/mensaje no se puede guardar; con la clave sola sí.
	_, err = s.UpsertMessage(bg, model.Message{Text: "x"})
	require.ErrorIs(t, err, port.ErrInvalid)
	created, err = s.UpsertMessage(bg, model.Message{Key: mkey("c1", "m2"), Timestamp: 2000, Kind: model.KindText, Text: "por clave"})
	require.NoError(t, err)
	require.True(t, created)
	_, err = s.GetMessage(bg, "c1", "m2")
	require.NoError(t, err)
	_, err = s.GetMessage(bg, "c1", "nada")
	require.ErrorIs(t, err, port.ErrNotFound)
}

func TestUnreadAndLastMessageRules(t *testing.T) {
	s := newTestAccount(t)
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: "c", Kind: model.ChatDirect, Title: "Ana"}))

	// Entrante nuevo y más reciente: +1 y vista previa.
	_, _ = s.UpsertMessage(bg, mkMsg("c", "m1", "primero", 100, false))
	require.Equal(t, 1, unread(t, s, "c"))
	c, _ := s.GetChat(bg, "c")
	require.Equal(t, int64(100), c.LastMessageAt)
	require.Equal(t, "primero", c.LastPreview)
	require.False(t, c.LastFromMe)
	require.Equal(t, "Ana", c.Title)

	// Mío: actualiza la vista previa, no suma.
	_, _ = s.UpsertMessage(bg, mkMsg("c", "m2", "respuesta mía", 200, true))
	require.Equal(t, 1, unread(t, s, "c"))
	c, _ = s.GetChat(bg, "c")
	require.Equal(t, "respuesta mía", c.LastPreview)
	require.True(t, c.LastFromMe)

	// Entrante más antiguo que el último: no suma ni cambia la vista previa.
	_, _ = s.UpsertMessage(bg, mkMsg("c", "m0", "llegó tarde", 50, false))
	require.Equal(t, 1, unread(t, s, "c"))
	c, _ = s.GetChat(bg, "c")
	require.Equal(t, int64(200), c.LastMessageAt)
	require.Equal(t, "respuesta mía", c.LastPreview)

	// Entrante nuevo más reciente: +1.
	img := mkMsg("c", "m3", "", 300, false)
	img.Kind = model.KindImage
	img.Media = &model.Media{ID: "med1", Mime: "image/jpeg"}
	_, _ = s.UpsertMessage(bg, img)
	require.Equal(t, 2, unread(t, s, "c"))
	c, _ = s.GetChat(bg, "c")
	require.Equal(t, "📷 Foto", c.LastPreview)

	// El histórico nunca suma, aunque sea más reciente, pero sí avanza el último mensaje.
	n, err := s.UpsertMessages(bg, []model.Message{
		mkMsg("c", "h1", "hist 1", 400, false), mkMsg("c", "h2", "hist 2", 500, false), mkMsg("c", "m1", "primero", 100, false),
	})
	require.NoError(t, err)
	require.Equal(t, 2, n) // m1 ya existía
	require.Equal(t, 2, unread(t, s, "c"))
	c, _ = s.GetChat(bg, "c")
	require.Equal(t, int64(500), c.LastMessageAt)
	require.Equal(t, "hist 2", c.LastPreview)

	// Un mensaje de un chat desconocido crea el chat.
	_, err = s.UpsertMessage(bg, mkMsg("nuevo", "x1", "hola", 10, false))
	require.NoError(t, err)
	c, err = s.GetChat(bg, "nuevo")
	require.NoError(t, err)
	require.Equal(t, 1, c.UnreadCount)
	require.Equal(t, model.ChatDirect, c.Kind)

	// Lote vacío y lote con un error se deshace entero.
	n, err = s.UpsertMessages(bg, nil)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	_, err = s.UpsertMessages(bg, []model.Message{mkMsg("c", "ok", "ok", 600, false), {Text: "sin id"}})
	require.Error(t, err)
	_, err = s.GetMessage(bg, "c", "ok")
	require.ErrorIs(t, err, port.ErrNotFound)
}

func TestDeliveryOrder(t *testing.T) {
	s := newTestAccount(t)
	deliveryOf := func(id string) model.DeliveryState {
		m, err := s.GetMessage(bg, "c", id)
		require.NoError(t, err)
		return m.Delivery
	}
	put := func(id string, d model.DeliveryState) {
		m := mkMsg("c", id, "t", 100, true)
		m.Delivery = d
		_, err := s.UpsertMessage(bg, m)
		require.NoError(t, err)
	}

	// Nunca baja: pending < sent < delivered < read < played.
	put("a", model.DeliveryRead)
	put("a", model.DeliverySent)
	require.Equal(t, model.DeliveryRead, deliveryOf("a"))
	put("a", model.DeliveryPending)
	require.Equal(t, model.DeliveryRead, deliveryOf("a"))
	put("a", model.DeliveryPlayed)
	require.Equal(t, model.DeliveryPlayed, deliveryOf("a"))
	put("a", model.DeliveryDelivered)
	require.Equal(t, model.DeliveryPlayed, deliveryOf("a"))

	// failed solo pisa a pending.
	put("b", model.DeliveryPending)
	put("b", model.DeliveryFailed)
	require.Equal(t, model.DeliveryFailed, deliveryOf("b"))
	put("c1", model.DeliverySent)
	put("c1", model.DeliveryFailed)
	require.Equal(t, model.DeliverySent, deliveryOf("c1"))
	put("d", model.DeliveryRead)
	put("d", model.DeliveryFailed)
	require.Equal(t, model.DeliveryRead, deliveryOf("d"))
	// Lo que llega después de un fallo lo reemplaza.
	put("b", model.DeliverySent)
	require.Equal(t, model.DeliverySent, deliveryOf("b"))

	// Un mensaje sin entrega indicada queda como "sent".
	m := mkMsg("c", "e", "t", 100, true)
	m.Delivery = ""
	_, _ = s.UpsertMessage(bg, m)
	require.Equal(t, model.DeliverySent, deliveryOf("e"))

	// Lo mismo por ApplyUpdate y ApplyReceipt.
	_, err := s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "e"), Delivery: model.DeliveryDelivered})
	require.NoError(t, err)
	_, err = s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "e"), Delivery: model.DeliverySent})
	require.NoError(t, err)
	require.Equal(t, model.DeliveryDelivered, deliveryOf("e"))
	require.NoError(t, s.ApplyReceipt(bg, model.ReceiptEvent{
		ChatKey: model.NewChatKey(testAcc, "c"), Delivery: model.DeliveryRead,
		MessageKeys: []model.MessageKey{mkey("c", "e"), mkey("c", "a"), mkey("c", "no-existe")},
	}))
	require.Equal(t, model.DeliveryRead, deliveryOf("e"))
	require.Equal(t, model.DeliveryPlayed, deliveryOf("a")) // no baja a read
	require.NoError(t, s.ApplyReceipt(bg, model.ReceiptEvent{Delivery: model.DeliveryDelivered, MessageKeys: []model.MessageKey{mkey("c", "e")}}))
	require.Equal(t, model.DeliveryRead, deliveryOf("e"))
	require.NoError(t, s.ApplyReceipt(bg, model.ReceiptEvent{Delivery: "", MessageKeys: []model.MessageKey{mkey("c", "e")}}))
	require.NoError(t, s.ApplyReceipt(bg, model.ReceiptEvent{Delivery: model.DeliveryRead}))
}

func TestReactionsOnePerPerson(t *testing.T) {
	s := newTestAccount(t)
	_, _ = s.UpsertMessage(bg, mkMsg("c", "m1", "hola", 100, false))
	react := func(sender, name, emoji string, at int64) model.Message {
		m, err := s.ApplyReaction(bg, model.ReactionEvent{
			MessageKey: mkey("c", "m1"), Reaction: model.Reaction{SenderNativeID: sender, SenderName: name, Emoji: emoji, At: at},
		})
		require.NoError(t, err)
		return m
	}

	m := react("ana", "Ana", "👍", 10)
	require.Len(t, m.Reactions, 1)
	// La última de la misma persona reemplaza a la anterior.
	m = react("ana", "Ana", "❤️", 20)
	require.Len(t, m.Reactions, 1)
	require.Equal(t, "❤️", m.Reactions[0].Emoji)
	require.Equal(t, int64(20), m.Reactions[0].At)
	// Otra persona suma.
	m = react("beto", "Beto", "😂", 15)
	require.Len(t, m.Reactions, 2)
	require.Equal(t, "beto", m.Reactions[0].SenderNativeID) // por hora
	// Una nueva reacción de Ana no toca la de Beto.
	m = react("ana", "Ana", "🔥", 30)
	require.Len(t, m.Reactions, 2)
	require.Equal(t, "😂", m.Reactions[0].Emoji)
	require.Equal(t, "🔥", m.Reactions[1].Emoji)

	// Conservadas al volver a guardar el mensaje.
	_, err := s.UpsertMessage(bg, mkMsg("c", "m1", "hola", 100, false))
	require.NoError(t, err)
	got, _ := s.GetMessage(bg, "c", "m1")
	require.Len(t, got.Reactions, 2)
	page, _ := s.ListMessages(bg, "c", 0, 10)
	require.Len(t, page.Messages[0].Reactions, 2)

	// Emoji vacío la quita.
	m = react("ana", "Ana", "", 40)
	require.Len(t, m.Reactions, 1)
	require.Equal(t, "beto", m.Reactions[0].SenderNativeID)
	m = react("beto", "Beto", "", 41)
	require.Empty(t, m.Reactions)
	m = react("nadie", "", "", 42) // quitar lo que no existe no falla
	require.Empty(t, m.Reactions)

	// Mensaje desconocido y clave rota.
	_, err = s.ApplyReaction(bg, model.ReactionEvent{MessageKey: mkey("c", "zz"), Reaction: model.Reaction{Emoji: "👍"}})
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.ApplyReaction(bg, model.ReactionEvent{MessageKey: "mala"})
	require.ErrorIs(t, err, port.ErrInvalid)
}

func TestMediaKeepsLocalPath(t *testing.T) {
	s := newTestAccount(t)
	m := mkMsg("c", "m1", "mira", 100, false)
	m.Kind = model.KindImage
	m.Media = &model.Media{ID: "med1", Mime: "image/jpeg", FileName: "a.jpg", Size: 10, Width: 4, Height: 3, Duration: 0, Thumb: "data:image/jpeg;base64,AA", Handle: []byte{1, 2, 3}}
	_, err := s.UpsertMessage(bg, m)
	require.NoError(t, err)

	got, _ := s.GetMessage(bg, "c", "m1")
	require.Equal(t, "/media/wa-test/med1", got.Media.URL)
	require.Equal(t, "", got.Media.LocalPath)
	require.Equal(t, []byte{1, 2, 3}, got.Media.Handle)

	require.NoError(t, s.SetMediaPath(bg, "med1", `C:\datos\med1.jpg`))
	require.ErrorIs(t, s.SetMediaPath(bg, "nada", "x"), port.ErrNotFound)

	// Volver a guardar el mensaje (sin ruta ni asa ni miniatura) no borra lo conocido.
	again := m
	again.Media = &model.Media{ID: "med1", Mime: "image/jpeg"}
	created, err := s.UpsertMessage(bg, again)
	require.NoError(t, err)
	require.False(t, created)
	got, _ = s.GetMessage(bg, "c", "m1")
	require.Equal(t, `C:\datos\med1.jpg`, got.Media.LocalPath)
	require.Equal(t, []byte{1, 2, 3}, got.Media.Handle)
	require.Equal(t, "data:image/jpeg;base64,AA", got.Media.Thumb)
	require.Equal(t, "a.jpg", got.Media.FileName)
	require.Equal(t, int64(10), got.Media.Size)

	// Y tampoco si el mensaje vuelve sin medio.
	noMedia := m
	noMedia.Media = nil
	_, _ = s.UpsertMessage(bg, noMedia)
	got, _ = s.GetMessage(bg, "c", "m1")
	require.NotNil(t, got.Media)
	require.Equal(t, `C:\datos\med1.jpg`, got.Media.LocalPath)

	md, chat, err := s.GetMedia(bg, "med1")
	require.NoError(t, err)
	require.Equal(t, "c", chat)
	require.Equal(t, `C:\datos\med1.jpg`, md.LocalPath)
	require.Equal(t, "/media/wa-test/med1", md.URL)
	require.Equal(t, []byte{1, 2, 3}, md.Handle)
	_, _, err = s.GetMedia(bg, "nada")
	require.ErrorIs(t, err, port.ErrNotFound)

	// Medio sin id: se genera uno estable.
	n := mkMsg("c", "m2", "", 200, false)
	n.Kind = model.KindDocument
	n.Media = &model.Media{FileName: "doc.pdf"}
	_, err = s.UpsertMessage(bg, n)
	require.NoError(t, err)
	_, err = s.UpsertMessage(bg, n)
	require.NoError(t, err)
	got, _ = s.GetMessage(bg, "c", "m2")
	require.NotEmpty(t, got.Media.ID)
	var cnt int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&cnt))
	require.Equal(t, 2, cnt)
}

func TestApplyUpdate(t *testing.T) {
	s := newTestAccount(t)
	poll := mkMsg("c", "p1", "", 150, false)
	poll.Kind = model.KindPoll
	poll.Poll = &model.Poll{Question: "¿Pizza?", Options: []model.PollOption{{Name: "Sí"}, {Name: "No"}}}
	_, _ = s.UpsertMessage(bg, mkMsg("c", "m1", "texto original", 100, false))
	_, _ = s.UpsertMessage(bg, poll)
	_, _ = s.UpsertMessage(bg, mkMsg("c", "m2", "el último", 200, false))

	// Editar el último actualiza su vista previa; la búsqueda sigue al texto nuevo.
	txt := "el último editado"
	got, err := s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "m2"), Text: &txt, EditedAt: 250})
	require.NoError(t, err)
	require.Equal(t, txt, got.Text)
	require.Equal(t, int64(250), got.EditedAt)
	c, _ := s.GetChat(bg, "c")
	require.Equal(t, txt, c.LastPreview)
	res, _ := s.SearchMessages(bg, "editado", "", 10)
	require.Len(t, res, 1)
	res, _ = s.SearchMessages(bg, "último", "", 10)
	require.Len(t, res, 1)

	// Editar uno que no es el último no toca la vista previa.
	txt1 := "primero editado"
	_, err = s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "m1"), Text: &txt1})
	require.NoError(t, err)
	c, _ = s.GetChat(bg, "c")
	require.Equal(t, "el último editado", c.LastPreview)
	m1, _ := s.GetMessage(bg, "c", "m1")
	require.NotZero(t, m1.EditedAt) // sin hora, se pone la actual

	// Una repetición vieja del mensaje no deshace la edición.
	_, _ = s.UpsertMessage(bg, mkMsg("c", "m2", "el último", 200, false))
	got, _ = s.GetMessage(bg, "c", "m2")
	require.Equal(t, txt, got.Text)
	require.Equal(t, int64(250), got.EditedAt)

	// Votos de la encuesta; no los pisa una repetición del mensaje original.
	votes := &model.Poll{Question: "¿Pizza?", Options: []model.PollOption{{Name: "Sí", Voters: []string{"a@s", "b@s"}}, {Name: "No"}}}
	got, err = s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "p1"), Poll: votes})
	require.NoError(t, err)
	require.Equal(t, []string{"a@s", "b@s"}, got.Poll.Options[0].Voters)
	_, _ = s.UpsertMessage(bg, poll)
	got, _ = s.GetMessage(bg, "c", "p1")
	require.Len(t, got.Poll.Options[0].Voters, 2)

	// Borrado: vista previa, FTS y marca.
	got, err = s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "m2"), Deleted: true})
	require.NoError(t, err)
	require.NotZero(t, got.DeletedAt)
	c, _ = s.GetChat(bg, "c")
	require.Equal(t, "Mensaje eliminado", c.LastPreview)
	res, _ = s.SearchMessages(bg, "último", "", 10)
	require.Empty(t, res)
	// Un reenvío del mensaje no lo "des-borra".
	_, _ = s.UpsertMessage(bg, mkMsg("c", "m2", "el último", 200, false))
	got, _ = s.GetMessage(bg, "c", "m2")
	require.NotZero(t, got.DeletedAt)

	// Descarga de medio: se guarda la ruta local y se crea el medio si no estaba.
	img := mkMsg("c", "i1", "", 300, false)
	img.Kind = model.KindImage
	img.Media = &model.Media{ID: "mi", Mime: "image/png"}
	_, _ = s.UpsertMessage(bg, img)
	got, err = s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "i1"), Media: &model.Media{ID: "mi", LocalPath: "/tmp/mi.png", Size: 99}})
	require.NoError(t, err)
	require.Equal(t, "/tmp/mi.png", got.Media.LocalPath)
	require.Equal(t, int64(99), got.Media.Size)
	require.Equal(t, "image/png", got.Media.Mime)
	// Sin id en el medio nuevo, usa el del mensaje.
	got, err = s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "i1"), Media: &model.Media{Width: 640}})
	require.NoError(t, err)
	require.Equal(t, "mi", got.Media.ID)
	require.Equal(t, 640, got.Media.Width)
	require.Equal(t, "/tmp/mi.png", got.Media.LocalPath)

	_, err = s.ApplyUpdate(bg, model.MessageUpdate{Key: mkey("c", "zz"), Deleted: true})
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.ApplyUpdate(bg, model.MessageUpdate{Key: "mala"})
	require.ErrorIs(t, err, port.ErrInvalid)
}

func TestListMessagesPaging(t *testing.T) {
	s := newTestAccount(t)
	var batch []model.Message
	for i := 1; i <= 25; i++ {
		batch = append(batch, mkMsg("c", fmt.Sprintf("m%02d", i), fmt.Sprintf("mensaje %d", i), int64(i*10), i%2 == 0))
	}
	batch = append(batch, mkMsg("otro", "x", "de otro chat", 5, false))
	_, err := s.UpsertMessages(bg, batch)
	require.NoError(t, err)

	p, err := s.ListMessages(bg, "c", 0, 10)
	require.NoError(t, err)
	require.Len(t, p.Messages, 10)
	require.True(t, p.HasMore)
	require.Equal(t, "m16", p.Messages[0].NativeID) // de más antiguo a más reciente
	require.Equal(t, "m25", p.Messages[9].NativeID)

	p, _ = s.ListMessages(bg, "c", p.Messages[0].Timestamp, 10)
	require.Equal(t, "m06", p.Messages[0].NativeID)
	require.Equal(t, "m15", p.Messages[9].NativeID)
	require.True(t, p.HasMore)
	p, _ = s.ListMessages(bg, "c", p.Messages[0].Timestamp, 10)
	require.Len(t, p.Messages, 5)
	require.False(t, p.HasMore)
	require.Equal(t, "m01", p.Messages[0].NativeID)

	p, _ = s.ListMessages(bg, "c", 0, 0) // límite por defecto (50)
	require.Len(t, p.Messages, 25)
	require.False(t, p.HasMore)
	p, _ = s.ListMessages(bg, "vacío", 0, 10)
	require.NotNil(t, p.Messages)
	require.Empty(t, p.Messages)
	require.False(t, p.HasMore)
}

func TestSearchMessagesFTS(t *testing.T) {
	s := newTestAccount(t)
	for i, tx := range []struct{ chat, text string }{
		{"c1", "Quiero una cotización de las lámparas"},
		{"c1", "Gracias, ya está todo"},
		{"c2", "La COTIZACION llegó ayer"},
		{"c2", "nada que ver"},
		{"c2", `comillas "raras" y (paréntesis) OR AND *`},
	} {
		_, err := s.UpsertMessage(bg, mkMsg(tx.chat, fmt.Sprintf("m%d", i), tx.text, int64(100+i), false))
		require.NoError(t, err)
	}
	// Mensaje sin texto: no entra al índice.
	img := mkMsg("c1", "img", "", 500, false)
	img.Kind = model.KindImage
	_, _ = s.UpsertMessage(bg, img)
	require.Equal(t, 5, ftsCount(t, s))

	texts := func(ms []model.Message) []string {
		out := []string{}
		for _, m := range ms {
			out = append(out, m.NativeID)
		}
		return out
	}
	// Sin distinguir mayúsculas ni tildes; los más recientes primero.
	r, err := s.SearchMessages(bg, "cotizacion", "", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"m2", "m0"}, texts(r))
	require.Equal(t, "c2", r[0].ChatNativeID)
	r, _ = s.SearchMessages(bg, "COTIZACIÓN", "c1", 10)
	require.Equal(t, []string{"m0"}, texts(r))
	// Varias palabras (Y) y prefijo de la última.
	r, _ = s.SearchMessages(bg, "quiero lamp", "", 10)
	require.Equal(t, []string{"m0"}, texts(r))
	r, _ = s.SearchMessages(bg, "quiero cotizacion ayer", "", 10)
	require.Empty(t, r)
	// Límite.
	r, _ = s.SearchMessages(bg, "cotiz", "", 1)
	require.Len(t, r, 1)
	// Consultas con símbolos de FTS5 no fallan.
	for _, q := range []string{`"`, `raras"`, `(paréntesis`, `OR`, `AND *`, `*`, `NEAR(`, `a:b`, `-x`, "   ", ""} {
		_, err := s.SearchMessages(bg, q, "", 10)
		require.NoError(t, err, q)
	}
	r, _ = s.SearchMessages(bg, `"raras"`, "", 10)
	require.Equal(t, []string{"m4"}, texts(r))
	// Trae el mensaje completo (con reacciones).
	_, _ = s.ApplyReaction(bg, model.ReactionEvent{MessageKey: mkey("c2", "m2"), Reaction: model.Reaction{SenderNativeID: "x", Emoji: "👍", At: 1}})
	r, _ = s.SearchMessages(bg, "llegó", "", 10)
	require.Len(t, r, 1)
	require.Len(t, r[0].Reactions, 1)

	// Borrar un chat quita sus mensajes del índice.
	require.NoError(t, s.DeleteChat(bg, "c2"))
	r, _ = s.SearchMessages(bg, "cotizacion", "", 10)
	require.Equal(t, []string{"m0"}, texts(r))
	require.Equal(t, 2, ftsCount(t, s))
}

func TestMessageStats(t *testing.T) {
	s := newTestAccount(t)
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: "g", Kind: model.ChatGroup, Title: "Grupo"}))
	_, _ = s.UpsertMessage(bg, mkMsg("g", "a", "hola", 100, false))
	_, _ = s.UpsertMessage(bg, mkMsg("g", "b", "ñandú ✓", 200, true))
	img := mkMsg("d", "c", "", 300, false)
	img.Kind = model.KindImage
	_, _ = s.UpsertMessage(bg, img)
	_, _ = s.UpsertMessage(bg, mkMsg("d", "e", "tarde", 900, false))

	rows, err := s.MessageStats(bg, 100, 300, nil)
	require.NoError(t, err)
	require.Equal(t, []port.StatRow{
		{ChatNativeID: "g", ChatKind: model.ChatGroup, SenderID: "s-a", FromMe: false, Timestamp: 100, Kind: model.KindText, TextLen: 4},
		{ChatNativeID: "g", ChatKind: model.ChatGroup, SenderID: "s-b", FromMe: true, Timestamp: 200, Kind: model.KindText, TextLen: 7},
		{ChatNativeID: "d", ChatKind: model.ChatDirect, SenderID: "s-c", FromMe: false, Timestamp: 300, Kind: model.KindImage, TextLen: 0},
	}, rows)
	rows, _ = s.MessageStats(bg, 0, 0, []string{"d"})
	require.Len(t, rows, 2)
	rows, _ = s.MessageStats(bg, 400, 0, nil)
	require.Len(t, rows, 1)
	rows, _ = s.MessageStats(bg, 1000, 2000, nil)
	require.NotNil(t, rows)
	require.Empty(t, rows)
}

func TestDeleteChatRemovesEverything(t *testing.T) {
	s := newTestAccount(t)
	img := mkMsg("c", "m1", "foto de gato", 100, false)
	img.Kind = model.KindImage
	img.Media = &model.Media{ID: "med"}
	_, _ = s.UpsertMessage(bg, img)
	_, _ = s.UpsertMessage(bg, mkMsg("keep", "k1", "otro chat", 100, false))
	_, _ = s.ApplyReaction(bg, model.ReactionEvent{MessageKey: mkey("c", "m1"), Reaction: model.Reaction{SenderNativeID: "x", Emoji: "👍"}})

	require.NoError(t, s.DeleteChat(bg, "c"))
	_, err := s.GetChat(bg, "c")
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.GetMessage(bg, "c", "m1")
	require.ErrorIs(t, err, port.ErrNotFound)
	_, _, err = s.GetMedia(bg, "med")
	require.ErrorIs(t, err, port.ErrNotFound)
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM reactions`).Scan(&n))
	require.Equal(t, 0, n)
	require.Equal(t, 1, ftsCount(t, s))
	_, err = s.GetMessage(bg, "keep", "k1")
	require.NoError(t, err)
	require.NoError(t, s.DeleteChat(bg, "no-existe"))
}
