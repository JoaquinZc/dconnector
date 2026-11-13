package whatsapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
)

const (
	testAcc     = model.AccountID("wa-test")
	testTs      = int64(1_700_000_000_000)
	pnPeer      = "593991111111@s.whatsapp.net"
	lidPeer     = "111@lid"
	groupID     = "120363000000000001@g.us"
	testSelfPN  = "593990000001"
	testSelfLID = "2000000000001"
)

var (
	jSelf    = types.NewJID(testSelfPN, types.DefaultUserServer)
	jSelfLID = types.NewJID(testSelfLID, types.HiddenUserServer)
	jPeer    = types.NewJID("593991111111", types.DefaultUserServer)
	jPeerLID = types.NewJID("111", types.HiddenUserServer)
	jOther   = types.NewJID("593992222222", types.DefaultUserServer)
	jGroup   = types.NewJID("120363000000000001", types.GroupServer)
)

func testConv() converter {
	lids := map[types.JID]types.JID{jPeerLID: jPeer}
	names := map[string]string{"593991111111": "Ana", "593992222222": "Beto"}
	return converter{
		acc: testAcc, self: jSelf, selfLID: jSelfLID,
		resolve: func(j types.JID) types.JID { return lids[j.ToNonAD()] },
		name:    func(j types.JID) string { return names[j.User] },
		now:     func() time.Time { return time.UnixMilli(testTs) },
	}
}

func infoDM(id string, fromMe bool) types.MessageInfo {
	info := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: jPeer, Sender: jPeer, IsFromMe: fromMe},
		ID:            id, PushName: "Ana P", Timestamp: time.UnixMilli(testTs),
	}
	if fromMe {
		info.Sender = jSelf
	}
	return info
}

func infoGroup(id string, sender types.JID) types.MessageInfo {
	return types.MessageInfo{
		MessageSource: types.MessageSource{Chat: jGroup, Sender: sender, IsGroup: true},
		ID:            id, PushName: "Beto B", Timestamp: time.UnixMilli(testTs),
	}
}

func txt(s string) *waE2E.Message { return &waE2E.Message{Conversation: proto.String(s)} }

func keyOf(id string) *waCommon.MessageKey { return &waCommon.MessageKey{ID: proto.String(id)} }

// ---------------------------------------------------------------------------------------------
// JID ↔ id nativo

func TestNativeToJID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"593991111111@s.whatsapp.net", "593991111111@s.whatsapp.net", false},
		{"593991111111:12@s.whatsapp.net", "593991111111@s.whatsapp.net", false}, // quita el dispositivo
		{"593991111111@c.us", "593991111111@s.whatsapp.net", false},              // servidor antiguo
		{"+593 99 111-1111", "593991111111@s.whatsapp.net", false},               // número suelto
		{"593991111111", "593991111111@s.whatsapp.net", false},
		{groupID, groupID, false},
		{"111@lid", "111@lid", false},
		{"120363@newsletter", "120363@newsletter", false},
		{"status@broadcast", "status@broadcast", false},
		{"", "", true},
		{"hola", "", true},
		{"12", "", true},
	}
	for _, tc := range cases {
		got, err := nativeToJID(tc.in)
		if tc.wantErr {
			assert.Error(t, err, tc.in)
			continue
		}
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, jidToNative(got), tc.in)
	}
	assert.Equal(t, "", jidToNative(types.EmptyJID))
}

func TestChatKindOf(t *testing.T) {
	assert.Equal(t, model.ChatGroup, chatKindOf(jGroup, jSelf, jSelfLID))
	assert.Equal(t, model.ChatChannel, chatKindOf(types.NewJID("1203", types.NewsletterServer), jSelf, jSelfLID))
	assert.Equal(t, model.ChatSelf, chatKindOf(jSelf, jSelf, jSelfLID))
	assert.Equal(t, model.ChatSelf, chatKindOf(jSelfLID, jSelf, jSelfLID))
	assert.Equal(t, model.ChatDirect, chatKindOf(jPeer, jSelf, jSelfLID))
	assert.Equal(t, model.ChatDirect, chatKindOf(jPeerLID, jSelf, jSelfLID))
}

func TestMediaIDAndShortID(t *testing.T) {
	a := mediaID("c", "m")
	assert.Len(t, a, 24)
	assert.Equal(t, a, mediaID("c", "m"))
	assert.NotEqual(t, a, mediaID("c", "m2"))
	assert.NotEqual(t, a, mediaID("c2", "m"))
	assert.Equal(t, "…1111", shortID(pnPeer))
	assert.Equal(t, "abc", shortID("abc"))
	assert.True(t, isStatusJID(types.StatusBroadcastJID))
	assert.False(t, isStatusJID(types.NewJID("123", types.BroadcastServer)))
}

// ---------------------------------------------------------------------------------------------
// Tabla de 07 §6: cada tipo de mensaje → model.Message

func TestConvertText(t *testing.T) {
	c := testConv()
	m := c.convertMessage(infoDM("M1", false), txt("Hola"))
	assert.Equal(t, model.KindText, m.Kind)
	assert.Equal(t, "Hola", m.Text)
	assert.Equal(t, "M1", m.NativeID)
	assert.Equal(t, pnPeer, m.ChatNativeID)
	assert.Equal(t, pnPeer, m.SenderNativeID)
	assert.Equal(t, "Ana P", m.SenderName)
	assert.False(t, m.FromMe)
	assert.Equal(t, testTs, m.Timestamp)
	assert.Equal(t, model.DeliverySent, m.Delivery)
	assert.Equal(t, model.NewMessageKey(testAcc, pnPeer, "M1"), m.Key)
	assert.Equal(t, model.ChannelWhatsApp, m.Channel)

	mine := c.convertMessage(infoDM("M2", true), txt("Yo"))
	assert.True(t, mine.FromMe)
	assert.Equal(t, jidToNative(jSelf), mine.SenderNativeID)
	assert.Equal(t, "", mine.SenderName)
}

