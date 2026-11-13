package telegram

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

const (
	testAcc  = model.AccountID("tg-test")
	selfUID  = int64(1)
	otherUID = int64(100)
)

// testEntities: Ana (contacto), Luis (no contacto), un grupo, un canal de difusión y un supergrupo.
func testEntities() tg.Entities {
	return tg.Entities{
		Users: map[int64]*tg.User{
			selfUID:  {ID: selfUID, FirstName: "Yo", Self: true, AccessHash: 1},
			otherUID: {ID: otherUID, FirstName: "Ana", LastName: "Pérez", Username: "ana", Phone: "593991234567", Contact: true, AccessHash: 11},
			101:      {ID: 101, FirstName: "Luis", Username: "luis", AccessHash: 12},
		},
		Chats:    map[int64]*tg.Chat{7: {ID: 7, Title: "Familia"}},
		Channels: map[int64]*tg.Channel{20: {ID: 20, Title: "Noticias", Broadcast: true, AccessHash: 21}, 30: {ID: 30, Title: "Vecinos", Megagroup: true, AccessHash: 31}},
	}
}

func newConv() converter {
	e := testEntities()
	return converter{acc: testAcc, selfID: selfUID, view: entitiesView{e}}
}

func privMsg(id int, text string) *tg.Message {
	return &tg.Message{ID: id, PeerID: &tg.PeerUser{UserID: otherUID}, FromID: &tg.PeerUser{UserID: otherUID}, Date: 1700000000, Message: text}
}

// ---------------------------------------------------------------------------------------------
// Texto, emisor e ids

func TestConvertTextBasics(t *testing.T) {
	m := newConv().message(privMsg(5, "hola"))
	assert.Equal(t, model.NewMessageKey(testAcc, "user:100", "5"), m.Key)
	assert.Equal(t, model.NewChatKey(testAcc, "user:100"), m.ChatKey)
	assert.Equal(t, testAcc, m.AccountID)
	assert.Equal(t, model.ChannelTelegram, m.Channel)
	assert.Equal(t, "5", m.NativeID)
	assert.Equal(t, "user:100", m.ChatNativeID)
	assert.Equal(t, "user:100", m.SenderNativeID)
	assert.Equal(t, "Ana Pérez", m.SenderName)
	assert.False(t, m.FromMe)
	assert.Equal(t, int64(1700000000000), m.Timestamp)
	assert.Equal(t, model.KindText, m.Kind)
	assert.Equal(t, "hola", m.Text)
	assert.Equal(t, model.DeliverySent, m.Delivery)
	assert.False(t, m.Forwarded)
	assert.Zero(t, m.EditedAt)
	assert.Zero(t, m.DeletedAt)
	assert.Equal(t, "hola", model.Preview(m))
}

func TestConvertPrivateWithoutFromID(t *testing.T) {
	// Entrante sin FromID: el emisor es el otro usuario (PeerID).
	in := &tg.Message{ID: 1, PeerID: &tg.PeerUser{UserID: otherUID}, Date: 10, Message: "x"}
	m := newConv().message(in)
	assert.Equal(t, "user:100", m.SenderNativeID)
	assert.False(t, m.FromMe)
	// Saliente sin FromID: el emisor soy yo.
	out := &tg.Message{ID: 2, Out: true, PeerID: &tg.PeerUser{UserID: otherUID}, Date: 10, Message: "y"}
	m = newConv().message(out)
	assert.Equal(t, "user:1", m.SenderNativeID)
	assert.True(t, m.FromMe)
	assert.Equal(t, "user:100", m.ChatNativeID)
}

