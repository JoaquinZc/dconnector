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

	"dconnector/internal/core/campaign"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

// ---------------------------------------------------------------------------------------------
// Dobles de prueba de campañas y programados

// campApp: AppStore con campañas, cola y programados en memoria. Se porta como el de SQLite: los
// totales de una campaña salen de su cola (Replied y OptOut no se guardan).
type campApp struct {
	*memApp
	cmu   sync.Mutex
	camps map[string]model.Campaign
	queue map[string][]model.QueueItem
	sched map[string]model.Scheduled
	seq   int
}

func newCampApp(base *memApp) *campApp {
	return &campApp{memApp: base, camps: map[string]model.Campaign{}, queue: map[string][]model.QueueItem{}, sched: map[string]model.Scheduled{}}
}

func (a *campApp) withTotals(c model.Campaign) model.Campaign {
	c.Totals = model.CampaignTotals{}
	for _, it := range a.queue[c.ID] {
		switch it.State {
		case "pending", "held":
			c.Totals.Queued++
		case "sent":
			c.Totals.Sent++
		case "failed":
			c.Totals.Failed++
		case "skipped":
			c.Totals.Skipped++
		}
	}
	return c
}

func (a *campApp) ListCampaigns(ctx context.Context) ([]model.Campaign, error) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	out := []model.Campaign{}
	for _, c := range a.camps {
		out = append(out, a.withTotals(c))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (a *campApp) GetCampaign(ctx context.Context, id string) (model.Campaign, error) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	c, ok := a.camps[id]
	if !ok {
		return model.Campaign{}, port.ErrNotFound
	}
	return a.withTotals(c), nil
}

func (a *campApp) SaveCampaign(ctx context.Context, c model.Campaign) error {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	if c.ID == "" {
		return port.ErrInvalid
	}
	c.Totals = model.CampaignTotals{}
	if c.Window.Weekdays == nil {
		c.Window.Weekdays = []int{}
	}
	a.camps[c.ID] = c
	return nil
}

func (a *campApp) DeleteCampaign(ctx context.Context, id string) error {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	delete(a.camps, id)
	delete(a.queue, id)
	return nil
}

func (a *campApp) ReplaceQueue(ctx context.Context, id string, items []model.QueueItem) error {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	if _, ok := a.camps[id]; !ok {
		return port.ErrNotFound
	}
	cp := make([]model.QueueItem, len(items))
	for i, it := range items {
		if it.ID == "" {
			a.seq++
			it.ID = fmt.Sprintf("q%d", a.seq)
		}
		cp[i] = it
	}
	a.queue[id] = cp
	return nil
}

func (a *campApp) ListQueue(ctx context.Context, id string, states []string, limit int) ([]model.QueueItem, error) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	out := []model.QueueItem{}
	for _, it := range a.queue[id] {
		if len(states) > 0 {
			ok := false
			for _, s := range states {
				ok = ok || s == it.State
			}
			if !ok {
				continue
			}
		}
		out = append(out, it)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (a *campApp) UpdateQueueItem(ctx context.Context, it model.QueueItem) error {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	items := a.queue[it.CampaignID]
	for i := range items {
		if items[i].ID == it.ID {
			items[i] = it
			return nil
		}
	}
	return port.ErrNotFound
}

func (a *campApp) CountSentSince(ctx context.Context, acc model.AccountID, since int64) (int, error) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	n := 0
	for id, c := range a.camps {
		if c.AccountID != acc {
			continue
		}
		for _, it := range a.queue[id] {
			if it.State == "sent" && it.SentAt >= since {
				n++
			}
		}
	}
	return n, nil
}

func (a *campApp) QueueItemByMessage(ctx context.Context, key model.MessageKey) (model.QueueItem, error) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	for _, items := range a.queue {
		for _, it := range items {
			if it.MessageKey == key && key != "" {
				return it, nil
			}
		}
	}
	return model.QueueItem{}, port.ErrNotFound
}

func (a *campApp) ListMembers(ctx context.Context, listIDs []string) ([]model.Contact, error) {
	a.memApp.mu.Lock()
	defer a.memApp.mu.Unlock()
	var out []model.Contact
	for _, ct := range a.memApp.contacts {
		for _, l := range ct.ListIDs {
			for _, want := range listIDs {
				if l == want {
					out = append(out, ct)
					goto next
				}
			}
		}
	next:
	}
	return out, nil
}

