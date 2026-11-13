package telegram

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func (s *sinkRec) reset() {
	s.mu.Lock()
	s.evs = nil
	s.mu.Unlock()
}

func (s *sinkRec) states() []model.ConnState {
	var out []model.ConnState
	for _, e := range s.ofType(model.EvAccountStatus) {
		out = append(out, e.AccountStatus.State)
	}
	return out
}

// createMessagesTable crea una versión mínima de la tabla messages del almacén (05-ALMACEN).
func createMessagesTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE messages (
  chat_native_id TEXT NOT NULL, native_id TEXT NOT NULL, sender_native_id TEXT NOT NULL DEFAULT '',
  sender_name TEXT NOT NULL DEFAULT '', from_me INTEGER NOT NULL, ts INTEGER NOT NULL, kind TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '', delivery TEXT NOT NULL DEFAULT 'sent', PRIMARY KEY (chat_native_id, native_id))`)
	require.NoError(t, err)
}

func insertMessage(t *testing.T, db *sql.DB, chat, id string, fromMe int, kind, text, delivery string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO messages(chat_native_id, native_id, sender_name, from_me, ts, kind, text, delivery)
VALUES(?, ?, 'Ana', ?, 1, ?, ?, ?)`, chat, id, fromMe, kind, text, delivery)
	require.NoError(t, err)
}

