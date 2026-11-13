package wails

import (
	"testing"

	"dconnector/internal/adapter/desktop/bridge"
)

func TestAccountMenuLabel(t *testing.T) {
	cases := []struct {
		in   bridge.AccountLine
		want string
	}{
		{bridge.AccountLine{Label: "Ventas", Channel: "whatsapp", State: "connected", Connected: true}, "● Ventas (WhatsApp) — Conectada"},
		{bridge.AccountLine{Label: "Soporte", Channel: "telegram", State: "error"}, "○ Soporte (Telegram) — Con error"},
		{bridge.AccountLine{Label: "Marca", Channel: "instagram", State: "pairing"}, "○ Marca (Instagram) — Esperando vinculación"},
		{bridge.AccountLine{Label: "X", Channel: "otro", State: "raro"}, "○ X (otro) — Desconectada"},
	}
	for _, c := range cases {
		if got := accountMenuLabel(c.in); got != c.want {
			t.Errorf("%q ≠ %q", got, c.want)
		}
	}
}