func TestConvertGroupChannelAndPost(t *testing.T) {
	c := newConv()
	g := c.message(&tg.Message{ID: 3, PeerID: &tg.PeerChat{ChatID: 7}, FromID: &tg.PeerUser{UserID: 101}, Date: 10, Message: "g"})
	assert.Equal(t, "chat:7", g.ChatNativeID)
	assert.Equal(t, "user:101", g.SenderNativeID)
	assert.Equal(t, "Luis", g.SenderName)

	mg := c.message(&tg.Message{ID: 4, PeerID: &tg.PeerChannel{ChannelID: 30}, FromID: &tg.PeerUser{UserID: 101}, Date: 10, Message: "s"})
	assert.Equal(t, "channel:30", mg.ChatNativeID)

	post := c.message(&tg.Message{ID: 5, Post: true, PeerID: &tg.PeerChannel{ChannelID: 20}, Date: 10, Message: "noticia"})
	assert.Equal(t, "channel:20", post.ChatNativeID)
	assert.Equal(t, "channel:20", post.SenderNativeID, "en un canal de difusión el emisor es el propio canal")
	assert.Equal(t, "Noticias", post.SenderName)
}

func TestConvertEditedAndForwarded(t *testing.T) {
	m := privMsg(9, "texto")
	m.EditDate = 1700000100
	m.SetFwdFrom(tg.MessageFwdHeader{FromName: "Alguien", Date: 5})
	got := newConv().message(m)
	assert.Equal(t, int64(1700000100000), got.EditedAt)
	assert.True(t, got.Forwarded)

	// EditHide: no se marca como editado.
	m2 := privMsg(10, "texto")
	m2.EditDate, m2.EditHide = 1700000100, true
	assert.Zero(t, newConv().message(m2).EditedAt)
}

// ---------------------------------------------------------------------------------------------
// Menciones y citas

func TestUTF16Slice(t *testing.T) {
	s := "😀 @juan hola"
	assert.Equal(t, "@juan", utf16Slice(s, 3, 5)) // el emoji ocupa 2 unidades UTF-16
	assert.Equal(t, "", utf16Slice(s, 100, 2))
	assert.Equal(t, "hola", utf16Slice(s, 9, 50))
	assert.Equal(t, "", utf16Slice(s, -1, 2))
}

func TestConvertMentions(t *testing.T) {
	m := privMsg(11, "😀 @luis y Ana")
	m.Entities = []tg.MessageEntityClass{
		&tg.MessageEntityMention{Offset: 3, Length: 5},
		&tg.MessageEntityMentionName{Offset: 11, Length: 3, UserID: otherUID},
		&tg.MessageEntityMentionName{Offset: 11, Length: 3, UserID: otherUID}, // repetida
		&tg.MessageEntityBold{Offset: 0, Length: 2},
	}
	got := newConv().message(m)
	assert.Equal(t, []string{"user:101", "user:100"}, got.Mentions)

	// Una @mención de alguien que no se conoce se omite.
	m2 := privMsg(12, "@nadie")
	m2.Entities = []tg.MessageEntityClass{&tg.MessageEntityMention{Offset: 0, Length: 6}}
	assert.Empty(t, newConv().message(m2).Mentions)
}

func TestConvertReplyQuote(t *testing.T) {
	m := privMsg(20, "respuesta")
	m.ReplyTo = &tg.MessageReplyHeader{ReplyToMsgID: 7, QuoteText: "  texto   citado "}
	got := newConv().message(m)
	assert.Equal(t, "7", got.QuotedNativeID)
	assert.Equal(t, "texto citado", got.QuotedPreview)

	// Con el medio citado dentro del encabezado.
	m = privMsg(21, "r")
	m.ReplyTo = &tg.MessageReplyHeader{ReplyToMsgID: 8, ReplyMedia: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 1}}}
	got = newConv().message(m)
	assert.Equal(t, "📷 Foto", got.QuotedPreview)

	// Con el emisor del encabezado y el gancho de mensajes conocidos.
	c := newConv()
	c.quote = func(chat string, id int) (string, string, bool) {
		if chat == "user:100" && id == 9 {
			return "hola que tal", "Ana Pérez", true
		}
		return "", "", false
	}
	m = privMsg(22, "r")
	m.ReplyTo = &tg.MessageReplyHeader{ReplyToMsgID: 9}
	got = c.message(m)
	assert.Equal(t, "hola que tal", got.QuotedPreview)
	assert.Equal(t, "Ana Pérez", got.QuotedSender)

	// Mensaje de tema de foro (no es una respuesta real): sin cita.
	m = privMsg(23, "r")
	m.ReplyTo = &tg.MessageReplyHeader{ReplyToMsgID: 3, ForumTopic: true}
	assert.Empty(t, newConv().message(m).QuotedNativeID)
}

