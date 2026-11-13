package whatsapp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/gcmutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// fakeMediaClient: doble del cliente de WhatsApp para la descarga con reintento.
type fakeMediaClient struct {
	mu        sync.Mutex
	paths     []string // rutas con que se pidió descargar, en orden
	receipts  []types.MessageInfo
	keys      [][]byte
	download  func(path string) ([]byte, error)
	onReceipt func(info *types.MessageInfo) error
}

func (f *fakeMediaClient) Download(_ context.Context, msg whatsmeow.DownloadableMessage) ([]byte, error) {
	f.mu.Lock()
	f.paths = append(f.paths, msg.GetDirectPath())
	f.mu.Unlock()
	return f.download(msg.GetDirectPath())
}

func (f *fakeMediaClient) SendMediaRetryReceipt(_ context.Context, info *types.MessageInfo, key []byte) error {
	f.mu.Lock()
	f.receipts = append(f.receipts, *info)
	f.keys = append(f.keys, key)
	f.mu.Unlock()
	if f.onReceipt != nil {
		return f.onReceipt(info)
	}
	return nil
}

func (f *fakeMediaClient) receiptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.receipts)
}

func (f *fakeMediaClient) downloaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func expiredClient() *fakeMediaClient {
	return &fakeMediaClient{download: func(path string) ([]byte, error) {
		if path == "/new" {
			return []byte("CONTENIDO"), nil
		}
		return nil, whatsmeow.ErrMediaDownloadFailedWith403
	}}
}

func imageSub() *waE2E.ImageMessage {
	return &waE2E.ImageMessage{
		DirectPath: proto.String("/old"), MediaKey: []byte("clave-de-medio"),
		FileSHA256: []byte("sha"), FileEncSHA256: []byte("enc"), Mimetype: proto.String("image/jpeg"),
	}
}

func notif(result waMmsRetry.MediaRetryNotification_ResultType, path string) *waMmsRetry.MediaRetryNotification {
	n := &waMmsRetry.MediaRetryNotification{Result: result.Enum()}
	if path != "" {
		n.DirectPath = proto.String(path)
	}
	return n
}

// retryAdapter arma un adaptador de prueba con el reintento apuntando a `info` y a la respuesta dada.
func retryAdapter(t *testing.T, result *waMmsRetry.MediaRetryNotification, decErr error) *Adapter {
	t.Helper()
	a, _ := newTestAdapter(t)
	a.retry.timeout = 2 * time.Second
	a.retry.decrypt = func(*events.MediaRetry, []byte) (*waMmsRetry.MediaRetryNotification, error) { return result, decErr }
	a.retry.lookup = func(_ context.Context, id string) (types.MessageInfo, error) {
		return types.MessageInfo{
			MessageSource: types.MessageSource{Chat: jPeer, Sender: jPeer},
			ID:            "MSG-" + id,
		}, nil
	}
	return a
}

func readAll(t *testing.T, rc io.ReadCloser) string {
	t.Helper()
	defer rc.Close()
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	return string(b)
}

func TestFetchMediaRetrySucceeds(t *testing.T) {
	a := retryAdapter(t, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"), nil)
	cli := expiredClient()
	// La respuesta llega por el despachador de eventos de verdad (a.handle).
	cli.onReceipt = func(info *types.MessageInfo) error {
		go a.handle(&events.MediaRetry{MessageID: info.ID, ChatID: info.Chat})
		return nil
	}
	rc, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "m1"}, imageSub())
	require.NoError(t, err)
	assert.Equal(t, "CONTENIDO", readAll(t, rc))
	assert.Equal(t, []string{"/old", "/new"}, cli.downloaded(), "una descarga fallida y una con la ruta nueva")
	require.Equal(t, 1, cli.receiptCount())
	assert.Equal(t, "MSG-m1", cli.receipts[0].ID)
	assert.Equal(t, jPeer, cli.receipts[0].Chat)
	assert.Equal(t, []byte("clave-de-medio"), cli.keys[0])
	assert.Empty(t, a.retry.waits, "la espera se limpia")
}

