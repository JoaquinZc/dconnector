package instagram

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/mautrix-meta/pkg/instameow"
	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
	"go.mau.fi/mautrix-meta/pkg/messagix/dgw"
	_ "modernc.org/sqlite"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

type sinkRec struct {
	mu  sync.Mutex
	evs []model.Event
}

func (s *sinkRec) Emit(ev model.Event) {
	s.mu.Lock()
	s.evs = append(s.evs, ev)
	s.mu.Unlock()
}

func (s *sinkRec) all() []model.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.Event(nil), s.evs...)
}

func (s *sinkRec) ofType(t model.EventType) []model.Event {
	var out []model.Event
	for _, e := range s.all() {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func (s *sinkRec) lastStatus() model.AccountStatus {
	st := s.ofType(model.EvAccountStatus)
	if len(st) == 0 {
		return model.AccountStatus{}
	}
	return *st[len(st)-1].AccountStatus
}

type fixture struct {
	a    *Adapter
	sink *sinkRec
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "account.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ad, err := New(port.AdapterDeps{
		Account: model.Account{ID: "ig-test", Channel: model.ChannelInstagram, SelfID: "100"},
		DB:      db, DBPath: filepath.Join(dir, "account.db"),
		MediaDir: filepath.Join(dir, "media"), AvatarDir: filepath.Join(dir, "avatars"),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	a := ad.(*Adapter)
	sink := &sinkRec{}
	a.SetSink(sink)
	return &fixture{a: a, sink: sink}
}

func TestFactoryAndChannel(t *testing.T) {
	f := newFixture(t)
	assert.Equal(t, model.ChannelInstagram, f.a.Channel())
	_, err := New(port.AdapterDeps{Account: model.Account{ID: "ig-x"}})
	assert.Error(t, err)
}

func TestCapabilities(t *testing.T) {
	f := newFixture(t)
	c := f.a.Capabilities(nil)
	assert.True(t, c.SendText && c.SendImage && c.SendVideo && c.SendAudio && c.SendVoice)
	assert.False(t, c.SendDocument, "Instagram no admite documentos")
	assert.False(t, c.SendSticker || c.SendLocation || c.SendContact || c.SendPoll)
	assert.True(t, c.Reply && c.React)
	assert.False(t, c.Forward)
	assert.Equal(t, 1, c.MaxReactionsPerUser)
	assert.True(t, c.Edit)
	assert.Equal(t, 15*time.Minute, c.EditWindow)
	assert.True(t, c.DeleteForEveryone)
	assert.True(t, c.ReadReceipts && c.Typing && c.Groups)
	assert.True(t, c.HistoryOnDemand)
	assert.False(t, c.E2EEChats)
	assert.False(t, c.Statuses || c.PostStatus || c.StatusViewers || c.StatusAudience || c.StatusReact)
	assert.Equal(t, 1000, c.MaxTextLen)
	assert.Equal(t, int64(25<<20), c.MaxFileSize)
	assert.False(t, f.a.Capabilities(&model.Chat{ReadOnly: true}).SendText)
}

func TestStartWithoutCookiesAsksForThem(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.a.Start(context.Background(), f.sink))
	st := f.sink.lastStatus()
	assert.Equal(t, model.StatePairing, st.State)
	assert.Equal(t, model.NeedCookies, st.Need)
	assert.Contains(t, st.Detail, "instagram.com")
	require.NoError(t, f.a.Stop(context.Background()))
	assert.Equal(t, model.StateDisconnected, f.sink.lastStatus().State)
}

func TestPairing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.a.BeginPairing(ctx, model.PairRequest{Method: "password_login"}))
	assert.Equal(t, model.NeedPassword, f.sink.lastStatus().Need)
	assert.ErrorIs(t, f.a.BeginPairing(ctx, model.PairRequest{Method: "phone"}), port.ErrInvalid)
	require.NoError(t, f.a.BeginPairing(ctx, model.PairRequest{Method: "cookies"}))

	// Cookies de Messenger no sirven para Instagram: dice cuáles faltan.
	err := f.a.SubmitPairing(ctx, model.PairInput{Cookies: "c_user=1; xs=2; datr=3"})
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalid)
	d := f.sink.lastStatus().Detail
	assert.Contains(t, d, "sessionid")
	assert.Contains(t, d, "ds_user_id")
	assert.Contains(t, d, "csrftoken")
	assert.NotContains(t, err.Error(), "xs=2")
}

