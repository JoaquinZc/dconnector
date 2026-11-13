package messenger

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waArmadilloApplication"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
	"go.mau.fi/whatsmeow/proto/waMsgApplication"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
)

const e2eeAcc = model.AccountID("fb-test")

var e2eeNow = time.UnixMilli(1_700_000_000_000)

func e2eeConv() *e2eeConverter {
	return &e2eeConverter{acc: e2eeAcc, self: "100", name: func(id int64) string {
		if id == 200 {
			return "Ana"
		}
		return ""
	}}
}

func dmEvent(sender string, fromMe bool, msg *waConsumerApplication.ConsumerApplication) *events.FBMessage {
	return &events.FBMessage{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat: types.NewJID("200", types.MessengerServer), Sender: types.NewJID(sender, types.MessengerServer), IsFromMe: fromMe,
			},
			ID: "ABCD1", Timestamp: e2eeNow,
		},
		Message: msg,
	}
}

func contentApp(c *waConsumerApplication.ConsumerApplication_Content) *waConsumerApplication.ConsumerApplication {
	return wrapContent(c)
}

func text(s string) *waConsumerApplication.ConsumerApplication {
	return contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_MessageText{MessageText: textMessage(s)},
	})
}

func TestE2EEText(t *testing.T) {
	out, ok := e2eeConv().convert(dmEvent("200", false, text("hola cifrado")))
	require.True(t, ok)
	require.NotNil(t, out.Message)
	m := out.Message
	assert.Equal(t, "200", out.Chat)
	assert.False(t, out.Group)
	assert.Equal(t, model.KindText, m.Kind)
	assert.Equal(t, "hola cifrado", m.Text)
	assert.Equal(t, "ABCD1", m.NativeID)
	assert.Equal(t, "200", m.ChatNativeID)
	assert.Equal(t, "200", m.SenderNativeID)
	assert.Equal(t, "Ana", m.SenderName)
	assert.False(t, m.FromMe)
	assert.Equal(t, e2eeNow.UnixMilli(), m.Timestamp)
	assert.Equal(t, model.NewMessageKey(e2eeAcc, "200", "ABCD1"), m.Key)
}

func TestE2EEOwnMessageFromAnotherDevice(t *testing.T) {
	ev := dmEvent("100", true, text("desde mi teléfono"))
	out, ok := e2eeConv().convert(ev)
	require.True(t, ok)
	assert.True(t, out.Message.FromMe)
	assert.Equal(t, "200", out.Chat, "el chat sigue siendo el de la otra persona")
	assert.Equal(t, "Tú", out.Message.SenderName)
}

func TestE2EEGroupChatID(t *testing.T) {
	ev := dmEvent("200", false, text("grupo"))
	ev.Info.Chat = types.NewJID("987654321", types.GroupServer)
	ev.Info.IsGroup = true
	out, ok := e2eeConv().convert(ev)
	require.True(t, ok)
	assert.Equal(t, "987654321", out.Chat)
	assert.True(t, out.Group)
	assert.Equal(t, "200", out.Message.SenderNativeID)
}

func TestE2EEEmptyAndExtendedText(t *testing.T) {
	_, ok := e2eeConv().convert(dmEvent("200", false, text("   ")))
	assert.False(t, ok, "texto vacío no se entrega")
	out, ok := e2eeConv().convert(dmEvent("200", false, contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_ExtendedTextMessage{
			ExtendedTextMessage: &waConsumerApplication.ConsumerApplication_ExtendedTextMessage{Text: textMessage("con enlace https://ej.com")},
		},
	})))
	require.True(t, ok)
	assert.Equal(t, "con enlace https://ej.com", out.Message.Text)
	_, ok = e2eeConv().convert(&events.FBMessage{Info: types.MessageInfo{ID: "x"}})
	assert.False(t, ok, "sin contenido (p. ej. cambio de mensajes temporales)")
}

