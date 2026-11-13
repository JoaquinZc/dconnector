package campaign

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Valores calculados con Node ejecutando seededRandom de ../dconnector/src/shared/variants.ts
// (mismo algoritmo: FNV-1a + xorshift32). Son los 5 primeros números de cada semilla.
var seededWant = map[string][]float64{
	"1":               {0.998795, 0.112971, 0.069412, 0.857711, 0.139394},
	"campaign":        {0.646432, 0.013372, 0.162297, 0.042722, 0.590593},
	"camp-01|ñandú 🙂": {0.471991, 0.543112, 0.355697, 0.280208, 0.267835},
}

func TestSeededRandomMatchesNode(t *testing.T) {
	for seed, want := range seededWant {
		rnd := SeededRandom(seed)
		for i, w := range want {
			if got := rnd(); got != w {
				t.Errorf("semilla %q, valor %d = %v, quería %v", seed, i+1, got, w)
			}
		}
	}
}

func TestSeededRandomReproducible(t *testing.T) {
	a, b := SeededRandom("x"), SeededRandom("x")
	for i := 0; i < 20; i++ {
		if a() != b() {
			t.Fatal("la misma semilla dio secuencias distintas")
		}
	}
	if SeededRandom("x")() == SeededRandom("y")() {
		t.Error("semillas distintas dieron el mismo primer valor")
	}
	// Valores siempre en [0, 1).
	r := SeededRandom("rango")
	for i := 0; i < 1000; i++ {
		if v := r(); v < 0 || v >= 1 {
			t.Fatalf("fuera de rango: %v", v)
		}
	}
}

func TestSpinOptionsAndRotations(t *testing.T) {
	tpl := "{Hola|Buenas} {{Nombre}}, {saludo}. {emoji} {desconocido}"
	a := Spin(tpl, SeededRandom("1"))
	if !regexp.MustCompile(`^(Hola|Buenas) \{\{Nombre\}\}, `).MatchString(a) {
		t.Errorf("no empieza como se esperaba: %q", a)
	}
	if !strings.Contains(a, "{desconocido}") {
		t.Errorf("tocó una llave desconocida: %q", a)
	}
	r := RenderTemplate(a, TemplateContact{FullName: "Ana"}, nil).Text
	if !regexp.MustCompile(`^(Hola|Buenas) Ana,`).MatchString(r) {
		t.Errorf("render: %q", r)
	}
	if Spin(tpl, SeededRandom("1")) != a {
		t.Error("mismo generador, distinto texto")
	}
	if got := SpinCount("{a|b|c} y {x|y}"); got != 6 {
		t.Errorf("SpinCount = %d", got)
	}
	if got := SpinCount("{{Nombre}} {saludo}"); got != 5 {
		t.Errorf("SpinCount con rotación = %d", got)
	}
	if got := DistinctTexts([]string{"{a|b}", "fijo", ""}); got != 3 {
		t.Errorf("DistinctTexts = %d", got)
	}
}

func TestSpinBracesEdgeCases(t *testing.T) {
	zero := func() float64 { return 0 }
	cases := map[string]string{
		"{{a}}":          "{{a}}",
		"{a}}":           "{a}}",
		"{{a}":           "{{a}",
		"{a|b}{c|d}":     "ac",
		"{}":             "{}",
		"{ a | b }":      "a",
		"x {Hola} y":     "x {Hola} y", // una sola opción sin | que no es rotación: se queda
		"{SALUDO} amigo": "Hola amigo",
	}
	for in, want := range cases {
		if got := Spin(in, zero); got != want {
			t.Errorf("Spin(%q) = %q, quería %q", in, got, want)
		}
	}
}

func TestSimilarity(t *testing.T) {
	if got := Similarity("Hola {{Nombre}}, tenemos oferta", "Hola {{Nombre}}, tenemos oferta"); got != 100 {
		t.Errorf("iguales = %d", got)
	}
	if got := Similarity("Hola, tenemos pan fresco hoy", "Mañana llega el pedido de flores"); got >= 20 {
		t.Errorf("distintos = %d", got)
	}
	m := MaxSimilarity([]string{"uno dos tres cuatro", "cinco seis siete ocho", "uno dos tres cinco"})
	if m.Pair == nil || *m.Pair != [2]int{0, 2} {
		t.Errorf("par = %v", m.Pair)
	}
	if got := MaxSimilarity([]string{"solo uno", ""}); got.Pair != nil || got.Value != 0 {
		t.Errorf("un solo texto: %+v", got)
	}
	if SimilarityLevel(80) != LevelYellow || SimilarityLevel(81) != LevelRed || SimilarityLevel(59) != LevelGreen || SimilarityLevel(60) != LevelYellow {
		t.Error("niveles de parecido")
	}
}