func TestOperationsWithoutConnection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ref := model.MessageRef{ChatNativeID: "340282", NativeID: "mid.1", FromMe: true, Timestamp: time.Now().UnixMilli()}
	_, err := f.a.Send(ctx, "340282", model.Outgoing{Kind: model.KindText, Text: "hola"})
	assert.ErrorIs(t, err, port.ErrNotConnected)
	assert.ErrorIs(t, f.a.React(ctx, ref, "👍"), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.Edit(ctx, ref, "nuevo"), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.Delete(ctx, ref, true), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.MarkRead(ctx, "340282", nil), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.SetTyping(ctx, "340282", true), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.SyncChats(ctx), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.RequestHistory(ctx, "340282", nil, 20), port.ErrNotConnected)
}

func TestValidationBeforeNetwork(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.a.Send(ctx, "1", model.Outgoing{Kind: model.KindText, Text: " "})
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, err = f.a.Send(ctx, "1", model.Outgoing{Kind: model.KindDocument, FilePath: "x.pdf"})
	assert.ErrorIs(t, err, port.ErrUnsupported, "sin documentos")
	_, err = f.a.Send(ctx, "1", model.Outgoing{Kind: model.KindSticker})
	assert.ErrorIs(t, err, port.ErrUnsupported)
	long := make([]rune, 1001)
	for i := range long {
		long[i] = 'a'
	}
	_, err = f.a.Send(ctx, "1", model.Outgoing{Kind: model.KindText, Text: string(long)})
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, err = f.a.Send(ctx, "1", model.Outgoing{Kind: model.KindText, Text: string(long[:1000])})
	assert.ErrorIs(t, err, port.ErrNotConnected, "1000 caracteres sí caben")

	mine := model.MessageRef{ChatNativeID: "1", NativeID: "m", FromMe: true, Timestamp: time.Now().UnixMilli()}
	assert.ErrorIs(t, f.a.Edit(ctx, mine, ""), port.ErrInvalid)
	theirs := mine
	theirs.FromMe = false
	assert.ErrorIs(t, f.a.Edit(ctx, theirs, "x"), port.ErrInvalid)
	old := mine
	old.Timestamp = time.Now().Add(-time.Hour).UnixMilli()
	assert.ErrorIs(t, f.a.Edit(ctx, old, "x"), port.ErrInvalid)

	// Instagram no tiene «borrar solo para mí»; y solo se deshace el envío de mensajes propios.
	assert.ErrorIs(t, f.a.Delete(ctx, mine, false), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.Delete(ctx, theirs, true), port.ErrInvalid)
}

func TestUnsupportedOperations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ref := model.MessageRef{ChatNativeID: "1", NativeID: "m"}
	_, err := f.a.Forward(ctx, ref, "2")
	assert.ErrorIs(t, err, port.ErrUnsupported)
	assert.ErrorIs(t, f.a.VotePoll(ctx, ref, nil), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.ArchiveChat(ctx, "1", true), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.PinChat(ctx, "1", true), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.MuteChat(ctx, "1", 0), port.ErrUnsupported)
	_, err = f.a.PostStatus(ctx, model.OutgoingStatus{})
	assert.ErrorIs(t, err, port.ErrUnsupported)
	_, err = f.a.StatusViewers(ctx, "x")
	assert.ErrorIs(t, err, port.ErrUnsupported)
}

func TestLogoutWipesLocalSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.a.KV.Ensure(ctx))
	require.NoError(t, meta.SaveCookies(ctx, f.a.KV, map[string]string{"sessionid": "s", "ds_user_id": "7", "csrftoken": "t"}))
	f.a.saveSeq(ctx, 99, time.UnixMilli(1000))
	require.NoError(t, f.a.Logout(ctx))
	assert.Equal(t, model.StateLoggedOut, f.sink.lastStatus().State)
	got, err := meta.LoadCookies(ctx, f.a.KV)
	require.NoError(t, err)
	assert.Nil(t, got)
	_, ok := f.a.loadSeq(ctx)
	assert.False(t, ok, "el cursor también se borra")
	require.NoError(t, f.a.Start(ctx, f.sink))
	assert.Equal(t, model.NeedCookies, f.sink.lastStatus().Need)
}

func TestSeqPersists(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.a.KV.Ensure(ctx))
	_, ok := f.a.loadSeq(ctx)
	assert.False(t, ok)
	f.a.saveSeq(ctx, 12345, time.UnixMilli(777))
	s, ok := f.a.loadSeq(ctx)
	require.True(t, ok)
	assert.Equal(t, int64(12345), s.Seq)
	assert.Equal(t, int64(777), s.TS)
}

func TestHandleEvent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.a.KV.Ensure(ctx))
	gen := f.a.currentGen()

	// Un evento de una conexión vieja se ignora.
	require.NoError(t, f.a.handleEvent(ctx, gen+1, &slidetypes.SeqIDUpdate{SeqID: 5}))
	_, ok := f.a.loadSeq(ctx)
	assert.False(t, ok)
	require.NoError(t, f.a.handleEvent(ctx, gen, &slidetypes.SeqIDUpdate{SeqID: 5, Timestamp: time.UnixMilli(10)}))
	s, ok := f.a.loadSeq(ctx)
	require.True(t, ok)
	assert.Equal(t, int64(5), s.Seq)

	// Se cortó la conexión sin más: estado «conectando» (solo si estaba conectado) y se sigue reintentando.
	f.a.SetStatus(model.AccountStatus{State: model.StateConnected})
	require.NoError(t, f.a.handleEvent(ctx, gen, &slidetypes.Disconnected{Error: errors.New("red caída")}))
	assert.Equal(t, model.StateConnecting, f.sink.lastStatus().State)
	assert.Contains(t, f.sink.lastStatus().Detail, "Reconectando")

	// Instagram dice que la sesión no vale: cierra sesión, borra cookies y manda parar los reintentos.
	require.NoError(t, meta.SaveCookies(ctx, f.a.KV, map[string]string{"sessionid": "s"}))
	err := f.a.handleEvent(ctx, gen, &slidetypes.Disconnected{Error: dgw.ErrUnauthorized})
	require.Error(t, err)
	assert.Equal(t, model.StateLoggedOut, f.sink.lastStatus().State)
	got, _ := meta.LoadCookies(ctx, f.a.KV)
	assert.Nil(t, got)

	// Demasiados cierres del flujo principal: error y se paran los reintentos.
	err = f.a.handleEvent(ctx, gen, &slidetypes.Disconnected{Error: instameow.ErrMainStreamClosed, FailureCount: 6})
	require.Error(t, err)
	assert.Equal(t, model.StateError, f.sink.lastStatus().State)
}

func TestOnDeltaDeliversEventsAndRemembersChats(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.a.conv.Thread(dmThread())
	f.a.onDelta(ctx, delta("thr1", &slidetypes.NewMessageEvent{Message: textMsg("m.1", "hola", 200, 10)}))
	ms := f.sink.ofType(model.EvMessageNew)
	require.Len(t, ms, 1)
	assert.Equal(t, "ig-test", string(ms[0].AccountID))
	assert.Equal(t, model.ChannelInstagram, ms[0].Channel)
	assert.Equal(t, "hola", ms[0].Message.Text)
	assert.Equal(t, "thr1", f.a.local.ChatOfMessage(ctx, "m.1"))

	// Un hilo que no se conoce y no se puede pedir (sin conexión): no se entrega nada ni se cae.
	before := len(f.sink.all())
	f.a.onDelta(ctx, delta("otro", &slidetypes.NewMessageEvent{Message: textMsg("m.2", "x", 200, 11)}))
	assert.Equal(t, before, len(f.sink.all()))
}

