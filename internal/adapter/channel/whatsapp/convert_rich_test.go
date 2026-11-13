package whatsapp

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
)

// Más tipos de mensaje de WhatsApp (convert_rich.go).

func TestConvertPtvAndInstantImage(t *testing.T) {
	c := testConv()
	m := c.convertMessage(infoDM("PTV", false), &waE2E.Message{PtvMessage: &waE2E.VideoMessage{
		Mimetype: proto.String("video/mp4"), Seconds: proto.Uint32(7), Width: proto.Uint32(480), Height: proto.Uint32(480),
		MediaKey: []byte("k"), DirectPath: proto.String("/v/p"), JPEGThumbnail: []byte{1, 2},
	}})
	assert.Equal(t, model.KindVideo, m.Kind)
	require.NotNil(t, m.Media)
	assert.Equal(t, 7.0, m.Media.Duration)
	assert.Equal(t, 480, m.Media.Width)
	assert.NotEmpty(t, m.Media.Thumb)
	sub, err := parseHandle(m.Media.Handle)
	require.NoError(t, err)
	assert.Equal(t, "/v/p", sub.GetDirectPath())

	img := c.convertMessage(infoDM("INST", false), &waE2E.Message{InstantImageMessage: &waE2E.ImageMessage{
		Mimetype: proto.String("image/jpeg"), Caption: proto.String("al momento"), MediaKey: []byte("k"),
	}})
	assert.Equal(t, model.KindImage, img.Kind)
	assert.Equal(t, "al momento", img.Text)
	require.NotNil(t, img.Media)
}

func TestConvertLottieSticker(t *testing.T) {
	c := testConv()
	m := c.convertMessage(infoDM("LOT", false), &waE2E.Message{LottieStickerMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
			Mimetype: proto.String("application/was"), MediaKey: []byte("k"), IsLottie: proto.Bool(true),
		}},
	}})
	assert.Equal(t, model.KindSticker, m.Kind)
	require.NotNil(t, m.Media)
	assert.Equal(t, "application/was", m.Media.Mime)
}

