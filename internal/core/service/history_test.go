package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/fake"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

// ---------------------------------------------------------------------------------------------
// Entorno

// histApp: AppStore en memoria que además guarda el avance del historial (HistoryStore).
type histApp struct {
	*memApp
	*memHistoryStore
}

type histCall struct {
	chat     string
	beforeID string
	hasRef   bool
	count    int
}

// histScript decide qué hace RequestHistory en una llamada (n empieza en 1 y cuenta todas las del adaptador).
type histScript func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error

type histAdapter struct {
	*fake.Adapter
	env *histEnv
	mu  sync.Mutex
	n   int
	log []histCall
}

func (a *histAdapter) RequestHistory(ctx context.Context, chat string, before *model.MessageRef, count int) error {
	a.mu.Lock()
	a.n++
	n := a.n
	c := histCall{chat: chat, count: count}
	if before != nil {
		c.hasRef, c.beforeID = true, before.NativeID
	}
	a.log = append(a.log, c)
	a.mu.Unlock()
	a.env.enter()
	defer a.env.leave()
	if s := a.env.scriptOf(); s != nil {
		return s(a, n, chat, before, count)
	}
	return nil
}

func (a *histAdapter) calls() []histCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]histCall(nil), a.log...)
}

type histEnv struct {
	t        *testing.T
	app      *histApp
	bus      *bus.Bus
	router   *Router
	accounts *Accounts
	hist     *History

	mu       sync.Mutex
	script   histScript
	adapters map[model.AccountID]*histAdapter
	stores   map[model.AccountID]*memAccountStore
	active   int
	maxAct   int
}

func (e *histEnv) enter() {
	e.mu.Lock()
	e.active++
	if e.active > e.maxAct {
		e.maxAct = e.active
	}
	e.mu.Unlock()
}

func (e *histEnv) leave() {
	e.mu.Lock()
	e.active--
	e.mu.Unlock()
}

func (e *histEnv) scriptOf() histScript {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.script
}

func (e *histEnv) setScript(s histScript) {
	e.mu.Lock()
	e.script = s
	e.mu.Unlock()
}

// fastPace: el ritmo de un canal reducido a milisegundos para las pruebas.
func fastPace(base historyPacing) historyPacing {
	base.Pause, base.Wait, base.Settle = time.Millisecond, 400*time.Millisecond, 15*time.Millisecond
	return base
}

func newHistEnv(t *testing.T) *histEnv {
	t.Helper()
	if err := paths.InitAt(t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	e := &histEnv{
		t: t, bus: bus.New(), app: &histApp{memApp: newMemApp(), memHistoryStore: newMemHistoryStore()},
		adapters: map[model.AccountID]*histAdapter{}, stores: map[model.AccountID]*memAccountStore{},
	}
	e.router = NewRouter(e.app, e.bus)
	factory := func(deps port.AdapterDeps) (port.ChannelAdapter, error) {
		a := &histAdapter{Adapter: fake.New(deps.Account), env: e}
		e.mu.Lock()
		e.adapters[deps.Account.ID] = a
		e.mu.Unlock()
		return a, nil
	}
	reg := map[model.Channel]port.AdapterFactory{}
	for _, ch := range model.AllChannels {
		reg[ch] = factory
	}
	open := func(id model.AccountID) (port.AccountStore, *sql.DB, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		st, ok := e.stores[id]
		if !ok {
			st = newMemAccountStore(id)
			e.stores[id] = st
		}
		return st, nil, nil
	}
	e.accounts = NewAccounts(e.app, e.router, e.bus, reg, open)
	e.accounts.backoffMin, e.accounts.backoffMax = 10*time.Millisecond, 40*time.Millisecond
	e.hist = NewHistory(e.app, e.accounts, e.bus)
	for ch, p := range e.hist.pacing {
		e.hist.pacing[ch] = fastPace(p)
	}
	e.hist.floodPad = 0
	e.hist.autoDelay, e.hist.chainGap = 10*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() {
		e.hist.StopAll()
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = e.accounts.StopAll(ctx)
	})
	return e
}

func (e *histEnv) adapter(id model.AccountID) *histAdapter {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.adapters[id]
}

func (e *histEnv) store(id model.AccountID) *memAccountStore {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stores[id]
}

