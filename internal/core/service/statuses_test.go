package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/fake"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/stats"
	"dconnector/internal/paths"
)

// ---------------------------------------------------------------------------------------------
// Dobles de prueba de estados (el almacén en memoria de testutil_test.go no lista estados).

// stStore envuelve el almacén en memoria y sí implementa las consultas de estados, leyendo los
// mismos mapas que llena el Router con UpsertStatus / UpsertStatusView.
type stStore struct{ *memAccountStore }

func (s *stStore) ListStatusAuthors(ctx context.Context, now int64) ([]model.StatusAuthor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	by := map[string]*model.StatusAuthor{}
	for _, st := range s.statuses {
		if st.FromMe || st.DeletedAt > 0 || st.ExpiresAt <= now {
			continue
		}
		a := by[st.AuthorNativeID]
		if a == nil {
			a = &model.StatusAuthor{AccountID: s.id, AuthorID: st.AuthorNativeID, AuthorName: st.AuthorName}
			by[st.AuthorNativeID] = a
		}
		a.Count++
		if !st.Viewed {
			a.Unviewed++
		}
		if st.PostedAt > a.LastAt {
			a.LastAt = st.PostedAt
		}
	}
	out := make([]model.StatusAuthor, 0, len(by))
	for _, a := range by {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AuthorID < out[j].AuthorID })
	return out, nil
}

func (s *stStore) ListStatuses(ctx context.Context, authorID string, now int64) ([]model.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Status
	for _, st := range s.statuses {
		if !st.FromMe && st.AuthorNativeID == authorID && st.DeletedAt == 0 && st.ExpiresAt > now {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PostedAt < out[j].PostedAt })
	return out, nil
}

func (s *stStore) ListMyStatuses(ctx context.Context, from, to int64) ([]model.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Status
	for _, st := range s.statuses {
		if st.FromMe && st.PostedAt >= from && st.PostedAt < to {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PostedAt > out[j].PostedAt })
	return out, nil
}

func (s *stStore) ListStatusViews(ctx context.Context, statusKey string) ([]model.StatusView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.StatusView
	for _, v := range s.views {
		if v.StatusKey == statusKey {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ViewerID < out[j].ViewerID })
	return out, nil
}

func (s *stStore) status(key string) (model.Status, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.statuses[key]
	return st, ok
}

func (s *stStore) viewCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.views)
}

func (s *stStore) view(statusKey, viewer string) (model.StatusView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.views[statusKey+"|"+viewer]
	return v, ok
}

// stAdapter añade a testAdapter lo que hace falta para probar estados.
type stAdapter struct {
	*testAdapter
	smu         sync.Mutex
	posted      []model.OutgoingStatus
	postErr     error
	markErr     error
	viewers     map[string][]model.StatusView
	viewersErr  error
	viewerCalls int
}

func (a *stAdapter) PostStatus(ctx context.Context, s model.OutgoingStatus) (model.Status, error) {
	a.smu.Lock()
	err := a.postErr
	if err == nil {
		a.posted = append(a.posted, s)
	}
	a.smu.Unlock()
	if err != nil {
		return model.Status{}, err
	}
	return a.testAdapter.PostStatus(ctx, s)
}

func (a *stAdapter) MarkStatusViewed(ctx context.Context, st model.Status) error {
	a.smu.Lock()
	err := a.markErr
	a.smu.Unlock()
	if err != nil {
		return err
	}
	return a.testAdapter.MarkStatusViewed(ctx, st)
}

