// Package telegram: adaptador de Telegram (cuenta personal por MTProto) sobre gotd/td
// (08-ADAPTADOR-TELEGRAM).
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	glog "github.com/gotd/log"
	"github.com/gotd/log/logslog"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"dconnector/internal/buildinfo"
	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// opTimeout: tope de las llamadas de red cortas que hace el adaptador por su cuenta.
	opTimeout = 30 * time.Second
	// stopWait: cuánto espera Stop a que termine la conexión.
	stopWait = 5 * time.Second
	// floodMaxWait: espera máxima que se acepta ante un FLOOD_WAIT; más que eso se devuelve ErrRateLimited.
	floodMaxWait = 20 * time.Second
	// authReadyWait: cuánto espera BeginPairing a que haya conexión para pedir el código.
	authReadyWait = 30 * time.Second
)

// baseCapabilities: capacidades de una cuenta de Telegram (08 §11). PostStatus se completa con lo
// que diga StoriesCanSendStory al conectar.
func baseCapabilities(postStatus bool) capability.Set {
	return capability.Set{
		SendText: true, SendImage: true, SendVideo: true, SendAudio: true, SendVoice: true,
		SendDocument: true, SendSticker: true, SendLocation: true, SendContact: true,
		SendPoll: true, VotePoll: true,
		Reply: true, Forward: true, React: true, MaxReactionsPerUser: 1,
		Edit: true, EditWindow: 48 * time.Hour,
		DeleteForEveryone: true, DeleteWindow: 0,
		ReadReceipts: true, Typing: true, Mentions: true, Groups: true, StartChat: true,
		Statuses: true, PostStatus: postStatus, StatusViewers: true, StatusAudience: true, StatusReact: true,
		HistoryOnDemand: true,
		MaxFileSize:     2000 << 20, MaxImageSize: 10 << 20, MaxVideoSize: 2000 << 20, MaxTextLen: 4096,
	}
}

// Adapter: una instancia por cuenta de Telegram.
type Adapter struct {
	port.Base

	deps port.AdapterDeps
	acc  model.Account
	log  *slog.Logger

	kv    *kvStore
	sess  *kvSession
	peers *peerCache
	pair  *pairFlow
	local *localState

	avatars      *avatarQueue
	typingMu     sync.Mutex
	typingTimers map[string]*time.Timer

	selfID     atomic.Int64
	postStatus atomic.Bool
	authLost   atomic.Bool
	live       atomic.Bool // conectado y con actualizaciones en marcha

	mu         sync.Mutex
	sink       port.EventSink
	runCtx     context.Context
	cancel     context.CancelFunc
	runDone    chan struct{}
	client     *telegram.Client
	api        *tg.Client
	lastStatus model.AccountStatus
	authReady  chan struct{}
	authDone   chan struct{}
	authOnce   sync.Once
	doneOnce   sync.Once
	gen        int
}

var _ port.ChannelAdapter = (*Adapter)(nil)

// New es la fábrica registrada en channel.Registry.
func New(deps port.AdapterDeps) (port.ChannelAdapter, error) {
	if deps.DB == nil {
		return nil, errors.New("telegram: falta la base de datos de la cuenta")
	}
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("channel", string(model.ChannelTelegram), "accountId", string(deps.Account.ID))
	deps.Log = log
	a := &Adapter{deps: deps, acc: deps.Account, log: log}
	a.kv = newKV(deps.DB)
	a.sess = &kvSession{kv: a.kv}
	a.peers = newPeerCache(a.kv, a.selfID.Load)
	a.local = newLocalState(deps.DB, a.kv)
	a.avatars = newAvatarQueue()
	a.pair = newPairFlow(a.setStatus, a.authFinished)
	return a, nil
}

func (a *Adapter) Channel() model.Channel { return model.ChannelTelegram }

// Capabilities: las de la cuenta (chat == nil) o las de un chat concreto (08 §11, última línea).
func (a *Adapter) Capabilities(chat *model.Chat) capability.Set {
	caps := baseCapabilities(a.postStatus.Load())
	if chat == nil {
		return caps
	}
	if chat.ReadOnly || a.chatReadOnly(chat.NativeID) {
		return caps.ReadOnly()
	}
	return caps
}

// chatReadOnly: canales de difusión sin permiso de publicar y supergrupos donde no se puede escribir.
func (a *Adapter) chatReadOnly(native string) bool {
	kind, id, ok := parseNative(native)
	if !ok || kind != kindChannel {
		return false
	}
	if ch, ok := a.peers.Channel(id); ok {
		return channelReadOnly(ch)
	}
	return false
}

