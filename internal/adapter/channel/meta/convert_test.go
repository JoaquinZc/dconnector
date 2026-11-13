package meta

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"

	"dconnector/internal/core/model"
)

const (
	acc  = model.AccountID("fb-test")
	self = int64(100)
)

func newConv() *Converter {
	c := NewConverter(acc, model.ChannelMessenger, self)
	c.Now = func() time.Time { return time.UnixMilli(5_000_000) }
	return c
}

func chatByID(r *Result, id string) *model.Chat {
	for i := range r.Chats {
		if r.Chats[i].NativeID == id {
			return &r.Chats[i]
		}
	}
	return nil
}

func msgByID(r *Result, id string) *model.Message {
	for i := range r.Messages {
		if r.Messages[i].NativeID == id {
			return &r.Messages[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Hilos y personas

func TestThreadsDirectAndGroup(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSDeleteThenInsertContact: []*table.LSDeleteThenInsertContact{{Id: 200, Name: "Ana Pérez", Username: "ana", PhoneNumber: 593991112222}},
		LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{
			{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox", LastActivityTimestampMs: 2000,
				LastReadWatermarkTimestampMs: 2000, Snippet: "hola", SnippetSenderContactId: 200},
			{ThreadKey: 300, ThreadType: table.GROUP_THREAD, FolderName: "inbox", ThreadName: "Equipo", LastActivityTimestampMs: 3000,
				LastReadWatermarkTimestampMs: 1000, MuteExpireTimeMs: -1, UnreadMessageCount: 3},
			{ThreadKey: 100, ThreadType: table.ONE_TO_ONE, FolderName: "inbox", LastActivityTimestampMs: 10, LastReadWatermarkTimestampMs: 10},
		},
	})
	require.Len(t, r.Persons, 1)
	assert.Equal(t, "200", r.Persons[0].NativeID)
	assert.Equal(t, "Ana Pérez", r.Persons[0].PushName)
	assert.Equal(t, "ana", r.Persons[0].Username)
	assert.Equal(t, "+593991112222", r.Persons[0].Phone)
	require.Len(t, r.Chats, 3)

	d := chatByID(r, "200")
	require.NotNil(t, d)
	assert.Equal(t, model.ChatDirect, d.Kind)
	assert.Equal(t, "Ana Pérez", d.Title)
	assert.Equal(t, "200", d.PeerNativeID)
	assert.Equal(t, 0, d.UnreadCount, "leído hasta la última actividad")
	assert.Equal(t, "hola", d.LastPreview)
	assert.Equal(t, int64(2000), d.LastMessageAt)
	assert.False(t, d.LastFromMe)
	assert.Equal(t, model.NewChatKey(acc, "200"), d.Key)

	g := chatByID(r, "300")
	require.NotNil(t, g)
	assert.Equal(t, model.ChatGroup, g.Kind)
	assert.Equal(t, "Equipo", g.Title)
	assert.Equal(t, 3, g.UnreadCount)
	assert.Equal(t, int64(-1), g.MutedUntil)
	assert.Empty(t, g.PeerNativeID)

	s := chatByID(r, "100")
	require.NotNil(t, s)
	assert.Equal(t, model.ChatSelf, s.Kind)
	assert.Equal(t, "Tú", s.Title)

	// Un aviso parcial lleva el contador «sin cambio» y conserva título y marcas.
	r2 := c.Convert(context.Background(), &table.LSTable{
		LSUpdateThreadSnippet: []*table.LSUpdateThreadSnippet{{ThreadKey: 300, Snippet: "nuevo", SnippetSenderContactId: self}},
	})
	require.Len(t, r2.Chats, 1)
	assert.Equal(t, -1, r2.Chats[0].UnreadCount)
	assert.Equal(t, "Equipo", r2.Chats[0].Title)
	assert.Equal(t, int64(-1), r2.Chats[0].MutedUntil)
	assert.Equal(t, "nuevo", r2.Chats[0].LastPreview)
	assert.True(t, r2.Chats[0].LastFromMe)
}

func TestUnreadUnknownIsMinusOne(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
			{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox", LastActivityTimestampMs: 2000, LastReadWatermarkTimestampMs: 1000},
		},
	})
	require.Len(t, r.Chats, 1)
	assert.Equal(t, -1, r.Chats[0].UnreadCount)
}

