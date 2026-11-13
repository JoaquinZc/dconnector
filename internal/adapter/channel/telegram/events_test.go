package telegram

import (
	"strconv"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

func TestOnMessageEmitsPersonsAndMessage(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.onMessage(bg, testEntities(), privMsg(5, "hola"))

	// Las personas nuevas se anuncian antes del mensaje.
	persons := sink.ofType(model.EvPerson)
	require.NotEmpty(t, persons)
	var ana *model.Person
	for _, p := range persons {
		if p.Person.NativeID == "user:100" {
			ana = p.Person
		}
	}
	require.NotNil(t, ana)
	assert.Equal(t, "Ana Pérez", ana.Name)
	assert.Equal(t, "+593991234567", ana.Phone)

	msgs := sink.ofType(model.EvMessageNew)
	require.Len(t, msgs, 1)
	m := msgs[0].Message
	assert.Equal(t, "hola", m.Text)
	assert.Equal(t, model.NewMessageKey(testAcc, "user:100", "5"), m.Key)
	assert.Equal(t, m.ChatKey, msgs[0].ChatKey)
	assert.Equal(t, int64(1700000000000), msgs[0].At)
	assert.Equal(t, testAcc, msgs[0].AccountID)

	// El índice de chats permite resolver un borrado posterior; la vista previa de cita queda recordada.
	assert.Equal(t, "user:100", a.local.chatOfMessage(bg, 5))
	pv, snd, ok := a.local.quoteLookup("user:100", 5)
	assert.True(t, ok)
	assert.Equal(t, "hola", pv)
	assert.Equal(t, "Ana Pérez", snd)

	// Sin sincronizar no se anuncian chats; la misma persona no se vuelve a anunciar.
	assert.Empty(t, sink.ofType(model.EvChatUpsert))
	sink.reset()
	a.onMessage(bg, testEntities(), privMsg(6, "otra"))
	assert.Empty(t, sink.ofType(model.EvPerson))
	assert.Len(t, sink.ofType(model.EvMessageNew), 1)
}

func TestOnMessageService(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.onMessage(bg, testEntities(), svc(&tg.MessageActionChatEditTitle{Title: "Casa"}, &tg.PeerUser{UserID: otherUID}))
	msgs := sink.ofType(model.EvMessageNew)
	require.Len(t, msgs, 1)
	assert.Equal(t, model.KindSystem, msgs[0].Message.Kind)
	assert.Equal(t, "chat:7", msgs[0].Message.ChatNativeID)

	sink.reset()
	a.onMessage(bg, testEntities(), svc(&tg.MessageActionHistoryClear{}, nil))
	assert.Empty(t, sink.all(), "las acciones que no se muestran se omiten")
	a.onMessage(bg, testEntities(), &tg.MessageEmpty{ID: 3})
	assert.Empty(t, sink.ofType(model.EvMessageNew))
}

func TestAnnounceChatAfterSync(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.local.markSynced()
	a.onMessage(bg, testEntities(), privMsg(5, "primer mensaje"))
	chats := sink.ofType(model.EvChatUpsert)
	require.Len(t, chats, 1)
	ch := chats[0].Chat
	assert.Equal(t, "user:100", ch.NativeID)
	assert.Equal(t, model.ChatDirect, ch.Kind)
	assert.Equal(t, "Ana Pérez", ch.Title)
	assert.Equal(t, "user:100", ch.PeerNativeID)
	assert.Equal(t, -1, ch.UnreadCount, "no se sabe cuántos no leídos hay")
	assert.Equal(t, int64(1700000000000), ch.LastMessageAt)
	assert.Equal(t, "primer mensaje", ch.LastPreview)
	assert.False(t, ch.LastFromMe)
	assert.False(t, ch.Archived || ch.Pinned)
	assert.Zero(t, ch.MutedUntil)

	// Solo la primera vez.
	sink.reset()
	a.onMessage(bg, testEntities(), privMsg(6, "segundo"))
	assert.Empty(t, sink.ofType(model.EvChatUpsert))

	// Un grupo y un canal de difusión de solo lectura.
	a.onMessage(bg, testEntities(), &tg.Message{ID: 7, PeerID: &tg.PeerChat{ChatID: 7}, FromID: &tg.PeerUser{UserID: 101}, Date: 5, Message: "g"})
	a.onMessage(bg, testEntities(), &tg.Message{ID: 8, Post: true, PeerID: &tg.PeerChannel{ChannelID: 20}, Date: 6, Message: "n"})
	byID := map[string]*model.Chat{}
	for _, e := range sink.ofType(model.EvChatUpsert) {
		byID[e.Chat.NativeID] = e.Chat
	}
	require.Contains(t, byID, "chat:7")
	assert.Equal(t, model.ChatGroup, byID["chat:7"].Kind)
	assert.Equal(t, "Familia", byID["chat:7"].Title)
	require.Contains(t, byID, "channel:20")
	assert.Equal(t, model.ChatChannel, byID["channel:20"].Kind)
	assert.True(t, byID["channel:20"].ReadOnly)
}

func TestChatShell(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	v := entitiesView{testEntities()}
	self, ok := a.chatShell("user:1", v)
	require.True(t, ok)
	assert.Equal(t, model.ChatSelf, self.Kind)
	assert.Equal(t, "Mensajes guardados", self.Title)
	assert.Equal(t, "user:1", self.PeerNativeID)

	mega, _ := a.chatShell("channel:30", v)
	assert.Equal(t, model.ChatGroup, mega.Kind)
	assert.False(t, mega.ReadOnly)

	unknown, ok := a.chatShell("user:555", v)
	assert.False(t, ok, "sin título no se emite")
	assert.Equal(t, "user:555", unknown.NativeID)

	_, ok = a.chatShell("raro", v)
	assert.False(t, ok)

	// Las marcas conocidas van siempre completas.
	a.local.setMarks("chat:7", chatMarks{Archived: true, Pinned: true, Muted: -1})
	g, _ := a.chatShell("chat:7", v)
	assert.True(t, g.Archived && g.Pinned)
	assert.Equal(t, int64(-1), g.MutedUntil)
}

func TestOnEdit(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	m := privMsg(5, "texto corregido")
	m.EditDate = 1700000100
	a.onEdit(bg, testEntities(), m)
	ups := sink.ofType(model.EvMessageUpdate)
	require.Len(t, ups, 1)
	u := ups[0].Update
	assert.Equal(t, model.NewMessageKey(testAcc, "user:100", "5"), u.Key)
	require.NotNil(t, u.Text)
	assert.Equal(t, "texto corregido", *u.Text)
	assert.Equal(t, int64(1700000100000), u.EditedAt)
	assert.False(t, u.Deleted)

	// Sin fecha de edición (p. ej. se cargó una vista previa): se usa la hora actual.
	sink.reset()
	a.onEdit(bg, testEntities(), privMsg(6, "x"))
	got := sink.ofType(model.EvMessageUpdate)[0].Update
	assert.InDelta(t, time.Now().UnixMilli(), got.EditedAt, 5000)
	a.onEdit(bg, testEntities(), &tg.MessageEmpty{})
	assert.Len(t, sink.ofType(model.EvMessageUpdate), 1)
}

func TestOnDelete(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	createMessagesTable(t, a.deps.DB)
	// Mensaje visto en vivo: el índice kv resuelve el chat.
	a.onMessage(bg, testEntities(), privMsg(5, "borrame"))
	// Mensaje que solo está en la tabla messages (visto antes de existir el índice).
	insertMessage(t, a.deps.DB, "chat:7", "9", 0, "text", "viejo", "sent")
	sink.reset()

	a.onDelete(bg, "", []int{5, 9, 77})
	ups := sink.ofType(model.EvMessageUpdate)
	require.Len(t, ups, 2, "el 77 no se conoce y se omite")
	assert.Equal(t, model.NewMessageKey(testAcc, "user:100", "5"), ups[0].Update.Key)
	assert.True(t, ups[0].Update.Deleted)
	assert.Equal(t, model.NewMessageKey(testAcc, "chat:7", "9"), ups[1].Update.Key)

	// En canales el aviso trae el canal.
	sink.reset()
	a.onDelete(bg, "channel:20", []int{3, 4})
	ups = sink.ofType(model.EvMessageUpdate)
	require.Len(t, ups, 2)
	assert.Equal(t, model.NewMessageKey(testAcc, "channel:20", "3"), ups[0].Update.Key)
	assert.Equal(t, model.NewChatKey(testAcc, "channel:20"), ups[0].ChatKey)
}

func TestIndexMsgChatsSkipsChannels(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	a.local.indexMsgChats(bg, []model.Message{
		{ChatNativeID: "channel:20", NativeID: "3"}, {ChatNativeID: "chat:7", NativeID: "4"}, {ChatNativeID: "", NativeID: "5"},
	})
	_, ok, _ := a.kv.get(bg, prefMsgChat+"3")
	assert.False(t, ok, "los ids de canal son por canal")
	v, ok, _ := a.kv.get(bg, prefMsgChat+"4")
	assert.True(t, ok)
	assert.Equal(t, "chat:7", string(v))
	assert.Equal(t, "", a.local.chatOfMessage(bg, 5))
}

func TestOnReadOutbox(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	createMessagesTable(t, a.deps.DB)
	for i := 1; i <= 5; i++ {
		insertMessage(t, a.deps.DB, "user:100", strconv.Itoa(i), 1, "text", "m", "sent")
	}
	insertMessage(t, a.deps.DB, "user:100", "6", 0, "text", "ajeno", "sent")                // entrante: no cuenta
	_, err := a.deps.DB.Exec("UPDATE messages SET delivery = 'read' WHERE native_id = '2'") // ya leído
	require.NoError(t, err)
	insertMessage(t, a.deps.DB, "user:101", "1", 1, "text", "otro chat", "sent")

	a.onReadOutbox(bg, "user:100", 4)
	rs := sink.ofType(model.EvReceipt)
	require.Len(t, rs, 1)
	r := rs[0].Receipt
	assert.Equal(t, model.DeliveryRead, r.Delivery)
	assert.Equal(t, model.NewChatKey(testAcc, "user:100"), r.ChatKey)
	assert.Equal(t, "user:100", r.ReaderID)
	var ids []string
	for _, k := range r.MessageKeys {
		_, _, id := k.Split()
		ids = append(ids, id)
	}
	assert.ElementsMatch(t, []string{"1", "3", "4"}, ids)

	sink.reset()
	a.onReadOutbox(bg, "user:100", 0)
	a.onReadOutbox(bg, "", 4)
	a.onReadOutbox(bg, "user:555", 4)
	assert.Empty(t, sink.all())
}

func TestOnTyping(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.onTyping("user:100", "user:100", &tg.SendMessageTypingAction{})
	ty := sink.ofType(model.EvTyping)
	require.Len(t, ty, 1)
	assert.True(t, ty[0].Typing.Typing)
	assert.False(t, ty[0].Typing.Recording)
	assert.Equal(t, "user:100", ty[0].Typing.PeerID)
	assert.Equal(t, model.NewChatKey(testAcc, "user:100"), ty[0].Typing.ChatKey)
	a.typingMu.Lock()
	assert.Len(t, a.typingTimers, 1, "queda programado el fin de «escribiendo…»")
	a.typingMu.Unlock()

	// Grabando una nota de voz en un grupo.
	a.onTyping("chat:7", "user:101", &tg.SendMessageRecordAudioAction{})
	assert.True(t, sink.ofType(model.EvTyping)[1].Typing.Recording)

	// Cancelar quita el temporizador y avisa que dejó de escribir.
	a.onTyping("user:100", "user:100", &tg.SendMessageCancelAction{})
	last := sink.ofType(model.EvTyping)[2].Typing
	assert.False(t, last.Typing || last.Recording)
	a.typingMu.Lock()
	assert.Len(t, a.typingTimers, 1)
	a.typingMu.Unlock()

	// Mi propio «escribiendo» y los datos vacíos se ignoran.
	n := len(sink.ofType(model.EvTyping))
	a.onTyping("chat:7", "user:1", &tg.SendMessageTypingAction{})
	a.onTyping("", "user:5", &tg.SendMessageTypingAction{})
	assert.Len(t, sink.ofType(model.EvTyping), n)
}

func TestOnReactions(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.onReactions(testEntities(), &tg.UpdateMessageReactions{
		Peer: &tg.PeerUser{UserID: otherUID}, MsgID: 12,
		Reactions: tg.MessageReactions{RecentReactions: []tg.MessagePeerReaction{
			{PeerID: &tg.PeerUser{UserID: otherUID}, Date: 100, Reaction: &tg.ReactionEmoji{Emoticon: "🔥"}},
			{PeerID: &tg.PeerUser{UserID: 101}, Date: 101, Reaction: &tg.ReactionEmoji{Emoticon: "👍"}},
		}},
	})
	rs := sink.ofType(model.EvReaction)
	require.Len(t, rs, 2)
	assert.Equal(t, model.NewMessageKey(testAcc, "user:100", "12"), rs[0].Reaction.MessageKey)
	assert.Equal(t, "🔥", rs[0].Reaction.Reaction.Emoji)
	assert.Equal(t, "Ana Pérez", rs[0].Reaction.Reaction.SenderName)
	assert.Equal(t, int64(100000), rs[0].Reaction.Reaction.At)
	assert.Equal(t, "user:101", rs[1].Reaction.Reaction.SenderNativeID)
}

func TestOnPoll(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	// Una encuesta vista antes: sus opciones están guardadas.
	m := privMsg(40, "")
	m.Media = &tg.MessageMediaPoll{Poll: samplePoll()}
	a.onMessage(bg, testEntities(), m)
	sink.reset()

	results := tg.PollResults{Results: []tg.PollAnswerVoters{{Option: []byte("1"), Voters: 2, RecentVoters: []tg.PeerClass{&tg.PeerUser{UserID: 101}}}}}
	// Con el chat y el mensaje en la actualización.
	a.onPoll(bg, testEntities(), &tg.UpdateMessagePoll{Peer: &tg.PeerUser{UserID: otherUID}, MsgID: 40, PollID: 900, Results: results})
	ups := sink.ofType(model.EvMessageUpdate)
	require.Len(t, ups, 1)
	assert.Equal(t, model.NewMessageKey(testAcc, "user:100", "40"), ups[0].Update.Key)
	p := ups[0].Update.Poll
	require.NotNil(t, p)
	assert.Equal(t, []string{"user:101", "anon"}, p.Options[1].Voters)
	assert.Equal(t, "Hoy", p.Options[0].Name)

	// Sin chat ni mensaje: se busca por el id de la encuesta.
	sink.reset()
	a.onPoll(bg, testEntities(), &tg.UpdateMessagePoll{PollID: 900, Results: results})
	require.Len(t, sink.ofType(model.EvMessageUpdate), 1)

	// Encuesta desconocida: no se emite nada.
	sink.reset()
	a.onPoll(bg, testEntities(), &tg.UpdateMessagePoll{PollID: 12345, Results: results})
	assert.Empty(t, sink.all())
}

func TestOnStory(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	it := &tg.StoryItem{ID: 15, Date: 100, ExpireDate: 200, Caption: "hola", Media: &tg.MessageMediaPhoto{Photo: samplePhoto()}}
	a.onStory(testEntities(), &tg.UpdateStory{Peer: &tg.PeerUser{UserID: otherUID}, Story: it})
	sts := sink.ofType(model.EvStatusUpsert)
	require.Len(t, sts, 1)
	assert.Equal(t, "user:100/15", sts[0].Status.NativeID)
	assert.Equal(t, "hola", sts[0].Status.Text)
	assert.Equal(t, int64(100000), sts[0].At)

	a.onStory(testEntities(), &tg.UpdateStory{Peer: &tg.PeerUser{UserID: otherUID}, Story: &tg.StoryItemDeleted{ID: 15}})
	sts = sink.ofType(model.EvStatusUpsert)
	require.Len(t, sts, 2)
	del := sts[1].Status
	assert.Equal(t, "tg-test|user:100/15", del.Key)
	assert.NotZero(t, del.DeletedAt)
	assert.Equal(t, "user:100", del.AuthorNativeID)
	assert.Nil(t, del.Media)
}

func TestOnMarks(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	// Chat aún no sincronizado: no se emite nada (la sincronización traerá las marcas).
	a.onMarks(bg, testEntities(), "chat:7", func(m *chatMarks) { m.Pinned = true })
	assert.Empty(t, sink.ofType(model.EvChatUpsert))

	a.local.setMarks("chat:7", chatMarks{})
	a.onMarks(bg, testEntities(), "chat:7", func(m *chatMarks) { m.Pinned = true })
	a.onMarks(bg, testEntities(), "chat:7", func(m *chatMarks) { m.Muted = 5000 })
	chats := sink.ofType(model.EvChatUpsert)
	require.Len(t, chats, 2)
	assert.True(t, chats[0].Chat.Pinned)
	assert.Equal(t, "Familia", chats[0].Chat.Title)
	assert.Equal(t, -1, chats[0].Chat.UnreadCount)
	assert.True(t, chats[1].Chat.Pinned, "las marcas anteriores se conservan")
	assert.Equal(t, int64(5000), chats[1].Chat.MutedUntil)
}

func TestMuteMs(t *testing.T) {
	now := time.Unix(1700000000, 0)
	assert.Zero(t, muteMs(0, now))
	assert.Zero(t, muteMs(-5, now))
	assert.Zero(t, muteMs(1699999999, now), "ya venció")
	assert.Equal(t, int64(1700003600000), muteMs(1700003600, now))
	assert.Equal(t, int64(-1), muteMs(2147483647, now), "«siempre»")
	assert.Equal(t, int64(-1), muteMs(2100000000, now), "más de 10 años = siempre")
}

func TestDialogChat(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	e := testEntities()
	c := converter{acc: testAcc, selfID: selfUID, view: entitiesView{e}}
	now := time.Unix(1700000500, 0)

	dlg := &tg.Dialog{Peer: &tg.PeerUser{UserID: otherUID}, Pinned: true, UnreadCount: 3, FolderID: 1}
	dlg.NotifySettings.MuteUntil = 1700003600
	last := privMsg(50, "último mensaje")
	ch, msgs, ok := a.dialogChat(c, dlg, last, now)
	require.True(t, ok)
	assert.Equal(t, "user:100", ch.NativeID)
	assert.Equal(t, "Ana Pérez", ch.Title)
	assert.Equal(t, 3, ch.UnreadCount)
	assert.True(t, ch.Pinned)
	assert.True(t, ch.Archived)
	assert.Equal(t, int64(1700003600000), ch.MutedUntil)
	assert.Equal(t, int64(1700000000000), ch.LastMessageAt)
	assert.Equal(t, "último mensaje", ch.LastPreview)
	require.Len(t, msgs, 1)
	assert.Equal(t, "50", msgs[0].NativeID)
	marks, known := a.local.marksOf("user:100")
	assert.True(t, known)
	assert.Equal(t, chatMarks{Archived: true, Pinned: true, Muted: 1700003600000}, marks)

	// Marcado como no leído sin contador.
	dlg2 := &tg.Dialog{Peer: &tg.PeerChat{ChatID: 7}, UnreadMark: true}
	ch2, _, ok := a.dialogChat(c, dlg2, nil, now)
	require.True(t, ok)
	assert.Equal(t, 1, ch2.UnreadCount)
	assert.Zero(t, ch2.LastMessageAt)

	// Último mensaje de servicio y mío.
	dlg3 := &tg.Dialog{Peer: &tg.PeerChat{ChatID: 7}}
	ch3, msgs3, _ := a.dialogChat(c, dlg3, svc(&tg.MessageActionChatEditTitle{Title: "X"}, &tg.PeerUser{UserID: otherUID}), now)
	require.Len(t, msgs3, 1)
	assert.Contains(t, ch3.LastPreview, "cambió el nombre")
	mine := &tg.Message{ID: 51, Out: true, PeerID: &tg.PeerUser{UserID: otherUID}, Date: 1700000400, Message: "yo"}
	ch4, _, _ := a.dialogChat(c, &tg.Dialog{Peer: &tg.PeerUser{UserID: otherUID}}, mine, now)
	assert.True(t, ch4.LastFromMe)

	// Par sin título conocido: se emite igual con un nombre genérico.
	ch5, _, ok := a.dialogChat(c, &tg.Dialog{Peer: &tg.PeerUser{UserID: 999}}, nil, now)
	require.True(t, ok)
	assert.Equal(t, "Chat de Telegram", ch5.Title)
	_, _, ok = a.dialogChat(c, &tg.Dialog{}, nil, now)
	assert.False(t, ok)
}

func TestFeedEntitiesAnnouncesChangedPersons(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.feedEntities(bg, tg.Entities{Users: map[int64]*tg.User{5: {ID: 5, FirstName: "Eva", AccessHash: 9}}})
	require.Len(t, sink.ofType(model.EvPerson), 1)
	// Mismo usuario sin cambios: nada.
	a.feedEntities(bg, tg.Entities{Users: map[int64]*tg.User{5: {ID: 5, FirstName: "Eva", AccessHash: 9}}})
	assert.Len(t, sink.ofType(model.EvPerson), 1)
	// Cambia el nombre.
	a.feedEntities(bg, tg.Entities{Users: map[int64]*tg.User{5: {ID: 5, FirstName: "Eva", LastName: "Ríos", AccessHash: 9}}})
	ps := sink.ofType(model.EvPerson)
	require.Len(t, ps, 2)
	assert.Equal(t, "Eva Ríos", ps[1].Person.PushName)
	// Un usuario «mínimo» (sin datos completos) no se anuncia.
	a.feedEntities(bg, tg.Entities{Users: map[int64]*tg.User{6: {ID: 6, FirstName: "Min", Min: true}}})
	assert.Len(t, sink.ofType(model.EvPerson), 2)
}

func TestOnUserName(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.feedEntities(bg, tg.Entities{Users: map[int64]*tg.User{5: {ID: 5, FirstName: "Eva", AccessHash: 9}}})
	sink.reset()
	a.onUserName(bg, &tg.UpdateUserName{UserID: 5, FirstName: "Eva", LastName: "Luna", Usernames: []tg.Username{{Username: "evaluna", Active: true}}})
	ps := sink.ofType(model.EvPerson)
	require.Len(t, ps, 1)
	assert.Equal(t, "Eva Luna", ps[0].Person.PushName)
	assert.Equal(t, "evaluna", ps[0].Person.Username)
	// Un usuario desconocido no hace nada.
	sink.reset()
	a.onUserName(bg, &tg.UpdateUserName{UserID: 77, FirstName: "X"})
	assert.Empty(t, sink.all())
}

func TestAnnounceAvatar(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.peers.feed(bg, testEntities())
	a.local.setMarks("user:100", chatMarks{Pinned: true})
	a.announceAvatar("user:100", "abc-1.jpg")
	chats := sink.ofType(model.EvChatUpsert)
	require.Len(t, chats, 1)
	assert.Equal(t, "/avatar/tg-test/abc-1.jpg", chats[0].Chat.AvatarURL)
	assert.True(t, chats[0].Chat.Pinned, "las marcas van completas")
	ps := sink.ofType(model.EvPerson)
	require.Len(t, ps, 1)
	assert.Equal(t, "/avatar/tg-test/abc-1.jpg", ps[0].Person.AvatarURL)
	assert.Equal(t, "", a.avatarURL(""))
	assert.Equal(t, 16+1+len("5.jpg"), len(avatarName("user:100", 5)))
}

func TestPhotoIDOf(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	e := testEntities()
	e.Users[otherUID].Photo = &tg.UserProfilePhoto{PhotoID: 77}
	e.Chats[7].Photo = &tg.ChatPhoto{PhotoID: 88}
	e.Channels[20].Photo = &tg.ChatPhoto{PhotoID: 99}
	a.peers.feed(bg, e)
	assert.Equal(t, int64(77), a.photoIDOf("user:100"))
	assert.Equal(t, int64(88), a.photoIDOf("chat:7"))
	assert.Equal(t, int64(99), a.photoIDOf("channel:20"))
	assert.Zero(t, a.photoIDOf("user:101"))
	assert.Zero(t, a.photoIDOf("basura"))
}