func (e *histEnv) newAccount(ch model.Channel) model.Account {
	e.t.Helper()
	acc, err := e.accounts.Create(context.Background(), ch, "", "", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	eventually(e.t, "cuenta conectada", func() bool { return e.accounts.Status(acc.ID).State == model.StateConnected })
	return acc
}

func (e *histEnv) chat(acc model.AccountID, native string, kind model.ChatKind, last int64) {
	e.t.Helper()
	c := model.Chat{
		Key: model.NewChatKey(acc, native), AccountID: acc, NativeID: native, Kind: kind,
		Title: "Chat " + native, LastMessageAt: last,
	}
	if err := e.store(acc).UpsertChat(context.Background(), c); err != nil {
		e.t.Fatal(err)
	}
}

func (e *histEnv) msg(acc model.AccountID, chat, native string, ts int64) {
	e.t.Helper()
	m := model.Message{
		Key: model.NewMessageKey(acc, chat, native), ChatKey: model.NewChatKey(acc, chat), AccountID: acc,
		NativeID: native, ChatNativeID: chat, SenderNativeID: chat, Timestamp: ts, Kind: model.KindText,
		Text: "m " + native, Delivery: model.DeliverySent,
	}
	if _, err := e.store(acc).UpsertMessage(context.Background(), m); err != nil {
		e.t.Fatal(err)
	}
}

func (e *histEnv) count(acc model.AccountID, chat string) int {
	st, err := chatStats(context.Background(), e.store(acc), chat)
	if err != nil {
		e.t.Fatal(err)
	}
	return st.Count
}

// waitIdle espera a que la cuenta no tenga tanda en curso.
func (e *histEnv) waitIdle(acc model.AccountID) HistoryStatus {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		e.hist.mu.Lock()
		running := e.hist.runs[acc] != nil
		e.hist.mu.Unlock()
		if !running {
			return e.hist.Status(acc)
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.t.Fatal("la tanda no terminó a tiempo")
	return HistoryStatus{}
}

func (e *histEnv) rows(acc model.AccountID) map[string]model.HistorySyncRow {
	e.t.Helper()
	rows, err := e.hist.loadRows(context.Background(), acc)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *histEnv) start(acc model.AccountID, opts HistoryOptions) {
	e.t.Helper()
	if err := e.hist.Start(context.Background(), acc, opts); err != nil {
		e.t.Fatal(err)
	}
}

func (e *histEnv) setPace(ch model.Channel, fn func(p *historyPacing)) {
	p := e.hist.pacing[ch]
	fn(&p)
	e.hist.pacing[ch] = p
}

// archive: el historial «remoto» de cada chat (lo que el canal tiene más antiguo que lo guardado).
type archive struct {
	mu     sync.Mutex
	chats  map[string][]model.Message // por chat, de más antiguo a más reciente
	marker bool                       // contestar con un lote vacío cuando no queda nada
}

func newArchive(marker bool) *archive {
	return &archive{chats: map[string][]model.Message{}, marker: marker}
}

// fill crea n mensajes del chat con ts 1..n (los ids son "<chat>-old-<ts>").
func (ar *archive) fill(chat string, n int) {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	for i := 1; i <= n; i++ {
		ar.chats[chat] = append(ar.chats[chat], model.Message{
			NativeID: fmt.Sprintf("%s-old-%d", chat, i), ChatNativeID: chat, SenderNativeID: chat,
			Timestamp: int64(i), Kind: model.KindText, Text: "viejo", Delivery: model.DeliverySent,
		})
	}
}

func (ar *archive) script() histScript {
	return func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		limit := int64(math.MaxInt64)
		if before != nil {
			limit = before.Timestamp
		}
		ar.mu.Lock()
		var older []model.Message
		for _, m := range ar.chats[chat] {
			if m.Timestamp < limit {
				older = append(older, m)
			}
		}
		ar.mu.Unlock()
		if len(older) > count {
			older = older[len(older)-count:]
		}
		switch {
		case len(older) > 0:
			a.Inject(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{Messages: older, Progress: -1}})
		case ar.marker:
			a.Inject(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{Progress: -1}})
		}
		return nil
	}
}

func chatsOf(calls []histCall) []string {
	var out []string
	for _, c := range calls {
		out = append(out, c.chat)
	}
	return out
}

// waitErr: error de espera con el tiempo que pide el canal.
type waitErr struct{ d time.Duration }

func (w waitErr) Error() string             { return "espera pedida" }
func (w waitErr) RetryAfter() time.Duration { return w.d }
func (w waitErr) Unwrap() error             { return port.ErrRateLimited }

// ---------------------------------------------------------------------------------------------
// Tanda: avance, orden, reanudación

func TestHistoryDrainsAChatAndStops(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "c1", model.ChatDirect, 1000)
	for i := 0; i < 3; i++ {
		e.msg(acc.ID, "c1", fmt.Sprintf("new-%d", i), int64(1000+i))
	}
	ar := newArchive(true)
	ar.fill("c1", 35)
	e.setScript(ar.script())
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Count = 10 })

	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)

	if st.Running || st.Phase != HistoryPhaseIdle || st.StoppedReason != "" {
		t.Fatalf("estado final: %+v", st)
	}
	if got := e.count(acc.ID, "c1"); got != 38 {
		t.Fatalf("mensajes del chat = %d, quería 38", got)
	}
	if st.Added != 35 || st.Imported != 35 || st.ChatsTotal != 1 || st.ChatsDone != 1 || st.Visited != 1 {
		t.Fatalf("conteos: %+v", st)
	}
	row := e.rows(acc.ID)["c1"]
	// 10 + 10 + 10 + 5 con avance, y dos vacías que completan el chat.
	if row.State != model.HistoryDone || row.Requests != 6 || row.Added != 35 || row.EmptyStreak != 2 || row.DoneAt == 0 || row.OldestTs != 1 {
		t.Fatalf("avance guardado: %+v", row)
	}
	calls := e.adapter(acc.ID).calls()
	if len(calls) != 6 {
		t.Fatalf("peticiones: %+v", calls)
	}
	if !calls[0].hasRef || calls[0].beforeID != "new-0" || calls[0].count != 10 {
		t.Fatalf("la primera petición parte del mensaje más antiguo guardado: %+v", calls[0])
	}
	// Cada petición parte del más antiguo traído por la anterior.
	if calls[1].beforeID != "c1-old-26" || calls[2].beforeID != "c1-old-16" {
		t.Fatalf("anclas: %+v", calls)
	}
	// Un chat completo no se vuelve a pedir.
	e.start(acc.ID, HistoryOptions{})
	e.waitIdle(acc.ID)
	if n := len(e.adapter(acc.ID).calls()); n != 6 {
		t.Fatalf("un chat completo se volvió a pedir: %d peticiones", n)
	}
}

