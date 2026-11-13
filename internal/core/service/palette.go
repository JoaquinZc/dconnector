package service

import (
	"fmt"
	"regexp"
	"strings"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// accountPalette: paleta de colores de cuenta, en orden por defecto (11-INTERFAZ §5).
var accountPalette = []string{
	"#25D366", "#229ED9", "#8B5CF6", "#F59E0B", "#EF4444",
	"#0EA5E9", "#10B981", "#EC4899", "#6366F1", "#84CC16",
}

var hexColorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// nextPaletteColor devuelve el primer color de la paleta que no use ninguna cuenta; si todos
// están en uso, vuelve a empezar la paleta según cuántas cuentas hay.
func nextPaletteColor(used []string) string {
	taken := map[string]bool{}
	for _, c := range used {
		taken[strings.ToUpper(c)] = true
	}
	for _, c := range accountPalette {
		if !taken[c] {
			return c
		}
	}
	return accountPalette[len(used)%len(accountPalette)]
}

// normalizeColor valida "#RRGGBB" y lo devuelve en mayúsculas.
func normalizeColor(c string) (string, error) {
	c = strings.TrimSpace(c)
	if !hexColorRe.MatchString(c) {
		return "", fmt.Errorf("%w: el color debe ser #RRGGBB", port.ErrInvalid)
	}
	return strings.ToUpper(c), nil
}

func defaultAccountLabel(ch model.Channel) string {
	switch ch {
	case model.ChannelWhatsApp:
		return "WhatsApp"
	case model.ChannelTelegram:
		return "Telegram"
	case model.ChannelMessenger:
		return "Messenger"
	case model.ChannelInstagram:
		return "Instagram"
	}
	return string(ch)
}
