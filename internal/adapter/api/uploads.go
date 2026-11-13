package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

// Límites de docs/14 §6.
const (
	defaultMaxUpload = 100 << 20 // por archivo
	defaultMaxTotal  = 1 << 30   // por usuario
	uploadTTL        = 24 * time.Hour
	uploadSweepEach  = 15 * time.Minute
	uploadStall      = 60 * time.Second // sin datos durante este tiempo se corta
	uploadScheme     = "upload://"
	metaFile         = ".meta.json"
	partFile         = ".part"
)

// Límites efectivos (variables para poder bajarlos en las pruebas).
var (
	maxUploadSize   int64 = defaultMaxUpload
	maxUploadsTotal int64 = defaultMaxTotal
)

// uploadMeta: la ficha de un archivo subido (<data>/uploads/<id>/.meta.json).
type uploadMeta struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	Owner     string `json:"owner"`
	CreatedAt int64  `json:"createdAt"`
	File      string `json:"file"` // nombre del archivo dentro de la carpeta del id
}

// uploadInfo: lo que devuelve POST /files.
type uploadInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// userError es un error con mensaje apto para mostrar y una clase de port (ErrInvalid…).
type userError struct {
	kind error
	msg  string
}

func (e *userError) Error() string { return e.msg }
func (e *userError) Unwrap() error { return e.kind }

func invalidArg(msg string) error { return &userError{port.ErrInvalid, msg} }

// uploads gestiona <data>/uploads: cada archivo vive en <data>/uploads/<ULID>/<nombre limpio> con su ficha.
// Se guarda dentro de una carpeta por id para que el nombre (y la extensión) que ven el canal y la campaña
// sean los del archivo original sin que el nombre del cliente sea nunca una ruta.
type uploads struct {
	log *slog.Logger
	mu  sync.Mutex // serializa la comprobación de cupo y la creación
	now func() time.Time
	ttl time.Duration
	// reserved: bytes que las subidas en curso de cada usuario pueden llegar a ocupar. Sin esta reserva,
	// N subidas simultáneas ven todas el cupo entero libre y lo superan N veces.
	reserved map[string]int64
}

func newUploads(log *slog.Logger) *uploads {
	return &uploads{log: log, now: time.Now, ttl: uploadTTL, reserved: map[string]int64{}}
}

func uploadsDir() string { return filepath.Join(paths.DataDir(), "uploads") }

func parseUploadID(s string) (string, error) {
	id, err := ulid.ParseStrict(s)
	if err != nil {
		return "", invalidArg("El identificador del archivo no es válido.")
	}
	return id.String(), nil
}

