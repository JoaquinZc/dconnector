package model

import (
	"strings"

	"github.com/oklog/ulid/v2"
)

type Channel string

const (
	ChannelWhatsApp  Channel = "whatsapp"
	ChannelTelegram  Channel = "telegram"
	ChannelMessenger Channel = "messenger"
	ChannelInstagram Channel = "instagram"
)

var AllChannels = []Channel{ChannelWhatsApp, ChannelTelegram, ChannelMessenger, ChannelInstagram}

// AccountID: prefijo del canal + ULID en minúsculas. Ej.: "wa-01jc…", "tg-…", "fb-…", "ig-…".
type AccountID string

func NewAccountID(c Channel) AccountID {
	p := map[Channel]string{ChannelWhatsApp: "wa", ChannelTelegram: "tg", ChannelMessenger: "fb", ChannelInstagram: "ig"}[c]
	return AccountID(p + "-" + strings.ToLower(ulid.Make().String()))
}

// NewID para el resto de entidades de app.db (contactos, listas, campañas…).
func NewID() string { return strings.ToLower(ulid.Make().String()) }

const keySep = "|"

// ChatKey = "<accountId>|<idNativoDelChat>"
type ChatKey string

func NewChatKey(acc AccountID, nativeChatID string) ChatKey {
	return ChatKey(string(acc) + keySep + nativeChatID)
}

func (k ChatKey) Split() (AccountID, string) {
	p := strings.SplitN(string(k), keySep, 2)
	if len(p) != 2 {
		return "", ""
	}
	return AccountID(p[0]), p[1]
}

// MessageKey = "<accountId>|<idNativoDelChat>|<idNativoDelMensaje>"
type MessageKey string

func NewMessageKey(acc AccountID, nativeChatID, nativeMsgID string) MessageKey {
	return MessageKey(string(acc) + keySep + nativeChatID + keySep + nativeMsgID)
}

func (k MessageKey) Split() (AccountID, string, string) {
	p := strings.SplitN(string(k), keySep, 3)
	if len(p) != 3 {
		return "", "", ""
	}
	return AccountID(p[0]), p[1], p[2]
}

func (k MessageKey) Chat() ChatKey { a, c, _ := k.Split(); return NewChatKey(a, c) }

// MessageRef: lo mínimo que un adaptador necesita para actuar sobre un mensaje.
type MessageRef struct {
	ChatNativeID   string `json:"chatNativeId"`
	NativeID       string `json:"nativeId"`
	SenderNativeID string `json:"senderNativeId"`
	FromMe         bool   `json:"fromMe"`
	Timestamp      int64  `json:"timestamp"` // ms
}
