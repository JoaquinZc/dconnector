package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/campaign"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// defaultSinkBuffer: tamaño del canal de eventos de cada cuenta (02 §5).
	defaultSinkBuffer = 4096
	// defaultEmitWait: cuánto espera Emit con el canal lleno antes de descartar.
	defaultEmitWait = 2 * time.Second
	// publishedGen: cuántos mensajes recuerda cada generación del conjunto de «ya publicados».
	publishedGen = 20000
	// optOutKeywordsTTL: caché de las palabras de baja.
	optOutKeywordsTTL = time.Minute
)

// Router: eventos de adaptadores → almacén → bus (06 §2).
type Router struct {
	app port.AppStore
	bus *bus.Bus

	// Ajustables en pruebas (se fijan antes de Attach).
	bufSize  int
	emitWait time.Duration
	now      func() time.Time

	mu        sync.RWMutex
	sinks     map[model.AccountID]*accountSink
	outbox    OutboxWriter
	observers []IncomingObserver
	contacts  ContactSync

	kwMu     sync.Mutex
	kwValue  string
	kwLoaded time.Time
}

func NewRouter(app port.AppStore, b *bus.Bus) *Router {
	return &Router{
		app: app, bus: b,
		bufSize: defaultSinkBuffer, emitWait: defaultEmitWait, now: time.Now,
		sinks: map[model.AccountID]*accountSink{},
	}
}

// SetOutbox lo pone main.go con el manager del protocolo.
func (r *Router) SetOutbox(o OutboxWriter) {
	r.mu.Lock()
	r.outbox = o
	r.mu.Unlock()
}

// SetContactSync lo pone main.go con Contacts: cada persona y cada chat directo que se guarda crea o
// completa su contacto (contactos automáticos).
func (r *Router) SetContactSync(cs ContactSync) {
	r.mu.Lock()
	r.contacts = cs
	r.mu.Unlock()
}

func (r *Router) contactSync() ContactSync {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.contacts
}

// AddObserver lo pone main.go con Campaigns.
func (r *Router) AddObserver(o IncomingObserver) {
	if o == nil {
		return
	}
	r.mu.Lock()
	r.observers = append(r.observers, o)
	r.mu.Unlock()
}

// Attach crea el sink de la cuenta (canal con búfer + una goroutine que procesa en orden).
// Si ya había uno para esa cuenta, lo reemplaza (el anterior termina de procesar lo pendiente).
func (r *Router) Attach(acc model.Account, st port.AccountStore) port.EventSink {
	baseCtx, baseCancel := context.WithCancel(context.Background())
	s := &accountSink{
		baseCtx: baseCtx, baseCancel: baseCancel,
		r: r, acc: acc, store: st,
		log:  slog.Default().With("accountId", string(acc.ID), "channel", string(acc.Channel)),
		ch:   make(chan model.Event, r.bufSize),
		stop: make(chan struct{}), done: make(chan struct{}),
		pubCur: map[model.MessageKey]struct{}{},
	}
	r.mu.Lock()
	old := r.sinks[acc.ID]
	r.sinks[acc.ID] = s
	r.mu.Unlock()
	if old != nil {
		old.shutdown(true)
	}
	go s.run()
	return s
}

// SinkFor devuelve el sink de una cuenta ya vinculada.
func (r *Router) SinkFor(id model.AccountID) (port.EventSink, bool) {
	r.mu.RLock()
	s, ok := r.sinks[id]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return s, true
}

// Detach para el sink de la cuenta y descarta lo que quedara en cola.
func (r *Router) Detach(id model.AccountID) { r.detach(id, false) }

// detach quita el sink; con drain procesa antes lo que quedara en cola (el almacén sigue abierto).
func (r *Router) detach(id model.AccountID, drain bool) {
	r.detachCtx(context.Background(), id, drain)
}

// detachCtx es detach con plazo: si ctx termina antes de que se vacíe la cola, se descarta lo que falte
// (con aviso) para que el apagado del servidor respete su límite de 10 s.
func (r *Router) detachCtx(ctx context.Context, id model.AccountID, drain bool) {
	r.mu.Lock()
	s := r.sinks[id]
	delete(r.sinks, id)
	r.mu.Unlock()
	if s != nil {
		s.shutdownCtx(ctx, drain)
	}
}

