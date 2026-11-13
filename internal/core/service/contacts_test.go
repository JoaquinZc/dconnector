package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// ---------------------------------------------------------------------------------------------
// Dobles propios de estas pruebas (no dependen de testutil_test.go). Todo lo que no se
// implementa entra en pánico por la interfaz embebida en nil.

// ctApp: AppStore en memoria solo con contactos y listas, con la semántica del almacén real:
// una identidad (cuenta + id nativo) tiene un único dueño y las listas se guardan con el contacto.
type ctApp struct {
	port.AppStore
	mu       sync.Mutex
	contacts map[string]model.Contact
	lists    map[string]model.ContactList
	members  map[string]map[string]bool // lista -> contactos
	settings map[string]string
	saves    int // cuántas veces se llamó a SaveContact
}

func newCtApp() *ctApp {
	return &ctApp{
		contacts: map[string]model.Contact{}, lists: map[string]model.ContactList{},
		members: map[string]map[string]bool{},
	}
}

func (a *ctApp) full(c model.Contact) model.Contact {
	c.Identities = append([]model.Identity{}, c.Identities...)
	c.Tags = append([]string{}, c.Tags...)
	c.ListIDs = []string{}
	for lid, ms := range a.members {
		if ms[c.ID] {
			c.ListIDs = append(c.ListIDs, lid)
		}
	}
	sort.Strings(c.ListIDs)
	return c
}

func (a *ctApp) GetContact(ctx context.Context, id string) (model.Contact, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.contacts[id]
	if !ok {
		return model.Contact{}, port.ErrNotFound
	}
	return a.full(c), nil
}

func (a *ctApp) FindContactByIdentity(ctx context.Context, acc model.AccountID, peer string) (model.Contact, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.contacts {
		for _, it := range c.Identities {
			if it.AccountID == acc && it.PeerNativeID == peer {
				return a.full(c), nil
			}
		}
	}
	return model.Contact{}, port.ErrNotFound
}

