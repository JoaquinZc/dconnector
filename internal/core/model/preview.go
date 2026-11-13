package model

import "strings"

const previewMaxRunes = 80

// Preview devuelve el texto corto con que se muestra un mensaje en la lista de chats
// y en citas. Es común a todos los adaptadores (07-ADAPTADOR-WHATSAPP §6).
func Preview(m Message) string {
	if m.DeletedAt > 0 {
		return "Mensaje eliminado"
	}
	if t := strings.TrimSpace(m.Text); t != "" {
		return truncateRunes(strings.Join(strings.Fields(t), " "), previewMaxRunes)
	}
	switch m.Kind {
	case KindImage:
		return "📷 Foto"
	case KindVideo:
		return "🎥 Vídeo"
	case KindVoice:
		return "🎤 Nota de voz"
	case KindAudio:
		return "🎵 Audio"
	case KindDocument:
		name := ""
		if m.Media != nil {
			name = m.Media.FileName
		}
		if name == "" {
			name = "Documento"
		}
		return truncateRunes("📄 "+name, previewMaxRunes)
	case KindSticker:
		return "Sticker"
	case KindLocation:
		return "📍 Ubicación"
	case KindContact:
		name := ""
		if len(m.Contacts) > 0 {
			name = m.Contacts[0].Name
		}
		if name == "" {
			name = "Contacto"
		}
		return truncateRunes("👤 "+name, previewMaxRunes)
	case KindPoll:
		q := ""
		if m.Poll != nil {
			q = m.Poll.Question
		}
		if q == "" {
			q = "Encuesta"
		}
		return truncateRunes("📊 "+q, previewMaxRunes)
	case KindSystem:
		return ""
	}
	return "Mensaje no compatible"
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
