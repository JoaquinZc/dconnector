package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultHosts son los únicos servidores desde los que se acepta descargar (y a los que puede redirigir GitHub).
var DefaultHosts = []string{"api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

const (
	maxRedirects = 5
	maxSumsBytes = 1 << 20
	stallTimeout = 45 * time.Second
)

// policy decide qué URL se pueden pedir: solo https y solo hosts permitidos. http se admite únicamente hacia
// los hosts de loopback de la API de pruebas (DCONNECTOR_UPDATE_API).
type policy struct {
	hosts    map[string]bool
	insecure map[string]bool
}

func newPolicy(hosts []string) *policy {
	p := &policy{hosts: map[string]bool{}, insecure: map[string]bool{}}
	for _, h := range hosts {
		p.hosts[strings.ToLower(h)] = true
	}
	return p
}

// allowLoopbackHTTP admite http (y el host) para la API de pruebas, solo si es loopback.
func (p *policy) allowLoopbackHTTP(host string) {
	h := strings.ToLower(host)
	p.hosts[h] = true
	p.insecure[h] = true
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (p *policy) check(u *url.URL) error {
	host := strings.ToLower(u.Hostname())
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && p.insecure[host]:
	default:
		return fmt.Errorf("se rechazó una dirección que no es https (%s)", safeHost(u))
	}
	if u.User != nil {
		return errors.New("se rechazó una dirección con usuario incrustado")
	}
	if !p.hosts[host] {
		return fmt.Errorf("se rechazó la descarga desde un servidor no permitido (%s)", safeHost(u))
	}
	return nil
}

func safeHost(u *url.URL) string {
	h := u.Hostname()
	if len(h) > 60 {
		h = h[:60] + "…"
	}
	return h
}

// httpClient devuelve un cliente que valida CADA redirección contra la política.
func (u *Updater) httpClient() *http.Client {
	base := u.client
	if base == nil {
		base = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConns:          2,
		}}
	}
	c := *base
	c.Jar = nil
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("demasiadas redirecciones")
		}
		return u.pol.check(req.URL)
	}
	return &c
}

// get hace un GET validado. No envía nada salvo User-Agent y Accept.
func (u *Updater) get(ctx context.Context, rawURL, accept string, hdr ...string) (*http.Response, error) {
	pu, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("la dirección de descarga no es válida")
	}
	if err := u.pol.check(pu); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dConnector/"+u.current)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := u.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var ue *url.Error
		if errors.As(err, &ue) && strings.Contains(ue.Err.Error(), "rechaz") {
			return nil, ue.Err
		}
		return nil, errors.New("No se pudo conectar con GitHub. Revisa tu conexión a internet e inténtalo de nuevo.")
	}
	return resp, nil
}

// fetchSums descarga SHA256SUMS y devuelve el hash esperado del asset.
func (u *Updater) fetchSums(ctx context.Context, rel *Release) (string, error) {
	if rel.sumsURL == "" {
		return "", errors.New("Esta versión no publica SHA256SUMS, así que no se puede verificar la descarga. No se instaló nada.")
	}
	resp, err := u.get(ctx, rel.sumsURL, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("No se pudo descargar SHA256SUMS (error %d).", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSumsBytes+1))
	if err != nil {
		return "", errors.New("Se cortó la descarga de SHA256SUMS. Inténtalo de nuevo.")
	}
	if len(b) > maxSumsBytes {
		return "", errors.New("SHA256SUMS es demasiado grande; se rechazó.")
	}
	return findSum(string(b), rel.AssetName)
}

// findSum busca «hash  nombre» (o «hash *nombre») en el contenido de SHA256SUMS.
func findSum(sums, name string) (string, error) {
	found := ""
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 || len(f[0]) != 64 {
			continue
		}
		if _, err := hex.DecodeString(f[0]); err != nil {
			continue
		}
		if strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		h := strings.ToLower(f[0])
		if found != "" && found != h {
			return "", errors.New("SHA256SUMS trae dos sumas distintas para el mismo archivo; se rechazó.")
		}
		found = h
	}
	if found == "" {
		return "", errors.New("SHA256SUMS no incluye este archivo, así que no se puede verificar. No se instaló nada.")
	}
	return found, nil
}

