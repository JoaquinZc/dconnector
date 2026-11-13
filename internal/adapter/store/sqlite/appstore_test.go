package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

var ctx = context.Background()

func TestAccountsSortAndCRUD(t *testing.T) {
	s := newTestApp(t)
	for _, id := range []model.AccountID{"wa-a", "tg-b", "fb-c"} {
		require.NoError(t, s.SaveAccount(ctx, model.Account{
			ID: id, Channel: channelOf(id), Label: string(id), Color: "#111111", Enabled: true, CreatedAt: 10,
			Config: map[string]string{"api_id": "1"},
		}))
	}
	list, err := s.ListAccounts(ctx)
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, []model.AccountID{"wa-a", "tg-b", "fb-c"}, []model.AccountID{list[0].ID, list[1].ID, list[2].ID})
	require.Equal(t, model.ChannelTelegram, list[1].Channel)
	require.Equal(t, map[string]string{"api_id": "1"}, list[0].Config)

	// Guardar de nuevo no cambia el orden ni la fecha de creación.
	upd := list[0]
	upd.Label = "Ventas"
	upd.Enabled = false
	upd.SelfID = "5939@s.whatsapp.net"
	upd.CreatedAt = 999
	upd.Config = nil
	require.NoError(t, s.SaveAccount(ctx, upd))
	got, err := s.GetAccount(ctx, "wa-a")
	require.NoError(t, err)
	require.Equal(t, "Ventas", got.Label)
	require.False(t, got.Enabled)
	require.Equal(t, "5939@s.whatsapp.net", got.SelfID)
	require.Equal(t, int64(10), got.CreatedAt)
	require.NotNil(t, got.Config)
	list, _ = s.ListAccounts(ctx)
	require.Equal(t, model.AccountID("wa-a"), list[0].ID)

	_, err = s.GetAccount(ctx, "nope")
	require.ErrorIs(t, err, port.ErrNotFound)
	require.Error(t, s.SaveAccount(ctx, model.Account{}))

	// Reordenar (extra, no está en el puerto).
	require.NoError(t, s.ReorderAccounts(ctx, []model.AccountID{"fb-c", "wa-a"}))
	list, _ = s.ListAccounts(ctx)
	require.Equal(t, []model.AccountID{"fb-c", "wa-a", "tg-b"}, []model.AccountID{list[0].ID, list[1].ID, list[2].ID})
	// Una cuenta nueva queda al final.
	saveTestAccount(t, s, "ig-d")
	list, _ = s.ListAccounts(ctx)
	require.Equal(t, model.AccountID("ig-d"), list[3].ID)
}

func TestDeleteAccountRemovesDependents(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	saveTestAccount(t, s, "wa-b")
	require.NoError(t, s.SaveContact(ctx, model.Contact{ID: "c1", Name: "Ana", Identities: []model.Identity{
		{AccountID: "wa-a", PeerNativeID: "1@s", DisplayName: "Ana"}, {AccountID: "wa-b", PeerNativeID: "2@s"},
	}}))
	require.NoError(t, s.SaveCampaign(ctx, model.Campaign{ID: "camp", AccountID: "wa-a", Name: "x", State: model.CampaignDraft}))
	require.NoError(t, s.ReplaceQueue(ctx, "camp", []model.QueueItem{{ID: "q1", ContactID: "c1", ChatNativeID: "1@s", State: "pending", Risk: "warm", Text: "hola"}}))
	require.NoError(t, s.SaveScheduled(ctx, model.Scheduled{ID: "s1", AccountID: "wa-a", Kind: "message", SendAt: 1, State: "pending", Message: &model.Outgoing{Kind: model.KindText, Text: "x"}}))
	require.NoError(t, s.SetSetting(ctx, "pacing:wa-a", "{}"))
	require.NoError(t, s.SetSetting(ctx, "pacing:wa-b", "{}"))

	require.NoError(t, s.DeleteAccount(ctx, "wa-a"))

	_, err := s.GetAccount(ctx, "wa-a")
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.FindContactByIdentity(ctx, "wa-a", "1@s")
	require.ErrorIs(t, err, port.ErrNotFound)
	c, err := s.GetContact(ctx, "c1")
	require.NoError(t, err)
	require.Len(t, c.Identities, 1)
	require.Equal(t, model.AccountID("wa-b"), c.Identities[0].AccountID)
	_, err = s.GetCampaign(ctx, "camp")
	require.ErrorIs(t, err, port.ErrNotFound)
	q, err := s.ListQueue(ctx, "camp", nil, 0)
	require.NoError(t, err)
	require.Empty(t, q)
	sc, _ := s.ListScheduled(ctx, 0, 0, nil)
	require.Empty(t, sc)
	v, _ := s.GetSetting(ctx, "pacing:wa-a")
	require.Equal(t, "", v)
	v, _ = s.GetSetting(ctx, "pacing:wa-b")
	require.Equal(t, "{}", v)
	// Borrar una cuenta inexistente no falla.
	require.NoError(t, s.DeleteAccount(ctx, "wa-zzz"))
}