func TestConvertReplyToStory(t *testing.T) {
	m := privMsg(24, "me gustó")
	m.ReplyTo = &tg.MessageReplyStoryHeader{Peer: &tg.PeerUser{UserID: otherUID}, StoryID: 15}
	got := newConv().message(m)
	assert.Equal(t, "user:100/15", got.FromStatusID)
	assert.Empty(t, got.QuotedNativeID)
}

// ---------------------------------------------------------------------------------------------
// Fotos y documentos

func samplePhoto() *tg.Photo {
	return &tg.Photo{
		ID: 11, AccessHash: 22, FileReference: []byte{1, 2, 3}, DCID: 4,
		Sizes: []tg.PhotoSizeClass{
			&tg.PhotoStrippedSize{Type: "i", Bytes: []byte{1, 40, 40, 0xAA, 0xBB}},
			&tg.PhotoSize{Type: "m", W: 320, H: 240, Size: 1000},
			&tg.PhotoSizeProgressive{Type: "y", W: 1280, H: 960, Sizes: []int{500, 2000, 45000}},
			&tg.PhotoSize{Type: "x", W: 800, H: 600, Size: 9000},
		},
	}
}

func TestConvertPhoto(t *testing.T) {
	m := privMsg(30, "pie de foto")
	m.Media = &tg.MessageMediaPhoto{Photo: samplePhoto()}
	got := newConv().message(m)
	require.Equal(t, model.KindImage, got.Kind)
	assert.Equal(t, "pie de foto", got.Text)
	require.NotNil(t, got.Media)
	assert.Equal(t, mediaID("user:100", "30"), got.Media.ID)
	assert.Equal(t, "image/jpeg", got.Media.Mime)
	assert.Equal(t, 1280, got.Media.Width)
	assert.Equal(t, 960, got.Media.Height)
	assert.Equal(t, int64(45000), got.Media.Size)
	assert.True(t, strings.HasPrefix(got.Media.Thumb, "data:image/jpeg;base64,"))
	assert.Equal(t, "/9j/", strings.TrimPrefix(got.Media.Thumb, "data:image/jpeg;base64,")[:4]) // cabecera JPEG expandida

	h, ok := parseHandle(got.Media.Handle)
	require.True(t, ok)
	assert.Equal(t, "photo", h.T)
	assert.Equal(t, int64(11), h.ID)
	assert.Equal(t, int64(22), h.AH)
	assert.Equal(t, []byte{1, 2, 3}, h.fileRef())
	assert.Equal(t, "y", h.TS, "se descarga el tamaño mayor")
	assert.Equal(t, 4, h.DC)
	assert.Equal(t, "user:100", h.C)
	assert.Equal(t, 30, h.M)
	loc, ok := h.location().(*tg.InputPhotoFileLocation)
	require.True(t, ok)
	assert.Equal(t, int64(11), loc.ID)
	assert.Equal(t, "y", loc.ThumbSize)
	assert.Equal(t, "📷 Foto", model.Preview(model.Message{Kind: got.Kind, Media: got.Media}))
}

func docMsg(id int, doc *tg.Document, mm *tg.MessageMediaDocument) *tg.Message {
	if mm == nil {
		mm = &tg.MessageMediaDocument{}
	}
	mm.Document = doc
	m := privMsg(id, "")
	m.Media = mm
	return m
}

