package connection

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"dconnector/internal/adapter/desktop/client"
	"dconnector/internal/adapter/desktop/localserver"
	"dconnector/internal/adapter/desktop/secret"
)

// APIVersion de la API que entiende este escritorio (docs/14 §9).
const APIVersion = 1

// Info es lo que la interfaz necesita para hablar directo con el servidor (JSON: ver DesktopService).
type Info struct {
	Mode           string `json:"mode"`
	BaseURL        string `json:"baseUrl"`
	Token          string `json:"token"`
	User           string `json:"user"`
	TLSFingerprint string `json:"tlsFingerprint"`
	ServerVersion  string `json:"serverVersion"`
}

// LocalServer es lo que el administrador necesita del lanzador del servidor local (*localserver.Launcher).
type LocalServer interface {
	Start() error
	Stop()
	WaitReady(ctx context.Context) error
	BaseURL() string
	Token() string
	Version() string
}

// State describe el estado del servidor local, para avisar a la interfaz.
type State struct {
	Mode    string
	Phase   localserver.Phase
	Err     string
	RetryIn time.Duration
}

// Options configura el administrador.
type Options struct {
	Store     *Store
	Protector secret.Protector
	// NewLocal crea el lanzador del servidor local. keep dice si «seguir en segundo plano» está activo;
	// onStatus recibe sus cambios de estado.
	NewLocal   func(keep func() bool, onStatus func(localserver.Status)) LocalServer
	ClientName string
	// LocalWait: cuánto espera ConnectionInfo a que el servidor local esté listo (45 s).
	LocalWait time.Duration
	// OnState se llama en cada cambio de estado del servidor local o de modo.
	OnState func(State)
}

// Manager es el dueño de la conexión actual: el servidor local (que lanza) o el remoto (con sesión).
type Manager struct {
	o Options

	mu       sync.Mutex
	local    LocalServer
	localErr error
	session  struct{ token, user string }
	relay    *relay
	relayFor string
	pending  *CertError
	changed  chan struct{}
	closed   bool
}

// New crea el administrador. Llame a Start para arrancar el servidor local si corresponde.
func New(o Options) *Manager {
	if o.LocalWait <= 0 {
		o.LocalWait = 45 * time.Second
	}
	if o.ClientName == "" {
		o.ClientName = "dConnector-desktop"
	}
	return &Manager{o: o, changed: make(chan struct{})}
}

// Start arranca el servidor local si el modo es local. Un servidor que falta no es un fallo de arranque: se
// informa en ConnectionInfo (la interfaz muestra el motivo).
func (m *Manager) Start() {
	if m.o.Store.Get().Mode == ModeLocal {
		m.startLocal()
	}
}

// Mode devuelve el modo actual.
func (m *Manager) Mode() string { return m.o.Store.Get().Mode }

// Changed devuelve un canal que se cierra cuando cambia el destino (modo o servidor local listo de nuevo).
// Tras cada cierre, pida otro con Changed.
func (m *Manager) Changed() <-chan struct{} { m.mu.Lock(); defer m.mu.Unlock(); return m.changed }

func (m *Manager) notify() {
	m.mu.Lock()
	old := m.changed
	m.changed = make(chan struct{})
	m.mu.Unlock()
	close(old)
}

func (m *Manager) startLocal() {
	m.mu.Lock()
	if m.local != nil || m.closed {
		m.mu.Unlock()
		return
	}
	l := m.o.NewLocal(func() bool { return m.o.Store.Get().KeepRunning }, m.onLocalStatus)
	m.local = l
	m.localErr = nil
	m.mu.Unlock()
	if err := l.Start(); err != nil {
		slog.Warn("servidor local", "error", err)
		m.mu.Lock()
		m.localErr = err
		m.mu.Unlock()
		m.emit(State{Mode: ModeLocal, Phase: localserver.PhaseStopped, Err: err.Error()})
	}
}

