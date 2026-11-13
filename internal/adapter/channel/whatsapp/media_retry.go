package whatsapp

// Medios caducados: cuando la descarga falla con 403/404/410 (la URL del historial ya no sirve),
// se le pide al teléfono que vuelva a subir el archivo (SendMediaRetryReceipt) y se espera su
// respuesta (*events.MediaRetry), que trae la ruta nueva.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// mediaRetryTimeout: cuánto se espera la respuesta del teléfono.
	mediaRetryTimeout = 30 * time.Second
	// mediaRetryMax: pedidos simultáneos al teléfono por cuenta.
	mediaRetryMax = 3
)

// Textos de error (en español, para quien los lee en pantalla).
const (
	msgMediaGone     = "whatsapp: el archivo ya no está disponible en tu teléfono"
	msgMediaNoPhone  = "whatsapp: el teléfono no respondió; ábrelo con internet y vuelve a intentarlo"
	msgMediaExpired  = "whatsapp: el archivo ya no está disponible en WhatsApp"
	msgMediaNoSource = "whatsapp: el archivo caducó y no se puede pedir de nuevo al teléfono"
)

// mediaClient: lo mínimo del cliente de WhatsApp que usa la descarga (así se prueba con dobles).
type mediaClient interface {
	Download(ctx context.Context, msg whatsmeow.DownloadableMessage) ([]byte, error)
	SendMediaRetryReceipt(ctx context.Context, message *types.MessageInfo, mediaKey []byte) error
}

var _ mediaClient = (*whatsmeow.Client)(nil)

// retryDecrypt descifra la respuesta del teléfono (whatsmeow.DecryptMediaRetryNotification).
type retryDecrypt func(evt *events.MediaRetry, mediaKey []byte) (*waMmsRetry.MediaRetryNotification, error)

// retryWait: una espera en curso por el archivo de un mensaje. Quien la crea (el líder) envía el
// pedido y espera; los demás que piden el mismo mensaje solo esperan el resultado.
type retryWait struct {
	evt  chan *events.MediaRetry // la respuesta del teléfono (una sola)
	done chan struct{}           // se cierra cuando notif y err ya están fijados
	// notif y err se escriben antes de cerrar done.
	notif *waMmsRetry.MediaRetryNotification
	err   error
}

// mediaRetrier coordina los reintentos de una cuenta.
type mediaRetrier struct {
	timeout time.Duration
	decrypt retryDecrypt
	lookup  mediaInfoFn // nil = Adapter.lookupMediaInfo
	sem     chan struct{}

	mu    sync.Mutex
	waits map[string]*retryWait // por id de mensaje
}

func newMediaRetrier() *mediaRetrier {
	return &mediaRetrier{
		timeout: mediaRetryTimeout,
		decrypt: whatsmeow.DecryptMediaRetryNotification,
		sem:     make(chan struct{}, mediaRetryMax),
		waits:   map[string]*retryWait{},
	}
}

// deliver entrega la respuesta del teléfono a quien la espera. Respuestas que nadie espera
// (pedidas hace rato o por otra sesión) se descartan.
func (r *mediaRetrier) deliver(evt *events.MediaRetry) bool {
	if r == nil || evt == nil {
		return false
	}
	r.mu.Lock()
	w := r.waits[evt.MessageID]
	r.mu.Unlock()
	if w == nil {
		return false
	}
	select {
	case w.evt <- evt:
		return true
	default:
		return false // ya llegó una
	}
}

func (r *mediaRetrier) join(id string) (w *retryWait, leader bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w, ok := r.waits[id]; ok {
		return w, false
	}
	w = &retryWait{evt: make(chan *events.MediaRetry, 1), done: make(chan struct{})}
	r.waits[id] = w
	return w, true
}

func (r *mediaRetrier) finish(id string, w *retryWait, notif *waMmsRetry.MediaRetryNotification, err error) {
	r.mu.Lock()
	delete(r.waits, id)
	r.mu.Unlock()
	w.notif, w.err = notif, err
	close(w.done)
}

