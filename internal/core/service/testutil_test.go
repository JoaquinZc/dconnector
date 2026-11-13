package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/fake"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

// ---------------------------------------------------------------------------------------------
// AppStore en memoria

type memApp struct {
	mu           sync.Mutex
	accounts     map[model.AccountID]model.Account
	settings     map[string]string
	contacts     map[string]model.Contact
	destinations map[string]model.Destination
	saveAccErr   error
}

func newMemApp() *memApp {
	return &memApp{
		accounts: map[model.AccountID]model.Account{}, settings: map[string]string{},
		contacts: map[string]model.Contact{}, destinations: map[string]model.Destination{},
	}
}

func (s *memApp) Close() error { return nil }

func (s *memApp) ListAccounts(ctx context.Context) ([]model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *memApp) GetAccount(ctx context.Context, id model.AccountID) (model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[id]
	if !ok {
		return model.Account{}, port.ErrNotFound
	}
	return a, nil
}

func (s *memApp) SaveAccount(ctx context.Context, a model.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveAccErr != nil {
		return s.saveAccErr
	}
	s.accounts[a.ID] = a
	return nil
}

func (s *memApp) DeleteAccount(ctx context.Context, id model.AccountID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.accounts, id)
	return nil
}

func (s *memApp) GetSetting(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings[key], nil
}

func (s *memApp) SetSetting(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[key] = value
	return nil
}

func (s *memApp) ListContacts(ctx context.Context, f model.ContactFilter) ([]model.Contact, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.Contact{}
	for _, c := range s.contacts {
		out = append(out, c)
	}
	return out, len(out), nil
}

func (s *memApp) GetContact(ctx context.Context, id string) (model.Contact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.contacts[id]
	if !ok {
		return model.Contact{}, port.ErrNotFound
	}
	return c, nil
}

func (s *memApp) FindContactByIdentity(ctx context.Context, acc model.AccountID, peer string) (model.Contact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.contacts {
		for _, id := range c.Identities {
			if id.AccountID == acc && id.PeerNativeID == peer {
				return c, nil
			}
		}
	}
	return model.Contact{}, port.ErrNotFound
}

func (s *memApp) FindContactsByPhone(ctx context.Context, phone string) ([]model.Contact, error) {
	return nil, nil
}

func (s *memApp) SaveContact(ctx context.Context, c model.Contact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contacts[c.ID] = c
	return nil
}

func (s *memApp) DeleteContact(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.contacts, id)
	return nil
}

func (s *memApp) ListLists(ctx context.Context) ([]model.ContactList, error) { return nil, nil }
func (s *memApp) SaveList(ctx context.Context, l model.ContactList) error    { return nil }
func (s *memApp) DeleteList(ctx context.Context, id string) error            { return nil }
func (s *memApp) SetListMembers(ctx context.Context, listID string, add, remove []string) error {
	return nil
}
func (s *memApp) ListMembers(ctx context.Context, listIDs []string) ([]model.Contact, error) {
	return nil, nil
}

func (s *memApp) ListCampaigns(ctx context.Context) ([]model.Campaign, error) { return nil, nil }
func (s *memApp) GetCampaign(ctx context.Context, id string) (model.Campaign, error) {
	return model.Campaign{}, port.ErrNotFound
}
func (s *memApp) SaveCampaign(ctx context.Context, c model.Campaign) error { return nil }
func (s *memApp) DeleteCampaign(ctx context.Context, id string) error      { return nil }
func (s *memApp) ReplaceQueue(ctx context.Context, campaignID string, items []model.QueueItem) error {
	return nil
}
func (s *memApp) ListQueue(ctx context.Context, campaignID string, states []string, limit int) ([]model.QueueItem, error) {
	return nil, nil
}
func (s *memApp) UpdateQueueItem(ctx context.Context, it model.QueueItem) error { return nil }
func (s *memApp) CountSentSince(ctx context.Context, acc model.AccountID, since int64) (int, error) {
	return 0, nil
}
func (s *memApp) QueueItemByMessage(ctx context.Context, key model.MessageKey) (model.QueueItem, error) {
	return model.QueueItem{}, port.ErrNotFound
}
func (s *memApp) ListScheduled(ctx context.Context, from, to int64, states []string) ([]model.Scheduled, error) {
	return nil, nil
}
func (s *memApp) DueScheduled(ctx context.Context, now int64) ([]model.Scheduled, error) {
	return nil, nil
}
func (s *memApp) SaveScheduled(ctx context.Context, sc model.Scheduled) error { return nil }
func (s *memApp) DeleteScheduled(ctx context.Context, id string) error        { return nil }