// ---------------------------------------------------------------------------------------------
// Configuración

// credentials lee api_id y api_hash de Account.Config.
func credentials(cfg map[string]string) (int, string, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(cfg["api_id"]))
	hash := strings.TrimSpace(cfg["api_hash"])
	if err != nil || id <= 0 || hash == "" {
		return 0, "", false
	}
	return id, hash, true
}

func (a *Adapter) hasKeys() bool {
	_, _, ok := credentials(a.acc.Config)
	return ok
}

// ---------------------------------------------------------------------------------------------
// Estado y eventos

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
	ev.AccountID, ev.Channel = a.acc.ID, model.ChannelTelegram
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

// recoverLoop registra un pánico de una goroutine de larga vida sin tumbar el proceso (02 §6).
func (a *Adapter) recoverLoop(where string) {
	if r := recover(); r != nil {
		a.log.Error("pánico recuperado", "where", where, "panic", fmt.Sprint(r))
	}
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

// conv arma el traductor puro con lo que sabe el adaptador ahora.
func (a *Adapter) conv(e *tg.Entities) converter {
	view := peerView(a.peers)
	if e != nil {
		view = chainView{entitiesView{*e}, a.peers}
	}
	return converter{acc: a.acc.ID, selfID: a.selfID.Load(), view: view, quote: a.local.quoteLookup}
}

// ---------------------------------------------------------------------------------------------
// Ciclo de vida (08 §2)

// Start prepara la conexión y vuelve enseguida; la conexión vive en goroutines propias.
func (a *Adapter) Start(ctx context.Context, sink port.EventSink) error {
	a.mu.Lock()
	a.sink = sink
	if a.cancel != nil {
		a.cancel()
	}
	run, cancel := context.WithCancel(ctx)
	a.runCtx, a.cancel = run, cancel
	a.gen++
	gen := a.gen
	done := make(chan struct{})
	a.runDone = done
	a.authReady = make(chan struct{})
	a.authDone = make(chan struct{})
	a.authOnce, a.doneOnce = sync.Once{}, sync.Once{}
	a.authLost.Store(false)
	a.mu.Unlock()

	if err := a.kv.ensure(run); err != nil {
		cancel()
		close(done)
		a.setStatus(model.AccountStatus{State: model.StateError, Detail: "No se pudo preparar la sesión de Telegram."})
		return fmt.Errorf("telegram: %w", err)
	}
	if !a.hasKeys() {
		close(done)
		a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedNone, Detail: detailMissingKeys})
		return nil
	}
	a.setStatus(model.AccountStatus{State: model.StateConnecting})
	go func() {
		defer close(done)
		defer a.recoverLoop("run")
		a.runLoop(run, gen)
	}()
	return nil
}

// gotdLogger: registrador de gotd sobre slog, solo avisos y errores (nunca contenido de mensajes).
func gotdLogger(log *slog.Logger) glog.Logger {
	return logslog.New(slog.New(minLevelHandler{h: log.Handler(), min: slog.LevelWarn}).With("lib", "gotd"))
}

type minLevelHandler struct {
	h   slog.Handler
	min slog.Level
}

func (m minLevelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= m.min && m.h.Enabled(ctx, l)
}
func (m minLevelHandler) Handle(ctx context.Context, r slog.Record) error { return m.h.Handle(ctx, r) }
func (m minLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return minLevelHandler{h: m.h.WithAttrs(attrs), min: m.min}
}
func (m minLevelHandler) WithGroup(name string) slog.Handler {
	return minLevelHandler{h: m.h.WithGroup(name), min: m.min}
}

// newClient crea el cliente MTProto y el gestor de actualizaciones de esta conexión.
func (a *Adapter) newClient() (*telegram.Client, *updates.Manager) {
	apiID, apiHash, _ := credentials(a.acc.Config)
	zl := gotdLogger(a.log)
	hasher := &kvAccessHasher{kv: a.kv}
	gaps := updates.New(updates.Config{
		Handler:      a.dispatcher(),
		Storage:      &kvUpdatesState{kv: a.kv},
		AccessHasher: hasher,
		Logger:       zl,
		OnChannelTooLong: func(channelID int64) {
			a.log.Warn("hueco en un canal: algunos mensajes pueden faltar", "channel", shortID(channelNative(channelID)))
		},
		OnTooLong: func() { a.log.Warn("hueco largo de actualizaciones: algunos mensajes pueden faltar") },
	})
	waiter := floodwait.NewSimpleWaiter().WithMaxWait(floodMaxWait).WithMaxRetries(3)
	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: a.sess,
		UpdateHandler:  gaps,
		Middlewares:    []telegram.Middleware{waiter},
		Device: telegram.DeviceConfig{
			DeviceModel: "dConnector", SystemVersion: "Windows", AppVersion: buildinfo.Version,
			LangCode: "es", SystemLangCode: "es",
		},
		Logger: zl,
		OnAuthLost: func(ctx context.Context, err error) error {
			a.authLost.Store(true)
			return err
		},
	})
	return client, gaps
}