func TestHistoryDirectChatsBeforeGroups(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "g-reciente", model.ChatGroup, 9000)
	e.chat(acc.ID, "d-viejo", model.ChatDirect, 100)
	e.chat(acc.ID, "d-nuevo", model.ChatDirect, 500)
	e.chat(acc.ID, "g-viejo", model.ChatGroup, 50)
	e.setScript(newArchive(true).script())

	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	var order []string
	seen := map[string]bool{}
	for _, c := range chatsOf(e.adapter(acc.ID).calls()) {
		if !seen[c] {
			seen[c] = true
			order = append(order, c)
		}
	}
	want := []string{"d-nuevo", "d-viejo", "g-reciente", "g-viejo"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("orden = %v, quería %v", order, want)
	}
	if st.ChatsTotal != 4 || st.ChatsDone != 4 || st.GroupsTotal != 2 || st.GroupsDone != 2 {
		t.Fatalf("conteos: %+v", st)
	}
}

func TestHistoryPrioritizeOrder(t *testing.T) {
	chats := []model.Chat{
		{NativeID: "g2", Kind: model.ChatGroup, LastMessageAt: 10},
		{NativeID: "d2", Kind: model.ChatDirect, LastMessageAt: 5},
		{NativeID: "g1", Kind: model.ChatChannel, LastMessageAt: 20},
		{NativeID: "d1", Kind: model.ChatSelf, LastMessageAt: 7},
	}
	var ids []string
	for _, c := range prioritize(chats) {
		ids = append(ids, c.NativeID)
	}
	if strings.Join(ids, ",") != "d1,d2,g1,g2" {
		t.Fatalf("orden: %v", ids)
	}
}

func TestHistoryChatWithoutMessagesStartsWithoutAnchor(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "vacio", model.ChatDirect, 100)
	ar := newArchive(true)
	ar.fill("vacio", 4)
	e.setScript(ar.script())
	e.start(acc.ID, HistoryOptions{})
	e.waitIdle(acc.ID)
	calls := e.adapter(acc.ID).calls()
	if calls[0].hasRef {
		t.Fatalf("un chat sin mensajes se pide con before=nil: %+v", calls[0])
	}
	if e.count(acc.ID, "vacio") != 4 {
		t.Fatalf("no se guardaron los mensajes")
	}
}

func TestHistoryPerChatQuotaAndResume(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "n0", 1000)
	ar := newArchive(true)
	ar.fill("c1", 40)
	e.setScript(ar.script())
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Count = 10 })

	// Primera tanda: tope de 2 peticiones por chat (solo hacia abajo).
	e.start(acc.ID, HistoryOptions{MaxPerChat: 2})
	st := e.waitIdle(acc.ID)
	row := e.rows(acc.ID)["c1"]
	if row.State != model.HistoryPending || row.Requests != 2 || row.Added != 20 || st.ChatsDone != 0 {
		t.Fatalf("tras la primera tanda: %+v %+v", row, st)
	}
	// Segunda tanda con otro servicio (como tras reiniciar la app): sigue donde se quedó.
	h2 := NewHistory(e.app, e.accounts, e.bus)
	h2.pacing = e.hist.pacing
	h2.floodPad = 0
	if err := h2.Start(context.Background(), acc.ID, HistoryOptions{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for h2.Status(acc.ID).Running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	row = e.rows(acc.ID)["c1"]
	if row.State != model.HistoryDone || row.Added != 40 {
		t.Fatalf("tras reanudar: %+v", row)
	}
	if e.count(acc.ID, "c1") != 41 {
		t.Fatalf("mensajes = %d", e.count(acc.ID, "c1"))
	}
	h2.StopAll()
}

// ---------------------------------------------------------------------------------------------
// Respuestas, silencios y errores

func TestHistoryWhatsAppStopsAfterThreeSilentRequests(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	for _, c := range []string{"a", "b", "c", "d"} {
		e.chat(acc.ID, c, model.ChatDirect, 100)
		e.msg(acc.ID, c, c+"-m", 1000)
	}
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error { return nil })
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Wait = 40 * time.Millisecond })

	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	if st.StoppedReason != "El teléfono no responde: comprueba que tenga internet y WhatsApp abierto." {
		t.Fatalf("motivo: %q", st.StoppedReason)
	}
	if n := len(e.adapter(acc.ID).calls()); n != 3 {
		t.Fatalf("debía parar tras 3 peticiones sin respuesta: %d", n)
	}
	for _, r := range e.rows(acc.ID) {
		if r.State == model.HistoryDone {
			t.Fatalf("el silencio no debe dar un chat por completo: %+v", r)
		}
	}
}

func TestHistorySyncChannelsTreatNoBatchAsEmpty(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelTelegram)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error { return nil })
	e.setPace(model.ChannelTelegram, func(p *historyPacing) { p.Wait = 30 * time.Millisecond })

	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	row := e.rows(acc.ID)["c1"]
	if st.StoppedReason != "" || row.State != model.HistoryDone || row.Requests != 2 {
		t.Fatalf("un canal síncrono sin lote es «no hay más»: %+v %+v", st, row)
	}
}