func (s *memApp) ListDestinations(ctx context.Context) ([]model.Destination, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Destination, 0, len(s.destinations))
	for _, d := range s.destinations {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *memApp) SaveDestination(ctx context.Context, d model.Destination) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destinations[d.ID] = d
	return nil
}

func (s *memApp) DeleteDestination(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.destinations, id)
	return nil
}

func (s *memApp) OutboxAdd(ctx context.Context, destinationID, eventID string, payload []byte) error {
	return nil
}
func (s *memApp) OutboxPending(ctx context.Context, destinationID string, limit int) ([]port.OutboxRow, error) {
	return nil, nil
}
func (s *memApp) OutboxAck(ctx context.Context, destinationID string, eventIDs []string) error {
	return nil
}
func (s *memApp) OutboxCount(ctx context.Context, destinationID string) (int, error) { return 0, nil }
func (s *memApp) OutboxTrim(ctx context.Context, olderThan int64, maxRows int) (int, error) {
	return 0, nil
}

var _ port.AppStore = (*memApp)(nil)

// ---------------------------------------------------------------------------------------------
// AccountStore en memoria

func deliveryRank(d model.DeliveryState) int {
	switch d {
	case model.DeliveryPending:
		return 1
	case model.DeliverySent:
		return 2
	case model.DeliveryDelivered:
		return 3
	case model.DeliveryRead:
		return 4
	case model.DeliveryPlayed:
		return 5
	}
	return 0
}

type memAccountStore struct {
	mu       sync.Mutex
	id       model.AccountID
	chats    map[string]model.Chat
	persons  map[string]model.Person
	msgs     map[string]model.Message // "chat|msg"
	statuses map[string]model.Status
	views    map[string]model.StatusView
	kv       map[string][]byte
	closed   bool

	// instrumentación
	batchCalls int
	gate       chan struct{} // si no es nil, UpsertChat espera a que se cierre
}

func newMemAccountStore(id model.AccountID) *memAccountStore {
	return &memAccountStore{
		id: id, chats: map[string]model.Chat{}, persons: map[string]model.Person{},
		msgs: map[string]model.Message{}, statuses: map[string]model.Status{},
		views: map[string]model.StatusView{}, kv: map[string][]byte{},
	}
}

func (s *memAccountStore) AccountID() model.AccountID { return s.id }
func (s *memAccountStore) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}
func (s *memAccountStore) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *memAccountStore) UpsertChat(ctx context.Context, c model.Chat) error {
	s.mu.Lock()
	gate := s.gate
	s.mu.Unlock()
	if gate != nil {
		<-gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chats[c.NativeID] = c
	return nil
}

func (s *memAccountStore) PatchChat(ctx context.Context, c model.Chat, fields []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.chats[c.NativeID]
	if !ok {
		return port.ErrNotFound
	}
	for _, f := range fields {
		switch f {
		case "title":
			cur.Title = c.Title
		case "unreadCount":
			cur.UnreadCount = c.UnreadCount
		case "archived":
			cur.Archived = c.Archived
		case "pinned":
			cur.Pinned = c.Pinned
		case "mutedUntil":
			cur.MutedUntil = c.MutedUntil
		}
	}
	s.chats[c.NativeID] = cur
	return nil
}

func (s *memAccountStore) GetChat(ctx context.Context, nativeID string) (model.Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chats[nativeID]
	if !ok {
		return model.Chat{}, port.ErrNotFound
	}
	return c, nil
}

func (s *memAccountStore) ListChats(ctx context.Context, f model.ChatFilter) ([]model.Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Chat
	for _, c := range s.chats {
		switch f.Tab {
		case "unread":
			if c.UnreadCount <= 0 {
				continue
			}
		case "groups":
			if c.Kind != model.ChatGroup {
				continue
			}
		case "archived":
			if !c.Archived {
				continue
			}
		default:
			if c.Archived {
				continue
			}
		}
		if !f.IncludeEmpty && (c.Kind == model.ChatDirect || c.Kind == model.ChatSelf) && c.LastMessageAt == 0 && !c.Pinned {
			continue // como el almacén real: un directo sin actividad no es una conversación
		}
		if f.Query != "" && !strings.Contains(strings.ToLower(c.Title), strings.ToLower(f.Query)) {
			continue
		}
		if f.Before > 0 && c.LastMessageAt >= f.Before {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].LastMessageAt > out[j].LastMessageAt
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *memAccountStore) DeleteChat(ctx context.Context, nativeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.chats, nativeID)
	return nil
}

func (s *memAccountStore) UpsertPerson(ctx context.Context, p model.Person) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persons[p.NativeID] = p
	return nil
}