// runLoop mantiene la conexión hasta que se cancele el contexto o falle.
func (a *Adapter) runLoop(ctx context.Context, gen int) {
	client, gaps := a.newClient()
	a.mu.Lock()
	a.client, a.api = client, client.API()
	a.mu.Unlock()
	err := client.Run(ctx, func(ctx context.Context) error { return a.session(ctx, client, gaps) })
	a.live.Store(false)
	a.pair.detach()
	a.mu.Lock()
	if a.gen == gen {
		a.client, a.api = nil, nil
	}
	a.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	a.handleRunEnd(err)
}

// handleRunEnd traduce el fin de la conexión: sesión perdida (logged_out) o error (se reintenta).
func (a *Adapter) handleRunEnd(err error) {
	if a.authLost.Load() || isAuthLost(err) {
		a.log.Warn("Telegram cerró la sesión")
		a.wipeLocal()
		a.setStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: "Telegram cerró esta sesión. Vuelve a vincular la cuenta para usarla."})
		return
	}
	if err != nil {
		a.log.Warn("la conexión con Telegram terminó", "err", redactErr(err))
	}
	a.setStatus(model.AccountStatus{State: model.StateError, Detail: "No se pudo conectar con Telegram. Revisa tu conexión a internet."})
}

func isAuthLost(err error) bool {
	if err == nil {
		return false
	}
	var lost *telegram.AuthLostError
	return errors.As(err, &lost) || telegram.IsAuthLost(err) || auth.IsUnauthorized(err)
}

// wipeLocal borra la sesión local y todo lo que guarda el adaptador.
func (a *Adapter) wipeLocal() {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := a.kv.wipe(ctx); err != nil {
		a.log.Warn("no se pudo borrar la sesión local", "err", redactErr(err))
	}
	a.peers.reset()
	a.local.reset()
	a.selfID.Store(0)
	a.postStatus.Store(false)
}

// authFinished avisa al arranque de la sesión que la vinculación terminó.
func (a *Adapter) authFinished() {
	a.mu.Lock()
	done := a.authDone
	a.mu.Unlock()
	a.doneOnce.Do(func() {
		if done != nil {
			close(done)
		}
	})
}

// waitAuthReady espera a que la conexión esté lista para vincular.
func (a *Adapter) waitAuthReady(ctx context.Context) error {
	a.mu.Lock()
	ready := a.authReady
	a.mu.Unlock()
	if ready == nil {
		return fmt.Errorf("telegram: la cuenta no está arrancada: %w", port.ErrNotConnected)
	}
	t := time.NewTimer(authReadyWait)
	defer t.Stop()
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return fmt.Errorf("telegram: Telegram tardó demasiado en responder, inténtalo de nuevo: %w", port.ErrNotConnected)
	}
}

// session es el cuerpo de client.Run: autenticación, persona propia y bucle de actualizaciones.
func (a *Adapter) session(ctx context.Context, client *telegram.Client, gaps *updates.Manager) error {
	st, err := client.Auth().Status(ctx)
	if err != nil {
		return err
	}
	self := st.User
	if !st.Authorized {
		a.pair.attach(realAuth{c: client.Auth()})
		a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedPhone, Detail: detailNeedPhone})
		a.mu.Lock()
		ready, done := a.authReady, a.authDone
		a.mu.Unlock()
		a.authOnce.Do(func() {
			if ready != nil {
				close(ready)
			}
		})
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
		a.pair.detach()
		if self, err = client.Self(ctx); err != nil {
			return err
		}
	}
	if self == nil {
		if self, err = client.Self(ctx); err != nil {
			return err
		}
	}
	a.selfID.Store(self.ID)
	a.peers.feed(ctx, tg.Entities{Users: map[int64]*tg.User{self.ID: self}})
	// Primero la persona propia: el Router rellena SelfID/SelfName/SelfHandle con ella.
	p := personOf(a.acc.ID, self)
	a.local.noteUserSig(self)
	a.emit(model.Event{Type: model.EvPerson, Person: &p})

	return gaps.Run(ctx, client.API(), self.ID, updates.AuthOptions{
		IsBot: false,
		OnStart: func(ctx context.Context) {
			a.onStarted(ctx)
		},
	})
}