func newTestAdapter(t *testing.T, cfg map[string]string) (*Adapter, *sinkRec) {
	t.Helper()
	deps := port.AdapterDeps{
		Account:   model.Account{ID: testAcc, Channel: model.ChannelTelegram, Config: cfg},
		DB:        openTestDB(t),
		AvatarDir: filepath.Join(t.TempDir(), "avatars"),
		MediaDir:  filepath.Join(t.TempDir(), "media"),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ad, err := New(deps)
	require.NoError(t, err)
	a := ad.(*Adapter)
	require.NoError(t, a.kv.ensure(bg))
	sink := &sinkRec{}
	a.sink = sink
	a.selfID.Store(selfUID)
	return a, sink
}

var _ port.ChannelAdapter = (*Adapter)(nil)

func TestNewRequiresDB(t *testing.T) {
	_, err := New(port.AdapterDeps{})
	assert.Error(t, err)
}

func TestFactoryShape(t *testing.T) {
	var f port.AdapterFactory = New
	ad, err := f(port.AdapterDeps{Account: model.Account{ID: testAcc}, DB: openTestDB(t)})
	require.NoError(t, err)
	assert.Equal(t, model.ChannelTelegram, ad.Channel())
}

func TestCredentials(t *testing.T) {
	id, hash, ok := credentials(map[string]string{"api_id": " 12345 ", "api_hash": " abcdef "})
	assert.True(t, ok)
	assert.Equal(t, 12345, id)
	assert.Equal(t, "abcdef", hash)
	for _, cfg := range []map[string]string{
		nil, {}, {"api_id": "12"}, {"api_hash": "x"}, {"api_id": "abc", "api_hash": "x"}, {"api_id": "0", "api_hash": "x"},
		{"api_id": "-3", "api_hash": "x"}, {"api_id": "5", "api_hash": "  "},
	} {
		_, _, ok := credentials(cfg)
		assert.False(t, ok, "%v", cfg)
	}
}

func TestStartWithoutKeysEmitsPairing(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	require.NoError(t, a.Start(bg, sink))
	evs := sink.ofType(model.EvAccountStatus)
	require.Len(t, evs, 1)
	st := evs[0].AccountStatus
	assert.Equal(t, model.StatePairing, st.State)
	assert.Equal(t, "Falta api_id/api_hash", st.Detail)
	assert.Equal(t, testAcc, st.AccountID)
	assert.Equal(t, testAcc, evs[0].AccountID)
	assert.Equal(t, model.ChannelTelegram, evs[0].Channel)
	assert.NotZero(t, evs[0].At)

	// BeginPairing sin llaves es un error claro, no una espera.
	err := a.BeginPairing(bg, model.PairRequest{Method: "login", Phone: "+593991234567"})
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.ErrorIs(t, a.SubmitPairing(bg, model.PairInput{Code: "1"}), port.ErrInvalid)

	require.NoError(t, a.Stop(bg))
	assert.Equal(t, []model.ConnState{model.StatePairing, model.StateDisconnected}, sink.states())
	// Parar dos veces no rompe nada.
	require.NoError(t, a.Stop(bg))
}

func TestBeginPairingWithoutStartIsNotConnected(t *testing.T) {
	a, _ := newTestAdapter(t, map[string]string{"api_id": "1", "api_hash": "h"})
	err := a.BeginPairing(bg, model.PairRequest{Method: "login", Phone: "+593991234567"})
	assert.ErrorIs(t, err, port.ErrNotConnected)
	// Con la cuenta ya conectada, BeginPairing no hace nada.
	a.live.Store(true)
	assert.NoError(t, a.BeginPairing(bg, model.PairRequest{Method: "login", Phone: "+593991234567"}))
}

func TestWaitAuthReadyCancel(t *testing.T) {
	a, _ := newTestAdapter(t, map[string]string{"api_id": "1", "api_hash": "h"})
	a.mu.Lock()
	a.authReady = make(chan struct{})
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(bg)
	cancel()
	assert.ErrorIs(t, a.waitAuthReady(ctx), context.Canceled)
	a.mu.Lock()
	close(a.authReady)
	a.mu.Unlock()
	assert.NoError(t, a.waitAuthReady(bg))
}

func TestLogoutWithoutSessionWipesAndReportsLoggedOut(t *testing.T) {
	a, sink := newTestAdapter(t, map[string]string{"api_id": "1", "api_hash": "h"})
	require.NoError(t, a.kv.set(bg, "tg.peer.user:5", []byte(`{"h":1}`)))
	require.NoError(t, a.kv.set(bg, "otra.clave", []byte("x")))
	require.NoError(t, a.Logout(bg))
	assert.Equal(t, []model.ConnState{model.StateLoggedOut}, sink.states())
	_, ok, _ := a.kv.get(bg, "tg.peer.user:5")
	assert.False(t, ok, "se borra lo propio")
	_, ok, _ = a.kv.get(bg, "otra.clave")
	assert.True(t, ok, "no se borra lo ajeno")
	assert.Zero(t, a.selfID.Load())
}

func TestLogoutWithSessionButOffline(t *testing.T) {
	a, sink := newTestAdapter(t, map[string]string{"api_id": "1", "api_hash": "h"})
	require.NoError(t, a.sess.StoreSession(bg, []byte("sesion")))
	err := a.Logout(bg)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrNotConnected)
	assert.Contains(t, err.Error(), "la sesión local se cerró")
	assert.False(t, a.sess.hasSession(bg), "la sesión local se borra igual")
	assert.Equal(t, []model.ConnState{model.StateLoggedOut}, sink.states())
}