func (a *ctApp) FindContactsByPhone(ctx context.Context, phone string) ([]model.Contact, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []model.Contact{}
	for _, c := range a.contacts {
		hit := c.Phone == phone
		for _, it := range c.Identities {
			hit = hit || it.Phone == phone
		}
		if hit {
			out = append(out, a.full(c))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (a *ctApp) SaveContact(ctx context.Context, c model.Contact) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.saves++
	for id, other := range a.contacts {
		if id == c.ID {
			continue
		}
		var keep []model.Identity
		for _, it := range other.Identities {
			moved := false
			for _, mine := range c.Identities {
				if mine.AccountID == it.AccountID && mine.PeerNativeID == it.PeerNativeID {
					moved = true
				}
			}
			if !moved {
				keep = append(keep, it)
			}
		}
		other.Identities = keep
		a.contacts[id] = other
	}
	for _, ms := range a.members {
		delete(ms, c.ID)
	}
	for _, lid := range c.ListIDs {
		if _, ok := a.lists[lid]; ok {
			a.members[lid][c.ID] = true
		}
	}
	c.ListIDs = nil
	a.contacts[c.ID] = c
	return nil
}

func (a *ctApp) DeleteContact(ctx context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.contacts, id)
	for _, ms := range a.members {
		delete(ms, id)
	}
	return nil
}

func (a *ctApp) ListContacts(ctx context.Context, f model.ContactFilter) ([]model.Contact, int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var all []model.Contact
	for _, c := range a.contacts {
		if f.ListID != "" && !a.members[f.ListID][c.ID] {
			continue
		}
		if q := strings.ToLower(f.Query); q != "" &&
			!strings.Contains(strings.ToLower(c.Name), q) && !strings.Contains(c.Phone, q) {
			continue
		}
		all = append(all, a.full(c))
	}
	sort.Slice(all, func(i, j int) bool {
		x, y := strings.ToLower(all[i].Name), strings.ToLower(all[j].Name)
		if x != y {
			return x < y
		}
		return all[i].ID < all[j].ID
	})
	total := len(all)
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if f.Offset >= len(all) {
		return []model.Contact{}, total, nil
	}
	all = all[f.Offset:]
	if len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

func (a *ctApp) ListLists(ctx context.Context) ([]model.ContactList, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []model.ContactList{}
	for _, l := range a.lists {
		l.MemberCount = len(a.members[l.ID])
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (a *ctApp) SaveList(ctx context.Context, l model.ContactList) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if l.Color == "" {
		l.Color = "#64748b"
	}
	a.lists[l.ID] = l
	if a.members[l.ID] == nil {
		a.members[l.ID] = map[string]bool{}
	}
	return nil
}

func (a *ctApp) DeleteList(ctx context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.lists, id)
	delete(a.members, id)
	return nil
}

func (a *ctApp) SetListMembers(ctx context.Context, listID string, add, remove []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.lists[listID]; !ok {
		return port.ErrNotFound
	}
	for _, id := range remove {
		delete(a.members[listID], id)
	}
	for _, id := range add {
		if _, ok := a.contacts[id]; ok {
			a.members[listID][id] = true
		}
	}
	return nil
}

// ctStore: AccountStore con chats y personas.
type ctStore struct {
	port.AccountStore
	chats   map[string]model.Chat
	persons map[string]model.Person
}

func (s *ctStore) GetChat(ctx context.Context, nativeID string) (model.Chat, error) {
	c, ok := s.chats[nativeID]
	if !ok {
		return model.Chat{}, port.ErrNotFound
	}
	return c, nil
}

func (s *ctStore) GetPerson(ctx context.Context, nativeID string) (model.Person, error) {
	p, ok := s.persons[nativeID]
	if !ok {
		return model.Person{}, port.ErrNotFound
	}
	return p, nil
}

// ctAdapter: adaptador con solo ListPersons.
type ctAdapter struct {
	port.ChannelAdapter
	persons []model.Person
	err     error
}

func (a *ctAdapter) ListPersons(ctx context.Context) ([]model.Person, error) {
	return a.persons, a.err
}

type ctEnv struct {
	app     *ctApp
	svc     *Contacts
	accs    map[model.AccountID]*ctStore
	adapter map[model.AccountID]*ctAdapter
	tick    int64
}

// newCtEnv: dos cuentas, `wa-1` (WhatsApp) y `tg-1` (Telegram).
func newCtEnv(t *testing.T) *ctEnv {
	t.Helper()
	e := &ctEnv{app: newCtApp(), accs: map[model.AccountID]*ctStore{}, adapter: map[model.AccountID]*ctAdapter{}}
	accounts := &Accounts{instances: map[model.AccountID]*instance{}}
	for _, a := range []model.Account{
		{ID: "wa-1", Channel: model.ChannelWhatsApp, SelfID: "593900000000@s.whatsapp.net"},
		{ID: "tg-1", Channel: model.ChannelTelegram, SelfID: "user:1"},
	} {
		st := &ctStore{chats: map[string]model.Chat{}, persons: map[string]model.Person{}}
		ad := &ctAdapter{}
		e.accs[a.ID], e.adapter[a.ID] = st, ad
		accounts.instances[a.ID] = &instance{acc: a, store: st, adapter: ad}
	}
	e.svc = NewContacts(e.app, accounts)
	// Reloj que avanza un segundo por llamada: el orden de creación es el de las llamadas.
	e.svc.now = func() time.Time {
		e.tick++
		return time.UnixMilli(1_700_000_000_000 + e.tick*1000)
	}
	return e
}

func (e *ctEnv) mustSave(t *testing.T, ct model.Contact) model.Contact {
	t.Helper()
	got, err := e.svc.Save(context.Background(), ct)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	return got
}

func ctIdent(acc model.AccountID, peer, name, phone string) model.Identity {
	ch := model.ChannelWhatsApp
	if strings.HasPrefix(string(acc), "tg") {
		ch = model.ChannelTelegram
	}
	return model.Identity{AccountID: acc, Channel: ch, PeerNativeID: peer, DisplayName: name, Phone: phone}
}

func ctIsErr(err, target error) bool { return err != nil && errors.Is(err, target) }

// ---------------------------------------------------------------------------------------------

func TestNormalizePhoneE164(t *testing.T) {
	cases := map[string]string{
		"+593 98 765 4321":     "+593987654321",
		"+593987654321":        "+593987654321",
		"0987654321":           "+593987654321", // Ecuador: 10 dígitos que empiezan por 0
		"098-765-4321":         "+593987654321",
		"593987654321":         "+593987654321",
		"(593) 98 765 4321":    "+593987654321",
		"0059398765432":        "+59398765432", // 00 = +
		"+1 (415) 555-2671":    "+14155552671",
		"4155552671":           "+4155552671", // 10 dígitos que no empiezan por 0: no se asume nada
		"  ":                   "",
		"":                     "",
		"abc":                  "",
		"123":                  "", // demasiado corto
		"+1234567890123456789": "", // demasiado largo
		"+0987654321":          "+0987654321",
	}
	for in, want := range cases {
		if got := NormalizePhoneE164(in); got != want {
			t.Errorf("NormalizePhoneE164(%q) = %q, quería %q", in, got, want)
		}
	}
}

func TestSaveNormalizesAndValidates(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	got := e.mustSave(t, model.Contact{
		Name: "  Ana ", Phone: "0987654321", Tags: []string{"vip", " VIP ", "", "mayorista"}, ListIDs: nil,
		Identities: []model.Identity{
			ctIdent("wa-1", "a@s", "Ana", "098 765 4321"), ctIdent("wa-1", "a@s", "dup", ""), ctIdent("wa-1", "", "vacía", ""),
		},
	})
	if got.ID == "" || got.Name != "Ana" || got.Phone != "+593987654321" || got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Fatalf("contacto: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "vip" || got.Tags[1] != "mayorista" {
		t.Fatalf("etiquetas: %v", got.Tags)
	}
	if len(got.Identities) != 1 || got.Identities[0].Phone != "+593987654321" {
		t.Fatalf("identidades: %+v", got.Identities)
	}

	// Actualizar conserva la fecha de creación.
	got.Notes = "nota"
	upd, err := e.svc.Save(ctx, got)
	if err != nil || upd.CreatedAt != got.CreatedAt || upd.UpdatedAt <= got.UpdatedAt || upd.Notes != "nota" {
		t.Fatalf("actualizar: %+v %v", upd, err)
	}

	// Sin nombre se usa el teléfono; sin nada, error.
	if p, err := e.svc.Save(ctx, model.Contact{Phone: "0991112233"}); err != nil || p.Name != "+593991112233" {
		t.Fatalf("solo teléfono: %+v %v", p, err)
	}
	if _, err := e.svc.Save(ctx, model.Contact{}); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("vacío: %v", err)
	}
	if _, err := e.svc.Save(ctx, model.Contact{Name: "X", Phone: "12"}); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("teléfono inválido: %v", err)
	}
	if _, err := e.svc.Save(ctx, model.Contact{ID: "no-existe", Name: "X"}); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("id desconocido: %v", err)
	}
	if _, err := e.svc.Get(ctx, ""); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("Get vacío: %v", err)
	}
	if g, err := e.svc.Get(ctx, got.ID); err != nil || g.ID != got.ID {
		t.Fatalf("Get: %+v %v", g, err)
	}
}