func TestConvertStructuredTexts(t *testing.T) {
	c := testConv()
	btn := func(s string) *waE2E.ButtonsMessage_Button {
		return &waE2E.ButtonsMessage_Button{ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String(s)}}
	}
	hydrated := &waE2E.TemplateMessage_HydratedFourRowTemplate{
		Title:               &waE2E.TemplateMessage_HydratedFourRowTemplate_HydratedTitleText{HydratedTitleText: "Tu pedido"},
		HydratedContentText: proto.String("Llega hoy"), HydratedFooterText: proto.String("Gracias"),
		HydratedButtons: []*waE2E.HydratedTemplateButton{
			{Index: proto.Uint32(0), HydratedButton: &waE2E.HydratedTemplateButton_QuickReplyButton{
				QuickReplyButton: &waE2E.HydratedTemplateButton_HydratedQuickReplyButton{DisplayText: proto.String("Confirmar")}}},
			{Index: proto.Uint32(1), HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{
				UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{DisplayText: proto.String("Ver pedido")}}},
		},
	}
	cases := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{"botones", &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{
			Header:      &waE2E.ButtonsMessage_Text{Text: "Menú"},
			ContentText: proto.String("¿Qué necesitas?"), FooterText: proto.String("Atención 24h"),
			Buttons: []*waE2E.ButtonsMessage_Button{btn("Ventas"), btn("Soporte")},
		}}, "Menú\n¿Qué necesitas?\nAtención 24h\nOpciones: Ventas · Soporte"},
		{"botones vacíos", &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{}}, "Mensaje con botones"},
		{"lista", &waE2E.Message{ListMessage: &waE2E.ListMessage{
			Title: proto.String("Productos"), Description: proto.String("Elige uno"), FooterText: proto.String("Precios con IVA"),
		}}, "Productos\nElige uno\nPrecios con IVA"},
		{"plantilla", &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{HydratedTemplate: hydrated}},
			"Tu pedido\nLlega hoy\nGracias\nOpciones: Confirmar · Ver pedido"},
		{"plantilla hsm", &waE2E.Message{HighlyStructuredMessage: &waE2E.HighlyStructuredMessage{
			HydratedHsm: &waE2E.TemplateMessage{Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{HydratedFourRowTemplate: hydrated}},
		}}, "Tu pedido\nLlega hoy\nGracias\nOpciones: Confirmar · Ver pedido"},
		{"hsm con alterno", &waE2E.Message{HighlyStructuredMessage: &waE2E.HighlyStructuredMessage{
			FallbackLg: proto.String("Código 1234"),
		}}, "Código 1234"},
		{"interactivo", &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
			Header: &waE2E.InteractiveMessage_Header{Title: proto.String("Oferta")},
			Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("20% de descuento")},
			Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String("Solo hoy")},
		}}, "Oferta\n20% de descuento\nSolo hoy"},
		{"resp botones", &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
			Response: &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Ventas"},
		}}, "Ventas"},
		{"resp lista", &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{
			Title: proto.String("Pizza"), Description: proto.String("Grande"),
		}}, "Pizza\nGrande"},
		{"resp plantilla", &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
			SelectedDisplayText: proto.String("Confirmar"),
		}}, "Confirmar"},
		{"resp interactiva", &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
			Body: &waE2E.InteractiveResponseMessage_Body{Text: proto.String("Opción B")},
		}}, "Opción B"},
		{"resp interactiva vacía", &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{}},
			"Respuesta a un mensaje interactivo"},
		{"invitación a grupo", &waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{GroupName: proto.String("Vecinos")}},
			"Invitación al grupo «Vecinos»"},
		{"invitación sin nombre", &waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{}}, "Invitación a un grupo"},
		{"evento", &waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("Cumple")}}, "📅 Evento: Cumple"},
		{"evento cancelado", &waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("Cumple"), IsCanceled: proto.Bool(true)}},
			"📅 Evento: Cumple (cancelado)"},
		{"invitación a evento", &waE2E.Message{EventInviteMessage: &waE2E.EventInviteMessage{EventTitle: proto.String("Boda")}}, "📅 Evento: Boda"},
		{"pedido", &waE2E.Message{OrderMessage: &waE2E.OrderMessage{
			OrderTitle: proto.String("Pedido 12"), ItemCount: proto.Int32(3), Message: proto.String("Para las 5"),
		}}, "🛒 Pedido: Pedido 12 (3 artículos)\nPara las 5"},
		{"producto", &waE2E.Message{ProductMessage: &waE2E.ProductMessage{
			Product: &waE2E.ProductMessage_ProductSnapshot{Title: proto.String("Taza")}, Body: proto.String("Nueva"),
		}}, "🛍️ Producto: Taza\nNueva"},
		{"producto vacío", &waE2E.Message{ProductMessage: &waE2E.ProductMessage{}}, "🛍️ Producto"},
		{"canal admin", &waE2E.Message{NewsletterAdminInviteMessage: &waE2E.NewsletterAdminInviteMessage{NewsletterName: proto.String("Noticias")}},
			"Invitación para administrar el canal «Noticias»"},
		{"canal seguidor", &waE2E.Message{NewsletterFollowerInviteMessageV2: &waE2E.NewsletterFollowerInviteMessage{NewsletterName: proto.String("Noticias")}},
			"Invitación al canal «Noticias»"},
		{"llamada programada", &waE2E.Message{ScheduledCallCreationMessage: &waE2E.ScheduledCallCreationMessage{Title: proto.String("Reunión")}},
			"📞 Llamada programada: Reunión"},
		{"resultados de encuesta", &waE2E.Message{PollResultSnapshotMessage: &waE2E.PollResultSnapshotMessage{Name: proto.String("¿Cena?")}},
			"📊 Resultados de la encuesta: ¿Cena?"},
		{"paquete de stickers", &waE2E.Message{StickerPackMessage: &waE2E.StickerPackMessage{Name: proto.String("Gatos")}},
			"Paquete de stickers: Gatos"},
	}
	for _, tc := range cases {
		m := c.convertMessage(infoDM("S", false), tc.msg)
		assert.Equal(t, model.KindText, m.Kind, tc.name)
		assert.Equal(t, tc.want, m.Text, tc.name)
		assert.NotEqual(t, "Mensaje no compatible", model.Preview(m), tc.name)
	}

	// La cita de una respuesta a botones se conserva.
	reply := c.convertMessage(infoDM("S2", false), &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
		Response:    &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Ventas"},
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("ORIG"), QuotedMessage: txt("¿Qué necesitas?")},
	}})
	assert.Equal(t, "ORIG", reply.QuotedNativeID)
	assert.Equal(t, "¿Qué necesitas?", reply.QuotedPreview)

	// Un texto largo se recorta.
	long := c.convertMessage(infoDM("S3", false), &waE2E.Message{ListMessage: &waE2E.ListMessage{
		Description: proto.String(strings.Repeat("a", 2000)),
	}})
	assert.LessOrEqual(t, len([]rune(long.Text)), richMaxRunes)
}