func TestAssignVariantsBalancedAndReproducible(t *testing.T) {
	ids := make([]string, 31)
	for i := range ids {
		ids[i] = fmt.Sprintf("c%d", i)
	}
	a := AssignVariants(ids, 3, "x")
	counts := [3]int{}
	for _, x := range a {
		counts[x.Variant]++
	}
	if d := max(counts[0], counts[1], counts[2]) - min(counts[0], counts[1], counts[2]); d > 1 {
		t.Errorf("reparto desigual: %v", counts)
	}
	if !reflect.DeepEqual(a, AssignVariants(ids, 3, "x")) {
		t.Error("no es reproducible")
	}
	if len(a) != len(ids) {
		t.Errorf("len = %d", len(a))
	}
	if RequiredVariants(29) != 0 || RequiredVariants(30) != 3 {
		t.Error("RequiredVariants")
	}
	// Mismo orden que calcula la base (valores de Node): ids a..g, 3 variantes, semilla "x".
	got := AssignVariants([]string{"a", "b", "c", "d", "e", "f", "g"}, 3, "x")
	var s []string
	for _, x := range got {
		s = append(s, fmt.Sprintf("%s:%d", x.ID, x.Variant))
	}
	if want := "c:0 f:1 e:2 b:0 a:1 d:2 g:0"; strings.Join(s, " ") != want {
		t.Errorf("orden = %q, quería %q", strings.Join(s, " "), want)
	}
	// count fuera de rango se limita a 1..5.
	for _, x := range AssignVariants(ids, 99, "x") {
		if x.Variant > 4 {
			t.Fatal("más de 5 variantes")
		}
	}
	for _, x := range AssignVariants(ids, 0, "x") {
		if x.Variant != 0 {
			t.Fatal("con 0 variantes todo debe ser la A")
		}
	}
}

func codes(items []ReviewItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Code
	}
	return out
}

func hasAll(have []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			found = found || h == w
		}
		if !found {
			return false
		}
	}
	return true
}

func TestReviewMessage(t *testing.T) {
	red := ReviewMessage("OFERTA INCREIBLE SOLO HOY ENTRA YA bit.ly/xyz", nil)
	if !hasAll(codes(red), "shortener", "caps", "personal") {
		t.Errorf("códigos = %v", codes(red))
	}
	if WorstLevel(red) != LevelRed {
		t.Error("debería ser rojo")
	}
	y := ReviewMessage("Hola {{Nombre}}, es gratis, mira https://mi.tienda/promo", nil)
	if !hasAll(codes(y), "links", "spam") {
		t.Errorf("códigos = %v", codes(y))
	}
	if WorstLevel(y) != LevelYellow {
		t.Errorf("debería ser amarillo: %+v", y)
	}
	if got := WorstLevel(ReviewMessage("Hola {{Nombre}}, ¿te separo una torta para el sábado?", nil)); got != LevelGreen {
		t.Errorf("debería ser verde, es %s", got)
	}
	rep := ReviewMessage("Hola {{Nombre}}, ya llegó la colección", []string{"Hola {{Nombre}}, ya llegó la colección"})
	found := false
	for _, it := range rep {
		if it.Code == "repeated" && it.Level == LevelYellow {
			found = true
		}
	}
	if !found {
		t.Errorf("faltó el aviso de repetido: %+v", rep)
	}
}

func TestReviewMessageDetails(t *testing.T) {
	// Varios enlaces.
	items := ReviewMessage("Hola {{nombre}} https://a.com y www.b.com", nil)
	if len(items) != 1 || items[0].Code != "links" || items[0].Message != "Tiene 2 enlaces: deja uno solo." {
		t.Errorf("varios enlaces: %+v", items)
	}
	// Spam: 3 o más palabras = rojo.
	items = ReviewMessage("Hola {{nombre}}: gratis, urgente y premio", nil)
	if WorstLevel(items) != LevelRed {
		t.Errorf("spam rojo: %+v", items)
	}
	// Muchos emojis.
	items = ReviewMessage("Hola {{nombre}} "+strings.Repeat("😊", 8), nil)
	if len(items) != 1 || items[0].Code != "emojis" || items[0].Level != LevelYellow {
		t.Errorf("emojis: %+v", items)
	}
	items = ReviewMessage("Hola {{nombre}} "+strings.Repeat("😊", 13), nil)
	if items[0].Level != LevelRed {
		t.Errorf("emojis rojo: %+v", items)
	}
	// Largo.
	items = ReviewMessage("{{nombre}} "+strings.Repeat("a", 800), nil)
	if len(items) != 1 || items[0].Code != "length" || items[0].Level != LevelYellow {
		t.Errorf("largo: %+v", items)
	}
	items = ReviewMessage("{{nombre}} "+strings.Repeat("a", 1600), nil)
	if items[0].Level != LevelRed {
		t.Errorf("muy largo: %+v", items)
	}
	// Las etiquetas no cuentan como MAYÚSCULAS y las opciones se evalúan con la primera.
	if got := ReviewMessage("{Hola|Buenas} {{NOMBRE}}, ¿cómo te va con tu pedido de la semana?", nil); len(got) != 0 {
		t.Errorf("debería estar limpio: %+v", got)
	}
}

func TestWorstLevelEmpty(t *testing.T) {
	if WorstLevel(nil) != LevelGreen {
		t.Error("sin hallazgos es verde")
	}
}