func TestListAndDelete(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	if out, total, err := e.svc.List(ctx, model.ContactFilter{}); err != nil || out == nil || total != 0 {
		t.Fatalf("vacío: %#v %d %v", out, total, err)
	}
	a := e.mustSave(t, model.Contact{Name: "Ana", Phone: "0987654321"})
	e.mustSave(t, model.Contact{Name: "Beto", Phone: "0991112233"})
	out, total, err := e.svc.List(ctx, model.ContactFilter{Query: "an"})
	if err != nil || total != 1 || out[0].ID != a.ID {
		t.Fatalf("filtro: %+v %d %v", out, total, err)
	}
	if err := e.svc.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, a.ID); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("borrado: %v", err)
	}
	if err := e.svc.Delete(ctx, ""); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("Delete vacío: %v", err)
	}
}

func TestFromChat(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	wa := e.accs["wa-1"]
	wa.chats["593981112233@s.whatsapp.net"] = model.Chat{
		NativeID: "593981112233@s.whatsapp.net", Kind: model.ChatDirect, Title: "María", PeerNativeID: "593981112233@s.whatsapp.net",
	}
	wa.persons["593981112233@s.whatsapp.net"] = model.Person{NativeID: "593981112233@s.whatsapp.net", Name: "María", Phone: "+593981112233"}
	wa.chats["g1@g.us"] = model.Chat{NativeID: "g1@g.us", Kind: model.ChatGroup, Title: "Grupo"}
	key := model.NewChatKey("wa-1", "593981112233@s.whatsapp.net")

	ct, err := e.svc.FromChat(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if ct.Name != "María" || ct.Phone != "+593981112233" || len(ct.Identities) != 1 ||
		ct.Identities[0].AccountID != "wa-1" || ct.Identities[0].Channel != model.ChannelWhatsApp {
		t.Fatalf("contacto: %+v", ct)
	}
	again, err := e.svc.FromChat(ctx, key)
	if err != nil || again.ID != ct.ID {
		t.Fatalf("segunda vez: %+v %v", again, err)
	}
	if _, total, _ := e.svc.List(ctx, model.ContactFilter{}); total != 1 {
		t.Fatalf("duplicó: %d", total)
	}

	// Un chat de Telegram de la misma persona (mismo teléfono) se añade al contacto existente.
	e.accs["tg-1"].chats["user:77"] = model.Chat{NativeID: "user:77", Kind: model.ChatDirect, Title: "Mari", PeerNativeID: "user:77"}
	e.accs["tg-1"].persons["user:77"] = model.Person{NativeID: "user:77", Name: "Mari", Phone: "0981112233"}
	joined, err := e.svc.FromChat(ctx, model.NewChatKey("tg-1", "user:77"))
	if err != nil || joined.ID != ct.ID || len(joined.Identities) != 2 {
		t.Fatalf("unir por teléfono: %+v %v", joined, err)
	}

	// Un chat sin persona guardada ni teléfono crea un contacto solo con la identidad.
	e.accs["tg-1"].chats["user:88"] = model.Chat{NativeID: "user:88", Kind: model.ChatDirect, Title: "Pedro"}
	solo, err := e.svc.FromChat(ctx, model.NewChatKey("tg-1", "user:88"))
	if err != nil || solo.Name != "Pedro" || solo.Phone != "" || len(solo.Identities) != 1 || solo.Identities[0].PeerNativeID != "user:88" {
		t.Fatalf("sin teléfono: %+v %v", solo, err)
	}

	if _, err := e.svc.FromChat(ctx, model.NewChatKey("wa-1", "g1@g.us")); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("grupo: %v", err)
	}
	if _, err := e.svc.FromChat(ctx, model.NewChatKey("wa-1", "nada")); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("chat desconocido: %v", err)
	}
	if _, err := e.svc.FromChat(ctx, model.NewChatKey("xx-9", "nada")); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
	if _, err := e.svc.FromChat(ctx, "sin-barra"); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("clave inválida: %v", err)
	}
}

