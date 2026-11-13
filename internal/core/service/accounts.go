package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

const (
	defaultBackoffMin = 2 * time.Second
	defaultBackoffMax = 5 * time.Minute
	// readyWait: cuánto espera BeginPairing a que termine el primer Start.
	readyWait = 15 * time.Second
	// stopWait: tope para que una goroutine de supervisión termine al parar.
	stopWait = 10 * time.Second
)

// instance: una cuenta cargada (06 §3).
type instance struct {
	mu      sync.Mutex
	acc     model.Account
	adapter port.ChannelAdapter
	store   port.AccountStore
	db      *sql.DB
	status  model.AccountStatus

	cancel   context.CancelFunc // nil si no corre la supervisión
	done     chan struct{}      // se cierra cuando termina la goroutine de supervisión
	ready    chan struct{}      // se cierra tras el primer Start de esta ejecución
	notify   chan struct{}      // aviso de cambio de estado (búfer 1)
	needStop bool               // la última ejecución terminó en logged_out sin parar el adaptador
}

func (i *instance) getAdapter() port.ChannelAdapter {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.adapter
}

func (i *instance) getStatus() model.AccountStatus {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.status
}

func (i *instance) setStatus(st model.AccountStatus) {
	i.mu.Lock()
	i.status = st
	i.mu.Unlock()
}

func (i *instance) wake() {
	select {
	case i.notify <- struct{}{}:
	default:
	}
}

// Accounts: instancias, ciclo de vida y vinculación (06 §3).
type Accounts struct {
	app      port.AppStore
	router   *Router
	bus      *bus.Bus
	registry map[model.Channel]port.AdapterFactory
	open     OpenAccountStore

	// Ajustables en pruebas (se fijan antes de arrancar cuentas).
	backoffMin, backoffMax time.Duration

	mu        sync.RWMutex
	instances map[model.AccountID]*instance
	failed    map[model.AccountID]model.AccountStatus // cuentas que no se pudieron cargar
	base      context.Context
	subCancel func()
	subDone   chan struct{}
}

func NewAccounts(app port.AppStore, r *Router, b *bus.Bus, reg map[model.Channel]port.AdapterFactory, open OpenAccountStore) *Accounts {
	return &Accounts{
		app: app, router: r, bus: b, registry: reg, open: open,
		backoffMin: defaultBackoffMin, backoffMax: defaultBackoffMax,
		instances: map[model.AccountID]*instance{},
		failed:    map[model.AccountID]model.AccountStatus{},
		base:      context.Background(),
	}
}

func accNowMs() int64 { return time.Now().UnixMilli() }

// ---------------------------------------------------------------------------------------------
// Escucha del bus: mantiene el estado de cada instancia

func (a *Accounts) ensureListener() {
	a.mu.Lock()
	if a.subCancel != nil {
		a.mu.Unlock()
		return
	}
	ch, cancel := a.bus.Subscribe("accounts", bus.DefaultBuffer)
	done := make(chan struct{})
	a.subCancel, a.subDone = cancel, done
	a.mu.Unlock()
	go func() {
		defer close(done)
		for ev := range ch {
			if ev.Type == model.EvAccountStatus && ev.AccountStatus != nil {
				safeCall("estado de cuenta", func() { a.applyStatus(ev.AccountID, *ev.AccountStatus) })
			}
		}
	}()
}