func TestE2EEQuotedMessage(t *testing.T) {
	ev := dmEvent("200", false, text("respuesta"))
	ev.FBApplication = &waMsgApplication.MessageApplication{
		Metadata: &waMsgApplication.MessageApplication_Metadata{
			QuotedMessage: &waMsgApplication.MessageApplication_Metadata_QuotedMessage{
				StanzaID: proto.String("ORIGINAL"), Participant: proto.String("200@msgr"),
			},
		},
	}
	out, ok := e2eeConv().convert(ev)
	require.True(t, ok)
	assert.Equal(t, "ORIGINAL", out.Message.QuotedNativeID)
	assert.Equal(t, "Ana", out.Message.QuotedSender)
}

func TestE2EEReactionEditAndRevoke(t *testing.T) {
	key := &waCommon.MessageKey{RemoteJID: proto.String("200@msgr"), ID: proto.String("TARGET")}
	out, ok := e2eeConv().convert(dmEvent("200", false, contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_ReactionMessage{
			ReactionMessage: &waConsumerApplication.ConsumerApplication_ReactionMessage{Key: key, Text: proto.String("❤"), SenderTimestampMS: proto.Int64(1234)},
		},
	})))
	require.True(t, ok)
	require.NotNil(t, out.Reaction)
	assert.Equal(t, model.NewMessageKey(e2eeAcc, "200", "TARGET"), out.Reaction.MessageKey)
	assert.Equal(t, "❤", out.Reaction.Reaction.Emoji)
	assert.Equal(t, int64(1234), out.Reaction.Reaction.At)
	assert.Equal(t, "Ana", out.Reaction.Reaction.SenderName)
	assert.Nil(t, out.Message)

	// Quitar la reacción llega con texto vacío.
	out, _ = e2eeConv().convert(dmEvent("200", false, contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_ReactionMessage{
			ReactionMessage: &waConsumerApplication.ConsumerApplication_ReactionMessage{Key: key, Text: proto.String("")},
		},
	})))
	require.NotNil(t, out.Reaction)
	assert.Equal(t, "", out.Reaction.Reaction.Emoji)
	assert.Equal(t, e2eeNow.UnixMilli(), out.Reaction.Reaction.At, "sin hora propia usa la del mensaje")

	out, ok = e2eeConv().convert(dmEvent("200", false, contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_EditMessage{
			EditMessage: &waConsumerApplication.ConsumerApplication_EditMessage{Key: key, Message: textMessage("corregido"), TimestampMS: proto.Int64(999)},
		},
	})))
	require.True(t, ok)
	require.NotNil(t, out.Update)
	assert.Equal(t, "corregido", *out.Update.Text)
	assert.Equal(t, int64(999), out.Update.EditedAt)
	assert.Equal(t, model.NewMessageKey(e2eeAcc, "200", "TARGET"), out.Update.Key)

	revoke := &waConsumerApplication.ConsumerApplication{
		Payload: &waConsumerApplication.ConsumerApplication_Payload{
			Payload: &waConsumerApplication.ConsumerApplication_Payload_ApplicationData{
				ApplicationData: &waConsumerApplication.ConsumerApplication_ApplicationData{
					ApplicationContent: &waConsumerApplication.ConsumerApplication_ApplicationData_Revoke{
						Revoke: &waConsumerApplication.ConsumerApplication_RevokeMessage{Key: key},
					},
				},
			},
		},
	}
	out, ok = e2eeConv().convert(dmEvent("200", false, revoke))
	require.True(t, ok)
	require.NotNil(t, out.Update)
	assert.True(t, out.Update.Deleted)
	assert.Equal(t, model.NewMessageKey(e2eeAcc, "200", "TARGET"), out.Update.Key)
}

func transport(mime string, size uint64) *waMediaTransport.WAMediaTransport {
	return &waMediaTransport.WAMediaTransport{
		Integral: &waMediaTransport.WAMediaTransport_Integral{
			FileSHA256: []byte("sha"), MediaKey: []byte("key"), FileEncSHA256: []byte("enc"), DirectPath: proto.String("/v/t62/abc"),
		},
		Ancillary: &waMediaTransport.WAMediaTransport_Ancillary{FileLength: proto.Uint64(size), Mimetype: proto.String(mime)},
	}
}

