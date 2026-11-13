// Package whatsapp: adaptador de WhatsApp sobre whatsmeow (07-ADAPTADOR-WHATSAPP).
package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// sqlDialect: nombre de dialecto que acepta dbutil con el driver "sqlite" de modernc.org/sqlite
	// (dbutil acepta cualquier nombre que empiece por «sqlite»).
	sqlDialect = "sqlite"
	// onlineHold: cuánto se queda «en línea» tras escribir, para que los demás reciban «escribiendo…».
	onlineHold = 45 * time.Second
	// opTimeout: tope de las llamadas de red cortas que hace el adaptador por su cuenta.
	opTimeout = 20 * time.Second
)

// baseCapabilities: capacidades de una cuenta de WhatsApp (07 §12). StatusAudience queda en
// false: la versión instalada de whatsmeow no permite fijar la audiencia de un estado.
func baseCapabilities() capability.Set {
	return capability.Set{
		SendText: true, SendImage: true, SendVideo: true, SendAudio: true, SendVoice: true,
		SendDocument: true, SendSticker: true, SendLocation: true, SendContact: true,
		SendPoll: true, VotePoll: true,
		Reply: true, Forward: true, React: true, MaxReactionsPerUser: 1,
		Edit: true, EditWindow: 15 * time.Minute,
		DeleteForEveryone: true, DeleteWindow: 48 * time.Hour,
		ReadReceipts: true, Typing: true, Mentions: true, Groups: true, StartChat: true,
		Statuses: true, PostStatus: true, StatusViewers: true, StatusAudience: false, StatusReact: false,
		HistoryOnDemand: true,
		MaxFileSize:     100 << 20, MaxImageSize: 16 << 20, MaxVideoSize: 64 << 20, MaxTextLen: 65536,
	}
}

// Adapter: una instancia por cuenta de WhatsApp.
type Adapter struct {
	port.Base

	deps port.AdapterDeps
	acc  model.Account
	log  *slog.Logger

	container *sqlstore.Container
	local     *localDB
	polls     *pollBook

	initMu sync.Mutex // serializa la creación del cliente y del almacén

	mu          sync.Mutex
	client      *whatsmeow.Client
	sink        port.EventSink
	runCtx      context.Context
	cancel      context.CancelFunc
	schemaReady bool
	lastStatus  model.AccountStatus
	readOnly    map[string]bool      // chat → true si no se puede escribir
	wire        map[string]types.JID // id nativo (teléfono) → JID original (@lid) con que llegó
	titles      map[string]string    // chat → título conocido (grupos, canales)
	onlineTimer *time.Timer
	online      atomic.Bool

	pairMu     sync.Mutex
	pairCancel context.CancelFunc

	histMu sync.Mutex // mantiene en orden los lotes de historial

	unknownSeen sync.Map      // tipos de mensaje no soportados ya registrados en esta sesión
	retry       *mediaRetrier // reintentos de descarga de medios caducados (media_retry.go)
}

// New es la fábrica registrada en channel.Registry.
func New(deps port.AdapterDeps) (port.ChannelAdapter, error) {
	if deps.DB == nil {
		return nil, errors.New("whatsapp: falta la base de datos de la cuenta")
	}
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("channel", string(model.ChannelWhatsApp), "accountId", string(deps.Account.ID))
	deps.Log = log
	a := &Adapter{
		deps: deps, acc: deps.Account, log: log,
		container: sqlstore.NewWithDB(deps.DB, sqlDialect, newWALogger(log)),
		local:     newLocalDB(deps.DB),
		polls:     newPollBook(deps.DB),
		retry:     newMediaRetrier(),
		readOnly:  map[string]bool{},
		wire:      map[string]types.JID{},
		titles:    map[string]string{},
	}
	return a, nil
}

func (a *Adapter) Channel() model.Channel { return model.ChannelWhatsApp }

// Capabilities: las de la cuenta (chat == nil) o las de un chat concreto (07 §12, última línea).
func (a *Adapter) Capabilities(chat *model.Chat) capability.Set {
	caps := baseCapabilities()
	if chat == nil {
		return caps
	}
	if chat.ReadOnly || a.isReadOnly(chat.NativeID) {
		return caps.ReadOnly()
	}
	return caps
}

// isReadOnly: canales donde no eres administrador y grupos «solo administradores» donde no lo eres.
func (a *Adapter) isReadOnly(chatID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v, ok := a.readOnly[chatID]; ok {
		return v
	}
	return strings.HasSuffix(chatID, "@"+types.NewsletterServer)
}

func (a *Adapter) setReadOnly(chatID string, ro bool) {
	a.mu.Lock()
	a.readOnly[chatID] = ro
	a.mu.Unlock()
}

