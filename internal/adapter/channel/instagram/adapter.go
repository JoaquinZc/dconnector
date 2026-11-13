// Package instagram: adaptador de Instagram Direct (cuenta personal, vía NO oficial) sobre
// go.mau.fi/mautrix-meta/pkg/instameow (09-ADAPTADOR-META). El aviso legal lo muestra la interfaz.
//
// Librería: messagix ya no sirve Instagram (mautrix-meta lo sacó en v26.08); el cliente de Instagram es
// instameow, con su propio protocolo (bandeja «mailbox» + avisos «slide»). Ver docs/bitacora/3-C3.md.
package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow"
	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/dgw"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/mautrix-meta/pkg/messagix/useragent"
	"go.mau.fi/util/exhttp"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// editWindow: Instagram deja editar durante 15 minutos.
const editWindow = 15 * time.Minute

// typingMinTTL: tiempo mínimo que se mantiene «escribiendo» sin nuevas señales.
const typingMinTTL = 6 * time.Second

// recentBackfill: cuántos chats recientes cargan sus últimos mensajes al conectar.
const recentBackfill = 25

// baseCapabilities: capacidades de una cuenta de Instagram (09 §5). Sin documentos, sin chats cifrados;
// stickers, ubicación, contactos, encuestas, reenviar, menciones y estados quedan apagados.
func baseCapabilities() capability.Set {
	return capability.Set{
		SendText: true, SendImage: true, SendVideo: true, SendAudio: true, SendVoice: true, SendDocument: false,
		Reply: true, React: true, MaxReactionsPerUser: 1,
		Edit: true, EditWindow: editWindow, DeleteForEveryone: true, DeleteWindow: 0,
		ReadReceipts: true, Typing: true, Groups: true,
		HistoryOnDemand: true, E2EEChats: false,
		MaxFileSize: 25 << 20, MaxImageSize: 25 << 20, MaxVideoSize: 25 << 20, MaxTextLen: 1000,
	}
}

// Adapter: una instancia por cuenta de Instagram.
type Adapter struct {
	port.Base
	*meta.Core

	deps    port.AdapterDeps
	local   *meta.Local
	conv    *Converter
	pair    *meta.PairFlow
	typers  *meta.TypingTimers
	avatars *meta.Avatars
	dl      meta.Downloader

	evMu sync.Mutex // serializa la entrega de avisos

	mu      sync.Mutex
	root    context.Context
	runCtx  context.Context
	cancel  context.CancelFunc
	runDone chan struct{}
	gen     int
	client  *instameow.Client
	live    atomic.Bool
}

var _ port.ChannelAdapter = (*Adapter)(nil)

// New es la fábrica registrada en channel.Registry.
func New(deps port.AdapterDeps) (port.ChannelAdapter, error) {
	core, err := meta.NewCore(model.ChannelInstagram, deps)
	if err != nil {
		return nil, err
	}
	a := &Adapter{Core: core, deps: deps}
	a.local = meta.NewLocal(deps.DB)
	a.conv = NewConverter(deps.Account.ID, selfInt(deps.Account.SelfID))
	a.typers = meta.NewTypingTimers()
	a.dl = meta.Downloader{UserAgent: useragent.UserAgent}
	a.avatars = meta.NewAvatars(deps.AvatarDir, a.dl, core.Log)
	a.pair = meta.NewPairFlow(core, meta.PlatformInstagram, newPasswordDriver(a), a.sessionReady)
	return a, nil
}

func selfInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func (a *Adapter) Channel() model.Channel { return model.ChannelInstagram }

// Capabilities: las de la cuenta (chat == nil) o las de un chat concreto.
func (a *Adapter) Capabilities(chat *model.Chat) capability.Set {
	caps := baseCapabilities()
	if chat != nil && chat.ReadOnly {
		return caps.ReadOnly()
	}
	return caps
}

// ---------------------------------------------------------------------------------------------
// Ciclo de vida

