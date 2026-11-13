package messenger

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/whatsmeow/types"
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
	db   *sql.DB
	dir  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "account.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ad, err := New(port.AdapterDeps{
		Account: model.Account{ID: "fb-test", Channel: model.ChannelMessenger, SelfID: "100"},
		DB:      db, DBPath: filepath.Join(dir, "account.db"),
		MediaDir: filepath.Join(dir, "media"), AvatarDir: filepath.Join(dir, "avatars"),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	a := ad.(*Adapter)
	sink := &sinkRec{}
	a.SetSink(sink)
	return &fixture{a: a, sink: sink, db: db, dir: dir}
}

func TestFactoryAndChannel(t *testing.T) {
	f := newFixture(t)
	assert.Equal(t, model.ChannelMessenger, f.a.Channel())
	_, err := New(port.AdapterDeps{Account: model.Account{ID: "fb-x"}})
	assert.Error(t, err, "sin base de datos no se puede crear")
}

func TestCapabilities(t *testing.T) {
	f := newFixture(t)
	c := f.a.Capabilities(nil)
	assert.True(t, c.SendText)
	assert.True(t, c.SendImage && c.SendVideo && c.SendAudio && c.SendVoice && c.SendDocument)
	assert.False(t, c.SendSticker || c.SendLocation || c.SendContact || c.SendPoll || c.VotePoll, "lo no implementado no se declara")
	assert.True(t, c.Reply)
	assert.False(t, c.Forward)
	assert.True(t, c.React)
	assert.Equal(t, 1, c.MaxReactionsPerUser)
	assert.True(t, c.Edit)
	assert.Equal(t, 15*time.Minute, c.EditWindow)
	assert.True(t, c.DeleteForEveryone)
	assert.True(t, c.ReadReceipts && c.Typing && c.Groups)
	assert.True(t, c.HistoryOnDemand)
	assert.True(t, c.E2EEChats, "los chats cifrados están implementados")
	assert.False(t, c.Statuses || c.PostStatus || c.StatusViewers || c.StatusAudience || c.StatusReact)
	assert.False(t, c.Mentions)
	assert.Equal(t, int64(25<<20), c.MaxFileSize)
	assert.Equal(t, int64(25<<20), c.MaxImageSize)
	assert.Equal(t, int64(25<<20), c.MaxVideoSize)
	assert.Equal(t, 20000, c.MaxTextLen)

	ro := f.a.Capabilities(&model.Chat{ReadOnly: true})
	assert.False(t, ro.SendText || ro.React || ro.Edit)
	assert.True(t, f.a.Capabilities(&model.Chat{}).SendText)
}

func TestStartWithoutCookiesAsksForThem(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.a.Start(context.Background(), f.sink))
	st := f.sink.lastStatus()
	assert.Equal(t, model.StatePairing, st.State)
	assert.Equal(t, model.NeedCookies, st.Need)
	assert.Contains(t, st.Detail, "messenger.com")
	assert.Equal(t, 1, len(f.sink.ofType(model.EvAccountStatus)))
	require.NoError(t, f.a.Stop(context.Background()))
	assert.Equal(t, model.StateDisconnected, f.sink.lastStatus().State)
}

func TestPairingMethodsAndValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.a.BeginPairing(ctx, model.PairRequest{Method: "password_login"}))
	assert.Equal(t, model.NeedPassword, f.sink.lastStatus().Need)
	require.NoError(t, f.a.BeginPairing(ctx, model.PairRequest{Method: "cookies"}))
	assert.Equal(t, model.NeedCookies, f.sink.lastStatus().Need)
	assert.ErrorIs(t, f.a.BeginPairing(ctx, model.PairRequest{Method: "qr"}), port.ErrInvalid)

	err := f.a.SubmitPairing(ctx, model.PairInput{Cookies: "c_user=1000123; datr=d"})
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Contains(t, f.sink.lastStatus().Detail, "xs")
	assert.NotContains(t, err.Error(), "1000123")
	assert.Equal(t, model.NeedCookies, f.sink.lastStatus().Need)
}