// Publish publica un evento derivado (no viene de un adaptador y no se guarda en el almacén):
// le pone ID/At/Channel si faltan y lo entrega al bus y al outbox (salvo history.batch y typing).
func (r *Router) Publish(ev model.Event) {
	if ev.ID == "" {
		ev.ID = model.NewID()
	}
	if ev.At == 0 {
		ev.At = r.now().UnixMilli()
	}
	if ev.Channel == "" {
		r.mu.RLock()
		if s := r.sinks[ev.AccountID]; s != nil {
			ev.Channel = s.acc.Channel
		}
		r.mu.RUnlock()
	}
	r.bus.Publish(ev)
	r.enqueue(context.Background(), ev)
}

func (r *Router) enqueue(ctx context.Context, ev model.Event) {
	if ev.Type == model.EvHistoryBatch || ev.Type == model.EvTyping {
		return
	}
	r.mu.RLock()
	o := r.outbox
	r.mu.RUnlock()
	if o != nil {
		o.Enqueue(ctx, ev)
	}
}

func (r *Router) observerList() []IncomingObserver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.observers) == 0 {
		return nil
	}
	return append([]IncomingObserver(nil), r.observers...)
}

// optOutKeywords lee el ajuste optout_keywords con caché de 1 minuto.
func (r *Router) optOutKeywords(ctx context.Context) string {
	r.kwMu.Lock()
	defer r.kwMu.Unlock()
	if r.kwLoaded.IsZero() || r.now().Sub(r.kwLoaded) > optOutKeywordsTTL {
		v, err := r.app.GetSetting(ctx, SettingOptOutKeywords)
		if err != nil || strings.TrimSpace(v) == "" {
			v = defaultsOptOutKeywords
		}
		r.kwValue, r.kwLoaded = v, r.now()
	}
	return r.kwValue
}

// ---------------------------------------------------------------------------------------------
// Sink de una cuenta

type accountSink struct {
	r     *Router
	acc   model.Account // solo lo toca la goroutine run (SelfID, SelfName, SelfHandle)
	store port.AccountStore
	log   *slog.Logger

	ch    chan model.Event
	stop  chan struct{}
	done  chan struct{}
	drain bool // se fija antes de cerrar stop
	once  sync.Once

	// abort corta el vaciado de la cola (apagado con plazo): baseCtx se cancela para que el evento en curso
	// tampoco espere a su propio plazo de 30 s.
	abort      atomic.Bool
	baseCtx    context.Context
	baseCancel context.CancelFunc

	// mensajes ya publicados como message.received/sent (dos generaciones para acotar memoria).
	pubCur, pubOld map[model.MessageKey]struct{}
}

// Emit entrega un evento sin bloquear de forma indefinida: si el canal está lleno espera
// hasta emitWait y luego descarta con WARN.
func (s *accountSink) Emit(ev model.Event) {
	select {
	case <-s.stop:
		return
	default:
	}
	select {
	case s.ch <- ev:
		return
	default:
	}
	t := time.NewTimer(s.r.emitWait)
	defer t.Stop()
	select {
	case s.ch <- ev:
	case <-s.stop:
	case <-t.C:
		s.log.Warn("cola de eventos llena: se descarta el evento", "type", string(ev.Type))
	}
}

func (s *accountSink) shutdown(drain bool) { s.shutdownCtx(context.Background(), drain) }

func (s *accountSink) shutdownCtx(ctx context.Context, drain bool) {
	s.once.Do(func() {
		s.drain = drain
		close(s.stop)
	})
	select {
	case <-s.done:
		return
	case <-ctx.Done():
	}
	if n := len(s.ch); n > 0 {
		s.log.Warn("apagado con plazo vencido: se descartan los eventos que seguían en cola", "pendientes", n)
	}
	s.abort.Store(true)
	s.baseCancel()
	<-s.done
}