func (a *Accounts) stopListener() {
	a.mu.Lock()
	cancel, done := a.subCancel, a.subDone
	a.subCancel, a.subDone = nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (a *Accounts) applyStatus(id model.AccountID, st model.AccountStatus) {
	inst, ok := a.get(id)
	if !ok {
		return
	}
	st.AccountID = id
	if st.Since == 0 {
		st.Since = accNowMs()
	}
	inst.setStatus(st)
	inst.wake()
}

func (a *Accounts) get(id model.AccountID) (*instance, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	i, ok := a.instances[id]
	return i, ok
}

func (a *Accounts) mustGet(id model.AccountID) (*instance, error) {
	if i, ok := a.get(id); ok {
		return i, nil
	}
	return nil, fmt.Errorf("%w: cuenta %s", port.ErrNotFound, id)
}

// snapshot: las instancias cargadas, ordenadas por id.
func (a *Accounts) snapshot() []*instance {
	a.mu.RLock()
	out := make([]*instance, 0, len(a.instances))
	for _, i := range a.instances {
		out = append(out, i)
	}
	a.mu.RUnlock()
	sort.Slice(out, func(x, y int) bool { return out[x].acc.ID < out[y].acc.ID })
	return out
}

func (a *Accounts) baseCtx() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.base
}

// ---------------------------------------------------------------------------------------------
// Carga de instancias

func (a *Accounts) buildAdapter(acc model.Account, db *sql.DB) (port.ChannelAdapter, error) {
	factory, ok := a.registry[acc.Channel]
	if !ok || factory == nil {
		return nil, fmt.Errorf("%w: el canal %q no está disponible", port.ErrInvalid, acc.Channel)
	}
	return factory(port.AdapterDeps{
		Account: acc, DB: db, DBPath: paths.AccountDBPath(acc.ID),
		MediaDir: paths.MediaDir(acc.ID), AvatarDir: paths.AvatarDir(acc.ID),
		Log: slog.Default().With("channel", string(acc.Channel), "accountId", string(acc.ID)),
	})
}

// load crea carpetas, abre account.db, engancha el sink y construye el adaptador.
func (a *Accounts) load(acc model.Account) (*instance, error) {
	if _, ok := a.registry[acc.Channel]; !ok {
		return nil, fmt.Errorf("%w: el canal %q no está disponible", port.ErrInvalid, acc.Channel)
	}
	for _, d := range []string{paths.MediaDir(acc.ID), paths.AvatarDir(acc.ID)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("cuentas: crear carpeta: %w", err)
		}
	}
	st, db, err := a.open(acc.ID)
	if err != nil {
		return nil, fmt.Errorf("cuentas: abrir la base de la cuenta: %w", err)
	}
	a.router.Attach(acc, st)
	ad, err := a.buildAdapter(acc, db)
	if err != nil {
		a.router.Detach(acc.ID)
		_ = st.Close()
		return nil, err
	}
	inst := &instance{
		acc: acc, adapter: ad, store: st, db: db,
		status: model.AccountStatus{AccountID: acc.ID, State: model.StateDisconnected, Since: accNowMs()},
		notify: make(chan struct{}, 1),
	}
	a.mu.Lock()
	a.instances[acc.ID] = inst
	delete(a.failed, acc.ID)
	a.mu.Unlock()
	return inst, nil
}

// StartAll carga todas las cuentas y arranca las habilitadas, cada una en su goroutine.
// Una cuenta que no se pueda cargar queda con estado «error» y no impide las demás.
func (a *Accounts) StartAll(ctx context.Context) error { return a.loadAll(ctx, true) }

// LoadAll abre las cuentas (sus bases y adaptadores) SIN conectarlas: sirve para consultar los datos que ya
// existen sin hablar con ningún canal (dconnector-server --no-accounts). Aditivo.
func (a *Accounts) LoadAll(ctx context.Context) error { return a.loadAll(ctx, false) }

func (a *Accounts) loadAll(ctx context.Context, start bool) error {
	a.mu.Lock()
	a.base = ctx
	a.mu.Unlock()
	a.ensureListener()
	accs, err := a.app.ListAccounts(ctx)
	if err != nil {
		return fmt.Errorf("cuentas: listar: %w", err)
	}
	for _, acc := range accs {
		inst, ok := a.get(acc.ID)
		if !ok {
			var lerr error
			inst, lerr = a.load(acc)
			if lerr != nil {
				slog.Error("no se pudo cargar la cuenta", "accountId", string(acc.ID), "channel", string(acc.Channel), "err", lerr)
				a.mu.Lock()
				a.failed[acc.ID] = model.AccountStatus{
					AccountID: acc.ID, State: model.StateError, Since: accNowMs(),
					Detail: "No se pudo abrir la cuenta: " + lerr.Error(),
				}
				a.mu.Unlock()
				continue
			}
		}
		if start && acc.Enabled {
			a.startRun(inst)
		}
	}
	return nil
}