// SHA256File calcula el SHA-256 de un archivo (hex en minúsculas).
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// stallReader corta la lectura si no llegan datos en stallTimeout.
type stallReader struct {
	r      io.Reader
	timer  *time.Timer
	cancel context.CancelFunc
	d      time.Duration
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	s.timer.Reset(s.d)
	return n, err
}

// download baja rel.assetURL a dest (vía dest+".part"), con reanudación entre reintentos, tope de tamaño y
// progreso. No verifica el hash: eso lo hace el llamador.
func (u *Updater) download(ctx context.Context, rel *Release, dest string, progress func(done, total int64)) error {
	part := dest + ".part"
	var lastErr error
	for attempt := 0; attempt < u.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(u.retryDelay * time.Duration(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err := u.downloadOnce(ctx, rel, part, progress)
		if err == nil {
			if err := os.Rename(part, dest); err != nil {
				return fmt.Errorf("no se pudo guardar el archivo descargado: %w", err)
			}
			return nil
		}
		lastErr = err
		var fatal *fatalError
		if errors.As(err, &fatal) || ctx.Err() != nil {
			_ = os.Remove(part)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
	return fmt.Errorf("%w La descarga parcial se conserva para reanudarla.", lastErr)
}

// fatalError: un fallo que no mejora reintentando (tamaño, host, HTTP 4xx).
type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

func (u *Updater) downloadOnce(ctx context.Context, rel *Release, part string, progress func(done, total int64)) error {
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	var hdr []string
	if have > 0 {
		hdr = []string{"Range", fmt.Sprintf("bytes=%d-", have)}
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	resp, err := u.get(rctx, rel.assetURL, "application/octet-stream", hdr...)
	if err != nil {
		if strings.Contains(err.Error(), "rechaz") {
			return &fatalError{err.Error()}
		}
		return err
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusOK:
		have = 0 // el servidor no reanuda: se empieza de cero
		flags |= os.O_TRUNC
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(part)
		return errors.New("el servidor no aceptó reanudar; se empieza de nuevo")
	default:
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return &fatalError{fmt.Sprintf("GitHub no encontró el archivo de la versión nueva (error %d).", resp.StatusCode)}
		}
		return fmt.Errorf("GitHub respondió con un error (%d) al descargar.", resp.StatusCode)
	}
	total := have + resp.ContentLength
	if resp.ContentLength < 0 {
		total = rel.SizeBytes
	}
	if total > u.maxSize {
		return &fatalError{fmt.Sprintf("El archivo es demasiado grande (%d MiB; el máximo es %d MiB). Se canceló.", total>>20, u.maxSize>>20)}
	}
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		return fmt.Errorf("no se pudo preparar la carpeta de descargas: %w", err)
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return fmt.Errorf("no se pudo escribir la descarga: %w", err)
	}
	defer f.Close()
	timer := time.AfterFunc(stallTimeout, cancel)
	defer timer.Stop()
	body := &stallReader{r: resp.Body, timer: timer, cancel: cancel, d: stallTimeout}
	done := have
	buf := make([]byte, 64<<10)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > u.maxSize {
				return &fatalError{fmt.Sprintf("El archivo supera el máximo permitido (%d MiB). Se canceló.", u.maxSize>>20)}
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				return fmt.Errorf("no se pudo escribir la descarga: %w", werr)
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("Se cortó la descarga. Revisa tu conexión a internet.")
		}
	}
	if resp.ContentLength >= 0 && done != have+resp.ContentLength {
		return errors.New("La descarga quedó incompleta.")
	}
	return nil
}