func (s *accountSink) run() {
	defer close(s.done)
	defer s.baseCancel()
	for {
		select {
		case ev := <-s.ch:
			s.safeProcess(ev)
		case <-s.stop:
			if s.drain {
				for !s.abort.Load() {
					select {
					case ev := <-s.ch:
						s.safeProcess(ev)
					default:
						return
					}
				}
			}
			return
		}
	}
}

func (s *accountSink) safeProcess(ev model.Event) {
	defer func() {
		if rec := recover(); rec != nil {
			s.log.Error("pánico al procesar un evento", "type", string(ev.Type), "panic", rec)
		}
	}()
	ctx, cancel := context.WithTimeout(s.baseCtx, 30*time.Second)
	defer cancel()
	s.process(ctx, ev)
}

func (s *accountSink) wasPublished(k model.MessageKey) bool {
	if _, ok := s.pubCur[k]; ok {
		return true
	}
	_, ok := s.pubOld[k]
	return ok
}

func (s *accountSink) markPublished(k model.MessageKey) {
	if len(s.pubCur) >= publishedGen {
		s.pubOld, s.pubCur = s.pubCur, map[model.MessageKey]struct{}{}
	}
	s.pubCur[k] = struct{}{}
}

// process aplica los pasos de 06 §2 a un evento.
func (s *accountSink) process(ctx context.Context, ev model.Event) {
	// 1. identificación
	ev.ID = model.NewID()
	ev.AccountID = s.acc.ID
	ev.Channel = s.acc.Channel
	if ev.At == 0 {
		ev.At = s.r.now().UnixMilli()
	}
	// 2. claves
	s.fillKeys(&ev)

	// 3 y 4. persistir y completar
	var (
		newMsg    bool // mensaje nuevo (message.received/sent con created == true)
		keepGoing = true
	)
	switch ev.Type {
	case model.EvChatUpsert:
		keepGoing = s.persistChat(ctx, &ev)
	case model.EvChatRemoved:
		keepGoing = s.persistChatRemoved(ctx, &ev)
	case model.EvMessageNew, model.EvMessageSent:
		newMsg, keepGoing = s.persistMessage(ctx, &ev)
	case model.EvMessageUpdate:
		keepGoing = s.persistUpdate(ctx, &ev)
	case model.EvReaction:
		keepGoing = s.persistReaction(ctx, &ev)
	case model.EvReceipt:
		if ev.Receipt == nil {
			keepGoing = false
			break
		}
		if err := s.store.ApplyReceipt(ctx, *ev.Receipt); err != nil {
			s.logPersistErr(ev, err)
			keepGoing = false
		}
	case model.EvPerson:
		keepGoing = s.persistPerson(ctx, &ev)
	case model.EvStatusUpsert:
		if ev.Status == nil {
			keepGoing = false
			break
		}
		if err := s.store.UpsertStatus(ctx, *ev.Status); err != nil {
			s.logPersistErr(ev, err)
			keepGoing = false
		}
		// La URL del medio la calcula el almacén al leer; el estado del adaptador no la trae.
		ev.Status.Media = withMediaURL(s.acc.ID, ev.Status.Media)
	case model.EvStatusView:
		if ev.StatusView == nil {
			keepGoing = false
			break
		}
		if err := s.store.UpsertStatusView(ctx, *ev.StatusView); err != nil {
			s.logPersistErr(ev, err)
			keepGoing = false
		}
	case model.EvHistoryBatch:
		keepGoing = s.persistHistory(ctx, &ev)
	case model.EvAccountStatus, model.EvCapabilities, model.EvTyping:
		// solo se publican
	default:
		s.log.Debug("evento de tipo desconocido: solo se publica", "type", string(ev.Type))
	}
	if !keepGoing {
		return
	}

	// 5. publicar
	s.r.bus.Publish(ev)
	s.r.enqueue(ctx, ev)

	if ev.Type == model.EvMessageNew || ev.Type == model.EvMessageSent {
		s.markPublished(ev.Message.Key)
	}
	if newMsg && ev.Message != nil {
		s.publishChatSnapshot(ctx, ev)
		if ev.Type == model.EvMessageNew {
			s.notifyIncoming(ctx, *ev.Message)
		}
	}
}