// StopAll para todas las cuentas (adaptadores, sinks y bases) y las descarga.
func (a *Accounts) StopAll(ctx context.Context) error {
	insts := a.snapshot()
	var wg sync.WaitGroup
	for _, inst := range insts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.stopInstance(ctx, inst)
		}()
	}
	wg.Wait()
	// Cada cola se vacía por su lado y con el mismo plazo (ctx): sumadas una tras otra podían pasar de 10 s.
	var dwg sync.WaitGroup
	for _, inst := range insts {
		dwg.Add(1)
		go func() {
			defer dwg.Done()
			a.router.detachCtx(ctx, inst.acc.ID, true)
			_ = inst.store.Close()
			a.mu.Lock()
			delete(a.instances, inst.acc.ID)
			a.mu.Unlock()
		}()
	}
	dwg.Wait()
	a.stopListener()
	return nil
}

// ---------------------------------------------------------------------------------------------
// Supervisión

// startRun arranca la goroutine que conecta la cuenta y la reintenta con espera creciente.
func (a *Accounts) startRun(inst *instance) {
	inst.mu.Lock()
	if inst.cancel != nil {
		inst.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(a.baseCtx())
	done, ready := make(chan struct{}), make(chan struct{})
	inst.cancel, inst.done, inst.ready = cancel, done, ready
	adapter := inst.adapter
	inst.status = model.AccountStatus{AccountID: inst.acc.ID, State: model.StateConnecting, Since: accNowMs()}
	inst.mu.Unlock()
	go a.supervise(ctx, inst, adapter, done, ready)
}

// stopRun para la supervisión (no toca el adaptador) y espera a que termine.
func (a *Accounts) stopRun(inst *instance) {
	inst.mu.Lock()
	cancel, done := inst.cancel, inst.done
	inst.cancel = nil
	inst.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(stopWait):
		slog.Warn("la supervisión de la cuenta tardó en terminar", "accountId", string(inst.acc.ID))
	}
}

type runOutcome int

const (
	outcomeStop runOutcome = iota
	outcomeRetry
	outcomeLoggedOut
)

func (a *Accounts) supervise(ctx context.Context, inst *instance, adapter port.ChannelAdapter, done, ready chan struct{}) {
	var readyOnce sync.Once
	markReady := func() { readyOnce.Do(func() { close(ready) }) }
	defer close(done)
	defer markReady()
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("pánico en la supervisión de la cuenta", "accountId", string(inst.acc.ID), "panic", rec)
		}
	}()

	// Tras un logged_out el adaptador quedó sin parar: se limpia antes de volver a arrancar.
	inst.mu.Lock()
	needStop := inst.needStop
	inst.needStop = false
	inst.mu.Unlock()
	if needStop {
		sctx, cancel := context.WithTimeout(context.Background(), stopWait)
		_ = safeStop(sctx, adapter)
		cancel()
	}

	delay := a.backoffMin
	for {
		if ctx.Err() != nil {
			return
		}
		sink, ok := a.router.SinkFor(inst.acc.ID)
		var err error
		if !ok {
			err = fmt.Errorf("la cuenta no está enlazada al enrutador")
		} else {
			err = safeStart(ctx, adapter, sink)
		}
		markReady()
		outcome := outcomeRetry
		detail := ""
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("no se pudo conectar la cuenta", "accountId", string(inst.acc.ID), "err", err)
			detail = "No se pudo conectar: " + err.Error()
		} else {
			outcome = a.waitOutcome(ctx, inst, &delay)
			detail = inst.getStatus().Detail
		}
		switch outcome {
		case outcomeStop:
			return
		case outcomeLoggedOut:
			// No se reintenta: se espera a BeginPairing. El adaptador se limpia al relanzar.
			inst.mu.Lock()
			inst.needStop = true
			if inst.done == done && inst.cancel != nil {
				inst.cancel()
				inst.cancel = nil
			}
			inst.mu.Unlock()
			return
		}
		// Reintento: se para el adaptador, se deja visible el error y se espera.
		sctx, cancel := context.WithTimeout(context.Background(), stopWait)
		_ = safeStop(sctx, adapter)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if detail == "" {
			detail = "Error de conexión: se reintentará"
		}
		a.emitState(inst, model.StateError, detail)
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		delay *= 2
		if delay > a.backoffMax {
			delay = a.backoffMax
		}
		inst.setStatus(model.AccountStatus{AccountID: inst.acc.ID, State: model.StateConnecting, Since: accNowMs()})
	}
}