func TestOperationsWithoutConnection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ref := model.MessageRef{ChatNativeID: "200", NativeID: "mid.1", FromMe: true, Timestamp: time.Now().UnixMilli()}

	_, err := f.a.Send(ctx, "200", model.Outgoing{Kind: model.KindText, Text: "hola"})
	assert.ErrorIs(t, err, port.ErrNotConnected)
	assert.ErrorIs(t, f.a.React(ctx, ref, "👍"), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.Edit(ctx, ref, "nuevo"), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.Delete(ctx, ref, true), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.MarkRead(ctx, "200", nil), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.SetTyping(ctx, "200", true), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.SyncChats(ctx), port.ErrNotConnected)
	assert.ErrorIs(t, f.a.RequestHistory(ctx, "200", nil, 20), port.ErrNotConnected)
	_, err = f.a.ListPersons(ctx)
	assert.ErrorIs(t, err, port.ErrNotConnected)
}

func TestValidationBeforeNetwork(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.a.Send(ctx, "200", model.Outgoing{Kind: model.KindText, Text: "   "})
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, err = f.a.Send(ctx, "200", model.Outgoing{Kind: model.KindSticker, FilePath: "x.webp"})
	assert.ErrorIs(t, err, port.ErrUnsupported)
	_, err = f.a.Send(ctx, "200", model.Outgoing{Kind: model.KindLocation})
	assert.ErrorIs(t, err, port.ErrUnsupported)
	_, err = f.a.Send(ctx, "200", model.Outgoing{Kind: model.KindPoll})
	assert.ErrorIs(t, err, port.ErrUnsupported)
	long := make([]rune, 20001)
	for i := range long {
		long[i] = 'a'
	}
	_, err = f.a.Send(ctx, "200", model.Outgoing{Kind: model.KindText, Text: string(long)})
	assert.ErrorIs(t, err, port.ErrInvalid)

	mine := model.MessageRef{ChatNativeID: "200", NativeID: "m", FromMe: true, Timestamp: time.Now().UnixMilli()}
	assert.ErrorIs(t, f.a.Edit(ctx, mine, " "), port.ErrInvalid)
	theirs := mine
	theirs.FromMe = false
	assert.ErrorIs(t, f.a.Edit(ctx, theirs, "x"), port.ErrInvalid, "solo mis mensajes")
	old := mine
	old.Timestamp = time.Now().Add(-time.Hour).UnixMilli()
	err = f.a.Edit(ctx, old, "x")
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Contains(t, err.Error(), "tiempo")
	f.a.live.Store(false)
}

func TestUnsupportedOperations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ref := model.MessageRef{ChatNativeID: "200", NativeID: "m"}
	_, err := f.a.Forward(ctx, ref, "300")
	assert.ErrorIs(t, err, port.ErrUnsupported)
	assert.ErrorIs(t, f.a.VotePoll(ctx, ref, []string{"a"}), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.ArchiveChat(ctx, "200", true), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.PinChat(ctx, "200", true), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.MuteChat(ctx, "200", -1), port.ErrUnsupported)
	_, err = f.a.PostStatus(ctx, model.OutgoingStatus{})
	assert.ErrorIs(t, err, port.ErrUnsupported)
	assert.ErrorIs(t, f.a.DeleteStatus(ctx, "x"), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.MarkStatusViewed(ctx, model.Status{}), port.ErrUnsupported)
	assert.ErrorIs(t, f.a.ReactStatus(ctx, model.Status{}, "x"), port.ErrUnsupported)
	_, err = f.a.StatusViewers(ctx, "x")
	assert.ErrorIs(t, err, port.ErrUnsupported)
}

func TestLogoutWipesLocalSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.a.KV.Ensure(ctx))
	require.NoError(t, meta.SaveCookies(ctx, f.a.KV, map[string]string{"c_user": "100", "xs": "x", "datr": "d"}))
	require.NoError(t, f.a.KV.Set(ctx, meta.KVPrefix+"e2ee.200", []byte("200@msgr")))
	require.NoError(t, f.a.Logout(ctx))
	assert.Equal(t, model.StateLoggedOut, f.sink.lastStatus().State)
	got, err := meta.LoadCookies(ctx, f.a.KV)
	require.NoError(t, err)
	assert.Nil(t, got)
	_, ok, _ := f.a.KV.Get(ctx, meta.KVPrefix+"e2ee.200")
	assert.False(t, ok)
	assert.Equal(t, "", f.a.SelfID())

	// Tras cerrar sesión, un nuevo Start vuelve a pedir las cookies.
	require.NoError(t, f.a.Start(ctx, f.sink))
	assert.Equal(t, model.NeedCookies, f.sink.lastStatus().Need)
}

func TestDownloadMedia(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("bytes del archivo"))
	}))
	defer srv.Close()

	_, err := f.a.DownloadMedia(ctx, model.Media{})
	assert.ErrorIs(t, err, port.ErrNotFound)

	rc, err := f.a.DownloadMedia(ctx, model.Media{Handle: meta.MakeHandle(meta.Handle{URL: srv.URL + "/ok"})})
	require.NoError(t, err)
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	assert.Equal(t, "bytes del archivo", string(data))

	_, err = f.a.DownloadMedia(ctx, model.Media{Handle: meta.MakeHandle(meta.Handle{URL: srv.URL + "/ok", Expires: 1})})
	assert.ErrorIs(t, err, meta.ErrMediaGone)
	_, err = f.a.DownloadMedia(ctx, model.Media{Handle: meta.MakeHandle(meta.Handle{URL: srv.URL + "/gone"})})
	assert.ErrorIs(t, err, meta.ErrMediaGone)

	// Un adjunto cifrado sin conexión de chats cifrados.
	_, err = f.a.DownloadMedia(ctx, model.Media{Handle: meta.MakeHandle(meta.Handle{E2EE: []byte(`{"directPath":"/x"}`), Type: "WhatsApp Image Keys"})})
	assert.ErrorIs(t, err, port.ErrNotConnected)
}

func TestFetchAvatarOnlyReadsDisk(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	name, err := f.a.FetchAvatar(ctx, "200")
	require.NoError(t, err)
	assert.Equal(t, "", name)
	require.NoError(t, os.MkdirAll(f.a.deps.AvatarDir, 0o755))
	file := meta.AvatarPrefix("200") + "abcd1234.jpg"
	require.NoError(t, os.WriteFile(filepath.Join(f.a.deps.AvatarDir, file), []byte("x"), 0o644))
	name, err = f.a.FetchAvatar(ctx, "200")
	require.NoError(t, err)
	assert.Equal(t, file, name)
}