func (s *memAccountStore) GetPerson(ctx context.Context, nativeID string) (model.Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.persons[nativeID]
	if !ok {
		return model.Person{}, port.ErrNotFound
	}
	return p, nil
}

func (s *memAccountStore) ListPersons(ctx context.Context, query string, limit int) ([]model.Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Person
	for _, p := range s.persons {
		out = append(out, p)
	}
	return out, nil
}

func msgMapKey(chat, native string) string { return chat + "|" + native }

func (s *memAccountStore) putMessage(m model.Message, countUnread bool) bool {
	k := msgMapKey(m.ChatNativeID, m.NativeID)
	old, exists := s.msgs[k]
	if exists {
		m.Reactions = old.Reactions
		if m.Media != nil && old.Media != nil && m.Media.LocalPath == "" {
			mc := *m.Media
			mc.LocalPath = old.Media.LocalPath
			m.Media = &mc
		}
		if deliveryRank(old.Delivery) > deliveryRank(m.Delivery) {
			m.Delivery = old.Delivery
		}
		s.msgs[k] = m
		return false
	}
	s.msgs[k] = m
	if chat, ok := s.chats[m.ChatNativeID]; ok {
		if m.Timestamp >= chat.LastMessageAt {
			chat.LastMessageAt, chat.LastPreview, chat.LastFromMe = m.Timestamp, m.Text, m.FromMe
		}
		if countUnread && !m.FromMe {
			chat.UnreadCount++
		}
		s.chats[m.ChatNativeID] = chat
	}
	return true
}

func (s *memAccountStore) UpsertMessage(ctx context.Context, m model.Message) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putMessage(m, true), nil
}

func (s *memAccountStore) UpsertMessages(ctx context.Context, ms []model.Message) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls++
	n := 0
	for _, m := range ms {
		if s.putMessage(m, false) {
			n++
		}
	}
	return n, nil
}

func (s *memAccountStore) find(key model.MessageKey) (string, model.Message, bool) {
	_, chat, native := key.Split()
	k := msgMapKey(chat, native)
	m, ok := s.msgs[k]
	return k, m, ok
}

func (s *memAccountStore) ApplyUpdate(ctx context.Context, u model.MessageUpdate) (model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, m, ok := s.find(u.Key)
	if !ok {
		return model.Message{}, port.ErrNotFound
	}
	if u.Text != nil {
		m.Text = *u.Text
	}
	if u.EditedAt != 0 {
		m.EditedAt = u.EditedAt
	}
	if u.Deleted {
		m.DeletedAt = time.Now().UnixMilli()
	}
	if deliveryRank(u.Delivery) > deliveryRank(m.Delivery) {
		m.Delivery = u.Delivery
	}
	if u.Poll != nil {
		m.Poll = u.Poll
	}
	if u.Media != nil {
		mc := *u.Media
		m.Media = &mc
	}
	s.msgs[k] = m
	return m, nil
}

func (s *memAccountStore) ApplyReaction(ctx context.Context, r model.ReactionEvent) (model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, m, ok := s.find(r.MessageKey)
	if !ok {
		return model.Message{}, port.ErrNotFound
	}
	var rs []model.Reaction
	for _, x := range m.Reactions {
		if x.SenderNativeID != r.Reaction.SenderNativeID {
			rs = append(rs, x)
		}
	}
	if r.Reaction.Emoji != "" {
		rs = append(rs, r.Reaction)
	}
	m.Reactions = rs
	s.msgs[k] = m
	return m, nil
}