func TestMuteExpiredAndArchived(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
			{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "archived", MuteExpireTimeMs: 4_000_000},
			{ThreadKey: 201, ThreadType: table.ONE_TO_ONE, FolderName: "inbox", MuteExpireTimeMs: 9_000_000},
		},
	})
	a := chatByID(r, "200")
	require.NotNil(t, a)
	assert.True(t, a.Archived)
	assert.Equal(t, int64(0), a.MutedUntil, "el silencio venció")
	b := chatByID(r, "201")
	require.NotNil(t, b)
	assert.False(t, b.Archived)
	assert.Equal(t, int64(9_000_000), b.MutedUntil)
}

func TestSpamAndEncryptedHidden(t *testing.T) {
	c := newConv()
	tbl := func() *table.LSTable {
		return &table.LSTable{
			LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
				{ThreadKey: 400, ThreadType: table.ONE_TO_ONE, FolderName: "spam"},
				{ThreadKey: 401, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, FolderName: "e2ee_cutover"},
				{ThreadKey: 402, ThreadType: table.MONTAGE},
				{ThreadKey: 403, ThreadType: table.ONE_TO_ONE, FolderName: "pending"},
			},
		}
	}
	r := c.Convert(context.Background(), tbl())
	require.Len(t, r.Chats, 1, "solo el de solicitudes pendientes se muestra")
	assert.Equal(t, "403", r.Chats[0].NativeID)

	c2 := newConv()
	c2.ShowEncrypted = true
	r2 := c2.Convert(context.Background(), tbl())
	require.Len(t, r2.Chats, 2)
	assert.NotNil(t, chatByID(r2, "401"))
}

func TestDirectTitleArrivesWithLaterContact(t *testing.T) {
	c := newConv()
	c.Convert(context.Background(), &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox"}},
	})
	r := c.Convert(context.Background(), &table.LSTable{
		LSVerifyContactRowExists: []*table.LSVerifyContactRowExists{{ContactId: 200, Name: "Luis", SecondaryName: "luis.g"}},
	})
	require.Len(t, r.Persons, 1)
	assert.Equal(t, "luis.g", r.Persons[0].Username)
	require.Len(t, r.Chats, 1, "el chat se vuelve a anunciar con el nombre")
	assert.Equal(t, "Luis", r.Chats[0].Title)
}

func TestThreadNameImageSnippetAndAvatars(t *testing.T) {
	c := newConv()
	c.Convert(context.Background(), &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{ThreadKey: 300, ThreadType: table.GROUP_THREAD, FolderName: "inbox", ThreadName: "Viejo"}},
	})
	r := c.Convert(context.Background(), &table.LSTable{
		LSSyncUpdateThreadName: []*table.LSSyncUpdateThreadName{{ThreadKey: 300, ThreadName: "Nuevo nombre"}},
		LSSetThreadImageURL:    []*table.LSSetThreadImageURL{{ThreadKey: 300, ImageURL: "https://cdn/g.jpg"}},
	})
	require.Len(t, r.Chats, 1)
	assert.Equal(t, "Nuevo nombre", r.Chats[0].Title)
	assert.Equal(t, "https://cdn/g.jpg", r.Avatars["300"])
	assert.Equal(t, "https://cdn/g.jpg", c.AvatarURL("300"))

	c.SetAvatarFile("300", "abc.jpg")
	ch, ok := c.ChatSnapshot(300)
	require.True(t, ok)
	assert.Equal(t, "/avatar/fb-test/abc.jpg", ch.AvatarURL)
}

// ---------------------------------------------------------------------------------------------
// Mensajes

