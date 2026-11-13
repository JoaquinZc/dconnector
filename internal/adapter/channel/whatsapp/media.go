package whatsapp

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"

	// Formatos que image.DecodeConfig / image.Decode deben reconocer al enviar fotos y stickers.
	_ "image/gif"
	_ "image/png"
)

const (
	thumbMaxSide     = 72 // 07 §9: miniatura JPEG de 72 px de lado mayor
	thumbJPEGQuality = 70
	avatarTTL        = 24 * time.Hour // 07 §11: como mucho una consulta por JID cada 24 h
	avatarHTTPLimit  = 15 * time.Second
	avatarMaxBytes   = 4 << 20
)

// ---------------------------------------------------------------------------------------------
// Media.Handle (07 §6): []byte{tipo} + proto.Marshal(subMensaje)

// makeHandle serializa el submensaje de medio. Quita lo que no sirve para descargar (miniatura,
// cita, anotaciones) para que el dato opaco pese poco.
func makeHandle(typ byte, sub proto.Message) []byte {
	cl := proto.Clone(sub)
	switch s := cl.(type) {
	case *waE2E.ImageMessage:
		s.ContextInfo, s.JPEGThumbnail, s.InteractiveAnnotations, s.Annotations, s.Caption = nil, nil, nil, nil, nil
	case *waE2E.VideoMessage:
		s.ContextInfo, s.JPEGThumbnail, s.InteractiveAnnotations, s.Annotations, s.Caption = nil, nil, nil, nil, nil
	case *waE2E.AudioMessage:
		s.ContextInfo, s.Waveform = nil, nil
	case *waE2E.DocumentMessage:
		s.ContextInfo, s.JPEGThumbnail, s.Caption = nil, nil, nil
	case *waE2E.StickerMessage:
		s.ContextInfo, s.PngThumbnail = nil, nil
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(cl)
	if err != nil {
		return nil
	}
	return append([]byte{typ}, b...)
}

// parseHandle deshace makeHandle y devuelve el submensaje descargable.
func parseHandle(b []byte) (whatsmeow.DownloadableMessage, error) {
	if len(b) < 2 {
		return nil, fmt.Errorf("whatsapp: dato de descarga vacío: %w", port.ErrNotFound)
	}
	var sub interface {
		proto.Message
		whatsmeow.DownloadableMessage
	}
	switch b[0] {
	case handleImage:
		sub = &waE2E.ImageMessage{}
	case handleVideo:
		sub = &waE2E.VideoMessage{}
	case handleAudio:
		sub = &waE2E.AudioMessage{}
	case handleDocument:
		sub = &waE2E.DocumentMessage{}
	case handleSticker:
		sub = &waE2E.StickerMessage{}
	default:
		return nil, fmt.Errorf("whatsapp: tipo de archivo %d desconocido: %w", b[0], port.ErrInvalid)
	}
	if err := proto.Unmarshal(b[1:], sub); err != nil {
		return nil, fmt.Errorf("whatsapp: dato de descarga dañado: %w", port.ErrInvalid)
	}
	return sub, nil
}

// DownloadMedia descarga y descifra un archivo recibido (07 §6).
func (a *Adapter) DownloadMedia(ctx context.Context, m model.Media) (io.ReadCloser, error) {
	if len(m.Handle) == 0 {
		return nil, fmt.Errorf("whatsapp: este archivo no se puede descargar: %w", port.ErrNotFound)
	}
	sub, err := parseHandle(m.Handle)
	if err != nil {
		return nil, err
	}
	cli, err := a.live()
	if err != nil {
		return nil, err
	}
	// Si la URL caducó (403/404/410), fetchMedia le pide al teléfono que vuelva a subir el archivo
	// (media_retry.go).
	return a.fetchMedia(ctx, cli, m, sub)
}

// ---------------------------------------------------------------------------------------------
// Preparación de archivos a enviar (07 §9)

// checkSize aplica los máximos de capacidades (07 §12).
func checkSize(kind model.ContentKind, size int64) error {
	caps := baseCapabilities()
	limit := caps.MaxFileSize
	switch kind {
	case model.KindImage:
		limit = caps.MaxImageSize
	case model.KindVideo:
		limit = caps.MaxVideoSize
	}
	if size <= 0 {
		return fmt.Errorf("whatsapp: el archivo está vacío: %w", port.ErrInvalid)
	}
	if size > limit {
		return fmt.Errorf("whatsapp: el archivo pesa más del máximo permitido (%d MB): %w", limit>>20, port.ErrInvalid)
	}
	return nil
}

// detectMime elige el tipo MIME: el que pide quien envía, si no el de la extensión, si no el del contenido.
func detectMime(hint, path string, data []byte) string {
	if h := strings.TrimSpace(hint); h != "" {
		return h
	}
	if ext := filepath.Ext(path); ext != "" {
		if m := mime.TypeByExtension(strings.ToLower(ext)); m != "" {
			return m
		}
	}
	if len(data) > 512 {
		data = data[:512]
	}
	return http.DetectContentType(data)
}

// imageInfo devuelve ancho, alto y miniatura JPEG de una imagen. Si no se puede decodificar,
// devuelve ceros y miniatura nula (el envío sigue adelante).
func imageInfo(data []byte) (w, h int, thumb []byte) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		cfg, _, cerr := image.DecodeConfig(bytes.NewReader(data))
		if cerr != nil {
			return 0, 0, nil
		}
		return cfg.Width, cfg.Height, nil
	}
	b := img.Bounds()
	return b.Dx(), b.Dy(), makeThumb(img)
}

