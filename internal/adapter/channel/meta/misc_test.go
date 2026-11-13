package meta

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func newDBWithMessages(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "account.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE messages (
  chat_native_id TEXT NOT NULL, native_id TEXT NOT NULL, sender_native_id TEXT NOT NULL DEFAULT '',
  sender_name TEXT NOT NULL DEFAULT '', from_me INTEGER NOT NULL, ts INTEGER NOT NULL, kind TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '', delivery TEXT NOT NULL DEFAULT 'sent', PRIMARY KEY (chat_native_id, native_id))`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE reactions (chat_native_id TEXT, message_native_id TEXT, sender_native_id TEXT, sender_name TEXT, emoji TEXT, at INTEGER)`)
	require.NoError(t, err)
	return db
}

func TestLocalLookups(t *testing.T) {
	ctx := context.Background()
	db := newDBWithMessages(t)
	ins := func(chat, id string, fromMe int, ts int64, kind, text, delivery string) {
		_, err := db.Exec(`INSERT INTO messages(chat_native_id, native_id, sender_name, from_me, ts, kind, text, delivery) VALUES(?,?,'Ana',?,?,?,?,?)`,
			chat, id, fromMe, ts, kind, text, delivery)
		require.NoError(t, err)
	}
	ins("200", "m1", 1, 100, "text", "uno", "sent")
	ins("200", "m2", 1, 200, "text", "dos", "read")
	ins("200", "m3", 0, 300, "image", "", "sent")
	ins("200", "m4", 1, 400, "text", "cuatro", "delivered")
	_, err := db.Exec(`INSERT INTO reactions(chat_native_id, message_native_id, sender_native_id, emoji, at) VALUES('200','m3','100','👍',1)`)
	require.NoError(t, err)

	l := NewLocal(db)
	assert.Equal(t, "200", l.ChatOfMessage(ctx, "m3"))
	assert.Equal(t, "", l.ChatOfMessage(ctx, "nada"))
	l.Remember("999", "mem")
	assert.Equal(t, "999", l.ChatOfMessage(ctx, "mem"), "la memoria gana a la base")

	assert.Equal(t, []string{"m1"}, l.OutgoingUpTo(ctx, "200", 250), "m2 ya está leído")
	assert.Equal(t, []string{"m4", "m1"}, l.OutgoingUpTo(ctx, "200", 500))

	id, ts := l.Oldest(ctx, "200")
	assert.Equal(t, "m1", id)
	assert.Equal(t, int64(100), ts)
	id, ts = l.Newest(ctx, "200")
	assert.Equal(t, "m4", id)
	assert.Equal(t, int64(400), ts)
	id, _ = l.Oldest(ctx, "vacío")
	assert.Equal(t, "", id)

	prev, sender := l.Quote(ctx, "200", "m3")
	assert.Equal(t, "📷 Foto", prev)
	assert.Equal(t, "Ana", sender)
	prev, _ = l.Quote(ctx, "200", "no")
	assert.Equal(t, "", prev)
	assert.Equal(t, "👍", l.MyReaction(ctx, "200", "m3", "100"))
	assert.Equal(t, "", l.MyReaction(ctx, "200", "m3", "5"))

	// Sin base o sin tabla: todo vacío, sin pánico.
	nl := NewLocal(nil)
	assert.Equal(t, "", nl.ChatOfMessage(ctx, "x"))
	assert.Nil(t, nl.OutgoingUpTo(ctx, "1", 1))
	empty, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "e.db"))
	require.NoError(t, err)
	defer empty.Close()
	el := NewLocal(empty)
	assert.Equal(t, "", el.ChatOfMessage(ctx, "x"))
	assert.Nil(t, el.OutgoingUpTo(ctx, "1", 1))
}

func TestLocalRememberEvicts(t *testing.T) {
	l := NewLocal(nil)
	for i := 0; i < msgChatCap+500; i++ {
		l.Remember("c", fmt.Sprintf("m%d", i))
	}
	l.mu.Lock()
	n := len(l.msgChat)
	l.mu.Unlock()
	assert.LessOrEqual(t, n, msgChatCap)
	assert.Equal(t, "c", l.ChatOfMessage(context.Background(), fmt.Sprintf("m%d", msgChatCap+499)), "lo más reciente se conserva")
}

func TestTypingTimers(t *testing.T) {
	tt := NewTypingTimers()
	var fired atomic.Int32
	tt.Touch("a", true, 30*time.Millisecond, func() { fired.Add(1) })
	tt.Touch("a", true, 30*time.Millisecond, func() { fired.Add(1) }) // reinicia: solo vence uno
	time.Sleep(120 * time.Millisecond)
	assert.Equal(t, int32(1), fired.Load())

	tt.Touch("b", true, 30*time.Millisecond, func() { fired.Add(10) })
	tt.Touch("b", false, 0, nil) // dejó de escribir: se cancela
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, int32(1), fired.Load())

	tt.Touch("c", true, 30*time.Millisecond, func() { fired.Add(100) })
	tt.StopAll()
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, int32(1), fired.Load())
}

