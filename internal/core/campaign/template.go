package campaign

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Plantillas: se renderizan igual en el envío real y en la vista previa (portado de template.ts).

// TemplateContact: los datos de un contacto que usa una plantilla.
type TemplateContact struct {
	FullName    string
	PhoneNumber string
	// Metadata: columnas extra del contacto (claves libres, se normalizan).
	Metadata map[string]string
	// Nickname: apodo o trato con el que le escribes («primo», «JuanPa», «amiga»).
	Nickname string
	// SalutationForm: forma del trato general ("F" femenino, "M" masculino).
	SalutationForm string
}

// Variables ocultas con el trato general del negocio (van en `extra`).
const (
	TratoGeneralKey = "__trato_general"
	TratoModeKey    = "__trato_modo"
)

// Modos del trato: «nombre» = primer nombre antes que el trato general; «general» = al revés.
const (
	TratoModeNombre  = "nombre"
	TratoModeGeneral = "general"
)

// TratoSettings devuelve las variables ocultas con el trato general del negocio.
func TratoSettings(general, mode string) map[string]string {
	m := TratoModeNombre
	if mode == TratoModeGeneral {
		m = TratoModeGeneral
	}
	return map[string]string{TratoGeneralKey: strings.TrimSpace(general), TratoModeKey: m}
}

// titles: tratamientos que van antes del nombre («Ing. Juan Pablo…»), ya sin tildes.
var titles = func() map[string]bool {
	m := map[string]bool{}
	for _, t := range []string{
		"ing", "inga", "dr", "dra", "lcdo", "lcda", "lic", "licdo", "licda", "abg", "ab", "arq",
		"eco", "econ", "sr", "sra", "srta", "don", "doña", "dona", "msc", "mgs", "mg", "mgtr",
		"phd", "prof", "tlgo", "tlga", "tnlgo", "odont", "psic", "ps", "cpa", "mvz", "ec", "tec",
		"tco", "sgto", "cap", "crnl", "gral", "pdte", "mons", "pbro", "hno", "hna", "sor", "fray",
	} {
		m[foldAccents(t)] = true
	}
	return m
}()

func trimTrailPunct(s string) string { return strings.TrimRight(s, ".,:;") }

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// FirstName: «ING. JUAN PABLO PÉREZ» → «Juan»: sin títulos y con mayúscula inicial.
func FirstName(fullName string) string {
	for _, w := range strings.Fields(fullName) {
		bare := foldAccents(strings.ToLower(trimTrailPunct(w)))
		if bare == "" || titles[bare] || !hasLetter(bare) {
			continue
		}
		clean := trimTrailPunct(w)
		// Respeta «JuanPa» o «McDonald»; solo corrige lo que está todo en mayúsculas o minúsculas.
		if clean == strings.ToUpper(clean) || clean == strings.ToLower(clean) {
			return capitalize(strings.ToLower(clean))
		}
		return clean
	}
	return ""
}

func capitalize(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}

// GeneralTrato: «amigo/amiga» → «amiga» para F; un solo término vale para todos.
func GeneralTrato(general, form string) string {
	parts := strings.Split(general, "/")
	m := strings.TrimSpace(parts[0])
	if len(parts) < 2 {
		return m
	}
	f := strings.TrimSpace(parts[1])
	if f == "" {
		return m
	}
	if form == "F" {
		return f
	}
	return m
}

// TratoFor es {{trato}}: el apodo si lo hay; si no, el primer nombre o el trato general del
// negocio (según el modo).
func TratoFor(c TemplateContact, general, mode string) string {
	if nick := strings.TrimSpace(c.Nickname); nick != "" {
		return nick
	}
	first := FirstName(c.FullName)
	term := ""
	if general != "" {
		term = GeneralTrato(general, c.SalutationForm)
	}
	if mode == TratoModeGeneral {
		if term != "" {
			return term
		}
		return first
	}
	if first != "" {
		return first
	}
	return term
}