func (u *uploads) readMeta(canon string) (*uploadMeta, error) {
	b, err := os.ReadFile(filepath.Join(uploadsDir(), canon, metaFile))
	if err != nil {
		return nil, &userError{port.ErrNotFound, "El archivo subido no existe o ya caducó."}
	}
	var m uploadMeta
	if err := json.Unmarshal(b, &m); err != nil || m.File == "" || strings.ContainsAny(m.File, `/\`) {
		return nil, &userError{port.ErrNotFound, "El archivo subido no existe o ya caducó."}
	}
	return &m, nil
}

// lookup devuelve la ficha y la ruta de un archivo subido.
func (u *uploads) lookup(id string) (*uploadMeta, string, error) {
	canon, err := parseUploadID(id)
	if err != nil {
		return nil, "", err
	}
	m, err := u.readMeta(canon)
	if err != nil {
		return nil, "", err
	}
	p := filepath.Join(uploadsDir(), canon, m.File)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return nil, "", &userError{port.ErrNotFound, "El archivo subido no existe o ya caducó."}
	}
	return m, p, nil
}

// resolvePath: ver Server.resolveFileArgs. Devuelve la ficha si la ruta era un `upload://`.
func (u *uploads) resolvePath(p string, local bool) (string, *uploadMeta, error) {
	if rest, ok := strings.CutPrefix(p, uploadScheme); ok {
		m, path, err := u.lookup(rest)
		if err != nil {
			// Un id con mal formato o un archivo caducado son argumentos no válidos de la llamada.
			return "", nil, invalidArg(err.Error())
		}
		return path, m, nil
	}
	if local {
		if !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
			return "", nil, invalidArg("La ruta del archivo debe ser absoluta.")
		}
		return p, nil, nil
	}
	if insideManagedMedia(p) {
		return p, nil, nil
	}
	return "", nil, invalidArg("El archivo debe subirse antes con /files y enviarse como «upload://<id>».")
}

// unsafeRemotePath: rutas que el servidor no debe ni tocar a petición de un cliente remoto. Una ruta UNC
// (`\\host\share`, `//host/share`) o de dispositivo (`\\?\`, `\\.\`) hace que Windows abra una conexión SMB
// al evaluarla (con EvalSymlinks, solo para comprobar si está en una carpeta permitida) y entregue el hash
// NTLM de la cuenta del servidor a quien indicó el host; un flujo NTFS (`archivo:flujo`) lee datos ocultos.
func unsafeRemotePath(p string, windows bool) bool {
	if strings.ContainsRune(p, 0) {
		return true
	}
	if strings.HasPrefix(strings.ReplaceAll(p, "/", `\`), `\\`) {
		return true
	}
	if windows {
		for i := 0; i < len(p); i++ {
			if p[i] == ':' && i != 1 { // solo «X:» como unidad
				return true
			}
		}
	}
	return false
}

// insideManagedMedia: la ruta (con enlaces resueltos) está en una carpeta de medios del propio servidor.
func insideManagedMedia(p string) bool {
	if p == "" || unsafeRemotePath(p, os.PathSeparator == '\\') || !filepath.IsAbs(p) {
		return false
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return false
	}
	for _, d := range []string{paths.CampaignMediaDir(), paths.StatusMediaDir(), filepath.Join(paths.DataDir(), "scheduled-media")} {
		base, err := filepath.EvalSymlinks(d)
		if err != nil {
			continue
		}
		if inside(base, real) {
			return true
		}
	}
	return false
}

func inside(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// usedBy suma el tamaño de los archivos subidos de un usuario.
func (u *uploads) usedBy(owner string) int64 {
	ents, err := os.ReadDir(uploadsDir())
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if m, err := u.readMeta(e.Name()); err == nil && m.Owner == owner {
			total += m.Size
		}
	}
	return total
}

// sweep borra lo caducado (a las 24 h de subirse) y lo que quedó a medias.
func (u *uploads) sweep() {
	dir := uploadsDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := u.now()
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		if !e.IsDir() {
			_ = os.Remove(p)
			continue
		}
		var created time.Time
		if m, err := u.readMeta(e.Name()); err == nil {
			created = time.UnixMilli(m.CreatedAt)
		} else if st, err := os.Stat(p); err == nil {
			created = st.ModTime() // sin ficha: una subida a medias o rota
		}
		if now.Sub(created) >= u.ttl || (created.IsZero()) {
			if err := os.RemoveAll(p); err != nil {
				u.log.Warn("no se pudo borrar un archivo subido caducado", "error", err)
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------
// POST /api/v1/files

// stallReader renueva el plazo de lectura en cada lectura: 60 s sin datos corta la subida.
type stallReader struct {
	r  io.Reader
	rc *http.ResponseController
}

func (s stallReader) Read(p []byte) (int, error) {
	_ = s.rc.SetReadDeadline(time.Now().Add(uploadStall))
	return s.r.Read(p)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, codeMethod, "Usa POST para subir un archivo.")
		return
	}
	if !s.gateReady(w) {
		return
	}
	owner := s.cfg.Auth.Username(r)
	rc := http.NewResponseController(w)
	body := stallReader{r: http.MaxBytesReader(w, r.Body, maxUploadSize+(1<<20)), rc: rc}

	mr, err := multipartReader(r, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalid, "La subida debe ser multipart/form-data con un campo «file».")
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			s.uploadReadError(w, err, "Falta el campo «file» con el archivo.")
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		info, err := s.files.store(owner, part.FileName(), part)
		_ = part.Close()
		if err != nil {
			s.uploadReadError(w, err, genericInternal)
			return
		}
		writeJSON(w, http.StatusOK, info)
		return
	}
}

func multipartReader(r *http.Request, body io.Reader) (*multipart.Reader, error) {
	ct := r.Header.Get("Content-Type")
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		return nil, errors.New("no es multipart")
	}
	return multipart.NewReader(body, params["boundary"]), nil
}

var errTooLarge = errors.New("archivo demasiado grande")

type quotaError struct{}

func (quotaError) Error() string { return "cupo de subidas agotado" }

func (s *Server) uploadReadError(w http.ResponseWriter, err error, fallback string) {
	var mbe *http.MaxBytesError
	switch {
	case errors.Is(err, errTooLarge) || errors.As(err, &mbe):
		writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, "El archivo supera el límite de 100 MiB.")
	case errors.As(err, new(quotaError)):
		writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, "Se alcanzó el límite de 1 GiB de archivos subidos. Espera a que caduquen (24 h) o envía los pendientes.")
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		writeError(w, http.StatusBadRequest, codeInvalid, fallback)
	case strings.Contains(err.Error(), "multipart"), errors.As(err, new(*textproto.ProtocolError)), strings.Contains(err.Error(), "MIME"):
		writeError(w, http.StatusBadRequest, codeInvalid, "La subida no tiene un formato multipart válido.")
	default:
		s.log.Error("subida de archivo", "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, genericInternal)
	}
}

// store guarda un archivo subido y devuelve su ficha.
func (u *uploads) store(owner, name string, src io.Reader) (uploadInfo, error) {
	clean := cleanFileName(name)

	u.mu.Lock()
	remaining := maxUploadsTotal - u.usedBy(owner) - u.reserved[owner]
	if remaining <= 0 {
		u.mu.Unlock()
		return uploadInfo{}, quotaError{}
	}
	limit := maxUploadSize
	quota := false
	if remaining < limit {
		limit, quota = remaining, true
	}
	u.reserved[owner] += limit
	u.mu.Unlock()
	defer func() { // se libera al terminar, cuando la ficha ya cuenta en usedBy (o la subida falló)
		u.mu.Lock()
		if u.reserved[owner] -= limit; u.reserved[owner] <= 0 {
			delete(u.reserved, owner)
		}
		u.mu.Unlock()
	}()

	id := ulid.Make().String()
	dir := filepath.Join(uploadsDir(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return uploadInfo{}, fmt.Errorf("crear la carpeta de la subida: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	tmp := filepath.Join(dir, partFile)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return uploadInfo{}, fmt.Errorf("crear el archivo: %w", err)
	}
	br := bufio.NewReaderSize(io.LimitReader(src, limit+1), 4096)
	head, _ := br.Peek(512)
	detected := detectMime(head, clean)
	n, err := io.Copy(f, br)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return uploadInfo{}, err
	}
	if n > limit {
		if quota {
			return uploadInfo{}, quotaError{}
		}
		return uploadInfo{}, errTooLarge
	}
	if err := os.Rename(tmp, filepath.Join(dir, clean)); err != nil {
		return uploadInfo{}, fmt.Errorf("guardar el archivo: %w", err)
	}
	meta := uploadMeta{ID: id, Name: filepathBase(name, clean), Mime: detected, Size: n, Owner: owner, CreatedAt: u.now().UnixMilli(), File: clean}
	b, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, metaFile), b, 0o600); err != nil {
		return uploadInfo{}, fmt.Errorf("guardar la ficha: %w", err)
	}
	ok = true
	return uploadInfo{ID: id, Name: meta.Name, Mime: meta.Mime, Size: meta.Size}, nil
}

// filepathBase: el nombre original (solo para mostrarlo) sin carpetas ni caracteres de control.
func filepathBase(orig, clean string) string {
	n := strings.ReplaceAll(orig, `\`, "/")
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, n)
	n = strings.TrimSpace(n)
	if n == "" || !utf8.ValidString(n) {
		return clean
	}
	if len(n) > 255 {
		n = clean
	}
	return n
}

var winReserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true,
	"com0": true, "lpt0": true, "com¹": true, "com²": true, "com³": true, "lpt¹": true, "lpt²": true, "lpt³": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true}

// cleanFileName deja un nombre seguro para usarlo como archivo en cualquier sistema (nunca una ruta).
func cleanFileName(orig string) string {
	n := strings.ReplaceAll(orig, `\`, "/")
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	n = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f, strings.ContainsRune(`:*?"<>|`, r):
			return '_'
		}
		return r
	}, n)
	if !utf8.ValidString(n) {
		n = strings.ToValidUTF8(n, "_")
	}
	n = strings.Trim(strings.TrimSpace(n), ". ")
	if len(n) > 120 { // recorta por bytes sin partir un carácter, conservando la extensión
		ext := filepath.Ext(n)
		if len(ext) > 16 {
			ext = ""
		}
		stem := n[:120-len(ext)]
		for !utf8.ValidString(stem) {
			stem = stem[:len(stem)-1]
		}
		n = stem + ext
	}
	if n == "" {
		return "archivo"
	}
	if isWinReserved(n) {
		n = "_" + n
	}
	return n
}

