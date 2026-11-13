package messenger

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
)

// DownloadMedia baja un adjunto: normal (URL firmada del CDN) o cifrado (E2EE, e2ee.go).
func (a *Adapter) DownloadMedia(ctx context.Context, m model.Media) (io.ReadCloser, error) {
	h, err := meta.ParseHandle(m.Handle)
	if err != nil {
		return nil, fmt.Errorf("messenger: %w", err)
	}
	if len(h.E2EE) > 0 {
		return a.e2eeDownload(ctx, h)
	}
	rc, err := a.dl.Open(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("messenger: %w", err)
	}
	return rc, nil
}

// FetchAvatar devuelve el nombre del archivo de la foto ya bajada de una persona o grupo ("" si no hay).
// Las fotos se bajan solas en segundo plano (avatarsInit); esto solo consulta lo que ya está en AvatarDir.
func (a *Adapter) FetchAvatar(ctx context.Context, peerOrChatNativeID string) (string, error) {
	if a.deps.AvatarDir == "" || peerOrChatNativeID == "" {
		return "", nil
	}
	prefix := meta.AvatarPrefix(peerOrChatNativeID)
	matches, _ := filepath.Glob(filepath.Join(a.deps.AvatarDir, prefix+"*"))
	for _, f := range matches {
		if !strings.HasSuffix(f, ".part") {
			return filepath.Base(f), nil
		}
	}
	if u := a.conv.AvatarURL(peerOrChatNativeID); u != "" {
		a.avatars.Enqueue(peerOrChatNativeID, u)
	}
	return "", nil
}

// avatarsInit arranca la cola de fotos de esta conexión y avisa de cada foto nueva.
func (a *Adapter) avatarsInit(ctx context.Context) {
	a.avatars.Reset()
	go func() {
		defer a.RecoverLoop("fotos")
		a.avatars.Run(ctx, a.onAvatar)
	}()
}

// onAvatar: hay un archivo nuevo; se emiten person.updated y chat.updated con AvatarURL.
func (a *Adapter) onAvatar(id, file string) {
	a.conv.SetAvatarFile(id, file)
	n, err := meta.ParseID(id)
	if err != nil {
		return
	}
	p := a.conv.PersonSnapshot(n)
	a.Emit(model.Event{Type: model.EvPerson, Person: &p})
	if ch, ok := a.conv.ChatSnapshot(n); ok {
		ch.UnreadCount = -1
		a.Emit(model.Event{Type: model.EvChatUpsert, Chat: &ch, ChatKey: ch.Key})
	}
}
