package facade

import (
	"context"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Contacts: contactos unificados y listas.
type Contacts struct{ c *service.Contacts }

// ContactPage: página de contactos con el total.
type ContactPage struct {
	Contacts []model.Contact `json:"contacts"`
	Total    int             `json:"total"`
}

func (s *Contacts) List(ctx context.Context, f model.ContactFilter) (ContactPage, error) {
	cs, total, err := s.c.List(ctx, f)
	if err != nil {
		return ContactPage{}, err
	}
	return ContactPage{Contacts: cs, Total: total}, nil
}

func (s *Contacts) Get(ctx context.Context, id string) (model.Contact, error) {
	return s.c.Get(ctx, id)
}

func (s *Contacts) Save(ctx context.Context, ct model.Contact) (model.Contact, error) {
	return s.c.Save(ctx, ct)
}

func (s *Contacts) Delete(ctx context.Context, id string) error { return s.c.Delete(ctx, id) }

func (s *Contacts) FromChat(ctx context.Context, chatKey string) (model.Contact, error) {
	return s.c.FromChat(ctx, model.ChatKey(chatKey))
}

func (s *Contacts) Merge(ctx context.Context, keepID, mergeID string) (model.Contact, error) {
	return s.c.Merge(ctx, keepID, mergeID)
}

func (s *Contacts) SplitIdentity(ctx context.Context, contactID string, acc string, peer string) (model.Contact, error) {
	return s.c.SplitIdentity(ctx, contactID, model.AccountID(acc), peer)
}

func (s *Contacts) ImportFromAccount(ctx context.Context, acc string) (service.ImportResult, error) {
	return s.c.ImportFromAccount(ctx, model.AccountID(acc))
}

func (s *Contacts) SuggestMerges(ctx context.Context) ([][2]string, error) {
	return s.c.SuggestMerges(ctx)
}

func (s *Contacts) Lists(ctx context.Context) ([]model.ContactList, error) {
	return s.c.Lists(ctx)
}

func (s *Contacts) SaveList(ctx context.Context, l model.ContactList) (model.ContactList, error) {
	return s.c.SaveList(ctx, l)
}

func (s *Contacts) DeleteList(ctx context.Context, id string) error {
	return s.c.DeleteList(ctx, id)
}

func (s *Contacts) SetListMembers(ctx context.Context, listID string, add, remove []string) error {
	return s.c.SetListMembers(ctx, listID, add, remove)
}

func (s *Contacts) ImportCSV(ctx context.Context, path string, mapping service.CSVMapping) (service.ImportResult, error) {
	return s.c.ImportCSV(ctx, path, mapping)
}

// SyncAll rellena los contactos con las personas y chats directos ya guardados de cada cuenta
// (contactos automáticos, `source == "auto"`). Devuelve cuántos contactos nuevos se crearon.
func (s *Contacts) SyncAll(ctx context.Context) (int, error) { return s.c.SyncAll(ctx) }
