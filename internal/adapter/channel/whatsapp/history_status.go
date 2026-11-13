package whatsapp

// Estados que llegan dentro de la sincronización de historial (07 §10). Al vincular una cuenta nueva, WhatsApp
// manda los estados que ya existían (de los últimos días) en el campo StatusV3Messages de un HistorySync de tipo
// INITIAL_STATUS_V3, y no como mensajes sueltos: si no se leen, esos estados nunca aparecen. Los que se publican
// después llegan en vivo por onMessage.

import (
	"sort"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/reflect/protoreflect"

	"dconnector/internal/core/model"
)

// statusesFromHistory extrae los estados vigentes de un HistorySync: los de StatusV3Messages y los de una
// conversación «status@broadcast» si la trajera. Descarta los vencidos (más de 24 h), los borrados y lo que no
// es texto, foto ni vídeo. Función pura salvo por `parse`.
func (c converter) statusesFromHistory(data *waHistorySync.HistorySync, parse webParser) []model.Status {
	if data == nil || parse == nil {
		return nil
	}
	var web []*waWeb.WebMessageInfo
	web = append(web, data.GetStatusV3Messages()...)
	for _, conv := range data.GetConversations() {
		jid, err := types.ParseJID(conv.GetID())
		if err != nil || !isStatusJID(jid) {
			continue
		}
		for _, hm := range conv.GetMessages() {
			if w := hm.GetMessage(); w != nil {
				web = append(web, w)
			}
		}
	}
	now := c.clock().UnixMilli()
	seen := map[string]bool{}
	var out []model.Status
	for _, w := range web {
		evt, err := parse(types.StatusBroadcastJID, w)
		if err != nil || evt == nil {
			continue
		}
		in := c.classify(evt)
		if in.Kind != inStatus || in.Status.ExpiresAt <= now || seen[in.Status.Key] {
			continue
		}
		seen[in.Status.Key] = true
		out = append(out, in.Status)
	}
	return out
}

// messageKindName: nombres de los campos que trae un mensaje (p. ej. «imageMessage,messageContextInfo»), para el
// registro de diagnóstico: dice de qué tipo es sin guardar texto ni datos personales.
func messageKindName(m *waE2E.Message) string {
	if m == nil {
		return "vacío"
	}
	var names []string
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		names = append(names, string(fd.Name()))
		return true
	})
	if len(names) == 0 {
		return "vacío"
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}