// request pide al teléfono que vuelva a subir el archivo del mensaje y devuelve la respuesta ya
// descifrada. send manda el pedido. Si ya hay un pedido en curso para ese mensaje, lo comparte.
func (r *mediaRetrier) request(ctx context.Context, msgID string, mediaKey []byte, send func(context.Context) error) (notif *waMmsRetry.MediaRetryNotification, err error) {
	w, leader := r.join(msgID)
	if !leader {
		select {
		case <-w.done:
			return w.notif, w.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer func() { r.finish(msgID, w, notif, err) }()

	// Como mucho mediaRetryMax pedidos a la vez, para no saturar el teléfono.
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := send(ctx); err != nil {
		return nil, err
	}
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	var evt *events.MediaRetry
	select {
	case evt = <-w.evt:
	case <-timer.C:
		return nil, fmt.Errorf("%s: %w", msgMediaNoPhone, context.DeadlineExceeded)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	n, derr := r.decrypt(evt, mediaKey)
	if derr != nil {
		// Sin respuesta cifrada (error 2: «ya no está en el teléfono») o clave que no descifra.
		return nil, fmt.Errorf("%s: %w", msgMediaGone, port.ErrNotFound)
	}
	// NOT_FOUND, DECRYPTION_ERROR y GENERAL_ERROR: el teléfono no pudo (o no quiso) volver a subirlo.
	if n.GetResult() != waMmsRetry.MediaRetryNotification_SUCCESS || n.GetDirectPath() == "" {
		return nil, fmt.Errorf("%s: %w", msgMediaGone, port.ErrNotFound)
	}
	return n, nil
}

// ---------------------------------------------------------------------------------------------
// Descarga con reintento

// isExpiredDownload: la descarga falló porque la URL del archivo ya no sirve (403, 404 o 410) o
// el mensaje no trae URL. En esos casos se puede pedir al teléfono que lo vuelva a subir.
func isExpiredDownload(err error) bool {
	return errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) ||
		errors.Is(err, whatsmeow.ErrNoURLPresent)
}

// withDirectPath copia el submensaje descargable con otra ruta de descarga.
func withDirectPath(sub whatsmeow.DownloadableMessage, path string) (whatsmeow.DownloadableMessage, bool) {
	switch s := sub.(type) {
	case *waE2E.ImageMessage:
		c := proto.Clone(s).(*waE2E.ImageMessage)
		c.DirectPath = proto.String(path)
		return c, true
	case *waE2E.VideoMessage:
		c := proto.Clone(s).(*waE2E.VideoMessage)
		c.DirectPath = proto.String(path)
		return c, true
	case *waE2E.AudioMessage:
		c := proto.Clone(s).(*waE2E.AudioMessage)
		c.DirectPath = proto.String(path)
		return c, true
	case *waE2E.DocumentMessage:
		c := proto.Clone(s).(*waE2E.DocumentMessage)
		c.DirectPath = proto.String(path)
		return c, true
	case *waE2E.StickerMessage:
		c := proto.Clone(s).(*waE2E.StickerMessage)
		c.DirectPath = proto.String(path)
		return c, true
	}
	return nil, false
}

// mediaInfoFn reconstruye el MessageInfo que pide SendMediaRetryReceipt a partir del id del medio.
type mediaInfoFn func(ctx context.Context, mediaID string) (types.MessageInfo, error)

// lookupMediaInfo lee de la base de la cuenta el chat y el mensaje de un medio (tablas media y
// messages de 05) y arma el MessageInfo. Es lo mismo que hace oldestMessage: no toca el almacén.
func (a *Adapter) lookupMediaInfo(ctx context.Context, mediaID string) (types.MessageInfo, error) {
	var chatID, msgID, senderID string
	var fromMe int
	err := a.deps.DB.QueryRowContext(ctx,
		`SELECT m.chat_native_id, m.message_native_id, COALESCE(s.sender_native_id, ''), COALESCE(s.from_me, 0)
		 FROM media m LEFT JOIN messages s ON s.chat_native_id = m.chat_native_id AND s.native_id = m.message_native_id
		 WHERE m.id = ?`, mediaID).Scan(&chatID, &msgID, &senderID, &fromMe)
	if errors.Is(err, sql.ErrNoRows) {
		return types.MessageInfo{}, fmt.Errorf("%s: %w", msgMediaNoSource, port.ErrNotFound)
	}
	if err != nil {
		return types.MessageInfo{}, fmt.Errorf("whatsapp: no se pudo leer el mensaje del archivo: %w", err)
	}
	chat, err := nativeToJID(chatID)
	if err != nil {
		return types.MessageInfo{}, fmt.Errorf("%s: %w", msgMediaNoSource, port.ErrNotFound)
	}
	info := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: a.wireJID(chat), IsFromMe: fromMe != 0, IsGroup: chat.Server == types.GroupServer},
		ID:            msgID,
	}
	if sender, serr := nativeToJID(senderID); serr == nil && senderID != "" {
		info.Sender = a.wireJID(sender)
	}
	if info.IsGroup && info.Sender.IsEmpty() {
		return types.MessageInfo{}, fmt.Errorf("%s: %w", msgMediaNoSource, port.ErrNotFound)
	}
	return info, nil
}