func (a *stAdapter) StatusViewers(ctx context.Context, native string) ([]model.StatusView, error) {
	a.smu.Lock()
	a.viewerCalls++
	err := a.viewersErr
	v := append([]model.StatusView(nil), a.viewers[native]...)
	a.smu.Unlock()
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (a *stAdapter) setViewers(native string, v []model.StatusView) {
	a.smu.Lock()
	if a.viewers == nil {
		a.viewers = map[string][]model.StatusView{}
	}
	a.viewers[native] = v
	a.smu.Unlock()
}

func (a *stAdapter) calls() int {
	a.smu.Lock()
	defer a.smu.Unlock()
	return a.viewerCalls
}

func (a *stAdapter) lastPosted() (model.OutgoingStatus, bool) {
	a.smu.Lock()
	defer a.smu.Unlock()
	if len(a.posted) == 0 {
		return model.OutgoingStatus{}, false
	}
	return a.posted[len(a.posted)-1], true
}

var _ port.ChannelAdapter = (*stAdapter)(nil)

// listApp: AppStore con listas de contactos (el de testutil_test.go devuelve vacío).
type listApp struct {
	*memApp
	members map[string][]model.Contact // listId → miembros
}

func (a *listApp) ListMembers(ctx context.Context, listIDs []string) ([]model.Contact, error) {
	seen := map[string]bool{}
	var out []model.Contact
	for _, id := range listIDs {
		for _, c := range a.members[id] {
			if !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c)
			}
		}
	}
	return out, nil
}

type stEnv struct {
	*testEnv
	st  *Statuses
	app *listApp
}

// wrap sustituye almacén y adaptador de una cuenta por los dobles de estados.
func (e *stEnv) wrap(id model.AccountID) (*stStore, *stAdapter) {
	e.t.Helper()
	inst, err := e.accounts.mustGet(id)
	if err != nil {
		e.t.Fatal(err)
	}
	ss := &stStore{memAccountStore: e.store(id)}
	ad := &stAdapter{testAdapter: e.adapter(id)}
	inst.mu.Lock()
	inst.store, inst.adapter = ss, ad
	inst.mu.Unlock()
	return ss, ad
}

func newStatusEnv(t *testing.T) *stEnv {
	t.Helper()
	e := newTestEnv(t)
	app := &listApp{memApp: e.app, members: map[string][]model.Contact{}}
	st := NewStatuses(e.accounts, e.router, app)
	t.Cleanup(st.Stop) // corre antes que la limpieza del entorno
	return &stEnv{testEnv: e, st: st, app: app}
}

// account crea una cuenta conectada con los dobles ya puestos.
func (e *stEnv) account(ch model.Channel) (model.Account, *stStore, *stAdapter) {
	acc := e.newAccount(ch, "")
	ss, ad := e.wrap(acc.ID)
	return acc, ss, ad
}

func nowMs() int64 { return time.Now().UnixMilli() }