func TestFetchMediaRetryOnlyForExpiredErrors(t *testing.T) {
	for name, status := range map[string]error{
		"403": whatsmeow.ErrMediaDownloadFailedWith403,
		"404": whatsmeow.ErrMediaDownloadFailedWith404,
		"410": whatsmeow.ErrMediaDownloadFailedWith410,
	} {
		a := retryAdapter(t, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"), nil)
		cli := &fakeMediaClient{download: func(path string) ([]byte, error) {
			if path == "/new" {
				return []byte("ok"), nil
			}
			return nil, status
		}}
		cli.onReceipt = func(info *types.MessageInfo) error {
			go a.retry.deliver(&events.MediaRetry{MessageID: info.ID})
			return nil
		}
		rc, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "m"}, imageSub())
		require.NoError(t, err, name)
		assert.Equal(t, "ok", readAll(t, rc), name)
	}

	// Otros errores HTTP o de red no piden nada al teléfono.
	a := retryAdapter(t, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"), nil)
	cli := &fakeMediaClient{download: func(string) ([]byte, error) {
		return nil, whatsmeow.DownloadHTTPError{Response: &http.Response{StatusCode: 500}}
	}}
	_, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "m"}, imageSub())
	require.Error(t, err)
	assert.Equal(t, 0, cli.receiptCount())
	assert.NotErrorIs(t, err, port.ErrNotFound)
}

func TestFetchMediaRetryPhoneSaysGone(t *testing.T) {
	cases := map[string]struct {
		n   *waMmsRetry.MediaRetryNotification
		err error
	}{
		"NOT_FOUND":        {notif(waMmsRetry.MediaRetryNotification_NOT_FOUND, ""), nil},
		"DECRYPTION_ERROR": {notif(waMmsRetry.MediaRetryNotification_DECRYPTION_ERROR, ""), nil},
		"GENERAL_ERROR":    {notif(waMmsRetry.MediaRetryNotification_GENERAL_ERROR, ""), nil},
		"éxito sin ruta":   {notif(waMmsRetry.MediaRetryNotification_SUCCESS, ""), nil},
		"error sin cifrar": {nil, whatsmeow.ErrMediaNotAvailableOnPhone},
		"no descifra":      {nil, errors.New("no descifra")},
	}
	for name, tc := range cases {
		a := retryAdapter(t, tc.n, tc.err)
		cli := expiredClient()
		cli.onReceipt = func(info *types.MessageInfo) error {
			go a.retry.deliver(&events.MediaRetry{MessageID: info.ID})
			return nil
		}
		_, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "m"}, imageSub())
		require.ErrorIs(t, err, port.ErrNotFound, name)
		assert.Contains(t, err.Error(), "el archivo ya no está disponible en tu teléfono", name)
		assert.Equal(t, []string{"/old"}, cli.downloaded(), name+": no se descarga de nuevo")
	}
}

func TestFetchMediaRetryTimeout(t *testing.T) {
	a := retryAdapter(t, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"), nil)
	a.retry.timeout = 40 * time.Millisecond
	cli := expiredClient() // el teléfono nunca responde
	start := time.Now()
	_, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "m"}, imageSub())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotErrorIs(t, err, port.ErrNotFound)
	assert.Contains(t, err.Error(), "el teléfono no respondió; ábrelo con internet y vuelve a intentarlo")
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.Equal(t, 1, cli.receiptCount())
	assert.Empty(t, a.retry.waits, "la espera se limpia aunque venza")

	// Una respuesta tardía no rompe nada y se descarta.
	assert.False(t, a.retry.deliver(&events.MediaRetry{MessageID: "MSG-m"}))
}

func TestFetchMediaRetryContextCancelled(t *testing.T) {
	a := retryAdapter(t, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"), nil)
	cli := expiredClient()
	ctx, cancel := context.WithCancel(context.Background())
	cli.onReceipt = func(*types.MessageInfo) error { cancel(); return nil }
	_, err := a.fetchMedia(ctx, cli, model.Media{ID: "m"}, imageSub())
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, a.retry.waits)
}