func TestImportFromAccount(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	// Un contacto que ya existe con el teléfono de «Luis».
	luis := e.mustSave(t, model.Contact{Name: "Luis (cliente)", Phone: "+593982223344", Tags: []string{"vip"}})

	e.adapter["wa-1"].persons = []model.Person{
		{NativeID: "593981112233@s.whatsapp.net", Name: "Ana", Phone: "+593981112233"},
		{NativeID: "593982223344@s.whatsapp.net", PushName: "Luis P", Phone: "0982223344"}, // ya existe por teléfono
		{NativeID: "593900000000@s.whatsapp.net", Name: "Yo", Phone: "+593900000000"},      // la propia cuenta
		{NativeID: "", Name: "Sin id"},
		{NativeID: "lid:1", PushName: "Anónimo"}, // sin teléfono
	}
	res, err := e.svc.ImportFromAccount(ctx, "wa-1")
	if err != nil {
		t.Fatal(err)
	}
	if res != (ImportResult{Created: 2, Updated: 1, Skipped: 2}) {
		t.Fatalf("resultado: %+v", res)
	}
	got, _ := e.svc.Get(ctx, luis.ID)
	if len(got.Identities) != 1 || got.Identities[0].PeerNativeID != "593982223344@s.whatsapp.net" ||
		got.Identities[0].Phone != "+593982223344" || got.Name != "Luis (cliente)" || len(got.Tags) != 1 {
		t.Fatalf("Luis: %+v", got)
	}
	if _, total, _ := e.svc.List(ctx, model.ContactFilter{}); total != 3 {
		t.Fatalf("contactos: %d", total)
	}
	anon, err := e.app.FindContactByIdentity(ctx, "wa-1", "lid:1")
	if err != nil || anon.Name != "Anónimo" || anon.Phone != "" || len(anon.Identities) != 1 {
		t.Fatalf("sin teléfono: %+v %v", anon, err)
	}

	// Volver a importar no duplica nada.
	res, err = e.svc.ImportFromAccount(ctx, "wa-1")
	if err != nil || res.Created != 0 || res.Updated != 0 || res.Skipped != 5 {
		t.Fatalf("reimportar: %+v %v", res, err)
	}
	// Si la persona cambió de nombre, se actualiza la identidad.
	e.adapter["wa-1"].persons[0].Name = "Ana María"
	res, _ = e.svc.ImportFromAccount(ctx, "wa-1")
	if res.Updated != 1 || res.Created != 0 {
		t.Fatalf("cambio de nombre: %+v", res)
	}

	// La misma persona en Telegram (mismo teléfono) se une al contacto de WhatsApp.
	e.adapter["tg-1"].persons = []model.Person{{NativeID: "user:5", Name: "Ana T", Phone: "+593981112233", Username: "ana"}}
	res, err = e.svc.ImportFromAccount(ctx, "tg-1")
	if err != nil || res != (ImportResult{Updated: 1}) {
		t.Fatalf("telegram: %+v %v", res, err)
	}
	ana, _ := e.app.FindContactByIdentity(ctx, "tg-1", "user:5")
	if len(ana.Identities) != 2 {
		t.Fatalf("Ana: %+v", ana)
	}

	// Errores.
	e.adapter["tg-1"].err = port.ErrUnsupported
	if _, err := e.svc.ImportFromAccount(ctx, "tg-1"); !ctIsErr(err, port.ErrUnsupported) {
		t.Fatalf("no admitido: %v", err)
	}
	if _, err := e.svc.ImportFromAccount(ctx, "xx-1"); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
}