func TestConvertDocumentKinds(t *testing.T) {
	base := func(attrs ...tg.DocumentAttributeClass) *tg.Document {
		return &tg.Document{ID: 50, AccessHash: 51, FileReference: []byte{9}, DCID: 2, MimeType: "application/octet-stream", Size: 1234, Attributes: attrs}
	}
	tests := []struct {
		name     string
		doc      *tg.Document
		mm       *tg.MessageMediaDocument
		kind     model.ContentKind
		fileName string
		dur      float64
		w, h     int
		preview  string
	}{
		{"sticker", base(&tg.DocumentAttributeSticker{Alt: "😀"}, &tg.DocumentAttributeImageSize{W: 512, H: 512}), nil, model.KindSticker, "", 0, 512, 512, "Sticker"},
		{"sticker en vídeo", base(&tg.DocumentAttributeVideo{W: 512, H: 512, Duration: 2}, &tg.DocumentAttributeSticker{}), nil, model.KindSticker, "", 2, 512, 512, "Sticker"},
		{"nota de voz", base(&tg.DocumentAttributeAudio{Voice: true, Duration: 7}), nil, model.KindVoice, "", 7, 0, 0, "🎤 Nota de voz"},
		{"audio", base(&tg.DocumentAttributeAudio{Duration: 200, Title: "Canción", Performer: "Grupo"}), nil, model.KindAudio, "Grupo - Canción", 200, 0, 0, "🎵 Audio"},
		{"audio con nombre de archivo", base(&tg.DocumentAttributeAudio{Duration: 9}, &tg.DocumentAttributeFilename{FileName: "a.mp3"}), nil, model.KindAudio, "a.mp3", 9, 0, 0, "🎵 Audio"},
		{"vídeo", base(&tg.DocumentAttributeVideo{W: 1920, H: 1080, Duration: 12.5}), nil, model.KindVideo, "", 12.5, 1920, 1080, "🎥 Vídeo"},
		{"vídeo redondo", base(&tg.DocumentAttributeVideo{RoundMessage: true, W: 384, H: 384, Duration: 5}), nil, model.KindVideo, "", 5, 384, 384, "🎥 Vídeo"},
		{"gif animado", base(&tg.DocumentAttributeAnimated{}), nil, model.KindVideo, "", 0, 0, 0, "🎥 Vídeo"},
		{"gif con vídeo", base(&tg.DocumentAttributeVideo{W: 200, H: 100, Duration: 1}, &tg.DocumentAttributeAnimated{}), nil, model.KindVideo, "", 1, 200, 100, "🎥 Vídeo"},
		{"documento", base(&tg.DocumentAttributeFilename{FileName: "informe.pdf"}), nil, model.KindDocument, "informe.pdf", 0, 0, 0, "📄 informe.pdf"},
		{"documento sin nombre", base(), nil, model.KindDocument, "", 0, 0, 0, "📄 Documento"},
		{"voz solo por indicador", base(), &tg.MessageMediaDocument{Voice: true}, model.KindVoice, "", 0, 0, 0, "🎤 Nota de voz"},
		{"vídeo solo por indicador", base(), &tg.MessageMediaDocument{Round: true}, model.KindVideo, "", 0, 0, 0, "🎥 Vídeo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := newConv().message(docMsg(60, tc.doc, tc.mm))
			require.Equal(t, tc.kind, got.Kind)
			require.NotNil(t, got.Media)
			assert.Equal(t, tc.fileName, got.Media.FileName)
			assert.Equal(t, tc.dur, got.Media.Duration)
			assert.Equal(t, tc.w, got.Media.Width)
			assert.Equal(t, tc.h, got.Media.Height)
			assert.Equal(t, int64(1234), got.Media.Size)
			assert.Equal(t, "application/octet-stream", got.Media.Mime)
			assert.Equal(t, tc.preview, model.Preview(got))
			h, ok := parseHandle(got.Media.Handle)
			require.True(t, ok)
			assert.Equal(t, "doc", h.T)
			assert.Equal(t, int64(50), h.ID)
			assert.Equal(t, int64(51), h.AH)
			dl, ok := h.location().(*tg.InputDocumentFileLocation)
			require.True(t, ok)
			assert.Equal(t, int64(50), dl.ID)
			assert.Equal(t, []byte{9}, dl.FileReference)
		})
	}
}