// onStarted: la conexión y las actualizaciones están en marcha.
func (a *Adapter) onStarted(ctx context.Context) {
	a.live.Store(true)
	a.avatars.resetSeen()
	a.setStatus(model.AccountStatus{State: model.StateConnected})
	go a.avatarWorker(ctx)
	go func() {
		defer a.recoverLoop("arranque")
		a.refreshStoryCapability(ctx)
		for attempt := 1; attempt <= 3; attempt++ {
			err := a.SyncChats(ctx)
			if err == nil || ctx.Err() != nil {
				break
			}
			a.log.Warn("no se pudieron sincronizar los chats", "attempt", attempt, "err", redactErr(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt) * 20 * time.Second):
			}
		}
		a.storyLoop(ctx)
	}()
}

// Stop corta la conexión y avisa; no toca la sesión guardada.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	cancel, done := a.cancel, a.runDone
	a.cancel, a.runCtx = nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		t := time.NewTimer(stopWait)
		select {
		case <-done:
		case <-ctx.Done():
		case <-t.C:
		}
		t.Stop()
	}
	a.live.Store(false)
	a.setStatus(model.AccountStatus{State: model.StateDisconnected})
	return nil
}

// Logout cierra la sesión en Telegram (dispositivo vinculado) y borra la sesión local.
func (a *Adapter) Logout(ctx context.Context) error {
	var serverErr error
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	hasSession := a.sess.hasSession(ctx)
	switch {
	case client != nil && a.live.Load():
		lctx, cancel := context.WithTimeout(ctx, opTimeout)
		if _, err := client.API().AuthLogOut(lctx); err != nil {
			serverErr = err
		}
		cancel()
	case hasSession:
		serverErr = port.ErrNotConnected
	}
	// La sesión local se borra siempre: primero se cierra la conexión.
	a.mu.Lock()
	cancel, done := a.cancel, a.runDone
	a.cancel, a.runCtx = nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		t := time.NewTimer(stopWait)
		select {
		case <-done:
		case <-ctx.Done():
		case <-t.C:
		}
		t.Stop()
	}
	a.live.Store(false)
	if err := a.kv.ensure(ctx); err == nil {
		_ = a.kv.wipe(ctx)
	}
	a.peers.reset()
	a.local.reset()
	a.selfID.Store(0)
	a.postStatus.Store(false)
	a.setStatus(model.AccountStatus{State: model.StateLoggedOut, Detail: "Sesión cerrada. Vuelve a vincular la cuenta para usarla."})
	if serverErr != nil {
		a.log.Warn("no se pudo avisar a Telegram del cierre de sesión", "err", redactErr(serverErr))
		return fmt.Errorf("telegram: la sesión local se cerró, pero no se pudo avisar a Telegram (el dispositivo puede seguir vinculado en la app): %w", serverErr)
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Cliente en uso

// liveAPI devuelve el cliente de la API si está conectado; si no, ErrNotConnected.
func (a *Adapter) liveAPI() (*tg.Client, error) {
	a.mu.Lock()
	api := a.api
	a.mu.Unlock()
	if api == nil || !a.live.Load() {
		return nil, fmt.Errorf("telegram: %w", port.ErrNotConnected)
	}
	return api, nil
}

func (a *Adapter) telegramClient() *telegram.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client
}

// ---------------------------------------------------------------------------------------------
// Errores

// redactErr deja un texto de error seguro para el registro (sin números de teléfono completos).
func redactErr(err error) string {
	if err == nil {
		return ""
	}
	return redactDigits(err.Error())
}

// redactDigits oculta las rachas largas de dígitos (números de teléfono) dejando los últimos 4.
func redactDigits(s string) string {
	var b strings.Builder
	flush := func(start int, end int) {
		seg := s[start:end]
		if len(seg) >= 7 {
			b.WriteString("…" + seg[len(seg)-4:])
		} else {
			b.WriteString(seg)
		}
	}
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			flush(start, i)
			start = -1
		}
		b.WriteByte(c)
	}
	if start >= 0 {
		flush(start, len(s))
	}
	return b.String()
}