func TestMerge(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	l1, _ := e.svc.SaveList(ctx, model.ContactList{Name: "Clientes"})
	l2, _ := e.svc.SaveList(ctx, model.ContactList{Name: "Promo"})
	keep := e.mustSave(t, model.Contact{
		Name: "Ana", Phone: "0981112233", Notes: "Prefiere tarde", Tags: []string{"vip"}, ListIDs: []string{l1.ID},
		Identities: []model.Identity{ctIdent("wa-1", "a@s", "Ana", "+593981112233")},
	})
	gone := e.mustSave(t, model.Contact{
		Name: "Ana T", Notes: "Paga al contado", Tags: []string{"VIP", "nueva"}, ListIDs: []string{l1.ID, l2.ID}, OptedOut: true,
		Identities: []model.Identity{ctIdent("tg-1", "user:5", "Ana T", ""), ctIdent("wa-1", "a@s", "Ana", "+593981112233")},
	})
	// `gone` guardó la identidad wa-1|a@s y se la quitó a `keep`: ponérsela de nuevo no es lo que se prueba;
	// se vuelve a asignar para que cada uno tenga la suya.
	keep = e.mustSave(t, model.Contact{ID: keep.ID, Name: keep.Name, Phone: keep.Phone, Notes: keep.Notes, Tags: keep.Tags,
		ListIDs: []string{l1.ID}, Identities: []model.Identity{ctIdent("wa-1", "a@s", "Ana", "+593981112233")}})
	gone, _ = e.svc.Get(ctx, gone.ID)

	merged, err := e.svc.Merge(ctx, keep.ID, gone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != keep.ID || merged.Name != "Ana" || merged.Phone != "+593981112233" || !merged.OptedOut {
		t.Fatalf("resultado: %+v", merged)
	}
	if len(merged.Identities) != 2 {
		t.Fatalf("identidades: %+v", merged.Identities)
	}
	if len(merged.Tags) != 2 || merged.Tags[0] != "vip" || merged.Tags[1] != "nueva" {
		t.Fatalf("etiquetas: %v", merged.Tags)
	}
	if len(merged.ListIDs) != 2 {
		t.Fatalf("listas: %v", merged.ListIDs)
	}
	if !strings.Contains(merged.Notes, "Prefiere tarde") || !strings.Contains(merged.Notes, "Paga al contado") {
		t.Fatalf("notas: %q", merged.Notes)
	}
	if _, err := e.svc.Get(ctx, gone.ID); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("el absorbido debe borrarse: %v", err)
	}
	if lists, _ := e.svc.Lists(ctx); len(lists) != 2 || lists[0].MemberCount != 1 || lists[1].MemberCount != 1 {
		t.Fatalf("miembros: %+v", lists)
	}

	// Errores y casos límite.
	if _, err := e.svc.Merge(ctx, keep.ID, keep.ID); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("consigo mismo: %v", err)
	}
	if _, err := e.svc.Merge(ctx, keep.ID, "nada"); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("absorbido inexistente: %v", err)
	}
	if _, err := e.svc.Merge(ctx, "nada", keep.ID); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("principal inexistente: %v", err)
	}
	if _, err := e.svc.Merge(ctx, "", keep.ID); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("vacío: %v", err)
	}
	// Si quien se conserva ya no recibe campañas y el otro sí, sigue sin recibirlas.
	a := e.mustSave(t, model.Contact{Name: "A", Phone: "0990000001", OptedOut: true})
	b := e.mustSave(t, model.Contact{Name: "B", Phone: "0990000002"})
	if m, err := e.svc.Merge(ctx, a.ID, b.ID); err != nil || !m.OptedOut {
		t.Fatalf("OptedOut conservado: %+v %v", m, err)
	}
	// Notas repetidas no se duplican.
	c1 := e.mustSave(t, model.Contact{Name: "C1", Phone: "0990000003", Notes: "igual"})
	c2 := e.mustSave(t, model.Contact{Name: "C2", Phone: "0990000004", Notes: "igual"})
	if m, _ := e.svc.Merge(ctx, c1.ID, c2.ID); m.Notes != "igual" {
		t.Fatalf("notas duplicadas: %q", m.Notes)
	}
}