func TestConvertDocumentThumbAndCaption(t *testing.T) {
	d := &tg.Document{ID: 1, Thumbs: []tg.PhotoSizeClass{&tg.PhotoStrippedSize{Type: "i", Bytes: []byte{1, 10, 10, 1, 2, 3}}}}
	m := docMsg(61, d, nil)
	m.Message = "mira esto"
	got := newConv().message(m)
	assert.Equal(t, "mira esto", got.Text)
	assert.True(t, strings.HasPrefix(got.Media.Thumb, "data:image/jpeg;base64,"))
}

func TestMediaHandleRoundTrip(t *testing.T) {
	h := mediaHandle{T: "doc", ID: 5, AH: 6, FR: base64.StdEncoding.EncodeToString([]byte{7, 8}), DC: 3, C: "chat:7", M: 12, Sz: 99}
	got, ok := parseHandle(h.encode())
	require.True(t, ok)
	assert.Equal(t, h, got)
	for _, bad := range [][]byte{nil, []byte("x"), []byte(`{"t":"zip","id":1}`), []byte(`{"t":"doc"}`)} {
		_, ok := parseHandle(bad)
		assert.False(t, ok, string(bad))
	}
	// Un handle con S pertenece a una historia.
	m := &model.Media{Handle: h.encode()}
	setHandleStory(m, "user:100", 4)
	sh, _ := parseHandle(m.Handle)
	assert.Equal(t, 4, sh.S)
	assert.Equal(t, "user:100", sh.C)
	assert.Zero(t, sh.M)
}

// ---------------------------------------------------------------------------------------------
// Ubicación, contacto, encuesta y no compatibles

func TestConvertLocation(t *testing.T) {
	m := privMsg(70, "")
	m.Media = &tg.MessageMediaGeo{Geo: &tg.GeoPoint{Lat: -0.18, Long: -78.47}}
	got := newConv().message(m)
	require.Equal(t, model.KindLocation, got.Kind)
	assert.Equal(t, model.Location{Lat: -0.18, Lng: -78.47}, *got.Location)
	assert.Equal(t, "📍 Ubicación", model.Preview(got))

	m.Media = &tg.MessageMediaGeoLive{Geo: &tg.GeoPoint{Lat: 1, Long: 2}, Period: 900}
	got = newConv().message(m)
	require.Equal(t, model.KindLocation, got.Kind)
	assert.True(t, got.Location.Live)

	m.Media = &tg.MessageMediaVenue{Geo: &tg.GeoPoint{Lat: 3, Long: 4}, Title: "Café", Address: "Calle 1"}
	got = newConv().message(m)
	assert.Equal(t, model.Location{Lat: 3, Lng: 4, Name: "Café", Address: "Calle 1"}, *got.Location)

	m.Media = &tg.MessageMediaGeo{Geo: &tg.GeoPointEmpty{}}
	assert.Equal(t, model.KindUnsupported, newConv().message(m).Kind)
}

func TestConvertContact(t *testing.T) {
	m := privMsg(71, "")
	m.Media = &tg.MessageMediaContact{PhoneNumber: "593991234567", FirstName: "Luis", LastName: "Mora", Vcard: "BEGIN:VCARD"}
	got := newConv().message(m)
	require.Equal(t, model.KindContact, got.Kind)
	require.Len(t, got.Contacts, 1)
	assert.Equal(t, model.ContactCard{Name: "Luis Mora", Phone: "+593991234567", VCard: "BEGIN:VCARD"}, got.Contacts[0])
	assert.Equal(t, "👤 Luis Mora", model.Preview(got))
}