// mapErr traduce un error de gotd a los errores del dominio (02 §6), con texto en español.
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
	if d, ok := tgerr.AsFloodWait(err); ok {
		return fmt.Errorf("telegram: Telegram pidió esperar %s antes de %s: %w", d.Round(time.Second), action, port.ErrRateLimited)
	}
	if isAuthLost(err) {
		a.authLost.Store(true)
		return fmt.Errorf("telegram: %s: la sesión ya no es válida: %w", action, port.ErrNotConnected)
	}
	switch {
	case tgerr.Is(err, "PEER_ID_INVALID", "CHAT_ID_INVALID", "CHANNEL_INVALID", "USER_ID_INVALID", "MESSAGE_ID_INVALID",
		"MSG_ID_INVALID", "INPUT_USER_DEACTIVATED", "USERNAME_NOT_OCCUPIED"):
		return fmt.Errorf("telegram: no se encontró el chat o el mensaje: %w", port.ErrNotFound)
	case tgerr.Is(err, "CHAT_WRITE_FORBIDDEN", "CHAT_SEND_PLAIN_FORBIDDEN", "CHAT_SEND_MEDIA_FORBIDDEN", "CHAT_SEND_STICKERS_FORBIDDEN",
		"CHAT_SEND_POLL_FORBIDDEN", "CHAT_SEND_VOICES_FORBIDDEN", "CHAT_SEND_PHOTOS_FORBIDDEN", "CHAT_SEND_VIDEOS_FORBIDDEN",
		"CHAT_SEND_DOCS_FORBIDDEN", "CHAT_SEND_AUDIOS_FORBIDDEN", "USER_BANNED_IN_CHANNEL", "CHAT_ADMIN_REQUIRED", "CHANNEL_PRIVATE"):
		return fmt.Errorf("telegram: no tienes permiso para hacer eso en este chat: %w", port.ErrInvalid)
	case tgerr.Is(err, "USER_IS_BLOCKED", "YOU_BLOCKED_USER", "USER_PRIVACY_RESTRICTED", "PRIVACY_PREMIUM_REQUIRED"):
		return fmt.Errorf("telegram: esa persona no puede recibir tus mensajes: %w", port.ErrInvalid)
	case tgerr.Is(err, "MESSAGE_EDIT_TIME_EXPIRED"):
		return fmt.Errorf("telegram: ya pasó el tiempo para editar ese mensaje: %w", port.ErrInvalid)
	case tgerr.Is(err, "MESSAGE_AUTHOR_REQUIRED"):
		return fmt.Errorf("telegram: solo puedes editar tus propios mensajes: %w", port.ErrInvalid)
	case tgerr.Is(err, "MESSAGE_TOO_LONG"):
		return fmt.Errorf("telegram: el mensaje es demasiado largo: %w", port.ErrInvalid)
	case tgerr.Is(err, "MESSAGE_EMPTY"):
		return fmt.Errorf("telegram: el mensaje está vacío: %w", port.ErrInvalid)
	case tgerr.Is(err, "MEDIA_INVALID", "MEDIA_EMPTY", "PHOTO_INVALID_DIMENSIONS", "PHOTO_SAVE_FILE_INVALID", "IMAGE_PROCESS_FAILED",
		"FILE_PARTS_INVALID", "FILE_PART_EMPTY", "FILE_PART_SIZE_INVALID", "MEDIA_CAPTION_TOO_LONG", "DOCUMENT_INVALID"):
		return fmt.Errorf("telegram: Telegram no aceptó el archivo: %w", port.ErrInvalid)
	case tgerr.Is(err, "POLL_ANSWERS_INVALID", "POLL_OPTION_INVALID", "POLL_QUESTION_INVALID", "OPTIONS_TOO_MUCH", "OPTION_INVALID"):
		return fmt.Errorf("telegram: Telegram no aceptó la encuesta: %w", port.ErrInvalid)
	case tgerr.Is(err, "REACTION_INVALID", "REACTIONS_TOO_MANY"):
		return fmt.Errorf("telegram: no se puede usar esa reacción aquí: %w", port.ErrInvalid)
	case tgerr.Is(err, "STORY_SEND_FLOOD_WEEKLY", "STORY_SEND_FLOOD_MONTHLY", "STORIES_TOO_MUCH"):
		return fmt.Errorf("telegram: llegaste al límite de historias: %w", port.ErrRateLimited)
	}
	a.log.Warn("error de Telegram", "action", action, "err", redactErr(err))
	return fmt.Errorf("telegram: %s: %w", action, err)
}