func TestSplitIdentity(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	l, _ := e.svc.SaveList(ctx, model.ContactList{Name: "Clientes"})
	ct := e.mustSave(t, model.Contact{
		Name: "Ana", Phone: "+593981112233", OptedOut: true, ListIDs: []string{l.ID},
		Identities: []model.Identity{ctIdent("wa-1", "a@s", "Ana", "+593981112233"), ctIdent("tg-1", "user:5", "Otra Ana", "")},
	})
	fresh, err := e.svc.SplitIdentity(ctx, ct.ID, "tg-1", "user:5")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == ct.ID || fresh.Name != "Otra Ana" || !fresh.OptedOut || len(fresh.Identities) != 1 ||
		fresh.Identities[0].PeerNativeID != "user:5" || len(fresh.ListIDs) != 0 {
		t.Fatalf("nuevo: %+v", fresh)
	}
	orig, _ := e.svc.Get(ctx, ct.ID)
	if len(orig.Identities) != 1 || orig.Identities[0].PeerNativeID != "a@s" || len(orig.ListIDs) != 1 {
		t.Fatalf("original: %+v", orig)
	}
	if by, err := e.app.FindContactByIdentity(ctx, "tg-1", "user:5"); err != nil || by.ID != fresh.ID {
		t.Fatalf("la identidad debe ser del nuevo: %+v %v", by, err)
	}
	if _, err := e.svc.SplitIdentity(ctx, ct.ID, "wa-1", "a@s"); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("única identidad: %v", err)
	}
	if _, err := e.svc.SplitIdentity(ctx, ct.ID, "wa-1", "otra@s"); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("identidad ajena: %v", err)
	}
	if _, err := e.svc.SplitIdentity(ctx, "nada", "wa-1", "a@s"); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("contacto desconocido: %v", err)
	}
	if _, err := e.svc.SplitIdentity(ctx, ct.ID, "", ""); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("vacío: %v", err)
	}
}

func TestSuggestMerges(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	if pairs, err := e.svc.SuggestMerges(ctx); err != nil || pairs == nil || len(pairs) != 0 {
		t.Fatalf("vacío: %#v %v", pairs, err)
	}
	old := e.mustSave(t, model.Contact{Name: "Zoe", Phone: "+593981112233"}) // el más antiguo, aunque su nombre va al final
	byIdentity := e.mustSave(t, model.Contact{Name: "Ana", Identities: []model.Identity{ctIdent("tg-1", "user:5", "Ana", "0981112233")}})
	third := e.mustSave(t, model.Contact{Name: "Beto", Phone: "593981112233"})
	e.mustSave(t, model.Contact{Name: "Otro", Phone: "0990000009"})
	e.mustSave(t, model.Contact{Name: "Sin teléfono"})

	pairs, err := e.svc.SuggestMerges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]string]bool{{old.ID, byIdentity.ID}: true, {old.ID, third.ID}: true}
	if len(pairs) != 2 || !want[pairs[0]] || !want[pairs[1]] || pairs[0] == pairs[1] {
		t.Fatalf("pares: %v (quería %v)", pairs, want)
	}
	for _, p := range pairs {
		if p[0] != old.ID {
			t.Fatalf("se conserva el más antiguo: %v", p)
		}
	}
	// Un mismo par por dos teléfonos distintos sale una sola vez.
	x := e.mustSave(t, model.Contact{Name: "X", Phone: "0991110000", Identities: []model.Identity{ctIdent("wa-1", "x@s", "X", "0991110001")}})
	y := e.mustSave(t, model.Contact{Name: "Y", Phone: "0991110001", Identities: []model.Identity{ctIdent("wa-1", "y@s", "Y", "0991110000")}})
	n := 0
	pairs, _ = e.svc.SuggestMerges(ctx)
	for _, p := range pairs {
		if p == [2]string{x.ID, y.ID} {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("par repetido %d veces: %v", n, pairs)
	}
}

func TestSuggestMergesPaginates(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	for i := 0; i < contactsPage+30; i++ {
		_ = e.app.SaveContact(ctx, model.Contact{
			ID: model.NewID(), Name: fmt.Sprintf("c%04d", i), Phone: fmt.Sprintf("+59391111%05d", i), CreatedAt: int64(i + 1),
		})
	}
	// El último comparte teléfono con el primero.
	_ = e.app.SaveContact(ctx, model.Contact{ID: "zz", Name: "zz", Phone: "+5939111100000", CreatedAt: 99999})
	pairs, err := e.svc.SuggestMerges(ctx)
	if err != nil || len(pairs) != 1 || pairs[0][1] != "zz" {
		t.Fatalf("pares: %v %v", pairs, err)
	}
}

