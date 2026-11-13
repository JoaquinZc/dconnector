package service

// Sincronización de historial (copia, para los cuatro canales, de lo que hacía dConnector v1 con
// WhatsApp): por cuenta y por chat se pide hacia atrás el historial al canal hasta agotarlo, de forma
// lenta, reanudable y sin bloquear la cuenta.
//
// Cómo funciona una tanda:
//   - Se toman los chats de la cuenta que aún no están completos (tabla history_sync de app.db):
//     primero los individuales y luego los grupos, cada grupo por actividad reciente.
//   - Por chat se llama a RequestHistory(chat, before=mensaje más antiguo guardado, count) y se espera
//     a que el canal responda con un history.batch (el Router lo guarda antes de publicarlo).
//   - Hubo avance si el chat tiene mensajes guardados más antiguos que antes. Tras dos peticiones
//     seguidas sin avance el chat se da por completo. El avance se guarda tras cada petición.
//   - Cada canal tiene su propio ritmo y sus propios topes (tabla historyPacingTable): nunca se va
//     más rápido que el piso del canal; las opciones solo pueden bajar los topes o subir las pausas.
//
// Nada de lo que se recibe (texto de mensajes, teléfonos, nombres) se escribe en el registro: solo
// conteos.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Fases de HistoryStatus.Phase.
const (
	HistoryPhaseIdle       = "idle"       // no corre ninguna tanda
	HistoryPhaseQueued     = "queued"     // espera su turno: otra cuenta del mismo canal está pidiendo historial
	HistoryPhaseRequesting = "requesting" // pidiendo historial al canal
	HistoryPhaseWaiting    = "waiting"    // el canal pidió esperar (límite de peticiones)
)

const (
	// historySettingAutoAt: prefijo del ajuste que guarda cuándo empezó la última tanda automática.
	historySettingAutoAt = "history_auto_at:"
	// historyLoadMoreRequests: peticiones que hace RequestChat («cargar más» del chat abierto).
	historyLoadMoreRequests = 5
	// historyEmitEvery: como mucho un aviso de estado por segundo y cuenta.
	historyEmitEvery = time.Second
	// historyCallTimeout: tope de una llamada a RequestHistory (la respuesta se espera aparte).
	historyCallTimeout = 60 * time.Second
	// historyFloodDefault: espera cuando el canal pide esperar sin decir cuánto.
	historyFloodDefault = 60 * time.Second
	// historyFloodMax: si el canal pide esperar más que esto, la tanda se detiene en vez de esperar.
	historyFloodMax = 15 * time.Minute
	// historyFloodPad: margen que se suma a la espera pedida por el canal.
	historyFloodPad = time.Second
	// historyFloodRetries: veces que se espera y se repite la misma petición antes de rendirse.
	historyFloodRetries = 3
	// historyUnknownStop: errores desconocidos seguidos tras los que se detiene la tanda.
	historyUnknownStop = 3
	// historyInvalidStop: peticiones rechazadas (ErrInvalid) seguidas tras las que se detiene la tanda.
	historyInvalidStop = 5
	// historyAutoDelay: espera tras conectar antes de la tanda automática (que llegue la sincronización inicial).
	historyAutoDelay = 30 * time.Second
	// historyChainGap: espera entre dos tandas automáticas seguidas de un canal sin tope por horas.
	historyChainGap = 5 * time.Minute
	// historyTotalsEvery: cada cuántas peticiones se recuentan los mensajes guardados de la cuenta.
	historyTotalsEvery = 20
	// historyStoreTimeout: tope de las lecturas y escrituras de avance.
	historyStoreTimeout = 10 * time.Second
)

// historyPacing: ritmo y topes de un canal. Los valores de la tabla son el piso de seguridad.
type historyPacing struct {
	Pause     time.Duration // espera mínima entre dos peticiones al canal
	PerChat   int           // peticiones por chat y tanda
	Count     int           // mensajes que se piden en cada petición
	MaxChats  int           // chats por tanda (0 = todos)
	AutoEvery time.Duration // separación mínima entre tandas automáticas de una cuenta (0 = sin tope)
	Timeouts  int           // peticiones seguidas sin respuesta tras las que se detiene la tanda
	Async     bool          // la respuesta llega después de RequestHistory (el canal contesta por evento)
	Wait      time.Duration // tope de espera de la respuesta (o de que se guarde lo ya entregado)
	Settle    time.Duration // tras un lote, espera por si llegan más de la misma respuesta
	EmptyDone int           // peticiones seguidas sin nada nuevo para dar un chat por completo
}

// Ritmo por canal (constantes con nombre; ver docs/bitacora/5-C.md).
const (
	// WhatsApp: lo mismo que v1 (~50 mensajes por petición, 2,5 s entre peticiones, 20 por chat y tanda).
	waHistoryPause   = 2500 * time.Millisecond
	waHistoryPerChat = 20
	waHistoryCount   = 50
	waHistoryWait    = 30 * time.Second
	waHistoryTimeout = 3
	// Telegram: gotd ya gestiona FLOOD_WAIT cortos; 100 mensajes por página.
	tgHistoryPause   = time.Second
	tgHistoryPerChat = 40
	tgHistoryCount   = 100
	tgHistoryWait    = 4 * time.Second
	// Messenger e Instagram: vía no oficial, con riesgo de bloqueo → muy despacio y poco.
	metaHistoryPause     = 6 * time.Second
	metaHistoryPerChat   = 8
	metaHistoryCount     = 20
	metaHistoryMaxChats  = 25
	metaHistoryAutoEvery = 6 * time.Hour
	metaHistoryWait      = 4 * time.Second
	// Comunes.
	historySettle    = 700 * time.Millisecond
	historyEmptyDone = 2
	historyTimeouts  = 3
)

// defaultHistoryPacing devuelve la tabla `pacing` del servicio.
func defaultHistoryPacing() map[model.Channel]historyPacing {
	meta := historyPacing{
		Pause: metaHistoryPause, PerChat: metaHistoryPerChat, Count: metaHistoryCount, MaxChats: metaHistoryMaxChats,
		AutoEvery: metaHistoryAutoEvery, Timeouts: historyTimeouts, Wait: metaHistoryWait,
		Settle: historySettle, EmptyDone: historyEmptyDone,
	}
	return map[model.Channel]historyPacing{
		model.ChannelWhatsApp: {
			Pause: waHistoryPause, PerChat: waHistoryPerChat, Count: waHistoryCount, Timeouts: waHistoryTimeout,
			Async: true, Wait: waHistoryWait, Settle: historySettle, EmptyDone: historyEmptyDone,
		},
		model.ChannelTelegram: {
			Pause: tgHistoryPause, PerChat: tgHistoryPerChat, Count: tgHistoryCount, Timeouts: historyTimeouts,
			Wait: tgHistoryWait, Settle: historySettle, EmptyDone: historyEmptyDone,
		},
		model.ChannelMessenger: meta,
		model.ChannelInstagram: meta,
	}
}

