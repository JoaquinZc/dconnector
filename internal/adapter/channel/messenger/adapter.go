// Package messenger: adaptador de Messenger (cuenta personal, vía NO oficial) sobre
// go.mau.fi/mautrix-meta/pkg/messagix (09-ADAPTADOR-META). El aviso legal lo muestra la interfaz.
package messenger

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/mautrix-meta/pkg/messagix/useragent"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// editWindow: Messenger deja editar durante 15 minutos.
const editWindow = 15 * time.Minute

// typingTTL: Messenger repite «escribiendo» cada ~3 s; sin señales en 7 s se da por terminado.
const typingTTL = 7 * time.Second

// baseCapabilities: capacidades de una cuenta de Messenger (09 §5). Solo lo implementado:
// stickers, ubicación, contactos, encuestas, reenviar, menciones y estados quedan apagados.
func baseCapabilities(e2ee bool) capability.Set {
	return capability.Set{
		SendText: true, SendImage: true, SendVideo: true, SendAudio: true, SendVoice: true, SendDocument: true,
		Reply: true, React: true, MaxReactionsPerUser: 1,
		Edit: true, EditWindow: editWindow, DeleteForEveryone: true, DeleteWindow: 0,
		ReadReceipts: true, Typing: true, Groups: true,
		HistoryOnDemand: true, E2EEChats: e2ee,
		MaxFileSize: 25 << 20, MaxImageSize: 25 << 20, MaxVideoSize: 25 << 20, MaxTextLen: 20000,
	}
}

// Adapter: una instancia por cuenta de Messenger.
type Adapter struct {
	port.Base
	*meta.Core

	deps    port.AdapterDeps
	local   *meta.Local
	conv    *meta.Converter
	pair    *meta.PairFlow
	typers  *meta.TypingTimers
	avatars *meta.Avatars
	dl      meta.Downloader
	e2ee    *e2eeState

	tableMu sync.Mutex

	mu      sync.Mutex
	root    context.Context // contexto de la cuenta (el de Start): de él cuelga cada conexión
	runCtx  context.Context
	cancel  context.CancelFunc
	runDone chan struct{}
	gen     int
	client  *messagix.Client
	live    atomic.Bool
}

var _ port.ChannelAdapter = (*Adapter)(nil)

// New es la fábrica registrada en channel.Registry.
func New(deps port.AdapterDeps) (port.ChannelAdapter, error) {
	core, err := meta.NewCore(model.ChannelMessenger, deps)
	if err != nil {
		return nil, err
	}
	a := &Adapter{Core: core, deps: deps}
	a.local = meta.NewLocal(deps.DB)
	a.conv = meta.NewConverter(deps.Account.ID, model.ChannelMessenger, selfInt(deps.Account.SelfID))
	a.conv.Lookups = meta.Lookups{ChatOfMessage: a.local.ChatOfMessage, OutgoingUpTo: a.local.OutgoingUpTo}
	a.typers = meta.NewTypingTimers()
	a.dl = meta.Downloader{UserAgent: useragent.UserAgent}
	a.avatars = meta.NewAvatars(deps.AvatarDir, a.dl, core.Log)
	a.e2ee = newE2EE(a)
	a.conv.ShowEncrypted = a.e2ee != nil
	a.pair = meta.NewPairFlow(core, meta.PlatformMessenger, newPasswordDriver(a), a.sessionReady)
	return a, nil
}

func selfInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func (a *Adapter) Channel() model.Channel { return model.ChannelMessenger }

// Capabilities: las de la cuenta (chat == nil) o las de un chat concreto.
func (a *Adapter) Capabilities(chat *model.Chat) capability.Set {
	caps := baseCapabilities(a.e2ee != nil)
	if chat != nil && chat.ReadOnly {
		return caps.ReadOnly()
	}
	return caps
}

// ---------------------------------------------------------------------------------------------
// Ciclo de vida (09 §2)

// Start prepara la conexión y vuelve enseguida; la conexión vive en goroutines propias.
func (a *Adapter) Start(ctx context.Context, sink port.EventSink) error {
	a.SetSink(sink)
	a.mu.Lock()
	a.root = ctx
	a.mu.Unlock()
	if err := a.KV.Ensure(ctx); err != nil {
		a.SetStatus(model.AccountStatus{State: model.StateError, Detail: "No se pudo preparar la sesión de Messenger."})
		return fmt.Errorf("messenger: %w", err)
	}
	ck, err := meta.LoadCookies(ctx, a.KV)
	if err != nil {
		a.Log.Warn("no se pudieron leer las cookies guardadas", "err", meta.RedactErr(err))
		ck = nil
	}
	if len(ck) == 0 {
		a.stopRun(ctx)
		a.pair.PromptCookies()
		return nil
	}
	a.startRun(ck)
	return nil
}