func samplePoll() tg.Poll {
	return tg.Poll{
		ID: 900, MultipleChoice: true, Question: tg.TextWithEntities{Text: "¿Cuándo?"},
		Answers: []tg.PollAnswerClass{
			&tg.PollAnswer{Text: tg.TextWithEntities{Text: "Hoy"}, Option: []byte("0")},
			&tg.PollAnswer{Text: tg.TextWithEntities{Text: "Mañana"}, Option: []byte("1")},
			&tg.PollAnswer{Text: tg.TextWithEntities{Text: "Nunca"}, Option: []byte("2")},
		},
	}
}

func TestConvertPoll(t *testing.T) {
	m := privMsg(72, "")
	m.Media = &tg.MessageMediaPoll{Poll: samplePoll(), Results: tg.PollResults{
		Results: []tg.PollAnswerVoters{
			{Option: []byte("0"), Voters: 3, Chosen: true, RecentVoters: []tg.PeerClass{&tg.PeerUser{UserID: otherUID}}},
			{Option: []byte("1"), Voters: 1},
		},
		TotalVoters: 4,
	}}
	got := newConv().message(m)
	require.Equal(t, model.KindPoll, got.Kind)
	require.NotNil(t, got.Poll)
	assert.Equal(t, "¿Cuándo?", got.Poll.Question)
	assert.True(t, got.Poll.Multi)
	require.Len(t, got.Poll.Options, 3)
	assert.Equal(t, "Hoy", got.Poll.Options[0].Name)
	// Hoy: Ana (reciente), yo (elegida) y un voto anónimo más hasta los 3.
	assert.Equal(t, []string{"user:100", "user:1", "anon"}, got.Poll.Options[0].Voters)
	assert.Equal(t, []string{"anon"}, got.Poll.Options[1].Voters)
	assert.Empty(t, got.Poll.Options[2].Voters)
	assert.Equal(t, "📊 ¿Cuándo?", model.Preview(got))

	rec := recordOfPoll(samplePoll())
	assert.Equal(t, "¿Cuándo?", rec.Question)
	require.Len(t, rec.Options, 3)
	assert.Equal(t, []byte("2"), rec.Options[2].Opt)
}

func TestConvertUnsupported(t *testing.T) {
	m := privMsg(73, "mira")
	m.Media = &tg.MessageMediaUnsupported{}
	got := newConv().message(m)
	assert.Equal(t, model.KindUnsupported, got.Kind)
	assert.Equal(t, "mira", got.Text, "se conserva el texto que traiga")

	m.Media = &tg.MessageMediaGame{}
	assert.Equal(t, model.KindUnsupported, newConv().message(m).Kind)

	// Foto caducada (sin foto).
	m.Media = &tg.MessageMediaPhoto{Photo: &tg.PhotoEmpty{}}
	assert.Equal(t, model.KindUnsupported, newConv().message(m).Kind)

	// Sin texto: «Mensaje no compatible».
	m.Message = ""
	assert.Equal(t, "Mensaje no compatible", model.Preview(newConv().message(m)))
}

func TestConvertWebPageIsText(t *testing.T) {
	m := privMsg(74, "mira https://ejemplo.com")
	m.Media = &tg.MessageMediaWebPage{}
	got := newConv().message(m)
	assert.Equal(t, model.KindText, got.Kind)
	assert.Nil(t, got.Media)
}

// ---------------------------------------------------------------------------------------------
// Reacciones

func TestConvertReactions(t *testing.T) {
	m := privMsg(80, "x")
	m.Reactions = tg.MessageReactions{RecentReactions: []tg.MessagePeerReaction{
		{PeerID: &tg.PeerUser{UserID: otherUID}, Date: 1700000050, Reaction: &tg.ReactionEmoji{Emoticon: "👍"}},
		{PeerID: &tg.PeerUser{UserID: 101}, Date: 1700000051, Reaction: &tg.ReactionCustomEmoji{DocumentID: 1}},
		{PeerID: &tg.PeerUser{UserID: otherUID}, My: true, Date: 1700000052, Reaction: &tg.ReactionEmoji{Emoticon: "❤"}},
	}}
	got := newConv().message(m)
	require.Len(t, got.Reactions, 2, "la reacción personalizada se omite")
	assert.Equal(t, model.Reaction{SenderNativeID: "user:100", SenderName: "Ana Pérez", Emoji: "👍", At: 1700000050000}, got.Reactions[0])
	assert.Equal(t, "user:1", got.Reactions[1].SenderNativeID, "la mía lleva mi id")
}