func (s *memAccountStore) ApplyReceipt(ctx context.Context, r model.ReceiptEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range r.MessageKeys {
		k, m, ok := s.find(key)
		if ok && deliveryRank(r.Delivery) > deliveryRank(m.Delivery) {
			m.Delivery = r.Delivery
			s.msgs[k] = m
		}
	}
	return nil
}

func (s *memAccountStore) GetMessage(ctx context.Context, chatNativeID, nativeID string) (model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.msgs[msgMapKey(chatNativeID, nativeID)]
	if !ok {
		return model.Message{}, port.ErrNotFound
	}
	return m, nil
}

func (s *memAccountStore) ListMessages(ctx context.Context, chatNativeID string, beforeTs int64, limit int) (model.MessagePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ms []model.Message
	for _, m := range s.msgs {
		if m.ChatNativeID != chatNativeID || (beforeTs > 0 && m.Timestamp >= beforeTs) {
			continue
		}
		ms = append(ms, m)
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Timestamp < ms[j].Timestamp })
	more := false
	if limit > 0 && len(ms) > limit {
		ms, more = ms[len(ms)-limit:], true
	}
	return model.MessagePage{Messages: ms, HasMore: more}, nil
}

func (s *memAccountStore) SearchMessages(ctx context.Context, query, chatNativeID string, limit int) ([]model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Message
	for _, m := range s.msgs {
		if chatNativeID != "" && m.ChatNativeID != chatNativeID {
			continue
		}
		if strings.Contains(strings.ToLower(m.Text), strings.ToLower(query)) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *memAccountStore) SetMediaPath(ctx context.Context, mediaID, localPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, m := range s.msgs {
		if m.Media != nil && m.Media.ID == mediaID {
			mc := *m.Media
			mc.LocalPath = localPath
			m.Media = &mc
			s.msgs[k] = m
			return nil
		}
	}
	return port.ErrNotFound
}

func (s *memAccountStore) GetMedia(ctx context.Context, mediaID string) (model.Media, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.msgs {
		if m.Media != nil && m.Media.ID == mediaID {
			return *m.Media, m.ChatNativeID, nil
		}
	}
	return model.Media{}, "", port.ErrNotFound
}

func (s *memAccountStore) MessageStats(ctx context.Context, from, to int64, chatNativeIDs []string) ([]port.StatRow, error) {
	return nil, nil
}

func (s *memAccountStore) UpsertStatus(ctx context.Context, st model.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[st.Key] = st
	return nil
}

func (s *memAccountStore) ListStatusAuthors(ctx context.Context, now int64) ([]model.StatusAuthor, error) {
	return nil, nil
}
func (s *memAccountStore) ListStatuses(ctx context.Context, authorID string, now int64) ([]model.Status, error) {
	return nil, nil
}
func (s *memAccountStore) ListMyStatuses(ctx context.Context, from, to int64) ([]model.Status, error) {
	return nil, nil
}

func (s *memAccountStore) UpsertStatusView(ctx context.Context, v model.StatusView) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.views[v.StatusKey+"|"+v.ViewerID] = v
	return nil
}

func (s *memAccountStore) ListStatusViews(ctx context.Context, statusKey string) ([]model.StatusView, error) {
	return nil, nil
}

func (s *memAccountStore) KVGet(ctx context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kv[key], nil
}

func (s *memAccountStore) KVSet(ctx context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kv[key] = value
	return nil
}

// Ayudas de prueba (acceso directo a lo guardado).
func (s *memAccountStore) chat(native string) (model.Chat, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chats[native]
	return c, ok
}

func (s *memAccountStore) message(chat, native string) (model.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.msgs[msgMapKey(chat, native)]
	return m, ok
}

func (s *memAccountStore) person(native string) (model.Person, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.persons[native]
	return p, ok
}

func (s *memAccountStore) batches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.batchCalls
}

var _ port.AccountStore = (*memAccountStore)(nil)

// ---------------------------------------------------------------------------------------------
// Adaptador de prueba: el falso con ganchos

type testAdapter struct {
	*fake.Adapter
	mu         sync.Mutex
	deps       port.AdapterDeps
	startCalls int
	startErrs  int   // los primeros N Start devuelven error
	archiveErr error // lo que devuelve ArchiveChat/PinChat/MuteChat
	dlGate     chan struct{}
	dlActive   int
	dlMax      int
	dlCalls    int
	// resolve: si no es nil, el adaptador de prueba hace de peerResolver (interfaz opcional de «Nuevo chat»).
	resolve func(ctx context.Context, input string) (string, string, error)
}