// HistoryOptions: ajustes de una tanda. Solo bajan los topes o suben las pausas: nunca se pide más
// rápido ni más de lo que permite el piso del canal.
type HistoryOptions struct {
	MaxPerChat int           `json:"maxPerChat"` // peticiones por chat (0 = lo del canal; solo hacia abajo)
	MaxChats   int           `json:"maxChats"`   // chats por tanda (0 = lo del canal; solo hacia abajo)
	Pause      time.Duration `json:"pause"`      // pausa entre peticiones (nanosegundos; solo hacia arriba)
	OnlyChats  []string      `json:"onlyChats"`  // si no está vacío, solo estos chats (aunque estén completos)
}

// HistoryStatus: estado de la sincronización de historial de una cuenta (se emite como history:sync).
type HistoryStatus struct {
	AccountID     model.AccountID `json:"accountId"`
	Running       bool            `json:"running"`
	Phase         string          `json:"phase"` // idle | queued | requesting | waiting
	ChatsTotal    int             `json:"chatsTotal"`
	ChatsDone     int             `json:"chatsDone"` // completos o sin posibilidad de pedir más
	GroupsTotal   int             `json:"groupsTotal"`
	GroupsDone    int             `json:"groupsDone"`
	Visited       int             `json:"visited"`       // chats tocados en la tanda en curso
	Added         int             `json:"added"`         // mensajes nuevos de la tanda en curso
	Current       string          `json:"current"`       // nombre del chat que se está pidiendo
	StoppedReason string          `json:"stoppedReason"` // por qué paró la última tanda ("" = terminó o sigue)
	Imported      int             `json:"imported"`      // mensajes traídos en total por esta función
	Messages      int             `json:"messages"`      // mensajes guardados de la cuenta
	OldestAt      int64           `json:"oldestAt"`      // ms del mensaje guardado más antiguo (0 = ninguno)
	Auto          bool            `json:"auto"`          // la tanda en curso se lanzó sola
	NextAutoAt    int64           `json:"nextAutoAt"`    // ms de la próxima tanda automática (0 = ninguna)
}

// HistoryStore: avance por chat en app.db (tabla history_sync). Lo implementa el AppStore de SQLite;
// el servicio lo descubre con una aserción de tipo (si no existe, el avance vive solo en memoria).
type HistoryStore interface {
	ListHistorySync(ctx context.Context, acc model.AccountID) ([]model.HistorySyncRow, error)
	SaveHistorySync(ctx context.Context, row model.HistorySyncRow) error
	ResetHistorySync(ctx context.Context, acc model.AccountID) error
}

// HistoryStatsStore: opcional en port.AccountStore; da el recuento y el ancla de un chat con una
// consulta en vez de recorrer páginas.
type HistoryStatsStore interface {
	ChatHistoryStats(ctx context.Context, chatNativeID string) (model.ChatHistoryStats, error)
	HistoryTotals(ctx context.Context) (messages int, oldestTs int64, err error)
}

// retryAfterer: errores del adaptador que dicen cuánto esperar (p. ej. FLOOD_WAIT de Telegram).
type retryAfterer interface{ RetryAfter() time.Duration }

// History: servicio de sincronización de historial (ver arriba).
type History struct {
	app   port.AppStore
	acc   *Accounts
	bus   *bus.Bus
	store HistoryStore

	// Ajustables en pruebas (se fijan antes de usar el servicio).
	pacing    map[model.Channel]historyPacing
	now       func() time.Time
	autoDelay time.Duration
	chainGap  time.Duration
	floodPad  time.Duration // margen que se suma a la espera que pide el canal

	mu         sync.Mutex
	base       context.Context
	baseCancel context.CancelFunc
	runs       map[model.AccountID]*historyRun
	slots      map[model.Channel]chan struct{} // una cuenta a la vez por canal
	status     map[model.AccountID]*HistoryStatus
	loaded     map[model.AccountID]bool
	lastEmit   map[model.AccountID]time.Time
	timers     map[model.AccountID]*historyTimer
	subs       map[int]chan HistoryStatus
	callbacks  map[int]func(HistoryStatus)
	nextSub    int
	autoOn     bool
	autoCancel func()
	autoStop   func() bool
	wg         sync.WaitGroup
}

// historyTimer: tanda automática programada.
type historyTimer struct {
	cancel context.CancelFunc
	at     int64
}

// historyRun: una tanda en curso de una cuenta.
type historyRun struct {
	acc    model.AccountID
	ch     model.Channel
	cancel context.CancelFunc
	done   chan struct{}
	auto   bool
	opts   HistoryOptions
	pace   historyPacing

	mu     sync.Mutex
	prio   []string        // chats pedidos con prioridad, en orden
	prioOf map[string]bool // los mismos, como conjunto
}

func (r *historyRun) addPrio(chat string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.prioOf == nil {
		r.prioOf = map[string]bool{}
	}
	if !r.prioOf[chat] {
		r.prioOf[chat] = true
		r.prio = append(r.prio, chat)
	}
}

func (r *historyRun) hasPrio() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.prio) > 0
}

func (r *historyRun) isPrio(chat string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prioOf[chat]
}

func (r *historyRun) popPrio() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.prio) == 0 {
		return "", false
	}
	c := r.prio[0]
	r.prio = r.prio[1:]
	return c, true
}

func (r *historyRun) donePrio(chat string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.prioOf, chat)
}

// NewHistory crea el servicio. app puede implementar HistoryStore para guardar el avance.
func NewHistory(app port.AppStore, acc *Accounts, b *bus.Bus) *History {
	h := &History{
		app: app, acc: acc, bus: b,
		pacing: defaultHistoryPacing(), now: time.Now,
		autoDelay: historyAutoDelay, chainGap: historyChainGap, floodPad: historyFloodPad,
		runs: map[model.AccountID]*historyRun{}, slots: map[model.Channel]chan struct{}{},
		status: map[model.AccountID]*HistoryStatus{}, loaded: map[model.AccountID]bool{},
		lastEmit: map[model.AccountID]time.Time{}, timers: map[model.AccountID]*historyTimer{},
		subs: map[int]chan HistoryStatus{}, callbacks: map[int]func(HistoryStatus){},
	}
	h.base, h.baseCancel = context.WithCancel(context.Background())
	if hs, ok := app.(HistoryStore); ok {
		h.store = hs
	} else {
		h.store = newMemHistoryStore()
	}
	return h
}

