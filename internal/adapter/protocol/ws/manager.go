package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/service"
)

const (
	// stopWait: tope para cerrar todo en Stop.
	stopWait = 5 * time.Second
	// destCacheTTL: cuánto se recuerda la lista de destinos habilitados para el outbox.
	destCacheTTL = 5 * time.Second
	// accountCacheTTL: caché de las cuentas (etiqueta y canal) para los sobres.
	accountCacheTTL = 5 * time.Second
	// stateFlushEvery: cada cuánto se avisan los cambios de «pendientes».
	stateFlushEvery = time.Second
	// trimEvery, outboxMaxAge y outboxMaxRows: limpieza del outbox (10 §4).
	trimEvery     = time.Hour
	outboxMaxAge  = 7 * 24 * time.Hour
	outboxMaxRows = 50000
)

// Services: los servicios del núcleo que usan los comandos (10 §5). Son los mismos que usa la
// interfaz; el protocolo no tiene lógica propia.
type Services struct {
	Accounts  *service.Accounts
	Inbox     *service.Inbox
	Messaging *service.Messaging
	Statuses  *service.Statuses
	Contacts  *service.Contacts
}

type accEntry struct {
	acc model.Account
	at  time.Time
}

// Manager: un cliente por destino habilitado. Implementa service.OutboxWriter.
type Manager struct {
	dsts     *service.Destinations
	svc      Services
	bus      *bus.Bus // reservado: el outbox no se apoya en el bus (02 §5)
	app      port.AppStore
	log      *slog.Logger
	settings *service.Settings

	// Ajustables en pruebas (se fijan antes de Start).
	backoffMin, backoffMax time.Duration
	pingEvery, pingTimeout time.Duration
	welcomeWait            time.Duration
	httpClient             *http.Client

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	clients map[string]*client
	wg      sync.WaitGroup
	syncCh  chan struct{}

	stateMu sync.Mutex
	onState func(model.DestinationState)
	dirty   map[string]struct{}

	cacheMu   sync.Mutex
	cacheAt   time.Time
	cacheDsts []model.Destination
	accCache  map[model.AccountID]accEntry
	instance  string
	scope     scopeCache // pertenencia de contactos a listas (alcance de contactos de los destinos)

	tmu     sync.Mutex
	tickets map[model.ChatKey]*sendTicket

	umu     sync.Mutex
	uploads map[string]*upload
}

var _ service.OutboxWriter = (*Manager)(nil)

// NewManager crea el manager. Start lo pone en marcha.
func NewManager(dst *service.Destinations, svc Services, b *bus.Bus, app port.AppStore, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		dsts: dst, svc: svc, bus: b, app: app, log: log.With("component", "protocolo"),
		settings:   service.NewSettings(app),
		backoffMin: time.Second, backoffMax: 60 * time.Second,
		pingEvery: 25 * time.Second, pingTimeout: 10 * time.Second,
		welcomeWait: 10 * time.Second,
		httpClient:  &http.Client{Timeout: 60 * time.Second},
		clients:     map[string]*client{},
		syncCh:      make(chan struct{}, 1),
		dirty:       map[string]struct{}{},
		accCache:    map[model.AccountID]accEntry{},
		tickets:     map[model.ChatKey]*sendTicket{},
		uploads:     map[string]*upload{},
	}
}

// OnState registra la función que recibe cada cambio de estado de un destino (la interfaz lo
// muestra como el evento destination:state).
func (m *Manager) OnState(fn func(model.DestinationState)) {
	m.stateMu.Lock()
	m.onState = fn
	m.stateMu.Unlock()
}

// Start arranca un cliente por destino habilitado y se re-sincroniza cuando cambian los destinos.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	rctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()

	m.dsts.OnChange(func() {
		m.invalidateCache()
		m.requestSync()
	})
	m.invalidateCache() // los destinos pudieron cambiar antes de registrar OnChange

	m.goLoop(func() { m.syncLoop(rctx) })
	m.goLoop(func() { m.housekeeping(rctx) })
	m.requestSync()
}

func (m *Manager) goLoop(fn func()) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				m.log.Error("pánico en una tarea del protocolo", "panic", rec)
			}
		}()
		fn()
	}()
}

func (m *Manager) requestSync() {
	select {
	case m.syncCh <- struct{}{}:
	default:
	}
}

func (m *Manager) syncLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.syncCh:
			m.sync(ctx)
		}
	}
}

