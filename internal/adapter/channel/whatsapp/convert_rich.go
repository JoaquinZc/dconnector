package whatsapp

// Tipos de mensaje «raros» de WhatsApp (botones, listas, plantillas, invitaciones, llamadas…) y
// los que no se deben guardar (solo metadatos). Funciones puras: se prueban con protos a mano.

import (
	"fmt"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"

	"dconnector/internal/core/model"
)

// viewOnceText: lo que se muestra en lugar de una foto, vídeo o audio de «ver una vez».
const viewOnceText = "Mensaje de una sola vez: ábrelo en tu teléfono"

// richMaxRunes: tope del texto legible de un mensaje con botones, lista o plantilla.
const richMaxRunes = 600

// hasViewOnceWrapper: el mensaje aún trae el envoltorio de «ver una vez» (whatsmeow suele
// quitarlo y deja la marca IsViewOnce en el evento; el historial puede traerlo sin quitar).
func hasViewOnceWrapper(m *waE2E.Message) bool {
	for i := 0; i < 8 && m != nil; i++ {
		switch {
		case m.GetViewOnceMessage() != nil, m.GetViewOnceMessageV2() != nil, m.GetViewOnceMessageV2Extension() != nil:
			return true
		}
		var next *waE2E.Message
		switch {
		case m.GetDeviceSentMessage().GetMessage() != nil:
			next = m.GetDeviceSentMessage().GetMessage()
		case m.GetEphemeralMessage().GetMessage() != nil:
			next = m.GetEphemeralMessage().GetMessage()
		case m.GetBotInvokeMessage().GetMessage() != nil:
			next = m.GetBotInvokeMessage().GetMessage()
		}
		m = next
	}
	return false
}