func (m *Manager) onLocalStatus(s localserver.Status) {
	m.emit(State{Mode: ModeLocal, Phase: s.Phase, Err: s.Err, RetryIn: s.RetryIn})
	if s.Phase == localserver.PhaseReady {
		m.notify()
	}
}

func (m *Manager) emit(s State) {
	if m.o.OnState != nil {
		m.o.OnState(s)
	}
}

func (m *Manager) stopLocal() {
	m.mu.Lock()
	l := m.local
	m.local = nil
	m.mu.Unlock()
	if l != nil {
		l.Stop()
	}
}

// Shutdown apaga lo que el escritorio lanzó (respetando «seguir en segundo plano») y cierra el reenvío.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closed = true
	r := m.relay
	m.relay = nil
	m.mu.Unlock()
	if r != nil {
		r.Close()
	}
	m.stopLocal()
}

// ---- Información de conexión ----

// Info devuelve cómo hablar con el servidor ahora mismo. En modo local espera (hasta LocalWait) a que el
// servidor esté listo; en remoto comprueba la sesión y, si caducó, vuelve a iniciar sesión con la clave guardada.
func (m *Manager) Info(ctx context.Context) (Info, error) {
	if m.o.Store.Get().Mode == ModeRemote {
		return m.remoteInfo(ctx)
	}
	return m.localInfo(ctx)
}

func (m *Manager) localInfo(ctx context.Context) (Info, error) {
	m.mu.Lock()
	l, lerr := m.local, m.localErr
	m.mu.Unlock()
	if lerr != nil {
		return Info{}, lerr
	}
	if l == nil {
		return Info{}, errors.New("el servidor local no está arrancado")
	}
	wctx, cancel := context.WithTimeout(ctx, m.o.LocalWait)
	defer cancel()
	if err := l.WaitReady(wctx); err != nil {
		return Info{}, err
	}
	return Info{Mode: ModeLocal, BaseURL: l.BaseURL(), Token: l.Token(), User: "local", ServerVersion: l.Version()}, nil
}

// transport devuelve el transporte HTTP para un servidor remoto: https con huella fijada o confianza del sistema.
func (m *Manager) transport(base string) http.RoundTripper {
	if !strings.HasPrefix(base, "https://") {
		return http.DefaultTransport
	}
	hp := HostPort(base)
	return pinnedTransport(hp, func() string { return m.o.Store.Get().TLSPins[hp] }, func(ce *CertError) {
		m.mu.Lock()
		m.pending = ce
		m.mu.Unlock()
	})
}

// certErr devuelve el error de certificado pendiente de este servidor (o nil) y lo consume.
func (m *Manager) certErr(base string, err error) error {
	var ce *CertError
	if errors.As(err, &ce) {
		return ce
	}
	m.mu.Lock()
	p := m.pending
	m.mu.Unlock()
	if p != nil && p.HostPort == HostPort(base) {
		return p
	}
	return nil
}

func (m *Manager) probeRemote(ctx context.Context, base string, tok func() string, reauth func(context.Context) error) (*client.Client, client.Health, error) {
	cl := client.New(client.Config{BaseURL: base, Token: tok, Reauth: reauth, Transport: m.transport(base), ClientName: m.o.ClientName})
	h, err := cl.Health(ctx)
	if err != nil {
		if ce := m.certErr(base, err); ce != nil {
			return cl, h, ce
		}
		return cl, h, err
	}
	m.mu.Lock()
	m.pending = nil
	m.mu.Unlock()
	if !h.OK() {
		return cl, h, userError("El servidor todavía está arrancando o apagándose. Prueba en unos segundos.")
	}
	if h.APIVersion != APIVersion {
		return cl, h, userError(fmt.Sprintf("Este servidor usa otra versión de la API (%d; esta aplicación entiende la %d). Actualiza dConnector o el servidor.", h.APIVersion, APIVersion))
	}
	return cl, h, nil
}

func (m *Manager) sessionToken() string { m.mu.Lock(); defer m.mu.Unlock(); return m.session.token }

