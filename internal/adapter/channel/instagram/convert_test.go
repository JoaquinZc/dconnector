package instagram

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
	"go.mau.fi/util/jsontime"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
)

const acc = model.AccountID("ig-test")

func ms(v int64) jsontime.UnixMilliString { return jsontime.UnixMilliString{Time: time.UnixMilli(v)} }

func newConv() *Converter {
	c := NewConverter(acc, 100)
	c.Now = func() time.Time { return time.UnixMilli(5_000_000) }
	return c
}

func boolp(b bool) *bool { return &b }

func dmThread() *slidetypes.ThreadInfo {
	return &slidetypes.ThreadInfo{
		ID: "thr1", ThreadKey: 200, ThreadFBID: "thr1", ThreadID: "long1", ThreadSubtype: slidetypes.ThreadSubtypeDirect,
		Viewer:                  &slidetypes.User{InteropMessagingUserFBID: 100, Username: "yo", FullName: "Yo"},
		Users:                   []*slidetypes.User{{InteropMessagingUserFBID: 200, Username: "ana", FullName: "Ana Pérez", ID: "pk200", ProfilePicURL: "https://cdn/ana.jpg"}},
		LastActivityTimestampMS: ms(2000),
		SlideMessages: &slidetypes.SlideMessages{Edges: []slidetypes.Node[*slidetypes.Message]{
			{Node: &slidetypes.Message{TimestampMS: ms(1000), IGDSnippet: "viejo"}},
			{Node: &slidetypes.Message{TimestampMS: ms(2000), IGDSnippet: "último mensaje"}},
		}},
		SlideReadReceipts: []slidetypes.ReadReceipt{{ParticipantFBID: 100, WatermarkTimestampMS: ms(2000)}},
		IsMuted:           boolp(true), IsPin: boolp(true),
	}
}

func textMsg(id, body string, from int64, ts int64) *slidetypes.Message {
	return &slidetypes.Message{
		ID: id, MessageID: id, SenderFBID: from, ThreadFBID: "thr1", TimestampMS: ms(ts), TextBody: body,
		Content: slidetypes.MessageContentWrapper{Content: &slidetypes.MessageContentText{TextBody: body}},
	}
}

func TestThreadDirect(t *testing.T) {
	c := NewConverter(acc, 0) // el id propio se aprende del «viewer» del hilo
	r := c.Thread(dmThread())
	assert.Equal(t, int64(100), c.Self())
	require.Len(t, r.Persons, 1)
	assert.Equal(t, "200", r.Persons[0].NativeID)
	assert.Equal(t, "Ana Pérez", r.Persons[0].PushName)
	assert.Equal(t, "ana", r.Persons[0].Username)
	require.Len(t, r.Chats, 1)
	ch := r.Chats[0]
	assert.Equal(t, "thr1", ch.NativeID, "el chat es el thread_fbid")
	assert.Equal(t, model.ChatDirect, ch.Kind)
	assert.Equal(t, "Ana Pérez", ch.Title)
	assert.Equal(t, "200", ch.PeerNativeID)
	assert.Equal(t, int64(2000), ch.LastMessageAt)
	assert.Equal(t, "último mensaje", ch.LastPreview)
	assert.Equal(t, int64(-1), ch.MutedUntil)
	assert.True(t, ch.Pinned)
	assert.Equal(t, 0, ch.UnreadCount, "mi marca alcanza la última actividad")
	assert.Equal(t, "https://cdn/ana.jpg", r.Avatars["thr1"])
	assert.Equal(t, "https://cdn/ana.jpg", r.Avatars["200"])
	assert.Equal(t, "long1", c.LongID("thr1"))
	assert.Equal(t, "thr1", c.ShortOfLong("long1"))
	assert.True(t, c.Known("thr1"))

	// El contador se entrega una sola vez.
	snap, ok := c.ChatSnapshot("thr1")
	require.True(t, ok)
	assert.Equal(t, -1, snap.UnreadCount)
}

