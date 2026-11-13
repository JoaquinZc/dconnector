package whatsapp

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"

	_ "modernc.org/sqlite"
)

// openTestDB abre un account.db temporal como lo hace el almacén: driver "sqlite" de modernc con
// claves foráneas activadas.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "account.db")) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

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

func (s *sinkRec) states() []model.ConnState {
	var out []model.ConnState
	for _, e := range s.all() {
		if e.Type == model.EvAccountStatus {
			out = append(out, e.AccountStatus.State)
		}
	}
	return out
}

func newTestAdapter(t *testing.T) (*Adapter, *sinkRec) {
	t.Helper()
	deps := port.AdapterDeps{
		Account:   model.Account{ID: testAcc, Channel: model.ChannelWhatsApp},
		DB:        openTestDB(t),
		AvatarDir: filepath.Join(t.TempDir(), "avatars"),
		MediaDir:  filepath.Join(t.TempDir(), "media"),
		Log:       slog.New(slog.NewTextHandler(&strings.Builder{}, nil)),
	}
	ad, err := New(deps)
	require.NoError(t, err)
	a := ad.(*Adapter)
	return a, &sinkRec{}
}

var _ port.ChannelAdapter = (*Adapter)(nil)

func TestNewRequiresDB(t *testing.T) {
	_, err := New(port.AdapterDeps{})
	assert.Error(t, err)
}

func TestFactoryRegisteredShape(t *testing.T) {
	var f port.AdapterFactory = New
	_ = f
	a, _ := newTestAdapter(t)
	assert.Equal(t, model.ChannelWhatsApp, a.Channel())
}