// ---------------------------------------------------------------------------------------------
// Avance en memoria (cuando el AppStore no implementa HistoryStore)

type memHistoryStore struct {
	mu   sync.Mutex
	rows map[model.AccountID]map[string]model.HistorySyncRow
}

func newMemHistoryStore() *memHistoryStore {
	return &memHistoryStore{rows: map[model.AccountID]map[string]model.HistorySyncRow{}}
}

func (m *memHistoryStore) ListHistorySync(ctx context.Context, acc model.AccountID) ([]model.HistorySyncRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.HistorySyncRow, 0, len(m.rows[acc]))
	for _, r := range m.rows[acc] {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChatNativeID < out[j].ChatNativeID })
	return out, nil
}

func (m *memHistoryStore) SaveHistorySync(ctx context.Context, row model.HistorySyncRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows[row.AccountID] == nil {
		m.rows[row.AccountID] = map[string]model.HistorySyncRow{}
	}
	m.rows[row.AccountID][row.ChatNativeID] = row
	return nil
}

func (m *memHistoryStore) ResetHistorySync(ctx context.Context, acc model.AccountID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, acc)
	return nil
}

// ---------------------------------------------------------------------------------------------
// Estado y avisos

// Subscribe devuelve un canal con cada cambio de estado (como mucho uno por segundo y cuenta, más el
// final de cada tanda) y la función que lo cancela. Un suscriptor lento pierde avisos.
func (h *History) Subscribe(buf int) (<-chan HistoryStatus, func()) {
	if buf <= 0 {
		buf = 64
	}
	ch := make(chan HistoryStatus, buf)
	h.mu.Lock()
	id := h.nextSub
	h.nextSub++
	h.subs[id] = ch
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// OnStatus registra una función que se llama con cada aviso de estado (no debe bloquear). Devuelve
// la función que la quita.
func (h *History) OnStatus(fn func(HistoryStatus)) (cancel func()) {
	h.mu.Lock()
	id := h.nextSub
	h.nextSub++
	h.callbacks[id] = fn
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.callbacks, id)
		h.mu.Unlock()
	}
}

// statusLocked devuelve el estado editable de la cuenta (lo crea si falta). Con h.mu tomado.
func (h *History) statusLocked(acc model.AccountID) *HistoryStatus {
	st := h.status[acc]
	if st == nil {
		st = &HistoryStatus{AccountID: acc, Phase: HistoryPhaseIdle}
		h.status[acc] = st
	}
	return st
}

// update cambia el estado de la cuenta y lo avisa (limitado a 1 por segundo salvo force).
func (h *History) update(acc model.AccountID, force bool, fn func(*HistoryStatus)) {
	h.mu.Lock()
	st := h.statusLocked(acc)
	fn(st)
	cp := *st
	now := h.now()
	if !force && now.Sub(h.lastEmit[acc]) < historyEmitEvery {
		h.mu.Unlock()
		return
	}
	h.lastEmit[acc] = now
	subs := make([]chan HistoryStatus, 0, len(h.subs))
	for _, c := range h.subs {
		subs = append(subs, c)
	}
	cbs := make([]func(HistoryStatus), 0, len(h.callbacks))
	for _, f := range h.callbacks {
		cbs = append(cbs, f)
	}
	// El envío va con h.mu tomado para que cancelar (que cierra el canal) no corra a la vez.
	for _, c := range subs {
		select {
		case c <- cp:
		default:
		}
	}
	h.mu.Unlock()
	for _, f := range cbs {
		safeCall("aviso de historial", func() { f(cp) })
	}
}

// Status devuelve el estado de una cuenta (con los totales leídos de los almacenes la primera vez).
func (h *History) Status(acc model.AccountID) HistoryStatus {
	h.mu.Lock()
	loaded := h.loaded[acc]
	running := h.runs[acc] != nil
	h.mu.Unlock()
	if !loaded && !running {
		ctx, cancel := context.WithTimeout(h.baseCtx(), historyStoreTimeout)
		h.refreshCounts(ctx, acc)
		cancel()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return *h.statusLocked(acc)
}

// StatusAll devuelve el estado de todas las cuentas cargadas, ordenado por id.
func (h *History) StatusAll() []HistoryStatus {
	insts := h.acc.snapshot()
	out := make([]HistoryStatus, 0, len(insts))
	for _, inst := range insts {
		out = append(out, h.Status(inst.acc.ID))
	}
	return out
}

func (h *History) baseCtx() context.Context {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.base
}

// persistCtx: contexto para guardar avance aunque la tanda se haya cancelado.
func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), historyStoreTimeout)
}

// ---------------------------------------------------------------------------------------------
// Lectura de chats y avance

// allChats lista los chats de la cuenta (también los archivados), sin repetir.
func allChats(ctx context.Context, st port.AccountStore) ([]model.Chat, error) {
	seen := map[string]bool{}
	var out []model.Chat
	for _, tab := range []string{"all", "archived"} {
		cs, err := st.ListChats(ctx, model.ChatFilter{Tab: tab, Limit: 1_000_000, IncludeEmpty: true})
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			if c.NativeID == "" || seen[c.NativeID] {
				continue
			}
			seen[c.NativeID] = true
			out = append(out, c)
		}
	}
	return out, nil
}

func isGroupKind(k model.ChatKind) bool { return k == model.ChatGroup || k == model.ChatChannel }

// prioritize ordena: individuales primero y luego grupos; en cada bloque, el más reciente primero.
func prioritize(chats []model.Chat) []model.Chat {
	out := append([]model.Chat(nil), chats...)
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := isGroupKind(out[i].Kind), isGroupKind(out[j].Kind)
		if gi != gj {
			return !gi
		}
		if out[i].LastMessageAt != out[j].LastMessageAt {
			return out[i].LastMessageAt > out[j].LastMessageAt
		}
		return out[i].NativeID < out[j].NativeID
	})
	return out
}

