package meta

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxAvatarBytes: tope de una foto de perfil (5 MB).
const maxAvatarBytes = 5 << 20

// Avatars baja las fotos de perfil en segundo plano, una a la vez (como Telegram: 150 ms entre pedidos),
// y avisa al adaptador cuando hay un archivo nuevo para que emita chat.updated / person.updated con
// AvatarURL = "/avatar/<cuenta>/<archivo>". El nombre del archivo es <sha1(id)[:16]>-<sha1(url)[:8]><ext>;
// si ya existe uno con la misma URL no se vuelve a bajar (así el reinicio no repite descargas).
type Avatars struct {
	Dir string
	DL  Downloader
	Log *slog.Logger

	mu    sync.Mutex
	seen  map[string]string // id → URL ya atendida
	queue chan avatarJob
}

type avatarJob struct{ id, url string }

// NewAvatars crea la cola.
func NewAvatars(dir string, dl Downloader, log *slog.Logger) *Avatars {
	return &Avatars{Dir: dir, DL: dl, Log: log, seen: map[string]string{}, queue: make(chan avatarJob, 2048)}
}

// Reset olvida lo atendido (al reconectar se vuelven a revisar las fotos).
func (a *Avatars) Reset() {
	a.mu.Lock()
	a.seen = map[string]string{}
	a.mu.Unlock()
}

// Enqueue pide la foto de id en la URL dada; no hace nada si ya se atendió esa URL.
func (a *Avatars) Enqueue(id, rawURL string) {
	if id == "" || rawURL == "" || a.Dir == "" {
		return
	}
	a.mu.Lock()
	if a.seen[id] == rawURL {
		a.mu.Unlock()
		return
	}
	a.seen[id] = rawURL
	a.mu.Unlock()
	select {
	case a.queue <- avatarJob{id, rawURL}:
	default:
		a.mu.Lock()
		delete(a.seen, id) // cola llena: se reintenta en la próxima sincronización
		a.mu.Unlock()
	}
}

func sha1Hex(s string, n int) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:n]
}

// avatarURLKey identifica la foto sin los parámetros firmados que cambian en cada petición.
func avatarURLKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return sha1Hex(raw, 8)
	}
	return sha1Hex(u.Host+u.Path, 8)
}

// Run atiende la cola hasta que se cancele el contexto. onDone recibe el id y el nombre de archivo.
func (a *Avatars) Run(ctx context.Context, onDone func(id, file string)) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-a.queue:
			file := a.fetch(ctx, job)
			if file != "" {
				onDone(job.id, file)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
		}
	}
}

func (a *Avatars) fetch(ctx context.Context, job avatarJob) string {
	prefix := AvatarPrefix(job.id)
	key := avatarURLKey(job.url)
	if matches, _ := filepath.Glob(filepath.Join(a.Dir, prefix+key+".*")); len(matches) > 0 {
		return filepath.Base(matches[0])
	}
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, job.url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/*,*/*")
	if a.DL.UserAgent != "" {
		req.Header.Set("User-Agent", a.DL.UserAgent)
	}
	cli := a.DL.Client
	if cli == nil {
		cli = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := cli.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes))
	if err != nil || len(data) == 0 {
		return ""
	}
	ext := ".jpg"
	if exts, _ := mime.ExtensionsByType(strings.Split(resp.Header.Get("Content-Type"), ";")[0]); len(exts) > 0 {
		ext = exts[0]
		if ext == ".jpe" || ext == ".jfif" {
			ext = ".jpg"
		}
	}
	name := prefix + key + ext
	tmp := filepath.Join(a.Dir, name+".part")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return ""
	}
	// Se borran las versiones anteriores del mismo id.
	if old, _ := filepath.Glob(filepath.Join(a.Dir, prefix+"*")); len(old) > 0 {
		for _, f := range old {
			if !strings.HasSuffix(f, ".part") {
				_ = os.Remove(f)
			}
		}
	}
	if err := os.Rename(tmp, filepath.Join(a.Dir, name)); err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return name
}

// AvatarPrefix es el prefijo de los archivos de la foto de un id dentro de AvatarDir.
func AvatarPrefix(id string) string { return sha1Hex(id, 16) + "-" }