func TestThreadGroupSelfAndUnread(t *testing.T) {
	c := newConv()
	r := c.Thread(&slidetypes.ThreadInfo{
		ID: "g1", ThreadID: "longg", IsGroup: true, ThreadTitle: "Equipo", ThreadImageURL: "https://cdn/g.jpg",
		Users:          []*slidetypes.User{{InteropMessagingUserFBID: 200, FullName: "Ana"}, {InteropMessagingUserFBID: 300, Username: "luis"}},
		MarkedAsUnread: true, LastActivityTimestampMS: ms(10),
	})
	require.Len(t, r.Chats, 1)
	assert.Equal(t, model.ChatGroup, r.Chats[0].Kind)
	assert.Equal(t, "Equipo", r.Chats[0].Title)
	assert.Equal(t, 1, r.Chats[0].UnreadCount, "marcado como no leído")
	assert.Equal(t, "https://cdn/g.jpg", r.Avatars["g1"])
	assert.Empty(t, r.Chats[0].PeerNativeID)
	require.Len(t, r.Persons, 2)

	r = c.Thread(&slidetypes.ThreadInfo{ID: "me", ThreadID: "longme", Viewer: &slidetypes.User{InteropMessagingUserFBID: 100}})
	require.Len(t, r.Chats, 1)
	assert.Equal(t, model.ChatSelf, r.Chats[0].Kind)
	assert.Equal(t, "Tú", r.Chats[0].Title)

	assert.Empty(t, c.Thread(nil).Chats)
	assert.Empty(t, c.Thread(&slidetypes.ThreadInfo{}).Chats)
}

func TestAllChatsAndSnapshots(t *testing.T) {
	c := newConv()
	c.Thread(dmThread())
	c.Thread(&slidetypes.ThreadInfo{ID: "g1", IsGroup: true, ThreadTitle: "G", LastActivityTimestampMS: ms(9999)})
	all := c.AllChats()
	require.Len(t, all, 2)
	assert.Equal(t, "g1", all[0].NativeID, "de más reciente a más antiguo")
	p := c.PersonSnapshot(200)
	assert.Equal(t, "Ana Pérez", p.PushName)
	assert.Equal(t, "ana", p.Username)
	c.SetAvatarFile("200", "a.jpg")
	assert.Equal(t, "/avatar/ig-test/a.jpg", c.PersonSnapshot(200).AvatarURL)
	known := c.KnownPersons()
	require.Len(t, known, 1)
	assert.Equal(t, "200", known[0].NativeID)
	assert.Equal(t, "Ana Pérez", c.Name(200))
	assert.Equal(t, "https://cdn/ana.jpg", c.AvatarURLs()["200"])
	_, ok := c.ChatSnapshot("desconocido")
	assert.False(t, ok)
}

func attach(url string, typ int) *slidetypes.Attachment {
	return &slidetypes.Attachment{AttachmentCDNURL: url, AttachmentType: typ, PreviewWidth: 640, PreviewHeight: 480}
}

