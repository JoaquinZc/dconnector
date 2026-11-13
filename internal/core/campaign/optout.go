package campaign

import (
	"strings"
	"unicode"
)

// normalizeOptOut: sin tildes, en mayúsculas, solo letras, números y espacios simples.
func normalizeOptOut(text string) string {
	s := strings.ToUpper(foldAccents(text))
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		} else {
			space = true
		}
	}
	return b.String()
}

// IsOptOut dice si un mensaje es de baja: solo si el texto COMPLETO es una de las palabras
// clave separadas por comas (sin importar mayúsculas, tildes ni signos): «Baja», «STOP!»,
// «no más». Así una frase normal que contenga «no más» no da de baja al contacto.
func IsOptOut(text, keywords string) bool {
	message := normalizeOptOut(text)
	if message == "" {
		return false
	}
	for _, kw := range strings.Split(keywords, ",") {
		if k := normalizeOptOut(kw); k != "" && k == message {
			return true
		}
	}
	return false
}

// suggestPhrases: frases que suelen pedir que no les escriban. Solo se *sugiere* la baja.
var suggestPhrases = func() []string {
	raw := []string{
		"NO ME ESCRIBAS", "NO ME ESCRIBA", "NO ME ENVIES", "NO ME MANDES", "NO ME INTERESA",
		"NO ESTOY INTERESADO", "NO ESTOY INTERESADA", "DEJA DE", "DEJEN DE", "DEJE DE",
		"NO QUIERO RECIBIR", "NO QUIERO MAS", "NO QUIERO MÁS", "QUITAME", "QUITENME", "BORRAME",
		"BORRENME", "ELIMINAME", "SACAME DE", "NO MOLESTES", "NO MOLESTEN", "QUIEN ERES",
		"DE DONDE SACASTE MI NUMERO", "COMO CONSIGUIO MI NUMERO", "COMO CONSEGUISTE MI NUMERO",
		"SPAM", "TE VOY A BLOQUEAR", "LOS VOY A BLOQUEAR", "VOY A REPORTAR", "DENUNCIAR",
	}
	out := make([]string, len(raw))
	for i, p := range raw {
		out[i] = normalizeOptOut(p)
	}
	return out
}()

// SuggestOptOut dice si parece que no quiere recibir más mensajes: devuelve la frase encontrada
// (para mostrarla) o "". No da de baja: el usuario decide.
func SuggestOptOut(text string) string {
	norm := normalizeOptOut(text)
	if len(norm) < 3 {
		return ""
	}
	message := " " + norm + " "
	// Frases completas (entre espacios): «deja de» no coincide dentro de otra palabra.
	for _, phrase := range suggestPhrases {
		if strings.Contains(message, " "+phrase+" ") {
			return strings.ToLower(phrase)
		}
	}
	return ""
}