// waitOutcome espera a que la cuenta falle, cierre sesión o se pare la supervisión.
func (a *Accounts) waitOutcome(ctx context.Context, inst *instance, delay *time.Duration) runOutcome {
	for {
		switch inst.getStatus().State {
		case model.StateConnected:
			*delay = a.backoffMin
		case model.StateError:
			return outcomeRetry
		case model.StateLoggedOut:
			return outcomeLoggedOut
		}
		select {
		case <-ctx.Done():
			return outcomeStop
		case <-inst.notify:
		}
	}
}

// safeCall ejecuta fn sin dejar que un pánico tumbe la goroutine que la llama (02 §6).
func safeCall(what string, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("pánico recuperado", "where", what, "panic", rec)
		}
	}()
	fn()
}

func safeStart(ctx context.Context, ad port.ChannelAdapter, sink port.EventSink) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("fallo interno al conectar: %v", rec)
		}
	}()
	return ad.Start(ctx, sink)
}

func safeStop(ctx context.Context, ad port.ChannelAdapter) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("fallo interno al parar: %v", rec)
		}
	}()
	return ad.Stop(ctx)
}

// emitState fija el estado local y lo emite por el sink de la cuenta (se persiste y publica
// igual que lo que emite el adaptador).
func (a *Accounts) emitState(inst *instance, state model.ConnState, detail string) {
	now := accNowMs()
	st := model.AccountStatus{AccountID: inst.acc.ID, State: state, Detail: detail, Since: now}
	inst.setStatus(st)
	inst.wake()
	if sink, ok := a.router.SinkFor(inst.acc.ID); ok {
		sink.Emit(model.Event{
			Type: model.EvAccountStatus, AccountID: inst.acc.ID, Channel: inst.acc.Channel, At: now,
			AccountStatus: &st,
		})
	}
}

// stopInstance para la supervisión y el adaptador y deja la cuenta «desconectada».
func (a *Accounts) stopInstance(ctx context.Context, inst *instance) {
	a.stopRun(inst)
	if err := safeStop(ctx, inst.getAdapter()); err != nil {
		slog.Warn("error al parar la cuenta", "accountId", string(inst.acc.ID), "err", err)
	}
	if st := inst.getStatus().State; st != model.StateLoggedOut {
		a.emitState(inst, model.StateDisconnected, "")
	}
}

// ---------------------------------------------------------------------------------------------
// Orden de cuentas

func (a *Accounts) loadOrder(ctx context.Context) []model.AccountID {
	raw, err := a.app.GetSetting(ctx, SettingAccountOrder)
	if err != nil || raw == "" {
		return nil
	}
	var ids []model.AccountID
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return nil
	}
	return ids
}