func seedStatus(t *testing.T, ss *stStore, acc model.AccountID, native, author string, fromMe bool, postedAt int64) model.Status {
	t.Helper()
	st := model.Status{
		Key: string(acc) + "|" + native, AccountID: acc, NativeID: native, AuthorNativeID: author, AuthorName: "Persona " + author,
		FromMe: fromMe, PostedAt: postedAt, ExpiresAt: postedAt + 24*3600*1000, Kind: model.KindText, Text: "estado " + native,
		Delivery: model.DeliverySent,
	}
	if !fromMe {
		st.ExpiresAt = nowMs() + 3600*1000 // los ajenos siguen vigentes
	}
	if err := ss.UpsertStatus(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return st
}

func isCall(a *stAdapter, call string) bool { return hasCall(a.testAdapter, call) }

// ---------------------------------------------------------------------------------------------
// Publicar

func TestStatusesPostText(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, ss, ad := e.account(model.ChannelWhatsApp)

	from := e.mark()
	got, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{Kind: model.KindText, Text: "Hola a todos", BgColor: "#25D366", Font: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.NativeID == "" || got.Key != string(acc.ID)+"|"+got.NativeID || got.AccountID != acc.ID || !got.FromMe ||
		got.Kind != model.KindText || got.Text != "Hola a todos" || got.BgColor != "#25D366" || got.Font != 2 {
		t.Fatalf("estado devuelto: %+v", got)
	}
	if got.PostedAt == 0 || got.ExpiresAt <= got.PostedAt || got.Delivery != model.DeliverySent {
		t.Fatalf("fechas o entrega sin completar: %+v", got)
	}
	out, ok := ad.lastPosted()
	if !ok || out.Audience.Mode != "contacts" || len(out.Audience.PeerIDs) != 0 {
		t.Fatalf("el adaptador recibió: %+v", out)
	}
	ev := e.waitEvent(from, "status.posted", ofType(model.EvStatusUpsert))
	if ev.Status == nil || ev.Status.Key != got.Key || ev.AccountID != acc.ID {
		t.Fatalf("evento: %+v", ev.Status)
	}
	eventually(t, "estado guardado", func() bool { _, ok := ss.status(got.Key); return ok })
}

func TestStatusesPostMediaCopiesFile(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, _, ad := e.account(model.ChannelWhatsApp)

	src := filepath.Join(t.TempDir(), "foto.JPG")
	if err := os.WriteFile(src, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{Kind: model.KindImage, Text: "pie", FilePath: src})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := ad.lastPosted()
	if out.FilePath == src || filepath.Dir(out.FilePath) != paths.StatusMediaDir() {
		t.Fatalf("el adaptador debe recibir la copia en status-media: %s", out.FilePath)
	}
	if out.Mime != "image/jpeg" {
		t.Fatalf("mime: %q", out.Mime)
	}
	if b, err := os.ReadFile(out.FilePath); err != nil || string(b) != "0123456789" {
		t.Fatalf("copia: %q %v", b, err)
	}
	if filepath.Ext(out.FilePath) != ".jpg" {
		t.Fatalf("extensión: %s", out.FilePath)
	}
	if got.Media == nil || got.Media.LocalPath != out.FilePath || got.Media.Size != 10 || got.Media.Mime != "image/jpeg" {
		t.Fatalf("medio del estado: %+v", got.Media)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("el original debe quedar: %v", err)
	}

	// Si el canal falla, la copia se borra.
	ad.smu.Lock()
	ad.postErr = errors.New("sin red")
	ad.smu.Unlock()
	before, _ := os.ReadDir(paths.StatusMediaDir())
	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{Kind: model.KindVideo, FilePath: src}); err == nil {
		t.Fatal("esperaba el error del canal")
	}
	after, _ := os.ReadDir(paths.StatusMediaDir())
	if len(after) != len(before) {
		t.Fatalf("quedó una copia huérfana: %d → %d archivos", len(before), len(after))
	}
}

func TestStatusesPostValidation(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, _, ad := e.account(model.ChannelWhatsApp)
	file := filepath.Join(t.TempDir(), "v.mp4")
	if err := os.WriteFile(file, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	caps := fake.FullCapabilities()
	set := func(f func(c *capability.Set)) {
		c := caps
		f(&c)
		ad.SetCapabilities(c)
	}

	if _, err := e.st.Post(ctx, "wa-nope", model.OutgoingStatus{Kind: model.KindText, Text: "x"}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
	for name, st := range map[string]model.OutgoingStatus{
		"texto vacío":          {Kind: model.KindText, Text: "  "},
		"tipo no válido":       {Kind: model.KindAudio, FilePath: file},
		"sin archivo":          {Kind: model.KindImage},
		"archivo falso":        {Kind: model.KindVideo, FilePath: file + ".no"},
		"modo raro":            {Kind: model.KindText, Text: "x", Audience: model.StatusAudience{Mode: "todos"}},
		"allow sin nadie":      {Kind: model.KindText, Text: "x", Audience: model.StatusAudience{Mode: "allow"}},
		"personas en contacts": {Kind: model.KindText, Text: "x", Audience: model.StatusAudience{Mode: "contacts", PeerIDs: []string{"a"}}},
	} {
		if _, err := e.st.Post(ctx, acc.ID, st); !errors.Is(err, port.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}

	set(func(c *capability.Set) { c.MaxTextLen = 5 })
	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{Kind: model.KindText, Text: "demasiado largo"}); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("texto largo: %v", err)
	}
	set(func(c *capability.Set) { c.MaxVideoSize = 10 })
	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{Kind: model.KindVideo, FilePath: file}); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("archivo grande: %v", err)
	}
	if entries, _ := os.ReadDir(paths.StatusMediaDir()); len(entries) != 0 {
		t.Fatalf("no debe copiarse un archivo rechazado: %d", len(entries))
	}
	set(func(c *capability.Set) { c.StatusAudience = false })
	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{Kind: model.KindText, Text: "x", Audience: model.StatusAudience{Mode: "deny", PeerIDs: []string{"a"}}}); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("audiencia sin capacidad: %v", err)
	}
	// La audiencia por defecto sí se puede aunque el canal no deje elegir.
	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{Kind: model.KindText, Text: "ok"}); err != nil {
		t.Fatalf("audiencia por defecto: %v", err)
	}
	set(func(c *capability.Set) { c.PostStatus = false })
	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{Kind: model.KindText, Text: "x"}); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("sin PostStatus: %v", err)
	}
}