func (s *accountSink) logPersistErr(ev model.Event, err error) {
	if errors.Is(err, port.ErrNotFound) {
		s.log.Debug("evento sobre algo que aún no existe: se descarta", "type", string(ev.Type))
		return
	}
	s.log.Error("no se pudo guardar el evento", "type", string(ev.Type), "err", err)
}

// ---------------------------------------------------------------------------------------------
// Claves

func (s *accountSink) fixChat(c *model.Chat) {
	c.AccountID, c.Channel = s.acc.ID, s.acc.Channel
	c.Key = model.NewChatKey(s.acc.ID, c.NativeID)
}

func (s *accountSink) fixMessage(m *model.Message, fallbackChat model.ChatKey) {
	if m.ChatNativeID == "" && fallbackChat != "" {
		_, m.ChatNativeID = fallbackChat.Split()
	}
	m.AccountID, m.Channel = s.acc.ID, s.acc.Channel
	m.Key = model.NewMessageKey(s.acc.ID, m.ChatNativeID, m.NativeID)
	m.ChatKey = model.NewChatKey(s.acc.ID, m.ChatNativeID)
}

func (s *accountSink) fillKeys(ev *model.Event) {
	acc := s.acc.ID
	if ev.Chat != nil {
		c := *ev.Chat
		s.fixChat(&c)
		ev.Chat = &c
		if ev.ChatKey == "" {
			ev.ChatKey = c.Key
		}
	}
	if ev.Message != nil {
		m := *ev.Message
		s.fixMessage(&m, ev.ChatKey)
		ev.Message = &m
		if ev.ChatKey == "" {
			ev.ChatKey = m.ChatKey
		}
	}
	if ev.Update != nil {
		u := *ev.Update
		if u.Key == "" && ev.Message != nil {
			u.Key = ev.Message.Key
		}
		ev.Update = &u
	}
	if ev.Reaction != nil {
		r := *ev.Reaction
		if r.MessageKey == "" && ev.Message != nil {
			r.MessageKey = ev.Message.Key
		}
		ev.Reaction = &r
	}
	if ev.Receipt != nil {
		r := *ev.Receipt
		if r.ChatKey == "" {
			r.ChatKey = ev.ChatKey
		}
		ev.Receipt = &r
	}
	if ev.Typing != nil {
		t := *ev.Typing
		if t.ChatKey == "" {
			t.ChatKey = ev.ChatKey
		}
		ev.Typing = &t
	}
	if ev.Person != nil {
		p := *ev.Person
		p.AccountID = acc
		ev.Person = &p
	}
	if ev.Status != nil {
		st := *ev.Status
		st.AccountID, st.Channel = acc, s.acc.Channel
		if st.Key == "" {
			st.Key = string(acc) + "|" + st.NativeID
		}
		ev.Status = &st
	}
	if ev.StatusView != nil {
		v := *ev.StatusView
		ev.StatusView = &v
	}
	if ev.AccountStatus != nil {
		a := *ev.AccountStatus
		a.AccountID = acc
		if a.Since == 0 {
			a.Since = ev.At
		}
		ev.AccountStatus = &a
	}
	if ev.History != nil {
		h := *ev.History
		h.Chats = append([]model.Chat(nil), h.Chats...)
		h.Messages = append([]model.Message(nil), h.Messages...)
		h.Persons = append([]model.Person(nil), h.Persons...)
		for i := range h.Chats {
			s.fixChat(&h.Chats[i])
		}
		for i := range h.Messages {
			s.fixMessage(&h.Messages[i], "")
		}
		for i := range h.Persons {
			h.Persons[i].AccountID = acc
		}
		ev.History = &h
	}
}

// ---------------------------------------------------------------------------------------------
// Persistencia por tipo

// resolveContact (paso 4): en chats directos, el contacto unificado por identidad.
func (s *accountSink) resolveContact(ctx context.Context, c *model.Chat) {
	if c.Kind != model.ChatDirect {
		return
	}
	peer := c.PeerNativeID
	if peer == "" {
		peer = c.NativeID
	}
	ct, err := s.r.app.FindContactByIdentity(ctx, s.acc.ID, peer)
	switch {
	case err == nil:
		c.ContactID = ct.ID
	case errors.Is(err, port.ErrNotFound):
		c.ContactID = ""
	default:
		s.log.Warn("no se pudo buscar el contacto del chat", "err", err)
	}
}

