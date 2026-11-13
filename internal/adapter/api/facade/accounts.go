package facade

import (
	"context"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Accounts: cuentas y vinculación.
type Accounts struct{ a *service.Accounts }

func (s *Accounts) List(ctx context.Context) ([]service.AccountView, error) {
	return s.a.List(ctx)
}

func (s *Accounts) Create(ctx context.Context, channel string, label, color string, config map[string]string) (model.Account, error) {
	return s.a.Create(ctx, model.Channel(channel), label, color, config)
}

func (s *Accounts) Update(ctx context.Context, id string, label, color string, enabled bool) error {
	return s.a.Update(ctx, model.AccountID(id), label, color, enabled)
}

func (s *Accounts) SetConfig(ctx context.Context, id string, config map[string]string) error {
	return s.a.SetConfig(ctx, model.AccountID(id), config)
}

func (s *Accounts) Connect(ctx context.Context, id string) error {
	return s.a.Connect(ctx, model.AccountID(id))
}

func (s *Accounts) Disconnect(ctx context.Context, id string) error {
	return s.a.Disconnect(ctx, model.AccountID(id))
}

func (s *Accounts) BeginPairing(ctx context.Context, id string, req model.PairRequest) error {
	return s.a.BeginPairing(ctx, model.AccountID(id), req)
}

func (s *Accounts) SubmitPairing(ctx context.Context, id string, in model.PairInput) error {
	return s.a.SubmitPairing(ctx, model.AccountID(id), in)
}

func (s *Accounts) Logout(ctx context.Context, id string) error {
	return s.a.Logout(ctx, model.AccountID(id))
}

func (s *Accounts) Delete(ctx context.Context, id string) error {
	return s.a.Delete(ctx, model.AccountID(id))
}

func (s *Accounts) Reorder(ctx context.Context, ids []string) error {
	return s.a.Reorder(ctx, accIDs(ids))
}

// Capabilities: de la cuenta de un chat, o del chat concreto si chatKey trae el id nativo.
func (s *Accounts) Capabilities(ctx context.Context, chatKey string) (capability.Set, error) {
	acc, native := model.ChatKey(chatKey).Split()
	if acc == "" {
		acc = model.AccountID(chatKey) // solo el id de la cuenta
	}
	return s.a.Capabilities(ctx, acc, native)
}
