package telegram

// Medios y avatares (08 §8).

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func isTg(err error, codes ...string) bool { return tgerr.Is(err, codes...) }

// countingWriter cuenta lo escrito para saber si se puede reintentar sin duplicar datos.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// pipeReader cierra la descarga al cerrar la lectura.
type pipeReader struct {
	*io.PipeReader
	cancel context.CancelFunc
}

func (p pipeReader) Close() error {
	p.cancel()
	return p.PipeReader.Close()
}

// DownloadMedia descarga un medio con el dato opaco Media.Handle. Si la referencia del archivo
// caducó (FILE_REFERENCE_EXPIRED), vuelve a pedir el mensaje y reintenta una vez.
func (a *Adapter) DownloadMedia(ctx context.Context, m model.Media) (io.ReadCloser, error) {
	h, ok := parseHandle(m.Handle)
	if !ok {
		return nil, fmt.Errorf("telegram: este medio no se puede descargar: %w", port.ErrNotFound)
	}
	api, err := a.liveAPI()
	if err != nil {
		return nil, err
	}
	client := a.telegramClient()
	if client == nil {
		return nil, fmt.Errorf("telegram: %w", port.ErrNotConnected)
	}
	dctx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	go func() {
		defer a.recoverLoop("descarga")
		cw := &countingWriter{w: pw}
		_, derr := client.Download(h.location()).Stream(dctx, cw)
		if derr != nil && cw.n == 0 && (downloader.IsFileReferenceError(derr) || isTg(derr, "FILE_REFERENCE_EXPIRED", "FILE_REFERENCE_INVALID")) {
			if nh, rerr := a.refreshHandle(dctx, api, h); rerr == nil {
				_, derr = client.Download(nh.location()).Stream(dctx, cw)
			} else {
				derr = rerr
			}
		}
		if derr != nil {
			derr = a.mapErr("descargar el archivo", derr)
		}
		_ = pw.CloseWithError(derr)
	}()
	return pipeReader{PipeReader: pr, cancel: cancel}, nil
}

// refreshHandle vuelve a pedir el mensaje (o la historia) para obtener una referencia nueva.
func (a *Adapter) refreshHandle(ctx context.Context, api *tg.Client, h mediaHandle) (mediaHandle, error) {
	if h.C == "" {
		return mediaHandle{}, fmt.Errorf("telegram: no se puede renovar la referencia del archivo: %w", port.ErrNotFound)
	}
	c := a.conv(nil)
	var media *model.Media
	switch {
	case h.S != 0:
		peer, err := a.peers.input(ctx, h.C)
		if err != nil {
			return mediaHandle{}, err
		}
		res, err := api.StoriesGetStoriesByID(ctx, &tg.StoriesGetStoriesByIDRequest{Peer: peer, ID: []int{h.S}})
		if err != nil {
			return mediaHandle{}, a.mapErr("renovar la historia", err)
		}
		a.peers.feedLists(ctx, res.Users, res.Chats)
		for _, sc := range res.Stories {
			if it, ok := sc.(*tg.StoryItem); ok && it.ID == h.S {
				if pp, ok := peerFromNative(h.C); ok {
					media = c.storyOf(pp, it).Media
				}
			}
		}
	case h.M != 0:
		m, err := a.fetchMessage(ctx, api, h.C, h.M)
		if err != nil {
			return mediaHandle{}, err
		}
		media = c.message(m).Media
	}
	if media == nil {
		return mediaHandle{}, fmt.Errorf("telegram: el archivo ya no está disponible: %w", port.ErrNotFound)
	}
	nh, ok := parseHandle(media.Handle)
	if !ok {
		return mediaHandle{}, fmt.Errorf("telegram: el archivo ya no está disponible: %w", port.ErrNotFound)
	}
	return nh, nil
}

// ---------------------------------------------------------------------------------------------
// Avatares

func avatarPrefix(native string) string {
	sum := sha1.Sum([]byte(native))
	return hex.EncodeToString(sum[:])[:16]
}

// avatarName: nombre de archivo de un avatar, estable por par y foto.
func avatarName(native string, photoID int64) string {
	return avatarPrefix(native) + "-" + strconv.FormatInt(photoID, 10) + ".jpg"
}

// avatarURL: la dirección que sirve la interfaz.
func (a *Adapter) avatarURL(file string) string {
	if file == "" {
		return ""
	}
	return "/avatar/" + string(a.acc.ID) + "/" + file
}

// photoIDOf devuelve el id de la foto de perfil actual de un par (0 si no tiene).
func (a *Adapter) photoIDOf(native string) int64 {
	kind, id, ok := parseNative(native)
	if !ok {
		return 0
	}
	switch kind {
	case kindUser:
		if u, ok := a.peers.User(id); ok {
			if p, ok := u.Photo.(*tg.UserProfilePhoto); ok {
				return p.PhotoID
			}
		}
	case kindChat:
		if c, ok := a.peers.Chat(id); ok {
			if p, ok := c.Photo.(*tg.ChatPhoto); ok {
				return p.PhotoID
			}
		}
	case kindChannel:
		if c, ok := a.peers.Channel(id); ok {
			if p, ok := c.Photo.(*tg.ChatPhoto); ok {
				return p.PhotoID
			}
		}
	}
	return 0
}