// startRun lanza la conexión con las cookies dadas (cancela la anterior).
func (a *Adapter) startRun(ck map[string]string) {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	prev := a.runDone
	root := a.root
	if root == nil {
		root = context.Background()
	}
	run, cancel := context.WithCancel(root)
	a.runCtx, a.cancel = run, cancel
	a.gen++
	gen := a.gen
	done := make(chan struct{})
	a.runDone = done
	a.mu.Unlock()
	if prev != nil {
		select {
		case <-prev:
		case <-time.After(meta.StopWait):
		}
	}
	a.SetStatus(model.AccountStatus{State: model.StateConnecting})
	go func() {
		defer close(done)
		defer a.RecoverLoop("run")
		a.runLoop(run, gen, ck)
	}()
}

// stopRun corta la conexión si la hay y espera a que termine.
func (a *Adapter) stopRun(ctx context.Context) {
	a.mu.Lock()
	cancel, done := a.cancel, a.runDone
	a.cancel, a.runCtx = nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		t := time.NewTimer(meta.StopWait)
		select {
		case <-done:
		case <-ctx.Done():
		case <-t.C:
		}
		t.Stop()
	}
	a.live.Store(false)
	a.typers.StopAll()
}

// sessionReady: la vinculación entregó cookies válidas; se guardan y se arranca la conexión.
func (a *Adapter) sessionReady(ctx context.Context, ck map[string]string) error {
	if err := meta.SaveCookies(ctx, a.KV, ck); err != nil {
		return err
	}
	if id := meta.UserID(meta.PlatformMessenger, ck); id != "" {
		a.SetSelfID(id)
	}
	a.startRun(ck)
	return nil
}

// platformOf: la plataforma de la librería según cómo se obtuvo la sesión.
func (a *Adapter) platformOf(ctx context.Context) types.Platform {
	if b, ok, _ := a.KV.Get(ctx, meta.KeyPlatform); ok && string(b) == platformLite {
		return types.MessengerLiteIOS
	}
	return types.Messenger
}

const platformLite = "messenger-lite"

// runLoop abre la sesión, entrega el historial inicial y conecta el socket; vive hasta cancelar el contexto.
func (a *Adapter) runLoop(ctx context.Context, gen int, ck map[string]string) {
	zl := meta.ZerologToSlog(a.Log)
	libCtx := meta.LibContext(ctx, zl)

	jar := &cookies.Cookies{Platform: a.platformOf(ctx)}
	vals := make(map[cookies.MetaCookieName]string, len(ck))
	for k, v := range ck {
		vals[cookies.MetaCookieName(k)] = v
	}
	jar.UpdateValues(vals)
	client := messagix.NewClient(jar, zl, &messagix.Config{})
	client.SetEventHandler(func(_ context.Context, evt any) { a.handleEvent(libCtx, gen, evt) })

	lctx, cancel := context.WithTimeout(libCtx, 2*time.Minute)
	currentUser, initial, err := client.LoadMessagesPage(lctx)
	cancel()
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		a.handleStartError(ctx, err)
		return
	}
	a.mu.Lock()
	if a.gen != gen {
		a.mu.Unlock()
		return
	}
	a.client = client
	a.mu.Unlock()

	self := currentUser.GetFBID()
	if self == 0 {
		self = selfInt(meta.UserID(meta.PlatformMessenger, ck))
	}
	a.conv.SetSelf(self)
	a.SetSelfID(meta.NativeUser(self))
	_ = a.KV.Set(ctx, meta.KeySelf, []byte(meta.NativeUser(self)))

	// Primero la persona propia: el Router rellena SelfID/SelfName/SelfHandle con ella (09, regla 1).
	me := model.Person{AccountID: a.Acc.ID, NativeID: meta.NativeUser(self), PushName: currentUser.GetName(), Username: currentUser.GetUsername()}
	a.Emit(model.Event{Type: model.EvPerson, Person: &me})

	a.avatarsInit(ctx)
	if u := currentUser.GetAvatarURL(); u != "" {
		a.avatars.Enqueue(meta.NativeUser(self), u)
	}
	a.deliverHistory(ctx, initial)

	if err := client.Connect(libCtx); err != nil {
		a.Log.Warn("no se pudo abrir el socket de Messenger", "err", meta.RedactErr(err))
		a.SetStatus(model.AccountStatus{State: model.StateError, Detail: "No se pudo conectar con Messenger. Revisa tu conexión a internet."})
		return
	}
	<-ctx.Done()
	client.SetEventHandler(nil)
	client.Disconnect()
	a.live.Store(false)
	a.mu.Lock()
	if a.client == client {
		a.client = nil
	}
	a.mu.Unlock()
}