func TestHistoryFloodWaitIsHonoredAndRetried(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelTelegram)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	ar := newArchive(true)
	ar.fill("c1", 3)
	inner := ar.script()
	var floods int
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		if n == 1 {
			floods++
			return waitErr{d: 60 * time.Millisecond}
		}
		return inner(a, n, chat, before, count)
	})
	sub, cancel := e.hist.Subscribe(256)
	defer cancel()

	start := time.Now()
	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	if time.Since(start) < 60*time.Millisecond {
		t.Fatalf("no esperó lo que pidió el canal")
	}
	if floods != 1 || st.StoppedReason != "" || e.count(acc.ID, "c1") != 4 {
		t.Fatalf("floods=%d estado=%+v mensajes=%d", floods, st, e.count(acc.ID, "c1"))
	}
	sawWaiting := false
	for len(sub) > 0 {
		if (<-sub).Phase == HistoryPhaseWaiting {
			sawWaiting = true
		}
	}
	if !sawWaiting {
		t.Fatalf("debía avisar de la fase «waiting»")
	}
}

func TestHistoryFloodWaitTooLongStopsTheRun(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelTelegram)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		return waitErr{d: 2 * time.Hour}
	})
	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	if !strings.Contains(st.StoppedReason, "Telegram pidió esperar demasiado") {
		t.Fatalf("motivo: %q", st.StoppedReason)
	}
}

func TestHistoryErrorsPerChat(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "canal", model.ChatChannel, 400)
	e.chat(acc.ID, "nada", model.ChatDirect, 300)
	e.chat(acc.ID, "ok", model.ChatDirect, 200)
	e.msg(acc.ID, "ok", "m", 1000)
	e.msg(acc.ID, "nada", "m", 1000)
	e.msg(acc.ID, "canal", "m", 1000)
	inner := newArchive(true).script()
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		switch chat {
		case "canal":
			return fmt.Errorf("whatsapp: %w", port.ErrUnsupported)
		case "nada":
			return fmt.Errorf("sin mensajes 593987654321: %w", port.ErrNotFound)
		}
		return inner(a, n, chat, before, count)
	})
	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	rows := e.rows(acc.ID)
	if rows["canal"].State != model.HistorySkipped {
		t.Fatalf("canal: %+v", rows["canal"])
	}
	if rows["nada"].State != model.HistoryFailed || strings.Contains(rows["nada"].LastError, "593987654321") {
		t.Fatalf("nada (debe fallar sin guardar el número): %+v", rows["nada"])
	}
	if rows["ok"].State != model.HistoryDone {
		t.Fatalf("un chat con error no frena a los demás: %+v", rows["ok"])
	}
	if st.StoppedReason != "" || st.ChatsDone != 2 {
		t.Fatalf("estado: %+v", st)
	}
	// «failed» se reintenta en la siguiente tanda; «skipped» no.
	before := len(e.adapter(acc.ID).calls())
	e.start(acc.ID, HistoryOptions{})
	e.waitIdle(acc.ID)
	calls := e.adapter(acc.ID).calls()[before:]
	if got := chatsOf(calls); len(got) != 1 || got[0] != "nada" {
		t.Fatalf("segunda tanda: %v", got)
	}
}

func TestHistoryUnknownErrorsStopTheRun(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	for _, c := range []string{"a", "b", "c", "d", "e"} {
		e.chat(acc.ID, c, model.ChatDirect, 100)
		e.msg(acc.ID, c, c+"-m", 1000)
	}
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		return errors.New("rate-overlimit")
	})
	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	if !strings.Contains(st.StoppedReason, "WhatsApp dejó de aceptar peticiones de historial") {
		t.Fatalf("motivo: %q", st.StoppedReason)
	}
	if n := len(e.adapter(acc.ID).calls()); n != historyUnknownStop {
		t.Fatalf("peticiones: %d", n)
	}
}

func TestHistoryRepeatedRejectionsStopTheRun(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelInstagram)
	for i := 0; i < 8; i++ {
		c := fmt.Sprintf("c%d", i)
		e.chat(acc.ID, c, model.ChatDirect, int64(100-i))
		e.msg(acc.ID, c, c+"-m", 1000)
	}
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		return fmt.Errorf("instagram: pedir el historial: no permitido: %w", port.ErrInvalid)
	})
	e.setPace(model.ChannelInstagram, func(p *historyPacing) { p.MaxChats = 0 })
	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	if !strings.Contains(st.StoppedReason, "Instagram rechazó varias peticiones") {
		t.Fatalf("motivo: %q", st.StoppedReason)
	}
	if n := len(e.adapter(acc.ID).calls()); n != historyInvalidStop {
		t.Fatalf("peticiones: %d", n)
	}
}

func TestHistoryNotFoundChatsDoNotStopTheRun(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelMessenger)
	for i := 0; i < 12; i++ {
		c := fmt.Sprintf("c%02d", i)
		e.chat(acc.ID, c, model.ChatDirect, int64(100-i))
	}
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		return fmt.Errorf("messenger: todavía no hay mensajes de este chat: %w", port.ErrNotFound)
	})
	e.setPace(model.ChannelMessenger, func(p *historyPacing) { p.MaxChats = 0 })
	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	if st.StoppedReason != "" || len(e.adapter(acc.ID).calls()) != 12 {
		t.Fatalf("estado: %+v, peticiones: %d", st, len(e.adapter(acc.ID).calls()))
	}
	for _, r := range e.rows(acc.ID) {
		if r.State != model.HistoryFailed {
			t.Fatalf("debe quedar «failed» para reintentar: %+v", r)
		}
	}
}