func mediaEvent(t *testing.T, kind model.ContentKind, file meta.OutFile, tr *waMediaTransport.WAMediaTransport, caption string, w, h int) *events.FBMessage {
	t.Helper()
	var content waConsumerApplication.ConsumerApplication_Content
	require.NoError(t, setMedia(&content, kind, file, tr, textMessage(caption), w, h))
	return dmEvent("200", false, contentApp(&content))
}

func TestE2EEMediaRoundTripThroughSetMedia(t *testing.T) {
	cases := []struct {
		name string
		kind model.ContentKind
		file meta.OutFile
		mime string
		want model.ContentKind
		mt   whatsmeow.MediaType
	}{
		{"imagen", model.KindImage, meta.OutFile{Name: "f.jpg", Mime: "image/jpeg"}, "image/jpeg", model.KindImage, whatsmeow.MediaImage},
		{"vídeo", model.KindVideo, meta.OutFile{Name: "v.mp4", Mime: "video/mp4"}, "video/mp4", model.KindVideo, whatsmeow.MediaVideo},
		{"voz", model.KindVoice, meta.OutFile{Name: "a.ogg", Mime: "audio/ogg"}, "audio/ogg", model.KindVoice, whatsmeow.MediaAudio},
		{"audio", model.KindAudio, meta.OutFile{Name: "a.mp3", Mime: "audio/mpeg"}, "audio/mpeg", model.KindVoice, whatsmeow.MediaAudio},
		{"documento", model.KindDocument, meta.OutFile{Name: "factura.pdf", Mime: "application/pdf"}, "application/pdf", model.KindDocument, whatsmeow.MediaDocument},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, ok := e2eeConv().convert(mediaEvent(t, c.kind, c.file, transport(c.mime, 4321), "pie", 640, 480))
			require.True(t, ok)
			m := out.Message
			require.NotNil(t, m)
			assert.Equal(t, c.want, m.Kind)
			require.NotNil(t, m.Media)
			assert.Equal(t, c.mime, m.Media.Mime)
			assert.Equal(t, int64(4321), m.Media.Size)
			assert.Equal(t, meta.MediaID("200", "ABCD1", 0), m.Media.ID)
			if c.kind == model.KindImage || c.kind == model.KindVideo {
				assert.Equal(t, 640, m.Media.Width)
				assert.Equal(t, 480, m.Media.Height)
				assert.Equal(t, "pie", m.Text)
			}
			if c.kind == model.KindDocument {
				assert.Equal(t, "factura.pdf", m.Media.FileName)
			} else {
				assert.NotEmpty(t, m.Media.FileName)
			}
			// El Handle lleva las llaves de descarga para DownloadFB.
			h, err := meta.ParseHandle(m.Media.Handle)
			require.NoError(t, err)
			assert.Equal(t, string(c.mt), h.Type)
			var integral waMediaTransport.WAMediaTransport_Integral
			require.NoError(t, protojson.Unmarshal(h.E2EE, &integral))
			assert.Equal(t, "/v/t62/abc", integral.GetDirectPath())
			assert.Equal(t, []byte("key"), integral.GetMediaKey())
		})
	}
}

func TestE2EEGifSentAsVideoComesBackAsImage(t *testing.T) {
	// Un GIF sale como vídeo con la marca de GIF; si llega un «vídeo» cuyo medio es imagen, se muestra como imagen.
	out, ok := e2eeConv().convert(mediaEvent(t, model.KindVideo, meta.OutFile{Name: "x.gif", Mime: "image/gif"}, transport("image/gif", 10), "", 100, 100))
	require.True(t, ok)
	assert.Equal(t, model.KindImage, out.Message.Kind)
}