// Reorder guarda el orden elegido en el ajuste account_order.
func (a *Accounts) Reorder(ctx context.Context, ids []model.AccountID) error {
	seen := map[model.AccountID]bool{}
	clean := make([]model.AccountID, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	return a.app.SetSetting(ctx, SettingAccountOrder, string(b))
}

func sortAccounts(accs []model.Account, order []model.AccountID) {
	pos := map[model.AccountID]int{}
	for i, id := range order {
		if _, ok := pos[id]; !ok {
			pos[id] = i
		}
	}
	sort.SliceStable(accs, func(x, y int) bool {
		px, okx := pos[accs[x].ID]
		py, oky := pos[accs[y].ID]
		switch {
		case okx && oky:
			return px < py
		case okx != oky:
			return okx // las que están en la lista, primero
		}
		if accs[x].CreatedAt != accs[y].CreatedAt {
			return accs[x].CreatedAt < accs[y].CreatedAt
		}
		return accs[x].ID < accs[y].ID
	})
}

// ---------------------------------------------------------------------------------------------
// Consulta

// List devuelve las cuentas en el orden elegido, con estado, capacidades y no leídos.
func (a *Accounts) List(ctx context.Context) ([]AccountView, error) {
	accs, err := a.app.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	sortAccounts(accs, a.loadOrder(ctx))
	out := make([]AccountView, 0, len(accs))
	for _, acc := range accs {
		v := AccountView{Account: acc, Status: a.Status(acc.ID)}
		if inst, ok := a.get(acc.ID); ok {
			v.Capabilities = inst.getAdapter().Capabilities(nil)
			v.Unread = unreadOf(ctx, inst.store)
		}
		out = append(out, v)
	}
	return out, nil
}

// unreadOf suma los no leídos de una cuenta.
func unreadOf(ctx context.Context, st port.AccountStore) int {
	chats, err := st.ListChats(ctx, model.ChatFilter{Tab: "unread", Limit: 100000, IncludeEmpty: true})
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range chats {
		if c.UnreadCount > 0 {
			n += c.UnreadCount
		}
	}
	return n
}

func (a *Accounts) Adapter(id model.AccountID) (port.ChannelAdapter, error) {
	inst, err := a.mustGet(id)
	if err != nil {
		return nil, err
	}
	return inst.getAdapter(), nil
}

func (a *Accounts) Store(id model.AccountID) (port.AccountStore, error) {
	inst, err := a.mustGet(id)
	if err != nil {
		return nil, err
	}
	return inst.store, nil
}

func (a *Accounts) Status(id model.AccountID) model.AccountStatus {
	if inst, ok := a.get(id); ok {
		return inst.getStatus()
	}
	a.mu.RLock()
	st, ok := a.failed[id]
	a.mu.RUnlock()
	if ok {
		return st
	}
	return model.AccountStatus{AccountID: id, State: model.StateDisconnected}
}

// Capabilities: de la cuenta, o del chat concreto si chatNativeID no viene vacío y el chat existe.
func (a *Accounts) Capabilities(ctx context.Context, id model.AccountID, chatNativeID string) (capability.Set, error) {
	inst, err := a.mustGet(id)
	if err != nil {
		return capability.Set{}, err
	}
	var chat *model.Chat
	if chatNativeID != "" {
		if c, err := inst.store.GetChat(ctx, chatNativeID); err == nil {
			chat = &c
		}
	}
	return inst.getAdapter().Capabilities(chat), nil
}

// ---------------------------------------------------------------------------------------------
// Altas, cambios y bajas

// Create crea la carpeta y account.db, guarda la cuenta, guarda pacing:<id>, construye el
// adaptador y lo arranca (emitirá «pairing» si no hay sesión).
func (a *Accounts) Create(ctx context.Context, ch model.Channel, label, color string, config map[string]string) (model.Account, error) {
	if _, ok := a.registry[ch]; !ok {
		return model.Account{}, fmt.Errorf("%w: el canal %q no está disponible", port.ErrInvalid, ch)
	}
	a.ensureListener()
	existing, err := a.app.ListAccounts(ctx)
	if err != nil {
		return model.Account{}, err
	}
	if color == "" {
		used := make([]string, 0, len(existing))
		for _, e := range existing {
			used = append(used, e.Color)
		}
		color = nextPaletteColor(used)
	} else if color, err = normalizeColor(color); err != nil {
		return model.Account{}, err
	}
	if label == "" {
		label = defaultAccountLabel(ch)
	}
	cfg := map[string]string{}
	for k, v := range config {
		cfg[k] = v
	}
	acc := model.Account{
		ID: model.NewAccountID(ch), Channel: ch, Label: label, Color: color, Enabled: true,
		CreatedAt: accNowMs(), Config: cfg,
	}
	inst, err := a.load(acc)
	if err != nil {
		_ = os.RemoveAll(paths.AccountDir(acc.ID))
		return model.Account{}, err
	}
	rollback := func() {
		a.router.Detach(acc.ID)
		_ = inst.store.Close()
		a.mu.Lock()
		delete(a.instances, acc.ID)
		a.mu.Unlock()
		_ = os.RemoveAll(paths.AccountDir(acc.ID))
		_ = a.app.DeleteAccount(ctx, acc.ID)
	}
	if err := a.app.SaveAccount(ctx, acc); err != nil {
		rollback()
		return model.Account{}, fmt.Errorf("cuentas: guardar: %w", err)
	}
	pacing, _ := json.Marshal(model.DefaultPacing)
	if err := a.app.SetSetting(ctx, "pacing:"+string(acc.ID), string(pacing)); err != nil {
		rollback()
		return model.Account{}, fmt.Errorf("cuentas: guardar el ritmo de envío: %w", err)
	}
	a.startRun(inst)
	return acc, nil
}

// Update cambia nombre, color y habilitada; arranca o para la cuenta según `enabled`.
func (a *Accounts) Update(ctx context.Context, id model.AccountID, label, color string, enabled bool) error {
	inst, err := a.mustGet(id)
	if err != nil {
		return err
	}
	acc, err := a.app.GetAccount(ctx, id)
	if err != nil {
		return err
	}
	if label != "" {
		acc.Label = label
	}
	if color != "" {
		if acc.Color, err = normalizeColor(color); err != nil {
			return err
		}
	}
	wasEnabled := acc.Enabled
	acc.Enabled = enabled
	if err := a.app.SaveAccount(ctx, acc); err != nil {
		return err
	}
	inst.mu.Lock()
	inst.acc.Label, inst.acc.Color, inst.acc.Enabled = acc.Label, acc.Color, acc.Enabled
	inst.mu.Unlock()
	switch {
	case enabled && !wasEnabled:
		a.startRun(inst)
	case !enabled && wasEnabled:
		a.stopInstance(ctx, inst)
	}
	return nil
}

// SetConfig cambia la configuración del canal y reconstruye el adaptador (que recibe la
// cuenta con la configuración nueva); si la cuenta está habilitada, se vuelve a conectar.
func (a *Accounts) SetConfig(ctx context.Context, id model.AccountID, config map[string]string) error {
	inst, err := a.mustGet(id)
	if err != nil {
		return err
	}
	acc, err := a.app.GetAccount(ctx, id)
	if err != nil {
		return err
	}
	cfg := map[string]string{}
	for k, v := range config {
		cfg[k] = v
	}
	acc.Config = cfg
	if err := a.app.SaveAccount(ctx, acc); err != nil {
		return err
	}
	a.stopInstance(ctx, inst)
	ad, err := a.buildAdapter(acc, inst.db)
	if err != nil {
		return err
	}
	inst.mu.Lock()
	inst.adapter = ad
	inst.acc.Config = cfg
	inst.mu.Unlock()
	if acc.Enabled {
		a.startRun(inst)
	}
	return nil
}

// Connect arranca la conexión de la cuenta (si no corre ya).
func (a *Accounts) Connect(ctx context.Context, id model.AccountID) error {
	inst, err := a.mustGet(id)
	if err != nil {
		return err
	}
	a.startRun(inst)
	return nil
}

// Disconnect para la conexión de la cuenta (sigue habilitada: se vuelve a conectar al abrir la app).
func (a *Accounts) Disconnect(ctx context.Context, id model.AccountID) error {
	inst, err := a.mustGet(id)
	if err != nil {
		return err
	}
	a.stopInstance(ctx, inst)
	return nil
}

// BeginPairing arranca el adaptador si hace falta y le pide empezar la vinculación.
func (a *Accounts) BeginPairing(ctx context.Context, id model.AccountID, req model.PairRequest) error {
	inst, err := a.mustGet(id)
	if err != nil {
		return err
	}
	a.startRun(inst)
	inst.mu.Lock()
	ready := inst.ready
	inst.mu.Unlock()
	if ready != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readyWait):
		}
	}
	return inst.getAdapter().BeginPairing(ctx, req)
}