func TestConvertCallLog(t *testing.T) {
	c := testConv()
	cases := []struct {
		log  *waE2E.CallLogMessage
		want string
	}{
		{&waE2E.CallLogMessage{CallOutcome: waE2E.CallLogMessage_MISSED.Enum()}, "Llamada perdida"},
		{&waE2E.CallLogMessage{CallOutcome: waE2E.CallLogMessage_MISSED.Enum(), IsVideo: proto.Bool(true)}, "Videollamada perdida"},
		{&waE2E.CallLogMessage{CallOutcome: waE2E.CallLogMessage_REJECTED.Enum()}, "Llamada rechazada"},
		{&waE2E.CallLogMessage{CallOutcome: waE2E.CallLogMessage_CONNECTED.Enum(), DurationSecs: proto.Int64(125)}, "Llamada de 2:05"},
		{&waE2E.CallLogMessage{CallOutcome: waE2E.CallLogMessage_ACCEPTED_ELSEWHERE.Enum()}, "Llamada atendida en otro dispositivo"},
	}
	for _, tc := range cases {
		m := c.convertMessage(infoDM("CL", false), &waE2E.Message{CallLogMesssage: tc.log})
		assert.Equal(t, model.KindSystem, m.Kind)
		assert.Equal(t, tc.want, m.Text)
	}
}

func TestConvertViewOnce(t *testing.T) {
	c := testConv()
	const want = "Mensaje de una sola vez: ábrelo en tu teléfono"
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Mimetype: proto.String("image/jpeg"), MediaKey: []byte("k"), DirectPath: proto.String("/p"), JPEGThumbnail: []byte{9},
	}}
	for name, w := range map[string]*waE2E.Message{
		"v1":  {ViewOnceMessage: &waE2E.FutureProofMessage{Message: img}},
		"v2":  {ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: img}},
		"ext": {ViewOnceMessageV2Extension: &waE2E.FutureProofMessage{Message: img}},
		"efímero y v2": {EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: img}}}},
	} {
		m := c.convertMessage(infoDM("VO", false), w)
		assert.Equal(t, model.KindUnsupported, m.Kind, name)
		assert.Equal(t, want, m.Text, name)
		assert.Nil(t, m.Media, name)
		assert.Equal(t, want, model.Preview(m), name)
	}
	// whatsmeow ya quitó el envoltorio y dejó la marca en el evento (también vídeo y audio).
	for name, inner := range map[string]*waE2E.Message{
		"imagen": img,
		"vídeo":  {VideoMessage: &waE2E.VideoMessage{MediaKey: []byte("k")}},
		"audio":  {AudioMessage: &waE2E.AudioMessage{MediaKey: []byte("k")}},
	} {
		in := c.classify(&events.Message{Info: infoDM("VO2", false), Message: inner, IsViewOnce: true, IsViewOnceV2: true})
		require.Equal(t, inMessage, in.Kind, name)
		assert.Equal(t, model.KindUnsupported, in.Message.Kind, name)
		assert.Equal(t, want, in.Message.Text, name)
		assert.Nil(t, in.Message.Media, name)
	}
	// Sin la marca, la misma foto es una foto.
	in := c.classify(&events.Message{Info: infoDM("VO3", false), Message: img})
	assert.Equal(t, model.KindImage, in.Message.Kind)
}