func TestHandleAndMediaHelpers(t *testing.T) {
	b := MakeHandle(Handle{URL: "https://x/y", Expires: 10, Mime: "image/png"})
	h, err := ParseHandle(b)
	require.NoError(t, err)
	assert.Equal(t, "https://x/y", h.URL)
	assert.False(t, (Handle{}).Expired(99), "sin expiración conocida nunca caduca")

	for _, bad := range [][]byte{nil, []byte(""), []byte("{roto"), []byte("{}")} {
		_, err := ParseHandle(bad)
		assert.ErrorIs(t, err, port.ErrNotFound)
	}
	e, err := ParseHandle(MakeHandle(Handle{E2EE: []byte(`{"directPath":"/x"}`), Type: "WhatsApp Image Keys"}))
	require.NoError(t, err)
	assert.Equal(t, "WhatsApp Image Keys", e.Type)

	assert.Len(t, MediaID("c", "m", 0), 24)
	assert.NotEqual(t, MediaID("c", "m", 0), MediaID("c", "m", 1))
	assert.Equal(t, MediaID("c", "m", 0), MediaID("c", "m", 0))

	assert.Equal(t, model.KindImage, KindFromMime("image/png"))
	assert.Equal(t, model.KindVideo, KindFromMime("VIDEO/mp4"))
	assert.Equal(t, model.KindAudio, KindFromMime("audio/ogg"))
	assert.Equal(t, model.KindDocument, KindFromMime("application/pdf"))
	assert.Equal(t, "application/pdf", MimeFromName("a.PDF"))
	assert.Equal(t, "", MimeFromName("sin_extension"))
	assert.Equal(t, "imagen.jpg", FileNameFor(model.KindImage, "image/jpeg"))
	assert.Equal(t, "nota-de-voz.m4a", FileNameFor(model.KindVoice, "audio/mp4"))
	assert.Equal(t, "archivo", FileNameFor(model.KindDocument, ""))
}

func TestDownloader(t *testing.T) {
	var gotUA, gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotCookie = r.Header.Get("User-Agent"), r.Header.Get("Cookie")
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("contenido"))
		case "/403":
			w.WriteHeader(http.StatusForbidden)
		case "/410":
			w.WriteHeader(http.StatusGone)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	d := Downloader{UserAgent: "UA-prueba", Now: func() time.Time { return time.UnixMilli(1000) }}
	ctx := context.Background()

	rc, err := d.Open(ctx, Handle{URL: srv.URL + "/ok", Expires: 5000})
	require.NoError(t, err)
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	assert.Equal(t, "contenido", string(data))
	assert.Equal(t, "UA-prueba", gotUA)
	assert.Empty(t, gotCookie, "las cookies de la cuenta no se mandan al CDN")

	_, err = d.Open(ctx, Handle{URL: srv.URL + "/ok", Expires: 999})
	assert.ErrorIs(t, err, ErrMediaGone, "caducó")
	assert.ErrorIs(t, err, port.ErrNotFound)
	assert.Contains(t, err.Error(), "El archivo ya no está disponible")
	_, err = d.Open(ctx, Handle{URL: srv.URL + "/403"})
	assert.ErrorIs(t, err, ErrMediaGone)
	_, err = d.Open(ctx, Handle{URL: srv.URL + "/410"})
	assert.ErrorIs(t, err, ErrMediaGone)
	_, err = d.Open(ctx, Handle{URL: srv.URL + "/500"})
	assert.ErrorIs(t, err, port.ErrNotConnected)
	_, err = d.Open(ctx, Handle{})
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestReadOutgoing(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "a.png")
	require.NoError(t, os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n0000"), 0o644))
	caps := capability.Set{MaxFileSize: 100, MaxImageSize: 5, MaxVideoSize: 1000}

	_, err := ReadOutgoing(model.Outgoing{Kind: model.KindImage}, caps)
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, err = ReadOutgoing(model.Outgoing{Kind: model.KindImage, FilePath: filepath.Join(dir, "no.png")}, caps)
	assert.ErrorIs(t, err, port.ErrNotFound)
	_, err = ReadOutgoing(model.Outgoing{Kind: model.KindImage, FilePath: png}, caps)
	assert.ErrorIs(t, err, port.ErrInvalid, "más grande que MaxImageSize")
	f, err := ReadOutgoing(model.Outgoing{Kind: model.KindDocument, FilePath: png, FileName: "mi foto.png"}, caps)
	require.NoError(t, err)
	assert.Equal(t, "mi foto.png", f.Name)
	assert.Equal(t, "image/png", f.Mime)
	assert.Equal(t, int64(12), f.Size)
	f, err = ReadOutgoing(model.Outgoing{Kind: model.KindDocument, FilePath: png, Mime: "application/x-test"}, caps)
	require.NoError(t, err)
	assert.Equal(t, "application/x-test", f.Mime)
	assert.Equal(t, "a.png", f.Name)
	empty := filepath.Join(dir, "vacio.bin")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	_, err = ReadOutgoing(model.Outgoing{Kind: model.KindDocument, FilePath: empty}, caps)
	assert.ErrorIs(t, err, port.ErrInvalid)
}