func TestCapabilities(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	c := a.Capabilities(nil)
	assert.True(t, c.SendText && c.SendImage && c.SendVideo && c.SendAudio && c.SendVoice)
	assert.True(t, c.SendDocument && c.SendSticker && c.SendLocation && c.SendContact && c.SendPoll && c.VotePoll)
	assert.True(t, c.Reply && c.Forward && c.React && c.Edit && c.DeleteForEveryone)
	assert.Equal(t, 1, c.MaxReactionsPerUser)
	assert.Equal(t, 48*time.Hour, c.EditWindow)
	assert.Zero(t, c.DeleteWindow)
	assert.True(t, c.ReadReceipts && c.Typing && c.Mentions && c.Groups && c.HistoryOnDemand)
	assert.True(t, c.Statuses && c.StatusViewers && c.StatusAudience && c.StatusReact)
	assert.False(t, c.PostStatus, "hasta que StoriesCanSendStory lo confirme")
	assert.Equal(t, int64(2000<<20), c.MaxFileSize)
	assert.Equal(t, int64(10<<20), c.MaxImageSize)
	assert.Equal(t, int64(2000<<20), c.MaxVideoSize)
	assert.Equal(t, 4096, c.MaxTextLen)

	a.postStatus.Store(true)
	assert.True(t, a.Capabilities(nil).PostStatus)
	assert.Equal(t, baseCapabilities(true), a.Capabilities(nil))

	// Un chat marcado como de solo lectura.
	ro := a.Capabilities(&model.Chat{NativeID: "user:5", ReadOnly: true})
	assert.False(t, ro.SendText || ro.SendImage || ro.React || ro.Edit)
	assert.True(t, ro.Forward)
	assert.True(t, a.Capabilities(&model.Chat{NativeID: "user:5"}).SendText)
}

func TestCapabilitiesReadOnlyChannels(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	a.peers.feed(bg, tg.Entities{Channels: map[int64]*tg.Channel{
		20: {ID: 20, Title: "Noticias", Broadcast: true, AccessHash: 1},
		21: {ID: 21, Title: "Mi canal", Broadcast: true, Creator: true, AccessHash: 1},
		22: {ID: 22, Title: "Canal con permiso", Broadcast: true, AccessHash: 1, AdminRights: tg.ChatAdminRights{PostMessages: true}},
		23: {ID: 23, Title: "Canal de admin sin publicar", Broadcast: true, AccessHash: 1, AdminRights: tg.ChatAdminRights{DeleteMessages: true}},
		30: {ID: 30, Title: "Grupo libre", Megagroup: true, AccessHash: 1},
		31: {ID: 31, Title: "Grupo mudo", Megagroup: true, AccessHash: 1, DefaultBannedRights: tg.ChatBannedRights{SendMessages: true}},
		32: {ID: 32, Title: "Grupo mudo pero admin", Megagroup: true, AccessHash: 1, DefaultBannedRights: tg.ChatBannedRights{SendMessages: true}, AdminRights: tg.ChatAdminRights{DeleteMessages: true}},
		33: {ID: 33, Title: "Canal que dejé", Broadcast: true, Left: true, AccessHash: 1},
	}})
	want := map[string]bool{
		"channel:20": false, "channel:21": true, "channel:22": true, "channel:23": false,
		"channel:30": true, "channel:31": false, "channel:32": true, "channel:33": false,
		"channel:99": true, // desconocido: se asume escribible
		"user:5":     true, "chat:7": true,
	}
	for chat, canSend := range want {
		got := a.Capabilities(&model.Chat{NativeID: chat}).SendText
		assert.Equal(t, canSend, got, chat)
	}
}

// ---------------------------------------------------------------------------------------------
// Operaciones de red sin conexión