func TestSettings(t *testing.T) {
	s := newTestApp(t)
	v, err := s.GetSetting(ctx, "theme")
	require.NoError(t, err)
	require.Equal(t, "", v)
	require.NoError(t, s.SetSetting(ctx, "theme", `"dark"`))
	require.NoError(t, s.SetSetting(ctx, "theme", `"light"`))
	v, _ = s.GetSetting(ctx, "theme")
	require.Equal(t, `"light"`, v)
	require.Error(t, s.SetSetting(ctx, "", "x"))
}

func TestContactsCRUDAndFilters(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	saveTestAccount(t, s, "tg-b")
	require.NoError(t, s.SaveList(ctx, model.ContactList{ID: "l1", Name: "Clientes"}))
	require.NoError(t, s.SaveList(ctx, model.ContactList{ID: "l2", Name: "Amigos", Color: "#ff0000"}))

	require.NoError(t, s.SaveContact(ctx, model.Contact{
		ID: "c1", Name: "Ana Pérez", Phone: "+593991111111", Notes: "VIP 100%", Tags: []string{"vip", "quito"},
		ListIDs: []string{"l1", "l2"}, CreatedAt: 5,
		Identities: []model.Identity{
			{AccountID: "wa-a", PeerNativeID: "593991111111@s.whatsapp.net", DisplayName: "Ana", Phone: "+593991111111"},
			{AccountID: "tg-b", PeerNativeID: "777", DisplayName: "ana_p"},
		},
	}))
	require.NoError(t, s.SaveContact(ctx, model.Contact{ID: "c2", Name: "bruno", Phone: "+593992222222", Tags: []string{"Quito"}, ListIDs: []string{"l1", "no-existe"}}))
	require.NoError(t, s.SaveContact(ctx, model.Contact{ID: "c3", Name: "Carla", OptedOut: true}))

	c1, err := s.GetContact(ctx, "c1")
	require.NoError(t, err)
	require.Equal(t, "Ana Pérez", c1.Name)
	require.Equal(t, []string{"vip", "quito"}, c1.Tags)
	require.Equal(t, []string{"l1", "l2"}, c1.ListIDs)
	require.Len(t, c1.Identities, 2)
	require.Equal(t, model.ChannelTelegram, c1.Identities[0].Channel) // orden por cuenta: tg-b antes que wa-a
	require.Equal(t, model.ChannelWhatsApp, c1.Identities[1].Channel)
	require.Equal(t, int64(5), c1.CreatedAt)
	require.NotZero(t, c1.UpdatedAt)

	c2, _ := s.GetContact(ctx, "c2")
	require.Equal(t, []string{"l1"}, c2.ListIDs) // la lista que no existe se ignora
	c3, _ := s.GetContact(ctx, "c3")
	require.True(t, c3.OptedOut)
	require.Empty(t, c3.Tags)
	require.NotNil(t, c3.Tags)
	_, err = s.GetContact(ctx, "zzz")
	require.ErrorIs(t, err, port.ErrNotFound)

	// Orden alfabético sin distinguir mayúsculas.
	all, total, err := s.ListContacts(ctx, model.ContactFilter{})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Equal(t, []string{"c1", "c2", "c3"}, ids(all))

	// Paginación: el total no cambia.
	page, total, _ := s.ListContacts(ctx, model.ContactFilter{Limit: 1, Offset: 1})
	require.Equal(t, 3, total)
	require.Equal(t, []string{"c2"}, ids(page))

	// Texto: nombre, teléfono, notas (con comodín escapado) e identidades.
	for q, want := range map[string][]string{
		"ana": {"c1"}, "99222": {"c2"}, "100%": {"c1"}, "ana_p": {"c1"}, "777": {"c1"}, "%": {"c1"}, "zzzz": {},
	} {
		got, n, err := s.ListContacts(ctx, model.ContactFilter{Query: q})
		require.NoError(t, err, q)
		require.Equal(t, len(want), n, q)
		require.Equal(t, want, ids(got), q)
	}
	got, _, _ := s.ListContacts(ctx, model.ContactFilter{ListID: "l1"})
	require.Equal(t, []string{"c1", "c2"}, ids(got))
	got, _, _ = s.ListContacts(ctx, model.ContactFilter{ListID: "l2"})
	require.Equal(t, []string{"c1"}, ids(got))
	got, _, _ = s.ListContacts(ctx, model.ContactFilter{Tag: "QUITO"})
	require.Equal(t, []string{"c1", "c2"}, ids(got))
	got, _, _ = s.ListContacts(ctx, model.ContactFilter{Tag: "vip", ListID: "l1"})
	require.Equal(t, []string{"c1"}, ids(got))

	// Buscar por identidad y por teléfono.
	f, err := s.FindContactByIdentity(ctx, "tg-b", "777")
	require.NoError(t, err)
	require.Equal(t, "c1", f.ID)
	_, err = s.FindContactByIdentity(ctx, "tg-b", "888")
	require.ErrorIs(t, err, port.ErrNotFound)
	ph, err := s.FindContactsByPhone(ctx, "+593991111111")
	require.NoError(t, err)
	require.Equal(t, []string{"c1"}, ids(ph))
	ph, _ = s.FindContactsByPhone(ctx, "+593999999999")
	require.Empty(t, ph)

	// Guardar de nuevo reemplaza identidades, etiquetas y listas.
	c1.Name = "Ana P."
	c1.Tags = []string{"nuevo"}
	c1.ListIDs = []string{"l2"}
	c1.Identities = c1.Identities[:1]
	require.NoError(t, s.SaveContact(ctx, c1))
	c1b, _ := s.GetContact(ctx, "c1")
	require.Equal(t, "Ana P.", c1b.Name)
	require.Equal(t, []string{"nuevo"}, c1b.Tags)
	require.Equal(t, []string{"l2"}, c1b.ListIDs)
	require.Len(t, c1b.Identities, 1)

	// Una identidad que ya era de otro contacto pasa al nuevo.
	require.NoError(t, s.SaveContact(ctx, model.Contact{ID: "c2", Name: "bruno", Identities: []model.Identity{
		{AccountID: "wa-a", PeerNativeID: "593991111111@s.whatsapp.net"}}}))
	f, _ = s.FindContactByIdentity(ctx, "wa-a", "593991111111@s.whatsapp.net")
	require.Equal(t, "c2", f.ID)

	// Borrar el contacto lo saca de las listas.
	require.NoError(t, s.DeleteContact(ctx, "c2"))
	_, err = s.GetContact(ctx, "c2")
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.FindContactByIdentity(ctx, "wa-a", "593991111111@s.whatsapp.net")
	require.ErrorIs(t, err, port.ErrNotFound)
	lists, _ := s.ListLists(ctx)
	require.Equal(t, 0, memberCount(lists, "l1"))
}