// ---------------------------------------------------------------------------------------------
// Cliente

func (a *Adapter) getClient() *whatsmeow.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client
}

// live devuelve el cliente si está conectado y con sesión; si no, ErrNotConnected.
func (a *Adapter) live() (*whatsmeow.Client, error) {
	cli := a.getClient()
	if cli == nil || !cli.IsConnected() || !cli.IsLoggedIn() {
		return nil, port.ErrNotConnected
	}
	return cli, nil
}

// runContext: contexto de las tareas de fondo; se cancela con Stop.
func (a *Adapter) runContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runCtx != nil {
		return a.runCtx
	}
	return context.Background()
}

var osInfoOnce sync.Once

// ensureClient crea el almacén de sesión y el cliente si aún no existen (07 §2).
func (a *Adapter) ensureClient(ctx context.Context) error {
	a.initMu.Lock()
	defer a.initMu.Unlock()
	if a.getClient() != nil {
		return nil
	}
	a.mu.Lock()
	ready := a.schemaReady
	a.mu.Unlock()
	if !ready {
		if err := a.container.Upgrade(ctx); err != nil {
			return fmt.Errorf("whatsapp: no se pudo preparar la sesión: %w", err)
		}
		if err := a.local.ensure(ctx); err != nil {
			return fmt.Errorf("whatsapp: %w", err)
		}
		a.mu.Lock()
		a.schemaReady = true
		a.mu.Unlock()
	}
	// Una cuenta tiene un solo dispositivo en su account.db: GetFirstDevice devuelve el guardado
	// o uno nuevo (el JID vive en whatsmeow_device; no hace falta kv["wa.device"]).
	device, err := a.container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp: no se pudo leer la sesión guardada: %w", err)
	}
	osInfoOnce.Do(func() {
		store.SetOSInfo("dConnector", [3]uint32{2, 0, 0})
		store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
		// Solo afecta a las cuentas que se vinculen después (ver historyDeviceProps).
		historyDeviceProps(store.DeviceProps)
	})
	cli := whatsmeow.NewClient(device, newWALogger(a.log))
	cli.EnableAutoReconnect = true
	cli.AutomaticMessageRerequestFromPhone = true
	// Sin esto, mientras la cuenta esté «no disponible» los recibos de entrega salen como
	// «inactive» y quien nos escribe nunca ve los dos ticks grises.
	cli.SetForceActiveDeliveryReceipts(true)
	cli.AddEventHandler(a.handle)
	a.mu.Lock()
	a.client = cli
	a.mu.Unlock()
	return nil
}

// dropClient suelta el cliente actual; con wipe también borra la sesión local y lo guardado por
// el adaptador. El siguiente Start o BeginPairing crea un dispositivo nuevo.
func (a *Adapter) dropClient(ctx context.Context, wipe bool) {
	a.mu.Lock()
	cli := a.client
	a.client = nil
	if a.onlineTimer != nil {
		a.onlineTimer.Stop()
		a.onlineTimer = nil
	}
	a.mu.Unlock()
	a.online.Store(false)
	a.cancelPairing()
	if cli != nil {
		cli.RemoveEventHandlers()
		cli.Disconnect()
		if wipe && cli.Store != nil && cli.Store.ID != nil {
			if err := cli.Store.Delete(ctx); err != nil {
				a.log.Warn("no se pudo borrar la sesión local", "err", redact(err.Error()))
			}
		}
	}
	if wipe {
		a.local.wipe(ctx)
		a.polls.reset()
		a.mu.Lock()
		a.readOnly = map[string]bool{}
		a.wire = map[string]types.JID{}
		a.titles = map[string]string{}
		a.mu.Unlock()
	}
}

// ---------------------------------------------------------------------------------------------
// Ciclo de vida (07 §3)

func (a *Adapter) Start(ctx context.Context, sink port.EventSink) error {
	a.mu.Lock()
	a.sink = sink
	if a.cancel != nil {
		a.cancel()
	}
	run, cancel := context.WithCancel(ctx)
	a.runCtx, a.cancel = run, cancel
	a.mu.Unlock()

	if err := a.ensureClient(run); err != nil {
		a.setStatus(model.AccountStatus{State: model.StateError, Detail: "No se pudo preparar la sesión de WhatsApp."})
		return err
	}
	// Si el contexto padre se cancela sin Stop, desconectar.
	go func() {
		<-run.Done()
		a.mu.Lock()
		same := a.runCtx == run
		a.mu.Unlock()
		if same {
			a.disconnect()
		}
	}()

	cli := a.getClient()
	if cli.Store.ID == nil {
		a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedNone})
		return nil
	}
	return a.connect(run, cli)
}