func (a *campApp) ListScheduled(ctx context.Context, from, to int64, states []string) ([]model.Scheduled, error) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	out := []model.Scheduled{}
	for _, s := range a.sched {
		if (from > 0 && s.SendAt < from) || (to > 0 && s.SendAt > to) {
			continue
		}
		if len(states) > 0 {
			ok := false
			for _, st := range states {
				ok = ok || st == s.State
			}
			if !ok {
				continue
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SendAt != out[j].SendAt {
			return out[i].SendAt < out[j].SendAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (a *campApp) DueScheduled(ctx context.Context, now int64) ([]model.Scheduled, error) {
	all, _ := a.ListScheduled(ctx, 0, 0, []string{"pending"})
	out := []model.Scheduled{}
	for _, s := range all {
		if s.SendAt <= now {
			out = append(out, s)
		}
	}
	return out, nil
}

func (a *campApp) SaveScheduled(ctx context.Context, s model.Scheduled) error {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	a.sched[s.ID] = s
	return nil
}

func (a *campApp) DeleteScheduled(ctx context.Context, id string) error {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	delete(a.sched, id)
	return nil
}

var _ port.AppStore = (*campApp)(nil)

// campClock: reloj de prueba que solo avanza cuando el test lo pide.
type campClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *campClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *campClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// campEnv: el entorno de prueba con el motor de campañas y un reloj propio.
type campEnv struct {
	*testEnv
	app   *campApp
	c     *Campaigns
	clock *campClock

	smu   sync.Mutex
	slept []time.Duration
}

// 2026-11-12 es jueves.
var campStart = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

func newCampEnv(t *testing.T) *campEnv {
	t.Helper()
	te := newTestEnv(t)
	e := &campEnv{testEnv: te, app: newCampApp(te.app), clock: &campClock{t: campStart}}
	e.c = NewCampaigns(e.app, te.accounts, te.msg, te.bus)
	e.c.now, e.c.loc = e.clock.Now, time.UTC
	e.c.tick = 5 * time.Millisecond
	e.c.randf = func() float64 { return 0.5 }
	e.c.sleep = func(ctx context.Context, d time.Duration) {
		e.smu.Lock()
		e.slept = append(e.slept, d)
		e.smu.Unlock()
	}
	t.Cleanup(e.c.Stop) // corre antes que la limpieza del entorno
	return e
}

func (e *campEnv) sleeps() []time.Duration {
	e.smu.Lock()
	defer e.smu.Unlock()
	return append([]time.Duration(nil), e.slept...)
}

// campPacing: ritmo de prueba, sin límites, sin calentamiento ni freno y con esperas de 1 s.
func campPacing() model.PacingSettings {
	return model.PacingSettings{MinDelaySec: 1, MaxDelaySec: 1, ColdMinDelaySec: 1, ColdMaxDelaySec: 1}
}

func peerOf(name string) string { return strings.ToLower(name) + "@s.whatsapp.net" }

// contact guarda un contacto con identidad en la cuenta (id «ct-<nombre en minúsculas>»).
func (e *campEnv) contact(acc model.AccountID, name string) model.Contact {
	e.t.Helper()
	ct := model.Contact{
		ID: "ct-" + strings.ToLower(name), Name: name, Phone: "+5939" + fmt.Sprintf("%07d", len(name)),
		Identities: []model.Identity{{AccountID: acc, Channel: model.ChannelWhatsApp, PeerNativeID: peerOf(name)}},
	}
	if err := e.testEnv.app.SaveContact(context.Background(), ct); err != nil {
		e.t.Fatal(err)
	}
	return ct
}

func (e *campEnv) warm(acc model.AccountID, name string) model.Contact {
	ct := e.contact(acc, name)
	e.seedChat(acc, peerOf(name), name, 1000)
	e.seedMessage(acc, peerOf(name), "in1", 1000, false, "hola")
	return ct
}

func (e *campEnv) lukewarm(acc model.AccountID, name string) model.Contact {
	ct := e.contact(acc, name)
	e.seedChat(acc, peerOf(name), name, 1000)
	e.seedMessage(acc, peerOf(name), "out1", 1000, true, "hola, soy yo")
	return ct
}

func ids(cs ...model.Contact) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

// save guarda una campaña de prueba para esos contactos.
func (e *campEnv) save(acc model.AccountID, mut func(*model.Campaign), cs ...model.Contact) model.Campaign {
	e.t.Helper()
	cp := model.Campaign{
		Name: "Prueba", AccountID: acc, Text: "Hola {{nombre}}, tenemos algo para ti",
		ContactIDs: ids(cs...), Pacing: campPacing(),
	}
	if mut != nil {
		mut(&cp)
	}
	out, err := e.c.Save(context.Background(), cp)
	if err != nil {
		e.t.Fatalf("Save: %v", err)
	}
	return out
}

func (e *campEnv) launch(id string) {
	e.t.Helper()
	if err := e.c.Launch(context.Background(), id); err != nil {
		e.t.Fatalf("Launch: %v", err)
	}
}

func (e *campEnv) queue(id string, states ...string) []model.QueueItem {
	e.t.Helper()
	q, err := e.c.Queue(context.Background(), id, states, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return q
}

func (e *campEnv) get(id string) model.Campaign {
	e.t.Helper()
	cp, err := e.c.Get(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return cp
}

func (e *campEnv) sentTexts(acc model.AccountID) []string {
	var out []string
	for _, s := range e.adapter(acc).Sent() {
		out = append(out, s.Text)
	}
	return out
}

// tick corre un tick del motor para la cuenta y devuelve si todavía hay campañas en marcha.
func (e *campEnv) tick(acc model.AccountID) bool {
	e.t.Helper()
	return e.c.processAccount(context.Background(), acc)
}

// run corre ticks avanzando el reloj `step` entre ellos hasta que ya no haya campañas en marcha
// (o `max` ticks).
func (e *campEnv) run(acc model.AccountID, step time.Duration, max int) {
	e.t.Helper()
	for i := 0; i < max; i++ {
		if !e.tick(acc) {
			return
		}
		e.clock.Advance(step)
	}
}

// progress recoge los avisos de progreso pendientes.
func (e *campEnv) progress() []CampaignProgress {
	var out []CampaignProgress
	for {
		select {
		case p := <-e.c.Progress():
			out = append(out, p)
		default:
			return out
		}
	}
}

func lastProgress(ps []CampaignProgress, id string) (CampaignProgress, bool) {
	for i := len(ps) - 1; i >= 0; i-- {
		if ps[i].CampaignID == id {
			return ps[i], true
		}
	}
	return CampaignProgress{}, false
}

func campIsErr(err, target error) bool { return err != nil && errors.Is(err, target) }

// ---------------------------------------------------------------------------------------------
// Guardar

func TestCampaignsSaveCreatesDraft(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "Ventas")
	ana := e.contact(acc.ID, "Ana")

	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.Variants = []string{" B ", "", "C"}
		c.ListIDs = []string{"l1", " l1 ", ""}
		c.State = model.CampaignRunning // lo ignora: quien llama no fija el estado
		c.Pacing = model.PacingSettings{}
	}, ana)
	if cp.ID == "" || cp.State != model.CampaignDraft || cp.CreatedAt == 0 || cp.UpdatedAt == 0 {
		t.Fatalf("campaña nueva: %+v", cp)
	}
	if len(cp.Variants) != 2 || cp.Variants[0] != "B" || cp.Variants[1] != "C" {
		t.Errorf("variantes: %q", cp.Variants)
	}
	if len(cp.ListIDs) != 1 || cp.ListIDs[0] != "l1" {
		t.Errorf("listas: %q", cp.ListIDs)
	}
	// Sin ritmo propio toma el de la cuenta (el que guardó Accounts.Create).
	if cp.Pacing != model.DefaultPacing {
		t.Errorf("ritmo: %+v", cp.Pacing)
	}
	got, err := e.c.Get(context.Background(), cp.ID)
	if err != nil || got.Name != "Prueba" {
		t.Fatalf("Get: %+v %v", got, err)
	}
	list, err := e.c.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v %v", list, err)
	}

	// Editar una borrador conserva id y fecha de creación.
	edited := cp
	edited.Name = "Otro nombre"
	out, err := e.c.Save(context.Background(), edited)
	if err != nil || out.ID != cp.ID || out.CreatedAt != cp.CreatedAt || out.Name != "Otro nombre" {
		t.Fatalf("editar: %+v %v", out, err)
	}
}

func TestCampaignsSaveValidation(t *testing.T) {
	e := newCampEnv(t)
	wa := e.newAccount(model.ChannelWhatsApp, "")
	ms := e.newAccount(model.ChannelMessenger, "")
	ig := e.newAccount(model.ChannelInstagram, "")
	tg := e.newAccount(model.ChannelTelegram, "")
	ctx := context.Background()

	for _, a := range []model.Account{ms, ig} {
		_, err := e.c.Save(ctx, model.Campaign{Name: "x", AccountID: a.ID, Text: "hola"})
		if !campIsErr(err, port.ErrUnsupported) || !strings.Contains(err.Error(), "WhatsApp o Telegram") {
			t.Errorf("%s: %v", a.Channel, err)
		}
	}
	if _, err := e.c.Save(ctx, model.Campaign{Name: "x", AccountID: tg.ID, Text: "hola"}); err != nil {
		t.Errorf("Telegram debe poder: %v", err)
	}
	if _, err := e.c.Save(ctx, model.Campaign{Name: "x", AccountID: "wa-noexiste", Text: "hola"}); !campIsErr(err, port.ErrNotFound) {
		t.Errorf("cuenta inexistente: %v", err)
	}
	if _, err := e.c.Save(ctx, model.Campaign{Name: "x", Text: "hola"}); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("sin cuenta: %v", err)
	}
	if _, err := e.c.Save(ctx, model.Campaign{Name: "  ", AccountID: wa.ID, Text: "hola"}); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("sin nombre: %v", err)
	}
	if _, err := e.c.Save(ctx, model.Campaign{Name: "x", AccountID: wa.ID, Window: model.SendWindow{FromMinute: 600, ToMinute: 2000}}); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("ventana: %v", err)
	}
	if _, err := e.c.Save(ctx, model.Campaign{Name: "x", AccountID: wa.ID, Window: model.SendWindow{Weekdays: []int{7}}}); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("días: %v", err)
	}
	if _, err := e.c.Save(ctx, model.Campaign{Name: "x", AccountID: wa.ID, Variants: []string{"1", "2", "3", "4", "5"}}); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("demasiadas variantes: %v", err)
	}
	if _, err := e.c.Save(ctx, model.Campaign{Name: "x", AccountID: wa.ID, FilePath: filepath.Join(t.TempDir(), "no-existe.jpg")}); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("adjunto inexistente: %v", err)
	}
}