func TestOnAvatarEmitsPersonAndChat(t *testing.T) {
	f := newFixture(t)
	f.a.conv.Convert(context.Background(), &table.LSTable{
		LSDeleteThenInsertContact: []*table.LSDeleteThenInsertContact{{Id: 200, Name: "Ana"}},
		LSUpdateOrInsertThread:    []*table.LSUpdateOrInsertThread{{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox"}},
	})
	f.a.onAvatar("200", "foto.jpg")
	ps := f.sink.ofType(model.EvPerson)
	require.Len(t, ps, 1)
	assert.Equal(t, "/avatar/fb-test/foto.jpg", ps[0].Person.AvatarURL)
	cs := f.sink.ofType(model.EvChatUpsert)
	require.Len(t, cs, 1)
	assert.Equal(t, "/avatar/fb-test/foto.jpg", cs[0].Chat.AvatarURL)
	assert.Equal(t, -1, cs[0].Chat.UnreadCount, "las marcas salen completas pero el contador no cambia")
}

func TestOnTableDeliversLiveEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.a.onTable(ctx, &table.LSTable{
		LSDeleteThenInsertContact: []*table.LSDeleteThenInsertContact{{Id: 200, Name: "Ana"}},
		LSUpdateOrInsertThread:    []*table.LSUpdateOrInsertThread{{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox"}},
		LSInsertMessage:           []*table.LSInsertMessage{{ThreadKey: 200, MessageId: "mid.1", Text: "hola", SenderId: 200, TimestampMs: 10}},
		LSUpdateTypingIndicator:   []*table.LSUpdateTypingIndicator{{ThreadKey: 200, SenderId: 200, IsTyping: true}},
	})
	var order []model.EventType
	for _, e := range f.sink.all() {
		order = append(order, e.Type)
	}
	assert.Equal(t, []model.EventType{model.EvPerson, model.EvChatUpsert, model.EvMessageNew, model.EvTyping}, order)
	m := f.sink.ofType(model.EvMessageNew)[0]
	assert.Equal(t, "fb-test", string(m.AccountID))
	assert.Equal(t, model.ChannelMessenger, m.Channel)
	assert.NotZero(t, m.At)
	assert.Equal(t, "hola", m.Message.Text)
	// El adaptador recuerda el chat de cada mensaje (para ediciones sin hilo).
	assert.Equal(t, "200", f.a.local.ChatOfMessage(ctx, "mid.1"))

	// Una edición posterior se resuelve con ese recuerdo.
	f.a.onTable(ctx, &table.LSTable{LSEditMessage: []*table.LSEditMessage{{MessageID: "mid.1", Text: "hola!"}}})
	us := f.sink.ofType(model.EvMessageUpdate)
	require.Len(t, us, 1)
	assert.Equal(t, "hola!", *us[0].Update.Text)
	f.a.typers.StopAll()
}

func TestDeliverHistoryBatches(t *testing.T) {
	f := newFixture(t)
	tbl := &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{ThreadKey: 200, ThreadType: table.ONE_TO_ONE, FolderName: "inbox"}},
	}
	for i := 0; i < 1200; i++ {
		tbl.LSInsertMessage = append(tbl.LSInsertMessage, &table.LSInsertMessage{
			ThreadKey: 200, MessageId: "mid." + string(rune('a'+i%26)) + time.Duration(i).String(), Text: "x", SenderId: 200, TimestampMs: int64(i + 1),
		})
	}
	f.a.deliverHistory(context.Background(), tbl)
	hs := f.sink.ofType(model.EvHistoryBatch)
	require.Len(t, hs, 3)
	assert.Len(t, hs[0].History.Messages, 500)
	assert.Len(t, hs[0].History.Chats, 1)
	assert.Equal(t, 1.0, hs[2].History.Progress)
	assert.Empty(t, f.sink.ofType(model.EvMessageNew), "el historial no es «mensaje nuevo»")
}

func TestE2EEChatDetection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.a.KV.Ensure(ctx))

	_, ok := f.a.e2eeChat("200")
	assert.False(t, ok, "un chat normal no es cifrado")
	_, ok = f.a.e2eeChat("")
	assert.False(t, ok)

	// Por el tipo de hilo.
	f.a.conv.ShowEncrypted = true
	f.a.conv.Convert(ctx, &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
			{ThreadKey: 300, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, FolderName: "e2ee_cutover"},
			{ThreadKey: 400, ThreadType: table.ENCRYPTED_OVER_WA_GROUP, FolderName: "e2ee_cutover", ThreadName: "G"},
		},
	})
	jid, ok := f.a.e2eeChat("300")
	require.True(t, ok)
	assert.Equal(t, types.NewJID("300", types.MessengerServer), jid)
	jid, ok = f.a.e2eeChat("400")
	require.True(t, ok)
	assert.Equal(t, types.NewJID("400", types.GroupServer), jid)

	// Por lo aprendido de un mensaje, que además se guarda en el KV.
	f.a.rememberE2EEChat("500", types.NewJID("500", types.MessengerServer))
	jid, ok = f.a.e2eeChat("500")
	require.True(t, ok)
	assert.Equal(t, "500@msgr", jid.String())
	b, found, _ := f.a.KV.Get(ctx, meta.KVPrefix+"e2ee.500")
	assert.True(t, found)
	assert.Equal(t, "500@msgr", string(b))

	// Un adaptador nuevo con la misma base lo recuerda.
	f2 := &Adapter{Core: f.a.Core, deps: f.a.deps, conv: meta.NewConverter("fb-test", model.ChannelMessenger, 100)}
	f2.e2ee = newE2EE(f2)
	jid, ok = f2.e2eeChat("500")
	require.True(t, ok)
	assert.Equal(t, "500@msgr", jid.String())
}

