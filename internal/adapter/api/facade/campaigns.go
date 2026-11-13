package facade

import (
	"context"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Campaigns: campañas.
type Campaigns struct{ c *service.Campaigns }

func (s *Campaigns) List(ctx context.Context) ([]model.Campaign, error) {
	return s.c.List(ctx)
}

func (s *Campaigns) Get(ctx context.Context, id string) (model.Campaign, error) {
	return s.c.Get(ctx, id)
}

func (s *Campaigns) Save(ctx context.Context, cp model.Campaign) (model.Campaign, error) {
	return s.c.Save(ctx, cp)
}

func (s *Campaigns) Review(ctx context.Context, cp model.Campaign) (service.CampaignReview, error) {
	return s.c.Review(ctx, cp)
}

func (s *Campaigns) Launch(ctx context.Context, id string) error { return s.c.Launch(ctx, id) }
func (s *Campaigns) Pause(ctx context.Context, id string) error  { return s.c.Pause(ctx, id) }
func (s *Campaigns) Resume(ctx context.Context, id string) error { return s.c.Resume(ctx, id) }
func (s *Campaigns) Cancel(ctx context.Context, id string) error { return s.c.Cancel(ctx, id) }
func (s *Campaigns) Delete(ctx context.Context, id string) error { return s.c.Delete(ctx, id) }

func (s *Campaigns) Queue(ctx context.Context, id string, states []string, limit int) ([]model.QueueItem, error) {
	return s.c.Queue(ctx, id, states, limit)
}

func (s *Campaigns) GetPacing(ctx context.Context, acc string) (model.PacingSettings, error) {
	return s.c.GetPacing(ctx, model.AccountID(acc))
}

func (s *Campaigns) SetPacing(ctx context.Context, acc string, p model.PacingSettings) error {
	return s.c.SetPacing(ctx, model.AccountID(acc), p)
}