func TestCampaignsSaveOnlyInEditableStates(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.contact(acc.ID, "Ana")
	cp := e.save(acc.ID, nil, ana)
	e.launch(cp.ID)

	edit := e.get(cp.ID)
	edit.Name = "cambiado"
	if _, err := e.c.Save(ctx, edit); !campIsErr(err, port.ErrInvalid) {
		t.Fatalf("running no se edita: %v", err)
	}
	if err := e.c.Pause(ctx, cp.ID); err != nil {
		t.Fatal(err)
	}
	if out, err := e.c.Save(ctx, edit); err != nil || out.State != model.CampaignPaused || out.Name != "cambiado" {
		t.Fatalf("paused sí se edita y sigue paused: %+v %v", out, err)
	}
	if err := e.c.Cancel(ctx, cp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Save(ctx, edit); !campIsErr(err, port.ErrInvalid) {
		t.Fatalf("canceled no se edita: %v", err)
	}

	// Una campaña ready (programada) vuelve a draft al guardarla.
	cp2 := e.save(acc.ID, func(c *model.Campaign) { c.StartAt = campStart.Add(time.Hour).UnixMilli() }, ana)
	e.launch(cp2.ID)
	if e.get(cp2.ID).State != model.CampaignReady {
		t.Fatal("debería quedar ready")
	}
	if out, err := e.c.Save(ctx, e.get(cp2.ID)); err != nil || out.State != model.CampaignDraft {
		t.Fatalf("ready → draft: %+v %v", out, err)
	}
}

func TestCampaignsAttachmentCopy(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.contact(acc.ID, "Ana")

	src := filepath.Join(t.TempDir(), "foto.jpg")
	if err := os.WriteFile(src, []byte("jpegdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp := e.save(acc.ID, func(c *model.Campaign) { c.FilePath = src }, ana)
	wantDir := filepath.Join(paths.CampaignMediaDir(), cp.ID)
	if filepath.Dir(cp.FilePath) != wantDir || filepath.Base(cp.FilePath) != "foto.jpg" {
		t.Fatalf("copia en %s", cp.FilePath)
	}
	if b, err := os.ReadFile(cp.FilePath); err != nil || string(b) != "jpegdata" {
		t.Fatalf("contenido: %q %v", b, err)
	}
	// El original puede desaparecer.
	os.Remove(src)
	// Volver a guardar con la copia no duplica.
	if out, err := e.c.Save(ctx, cp); err != nil || out.FilePath != cp.FilePath {
		t.Fatalf("re-guardar: %+v %v", out, err)
	}
	// Cambiar de adjunto borra la copia anterior.
	src2 := filepath.Join(t.TempDir(), "clip.mp4")
	os.WriteFile(src2, []byte("video"), 0o644)
	edit := cp
	edit.FilePath = src2
	out, err := e.c.Save(ctx, edit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cp.FilePath); !os.IsNotExist(err) {
		t.Error("la copia anterior debía borrarse")
	}
	if filepath.Base(out.FilePath) != "clip.mp4" {
		t.Fatalf("copia nueva: %s", out.FilePath)
	}
	// Quitar el adjunto borra la copia.
	edit = out
	edit.FilePath = ""
	if out2, err := e.c.Save(ctx, edit); err != nil || out2.FilePath != "" {
		t.Fatalf("quitar: %+v %v", out2, err)
	}
	if _, err := os.Stat(out.FilePath); !os.IsNotExist(err) {
		t.Error("la copia debía borrarse al quitar el adjunto")
	}
}

func TestCampaignsDeleteRules(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.contact(acc.ID, "Ana")
	src := filepath.Join(t.TempDir(), "doc.pdf")
	os.WriteFile(src, []byte("pdf"), 0o644)
	cp := e.save(acc.ID, func(c *model.Campaign) { c.FilePath = src }, ana)
	e.launch(cp.ID)
	if err := e.c.Delete(ctx, cp.ID); !campIsErr(err, port.ErrInvalid) {
		t.Fatalf("en marcha no se borra: %v", err)
	}
	if err := e.c.Pause(ctx, cp.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.c.Delete(ctx, cp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Get(ctx, cp.ID); !campIsErr(err, port.ErrNotFound) {
		t.Fatalf("debía desaparecer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.CampaignMediaDir(), cp.ID)); !os.IsNotExist(err) {
		t.Error("la carpeta del adjunto debía borrarse")
	}
	if err := e.c.Delete(ctx, cp.ID); !campIsErr(err, port.ErrNotFound) {
		t.Fatalf("borrar dos veces: %v", err)
	}
}

func TestCampaignsStateTransitions(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.contact(acc.ID, "Ana")
	cp := e.save(acc.ID, nil, ana)

	if err := e.c.Pause(ctx, cp.ID); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("pausar un borrador: %v", err)
	}
	if err := e.c.Resume(ctx, cp.ID); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("reanudar un borrador: %v", err)
	}
	e.launch(cp.ID)
	if err := e.c.Launch(ctx, cp.ID); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("lanzar una en marcha: %v", err)
	}
	if err := e.c.Pause(ctx, cp.ID); err != nil || e.get(cp.ID).State != model.CampaignPaused {
		t.Fatalf("pausar: %v", err)
	}
	if err := e.c.Resume(ctx, cp.ID); err != nil || e.get(cp.ID).State != model.CampaignRunning {
		t.Fatalf("reanudar: %v", err)
	}
	if err := e.c.Cancel(ctx, cp.ID); err != nil || e.get(cp.ID).State != model.CampaignCanceled {
		t.Fatalf("cancelar: %v", err)
	}
	if err := e.c.Cancel(ctx, cp.ID); !campIsErr(err, port.ErrInvalid) {
		t.Errorf("cancelar dos veces: %v", err)
	}
	for _, f := range []func(context.Context, string) error{e.c.Pause, e.c.Resume, e.c.Launch} {
		if err := f(ctx, "no-existe"); !campIsErr(err, port.ErrNotFound) {
			t.Errorf("campaña inexistente: %v", err)
		}
	}
	if _, err := e.c.Queue(ctx, "no-existe", nil, 0); !campIsErr(err, port.ErrNotFound) {
		t.Errorf("cola de una inexistente: %v", err)
	}
}

// ---------------------------------------------------------------------------------------------
// Ritmo por cuenta

func TestCampaignsPacingSetting(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")

	got, err := e.c.GetPacing(ctx, acc.ID)
	if err != nil || got != model.DefaultPacing {
		t.Fatalf("por defecto: %+v %v", got, err)
	}
	p := model.DefaultPacing
	p.DailyLimit, p.MinDelaySec, p.MaxDelaySec, p.HourlyLimit, p.WarmupDays = 50, 90, 30, -4, 0
	if err := e.c.SetPacing(ctx, acc.ID, p); err != nil {
		t.Fatal(err)
	}
	got, _ = e.c.GetPacing(ctx, acc.ID)
	if got.DailyLimit != 50 || got.MinDelaySec != 30 || got.MaxDelaySec != 90 || got.HourlyLimit != 0 || got.WarmupDays != 1 {
		t.Errorf("no se corrigió: %+v", got)
	}
	raw, _ := e.testEnv.app.GetSetting(ctx, "pacing:"+string(acc.ID))
	if !strings.Contains(raw, `"dailyLimit":50`) {
		t.Errorf("ajuste guardado: %s", raw)
	}
	if err := e.c.SetPacing(ctx, "wa-noexiste", p); !campIsErr(err, port.ErrNotFound) {
		t.Errorf("cuenta inexistente: %v", err)
	}
	// Un ajuste dañado vuelve al de por defecto.
	_ = e.testEnv.app.SetSetting(ctx, "pacing:"+string(acc.ID), "{no es json")
	if got, err := e.c.GetPacing(ctx, acc.ID); err != nil || got != model.DefaultPacing {
		t.Errorf("dañado: %+v %v", got, err)
	}
	// Un ajuste parcial conserva el resto por defecto.
	_ = e.testEnv.app.SetSetting(ctx, "pacing:"+string(acc.ID), `{"dailyLimit":7}`)
	got, _ = e.c.GetPacing(ctx, acc.ID)
	if got.DailyLimit != 7 || got.MinDelaySec != model.DefaultPacing.MinDelaySec {
		t.Errorf("parcial: %+v", got)
	}
}

// ---------------------------------------------------------------------------------------------
// Revisión y lanzamiento

func TestCampaignsReviewCountsRisksAndSkips(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")

	w1, w2 := e.warm(acc.ID, "Ana"), e.warm(acc.ID, "Beto")
	l1 := e.lukewarm(acc.ID, "Carla")
	c1, c2 := e.contact(acc.ID, "Dani"), e.contact(acc.ID, "Eva")
	opt := e.contact(acc.ID, "Fabi")
	opt.OptedOut = true
	_ = e.testEnv.app.SaveContact(ctx, opt)
	noid := model.Contact{ID: "ct-gus", Name: "Gus", Identities: []model.Identity{{AccountID: "otra-cuenta", PeerNativeID: "x"}}}
	_ = e.testEnv.app.SaveContact(ctx, noid)
	freq := e.contact(acc.ID, "Hugo")

	// Hugo ya recibió 2 campañas en los últimos 7 días.
	for i := 0; i < 2; i++ {
		prev := model.Campaign{ID: fmt.Sprintf("prev%d", i), Name: "previa", AccountID: acc.ID, State: model.CampaignDone, CreatedAt: int64(i + 1)}
		_ = e.app.SaveCampaign(ctx, prev)
		_ = e.app.ReplaceQueue(ctx, prev.ID, []model.QueueItem{{ContactID: freq.ID, State: "sent", SentAt: campStart.Add(-24 * time.Hour).UnixMilli()}})
	}

	pacing := campPacing()
	pacing.FreqMax, pacing.FreqDays = 2, 7
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, w1, w2, l1, c1, c2, opt, noid, freq)

	rev, err := e.c.Review(ctx, cp)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Recipients != 5 {
		t.Errorf("destinatarios = %d", rev.Recipients)
	}
	if rev.Risks["warm"] != 2 || rev.Risks["lukewarm"] != 1 || rev.Risks["cold"] != 2 {
		t.Errorf("riesgos = %v", rev.Risks)
	}
	if rev.Skipped["optedOut"] != 1 || rev.Skipped["noIdentity"] != 1 || rev.Skipped["frequency"] != 1 {
		t.Errorf("saltados = %v", rev.Skipped)
	}
	if rev.Level == campaign.LevelRed || rev.Required != 0 {
		t.Errorf("nivel %s, requeridas %d, hallazgos %+v", rev.Level, rev.Required, rev.Items)
	}
	if rev.Estimate.Seconds <= 0 || rev.Estimate.Days != 1 {
		t.Errorf("estimación = %+v", rev.Estimate)
	}
	// Sin límite de fríos al día hay aviso.
	found := false
	for _, w := range rev.Warnings {
		found = found || strings.Contains(w, "límite prudente")
	}
	if !found {
		t.Errorf("avisos = %v", rev.Warnings)
	}
}

func TestCampaignsReviewBlockingProblems(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ms := e.newAccount(model.ChannelMessenger, "")
	ana := e.contact(acc.ID, "Ana")

	codes := func(rev CampaignReview, level string) map[string]bool {
		out := map[string]bool{}
		for _, it := range rev.Items {
			if it.Level == level {
				out[it.Code] = true
			}
		}
		return out
	}
	// Sin texto ni adjunto.
	rev, _ := e.c.Review(ctx, model.Campaign{Name: "x", AccountID: acc.ID, ContactIDs: ids(ana)})
	if rev.Level != campaign.LevelRed || !codes(rev, campaign.LevelRed)["empty"] {
		t.Errorf("vacío: %+v", rev)
	}
	// Sin destinatarios.
	rev, _ = e.c.Review(ctx, model.Campaign{Name: "x", AccountID: acc.ID, Text: "Hola {{nombre}}"})
	if rev.Level != campaign.LevelRed || !codes(rev, campaign.LevelRed)["recipients"] {
		t.Errorf("sin destinatarios: %+v", rev)
	}
	// Cuenta de Messenger: rojo, no error.
	rev, err := e.c.Review(ctx, model.Campaign{Name: "x", AccountID: ms.ID, Text: "Hola"})
	if err != nil || !codes(rev, campaign.LevelRed)["account"] {
		t.Errorf("Messenger: %+v %v", rev, err)
	}
	// Cuenta inexistente.
	rev, err = e.c.Review(ctx, model.Campaign{Name: "x", AccountID: "wa-nada", Text: "Hola"})
	if err != nil || !codes(rev, campaign.LevelRed)["account"] {
		t.Errorf("inexistente: %+v %v", rev, err)
	}
	// 30 destinatarios con un solo mensaje fijo: faltan variantes. Con {a|b|c} basta.
	var many []model.Contact
	for i := 0; i < 30; i++ {
		many = append(many, e.contact(acc.ID, fmt.Sprintf("P%02d", i)))
	}
	rev, _ = e.c.Review(ctx, model.Campaign{Name: "x", AccountID: acc.ID, Text: "Hola {{nombre}}, mira esto", ContactIDs: ids(many...), Pacing: campPacing()})
	if rev.Required != 3 || rev.Level != campaign.LevelRed || !codes(rev, campaign.LevelRed)["variants"] {
		t.Errorf("faltan variantes: %+v", rev)
	}
	rev, _ = e.c.Review(ctx, model.Campaign{Name: "x", AccountID: acc.ID, Text: "{Hola|Buenas|Qué tal} {{nombre}}, mira esto", ContactIDs: ids(many...), Pacing: campPacing()})
	if rev.Level == campaign.LevelRed {
		t.Errorf("con opciones {a|b|c} debía bastar: %+v", rev.Items)
	}
	// Variantes casi iguales: aviso de parecido.
	rev, _ = e.c.Review(ctx, model.Campaign{Name: "x", AccountID: acc.ID, Text: "Hola {{nombre}}, tenemos pan fresco hoy mismo",
		Variants: []string{"Hola {{nombre}}, tenemos pan fresco hoy mismo!"}, ContactIDs: ids(ana)})
	if !codes(rev, campaign.LevelRed)["similar"] {
		t.Errorf("parecido: %+v", rev.Items)
	}
	// Los textos de otras campañas recientes avisan de repetido.
	other := e.save(acc.ID, func(c *model.Campaign) {
		c.Name = "otra"
		c.Text = "Hola {{nombre}}, ya llegó la colección de invierno"
	}, ana)
	rev, _ = e.c.Review(ctx, model.Campaign{Name: "x", AccountID: acc.ID, Text: "Hola {{nombre}}, ya llegó la colección de invierno", ContactIDs: ids(ana)})
	if !codes(rev, campaign.LevelYellow)["repeated"] {
		t.Errorf("repetido (otra=%s): %+v", other.ID, rev.Items)
	}
	// Launch se niega con una revisión en rojo.
	bad := e.save(acc.ID, func(c *model.Campaign) { c.Text = "" })
	if err := e.c.Launch(ctx, bad.ID); !campIsErr(err, port.ErrInvalid) || !strings.Contains(err.Error(), "vacío") {
		t.Errorf("lanzar en rojo: %v", err)
	}
	if e.get(bad.ID).State != model.CampaignDraft {
		t.Error("debía seguir en borrador")
	}
}

func TestCampaignsLaunchBuildsOrderedQueue(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")

	var cs []model.Contact
	cs = append(cs, e.contact(acc.ID, "Dani"), e.contact(acc.ID, "Eva")) // fríos
	cs = append(cs, e.lukewarm(acc.ID, "Carla"))
	cs = append(cs, e.warm(acc.ID, "Ana"), e.warm(acc.ID, "Beto"))
	opt := e.contact(acc.ID, "Fabi")
	opt.OptedOut = true
	_ = e.testEnv.app.SaveContact(ctx, opt)
	cs = append(cs, opt)

	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.Text = "{Hola|Buenas} {{nombre}}, ¿nos vemos?"
		c.OptOutFooter = true
	}, cs...)
	e.launch(cp.ID)

	got := e.get(cp.ID)
	if got.State != model.CampaignRunning || got.Totals.Queued != 5 || got.Totals.Skipped != 1 {
		t.Fatalf("campaña: %+v", got)
	}
	q := e.queue(cp.ID)
	if len(q) != 6 {
		t.Fatalf("cola: %+v", q)
	}
	var risks []string
	for _, it := range q[:5] {
		risks = append(risks, it.Risk)
		if it.State != "pending" || it.Variant != 0 || it.ABSample || it.ChatNativeID == "" {
			t.Errorf("item: %+v", it)
		}
	}
	if strings.Join(risks, ",") != "warm,warm,lukewarm,cold,cold" {
		t.Fatalf("orden de riesgo: %v", risks)
	}
	foot := "\n\nResponde BAJA para no recibir más mensajes."
	for _, it := range q[:5] {
		name := strings.TrimSuffix(strings.SplitN(it.ChatNativeID, "@", 2)[0], "")
		want := strings.ToUpper(name[:1]) + name[1:]
		if !strings.HasSuffix(it.Text, foot) || !strings.Contains(it.Text, want) || strings.Contains(it.Text, "{") {
			t.Errorf("texto personalizado: %q", it.Text)
		}
		if !strings.HasPrefix(it.Text, "Hola "+want) && !strings.HasPrefix(it.Text, "Buenas "+want) {
			t.Errorf("opción sin resolver: %q", it.Text)
		}
	}
	if q[5].State != "skipped" || q[5].ContactID != opt.ID || q[5].Error == "" {
		t.Errorf("saltado: %+v", q[5])
	}
	// Pie de baja personalizado con el ajuste.
	_ = e.testEnv.app.SetSetting(ctx, SettingOptOutFooter, "Escribe STOP para salir.")
	cp2 := e.save(acc.ID, func(c *model.Campaign) { c.OptOutFooter = true; c.Name = "con pie propio" }, cs[3])
	e.launch(cp2.ID)
	if txt := e.queue(cp2.ID)[0].Text; !strings.HasSuffix(txt, "\n\nEscribe STOP para salir.") {
		t.Errorf("pie propio: %q", txt)
	}
	// Sin OptOutFooter no hay pie.
	cp3 := e.save(acc.ID, func(c *model.Campaign) { c.Name = "sin pie" }, cs[3])
	e.launch(cp3.ID)
	if txt := e.queue(cp3.ID)[0].Text; strings.Contains(txt, "STOP") || strings.Contains(txt, "BAJA") {
		t.Errorf("no debía llevar pie: %q", txt)
	}
}