func ids(cs []model.Contact) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func memberCount(ls []model.ContactList, id string) int {
	for _, l := range ls {
		if l.ID == id {
			return l.MemberCount
		}
	}
	return -1
}

func TestListsAndMembers(t *testing.T) {
	s := newTestApp(t)
	require.NoError(t, s.SaveList(ctx, model.ContactList{ID: "l1", Name: "zeta"}))
	require.NoError(t, s.SaveList(ctx, model.ContactList{ID: "l2", Name: "Alfa", Color: "#00ff00"}))
	for _, id := range []string{"c1", "c2", "c3"} {
		require.NoError(t, s.SaveContact(ctx, model.Contact{ID: id, Name: "Contacto " + id}))
	}
	lists, err := s.ListLists(ctx)
	require.NoError(t, err)
	require.Equal(t, "l2", lists[0].ID) // por nombre
	require.Equal(t, "#64748b", lists[1].Color)
	require.Equal(t, "#00ff00", lists[0].Color)

	require.NoError(t, s.SetListMembers(ctx, "l1", []string{"c1", "c2", "c2", "fantasma"}, nil))
	require.NoError(t, s.SetListMembers(ctx, "l2", []string{"c2", "c3"}, nil))
	lists, _ = s.ListLists(ctx)
	require.Equal(t, 2, memberCount(lists, "l1"))
	require.Equal(t, 2, memberCount(lists, "l2"))

	// Únicos aunque estén en varias listas.
	m, err := s.ListMembers(ctx, []string{"l1", "l2"})
	require.NoError(t, err)
	require.Equal(t, []string{"c1", "c2", "c3"}, ids(m))
	m, _ = s.ListMembers(ctx, []string{"l1"})
	require.Equal(t, []string{"c1", "c2"}, ids(m))
	m, _ = s.ListMembers(ctx, nil)
	require.Empty(t, m)

	require.NoError(t, s.SetListMembers(ctx, "l1", []string{"c3"}, []string{"c1", "c9"}))
	m, _ = s.ListMembers(ctx, []string{"l1"})
	require.Equal(t, []string{"c2", "c3"}, ids(m))
	require.ErrorIs(t, s.SetListMembers(ctx, "nada", []string{"c1"}, nil), port.ErrNotFound)

	// Renombrar no toca los miembros; borrar la lista sí los suelta.
	require.NoError(t, s.SaveList(ctx, model.ContactList{ID: "l1", Name: "Otro", Color: "#123456"}))
	lists, _ = s.ListLists(ctx)
	require.Equal(t, 2, memberCount(lists, "l1"))
	require.NoError(t, s.DeleteList(ctx, "l1"))
	lists, _ = s.ListLists(ctx)
	require.Len(t, lists, 1)
	c, _ := s.GetContact(ctx, "c3")
	require.Equal(t, []string{"l2"}, c.ListIDs)
	require.Error(t, s.SaveList(ctx, model.ContactList{}))
}