func TestStatusesPostAudienceFromLists(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, _, ad := e.account(model.ChannelWhatsApp)
	other := e.newAccount(model.ChannelTelegram, "")

	e.app.members["vip"] = []model.Contact{
		{ID: "c1", Identities: []model.Identity{
			{AccountID: acc.ID, PeerNativeID: "593111"},
			{AccountID: other.ID, PeerNativeID: "tg-1"}, // de otra cuenta: no cuenta
		}},
		{ID: "c2", Identities: []model.Identity{{AccountID: acc.ID, PeerNativeID: "593222"}}},
	}
	e.app.members["amigos"] = []model.Contact{
		{ID: "c2", Identities: []model.Identity{{AccountID: acc.ID, PeerNativeID: "593222"}}}, // repetido
		{ID: "c3", Identities: []model.Identity{{AccountID: acc.ID, PeerNativeID: "593333"}}},
		{ID: "c4", Identities: []model.Identity{{AccountID: other.ID, PeerNativeID: "tg-4"}}}, // sin identidad aquí
	}
	e.app.members["solo-otra"] = []model.Contact{{ID: "c5", Identities: []model.Identity{{AccountID: other.ID, PeerNativeID: "tg-5"}}}}

	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{
		Kind: model.KindText, Text: "solo vip",
		Audience: model.StatusAudience{Mode: "contacts", ListIDs: []string{"vip", "amigos"}, PeerIDs: []string{"593999", "593111"}},
	}); err != nil {
		t.Fatal(err)
	}
	out, _ := ad.lastPosted()
	want := model.StatusAudience{Mode: "allow", PeerIDs: []string{"593999", "593111", "593222", "593333"}, ListIDs: []string{}}
	if !reflect.DeepEqual(out.Audience, want) {
		t.Fatalf("audiencia:\n got %+v\nwant %+v", out.Audience, want)
	}

	// «Excepto»: se mantiene deny (no se invierte la privacidad).
	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{
		Kind: model.KindText, Text: "excepto", Audience: model.StatusAudience{Mode: "deny", ListIDs: []string{"amigos"}},
	}); err != nil {
		t.Fatal(err)
	}
	out, _ = ad.lastPosted()
	if out.Audience.Mode != "deny" || !reflect.DeepEqual(out.Audience.PeerIDs, []string{"593222", "593333"}) {
		t.Fatalf("deny: %+v", out.Audience)
	}

	// Listas sin nadie en esta cuenta: error (no se publica para todos por accidente).
	if _, err := e.st.Post(ctx, acc.ID, model.OutgoingStatus{
		Kind: model.KindText, Text: "x", Audience: model.StatusAudience{ListIDs: []string{"solo-otra"}},
	}); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("lista sin miembros aquí: %v", err)
	}
}

// ---------------------------------------------------------------------------------------------
// Consultas