// Start prepara la conexión y vuelve enseguida; la conexión vive en goroutines propias.
func (a *Adapter) Start(ctx context.Context, sink port.EventSink) error {
	a.SetSink(sink)
	a.mu.Lock()
	a.root = ctx
	a.mu.Unlock()
	if err := a.KV.Ensure(ctx); err != nil {
		a.SetStatus(model.AccountStatus{State: model.StateError, Detail: "No se pudo preparar la sesión de Instagram."})
		return fmt.Errorf("instagram: %w", err)
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
	_ = a.KV.Del(ctx, meta.KeySeq) // sesión nueva: sin cursor viejo
	a.startRun(ck)
	return nil
}

type seqState struct {
	Seq int64 `json:"seq"`
	TS  int64 `json:"ts"`
}

func (a *Adapter) loadSeq(ctx context.Context) (seqState, bool) {
	b, ok, err := a.KV.Get(ctx, meta.KeySeq)
	if err != nil || !ok {
		return seqState{}, false
	}
	var s seqState
	if json.Unmarshal(b, &s) != nil || s.Seq == 0 {
		return seqState{}, false
	}
	return s, true
}

func (a *Adapter) saveSeq(ctx context.Context, seq int64, ts time.Time) {
	b, _ := json.Marshal(seqState{Seq: seq, TS: ts.UnixMilli()})
	_ = a.KV.Set(ctx, meta.KeySeq, b)
}

// runLoop abre la sesión, entrega la bandeja y conecta el socket; vive hasta cancelar el contexto.
func (a *Adapter) runLoop(ctx context.Context, gen int, ck map[string]string) {
	zl := meta.ZerologToSlog(a.Log)
	libCtx := meta.LibContext(ctx, zl)

	jar := &cookies.Cookies{Platform: types.Instagram}
	vals := make(map[cookies.MetaCookieName]string, len(ck))
	for k, v := range ck {
		vals[cookies.MetaCookieName(k)] = v
	}
	jar.UpdateValues(vals)
	client := instameow.NewClient(instameow.ClientParams{
		Cookies:      jar,
		Log:          zl,
		Settings:     exhttp.ClientSettings{},
		EventHandler: func(_ context.Context, evt slidetypes.ClientEvent) error { return a.handleEvent(libCtx, gen, evt) },
	})

	lctx, cancel := context.WithTimeout(libCtx, 2*time.Minute)
	viewer, mailbox, err := client.LoadIndex(lctx)
	cancel()
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		a.handleStartError(err)
		return
	}
	a.mu.Lock()
	if a.gen != gen {
		a.mu.Unlock()
		return
	}
	a.client = client
	a.mu.Unlock()

	// Con un cursor guardado, los avisos que llegaron mientras la cuenta estaba apagada se recuperan.
	if s, ok := a.loadSeq(ctx); ok {
		client.SetSeqID(s.Seq, time.UnixMilli(s.TS))
	}

	// Id propio: el de mensajería (no el ds_user_id), el que aparece como remitente de mis mensajes.
	self := client.GetOwnFBID()
	if self == 0 && mailbox != nil {
		for _, e := range mailbox.ThreadsByFolder.Edges {
			if t := e.Node.AsIGDirectThread; t != nil && t.Viewer != nil && t.Viewer.InteropMessagingUserFBID != 0 {
				self = t.Viewer.InteropMessagingUserFBID
				break
			}
		}
	}
	if self == 0 {
		self = viewer.GetFBID()
		a.Log.Warn("no se encontró el id de mensajería propio; se usa el del perfil")
	}
	a.conv.SetSelf(self)
	a.SetSelfID(meta.NativeUser(self))
	_ = a.KV.Set(ctx, meta.KeySelf, []byte(meta.NativeUser(self)))

	// Primero la persona propia: el Router rellena SelfID/SelfName/SelfHandle con ella (09, regla 1).
	me := model.Person{AccountID: a.Acc.ID, NativeID: meta.NativeUser(self), PushName: viewer.GetName(), Username: viewer.GetUsername()}
	a.Emit(model.Event{Type: model.EvPerson, Person: &me})

	a.avatarsInit(ctx)
	if u := viewer.GetAvatarURL(); u != "" {
		a.avatars.Enqueue(meta.NativeUser(self), u)
	}
	a.deliverMailbox(mailbox)

	go func() {
		defer a.RecoverLoop("socket")
		client.Connect(libCtx)
	}()
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
func (a *Adapter) handleStartError(err error) {
	problem, detail := meta.ClassifyAuthError("Instagram", "instagram.com", err)
	a.Log.Warn("no se pudo abrir la sesión de Instagram", "problem", int(problem), "err", meta.RedactErr(err))
	switch problem {
	case meta.ProblemLoggedOut, meta.ProblemSuspended, meta.ProblemVerify:
		a.wipeSession()
		a.SetStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: detail})
	default:
		a.SetStatus(model.AccountStatus{State: model.StateError, Detail: detail})
	}
}