func TestConvertExtendedTextWithQuoteMentionsAndForward(t *testing.T) {
	c := testConv()
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("Mira @Beto"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("Q1"),
			Participant:   proto.String(lidPeer),
			QuotedMessage: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")}},
			IsForwarded:   proto.Bool(true),
			MentionedJID:  []string{lidPeer, "593992222222@s.whatsapp.net"},
		},
	}}
	m := c.convertMessage(infoGroup("M3", jPeerLID), msg)
	assert.Equal(t, model.KindText, m.Kind)
	assert.Equal(t, "Mira @Beto", m.Text)
	assert.Equal(t, "Q1", m.QuotedNativeID)
	assert.Equal(t, "📷 Foto", m.QuotedPreview)
	assert.Equal(t, pnPeer, m.QuotedSender) // @lid → teléfono
	assert.True(t, m.Forwarded)
	assert.Equal(t, []string{pnPeer, "593992222222@s.whatsapp.net"}, m.Mentions)
	assert.Equal(t, groupID, m.ChatNativeID)
	assert.Equal(t, pnPeer, m.SenderNativeID) // emisor de grupo resuelto de @lid
}

func TestConvertQuotedText(t *testing.T) {
	c := testConv()
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("ok"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID: proto.String("Q2"), Participant: proto.String(pnPeer), QuotedMessage: txt("  texto\n   citado "),
		},
	}}
	m := c.convertMessage(infoDM("M", false), msg)
	assert.Equal(t, "texto citado", m.QuotedPreview)
}

func TestConvertStatusReply(t *testing.T) {
	c := testConv()
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String("qué bueno"),
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("S1"), RemoteJID: proto.String("status@broadcast")},
	}}
	m := c.convertMessage(infoDM("M", false), msg)
	assert.Equal(t, "S1", m.FromStatusID)
	assert.Equal(t, "", m.QuotedNativeID)
}

func TestConvertMediaKinds(t *testing.T) {
	c := testConv()
	info := infoDM("MED", false)
	chatID := pnPeer

	t.Run("image", func(t *testing.T) {
		im := &waE2E.ImageMessage{
			URL: proto.String("https://mmg/x"), Mimetype: proto.String("image/jpeg"), Caption: proto.String("hola"),
			FileLength: proto.Uint64(1234), Height: proto.Uint32(600), Width: proto.Uint32(800),
			JPEGThumbnail: []byte{1, 2, 3}, MediaKey: []byte("key"), DirectPath: proto.String("/v/p"),
			FileEncSHA256: []byte("enc"), FileSHA256: []byte("sha"),
			ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("zz")},
		}
		m := c.convertMessage(info, &waE2E.Message{ImageMessage: im})
		assert.Equal(t, model.KindImage, m.Kind)
		assert.Equal(t, "hola", m.Text)
		require.NotNil(t, m.Media)
		assert.Equal(t, mediaID(chatID, "MED"), m.Media.ID)
		assert.Equal(t, "image/jpeg", m.Media.Mime)
		assert.EqualValues(t, 1234, m.Media.Size)
		assert.Equal(t, 800, m.Media.Width)
		assert.Equal(t, 600, m.Media.Height)
		assert.Equal(t, "data:image/jpeg;base64,AQID", m.Media.Thumb)
		assert.Equal(t, byte(1), m.Media.Handle[0])

		// El Handle se deshace en un submensaje descargable, sin miniatura ni cita.
		sub, err := parseHandle(m.Media.Handle)
		require.NoError(t, err)
		got, ok := sub.(*waE2E.ImageMessage)
		require.True(t, ok)
		assert.Equal(t, []byte("key"), got.GetMediaKey())
		assert.Equal(t, "/v/p", got.GetDirectPath())
		assert.Equal(t, []byte("enc"), got.GetFileEncSHA256())
		assert.Equal(t, []byte("sha"), got.GetFileSHA256())
		assert.Empty(t, got.GetJPEGThumbnail())
		assert.Nil(t, got.GetContextInfo())
		assert.Equal(t, "", got.GetCaption())
		// Y el mensaje original no se modificó.
		assert.NotEmpty(t, im.GetJPEGThumbnail())
		assert.NotNil(t, im.GetContextInfo())
	})

	t.Run("video", func(t *testing.T) {
		vi := &waE2E.VideoMessage{
			Mimetype: proto.String("video/mp4"), Seconds: proto.Uint32(12), Caption: proto.String("clip"),
			Width: proto.Uint32(640), Height: proto.Uint32(360), FileLength: proto.Uint64(99),
			JPEGThumbnail: []byte{9}, MediaKey: []byte("k"), DirectPath: proto.String("/v"),
		}
		m := c.convertMessage(info, &waE2E.Message{VideoMessage: vi})
		assert.Equal(t, model.KindVideo, m.Kind)
		assert.Equal(t, "clip", m.Text)
		assert.Equal(t, 12.0, m.Media.Duration)
		assert.Equal(t, byte(2), m.Media.Handle[0])
		sub, err := parseHandle(m.Media.Handle)
		require.NoError(t, err)
		assert.Equal(t, []byte("k"), sub.GetMediaKey())
	})

	t.Run("voice and audio", func(t *testing.T) {
		voice := c.convertMessage(info, &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			PTT: proto.Bool(true), Seconds: proto.Uint32(7), Mimetype: proto.String("audio/ogg; codecs=opus"),
			MediaKey: []byte("k"), DirectPath: proto.String("/a"),
		}})
		assert.Equal(t, model.KindVoice, voice.Kind)
		assert.Equal(t, 7.0, voice.Media.Duration)
		assert.Equal(t, "audio/ogg; codecs=opus", voice.Media.Mime)
		assert.Equal(t, byte(3), voice.Media.Handle[0])

		audio := c.convertMessage(info, &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			Mimetype: proto.String("audio/mpeg"), Seconds: proto.Uint32(60),
		}})
		assert.Equal(t, model.KindAudio, audio.Kind)
	})

	t.Run("document", func(t *testing.T) {
		d := &waE2E.DocumentMessage{
			FileName: proto.String("informe.pdf"), Title: proto.String("Informe"), Caption: proto.String("adjunto"),
			Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(5000), MediaKey: []byte("k"),
		}
		m := c.convertMessage(info, &waE2E.Message{DocumentMessage: d})
		assert.Equal(t, model.KindDocument, m.Kind)
		assert.Equal(t, "adjunto", m.Text)
		assert.Equal(t, "informe.pdf", m.Media.FileName)
		assert.Equal(t, "application/pdf", m.Media.Mime)
		assert.Equal(t, byte(4), m.Media.Handle[0])
		// Sin FileName se usa Title.
		d2 := &waE2E.DocumentMessage{Title: proto.String("Solo título")}
		m2 := c.convertMessage(info, &waE2E.Message{DocumentMessage: d2})
		assert.Equal(t, "Solo título", m2.Media.FileName)
		assert.Equal(t, "📄 Solo título", model.Preview(m2))
	})

	t.Run("sticker", func(t *testing.T) {
		s := &waE2E.StickerMessage{Width: proto.Uint32(512), Height: proto.Uint32(512), PngThumbnail: []byte{7}, MediaKey: []byte("k")}
		m := c.convertMessage(info, &waE2E.Message{StickerMessage: s})
		assert.Equal(t, model.KindSticker, m.Kind)
		assert.Equal(t, "image/webp", m.Media.Mime) // por defecto
		assert.Equal(t, 512, m.Media.Width)
		assert.Equal(t, "data:image/png;base64,Bw==", m.Media.Thumb)
		assert.Equal(t, byte(5), m.Media.Handle[0])
		assert.Equal(t, "Sticker", model.Preview(m))
	})
}