func TestCampaignsLaunchIsReproducible(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 12; i++ {
		cs = append(cs, e.contact(acc.ID, fmt.Sprintf("P%02d", i)))
	}
	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.Text = "{Hola|Buenas|Qué tal} {{nombre}}"
		c.StartAt = campStart.Add(time.Hour).UnixMilli() // queda ready: se puede relanzar
	}, cs...)
	e.launch(cp.ID)
	first := e.queue(cp.ID)
	e.launch(cp.ID) // regenera la cola con la misma semilla (id de la campaña)
	second := e.queue(cp.ID)
	if len(first) != 12 || len(second) != 12 {
		t.Fatalf("colas: %d y %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ContactID != second[i].ContactID || first[i].Text != second[i].Text || first[i].Variant != second[i].Variant {
			t.Fatalf("la cola no es reproducible en la posición %d: %+v / %+v", i, first[i], second[i])
		}
	}
	// Dos campañas con el mismo id de semilla no existen; comprobamos la semilla directamente.
	a := personalize("c1", "{a|b|c|d} {{nombre}}", model.Contact{ID: "x", Name: "Ana"}, "")
	b := personalize("c1", "{a|b|c|d} {{nombre}}", model.Contact{ID: "x", Name: "Ana"}, "")
	if a != b {
		t.Errorf("personalize no es reproducible: %q %q", a, b)
	}
}

