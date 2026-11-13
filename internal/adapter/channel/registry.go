// Package channel: registro de fábricas de adaptadores por canal.
package channel

import (
	"dconnector/internal/adapter/channel/instagram"
	"dconnector/internal/adapter/channel/messenger"
	"dconnector/internal/adapter/channel/telegram"
	"dconnector/internal/adapter/channel/whatsapp"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Registry mapea cada canal a la fábrica de su adaptador. Cada agente de canal añade
// SOLO su línea al registrarse (WhatsApp: A3, Telegram: B1, Messenger/Instagram: C3).
var Registry = map[model.Channel]port.AdapterFactory{
	model.ChannelWhatsApp:  whatsapp.New,
	model.ChannelTelegram:  telegram.New,
	model.ChannelMessenger: messenger.New,
	model.ChannelInstagram: instagram.New,
}