func TestE2EEStickerLocationArmadilloAndUnknown(t *testing.T) {
	st := &waConsumerApplication.ConsumerApplication_StickerMessage{}
	require.NoError(t, st.Set(&waMediaTransport.StickerTransport{
		Integral:  &waMediaTransport.StickerTransport_Integral{Transport: transport("image/webp", 5)},
		Ancillary: &waMediaTransport.StickerTransport_Ancillary{Width: proto.Uint32(96), Height: proto.Uint32(96)},
	}))
	out, ok := e2eeConv().convert(dmEvent("200", false, contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_StickerMessage{StickerMessage: st},
	})))
	require.True(t, ok)
	assert.Equal(t, model.KindSticker, out.Message.Kind)
	assert.Equal(t, 96, out.Message.Media.Width)

	out, ok = e2eeConv().convert(dmEvent("200", false, contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_LocationMessage{
			LocationMessage: &waConsumerApplication.ConsumerApplication_LocationMessage{
				Location: &waConsumerApplication.ConsumerApplication_Location{DegreesLatitude: proto.Float64(-0.18), DegreesLongitude: proto.Float64(-78.46), Name: proto.String("Quito")},
				Address:  proto.String("Centro"),
			},
		},
	})))
	require.True(t, ok)
	assert.Equal(t, model.KindLocation, out.Message.Kind)
	assert.InDelta(t, -0.18, out.Message.Location.Lat, 0.0001)
	assert.Equal(t, "Quito", out.Message.Location.Name)
	assert.Equal(t, "Centro", out.Message.Location.Address)

	// Contenido no soportado (contacto): mensaje «no compatible».
	out, ok = e2eeConv().convert(dmEvent("200", false, contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_ContactMessage{ContactMessage: &waConsumerApplication.ConsumerApplication_ContactMessage{}},
	})))
	require.True(t, ok)
	assert.Equal(t, model.KindUnsupported, out.Message.Kind)

	// Armadillo (compartidos, enlaces enriquecidos…).
	ev := dmEvent("200", false, nil)
	ev.Message = &waArmadilloApplication.Armadillo{}
	out, ok = e2eeConv().convert(ev)
	require.True(t, ok)
	assert.Equal(t, model.KindUnsupported, out.Message.Kind)

	// Una pegatina que se pide aparte (receiverFetchID) no se muestra.
	st2 := &waConsumerApplication.ConsumerApplication_StickerMessage{}
	require.NoError(t, st2.Set(&waMediaTransport.StickerTransport{
		Integral:  &waMediaTransport.StickerTransport_Integral{Transport: transport("image/webp", 5)},
		Ancillary: &waMediaTransport.StickerTransport_Ancillary{ReceiverFetchID: proto.String("abc")},
	}))
	out, _ = e2eeConv().convert(dmEvent("200", false, contentApp(&waConsumerApplication.ConsumerApplication_Content{
		Content: &waConsumerApplication.ConsumerApplication_Content_StickerMessage{StickerMessage: st2},
	})))
	assert.Equal(t, model.KindUnsupported, out.Message.Kind)
}