func TestParseHandleErrors(t *testing.T) {
	_, err := parseHandle(nil)
	assert.Error(t, err)
	_, err = parseHandle([]byte{9, 1, 2})
	assert.Error(t, err)
	_, err = parseHandle([]byte{1, 0xff, 0xff, 0xff})
	assert.Error(t, err)
}

func TestConvertLocationContactPoll(t *testing.T) {
	c := testConv()
	info := infoDM("X", false)

	loc := c.convertMessage(info, &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
		DegreesLatitude: proto.Float64(-2.17), DegreesLongitude: proto.Float64(-79.92),
		Name: proto.String("Oficina"), Address: proto.String("Av. 9 de Octubre"), IsLive: proto.Bool(false),
	}})
	assert.Equal(t, model.KindLocation, loc.Kind)
	assert.Equal(t, &model.Location{Lat: -2.17, Lng: -79.92, Name: "Oficina", Address: "Av. 9 de Octubre"}, loc.Location)

	live := c.convertMessage(info, &waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{
		DegreesLatitude: proto.Float64(1), DegreesLongitude: proto.Float64(2), Caption: proto.String("voy en camino"),
	}})
	assert.Equal(t, model.KindLocation, live.Kind)
	assert.True(t, live.Location.Live)
	assert.Equal(t, "voy en camino", live.Text)

	vcard := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ana\r\nTEL;type=CELL;type=VOICE;waid=593991234567:+593 99 123 4567\r\nEND:VCARD"
	one := c.convertMessage(info, &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String("Ana"), Vcard: proto.String(vcard),
	}})
	assert.Equal(t, model.KindContact, one.Kind)
	require.Len(t, one.Contacts, 1)
	assert.Equal(t, "Ana", one.Contacts[0].Name)
	assert.Equal(t, "+593991234567", one.Contacts[0].Phone)
	assert.Equal(t, vcard, one.Contacts[0].VCard)

	many := c.convertMessage(info, &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{
		DisplayName: proto.String("2 contactos"),
		Contacts: []*waE2E.ContactMessage{
			{DisplayName: proto.String("A"), Vcard: proto.String("BEGIN:VCARD\nTEL:+1 (555) 010-9999\nEND:VCARD")},
			{DisplayName: proto.String("B"), Vcard: proto.String("BEGIN:VCARD\nitem1.TEL;waid=34600111222:+34 600 11 12 22\nEND:VCARD")},
			{DisplayName: proto.String("C"), Vcard: proto.String("BEGIN:VCARD\nFN:C\nEND:VCARD")},
		},
	}})
	require.Len(t, many.Contacts, 3)
	assert.Equal(t, "+15550109999", many.Contacts[0].Phone)
	assert.Equal(t, "+34600111222", many.Contacts[1].Phone)
	assert.Equal(t, "", many.Contacts[2].Phone)
	assert.Equal(t, "👤 A", model.Preview(many))

	poll := &waE2E.PollCreationMessage{
		Name: proto.String("¿Dónde comemos?"),
		Options: []*waE2E.PollCreationMessage_Option{
			{OptionName: proto.String("Pizza")}, {OptionName: proto.String("Sushi")},
		},
		SelectableOptionsCount: proto.Uint32(1),
	}
	for name, wrap := range map[string]*waE2E.Message{
		"v1": {PollCreationMessage: poll}, "v2": {PollCreationMessageV2: poll}, "v3": {PollCreationMessageV3: poll},
	} {
		m := c.convertMessage(info, wrap)
		require.Equal(t, model.KindPoll, m.Kind, name)
		assert.Equal(t, "¿Dónde comemos?", m.Poll.Question, name)
		assert.False(t, m.Poll.Multi, name)
		require.Len(t, m.Poll.Options, 2, name)
		assert.Equal(t, "Pizza", m.Poll.Options[0].Name, name)
		assert.Empty(t, m.Poll.Options[0].Voters, name)
	}
	multi := proto.Clone(poll).(*waE2E.PollCreationMessage)
	multi.SelectableOptionsCount = proto.Uint32(0)
	assert.True(t, c.convertMessage(info, &waE2E.Message{PollCreationMessage: multi}).Poll.Multi)
	multi.SelectableOptionsCount = proto.Uint32(2)
	assert.True(t, c.convertMessage(info, &waE2E.Message{PollCreationMessage: multi}).Poll.Multi)
}

func TestConvertUnsupported(t *testing.T) {
	c := testConv()
	m := c.convertMessage(infoDM("U", false), &waE2E.Message{MusicMessage: &waE2E.MusicMessage{}})
	assert.Equal(t, model.KindUnsupported, m.Kind)
	assert.Equal(t, "", m.Text)
	assert.Equal(t, "Mensaje no compatible", model.Preview(m))
	assert.Equal(t, model.KindUnsupported, c.convertMessage(infoDM("U2", false), nil).Kind)
}