// connect abre la conexión en segundo plano; los errores salen como estado error.
func (a *Adapter) connect(ctx context.Context, cli *whatsmeow.Client) error {
	if cli.IsConnected() {
		a.setStatus(model.AccountStatus{State: model.StateConnected})
		return nil
	}
	a.setStatus(model.AccountStatus{State: model.StateConnecting})
	go func() {
		defer a.recoverLoop("connect")
		if err := cli.ConnectContext(ctx); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
			if ctx.Err() != nil {
				return
			}
			a.log.Warn("no se pudo conectar", "err", redact(err.Error()))
			a.setStatus(model.AccountStatus{
				State: model.StateError, Detail: "No se pudo conectar con WhatsApp. Revisa tu conexión a internet.",
			})
		}
	}()
	return nil
}

// disconnect corta la conexión y avisa; no toca la sesión guardada.
func (a *Adapter) disconnect() {
	a.cancelPairing()
	if cli := a.getClient(); cli != nil {
		cli.Disconnect()
	}
	a.mu.Lock()
	if a.onlineTimer != nil {
		a.onlineTimer.Stop()
		a.onlineTimer = nil
	}
	a.mu.Unlock()
	a.online.Store(false)
	a.setStatus(model.AccountStatus{State: model.StateDisconnected})
}

func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	a.cancel, a.runCtx = nil, nil
	a.mu.Unlock()
	a.disconnect()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *Adapter) Logout(ctx context.Context) error {
	cli := a.getClient()
	var serverErr error
	if cli != nil && cli.Store.ID != nil {
		if cli.IsConnected() {
			if err := cli.Logout(ctx); err != nil {
				serverErr = err
			}
		} else {
			serverErr = port.ErrNotConnected
		}
	}
	// La sesión local se borra siempre.
	a.dropClient(ctx, true)
	a.setStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: "Sesión cerrada. Vuelve a vincular la cuenta para usarla."})
	if serverErr != nil {
		a.log.Warn("no se pudo avisar a WhatsApp del cierre de sesión", "err", redact(serverErr.Error()))
		return fmt.Errorf("whatsapp: la sesión local se cerró, pero no se pudo avisar a WhatsApp (el dispositivo puede seguir vinculado en el teléfono): %w", serverErr)
	}
	return nil
}

// recoverLoop registra un pánico de una goroutine de larga vida sin tumbar el proceso (02 §6).
func (a *Adapter) recoverLoop(where string) {
	if r := recover(); r != nil {
		a.log.Error("pánico recuperado", "where", where, "panic", fmt.Sprint(r))
	}
}

// ---------------------------------------------------------------------------------------------
// Emisión de eventos

func (a *Adapter) currentSink() port.EventSink {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sink
}

// emit completa AccountID, Channel y At y entrega el evento al sink (no bloquea).
func (a *Adapter) emit(ev model.Event) {
	sink := a.currentSink()
	if sink == nil {
		return
	}
	ev.AccountID, ev.Channel = a.acc.ID, model.ChannelWhatsApp
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	sink.Emit(ev)
}

// setStatus emite account.state; descarta repeticiones idénticas.
func (a *Adapter) setStatus(st model.AccountStatus) {
	st.AccountID = a.acc.ID
	a.mu.Lock()
	prev := a.lastStatus
	same := prev.State == st.State && prev.Need == st.Need && prev.QRCode == st.QRCode &&
		prev.PairCode == st.PairCode && prev.Detail == st.Detail
	if !same {
		st.Since = time.Now().UnixMilli()
		a.lastStatus = st
	}
	a.mu.Unlock()
	if same {
		return
	}
	a.emit(model.Event{Type: model.EvAccountStatus, At: st.Since, AccountStatus: &st})
}

// mapErr traduce un error de whatsmeow a los errores del dominio (02 §6), con texto en español.
func (a *Adapter) mapErr(action string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, port.ErrUnsupported), errors.Is(err, port.ErrNotConnected), errors.Is(err, port.ErrNotFound),
		errors.Is(err, port.ErrInvalid), errors.Is(err, port.ErrRateLimited):
		return err
	case errors.Is(err, whatsmeow.ErrNotConnected), errors.Is(err, whatsmeow.ErrNotLoggedIn),
		errors.Is(err, whatsmeow.ErrClientIsNil):
		return fmt.Errorf("whatsapp: %s: %w", action, port.ErrNotConnected)
	case errors.Is(err, whatsmeow.ErrRecipientNotOnWhatsApp):
		return fmt.Errorf("whatsapp: ese número no está en WhatsApp: %w", port.ErrNotFound)
	case errors.Is(err, whatsmeow.ErrNoPermissionInGroup):
		return fmt.Errorf("whatsapp: no tienes permiso para escribir en este grupo: %w", port.ErrInvalid)
	case errors.Is(err, whatsmeow.ErrMessageTimedOut), errors.Is(err, whatsmeow.ErrIQTimedOut):
		return fmt.Errorf("whatsapp: %s: WhatsApp tardó demasiado en responder: %w", action, err)
	}
	a.log.Warn("error de WhatsApp", "action", action, "err", redact(err.Error()))
	return fmt.Errorf("whatsapp: %s: %w", action, err)
}