// El dialecto «sqlite» de whatsmeow funciona con el driver modernc: crea las tablas whatsmeow_*
// y entrega un dispositivo nuevo (sin red).
func TestSessionStoreUpgradeWithModernc(t *testing.T) {
	a, _ := newTestAdapter(t)
	ctx := context.Background()
	require.NoError(t, a.ensureClient(ctx))
	cli := a.getClient()
	require.NotNil(t, cli)
	assert.Nil(t, cli.Store.ID, "sin sesión guardada")

	var n int
	require.NoError(t, a.deps.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'whatsmeow_%'").Scan(&n))
	assert.Greater(t, n, 5)
	require.NoError(t, a.deps.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'wa\\_%' ESCAPE '\\'").Scan(&n))
	assert.Equal(t, 4, n)

	// Idempotente.
	require.NoError(t, a.ensureClient(ctx))
	assert.Same(t, cli, a.getClient())
}

func TestStartWithoutSessionWaitsForPairing(t *testing.T) {
	a, sink := newTestAdapter(t)
	require.NoError(t, a.Start(context.Background(), sink))
	assert.Equal(t, []model.ConnState{model.StatePairing}, sink.states())
	st := sink.all()[0].AccountStatus
	assert.Equal(t, model.NeedNone, st.Need)
	assert.Equal(t, testAcc, st.AccountID)
	assert.Equal(t, model.ChannelWhatsApp, sink.all()[0].Channel)
	assert.NotZero(t, st.Since)

	require.NoError(t, a.Stop(context.Background()))
	assert.Equal(t, []model.ConnState{model.StatePairing, model.StateDisconnected}, sink.states())
}

func TestStopOnContextCancel(t *testing.T) {
	a, sink := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, a.Start(ctx, sink))
	cancel()
	require.Eventually(t, func() bool {
		s := sink.states()
		return len(s) >= 2 && s[len(s)-1] == model.StateDisconnected
	}, 2*time.Second, 10*time.Millisecond)
}

func TestLogoutWithoutSession(t *testing.T) {
	a, sink := newTestAdapter(t)
	require.NoError(t, a.Start(context.Background(), sink))
	require.NoError(t, a.Logout(context.Background()))
	states := sink.states()
	assert.Equal(t, model.StateLoggedOut, states[len(states)-1])
	last := sink.all()[len(sink.all())-1].AccountStatus
	assert.NotEmpty(t, last.Detail)
	assert.Nil(t, a.getClient(), "el siguiente Start crea un dispositivo nuevo")
	// Vuelve a arrancar.
	require.NoError(t, a.Start(context.Background(), sink))
	assert.NotNil(t, a.getClient())
}

func TestSetStatusDeduplicates(t *testing.T) {
	a, sink := newTestAdapter(t)
	a.sink = sink
	a.setStatus(model.AccountStatus{State: model.StateConnecting})
	a.setStatus(model.AccountStatus{State: model.StateConnecting})
	a.setStatus(model.AccountStatus{State: model.StateConnected})
	assert.Equal(t, []model.ConnState{model.StateConnecting, model.StateConnected}, sink.states())
}

func TestPairingValidationAndUnsupported(t *testing.T) {
	a, _ := newTestAdapter(t)
	ctx := context.Background()
	assert.ErrorIs(t, a.BeginPairing(ctx, model.PairRequest{Method: "cookies"}), port.ErrInvalid)
	assert.ErrorIs(t, a.BeginPairing(ctx, model.PairRequest{Method: "phone", Phone: "123"}), port.ErrInvalid)
	assert.ErrorIs(t, a.BeginPairing(ctx, model.PairRequest{Method: "phone", Phone: "0991234567"}), port.ErrInvalid)
	assert.ErrorIs(t, a.SubmitPairing(ctx, model.PairInput{}), port.ErrUnsupported)
}

func TestUnsupportedAndNotConnected(t *testing.T) {
	a, _ := newTestAdapter(t)
	ctx := context.Background()
	// Sin conexión, todo lo que habla con WhatsApp avisa que la cuenta no está conectada.
	_, err := a.Send(ctx, pnPeer, model.Outgoing{Kind: model.KindText, Text: "hola"})
	assert.ErrorIs(t, err, port.ErrNotConnected)
	assert.ErrorIs(t, a.React(ctx, model.MessageRef{ChatNativeID: pnPeer, NativeID: "m"}, "👍"), port.ErrNotConnected)
	assert.ErrorIs(t, a.Edit(ctx, model.MessageRef{ChatNativeID: pnPeer, NativeID: "m", FromMe: true}, "x"), port.ErrNotConnected)
	assert.ErrorIs(t, a.Delete(ctx, model.MessageRef{ChatNativeID: pnPeer, NativeID: "m"}, true), port.ErrNotConnected)
	assert.ErrorIs(t, a.MarkRead(ctx, pnPeer, nil), port.ErrNotConnected)
	assert.ErrorIs(t, a.SetTyping(ctx, pnPeer, true), port.ErrNotConnected)
	assert.ErrorIs(t, a.ArchiveChat(ctx, pnPeer, true), port.ErrNotConnected)
	assert.ErrorIs(t, a.RequestHistory(ctx, pnPeer, nil, 10), port.ErrNotConnected)
	assert.ErrorIs(t, a.SyncChats(ctx), port.ErrNotConnected)
	_, err = a.PostStatus(ctx, model.OutgoingStatus{Kind: model.KindText, Text: "x"})
	assert.ErrorIs(t, err, port.ErrNotConnected)
	_, err = a.FetchAvatar(ctx, pnPeer)
	assert.ErrorIs(t, err, port.ErrNotConnected)
	_, err = a.ListPersons(ctx)
	assert.ErrorIs(t, err, port.ErrNotConnected)

	// Borrar solo para mí no llama a WhatsApp.
	assert.NoError(t, a.Delete(ctx, model.MessageRef{ChatNativeID: pnPeer, NativeID: "m"}, false))

	// Lo que no se implementa sale de port.Base.
	assert.ErrorIs(t, a.ReactStatus(ctx, model.Status{}, "👍"), port.ErrUnsupported)
	_, err = a.StatusViewers(ctx, "st")
	assert.ErrorIs(t, err, port.ErrUnsupported)

	// Descargar sin dato de descarga: no encontrado (no hay red que probar).
	_, err = a.DownloadMedia(ctx, model.Media{})
	assert.ErrorIs(t, err, port.ErrNotFound)
	_, err = a.DownloadMedia(ctx, model.Media{Handle: []byte{9, 1}})
	assert.ErrorIs(t, err, port.ErrInvalid)
}

func TestCapabilities(t *testing.T) {
	a, _ := newTestAdapter(t)
	caps := a.Capabilities(nil)
	assert.True(t, caps.SendText && caps.SendPoll && caps.VotePoll && caps.Forward && caps.Edit && caps.DeleteForEveryone)
	assert.Equal(t, 1, caps.MaxReactionsPerUser)
	assert.Equal(t, 15*time.Minute, caps.EditWindow)
	assert.Equal(t, 48*time.Hour, caps.DeleteWindow)
	assert.True(t, caps.Statuses && caps.PostStatus && caps.StatusViewers)
	assert.False(t, caps.StatusAudience, "whatsmeow no permite fijar la audiencia de un estado")
	assert.False(t, caps.StatusReact)
	assert.True(t, caps.HistoryOnDemand)
	assert.EqualValues(t, 100<<20, caps.MaxFileSize)
	assert.EqualValues(t, 16<<20, caps.MaxImageSize)
	assert.EqualValues(t, 64<<20, caps.MaxVideoSize)
	assert.Equal(t, 65536, caps.MaxTextLen)

	// Chat normal: las mismas.
	assert.Equal(t, caps, a.Capabilities(&model.Chat{NativeID: pnPeer}))
	// Chat marcado de solo lectura.
	ro := a.Capabilities(&model.Chat{NativeID: groupID, ReadOnly: true})
	assert.False(t, ro.SendText)
	assert.False(t, ro.React)
	assert.True(t, ro.ReadReceipts)
	// Canal donde no soy admin: solo lectura por defecto.
	assert.False(t, a.Capabilities(&model.Chat{NativeID: "1203@newsletter"}).SendText)
	a.setReadOnly("1203@newsletter", false)
	assert.True(t, a.Capabilities(&model.Chat{NativeID: "1203@newsletter"}).SendText)
	// Grupo «solo administradores» donde no lo soy.
	a.setReadOnly(groupID, true)
	assert.False(t, a.Capabilities(&model.Chat{NativeID: groupID}).SendText)
}

func TestWireJIDRemembersOriginalAddressing(t *testing.T) {
	a, _ := newTestAdapter(t)
	assert.Equal(t, jPeer, a.wireJID(jPeer))
	a.noteWire(jPeerLID, jPeer)
	assert.Equal(t, jPeerLID, a.wireJID(jPeer))
	// Solo se recuerda @lid → teléfono.
	a.noteWire(jPeer, jOther)
	assert.Equal(t, jOther, a.wireJID(jOther))
}

func TestChatShellUsesMirrorFlags(t *testing.T) {
	a, _ := newTestAdapter(t)
	ctx := context.Background()
	require.NoError(t, a.local.ensure(ctx))
	c := testConv()
	ch := a.chatShell(ctx, c, jPeer, false)
	assert.Equal(t, model.ChatDirect, ch.Kind)
	assert.Equal(t, pnPeer, ch.PeerNativeID)
	assert.Equal(t, "+593991111111", ch.Title) // sin nombre conocido, el teléfono
	assert.False(t, ch.Archived)

	a.local.saveChatFlags(ctx, pnPeer, chatFlags{Archived: true, Pinned: true, Muted: -1})
	ch = a.chatShell(ctx, c, jPeer, false)
	assert.True(t, ch.Archived)
	assert.True(t, ch.Pinned)
	assert.EqualValues(t, -1, ch.MutedUntil)

	self := a.chatShell(ctx, c, jSelf, false)
	assert.Equal(t, model.ChatSelf, self.Kind)

	g := a.chatShell(ctx, c, jGroup, false)
	assert.Equal(t, model.ChatGroup, g.Kind)
	assert.Equal(t, "", g.PeerNativeID)

	nl := a.chatShell(ctx, c, mustJID(t, "1203@newsletter"), false)
	assert.Equal(t, model.ChatChannel, nl.Kind)
	assert.True(t, nl.ReadOnly)
}

func mustJID(t *testing.T, s string) types.JID {
	t.Helper()
	j, err := nativeToJID(s)
	require.NoError(t, err)
	return j
}

// ---------------------------------------------------------------------------------------------
// Tablas propias (wa_)

func TestLocalDBChatsAndMessages(t *testing.T) {
	db := openTestDB(t)
	l := newLocalDB(db)
	ctx := context.Background()
	require.NoError(t, l.ensure(ctx))
	require.NoError(t, l.ensure(ctx)) // idempotente

	assert.False(t, l.chatKnown(ctx, pnPeer))
	l.markChat(ctx, pnPeer)
	assert.True(t, l.chatKnown(ctx, pnPeer))
	// Sobrevive a un objeto nuevo sobre la misma base.
	assert.True(t, newLocalDB(db).chatKnown(ctx, pnPeer))

	assert.Equal(t, chatFlags{}, l.chatFlagsOf(ctx, pnPeer))
	l.saveChatFlags(ctx, pnPeer, chatFlags{Archived: true, Muted: 123})
	assert.Equal(t, chatFlags{Archived: true, Muted: 123}, newLocalDB(db).chatFlagsOf(ctx, pnPeer))

	// kv
	_, ok := l.kvGet(ctx, "k")
	assert.False(t, ok)
	l.kvSet(ctx, "k", "1")
	l.kvSet(ctx, "k", "2")
	v, ok := l.kvGet(ctx, "k")
	assert.True(t, ok)
	assert.Equal(t, "2", v)

	// Mensajes: se guardan sin ContextInfo.
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("guardado"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("Q")},
	}}
	l.putMsg(ctx, pnPeer, "M1", testTs, msg)
	got, ok := l.getMsg(ctx, pnPeer, "M1")
	require.True(t, ok)
	assert.Equal(t, "guardado", got.GetExtendedTextMessage().GetText())
	assert.Nil(t, got.GetExtendedTextMessage().GetContextInfo())
	_, ok = l.getMsg(ctx, pnPeer, "otro")
	assert.False(t, ok)
	l.putMsg(ctx, pnPeer, "", testTs, msg) // sin id: se ignora
	l.putMsg(ctx, pnPeer, "M2", testTs, nil)

	l.wipe(ctx)
	assert.False(t, l.chatKnown(ctx, pnPeer))
	_, ok = l.getMsg(ctx, pnPeer, "M1")
	assert.False(t, ok)
	_, ok = l.kvGet(ctx, "k")
	assert.False(t, ok)
}

