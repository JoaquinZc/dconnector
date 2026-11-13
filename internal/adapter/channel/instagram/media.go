package instagram

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
)

// DownloadMedia baja un adjunto de la URL firmada del CDN de Instagram.
func (a *Adapter) DownloadMedia(ctx context.Context, m model.Media) (io.ReadCloser, error) {
	h, err := meta.ParseHandle(m.Handle)
	if err != nil {
		return nil, fmt.Errorf("instagram: %w", err)
	}
	rc, err := a.dl.Open(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("instagram: %w", err)
	}
	return rc, nil
}

// FetchAvatar devuelve el nombre del archivo de la foto ya bajada de una persona o grupo ("" si no hay).
// Las fotos se bajan solas en segundo plano (avatarsInit); esto solo consulta lo que ya está en AvatarDir.
func (a *Adapter) FetchAvatar(ctx context.Context, peerOrChatNativeID string) (string, error) {
	if a.deps.AvatarDir == "" || peerOrChatNativeID == "" {
		return "", nil
	}
	matches, _ := filepath.Glob(filepath.Join(a.deps.AvatarDir, meta.AvatarPrefix(peerOrChatNativeID)+"*"))
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

// onAvatar: hay un archivo nuevo; se emiten person.updated (si es una persona) y chat.updated con AvatarURL.
func (a *Adapter) onAvatar(id, file string) {
	a.conv.SetAvatarFile(id, file)
	if n, err := meta.ParseID(id); err == nil {
		p := a.conv.PersonSnapshot(n)
		if p.PushName != "" || p.Username != "" || p.NativeID == a.SelfID() {
			a.Emit(model.Event{Type: model.EvPerson, Person: &p})
		}
	}
	if ch, ok := a.conv.ChatSnapshot(id); ok {
		ch.UnreadCount = -1
		a.Emit(model.Event{Type: model.EvChatUpsert, Chat: &ch, ChatKey: ch.Key})
	}
}