func rowFinished(r model.HistorySyncRow) bool {
	return r.State == model.HistoryDone || r.State == model.HistorySkipped
}

type historyCounts struct {
	total, done, groups, groupsDone, imported int
}

func countHistory(chats []model.Chat, rows map[string]model.HistorySyncRow) historyCounts {
	var c historyCounts
	for _, ch := range chats {
		c.total++
		g := isGroupKind(ch.Kind)
		if g {
			c.groups++
		}
		if r, ok := rows[ch.NativeID]; ok {
			c.imported += r.Added
			if rowFinished(r) {
				c.done++
				if g {
					c.groupsDone++
				}
			}
		}
	}
	return c
}

func (h *History) loadRows(ctx context.Context, acc model.AccountID) (map[string]model.HistorySyncRow, error) {
	list, err := h.store.ListHistorySync(ctx, acc)
	if err != nil {
		return nil, err
	}
	rows := make(map[string]model.HistorySyncRow, len(list))
	for _, r := range list {
		rows[r.ChatNativeID] = r
	}
	return rows, nil
}

// refreshCounts recalcula totales y recuento de mensajes de la cuenta desde los almacenes.
func (h *History) refreshCounts(ctx context.Context, acc model.AccountID) {
	inst, ok := h.acc.get(acc)
	if !ok {
		return
	}
	chats, err := allChats(ctx, inst.store)
	if err != nil {
		return
	}
	rows, err := h.loadRows(ctx, acc)
	if err != nil {
		return
	}
	c := countHistory(chats, rows)
	msgs, oldest := -1, int64(0)
	if hs, ok := inst.store.(HistoryStatsStore); ok {
		if n, o, err := hs.HistoryTotals(ctx); err == nil {
			msgs, oldest = n, o
		}
	}
	h.mu.Lock()
	st := h.statusLocked(acc)
	st.ChatsTotal, st.ChatsDone, st.GroupsTotal, st.GroupsDone, st.Imported = c.total, c.done, c.groups, c.groupsDone, c.imported
	if msgs >= 0 {
		st.Messages, st.OldestAt = msgs, oldest
	}
	h.loaded[acc] = true
	h.mu.Unlock()
}

// chatStats: mensajes guardados del chat y el más antiguo (con la interfaz opcional, o recorriendo páginas).
func chatStats(ctx context.Context, st port.AccountStore, chat string) (model.ChatHistoryStats, error) {
	if hs, ok := st.(HistoryStatsStore); ok {
		return hs.ChatHistoryStats(ctx, chat)
	}
	const pageSize = 200
	var out model.ChatHistoryStats
	beforeTs := int64(0)
	for p := 0; p < oldestScanPages; p++ {
		page, err := st.ListMessages(ctx, chat, beforeTs, pageSize)
		if err != nil {
			return out, err
		}
		if len(page.Messages) == 0 {
			break
		}
		if p == 0 {
			out.NewestTs = page.Messages[len(page.Messages)-1].Timestamp
		}
		out.Count += len(page.Messages)
		out.Oldest = page.Messages[0].Ref()
		if len(page.Messages) < pageSize {
			break
		}
		beforeTs = out.Oldest.Timestamp
	}
	return out, nil
}

// ---------------------------------------------------------------------------------------------
// Arranque y parada

func channelName(ch model.Channel) string {
	switch ch {
	case model.ChannelWhatsApp:
		return "WhatsApp"
	case model.ChannelTelegram:
		return "Telegram"
	case model.ChannelMessenger:
		return "Messenger"
	case model.ChannelInstagram:
		return "Instagram"
	}
	return string(ch)
}

// pacingFor devuelve el ritmo del canal; un canal desconocido recibe el más prudente (el de Meta).
func (h *History) pacingFor(ch model.Channel) historyPacing {
	if p, ok := h.pacing[ch]; ok {
		return p
	}
	return h.pacing[model.ChannelMessenger]
}

// resolveOptions aplica las opciones al ritmo del canal: solo hacia abajo en topes, solo hacia arriba en pausas.
func resolveOptions(p historyPacing, o HistoryOptions) historyPacing {
	if o.MaxPerChat > 0 && o.MaxPerChat < p.PerChat {
		p.PerChat = o.MaxPerChat
	}
	if o.MaxChats > 0 && (p.MaxChats == 0 || o.MaxChats < p.MaxChats) {
		p.MaxChats = o.MaxChats
	}
	if o.Pause > p.Pause {
		p.Pause = o.Pause
	}
	return p
}

// check valida que la cuenta pueda pedir historial ahora.
func (h *History) check(id model.AccountID) (*instance, error) {
	inst, err := h.acc.mustGet(id)
	if err != nil {
		return nil, err
	}
	if !inst.getAdapter().Capabilities(nil).HistoryOnDemand {
		return nil, fmt.Errorf("%w: %s no permite pedir historial anterior", port.ErrUnsupported, channelName(inst.acc.Channel))
	}
	if inst.getStatus().State != model.StateConnected {
		return nil, fmt.Errorf("%w: conecta la cuenta para pedir su historial", port.ErrNotConnected)
	}
	return inst, nil
}

// Start lanza una tanda manual en segundo plano. Si ya corre una en esa cuenta no hace nada. ctx solo
// se usa para validar: la tanda sigue aunque quien llamó ya haya terminado (se detiene con Stop o
// StopAll).
func (h *History) Start(ctx context.Context, id model.AccountID, opts HistoryOptions) error {
	if _, err := h.check(id); err != nil {
		return err
	}
	h.cancelTimer(id)
	_, err := h.start(id, opts, false, "")
	return err
}

