package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"dconnector/internal/core/port"
)

// Platform: plataforma de Meta a la que pertenece la cuenta.
type Platform string

const (
	PlatformMessenger Platform = "messenger"
	PlatformInstagram Platform = "instagram"
)

// RequiredCookies devuelve las cookies sin las que la sesión no puede abrirse (09 §2).
// Desviación del documento: Messenger pide c_user, xs y datr (lo mismo que exige la librería,
// FBRequiredCookies); fr y mid se aceptan si vienen pero no se exigen, porque messenger.com e
// instagram.com no siempre las entregan y rechazar una sesión válida dejaría al usuario atascado.
func RequiredCookies(p Platform) []string {
	if p == PlatformInstagram {
		return []string{"sessionid", "ds_user_id", "csrftoken"}
	}
	return []string{"c_user", "xs", "datr"}
}

// UserCookie es la cookie que lleva el id numérico del usuario.
func UserCookie(p Platform) string {
	if p == PlatformInstagram {
		return "ds_user_id"
	}
	return "c_user"
}

// MissingCookies devuelve los nombres de las cookies obligatorias que faltan (o vienen vacías).
func MissingCookies(p Platform, c map[string]string) []string {
	var missing []string
	for _, name := range RequiredCookies(p) {
		if strings.TrimSpace(c[name]) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

// UserID devuelve el id numérico de la cookie de usuario ("" si no está).
func UserID(p Platform, c map[string]string) string {
	return strings.TrimSpace(c[UserCookie(p)])
}

// ParseCookies acepta tres formas (09 §2):
//   - JSON: arreglo de {name, value} exportado del navegador;
//   - JSON: objeto {"c_user": "...", "xs": "..."};
//   - cabecera «nombre=valor; nombre2=valor2» (también con el prefijo «Cookie:» o dentro de un comando cURL).
//
// Nunca incluye valores en los errores.
func ParseCookies(input string) (map[string]string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, fmt.Errorf("no pegaste ninguna cookie: %w", port.ErrInvalid)
	}
	out := map[string]string{}
	switch s[0] {
	case '[':
		var arr []map[string]any
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil, fmt.Errorf("el JSON de cookies no se pudo leer: %w", port.ErrInvalid)
		}
		for _, item := range arr {
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			out[strings.TrimSpace(name)] = scalar(item["value"])
		}
	case '{':
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return nil, fmt.Errorf("el JSON de cookies no se pudo leer: %w", port.ErrInvalid)
		}
		for k, v := range obj {
			out[strings.TrimSpace(k)] = scalar(v)
		}
	default:
		header := s
		if h, ok := curlCookieHeader(s); ok {
			header = h
		}
		header = strings.TrimSpace(header)
		if len(header) >= 7 && strings.EqualFold(header[:7], "cookie:") {
			header = header[7:]
		}
		for _, part := range strings.Split(header, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			name = strings.TrimSpace(name)
			if !ok || name == "" {
				continue
			}
			out[name] = cleanValue(value)
		}
	}
	for k, v := range out {
		if v == "" {
			delete(out, k)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no se encontró ninguna cookie en lo que pegaste: %w", port.ErrInvalid)
	}
	return out, nil
}

var (
	curlB      = regexp.MustCompile(`(?s)(?:-b|--cookie)\s+(?:'([^']*)'|"([^"]*)")`)
	curlHeader = regexp.MustCompile(`(?is)(?:-H|--header)\s+(?:'\s*cookie:\s*([^']*)'|"\s*cookie:\s*([^"]*)")`)
)

// curlCookieHeader extrae la cabecera de cookies de un comando cURL copiado de las herramientas del navegador.
func curlCookieHeader(s string) (string, bool) {
	if !strings.Contains(strings.ToLower(s), "curl") {
		return "", false
	}
	for _, re := range []*regexp.Regexp{curlHeader, curlB} {
		if m := re.FindStringSubmatch(s); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					return g, true
				}
			}
		}
	}
	return "", false
}

func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return cleanValue(t)
	case float64:
		return fmt.Sprintf("%.0f", t)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return ""
	}
}

// cleanValue quita comillas envolventes y espacios; las cookies del navegador a veces vienen con %XX
// ya codificado y así se dejan (la librería las manda tal cual).
func cleanValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	return v
}

// ValidateCookies interpreta el texto y comprueba que estén las obligatorias de la plataforma.
// El error dice cuáles faltan (solo nombres, nunca valores).
func ValidateCookies(p Platform, input string) (map[string]string, error) {
	c, err := ParseCookies(input)
	if err != nil {
		return nil, err
	}
	if missing := MissingCookies(p, c); len(missing) > 0 {
		return nil, fmt.Errorf("faltan estas cookies: %s: %w", strings.Join(missing, ", "), port.ErrInvalid)
	}
	if id := UserID(p, c); !isDigits(id) {
		return nil, fmt.Errorf("la cookie %s no es un número de usuario válido: %w", UserCookie(p), port.ErrInvalid)
	}
	return c, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Header arma «nombre=valor; …» con orden estable (para pruebas y para descargas autenticadas).
func Header(c map[string]string) string {
	names := make([]string, 0, len(c))
	for k := range c {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for i, k := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(c[k])
	}
	return b.String()
}

// NamesOnly devuelve los nombres de las cookies (para registrar sin valores).
func NamesOnly(c map[string]string) []string {
	names := make([]string, 0, len(c))
	for k := range c {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// SaveCookies guarda las cookies en kv["meta.cookies"] (JSON).
func SaveCookies(ctx context.Context, kv *KV, c map[string]string) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return kv.Set(ctx, KeyCookies, b)
}

// LoadCookies lee kv["meta.cookies"]; nil si no hay sesión guardada.
func LoadCookies(ctx context.Context, kv *KV) (map[string]string, error) {
	b, ok, err := kv.Get(ctx, KeyCookies)
	if err != nil || !ok {
		return nil, err
	}
	var c map[string]string
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("las cookies guardadas están dañadas: %w", err)
	}
	if len(c) == 0 {
		return nil, nil
	}
	return c, nil
}
