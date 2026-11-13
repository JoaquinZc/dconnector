package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"dconnector/internal/buildinfo"
	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// errTokenRejected: el servidor respondió 401/403 al abrir la conexión. No se reintenta hasta que
// se edite el destino (10 §1).
var errTokenRejected = errors.New("Token rechazado")

// fatalError: un error que no se arregla reintentando.
type fatalError struct{ err error }

func (f *fatalError) Error() string { return f.err.Error() }
func (f *fatalError) Unwrap() error { return f.err }

// maxRetryAfter: lo máximo que dConnector acepta esperar por un `Retry-After` del servidor (10 §1).
const maxRetryAfter = time.Hour

// retryAfterError: el servidor respondió al abrir con un código que no es 401/403 y pidió esperar
// (cabecera `Retry-After`). `after` ya viene acotado a `maxRetryAfter`; 0 si no pidió nada válido.
type retryAfterError struct {
	after  time.Duration
	status int
}

func (e *retryAfterError) Error() string {
	return fmt.Sprintf("el servidor respondió con el código HTTP %d", e.status)
}

// parseRetryAfter lee `Retry-After`: segundos enteros o fecha HTTP. Devuelve 0 si falta, es inválido,
// negativo o ya pasó. El resultado se acota a `maxRetryAfter`.
func parseRetryAfter(h http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if v[0] >= '0' && v[0] <= '9' {
		for _, r := range v {
			if r < '0' || r > '9' {
				return 0
			}
		}
		if len(v) > 9 { // más segundos de los que se aceptan: tope
			return maxRetryAfter
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return 0
		}
		return min(time.Duration(n)*time.Second, maxRetryAfter)
	}
	t, err := http.ParseTime(v)
	if err != nil {
		return 0
	}
	if d := t.Sub(now); d > 0 {
		return min(d, maxRetryAfter)
	}
	return 0
}

// retryWait: cuánto esperar antes de reintentar. Sin `Retry-After` es la espera creciente de siempre
// (±20 %). Con él, el servidor manda: es el mínimo (tope: 1 h) y la variación es solo hacia arriba
// (0–20 %), para no quedar por debajo de lo que pidió y a la vez no sincronizar a los clientes.
func (m *Manager) retryWait(attempt int, retryAfter time.Duration) time.Duration {
	base := m.backoff(attempt)
	if retryAfter <= 0 {
		return base
	}
	retryAfter = min(retryAfter, maxRetryAfter)
	if retryAfter <= base {
		return base
	}
	return min(retryAfter+time.Duration(float64(retryAfter)*0.2*rand.Float64()), maxRetryAfter)
}

// waitText: una espera en español para el usuario; sin segundos pasados 2 minutos.
func waitText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", max(1, int(d.Round(time.Second)/time.Second)))
	case d <= 2*time.Minute:
		s := int(d.Round(time.Second) / time.Second)
		if s%60 == 0 {
			return fmt.Sprintf("%d min", s/60)
		}
		return fmt.Sprintf("%d min %d s", s/60, s%60)
	case d >= 59*time.Minute+30*time.Second:
		return "1 hora"
	default:
		return fmt.Sprintf("%d min", int(d.Round(time.Minute)/time.Minute))
	}
}

// client: la conexión de dConnector con un destino. Reconecta con espera creciente.
type client struct {
	m   *Manager
	dst model.Destination
	log *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}

	mu          sync.Mutex
	state       string
	lastErr     string
	connectedAt int64
	fatal       bool
	held        bool // esperando un `Retry-After` del servidor
}

func newClient(m *Manager, d model.Destination) *client {
	return &client{
		m: m, dst: d,
		log:   m.log.With("destinationId", d.ID),
		done:  make(chan struct{}),
		wake:  make(chan struct{}, 1),
		state: "connecting",
	}
}

func (c *client) start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	go func() {
		defer close(c.done)
		defer func() {
			if rec := recover(); rec != nil {
				c.log.Error("pánico en el cliente del destino", "panic", rec)
			}
		}()
		c.run(ctx)
	}()
	c.m.emitState(c.dst.ID)
}

// stop corta el cliente y espera a que termine (hasta `wait`).
func (c *client) stop(wait time.Duration) {
	if c.cancel != nil {
		c.cancel()
	}
	select {
	case <-c.done:
	case <-time.After(wait):
	}
}

func (c *client) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *client) isFatal() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fatal
}

func (c *client) snapshot() model.DestinationState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return model.DestinationState{
		DestinationID: c.dst.ID, State: c.state, LastError: c.lastErr, ConnectedAt: c.connectedAt,
	}
}