// start crea la tanda y su goroutine. Devuelve false si ya había una en marcha.
func (h *History) start(id model.AccountID, opts HistoryOptions, auto bool, prio string) (bool, error) {
	inst, err := h.acc.mustGet(id)
	if err != nil {
		return false, err
	}
	h.mu.Lock()
	if h.base.Err() != nil {
		h.mu.Unlock()
		return false, fmt.Errorf("%w: el servicio de historial está detenido", port.ErrInvalid)
	}
	if h.runs[id] != nil {
		h.mu.Unlock()
		return false, nil
	}
	ctx, cancel := context.WithCancel(h.base)
	r := &historyRun{
		acc: id, ch: inst.acc.Channel, cancel: cancel, done: make(chan struct{}), auto: auto, opts: opts,
		pace: resolveOptions(h.pacingFor(inst.acc.Channel), opts),
	}
	if prio != "" {
		r.addPrio(prio)
	}
	h.runs[id] = r
	h.wg.Add(1)
	h.mu.Unlock()
	h.update(id, true, func(st *HistoryStatus) {
		*st = HistoryStatus{
			AccountID: id, Running: true, Phase: HistoryPhaseQueued, Auto: auto,
			ChatsTotal: st.ChatsTotal, ChatsDone: st.ChatsDone, GroupsTotal: st.GroupsTotal, GroupsDone: st.GroupsDone,
			Messages: st.Messages, OldestAt: st.OldestAt, Imported: st.Imported, NextAutoAt: st.NextAutoAt,
		}
	})
	if auto {
		sctx, c := persistCtx(ctx)
		_ = h.app.SetSetting(sctx, historySettingAutoAt+string(id), strconv.FormatInt(h.now().UnixMilli(), 10))
		c()
	}
	go h.execute(ctx, r)
	return true, nil
}

// Stop detiene la tanda de la cuenta (y cancela su tanda automática programada). El avance queda
// guardado: al volver a empezar se sigue donde se quedó.
func (h *History) Stop(id model.AccountID) {
	h.cancelTimer(id)
	h.mu.Lock()
	r := h.runs[id]
	h.mu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(stopWait):
		slog.Warn("la sincronización de historial tardó en detenerse", "accountId", string(id))
	}
}

// StopAll detiene todas las tandas y las programadas, y espera a que terminen (apagado de la app).
// Después el servicio se puede volver a usar.
func (h *History) StopAll() {
	h.mu.Lock()
	h.baseCancel()
	if h.autoCancel != nil {
		h.autoCancel()
		h.autoCancel = nil
	}
	if h.autoStop != nil {
		h.autoStop()
		h.autoStop = nil
	}
	h.autoOn = false
	h.mu.Unlock()
	h.wg.Wait()
	h.mu.Lock()
	if h.base.Err() != nil {
		h.base, h.baseCancel = context.WithCancel(context.Background())
	}
	h.timers = map[model.AccountID]*historyTimer{}
	for _, st := range h.status {
		st.NextAutoAt = 0
	}
	h.mu.Unlock()
}

// Reset borra el avance de la cuenta para volver a recorrer todos sus chats desde el principio.
func (h *History) Reset(id model.AccountID) error {
	if _, err := h.acc.mustGet(id); err != nil {
		return err
	}
	h.Stop(id)
	ctx, cancel := persistCtx(h.baseCtx())
	defer cancel()
	if err := h.store.ResetHistorySync(ctx, id); err != nil {
		return err
	}
	h.update(id, true, func(st *HistoryStatus) {
		st.Visited, st.Added, st.Current, st.StoppedReason = 0, 0, "", ""
		st.Phase = HistoryPhaseIdle
	})
	h.refreshCounts(ctx, id)
	h.update(id, true, func(*HistoryStatus) {})
	return nil
}

// RequestChat pide más historial de un solo chat (el que está abierto): hasta historyLoadMoreRequests
// peticiones, con prioridad sobre el resto de la tanda si hay una en curso.
func (h *History) RequestChat(ctx context.Context, id model.AccountID, chatNativeID string) error {
	inst, err := h.check(id)
	if err != nil {
		return err
	}
	if chatNativeID == "" {
		return fmt.Errorf("%w: falta el chat", port.ErrInvalid)
	}
	if _, err := inst.store.GetChat(ctx, chatNativeID); err != nil {
		return err
	}
	h.mu.Lock()
	if r := h.runs[id]; r != nil {
		r.addPrio(chatNativeID)
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()
	opts := HistoryOptions{OnlyChats: []string{chatNativeID}, MaxPerChat: historyLoadMoreRequests}
	if _, err := h.start(id, opts, false, chatNativeID); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Automático

// AutoRun deja el servicio atento a las cuentas: cuando una pasa a «conectada» y le quedan chats por
// completar, lanza una tanda en segundo plano (si el ajuste AutoHistory está encendido). No bloquea.
// Al cancelarse ctx se detiene todo.
func (h *History) AutoRun(ctx context.Context) {
	h.mu.Lock()
	if h.autoOn || h.base.Err() != nil {
		h.mu.Unlock()
		return
	}
	h.autoOn = true
	ch, cancel := h.bus.Subscribe("history", bus.DefaultBuffer)
	h.autoCancel = cancel
	h.autoStop = context.AfterFunc(ctx, h.StopAll)
	h.wg.Add(1)
	h.mu.Unlock()
	go func() {
		defer h.wg.Done()
		for ev := range ch {
			if ev.Type == model.EvAccountStatus && ev.AccountStatus != nil && ev.AccountStatus.State == model.StateConnected {
				h.scheduleAuto(ev.AccountID, h.autoDelay)
			}
		}
	}()
	for _, inst := range h.acc.snapshot() {
		if inst.getStatus().State == model.StateConnected {
			h.scheduleAuto(inst.acc.ID, h.autoDelay)
		}
	}
}

// autoEnabled lee el ajuste AutoHistory (por defecto encendido).
func (h *History) autoEnabled(ctx context.Context) bool {
	v, err := h.app.GetSetting(ctx, SettingAutoHistory)
	if err != nil {
		return false
	}
	return parseBoolSetting(v, true)
}

// scheduleAuto programa una tanda automática dentro de `delay` (si no hay ya una programada o en curso).
func (h *History) scheduleAuto(id model.AccountID, delay time.Duration) {
	h.mu.Lock()
	if h.base.Err() != nil || !h.autoOn || h.timers[id] != nil || h.runs[id] != nil {
		h.mu.Unlock()
		return
	}
	tctx, cancel := context.WithCancel(h.base)
	t := &historyTimer{cancel: cancel, at: h.now().Add(delay).UnixMilli()}
	h.timers[id] = t
	h.wg.Add(1)
	h.mu.Unlock()
	h.update(id, true, func(st *HistoryStatus) { st.NextAutoAt = t.at })
	go func() {
		defer h.wg.Done()
		defer cancel()
		fired := historySleep(tctx, delay)
		h.mu.Lock()
		if h.timers[id] == t {
			delete(h.timers, id)
		}
		h.mu.Unlock()
		h.update(id, true, func(st *HistoryStatus) {
			if st.NextAutoAt == t.at {
				st.NextAutoAt = 0
			}
		})
		if fired {
			safeCall("tanda automática de historial", func() { h.tryAuto(tctx, id) })
		}
	}()
}

// cancelTimer quita la tanda automática programada de la cuenta, si la hay.
func (h *History) cancelTimer(id model.AccountID) {
	h.mu.Lock()
	t := h.timers[id]
	h.mu.Unlock()
	if t != nil {
		t.cancel()
	}
}

// tryAuto lanza la tanda automática si se dan todas las condiciones.
func (h *History) tryAuto(ctx context.Context, id model.AccountID) {
	inst, ok := h.acc.get(id)
	if !ok || inst.getStatus().State != model.StateConnected {
		return
	}
	if !h.autoEnabled(ctx) || !inst.getAdapter().Capabilities(nil).HistoryOnDemand {
		return
	}
	pace := h.pacingFor(inst.acc.Channel)
	if pace.AutoEvery > 0 {
		if v, _ := h.app.GetSetting(ctx, historySettingAutoAt+string(id)); v != "" {
			if last, err := strconv.ParseInt(v, 10, 64); err == nil {
				if wait := time.UnixMilli(last).Add(pace.AutoEvery).Sub(h.now()); wait > 0 {
					h.scheduleAuto(id, wait)
					return
				}
			}
		}
	}
	chats, err := allChats(ctx, inst.store)
	if err != nil {
		return
	}
	rows, err := h.loadRows(ctx, id)
	if err != nil {
		return
	}
	pending := false
	for _, c := range chats {
		if r, ok := rows[c.NativeID]; !ok || !rowFinished(r) {
			pending = true
			break
		}
	}
	if !pending {
		return
	}
	if _, err := h.start(id, HistoryOptions{}, true, ""); err != nil {
		slog.Debug("no se pudo lanzar la tanda automática de historial", "accountId", string(id), "err", scrubErr(err))
	}
}

// historySleep espera d; devuelve false si se cancela antes.
func historySleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

var longDigits = regexp.MustCompile(`\d{6,}`)

// scrubErr deja el texto de un error sin números largos (teléfonos, ids de chat) para guardarlo o registrarlo.
func scrubErr(err error) string {
	if err == nil {
		return ""
	}
	s := longDigits.ReplaceAllString(err.Error(), "…")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

// ---------------------------------------------------------------------------------------------
// La tanda

// execute corre una tanda hasta el final y deja el estado final publicado.
func (h *History) execute(ctx context.Context, r *historyRun) {
	defer h.wg.Done()
	defer close(r.done)
	reason := ""
	madeProgress := false
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("pánico en la sincronización de historial", "accountId", string(r.acc), "panic", rec)
				reason = "Error interno en la sincronización del historial."
			}
		}()
		reason, madeProgress = h.runBody(ctx, r)
	}()
	h.finish(ctx, r, reason, madeProgress)
}