func TestMessageKinds(t *testing.T) {
	c := newConv()
	base := func(id string, content slidetypes.MessageContent, text string) *slidetypes.Message {
		return &slidetypes.Message{ID: id, SenderFBID: 200, ThreadFBID: "thr1", TimestampMS: ms(100), TextBody: text,
			Content: slidetypes.MessageContentWrapper{Content: content}}
	}

	one := func(m *slidetypes.Message) model.Message {
		t.Helper()
		out := c.Messages(m, "thr1")
		require.Len(t, out, 1, m.ID)
		return out[0]
	}

	m := one(base("m.text", &slidetypes.MessageContentText{TextBody: "hola"}, "hola"))
	assert.Equal(t, model.KindText, m.Kind)
	assert.Equal(t, "hola", m.Text)
	assert.Equal(t, model.NewMessageKey(acc, "thr1", "m.text"), m.Key)
	assert.Equal(t, "200", m.SenderNativeID)
	assert.False(t, m.FromMe)

	m = one(base("m.img", &slidetypes.MessageContentImage{Attachments: []*slidetypes.Attachment{attach("https://cdn/i.jpg", 2)}}, "pie"))
	assert.Equal(t, model.KindImage, m.Kind)
	assert.Equal(t, "pie", m.Text)
	assert.Equal(t, 640, m.Media.Width)
	h, err := meta.ParseHandle(m.Media.Handle)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn/i.jpg", h.URL)
	assert.Equal(t, "image/jpeg", m.Media.Mime)
	assert.Equal(t, meta.MediaID("thr1", "m.img", 0), m.Media.ID)

	m = one(base("m.vid", &slidetypes.MessageContentVideo{Videos: []*slidetypes.Attachment{attach("https://cdn/v.mp4", 4)}}, ""))
	assert.Equal(t, model.KindVideo, m.Kind)
	assert.Equal(t, "video/mp4", m.Media.Mime)

	m = one(base("m.aud", &slidetypes.MessageContentAudio{AudioAttachments: []*slidetypes.AudioAttachment{{AttachmentCDNURL: "https://cdn/a.mp4", PlayableDurationMS: 4200}}}, ""))
	assert.Equal(t, model.KindVoice, m.Kind)
	assert.InDelta(t, 4.2, m.Media.Duration, 0.001)

	m = one(base("m.gif", &slidetypes.MessageContentAnimatedMedia{AnimatedMedia: []*slidetypes.AnimatedAttachment{{AttachmentMP4URL: "https://cdn/g.mp4", PreviewWidth: 200, PreviewHeight: 100}}}, ""))
	assert.Equal(t, model.KindVideo, m.Kind)
	m = one(base("m.stk", &slidetypes.MessageContentAnimatedMedia{AnimatedMedia: []*slidetypes.AnimatedAttachment{{AttachmentWebpURL: "https://cdn/s.webp", IsSticker: true}}}, ""))
	assert.Equal(t, model.KindSticker, m.Kind)
	m = one(base("m.stk2", &slidetypes.MessageContentSticker{PreviewURL: "https://cdn/s2.webp", PreviewWidth: 96, PreviewHeight: 96}, ""))
	assert.Equal(t, model.KindSticker, m.Kind)

	m = one(base("m.xma", &slidetypes.MessageContentXMA{XMATextBody: "mira esto", XMA: &slidetypes.XMAContent{TitleText: "Reel de ana", TargetURL: "https://www.instagram.com/reel/abc/"}}, ""))
	assert.Equal(t, model.KindText, m.Kind)
	assert.Equal(t, "mira esto\nReel de ana\nhttps://www.instagram.com/reel/abc/", m.Text)
	m = one(base("m.xma2", &slidetypes.MessageContentXMA{}, ""))
	assert.Equal(t, model.KindUnsupported, m.Kind)

	m = one(base("m.adm", &slidetypes.MessageContentAdminText{TextFragments: []slidetypes.TextFragment{{Plaintext: "Ana "}, {Plaintext: "creó el grupo"}}}, ""))
	assert.Equal(t, model.KindSystem, m.Kind)
	assert.Equal(t, "Ana creó el grupo", m.Text)
	assert.Empty(t, c.Messages(base("m.log", &slidetypes.MessageContentAdminText{IsReactionActionLog: true, TextFragments: []slidetypes.TextFragment{{Plaintext: "x"}}}, ""), "thr1"))

	m = one(base("m.mus", &slidetypes.MessageContentMusicSticker{}, ""))
	assert.Equal(t, model.KindUnsupported, m.Kind)
	m = one(base("m.unk", slidetypes.MessageContentUnknown{"k": "v"}, ""))
	assert.Equal(t, model.KindUnsupported, m.Kind)
	m = one(base("m.unk2", slidetypes.MessageContentUnknown{"k": "v"}, "con texto"))
	assert.Equal(t, model.KindText, m.Kind)

	// Fotos que se quedan en el chat sí; «ver una vez» o «repetir» no.
	m = one(base("m.raven1", &slidetypes.MessageContentRavenImage{ViewMode: slidetypes.RavenViewModeKeepInChat, Attachment: attach("https://cdn/r.jpg", 0)}, ""))
	assert.Equal(t, model.KindImage, m.Kind)
	m = one(base("m.raven2", &slidetypes.MessageContentRavenImage{ViewMode: slidetypes.RavenViewModeViewOnce, Attachment: attach("https://cdn/r.jpg", 0)}, ""))
	assert.Equal(t, model.KindUnsupported, m.Kind)
	assert.Nil(t, m.Media)
	rv := slidetypes.MessageContentRavenVideo{ViewMode: slidetypes.RavenViewModeKeepInChat, Attachment: attach("https://cdn/rv.mp4", 0)}
	m = one(base("m.raven3", &rv, ""))
	assert.Equal(t, model.KindVideo, m.Kind)

	assert.Empty(t, c.Messages(base("m.vacio", &slidetypes.MessageContentText{}, ""), "thr1"), "mensaje vacío")
	assert.Empty(t, c.Messages(nil, "thr1"))
	assert.Empty(t, c.Messages(base("", &slidetypes.MessageContentText{TextBody: "x"}, "x"), "thr1"), "sin id")
}