// isMediaKind: tipos de contenido que se reemplazan por el aviso de «ver una vez».
func isMediaKind(k model.ContentKind) bool {
	switch k {
	case model.KindImage, model.KindVideo, model.KindAudio, model.KindVoice:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------------------------
// Texto legible de mensajes con estructura

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func clip(s string) string { return truncate(s, richMaxRunes) }

func quoted(s string) string { return "«" + s + "»" }

// templateText: texto de una plantilla «hydrated» (título, cuerpo, pie y botones).
func templateText(t *waE2E.TemplateMessage) string {
	if t == nil {
		return ""
	}
	h := t.GetHydratedTemplate()
	if h == nil {
		h = t.GetHydratedFourRowTemplate()
	}
	if h == nil {
		return ""
	}
	var buttons []string
	for _, b := range h.GetHydratedButtons() {
		switch {
		case b.GetQuickReplyButton() != nil:
			buttons = append(buttons, b.GetQuickReplyButton().GetDisplayText())
		case b.GetUrlButton() != nil:
			buttons = append(buttons, b.GetUrlButton().GetDisplayText())
		case b.GetCallButton() != nil:
			buttons = append(buttons, b.GetCallButton().GetDisplayText())
		}
	}
	return joinNonEmpty("\n", h.GetHydratedTitleText(), h.GetHydratedContentText(), h.GetHydratedFooterText(), optionsLine(buttons))
}

// optionsLine: «Opciones: A · B · C» (vacío si no hay).
func optionsLine(opts []string) string {
	var clean []string
	for _, o := range opts {
		if o = strings.TrimSpace(o); o != "" {
			clean = append(clean, o)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return "Opciones: " + strings.Join(clean, " · ")
}

func callOutcomeText(c *waE2E.CallLogMessage) string {
	video := c.GetIsVideo()
	name := "Llamada"
	if video {
		name = "Videollamada"
	}
	switch c.GetCallOutcome() {
	case waE2E.CallLogMessage_MISSED:
		return name + " perdida"
	case waE2E.CallLogMessage_REJECTED:
		return name + " rechazada"
	case waE2E.CallLogMessage_FAILED:
		return name + " fallida"
	case waE2E.CallLogMessage_ACCEPTED_ELSEWHERE:
		return name + " atendida en otro dispositivo"
	case waE2E.CallLogMessage_ONGOING:
		return name + " en curso"
	case waE2E.CallLogMessage_SILENCED_BY_DND, waE2E.CallLogMessage_SILENCED_UNKNOWN_CALLER:
		return name + " silenciada"
	}
	if d := c.GetDurationSecs(); d > 0 {
		return fmt.Sprintf("%s de %d:%02d", name, d/60, d%60)
	}
	return name
}

// richContent traduce los mensajes que solo traen texto estructurado o un aviso. ok=false si el
// mensaje no es de este grupo. Nunca devuelve datos de medios.
func richContent(m *waE2E.Message) (kind model.ContentKind, text string, ok bool) {
	t := func(s string) (model.ContentKind, string, bool) { return model.KindText, clip(s), true }
	switch {
	// Botones, listas, plantillas e interactivos.
	case m.GetButtonsMessage() != nil:
		b := m.GetButtonsMessage()
		var labels []string
		for _, bt := range b.GetButtons() {
			labels = append(labels, bt.GetButtonText().GetDisplayText())
		}
		s := joinNonEmpty("\n", b.GetText(), b.GetContentText(), b.GetFooterText(), optionsLine(labels))
		if s == "" {
			s = "Mensaje con botones"
		}
		return t(s)
	case m.GetListMessage() != nil:
		l := m.GetListMessage()
		s := joinNonEmpty("\n", l.GetTitle(), l.GetDescription(), l.GetFooterText())
		if s == "" {
			s = "Lista de opciones"
		}
		return t(s)
	case m.GetTemplateMessage() != nil:
		s := templateText(m.GetTemplateMessage())
		if s == "" {
			s = "Mensaje de plantilla"
		}
		return t(s)
	case m.GetHighlyStructuredMessage() != nil:
		h := m.GetHighlyStructuredMessage()
		s := templateText(h.GetHydratedHsm())
		if s == "" {
			s = joinNonEmpty("\n", h.GetFallbackLg(), h.GetFallbackLc(), h.GetDeterministicLg())
		}
		if s == "" {
			s = "Mensaje de plantilla"
		}
		return t(s)
	case m.GetInteractiveMessage() != nil:
		im := m.GetInteractiveMessage()
		s := joinNonEmpty("\n", im.GetHeader().GetTitle(), im.GetHeader().GetSubtitle(), im.GetBody().GetText(), im.GetFooter().GetText())
		if s == "" {
			s = "Mensaje interactivo"
		}
		return t(s)

	// Respuestas a botones, listas y plantillas.
	case m.GetButtonsResponseMessage() != nil:
		r := m.GetButtonsResponseMessage()
		s := joinNonEmpty(" ", r.GetSelectedDisplayText())
		if s == "" {
			s = "Respuesta a un mensaje con botones"
		}
		return t(s)
	case m.GetListResponseMessage() != nil:
		r := m.GetListResponseMessage()
		s := joinNonEmpty("\n", r.GetTitle(), r.GetDescription())
		if s == "" {
			s = "Respuesta a una lista de opciones"
		}
		return t(s)
	case m.GetTemplateButtonReplyMessage() != nil:
		s := joinNonEmpty(" ", m.GetTemplateButtonReplyMessage().GetSelectedDisplayText())
		if s == "" {
			s = "Respuesta a una plantilla"
		}
		return t(s)
	case m.GetInteractiveResponseMessage() != nil:
		s := joinNonEmpty(" ", m.GetInteractiveResponseMessage().GetBody().GetText())
		if s == "" {
			s = "Respuesta a un mensaje interactivo"
		}
		return t(s)

	// Invitaciones, eventos y comercio.
	case m.GetGroupInviteMessage() != nil:
		g := m.GetGroupInviteMessage()
		if name := strings.TrimSpace(g.GetGroupName()); name != "" {
			return t("Invitación al grupo " + quoted(name))
		}
		return t("Invitación a un grupo")
	case m.GetEventMessage() != nil:
		e := m.GetEventMessage()
		name := strings.TrimSpace(e.GetName())
		if name == "" {
			name = "sin nombre"
		}
		s := "📅 Evento: " + name
		if e.GetIsCanceled() {
			s += " (cancelado)"
		}
		return t(s)
	case m.GetEventInviteMessage() != nil:
		e := m.GetEventInviteMessage()
		name := strings.TrimSpace(e.GetEventTitle())
		if name == "" {
			name = "sin nombre"
		}
		s := "📅 Evento: " + name
		if e.GetIsCanceled() {
			s += " (cancelado)"
		}
		return t(s)
	case m.GetOrderMessage() != nil:
		o := m.GetOrderMessage()
		s := "🛒 Pedido"
		if title := strings.TrimSpace(o.GetOrderTitle()); title != "" {
			s += ": " + title
		}
		if n := o.GetItemCount(); n > 0 {
			s += fmt.Sprintf(" (%d artículos)", n)
		}
		return t(joinNonEmpty("\n", s, o.GetMessage()))
	case m.GetProductMessage() != nil:
		p := m.GetProductMessage()
		s := "🛍️ Producto"
		if title := strings.TrimSpace(p.GetProduct().GetTitle()); title != "" {
			s += ": " + title
		}
		return t(joinNonEmpty("\n", s, p.GetBody()))
	case m.GetInvoiceMessage() != nil:
		return t(joinNonEmpty("\n", "🧾 Factura", m.GetInvoiceMessage().GetNote()))
	case m.GetRequestPaymentMessage() != nil:
		return t("💳 Solicitud de pago")
	case m.GetSendPaymentMessage() != nil:
		return t("💳 Pago")
	case m.GetPaymentInviteMessage() != nil:
		return t("💳 Invitación a usar pagos de WhatsApp")
	case m.GetStickerPackMessage() != nil:
		name := strings.TrimSpace(m.GetStickerPackMessage().GetName())
		if name == "" {
			return t("Paquete de stickers")
		}
		return t("Paquete de stickers: " + name)
	case m.GetNewsletterAdminInviteMessage() != nil:
		name := strings.TrimSpace(m.GetNewsletterAdminInviteMessage().GetNewsletterName())
		if name == "" {
			return t("Invitación para administrar un canal")
		}
		return t("Invitación para administrar el canal " + quoted(name))
	case m.GetNewsletterFollowerInviteMessageV2() != nil:
		name := strings.TrimSpace(m.GetNewsletterFollowerInviteMessageV2().GetNewsletterName())
		if name == "" {
			return t("Invitación a un canal")
		}
		return t("Invitación al canal " + quoted(name))
	case m.GetScheduledCallCreationMessage() != nil:
		title := strings.TrimSpace(m.GetScheduledCallCreationMessage().GetTitle())
		if title == "" {
			return t("📞 Llamada programada")
		}
		return t("📞 Llamada programada: " + title)
	case m.GetPollResultSnapshotMessage() != nil:
		name := strings.TrimSpace(m.GetPollResultSnapshotMessage().GetName())
		if name == "" {
			return t("📊 Resultados de una encuesta")
		}
		return t("📊 Resultados de la encuesta: " + name)

	// Llamadas: aviso de sistema.
	case m.GetCallLogMesssage() != nil:
		return model.KindSystem, callOutcomeText(m.GetCallLogMesssage()), true
	}
	return "", "", false
}

// ---------------------------------------------------------------------------------------------
// Mensajes que solo son metadatos

// ignoredFields: campos de waE2E.Message que son solo metadatos o cifrado interno. Un mensaje
// cuyo único contenido sean estos campos no se muestra ni se guarda.
var ignoredFields = map[string]bool{
	"albumMessage":                          true, // el padre de un álbum: los hijos son ImageMessage/VideoMessage normales
	"pinInChatMessage":                      true,
	"keepInChatMessage":                     true,
	"placeholderMessage":                    true,
	"requestPhoneNumberMessage":             true,
	"messageHistoryBundle":                  true,
	"messageHistoryNotice":                  true,
	"encReactionMessage":                    true,
	"encCommentMessage":                     true,
	"encEventResponseMessage":               true,
	"secretEncryptedMessage":                true,
	"stickerSyncRmrMessage":                 true,
	"pollAddOptionMessage":                  true,
	"groupRootKeyShare":                     true,
	"rootSecretDistributeMessage":           true,
	"scheduledCallEditMessage":              true,
	"statusNotificationMessage":             true,
	"statusLinkPreviewMetadata":             true,
	"bcallMessage":                          true,
	"chat":                                  true,
	"limitSharingMessage":                   true,
	"acp2SettingMessage":                    true,
	"botPlatformRegistrationSuccessMessage": true,
}

// noiseFields: campos que acompañan a cualquier mensaje y no cuentan como contenido.
var noiseFields = map[string]bool{
	"messageContextInfo":                         true,
	"senderKeyDistributionMessage":               true,
	"fastRatchetKeySenderKeyDistributionMessage": true,
}

// presentFields devuelve, ordenados, los nombres de los campos presentes del mensaje (sin los de
// ruido) y, aparte, los números de campo que esta versión de la librería no conoce.
func presentFields(m *waE2E.Message) (names []string, unknown []protowire.Number) {
	if m == nil {
		return nil, nil
	}
	r := m.ProtoReflect()
	r.Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if n := string(fd.Name()); !noiseFields[n] {
			names = append(names, n)
		}
		return true
	})
	sort.Strings(names)
	raw := r.GetUnknown()
	for len(raw) > 0 {
		num, typ, n := protowire.ConsumeTag(raw)
		if n < 0 {
			break
		}
		unknown = append(unknown, num)
		skip := protowire.ConsumeFieldValue(num, typ, raw[n:])
		if skip < 0 {
			break
		}
		raw = raw[n+skip:]
	}
	return names, unknown
}

// isMetadataOnly: todo lo que trae el mensaje es metadato (ver ignoredFields). Un mensaje vacío
// no cuenta: de eso se encarga onlySessionKeys.
func isMetadataOnly(m *waE2E.Message) bool {
	names, unknown := presentFields(m)
	if len(names) == 0 || len(unknown) > 0 {
		return false
	}
	for _, n := range names {
		if !ignoredFields[n] {
			return false
		}
	}
	return true
}

// unknownTypeLabel: nombre del tipo de mensaje no soportado, para el registro de diagnóstico.
// Nunca incluye contenido ni teléfonos, solo nombres de campos del protocolo.
func unknownTypeLabel(m *waE2E.Message) string {
	names, unknown := presentFields(m)
	for _, n := range unknown {
		names = append(names, fmt.Sprintf("campo-%d", n))
	}
	return strings.Join(names, ",")
}