// finish publica el final de la tanda y, si toca, programa la siguiente automática.
func (h *History) finish(ctx context.Context, r *historyRun, reason string, progress bool) {
	pctx, cancel := persistCtx(ctx)
	h.refreshCounts(pctx, r.acc)
	cancel()
	h.mu.Lock()
	if h.runs[r.acc] == r {
		delete(h.runs, r.acc)
	}
	h.mu.Unlock()
	h.update(r.acc, true, func(st *HistoryStatus) {
		st.Running, st.Phase, st.Current, st.StoppedReason, st.Auto = false, HistoryPhaseIdle, "", reason, false
	})
	st := h.Status(r.acc)
	slog.Info("sincronización de historial terminada", "accountId", string(r.acc), "channel", string(r.ch),
		"visited", st.Visited, "added", st.Added, "chatsDone", st.ChatsDone, "chatsTotal", st.ChatsTotal, "stopped", reason != "")
	// Una tanda automática que terminó sola, con avance y con chats pendientes, encadena la siguiente.
	if r.auto && reason == "" && progress && ctx.Err() == nil && st.ChatsDone < st.ChatsTotal {
		gap := h.chainGap
		if p := r.pace.AutoEvery; p > gap {
			gap = p
		}
		h.scheduleAuto(r.acc, gap)
	}
}

// historySession: lo que usa una tanda mientras corre.
type historySession struct {
	h    *History
	r    *historyRun
	inst *instance
	st   port.AccountStore
	pace historyPacing
	sub  <-chan model.Event

	rows     map[string]model.HistorySyncRow
	chats    map[string]model.Chat
	lastReq  time.Time
	timeouts int // peticiones seguidas sin respuesta
	unknown  int // errores desconocidos seguidos
	invalid  int // peticiones rechazadas (ErrInvalid) seguidas
	requests int // peticiones de la tanda
	added    int // mensajes nuevos de la tanda
	finished int // chats que se dieron por completos en la tanda
}

// progressed: la tanda trajo mensajes o completó algún chat.
func (s *historySession) progressed() bool { return s.added > 0 || s.finished > 0 }

