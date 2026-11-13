package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dconnector/internal/core/model"
	"dconnector/internal/paths"
)

// mediaResolver descarga (si falta) y devuelve la ruta local de un medio (lo cumple *service.Messaging).
type mediaResolver interface {
	MediaPath(ctx context.Context, acc model.AccountID, mediaID string) (string, error)
}

// hardenMedia: lo que se sirve viene de otras personas (un .html o .svg recibido). Con sandbox y nosniff,
// aunque se abriera la dirección directamente no ejecutaría nada dentro del origen de la aplicación.
func hardenMedia(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
}

// safeSegment rechaza segmentos vacíos o con separadores de ruta.
func safeSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && !strings.Contains(s, "\x00")
}

// splitTwo separa «<a>/<b>» tras el prefijo. r.URL.Path ya viene decodificado por net/http: no se vuelve a
// decodificar (un %252e%252e no debe acabar siendo «..»).
func splitTwo(path, prefix string) (a, b string, ok bool) {
	rest := strings.TrimPrefix(path, prefix)
	i := strings.Index(rest, "/")
	if i <= 0 {
		return "", "", false
	}
	a, b = rest[:i], rest[i+1:]
	return a, b, safeSegment(a) && safeSegment(b)
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) || !s.gateReady(w) {
		return
	}
	acc, mediaID, ok := splitTwo(r.URL.Path, apiBase+"/media/")
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalid, "La dirección del medio no es válida.")
		return
	}
	p, err := s.media.MediaPath(r.Context(), model.AccountID(acc), mediaID)
	if err != nil {
		writeServiceError(w, s.log, "media", err)
		return
	}
	if p == "" {
		writeError(w, http.StatusNotFound, codeNotFound, "El medio no está disponible.")
		return
	}
	serveLocalFile(w, r, p)
}

func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) || !s.gateReady(w) {
		return
	}
	acc, file, ok := splitTwo(r.URL.Path, apiBase+"/avatar/")
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalid, "La dirección del avatar no es válida.")
		return
	}
	dir := paths.AvatarDir(model.AccountID(acc))
	p := filepath.Join(dir, file)
	// Con los enlaces resueltos, el archivo debe seguir dentro de la carpeta de avatares de la cuenta.
	realDir, err1 := filepath.EvalSymlinks(dir)
	realFile, err2 := filepath.EvalSymlinks(p)
	if err1 != nil || err2 != nil || !inside(realDir, realFile) {
		writeError(w, http.StatusNotFound, codeNotFound, "El avatar no existe.")
		return
	}
	serveLocalFile(w, r, realFile)
}

func allowGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	writeError(w, http.StatusMethodNotAllowed, codeMethod, "Usa GET para descargar el medio.")
	return false
}

// serveLocalFile sirve un archivo con Range. Usa ServeContent (no ServeFile): ServeFile redirige las
// peticiones que terminan en «/index.html».
func serveLocalFile(w http.ResponseWriter, r *http.Request, p string) {
	f, err := os.Open(p)
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, "El medio no está disponible.")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeError(w, http.StatusNotFound, codeNotFound, "El medio no está disponible.")
		return
	}
	hardenMedia(w)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(p), st.ModTime(), f)
}