// makeThumb escala la imagen a thumbMaxSide px de lado mayor y la codifica en JPEG sobre fondo blanco.
func makeThumb(src image.Image) []byte {
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	tw, th := thumbMaxSide, thumbMaxSide
	if b.Dx() >= b.Dy() {
		th = max(1, b.Dy()*thumbMaxSide/b.Dx())
	} else {
		tw = max(1, b.Dx()*thumbMaxSide/b.Dy())
	}
	if b.Dx() < tw || b.Dy() < th { // no se amplía
		tw, th = b.Dx(), b.Dy()
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, xdraw.Src)
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbJPEGQuality}); err != nil {
		return nil
	}
	return buf.Bytes()
}

// webpSize: ancho y alto de un .webp.
func webpSize(data []byte) (int, int) {
	cfg, err := webp.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// oggOpusSeconds calcula la duración de un OGG/Opus leyendo la posición de grano de la última
// página (48 kHz, menos el pre-salto de OpusHead). 0 si no se puede.
func oggOpusSeconds(data []byte) int {
	last := bytes.LastIndex(data, []byte("OggS"))
	if last < 0 || len(data) < last+14 {
		return 0
	}
	granule := int64(binary.LittleEndian.Uint64(data[last+6 : last+14]))
	if granule <= 0 {
		return 0
	}
	preSkip := int64(0)
	if h := bytes.Index(data, []byte("OpusHead")); h >= 0 && len(data) >= h+12 {
		preSkip = int64(binary.LittleEndian.Uint16(data[h+10 : h+12]))
	}
	samples := granule - preSkip
	if samples <= 0 {
		return 0
	}
	secs := (samples + 47999) / 48000
	return int(secs)
}

// ---------------------------------------------------------------------------------------------
// Avatares (07 §11)

func avatarPrefix(jid types.JID) string {
	sum := sha1.Sum([]byte(jidToNative(jid)))
	return hex.EncodeToString(sum[:])[:16]
}

// latestAvatar devuelve el archivo de avatar más reciente que haya de ese JID ("" si no hay).
func (a *Adapter) latestAvatar(jid types.JID) string {
	matches, _ := filepath.Glob(filepath.Join(a.deps.AvatarDir, avatarPrefix(jid)+"-*.jpg"))
	best, bestTime := "", time.Time{}
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().After(bestTime) {
			best, bestTime = filepath.Base(m), fi.ModTime()
		}
	}
	return best
}

// FetchAvatar guarda la foto de perfil en AvatarDir y devuelve el nombre del archivo ("" si no tiene).
func (a *Adapter) FetchAvatar(ctx context.Context, peerOrChatNativeID string) (string, error) {
	jid, err := nativeToJID(peerOrChatNativeID)
	if err != nil {
		return "", err
	}
	if isStatusJID(jid) {
		return "", nil
	}
	return a.fetchAvatar(ctx, jid, false)
}

func (a *Adapter) fetchAvatar(ctx context.Context, jid types.JID, force bool) (string, error) {
	key := "avatar." + jidToNative(jid)
	if !force {
		if v, ok := a.local.kvGet(ctx, key); ok {
			var ts int64
			if _, err := fmt.Sscanf(v, "%d", &ts); err == nil && time.Since(time.UnixMilli(ts)) < avatarTTL {
				return a.latestAvatar(jid), nil
			}
		}
	}
	cli, err := a.live()
	if err != nil {
		return "", err
	}
	mark := func() { a.local.kvSet(ctx, key, fmt.Sprintf("%d", time.Now().UnixMilli())) }
	info, err := cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true})
	if err != nil {
		if errors.Is(err, whatsmeow.ErrProfilePictureNotSet) || errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) {
			mark()
			return "", nil
		}
		return "", a.mapErr("pedir la foto de perfil", err)
	}
	if info == nil || info.URL == "" {
		mark()
		return a.latestAvatar(jid), nil
	}
	name := fmt.Sprintf("%s-%s.jpg", avatarPrefix(jid), safeFilePart(info.ID))
	path := filepath.Join(a.deps.AvatarDir, name)
	if _, err := os.Stat(path); err == nil {
		mark()
		return name, nil
	}
	data, err := downloadAvatar(ctx, info.URL)
	if err != nil {
		return "", fmt.Errorf("whatsapp: no se pudo descargar la foto de perfil: %w", err)
	}
	if err := os.MkdirAll(a.deps.AvatarDir, 0o755); err != nil {
		return "", fmt.Errorf("whatsapp: no se pudo crear la carpeta de avatares: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("whatsapp: no se pudo guardar la foto de perfil: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("whatsapp: no se pudo guardar la foto de perfil: %w", err)
	}
	// Quita las versiones anteriores del mismo JID.
	old, _ := filepath.Glob(filepath.Join(a.deps.AvatarDir, avatarPrefix(jid)+"-*.jpg"))
	for _, o := range old {
		if filepath.Base(o) != name {
			_ = os.Remove(o)
		}
	}
	mark()
	return name, nil
}

func downloadAvatar(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, avatarHTTPLimit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: avatarHTTPLimit}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("respuesta HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, avatarMaxBytes))
}

// safeFilePart deja solo letras, dígitos, guion y guion bajo (el id de la foto va en el nombre).
func safeFilePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "0"
	}
	return b.String()
}