func TestTextMessageWithReplyForwardAndMentions(t *testing.T) {
	c := newConv()
	c.Convert(context.Background(), &table.LSTable{
		LSDeleteThenInsertContact: []*table.LSDeleteThenInsertContact{{Id: 200, Name: "Ana"}},
	})
	r := c.Convert(context.Background(), &table.LSTable{
		LSInsertMessage: []*table.LSInsertMessage{{
			ThreadKey: 200, MessageId: "mid.1", Text: "hola", SenderId: 200, TimestampMs: 1000,
			ReplySourceId: "mid.0", ReplyMessageText: "antes", ReplyToUserId: self, IsForwarded: true, MentionIds: "100, 300",
		}},
	})
	require.Len(t, r.Messages, 1)
	m := r.Messages[0]
	assert.Equal(t, model.KindText, m.Kind)
	assert.Equal(t, "hola", m.Text)
	assert.Equal(t, "200", m.SenderNativeID)
	assert.Equal(t, "Ana", m.SenderName)
	assert.False(t, m.FromMe)
	assert.Equal(t, int64(1000), m.Timestamp)
	assert.Equal(t, model.NewMessageKey(acc, "200", "mid.1"), m.Key)
	assert.Equal(t, model.NewChatKey(acc, "200"), m.ChatKey)
	assert.Equal(t, "mid.0", m.QuotedNativeID)
	assert.Equal(t, "antes", m.QuotedPreview)
	assert.Equal(t, "100", m.QuotedSender)
	assert.True(t, m.Forwarded)
	assert.Equal(t, []string{"100", "300"}, m.Mentions)
	assert.Equal(t, model.DeliverySent, m.Delivery)
	// El hilo no se conocía: el chat se anuncia.
	require.Len(t, r.Chats, 1)
	assert.Equal(t, "200", r.Chats[0].NativeID)
	assert.Equal(t, "Ana", r.Chats[0].Title)
}

func TestOwnMessageIsFromMe(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSInsertMessage: []*table.LSInsertMessage{{ThreadKey: 200, MessageId: "mid.1", Text: "yo", SenderId: self, TimestampMs: 1}},
	})
	require.Len(t, r.Messages, 1)
	assert.True(t, r.Messages[0].FromMe)
	assert.Equal(t, "Tú", r.Messages[0].SenderName)
}