func TestFetchMediaExpiredWithoutHandleOrMessage(t *testing.T) {
	// Sin MediaKey no hay cómo pedirlo al teléfono.
	a := retryAdapter(t, nil, nil)
	cli := expiredClient()
	sub := imageSub()
	sub.MediaKey = nil
	_, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "m"}, sub)
	require.ErrorIs(t, err, port.ErrNotFound)
	assert.Contains(t, err.Error(), "ya no está disponible")
	assert.Equal(t, 0, cli.receiptCount())

	// Sin id de medio tampoco.
	_, err = a.fetchMedia(context.Background(), cli, model.Media{}, imageSub())
	assert.ErrorIs(t, err, port.ErrNotFound)

	// El medio no tiene mensaje en la base: no se sabe de qué chat pedirlo.
	a.retry.lookup = nil
	setupMediaTables(t, a)
	_, err = a.fetchMedia(context.Background(), cli, model.Media{ID: "no-existe"}, imageSub())
	require.ErrorIs(t, err, port.ErrNotFound)
	assert.Equal(t, 0, cli.receiptCount())

	// Y por la puerta pública: sin handle, nada que descargar.
	_, err = a.DownloadMedia(context.Background(), model.Media{ID: "m"})
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestFetchMediaRetryOnlyOnce(t *testing.T) {
	// La ruta nueva también falla: no se vuelve a pedir.
	a := retryAdapter(t, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/nueva-rota"), nil)
	cli := expiredClient()
	cli.onReceipt = func(info *types.MessageInfo) error {
		go a.retry.deliver(&events.MediaRetry{MessageID: info.ID})
		return nil
	}
	_, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "m"}, imageSub())
	require.ErrorIs(t, err, port.ErrNotFound)
	assert.Equal(t, 1, cli.receiptCount())
	assert.Equal(t, []string{"/old", "/nueva-rota"}, cli.downloaded())
}