// keepLocalMarks: Messenger e Instagram no permiten fijar ni silenciar chats en el servidor; esas
// marcas son solo locales (Inbox.Pin/Mute las aplican con PatchChat). El chat.updated del adaptador
// siempre las trae apagadas y UpsertChat las reemplaza, así que se conservan las guardadas.
func (s *accountSink) keepLocalMarks(ctx context.Context, c *model.Chat) {
	if s.acc.Channel != model.ChannelMessenger && s.acc.Channel != model.ChannelInstagram {
		return
	}
	if cur, err := s.store.GetChat(ctx, c.NativeID); err == nil {
		c.Pinned, c.MutedUntil = cur.Pinned, cur.MutedUntil
	}
}

func (s *accountSink) persistChat(ctx context.Context, ev *model.Event) bool {
	if ev.Chat == nil || ev.Chat.NativeID == "" {
		return false
	}
	// El contacto se resuelve antes de guardar para que quede en el chat.
	s.resolveContact(ctx, ev.Chat)
	s.keepLocalMarks(ctx, ev.Chat)
	if err := s.store.UpsertChat(ctx, *ev.Chat); err != nil {
		s.logPersistErr(*ev, err)
		return false
	}
	// Un adaptador puede mandar un chat parcial (UnreadCount < 0, LastMessageAt 0…): lo que se
	// publica y se encola es el chat completo tal como quedó guardado.
	contactID := ev.Chat.ContactID
	if full, err := s.store.GetChat(ctx, ev.Chat.NativeID); err == nil {
		full.ContactID = contactID
		*ev.Chat = full
	} else if ev.Chat.UnreadCount < 0 {
		ev.Chat.UnreadCount = 0 // «sin cambio» (-1) nunca sale de aquí
	}
	// Contactos automáticos: un chat directo de alguien sin contacto lo crea. Si lo hay, el chat que
	// se publica ya lleva su contacto.
	if ev.Chat.Kind == model.ChatDirect && ev.Chat.ContactID == "" {
		if s.syncContacts(ctx, nil, []model.Chat{*ev.Chat}) {
			s.resolveContact(ctx, ev.Chat)
		}
	}
	return true
}

// syncContacts avisa a los contactos automáticos de personas y chats recién guardados. Nunca
// falla: un error solo se registra, el evento sigue su camino. Devuelve si se llegó a ejecutar.
func (s *accountSink) syncContacts(ctx context.Context, persons []model.Person, chats []model.Chat) bool {
	cs := s.r.contactSync()
	if cs == nil || (len(persons) == 0 && len(chats) == 0) {
		return false
	}
	ran := false
	callObserver(s.log, func() {
		if _, err := cs.SyncAccount(ctx, s.acc, s.store, persons, chats); err != nil {
			s.log.Warn("no se pudieron actualizar los contactos automáticos", "err", err)
			return
		}
		ran = true
	})
	return ran
}

func (s *accountSink) persistChatRemoved(ctx context.Context, ev *model.Event) bool {
	_, native := ev.ChatKey.Split()
	if native == "" {
		return false
	}
	if err := s.store.DeleteChat(ctx, native); err != nil && !errors.Is(err, port.ErrNotFound) {
		s.logPersistErr(*ev, err)
		return false
	}
	return true
}

// persistMessage guarda message.received / message.sent. Devuelve (nuevo, continuar).
// Si el mensaje ya existía y ya se había publicado, el evento pasa a message.updated.
func (s *accountSink) persistMessage(ctx context.Context, ev *model.Event) (bool, bool) {
	m := ev.Message
	if m == nil || m.NativeID == "" || m.ChatNativeID == "" {
		return false, false
	}
	published := s.wasPublished(m.Key)
	created, err := s.store.UpsertMessage(ctx, *m)
	if err != nil {
		s.logPersistErr(*ev, err)
		return false, false
	}
	if created || !published {
		s.completeMedia(ctx, m)
		return created, true
	}
	full, gerr := s.store.GetMessage(ctx, m.ChatNativeID, m.NativeID)
	if gerr != nil {
		return false, true // se publica tal cual
	}
	text := m.Text
	u := model.MessageUpdate{
		Key: m.Key, Text: &text, EditedAt: m.EditedAt, Deleted: m.DeletedAt > 0,
		Delivery: m.Delivery, Poll: m.Poll, Media: m.Media,
	}
	ev.Type = model.EvMessageUpdate
	ev.Update = &u
	ev.Message = &full
	return false, true
}