func TestMultiMediaMakesOneMessagePerAttachment(t *testing.T) {
	c := newConv()
	out := c.Messages(&slidetypes.Message{
		ID: "m.multi", SenderFBID: 200, TimestampMS: ms(1), TextBody: "fotos y vídeo",
		Content: slidetypes.MessageContentWrapper{Content: &slidetypes.MessageContentMultiMedia{Attachments: []*slidetypes.Attachment{
			attach("https://cdn/1.jpg", 2), attach("https://cdn/2.mp4", 4), attach("https://cdn/3.jpg", 2),
		}}},
	}, "thr1")
	require.Len(t, out, 3)
	assert.Equal(t, "m.multi", out[0].NativeID)
	assert.Equal(t, "m.multi#2", out[1].NativeID)
	assert.Equal(t, "m.multi#3", out[2].NativeID)
	assert.Equal(t, model.KindImage, out[0].Kind)
	assert.Equal(t, model.KindVideo, out[1].Kind)
	assert.Equal(t, "fotos y vídeo", out[0].Text)
	assert.Empty(t, out[1].Text)
	assert.NotEqual(t, out[0].Media.ID, out[1].Media.ID)
	assert.Equal(t, model.NewMessageKey(acc, "thr1", "m.multi#2"), out[1].Key)
}

func TestMessageReplyEditReactionsAndOwn(t *testing.T) {
	c := newConv()
	c.Thread(dmThread())
	m := textMsg("m.1", "hola", 100, 1000)
	m.RepliedToMessageID = "m.0"
	m.RepliedToMessage = &slidetypes.Message{TextBody: "antes", SenderFBID: 200}
	m.IGDIsForwarded = true
	m.SlideEditHistory = []slidetypes.MessageEditHistoryEntry{{TimestampMS: ms(1200)}, {TimestampMS: ms(1500)}}
	m.Reactions = []*slidetypes.Reaction{{Reaction: "❤", SenderFBID: 200, ReactionTimestampMS: ms(1100), LogMessageID: "log1"}, nil, {Reaction: ""}}
	m.Sender = &slidetypes.MessageSender{UserDict: slidetypes.User{InteropMessagingUserFBID: 300, FullName: "Luis"}}
	out := c.Messages(m, "thr1")
	require.Len(t, out, 1)
	got := out[0]
	assert.True(t, got.FromMe)
	assert.Equal(t, "Tú", got.SenderName)
	assert.Equal(t, "m.0", got.QuotedNativeID)
	assert.Equal(t, "antes", got.QuotedPreview)
	assert.Equal(t, "Ana Pérez", got.QuotedSender)
	assert.True(t, got.Forwarded)
	assert.Equal(t, int64(1500), got.EditedAt)
	require.Len(t, got.Reactions, 1)
	assert.Equal(t, "❤", got.Reactions[0].Emoji)
	assert.Equal(t, "Ana Pérez", got.Reactions[0].SenderName)
	assert.Equal(t, "Luis", c.Name(300), "el remitente del mensaje también registra a la persona")
}

func TestPageIsOldestFirst(t *testing.T) {
	c := newConv()
	r := c.Page("thr1", []slidetypes.Node[*slidetypes.Message]{
		{Node: textMsg("m.3", "c", 200, 300)}, {Node: textMsg("m.1", "a", 200, 100)}, {Node: textMsg("m.2", "b", 200, 200)}, {Node: nil},
	})
	require.Len(t, r.Messages, 3)
	assert.Equal(t, []string{"m.1", "m.2", "m.3"}, []string{r.Messages[0].NativeID, r.Messages[1].NativeID, r.Messages[2].NativeID})
}

func delta(chat string, data slidetypes.DeltaEvent) *slidetypes.Delta {
	return &slidetypes.Delta{TypeName: "x", ThreadIGID: chat, Data: data}
}

func TestDeltaNewMessageNeedsKnownThread(t *testing.T) {
	c := newConv()
	d := delta("thr1", &slidetypes.NewMessageEvent{Message: textMsg("m.1", "hola", 200, 10)})
	_, ok := c.Delta(context.Background(), d, LookupFunc{})
	assert.False(t, ok, "hilo desconocido: hay que pedirlo antes")

	c.Thread(dmThread())
	r, ok := c.Delta(context.Background(), d, LookupFunc{})
	require.True(t, ok)
	require.Len(t, r.Messages, 1)
	assert.Equal(t, "hola", r.Messages[0].Text)

	// Si el aviso no trae el id del hilo, se saca del mensaje.
	r, ok = c.Delta(context.Background(), delta("", &slidetypes.NewMessageEvent{Message: textMsg("m.2", "sin hilo", 200, 20)}), LookupFunc{})
	require.True(t, ok)
	require.Len(t, r.Messages, 1)
	assert.Equal(t, "thr1", r.Messages[0].ChatNativeID)

	// Un aviso sin hilo en absoluto no hace nada.
	r, ok = c.Delta(context.Background(), delta("", &slidetypes.MarkReadEvent{}), LookupFunc{})
	assert.True(t, ok)
	assert.True(t, r.Empty())
}