func (c *client) setState(state, lastErr string, connectedAt int64, fatal bool) {
	c.mu.Lock()
	changed := c.state != state || c.lastErr != lastErr || c.connectedAt != connectedAt
	c.state, c.lastErr, c.connectedAt, c.fatal = state, lastErr, connectedAt, fatal
	c.mu.Unlock()
	if changed {
		c.m.emitState(c.dst.ID)
	}
}

// backoff: 1 s, 2, 4, 8 … hasta 60 s, con ±20 % (10 §1).
// isHeld: el cliente espera porque el servidor pidió volver más tarde. Cualquier guardado de un
// destino (aunque no cambie nada) lo despierta: es la forma de «avisar» que ya se encendió en el servidor.
func (c *client) isHeld() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.held
}

func (c *client) setHeld(v bool) {
	c.mu.Lock()
	c.held = v
	c.mu.Unlock()
}

func (m *Manager) backoff(attempt int) time.Duration {
	d := m.backoffMin
	for i := 0; i < attempt && d < m.backoffMax; i++ {
		d *= 2
	}
	if d > m.backoffMax {
		d = m.backoffMax
	}
	jitter := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(d) * jitter)
}

func (c *client) run(ctx context.Context) {
	attempt := 0
	for ctx.Err() == nil {
		c.setState("connecting", c.snapshot().LastError, 0, false)
		began := time.Now()
		online, err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		var fe *fatalError
		if errors.As(err, &fe) {
			c.log.Warn("destino detenido hasta que se edite", "motivo", fe.Error())
			c.setState("error", fe.Error(), 0, true)
			<-ctx.Done()
			return
		}
		msg := "Conexión cerrada"
		if err != nil {
			msg = err.Error()
		}
		if online && time.Since(began) > 10*time.Second {
			attempt = 0
		}
		var retryAfter time.Duration
		var re *retryAfterError
		if errors.As(err, &re) {
			retryAfter = re.after
		}
		wait := c.m.retryWait(attempt, retryAfter)
		attempt++
		if re != nil && re.after > 0 {
			msg = fmt.Sprintf("El servidor pidió esperar (código HTTP %d); se reintentará en %s", re.status, waitText(wait))
		}
		c.setHeld(re != nil && re.after > 0)
		c.setState("error", msg, 0, false)
		c.log.Info("reconexión programada", "espera", wait.Round(time.Millisecond).String(), "motivo", msg)
		// Un solo temporizador por destino. Editar el destino cancela ctx (sync reinicia el cliente)
		// y la espera termina al instante.
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		c.setHeld(false)
	}
}

// ---------------------------------------------------------------------------------------------
// Conexión

// dial abre el WebSocket del destino con las cabeceras del protocolo (10 §1).
func (m *Manager) dial(ctx context.Context, d model.Destination) (*websocket.Conn, error) {
	if err := service.ValidateDestinationURL(d.URL); err != nil {
		return nil, &fatalError{err: fmt.Errorf("dirección no válida")}
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+d.Token)
	h.Set("X-DConnector-Id", m.instanceID(ctx))
	h.Set("X-DConnector-Protocol", fmt.Sprint(protocolVersion))
	h.Set("User-Agent", "dConnector/"+buildinfo.Version)
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(dctx, d.URL, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		if resp != nil {
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				return nil, &fatalError{err: errTokenRejected}
			}
			// Cualquier otro código se reintenta; si trae `Retry-After`, fija la espera mínima (10 §1).
			return nil, &retryAfterError{after: parseRetryAfter(resp.Header, time.Now()), status: resp.StatusCode}
		}
		return nil, fmt.Errorf("no se pudo conectar: %s", rootCause(err))
	}
	conn.SetReadLimit(maxFrameSize)
	return conn, nil
}

// rootCause da el motivo de fondo de un error de red, sin la dirección (que podría llevar claves).
func rootCause(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "tiempo agotado"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Err != nil {
		err = oe.Err
	}
	for {
		u := errors.Unwrap(err)
		if u == nil {
			break
		}
		err = u
	}
	return err.Error()
}

func writeText(ctx context.Context, conn *websocket.Conn, b []byte) error {
	return conn.Write(ctx, websocket.MessageText, b)
}

// closeConn cierra con el apretón de manos, pero sin esperar más de 2 s.
func closeConn(conn *websocket.Conn) {
	done := make(chan struct{})
	go func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = conn.CloseNow()
	}
}

// ---- hello ----

type helloAccount struct {
	ID           string         `json:"id"`
	Channel      string         `json:"channel"`
	Label        string         `json:"label"`
	Color        string         `json:"color"`
	State        string         `json:"state"`
	SelfID       string         `json:"selfId"`
	SelfName     string         `json:"selfName"`
	SelfHandle   string         `json:"selfHandle"`
	Capabilities capability.Set `json:"capabilities"`
}