// runBody es la tanda: devuelve el motivo de parada ("" si terminó) y si trajo algo.
func (h *History) runBody(ctx context.Context, r *historyRun) (string, bool) {
	inst, err := h.acc.mustGet(r.acc)
	if err != nil {
		return "La cuenta ya no existe.", false
	}
	// Una cuenta a la vez por canal.
	slot := h.slot(r.ch)
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return "Detenido", false
	}
	defer func() { <-slot }()

	chats, err := allChats(ctx, inst.store)
	if err != nil {
		return "No se pudieron leer los chats de la cuenta.", false
	}
	rows, err := h.loadRows(ctx, r.acc)
	if err != nil {
		return "No se pudo leer el avance guardado del historial.", false
	}
	s := &historySession{
		h: h, r: r, inst: inst, st: inst.store, pace: r.pace, rows: rows, chats: map[string]model.Chat{},
	}
	for _, c := range chats {
		s.chats[c.NativeID] = c
	}
	cnt := countHistory(chats, rows)
	h.update(r.acc, true, func(st *HistoryStatus) {
		st.ChatsTotal, st.ChatsDone, st.GroupsTotal, st.GroupsDone, st.Imported = cnt.total, cnt.done, cnt.groups, cnt.groupsDone, cnt.imported
		st.Phase = HistoryPhaseRequesting
	})
	h.refreshTotals(ctx, r.acc)

	// Cola de la tanda: chats pendientes (o los pedidos), individuales primero.
	var queue []string
	only := map[string]bool{}
	for _, c := range r.opts.OnlyChats {
		only[c] = true
	}
	for _, c := range prioritize(chats) {
		if len(only) > 0 {
			if !only[c.NativeID] {
				continue
			}
		} else if row, ok := rows[c.NativeID]; ok && rowFinished(row) {
			continue
		}
		queue = append(queue, c.NativeID)
	}
	if r.pace.MaxChats > 0 && len(queue) > r.pace.MaxChats {
		queue = queue[:r.pace.MaxChats]
	}

	sub, cancel := h.bus.Subscribe("history:"+string(r.acc), bus.DefaultBuffer)
	defer cancel()
	s.sub = sub

	visited := map[string]bool{}
	for {
		if ctx.Err() != nil {
			return "Detenido", s.progressed()
		}
		chat, isPrio := r.popPrio()
		if !isPrio {
			if len(queue) == 0 {
				break
			}
			chat, queue = queue[0], queue[1:]
		} else {
			// Un chat pedido con prioridad que ya estaba en la cola no se repite.
			for i, q := range queue {
				if q == chat {
					queue = append(queue[:i:i], queue[i+1:]...)
					break
				}
			}
			if _, known := s.chats[chat]; !known {
				continue
			}
		}
		if visited[chat] && !isPrio {
			continue
		}
		limit := r.pace.PerChat
		if r.isPrio(chat) {
			limit = min(limit, historyLoadMoreRequests)
		}
		reason, interrupted := s.processChat(ctx, chat, limit)
		visited[chat] = true
		r.donePrio(chat)
		if reason != "" {
			return reason, s.progressed()
		}
		if interrupted {
			// Lo pedido con prioridad pasó delante: este chat sigue pendiente, vuelve al principio de la cola.
			visited[chat] = false
			queue = append([]string{chat}, queue...)
		}
	}
	return "", s.progressed()
}

// slot devuelve el semáforo del canal (una cuenta a la vez).
func (h *History) slot(ch model.Channel) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.slots[ch]
	if s == nil {
		s = make(chan struct{}, 1)
		h.slots[ch] = s
	}
	return s
}

// refreshTotals actualiza el recuento de mensajes guardados de la cuenta en el estado.
func (h *History) refreshTotals(ctx context.Context, acc model.AccountID) {
	inst, ok := h.acc.get(acc)
	if !ok {
		return
	}
	hs, ok := inst.store.(HistoryStatsStore)
	if !ok {
		return
	}
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	n, oldest, err := hs.HistoryTotals(pctx)
	if err != nil {
		return
	}
	h.update(acc, false, func(st *HistoryStatus) { st.Messages, st.OldestAt = n, oldest })
}

func (s *historySession) title(chat string) string {
	if c, ok := s.chats[chat]; ok && c.Title != "" {
		return c.Title
	}
	return "Chat"
}

func (s *historySession) save(ctx context.Context, row model.HistorySyncRow) {
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if err := s.h.store.SaveHistorySync(pctx, row); err != nil {
		slog.Warn("no se pudo guardar el avance del historial", "accountId", string(s.r.acc), "err", scrubErr(err))
		return
	}
	s.rows[row.ChatNativeID] = row
}

// gap espera lo que falta para respetar la pausa mínima entre peticiones al canal.
func (s *historySession) gap(ctx context.Context) bool {
	if s.lastReq.IsZero() {
		return ctx.Err() == nil
	}
	return historySleep(ctx, s.pace.Pause-time.Since(s.lastReq))
}

func (s *historySession) connected() bool {
	return s.inst.getStatus().State == model.StateConnected
}