// fetchMedia descarga el archivo; si la URL caducó, pide al teléfono que lo vuelva a subir (una
// sola vez) y descarga con la ruta nueva.
func (a *Adapter) fetchMedia(ctx context.Context, cli mediaClient, m model.Media, sub whatsmeow.DownloadableMessage) (io.ReadCloser, error) {
	data, err := cli.Download(ctx, sub)
	if err == nil {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	if !isExpiredDownload(err) {
		if errors.Is(err, whatsmeow.ErrMediaNotAvailableOnPhone) {
			return nil, fmt.Errorf("%s: %w", msgMediaExpired, port.ErrNotFound)
		}
		return nil, a.mapErr("descargar el archivo", err)
	}
	key := sub.GetMediaKey()
	if len(key) == 0 || m.ID == "" {
		return nil, fmt.Errorf("%s: %w", msgMediaExpired, port.ErrNotFound)
	}
	rt := a.retry
	if rt == nil {
		return nil, fmt.Errorf("%s: %w", msgMediaExpired, port.ErrNotFound)
	}
	lookup := rt.lookup
	if lookup == nil {
		lookup = a.lookupMediaInfo
	}
	info, lerr := lookup(ctx, m.ID)
	if lerr != nil {
		return nil, lerr
	}
	a.log.Debug("archivo caducado: se le pide al teléfono que lo vuelva a subir", "mediaId", m.ID)
	notif, rerr := rt.request(ctx, info.ID, key, func(ctx context.Context) error {
		return cli.SendMediaRetryReceipt(ctx, &info, key)
	})
	if rerr != nil {
		if errors.Is(rerr, port.ErrNotFound) || errors.Is(rerr, context.DeadlineExceeded) || errors.Is(rerr, context.Canceled) {
			return nil, rerr
		}
		return nil, a.mapErr("pedir el archivo al teléfono", rerr)
	}
	fresh, ok := withDirectPath(sub, notif.GetDirectPath())
	if !ok {
		return nil, fmt.Errorf("%s: %w", msgMediaExpired, port.ErrNotFound)
	}
	data, err = cli.Download(ctx, fresh)
	if err != nil {
		if isExpiredDownload(err) || errors.Is(err, whatsmeow.ErrMediaNotAvailableOnPhone) {
			return nil, fmt.Errorf("%s: %w", msgMediaGone, port.ErrNotFound)
		}
		return nil, a.mapErr("descargar el archivo", err)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
