package facade

import (
	"context"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Inbox: bandeja unificada.
type Inbox struct{ i *service.Inbox }

func (s *Inbox) ListChats(ctx context.Context, f model.ChatFilter) ([]model.Chat, error) {
	return s.i.ListChats(ctx, f)
}

func (s *Inbox) GetChat(ctx context.Context, chatKey string) (model.Chat, error) {
	return s.i.GetChat(ctx, model.ChatKey(chatKey))
}

func (s *Inbox) Messages(ctx context.Context, chatKey string, beforeTs int64, limit int) (model.MessagePage, error) {
	return s.i.Messages(ctx, model.ChatKey(chatKey), beforeTs, limit)
}

func (s *Inbox) Search(ctx context.Context, query string, accountIDs []string, limit int) ([]model.Message, error) {
	return s.i.Search(ctx, query, accIDs(accountIDs), limit)
}

func (s *Inbox) UnreadTotals(ctx context.Context) (map[string]int, error) {
	m, err := s.i.UnreadTotals(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[string(k)] = v
	}
	return out, nil
}

func (s *Inbox) LoadOlder(ctx context.Context, chatKey string) error {
	return s.i.LoadOlder(ctx, model.ChatKey(chatKey))
}

func (s *Inbox) Archive(ctx context.Context, chatKey string, v bool) error {
	return s.i.Archive(ctx, model.ChatKey(chatKey), v)
}

func (s *Inbox) Pin(ctx context.Context, chatKey string, v bool) error {
	return s.i.Pin(ctx, model.ChatKey(chatKey), v)
}

func (s *Inbox) Mute(ctx context.Context, chatKey string, until int64) error {
	return s.i.Mute(ctx, model.ChatKey(chatKey), until)
}

// OpenChat devuelve el chat directo con una persona de la cuenta o lo crea vacío (sin enviar nada):
// «Nuevo chat» y el botón «Escribir» de Contactos. `peerNativeId` es el id nativo de la identidad del
// contacto o, en WhatsApp, un número de teléfono. Un chat sin actividad no sale en la lista de chats
// hasta que se envía el primer mensaje.
func (s *Inbox) OpenChat(ctx context.Context, accountID, peerNativeID string) (model.Chat, error) {
	return s.i.OpenChat(ctx, model.AccountID(accountID), peerNativeID)
}
