package campaign

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Campañas seguras: variantes del mensaje para que no todos reciban el mismo texto. Todo es por
// reglas, sin IA (portado de variants.ts).
//
// Dentro de un texto:
//   - {Hola|Buenas|Qué tal} elige una opción al azar para cada persona;
//   - {saludo}, {despedida} y {emoji} rotan entre listas comunes;
//   - las etiquetas {{Nombre}} no se tocan (las rellena RenderTemplate).

const (
	MaxVariants     = 5
	MinVariantsFrom = 30
	RequiredCount   = 3
)

// VariantLetters: la letra de cada variante (A es el texto principal).
var VariantLetters = []string{"A", "B", "C", "D", "E"}

// Rotations: listas comunes que rotan con {saludo}, {despedida} y {emoji}.
var Rotations = map[string][]string{
	"saludo":    {"Hola", "Buenas", "Qué tal", "Hola, ¿cómo estás?", "Saludos"},
	"despedida": {"Saludos", "Un abrazo", "Gracias", "Quedo atento", "Que tengas un buen día"},
	"emoji":     {"🙂", "😊", "👋", "✨", "🙌", "👍"},
}

// SeededRandom: generador pseudoaleatorio reproducible (xorshift32 sembrado con FNV-1a). Da la
// MISMA secuencia que seededRandom de variants.ts para la misma semilla.
func SeededRandom(seed string) func() float64 {
	h := uint32(2166136261)
	for _, r := range seed {
		// En JS, `ch.charCodeAt(0)` es la primera unidad UTF-16 del carácter: para los fuera del
		// plano básico (emojis) es el sustituto alto.
		unit := uint32(r)
		if r >= 0x10000 {
			r1, _ := utf16.EncodeRune(r)
			unit = uint32(r1)
		}
		h = (h ^ unit) * 16777619
	}
	x := h
	if x == 0 {
		x = 1
	}
	return func() float64 {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		return float64(x%1_000_000) / 1_000_000
	}
}