func TestOperationsWithoutConnection(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	ref := model.MessageRef{ChatNativeID: "user:100", NativeID: "5"}
	calls := map[string]func() error{
		"Send": func() error {
			_, err := a.Send(bg, "user:100", model.Outgoing{Kind: model.KindText, Text: "hola"})
			return err
		},
		"React":       func() error { return a.React(bg, ref, "👍") },
		"Edit":        func() error { return a.Edit(bg, ref, "nuevo") },
		"Delete":      func() error { return a.Delete(bg, ref, true) },
		"Forward":     func() error { _, err := a.Forward(bg, ref, "user:101"); return err },
		"VotePoll":    func() error { return a.VotePoll(bg, ref, []string{"a"}) },
		"MarkRead":    func() error { return a.MarkRead(bg, "user:100", []model.MessageRef{ref}) },
		"SetTyping":   func() error { return a.SetTyping(bg, "user:100", true) },
		"ArchiveChat": func() error { return a.ArchiveChat(bg, "user:100", true) },
		"PinChat":     func() error { return a.PinChat(bg, "user:100", true) },
		"MuteChat":    func() error { return a.MuteChat(bg, "user:100", -1) },
		"SyncChats":   func() error { return a.SyncChats(bg) },
		"History":     func() error { return a.RequestHistory(bg, "user:100", nil, 10) },
		"ListPersons": func() error { _, err := a.ListPersons(bg); return err },
		"Download": func() error {
			_, err := a.DownloadMedia(bg, model.Media{Handle: mediaHandle{T: "doc", ID: 1}.encode()})
			return err
		},
		"PostStatus": func() error {
			_, err := a.PostStatus(bg, model.OutgoingStatus{Kind: model.KindImage, FilePath: "x.jpg"})
			return err
		},
		"DeleteStatus":     func() error { return a.DeleteStatus(bg, "user:1/5") },
		"StatusViewers":    func() error { _, err := a.StatusViewers(bg, "user:1/5"); return err },
		"MarkStatusViewed": func() error { return a.MarkStatusViewed(bg, model.Status{NativeID: "user:100/5"}) },
		"ReactStatus":      func() error { return a.ReactStatus(bg, model.Status{NativeID: "user:100/5"}, "👍") },
	}
	for name, call := range calls {
		err := call()
		require.Error(t, err, name)
		if name == "PostStatus" { // el archivo no existe: se rechaza antes de tocar la red
			continue
		}
		assert.ErrorIs(t, err, port.ErrNotConnected, name)
	}
}

func TestOperationsValidateBeforeNetwork(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	// Sin conexión, pero lo inválido se rechaza con su propio error.
	_, err := a.Send(bg, "user:100", model.Outgoing{Kind: model.KindText, Text: "  "})
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, err = a.PostStatus(bg, model.OutgoingStatus{Kind: model.KindText, Text: "hola"})
	assert.ErrorIs(t, err, port.ErrUnsupported)
	assert.Contains(t, err.Error(), "Telegram no admite historias solo de texto")
	assert.ErrorIs(t, a.Edit(bg, model.MessageRef{ChatNativeID: "user:1", NativeID: "1"}, " "), port.ErrInvalid)
	_, err = a.DownloadMedia(bg, model.Media{})
	assert.ErrorIs(t, err, port.ErrNotFound)
	_, err = a.FetchAvatar(bg, "raro")
	assert.ErrorIs(t, err, port.ErrInvalid)
	av, err := a.FetchAvatar(bg, "user:100") // sin foto conocida
	assert.NoError(t, err)
	assert.Empty(t, av)
	assert.ErrorIs(t, a.DeleteStatus(bg, "user:100/5"), port.ErrInvalid, "no es una historia propia")
	assert.ErrorIs(t, a.DeleteStatus(bg, "basura"), port.ErrInvalid)
	_, err = a.StatusViewers(bg, "user:100/5")
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.NoError(t, a.MarkRead(bg, "user:100", nil), "nada que marcar")
}

// ---------------------------------------------------------------------------------------------
// Errores