func TestAttachmentsJoinedByMessageID(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSInsertMessage: []*table.LSInsertMessage{
			{ThreadKey: 200, MessageId: "mid.img", Text: "mira", SenderId: 200, TimestampMs: 10},
			{ThreadKey: 200, MessageId: "mid.vid", SenderId: 200, TimestampMs: 20},
			{ThreadKey: 200, MessageId: "mid.voz", SenderId: 200, TimestampMs: 30},
			{ThreadKey: 200, MessageId: "mid.doc", SenderId: 200, TimestampMs: 40},
			{ThreadKey: 200, MessageId: "mid.sticker", SenderId: 200, TimestampMs: 50},
			{ThreadKey: 200, MessageId: "mid.dos", SenderId: 200, TimestampMs: 60},
		},
		LSInsertBlobAttachment: []*table.LSInsertBlobAttachment{
			{MessageId: "mid.img", AttachmentType: table.AttachmentTypeImage, PreviewUrl: "https://cdn/img.jpg", PreviewUrlMimeType: "image/jpeg",
				PreviewWidth: 640, PreviewHeight: 480, PreviewUrlExpirationTimestampMs: 9_000_000, Filename: "foto.jpg", Filesize: 1234, AttachmentFbid: "a1"},
			{MessageId: "mid.vid", AttachmentType: table.AttachmentTypeVideo, PlayableUrl: "https://cdn/v.mp4", PlayableUrlMimeType: "video/mp4",
				PlayableDurationMs: 7500, PlayableUrlExpirationTimestampMs: 9_000_000},
			{MessageId: "mid.voz", AttachmentType: table.AttachmentTypeAudio, PlayableUrl: "https://cdn/a.mp4", PlayableUrlMimeType: "audio/mp4",
				PlayableDurationMs: 3000, WaveformData: "abc", Filename: "audioclip-1.mp4"},
			{MessageId: "mid.doc", AttachmentType: table.AttachmentTypeFile, PlayableUrl: "https://cdn/f.pdf", PlayableUrlMimeType: "application/pdf",
				Filename: "factura.pdf", Filesize: 99},
			{MessageId: "mid.dos", AttachmentType: table.AttachmentTypeImage, PreviewUrl: "https://cdn/1.jpg", PreviewUrlMimeType: "image/jpeg"},
			{MessageId: "mid.dos", AttachmentType: table.AttachmentTypeImage, PreviewUrl: "https://cdn/2.jpg", PreviewUrlMimeType: "image/jpeg"},
		},
		LSInsertStickerAttachment: []*table.LSInsertStickerAttachment{
			{MessageId: "mid.sticker", PlayableUrl: "https://cdn/s.webp", PlayableUrlMimeType: "image/webp", PreviewWidth: 96, PreviewHeight: 96},
		},
	})
	require.Len(t, r.Messages, 7)

	img := msgByID(r, "mid.img")
	require.NotNil(t, img)
	assert.Equal(t, model.KindImage, img.Kind)
	assert.Equal(t, "mira", img.Text, "el texto es el pie de foto")
	require.NotNil(t, img.Media)
	assert.Equal(t, "image/jpeg", img.Media.Mime)
	assert.Equal(t, "foto.jpg", img.Media.FileName)
	assert.Equal(t, 640, img.Media.Width)
	assert.Equal(t, 480, img.Media.Height)
	assert.Equal(t, int64(1234), img.Media.Size)
	assert.Equal(t, MediaID("200", "mid.img", 0), img.Media.ID)
	h, err := ParseHandle(img.Media.Handle)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn/img.jpg", h.URL)
	assert.Equal(t, int64(9_000_000), h.Expires)
	assert.False(t, h.Expired(1))
	assert.True(t, h.Expired(9_000_001))

	vid := msgByID(r, "mid.vid")
	require.NotNil(t, vid)
	assert.Equal(t, model.KindVideo, vid.Kind)
	assert.InDelta(t, 7.5, vid.Media.Duration, 0.001)
	assert.NotEmpty(t, vid.Media.FileName)

	voz := msgByID(r, "mid.voz")
	require.NotNil(t, voz)
	assert.Equal(t, model.KindVoice, voz.Kind)

	doc := msgByID(r, "mid.doc")
	require.NotNil(t, doc)
	assert.Equal(t, model.KindDocument, doc.Kind)
	assert.Equal(t, "factura.pdf", doc.Media.FileName)
	assert.Equal(t, "application/pdf", doc.Media.Mime)

	stk := msgByID(r, "mid.sticker")
	require.NotNil(t, stk)
	assert.Equal(t, model.KindSticker, stk.Kind)
	assert.Equal(t, 96, stk.Media.Width)

	d1, d2 := msgByID(r, "mid.dos"), msgByID(r, "mid.dos#2")
	require.NotNil(t, d1)
	require.NotNil(t, d2, "el segundo adjunto sale como otro mensaje")
	assert.NotEqual(t, d1.Media.ID, d2.Media.ID)
	assert.Equal(t, model.NewMessageKey(acc, "200", "mid.dos#2"), d2.Key)
}

func TestLegacyAttachmentAndViewOnce(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSInsertMessage: []*table.LSInsertMessage{
			{ThreadKey: 200, MessageId: "mid.a", SenderId: 200},
			{ThreadKey: 200, MessageId: "mid.b", SenderId: 200},
		},
		LSInsertAttachment: []*table.LSInsertAttachment{
			{MessageId: "mid.a", AttachmentType: table.AttachmentTypeAnimatedImage, PreviewUrl: "https://cdn/g.gif", PreviewUrlMimeType: "image/gif"},
			{MessageId: "mid.b", AttachmentType: table.AttachmentTypeEphemeralImage, PreviewUrl: "https://cdn/e.jpg"},
		},
	})
	a, b := msgByID(r, "mid.a"), msgByID(r, "mid.b")
	require.NotNil(t, a)
	require.NotNil(t, b)
	assert.Equal(t, model.KindImage, a.Kind)
	assert.Equal(t, "image/gif", a.Media.Mime)
	assert.Equal(t, model.KindUnsupported, b.Kind, "«ver una vez» no se muestra")
	assert.Nil(t, b.Media)
}