func TestDeliverMailboxEmitsHistoryBatch(t *testing.T) {
	f := newFixture(t)
	f.a.deliverMailbox(nil)
	assert.Empty(t, f.sink.all())
	mb := &slidetypes.Mailbox{
		ThreadsByFolder: slidetypes.Edged[slidetypes.Node[slidetypes.WrappedThreadInfo]]{Edges: []slidetypes.Node[slidetypes.WrappedThreadInfo]{
			{Node: slidetypes.WrappedThreadInfo{AsIGDirectThread: dmThread()}},
			{Node: slidetypes.WrappedThreadInfo{}},
		}},
		PinnedThreadsV2: []slidetypes.WrappedThreadInfo{{AsIGDirectThread: &slidetypes.ThreadInfo{ID: "g1", IsGroup: true, ThreadTitle: "G"}}},
	}
	f.a.deliverMailbox(mb)
	hs := f.sink.ofType(model.EvHistoryBatch)
	require.Len(t, hs, 1)
	assert.Len(t, hs[0].History.Chats, 2)
	assert.Len(t, hs[0].History.Persons, 1)
	assert.Empty(t, hs[0].History.Messages)
}

func TestDownloadMedia(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("foto"))
	}))
	defer srv.Close()
	_, err := f.a.DownloadMedia(ctx, model.Media{})
	assert.ErrorIs(t, err, port.ErrNotFound)
	rc, err := f.a.DownloadMedia(ctx, model.Media{Handle: meta.MakeHandle(meta.Handle{URL: srv.URL + "/ok"})})
	require.NoError(t, err)
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	assert.Equal(t, "foto", string(b))
	_, err = f.a.DownloadMedia(ctx, model.Media{Handle: meta.MakeHandle(meta.Handle{URL: srv.URL + "/gone"})})
	assert.ErrorIs(t, err, meta.ErrMediaGone)
}

func TestOnAvatarEmitsPersonAndChat(t *testing.T) {
	f := newFixture(t)
	f.a.conv.Thread(dmThread())
	f.a.onAvatar("thr1", "chat.jpg")
	cs := f.sink.ofType(model.EvChatUpsert)
	require.Len(t, cs, 1)
	assert.Equal(t, "/avatar/ig-test/chat.jpg", cs[0].Chat.AvatarURL)
	assert.Equal(t, -1, cs[0].Chat.UnreadCount)
	f.a.onAvatar("200", "ana.jpg")
	ps := f.sink.ofType(model.EvPerson)
	require.Len(t, ps, 1)
	assert.Equal(t, "/avatar/ig-test/ana.jpg", ps[0].Person.AvatarURL)
}

func TestResolveAndListPersons(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.a.conv.Thread(dmThread())
	p, err := f.a.ResolvePerson(ctx, "200")
	require.NoError(t, err)
	assert.Equal(t, "Ana Pérez", p.PushName)
	_, err = f.a.ResolvePerson(ctx, "no-numero")
	assert.ErrorIs(t, err, port.ErrInvalid)
	ps, err := f.a.ListPersons(ctx)
	require.NoError(t, err)
	require.Len(t, ps, 1)
	assert.Equal(t, "ana", ps[0].Username)
}

func TestMapErrAndHelpers(t *testing.T) {
	f := newFixture(t)
	assert.NoError(t, f.a.mapErr("x", nil))
	assert.ErrorIs(t, f.a.mapErr("x", port.ErrRateLimited), port.ErrRateLimited)
	assert.ErrorIs(t, f.a.mapErr("x", instameow.ErrThreadNotFound), port.ErrNotFound)
	err := f.a.mapErr("enviar", io.ErrUnexpectedEOF)
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Contains(t, err.Error(), "enviar")
	assert.Equal(t, "mid.1", baseMsgID("mid.1#3"))
	assert.Equal(t, "❤", stripVariation("❤️"))
}
