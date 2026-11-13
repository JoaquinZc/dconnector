package meta

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// ErrMediaGone: la URL del archivo caducó o el servidor ya no lo tiene (09 §2).
var ErrMediaGone = fmt.Errorf("El archivo ya no está disponible: %w", port.ErrNotFound)

// Handle es el dato opaco de Media.Handle: la URL de descarga y su expiración.
// Los chats cifrados (E2EE) añaden en E2EE el transporte del medio (JSON del proto) y en Type el tipo de medio.
type Handle struct {
	URL     string          `json:"u,omitempty"`
	Expires int64           `json:"e,omitempty"` // ms Unix UTC; 0 = no se sabe
	Mime    string          `json:"m,omitempty"`
	E2EE    json.RawMessage `json:"x,omitempty"`
	Type    string          `json:"t,omitempty"` // tipo de medio de whatsmeow (solo E2EE)
}

// MakeHandle serializa el Handle.
func MakeHandle(h Handle) []byte {
	b, _ := json.Marshal(h)
	return b
}

// ParseHandle lee un Handle; ErrNotFound si está vacío o dañado.
func ParseHandle(b []byte) (Handle, error) {
	var h Handle
	if len(b) == 0 || json.Unmarshal(b, &h) != nil || (h.URL == "" && len(h.E2EE) == 0) {
		return Handle{}, fmt.Errorf("el archivo no tiene datos de descarga: %w", port.ErrNotFound)
	}
	return h, nil
}

// Expired dice si la URL ya caducó en el instante dado (ms Unix).
func (h Handle) Expired(nowMs int64) bool {
	return h.Expires > 0 && nowMs >= h.Expires
}

// MediaID: primeros 24 hex de sha1(chat + "/" + mensaje + "/" + índice); único dentro de la cuenta.
func MediaID(chat, msgID string, idx int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s/%s/%d", chat, msgID, idx)))
	return hex.EncodeToString(sum[:])[:24]
}

// KindFromMime clasifica por tipo MIME cuando la librería no dice el tipo del adjunto.
func KindFromMime(m string) model.ContentKind {
	m = strings.ToLower(strings.TrimSpace(m))
	switch {
	case strings.HasPrefix(m, "image/"):
		return model.KindImage
	case strings.HasPrefix(m, "video/"):
		return model.KindVideo
	case strings.HasPrefix(m, "audio/"):
		return model.KindAudio
	}
	return model.KindDocument
}

// MimeFromName deduce el MIME por la extensión del nombre ("" si no se sabe).
func MimeFromName(name string) string {
	if ext := path.Ext(name); ext != "" {
		if m := mime.TypeByExtension(strings.ToLower(ext)); m != "" {
			return strings.Split(m, ";")[0]
		}
	}
	return ""
}

// FileNameFor da un nombre de archivo razonable cuando el mensaje no trae uno.
func FileNameFor(kind model.ContentKind, mimeType string) string {
	ext := ""
	if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
		ext = exts[0]
	}
	switch mimeType {
	case "image/jpeg":
		ext = ".jpg"
	case "audio/mp4", "audio/aac":
		ext = ".m4a"
	case "video/mp4":
		ext = ".mp4"
	}
	switch kind {
	case model.KindImage:
		return "imagen" + ext
	case model.KindVideo:
		return "video" + ext
	case model.KindVoice:
		return "nota-de-voz" + ext
	case model.KindAudio:
		return "audio" + ext
	case model.KindSticker:
		return "sticker" + ext
	}
	return "archivo" + ext
}

// Downloader baja archivos de las URLs firmadas del CDN de Meta.
//
// Desviación del documento (09 §2: «http.Get con las cookies»): las URL del CDN (fbcdn.net,
// cdninstagram.com) van firmadas y no necesitan sesión; mandarles las cookies de la cuenta a un dominio
// de terceros solo las expondría. La librería de mautrix tampoco las manda. Por eso no se envían.
type Downloader struct {
	Client    *http.Client
	UserAgent string
	Now       func() time.Time
}

// MaxDownload: tope de lo que se baja en memoria/disco (100 MB).
const MaxDownload = 100 << 20

// Open abre la descarga. Devuelve ErrMediaGone si caducó o el servidor responde 403/404/410.
func (d Downloader) Open(ctx context.Context, h Handle) (io.ReadCloser, error) {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	if h.URL == "" {
		return nil, fmt.Errorf("el archivo no tiene URL de descarga: %w", port.ErrNotFound)
	}
	if h.Expired(now().UnixMilli()) {
		return nil, ErrMediaGone
	}
	cli := d.Client
	if cli == nil {
		cli = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("la URL del archivo no es válida: %w", port.ErrInvalid)
	}
	req.Header.Set("Accept", "*/*")
	if d.UserAgent != "" {
		req.Header.Set("User-Agent", d.UserAgent)
	}
	resp, err := cli.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("no se pudo descargar el archivo: %w", port.ErrNotConnected)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		_ = resp.Body.Close()
		return nil, ErrMediaGone
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("el servidor respondió %d al bajar el archivo: %w", resp.StatusCode, port.ErrNotConnected)
	}
	return &limitedBody{ReadCloser: resp.Body, left: MaxDownload}, nil
}

type limitedBody struct {
	io.ReadCloser
	left int64
}

func (l *limitedBody) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, fmt.Errorf("el archivo es demasiado grande: %w", port.ErrInvalid)
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.ReadCloser.Read(p)
	l.left -= int64(n)
	return n, err
}