func TestUnwrapEnvelopes(t *testing.T) {
	c := testConv()
	inner := txt("dentro")
	wrappers := map[string]*waE2E.Message{
		"ephemeral":  {EphemeralMessage: &waE2E.FutureProofMessage{Message: inner}},
		"viewonce":   {ViewOnceMessage: &waE2E.FutureProofMessage{Message: inner}},
		"viewoncev2": {ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: inner}},
		"viewonceX":  {ViewOnceMessageV2Extension: &waE2E.FutureProofMessage{Message: inner}},
		"docCaption": {DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: inner}},
		"deviceSent": {DeviceSentMessage: &waE2E.DeviceSentMessage{Message: inner}},
		"nested":     {EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: inner}}}},
	}
	for name, w := range wrappers {
		m := c.convertMessage(infoDM("W", false), w)
		assert.Equal(t, model.KindText, m.Kind, name)
		assert.Equal(t, "dentro", m.Text, name)
	}
	// Envoltorios que antes salían como «no compatible»: se desenvuelven.
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), MediaKey: []byte("k")}}
	for name, w := range map[string]*waE2E.Message{
		"albumChild": {AssociatedChildMessage: &waE2E.FutureProofMessage{Message: img}},
		"spoiler":    {SpoilerMessage: &waE2E.FutureProofMessage{Message: img}},
		"groupStat":  {GroupStatusMessageV2: &waE2E.FutureProofMessage{Message: img}},
		"audioStick": {AudioStickerMessage: &waE2E.FutureProofMessage{Message: img}},
	} {
		m := c.convertMessage(infoDM("W3", false), w)
		assert.Equal(t, model.KindImage, m.Kind, name)
		require.NotNil(t, m.Media, name)
	}
}

// ---------------------------------------------------------------------------------------------
// Tabla de 07 §6 (sin Kind) y §7: eventos

func TestClassifyReaction(t *testing.T) {
	c := testConv()
	evt := &events.Message{Info: infoDM("R1", false), Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
		Key: keyOf("M1"), Text: proto.String("👍"), SenderTimestampMS: proto.Int64(777),
	}}}
	in := c.classify(evt)
	require.Equal(t, inReaction, in.Kind)
	assert.Equal(t, model.NewMessageKey(testAcc, pnPeer, "M1"), in.Reaction.MessageKey)
	assert.Equal(t, "👍", in.Reaction.Reaction.Emoji)
	assert.Equal(t, pnPeer, in.Reaction.Reaction.SenderNativeID)
	assert.Equal(t, "Ana P", in.Reaction.Reaction.SenderName)
	assert.EqualValues(t, 777, in.Reaction.Reaction.At)

	// Texto vacío = reacción quitada.
	evt.Message.ReactionMessage.Text = proto.String("")
	in = c.classify(evt)
	require.Equal(t, inReaction, in.Kind)
	assert.Equal(t, "", in.Reaction.Reaction.Emoji)
}

func TestClassifyRevokeAndEdit(t *testing.T) {
	c := testConv()
	revoke := c.classify(&events.Message{Info: infoDM("P1", false), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: keyOf("M2"),
	}}})
	require.Equal(t, inRevoke, revoke.Kind)
	assert.True(t, revoke.Update.Deleted)
	assert.Equal(t, model.NewMessageKey(testAcc, pnPeer, "M2"), revoke.Update.Key)

	edit := c.classify(&events.Message{Info: infoDM("P2", false), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: keyOf("M3"),
		EditedMessage: txt("texto nuevo"), TimestampMS: proto.Int64(5000),
	}}})
	require.Equal(t, inEdit, edit.Kind)
	require.NotNil(t, edit.Update.Text)
	assert.Equal(t, "texto nuevo", *edit.Update.Text)
	assert.EqualValues(t, 5000, edit.Update.EditedAt)
	assert.Equal(t, model.NewMessageKey(testAcc, pnPeer, "M3"), edit.Update.Key)

	// La edición de un pie de foto también cuenta.
	capEdit := c.classify(&events.Message{Info: infoDM("P3", false), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: keyOf("M4"),
		EditedMessage: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("pie nuevo")}},
	}}})
	require.Equal(t, inEdit, capEdit.Kind)
	assert.Equal(t, "pie nuevo", *capEdit.Update.Text)

	// Edición ya desenvuelta por whatsmeow (historial): Info.ID es el mensaje original.
	hist := c.classify(&events.Message{Info: infoDM("M5", false), IsEdit: true, Message: txt("corregido")})
	require.Equal(t, inEdit, hist.Kind)
	assert.Equal(t, model.NewMessageKey(testAcc, pnPeer, "M5"), hist.Update.Key)
	assert.Equal(t, "corregido", *hist.Update.Text)

	// EditedMessage sin desenvolver: se desenvuelve primero y cae en MESSAGE_EDIT.
	wrapped := c.classify(&events.Message{Info: infoDM("P6", false), Message: &waE2E.Message{
		EditedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: keyOf("M6"), EditedMessage: txt("v2"),
		}}},
	}})
	require.Equal(t, inEdit, wrapped.Kind)
	assert.Equal(t, "v2", *wrapped.Update.Text)
}

func TestClassifyIgnored(t *testing.T) {
	c := testConv()
	assert.Equal(t, inIgnore, c.classify(nil).Kind)
	assert.Equal(t, inIgnore, c.classify(&events.Message{Info: infoDM("N", false)}).Kind)
	other := c.classify(&events.Message{Info: infoDM("O", false), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_EPHEMERAL_SETTING.Enum(),
	}}})
	assert.Equal(t, inIgnore, other.Kind)
	skd := c.classify(&events.Message{Info: infoDM("S", false), Message: &waE2E.Message{
		SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{GroupID: proto.String("g")},
	}})
	assert.Equal(t, inIgnore, skd.Kind)
	// Reacción sin id objetivo.
	assert.Equal(t, inIgnore, c.classify(&events.Message{Info: infoDM("Z", false), Message: &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("x")},
	}}).Kind)
}

func TestClassifyPollVote(t *testing.T) {
	c := testConv()
	in := c.classify(&events.Message{Info: infoDM("V1", false), Message: &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{
		PollCreationMessageKey: keyOf("POLL1"),
	}}})
	require.Equal(t, inPollVote, in.Kind)
	assert.Equal(t, "POLL1", in.PollTarget)
	assert.Equal(t, pnPeer, in.ChatID)
}

func TestClassifyUnsupportedStillAMessage(t *testing.T) {
	c := testConv()
	in := c.classify(&events.Message{Info: infoDM("U", false), Message: &waE2E.Message{MusicMessage: &waE2E.MusicMessage{}}})
	require.Equal(t, inMessage, in.Kind)
	assert.Equal(t, model.KindUnsupported, in.Message.Kind)
}

// ---------------------------------------------------------------------------------------------
// Identidades: @lid ↔ teléfono (decisión C6), chat propio, difusión

