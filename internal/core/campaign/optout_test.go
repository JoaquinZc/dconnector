package campaign

import "testing"

const testKeywords = "baja,stop,no más,no mas,cancelar,unsubscribe"

func TestIsOptOut(t *testing.T) {
	yes := []string{"Baja", "STOP!", "no más", "  NO  MÁS. ", "¡Cancelar!", "UNSUBSCRIBE"}
	for _, m := range yes {
		if !IsOptOut(m, testKeywords) {
			t.Errorf("%q debería ser baja", m)
		}
	}
	no := []string{"", "   ", "no más por favor, gracias", "dame de baja ya", "stop it", "hola", "no mas info"}
	for _, m := range no {
		if IsOptOut(m, testKeywords) {
			t.Errorf("%q no debería ser baja", m)
		}
	}
	if IsOptOut("baja", "") || IsOptOut("baja", " , ,") {
		t.Error("sin palabras clave nunca es baja")
	}
	if !IsOptOut("BAJÁ", "  baja ,stop") {
		t.Error("las palabras clave también se normalizan")
	}
}

func TestSuggestOptOut(t *testing.T) {
	cases := map[string]string{
		"Por favor no me escribas más": "no me escribas",
		"¿Quién eres?":                 "quien eres",
		"Esto es SPAM":                 "spam",
		"deja de escribirme":           "deja de",
		"No estoy interesada, gracias": "no estoy interesada",
	}
	for in, want := range cases {
		if got := SuggestOptOut(in); got != want {
			t.Errorf("SuggestOptOut(%q) = %q, quería %q", in, got, want)
		}
	}
	for _, in := range []string{"Sí, me interesa, ¿cuánto cuesta?", "Dejame pensarlo", "ok", "", "antispam total"} {
		if got := SuggestOptOut(in); got != "" {
			t.Errorf("SuggestOptOut(%q) = %q, quería vacío", in, got)
		}
	}
}

// Los mismos casos que prueban el isOptOut provisional del Router (router_test.go): al cambiarlo
// por campaign.IsOptOut el comportamiento con las palabras por defecto no cambia.
func TestIsOptOutDefaultKeywordsLikeRouter(t *testing.T) {
	kw := "baja,stop,no más,no mas,cancelar,cancelar suscripción,unsubscribe,no quiero recibir"
	yes := []string{"baja", "BAJA", " Baja. ", "¡STOP!", "no más", "No mas", "NO MÁS.", "cancelar suscripción", "Cancelar  suscripcion", "unsubscribe", "no quiero recibir"}
	no := []string{"", "   ", "hola", "dar de baja mi cuenta", "no quiero", "baja por favor", "stopping", "gracias, baja"}
	for _, s := range yes {
		if !IsOptOut(s, kw) {
			t.Errorf("%q debería ser baja", s)
		}
	}
	for _, s := range no {
		if IsOptOut(s, kw) {
			t.Errorf("%q no debería ser baja", s)
		}
	}
	if !IsOptOut("chao", "hola, chao ,") {
		t.Error("las palabras se recortan y se ignoran las vacías")
	}
}