// wipeSession borra la sesión local (cookies, cursor y todo lo que guarda el adaptador).
func (a *Adapter) wipeSession() {
	ctx, cancel := context.WithTimeout(context.Background(), meta.OpTimeout)
	defer cancel()
	if err := a.KV.Wipe(ctx); err != nil {
		a.Log.Warn("no se pudo borrar la sesión local", "err", meta.RedactErr(err))
	}
}

// Stop corta la conexión y avisa; no toca la sesión guardada.
func (a *Adapter) Stop(ctx context.Context) error {
	a.stopRun(ctx)
	a.SetStatus(model.AccountStatus{State: model.StateDisconnected})
	return nil
}

// Logout cierra la conexión y borra la sesión local. Instagram no ofrece una llamada para cerrar la sesión web
// de las cookies: sigue figurando en «Actividad de inicio de sesión» hasta que el usuario la cierre allí.
func (a *Adapter) Logout(ctx context.Context) error {
	a.stopRun(ctx)
	if err := a.KV.Ensure(ctx); err == nil {
		_ = a.KV.Wipe(ctx)
	}
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

// restart vuelve a abrir la sesión desde cero (cuando Instagram pide una nueva instantánea).
func (a *Adapter) restart() {
	defer a.RecoverLoop("reinicio")
	ctx, cancel := context.WithTimeout(context.Background(), meta.OpTimeout)
	defer cancel()
	_ = a.KV.Del(ctx, meta.KeySeq)
	ck, err := meta.LoadCookies(ctx, a.KV)
	if err != nil || len(ck) == 0 {
		return
	}
	a.stopRun(ctx)
	a.startRun(ck)
}

// ---------------------------------------------------------------------------------------------
// Eventos de la librería

func (a *Adapter) currentGen() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gen
}

func (a *Adapter) handleEvent(ctx context.Context, gen int, raw slidetypes.ClientEvent) error {
	if gen != a.currentGen() {
		return nil
	}
	defer a.RecoverLoop("evento")
	switch evt := raw.(type) {
	case *slidetypes.Connected:
		a.live.Store(true)
		a.SetStatus(model.AccountStatus{State: model.StateConnected})
		go func() {
			defer a.RecoverLoop("arranque")
			a.onConnected(ctx)
		}()
	case *slidetypes.Disconnected:
		a.live.Store(false)
		if dgw.IsUnauthorized(evt.Error) || errors.Is(evt.Error, instameow.ErrUserIDMissing) {
			a.Log.Warn("Instagram cerró la sesión del socket")
			a.wipeSession()
			a.SetStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: "Instagram cerró esta sesión. Vuelve a vincular la cuenta para usarla."})
			return errors.New("sesión no autorizada")
		}
		if evt.FailureCount > 5 && errors.Is(evt.Error, instameow.ErrMainStreamClosed) {
			a.SetStatus(model.AccountStatus{State: model.StateError, Detail: "No se pudo mantener la conexión con Instagram. Revisa tu conexión a internet."})
			return errors.New("demasiados cierres del flujo principal")
		}
		if a.LastStatus().State == model.StateConnected {
			a.SetStatus(model.AccountStatus{State: model.StateConnecting, Detail: "Reconectando con Instagram…"})
		}
	case *slidetypes.AuthError:
		problem, detail := meta.ClassifyAuthError("Instagram", "instagram.com", evt.Error)
		a.Log.Warn("Instagram rechazó la sesión", "problem", int(problem), "err", meta.RedactErr(evt.Error))
		if problem != meta.ProblemTransient {
			a.wipeSession()
			a.SetStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: detail})
		}
	case *slidetypes.SeqIDUpdate:
		a.saveSeq(ctx, evt.SeqID, evt.Timestamp)
	case *slidetypes.ReconnectionStateUpdate:
		// no se guarda el estado de la librería: al reconectar se vuelve a cargar el índice
	case *slidetypes.ResnapshotRequired:
		go a.restart()
	case *slidetypes.Delta:
		a.onDelta(ctx, evt)
	case *slidetypes.TypingNotification:
		if t, ok := a.conv.Typing(evt); ok {
			ttl := time.Duration(evt.TTL) * time.Second
			if ttl < typingMinTTL {
				ttl = typingMinTTL
			}
			a.emitTyping(t, ttl)
		}
	}
	return nil
}

