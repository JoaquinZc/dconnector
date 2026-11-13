package ws

import (
	"context"

	"dconnector/internal/core/model"
)

// protocolEvents: los eventos del núcleo que viajan por el protocolo (10 §4). Quedan fuera
// history.batch, typing y chat.removed.
var protocolEvents = map[model.EventType]bool{
	model.EvAccountStatus: true,
	model.EvCapabilities:  true,
	model.EvChatUpsert:    true,
	model.EvMessageNew:    true,
	model.EvMessageSent:   true,
	model.EvMessageUpdate: true,
	model.EvReaction:      true,
	model.EvReceipt:       true,
	model.EvPerson:        true,
	model.EvStatusUpsert:  true,
	model.EvStatusView:    true,
}

// messageData: data de message.received y message.sent. Añade dos campos al Message: chatKind
// (tipo del chat, para que el servidor distinga grupos) y clientRef (el del comando que lo envió).
type messageData struct {
	model.Message
	ChatKind  model.ChatKind `json:"chatKind,omitempty"`
	ClientRef string         `json:"clientRef,omitempty"`
}

// updateData: data de message.updated.
type updateData struct {
	Update  *model.MessageUpdate `json:"update"`
	Message *model.Message       `json:"message,omitempty"`
}

// lookups guarda las consultas al almacén que se repiten al evaluar un mismo evento.
type lookups struct {
	m     *Manager
	ctx   context.Context
	done  map[model.ChatKey]bool
	chats map[model.ChatKey]*model.Chat
}

// chat devuelve el chat guardado, o nil si aún no existe en el almacén. Se lee una sola vez.
func (l *lookups) chat(acc model.AccountID, native string) *model.Chat {
	if acc == "" || native == "" {
		return nil
	}
	key := model.NewChatKey(acc, native)
	if l.done[key] {
		return l.chats[key]
	}
	if l.done == nil {
		l.done, l.chats = map[model.ChatKey]bool{}, map[model.ChatKey]*model.Chat{}
	}
	l.done[key] = true
	if c := l.m.storedChat(l.ctx, acc, native); c != nil {
		l.chats[key] = c
		return c
	}
	return nil
}

// chatKind devuelve el tipo del chat; ok es false si el chat aún no existe en el almacén.
func (l *lookups) chatKind(acc model.AccountID, native string) (model.ChatKind, bool) {
	if c := l.chat(acc, native); c != nil {
		return c.Kind, true
	}
	return "", false
}

// eventChat devuelve el chat al que pertenece un evento (acc, id nativo); native vacío si el
// evento no es de un chat.
func eventChat(ev model.Event) (model.AccountID, string, *model.ChatKind) {
	switch ev.Type {
	case model.EvChatUpsert:
		if ev.Chat != nil {
			k := ev.Chat.Kind
			return ev.AccountID, ev.Chat.NativeID, &k
		}
	case model.EvMessageNew, model.EvMessageSent:
		if ev.Message != nil {
			return ev.AccountID, ev.Message.ChatNativeID, nil
		}
	case model.EvMessageUpdate:
		if ev.Message != nil {
			return ev.AccountID, ev.Message.ChatNativeID, nil
		}
		if ev.Update != nil {
			a, c, _ := ev.Update.Key.Split()
			return a, c, nil
		}
	case model.EvReaction:
		if ev.Reaction != nil {
			a, c, _ := ev.Reaction.MessageKey.Split()
			return a, c, nil
		}
	case model.EvReceipt:
		if ev.Receipt != nil {
			a, c := ev.Receipt.ChatKey.Split()
			return a, c, nil
		}
	}
	return "", "", nil
}