func TestDeltaEditDeleteAndReactions(t *testing.T) {
	c := newConv()
	c.Thread(dmThread())
	ctx := context.Background()

	r, _ := c.Delta(ctx, delta("thr1", &slidetypes.EditMessageEvent{MessageID: "m.1", TextBody: "corregido"}), LookupFunc{})
	require.Len(t, r.Updates, 1)
	assert.Equal(t, "corregido", *r.Updates[0].Text)
	assert.Equal(t, int64(5_000_000), r.Updates[0].EditedAt)
	assert.Equal(t, model.NewMessageKey(acc, "thr1", "m.1"), r.Updates[0].Key)

	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.DeleteMessageEvent{MessageID: "m.1"}), LookupFunc{})
	require.Len(t, r.Updates, 1)
	assert.True(t, r.Updates[0].Deleted)

	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.CreateReactionEvent{MessageID: "m.2", Reaction: slidetypes.Reaction{
		Reaction: "😂", SenderFBID: 200, ReactionTimestampMS: ms(777), LogMessageID: "log9"}}), LookupFunc{})
	require.Len(t, r.Reactions, 1)
	assert.Equal(t, "😂", r.Reactions[0].Reaction.Emoji)
	assert.Equal(t, "Ana Pérez", r.Reactions[0].Reaction.SenderName)
	assert.Equal(t, int64(777), r.Reactions[0].Reaction.At)

	// Al quitarla, el aviso puede traer solo el id del registro: se resuelve con lo recordado.
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.DeleteReactionEvent{Reaction: slidetypes.Reaction{LogMessageID: "log9"}}), LookupFunc{})
	require.Len(t, r.Reactions, 1)
	assert.Equal(t, "", r.Reactions[0].Reaction.Emoji)
	assert.Equal(t, "200", r.Reactions[0].Reaction.SenderNativeID)
	assert.Equal(t, model.NewMessageKey(acc, "thr1", "m.2"), r.Reactions[0].MessageKey)

	// Un registro desconocido no produce nada.
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.DeleteReactionEvent{Reaction: slidetypes.Reaction{LogMessageID: "desconocido"}}), LookupFunc{})
	assert.Empty(t, r.Reactions)
}

func TestDeltaReadStateAndThreadChanges(t *testing.T) {
	c := newConv()
	c.Thread(dmThread())
	ctx := context.Background()

	r, _ := c.Delta(ctx, delta("thr1", &slidetypes.MarkReadEvent{}), LookupFunc{})
	require.Len(t, r.Chats, 1)
	assert.Equal(t, 0, r.Chats[0].UnreadCount)
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.MarkUnreadEvent{MarkedAsUnread: true}), LookupFunc{})
	require.Len(t, r.Chats, 1)
	assert.Equal(t, 1, r.Chats[0].UnreadCount)
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.MarkUnreadEvent{MarkedAsUnread: false}), LookupFunc{})
	assert.True(t, r.Empty())

	// Otra persona leyó mis mensajes.
	lk := LookupFunc{OutgoingUpTo: func(_ context.Context, chat string, upTo int64) []string {
		assert.Equal(t, "thr1", chat)
		assert.Equal(t, int64(1500), upTo)
		return []string{"m.a", "m.b"}
	}}
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.ReadReceiptEvent{ReadReceipt: slidetypes.ReadReceipt{ParticipantFBID: 200, WatermarkTimestampMS: ms(1500)}}), lk)
	require.Len(t, r.Receipts, 1)
	assert.Equal(t, model.DeliveryRead, r.Receipts[0].Delivery)
	assert.Equal(t, "200", r.Receipts[0].ReaderID)
	assert.Len(t, r.Receipts[0].MessageKeys, 2)
	// Yo leí desde otro dispositivo.
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.ReadReceiptEvent{ReadReceipt: slidetypes.ReadReceipt{ParticipantFBID: 100, WatermarkTimestampMS: ms(1500)}}), lk)
	assert.Empty(t, r.Receipts)
	require.Len(t, r.Chats, 1)
	assert.Equal(t, 0, r.Chats[0].UnreadCount)

	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.PinThreadEvent{IsPinned: false}), LookupFunc{})
	require.Len(t, r.Chats, 1)
	assert.False(t, r.Chats[0].Pinned)
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.MuteThreadEvent{IsMutedNow: false}), LookupFunc{})
	assert.Equal(t, int64(0), r.Chats[0].MutedUntil)
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.MuteThreadEvent{IsMutedNow: true}), LookupFunc{})
	assert.Equal(t, int64(-1), r.Chats[0].MutedUntil)

	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.UpdateThreadNameEvent{ThreadName: "Nuevo"}), LookupFunc{})
	require.Len(t, r.Chats, 1)
	assert.Equal(t, "Nuevo", r.Chats[0].Title)
	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.UpdateThreadImageEvent{ThreadImage: struct {
		URI string `json:"uri"`
	}{URI: "https://cdn/n.jpg"}}), LookupFunc{})
	assert.Equal(t, "https://cdn/n.jpg", r.Avatars["thr1"])

	r, _ = c.Delta(ctx, delta("thr1", &slidetypes.DeleteThreadEvent{}), LookupFunc{})
	require.Len(t, r.Removed, 1)
	assert.Equal(t, model.NewChatKey(acc, "thr1"), r.Removed[0])
	assert.False(t, c.Known("thr1"))
}