// completeMedia pone en el mensaje que se publica la URL del medio (ui/wails la sirve por
// /media/<cuenta>/<id>). El almacén la calcula al leer, pero el mensaje del adaptador no la trae y la
// interfaz pinta imágenes y audios con ella. Si el adaptador no puso id de medio, es el que el
// almacén le asignó al guardar.
func (s *accountSink) completeMedia(ctx context.Context, m *model.Message) {
	if m.Media == nil {
		return
	}
	md := *m.Media
	if md.ID == "" {
		if full, err := s.store.GetMessage(ctx, m.ChatNativeID, m.NativeID); err == nil && full.Media != nil {
			md.ID = full.Media.ID
		}
	}
	if md.URL == "" {
		md.URL = mediaURLFor(s.acc.ID, md.ID)
	}
	m.Media = &md
}

func (s *accountSink) persistUpdate(ctx context.Context, ev *model.Event) bool {
	if ev.Update == nil || ev.Update.Key == "" {
		return false
	}
	full, err := s.store.ApplyUpdate(ctx, *ev.Update)
	if err != nil {
		s.logPersistErr(*ev, err)
		return false
	}
	ev.Message = &full
	return true
}

func (s *accountSink) persistReaction(ctx context.Context, ev *model.Event) bool {
	if ev.Reaction == nil || ev.Reaction.MessageKey == "" {
		return false
	}
	full, err := s.store.ApplyReaction(ctx, *ev.Reaction)
	if err != nil {
		s.logPersistErr(*ev, err)
		return false
	}
	ev.Message = &full
	return true
}

func (s *accountSink) persistPerson(ctx context.Context, ev *model.Event) bool {
	p := ev.Person
	if p == nil || p.NativeID == "" {
		return false
	}
	if err := s.store.UpsertPerson(ctx, *p); err != nil {
		s.logPersistErr(*ev, err)
		return false
	}
	s.maybeUpdateSelf(ctx, *p, true)
	s.syncContacts(ctx, []model.Person{*p}, nil)
	return true
}

// maybeUpdateSelf: datos propios de la cuenta. Si la cuenta ya conoce su SelfID, solo la persona
// con ese id; si aún no lo conoce, la primera persona que llega es la propia (los adaptadores
// emiten su persona antes que ninguna otra).
func (s *accountSink) maybeUpdateSelf(ctx context.Context, p model.Person, adopt bool) {
	if s.acc.SelfID != "" && p.NativeID != s.acc.SelfID {
		return
	}
	if s.acc.SelfID == "" && !adopt {
		return
	}
	cur, err := s.r.app.GetAccount(ctx, s.acc.ID)
	if err != nil {
		s.log.Warn("no se pudo leer la cuenta para guardar sus datos propios", "err", err)
		return
	}
	if cur.SelfID != "" && cur.SelfID != p.NativeID {
		s.acc.SelfID = cur.SelfID // otra persona fue la propia: no se pisa
		return
	}
	changed := cur.SelfID != p.NativeID
	cur.SelfID = p.NativeID
	name := p.Name
	if name == "" {
		name = p.PushName
	}
	if name != "" && name != cur.SelfName {
		cur.SelfName, changed = name, true
	}
	handle := p.Phone
	if handle == "" && p.Username != "" {
		handle = "@" + p.Username
	}
	if handle != "" && handle != cur.SelfHandle {
		cur.SelfHandle, changed = handle, true
	}
	if changed {
		if err := s.r.app.SaveAccount(ctx, cur); err != nil {
			s.log.Warn("no se pudieron guardar los datos propios de la cuenta", "err", err)
			return
		}
	}
	s.acc.SelfID, s.acc.SelfName, s.acc.SelfHandle = cur.SelfID, cur.SelfName, cur.SelfHandle
}