func TestStatusesAuthorsByAuthorMine(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	a1, s1, _ := e.account(model.ChannelWhatsApp)
	a2, s2, _ := e.account(model.ChannelTelegram)
	now := nowMs()

	seedStatus(t, s1, a1.ID, "n1", "ana", false, now-5000)
	seedStatus(t, s1, a1.ID, "n2", "ana", false, now-1000)
	seedStatus(t, s2, a2.ID, "n3", "beto", false, now-3000)
	seedStatus(t, s1, a1.ID, "m1", "me", true, now-10_000)
	seedStatus(t, s1, a1.ID, "m2", "me", true, now-100_000)

	all, err := e.st.Authors(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].AuthorID != "ana" || all[0].Count != 2 || all[0].Unviewed != 2 || all[0].AccountID != a1.ID ||
		all[1].AuthorID != "beto" || all[1].AccountID != a2.ID {
		t.Fatalf("autores: %+v", all)
	}
	one, err := e.st.Authors(ctx, []model.AccountID{a2.ID, a2.ID})
	if err != nil || len(one) != 1 || one[0].AuthorID != "beto" {
		t.Fatalf("autores de una cuenta: %+v %v", one, err)
	}
	if _, err := e.st.Authors(ctx, []model.AccountID{"wa-nope"}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}

	list, err := e.st.ByAuthor(ctx, a1.ID, "ana")
	if err != nil || len(list) != 2 || list[0].NativeID != "n1" || list[1].NativeID != "n2" {
		t.Fatalf("de una persona: %+v %v", list, err)
	}
	if _, err := e.st.ByAuthor(ctx, a1.ID, ""); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("sin persona: %v", err)
	}

	mine, err := e.st.Mine(ctx, a1.ID, 0, 0)
	if err != nil || len(mine) != 2 || mine[0].NativeID != "m1" {
		t.Fatalf("míos: %+v %v", mine, err)
	}
	mine, err = e.st.Mine(ctx, a1.ID, now-50_000, 0)
	if err != nil || len(mine) != 1 || mine[0].NativeID != "m1" {
		t.Fatalf("míos desde: %+v %v", mine, err)
	}
}

// ---------------------------------------------------------------------------------------------
// Acciones

func TestStatusesMarkViewed(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, ss, ad := e.account(model.ChannelWhatsApp)
	other := seedStatus(t, ss, acc.ID, "o1", "ana", false, nowMs()-1000)
	mine := seedStatus(t, ss, acc.ID, "m1", "me", true, nowMs()-1000)

	from := e.mark()
	if err := e.st.MarkViewed(ctx, acc.ID, other.Key); err != nil {
		t.Fatal(err)
	}
	if !isCall(ad, "MarkStatusViewed:o1") {
		t.Fatalf("el adaptador no fue avisado: %v", ad.Calls())
	}
	ev := e.waitEvent(from, "status.posted con Viewed", func(ev model.Event) bool {
		return ev.Type == model.EvStatusUpsert && ev.Status != nil && ev.Status.NativeID == "o1" && ev.Status.Viewed
	})
	if ev.Status.Text != other.Text {
		t.Fatalf("el estado publicado perdió datos: %+v", ev.Status)
	}
	eventually(t, "Viewed guardado", func() bool { st, _ := ss.status(other.Key); return st.Viewed })

	// Repetir no vuelve a llamar al adaptador.
	calls := len(ad.Calls())
	if err := e.st.MarkViewed(ctx, acc.ID, other.Key); err != nil {
		t.Fatal(err)
	}
	if len(ad.Calls()) != calls {
		t.Fatal("un estado ya visto no debe avisar otra vez")
	}

	// Solo ajenos, clave coherente, existente.
	if err := e.st.MarkViewed(ctx, acc.ID, mine.Key); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("propio: %v", err)
	}
	if err := e.st.MarkViewed(ctx, acc.ID, "otra-cuenta|o1"); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("cuenta distinta: %v", err)
	}
	if err := e.st.MarkViewed(ctx, acc.ID, "basura"); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("clave rota: %v", err)
	}
	if err := e.st.MarkViewed(ctx, acc.ID, string(acc.ID)+"|nope"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}

	// El canal no admite avisar: se marca igualmente en local; otro error no marca.
	o2 := seedStatus(t, ss, acc.ID, "o2", "ana", false, nowMs())
	ad.smu.Lock()
	ad.markErr = port.ErrUnsupported
	ad.smu.Unlock()
	if err := e.st.MarkViewed(ctx, acc.ID, o2.Key); err != nil {
		t.Fatalf("ErrUnsupported debe tolerarse: %v", err)
	}
	eventually(t, "o2 visto en local", func() bool { st, _ := ss.status(o2.Key); return st.Viewed })
	o3 := seedStatus(t, ss, acc.ID, "o3", "ana", false, nowMs())
	ad.smu.Lock()
	ad.markErr = port.ErrNotConnected
	ad.smu.Unlock()
	if err := e.st.MarkViewed(ctx, acc.ID, o3.Key); !errors.Is(err, port.ErrNotConnected) {
		t.Fatalf("sin conexión: %v", err)
	}
	if st, _ := ss.status(o3.Key); st.Viewed {
		t.Fatal("no debe marcarse si el canal falló")
	}
}