func TestClassifyMetadataOnlyIsIgnored(t *testing.T) {
	c := testConv()
	ignored := map[string]*waE2E.Message{
		"álbum padre":   {AlbumMessage: &waE2E.AlbumMessage{ExpectedImageCount: proto.Uint32(3)}},
		"fijar":         {PinInChatMessage: &waE2E.PinInChatMessage{Key: keyOf("X")}},
		"conservar":     {KeepInChatMessage: &waE2E.KeepInChatMessage{Key: keyOf("X")}},
		"placeholder":   {PlaceholderMessage: &waE2E.PlaceholderMessage{}},
		"pedir número":  {RequestPhoneNumberMessage: &waE2E.RequestPhoneNumberMessage{}},
		"historial":     {MessageHistoryBundle: &waE2E.MessageHistoryBundle{}},
		"reacción cif.": {EncReactionMessage: &waE2E.EncReactionMessage{TargetMessageKey: keyOf("X")}},
		"secreto":       {SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{}},
		"con contexto": {
			AlbumMessage:       &waE2E.AlbumMessage{},
			MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: []byte("s")},
		},
		"dentro de un efímero": {EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{AlbumMessage: &waE2E.AlbumMessage{}}}},
	}
	for name, m := range ignored {
		assert.Equal(t, inIgnore, c.classify(&events.Message{Info: infoDM("MD", false), Message: m}).Kind, name)
	}
	// En estados también.
	st := infoDM("MDS", false)
	st.Chat = types.StatusBroadcastJID
	assert.Equal(t, inIgnore, c.classify(&events.Message{Info: st, Message: &waE2E.Message{AlbumMessage: &waE2E.AlbumMessage{}}}).Kind)

	// Un álbum son N fotos normales más el padre: las fotos se guardan, el padre no.
	photo := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), MediaKey: []byte("k")}}
	assert.Equal(t, inMessage, c.classify(&events.Message{Info: infoDM("AL1", false), Message: photo}).Kind)
	child := &waE2E.Message{AssociatedChildMessage: &waE2E.FutureProofMessage{Message: photo}}
	childIn := c.classify(&events.Message{Info: infoDM("AL2", false), Message: child})
	require.Equal(t, inMessage, childIn.Kind)
	assert.Equal(t, model.KindImage, childIn.Message.Kind)

	// Si trae algo de contenido además del metadato, no se ignora.
	mixed := &waE2E.Message{Conversation: proto.String("hola"), AlbumMessage: &waE2E.AlbumMessage{}}
	assert.Equal(t, inMessage, c.classify(&events.Message{Info: infoDM("MX", false), Message: mixed}).Kind)
}

