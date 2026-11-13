package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"dconnector/internal/core/model"
)

// Pruebas de los contactos automáticos (contacts_auto.go) con los dobles de contacts_test.go.

const (
	waSelf  = "593900000000@s.whatsapp.net"
	waAna   = "593991111111@s.whatsapp.net"
	waBeto  = "593992222222@s.whatsapp.net"
	waLuis  = "593993333333@s.whatsapp.net"
	waCarla = "593996666666@s.whatsapp.net"
)

func (e *ctEnv) allContacts(t *testing.T) []model.Contact {
	t.Helper()
	cs, _, err := e.app.ListContacts(context.Background(), model.ContactFilter{Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func (e *ctEnv) byName(t *testing.T, name string) model.Contact {
	t.Helper()
	for _, c := range e.allContacts(t) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no hay un contacto llamado %q", name)
	return model.Contact{}
}

// seedWA llena la cuenta wa-1 como la de una persona real: libreta, desconocidos con chat, gente
// que solo habló en un grupo, grupos, el propio número y estados.
func (e *ctEnv) seedWA() {
	st := e.accs["wa-1"]
	for _, p := range []model.Person{
		{NativeID: waAna, Name: "Ana", PushName: "Anita", Phone: "+593991111111"},
		{NativeID: waBeto, PushName: "Beto", Phone: "+593992222222"},      // sin libreta pero con chat directo
		{NativeID: waLuis, PushName: "Luis", Phone: "+593993333333"},      // solo habló en un grupo
		{NativeID: waSelf, Name: "Yo", Phone: "+593900000000"},            // la propia cuenta
		{NativeID: "1203@g.us", Name: "Grupo"},                            // un grupo
		{NativeID: "status@broadcast", Name: "Estados"},                   // estados
		{NativeID: "593994444444@s.whatsapp.net", Phone: "+593994444444"}, // nada que mostrar ni chat
	} {
		st.persons[p.NativeID] = p
	}
	for _, c := range []model.Chat{
		{NativeID: waBeto, PeerNativeID: waBeto, Kind: model.ChatDirect, Title: "Beto Ruiz"},
		{NativeID: "593995555555@s.whatsapp.net", Kind: model.ChatDirect, Title: "+593 99 555 5555"},
		{NativeID: waCarla, PeerNativeID: waCarla, Kind: model.ChatDirect, Title: "Carla", Archived: true},
		{NativeID: "1203@g.us", Kind: model.ChatGroup, Title: "Grupo"},
		{NativeID: "chan@newsletter", Kind: model.ChatChannel, Title: "Canal"},
		{NativeID: waSelf, PeerNativeID: waSelf, Kind: model.ChatSelf, Title: "Tú"},
	} {
		st.chats[c.NativeID] = c
	}
}

func TestSyncAllCreatesContactsOnlyForPeople(t *testing.T) {
	e := newCtEnv(t)
	e.seedWA()
	n, err := e.svc.SyncAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := e.allContacts(t)
	if n != 4 || len(cs) != 4 {
		t.Fatalf("creados %d, contactos %d; quería 4 (Ana, Beto, el número suelto y Carla): %+v", n, len(cs), cs)
	}
	ana := e.byName(t, "Ana") // la libreta gana al apodo
	if ana.Source != model.ContactSourceAuto || ana.Phone != "+593991111111" || len(ana.Identities) != 1 ||
		ana.Identities[0].AccountID != "wa-1" || ana.Identities[0].PeerNativeID != waAna ||
		ana.Identities[0].Channel != model.ChannelWhatsApp || ana.Identities[0].DisplayName != "Ana" {
		t.Fatalf("contacto de Ana: %+v", ana)
	}
	beto := e.byName(t, "Beto") // el apodo gana al título del chat
	if beto.Phone != "+593992222222" || beto.Source != model.ContactSourceAuto {
		t.Fatalf("contacto de Beto: %+v", beto)
	}
	phone := e.byName(t, "+593995555555") // título que es solo un número: nombre = teléfono
	if phone.Phone != "+593995555555" || len(phone.Identities) != 1 {
		t.Fatalf("contacto del número suelto: %+v", phone)
	}
	carla := e.byName(t, "Carla") // chat archivado sin persona
	if len(carla.Identities) != 1 || carla.Identities[0].PeerNativeID != waCarla || carla.Phone != "" {
		t.Fatalf("contacto de Carla: %+v", carla)
	}
	for _, c := range cs {
		for _, it := range c.Identities {
			if it.PeerNativeID == waSelf || it.PeerNativeID == waLuis || strings.Contains(it.PeerNativeID, "@g.us") ||
				strings.Contains(it.PeerNativeID, "newsletter") || strings.Contains(it.PeerNativeID, "broadcast") {
				t.Fatalf("no debía crearse contacto para %s", it.PeerNativeID)
			}
		}
	}
}

func TestSyncAllIsIdempotentAndWritesNothingWhenNothingIsNew(t *testing.T) {
	e := newCtEnv(t)
	e.seedWA()
	if _, err := e.svc.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	saves := e.app.saves
	n, err := e.svc.SyncAll(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("segunda vuelta: creados %d, err %v", n, err)
	}
	if e.app.saves != saves {
		t.Fatalf("la segunda vuelta escribió %d contactos", e.app.saves-saves)
	}
	if got := len(e.allContacts(t)); got != 4 {
		t.Fatalf("hay %d contactos, quería 4", got)
	}
}

func TestSyncAllJoinsAcrossAccountsByPhone(t *testing.T) {
	e := newCtEnv(t)
	e.accs["wa-1"].persons[waAna] = model.Person{NativeID: waAna, Name: "Ana", Phone: "+593991111111"}
	e.accs["tg-1"].persons["user:77"] = model.Person{NativeID: "user:77", Name: "Ana Telegram", Phone: "+593991111111"}
	n, err := e.svc.SyncAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := e.allContacts(t)
	if n != 1 || len(cs) != 1 || len(cs[0].Identities) != 2 {
		t.Fatalf("creados %d; contactos %+v; quería uno con dos identidades", n, cs)
	}
	chans := map[model.Channel]bool{}
	for _, it := range cs[0].Identities {
		chans[it.Channel] = true
	}
	if !chans[model.ChannelWhatsApp] || !chans[model.ChannelTelegram] {
		t.Fatalf("faltan identidades de canal: %+v", cs[0].Identities)
	}
}

func TestSamePersonInTwoAccountsOfTheSameChannelWithoutPhone(t *testing.T) {
	e := newCtEnv(t)
	// Mismo id nativo en dos cuentas distintas y sin teléfono: dos identidades, dos contactos.
	e.accs["wa-1"].persons["x@lid"] = model.Person{NativeID: "x@lid", Name: "Ana"}
	e.accs["tg-1"].persons["x@lid"] = model.Person{NativeID: "x@lid", Name: "Ana"}
	if _, err := e.svc.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(e.allContacts(t)); got != 2 {
		t.Fatalf("hay %d contactos, quería 2", got)
	}
}

func TestSyncKeepsManualContactIntact(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	vip, err := e.svc.SaveList(ctx, model.ContactList{Name: "vip"})
	if err != nil {
		t.Fatal(err)
	}
	mama := e.mustSave(t, model.Contact{Name: "Mamá", Phone: "+593991111111", Tags: []string{"familia"},
		Notes: "no llamar de noche", OptedOut: true, ListIDs: []string{vip.ID}})
	e.accs["wa-1"].persons[waAna] = model.Person{NativeID: waAna, Name: "Ana Pérez", Phone: "+593991111111"}

	n, err := e.svc.SyncAll(ctx)
	if err != nil || n != 0 {
		t.Fatalf("creados %d, err %v; quería unir a la existente", n, err)
	}
	got, _ := e.svc.Get(ctx, mama.ID)
	if got.Name != "Mamá" || got.Source != model.ContactSourceManual || !got.OptedOut || got.Notes != "no llamar de noche" ||
		len(got.Tags) != 1 || len(got.ListIDs) != 1 || len(got.Identities) != 1 || got.Identities[0].PeerNativeID != waAna {
		t.Fatalf("el contacto manual cambió o no recibió la identidad: %+v", got)
	}
	// Con la identidad ya puesta, un nombre distinto de la libreta tampoco lo cambia.
	e.accs["wa-1"].persons[waAna] = model.Person{NativeID: waAna, Name: "Ana Pérez Otra", Phone: "+593991111111"}
	if _, err := e.svc.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = e.svc.Get(ctx, mama.ID)
	if got.Name != "Mamá" || got.Identities[0].DisplayName != "Ana Pérez" {
		t.Fatalf("un contacto manual no cambia de nombre: %+v", got)
	}
}

func TestSyncKeepsImportedContactName(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	e.adapter["wa-1"].persons = []model.Person{{NativeID: waAna, Name: "Ana CSV", Phone: "+593991111111"}}
	if _, err := e.svc.ImportFromAccount(ctx, "wa-1"); err != nil {
		t.Fatal(err)
	}
	e.accs["wa-1"].persons[waAna] = model.Person{NativeID: waAna, Name: "Ana Libreta", Phone: "+593991111111"}
	if n, err := e.svc.SyncAll(ctx); err != nil || n != 0 {
		t.Fatalf("creados %d, err %v", n, err)
	}
	c := e.byName(t, "Ana CSV")
	if c.Source != model.ContactSourceImport {
		t.Fatalf("origen %q, quería import", c.Source)
	}
}

func TestAutoContactNameRefreshAndUserEdit(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	st := e.accs["wa-1"]
	acc := model.Account{ID: "wa-1", Channel: model.ChannelWhatsApp, SelfID: waSelf}

	// Primero solo se sabe el teléfono (chat con título numérico).
	chat := model.Chat{NativeID: waAna, PeerNativeID: waAna, Kind: model.ChatDirect, Title: "+593 99 111 1111"}
	st.chats[waAna] = chat
	if n, err := e.svc.SyncAccount(ctx, acc, st, nil, []model.Chat{chat}); err != nil || n != 1 {
		t.Fatalf("creados %d, err %v", n, err)
	}
	c := e.byName(t, "+593991111111")
	if c.Source != model.ContactSourceAuto {
		t.Fatalf("origen %q", c.Source)
	}

	// Llega el nombre de la libreta: el nombre solo-teléfono se refresca.
	p := model.Person{NativeID: waAna, Name: "Ana", Phone: "+593991111111"}
	st.persons[waAna] = p
	if n, err := e.svc.SyncAccount(ctx, acc, st, []model.Person{p}, nil); err != nil || n != 0 {
		t.Fatalf("creados %d, err %v", n, err)
	}
	c, _ = e.svc.Get(ctx, c.ID)
	if c.Name != "Ana" || c.Identities[0].DisplayName != "Ana" || c.Source != model.ContactSourceAuto {
		t.Fatalf("no se refrescó el nombre: %+v", c)
	}
	// Sigue el nombre de la misma identidad cuando cambia.
	p.Name = "Ana María"
	st.persons[waAna] = p
	if _, err := e.svc.SyncAccount(ctx, acc, st, []model.Person{p}, nil); err != nil {
		t.Fatal(err)
	}
	c, _ = e.svc.Get(ctx, c.ID)
	if c.Name != "Ana María" {
		t.Fatalf("nombre %q", c.Name)
	}

	// Guardar sin cambios no lo vuelve manual; las listas y etiquetas se conservan al sincronizar.
	if saved, err := e.svc.Save(ctx, c); err != nil || saved.Source != model.ContactSourceAuto {
		t.Fatalf("guardar sin cambios: %+v %v", saved, err)
	}

	// El usuario le cambia el nombre: pasa a manual y la libreta ya no lo pisa.
	c.Name = "Mi Ana"
	saved, err := e.svc.Save(ctx, c)
	if err != nil || saved.Source != model.ContactSourceManual {
		t.Fatalf("editar el nombre: %+v %v", saved, err)
	}
	p.Name = "Ana Otra Vez"
	st.persons[waAna] = p
	if _, err := e.svc.SyncAccount(ctx, acc, st, []model.Person{p}, nil); err != nil {
		t.Fatal(err)
	}
	c, _ = e.svc.Get(ctx, c.ID)
	if c.Name != "Mi Ana" {
		t.Fatalf("se pisó la edición manual: %q", c.Name)
	}
}

func TestEditingTagsOrAddingToListMakesAutoContactManual(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	e.accs["wa-1"].persons[waAna] = model.Person{NativeID: waAna, Name: "Ana", Phone: "+593991111111"}
	e.accs["wa-1"].persons[waBeto] = model.Person{NativeID: waBeto, Name: "Beto", Phone: "+593992222222"}
	if _, err := e.svc.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	ana, beto := e.byName(t, "Ana"), e.byName(t, "Beto")

	ana.Tags = []string{"vip"}
	if saved, err := e.svc.Save(ctx, ana); err != nil || saved.Source != model.ContactSourceManual {
		t.Fatalf("etiquetar: %+v %v", saved, err)
	}
	l, err := e.svc.SaveList(ctx, model.ContactList{Name: "Clientes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetListMembers(ctx, l.ID, []string{beto.ID}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Get(ctx, beto.ID)
	if got.Source != model.ContactSourceManual || len(got.ListIDs) != 1 {
		t.Fatalf("meter en una lista: %+v", got)
	}
}

func TestSyncNeverTouchesOptOutTagsListsOfAutoContacts(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	e.accs["wa-1"].persons[waAna] = model.Person{NativeID: waAna, PushName: "Anita", Phone: "+593991111111"}
	e.accs["wa-1"].chats[waAna] = model.Chat{NativeID: waAna, PeerNativeID: waAna, Kind: model.ChatDirect}
	if _, err := e.svc.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	ana := e.byName(t, "Anita")
	// Cambios hechos fuera de Contacts (la baja la marca el Router; las listas, SetListMembers).
	ana.OptedOut = true
	ana.Tags = []string{"x"}
	if err := e.app.SaveContact(ctx, ana); err != nil {
		t.Fatal(err)
	}
	e.accs["wa-1"].persons[waAna] = model.Person{NativeID: waAna, Name: "Ana", Phone: "+593991111111"}
	if _, err := e.svc.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Get(ctx, ana.ID)
	if got.Name != "Ana" || !got.OptedOut || len(got.Tags) != 1 {
		t.Fatalf("la sincronización pisó datos del usuario: %+v", got)
	}
}

func TestDeletedAutoContactIsNotRecreated(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	e.seedWA()
	if _, err := e.svc.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	ana := e.byName(t, "Ana")
	if err := e.svc.Delete(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	n, err := e.svc.SyncAll(ctx)
	if err != nil || n != 0 {
		t.Fatalf("creados %d, err %v; un contacto borrado a mano no vuelve", n, err)
	}
	if got := len(e.allContacts(t)); got != 3 {
		t.Fatalf("hay %d contactos, quería 3", got)
	}
}

func TestSyncSkipsChatsAlreadyWithContact(t *testing.T) {
	e := newCtEnv(t)
	acc := model.Account{ID: "wa-1", Channel: model.ChannelWhatsApp, SelfID: waSelf}
	ch := model.Chat{NativeID: waAna, PeerNativeID: waAna, Kind: model.ChatDirect, Title: "Ana", ContactID: "ya-existe"}
	n, err := e.svc.SyncAccount(context.Background(), acc, e.accs["wa-1"], nil, []model.Chat{ch})
	if err != nil || n != 0 || len(e.allContacts(t)) != 0 {
		t.Fatalf("creados %d, err %v", n, err)
	}
}

// batchApp cuenta las transacciones de lote: SaveContacts es la interfaz opcional del almacén.
type batchApp struct {
	*ctApp
	mu      sync.Mutex
	batches int
}

func (b *batchApp) SaveContacts(ctx context.Context, cs []model.Contact) error {
	b.mu.Lock()
	b.batches++
	b.mu.Unlock()
	for _, c := range cs {
		if err := b.ctApp.SaveContact(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func TestBigBatchesUseASingleTransaction(t *testing.T) {
	e := newCtEnv(t)
	ba := &batchApp{ctApp: e.app}
	e.svc.app = ba
	ctx := context.Background()
	acc := model.Account{ID: "wa-1", Channel: model.ChannelWhatsApp, SelfID: waSelf}

	persons := make([]model.Person, 0, 400)
	for i := 1; i <= 400; i++ {
		native := fmt.Sprintf("5939%08d@s.whatsapp.net", i)
		p := model.Person{NativeID: native, Name: fmt.Sprintf("Persona %03d", i), Phone: fmt.Sprintf("+5939%08d", i)}
		persons = append(persons, p)
		e.accs["wa-1"].persons[native] = p
	}
	n, err := e.svc.SyncAccount(ctx, acc, e.accs["wa-1"], persons, nil)
	if err != nil || n != 400 || ba.batches != 1 {
		t.Fatalf("SyncAccount: creados %d, lotes %d, err %v", n, ba.batches, err)
	}
	// SyncAll sobre lo ya sincronizado no escribe; con 100 nuevos, un solo lote más.
	if n, err := e.svc.SyncAll(ctx); err != nil || n != 0 || ba.batches != 1 {
		t.Fatalf("SyncAll sin novedades: creados %d, lotes %d, err %v", n, ba.batches, err)
	}
	for i := 401; i <= 500; i++ {
		native := fmt.Sprintf("5939%08d@s.whatsapp.net", i)
		e.accs["wa-1"].persons[native] = model.Person{NativeID: native, Name: fmt.Sprintf("Persona %03d", i)}
	}
	if n, err := e.svc.SyncAll(ctx); err != nil || n != 100 || ba.batches != 2 {
		t.Fatalf("SyncAll con 100 nuevos: creados %d, lotes %d, err %v", n, ba.batches, err)
	}
}

func TestSyncAllWithoutAccountsIsHarmless(t *testing.T) {
	e := newCtEnv(t)
	c := NewContacts(e.app, nil)
	if n, err := c.SyncAll(context.Background()); n != 0 || err != nil {
		t.Fatalf("sin servicio de cuentas: %d %v", n, err)
	}
	if n, err := e.svc.SyncAll(context.Background()); n != 0 || err != nil {
		t.Fatalf("sin personas: %d %v", n, err)
	}
}

// ---------------------------------------------------------------------------------------------
// Borrar la cuenta

func TestCleanOrphanAutoContactsFallbackScan(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	put := func(id, src string, ids ...model.Identity) {
		if err := e.app.SaveContact(ctx, model.Contact{ID: id, Name: id, Source: src, Identities: ids}); err != nil {
			t.Fatal(err)
		}
	}
	put("auto-solo", model.ContactSourceAuto)
	put("auto-con-tg", model.ContactSourceAuto, ctIdent("tg-1", "u1", "x", ""))
	put("manual", model.ContactSourceManual)
	put("import", model.ContactSourceImport)
	n, err := cleanOrphanAutoContacts(ctx, e.app) // ctApp no tiene DeleteOrphanAutoContacts: recorre
	if err != nil || n != 1 {
		t.Fatalf("borrados %d, err %v", n, err)
	}
	var left []string
	for _, c := range e.allContacts(t) {
		left = append(left, c.ID)
	}
	if len(left) != 3 {
		t.Fatalf("quedan %v", left)
	}
}

// cascadeApp imita el ON DELETE CASCADE de las identidades al borrar una cuenta.
type cascadeApp struct{ *memApp }

func (c *cascadeApp) DeleteAccount(ctx context.Context, id model.AccountID) error {
	c.memApp.mu.Lock()
	for k, ct := range c.memApp.contacts {
		var keep []model.Identity
		for _, it := range ct.Identities {
			if it.AccountID != id {
				keep = append(keep, it)
			}
		}
		ct.Identities = keep
		c.memApp.contacts[k] = ct
	}
	c.memApp.mu.Unlock()
	return c.memApp.DeleteAccount(ctx, id)
}

func TestDeleteAccountRemovesAutoContactsLeftWithoutIdentities(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.accounts.app = &cascadeApp{memApp: e.app}
	a1 := e.newAccount(model.ChannelWhatsApp, "Uno")
	a2 := e.newAccount(model.ChannelTelegram, "Dos")
	ident := func(acc model.Account, peer string) model.Identity {
		return model.Identity{AccountID: acc.ID, Channel: acc.Channel, PeerNativeID: peer}
	}
	put := func(id, src string, ids ...model.Identity) {
		_ = e.app.SaveContact(ctx, model.Contact{ID: id, Name: id, Source: src, Identities: ids})
	}
	put("auto-solo-a1", model.ContactSourceAuto, ident(a1, "1"))
	put("auto-a1-a2", model.ContactSourceAuto, ident(a1, "2"), ident(a2, "2"))
	put("manual-a1", model.ContactSourceManual, ident(a1, "3"))
	put("import-a1", model.ContactSourceImport, ident(a1, "4"))
	put("auto-a2", model.ContactSourceAuto, ident(a2, "5"))

	if err := e.accounts.Delete(ctx, a1.ID); err != nil {
		t.Fatal(err)
	}
	left := map[string]bool{}
	for id := range e.app.contacts {
		left[id] = true
	}
	want := map[string]bool{"auto-a1-a2": true, "manual-a1": true, "import-a1": true, "auto-a2": true}
	if len(left) != len(want) {
		t.Fatalf("quedan %v, quería %v", left, want)
	}
	for id := range want {
		if !left[id] {
			t.Fatalf("falta %s: quedan %v", id, left)
		}
	}
	if ids := e.app.contacts["auto-a1-a2"].Identities; len(ids) != 1 || ids[0].AccountID != a2.ID {
		t.Fatalf("identidades de auto-a1-a2: %+v", ids)
	}
}

// ---------------------------------------------------------------------------------------------
// El Router avisa a los contactos automáticos

func TestRouterCreatesAutoContactsFromPersonsChatsAndHistory(t *testing.T) {
	e := newTestEnv(t)
	contacts := NewContacts(e.app, e.accounts)
	e.router.SetContactSync(contacts)
	acc := e.newAccount(model.ChannelWhatsApp, "Ventas")
	ad := e.adapter(acc.ID)
	count := func() int {
		e.app.mu.Lock()
		defer e.app.mu.Unlock()
		return len(e.app.contacts)
	}
	nameOf := func(native string) string {
		ct, err := e.app.FindContactByIdentity(context.Background(), acc.ID, native)
		if err != nil {
			return ""
		}
		return ct.Name
	}

	// La primera persona es la propia cuenta: no crea contacto.
	ad.Inject(model.Event{Type: model.EvPerson, Person: &model.Person{NativeID: "me@s.whatsapp.net", PushName: "Yo"}})
	eventually(t, "SelfID", func() bool { a, _ := e.app.GetAccount(context.Background(), acc.ID); return a.SelfID != "" })
	if count() != 0 {
		t.Fatal("la propia cuenta creó un contacto")
	}

	// Una persona de la libreta.
	ad.Inject(model.Event{Type: model.EvPerson, Person: &model.Person{NativeID: "ana@s.whatsapp.net", Name: "Ana", Phone: "+593991111111"}})
	eventually(t, "contacto de Ana", func() bool { return nameOf("ana@s.whatsapp.net") == "Ana" })

	// Un chat directo nuevo: crea el contacto y el chat que se publica ya lo lleva.
	from := e.mark()
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("beto@s.whatsapp.net", "Beto")})
	ev := e.waitEvent(from, "chat.updated con contacto", func(ev model.Event) bool {
		return ev.Type == model.EvChatUpsert && ev.Chat != nil && ev.Chat.NativeID == "beto@s.whatsapp.net" && ev.Chat.ContactID != ""
	})
	if nameOf("beto@s.whatsapp.net") != "Beto" {
		t.Fatalf("contacto de Beto: %q (chat %+v)", nameOf("beto@s.whatsapp.net"), ev.Chat)
	}

	// Un grupo no crea contacto.
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: &model.Chat{NativeID: "g@g.us", Kind: model.ChatGroup, Title: "Grupo"}})
	e.waitEvent(from, "chat del grupo", func(ev model.Event) bool {
		return ev.Type == model.EvChatUpsert && ev.Chat != nil && ev.Chat.NativeID == "g@g.us"
	})
	if count() != 2 {
		t.Fatalf("hay %d contactos, quería 2", count())
	}

	// Un lote de historial con 120 personas y chats crea todos sus contactos.
	var persons []model.Person
	var chats []model.Chat
	for i := 0; i < 120; i++ {
		native := fmt.Sprintf("h%03d@s.whatsapp.net", i)
		persons = append(persons, model.Person{NativeID: native, PushName: fmt.Sprintf("Hist %03d", i)})
		chats = append(chats, *directChat(native, fmt.Sprintf("Hist %03d", i)))
	}
	persons = append(persons, model.Person{NativeID: "extraño@s.whatsapp.net", PushName: "Extraño"}) // sin chat ni libreta
	ad.Inject(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{Persons: persons, Chats: chats}})
	eventually(t, "contactos del historial", func() bool { return count() == 122 })
	if nameOf("extraño@s.whatsapp.net") != "" {
		t.Fatal("alguien que solo habló en un grupo no debe tener contacto")
	}
	all, _, _ := e.app.ListContacts(context.Background(), model.ContactFilter{})
	for _, ct := range all {
		if ct.Source != model.ContactSourceAuto {
			t.Fatalf("origen %q", ct.Source)
		}
	}
}