type helloData struct {
	InstanceID    string                  `json:"instanceId"`
	Version       string                  `json:"version"`
	Protocol      int                     `json:"protocol"`
	Accounts      []helloAccount          `json:"accounts"`
	Filter        model.DestinationFilter `json:"filter"`
	AllowCommands bool                    `json:"allowCommands"`
}

// allowedAccounts: las cuentas que pasan el filtro del destino.
func (m *Manager) allowedAccounts(ctx context.Context, d model.Destination) ([]model.Account, error) {
	accs, err := m.app.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Account, 0, len(accs))
	for _, a := range accs {
		if filterAllowsAccount(d.Filter, a.ID, a.Channel) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *Manager) helloFrame(ctx context.Context, d model.Destination) (frameOut, error) {
	accs, err := m.allowedAccounts(ctx, d)
	if err != nil {
		m.log.Warn("no se pudieron leer las cuentas para el saludo", "err", err)
	}
	h := helloData{
		InstanceID: m.instanceID(ctx), Version: buildinfo.Version, Protocol: protocolVersion,
		Accounts: make([]helloAccount, 0, len(accs)), Filter: d.Filter, AllowCommands: d.AllowCommands,
	}
	if h.Filter.AccountIDs == nil {
		h.Filter.AccountIDs = []model.AccountID{}
	}
	if h.Filter.Channels == nil {
		h.Filter.Channels = []model.Channel{}
	}
	if h.Filter.Events == nil {
		h.Filter.Events = []string{}
	}
	// El alcance de contactos viaja siempre completo (destinos antiguos sin el campo = todos).
	cs := &h.Filter.ContactScope
	if cs.Mode == "" {
		cs.Mode = model.ContactScopeAll
	}
	if cs.IncludeListIDs == nil {
		cs.IncludeListIDs = []string{}
	}
	if cs.ExcludeListIDs == nil {
		cs.ExcludeListIDs = []string{}
	}
	for _, a := range accs {
		ha := helloAccount{
			ID: string(a.ID), Channel: string(a.Channel), Label: a.Label, Color: a.Color,
			State: string(m.svc.Accounts.Status(a.ID).State), SelfID: a.SelfID, SelfName: a.SelfName, SelfHandle: a.SelfHandle,
		}
		if caps, err := m.svc.Accounts.Capabilities(ctx, a.ID, ""); err == nil {
			ha.Capabilities = caps
		}
		h.Accounts = append(h.Accounts, ha)
	}
	return frameOut{V: protocolVersion, ID: model.NewID(), Kind: kindHello, TS: nowMs(), Data: h}, nil
}

// ---------------------------------------------------------------------------------------------
// Sesión: una conexión abierta

type session struct {
	m    *Manager
	c    *client
	conn *websocket.Conn
	ctx  context.Context
	stop context.CancelFunc

	errc     chan error
	failOnce sync.Once
	welcomed chan struct{}
	welcOnce sync.Once
	sem      chan struct{}
	wg       sync.WaitGroup

	mu       sync.Mutex
	inflight map[string]struct{}
	acks     []string
	events   map[string]bool // welcome.events; nil = todos
}

// session abre una conexión y la sirve hasta que se corta. online indica si llegó a enviar el hello.
func (c *client) session(ctx context.Context) (online bool, err error) {
	conn, err := c.m.dial(ctx, c.dst)
	if err != nil {
		return false, err
	}
	sctx, stop := context.WithCancel(ctx)
	s := &session{
		m: c.m, c: c, conn: conn, ctx: sctx, stop: stop,
		errc: make(chan error, 1), welcomed: make(chan struct{}),
		sem: make(chan struct{}, maxCommands), inflight: map[string]struct{}{},
	}
	defer func() {
		stop()
		closeConn(conn)
		s.wg.Wait()
	}()

	hello, err := c.m.helloFrame(sctx, c.dst)
	if err != nil {
		return false, err
	}
	if err := s.writeFrame(hello, writeTimeout); err != nil {
		return false, fmt.Errorf("no se pudo enviar el saludo: %w", err)
	}
	c.setState("online", "", nowMs(), false)

	s.wg.Add(2)
	go func() { defer s.wg.Done(); defer s.recoverLoop("lectura"); s.readLoop() }()
	go func() { defer s.wg.Done(); defer s.recoverLoop("ping"); s.pingLoop() }()

	err = s.run()
	if ctx.Err() != nil {
		return true, nil
	}
	return true, err
}