func TestCampaignsLaunchSplitsVariantsEvenly(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 31; i++ {
		cs = append(cs, e.contact(acc.ID, fmt.Sprintf("P%02d", i)))
	}
	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.Text = "A: hola {{nombre}}, te cuento una novedad"
		c.Variants = []string{"B: buenas {{nombre}}, mira lo que llegó", "C: qué tal {{nombre}}, tengo algo nuevo"}
	}, cs...)
	e.launch(cp.ID)
	counts := map[int]int{}
	for _, it := range e.queue(cp.ID) {
		counts[it.Variant]++
		letter := campaign.VariantLetters[it.Variant]
		if !strings.HasPrefix(it.Text, letter+": ") {
			t.Fatalf("variante %d con texto %q", it.Variant, it.Text)
		}
	}
	if len(counts) != 3 {
		t.Fatalf("reparto: %v", counts)
	}
	mx, mn := 0, 100
	for _, n := range counts {
		mx, mn = max(mx, n), min(mn, n)
	}
	if mx-mn > 1 {
		t.Errorf("reparto desigual: %v", counts)
	}
}

func TestCampaignsLaunchScheduledStartsReady(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.contact(acc.ID, "Ana")
	cp := e.save(acc.ID, func(c *model.Campaign) { c.StartAt = campStart.Add(2 * time.Hour).UnixMilli() }, ana)
	e.launch(cp.ID)
	if e.get(cp.ID).State != model.CampaignReady {
		t.Fatal("con StartAt futuro debe quedar ready")
	}
	e.c.sync(ctx)
	if e.get(cp.ID).State != model.CampaignReady {
		t.Fatal("aún no es la hora")
	}
	e.clock.Advance(2*time.Hour + time.Second)
	e.c.sync(ctx) // promueve ready → running y arranca el trabajador
	if e.get(cp.ID).State != model.CampaignRunning {
		t.Fatal("pasada la hora debe pasar a running")
	}
	e.c.Stop() // espera al trabajador que arrancó sync
}