func TestXMALinkThumbsUpAdminUnsentAndEmpty(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSInsertMessage: []*table.LSInsertMessage{
			{ThreadKey: 200, MessageId: "mid.link", SenderId: 200},
			{ThreadKey: 200, MessageId: "mid.xmasolo", SenderId: 200},
			{ThreadKey: 200, MessageId: "mid.up", SenderId: 200, StickerId: thumbsUpMediumStickerID},
			{ThreadKey: 200, MessageId: "mid.adm", Text: "Ana salió del grupo", IsAdminMessage: true},
			{ThreadKey: 200, MessageId: "mid.unsent", Text: "borrado", IsUnsent: true, SenderId: 200},
			{ThreadKey: 200, MessageId: "mid.vacio", SenderId: 200},
		},
		LSInsertXmaAttachment: []*table.LSInsertXmaAttachment{
			{MessageId: "mid.link", TitleText: "Un título", ActionUrl: "https://ejemplo.com/x"},
			{MessageId: "mid.xmasolo"},
		},
		LSInsertStickerAttachment: []*table.LSInsertStickerAttachment{{MessageId: "mid.up", PlayableUrl: "https://cdn/up.png"}},
	})
	link := msgByID(r, "mid.link")
	require.NotNil(t, link)
	assert.Equal(t, model.KindText, link.Kind)
	assert.Equal(t, "Un título\nhttps://ejemplo.com/x", link.Text)

	xs := msgByID(r, "mid.xmasolo")
	require.NotNil(t, xs)
	assert.Equal(t, model.KindUnsupported, xs.Kind)

	up := msgByID(r, "mid.up")
	require.NotNil(t, up)
	assert.Equal(t, model.KindText, up.Kind)
	assert.Equal(t, "👍", up.Text)
	assert.Nil(t, up.Media)

	adm := msgByID(r, "mid.adm")
	require.NotNil(t, adm)
	assert.Equal(t, model.KindSystem, adm.Kind)

	assert.Nil(t, msgByID(r, "mid.unsent"))
	assert.Nil(t, msgByID(r, "mid.vacio"))
}

func TestUpsertMessageAndBundledReactions(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSUpsertMessage: []*table.LSUpsertMessage{{ThreadKey: 200, MessageId: "mid.u", Text: "viejo", SenderId: 200, TimestampMs: 5}},
		LSUpsertReaction: []*table.LSUpsertReaction{
			{ThreadKey: 200, MessageId: "mid.u", ActorId: self, Reaction: "👍", TimestampMs: 6},
			{ThreadKey: 200, MessageId: "mid.u", ActorId: 200, Reaction: "", TimestampMs: 7},
		},
	})
	require.Len(t, r.Messages, 1)
	require.Len(t, r.Messages[0].Reactions, 1, "para el historial las reacciones van dentro del mensaje")
	assert.Equal(t, "👍", r.Messages[0].Reactions[0].Emoji)
	assert.Equal(t, "100", r.Messages[0].Reactions[0].SenderNativeID)
}

func TestOwnMessageReadByOthersInHistory(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSInsertMessage: []*table.LSInsertMessage{
			{ThreadKey: 200, MessageId: "mid.viejo", Text: "a", SenderId: self, TimestampMs: 100},
			{ThreadKey: 200, MessageId: "mid.nuevo", Text: "b", SenderId: self, TimestampMs: 900},
			{ThreadKey: 200, MessageId: "mid.ajeno", Text: "c", SenderId: 200, TimestampMs: 50},
		},
		LSAddParticipantIdToGroupThread: []*table.LSAddParticipantIdToGroupThread{
			{ThreadKey: 200, ContactId: 200, ReadWatermarkTimestampMs: 500},
			{ThreadKey: 200, ContactId: self, ReadWatermarkTimestampMs: 9999},
		},
	})
	assert.Equal(t, model.DeliveryRead, msgByID(r, "mid.viejo").Delivery)
	assert.Equal(t, model.DeliverySent, msgByID(r, "mid.nuevo").Delivery)
	assert.Equal(t, model.DeliverySent, msgByID(r, "mid.ajeno").Delivery)
}

