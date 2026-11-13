package campaign

import (
	"strings"
	"unicode"
)

// foldTable: letra con acento → letra base (lo que en la base hacía normalize("NFD") y quitar
// las marcas combinadas). No se usa golang.org/x/text: este paquete solo importa la biblioteca
// estándar.
var foldTable = func() map[rune]rune {
	m := map[rune]rune{}
	add := func(base rune, set string) {
		for _, r := range set {
			m[r] = base
			if u := unicode.ToUpper(r); u != r {
				m[u] = unicode.ToUpper(base)
			}
		}
	}
	add('a', "àáâãäåāăą")
	add('c', "çćĉċč")
	add('d', "ď")
	add('e', "èéêëēĕėęě")
	add('g', "ĝğġģ")
	add('h', "ĥ")
	add('i', "ìíîïĩīĭį")
	add('j', "ĵ")
	add('k', "ķ")
	add('l', "ĺļľ")
	add('n', "ñńņň")
	add('o', "òóôõöōŏő")
	add('r', "ŕŗř")
	add('s', "śŝşš")
	add('t', "ţť")
	add('u', "ùúûüũūŭůűų")
	add('w', "ŵ")
	add('y', "ýÿŷ")
	add('z', "źżž")
	return m
}()

// foldAccents quita tildes y diéresis (y las marcas combinadas U+0300–U+036F si el texto ya
// venía descompuesto). «Ñ» pasa a «N», igual que en la base.
func foldAccents(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0x0300 && r <= 0x036F {
			continue
		}
		if f, ok := foldTable[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isPictographic aproxima \p{Extended_Pictographic} (emojis y símbolos gráficos) con los
// bloques principales de Unicode. No incluye dígitos ni «#» ni «*».
func isPictographic(r rune) bool {
	switch {
	case r == 0x00A9, r == 0x00AE, r == 0x203C, r == 0x2049, r == 0x2122, r == 0x2139:
		return true
	case r >= 0x2194 && r <= 0x21AA:
		return true
	case r >= 0x231A && r <= 0x23FF:
		return true
	case r == 0x24C2, r >= 0x25AA && r <= 0x25FE:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		return true
	case r >= 0x2934 && r <= 0x2935, r >= 0x2B05 && r <= 0x2B55:
		return true
	case r == 0x3030, r == 0x303D, r == 0x3297, r == 0x3299:
		return true
	case r >= 0x1F000 && r <= 0x1F1E5, r >= 0x1F200 && r <= 0x1F2FF: // sin los indicadores de bandera
		return true
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	}
	return false
}