func TestHistoryStopsWhenAccountDisconnects(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	ar := newArchive(true)
	ar.fill("c1", 100)
	inner := ar.script()
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		if n == 2 {
			inst, _ := e.accounts.get(acc.ID)
			inst.setStatus(model.AccountStatus{AccountID: acc.ID, State: model.StateDisconnected})
		}
		return inner(a, n, chat, before, count)
	})
	e.start(acc.ID, HistoryOptions{})
	st := e.waitIdle(acc.ID)
	if st.StoppedReason != "La cuenta se desconectó." {
		t.Fatalf("motivo: %q", st.StoppedReason)
	}
	if n := len(e.adapter(acc.ID).calls()); n != 2 {
		t.Fatalf("peticiones tras desconectar: %d", n)
	}
	// Con la cuenta desconectada no se puede empezar.
	if err := e.hist.Start(context.Background(), acc.ID, HistoryOptions{}); !errors.Is(err, port.ErrNotConnected) {
		t.Fatalf("Start desconectada: %v", err)
	}
}

// ---------------------------------------------------------------------------------------------
// Control: Stop, Start, opciones, Reset

func TestHistoryStopIsPromptAndResumable(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	entered := make(chan struct{}, 1)
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		return nil // no contesta: la tanda queda esperando la respuesta
	})
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Wait = 5 * time.Second })

	e.start(acc.ID, HistoryOptions{})
	<-entered
	if !e.hist.Status(acc.ID).Running {
		t.Fatal("debía estar corriendo")
	}
	t0 := time.Now()
	e.hist.Stop(acc.ID)
	if time.Since(t0) > time.Second {
		t.Fatalf("Stop tardó %v", time.Since(t0))
	}
	st := e.hist.Status(acc.ID)
	if st.Running || st.StoppedReason != "Detenido" || st.Phase != HistoryPhaseIdle {
		t.Fatalf("estado tras Stop: %+v", st)
	}
	// Se puede volver a empezar.
	e.setScript(newArchive(true).script())
	e.start(acc.ID, HistoryOptions{})
	if st := e.waitIdle(acc.ID); st.StoppedReason != "" || st.ChatsDone != 1 {
		t.Fatalf("tras reanudar: %+v", st)
	}
}

func TestHistoryStartIsIdempotentAndValidates(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	gate := make(chan struct{})
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		<-gate
		return nil
	})
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Wait = 20 * time.Millisecond })
	for i := 0; i < 3; i++ {
		e.start(acc.ID, HistoryOptions{})
	}
	eventually(t, "una petición en curso", func() bool { return len(e.adapter(acc.ID).calls()) == 1 })
	close(gate)
	e.hist.Stop(acc.ID)

	if err := e.hist.Start(context.Background(), "wa-no-existe", HistoryOptions{}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta inexistente: %v", err)
	}
	// Sin la capacidad HistoryOnDemand.
	caps := fake.FullCapabilities()
	caps.HistoryOnDemand = false
	e.adapter(acc.ID).SetCapabilities(caps)
	if err := e.hist.Start(context.Background(), acc.ID, HistoryOptions{}); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("sin HistoryOnDemand: %v", err)
	}
	if err := e.hist.RequestChat(context.Background(), acc.ID, "c1"); !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("RequestChat sin HistoryOnDemand: %v", err)
	}
}

func TestHistoryOptionsOnlyGoDown(t *testing.T) {
	base := historyPacing{Pause: 2500 * time.Millisecond, PerChat: 20, MaxChats: 25}
	got := resolveOptions(base, HistoryOptions{MaxPerChat: 5, MaxChats: 10, Pause: 4 * time.Second})
	if got.PerChat != 5 || got.MaxChats != 10 || got.Pause != 4*time.Second {
		t.Fatalf("bajar topes y subir pausa: %+v", got)
	}
	got = resolveOptions(base, HistoryOptions{MaxPerChat: 500, MaxChats: 1000, Pause: time.Millisecond})
	if got != base {
		t.Fatalf("nunca más rápido ni más que el piso: %+v", got)
	}
	// Sin tope de chats en el canal, la opción lo pone.
	got = resolveOptions(historyPacing{PerChat: 20}, HistoryOptions{MaxChats: 7})
	if got.MaxChats != 7 {
		t.Fatalf("MaxChats sin tope del canal: %+v", got)
	}
}

func TestHistoryPacingTable(t *testing.T) {
	p := defaultHistoryPacing()
	wa, tg, mg, ig := p[model.ChannelWhatsApp], p[model.ChannelTelegram], p[model.ChannelMessenger], p[model.ChannelInstagram]
	if wa.Pause != 2500*time.Millisecond || wa.PerChat != 20 || wa.Timeouts != 3 || !wa.Async || wa.Count != 50 {
		t.Fatalf("WhatsApp: %+v", wa)
	}
	if tg.Pause != time.Second || tg.Count != 100 || tg.PerChat != 40 || tg.Async {
		t.Fatalf("Telegram: %+v", tg)
	}
	for name, m := range map[string]historyPacing{"Messenger": mg, "Instagram": ig} {
		if m.Pause != 6*time.Second || m.PerChat != 8 || m.MaxChats != 25 || m.AutoEvery != 6*time.Hour {
			t.Fatalf("%s: %+v", name, m)
		}
	}
}