func (a *testAdapter) ResolvePeer(ctx context.Context, input string) (string, string, error) {
	a.mu.Lock()
	fn := a.resolve
	a.mu.Unlock()
	if fn == nil {
		return input, "", nil
	}
	return fn(ctx, input)
}

func (a *testAdapter) Start(ctx context.Context, sink port.EventSink) error {
	a.mu.Lock()
	a.startCalls++
	fail := a.startErrs > 0
	if fail {
		a.startErrs--
	}
	a.mu.Unlock()
	if fail {
		return errors.New("fallo de prueba")
	}
	return a.Adapter.Start(ctx, sink)
}

func (a *testAdapter) starts() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startCalls
}

func (a *testAdapter) ArchiveChat(ctx context.Context, chat string, v bool) error {
	if a.archiveErr != nil {
		return a.archiveErr
	}
	return a.Adapter.ArchiveChat(ctx, chat, v)
}

func (a *testAdapter) PinChat(ctx context.Context, chat string, v bool) error {
	if a.archiveErr != nil {
		return a.archiveErr
	}
	return a.Adapter.PinChat(ctx, chat, v)
}

func (a *testAdapter) MuteChat(ctx context.Context, chat string, until int64) error {
	if a.archiveErr != nil {
		return a.archiveErr
	}
	return a.Adapter.MuteChat(ctx, chat, until)
}

func (a *testAdapter) DownloadMedia(ctx context.Context, m model.Media) (io.ReadCloser, error) {
	a.mu.Lock()
	a.dlCalls++
	a.dlActive++
	if a.dlActive > a.dlMax {
		a.dlMax = a.dlActive
	}
	gate := a.dlGate
	a.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
	a.mu.Lock()
	a.dlActive--
	a.mu.Unlock()
	return a.Adapter.DownloadMedia(ctx, m)
}

func (a *testAdapter) downloads() (calls, maxActive int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dlCalls, a.dlMax
}

// ---------------------------------------------------------------------------------------------
// Entorno de prueba

type testEnv struct {
	t        *testing.T
	app      *memApp
	bus      *bus.Bus
	router   *Router
	accounts *Accounts
	inbox    *Inbox
	msg      *Messaging

	mu       sync.Mutex
	adapters map[model.AccountID]*testAdapter
	stores   map[model.AccountID]*memAccountStore
	openErr  error
	logMu    sync.Mutex
	log      []model.Event
	// configure se aplica a cada adaptador recién creado (antes de arrancarlo).
	configure func(a *testAdapter)
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	if err := paths.InitAt(t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	e := &testEnv{
		t: t, app: newMemApp(), bus: bus.New(),
		adapters: map[model.AccountID]*testAdapter{}, stores: map[model.AccountID]*memAccountStore{},
	}
	e.router = NewRouter(e.app, e.bus)
	factory := func(deps port.AdapterDeps) (port.ChannelAdapter, error) {
		a := &testAdapter{Adapter: fake.New(deps.Account), deps: deps}
		if e.configure != nil {
			e.configure(a)
		}
		e.mu.Lock()
		e.adapters[deps.Account.ID] = a
		e.mu.Unlock()
		return a, nil
	}
	reg := map[model.Channel]port.AdapterFactory{}
	for _, ch := range model.AllChannels {
		reg[ch] = factory
	}
	open := func(id model.AccountID) (port.AccountStore, *sql.DB, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.openErr != nil {
			return nil, nil, e.openErr
		}
		st, ok := e.stores[id]
		if !ok {
			st = newMemAccountStore(id)
			e.stores[id] = st
		}
		return st, nil, nil
	}
	e.accounts = NewAccounts(e.app, e.router, e.bus, reg, open)
	e.accounts.backoffMin, e.accounts.backoffMax = 10*time.Millisecond, 40*time.Millisecond
	e.inbox = NewInbox(e.accounts)
	e.msg = NewMessaging(e.accounts, e.router, e.bus, e.app)

	ch, cancel := e.bus.Subscribe("test", 8192)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			e.logMu.Lock()
			e.log = append(e.log, ev)
			e.logMu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = e.accounts.StopAll(ctx)
		e.msg.Close()
		cancel()
		<-done
	})
	return e
}