func TestUnknownTypeLabel(t *testing.T) {
	var labels []string
	c := testConv()
	c.unknown = func(l string) { labels = append(labels, l) }

	// Tipo conocido por la librería pero que no se traduce.
	m := c.convertMessage(infoDM("UK", false), &waE2E.Message{MusicMessage: &waE2E.MusicMessage{}})
	assert.Equal(t, model.KindUnsupported, m.Kind)
	assert.Equal(t, []string{"musicMessage"}, labels)

	// Varios campos: ordenados, sin el ruido de contexto.
	labels = nil
	c.convertMessage(infoDM("UK2", false), &waE2E.Message{
		MusicMessage: &waE2E.MusicMessage{}, StatusMentionMessage: &waE2E.FutureProofMessage{},
		MessageContextInfo: &waE2E.MessageContextInfo{},
	})
	assert.Equal(t, []string{"musicMessage,statusMentionMessage"}, labels)

	// Campo que esta versión de la librería no conoce: se informa su número.
	labels = nil
	raw := &waE2E.Message{}
	raw.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 9999, protowire.VarintType), 1))
	c.convertMessage(infoDM("UK3", false), raw)
	assert.Equal(t, []string{"campo-9999"}, labels)

	// Lo que sí se traduce no se registra, ni lo de «ver una vez», ni los textos.
	labels = nil
	c.convertMessage(infoDM("OK1", false), txt("hola"))
	c.convertMessage(infoDM("OK2", false), &waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("x")}})
	c.convertMessage(infoDM("OK3", false), &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{}}}})
	c.convertMessage(infoDM("OK4", false), nil)
	assert.Empty(t, labels)
}

func TestNoteUnknownTypeLogsOncePerType(t *testing.T) {
	a, _ := newTestAdapter(t)
	var buf strings.Builder
	a.log = slog.New(slog.NewTextHandler(&buf, nil))
	a.noteUnknownType("lottieStickerMessage")
	a.noteUnknownType("lottieStickerMessage")
	a.noteUnknownType("musicMessage")
	a.noteUnknownType("")
	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "tipo de mensaje de WhatsApp no soportado: lottieStickerMessage"))
	assert.Equal(t, 1, strings.Count(out, "tipo de mensaje de WhatsApp no soportado: musicMessage"))
	assert.Contains(t, out, "level=INFO")

	// El convertidor del adaptador ya trae el registro conectado.
	buf.Reset()
	a.conv().convertMessage(infoDM("UK", false), &waE2E.Message{MusicMessage: &waE2E.MusicMessage{}})
	assert.Equal(t, 0, strings.Count(buf.String(), "no soportado"), "ya se registró en esta sesión")
	a.conv().convertMessage(infoDM("UK", false), &waE2E.Message{StatusMentionMessage: &waE2E.FutureProofMessage{}})
	assert.Equal(t, 1, strings.Count(buf.String(), "no soportado: statusMentionMessage"))
}

func TestBuildHistorySkipsMetadataOnly(t *testing.T) {
	c := testConv()
	mk := func(id string, m *waE2E.Message) *waHistorySync.HistorySyncMsg {
		return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:     &waCommon.MessageKey{RemoteJID: proto.String(pnPeer), FromMe: proto.Bool(false), ID: proto.String(id)},
			Message: m, MessageTimestamp: proto.Uint64(uint64(testTs / 1000)),
		}}
	}
	parse := func(chat types.JID, w *waWeb.WebMessageInfo) (*events.Message, error) {
		return &events.Message{Info: infoDM(w.GetKey().GetID(), false), Message: w.GetMessage()}, nil
	}
	data := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{
		ID: proto.String(pnPeer),
		Messages: []*waHistorySync.HistorySyncMsg{
			mk("A", &waE2E.Message{AlbumMessage: &waE2E.AlbumMessage{}}),
			mk("B", &waE2E.Message{Conversation: proto.String("hola")}),
			mk("C", &waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("Cena")}}),
		},
	}}}
	batches := c.buildHistory(data, parse)
	require.Len(t, batches, 1)
	require.Len(t, batches[0].Messages, 2)
	assert.Equal(t, "B", batches[0].Messages[0].NativeID)
	assert.Equal(t, "📅 Evento: Cena", batches[0].Messages[1].Text)
}