func TestHistoryMetaTandaLimits(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelMessenger)
	for i := 0; i < 5; i++ {
		c := fmt.Sprintf("c%d", i)
		e.chat(acc.ID, c, model.ChatDirect, int64(500-i))
		e.msg(acc.ID, c, c+"-m", 1000)
	}
	ar := newArchive(true)
	ar.fill("c0", 1000)
	e.setScript(ar.script())
	e.setPace(model.ChannelMessenger, func(p *historyPacing) { p.MaxChats = 2 })
	e.start(acc.ID, HistoryOptions{})
	e.waitIdle(acc.ID)
	seen := map[string]bool{}
	perChat := map[string]int{}
	for _, c := range e.adapter(acc.ID).calls() {
		seen[c.chat] = true
		perChat[c.chat]++
	}
	if len(seen) != 2 || !seen["c0"] || !seen["c1"] {
		t.Fatalf("una tanda toca como mucho MaxChats chats: %v", seen)
	}
	if perChat["c0"] != metaHistoryPerChat {
		t.Fatalf("peticiones por chat y tanda = %d, quería %d", perChat["c0"], metaHistoryPerChat)
	}
}

func TestHistoryResetAndStatus(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "d", model.ChatDirect, 100)
	e.chat(acc.ID, "g", model.ChatGroup, 90)
	e.setScript(newArchive(true).script())
	st := e.hist.Status(acc.ID)
	if st.Running || st.ChatsTotal != 2 || st.GroupsTotal != 1 || st.ChatsDone != 0 || st.Phase != HistoryPhaseIdle {
		t.Fatalf("estado inicial: %+v", st)
	}
	e.start(acc.ID, HistoryOptions{})
	st = e.waitIdle(acc.ID)
	if st.ChatsDone != 2 || st.GroupsDone != 1 {
		t.Fatalf("tras la tanda: %+v", st)
	}
	all := e.hist.StatusAll()
	if len(all) != 1 || all[0].AccountID != acc.ID || all[0].ChatsDone != 2 {
		t.Fatalf("StatusAll: %+v", all)
	}
	if err := e.hist.Reset(acc.ID); err != nil {
		t.Fatal(err)
	}
	st = e.hist.Status(acc.ID)
	if st.ChatsDone != 0 || st.GroupsDone != 0 || st.Imported != 0 || st.StoppedReason != "" || len(e.rows(acc.ID)) != 0 {
		t.Fatalf("tras Reset: %+v", st)
	}
	before := len(e.adapter(acc.ID).calls())
	e.start(acc.ID, HistoryOptions{})
	e.waitIdle(acc.ID)
	if len(e.adapter(acc.ID).calls()) == before {
		t.Fatal("tras Reset se debe volver a recorrer todo")
	}
	if err := e.hist.Reset("wa-no-existe"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("Reset de una cuenta inexistente: %v", err)
	}
}

// ---------------------------------------------------------------------------------------------
// «Cargar más» de un chat

func TestHistoryRequestChatWhenIdle(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "abierto", model.ChatDirect, 100)
	e.chat(acc.ID, "otro", model.ChatDirect, 200)
	e.msg(acc.ID, "abierto", "m", 10_000)
	ar := newArchive(true)
	ar.fill("abierto", 1000)
	e.setScript(ar.script())
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Count = 10 })

	if err := e.hist.RequestChat(context.Background(), acc.ID, "abierto"); err != nil {
		t.Fatal(err)
	}
	e.waitIdle(acc.ID)
	calls := e.adapter(acc.ID).calls()
	if len(calls) != historyLoadMoreRequests {
		t.Fatalf("peticiones = %d, quería %d", len(calls), historyLoadMoreRequests)
	}
	for _, c := range calls {
		if c.chat != "abierto" {
			t.Fatalf("solo debe pedir el chat abierto: %v", chatsOf(calls))
		}
	}
	if e.count(acc.ID, "abierto") != 1+10*historyLoadMoreRequests {
		t.Fatalf("mensajes: %d", e.count(acc.ID, "abierto"))
	}
	if r := e.rows(acc.ID)["abierto"]; r.State != model.HistoryPending {
		t.Fatalf("sigue pendiente: %+v", r)
	}
	if err := e.hist.RequestChat(context.Background(), acc.ID, "no-existe"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("chat inexistente: %v", err)
	}
	if err := e.hist.RequestChat(context.Background(), acc.ID, ""); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("chat vacío: %v", err)
	}
}