func TestLIDResolution(t *testing.T) {
	c := testConv()

	// Chat directo que llega como @lid con teléfono conocido.
	info := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: jPeerLID, Sender: jPeerLID},
		ID:            "L1", Timestamp: time.UnixMilli(testTs),
	}
	m := c.convertMessage(info, txt("hola"))
	assert.Equal(t, pnPeer, m.ChatNativeID)
	assert.Equal(t, pnPeer, m.SenderNativeID)

	// @lid desconocido: se usa el JID alternativo que trae el mensaje.
	unknown := types.NewJID("999", types.HiddenUserServer)
	info2 := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: unknown, Sender: unknown, SenderAlt: jOther},
		ID:            "L2", Timestamp: time.UnixMilli(testTs),
	}
	m2 := c.convertMessage(info2, txt("hola"))
	assert.Equal(t, jidToNative(jOther), m2.ChatNativeID)
	assert.Equal(t, jidToNative(jOther), m2.SenderNativeID)

	// Mensaje propio a un chat @lid: el destinatario alterno resuelve el chat.
	info3 := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: unknown, Sender: jSelfLID, IsFromMe: true, RecipientAlt: jOther},
		ID:            "L3", Timestamp: time.UnixMilli(testTs),
	}
	m3 := c.convertMessage(info3, txt("yo"))
	assert.Equal(t, jidToNative(jOther), m3.ChatNativeID)
	assert.Equal(t, jidToNative(jSelf), m3.SenderNativeID)

	// Sin forma de resolver: el @lid queda como id (C5).
	info4 := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: unknown, Sender: unknown},
		ID:            "L4", Timestamp: time.UnixMilli(testTs),
	}
	assert.Equal(t, "999@lid", c.convertMessage(info4, txt("x")).ChatNativeID)

	// Mi propio @lid es mi teléfono.
	assert.Equal(t, jSelf, c.lidToPN(jSelfLID))
}

func TestSelfChatAndBroadcastList(t *testing.T) {
	c := testConv()
	self := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: jSelf, Sender: jSelf, IsFromMe: true},
		ID:            "SELF", Timestamp: time.UnixMilli(testTs),
	}
	m := c.convertMessage(self, txt("nota"))
	assert.Equal(t, jidToNative(jSelf), m.ChatNativeID)
	assert.Equal(t, model.ChatSelf, chatKindOf(c.chatJID(self), c.self, c.selfLID))

	// Difusión recibida: aparece en el chat directo con quien la envió.
	bc := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: types.NewJID("12345", types.BroadcastServer), Sender: jPeer},
		ID:            "BC", Timestamp: time.UnixMilli(testTs),
	}
	in := c.classify(&events.Message{Info: bc, Message: txt("promo")})
	require.Equal(t, inMessage, in.Kind)
	assert.Equal(t, pnPeer, in.Message.ChatNativeID)

	// Mi propia difusión a una lista: no es un chat que se muestre.
	own := bc
	own.IsFromMe = true
	own.Sender = jSelf
	assert.Equal(t, inIgnore, c.classify(&events.Message{Info: own, Message: txt("promo")}).Kind)
}

// ---------------------------------------------------------------------------------------------
// Estados (status@broadcast)

func statusInfo(id string, fromMe bool) types.MessageInfo {
	info := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: jPeer, IsFromMe: fromMe},
		ID:            id, PushName: "Ana P", Timestamp: time.UnixMilli(testTs),
	}
	if fromMe {
		info.Sender = jSelf
	}
	return info
}

func TestClassifyStatusText(t *testing.T) {
	c := testConv()
	in := c.classify(&events.Message{Info: statusInfo("ST1", false), Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("buenos días"), BackgroundArgb: proto.Uint32(0xFF112233), Font: waE2E.ExtendedTextMessage_FB_SCRIPT.Enum(),
	}}})
	require.Equal(t, inStatus, in.Kind)
	st := in.Status
	assert.Equal(t, string(testAcc)+"|ST1", st.Key)
	assert.Equal(t, "ST1", st.NativeID)
	assert.Equal(t, pnPeer, st.AuthorNativeID)
	assert.Equal(t, "Ana P", st.AuthorName)
	assert.False(t, st.FromMe)
	assert.Equal(t, model.KindText, st.Kind)
	assert.Equal(t, "buenos días", st.Text)
	assert.Equal(t, "#112233", st.BgColor)
	assert.Equal(t, 2, st.Font)
	assert.Equal(t, testTs, st.PostedAt)
	assert.Equal(t, testTs+int64(24*time.Hour/time.Millisecond), st.ExpiresAt)

	// Mi propio estado (publicado desde el teléfono).
	mine := c.classify(&events.Message{Info: statusInfo("ST2", true), Message: txt("yo")})
	require.Equal(t, inStatus, mine.Kind)
	assert.True(t, mine.Status.FromMe)
	assert.Equal(t, jidToNative(jSelf), mine.Status.AuthorNativeID)
	assert.Equal(t, "", mine.Status.AuthorName)
	assert.Equal(t, "", mine.Status.BgColor)
}

func TestClassifyStatusMediaAndOthers(t *testing.T) {
	c := testConv()
	img := c.classify(&events.Message{Info: statusInfo("ST3", false), Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption: proto.String("playa"), Mimetype: proto.String("image/jpeg"), MediaKey: []byte("k"),
	}}})
	require.Equal(t, inStatus, img.Kind)
	assert.Equal(t, model.KindImage, img.Status.Kind)
	assert.Equal(t, "playa", img.Status.Text)
	require.NotNil(t, img.Status.Media)
	assert.Equal(t, mediaID("status@broadcast", "ST3"), img.Status.Media.ID)

	vid := c.classify(&events.Message{Info: statusInfo("ST4", false), Message: &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(5)}}})
	require.Equal(t, inStatus, vid.Kind)
	assert.Equal(t, model.KindVideo, vid.Status.Kind)

	// Un estado de audio no es texto, foto ni vídeo.
	aud := c.classify(&events.Message{Info: statusInfo("ST5", false), Message: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{}}})
	assert.Equal(t, inIgnore, aud.Kind)
}

func TestClassifyStatusRevokeAndReaction(t *testing.T) {
	c := testConv()
	rev := c.classify(&events.Message{Info: statusInfo("X", false), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: keyOf("ST1"),
	}}})
	require.Equal(t, inStatusRevoke, rev.Kind)
	assert.Equal(t, "ST1", rev.Status.NativeID)
	assert.Equal(t, testTs, rev.Status.DeletedAt)
	assert.Equal(t, pnPeer, rev.Status.AuthorNativeID)

	react := c.classify(&events.Message{Info: statusInfo("Y", false), Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
		Key: keyOf("ST9"), Text: proto.String("❤️"),
	}}})
	require.Equal(t, inStatusReaction, react.Kind)
	assert.Equal(t, string(testAcc)+"|ST9", react.StatusView.StatusKey)
	assert.Equal(t, pnPeer, react.StatusView.ViewerID)
	assert.Equal(t, "❤️", react.StatusView.Reaction)
}