// recoverLoop: un pánico en una goroutine de la sesión la cierra (se reconecta), no tumba el proceso.
func (s *session) recoverLoop(where string) {
	if r := recover(); r != nil {
		s.c.log.Error("pánico en la conexión del destino", "where", where, "panic", r)
		s.fail(fmt.Errorf("fallo interno en la conexión"))
	}
}

// fail registra el primer error de la sesión y la cierra.
func (s *session) fail(err error) {
	s.failOnce.Do(func() {
		s.errc <- err
		s.stop()
	})
}

func (s *session) firstErr() error {
	select {
	case err := <-s.errc:
		return err
	default:
		return nil
	}
}

// run espera el welcome (hasta 10 s) y después sirve el outbox hasta que la sesión termina.
func (s *session) run() error {
	timer := time.NewTimer(s.m.welcomeWait)
	defer timer.Stop()
wait:
	for {
		select {
		case <-s.welcomed:
			break wait
		case <-timer.C:
			break wait
		case err := <-s.errc:
			return err
		case <-s.ctx.Done():
			return s.firstErr()
		}
	}
	for {
		if err := s.flush(); err != nil {
			return err
		}
		select {
		case <-s.c.wake:
		case err := <-s.errc:
			return err
		case <-s.ctx.Done():
			return s.firstErr()
		}
	}
}

func (s *session) wantedEvents() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events
}

// ---- escritura ----

func (s *session) writeRaw(b []byte, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	if err := writeText(ctx, s.conn, b); err != nil {
		err = fmt.Errorf("no se pudo escribir en la conexión: %s", rootCause(err))
		s.fail(err)
		return err
	}
	return nil
}

func (s *session) writeFrame(f frameOut, timeout time.Duration) error {
	b, err := marshalFrame(f)
	if err != nil {
		return err
	}
	return s.writeRaw(b, timeout)
}

// ---- lectura ----

func (s *session) readLoop() {
	for {
		typ, data, err := s.conn.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				s.fail(context.Canceled)
				return
			}
			if code := websocket.CloseStatus(err); code != -1 {
				s.fail(fmt.Errorf("el servidor cerró la conexión (código %d)", code))
			} else {
				s.fail(fmt.Errorf("conexión perdida: %s", rootCause(err)))
			}
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var f frameIn
		if err := json.Unmarshal(data, &f); err != nil {
			s.c.log.Debug("frame ilegible del servidor")
			continue
		}
		switch f.Kind {
		case kindWelcome:
			s.onWelcome(f)
		case kindAck:
			s.onAck(f)
		case kindCommand:
			s.onCommand(f)
		}
	}
}

func (s *session) onWelcome(f frameIn) {
	var w struct {
		Name   string   `json:"name"`
		Events []string `json:"events"`
	}
	if len(f.Data) > 0 {
		_ = json.Unmarshal(f.Data, &w)
	}
	if len(w.Events) > 0 {
		set := make(map[string]bool, len(w.Events))
		for _, e := range w.Events {
			set[e] = true
		}
		s.mu.Lock()
		s.events = set
		s.mu.Unlock()
	}
	s.welcOnce.Do(func() { close(s.welcomed) })
}

func (s *session) onAck(f frameIn) {
	var a struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(f.Data, &a); err != nil || len(a.IDs) == 0 {
		return
	}
	// Los ack los aplica la goroutine que envía (applyAcks), para que no se crucen con un envío:
	// así un evento confirmado no puede reenviarse dentro de la misma conexión.
	s.mu.Lock()
	s.acks = append(s.acks, a.IDs...)
	s.mu.Unlock()
	s.c.signal()
}

// applyAcks borra del outbox lo confirmado y libera sitio en la ventana.
func (s *session) applyAcks() {
	s.mu.Lock()
	ids := s.acks
	s.acks = nil
	s.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	if err := s.m.app.OutboxAck(s.ctx, s.c.dst.ID, ids); err != nil {
		if s.ctx.Err() == nil {
			s.c.log.Error("no se pudo confirmar el outbox", "err", err)
		}
		return
	}
	s.mu.Lock()
	for _, id := range ids {
		delete(s.inflight, id)
	}
	s.mu.Unlock()
	s.m.markDirty(s.c.dst.ID)
}

// ---- ping ----

func (s *session) pingLoop() {
	t := time.NewTicker(s.m.pingEvery)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(s.ctx, s.m.pingTimeout)
			err := s.conn.Ping(pctx)
			cancel()
			if err != nil && s.ctx.Err() == nil {
				s.fail(errors.New("el servidor no respondió al ping"))
				return
			}
		}
	}
}
