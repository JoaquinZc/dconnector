package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

func TestContactSourceRoundTrip(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")

	// Vacío se guarda como «manual»; auto e import se conservan.
	require.NoError(t, s.SaveContact(ctx, model.Contact{ID: "c1", Name: "Ana"}))
	require.NoError(t, s.SaveContact(ctx, model.Contact{ID: "c2", Name: "Beto", Source: model.ContactSourceImport}))
	require.NoError(t, s.SaveContact(ctx, model.Contact{ID: "c3", Name: "Carla", Source: model.ContactSourceAuto,
		Identities: []model.Identity{{AccountID: "wa-a", PeerNativeID: "1@s.whatsapp.net"}}}))
	for id, want := range map[string]string{"c1": "manual", "c2": "import", "c3": "auto"} {
		got, err := s.GetContact(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, got.Source, id)
	}
	f, err := s.FindContactByIdentity(ctx, "wa-a", "1@s.whatsapp.net")
	require.NoError(t, err)
	require.Equal(t, "auto", f.Source)

	// Actualizar cambia el origen; los filtros siguen funcionando con él.
	c3, _ := s.GetContact(ctx, "c3")
	c3.Source = model.ContactSourceManual
	require.NoError(t, s.SaveContact(ctx, c3))
	c3, _ = s.GetContact(ctx, "c3")
	require.Equal(t, "manual", c3.Source)
	got, total, err := s.ListContacts(ctx, model.ContactFilter{Query: "carl"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "c3", got[0].ID)
}

func TestSaveContactsBatchIsOneTransactionWorth(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	require.NoError(t, s.SaveList(ctx, model.ContactList{ID: "l1", Name: "Clientes"}))
	require.NoError(t, s.SaveContacts(ctx, []model.Contact{
		{ID: "a", Name: "Ana", Source: model.ContactSourceAuto, ListIDs: []string{"l1"}, Tags: []string{"x"},
			Identities: []model.Identity{{AccountID: "wa-a", PeerNativeID: "1@s"}}},
		{ID: "b", Name: "Beto", Source: model.ContactSourceAuto,
			Identities: []model.Identity{{AccountID: "wa-a", PeerNativeID: "2@s"}}},
	}))
	all, total, err := s.ListContacts(ctx, model.ContactFilter{})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Equal(t, []string{"l1"}, all[0].ListIDs)
	require.Equal(t, []string{"x"}, all[0].Tags)
	require.Len(t, all[1].Identities, 1)

	// Un lote con un contacto sin id no guarda nada.
	err = s.SaveContacts(ctx, []model.Contact{{ID: "c", Name: "Carla"}, {Name: "sin id"}})
	require.Error(t, err)
	_, err = s.GetContact(ctx, "c")
	require.Error(t, err)
	require.NoError(t, s.SaveContacts(ctx, nil))
}

func TestDeleteOrphanAutoContactsKeepsManualAndImported(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	saveTestAccount(t, s, "wa-b")
	for _, c := range []model.Contact{
		{ID: "auto-a", Name: "A", Source: model.ContactSourceAuto, Identities: []model.Identity{{AccountID: "wa-a", PeerNativeID: "1@s"}}},
		{ID: "auto-ab", Name: "AB", Source: model.ContactSourceAuto, Identities: []model.Identity{
			{AccountID: "wa-a", PeerNativeID: "2@s"}, {AccountID: "wa-b", PeerNativeID: "2@s"}}},
		{ID: "manual-a", Name: "M", Source: model.ContactSourceManual, Identities: []model.Identity{{AccountID: "wa-a", PeerNativeID: "3@s"}}},
		{ID: "import-a", Name: "I", Source: model.ContactSourceImport, Identities: []model.Identity{{AccountID: "wa-a", PeerNativeID: "4@s"}}},
		{ID: "auto-free", Name: "F", Source: model.ContactSourceAuto}, // ya huérfano
	} {
		require.NoError(t, s.SaveContact(ctx, c))
	}
	// Quitar la cuenta wa-a: sus identidades caen en cascada.
	require.NoError(t, s.DeleteAccount(ctx, "wa-a"))
	n, err := s.DeleteOrphanAutoContacts(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n, "auto-a y auto-free")
	all, _, _ := s.ListContacts(ctx, model.ContactFilter{})
	ids := map[string]bool{}
	for _, c := range all {
		ids[c.ID] = true
	}
	require.Equal(t, map[string]bool{"auto-ab": true, "manual-a": true, "import-a": true}, ids)
	ab, _ := s.GetContact(ctx, "auto-ab")
	require.Len(t, ab.Identities, 1)

	n, err = s.DeleteOrphanAutoContacts(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}