func TestArgbConversions(t *testing.T) {
	assert.Equal(t, "#112233", argbToHex(0xFF112233))
	assert.Equal(t, "#00FF0A", argbToHex(0x0000FF0A))
	v, ok := hexToArgb("#112233")
	assert.True(t, ok)
	assert.Equal(t, uint32(0xFF112233), v)
	v, ok = hexToArgb("aabbcc")
	assert.True(t, ok)
	assert.Equal(t, uint32(0xFFAABBCC), v)
	for _, bad := range []string{"", "#12", "#GGGGGG", "#1122334"} {
		_, ok = hexToArgb(bad)
		assert.False(t, ok, bad)
	}
}

// ---------------------------------------------------------------------------------------------
// Recibos, escribiendo

func TestReceiptEvents(t *testing.T) {
	c := testConv()
	ts := time.UnixMilli(testTs + 5000)
	base := events.Receipt{
		MessageSource: types.MessageSource{Chat: jPeer, Sender: jPeer},
		MessageIDs:    []string{"a", "b"}, Timestamp: ts,
	}

	cases := []struct {
		typ  types.ReceiptType
		want model.DeliveryState
	}{
		{types.ReceiptTypeDelivered, model.DeliveryDelivered},
		{types.ReceiptTypeRead, model.DeliveryRead},
		{types.ReceiptTypePlayed, model.DeliveryPlayed},
	}
	for _, tc := range cases {
		r := base
		r.Type = tc.typ
		evs := c.receiptEvents(&r)
		require.Len(t, evs, 1, string(tc.typ))
		assert.Equal(t, model.EvReceipt, evs[0].Type)
		rc := evs[0].Receipt
		assert.Equal(t, tc.want, rc.Delivery)
		assert.Equal(t, model.NewChatKey(testAcc, pnPeer), rc.ChatKey)
		assert.Equal(t, []model.MessageKey{
			model.NewMessageKey(testAcc, pnPeer, "a"), model.NewMessageKey(testAcc, pnPeer, "b"),
		}, rc.MessageKeys)
		assert.Equal(t, pnPeer, rc.ReaderID)
		assert.EqualValues(t, testTs+5000, rc.At)
	}

	// Tipos que no son entrega ni lectura se ignoran; también lo que leí yo desde otro dispositivo.
	for _, typ := range []types.ReceiptType{types.ReceiptTypeRetry, types.ReceiptTypeSender, types.ReceiptTypeReadSelf, types.ReceiptTypeServerError} {
		r := base
		r.Type = typ
		assert.Empty(t, c.receiptEvents(&r), string(typ))
	}
	mine := base
	mine.Type = types.ReceiptTypeRead
	mine.IsFromMe = true
	assert.Empty(t, c.receiptEvents(&mine))
	assert.Empty(t, c.receiptEvents(&events.Receipt{Type: types.ReceiptTypeRead}))

	// Recibo de un @lid: chat y lector en teléfono.
	lid := base
	lid.Type = types.ReceiptTypeRead
	lid.Chat, lid.Sender = jPeerLID, jPeerLID
	evs := c.receiptEvents(&lid)
	require.Len(t, evs, 1)
	assert.Equal(t, model.NewChatKey(testAcc, pnPeer), evs[0].Receipt.ChatKey)
	assert.Equal(t, pnPeer, evs[0].Receipt.ReaderID)

	// En grupo, el lector es quien mandó el recibo.
	grp := base
	grp.Type = types.ReceiptTypeRead
	grp.Chat, grp.Sender, grp.IsGroup = jGroup, jOther, true
	evs = c.receiptEvents(&grp)
	require.Len(t, evs, 1)
	assert.Equal(t, model.NewChatKey(testAcc, groupID), evs[0].Receipt.ChatKey)
	assert.Equal(t, jidToNative(jOther), evs[0].Receipt.ReaderID)
}

func TestReceiptOnStatusIsView(t *testing.T) {
	c := testConv()
	r := events.Receipt{
		MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: jPeerLID},
		MessageIDs:    []string{"ST1", "ST2"}, Timestamp: time.UnixMilli(testTs), Type: types.ReceiptTypeRead,
	}
	evs := c.receiptEvents(&r)
	require.Len(t, evs, 2)
	assert.Equal(t, model.EvStatusView, evs[0].Type)
	assert.Equal(t, string(testAcc)+"|ST1", evs[0].StatusView.StatusKey)
	assert.Equal(t, string(testAcc)+"|ST2", evs[1].StatusView.StatusKey)
	assert.Equal(t, pnPeer, evs[0].StatusView.ViewerID) // @lid → teléfono
	assert.Equal(t, "Ana", evs[0].StatusView.ViewerName)
	assert.EqualValues(t, testTs, evs[0].StatusView.At)

	// «Entregado» en un estado no cuenta como vista.
	r.Type = types.ReceiptTypeDelivered
	assert.Empty(t, c.receiptEvents(&r))
}

func TestPresenceEvent(t *testing.T) {
	c := testConv()
	mk := func(state types.ChatPresence, media types.ChatPresenceMedia) *events.ChatPresence {
		return &events.ChatPresence{
			MessageSource: types.MessageSource{Chat: jPeerLID, Sender: jPeerLID}, State: state, Media: media,
		}
	}
	ev, ok := c.presenceEvent(mk(types.ChatPresenceComposing, types.ChatPresenceMediaText))
	require.True(t, ok)
	assert.Equal(t, model.EvTyping, ev.Type)
	assert.True(t, ev.Typing.Typing)
	assert.False(t, ev.Typing.Recording)
	assert.Equal(t, pnPeer, ev.Typing.PeerID)
	assert.Equal(t, model.NewChatKey(testAcc, pnPeer), ev.Typing.ChatKey)

	ev, ok = c.presenceEvent(mk(types.ChatPresenceComposing, types.ChatPresenceMediaAudio))
	require.True(t, ok)
	assert.True(t, ev.Typing.Typing)
	assert.True(t, ev.Typing.Recording)

	ev, ok = c.presenceEvent(mk(types.ChatPresencePaused, types.ChatPresenceMediaText))
	require.True(t, ok)
	assert.False(t, ev.Typing.Typing)
	assert.False(t, ev.Typing.Recording)

	own := mk(types.ChatPresenceComposing, types.ChatPresenceMediaText)
	own.IsFromMe = true
	_, ok = c.presenceEvent(own)
	assert.False(t, ok)
}