func TestRedact(t *testing.T) {
	assert.Equal(t, "usuario …3456 y 12", Redact("usuario 1234563456 y 12"))
	assert.Equal(t, "…7890", ShortID("1234567890"))
	assert.Equal(t, "abc", ShortID("abc"))
	assert.Equal(t, "", RedactErr(nil))
	assert.Equal(t, "falló …1111", RedactErr(errors.New("falló 5551111")))
	assert.Equal(t, "", NativeUser(0))
	assert.Equal(t, "42", NativeUser(42))
	_, err := ParseID("abc")
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, err = ParseID("0")
	assert.ErrorIs(t, err, port.ErrInvalid)
	n, err := ParseID(" 77 ")
	require.NoError(t, err)
	assert.Equal(t, int64(77), n)
}

func TestClassifyAuthError(t *testing.T) {
	cases := []struct {
		err  error
		want AuthProblem
	}{
		{httpclient.ErrTokenInvalidated, ProblemLoggedOut},
		{fmt.Errorf("envuelto: %w", httpclient.ErrUserIDIsZero), ProblemLoggedOut},
		{&types.ErrorResponse{ErrorCode: 1357053}, ProblemLoggedOut},
		{httpclient.ErrCheckpointRequired, ProblemVerify},
		{httpclient.ErrChallengeRequired, ProblemVerify},
		{httpclient.ErrConsentRequired, ProblemVerify},
		{httpclient.ErrAccountSuspended, ProblemSuspended},
		{errors.New("sin red"), ProblemTransient},
		{&types.ErrorResponse{ErrorCode: 1}, ProblemTransient},
	}
	for _, c := range cases {
		got, detail := ClassifyAuthError("Messenger", "messenger.com", c.err)
		assert.Equal(t, c.want, got, c.err.Error())
		assert.Contains(t, detail, "Messenger")
	}
	p, d := ClassifyAuthError("Instagram", "instagram.com", nil)
	assert.Equal(t, ProblemTransient, p)
	assert.Equal(t, "", d)
}

func TestZerologBridgeKeepsOnlyWarnAndRedacts(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	zl := ZerologToSlog(log)
	zl.Debug().Str("texto", "MENSAJE SECRETO").Msg("depuración con cuerpo")
	zl.Info().Str("texto", "MENSAJE SECRETO").Msg("info con cuerpo")
	zl.Warn().Str("payload", "COOKIE SECRETA").Err(errors.New("falló el usuario 100012345678")).Msg("aviso del usuario 100012345678")
	out := buf.String()
	assert.NotContains(t, out, "SECRET")
	assert.NotContains(t, out, "depuración")
	assert.NotContains(t, out, "info con")
	assert.NotContains(t, out, "100012345678")
	assert.Contains(t, out, "…5678")
	assert.Contains(t, out, "aviso del usuario")
}

func TestAvatarsDownloadOnceAndReplace(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpegdata"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	av := NewAvatars(dir, Downloader{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 4)
	go av.Run(ctx, func(id, file string) { done <- id + "=" + file })

	wait := func(msg string) string {
		select {
		case v := <-done:
			return v
		case <-time.After(5 * time.Second):
			t.Fatal(msg)
			return ""
		}
	}
	av.Enqueue("200", srv.URL+"/foto.jpg?sig=1")
	av.Enqueue("200", srv.URL+"/foto.jpg?sig=1") // repetido: no se encola
	first := wait("la foto no se bajó")
	assert.Contains(t, first, "200="+AvatarPrefix("200"))
	assert.Equal(t, int32(1), hits.Load())
	matches, _ := filepath.Glob(filepath.Join(dir, AvatarPrefix("200")+"*"))
	assert.Len(t, matches, 1)

	// Tras reiniciar la cola, la misma foto ya está en disco: no se vuelve a bajar.
	av.Reset()
	av.Enqueue("200", srv.URL+"/foto.jpg?sig=2")
	wait("no avisó de la foto existente")
	assert.Equal(t, int32(1), hits.Load(), "la URL firmada cambió pero es la misma foto")

	// Otra foto del mismo id reemplaza a la anterior.
	av.Enqueue("200", srv.URL+"/otra.jpg")
	wait("la foto nueva no se bajó")
	assert.Equal(t, int32(2), hits.Load())
	matches, _ = filepath.Glob(filepath.Join(dir, AvatarPrefix("200")+"*"))
	assert.Len(t, matches, 1, "la versión anterior se borra")
}