func TestLists(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	if ls, err := e.svc.Lists(ctx); err != nil || ls == nil || len(ls) != 0 {
		t.Fatalf("vacío: %#v %v", ls, err)
	}
	l, err := e.svc.SaveList(ctx, model.ContactList{Name: "  Clientes "})
	if err != nil || l.ID == "" || l.Name != "Clientes" || l.Color == "" || l.MemberCount != 0 {
		t.Fatalf("crear: %+v %v", l, err)
	}
	renamed, err := e.svc.SaveList(ctx, model.ContactList{ID: l.ID, Name: "Mayoristas", Color: "#ff0000"})
	if err != nil || renamed.ID != l.ID || renamed.Name != "Mayoristas" || renamed.Color != "#ff0000" {
		t.Fatalf("renombrar: %+v %v", renamed, err)
	}
	if _, err := e.svc.SaveList(ctx, model.ContactList{Name: "mayoristas"}); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("nombre repetido: %v", err)
	}
	if _, err := e.svc.SaveList(ctx, model.ContactList{Name: " "}); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("sin nombre: %v", err)
	}
	if _, err := e.svc.SaveList(ctx, model.ContactList{ID: "nada", Name: "X"}); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("lista desconocida: %v", err)
	}

	a := e.mustSave(t, model.Contact{Name: "Ana", Phone: "0981112233"})
	b := e.mustSave(t, model.Contact{Name: "Beto", Phone: "0982223344"})
	if err := e.svc.SetListMembers(ctx, l.ID, []string{a.ID, b.ID, a.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if ls, _ := e.svc.Lists(ctx); ls[0].MemberCount != 2 {
		t.Fatalf("miembros: %+v", ls)
	}
	if err := e.svc.SetListMembers(ctx, l.ID, nil, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if out, total, _ := e.svc.List(ctx, model.ContactFilter{ListID: l.ID}); total != 1 || out[0].ID != b.ID {
		t.Fatalf("por lista: %+v", out)
	}
	if got, _ := e.svc.Get(ctx, b.ID); len(got.ListIDs) != 1 {
		t.Fatalf("ListIDs del contacto: %+v", got)
	}
	if err := e.svc.SetListMembers(ctx, "nada", []string{a.ID}, nil); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("lista desconocida: %v", err)
	}
	if err := e.svc.SetListMembers(ctx, "", nil, nil); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("sin lista: %v", err)
	}
	if err := e.svc.DeleteList(ctx, l.ID); err != nil {
		t.Fatal(err)
	}
	if ls, _ := e.svc.Lists(ctx); len(ls) != 0 {
		t.Fatalf("borrada: %+v", ls)
	}
	if err := e.svc.DeleteList(ctx, ""); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("DeleteList vacío: %v", err)
	}
}

// ---------------------------------------------------------------------------------------------
// CSV