func TestE2EEReceiptsAndTyping(t *testing.T) {
	c := e2eeConv()
	rc, ok := c.receiptEvent(&events.Receipt{
		MessageSource: types.MessageSource{Chat: types.NewJID("200", types.MessengerServer), Sender: types.NewJID("200", types.MessengerServer)},
		MessageIDs:    []string{"A", "B"}, Timestamp: e2eeNow, Type: types.ReceiptTypeRead,
	})
	require.True(t, ok)
	assert.Equal(t, model.DeliveryRead, rc.Delivery)
	assert.Equal(t, []model.MessageKey{model.NewMessageKey(e2eeAcc, "200", "A"), model.NewMessageKey(e2eeAcc, "200", "B")}, rc.MessageKeys)
	assert.Equal(t, "200", rc.ReaderID)
	assert.Equal(t, e2eeNow.UnixMilli(), rc.At)

	rc, ok = c.receiptEvent(&events.Receipt{
		MessageSource: types.MessageSource{Chat: types.NewJID("200", types.MessengerServer)}, MessageIDs: []string{"A"}, Type: types.ReceiptTypeDelivered,
	})
	require.True(t, ok)
	assert.Equal(t, model.DeliveryDelivered, rc.Delivery)

	_, ok = c.receiptEvent(&events.Receipt{MessageSource: types.MessageSource{Chat: types.NewJID("200", types.MessengerServer)}, MessageIDs: []string{"A"}, Type: types.ReceiptTypeSender})
	assert.False(t, ok)
	_, ok = c.receiptEvent(&events.Receipt{MessageSource: types.MessageSource{Chat: types.NewJID("200", types.MessengerServer)}, Type: types.ReceiptTypeRead})
	assert.False(t, ok, "sin mensajes")

	ty, ok := c.typingEvent(&events.ChatPresence{
		MessageSource: types.MessageSource{Chat: types.NewJID("200", types.MessengerServer), Sender: types.NewJID("200", types.MessengerServer)},
		State:         types.ChatPresenceComposing, Media: types.ChatPresenceMediaAudio,
	})
	require.True(t, ok)
	assert.True(t, ty.Typing)
	assert.True(t, ty.Recording)
	assert.Equal(t, model.NewChatKey(e2eeAcc, "200"), ty.ChatKey)
	assert.Equal(t, "200", ty.PeerID)

	ty, ok = c.typingEvent(&events.ChatPresence{
		MessageSource: types.MessageSource{Chat: types.NewJID("200", types.MessengerServer), Sender: types.NewJID("200", types.MessengerServer)},
		State:         types.ChatPresencePaused,
	})
	require.True(t, ok)
	assert.False(t, ty.Typing)

	_, ok = c.typingEvent(&events.ChatPresence{
		MessageSource: types.MessageSource{Chat: types.NewJID("200", types.MessengerServer), Sender: types.NewJID("100", types.MessengerServer)},
		State:         types.ChatPresenceComposing,
	})
	assert.False(t, ok, "mi propio «escribiendo» se ignora")
}

func TestWaKeyAndHelpers(t *testing.T) {
	dm := types.NewJID("200", types.MessengerServer)
	k := waKey(dm, model.MessageRef{NativeID: "M1", FromMe: true}, "100")
	assert.Equal(t, "200@msgr", k.GetRemoteJID())
	assert.Equal(t, "M1", k.GetID())
	assert.True(t, k.GetFromMe())
	assert.Empty(t, k.GetParticipant(), "en privados no hay participante")

	grp := types.NewJID("555", types.GroupServer)
	k = waKey(grp, model.MessageRef{NativeID: "M2", SenderNativeID: "300"}, "100")
	assert.False(t, k.GetFromMe())
	assert.Equal(t, "300@msgr", k.GetParticipant())
	k = waKey(grp, model.MessageRef{NativeID: "M3", FromMe: true}, "100")
	assert.Equal(t, "100@msgr", k.GetParticipant())

	assert.Equal(t, "200", e2eeChatID(types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("200", types.MessengerServer)}}))
	assert.Equal(t, whatsmeow.MediaVideo, whatsmeowMediaType(model.KindImage, "image/gif"))
	assert.Equal(t, whatsmeow.MediaImage, whatsmeowMediaType(model.KindImage, "image/png"))
	assert.Equal(t, whatsmeow.MediaAudio, whatsmeowMediaType(model.KindVoice, "audio/ogg"))
	assert.Equal(t, whatsmeow.MediaDocument, whatsmeowMediaType(model.KindDocument, "application/pdf"))
	assert.Equal(t, 400, clamp400(0))
	assert.Equal(t, 400, clamp400(2000))
	assert.Equal(t, 90, clamp400(90))
	w, h := imageSize([]byte("no es imagen"))
	assert.Equal(t, 0, w)
	assert.Equal(t, 0, h)
}
