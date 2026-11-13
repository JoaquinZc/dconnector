package facade

import (
	"context"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
	"dconnector/internal/core/stats"
)

// Statuses: estados.
type Statuses struct{ s *service.Statuses }

func (s *Statuses) Authors(ctx context.Context, accountIDs []string) ([]model.StatusAuthor, error) {
	return s.s.Authors(ctx, accIDs(accountIDs))
}

func (s *Statuses) ByAuthor(ctx context.Context, acc string, authorID string) ([]model.Status, error) {
	return s.s.ByAuthor(ctx, model.AccountID(acc), authorID)
}

func (s *Statuses) Mine(ctx context.Context, acc string, from, to int64) ([]model.Status, error) {
	return s.s.Mine(ctx, model.AccountID(acc), from, to)
}

func (s *Statuses) Post(ctx context.Context, acc string, st model.OutgoingStatus) (model.Status, error) {
	return s.s.Post(ctx, model.AccountID(acc), st)
}

func (s *Statuses) Delete(ctx context.Context, acc string, statusNativeID string) error {
	return s.s.Delete(ctx, model.AccountID(acc), statusNativeID)
}

func (s *Statuses) MarkViewed(ctx context.Context, acc string, statusKey string) error {
	return s.s.MarkViewed(ctx, model.AccountID(acc), statusKey)
}

func (s *Statuses) React(ctx context.Context, acc string, statusKey, emoji string) error {
	return s.s.React(ctx, model.AccountID(acc), statusKey, emoji)
}

func (s *Statuses) Viewers(ctx context.Context, acc string, statusNativeID string) ([]model.StatusView, error) {
	return s.s.Viewers(ctx, model.AccountID(acc), statusNativeID)
}

func (s *Statuses) Stats(ctx context.Context, acc string, rng stats.Range) (stats.StatusStats, error) {
	return s.s.Stats(ctx, model.AccountID(acc), rng)
}