func TestDeltaGroupMembershipMessages(t *testing.T) {
	c := newConv()
	c.Thread(dmThread())
	admin := &slidetypes.Message{ID: "adm.1", SenderFBID: 200, TimestampMS: ms(50),
		Content: slidetypes.MessageContentWrapper{Content: &slidetypes.MessageContentAdminText{TextFragments: []slidetypes.TextFragment{{Plaintext: "Luis se unió"}}}}}
	r, _ := c.Delta(context.Background(), delta("thr1", &slidetypes.ParticipantJoinEvent{Message: admin}), LookupFunc{})
	require.Len(t, r.Messages, 1)
	assert.Equal(t, model.KindSystem, r.Messages[0].Kind)
	r, _ = c.Delta(context.Background(), delta("thr1", &slidetypes.ParticipantLeaveEvent{Message: admin}), LookupFunc{})
	require.Len(t, r.Messages, 1)
	r, _ = c.Delta(context.Background(), delta("thr1", &slidetypes.AdminMessageEvent{Message: admin}), LookupFunc{})
	require.Len(t, r.Messages, 1)
	r, _ = c.Delta(context.Background(), delta("thr1", slidetypes.UnknownEvent{"a": 1}), LookupFunc{})
	assert.True(t, r.Empty())
}

func TestTyping(t *testing.T) {
	c := newConv()
	c.Thread(dmThread())
	ev, ok := c.Typing(&slidetypes.TypingNotification{ThreadID: "long1", SenderID: 200, ActivityStatus: 1})
	require.True(t, ok)
	assert.Equal(t, model.NewChatKey(acc, "thr1"), ev.ChatKey)
	assert.Equal(t, "200", ev.PeerID)
	assert.True(t, ev.Typing)
	ev, ok = c.Typing(&slidetypes.TypingNotification{ThreadID: "long1", SenderID: 200, ActivityStatus: 0})
	require.True(t, ok)
	assert.False(t, ev.Typing)
	_, ok = c.Typing(&slidetypes.TypingNotification{ThreadID: "long1", SenderID: 100, ActivityStatus: 1})
	assert.False(t, ok, "mi propio «escribiendo» se ignora")
	_, ok = c.Typing(&slidetypes.TypingNotification{ThreadID: "desconocido", SenderID: 200, ActivityStatus: 1})
	assert.False(t, ok)
}

func TestResultEventsOrder(t *testing.T) {
	c := newConv()
	r := c.Thread(dmThread())
	r.Messages = append(r.Messages, model.Message{NativeID: "m", ChatKey: model.NewChatKey(acc, "thr1")})
	r.Updates = append(r.Updates, model.MessageUpdate{Key: model.NewMessageKey(acc, "thr1", "m")})
	r.Removed = append(r.Removed, model.NewChatKey(acc, "z"))
	var types []model.EventType
	for _, e := range r.Events() {
		types = append(types, e.Type)
	}
	assert.Equal(t, []model.EventType{model.EvPerson, model.EvChatUpsert, model.EvMessageNew, model.EvMessageUpdate, model.EvChatRemoved}, types)
	h := r.History(1)
	assert.Len(t, h.Chats, 1)
	assert.Len(t, h.Messages, 1)
}