func (m *Manager) setSession(token, user string) {
	m.mu.Lock()
	m.session.token, m.session.user = token, user
	m.mu.Unlock()
}

func (m *Manager) remotePassword(cfg Config) (string, error) {
	if cfg.Remote.Password == "" {
		return "", userError("No hay una clave guardada para el servidor remoto. Vuelve a escribirla en Ajustes.")
	}
	blob, err := base64.StdEncoding.DecodeString(cfg.Remote.Password)
	if err != nil {
		return "", userError("La clave guardada del servidor remoto está dañada. Vuelve a escribirla en Ajustes.")
	}
	plain, err := m.o.Protector.Unprotect(blob)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// relogin inicia sesión de nuevo con la clave guardada.
func (m *Manager) relogin(ctx context.Context, cl *client.Client) error {
	cfg := m.o.Store.Get()
	if cfg.Mode != ModeRemote {
		return errors.New("no hay servidor remoto configurado")
	}
	pw, err := m.remotePassword(cfg)
	if err != nil {
		return err
	}
	s, err := cl.Login(ctx, cfg.Remote.User, pw)
	if err != nil {
		return err
	}
	m.setSession(s.Token, s.User.Username)
	return nil
}

func (m *Manager) remoteInfo(ctx context.Context) (Info, error) {
	cfg := m.o.Store.Get()
	base := cfg.Remote.URL
	if base == "" {
		return Info{}, userError("No hay un servidor remoto configurado.")
	}
	var cl *client.Client
	reauth := func(ctx context.Context) error { return m.relogin(ctx, cl) }
	cl, h, err := m.probeRemote(ctx, base, m.sessionToken, reauth)
	if err != nil {
		return Info{}, err
	}
	// ¿La sesión sigue valiendo? Me() renueva sola con Reauth si da 401.
	if m.sessionToken() == "" {
		if err := m.relogin(ctx, cl); err != nil {
			return Info{}, err
		}
	} else if _, err := cl.Me(ctx); err != nil {
		return Info{}, err
	}
	m.mu.Lock()
	tok, user := m.session.token, m.session.user
	m.mu.Unlock()
	hp := HostPort(base)
	pin := cfg.TLSPins[hp]
	out := Info{Mode: ModeRemote, BaseURL: base, Token: tok, User: user, ServerVersion: h.Version}
	if pin != "" {
		out.TLSFingerprint = FormatFingerprint(pin)
		// El WebView no conoce la huella fijada: habla con un reenvío en loopback que sí la verifica.
		rl, err := m.ensureRelay(base, hp)
		if err != nil {
			return Info{}, fmt.Errorf("no se pudo preparar la conexión segura: %w", err)
		}
		out.BaseURL = rl.URL()
	}
	return out, nil
}

func (m *Manager) ensureRelay(base, hp string) (*relay, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.relay != nil && m.relayFor == base {
		return m.relay, nil
	}
	if m.relay != nil {
		m.relay.Close()
		m.relay = nil
	}
	tr := pinnedTransport(hp, func() string { return m.o.Store.Get().TLSPins[hp] }, func(ce *CertError) { m.pending = ce })
	r, err := newRelay(base, tr)
	if err != nil {
		return nil, err
	}
	m.relay, m.relayFor = r, base
	return r, nil
}

// Client devuelve un cliente del servidor actual para uso del propio escritorio (eventos, avisos, ajustes).
func (m *Manager) Client(ctx context.Context) (*client.Client, Info, error) {
	info, err := m.Info(ctx)
	if err != nil {
		return nil, info, err
	}
	if info.Mode == ModeLocal {
		return client.New(client.Config{BaseURL: info.BaseURL, Token: func() string { return info.Token }, ClientName: m.o.ClientName}), info, nil
	}
	cfg := m.o.Store.Get()
	var cl *client.Client
	cl = client.New(client.Config{
		BaseURL: cfg.Remote.URL, Token: m.sessionToken, Transport: m.transport(cfg.Remote.URL), ClientName: m.o.ClientName,
		Reauth: func(ctx context.Context) error { return m.relogin(ctx, cl) },
	})
	return cl, info, nil
}

// ---- Cambios de configuración ----

// SaveRemote prueba /health y el login contra el servidor y, solo si ambos van bien, guarda la conexión
// remota (la clave cifrada con DPAPI) y pasa a modo remoto.
func (m *Manager) SaveRemote(ctx context.Context, rawURL, user, password string) error {
	base, err := NormalizeURL(rawURL)
	if err != nil {
		return err
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return userError("Escribe el usuario.")
	}
	if password == "" {
		return userError("Escribe la clave.")
	}
	cl, _, err := m.probeRemote(ctx, base, func() string { return "" }, nil)
	if err != nil {
		return err
	}
	s, err := cl.Login(ctx, user, password)
	if err != nil {
		return err
	}
	blob, err := m.o.Protector.Protect([]byte(password))
	if err != nil {
		return err
	}
	if err := m.o.Store.Update(func(c *Config) {
		c.Mode = ModeRemote
		c.Remote = RemoteConfig{URL: base, User: user, Password: base64.StdEncoding.EncodeToString(blob)}
	}); err != nil {
		return err
	}
	m.setSession(s.Token, s.User.Username)
	m.mu.Lock()
	if m.relay != nil && m.relayFor != base {
		m.relay.Close()
		m.relay = nil
	}
	m.mu.Unlock()
	m.stopLocal()
	m.emit(State{Mode: ModeRemote})
	m.notify()
	return nil
}

// UseLocal vuelve al servidor local (lo arranca si hace falta) y olvida la clave remota guardada.
func (m *Manager) UseLocal(ctx context.Context) error {
	if err := m.o.Store.Update(func(c *Config) {
		c.Mode = ModeLocal
	}); err != nil {
		return err
	}
	m.setSession("", "")
	m.mu.Lock()
	r := m.relay
	m.relay = nil
	m.mu.Unlock()
	if r != nil {
		r.Close()
	}
	m.startLocal()
	m.notify()
	m.mu.Lock()
	err := m.localErr
	m.mu.Unlock()
	return err
}

// KeepRunning dice si el servidor local debe seguir vivo al cerrar el escritorio.
func (m *Manager) KeepRunning() bool { return m.o.Store.Get().KeepRunning }

// SetKeepRunning guarda la opción «seguir en segundo plano».
func (m *Manager) SetKeepRunning(v bool) error {
	return m.o.Store.Update(func(c *Config) { c.KeepRunning = v })
}

// TrustTLS fija la huella del certificado que el servidor remoto presentó en el último intento. fingerprint
// debe ser exactamente esa (así no se confía en un certificado que la persona no vio).
func (m *Manager) TrustTLS(fingerprint string) error {
	fp, err := NormalizeFingerprint(fingerprint)
	if err != nil {
		return err
	}
	m.mu.Lock()
	p := m.pending
	m.mu.Unlock()
	if p == nil {
		return userError("No hay ningún certificado pendiente de confirmar. Vuelve a intentar la conexión.")
	}
	if p.Fingerprint != fp {
		return userError("La huella no coincide con la que presenta el servidor ahora. Vuelve a intentar la conexión y revisa la huella mostrada.")
	}
	if err := m.o.Store.Update(func(c *Config) {
		if c.TLSPins == nil {
			c.TLSPins = map[string]string{}
		}
		c.TLSPins[p.HostPort] = fp
	}); err != nil {
		return err
	}
	m.mu.Lock()
	m.pending = nil
	m.mu.Unlock()
	if m.o.Store.Get().Mode == ModeRemote {
		m.notify()
	}
	return nil
}

// NotificationsAsked / MarkNotificationsAsked: el permiso de avisos ya se comprobó en el primer arranque.
func (m *Manager) NotificationsAsked() bool { return m.o.Store.Get().NotificationsAsked }

func (m *Manager) MarkNotificationsAsked() error {
	return m.o.Store.Update(func(c *Config) { c.NotificationsAsked = true })
}