func TestHistoryRequestChatJumpsTheQueue(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	ar := newArchive(true)
	for i, c := range []string{"a", "b", "c"} {
		e.chat(acc.ID, c, model.ChatDirect, int64(300-i))
		e.msg(acc.ID, c, c+"-m", 10_000)
		ar.fill(c, 500)
	}
	inner := ar.script()
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		if n == 1 {
			entered <- struct{}{}
			<-gate
		}
		return inner(a, n, chat, before, count)
	})
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Count = 10 })
	e.start(acc.ID, HistoryOptions{MaxPerChat: 3})
	<-entered // la primera petición (del chat «a») está en curso
	if err := e.hist.RequestChat(context.Background(), acc.ID, "c"); err != nil {
		t.Fatal(err)
	}
	close(gate)
	e.waitIdle(acc.ID)
	got := chatsOf(e.adapter(acc.ID).calls())
	// «a» termina la petición en curso; «c» pasa delante de «b» (y de lo que le quedaba a «a»).
	if len(got) < 3 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("orden de peticiones: %v", got)
	}
	rows := e.rows(acc.ID)
	for _, c := range []string{"a", "b", "c"} {
		if rows[c].Requests == 0 {
			t.Fatalf("el chat %s quedó sin atender: %+v", c, rows)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Varias cuentas

func TestHistorySameChannelRunsOneAccountAtATime(t *testing.T) {
	e := newHistEnv(t)
	a1 := e.newAccount(model.ChannelWhatsApp)
	a2 := e.newAccount(model.ChannelWhatsApp)
	for _, a := range []model.AccountID{a1.ID, a2.ID} {
		e.chat(a, "c1", model.ChatDirect, 100)
		e.msg(a, "c1", "m", 1000)
	}
	ar := newArchive(true)
	ar.fill("c1", 30)
	inner := ar.script()
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		time.Sleep(5 * time.Millisecond)
		return inner(a, n, chat, before, count)
	})
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Count = 10 })
	e.start(a1.ID, HistoryOptions{})
	e.start(a2.ID, HistoryOptions{})
	// La segunda espera su turno.
	eventually(t, "una en cola", func() bool {
		return e.hist.Status(a1.ID).Phase == HistoryPhaseQueued || e.hist.Status(a2.ID).Phase == HistoryPhaseQueued
	})
	e.waitIdle(a1.ID)
	e.waitIdle(a2.ID)
	e.mu.Lock()
	maxAct := e.maxAct
	e.mu.Unlock()
	if maxAct != 1 {
		t.Fatalf("peticiones simultáneas del mismo canal: %d", maxAct)
	}
	for _, a := range []model.AccountID{a1.ID, a2.ID} {
		if e.count(a, "c1") != 31 {
			t.Fatalf("cuenta %s: %d mensajes", a, e.count(a, "c1"))
		}
	}
}

func TestHistoryDifferentChannelsRunTogether(t *testing.T) {
	e := newHistEnv(t)
	wa := e.newAccount(model.ChannelWhatsApp)
	tg := e.newAccount(model.ChannelTelegram)
	for _, a := range []model.AccountID{wa.ID, tg.ID} {
		e.chat(a, "c1", model.ChatDirect, 100)
		e.msg(a, "c1", "m", 1000)
	}
	both := make(chan struct{})
	var once sync.Once
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error {
		e.mu.Lock()
		act := e.active
		e.mu.Unlock()
		if act >= 2 {
			once.Do(func() { close(both) })
		}
		time.Sleep(60 * time.Millisecond)
		return nil
	})
	for _, ch := range []model.Channel{model.ChannelWhatsApp, model.ChannelTelegram} {
		e.setPace(ch, func(p *historyPacing) { p.Wait = 20 * time.Millisecond })
	}
	e.start(wa.ID, HistoryOptions{})
	e.start(tg.ID, HistoryOptions{})
	select {
	case <-both:
	case <-time.After(3 * time.Second):
		t.Fatal("canales distintos deben poder pedir a la vez")
	}
	e.hist.StopAll()
}

// ---------------------------------------------------------------------------------------------
// Avisos de estado

func TestHistoryStatusEventsAreThrottled(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	sub, cancel := e.hist.Subscribe(256)
	defer cancel()
	var cbMu sync.Mutex
	cbCount := 0
	off := e.hist.OnStatus(func(HistoryStatus) { cbMu.Lock(); cbCount++; cbMu.Unlock() })
	defer off()
	for i := 0; i < 50; i++ {
		e.hist.update(acc.ID, false, func(st *HistoryStatus) { st.Added++ })
	}
	if n := len(sub); n != 1 {
		t.Fatalf("avisos en un segundo: %d (debía ser 1)", n)
	}
	cbMu.Lock()
	if cbCount != 1 {
		t.Fatalf("callback: %d", cbCount)
	}
	cbMu.Unlock()
	// El aviso forzado (fin de tanda) pasa siempre y trae el estado más reciente.
	e.hist.update(acc.ID, true, func(*HistoryStatus) {})
	<-sub
	last := <-sub
	if last.Added != 50 || last.AccountID != acc.ID {
		t.Fatalf("aviso forzado: %+v", last)
	}
}

func TestHistoryScrubErr(t *testing.T) {
	got := scrubErr(errors.New("fallo con 593987654321@s.whatsapp.net y 12345"))
	if strings.Contains(got, "593987654321") || !strings.Contains(got, "12345") {
		t.Fatalf("scrubErr: %q", got)
	}
	long := scrubErr(errors.New(strings.Repeat("x", 500)))
	if len([]rune(long)) > 201 {
		t.Fatalf("scrubErr debe acotar el texto: %d", len(long))
	}
	if scrubErr(nil) != "" {
		t.Fatal("scrubErr(nil)")
	}
}

// ---------------------------------------------------------------------------------------------
// Automático y apagado