// ---------------------------------------------------------------------------------------------
// Mensajes de servicio

func svc(action tg.MessageActionClass, from tg.PeerClass) *tg.MessageService {
	return &tg.MessageService{ID: 90, PeerID: &tg.PeerChat{ChatID: 7}, FromID: from, Date: 1700000000, Action: action}
}

func TestConvertServiceMessages(t *testing.T) {
	ana := &tg.PeerUser{UserID: otherUID}
	luis := int64(101)
	tests := []struct {
		name   string
		action tg.MessageActionClass
		from   tg.PeerClass
		text   string
	}{
		{"crear grupo", &tg.MessageActionChatCreate{Title: "Familia", Users: []int64{luis}}, ana, "Ana Pérez creó el grupo «Familia»"},
		{"cambiar título", &tg.MessageActionChatEditTitle{Title: "Casa"}, ana, "Ana Pérez cambió el nombre del grupo a «Casa»"},
		{"añadir a otro", &tg.MessageActionChatAddUser{Users: []int64{luis}}, ana, "Ana Pérez añadió a Luis"},
		{"añadirse", &tg.MessageActionChatAddUser{Users: []int64{otherUID}}, ana, "Ana Pérez se unió al grupo"},
		{"sacar a otro", &tg.MessageActionChatDeleteUser{UserID: luis}, ana, "Ana Pérez eliminó a Luis"},
		{"salir", &tg.MessageActionChatDeleteUser{UserID: otherUID}, ana, "Ana Pérez salió del grupo"},
		{"enlace", &tg.MessageActionChatJoinedByLink{InviterID: luis}, ana, "Ana Pérez se unió con un enlace de invitación"},
		{"canal", &tg.MessageActionChannelCreate{Title: "Noticias"}, nil, "Se creó el canal «Noticias»"},
		{"foto", &tg.MessageActionChatEditPhoto{}, ana, "Ana Pérez cambió la foto del grupo"},
		{"quitar foto", &tg.MessageActionChatDeletePhoto{}, ana, "Ana Pérez eliminó la foto del grupo"},
		{"fijar", &tg.MessageActionPinMessage{}, ana, "Ana Pérez fijó un mensaje"},
		{"sin autor (lo hace el propio chat)", &tg.MessageActionPinMessage{}, nil, "Familia fijó un mensaje"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sm := svc(tc.action, tc.from)
			m, ok := newConv().service(sm)
			require.True(t, ok)
			assert.Equal(t, model.KindSystem, m.Kind)
			assert.Equal(t, tc.text, m.Text)
			assert.Equal(t, "chat:7", m.ChatNativeID)
			assert.Equal(t, "90", m.NativeID)
			assert.Equal(t, int64(1700000000000), m.Timestamp)
			assert.Equal(t, tc.text, model.Preview(m))
		})
	}
	// Una acción que no se muestra.
	_, ok := newConv().service(svc(&tg.MessageActionHistoryClear{}, ana))
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------------------------
// Personas, escribiendo e historias

func TestPersonOf(t *testing.T) {
	e := testEntities()
	ana := personOf(testAcc, e.Users[otherUID])
	assert.Equal(t, model.Person{AccountID: testAcc, NativeID: "user:100", Name: "Ana Pérez", Phone: "+593991234567", Username: "ana"}, ana)
	luis := personOf(testAcc, e.Users[101])
	assert.Equal(t, "", luis.Name)
	assert.Equal(t, "Luis", luis.PushName)
	assert.Equal(t, "Luis", luis.Display())

	// @usuario desde la lista de nombres de usuario.
	u := &tg.User{ID: 5, Usernames: []tg.Username{{Username: "viejo"}, {Username: "nuevo", Active: true}}}
	assert.Equal(t, "nuevo", personOf(testAcc, u).Username)
	assert.Equal(t, "@nuevo", userDisplay(u))
	assert.Equal(t, "Cuenta eliminada", userDisplay(&tg.User{ID: 6, Deleted: true}))
	assert.Equal(t, "+593111", userDisplay(&tg.User{ID: 7, Phone: "593111"}))
}