// ---------------------------------------------------------------------------------------------
// Ediciones, borrados, reacciones, recibos, «escribiendo»

func TestEditNeedsKnownChat(t *testing.T) {
	c := newConv()
	c.Lookups.ChatOfMessage = func(_ context.Context, id string) string {
		if id == "mid.1" {
			return "200"
		}
		return ""
	}
	r := c.Convert(context.Background(), &table.LSTable{
		LSEditMessage: []*table.LSEditMessage{
			{MessageID: "mid.1", Text: "corregido", EditCount: 1},
			{MessageID: "mid.desconocido", Text: "x"},
		},
	})
	require.Len(t, r.Updates, 1)
	u := r.Updates[0]
	assert.Equal(t, model.NewMessageKey(acc, "200", "mid.1"), u.Key)
	require.NotNil(t, u.Text)
	assert.Equal(t, "corregido", *u.Text)
	assert.Equal(t, int64(5_000_000), u.EditedAt)
	assert.Equal(t, 1, r.Skipped)
}

func TestDeleteAndUnsend(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSDeleteMessage: []*table.LSDeleteMessage{{ThreadKey: 200, MessageId: "mid.1"}},
		LSDeleteThenInsertMessage: []*table.LSDeleteThenInsertMessage{
			{ThreadKey: 200, MessageId: "mid.2", IsUnsent: true},
			{ThreadKey: 200, MessageId: "mid.3", IsUnsent: false},
		},
	})
	require.Len(t, r.Updates, 2)
	assert.True(t, r.Updates[0].Deleted)
	assert.Equal(t, model.NewMessageKey(acc, "200", "mid.1"), r.Updates[0].Key)
	assert.Equal(t, model.NewMessageKey(acc, "200", "mid.2"), r.Updates[1].Key)
}

func TestReactionsUpsertAndDelete(t *testing.T) {
	c := newConv()
	c.Convert(context.Background(), &table.LSTable{
		LSDeleteThenInsertContact: []*table.LSDeleteThenInsertContact{{Id: 200, Name: "Ana"}},
	})
	r := c.Convert(context.Background(), &table.LSTable{
		LSUpsertReaction: []*table.LSUpsertReaction{{ThreadKey: 200, MessageId: "mid.1", ActorId: 200, Reaction: "❤", TimestampMs: 77}},
		LSDeleteReaction: []*table.LSDeleteReaction{{ThreadKey: 200, MessageId: "mid.1", ActorId: 300}},
	})
	require.Len(t, r.Reactions, 2)
	assert.Equal(t, model.NewMessageKey(acc, "200", "mid.1"), r.Reactions[0].MessageKey)
	assert.Equal(t, "❤", r.Reactions[0].Reaction.Emoji)
	assert.Equal(t, "Ana", r.Reactions[0].Reaction.SenderName)
	assert.Equal(t, int64(77), r.Reactions[0].Reaction.At)
	assert.Equal(t, "", r.Reactions[1].Reaction.Emoji, "quitar la reacción = emoji vacío")
	assert.Equal(t, "300", r.Reactions[1].Reaction.SenderNativeID)
}