// handleStartError traduce el fallo al abrir la sesión: sesión perdida (logged_out) o error (se reintenta).
func (a *Adapter) handleStartError(ctx context.Context, err error) {
	problem, detail := meta.ClassifyAuthError("Messenger", "messenger.com", err)
	a.Log.Warn("no se pudo abrir la sesión de Messenger", "problem", int(problem), "err", meta.RedactErr(err))
	switch problem {
	case meta.ProblemLoggedOut, meta.ProblemSuspended, meta.ProblemVerify:
		a.wipeSession()
		a.SetStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: detail})
	default:
		a.SetStatus(model.AccountStatus{State: model.StateError, Detail: detail})
	}
}

// wipeSession borra la sesión local (cookies, llaves y todo lo que guarda el adaptador).
func (a *Adapter) wipeSession() {
	ctx, cancel := context.WithTimeout(context.Background(), meta.OpTimeout)
	defer cancel()
	if err := a.KV.Wipe(ctx); err != nil {
		a.Log.Warn("no se pudo borrar la sesión local", "err", meta.RedactErr(err))
	}
	a.e2eeReset(ctx)
}

// Stop corta la conexión y avisa; no toca la sesión guardada.
func (a *Adapter) Stop(ctx context.Context) error {
	a.stopRun(ctx)
	a.e2eeStop()
	a.SetStatus(model.AccountStatus{State: model.StateDisconnected})
	return nil
}

// Logout cierra la conexión y borra la sesión local. Meta no ofrece una llamada para cerrar la sesión web
// de las cookies: la sesión sigue figurando en «Seguridad» de la cuenta hasta que el usuario la cierre allí.
func (a *Adapter) Logout(ctx context.Context) error {
	a.stopRun(ctx)
	a.e2eeStop()
	if err := a.KV.Ensure(ctx); err == nil {
		_ = a.KV.Wipe(ctx)
	}
	a.e2eeReset(ctx)
	a.pair.Reset()
	a.SetSelfID("")
	a.SetStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: "Sesión cerrada. Vuelve a vincular la cuenta para usarla."})
	return nil
}

func (a *Adapter) BeginPairing(ctx context.Context, req model.PairRequest) error {
	return a.pair.Begin(ctx, req)
}

func (a *Adapter) SubmitPairing(ctx context.Context, in model.PairInput) error {
	return a.pair.Submit(ctx, in)
}

// ---------------------------------------------------------------------------------------------
// Eventos de la librería

func (a *Adapter) currentGen() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gen
}

func (a *Adapter) handleEvent(ctx context.Context, gen int, raw any) {
	if gen != a.currentGen() {
		return
	}
	defer a.RecoverLoop("evento")
	switch evt := raw.(type) {
	case *table.LSTable:
		a.onTable(ctx, evt)
	case *messagix.ConnectedEvent:
		a.live.Store(true)
		a.SetStatus(model.AccountStatus{State: model.StateConnected})
		go func() {
			defer a.RecoverLoop("arranque")
			a.onConnected(ctx)
		}()
	case *messagix.ReconnectedEvent:
		a.live.Store(true)
		a.SetStatus(model.AccountStatus{State: model.StateConnected})
	case *messagix.TransientDisconnectEvent:
		a.live.Store(false)
		if a.LastStatus().State == model.StateConnected {
			a.SetStatus(model.AccountStatus{State: model.StateConnecting, Detail: "Reconectando con Messenger…"})
		}
	case *messagix.PermanentErrorEvent:
		a.live.Store(false)
		problem, detail := meta.ClassifyAuthError("Messenger", "messenger.com", evt.Err)
		a.Log.Warn("error permanente del socket de Messenger", "err", meta.RedactErr(evt.Err))
		if problem == meta.ProblemTransient {
			// 401 del socket = la sesión ya no vale
			a.wipeSession()
			a.SetStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: "Messenger cerró esta sesión. Vuelve a vincular la cuenta para usarla."})
			return
		}
		a.wipeSession()
		a.SetStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: detail})
	default:
		a.Log.Debug("evento de Messenger no tratado", "type", fmt.Sprintf("%T", raw))
	}
}