func (a *Accounts) SubmitPairing(ctx context.Context, id model.AccountID, in model.PairInput) error {
	inst, err := a.mustGet(id)
	if err != nil {
		return err
	}
	return inst.getAdapter().SubmitPairing(ctx, in)
}

// Logout cierra la sesión en el canal. El adaptador emite «logged_out» y la supervisión no
// reintenta: queda a la espera de BeginPairing.
func (a *Accounts) Logout(ctx context.Context, id model.AccountID) error {
	inst, err := a.mustGet(id)
	if err != nil {
		return err
	}
	if err := inst.getAdapter().Logout(ctx); err != nil {
		return err
	}
	inst.mu.Lock()
	running := inst.cancel != nil
	inst.mu.Unlock()
	if !running {
		a.emitState(inst, model.StateLoggedOut, "")
	}
	return nil
}

// Delete: Logout (ignora el error), Stop, desenlaza, cierra y borra la carpeta, borra la
// cuenta de app.db y sus ajustes pacing:<id> y warmup_started_at:<id>.
func (a *Accounts) Delete(ctx context.Context, id model.AccountID) error {
	inst, ok := a.get(id)
	if ok {
		lctx, cancel := context.WithTimeout(ctx, stopWait)
		_ = inst.getAdapter().Logout(lctx)
		cancel()
		a.stopRun(inst)
		if err := safeStop(ctx, inst.getAdapter()); err != nil {
			slog.Warn("error al parar la cuenta", "accountId", string(id), "err", err)
		}
		a.router.Detach(id)
		_ = inst.store.Close()
		a.mu.Lock()
		delete(a.instances, id)
		a.mu.Unlock()
	} else if _, err := a.app.GetAccount(ctx, id); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.failed, id)
	a.mu.Unlock()
	if err := os.RemoveAll(paths.AccountDir(id)); err != nil {
		slog.Warn("no se pudo borrar la carpeta de la cuenta", "accountId", string(id), "err", err)
	}
	if err := a.app.DeleteAccount(ctx, id); err != nil && !errors.Is(err, port.ErrNotFound) {
		return err
	}
	// Las identidades de la cuenta cayeron en cascada: los contactos automáticos que se quedaron sin
	// ninguna se borran (los manuales y los importados se conservan).
	if n, err := cleanOrphanAutoContacts(ctx, a.app); err != nil {
		slog.Warn("no se pudieron limpiar los contactos automáticos de la cuenta", "accountId", string(id), "err", err)
	} else if n > 0 {
		slog.Info("contactos automáticos de la cuenta eliminados", "accountId", string(id), "count", n)
	}
	_ = a.app.SetSetting(ctx, "pacing:"+string(id), "")
	_ = a.app.SetSetting(ctx, "warmup_started_at:"+string(id), "")
	if order := a.loadOrder(ctx); len(order) > 0 {
		kept := order[:0:0]
		for _, o := range order {
			if o != id {
				kept = append(kept, o)
			}
		}
		_ = a.Reorder(ctx, kept)
	}
	return nil
}