// accepts aplica el filtro del destino a un evento (10 §4).
func accepts(f model.DestinationFilter, name string, ev model.Event, lk *lookups) bool {
	if !filterAllowsAccount(f, ev.AccountID, ev.Channel) {
		return false
	}
	if len(f.Events) > 0 && !containsStr(f.Events, name) {
		return false
	}
	if !f.IncludeFromMe {
		switch ev.Type {
		case model.EvMessageNew:
			if ev.Message != nil && ev.Message.FromMe {
				return false
			}
		case model.EvMessageSent:
			return false
		case model.EvMessageUpdate:
			if ev.Message != nil && ev.Message.FromMe {
				return false
			}
		}
	}
	if !f.IncludeGroups {
		acc, native, kind := eventChat(ev)
		if native != "" {
			var k model.ChatKind
			ok := false
			if kind != nil {
				k, ok = *kind, true
			} else {
				k, ok = lk.chatKind(acc, native)
			}
			if ok && (k == model.ChatGroup || k == model.ChatChannel) {
				return false
			}
		}
	}
	// Alcance de contactos: primero la inclusión (todos o solo ciertas listas), luego la exclusión.
	if cs := f.ContactScope; cs.Restricts() && !lk.contactScopeAllows(cs, ev) {
		return false
	}
	return true
}

// filterAllowsAccount: la cuenta pasa el filtro de cuentas y canales del destino.
func filterAllowsAccount(f model.DestinationFilter, acc model.AccountID, ch model.Channel) bool {
	if len(f.AccountIDs) > 0 {
		found := false
		for _, id := range f.AccountIDs {
			if id == acc {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(f.Channels) > 0 {
		found := false
		for _, c := range f.Channels {
			if c == ch {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// eventData arma el data del evento; ok es false si el evento no lleva su carga.
func eventData(ev model.Event, clientRef string, lk *lookups) (any, bool) {
	switch ev.Type {
	case model.EvAccountStatus:
		if ev.AccountStatus == nil {
			return nil, false
		}
		st := *ev.AccountStatus
		st.QRCode, st.PairCode = "", "" // nunca salen por el protocolo (10 §7)
		return st, true
	case model.EvCapabilities:
		if ev.Capabilities == nil {
			return nil, false
		}
		return ev.Capabilities, true
	case model.EvChatUpsert:
		if ev.Chat == nil {
			return nil, false
		}
		return ev.Chat, true
	case model.EvMessageNew, model.EvMessageSent:
		if ev.Message == nil {
			return nil, false
		}
		d := messageData{Message: *ev.Message}
		if ev.Type == model.EvMessageSent {
			d.ClientRef = clientRef
		}
		if k, ok := lk.chatKind(ev.AccountID, ev.Message.ChatNativeID); ok {
			d.ChatKind = k
		}
		return d, true
	case model.EvMessageUpdate:
		if ev.Update == nil {
			return nil, false
		}
		return updateData{Update: ev.Update, Message: ev.Message}, true
	case model.EvReaction:
		if ev.Reaction == nil {
			return nil, false
		}
		return ev.Reaction, true
	case model.EvReceipt:
		if ev.Receipt == nil {
			return nil, false
		}
		return ev.Receipt, true
	case model.EvPerson:
		if ev.Person == nil {
			return nil, false
		}
		return ev.Person, true
	case model.EvStatusUpsert:
		if ev.Status == nil {
			return nil, false
		}
		return ev.Status, true
	case model.EvStatusView:
		if ev.StatusView == nil {
			return nil, false
		}
		return ev.StatusView, true
	}
	return nil, false
}

// shrinkData quita las miniaturas (data URL) cuando el frame no cabe en 1 MiB.
func shrinkData(data any) any {
	switch d := data.(type) {
	case messageData:
		d.Message = shrinkMessage(d.Message)
		return d
	case updateData:
		if d.Message != nil {
			m := shrinkMessage(*d.Message)
			d.Message = &m
		}
		return d
	case *model.Status:
		st := *d
		if st.Media != nil {
			md := *st.Media
			md.Thumb = ""
			st.Media = &md
		}
		return &st
	}
	return data
}

func shrinkMessage(m model.Message) model.Message {
	if m.Media != nil {
		md := *m.Media
		md.Thumb = ""
		m.Media = &md
	}
	return m
}
