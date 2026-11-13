package ws

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

// ---------------------------------------------------------------------------------------------
// Bajar: media.get → result + media.chunk

type mediaGetResult struct {
	TransferID string `json:"transferId"`
	FileName   string `json:"fileName"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	Chunks     int    `json:"chunks"`

	path string
}

type chunkData struct {
	TransferID string `json:"transferId"`
	Seq        int    `json:"seq"`
	Last       bool   `json:"last"`
	B64        string `json:"b64"`
}

func (s *session) cmdMediaGet(ctx context.Context, f frameIn) (any, error) {
	var d struct {
		MediaID string `json:"mediaId"`
	}
	if err := decodeData(f, &d); err != nil {
		return nil, err
	}
	if err := requireText("mediaId", d.MediaID); err != nil {
		return nil, err
	}
	acc, err := s.accountFor(ctx, f, "")
	if err != nil {
		return nil, err
	}
	if s.c.dst.Filter.ContactScope.Restricts() {
		// El archivo es de un chat: ese chat debe pasar el filtro de contactos.
		st, err := s.m.svc.Accounts.Store(acc)
		if err != nil {
			return nil, err
		}
		_, chat, err := st.GetMedia(ctx, d.MediaID)
		if err != nil {
			return nil, err
		}
		if chat != "" {
			if err := s.checkChatScope(ctx, acc, chat); err != nil {
				return nil, err
			}
		}
	}
	path, err := s.m.svc.Messaging.MediaPath(ctx, acc, d.MediaID)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return nil, fmt.Errorf("%w: el archivo ya no está en disco", port.ErrNotFound)
	}
	if fi.Size() > maxMediaSize {
		return nil, invalidf("el archivo supera los 100 MB que admite el protocolo")
	}
	name, mt := filepath.Base(path), ""
	if st, err := s.m.svc.Accounts.Store(acc); err == nil {
		if md, _, err := st.GetMedia(ctx, d.MediaID); err == nil {
			if md.FileName != "" {
				name = md.FileName
			}
			mt = md.Mime
		}
	}
	if mt == "" {
		mt = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	}
	if mt == "" {
		mt = "application/octet-stream"
	}
	chunks := int((fi.Size() + chunkSize - 1) / chunkSize)
	if chunks == 0 {
		chunks = 1
	}
	return &mediaGetResult{
		TransferID: model.NewID(), FileName: name, Mime: mt, Size: fi.Size(), Chunks: chunks, path: path,
	}, nil
}

// after manda los trozos del archivo tras el `result`. Los media.chunk no pasan por el outbox.
func (r *mediaGetResult) after(ctx context.Context, s *session) error {
	fh, err := os.Open(r.path)
	if err != nil {
		return err
	}
	defer fh.Close()
	buf := make([]byte, chunkSize)
	for seq := 0; seq < r.Chunks; seq++ {
		n, err := io.ReadFull(fh, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return err
		}
		out := frameOut{
			V: protocolVersion, ID: model.NewID(), Kind: kindEvent, Name: nameMediaChunk, TS: nowMs(),
			Data: chunkData{
				TransferID: r.TransferID, Seq: seq, Last: seq == r.Chunks-1,
				B64: base64.StdEncoding.EncodeToString(buf[:n]),
			},
		}
		if err := s.writeFrame(out, mediaWriteTimeout); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Subir: media.upload + media.chunk (servidor → dConnector)

// upload: una subida en curso o terminada, en tmp/<uploadId>.
type upload struct {
	mu       sync.Mutex
	id       string
	dstID    string
	path     string
	fileName string
	mime     string
	size     int64
	received int64
	nextSeq  int
	done     bool
	created  time.Time
}

func (s *session) cmdMediaUpload(ctx context.Context, f frameIn) (any, error) {
	var d struct {
		FileName string `json:"fileName"`
		Mime     string `json:"mime"`
		Size     int64  `json:"size"`
	}
	if err := decodeData(f, &d); err != nil {
		return nil, err
	}
	if d.Size <= 0 {
		return nil, invalidf("falta el tamaño del archivo")
	}
	if d.Size > maxMediaSize {
		return nil, invalidf("el archivo supera los 100 MB que admite el protocolo")
	}
	s.m.sweepUploads()
	if err := os.MkdirAll(paths.TmpDir(), 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo preparar la carpeta temporal: %w", err)
	}
	id := model.NewID()
	path := filepath.Join(paths.TmpDir(), "up-"+id)
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el archivo temporal: %w", err)
	}
	_ = fh.Close()
	u := &upload{
		id: id, dstID: s.c.dst.ID, path: path, fileName: filepath.Base(d.FileName), mime: d.Mime,
		size: d.Size, created: time.Now(),
	}
	if u.fileName == "." || u.fileName == string(filepath.Separator) {
		u.fileName = ""
	}
	s.m.umu.Lock()
	s.m.uploads[id] = u
	s.m.umu.Unlock()
	return map[string]string{"uploadId": id}, nil
}

// handleChunk escribe un trozo de subida. Solo el último trozo recibe `result` (o cualquiera, si
// falla: la subida se descarta).
func (s *session) handleChunk(f frameIn) {
	var d struct {
		UploadID string `json:"uploadId"`
		Seq      int    `json:"seq"`
		Last     bool   `json:"last"`
		B64      string `json:"b64"`
	}
	fail := func(err error) {
		s.logCommand(f, "", false, errorCode(err))
		s.m.dropUpload(d.UploadID)
		_ = s.replyError(f, err, "")
	}
	if err := decodeData(f, &d); err != nil {
		fail(err)
		return
	}
	u := s.m.getUpload(d.UploadID, s.c.dst.ID)
	if u == nil {
		fail(invalidf("el uploadId no existe o caducó"))
		return
	}
	u.mu.Lock()
	err := u.write(d.Seq, d.B64, d.Last)
	u.mu.Unlock()
	if err != nil {
		fail(err)
		return
	}
	if d.Last {
		s.logCommand(f, "", true, "")
		_ = s.replyOK(f, empty())
	}
}

// write añade un trozo (con u.mu tomado).
func (u *upload) write(seq int, b64 string, last bool) error {
	if u.done {
		return invalidf("la subida ya terminó")
	}
	if seq != u.nextSeq {
		return invalidf("trozo fuera de orden: se esperaba el %d", u.nextSeq)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return invalidf("el trozo no está en base64")
	}
	if u.received+int64(len(raw)) > u.size {
		return invalidf("la subida supera el tamaño anunciado")
	}
	fh, err := os.OpenFile(u.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("no se pudo escribir el archivo temporal: %w", err)
	}
	_, werr := fh.Write(raw)
	cerr := fh.Close()
	if werr != nil || cerr != nil {
		return fmt.Errorf("no se pudo escribir el archivo temporal")
	}
	u.received += int64(len(raw))
	u.nextSeq++
	if last {
		if u.received != u.size {
			return invalidf("la subida terminó con %d bytes y se anunciaron %d", u.received, u.size)
		}
		u.done = true
	}
	return nil
}

func (m *Manager) getUpload(id, dstID string) *upload {
	m.umu.Lock()
	defer m.umu.Unlock()
	u := m.uploads[id]
	if u == nil || u.dstID != dstID || time.Since(u.created) > uploadTTL {
		return nil
	}
	return u
}

func (m *Manager) dropUpload(id string) {
	m.umu.Lock()
	u := m.uploads[id]
	delete(m.uploads, id)
	m.umu.Unlock()
	if u != nil {
		_ = os.Remove(u.path)
	}
}

// sweepUploads borra las subidas que pasaron de 10 minutos.
func (m *Manager) sweepUploads() {
	m.umu.Lock()
	var old []*upload
	for id, u := range m.uploads {
		if time.Since(u.created) > uploadTTL {
			old = append(old, u)
			delete(m.uploads, id)
		}
	}
	m.umu.Unlock()
	for _, u := range old {
		_ = os.Remove(u.path)
	}
}

func (m *Manager) removeAllUploads() {
	m.umu.Lock()
	all := m.uploads
	m.uploads = map[string]*upload{}
	m.umu.Unlock()
	for _, u := range all {
		_ = os.Remove(u.path)
	}
}

// ---------------------------------------------------------------------------------------------
// Archivos que pide un envío: fileUrl (https) o uploadId

// fileKinds: tipos de mensaje que llevan archivo.
var fileKinds = map[model.ContentKind]bool{
	model.KindImage: true, model.KindVideo: true, model.KindAudio: true,
	model.KindVoice: true, model.KindDocument: true, model.KindSticker: true,
}

// resolveFile deja el archivo de un envío en una ruta local de tmp/. Devuelve "" si el envío no
// lleva archivo. cleanup borra lo descargado (las subidas caducan solas). fileName y mime, si
// vienen vacíos, se rellenan con los de la subida.
func (m *Manager) resolveFile(ctx context.Context, dstID string, acc model.AccountID, chatNative string,
	kind model.ContentKind, fileURL, uploadID string, fileName, mimeType *string) (string, func(), error) {
	if fileURL == "" && uploadID == "" {
		return "", nil, nil
	}
	if kind != "" && !fileKinds[kind] {
		return "", nil, invalidf("fileUrl y uploadId solo valen para mensajes con archivo")
	}
	if fileURL != "" && uploadID != "" {
		return "", nil, invalidf("usa fileUrl o uploadId, no los dos")
	}
	if uploadID != "" {
		u := m.getUpload(uploadID, dstID)
		if u == nil {
			return "", nil, invalidf("el uploadId no existe o caducó")
		}
		u.mu.Lock()
		done := u.done
		u.mu.Unlock()
		if !done {
			return "", nil, invalidf("la subida aún no terminó")
		}
		if fileName != nil && *fileName == "" {
			*fileName = u.fileName
		}
		if mimeType != nil && *mimeType == "" {
			*mimeType = u.mime
		}
		return u.path, nil, nil
	}
	return m.downloadFile(ctx, acc, chatNative, kind, fileURL, fileName, mimeType)
}

// maxFileFor: tope de tamaño según las capacidades de la cuenta (y nunca más de 100 MB).
func (m *Manager) maxFileFor(ctx context.Context, acc model.AccountID, chatNative string, kind model.ContentKind) int64 {
	limit := int64(maxMediaSize)
	caps, err := m.svc.Accounts.Capabilities(ctx, acc, chatNative)
	if err != nil {
		return limit
	}
	var c int64
	switch kind {
	case model.KindImage:
		c = caps.MaxImageSize
	case model.KindVideo:
		c = caps.MaxVideoSize
	default:
		c = caps.MaxFileSize
	}
	if c > 0 && c < limit {
		limit = c
	}
	return limit
}

func (m *Manager) downloadFile(ctx context.Context, acc model.AccountID, chatNative string, kind model.ContentKind,
	rawURL string, fileName, mimeType *string) (string, func(), error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || !strings.EqualFold(u.Scheme, "https") {
		return "", nil, invalidf("fileUrl debe ser una dirección https")
	}
	limit := m.maxFileFor(ctx, acc, chatNative, kind)
	dctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, invalidf("fileUrl no es válida")
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", nil, invalidf("no se pudo descargar el archivo: %s", rootCause(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, invalidf("la descarga del archivo respondió HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return "", nil, invalidf("el archivo supera el máximo de %s", sizeLabel(limit))
	}
	if err := os.MkdirAll(paths.TmpDir(), 0o755); err != nil {
		return "", nil, fmt.Errorf("no se pudo preparar la carpeta temporal: %w", err)
	}
	ext := ""
	if fileName != nil {
		ext = filepath.Ext(*fileName)
	}
	if ext == "" {
		ext = filepath.Ext(u.Path)
	}
	if len(ext) > 8 {
		ext = ""
	}
	path := filepath.Join(paths.TmpDir(), "dl-"+model.NewID()+strings.ToLower(ext))
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("no se pudo crear el archivo temporal: %w", err)
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	cerr := out.Close()
	if err != nil || cerr != nil {
		_ = os.Remove(path)
		return "", nil, invalidf("no se pudo descargar el archivo")
	}
	if n > limit {
		_ = os.Remove(path)
		return "", nil, invalidf("el archivo supera el máximo de %s", sizeLabel(limit))
	}
	if mimeType != nil && *mimeType == "" {
		*mimeType = strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	}
	if fileName != nil && *fileName == "" {
		if b := filepath.Base(u.Path); b != "." && b != "/" && b != string(filepath.Separator) {
			*fileName = b
		}
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func sizeLabel(n int64) string { return fmt.Sprintf("%.0f MB", float64(n)/(1<<20)) }