func (e *testEnv) adapter(id model.AccountID) *testAdapter {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.adapters[id]
}

func (e *testEnv) store(id model.AccountID) *memAccountStore {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stores[id]
}

// eventually repite cond hasta que se cumpla o pasen 3 s.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no se cumplió a tiempo: %s", what)
}

func (e *testEnv) mark() int {
	e.logMu.Lock()
	defer e.logMu.Unlock()
	return len(e.log)
}

func (e *testEnv) eventsSince(from int) []model.Event {
	e.logMu.Lock()
	defer e.logMu.Unlock()
	if from > len(e.log) {
		from = len(e.log)
	}
	return append([]model.Event(nil), e.log[from:]...)
}

// waitEvent espera un evento publicado después de `from` que cumpla pred.
func (e *testEnv) waitEvent(from int, what string, pred func(model.Event) bool) model.Event {
	e.t.Helper()
	var found model.Event
	eventually(e.t, what, func() bool {
		for _, ev := range e.eventsSince(from) {
			if pred(ev) {
				found = ev
				return true
			}
		}
		return false
	})
	return found
}

func ofType(tp model.EventType) func(model.Event) bool {
	return func(ev model.Event) bool { return ev.Type == tp }
}

func (e *testEnv) waitState(id model.AccountID, st model.ConnState) {
	e.t.Helper()
	eventually(e.t, "estado "+string(st), func() bool { return e.accounts.Status(id).State == st })
}

// newAccount crea una cuenta, espera a que conecte y devuelve su id.
func (e *testEnv) newAccount(ch model.Channel, label string) model.Account {
	e.t.Helper()
	acc, err := e.accounts.Create(context.Background(), ch, label, "", nil)
	if err != nil {
		e.t.Fatalf("Create: %v", err)
	}
	e.waitState(acc.ID, model.StateConnected)
	return acc
}

// seedChat guarda un chat directo en el almacén de la cuenta.
func (e *testEnv) seedChat(acc model.AccountID, native, title string, last int64) model.Chat {
	c := model.Chat{
		Key: model.NewChatKey(acc, native), AccountID: acc, NativeID: native, Kind: model.ChatDirect,
		Title: title, LastMessageAt: last, PeerNativeID: native,
	}
	if err := e.store(acc).UpsertChat(context.Background(), c); err != nil {
		e.t.Fatal(err)
	}
	return c
}

// seedMessage guarda un mensaje directamente en el almacén.
func (e *testEnv) seedMessage(acc model.AccountID, chat, native string, ts int64, fromMe bool, text string) model.Message {
	m := model.Message{
		Key: model.NewMessageKey(acc, chat, native), ChatKey: model.NewChatKey(acc, chat), AccountID: acc,
		NativeID: native, ChatNativeID: chat, SenderNativeID: chat, FromMe: fromMe, Timestamp: ts,
		Kind: model.KindText, Text: text, Delivery: model.DeliverySent,
	}
	if fromMe {
		m.SenderNativeID = "me"
	}
	if _, err := e.store(acc).UpsertMessage(context.Background(), m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

// outboxRec: OutboxWriter de prueba.
type outboxRec struct {
	mu  sync.Mutex
	evs []model.Event
}

func (o *outboxRec) Enqueue(ctx context.Context, ev model.Event) {
	o.mu.Lock()
	o.evs = append(o.evs, ev)
	o.mu.Unlock()
}

func (o *outboxRec) list() []model.Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]model.Event(nil), o.evs...)
}

// obsRec: IncomingObserver de prueba.
type obsRec struct {
	mu       sync.Mutex
	incoming []string
	optOuts  []string
	panicky  bool
}

func (o *obsRec) OnIncoming(acc model.AccountID, chat string, ts int64) {
	o.mu.Lock()
	o.incoming = append(o.incoming, string(acc)+"|"+chat)
	o.mu.Unlock()
	if o.panicky {
		panic("observador roto")
	}
}

func (o *obsRec) OnOptOut(acc model.AccountID, contactID string) {
	o.mu.Lock()
	o.optOuts = append(o.optOuts, string(acc)+"|"+contactID)
	o.mu.Unlock()
}

func (o *obsRec) counts() (int, int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.incoming), len(o.optOuts)
}