func TestMapErr(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	assert.NoError(t, a.mapErr("x", nil))
	// Los errores del dominio pasan tal cual.
	for _, e := range []error{port.ErrUnsupported, port.ErrNotConnected, port.ErrNotFound, port.ErrInvalid, port.ErrRateLimited} {
		assert.Equal(t, e, a.mapErr("x", e))
	}
	assert.ErrorIs(t, a.mapErr("x", context.Canceled), context.Canceled)

	fw := a.mapErr("enviar", fmt.Errorf("send: %w", tgerr.New(420, "FLOOD_WAIT_45")))
	assert.ErrorIs(t, fw, port.ErrRateLimited)
	assert.Contains(t, fw.Error(), "45s")

	cases := map[string]error{
		"PEER_ID_INVALID":           port.ErrNotFound,
		"MESSAGE_ID_INVALID":        port.ErrNotFound,
		"CHAT_WRITE_FORBIDDEN":      port.ErrInvalid,
		"CHAT_ADMIN_REQUIRED":       port.ErrInvalid,
		"USER_IS_BLOCKED":           port.ErrInvalid,
		"MESSAGE_EDIT_TIME_EXPIRED": port.ErrInvalid,
		"MESSAGE_TOO_LONG":          port.ErrInvalid,
		"MEDIA_INVALID":             port.ErrInvalid,
		"POLL_ANSWERS_INVALID":      port.ErrInvalid,
		"REACTION_INVALID":          port.ErrInvalid,
		"STORY_SEND_FLOOD_WEEKLY":   port.ErrRateLimited,
	}
	for code, want := range cases {
		got := a.mapErr("hacer algo", tgerr.New(400, code))
		assert.ErrorIs(t, got, want, code)
		assert.True(t, strings.HasPrefix(got.Error(), "telegram: "), code)
	}

	// Sesión perdida: ErrNotConnected y marca para cerrar sesión.
	lost := a.mapErr("enviar", tgerr.New(401, "AUTH_KEY_UNREGISTERED"))
	assert.ErrorIs(t, lost, port.ErrNotConnected)
	assert.True(t, a.authLost.Load())

	// Desconocido: se envuelve con la acción.
	other := a.mapErr("enviar el mensaje", fmt.Errorf("algo raro"))
	assert.Contains(t, other.Error(), "enviar el mensaje")
	assert.False(t, isAuthLost(fmt.Errorf("algo raro")))
	assert.False(t, isAuthLost(nil))
}

func TestRedactDigits(t *testing.T) {
	assert.Equal(t, "sin números", redactDigits("sin números"))
	assert.Equal(t, "tel …4567 ok", redactDigits("tel 593991234567 ok"))
	assert.Equal(t, "…4567", redactDigits("+593991234567")[1:]) // el + queda
	assert.Equal(t, "código 12345 corto", redactDigits("código 12345 corto"))
	assert.Equal(t, "…0143", redactDigits("12025550143"))
	assert.Equal(t, "", redactErr(nil))
	assert.Contains(t, redactErr(fmt.Errorf("falló +593991234567")), "…4567")
	assert.NotContains(t, redactErr(fmt.Errorf("falló +593991234567")), "593991234567")
}

func TestHandleRunEnd(t *testing.T) {
	a, sink := newTestAdapter(t, map[string]string{"api_id": "1", "api_hash": "h"})
	require.NoError(t, a.sess.StoreSession(bg, []byte("sesion")))
	a.handleRunEnd(fmt.Errorf("sin red"))
	assert.Equal(t, []model.ConnState{model.StateError}, sink.states())
	assert.True(t, a.sess.hasSession(bg), "un error de red no borra la sesión")

	sink.reset()
	a.authLost.Store(true)
	a.handleRunEnd(fmt.Errorf("sesión revocada"))
	assert.Equal(t, []model.ConnState{model.StateLoggedOut}, sink.states())
	assert.False(t, a.sess.hasSession(bg), "una sesión perdida se borra")
}

func TestStatusDedup(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.setStatus(model.AccountStatus{State: model.StateConnecting})
	a.setStatus(model.AccountStatus{State: model.StateConnecting})
	a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedPhone})
	a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedPhone})
	a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedPhone, Detail: "otro"})
	assert.Len(t, sink.ofType(model.EvAccountStatus), 3)
}

func TestGotdLoggerOnlyWarnings(t *testing.T) {
	var sb strings.Builder
	log := slog.New(slog.NewTextHandler(&sb, &slog.HandlerOptions{Level: slog.LevelDebug}))
	zl := gotdLogger(log)
	require.NotNil(t, zl)
	h := minLevelHandler{h: log.Handler(), min: slog.LevelWarn}
	assert.False(t, h.Enabled(bg, slog.LevelInfo))
	assert.True(t, h.Enabled(bg, slog.LevelWarn))
}