// conv arma el traductor puro con lo que sabe el cliente ahora.
func (a *Adapter) conv() converter {
	c := converter{acc: a.acc.ID, resolve: a.resolveLID, name: a.contactName, unknown: a.noteUnknownType}
	if cli := a.getClient(); cli != nil && cli.Store != nil {
		c.self = cli.Store.GetJID().ToNonAD()
		c.selfLID = cli.Store.GetLID().ToNonAD()
	}
	return c
}

// resolveLID: @lid → JID de teléfono con client.Store.LIDs (decisión C6).
func (a *Adapter) resolveLID(lid types.JID) types.JID {
	cli := a.getClient()
	if cli == nil || cli.Store == nil || cli.Store.LIDs == nil {
		return types.EmptyJID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pn, err := cli.Store.LIDs.GetPNForLID(ctx, lid.ToNonAD())
	if err != nil || pn.IsEmpty() {
		return types.EmptyJID
	}
	return pn
}

// contactName: nombre de agenda o nombre propio de una persona; "" si no se conoce.
func (a *Adapter) contactName(j types.JID) string {
	ci, ok := a.contactInfo(j)
	if !ok {
		return ""
	}
	for _, n := range []string{ci.FullName, ci.FirstName, ci.PushName, ci.BusinessName} {
		if strings.TrimSpace(n) != "" {
			return n
		}
	}
	return ""
}

func (a *Adapter) contactInfo(j types.JID) (types.ContactInfo, bool) {
	cli := a.getClient()
	if cli == nil || cli.Store == nil || cli.Store.Contacts == nil {
		return types.ContactInfo{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ci, err := cli.Store.Contacts.GetContact(ctx, j.ToNonAD())
	if err != nil || !ci.Found {
		return ci, false
	}
	return ci, true
}

// wireJID devuelve el JID con el que WhatsApp conoce el chat (si llegó como @lid, ese @lid),
// para recibos, presencia e historial. Si no hay nada recordado, el mismo JID.
func (a *Adapter) wireJID(j types.JID) types.JID {
	a.mu.Lock()
	defer a.mu.Unlock()
	if w, ok := a.wire[jidToNative(j)]; ok {
		return w
	}
	return j.ToNonAD()
}

// noteWire recuerda que un JID resuelto a teléfono llegó originalmente como @lid.
func (a *Adapter) noteWire(orig, resolved types.JID) {
	orig, resolved = orig.ToNonAD(), resolved.ToNonAD()
	if orig.Server != types.HiddenUserServer || resolved.Server != types.DefaultUserServer {
		return
	}
	a.mu.Lock()
	a.wire[jidToNative(resolved)] = orig
	a.mu.Unlock()
}

// goOnline se pone «disponible» mientras se escribe (WhatsApp no entrega «escribiendo…» a quien
// está «no disponible») y vuelve a «no disponible» pasado onlineHold (07 §3).
func (a *Adapter) goOnline(cli *whatsmeow.Client) {
	if !a.online.Swap(true) {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		_ = cli.SendPresence(ctx, types.PresenceAvailable)
		cancel()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.onlineTimer != nil {
		a.onlineTimer.Stop()
	}
	a.onlineTimer = time.AfterFunc(onlineHold, func() {
		a.online.Store(false)
		if c := a.getClient(); c != nil && c.IsConnected() {
			ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
			defer cancel()
			_ = c.SendPresence(ctx, types.PresenceUnavailable)
		}
	})
}

// banDetail arma el texto de un bloqueo temporal (07 §3).
func banDetail(evt *events.TemporaryBan, now time.Time) string {
	reason := tempBanReason(evt.Code)
	if evt.Expire > 0 {
		return fmt.Sprintf("WhatsApp bloqueó temporalmente la cuenta: %s, hasta %s", reason, now.Add(evt.Expire).Format("02/01/2006 15:04"))
	}
	return fmt.Sprintf("WhatsApp bloqueó temporalmente la cuenta: %s", reason)
}