// processChat pide el historial de un chat hasta `limit` peticiones. Devuelve el motivo si hay que
// detener la tanda, o interrupted=true si lo pedido con prioridad pasó delante.
func (s *historySession) processChat(ctx context.Context, chat string, limit int) (stop string, interrupted bool) {
	h, r := s.h, s.r
	name := channelName(r.ch)
	interruptible := !r.isPrio(chat) // un chat pedido con prioridad no cede el turno
	row, known := s.rows[chat]
	if !known {
		row = model.HistorySyncRow{AccountID: r.acc, ChatNativeID: chat, State: model.HistoryPending}
	}
	floods := 0
	counted := false
	for n := 0; n < limit; n++ {
		if ctx.Err() != nil {
			return "Detenido", false
		}
		if !s.connected() {
			return "La cuenta se desconectó.", false
		}
		if interruptible && r.hasPrio() {
			return "", true
		}
		if !counted {
			counted = true
			h.update(r.acc, false, func(st *HistoryStatus) {
				st.Visited++
				st.Current = s.title(chat)
				st.Phase = HistoryPhaseRequesting
			})
		}
		if !s.gap(ctx) {
			return "Detenido", false
		}
		before, err := chatStats(ctx, s.st, chat)
		if err != nil {
			row.State, row.LastError = model.HistoryFailed, scrubErr(err)
			s.save(ctx, row)
			return "", false
		}
		s.drain()
		var ref *model.MessageRef
		if before.Count > 0 {
			o := before.Oldest
			ref = &o
		}
		s.lastReq = time.Now()
		s.requests++
		rerr := s.request(ctx, chat, ref)
		s.lastReq = time.Now()
		now := h.now().UnixMilli()

		switch {
		case rerr == nil:
		case ctx.Err() != nil:
			return "Detenido", false
		case errors.Is(rerr, port.ErrUnsupported):
			row.State, row.LastError, row.LastRequestAt = model.HistorySkipped, "El canal no permite pedir más historial de este chat.", now
			s.save(ctx, row)
			s.markFinished(chat)
			return "", false
		case errors.Is(rerr, port.ErrNotConnected):
			return "La cuenta se desconectó.", false
		case errors.Is(rerr, port.ErrRateLimited):
			d := historyFloodDefault
			var ra retryAfterer
			if errors.As(rerr, &ra) && ra.RetryAfter() > 0 {
				d = ra.RetryAfter()
			}
			floods++
			if d > historyFloodMax || floods > historyFloodRetries {
				row.LastError, row.LastRequestAt = scrubErr(rerr), now
				s.save(ctx, row)
				return fmt.Sprintf("%s pidió esperar demasiado antes de seguir con el historial. Inténtalo más tarde.", name), false
			}
			h.update(r.acc, true, func(st *HistoryStatus) { st.Phase = HistoryPhaseWaiting })
			if !historySleep(ctx, d+h.floodPad) {
				return "Detenido", false
			}
			h.update(r.acc, false, func(st *HistoryStatus) { st.Phase = HistoryPhaseRequesting })
			n-- // la petición no se hizo: se repite
			continue
		case errors.Is(rerr, port.ErrNotFound):
			// Lo normal en un chat que no tiene desde dónde partir: se reintenta en otra tanda.
			row.State, row.LastError, row.LastRequestAt = model.HistoryFailed, scrubErr(rerr), now
			s.save(ctx, row)
			return "", false
		case errors.Is(rerr, port.ErrInvalid):
			// Los canales de Meta devuelven ErrInvalid para cualquier fallo que no reconocen (incluidos los
			// bloqueos): si son varios seguidos, mejor parar que seguir insistiendo.
			s.invalid++
			row.State, row.LastError, row.LastRequestAt = model.HistoryFailed, scrubErr(rerr), now
			s.save(ctx, row)
			if s.invalid >= historyInvalidStop {
				return fmt.Sprintf("%s rechazó varias peticiones de historial seguidas. Inténtalo más tarde.", name), false
			}
			return "", false
		case errors.Is(rerr, context.DeadlineExceeded):
			s.timeouts++
			row.Requests++
			row.LastRequestAt, row.LastError = now, "Sin respuesta."
			s.save(ctx, row)
			if s.timeouts >= s.pace.Timeouts {
				return s.noAnswerReason(), false
			}
			continue
		default:
			s.unknown++
			row.State, row.LastError, row.LastRequestAt = model.HistoryFailed, scrubErr(rerr), now
			s.save(ctx, row)
			if s.unknown >= historyUnknownStop {
				return fmt.Sprintf("%s dejó de aceptar peticiones de historial por ahora. Inténtalo más tarde.", name), false
			}
			return "", false
		}

		// La petición salió: se espera la respuesta.
		seen := s.await(ctx, chat)
		if ctx.Err() != nil {
			return "Detenido", false
		}
		after, err := chatStats(ctx, s.st, chat)
		if err != nil {
			row.State, row.LastError = model.HistoryFailed, scrubErr(err)
			s.save(ctx, row)
			return "", false
		}
		progress := after.Count > before.Count && (before.Count == 0 || after.Oldest.NativeID != before.Oldest.NativeID || after.Oldest.Timestamp != before.Oldest.Timestamp)
		answered := seen || progress || !s.pace.Async
		row.Requests++
		row.LastRequestAt = now
		if !answered {
			s.timeouts++
			row.LastError = "Sin respuesta."
			s.save(ctx, row)
			if s.timeouts >= s.pace.Timeouts {
				return s.noAnswerReason(), false
			}
			continue
		}
		s.timeouts, s.unknown, s.invalid = 0, 0, 0
		row.LastError = ""
		if progress {
			added := after.Count - before.Count
			row.Added += added
			s.added += added
			row.EmptyStreak = 0
			if after.Oldest.Timestamp > 0 {
				row.OldestTs = after.Oldest.Timestamp
			}
			h.update(r.acc, false, func(st *HistoryStatus) { st.Added += added; st.Imported += added })
		} else {
			row.EmptyStreak++
		}
		if row.EmptyStreak >= s.pace.EmptyDone {
			row.State, row.DoneAt = model.HistoryDone, now
			s.save(ctx, row)
			s.markFinished(chat)
			return "", false
		}
		row.State = model.HistoryPending
		s.save(ctx, row)
		if s.requests%historyTotalsEvery == 0 {
			h.refreshTotals(ctx, r.acc)
		}
	}
	return "", false
}

// markFinished cuenta un chat más como completo en el estado.
func (s *historySession) markFinished(chat string) {
	s.finished++
	g := false
	if c, ok := s.chats[chat]; ok {
		g = isGroupKind(c.Kind)
	}
	s.h.update(s.r.acc, false, func(st *HistoryStatus) {
		st.ChatsDone++
		if g {
			st.GroupsDone++
		}
	})
}

func (s *historySession) noAnswerReason() string {
	if s.r.ch == model.ChannelWhatsApp {
		return "El teléfono no responde: comprueba que tenga internet y WhatsApp abierto."
	}
	return fmt.Sprintf("%s no responde a las peticiones de historial. Inténtalo más tarde.", channelName(s.r.ch))
}

// request llama al adaptador con un tope de tiempo y sin dejar pasar un pánico.
func (s *historySession) request(ctx context.Context, chat string, ref *model.MessageRef) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("fallo interno al pedir historial: %v", rec)
		}
	}()
	rctx, cancel := context.WithTimeout(ctx, historyCallTimeout)
	defer cancel()
	return s.inst.getAdapter().RequestHistory(rctx, chat, ref, s.pace.Count)
}

// drain descarta los lotes que llegaron antes de la petición (no son su respuesta).
func (s *historySession) drain() {
	for {
		select {
		case <-s.sub:
		default:
			return
		}
	}
}

// relevantBatch: ¿el lote puede ser la respuesta a una petición de este chat? Sí si trae mensajes de
// ese chat, o si está del todo vacío (la señal de «respondió, pero no hay nada más antiguo»).
func relevantBatch(b *model.HistoryBatch, chat string) bool {
	if b == nil {
		return false
	}
	if len(b.Messages) == 0 {
		return len(b.Chats) == 0 && len(b.Persons) == 0
	}
	for _, m := range b.Messages {
		if m.ChatNativeID == chat {
			return true
		}
	}
	return false
}

// await espera la respuesta a una petición: un lote relevante (el Router lo guardó antes de publicarlo)
// o el tope de espera del canal. Tras el primer lote espera un poco más por si llegan otros de la misma
// respuesta. Devuelve si llegó algún lote relevante.
func (s *historySession) await(ctx context.Context, chat string) bool {
	timeout := time.NewTimer(s.pace.Wait)
	defer timeout.Stop()
	var settle *time.Timer
	var settleC <-chan time.Time
	seen := false
	defer func() {
		if settle != nil {
			settle.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return seen
		case <-timeout.C:
			return seen
		case <-settleC:
			return true
		case ev, ok := <-s.sub:
			if !ok {
				return seen
			}
			if ev.Type != model.EvHistoryBatch || ev.AccountID != s.r.acc || !relevantBatch(ev.History, chat) {
				continue
			}
			seen = true
			if settle == nil {
				settle = time.NewTimer(s.pace.Settle)
				settleC = settle.C
			} else {
				if !settle.Stop() {
					select {
					case <-settle.C:
					default:
					}
				}
				settle.Reset(s.pace.Settle)
			}
		}
	}
}