func TestLocalDBPrunesOldMessages(t *testing.T) {
	db := openTestDB(t)
	l := newLocalDB(db)
	ctx := context.Background()
	require.NoError(t, l.ensure(ctx))
	total := msgCacheKeep + msgCachePruneEvery // el podado ocurre en múltiplos de msgCachePruneEvery
	tx, err := db.Begin()
	require.NoError(t, err)
	b, _ := proto.Marshal(txt("x"))
	for i := 0; i < msgCacheKeep+msgCachePruneEvery-1; i++ {
		_, err := tx.Exec("INSERT INTO wa_msgs(chat, id, ts, payload) VALUES(?,?,?,?)", "c", "id"+itoa(i), i, b)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	l.puts = msgCachePruneEvery - 1
	l.putMsg(ctx, "c", "nuevo", int64(total+10), txt("último")) // dispara el podado
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM wa_msgs").Scan(&n))
	assert.Equal(t, msgCacheKeep, n)
	_, ok := l.getMsg(ctx, "c", "nuevo")
	assert.True(t, ok, "lo más reciente se conserva")
	_, ok = l.getMsg(ctx, "c", "id0")
	assert.False(t, ok, "lo más viejo se poda")
}

func itoa(i int) string { return strconv.Itoa(i) }

// ---------------------------------------------------------------------------------------------
// Encuestas

func hashesOf(names ...string) [][]byte { return whatsmeow.HashPollOptions(names) }

func TestPollStateVotes(t *testing.T) {
	p := &pollState{Question: "¿?", Options: []string{"Pizza", "Sushi", "Tacos"}, Multi: true}

	assert.True(t, p.apply("ana", hashesOf("Pizza", "Tacos")))
	assert.True(t, p.apply("beto", hashesOf("Pizza")))
	got := p.model()
	assert.Equal(t, "¿?", got.Question)
	assert.True(t, got.Multi)
	require.Len(t, got.Options, 3)
	assert.Equal(t, []string{"ana", "beto"}, got.Options[0].Voters)
	assert.Equal(t, []string{}, got.Options[1].Voters)
	assert.Equal(t, []string{"ana"}, got.Options[2].Voters)

	// Un voto nuevo reemplaza al anterior de la misma persona.
	assert.True(t, p.apply("ana", hashesOf("Sushi")))
	got = p.model()
	assert.Equal(t, []string{"beto"}, got.Options[0].Voters)
	assert.Equal(t, []string{"ana"}, got.Options[1].Voters)
	assert.Equal(t, []string{}, got.Options[2].Voters)

	// Un voto vacío quita el voto.
	assert.True(t, p.apply("ana", nil))
	assert.Equal(t, []string{}, p.model().Options[1].Voters)

	// Hashes que no son de esta encuesta: no se aplica.
	assert.False(t, p.apply("carlos", hashesOf("Otra")))
	assert.False(t, p.apply("carlos", [][]byte{[]byte("corto")}))
	assert.NotContains(t, p.Votes, "carlos")

	// Mi propio voto por nombre.
	p.setVote("yo", []string{"Tacos", "inexistente"})
	assert.Equal(t, []string{"yo"}, p.model().Options[2].Voters)
	p.setVote("yo", nil)
	assert.Equal(t, []string{}, p.model().Options[2].Voters)
}

func TestPollBookPersists(t *testing.T) {
	db := openTestDB(t)
	l := newLocalDB(db)
	ctx := context.Background()
	require.NoError(t, l.ensure(ctx))

	book := newPollBook(db)
	poll := &model.Poll{Question: "¿Dónde?", Multi: false, Options: []model.PollOption{{Name: "A"}, {Name: "B"}}}
	reg := book.register(ctx, pnPeer, "P1", poll)
	require.Len(t, reg.Options, 2)

	got, ok := book.vote(ctx, pnPeer, "P1", "ana", hashesOf("B"))
	require.True(t, ok)
	assert.Equal(t, []string{"ana"}, got.Options[1].Voters)

	// Un voto a una encuesta que no se conoce se descarta.
	_, ok = book.vote(ctx, pnPeer, "NO", "ana", hashesOf("B"))
	assert.False(t, ok)

	// Otra instancia sobre la misma base recupera opciones y votos.
	book2 := newPollBook(db)
	opts, ok := book2.options(ctx, pnPeer, "P1")
	require.True(t, ok)
	assert.Equal(t, []string{"A", "B"}, opts)
	again := book2.register(ctx, pnPeer, "P1", poll) // volver a ver el mensaje no pierde los votos
	assert.Equal(t, []string{"ana"}, again.Options[1].Voters)
	mine, ok := book2.myVote(ctx, pnPeer, "P1", "yo", []string{"A"})
	require.True(t, ok)
	assert.Equal(t, []string{"yo"}, mine.Options[0].Voters)
	assert.Equal(t, []string{"ana"}, mine.Options[1].Voters)
	assert.Nil(t, book2.register(ctx, pnPeer, "X", nil))
}

// ---------------------------------------------------------------------------------------------
// Registro

func TestRedactAndWALogger(t *testing.T) {
	assert.Equal(t, "usuario …1111 conectado", redact("usuario 593991111111 conectado"))
	assert.Equal(t, "…1111@s.whatsapp.net", redact("593991111111@s.whatsapp.net"))
	assert.Equal(t, "id 12345 corto", redact("id 12345 corto"))

	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	l := newWALogger(log).Sub("Client")
	l.Infof("mensaje a %s enviado", "593991111111@s.whatsapp.net")
	l.Debugf("depuración")
	out := buf.String()
	assert.NotContains(t, out, "593991111111")
	assert.NotContains(t, out, "depuración", "el nivel Debug de la librería nunca se registra")
	assert.Contains(t, out, "…1111")
	assert.Contains(t, out, "lib=Client")

	var quiet strings.Builder
	q := newWALogger(slog.New(slog.NewTextHandler(&quiet, &slog.HandlerOptions{Level: slog.LevelWarn})))
	q.Infof("no sale")
	assert.Empty(t, quiet.String())
	q.Errorf("sí sale")
	assert.Contains(t, quiet.String(), "sí sale")
}

func TestMapErr(t *testing.T) {
	a, _ := newTestAdapter(t)
	assert.NoError(t, a.mapErr("x", nil))
	assert.ErrorIs(t, a.mapErr("enviar", whatsmeow.ErrNotConnected), port.ErrNotConnected)
	assert.ErrorIs(t, a.mapErr("enviar", whatsmeow.ErrNotLoggedIn), port.ErrNotConnected)
	assert.ErrorIs(t, a.mapErr("enviar", whatsmeow.ErrRecipientNotOnWhatsApp), port.ErrNotFound)
	assert.ErrorIs(t, a.mapErr("enviar", whatsmeow.ErrNoPermissionInGroup), port.ErrInvalid)
	assert.ErrorIs(t, a.mapErr("enviar", port.ErrUnsupported), port.ErrUnsupported)
	timeout := a.mapErr("enviar", whatsmeow.ErrMessageTimedOut)
	assert.ErrorIs(t, timeout, whatsmeow.ErrMessageTimedOut)
	assert.Contains(t, timeout.Error(), "tardó demasiado")
	other := a.mapErr("enviar", assert.AnError)
	assert.ErrorIs(t, other, assert.AnError)
	assert.True(t, strings.HasPrefix(other.Error(), "whatsapp: enviar: "))
}