func TestCampaignsLaunchWorksWithTelegram(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelTelegram, "")
	ct := model.Contact{ID: "ct-ana", Name: "Ana", Identities: []model.Identity{{AccountID: acc.ID, Channel: model.ChannelTelegram, PeerNativeID: "user:77"}}}
	_ = e.testEnv.app.SaveContact(context.Background(), ct)
	cp := e.save(acc.ID, nil, ct)
	e.launch(cp.ID)
	e.run(acc.ID, time.Minute, 10)
	if got := e.sentTexts(acc.ID); len(got) != 1 || got[0] != "Hola Ana, tenemos algo para ti" {
		t.Fatalf("enviados: %q", got)
	}
	if e.get(cp.ID).State != model.CampaignDone {
		t.Errorf("estado: %s", e.get(cp.ID).State)
	}
}

func TestCampaignsMembersFromLists(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.contact(acc.ID, "Ana")
	ana.ListIDs = []string{"l1"}
	_ = e.testEnv.app.SaveContact(ctx, ana)
	beto := e.contact(acc.ID, "Beto")
	beto.ListIDs = []string{"l1", "l2"}
	_ = e.testEnv.app.SaveContact(ctx, beto)
	carla := e.contact(acc.ID, "Carla")

	// Ana y Beto por lista; Beto también suelto y Carla suelta: cada uno una vez.
	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.ListIDs = []string{"l1", "l2"}
		c.ContactIDs = []string{beto.ID, carla.ID, "ct-fantasma"}
	})
	rev, err := e.c.Review(ctx, cp)
	if err != nil || rev.Recipients != 3 {
		t.Fatalf("destinatarios: %d %v", rev.Recipients, err)
	}
}