func TestReadReceiptsFromOthersAndFromMe(t *testing.T) {
	c := newConv()
	c.Lookups.OutgoingUpTo = func(_ context.Context, chat string, upTo int64) []string {
		assert.Equal(t, "200", chat)
		assert.Equal(t, int64(1500), upTo)
		return []string{"mid.a", "mid.b"}
	}
	c.Convert(context.Background(), &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
			{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox", LastActivityTimestampMs: 1000, LastReadWatermarkTimestampMs: 0},
		},
	})
	r := c.Convert(context.Background(), &table.LSTable{
		LSUpdateReadReceipt: []*table.LSUpdateReadReceipt{{ThreadKey: 200, ContactId: 200, ReadWatermarkTimestampMs: 1500, ReadActionTimestampMs: 1600}},
	})
	require.Len(t, r.Receipts, 1)
	rc := r.Receipts[0]
	assert.Equal(t, model.DeliveryRead, rc.Delivery)
	assert.Equal(t, "200", rc.ReaderID)
	assert.Equal(t, int64(1600), rc.At)
	assert.Equal(t, []model.MessageKey{model.NewMessageKey(acc, "200", "mid.a"), model.NewMessageKey(acc, "200", "mid.b")}, rc.MessageKeys)

	// Yo leí desde otro dispositivo: el chat queda en 0 no leídos.
	r2 := c.Convert(context.Background(), &table.LSTable{
		LSMarkThreadReadV2: []*table.LSMarkThreadReadV2{{ThreadKey: 200, LastReadWatermarkTimestampMs: 1000}},
	})
	require.Len(t, r2.Chats, 1)
	assert.Equal(t, 0, r2.Chats[0].UnreadCount)
	assert.Empty(t, r2.Receipts)
}

func TestReadReceiptWithoutKnownMessagesIsDropped(t *testing.T) {
	c := newConv()
	c.Lookups.OutgoingUpTo = func(context.Context, string, int64) []string { return nil }
	r := c.Convert(context.Background(), &table.LSTable{
		LSUpdateReadReceipt: []*table.LSUpdateReadReceipt{{ThreadKey: 200, ContactId: 200, ReadWatermarkTimestampMs: 1500}},
	})
	assert.True(t, r.Empty())
}

func TestTyping(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSUpdateTypingIndicator: []*table.LSUpdateTypingIndicator{
			{ThreadKey: 200, SenderId: 200, IsTyping: true},
			{ThreadKey: 200, SenderId: self, IsTyping: true},
			{ThreadKey: 300, SenderId: 201, IsTyping: false},
		},
	})
	require.Len(t, r.Typing, 2, "mi propio «escribiendo» se ignora")
	assert.Equal(t, model.NewChatKey(acc, "200"), r.Typing[0].ChatKey)
	assert.Equal(t, "200", r.Typing[0].PeerID)
	assert.True(t, r.Typing[0].Typing)
	assert.False(t, r.Typing[1].Typing)
}

// ---------------------------------------------------------------------------------------------
// Borrado de hilos, hilos cifrados, orden y lotes

func TestDeleteThreadIsCancelledByUpsertInSameTable(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSDeleteThread:           []*table.LSDeleteThread{{ThreadKey: 200}, {ThreadKey: 300}},
		LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox"}},
	})
	require.Len(t, r.Removed, 1)
	assert.Equal(t, model.NewChatKey(acc, "300"), r.Removed[0])
	assert.NotNil(t, chatByID(r, "200"))
}

func TestHybridThreadsRemapKeys(t *testing.T) {
	c := newConv()
	c.ShowEncrypted = true
	r := c.Convert(context.Background(), &table.LSTable{
		LSVerifyHybridThreadExists: []*table.LSVerifyHybridThreadExists{{ThreadKey: 111, ThreadJID: 222, ThreadType: table.ENCRYPTED_OVER_WA_GROUP, IsGroupThread: true}},
		LSUpdateOrInsertThread:     []*table.LSUpdateOrInsertThread{{ThreadKey: 111, ThreadType: table.ENCRYPTED_OVER_WA_GROUP, FolderName: "e2ee_cutover", ThreadName: "Cifrado"}},
		LSInsertMessage:            []*table.LSInsertMessage{{ThreadKey: 111, MessageId: "mid.x", Text: "hola", SenderId: 200, TimestampMs: 1}},
	})
	require.NotNil(t, chatByID(r, "222"), "el chat pasa a llamarse por el JID")
	assert.Nil(t, chatByID(r, "111"))
	assert.Equal(t, model.ChatGroup, chatByID(r, "222").Kind)
	assert.Equal(t, "Cifrado", chatByID(r, "222").Title)
	require.Len(t, r.Messages, 1)
	assert.Equal(t, "222", r.Messages[0].ChatNativeID)
	assert.Contains(t, r.Removed, model.NewChatKey(acc, "111"), "el hilo de Facebook que quedó atrás se retira")
	known, group, typ := c.ThreadKnown(222)
	assert.True(t, known)
	assert.True(t, group)
	assert.True(t, typ.IsWhatsApp())
}