// scanChoices recorre las llaves simples `{…}` que no son `{{…}}` (equivale a la expresión
// /(?<!\{)\{([^{}]+)\}(?!\})/g de la base) y llama a fn con cada una. Devuelve el texto con
// cada coincidencia reemplazada por lo que devuelva fn.
func scanChoices(text string, fn func(full, inner string) string) string {
	var b strings.Builder
	i, n := 0, len(text)
	for i < n {
		if text[i] == '{' && (i == 0 || text[i-1] != '{') {
			j := i + 1
			for j < n && text[j] != '{' && text[j] != '}' {
				j++
			}
			if j > i+1 && j < n && text[j] == '}' && (j+1 >= n || text[j+1] != '}') {
				b.WriteString(fn(text[i:j+1], text[i+1:j]))
				i = j + 1
				continue
			}
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

// choiceOptions: las opciones de `{a|b}` o de una rotación; nil si no es ninguna de las dos.
func choiceOptions(inner string) []string {
	if strings.Contains(inner, "|") {
		parts := strings.Split(inner, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return Rotations[strings.ToLower(strings.TrimSpace(inner))]
}

// Spin expande opciones y rotaciones. `rnd` decide (mismo `rnd` = mismo resultado); nil usa
// math/rand.
func Spin(text string, rnd func() float64) string {
	if rnd == nil {
		rnd = rand.Float64
	}
	return scanChoices(text, func(full, inner string) string {
		opts := choiceOptions(inner)
		if len(opts) == 0 {
			return full
		}
		return opts[int(math.Floor(rnd()*float64(len(opts))))%len(opts)]
	})
}

// SpinCount: cuántos textos distintos puede dar un texto (producto de sus opciones).
func SpinCount(text string) int {
	n := 1
	capped := false
	scanChoices(text, func(full, inner string) string {
		if capped {
			return full
		}
		k := 1
		if strings.Contains(inner, "|") {
			k = len(strings.Split(inner, "|"))
		} else if l := len(Rotations[strings.ToLower(strings.TrimSpace(inner))]); l > 0 {
			k = l
		}
		n *= max(1, k)
		if n > 1_000_000 {
			n, capped = 1_000_000, true
		}
		return full
	})
	return n
}

// DistinctTexts: textos distintos entre todas las variantes.
func DistinctTexts(variants []string) int {
	total := 0
	for _, v := range variants {
		if strings.TrimSpace(v) != "" {
			total += SpinCount(v)
		}
	}
	return total
}

// ---- Parecido ----

var (
	tagStripRe   = regexp.MustCompile(`\{\{[^}]*\}\}`)
	nonWordRe    = regexp.MustCompile(`[^a-z0-9ñ\s]`)
	fieldsSplitS = regexp.MustCompile(`\s+`)
)

func normalizeWords(text string) []string {
	t := Spin(text, func() float64 { return 0 })
	t = tagStripRe.ReplaceAllString(t, " ")
	t = foldAccents(strings.ToLower(t))
	t = nonWordRe.ReplaceAllString(t, " ")
	out := []string{}
	for _, w := range fieldsSplitS.Split(t, -1) {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

func shingles(words []string, k int) map[string]bool {
	out := map[string]bool{}
	if len(words) < k {
		for _, w := range words {
			out[w] = true
		}
	}
	for i := 0; i+k <= len(words); i++ {
		out[strings.Join(words[i:i+k], " ")] = true
	}
	return out
}

// Similarity: parecido entre dos textos (0–100), por pares de palabras en común.
func Similarity(a, b string) int {
	A := shingles(normalizeWords(a), 2)
	B := shingles(normalizeWords(b), 2)
	if len(A) == 0 && len(B) == 0 {
		return 100
	}
	common := 0
	for s := range A {
		if B[s] {
			common++
		}
	}
	return int(math.Round(float64(common) / float64(len(A)+len(B)-common) * 100))
}

// PairSimilarity: el par de variantes que más se parecen.
type PairSimilarity struct {
	Value int
	// Pair: índices (en la lista dada) de las dos variantes; nil si no hay par.
	Pair *[2]int
}

// MaxSimilarity devuelve el par de variantes que más se parecen (ignora las vacías).
func MaxSimilarity(variants []string) PairSimilarity {
	best := PairSimilarity{}
	type iv struct {
		text string
		idx  int
	}
	var list []iv
	for i, v := range variants {
		if strings.TrimSpace(v) != "" {
			list = append(list, iv{v, i})
		}
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if s := Similarity(list[i].text, list[j].text); s > best.Value {
				best = PairSimilarity{Value: s, Pair: &[2]int{list[i].idx, list[j].idx}}
			}
		}
	}
	return best
}

// Niveles del semáforo de la revisión.
const (
	LevelGreen  = "green"
	LevelYellow = "yellow"
	LevelRed    = "red"
)

// SimilarityLevel: qué tan distintas son: verde (< 60 %), amarillo (60–80 %), rojo (> 80 %).
func SimilarityLevel(value int) string {
	switch {
	case value > 80:
		return LevelRed
	case value >= 60:
		return LevelYellow
	}
	return LevelGreen
}

// ---- Reparto ----

// Assignment: a qué variante (0 = A) le toca un destinatario.
type Assignment struct {
	ID      string
	Variant int
}

// AssignVariants reparte las variantes entre los destinatarios en partes iguales (la diferencia
// es como mucho de uno), en un orden mezclado para que no salgan en bloque. El resultado va en
// ese orden mezclado (el mismo que el de la base).
func AssignVariants(ids []string, count int, seed string) []Assignment {
	n := max(1, min(MaxVariants, count))
	rnd := SeededRandom(seed)
	type keyed struct {
		id string
		r  float64
	}
	order := make([]keyed, len(ids))
	for i, id := range ids {
		order[i] = keyed{id, rnd()}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].r < order[j].r })
	out := make([]Assignment, len(order))
	for i, k := range order {
		out[i] = Assignment{ID: k.id, Variant: i % n}
	}
	return out
}

// ---- Revisión del mensaje ----

var (
	shortenersRe = regexp.MustCompile(`(?i)\b(bit\.ly|tinyurl\.com|goo\.gl|t\.co|ow\.ly|is\.gd|cutt\.ly|rebrand\.ly|shorturl\.at|acortar\.link)\b`)
	urlRe        = regexp.MustCompile(`(?i)(https?://\S+|www\.\S+)`)
	nameTagRe    = regexp.MustCompile(`(?i)\{\{\s*nombre`)
)

var spamWords = []string{
	"gratis", "gana dinero", "ganaste", "ganador", "urgente", "última oportunidad", "haz clic",
	"click aquí", "clic aquí", "100% garantizado", "sin costo", "dinero fácil", "oferta limitada",
	"premio", "promoción exclusiva", "inversión segura", "préstamo", "crédito inmediato",
}

// ReviewMessage revisa el texto de una variante. `others` son textos de otras campañas recientes.
func ReviewMessage(text string, others []string) []ReviewItem {
	t := Spin(text, func() float64 { return 0 })
	items := []ReviewItem{}
	add := func(level, code, msg string) {
		items = append(items, ReviewItem{Level: level, Code: code, Message: msg})
	}

	links := urlRe.FindAllString(t, -1)
	short := shortenersRe.MatchString(t)
	if short {
		add(LevelRed, "shortener", "Tiene un enlace acortado (bit.ly y similares): WhatsApp lo trata como sospechoso.")
	}
	if len(links) > 1 {
		add(LevelYellow, "links", fmt.Sprintf("Tiene %d enlaces: deja uno solo.", len(links)))
	} else if len(links) == 1 && !short {
		add(LevelYellow, "links", "Lleva un enlace: con quien no te conoce, mejor envíalo cuando responda.")
	}

	letters, upper := 0, 0
	for _, r := range tagStripRe.ReplaceAllString(t, "") {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	if letters >= 20 {
		ratio := float64(upper) / float64(letters)
		if ratio > 0.6 {
			add(LevelRed, "caps", "Casi todo está en MAYÚSCULAS: parece un grito o spam.")
		} else if ratio > 0.3 {
			add(LevelYellow, "caps", "Muchas MAYÚSCULAS: úsalas solo para resaltar.")
		}
	}

	emojis := 0
	for _, r := range t {
		if isPictographic(r) {
			emojis++
		}
	}
	if emojis > 12 {
		add(LevelRed, "emojis", fmt.Sprintf("%d emojis: demasiados.", emojis))
	} else if emojis > 6 {
		add(LevelYellow, "emojis", fmt.Sprintf("%d emojis: con 2 o 3 basta.", emojis))
	}

	lower := strings.ToLower(t)
	var spam []string
	for _, w := range spamWords {
		if strings.Contains(lower, w) {
			spam = append(spam, "«"+w+"»")
		}
	}
	if len(spam) > 0 {
		level := LevelYellow
		if len(spam) >= 3 {
			level = LevelRed
		}
		add(level, "spam", "Palabras típicas de spam: "+strings.Join(spam, ", ")+".")
	}

	// Longitud en unidades UTF-16, como el `length` de la base.
	n := len(utf16.Encode([]rune(t)))
	if n > 1500 {
		add(LevelRed, "length", fmt.Sprintf("Muy largo (%d caracteres): acórtalo.", n))
	} else if n > 700 {
		add(LevelYellow, "length", fmt.Sprintf("Largo (%d caracteres): lo corto se lee más.", n))
	}

	if !nameTagRe.MatchString(text) {
		add(LevelYellow, "personal", "No usa {{Nombre}}: un mensaje personal se reporta mucho menos.")
	}

	// Avisa, pero no bloquea: duplicar una campaña para repetirla es válido.
	repeated := 0
	for _, o := range others {
		repeated = max(repeated, Similarity(text, o))
	}
	if repeated >= 70 {
		if repeated >= 90 {
			add(LevelYellow, "repeated", "Es casi igual al de otra campaña reciente: cámbialo un poco o usa variantes.")
		} else {
			add(LevelYellow, "repeated", "Se parece mucho al de otra campaña reciente.")
		}
	}
	return items
}

// WorstLevel: el peor nivel (el semáforo de la campaña).
func WorstLevel(items []ReviewItem) string {
	level := LevelGreen
	for _, it := range items {
		switch it.Level {
		case LevelRed:
			return LevelRed
		case LevelYellow:
			level = LevelYellow
		}
	}
	return level
}

// RequiredVariants: variantes que hacen falta según los destinatarios (0 = no son obligatorias).
func RequiredVariants(recipients int) int {
	if recipients >= MinVariantsFrom {
		return RequiredCount
	}
	return 0
}