func (s *accountSink) persistHistory(ctx context.Context, ev *model.Event) bool {
	h := ev.History
	if h == nil {
		return false
	}
	for _, p := range h.Persons {
		if p.NativeID == "" {
			continue
		}
		if err := s.store.UpsertPerson(ctx, p); err != nil {
			s.logPersistErr(*ev, err)
			return false
		}
		s.maybeUpdateSelf(ctx, p, false)
	}
	for i := range h.Chats {
		if h.Chats[i].NativeID == "" {
			continue
		}
		s.resolveContact(ctx, &h.Chats[i])
		s.keepLocalMarks(ctx, &h.Chats[i])
		if err := s.store.UpsertChat(ctx, h.Chats[i]); err != nil {
			s.logPersistErr(*ev, err)
			return false
		}
	}
	// Contactos automáticos: todas las personas y chats del lote juntos (pocas transacciones).
	s.syncContacts(ctx, h.Persons, h.Chats)
	valid := h.Messages[:0:0]
	for _, m := range h.Messages {
		if m.NativeID != "" && m.ChatNativeID != "" {
			valid = append(valid, m)
		}
	}
	if len(valid) > 0 {
		if _, err := s.store.UpsertMessages(ctx, valid); err != nil {
			s.logPersistErr(*ev, err)
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------------------------
// Efectos tras publicar

// publishChatSnapshot: tras un mensaje nuevo el chat cambió (vista previa, no leídos): se
// publica un chat.updated derivado, solo al bus (el outbox ya recibe el mensaje).
func (s *accountSink) publishChatSnapshot(ctx context.Context, ev model.Event) {
	chat, err := s.store.GetChat(ctx, ev.Message.ChatNativeID)
	if err != nil {
		return
	}
	chat.AccountID, chat.Channel = s.acc.ID, s.acc.Channel
	chat.Key = model.NewChatKey(s.acc.ID, chat.NativeID)
	s.r.bus.Publish(model.Event{
		ID: model.NewID(), Type: model.EvChatUpsert, AccountID: s.acc.ID, Channel: s.acc.Channel,
		At: ev.At, Chat: &chat, ChatKey: chat.Key,
	})
}

// notifyIncoming: respuesta entrante en chat directo → observadores; si es una baja, marca el
// contacto y avisa también por OnOptOut.
func (s *accountSink) notifyIncoming(ctx context.Context, m model.Message) {
	if m.FromMe {
		return
	}
	peer := m.ChatNativeID
	if chat, err := s.store.GetChat(ctx, m.ChatNativeID); err == nil {
		if chat.Kind != model.ChatDirect {
			return
		}
		if chat.PeerNativeID != "" {
			peer = chat.PeerNativeID
		}
	}
	obs := s.r.observerList()
	optOut := m.Text != "" && isOptOut(m.Text, s.r.optOutKeywords(ctx))
	contactID := ""
	if optOut {
		if ct, err := s.r.app.FindContactByIdentity(ctx, s.acc.ID, peer); err == nil {
			contactID = ct.ID
			if !ct.OptedOut {
				ct.OptedOut = true
				if err := s.r.app.SaveContact(ctx, ct); err != nil {
					s.log.Warn("no se pudo marcar la baja del contacto", "err", err)
				}
			}
		}
	}
	for _, o := range obs {
		callObserver(s.log, func() { o.OnIncoming(s.acc.ID, m.ChatNativeID, m.Timestamp) })
		if optOut {
			callObserver(s.log, func() { o.OnOptOut(s.acc.ID, contactID) })
		}
	}
}

func callObserver(log *slog.Logger, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Error("pánico en un observador de mensajes", "panic", rec)
		}
	}()
	fn()
}

// ---------------------------------------------------------------------------------------------
// Bajas (opt-out)

// isOptOut dice si el mensaje es una baja: lo decide la lógica pura de campañas.
func isOptOut(text, keywords string) bool { return campaign.IsOptOut(text, keywords) }
