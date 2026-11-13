package telegram

// Ids nativos (08 §4): user:<id>, chat:<id> y channel:<id>.

import (
	"strconv"
	"strings"

	"github.com/gotd/td/tg"

	"dconnector/internal/core/model"
)

type peerKind int

const (
	kindNone peerKind = iota
	kindUser
	kindChat
	kindChannel
)

const (
	nativeUserPrefix    = "user:"
	nativeChatPrefix    = "chat:"
	nativeChannelPrefix = "channel:"
)

func userNative(id int64) string    { return nativeUserPrefix + strconv.FormatInt(id, 10) }
func chatNative(id int64) string    { return nativeChatPrefix + strconv.FormatInt(id, 10) }
func channelNative(id int64) string { return nativeChannelPrefix + strconv.FormatInt(id, 10) }

// nativeOf devuelve el id nativo de un par; "" si es nil o de un tipo desconocido.
func nativeOf(p tg.PeerClass) string {
	switch v := p.(type) {
	case *tg.PeerUser:
		return userNative(v.UserID)
	case *tg.PeerChat:
		return chatNative(v.ChatID)
	case *tg.PeerChannel:
		return channelNative(v.ChannelID)
	}
	return ""
}

// parseNative separa un id nativo en tipo y número.
func parseNative(s string) (peerKind, int64, bool) {
	var kind peerKind
	var rest string
	switch {
	case strings.HasPrefix(s, nativeUserPrefix):
		kind, rest = kindUser, s[len(nativeUserPrefix):]
	case strings.HasPrefix(s, nativeChatPrefix):
		kind, rest = kindChat, s[len(nativeChatPrefix):]
	case strings.HasPrefix(s, nativeChannelPrefix):
		kind, rest = kindChannel, s[len(nativeChannelPrefix):]
	default:
		return kindNone, 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return kindNone, 0, false
	}
	return kind, id, true
}

// peerFromNative arma el tg.PeerClass de un id nativo.
func peerFromNative(s string) (tg.PeerClass, bool) {
	kind, id, ok := parseNative(s)
	if !ok {
		return nil, false
	}
	switch kind {
	case kindUser:
		return &tg.PeerUser{UserID: id}, true
	case kindChat:
		return &tg.PeerChat{ChatID: id}, true
	case kindChannel:
		return &tg.PeerChannel{ChannelID: id}, true
	}
	return nil, false
}

// peerView: lo que el traductor puede consultar de los pares ya conocidos.
type peerView interface {
	User(id int64) (*tg.User, bool)
	Chat(id int64) (*tg.Chat, bool)
	Channel(id int64) (*tg.Channel, bool)
}

// entitiesView adapta tg.Entities a peerView.
type entitiesView struct{ e tg.Entities }

func (v entitiesView) User(id int64) (*tg.User, bool) {
	u, ok := v.e.Users[id]
	return u, ok && u != nil
}
func (v entitiesView) Chat(id int64) (*tg.Chat, bool) {
	c, ok := v.e.Chats[id]
	return c, ok && c != nil
}
func (v entitiesView) Channel(id int64) (*tg.Channel, bool) {
	c, ok := v.e.Channels[id]
	return c, ok && c != nil
}

// chainView consulta varias vistas en orden.
type chainView []peerView

func (c chainView) User(id int64) (*tg.User, bool) {
	for _, v := range c {
		if u, ok := v.User(id); ok {
			return u, true
		}
	}
	return nil, false
}

func (c chainView) Chat(id int64) (*tg.Chat, bool) {
	for _, v := range c {
		if ch, ok := v.Chat(id); ok {
			return ch, true
		}
	}
	return nil, false
}

func (c chainView) Channel(id int64) (*tg.Channel, bool) {
	for _, v := range c {
		if ch, ok := v.Channel(id); ok {
			return ch, true
		}
	}
	return nil, false
}

// emptyView no conoce a nadie.
type emptyView struct{}

func (emptyView) User(int64) (*tg.User, bool)       { return nil, false }
func (emptyView) Chat(int64) (*tg.Chat, bool)       { return nil, false }
func (emptyView) Channel(int64) (*tg.Channel, bool) { return nil, false }

// userDisplay: nombre visible de un usuario.
func userDisplay(u *tg.User) string {
	if u == nil {
		return ""
	}
	if u.Deleted {
		return "Cuenta eliminada"
	}
	name := strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName))
	if name != "" {
		return name
	}
	if un := usernameOf(u); un != "" {
		return "@" + un
	}
	if u.Phone != "" {
		return "+" + strings.TrimPrefix(u.Phone, "+")
	}
	return ""
}

// usernameOf: nombre de usuario principal (el campo Username o el primer activo de Usernames).
func usernameOf(u *tg.User) string {
	if u == nil {
		return ""
	}
	if u.Username != "" {
		return u.Username
	}
	for _, n := range u.Usernames {
		if n.Active && n.Username != "" {
			return n.Username
		}
	}
	return ""
}

// nameOfNative: nombre visible de un id nativo ("" si no se conoce).
func nameOfNative(v peerView, native string) string {
	kind, id, ok := parseNative(native)
	if !ok {
		return ""
	}
	switch kind {
	case kindUser:
		if u, ok := v.User(id); ok {
			return userDisplay(u)
		}
	case kindChat:
		if c, ok := v.Chat(id); ok {
			return c.Title
		}
	case kindChannel:
		if c, ok := v.Channel(id); ok {
			return c.Title
		}
	}
	return ""
}

// chatKindOf: tipo de chat según el par (08 §4). Un canal desconocido se toma por canal de difusión.
func chatKindOf(p tg.PeerClass, v peerView, selfID int64) model.ChatKind {
	switch x := p.(type) {
	case *tg.PeerUser:
		if selfID != 0 && x.UserID == selfID {
			return model.ChatSelf
		}
		return model.ChatDirect
	case *tg.PeerChat:
		return model.ChatGroup
	case *tg.PeerChannel:
		if ch, ok := v.Channel(x.ChannelID); ok && (ch.Megagroup || ch.Gigagroup) {
			return model.ChatGroup
		}
		return model.ChatChannel
	}
	return model.ChatDirect
}

// channelReadOnly: un canal de difusión donde no eres administrador con permiso de publicar, o un
// supergrupo donde no puedes escribir (08 §4 y §11).
func channelReadOnly(ch *tg.Channel) bool {
	if ch == nil {
		return false
	}
	if ch.Left {
		return true
	}
	if ch.Creator {
		return false
	}
	canPost := ch.AdminRights.PostMessages
	if ch.Broadcast && !ch.Megagroup {
		return !canPost
	}
	// Supergrupo: sin permiso de enviar mensajes (restricción propia o general) y sin ser admin.
	if ch.AdminRights.Zero() {
		if ch.BannedRights.SendMessages || ch.DefaultBannedRights.SendMessages {
			return true
		}
	}
	return false
}

// shortID abrevia un id para el registro (últimos 4 caracteres, 02 §7).
func shortID(s string) string {
	if len(s) <= 4 {
		return s
	}
	return "…" + s[len(s)-4:]
}
