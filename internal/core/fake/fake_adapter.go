// Package fake: adaptador de canal en memoria para las pruebas del núcleo (06-SERVICIOS §13).
package fake

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// FullCapabilities: conjunto de capacidades por defecto del adaptador falso (todo encendido).
func FullCapabilities() capability.Set {
	return capability.Set{
		SendText: true, SendImage: true, SendVideo: true, SendAudio: true, SendVoice: true,
		SendDocument: true, SendSticker: true, SendLocation: true, SendContact: true, SendPoll: true, VotePoll: true,
		Reply: true, Forward: true, React: true, MaxReactionsPerUser: 1,
		Edit: true, EditWindow: 15 * time.Minute, DeleteForEveryone: true, DeleteWindow: 48 * time.Hour,
		ReadReceipts: true, Typing: true, Mentions: true, Groups: true, StartChat: true,
		Statuses: true, PostStatus: true, StatusViewers: true, StatusAudience: true, StatusReact: true,
		HistoryOnDemand: true,
		MaxFileSize:     100 << 20, MaxImageSize: 16 << 20, MaxVideoSize: 64 << 20, MaxTextLen: 4096,
	}
}

// Adapter implementa port.ChannelAdapter en memoria. Capacidades configurables,
// Start emite connected, Send devuelve el mensaje con id incremental.
type Adapter struct {
	port.Base // no se usa: el falso implementa todo, pero deja claro el contrato

	mu      sync.Mutex
	acc     model.Account
	channel model.Channel
	caps    capability.Set
	sink    port.EventSink
	started bool
	nextID  int
	sent    []model.Outgoing
	calls   []string
	persons []model.Person
	media   map[string][]byte
	// Hooks opcionales para forzar errores en pruebas.
	SendErr error
}

// New crea un adaptador falso para la cuenta dada, con todas las capacidades.
func New(acc model.Account) *Adapter {
	return &Adapter{acc: acc, channel: acc.Channel, caps: FullCapabilities(), media: map[string][]byte{}}
}

// Factory devuelve una port.AdapterFactory que crea adaptadores falsos y los
// entrega por el canal `out` (útil para que el test los recoja).
func Factory(out chan<- *Adapter) port.AdapterFactory {
	return func(deps port.AdapterDeps) (port.ChannelAdapter, error) {
		a := New(deps.Account)
		if out != nil {
			select {
			case out <- a:
			default:
			}
		}
		return a, nil
	}
}

// SetCapabilities cambia las capacidades de la cuenta.
func (a *Adapter) SetCapabilities(c capability.Set) {
	a.mu.Lock()
	a.caps = c
	a.mu.Unlock()
}

// SetPersons fija lo que devuelve ListPersons.
func (a *Adapter) SetPersons(p []model.Person) {
	a.mu.Lock()
	a.persons = p
	a.mu.Unlock()
}

// SetMedia fija el contenido que devuelve DownloadMedia para un Media.ID.
func (a *Adapter) SetMedia(id string, data []byte) {
	a.mu.Lock()
	a.media[id] = data
	a.mu.Unlock()
}

// Inject emite un evento como si viniera del canal (rellena Channel y AccountID si faltan).
func (a *Adapter) Inject(ev model.Event) {
	a.mu.Lock()
	sink := a.sink
	a.mu.Unlock()
	if sink == nil {
		return
	}
	if ev.AccountID == "" {
		ev.AccountID = a.acc.ID
	}
	if ev.Channel == "" {
		ev.Channel = a.channel
	}
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	sink.Emit(ev)
}

// Sent devuelve lo que se envió con Send, en orden.
func (a *Adapter) Sent() []model.Outgoing {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]model.Outgoing(nil), a.sent...)
}

// Calls devuelve el registro de llamadas ("React:wamid:👍", "MarkRead:chat:3"…).
func (a *Adapter) Calls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

func (a *Adapter) record(format string, args ...any) {
	a.mu.Lock()
	a.calls = append(a.calls, fmt.Sprintf(format, args...))
	a.mu.Unlock()
}

func (a *Adapter) Channel() model.Channel { return a.channel }