// RenderResult: el texto resuelto y las etiquetas que no tuvieron valor.
type RenderResult struct {
	Text string
	// Missing: etiquetas usadas en la plantilla sin valor para este contacto (y sin valor por defecto).
	Missing []string
}

// {{ etiqueta }} o {{ etiqueta | valor por defecto }}
var tagRe = regexp.MustCompile(`\{\{\s*([^{}|]+?)\s*(?:\|\s*([^{}]*?)\s*)?\}\}`)

var spacesRe = regexp.MustCompile(`\s+`)

// NormalizeKey normaliza el nombre de una columna o etiqueta: minúsculas, sin tildes, espacios → «_».
func NormalizeKey(key string) string {
	k := foldAccents(strings.ToLower(strings.TrimSpace(key)))
	return spacesRe.ReplaceAllString(k, "_")
}

// BuildVariables: variables disponibles para un contacto, con claves normalizadas.
func BuildVariables(c TemplateContact) map[string]string {
	vars := map[string]string{}
	keys := make([]string, 0, len(c.Metadata))
	for k := range c.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys) // determinista si dos claves se normalizan igual
	for _, k := range keys {
		vars[NormalizeKey(k)] = strings.TrimSpace(c.Metadata[k])
	}
	// Campos fijos (tienen prioridad sobre columnas con el mismo nombre).
	vars["nombre"] = strings.TrimSpace(c.FullName)
	vars["telefono"] = strings.TrimSpace(c.PhoneNumber)
	// {{apodo}}, {{primer_nombre}} y {{trato}} (este último se completa en RenderTemplate).
	if nick := strings.TrimSpace(c.Nickname); nick != "" {
		vars["apodo"] = nick
	}
	if first := FirstName(c.FullName); first != "" {
		vars["primer_nombre"] = first
	}
	return vars
}

// ExtractTags: etiquetas (normalizadas, sin repetir) que usa una plantilla, en orden de aparición.
func ExtractTags(template string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range tagRe.FindAllStringSubmatch(template, -1) {
		k := NormalizeKey(m[1])
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

var (
	multiSpaceRe  = regexp.MustCompile(`[ \t]{2,}`)
	spaceBeforeRe = regexp.MustCompile(` +([,.;:!?])`)
)

// RenderTemplate resuelve las etiquetas de la plantilla. `extra` son variables comunes a toda la
// campaña; los datos del contacto tienen prioridad si coinciden de nombre.
func RenderTemplate(template string, c TemplateContact, extra map[string]string) RenderResult {
	vars := map[string]string{}
	for k, v := range extra {
		if v != "" {
			vars[NormalizeKey(k)] = v
		}
	}
	for k, v := range BuildVariables(c) {
		vars[k] = v
	}
	mode := TratoModeNombre
	if vars[TratoModeKey] == TratoModeGeneral {
		mode = TratoModeGeneral
	}
	if trato := TratoFor(c, vars[TratoGeneralKey], mode); trato != "" {
		vars["trato"] = trato
	}
	missing := []string{}
	seen := map[string]bool{}

	var b strings.Builder
	last := 0
	for _, m := range tagRe.FindAllStringSubmatchIndex(template, -1) {
		b.WriteString(template[last:m[0]])
		last = m[1]
		key := NormalizeKey(template[m[2]:m[3]])
		if v := vars[key]; v != "" {
			b.WriteString(v)
			continue
		}
		if m[4] >= 0 { // hay valor por defecto (puede ser vacío)
			b.WriteString(template[m[4]:m[5]])
			continue
		}
		if !seen[key] {
			seen[key] = true
			missing = append(missing, key)
		}
	}
	b.WriteString(template[last:])

	// Limpieza de huecos que deja una etiqueta vacía ("Hola  , Ana" -> "Hola, Ana").
	lines := strings.Split(b.String(), "\n")
	for i, line := range lines {
		line = multiSpaceRe.ReplaceAllString(line, " ")
		line = spaceBeforeRe.ReplaceAllString(line, "$1")
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	return RenderResult{Text: strings.TrimSpace(strings.Join(lines, "\n")), Missing: missing}
}