func TestEnsureThread(t *testing.T) {
	c := newConv()
	ch, known := c.EnsureThread(555, false, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	assert.False(t, known)
	assert.Equal(t, "555", ch.NativeID)
	assert.Equal(t, model.ChatDirect, ch.Kind)
	_, known = c.EnsureThread(555, false, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	assert.True(t, known)
}

func TestEventsOrderAndHistoryBatches(t *testing.T) {
	c := newConv()
	r := c.Convert(context.Background(), &table.LSTable{
		LSDeleteThenInsertContact: []*table.LSDeleteThenInsertContact{{Id: 200, Name: "Ana"}},
		LSUpdateOrInsertThread:    []*table.LSUpdateOrInsertThread{{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox"}},
		LSInsertMessage:           []*table.LSInsertMessage{{ThreadKey: 200, MessageId: "mid.1", Text: "a", SenderId: 200, TimestampMs: 1}},
		LSDeleteMessage:           []*table.LSDeleteMessage{{ThreadKey: 200, MessageId: "mid.0"}},
		LSUpsertReaction:          []*table.LSUpsertReaction{{ThreadKey: 200, MessageId: "mid.1", ActorId: 200, Reaction: "👍"}},
		LSUpdateTypingIndicator:   []*table.LSUpdateTypingIndicator{{ThreadKey: 200, SenderId: 200, IsTyping: true}},
		LSDeleteThread:            []*table.LSDeleteThread{{ThreadKey: 999}},
	})
	var types []model.EventType
	for _, ev := range r.Events() {
		types = append(types, ev.Type)
	}
	assert.Equal(t, []model.EventType{
		model.EvPerson, model.EvChatUpsert, model.EvMessageNew, model.EvMessageUpdate, model.EvReaction, model.EvTyping, model.EvChatRemoved,
	}, types)

	big := &Result{}
	for i := 0; i < 1100; i++ {
		big.Messages = append(big.Messages, model.Message{NativeID: "m"})
	}
	big.Chats = []model.Chat{{NativeID: "c"}}
	big.Persons = []model.Person{{NativeID: "p"}}
	batches := big.HistoryBatches(500)
	require.Len(t, batches, 3)
	assert.Len(t, batches[0].Messages, 500)
	assert.Len(t, batches[2].Messages, 100)
	assert.Len(t, batches[0].Chats, 1)
	assert.Empty(t, batches[1].Chats)
	assert.InDelta(t, 1.0, batches[2].Progress, 0.0001)
	assert.Len(t, (&Result{}).HistoryBatches(500), 1)
}

func TestNilTableIsEmpty(t *testing.T) {
	assert.True(t, newConv().Convert(context.Background(), nil).Empty())
}

func TestAllChatsAndPersonSnapshot(t *testing.T) {
	c := newConv()
	c.Convert(context.Background(), &table.LSTable{
		LSDeleteThenInsertContact: []*table.LSDeleteThenInsertContact{{Id: 200, Name: "Ana"}},
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
			{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox", LastActivityTimestampMs: 10},
			{ThreadKey: 300, ThreadType: table.GROUP_THREAD, FolderName: "inbox", ThreadName: "G", LastActivityTimestampMs: 20},
		},
	})
	all := c.AllChats()
	require.Len(t, all, 2)
	assert.Equal(t, "300", all[0].NativeID, "de más reciente a más antiguo")
	assert.Equal(t, -1, all[0].UnreadCount)
	p := c.PersonSnapshot(200)
	assert.Equal(t, "Ana", p.PushName)
	assert.Equal(t, "Ana", c.Name(200))
}