// sync deja un cliente por cada destino habilitado: arranca los nuevos, reinicia los que cambiaron
// (o los que quedaron rechazados por el token o esperando un `Retry-After`) y para los que ya no existen o se apagaron.
func (m *Manager) sync(ctx context.Context) {
	list, err := m.dsts.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			m.log.Error("no se pudieron leer los destinos", "err", err)
		}
		return
	}
	want := map[string]model.Destination{}
	for _, d := range list {
		if d.Enabled {
			want[d.ID] = d
		}
	}

	var stopped []*client
	m.mu.Lock()
	for id, c := range m.clients {
		d, ok := want[id]
		if !ok || !reflect.DeepEqual(d, c.dst) || c.isFatal() || c.isHeld() {
			stopped = append(stopped, c)
			delete(m.clients, id)
		}
	}
	m.mu.Unlock()
	for _, c := range stopped {
		c.stop(2 * time.Second)
	}
	for _, c := range stopped {
		if _, ok := want[c.dst.ID]; !ok {
			m.emitState(c.dst.ID)
		}
	}

	m.mu.Lock()
	if ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	var started []*client
	for id, d := range want {
		if _, ok := m.clients[id]; ok {
			continue
		}
		c := newClient(m, d)
		m.clients[id] = c
		started = append(started, c)
	}
	m.mu.Unlock()
	for _, c := range started {
		c.start(ctx)
	}
}

// housekeeping: aviso de «pendientes», limpieza del outbox y de las subidas caducadas.
func (m *Manager) housekeeping(ctx context.Context) {
	flush := time.NewTicker(stateFlushEvery)
	defer flush.Stop()
	trim := time.NewTicker(trimEvery)
	defer trim.Stop()
	m.trimOutbox(ctx)
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-flush.C:
			m.flushDirty()
			ticks++
			if ticks%60 == 0 {
				m.sweepUploads()
			}
		case <-trim.C:
			m.trimOutbox(ctx)
		}
	}
}

func (m *Manager) trimOutbox(ctx context.Context) {
	older := time.Now().Add(-outboxMaxAge).UnixMilli()
	n, err := m.app.OutboxTrim(ctx, older, outboxMaxRows)
	if err != nil {
		if ctx.Err() == nil {
			m.log.Error("no se pudo limpiar el outbox", "err", err)
		}
		return
	}
	if n > 0 {
		m.log.Warn("outbox: se borraron eventos viejos o sobrantes sin confirmar", "borrados", n)
	}
}

// Stop cierra todos los clientes de forma ordenada (máx. 5 s).
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.started = false
	cancel := m.cancel
	clients := make([]*client, 0, len(m.clients))
	for _, c := range m.clients {
		clients = append(clients, c)
	}
	m.clients = map[string]*client{}
	m.mu.Unlock()

	m.dsts.OnChange(nil)
	cancel()
	done := make(chan struct{})
	go func() {
		for _, c := range clients {
			<-c.done
		}
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(stopWait):
		m.log.Warn("el protocolo no terminó de cerrarse a tiempo")
	}
	m.removeAllUploads()
}

// States devuelve el estado de todos los destinos (los apagados salen como «off», con sus
// eventos pendientes).
func (m *Manager) States() []model.DestinationState {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	list, err := m.dsts.List(ctx)
	if err != nil {
		m.log.Error("no se pudieron leer los destinos", "err", err)
		return []model.DestinationState{}
	}
	out := make([]model.DestinationState, 0, len(list))
	for _, d := range list {
		out = append(out, m.stateOf(ctx, d.ID))
	}
	return out
}

// stateOf arma el estado de un destino: el del cliente (o «off») y los pendientes del outbox.
func (m *Manager) stateOf(ctx context.Context, id string) model.DestinationState {
	st := model.DestinationState{DestinationID: id, State: "off"}
	m.mu.Lock()
	c := m.clients[id]
	m.mu.Unlock()
	if c != nil {
		st = c.snapshot()
	}
	if n, err := m.app.OutboxCount(ctx, id); err == nil {
		st.Pending = n
	}
	return st
}

// emitState avisa el estado actual de un destino a quien escucha.
func (m *Manager) emitState(id string) {
	m.stateMu.Lock()
	fn := m.onState
	m.stateMu.Unlock()
	if fn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fn(m.stateOf(ctx, id))
}

func (m *Manager) markDirty(id string) {
	m.stateMu.Lock()
	m.dirty[id] = struct{}{}
	m.stateMu.Unlock()
}