func TestE2EEOnMessageEmitsChatAndMessage(t *testing.T) {
	f := newFixture(t)
	f.a.e2eeOnMessage(dmEvent("200", false, text("secreto")))
	cs := f.sink.ofType(model.EvChatUpsert)
	require.Len(t, cs, 1)
	assert.Equal(t, "200", cs[0].Chat.NativeID)
	assert.Equal(t, -1, cs[0].Chat.UnreadCount)
	ms := f.sink.ofType(model.EvMessageNew)
	require.Len(t, ms, 1)
	assert.Equal(t, "secreto", ms[0].Message.Text)
	_, ok := f.a.e2eeChat("200")
	assert.True(t, ok, "el chat queda marcado como cifrado")

	// El segundo mensaje del mismo chat ya no anuncia el chat.
	f.a.e2eeOnMessage(dmEvent("200", false, text("otro")))
	assert.Len(t, f.sink.ofType(model.EvChatUpsert), 1)
	assert.Len(t, f.sink.ofType(model.EvMessageNew), 2)
	// Sin conexión de chats cifrados, enviar falla con un error claro.
	_, err := f.a.Send(context.Background(), "200", model.Outgoing{Kind: model.KindText, Text: "x"})
	assert.ErrorIs(t, err, port.ErrNotConnected)
}

func TestMapErr(t *testing.T) {
	f := newFixture(t)
	assert.NoError(t, f.a.mapErr("x", nil))
	assert.ErrorIs(t, f.a.mapErr("x", port.ErrRateLimited), port.ErrRateLimited)
	assert.ErrorIs(t, f.a.mapErr("x", context.Canceled), context.Canceled)
	err := f.a.mapErr("enviar", io.ErrUnexpectedEOF)
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Contains(t, err.Error(), "enviar")
}

func TestStripVariationAndTimestamps(t *testing.T) {
	assert.Equal(t, "❤", stripVariation("❤️"))
	now := time.Now().UnixMilli()
	assert.InDelta(t, now, sendTimestamp("mid.invalido"), 2000)
}

// Interruptor de los chats cifrados: si newE2EE devolviera nil, E2EEChats sería false, los hilos cifrados
// no se mostrarían y nada de lo cifrado se tocaría (sin pánicos).
func TestE2EEOffSwitch(t *testing.T) {
	f := newFixture(t)
	assert.True(t, f.a.conv.ShowEncrypted)
	f.a.e2ee = nil
	f.a.conv.ShowEncrypted = false
	ctx := context.Background()
	assert.False(t, f.a.Capabilities(nil).E2EEChats)
	_, ok := f.a.e2eeChat("200")
	assert.False(t, ok)
	f.a.e2eeConnect(ctx)
	f.a.e2eeStop()
	f.a.e2eeReset(ctx)
	_, err := f.a.e2eeClient()
	assert.ErrorIs(t, err, port.ErrUnsupported)
	r := f.a.conv.Convert(ctx, &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{ThreadKey: 401, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, FolderName: "e2ee_cutover"}},
	})
	assert.Empty(t, r.Chats)
}