func TestCampaignsAndQueue(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	saveTestAccount(t, s, "wa-b")
	camp := model.Campaign{
		ID: "k1", Name: "Promo", AccountID: "wa-a", State: model.CampaignReady, Text: "Hola {nombre}",
		Variants: []string{"B", "C"}, FilePath: "campaign-media/k1/a.jpg", ListIDs: []string{"l1"}, ContactIDs: []string{"c1", "c2"},
		StartAt: 1000, Window: model.SendWindow{FromMinute: 540, ToMinute: 1080, Weekdays: []int{1, 2, 3}},
		Pacing: model.DefaultPacing, OptOutFooter: true, ABEnabled: true, CreatedAt: 50, UpdatedAt: 60,
	}
	require.NoError(t, s.SaveCampaign(ctx, camp))
	camp2 := camp
	camp2.ID, camp2.Name, camp2.AccountID, camp2.CreatedAt = "k2", "Otra", "wa-b", 70
	camp2.Variants, camp2.ListIDs, camp2.ContactIDs = nil, nil, nil
	camp2.Window.Weekdays = nil
	require.NoError(t, s.SaveCampaign(ctx, camp2))

	got, err := s.GetCampaign(ctx, "k1")
	require.NoError(t, err)
	require.Equal(t, camp, got)
	got2, _ := s.GetCampaign(ctx, "k2")
	require.Equal(t, []string{}, got2.Variants)
	require.Equal(t, []int{}, got2.Window.Weekdays)

	list, err := s.ListCampaigns(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, "k2", list[0].ID) // más reciente primero
	_, err = s.GetCampaign(ctx, "zzz")
	require.ErrorIs(t, err, port.ErrNotFound)

	items := []model.QueueItem{
		{ID: "q1", ContactID: "c1", ChatNativeID: "1@s", Risk: "warm", Variant: 0, State: "pending", Text: "t1"},
		{ID: "q2", ContactID: "c2", ChatNativeID: "2@s", Risk: "cold", Variant: 1, ABSample: true, State: "held", Text: "t2"},
		{ContactID: "c3", ChatNativeID: "3@s", Risk: "lukewarm", State: "pending", Text: "t3"},
		{ID: "q4", ContactID: "c4", ChatNativeID: "4@s", Risk: "cold", State: "pending", Text: "t4"},
	}
	require.NoError(t, s.ReplaceQueue(ctx, "k1", items))
	q, err := s.ListQueue(ctx, "k1", nil, 0)
	require.NoError(t, err)
	require.Len(t, q, 4)
	require.Equal(t, []string{"q1", "q2"}, []string{q[0].ID, q[1].ID})
	require.NotEmpty(t, q[2].ID) // se generó
	require.Equal(t, "k1", q[0].CampaignID)
	require.True(t, q[1].ABSample)

	q, _ = s.ListQueue(ctx, "k1", []string{"pending"}, 2)
	require.Len(t, q, 2)
	require.Equal(t, "q1", q[0].ID)
	q, _ = s.ListQueue(ctx, "k1", []string{"held", "sent"}, 0)
	require.Len(t, q, 1)

	// Reemplazar deja solo lo nuevo.
	require.NoError(t, s.ReplaceQueue(ctx, "k1", items[:2]))
	q, _ = s.ListQueue(ctx, "k1", nil, 0)
	require.Len(t, q, 2)
	require.ErrorIs(t, s.ReplaceQueue(ctx, "nada", items), port.ErrNotFound)

	// Actualizar, totales, enviados desde y búsqueda por mensaje.
	now := time.Now().UnixMilli()
	it := q[0]
	it.State, it.SentAt, it.MessageKey = "sent", now, model.NewMessageKey("wa-a", "1@s", "MSG1")
	require.NoError(t, s.UpdateQueueItem(ctx, it))
	it2 := q[1]
	it2.State, it2.Error = "failed", "sin conexión"
	require.NoError(t, s.UpdateQueueItem(ctx, it2))
	require.ErrorIs(t, s.UpdateQueueItem(ctx, model.QueueItem{ID: "nada"}), port.ErrNotFound)

	got, _ = s.GetCampaign(ctx, "k1")
	require.Equal(t, model.CampaignTotals{Sent: 1, Failed: 1}, got.Totals)
	n, err := s.CountSentSince(ctx, "wa-a", now-1000)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, _ = s.CountSentSince(ctx, "wa-a", now+1000)
	require.Equal(t, 0, n)
	n, _ = s.CountSentSince(ctx, "wa-b", 0)
	require.Equal(t, 0, n)

	f, err := s.QueueItemByMessage(ctx, model.NewMessageKey("wa-a", "1@s", "MSG1"))
	require.NoError(t, err)
	require.Equal(t, "q1", f.ID)
	_, err = s.QueueItemByMessage(ctx, "wa-a|1@s|otro")
	require.ErrorIs(t, err, port.ErrNotFound)

	// Totales de los retenidos y pendientes.
	require.NoError(t, s.ReplaceQueue(ctx, "k1", items))
	got, _ = s.GetCampaign(ctx, "k1")
	require.Equal(t, 4, got.Totals.Queued)

	// Borrar la campaña borra su cola.
	require.NoError(t, s.DeleteCampaign(ctx, "k1"))
	q, _ = s.ListQueue(ctx, "k1", nil, 0)
	require.Empty(t, q)
	var cnt int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM campaign_queue`).Scan(&cnt))
	require.Equal(t, 0, cnt)
}

func TestScheduled(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	msg := model.Scheduled{
		ID: "s1", AccountID: "wa-a", Kind: "message", ChatNativeID: "1@s", ChatTitle: "Ana", SendAt: 1000, State: "pending",
		Message: &model.Outgoing{Kind: model.KindText, Text: "hola", ReplyTo: &model.MessageRef{NativeID: "X"}}, CreatedAt: 1,
	}
	st := model.Scheduled{
		ID: "s2", AccountID: "wa-a", Kind: "status", SendAt: 2000, State: "pending",
		Status: &model.OutgoingStatus{Kind: model.KindText, Text: "estado", BgColor: "#000000", Audience: model.StatusAudience{Mode: "allow", PeerIDs: []string{"a"}}}, CreatedAt: 2,
	}
	done := model.Scheduled{ID: "s3", AccountID: "wa-a", Kind: "message", SendAt: 500, State: "sent", SentAt: 510, Message: &model.Outgoing{Kind: model.KindText, Text: "x"}}
	for _, it := range []model.Scheduled{msg, st, done} {
		require.NoError(t, s.SaveScheduled(ctx, it))
	}
	require.Error(t, s.SaveScheduled(ctx, model.Scheduled{ID: "bad", AccountID: "wa-a", Kind: "message"}))
	require.Error(t, s.SaveScheduled(ctx, model.Scheduled{AccountID: "wa-a", Kind: "message", Message: &model.Outgoing{}}))

	all, err := s.ListScheduled(ctx, 0, 0, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"s3", "s1", "s2"}, schedIDs(all))
	require.Equal(t, msg, all[1])
	require.Equal(t, st, all[2])
	require.Nil(t, all[2].Message)
	require.Nil(t, all[1].Status)

	l, _ := s.ListScheduled(ctx, 600, 1500, nil)
	require.Equal(t, []string{"s1"}, schedIDs(l))
	l, _ = s.ListScheduled(ctx, 0, 0, []string{"sent"})
	require.Equal(t, []string{"s3"}, schedIDs(l))
	l, _ = s.ListScheduled(ctx, 1000, 2000, []string{"pending", "failed"})
	require.Equal(t, []string{"s1", "s2"}, schedIDs(l))

	due, err := s.DueScheduled(ctx, 1500)
	require.NoError(t, err)
	require.Equal(t, []string{"s1"}, schedIDs(due)) // s3 ya se envió
	due, _ = s.DueScheduled(ctx, 999)
	require.Empty(t, due)

	// Marcar como enviado lo saca de los vencidos; el id repetido actualiza.
	msg.State, msg.SentAt, msg.CreatedAt = "sent", 1100, 999
	require.NoError(t, s.SaveScheduled(ctx, msg))
	due, _ = s.DueScheduled(ctx, 5000)
	require.Equal(t, []string{"s2"}, schedIDs(due))
	all, _ = s.ListScheduled(ctx, 0, 0, nil)
	require.Len(t, all, 3)
	require.Equal(t, int64(1), all[1].CreatedAt) // created_at no cambia

	require.NoError(t, s.DeleteScheduled(ctx, "s2"))
	all, _ = s.ListScheduled(ctx, 0, 0, nil)
	require.Len(t, all, 2)
}

func schedIDs(l []model.Scheduled) []string {
	out := []string{}
	for _, x := range l {
		out = append(out, x.ID)
	}
	return out
}

func TestDestinationsAndOutbox(t *testing.T) {
	s := newTestApp(t)
	d1 := model.Destination{ID: "d1", Name: "Servidor", URL: "wss://x/ws", Token: "t1", Enabled: true, AllowCommands: true, CreatedAt: 1,
		Filter: model.DestinationFilter{AccountIDs: []model.AccountID{"wa-a"}, Events: []string{"message.received"}, IncludeGroups: true}}
	d2 := model.Destination{ID: "d2", Name: "Otro", URL: "ws://y", Token: "t2", CreatedAt: 2}
	require.NoError(t, s.SaveDestination(ctx, d1))
	require.NoError(t, s.SaveDestination(ctx, d2))
	list, err := s.ListDestinations(ctx)
	require.NoError(t, err)
	require.Equal(t, []model.Destination{d1, d2}, list)
	d1.Name = "Renombrado"
	d1.Enabled = false
	require.NoError(t, s.SaveDestination(ctx, d1))
	list, _ = s.ListDestinations(ctx)
	require.Equal(t, "Renombrado", list[0].Name)
	require.False(t, list[0].Enabled)
	require.Len(t, list, 2)
	require.Error(t, s.SaveDestination(ctx, model.Destination{}))

	// Outbox: idempotente y en orden de llegada.
	for _, ev := range []string{"e1", "e2", "e3"} {
		require.NoError(t, s.OutboxAdd(ctx, "d1", ev, []byte(`{"id":"`+ev+`"}`)))
	}
	require.NoError(t, s.OutboxAdd(ctx, "d1", "e2", []byte("otro payload"))) // repetido: se ignora
	require.NoError(t, s.OutboxAdd(ctx, "d2", "e1", []byte(`{"id":"e1"}`)))
	n, err := s.OutboxCount(ctx, "d1")
	require.NoError(t, err)
	require.Equal(t, 3, n)

	rows, err := s.OutboxPending(ctx, "d1", 2)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "e1", rows[0].EventID)
	require.Equal(t, "e2", rows[1].EventID)
	require.Equal(t, `{"id":"e2"}`, string(rows[1].Payload))
	require.NotZero(t, rows[0].CreatedAt)
	require.Equal(t, 1, rows[0].Attempts)
	rows, _ = s.OutboxPending(ctx, "d1", 0)
	require.Len(t, rows, 3)
	require.Equal(t, 2, rows[0].Attempts)
	require.Equal(t, 1, rows[2].Attempts)

	require.NoError(t, s.OutboxAck(ctx, "d1", []string{"e1", "e2", "no-esta"}))
	require.NoError(t, s.OutboxAck(ctx, "d1", nil))
	rows, _ = s.OutboxPending(ctx, "d1", 10)
	require.Len(t, rows, 1)
	require.Equal(t, "e3", rows[0].EventID)
	n, _ = s.OutboxCount(ctx, "d2")
	require.Equal(t, 1, n)

	// Borrar el destino borra su outbox, no el de otro.
	require.NoError(t, s.DeleteDestination(ctx, "d1"))
	n, _ = s.OutboxCount(ctx, "d1")
	require.Equal(t, 0, n)
	n, _ = s.OutboxCount(ctx, "d2")
	require.Equal(t, 1, n)
	list, _ = s.ListDestinations(ctx)
	require.Len(t, list, 1)
}

func TestOutboxTrim(t *testing.T) {
	s := newTestApp(t)
	require.NoError(t, s.SaveDestination(ctx, model.Destination{ID: "d1", Name: "a", URL: "u", Token: "t"}))
	require.NoError(t, s.SaveDestination(ctx, model.Destination{ID: "d2", Name: "b", URL: "u", Token: "t"}))
	// Filas con fechas controladas.
	put := func(dest, ev string, at int64) {
		_, err := s.db.Exec(`INSERT INTO outbox(destination_id, event_id, payload, created_at) VALUES(?, ?, x'7b7d', ?)`, dest, ev, at)
		require.NoError(t, err)
	}
	for i := 1; i <= 5; i++ {
		put("d1", "a"+string(rune('0'+i)), int64(i*100))
	}
	put("d2", "b1", 150)
	put("d2", "b2", 250)

	// Por antigüedad: se van a1 (100) y b1 (150).
	n, err := s.OutboxTrim(ctx, 200, 0)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	c, _ := s.OutboxCount(ctx, "d1")
	require.Equal(t, 4, c)
	c, _ = s.OutboxCount(ctx, "d2")
	require.Equal(t, 1, c)

	// Por cantidad (por destino): d1 se queda con las 2 más nuevas; d2 no pierde nada.
	n, err = s.OutboxTrim(ctx, 0, 2)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	rows, _ := s.OutboxPending(ctx, "d1", 0)
	require.Equal(t, []string{"a4", "a5"}, []string{rows[0].EventID, rows[1].EventID})
	c, _ = s.OutboxCount(ctx, "d2")
	require.Equal(t, 1, c)

	// Sin límites no borra nada.
	n, err = s.OutboxTrim(ctx, 0, 0)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}