func (m *Manager) flushDirty() {
	m.stateMu.Lock()
	ids := make([]string, 0, len(m.dirty))
	for id := range m.dirty {
		ids = append(ids, id)
	}
	m.dirty = map[string]struct{}{}
	m.stateMu.Unlock()
	for _, id := range ids {
		m.emitState(id)
	}
}

// Test conecta con el destino, espera a que el hello se envíe y cierra. Sirve aunque el destino
// no esté habilitado. No toca el outbox ni el estado del cliente en marcha.
func (m *Manager) Test(ctx context.Context, id string) error {
	list, err := m.dsts.List(ctx)
	if err != nil {
		return err
	}
	var d *model.Destination
	for i := range list {
		if list[i].ID == id {
			d = &list[i]
			break
		}
	}
	if d == nil {
		return fmt.Errorf("%w: destino %s", port.ErrNotFound, id)
	}
	if err := service.ValidateDestinationURL(d.URL); err != nil {
		return err
	}
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := m.dial(tctx, *d)
	if err != nil {
		return err
	}
	defer closeConn(conn)
	hello, err := m.helloFrame(tctx, *d)
	if err != nil {
		return err
	}
	b, err := marshalFrame(hello)
	if err != nil {
		return err
	}
	wctx, wcancel := context.WithTimeout(tctx, writeTimeout)
	defer wcancel()
	if err := writeText(wctx, conn, b); err != nil {
		return fmt.Errorf("no se pudo enviar el saludo: %w", err)
	}
	return nil
}

// ---- Cachés ----

func (m *Manager) invalidateCache() {
	m.cacheMu.Lock()
	m.cacheAt = time.Time{}
	m.cacheMu.Unlock()
	m.resetScopeCache()
}

// InvalidateContactScope olvida la pertenencia a listas recordada (para quien cambie listas o
// contactos y quiera que el alcance de los destinos lo note ya, sin esperar al TTL).
func (m *Manager) InvalidateContactScope() { m.resetScopeCache() }

// enabledDestinations: destinos habilitados (los que reciben eventos en su outbox).
func (m *Manager) enabledDestinations(ctx context.Context) []model.Destination {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if !m.cacheAt.IsZero() && time.Since(m.cacheAt) < destCacheTTL {
		return m.cacheDsts
	}
	list, err := m.dsts.List(ctx)
	if err != nil {
		m.log.Error("no se pudieron leer los destinos", "err", err)
		return m.cacheDsts
	}
	var en []model.Destination
	for _, d := range list {
		if d.Enabled {
			en = append(en, d)
		}
	}
	m.cacheDsts, m.cacheAt = en, time.Now()
	return en
}

// account devuelve una cuenta del almacén (con caché corta).
func (m *Manager) account(ctx context.Context, id model.AccountID) (model.Account, error) {
	m.cacheMu.Lock()
	if e, ok := m.accCache[id]; ok && time.Since(e.at) < accountCacheTTL {
		m.cacheMu.Unlock()
		return e.acc, nil
	}
	m.cacheMu.Unlock()
	acc, err := m.app.GetAccount(ctx, id)
	if err != nil {
		return model.Account{}, err
	}
	m.cacheMu.Lock()
	m.accCache[id] = accEntry{acc: acc, at: time.Now()}
	m.cacheMu.Unlock()
	return acc, nil
}

func (m *Manager) accountRef(ctx context.Context, id model.AccountID, ch model.Channel) *accountRef {
	ref := &accountRef{ID: string(id), Channel: string(ch)}
	if acc, err := m.account(ctx, id); err == nil {
		ref.Label = acc.Label
		if ref.Channel == "" {
			ref.Channel = string(acc.Channel)
		}
	}
	return ref
}

// instanceID: el id de esta instalación (X-DConnector-Id y hello).
func (m *Manager) instanceID(ctx context.Context) string {
	m.cacheMu.Lock()
	id := m.instance
	m.cacheMu.Unlock()
	if id != "" {
		return id
	}
	id = m.settings.InstanceID(ctx)
	if id != "" {
		m.cacheMu.Lock()
		m.instance = id
		m.cacheMu.Unlock()
	}
	return id
}

// signalClient despierta al cliente de un destino (si está en marcha).
func (m *Manager) signalClient(id string) {
	m.mu.Lock()
	c := m.clients[id]
	m.mu.Unlock()
	if c != nil {
		c.signal()
	}
}