func ctWriteCSV(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "contactos.csv")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportCSVSemicolonWithBOMAndHeader(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	l, _ := e.svc.SaveList(ctx, model.ContactList{Name: "Importados"})
	path := ctWriteCSV(t, "\xef\xbb\xbfNombre;Teléfono;Etiquetas;Notas\r\n"+
		"Ana;0981112233;vip, mayorista;\"Llama de tarde; no escribir\"\r\n"+
		"Beto;+593 99 111 2233;;\r\n"+
		"Sin teléfono;;;\r\n"+
		"Corto;123;;\r\n"+
		"Ana otra vez;098 111 2233;nueva;otra nota\r\n")
	res, err := e.svc.ImportCSV(ctx, path, CSVMapping{Name: 0, Phone: 1, Tags: 2, Notes: 3, ListID: l.ID, HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if res != (ImportResult{Created: 2, Updated: 1, Skipped: 2}) {
		t.Fatalf("resultado: %+v", res)
	}
	found, _ := e.app.FindContactsByPhone(ctx, "+593981112233")
	if len(found) != 1 {
		t.Fatalf("Ana: %+v", found)
	}
	ana := found[0]
	if ana.Name != "Ana" || len(ana.Tags) != 3 || !strings.Contains(ana.Notes, "no escribir") || !strings.Contains(ana.Notes, "otra nota") {
		t.Fatalf("Ana: %+v", ana)
	}
	if got, _ := e.app.FindContactsByPhone(ctx, "+593991112233"); len(got) != 1 || got[0].Name != "Beto" {
		t.Fatalf("Beto: %+v", got)
	}
	if ls, _ := e.svc.Lists(ctx); ls[0].MemberCount != 2 {
		t.Fatalf("lista: %+v", ls)
	}

	// Volver a importar el mismo archivo no cambia nada: todo cuenta como omitido.
	res, err = e.svc.ImportCSV(ctx, path, CSVMapping{Name: 0, Phone: 1, Tags: 2, Notes: 3, ListID: l.ID, HasHeader: true})
	if err != nil || res.Created != 0 || res.Updated != 0 || res.Skipped != 5 {
		t.Fatalf("reimportar: %+v %v", res, err)
	}
	if _, total, _ := e.svc.List(ctx, model.ContactFilter{}); total != 2 {
		t.Fatalf("contactos: %d", total)
	}
}

func TestImportCSVCommaWithoutHeaderAndMissingColumns(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	path := ctWriteCSV(t, "0981112233,Ana\n\"0982223344\",\"Beto, el grande\"\n")
	// Solo teléfono y nombre; etiquetas y notas con -1.
	res, err := e.svc.ImportCSV(ctx, path, CSVMapping{Name: 1, Phone: 0, Tags: -1, Notes: -1})
	if err != nil || res.Created != 2 {
		t.Fatalf("resultado: %+v %v", res, err)
	}
	if got, _ := e.app.FindContactsByPhone(ctx, "+593982223344"); len(got) != 1 || got[0].Name != "Beto, el grande" {
		t.Fatalf("Beto: %+v", got)
	}
	// Sin columna de nombre: el nombre es el teléfono.
	path = ctWriteCSV(t, "0991112233\n")
	res, err = e.svc.ImportCSV(ctx, path, CSVMapping{Name: -1, Phone: 0, Tags: -1, Notes: -1})
	if err != nil || res.Created != 1 {
		t.Fatalf("sin nombre: %+v %v", res, err)
	}
	if got, _ := e.app.FindContactsByPhone(ctx, "+593991112233"); got[0].Name != "+593991112233" {
		t.Fatalf("nombre: %+v", got)
	}
	// Una fila más corta que el índice de las columnas no rompe nada.
	path = ctWriteCSV(t, "0971112233\n")
	res, err = e.svc.ImportCSV(ctx, path, CSVMapping{Name: 5, Phone: 0, Tags: 6, Notes: 7})
	if err != nil || res.Created != 1 {
		t.Fatalf("fila corta: %+v %v", res, err)
	}
}

func TestImportCSVLatin1(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	// «José» y «Peña» en Latin-1 (lo que guarda Excel en español).
	path := ctWriteCSV(t, "Nombre;Tel\r\nJos\xe9;0981112233\r\nPe\xf1a;0982223344\r\n")
	res, err := e.svc.ImportCSV(ctx, path, CSVMapping{Name: 0, Phone: 1, Tags: -1, Notes: -1, HasHeader: true})
	if err != nil || res.Created != 2 {
		t.Fatalf("resultado: %+v %v", res, err)
	}
	if got, _ := e.app.FindContactsByPhone(ctx, "+593981112233"); got[0].Name != "José" {
		t.Fatalf("José: %+v", got)
	}
	if got, _ := e.app.FindContactsByPhone(ctx, "+593982223344"); got[0].Name != "Peña" {
		t.Fatalf("Peña: %+v", got)
	}
}

func TestImportCSVErrors(t *testing.T) {
	e := newCtEnv(t)
	ctx := context.Background()
	ok := ctWriteCSV(t, "0981112233,Ana\n")
	if _, err := e.svc.ImportCSV(ctx, "", CSVMapping{Phone: 0}); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("sin ruta: %v", err)
	}
	if _, err := e.svc.ImportCSV(ctx, ok, CSVMapping{Name: 1, Phone: -1}); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("sin columna de teléfono: %v", err)
	}
	if _, err := e.svc.ImportCSV(ctx, filepath.Join(t.TempDir(), "no-existe.csv"), CSVMapping{Phone: 0}); !ctIsErr(err, port.ErrInvalid) {
		t.Fatalf("archivo inexistente: %v", err)
	}
	if _, err := e.svc.ImportCSV(ctx, ok, CSVMapping{Name: 1, Phone: 0, Tags: -1, Notes: -1, ListID: "nada"}); !ctIsErr(err, port.ErrNotFound) {
		t.Fatalf("lista inexistente: %v", err)
	}
	if _, total, _ := e.svc.List(ctx, model.ContactFilter{}); total != 0 {
		t.Fatalf("no debía importar nada: %d", total)
	}
	res, err := e.svc.ImportCSV(ctx, ctWriteCSV(t, ""), CSVMapping{Phone: 0, Name: -1, Tags: -1, Notes: -1})
	if err != nil || res != (ImportResult{}) {
		t.Fatalf("vacío: %+v %v", res, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.svc.ImportCSV(cctx, ok, CSVMapping{Name: 1, Phone: 0, Tags: -1, Notes: -1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelado: %v", err)
	}
}

func TestDetectDelimiter(t *testing.T) {
	cases := map[string]rune{
		"a;b;c\n1;2;3": ';', "a,b,c\n1,2,3": ',', "a;b,c": ',', "a,b;c;d": ';', "solo": ',', "": ',',
		"a;b\r\n1,2,3,4": ';',
	}
	for in, want := range cases {
		if got := detectDelimiter(in); got != want {
			t.Errorf("%q: %q, quería %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Ajustes y listados que necesitan los contactos automáticos

func (a *ctApp) GetSetting(ctx context.Context, key string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings[key], nil
}

func (a *ctApp) SetSetting(ctx context.Context, key, value string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings == nil {
		a.settings = map[string]string{}
	}
	a.settings[key] = value
	return nil
}

func (s *ctStore) ListPersons(ctx context.Context, query string, limit int) ([]model.Person, error) {
	out := make([]model.Person, 0, len(s.persons))
	for _, p := range s.persons {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NativeID < out[j].NativeID })
	return out, nil
}

func (s *ctStore) ListChats(ctx context.Context, f model.ChatFilter) ([]model.Chat, error) {
	out := []model.Chat{}
	for _, c := range s.chats {
		if (f.Tab == "archived") == c.Archived {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NativeID < out[j].NativeID })
	return out, nil
}