func (a *Adapter) Capabilities(chat *model.Chat) capability.Set {
	a.mu.Lock()
	defer a.mu.Unlock()
	if chat != nil && chat.ReadOnly {
		return a.caps.ReadOnly()
	}
	return a.caps
}

func (a *Adapter) emitState(state model.ConnState, need model.PairNeed) {
	a.mu.Lock()
	sink := a.sink
	a.mu.Unlock()
	if sink == nil {
		return
	}
	now := time.Now().UnixMilli()
	sink.Emit(model.Event{
		Type: model.EvAccountStatus, AccountID: a.acc.ID, Channel: a.channel, At: now,
		AccountStatus: &model.AccountStatus{AccountID: a.acc.ID, State: state, Need: need, Since: now},
	})
}

func (a *Adapter) Start(ctx context.Context, sink port.EventSink) error {
	a.mu.Lock()
	a.sink = sink
	a.started = true
	a.mu.Unlock()
	a.emitState(model.StateConnected, model.NeedNone)
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	a.emitState(model.StateDisconnected, model.NeedNone)
	a.mu.Lock()
	a.started = false
	a.mu.Unlock()
	return nil
}

func (a *Adapter) Logout(ctx context.Context) error {
	a.record("Logout")
	a.emitState(model.StateLoggedOut, model.NeedNone)
	return nil
}

func (a *Adapter) BeginPairing(ctx context.Context, req model.PairRequest) error {
	a.record("BeginPairing:%s", req.Method)
	a.emitState(model.StatePairing, model.NeedScanQR)
	return nil
}

func (a *Adapter) SubmitPairing(ctx context.Context, in model.PairInput) error {
	a.record("SubmitPairing")
	a.emitState(model.StateConnected, model.NeedNone)
	return nil
}

func (a *Adapter) Send(ctx context.Context, chatNativeID string, out model.Outgoing) (model.Message, error) {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return model.Message{}, port.ErrNotConnected
	}
	if a.SendErr != nil {
		err := a.SendErr
		a.mu.Unlock()
		return model.Message{}, err
	}
	a.nextID++
	id := fmt.Sprintf("fake-%d", a.nextID)
	a.sent = append(a.sent, out)
	a.mu.Unlock()

	m := model.Message{
		AccountID: a.acc.ID, Channel: a.channel, NativeID: id, ChatNativeID: chatNativeID,
		SenderNativeID: a.acc.SelfID, FromMe: true, Timestamp: time.Now().UnixMilli(),
		Kind: out.Kind, Text: out.Text, Location: out.Location, Poll: out.Poll,
		Delivery: model.DeliverySent,
	}
	if out.Contact != nil {
		m.Contacts = []model.ContactCard{*out.Contact}
	}
	if out.ReplyTo != nil {
		m.QuotedNativeID = out.ReplyTo.NativeID
	}
	if out.FilePath != "" {
		m.Media = &model.Media{ID: id, Mime: out.Mime, FileName: out.FileName}
	}
	return m, nil
}

func (a *Adapter) React(ctx context.Context, msg model.MessageRef, emoji string) error {
	a.record("React:%s:%s", msg.NativeID, emoji)
	return nil
}

func (a *Adapter) Edit(ctx context.Context, msg model.MessageRef, newText string) error {
	a.record("Edit:%s:%s", msg.NativeID, newText)
	return nil
}

func (a *Adapter) Delete(ctx context.Context, msg model.MessageRef, forEveryone bool) error {
	a.record("Delete:%s:%t", msg.NativeID, forEveryone)
	return nil
}

func (a *Adapter) Forward(ctx context.Context, msg model.MessageRef, toChatNativeID string) (model.Message, error) {
	a.record("Forward:%s:%s", msg.NativeID, toChatNativeID)
	a.mu.Lock()
	a.nextID++
	id := fmt.Sprintf("fake-%d", a.nextID)
	a.mu.Unlock()
	return model.Message{
		AccountID: a.acc.ID, Channel: a.channel, NativeID: id, ChatNativeID: toChatNativeID,
		SenderNativeID: a.acc.SelfID, FromMe: true, Timestamp: time.Now().UnixMilli(),
		Kind: model.KindText, Forwarded: true, Delivery: model.DeliverySent,
	}, nil
}