func TestTypingFromAction(t *testing.T) {
	cases := []struct {
		a         tg.SendMessageActionClass
		typing    bool
		recording bool
	}{
		{&tg.SendMessageTypingAction{}, true, false},
		{&tg.SendMessageCancelAction{}, false, false},
		{&tg.SendMessageRecordAudioAction{}, false, true},
		{&tg.SendMessageRecordVideoAction{}, false, true},
		{&tg.SendMessageRecordRoundAction{}, false, true},
		{&tg.SendMessageUploadPhotoAction{}, true, false},
		{&tg.SpeakingInGroupCallAction{}, false, false},
	}
	for _, c := range cases {
		ty, rec := typingFromAction(c.a)
		assert.Equal(t, c.typing, ty, "%T", c.a)
		assert.Equal(t, c.recording, rec, "%T", c.a)
	}
}

func TestStoryNativeIDs(t *testing.T) {
	assert.Equal(t, "user:100/15", storyNative("user:100", 15))
	a, id, ok := parseStoryNative("user:100/15")
	assert.True(t, ok)
	assert.Equal(t, "user:100", a)
	assert.Equal(t, 15, id)
	for _, bad := range []string{"", "user:100", "/5", "user:100/x", "user:100/0"} {
		_, _, ok := parseStoryNative(bad)
		assert.False(t, ok, bad)
	}
}

func TestConvertStory(t *testing.T) {
	it := &tg.StoryItem{
		ID: 15, Date: 1700000000, ExpireDate: 1700086400, Caption: "hoy",
		Media: &tg.MessageMediaPhoto{Photo: samplePhoto()},
		Views: tg.StoryViews{ViewsCount: 12, ReactionsCount: 3},
	}
	st := newConv().storyOf(&tg.PeerUser{UserID: otherUID}, it)
	assert.Equal(t, "tg-test|user:100/15", st.Key)
	assert.Equal(t, "user:100/15", st.NativeID)
	assert.Equal(t, "user:100", st.AuthorNativeID)
	assert.Equal(t, "Ana Pérez", st.AuthorName)
	assert.False(t, st.FromMe)
	assert.Equal(t, int64(1700000000000), st.PostedAt)
	assert.Equal(t, int64(1700086400000), st.ExpiresAt)
	assert.Equal(t, model.KindImage, st.Kind)
	assert.Equal(t, "hoy", st.Text)
	assert.Equal(t, 12, st.ViewCount)
	assert.Equal(t, 3, st.ReactionCount)
	require.NotNil(t, st.Media)
	assert.Equal(t, mediaID("user:100", "story15"), st.Media.ID)
	h, _ := parseHandle(st.Media.Handle)
	assert.Equal(t, 15, h.S)

	// Propia y de vídeo.
	vid := &tg.StoryItem{ID: 16, Out: true, Date: 1, ExpireDate: 2, Media: &tg.MessageMediaDocument{Document: &tg.Document{
		ID: 3, MimeType: "video/mp4", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeVideo{W: 720, H: 1280}},
	}}}
	own := newConv().storyOf(&tg.PeerUser{UserID: selfUID}, vid)
	assert.True(t, own.FromMe)
	assert.Equal(t, model.KindVideo, own.Kind)
}

func TestPollFromRecordWithoutResults(t *testing.T) {
	rec := recordOfPoll(samplePoll())
	p := newConv().pollFromRecord(rec, tg.PollResults{})
	require.Len(t, p.Options, 3)
	for _, o := range p.Options {
		assert.Empty(t, o.Voters)
	}
}