// onDelta traduce un aviso de Instagram; si es de un hilo que no se conoce, lo pide antes.
func (a *Adapter) onDelta(ctx context.Context, d *slidetypes.Delta) {
	a.evMu.Lock()
	defer a.evMu.Unlock()
	lk := LookupFunc{OutgoingUpTo: a.local.OutgoingUpTo}
	res, ok := a.conv.Delta(ctx, d, lk)
	if !ok {
		chat := d.ThreadIGID
		if nm, isNew := d.Data.(*slidetypes.NewMessageEvent); isNew && chat == "" && nm.Message != nil {
			chat = nm.Message.ThreadFBID
		}
		if err := a.fetchThread(ctx, chat); err != nil {
			a.Log.Warn("no se pudo pedir un chat nuevo", "chat", meta.ShortID(chat), "err", meta.RedactErr(err))
		}
		res, _ = a.conv.Delta(ctx, d, lk)
	}
	if res == nil {
		return
	}
	a.afterConvert(res)
	for _, ev := range res.Events() {
		a.Emit(ev)
	}
}

// afterConvert recuerda el chat de cada mensaje y pide las fotos que falten.
func (a *Adapter) afterConvert(res *Result) {
	for i := range res.Messages {
		a.local.Remember(res.Messages[i].ChatNativeID, res.Messages[i].NativeID)
	}
	for id, u := range res.Avatars {
		a.avatars.Enqueue(id, u)
	}
}

// emitTyping entrega «escribiendo» y programa el aviso de fin.
func (a *Adapter) emitTyping(t model.TypingEvent, ttl time.Duration) {
	ev := t
	a.Emit(model.Event{Type: model.EvTyping, Typing: &ev, ChatKey: ev.ChatKey})
	key := string(ev.ChatKey) + "|" + ev.PeerID
	a.typers.Touch(key, ev.Typing, ttl, func() {
		end := ev
		end.Typing = false
		a.Emit(model.Event{Type: model.EvTyping, Typing: &end, ChatKey: end.ChatKey})
	})
}

// deliverMailbox entrega los chats de la bandeja como history.batch (sin mensajes: en la bandeja vienen a medias).
func (a *Adapter) deliverMailbox(mb *slidetypes.Mailbox) {
	if mb == nil {
		return
	}
	a.evMu.Lock()
	defer a.evMu.Unlock()
	merged := &Result{Avatars: map[string]string{}}
	add := func(t *slidetypes.ThreadInfo) {
		if t == nil {
			return
		}
		r := a.conv.Thread(t)
		merged.Chats = append(merged.Chats, r.Chats...)
		merged.Persons = append(merged.Persons, r.Persons...)
	}
	for _, e := range mb.ThreadsByFolder.Edges {
		add(e.Node.AsIGDirectThread)
	}
	for _, p := range mb.PinnedThreadsV2 {
		add(p.AsIGDirectThread)
	}
	for id, u := range a.conv.AvatarURLs() {
		a.avatars.Enqueue(id, u)
	}
	b := merged.History(1)
	a.Emit(model.Event{Type: model.EvHistoryBatch, History: &b})
}

// onConnected: tras conectar, sincroniza la bandeja y carga los últimos mensajes de los chats recientes.
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
	a.backfillRecent(ctx)
}

// ---------------------------------------------------------------------------------------------
// Cliente en uso

// liveClient devuelve el cliente si está conectado; si no, ErrNotConnected.
func (a *Adapter) liveClient() (*instameow.Client, error) {
	a.mu.Lock()
	cli := a.client
	a.mu.Unlock()
	if cli == nil || !a.live.Load() {
		return nil, fmt.Errorf("instagram: %w", port.ErrNotConnected)
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
	case errors.Is(err, instameow.ErrThreadNotFound):
		return fmt.Errorf("instagram: no se encontró el chat: %w", port.ErrNotFound)
	}
	problem, _ := meta.ClassifyAuthError("Instagram", "instagram.com", err)
	if problem == meta.ProblemLoggedOut {
		return fmt.Errorf("instagram: %s: la sesión ya no es válida: %w", action, port.ErrNotConnected)
	}
	a.Log.Warn("error de Instagram", "action", action, "err", meta.RedactErr(err))
	return fmt.Errorf("instagram: %s: %s: %w", action, meta.Redact(err.Error()), port.ErrInvalid)
}
