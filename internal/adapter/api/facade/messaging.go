package facade

import (
	"context"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Messaging: enviar, reaccionar, editar, borrar… (sin SaveMediaAs: es de escritorio, docs/14 §4.3).
type Messaging struct{ m *service.Messaging }

func (s *Messaging) Send(ctx context.Context, chatKey string, out model.Outgoing) (model.Message, error) {
	return s.m.Send(ctx, model.ChatKey(chatKey), out)
}

func (s *Messaging) React(ctx context.Context, msgKey, emoji string) error {
	return s.m.React(ctx, model.MessageKey(msgKey), emoji)
}

func (s *Messaging) Edit(ctx context.Context, msgKey, text string) error {
	return s.m.Edit(ctx, model.MessageKey(msgKey), text)
}

func (s *Messaging) Delete(ctx context.Context, msgKey string, forEveryone bool) error {
	return s.m.Delete(ctx, model.MessageKey(msgKey), forEveryone)
}

func (s *Messaging) Forward(ctx context.Context, msgKey, toChatKey string) (model.Message, error) {
	return s.m.Forward(ctx, model.MessageKey(msgKey), model.ChatKey(toChatKey))
}

func (s *Messaging) VotePoll(ctx context.Context, msgKey string, options []string) error {
	return s.m.VotePoll(ctx, model.MessageKey(msgKey), options)
}

func (s *Messaging) MarkRead(ctx context.Context, chatKey string) error {
	return s.m.MarkRead(ctx, model.ChatKey(chatKey))
}

func (s *Messaging) SetTyping(ctx context.Context, chatKey string, typing bool) error {
	return s.m.SetTyping(ctx, model.ChatKey(chatKey), typing)
}