func TestPersonFrom(t *testing.T) {
	p := personFrom(testAcc, jPeer, types.ContactInfo{Found: true, FullName: "Ana Pérez", FirstName: "Ana", PushName: "anita"}, "/avatar/x/y.jpg")
	assert.Equal(t, model.Person{
		AccountID: testAcc, NativeID: pnPeer, Name: "Ana Pérez", PushName: "anita", Phone: "+593991111111", AvatarURL: "/avatar/x/y.jpg",
	}, p)
	p = personFrom(testAcc, jPeer, types.ContactInfo{FirstName: "Ana", BusinessName: "Tienda Ana"}, "")
	assert.Equal(t, "Ana", p.Name)
	assert.Equal(t, "Tienda Ana", p.PushName)
	// Un @lid no tiene teléfono.
	assert.Equal(t, "", personFrom(testAcc, jPeerLID, types.ContactInfo{}, "").Phone)
	assert.Equal(t, "+593991111111", phoneOf(jPeer))
	assert.Equal(t, "", phoneOf(jGroup))
}

// ---------------------------------------------------------------------------------------------
// Grupos: mensajes de sistema

func TestGroupSystemMessages(t *testing.T) {
	c := testConv()
	actorLID := jPeerLID // Ana, escrita como @lid
	evt := &events.GroupInfo{
		JID: jGroup, Timestamp: time.UnixMilli(testTs), Sender: &actorLID,
		Name:     &types.GroupName{Name: "Equipo nuevo"},
		Join:     []types.JID{jOther},
		Leave:    []types.JID{jPeer},
		Promote:  []types.JID{jOther},
		Demote:   []types.JID{jPeer},
		Announce: &types.GroupAnnounce{IsAnnounce: true},
	}
	msgs := c.groupSystemMessages(evt)
	var texts []string
	for _, m := range msgs {
		assert.Equal(t, model.KindSystem, m.Kind)
		assert.Equal(t, groupID, m.ChatNativeID)
		assert.Equal(t, pnPeer, m.SenderNativeID)
		assert.True(t, len(m.NativeID) > 4 && m.NativeID[:4] == "sys-")
		assert.Equal(t, model.NewMessageKey(testAcc, groupID, m.NativeID), m.Key)
		texts = append(texts, m.Text)
	}
	assert.Equal(t, []string{
		"Ana cambió el asunto del grupo a «Equipo nuevo»",
		"Ana añadió a Beto", // quien actúa no es quien entra
		"Ana salió",         // quien actúa es quien sale
		"Beto ahora es admin",
		"Ana ya no es admin",
		"Solo los admins pueden enviar mensajes",
	}, texts)

	// Los ids son estables y distintos entre sí.
	again := c.groupSystemMessages(evt)
	seen := map[string]bool{}
	for i, m := range msgs {
		assert.Equal(t, again[i].NativeID, m.NativeID)
		assert.False(t, seen[m.NativeID])
		seen[m.NativeID] = true
	}

	// Sin actor conocido.
	noActor := c.groupSystemMessages(&events.GroupInfo{JID: jGroup, Timestamp: time.UnixMilli(testTs), Join: []types.JID{jOther}, Topic: &types.GroupTopic{Topic: "x"}})
	require.Len(t, noActor, 2)
	assert.Equal(t, "Se cambió la descripción del grupo", noActor[0].Text)
	assert.Equal(t, "Beto se unió", noActor[1].Text)

	// Sin nombre conocido se muestra el teléfono.
	unknown := c.groupSystemMessages(&events.GroupInfo{JID: jGroup, Timestamp: time.UnixMilli(testTs), Leave: []types.JID{types.NewJID("593993333333", types.DefaultUserServer)}})
	require.Len(t, unknown, 1)
	assert.Equal(t, "+593993333333 salió", unknown[0].Text)

	assert.Empty(t, c.groupSystemMessages(&events.GroupInfo{JID: jGroup}))
}

// ---------------------------------------------------------------------------------------------
// Historial (07 §8)

func fakeParse(chat types.JID, w *waWeb.WebMessageInfo) (*events.Message, error) {
	info := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat: chat, IsFromMe: w.GetKey().GetFromMe(), IsGroup: chat.Server == types.GroupServer,
		},
		ID: w.GetKey().GetID(), Timestamp: time.Unix(int64(w.GetMessageTimestamp()), 0),
	}
	if info.IsFromMe {
		info.Sender = jSelf
	} else if chat.Server == types.GroupServer {
		p, _ := types.ParseJID(w.GetKey().GetParticipant())
		info.Sender = p
	} else {
		info.Sender = chat
	}
	return &events.Message{Info: info, Message: w.GetMessage(), RawMessage: w.GetMessage(), SourceWebMsg: w}, nil
}

func webMsg(id string, fromMe bool, ts uint64, m *waE2E.Message) *waHistorySync.HistorySyncMsg {
	return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{ID: proto.String(id), FromMe: proto.Bool(fromMe)},
		Message:          m,
		MessageTimestamp: proto.Uint64(ts),
	}}
}