// isWinReserved: en Windows un nombre de dispositivo (CON, NUL, COM1…) lo es también con cualquier
// extensión (CON.txt, NUL.tar.gz) y con espacios antes del punto («con .txt»): vale lo que hay antes del primer punto.
func isWinReserved(n string) bool {
	base := n
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	return winReserved[strings.ToLower(strings.TrimRight(base, " "))]
}

var extMimes = map[string]string{
	".csv": "text/csv", ".txt": "text/plain", ".json": "application/json", ".pdf": "application/pdf",
	".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg", ".mp3": "audio/mpeg", ".m4a": "audio/mp4",
	".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".mkv": "video/x-matroska",
	".webp": "image/webp", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// detectMime: por contenido y, si el contenido no basta (texto o binario genérico), por extensión.
func detectMime(head []byte, name string) string {
	d := http.DetectContentType(head)
	base, _, err := mime.ParseMediaType(d)
	if err != nil {
		base = d
	}
	if base == "application/octet-stream" || base == "text/plain" || base == "application/zip" || base == "text/xml" {
		ext := strings.ToLower(filepath.Ext(name))
		if m, ok := extMimes[ext]; ok { // tabla propia: el registro de Windows da otros tipos (.csv = ms-excel)
			return m
		}
		if byExt := mime.TypeByExtension(ext); byExt != "" {
			if b, _, err := mime.ParseMediaType(byExt); err == nil {
				return b
			}
			return byExt
		}
	}
	return base
}

// ---------------------------------------------------------------------------------------------
// GET /api/v1/files/{id}

func (s *Server) handleFileGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, codeMethod, "Usa GET para descargar un archivo.")
		return
	}
	if !s.gateReady(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, apiBase+"/files/")
	if _, err := parseUploadID(id); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalid, err.Error())
		return
	}
	meta, p, err := s.files.lookup(id)
	if err != nil {
		writeServiceError(w, s.log, "files.get", err)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, "El archivo subido no existe o ya caducó.")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, "El archivo subido no existe o ya caducó.")
		return
	}
	hardenMedia(w)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", meta.Mime)
	http.ServeContent(w, r, "", st.ModTime(), f)
}