// FetchAvatar guarda la foto de perfil en AvatarDir y devuelve el nombre del archivo ("" si no tiene).
func (a *Adapter) FetchAvatar(ctx context.Context, peerOrChatNativeID string) (string, error) {
	native := peerOrChatNativeID
	if _, _, ok := parseNative(native); !ok {
		return "", fmt.Errorf("telegram: identificador no válido: %w", port.ErrInvalid)
	}
	photo := a.photoIDOf(native)
	if photo == 0 {
		return "", nil
	}
	name := avatarName(native, photo)
	path := filepath.Join(a.deps.AvatarDir, name)
	if _, err := os.Stat(path); err == nil {
		return name, nil
	}
	client := a.telegramClient()
	if client == nil || !a.live.Load() {
		return "", fmt.Errorf("telegram: %w", port.ErrNotConnected)
	}
	peer, err := a.peers.input(ctx, native)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(a.deps.AvatarDir, 0o755); err != nil {
		return "", fmt.Errorf("telegram: no se pudo crear la carpeta de avatares: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("telegram: no se pudo guardar la foto de perfil: %w", err)
	}
	_, derr := client.Download(&tg.InputPeerPhotoFileLocation{Peer: peer, PhotoID: photo}).Stream(ctx, f)
	cerr := f.Close()
	if derr != nil || cerr != nil {
		_ = os.Remove(tmp)
		if derr != nil {
			return "", a.mapErr("descargar la foto de perfil", derr)
		}
		return "", fmt.Errorf("telegram: no se pudo guardar la foto de perfil: %w", cerr)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("telegram: no se pudo guardar la foto de perfil: %w", err)
	}
	// Quita las versiones anteriores del mismo par.
	old, _ := filepath.Glob(filepath.Join(a.deps.AvatarDir, avatarPrefix(native)+"-*.jpg"))
	for _, o := range old {
		if filepath.Base(o) != name {
			_ = os.Remove(o)
		}
	}
	return name, nil
}

// avatarQueue: cola de pares cuyo avatar se pide en segundo plano, uno a uno.
type avatarQueue struct {
	mu    sync.Mutex
	ch    chan string
	seen  map[string]bool
	files map[string]string // par → último archivo anunciado
}

func newAvatarQueue() *avatarQueue {
	return &avatarQueue{ch: make(chan string, 2048), seen: map[string]bool{}, files: map[string]string{}}
}

// resetSeen olvida los pares ya pedidos (cada conexión vuelve a comprobar las fotos).
func (q *avatarQueue) resetSeen() {
	q.mu.Lock()
	q.seen = map[string]bool{}
	q.mu.Unlock()
}

// queueAvatar pide el avatar de un par en segundo plano (una vez por conexión).
func (a *Adapter) queueAvatar(native string) {
	q := a.avatars
	q.mu.Lock()
	if q.seen[native] {
		q.mu.Unlock()
		return
	}
	q.seen[native] = true
	q.mu.Unlock()
	select {
	case q.ch <- native:
	default:
	}
}

// avatarWorker procesa la cola hasta que termine la conexión.
func (a *Adapter) avatarWorker(ctx context.Context) {
	defer a.recoverLoop("avatares")
	q := a.avatars
	for {
		select {
		case <-ctx.Done():
			return
		case native := <-q.ch:
			fctx, cancel := context.WithTimeout(ctx, opTimeout)
			file, err := a.FetchAvatar(fctx, native)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				a.log.Debug("no se pudo traer un avatar", "peer", shortID(native), "err", redactErr(err))
				continue
			}
			q.mu.Lock()
			same := q.files[native] == file
			q.files[native] = file
			q.mu.Unlock()
			if file == "" || same {
				continue
			}
			a.announceAvatar(native, file)
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
		}
	}
}

// announceAvatar emite chat.updated (y person.updated en directos) con el avatar.
func (a *Adapter) announceAvatar(native, file string) {
	url := a.avatarURL(file)
	if _, known := a.local.marksOf(native); known {
		if ch, ok := a.chatShell(native, a.peers); ok {
			ch.AvatarURL = url
			a.emit(model.Event{Type: model.EvChatUpsert, Chat: &ch})
		}
	}
	if kind, id, _ := parseNative(native); kind == kindUser {
		if u, ok := a.peers.User(id); ok {
			p := personOf(a.acc.ID, u)
			p.AvatarURL = url
			a.emit(model.Event{Type: model.EvPerson, Person: &p})
		}
	}
}