func TestBuildHistory(t *testing.T) {
	c := testConv()
	read := waWeb.WebMessageInfo_READ
	mineWithStatus := webMsg("H2", true, 1_700_000_100, txt("de vuelta"))
	mineWithStatus.Message.Status = &read

	data := &waHistorySync.HistorySync{
		Progress: proto.Uint32(40),
		Pushnames: []*waHistorySync.Pushname{
			{ID: proto.String(pnPeer), Pushname: proto.String("anita")},
			{ID: proto.String("basura"), Pushname: proto.String("x")},
			{ID: proto.String(lidPeer), Pushname: proto.String("anita-lid")},
		},
		Conversations: []*waHistorySync.Conversation{
			{
				ID: proto.String(lidPeer), PnJID: proto.String(pnPeer), UnreadCount: proto.Uint32(3), Archived: proto.Bool(true),
				Pinned: proto.Uint32(1), MuteEndTime: proto.Uint64(4102444800), ConversationTimestamp: proto.Uint64(1_700_000_000),
				Messages: []*waHistorySync.HistorySyncMsg{
					webMsg("H1", false, 1_700_000_000, txt("hola")),
					mineWithStatus,
					webMsg("H3", false, 1_700_000_050, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: keyOf("H1")}}),
					{Message: nil},
				},
			},
			{
				ID: proto.String(groupID), Name: proto.String("Equipo"), ReadOnly: proto.Bool(true),
				Messages: []*waHistorySync.HistorySyncMsg{
					webMsg("G1", false, 1_700_000_200, &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")}}),
				},
			},
			{ID: proto.String("status@broadcast")},
			{ID: proto.String("123@broadcast")},
			{ID: proto.String("basura")},
		},
	}
	// El mensaje del grupo trae su participante.
	data.Conversations[1].Messages[0].Message.Key.Participant = proto.String(jidToNative(jOther))

	batches := c.buildHistory(data, fakeParse)
	require.Len(t, batches, 1)
	b := batches[0]
	assert.InDelta(t, 0.4, b.Progress, 1e-9)

	require.Len(t, b.Persons, 2)
	assert.Equal(t, pnPeer, b.Persons[0].NativeID)
	assert.Equal(t, pnPeer, b.Persons[1].NativeID) // @lid resuelto
	assert.Equal(t, "+593991111111", b.Persons[0].Phone)

	require.Len(t, b.Chats, 2)
	dm := b.Chats[0]
	assert.Equal(t, pnPeer, dm.NativeID) // @lid → teléfono por PnJID
	assert.Equal(t, model.ChatDirect, dm.Kind)
	assert.Equal(t, "Ana", dm.Title) // nombre de agenda
	assert.Equal(t, 3, dm.UnreadCount)
	assert.True(t, dm.Archived)
	assert.True(t, dm.Pinned)
	assert.EqualValues(t, -1, dm.MutedUntil)
	assert.Equal(t, pnPeer, dm.PeerNativeID)
	assert.Equal(t, 1_700_000_100*int64(1000), dm.LastMessageAt)
	assert.Equal(t, "de vuelta", dm.LastPreview)
	assert.True(t, dm.LastFromMe)

	grp := b.Chats[1]
	assert.Equal(t, model.ChatGroup, grp.Kind)
	assert.Equal(t, "Equipo", grp.Title)
	assert.True(t, grp.ReadOnly)
	assert.Equal(t, "📷 Foto", grp.LastPreview)

	// Mensajes: solo los «reales» (sin reacción, sin vacío), todos en el chat resuelto.
	require.Len(t, b.Messages, 3)
	byID := map[string]model.Message{}
	for _, m := range b.Messages {
		byID[m.NativeID] = m
	}
	assert.Equal(t, pnPeer, byID["H1"].ChatNativeID)
	assert.Equal(t, pnPeer, byID["H1"].SenderNativeID)
	assert.Equal(t, mediaID(groupID, "G1"), byID["G1"].Media.ID)
	assert.Equal(t, jidToNative(jOther), byID["G1"].SenderNativeID)
	assert.True(t, byID["H2"].FromMe)
	assert.Equal(t, model.DeliveryRead, byID["H2"].Delivery)
}

func TestBuildHistoryBatches(t *testing.T) {
	c := testConv()
	conv := &waHistorySync.Conversation{ID: proto.String(pnPeer)}
	for i := 0; i < 1200; i++ {
		conv.Messages = append(conv.Messages, webMsg("M"+string(rune('a'+i%26))+time.Duration(i).String(), false, uint64(1_700_000_000+i), txt("x")))
	}
	batches := c.buildHistory(&waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{conv}}, fakeParse)
	require.Len(t, batches, 3)
	assert.Len(t, batches[0].Messages, 500)
	assert.Len(t, batches[1].Messages, 500)
	assert.Len(t, batches[2].Messages, 200)
	assert.Len(t, batches[0].Chats, 1)
	assert.Empty(t, batches[1].Chats) // los chats van solo en el primer lote
	assert.EqualValues(t, -1, batches[0].Progress)

	assert.Nil(t, c.buildHistory(nil, fakeParse))
	assert.Nil(t, c.buildHistory(&waHistorySync.HistorySync{}, fakeParse))
}

func TestHistoryMute(t *testing.T) {
	now := time.UnixMilli(testTs)
	assert.EqualValues(t, 0, historyMute(0, now))
	assert.EqualValues(t, -1, historyMute(4102444800, now))
	assert.EqualValues(t, -1, historyMute(^uint64(0), now))
	assert.EqualValues(t, 0, historyMute(1_000, now)) // ya venció
	future := uint64(testTs/1000 + 3600)
	assert.EqualValues(t, int64(future)*1000, historyMute(future, now))
}

func TestMuteUntil(t *testing.T) {
	now := time.UnixMilli(testTs)
	assert.EqualValues(t, 0, muteUntil(false, 123, now))
	assert.EqualValues(t, -1, muteUntil(true, 0, now))
	assert.EqualValues(t, -1, muteUntil(true, -1, now))
	assert.EqualValues(t, 0, muteUntil(true, testTs-1, now))
	assert.EqualValues(t, testTs+1000, muteUntil(true, testTs+1000, now))
}

func TestDeliveryFromWeb(t *testing.T) {
	assert.Equal(t, model.DeliveryFailed, deliveryFromWeb(0))
	assert.Equal(t, model.DeliveryPending, deliveryFromWeb(1))
	assert.Equal(t, model.DeliverySent, deliveryFromWeb(2))
	assert.Equal(t, model.DeliveryDelivered, deliveryFromWeb(3))
	assert.Equal(t, model.DeliveryRead, deliveryFromWeb(4))
	assert.Equal(t, model.DeliveryPlayed, deliveryFromWeb(5))
}

func TestTempBanDetail(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.Local)
	got := banDetail(&events.TemporaryBan{Code: events.TempBanSentToTooManyPeople, Expire: 2 * time.Hour}, now)
	assert.Equal(t, "WhatsApp bloqueó temporalmente la cuenta: enviaste demasiados mensajes a personas que no te tienen en su agenda, hasta 11/11/2026 12:00", got)
	assert.Contains(t, banDetail(&events.TemporaryBan{Code: events.TempBanBlockedByUsers}, now), "demasiadas personas te bloquearon")
	assert.NotContains(t, banDetail(&events.TemporaryBan{Code: events.TempBanBlockedByUsers}, now), "hasta")
	assert.Contains(t, banDetail(&events.TemporaryBan{Code: 999}, now), "código 999")
}