func TestStatusesReact(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, ss, ad := e.account(model.ChannelWhatsApp)
	other := seedStatus(t, ss, acc.ID, "o1", "ana", false, nowMs()-1000)
	mine := seedStatus(t, ss, acc.ID, "m1", "me", true, nowMs()-1000)

	if err := e.st.React(ctx, acc.ID, other.Key, "🔥"); err != nil {
		t.Fatal(err)
	}
	if !isCall(ad, "ReactStatus:o1:🔥") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}
	if err := e.st.React(ctx, acc.ID, mine.Key, "🔥"); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("propio: %v", err)
	}
	if err := e.st.React(ctx, acc.ID, string(acc.ID)+"|nope", "🔥"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
	c := fake.FullCapabilities()
	c.StatusReact = false
	ad.SetCapabilities(c)
	if err := e.st.React(ctx, acc.ID, other.Key, "🔥"); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("sin capacidad: %v", err)
	}
}

func TestStatusesDelete(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, ss, ad := e.account(model.ChannelWhatsApp)
	mine := seedStatus(t, ss, acc.ID, "m1", "me", true, nowMs()-1000)

	from := e.mark()
	if err := e.st.Delete(ctx, acc.ID, "m1"); err != nil {
		t.Fatal(err)
	}
	if !isCall(ad, "DeleteStatus:m1") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}
	e.waitEvent(from, "status.posted borrado", func(ev model.Event) bool {
		return ev.Type == model.EvStatusUpsert && ev.Status != nil && ev.Status.NativeID == "m1" && ev.Status.DeletedAt > 0
	})
	eventually(t, "borrado guardado", func() bool { st, _ := ss.status(mine.Key); return st.DeletedAt > 0 })

	// Un id que no conocemos igualmente se pide al canal.
	if err := e.st.Delete(ctx, acc.ID, "desconocido"); err != nil {
		t.Fatal(err)
	}
	if !isCall(ad, "DeleteStatus:desconocido") {
		t.Fatalf("llamadas: %v", ad.Calls())
	}
	if err := e.st.Delete(ctx, acc.ID, ""); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("vacío: %v", err)
	}
}

// ---------------------------------------------------------------------------------------------
// Vistas