func TestHistoryAutoRunStartsOnConnect(t *testing.T) {
	e := newHistEnv(t)
	e.setScript(newArchive(true).script())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.hist.AutoRun(ctx)

	acc := e.newAccount(model.ChannelWhatsApp)
	// Los chats llegan después de conectar (la sincronización inicial); al cumplirse autoDelay ya están.
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	eventually(t, "tanda automática", func() bool { return len(e.adapter(acc.ID).calls()) > 0 })
	st := e.waitIdle(acc.ID)
	if st.ChatsDone != 1 {
		t.Fatalf("estado: %+v", st)
	}
	// Ya no hay nada pendiente: no se lanza otra.
	n := len(e.adapter(acc.ID).calls())
	e.hist.scheduleAuto(acc.ID, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	if len(e.adapter(acc.ID).calls()) != n {
		t.Fatal("sin chats pendientes no se debe pedir nada")
	}
}

func TestHistoryAutoRunRespectsTheSetting(t *testing.T) {
	e := newHistEnv(t)
	e.setScript(newArchive(true).script())
	_ = e.app.SetSetting(context.Background(), SettingAutoHistory, "false")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.hist.AutoRun(ctx)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	time.Sleep(150 * time.Millisecond)
	if n := len(e.adapter(acc.ID).calls()); n != 0 {
		t.Fatalf("con AutoHistory apagado no se pide nada solo: %d", n)
	}
	// Encendido, la misma cuenta sí arranca.
	_ = e.app.SetSetting(context.Background(), SettingAutoHistory, "true")
	e.hist.scheduleAuto(acc.ID, 5*time.Millisecond)
	eventually(t, "tanda automática tras encender", func() bool { return len(e.adapter(acc.ID).calls()) > 0 })
}

func TestHistoryAutoRunCountsAccountsAlreadyConnected(t *testing.T) {
	e := newHistEnv(t)
	e.setScript(newArchive(true).script())
	acc := e.newAccount(model.ChannelTelegram)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.hist.AutoRun(ctx) // la cuenta ya estaba conectada: también cuenta
	eventually(t, "tanda automática de una cuenta ya conectada", func() bool { return len(e.adapter(acc.ID).calls()) > 0 })
	e.waitIdle(acc.ID)
}

func TestHistoryAutoRunMetaRespectsAutoEvery(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelInstagram)
	for i := 0; i < 3; i++ {
		c := fmt.Sprintf("c%d", i)
		e.chat(acc.ID, c, model.ChatDirect, int64(500-i))
		e.msg(acc.ID, c, c+"-m", 1000)
	}
	e.setScript(newArchive(true).script())
	const every = 250 * time.Millisecond
	e.setPace(model.ChannelInstagram, func(p *historyPacing) { p.MaxChats = 1; p.AutoEvery = every })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t0 := time.Now()
	e.hist.AutoRun(ctx)

	// Cada tanda automática toca un solo chat y entre dos tandas pasa al menos AutoEvery.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		st := e.hist.Status(acc.ID)
		if st.ChatsDone == 3 && !st.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := e.hist.Status(acc.ID); st.ChatsDone != 3 {
		t.Fatalf("las tandas automáticas encadenadas deben completar los chats: %+v", st)
	}
	if elapsed := time.Since(t0); elapsed < 2*every {
		t.Fatalf("3 tandas con tope de 1 chat cada %v no pueden tardar %v", every, elapsed)
	}
	if last, _ := e.app.GetSetting(context.Background(), historySettingAutoAt+string(acc.ID)); last == "" {
		t.Fatal("debe guardar cuándo empezó la última tanda automática")
	}
}

func TestHistoryStopAllLeavesNothingRunning(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error { return nil })
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Wait = 30 * time.Second })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.hist.AutoRun(ctx)
	eventually(t, "tanda en curso", func() bool { return e.hist.Status(acc.ID).Running })
	e.hist.scheduleAuto("tg-otra-cuenta", time.Hour) // una programada lejana tampoco debe colgarse

	done := make(chan struct{})
	go func() { e.hist.StopAll(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StopAll no terminó")
	}
	e.hist.mu.Lock()
	runs, timers := len(e.hist.runs), len(e.hist.timers)
	e.hist.mu.Unlock()
	if runs != 0 || timers != 0 {
		t.Fatalf("quedan tandas (%d) o programadas (%d)", runs, timers)
	}
	if st := e.hist.Status(acc.ID); st.Running {
		t.Fatalf("estado tras StopAll: %+v", st)
	}
	// El servicio se puede volver a usar después.
	e.setScript(newArchive(true).script())
	e.start(acc.ID, HistoryOptions{})
	e.waitIdle(acc.ID)
}

func TestHistoryContextCancelStopsEverything(t *testing.T) {
	e := newHistEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp)
	e.chat(acc.ID, "c1", model.ChatDirect, 100)
	e.msg(acc.ID, "c1", "m", 1000)
	e.setScript(func(a *histAdapter, n int, chat string, before *model.MessageRef, count int) error { return nil })
	e.setPace(model.ChannelWhatsApp, func(p *historyPacing) { p.Wait = 30 * time.Second })
	ctx, cancel := context.WithCancel(context.Background())
	e.hist.AutoRun(ctx)
	eventually(t, "tanda en curso", func() bool { return e.hist.Status(acc.ID).Running })
	cancel() // el contexto de AutoRun detiene todo
	eventually(t, "tanda detenida", func() bool { return !e.hist.Status(acc.ID).Running })
}
