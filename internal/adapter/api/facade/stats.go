package facade

import (
	"context"

	"dconnector/internal/core/service"
	"dconnector/internal/core/stats"
)

// Stats: estadísticas de conversaciones.
type Stats struct{ s *service.Stats }

func (s *Stats) Conversations(ctx context.Context, accountIDs []string, rng stats.Range) (stats.ConversationOverview, error) {
	return s.s.Conversations(ctx, accIDs(accountIDs), rng)
}

func (s *Stats) Chat(ctx context.Context, chatKey string, rng stats.Range) (stats.ContactConversationStats, error) {
	return s.s.Chat(ctx, chatKeyOf(chatKey), rng)
}

func (s *Stats) Groups(ctx context.Context, accountIDs []string, rng stats.Range) (stats.GroupsOverview, error) {
	return s.s.Groups(ctx, accIDs(accountIDs), rng)
}