// onTable traduce una publicación del socket y entrega sus eventos.
func (a *Adapter) onTable(ctx context.Context, tbl *table.LSTable) {
	a.tableMu.Lock()
	defer a.tableMu.Unlock()
	res := a.conv.Convert(ctx, tbl)
	a.afterConvert(res)
	a.deliverLive(res)
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	if client != nil {
		client.PostHandlePublishResponse(tbl)
	}
}

// afterConvert recuerda el chat de cada mensaje y pide las fotos que falten.
func (a *Adapter) afterConvert(res *meta.Result) {
	for i := range res.Messages {
		a.local.Remember(res.Messages[i].ChatNativeID, res.Messages[i].NativeID)
	}
	for id, u := range res.Avatars {
		a.avatars.Enqueue(id, u)
	}
}

// deliverLive entrega los eventos de una publicación en vivo.
func (a *Adapter) deliverLive(res *meta.Result) {
	typing := res.Typing
	res.Typing = nil
	for _, ev := range res.Events() {
		a.Emit(ev)
	}
	for _, t := range typing {
		a.emitTyping(t)
	}
}

// emitTyping entrega «escribiendo» y programa el aviso de fin (Meta no lo manda).
func (a *Adapter) emitTyping(t model.TypingEvent) {
	ev := t
	a.Emit(model.Event{Type: model.EvTyping, Typing: &ev, ChatKey: ev.ChatKey})
	key := string(ev.ChatKey) + "|" + ev.PeerID
	a.typers.Touch(key, ev.Typing, typingTTL, func() {
		end := ev
		end.Typing = false
		a.Emit(model.Event{Type: model.EvTyping, Typing: &end, ChatKey: end.ChatKey})
	})
}

// deliverHistory entrega una tabla de historial como history.batch (en lotes de 500 mensajes).
func (a *Adapter) deliverHistory(ctx context.Context, tbl *table.LSTable) {
	a.tableMu.Lock()
	defer a.tableMu.Unlock()
	res := a.conv.Convert(ctx, tbl)
	a.afterConvert(res)
	res.Typing = nil
	for _, b := range res.HistoryBatches(500) {
		batch := b
		a.Emit(model.Event{Type: model.EvHistoryBatch, History: &batch})
	}
}

// onConnected: tras conectar, sincroniza los chats y arranca lo cifrado (si está disponible).
func (a *Adapter) onConnected(ctx context.Context) {
	for attempt := 1; attempt <= 3; attempt++ {
		err := a.SyncChats(ctx)
		if err == nil || ctx.Err() != nil {
			break
		}
		a.Log.Warn("no se pudieron sincronizar los chats", "attempt", attempt, "err", meta.RedactErr(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * 20 * time.Second):
		}
	}
	a.e2eeConnect(ctx)
}

// ---------------------------------------------------------------------------------------------
// Cliente en uso

// liveClient devuelve el cliente si está conectado; si no, ErrNotConnected.
func (a *Adapter) liveClient() (*messagix.Client, error) {
	a.mu.Lock()
	cli := a.client
	a.mu.Unlock()
	if cli == nil || !a.live.Load() {
		return nil, fmt.Errorf("messenger: %w", port.ErrNotConnected)
	}
	return cli, nil
}

// mapErr traduce un error de la librería a los errores del dominio (02 §6), con texto en español.
func (a *Adapter) mapErr(action string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, port.ErrUnsupported), errors.Is(err, port.ErrNotConnected), errors.Is(err, port.ErrNotFound),
		errors.Is(err, port.ErrInvalid), errors.Is(err, port.ErrRateLimited):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	problem, _ := meta.ClassifyAuthError("Messenger", "messenger.com", err)
	if problem == meta.ProblemLoggedOut {
		return fmt.Errorf("messenger: %s: la sesión ya no es válida: %w", action, port.ErrNotConnected)
	}
	a.Log.Warn("error de Messenger", "action", action, "err", meta.RedactErr(err))
	return fmt.Errorf("messenger: %s: %s: %w", action, meta.Redact(err.Error()), port.ErrInvalid)
}