func TestStatusesViewers(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, ss, ad := e.account(model.ChannelWhatsApp)
	mine := seedStatus(t, ss, acc.ID, "m1", "me", true, nowMs()-1000)

	ad.setViewers("m1", []model.StatusView{
		{ViewerID: "ana", ViewerName: "Ana", At: 100, Reaction: "❤️"},
		{ViewerID: "beto", ViewerName: "Beto", At: 300},
		{ViewerID: "", At: 999}, // sin id: se ignora
	})
	from := e.mark()
	got, err := e.st.Viewers(ctx, acc.ID, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ViewerID != "beto" || got[1].ViewerID != "ana" || got[1].Reaction != "❤️" || got[0].StatusKey != mine.Key {
		t.Fatalf("vistas (la más reciente primero): %+v", got)
	}
	e.waitEvent(from, "status.viewed", ofType(model.EvStatusView))
	eventually(t, "vistas guardadas", func() bool { return ss.viewCount() == 2 })
	if v, ok := ss.view(mine.Key, "ana"); !ok || v.Reaction != "❤️" {
		t.Fatalf("vista guardada: %+v", v)
	}

	// Sin cambios no se vuelve a publicar nada; un cambio de reacción sí.
	from = e.mark()
	if _, err := e.st.Viewers(ctx, acc.ID, "m1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if n := len(filterType(e.eventsSince(from), model.EvStatusView)); n != 0 {
		t.Fatalf("se republicaron %d vistas sin cambios", n)
	}
	ad.setViewers("m1", []model.StatusView{{ViewerID: "beto", ViewerName: "Beto", At: 300, Reaction: "👍"}, {ViewerID: "caro", At: 500}})
	got, err = e.st.Viewers(ctx, acc.ID, "m1")
	if err != nil || len(got) != 3 || got[0].ViewerID != "caro" {
		t.Fatalf("vistas actualizadas: %+v %v", got, err)
	}
	eventually(t, "beto reaccionó", func() bool { v, _ := ss.view(mine.Key, "beto"); return v.Reaction == "👍" })
	eventually(t, "caro guardada", func() bool { _, ok := ss.view(mine.Key, "caro"); return ok })

	// Si el canal falla, se devuelve lo local sin error.
	ad.smu.Lock()
	ad.viewersErr = port.ErrNotConnected
	ad.smu.Unlock()
	got, err = e.st.Viewers(ctx, acc.ID, "m1")
	if err != nil || len(got) != 3 {
		t.Fatalf("respaldo local: %+v %v", got, err)
	}
	// Sin la capacidad no se pregunta al canal.
	calls := ad.calls()
	c := fake.FullCapabilities()
	c.StatusViewers = false
	ad.SetCapabilities(c)
	if got, err = e.st.Viewers(ctx, acc.ID, "m1"); err != nil || len(got) != 3 || ad.calls() != calls {
		t.Fatalf("sin capacidad: %+v %v (%d→%d llamadas)", got, err, calls, ad.calls())
	}
	if _, err := e.st.Viewers(ctx, acc.ID, ""); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("vacío: %v", err)
	}
}

func filterType(evs []model.Event, tp model.EventType) []model.Event {
	var out []model.Event
	for _, ev := range evs {
		if ev.Type == tp {
			out = append(out, ev)
		}
	}
	return out
}

func TestStatusesRefreshLoop(t *testing.T) {
	e := newStatusEnv(t)
	acc, ss, ad := e.account(model.ChannelWhatsApp)
	now := nowMs()
	recent := seedStatus(t, ss, acc.ID, "recent", "me", true, now-3600*1000)
	deleted := seedStatus(t, ss, acc.ID, "deleted", "me", true, now-3600*1000)
	deleted.DeletedAt = now
	_ = ss.UpsertStatus(context.Background(), deleted)
	seedStatus(t, ss, acc.ID, "old", "me", true, now-30*3600*1000) // fuera de las 24 h
	seedStatus(t, ss, acc.ID, "ajeno", "ana", false, now-1000)     // no es mío
	ad.setViewers("recent", []model.StatusView{{ViewerID: "ana", At: now}})
	ad.setViewers("deleted", []model.StatusView{{ViewerID: "x", At: now}})
	ad.setViewers("old", []model.StatusView{{ViewerID: "y", At: now}})

	e.st.refreshFirst, e.st.refreshEvery = 10*time.Millisecond, 20*time.Millisecond
	e.st.Start(context.Background())
	e.st.Start(context.Background()) // segunda llamada: no hace nada

	eventually(t, "vistas refrescadas", func() bool { _, ok := ss.view(recent.Key, "ana"); return ok })
	eventually(t, "varias pasadas", func() bool { return ad.calls() >= 3 })
	e.st.Stop()
	e.st.Stop() // segunda parada: no hace nada

	after := ad.calls()
	time.Sleep(80 * time.Millisecond)
	if ad.calls() != after {
		t.Fatal("el refresco sigue corriendo tras Stop")
	}
	if ss.viewCount() != 1 {
		t.Fatalf("solo debía refrescar mi estado reciente y vigente: %d vistas", ss.viewCount())
	}

	// Se puede volver a arrancar después de parar.
	e.st.Start(context.Background())
	eventually(t, "reanuda", func() bool { return ad.calls() > after })
	e.st.Stop()
}