func TestFetchMediaRetrySharedWait(t *testing.T) {
	a := retryAdapter(t, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"), nil)
	cli := expiredClient()
	release := make(chan struct{})
	cli.onReceipt = func(info *types.MessageInfo) error {
		go func() {
			<-release
			a.retry.deliver(&events.MediaRetry{MessageID: info.ID})
		}()
		return nil
	}
	var wg sync.WaitGroup
	results := make([]string, 3)
	errs := make([]error, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rc, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "same"}, imageSub())
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = readAll(t, rc)
		}()
	}
	// Espera a que los tres estén colgados de la misma espera.
	require.Eventually(t, func() bool { return cli.receiptCount() >= 1 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	close(release)
	wg.Wait()
	for i := range results {
		require.NoError(t, errs[i])
		assert.Equal(t, "CONTENIDO", results[i])
	}
	assert.Equal(t, 1, cli.receiptCount(), "los tres comparten un solo pedido al teléfono")
}

func TestMediaRetryAtMostThreeAtOnce(t *testing.T) {
	a := retryAdapter(t, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"), nil)
	cli := expiredClient()
	var inFlight, peak atomic.Int32
	release := make(chan struct{})
	cli.onReceipt = func(info *types.MessageInfo) error {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		go func() {
			<-release
			inFlight.Add(-1)
			a.retry.deliver(&events.MediaRetry{MessageID: info.ID})
		}()
		return nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 7; i++ {
		id := string(rune('a' + i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			rc, err := a.fetchMedia(context.Background(), cli, model.Media{ID: id}, imageSub())
			if assert.NoError(t, err) {
				_ = readAll(t, rc)
			}
		}()
	}
	require.Eventually(t, func() bool { return inFlight.Load() == mediaRetryMax }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(60 * time.Millisecond) // da tiempo a que se cuele un cuarto, si pudiera
	assert.EqualValues(t, mediaRetryMax, inFlight.Load())
	close(release)
	wg.Wait()
	assert.LessOrEqual(t, int(peak.Load()), mediaRetryMax)
	assert.Equal(t, 7, cli.receiptCount())
}

func TestMediaRetryDeliverWithoutWaiter(t *testing.T) {
	r := newMediaRetrier()
	assert.False(t, r.deliver(&events.MediaRetry{MessageID: "nadie"}))
	assert.False(t, r.deliver(nil))
	var nilRetrier *mediaRetrier
	assert.False(t, nilRetrier.deliver(&events.MediaRetry{}))
}

// encryptedRetry cifra una respuesta del teléfono como lo hace WhatsApp (clave HKDF del mediaKey,
// AES-GCM con el id del mensaje como dato adicional).
func encryptedRetry(t *testing.T, msgID string, mediaKey []byte, n *waMmsRetry.MediaRetryNotification) *events.MediaRetry {
	t.Helper()
	plain, err := proto.Marshal(n)
	require.NoError(t, err)
	key := hkdfutil.SHA256(mediaKey, nil, []byte("WhatsApp Media Retry Notification"), 32)
	iv := []byte("123456789012")
	ct, err := gcmutil.Encrypt(key, iv, plain, []byte(msgID))
	require.NoError(t, err)
	return &events.MediaRetry{MessageID: msgID, Ciphertext: ct, IV: iv}
}

// Con el descifrado real de whatsmeow: respuesta cifrada válida, clave equivocada y error sin cifrar.
func TestFetchMediaRetryRealDecryption(t *testing.T) {
	sub := imageSub()
	//lint:ignore ST1008 doble de prueba: el orden de retorno es el de la prueba
	run := func(evt func(info *types.MessageInfo) *events.MediaRetry) (*fakeMediaClient, error, string) {
		a, _ := newTestAdapter(t) // retry y decrypt reales
		a.retry.timeout = 2 * time.Second
		a.retry.lookup = func(_ context.Context, id string) (types.MessageInfo, error) {
			return types.MessageInfo{MessageSource: types.MessageSource{Chat: jPeer, Sender: jPeer}, ID: "REAL-1"}, nil
		}
		cli := expiredClient()
		cli.onReceipt = func(info *types.MessageInfo) error {
			go a.handle(evt(info))
			return nil
		}
		rc, err := a.fetchMedia(context.Background(), cli, model.Media{ID: "m"}, sub)
		if err != nil {
			return cli, err, ""
		}
		return cli, nil, readAll(t, rc)
	}

	cli, err, body := run(func(info *types.MessageInfo) *events.MediaRetry {
		return encryptedRetry(t, info.ID, sub.MediaKey, notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"))
	})
	require.NoError(t, err)
	assert.Equal(t, "CONTENIDO", body)
	assert.Equal(t, []string{"/old", "/new"}, cli.downloaded())

	_, err, _ = run(func(info *types.MessageInfo) *events.MediaRetry {
		return encryptedRetry(t, info.ID, sub.MediaKey, notif(waMmsRetry.MediaRetryNotification_NOT_FOUND, ""))
	})
	require.ErrorIs(t, err, port.ErrNotFound)

	_, err, _ = run(func(info *types.MessageInfo) *events.MediaRetry {
		return encryptedRetry(t, info.ID, []byte("otra-clave"), notif(waMmsRetry.MediaRetryNotification_SUCCESS, "/new"))
	})
	require.ErrorIs(t, err, port.ErrNotFound, "una respuesta que no descifra se trata como archivo perdido")

	_, err, _ = run(func(info *types.MessageInfo) *events.MediaRetry {
		return &events.MediaRetry{MessageID: info.ID, Error: &events.MediaRetryError{Code: 2}}
	})
	require.ErrorIs(t, err, port.ErrNotFound)
	assert.Contains(t, err.Error(), "ya no está disponible en tu teléfono")
}

func TestWithDirectPath(t *testing.T) {
	subs := []whatsmeow.DownloadableMessage{
		&waE2E.ImageMessage{DirectPath: proto.String("/a")}, &waE2E.VideoMessage{DirectPath: proto.String("/a")},
		&waE2E.AudioMessage{DirectPath: proto.String("/a")}, &waE2E.DocumentMessage{DirectPath: proto.String("/a")},
		&waE2E.StickerMessage{DirectPath: proto.String("/a")},
	}
	for _, s := range subs {
		fresh, ok := withDirectPath(s, "/b")
		require.True(t, ok)
		assert.Equal(t, "/b", fresh.GetDirectPath())
		assert.Equal(t, "/a", s.GetDirectPath(), "el original no cambia")
	}
	_, ok := withDirectPath(otherDownloadable{}, "/b")
	assert.False(t, ok)
}

func TestIsExpiredDownload(t *testing.T) {
	assert.True(t, isExpiredDownload(whatsmeow.ErrMediaDownloadFailedWith403))
	assert.True(t, isExpiredDownload(whatsmeow.ErrMediaDownloadFailedWith404))
	assert.True(t, isExpiredDownload(whatsmeow.ErrMediaDownloadFailedWith410))
	assert.True(t, isExpiredDownload(whatsmeow.ErrNoURLPresent))
	assert.True(t, isExpiredDownload(whatsmeow.DownloadHTTPError{Response: &http.Response{StatusCode: 403}}))
	assert.False(t, isExpiredDownload(whatsmeow.DownloadHTTPError{Response: &http.Response{StatusCode: 500}}))
	assert.False(t, isExpiredDownload(errors.New("red caída")))
	assert.False(t, isExpiredDownload(nil))
}

// setupMediaTables crea las tablas media y messages de 05 (solo las columnas que se leen).
func setupMediaTables(t *testing.T, a *Adapter) {
	t.Helper()
	_, err := a.deps.DB.Exec(`
CREATE TABLE IF NOT EXISTS messages (
  chat_native_id TEXT NOT NULL, native_id TEXT NOT NULL, sender_native_id TEXT NOT NULL DEFAULT '',
  from_me INTEGER NOT NULL, ts INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (chat_native_id, native_id));
CREATE TABLE IF NOT EXISTS media (
  id TEXT PRIMARY KEY, chat_native_id TEXT NOT NULL, message_native_id TEXT NOT NULL);`)
	require.NoError(t, err)
}

func TestLookupMediaInfo(t *testing.T) {
	a, _ := newTestAdapter(t)
	setupMediaTables(t, a)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		_, err := a.deps.DB.Exec(q, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO messages(chat_native_id, native_id, sender_native_id, from_me) VALUES(?,?,?,?)`, pnPeer, "D1", pnPeer, 0)
	exec(`INSERT INTO media(id, chat_native_id, message_native_id) VALUES('md1', ?, 'D1')`, pnPeer)
	exec(`INSERT INTO messages(chat_native_id, native_id, sender_native_id, from_me) VALUES(?,?,?,?)`, pnPeer, "D2", jSelf.String(), 1)
	exec(`INSERT INTO media(id, chat_native_id, message_native_id) VALUES('md2', ?, 'D2')`, pnPeer)
	exec(`INSERT INTO messages(chat_native_id, native_id, sender_native_id, from_me) VALUES(?,?,?,?)`, groupID, "G1", jOther.String(), 0)
	exec(`INSERT INTO media(id, chat_native_id, message_native_id) VALUES('mg1', ?, 'G1')`, groupID)
	exec(`INSERT INTO media(id, chat_native_id, message_native_id) VALUES('huerfano', ?, 'NOHAY')`, groupID)

	info, err := a.lookupMediaInfo(ctx, "md1")
	require.NoError(t, err)
	assert.Equal(t, "D1", info.ID)
	assert.Equal(t, jPeer, info.Chat)
	assert.False(t, info.IsFromMe)
	assert.False(t, info.IsGroup)

	info, err = a.lookupMediaInfo(ctx, "md2")
	require.NoError(t, err)
	assert.True(t, info.IsFromMe)

	info, err = a.lookupMediaInfo(ctx, "mg1")
	require.NoError(t, err)
	assert.True(t, info.IsGroup)
	assert.Equal(t, jGroup, info.Chat)
	assert.Equal(t, jOther, info.Sender, "en grupos va el participante")

	// Un chat que llegó como @lid se pide con ese mismo @lid.
	a.noteWire(jPeerLID, jPeer)
	info, err = a.lookupMediaInfo(ctx, "md1")
	require.NoError(t, err)
	assert.Equal(t, jPeerLID, info.Chat)

	// Sin fila de medio, o sin su mensaje en un grupo: no se puede pedir.
	_, err = a.lookupMediaInfo(ctx, "nada")
	assert.ErrorIs(t, err, port.ErrNotFound)
	_, err = a.lookupMediaInfo(ctx, "huerfano")
	assert.ErrorIs(t, err, port.ErrNotFound)
}

// otherDownloadable: un tipo descargable que el adaptador no usa.
type otherDownloadable struct{}

func (otherDownloadable) GetDirectPath() string    { return "" }
func (otherDownloadable) GetMediaKey() []byte      { return nil }
func (otherDownloadable) GetFileSHA256() []byte    { return nil }
func (otherDownloadable) GetFileEncSHA256() []byte { return nil }