func (a *Adapter) VotePoll(ctx context.Context, msg model.MessageRef, options []string) error {
	a.record("VotePoll:%s:%d", msg.NativeID, len(options))
	return nil
}

func (a *Adapter) MarkRead(ctx context.Context, chatNativeID string, upTo []model.MessageRef) error {
	a.record("MarkRead:%s:%d", chatNativeID, len(upTo))
	return nil
}

func (a *Adapter) SetTyping(ctx context.Context, chatNativeID string, typing bool) error {
	a.record("SetTyping:%s:%t", chatNativeID, typing)
	return nil
}

func (a *Adapter) DownloadMedia(ctx context.Context, m model.Media) (io.ReadCloser, error) {
	a.record("DownloadMedia:%s", m.ID)
	a.mu.Lock()
	data, ok := a.media[m.ID]
	a.mu.Unlock()
	if !ok {
		return nil, port.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (a *Adapter) FetchAvatar(ctx context.Context, peerOrChatNativeID string) (string, error) {
	a.record("FetchAvatar:%s", peerOrChatNativeID)
	return "", nil
}

func (a *Adapter) SyncChats(ctx context.Context) error {
	a.record("SyncChats")
	return nil
}

func (a *Adapter) RequestHistory(ctx context.Context, chatNativeID string, before *model.MessageRef, count int) error {
	a.record("RequestHistory:%s:%d", chatNativeID, count)
	return nil
}

func (a *Adapter) ArchiveChat(ctx context.Context, chatNativeID string, archived bool) error {
	a.record("ArchiveChat:%s:%t", chatNativeID, archived)
	return nil
}

func (a *Adapter) PinChat(ctx context.Context, chatNativeID string, pinned bool) error {
	a.record("PinChat:%s:%t", chatNativeID, pinned)
	return nil
}

func (a *Adapter) MuteChat(ctx context.Context, chatNativeID string, until int64) error {
	a.record("MuteChat:%s:%d", chatNativeID, until)
	return nil
}

func (a *Adapter) ResolvePerson(ctx context.Context, peerNativeID string) (model.Person, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.persons {
		if p.NativeID == peerNativeID {
			return p, nil
		}
	}
	return model.Person{AccountID: a.acc.ID, NativeID: peerNativeID}, nil
}

func (a *Adapter) ListPersons(ctx context.Context) ([]model.Person, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]model.Person(nil), a.persons...), nil
}

func (a *Adapter) PostStatus(ctx context.Context, s model.OutgoingStatus) (model.Status, error) {
	a.record("PostStatus:%s", s.Kind)
	a.mu.Lock()
	a.nextID++
	id := fmt.Sprintf("fake-st-%d", a.nextID)
	a.mu.Unlock()
	now := time.Now().UnixMilli()
	return model.Status{
		Key: string(a.acc.ID) + "|" + id, AccountID: a.acc.ID, Channel: a.channel, NativeID: id,
		AuthorNativeID: a.acc.SelfID, FromMe: true, PostedAt: now, ExpiresAt: now + int64(24*time.Hour/time.Millisecond),
		Kind: s.Kind, Text: s.Text, BgColor: s.BgColor, Font: s.Font, Delivery: model.DeliverySent,
	}, nil
}

func (a *Adapter) DeleteStatus(ctx context.Context, statusNativeID string) error {
	a.record("DeleteStatus:%s", statusNativeID)
	return nil
}

func (a *Adapter) MarkStatusViewed(ctx context.Context, st model.Status) error {
	a.record("MarkStatusViewed:%s", st.NativeID)
	return nil
}

func (a *Adapter) ReactStatus(ctx context.Context, st model.Status, emoji string) error {
	a.record("ReactStatus:%s:%s", st.NativeID, emoji)
	return nil
}

func (a *Adapter) StatusViewers(ctx context.Context, statusNativeID string) ([]model.StatusView, error) {
	a.record("StatusViewers:%s", statusNativeID)
	return nil, nil
}

var _ port.ChannelAdapter = (*Adapter)(nil)