func TestStatusesRefreshSkipsUnavailableAccounts(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, ss, ad := e.account(model.ChannelWhatsApp)
	seedStatus(t, ss, acc.ID, "m1", "me", true, nowMs()-1000)
	ad.setViewers("m1", []model.StatusView{{ViewerID: "ana", At: 1}})

	// Sin la capacidad: no se consulta.
	c := fake.FullCapabilities()
	c.StatusViewers = false
	ad.SetCapabilities(c)
	e.st.refreshViews(ctx)
	if ad.calls() != 0 {
		t.Fatal("no debe consultar sin StatusViewers")
	}
	// Con la capacidad pero desconectada: tampoco.
	ad.SetCapabilities(fake.FullCapabilities())
	if err := e.accounts.Disconnect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateDisconnected)
	e.st.refreshViews(ctx)
	if ad.calls() != 0 {
		t.Fatal("no debe consultar una cuenta desconectada")
	}
	// Un error del canal no detiene la pasada.
	if err := e.accounts.Connect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateConnected)
	ad.smu.Lock()
	ad.viewersErr = errors.New("fallo")
	ad.smu.Unlock()
	e.st.refreshViews(ctx)
	if ad.calls() != 1 {
		t.Fatalf("llamadas: %d", ad.calls())
	}
}

// ---------------------------------------------------------------------------------------------
// Estadísticas

func TestStatusesStats(t *testing.T) {
	e := newStatusEnv(t)
	ctx := context.Background()
	acc, ss, _ := e.account(model.ChannelWhatsApp)
	e.st.loc = time.FixedZone("EC", -5*3600)
	day := int64(24 * 3600 * 1000)
	now := e.st.now().UnixMilli()

	a := seedStatus(t, ss, acc.ID, "a", "me", true, now-1*day)
	b := seedStatus(t, ss, acc.ID, "b", "me", true, now-2*day)
	p := seedStatus(t, ss, acc.ID, "p", "me", true, now-9*day) // periodo anterior (7 a 14 días)
	seedStatus(t, ss, acc.ID, "viejo", "me", true, now-40*day) // fuera de ambos
	seedStatus(t, ss, acc.ID, "ajeno", "ana", false, now-1*day)
	for _, v := range []model.StatusView{
		{StatusKey: a.Key, ViewerID: "ana", At: a.PostedAt + 1000, Reaction: "❤️"},
		{StatusKey: a.Key, ViewerID: "beto", At: a.PostedAt + 2000},
		{StatusKey: b.Key, ViewerID: "ana", At: b.PostedAt + 1000},
		{StatusKey: p.Key, ViewerID: "ana", At: p.PostedAt + 1000},
	} {
		if err := ss.UpsertStatusView(ctx, v); err != nil {
			t.Fatal(err)
		}
	}

	got, err := e.st.Stats(ctx, acc.ID, stats.Range{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if c := got.Current; c.Posts != 2 || c.Views != 3 || c.Reach != 2 || c.Reactions != 1 || c.EngagedPeople != 1 {
		t.Fatalf("actual: %+v", c)
	}
	if p := got.Previous; p.Posts != 1 || p.Views != 1 || p.Reach != 1 {
		t.Fatalf("anterior: %+v", p)
	}
	if got.Changes.Posts == nil || *got.Changes.Posts != 1 {
		t.Fatalf("cambios: %+v", got.Changes)
	}
	if len(got.PerStatus) != 2 || got.PerStatus[0].Key != a.Key || len(got.PerDay) != 2 || got.Period.Days != 7 {
		t.Fatalf("detalle: %+v", got)
	}
	if _, err := e.st.Stats(ctx, "wa-nope", stats.Range{Days: 7}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
	// Un Range vacío vale 30 días: entran a, b y p, no el de hace 40.
	got, err = e.st.Stats(ctx, acc.ID, stats.Range{})
	if err != nil || got.Current.Posts != 3 || got.Period.Days != 30 {
		t.Fatalf("30 días por defecto: %+v %v", got.Current, err)
	}
}
