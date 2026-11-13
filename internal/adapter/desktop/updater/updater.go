package updater

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Eventos de Wails que emite el servicio.
const (
	EvtState    = "update:state"
	EvtProgress = "update:progress"
)

// Estados del actualizador.
const (
	StateIdle        = "idle"
	StateChecking    = "checking"
	StateUpToDate    = "uptodate"
	StateAvailable   = "available"
	StateDownloading = "downloading"
	StateVerifying   = "verifying"
	StateReady       = "ready"
	StateInstalling  = "installing"
	StateError       = "error"
)

// Progress es el avance que se emite por update:progress. Phase: checking|downloading|verifying|ready|installing|error.
type Progress struct {
	Bytes int64  `json:"bytes"`
	Total int64  `json:"total"`
	Phase string `json:"phase"`
}

// Status es lo que ve la interfaz.
type Status struct {
	CurrentVersion string   `json:"currentVersion"`
	InstallKind    string   `json:"installKind"`
	CanInstall     bool     `json:"canInstall"`
	LastCheck      int64    `json:"lastCheck"` // milisegundos Unix; 0 = nunca
	AutoCheck      bool     `json:"autoCheck"`
	Latest         *Release `json:"latest,omitempty"`
	State          string   `json:"state"`
	Progress       Progress `json:"progress"`
	Error          string   `json:"error,omitempty"`
	// Folder: carpeta donde quedó (o quedaría) el instalador descargado.
	Folder string `json:"folder,omitempty"`
}

// Prefs guarda lo que el actualizador recuerda entre sesiones (connection.UpdatePrefs lo cumple).
type Prefs interface {
	AutoCheck() bool
	SetAutoCheck(bool) error
	LastCheck() time.Time
	SetLastCheck(time.Time) error
}

// CheckInterval: la comprobación automática se hace como máximo cada 24 h.
const CheckInterval = 24 * time.Hour

// Options configura el actualizador.
type Options struct {
	// CurrentVersion: buildinfo.Version.
	CurrentVersion string
	// DataDir: carpeta de datos (de ahí cuelgan updates/ y logs/).
	DataDir string
	// Exe: ruta del ejecutable en marcha.
	Exe string
	// Kind: tipo de instalación ("" = detectarlo con SystemKind).
	Kind Kind
	// GOARCH: arquitectura ("" = runtime.GOARCH).
	GOARCH string
	// Repo y API: origen (por defecto DefaultRepo y DefaultAPI; la variable EnvAPI cambia API hacia loopback).
	Repo, API string
	// Client: cliente HTTP base (pruebas con TLS). Las redirecciones se validan siempre.
	Client *http.Client
	// Hosts: hosts permitidos (por defecto DefaultHosts). Solo para pruebas.
	Hosts []string
	// MaxSize: tamaño máximo de la descarga (200 MiB).
	MaxSize int64
	// Retries y RetryDelay: reintentos de la descarga (3 y 2 s).
	Retries    int
	RetryDelay time.Duration
	// Timeout: tiempo máximo de toda la descarga (20 min).
	Timeout time.Duration
	Prefs   Prefs
	Now     func() time.Time
	// Emit avisa a la interfaz (EvtState con Status, EvtProgress con Progress).
	Emit func(event string, payload any)
	// Quit cierra la aplicación (apagando también el servidor local). Se llama tras lanzar el ayudante.
	Quit func()
	// Minimized dice si la ventana está oculta o minimizada (para relanzar con --minimized).
	Minimized func() bool
	// StartHelper lanza el ayudante (por defecto: copia el ejecutable a una carpeta temporal y lo ejecuta).
	StartHelper func(exe string, args []string) error
}

// Updater coordina la comprobación, la descarga verificada y el lanzamiento del ayudante.
type Updater struct {
	current, repo, api, dataDir, exe string
	kind                             Kind
	arch                             string
	archOK                           bool
	client                           *http.Client
	pol                              *policy
	maxSize                          int64
	retries                          int
	retryDelay, timeout              time.Duration
	prefs                            Prefs
	now                              func() time.Time
	emit                             func(string, any)
	quit                             func()
	minimized                        func() bool
	startHelper                      func(string, []string) error

	mu       sync.Mutex
	st       Status
	busy     bool
	lastEmit time.Time
}

// New crea el actualizador (no hace ninguna llamada de red).
func New(o Options) *Updater {
	u := &Updater{
		current: o.CurrentVersion, repo: o.Repo, api: o.API, dataDir: o.DataDir, exe: o.Exe, kind: o.Kind,
		client: o.Client, maxSize: o.MaxSize, retries: o.Retries, retryDelay: o.RetryDelay, timeout: o.Timeout,
		prefs: o.Prefs, now: o.Now, emit: o.Emit, quit: o.Quit, minimized: o.Minimized, startHelper: o.StartHelper,
	}
	if u.repo == "" {
		u.repo = DefaultRepo
	}
	hosts := o.Hosts
	if hosts == nil {
		hosts = DefaultHosts
	}
	u.pol = newPolicy(hosts)
	if u.api == "" {
		if env := os.Getenv(EnvAPI); env != "" {
			if pu, err := url.Parse(env); err == nil && isLoopbackHost(pu.Hostname()) && (pu.Scheme == "http" || pu.Scheme == "https") {
				u.api = strings.TrimRight(env, "/")
				if pu.Scheme == "http" {
					u.pol.allowLoopbackHTTP(pu.Hostname())
				} else {
					u.pol.hosts[strings.ToLower(pu.Hostname())] = true
				}
				slog.Warn("actualizaciones: API de pruebas", "api", u.api)
			} else {
				slog.Warn("actualizaciones: se ignora " + EnvAPI + " (solo se admite una dirección de loopback)")
			}
		}
	}
	if u.api == "" {
		u.api = DefaultAPI
	}
	if u.maxSize <= 0 {
		u.maxSize = 200 << 20
	}
	if u.retries <= 0 {
		u.retries = 3
	}
	if u.retryDelay <= 0 {
		u.retryDelay = 2 * time.Second
	}
	if u.timeout <= 0 {
		u.timeout = 20 * time.Minute
	}
	if u.now == nil {
		u.now = time.Now
	}
	if u.emit == nil {
		u.emit = func(string, any) {}
	}
	if u.kind == "" {
		u.kind = SystemKind(u.exe)
	}
	goarch := o.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	u.arch, u.archOK = ArchName(goarch)
	if u.startHelper == nil {
		u.startHelper = defaultStartHelper
	}
	_, vErr := ParseVersion(u.current)
	canInstall := (u.kind == KindMSI || u.kind == KindPortable) && u.archOK && vErr == nil
	u.st = Status{CurrentVersion: u.current, InstallKind: string(u.kind), CanInstall: canInstall, State: StateIdle}
	return u
}

// UpdatesDir es la carpeta de descargas.
func (u *Updater) UpdatesDir() string { return filepath.Join(u.dataDir, "updates") }

// Status devuelve una copia del estado actual.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.snapshot()
}

func (u *Updater) snapshot() Status {
	s := u.st
	if u.prefs != nil {
		s.AutoCheck = u.prefs.AutoCheck()
		if t := u.prefs.LastCheck(); !t.IsZero() {
			s.LastCheck = t.UnixMilli()
		}
	} else {
		s.AutoCheck = true
	}
	if s.Latest != nil {
		c := *s.Latest
		s.Latest = &c
	}
	return s
}

func (u *Updater) setState(state, errMsg string, phase string) {
	u.mu.Lock()
	u.st.State, u.st.Error = state, errMsg
	if phase != "" {
		u.st.Progress.Phase = phase
	}
	if state != StateDownloading {
		u.st.Progress.Bytes, u.st.Progress.Total = 0, 0
	}
	s := u.snapshot()
	u.mu.Unlock()
	u.emit(EvtState, s)
}

func (u *Updater) setProgress(done, total int64) {
	u.mu.Lock()
	u.st.Progress = Progress{Bytes: done, Total: total, Phase: StateDownloading}
	if time.Since(u.lastEmit) < 100*time.Millisecond && done < total {
		u.mu.Unlock()
		return
	}
	u.lastEmit = time.Now()
	p := u.st.Progress
	u.mu.Unlock()
	u.emit(EvtProgress, p)
}

// SetAutoCheck activa o apaga la búsqueda al abrir.
func (u *Updater) SetAutoCheck(on bool) error {
	if u.prefs == nil {
		return errors.New("no hay dónde guardar el ajuste")
	}
	if err := u.prefs.SetAutoCheck(on); err != nil {
		return err
	}
	u.emit(EvtState, u.Status())
	return nil
}

// AutoCheckDue: ¿toca buscar solo? Hay que tener la búsqueda activada y haber pasado 24 h de la última.
func (u *Updater) AutoCheckDue() bool {
	if u.prefs == nil || !u.prefs.AutoCheck() {
		return false
	}
	last := u.prefs.LastCheck()
	return last.IsZero() || u.now().Sub(last) >= CheckInterval || last.After(u.now().Add(time.Hour))
}

// AutoCheck es la comprobación al abrir: respeta el interruptor y las 24 h, y sus fallos son SILENCIOSOS
// (solo al registro, sin cambiar el estado ni avisar).
func (u *Updater) AutoCheck(ctx context.Context) {
	if !u.AutoCheckDue() {
		return
	}
	if _, err := u.check(ctx, true); err != nil {
		slog.Info("actualizaciones: comprobación automática sin resultado", "error", err)
	}
}

// Check consulta GitHub a petición de la persona. Un fallo deja el estado en «error» con el motivo.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	st, err := u.check(ctx, false)
	return st, err
}

func (u *Updater) check(ctx context.Context, silent bool) (Status, error) {
	u.mu.Lock()
	if u.busy {
		s := u.snapshot()
		u.mu.Unlock()
		return s, nil
	}
	u.busy = true
	u.mu.Unlock()
	defer func() { u.mu.Lock(); u.busy = false; u.mu.Unlock() }()

	if !silent {
		u.setState(StateChecking, "", "checking")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rel, err := u.fetchLatest(cctx)
	switch {
	case errors.Is(err, ErrNoRelease):
		rel, err = nil, nil
	case err != nil:
		if !silent {
			u.setState(StateError, err.Error(), "error")
		}
		return u.Status(), err
	}
	if u.prefs != nil {
		if perr := u.prefs.SetLastCheck(u.now()); perr != nil {
			slog.Warn("actualizaciones: no se pudo guardar la hora de la comprobación", "error", perr)
		}
	}
	u.mu.Lock()
	if rel != nil && IsNewer(rel.Version, u.current) {
		u.st.Latest = rel
		u.st.Folder = u.versionDir(rel.Version)
		u.mu.Unlock()
		u.setState(StateAvailable, "", "")
		return u.Status(), nil
	}
	u.st.Latest = nil
	u.mu.Unlock()
	u.setState(StateUpToDate, "", "")
	return u.Status(), nil
}

func (u *Updater) versionDir(v string) string { return filepath.Join(u.UpdatesDir(), v) }

// CanApply explica por qué no se puede instalar desde la aplicación ("" = sí se puede).
func (u *Updater) CanApply() string {
	switch {
	case u.kind != KindMSI && u.kind != KindPortable:
		return "Esta copia de dConnector no se instaló con el instalador ni como portable: actualízala desde la página de la versión."
	case !u.archOK:
		return "Esta arquitectura de Windows no tiene actualización automática: descárgala desde la página de la versión."
	}
	if _, err := ParseVersion(u.current); err != nil {
		return "Esta es una compilación de desarrollo: no se actualiza sola."
	}
	return ""
}

// Apply descarga y verifica el instalador, lanza el ayudante y cierra la aplicación. Si algo falla, el estado
// queda en «error» con el motivo y la carpeta del archivo descargado.
func (u *Updater) Apply(ctx context.Context) error {
	if why := u.CanApply(); why != "" {
		return errors.New(why)
	}
	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return errors.New("Ya hay una operación en curso.")
	}
	rel := u.st.Latest
	if rel == nil {
		u.mu.Unlock()
		return errors.New("Primero busca actualizaciones.")
	}
	u.busy = true
	u.mu.Unlock()
	rc := *rel
	err := u.apply(ctx, &rc)
	if err != nil {
		u.mu.Lock()
		u.busy = false
		u.mu.Unlock()
		u.setState(StateError, err.Error(), "error")
		return err
	}
	return nil // la aplicación se está cerrando: busy no se libera
}

func (u *Updater) apply(ctx context.Context, rel *Release) error {
	if rel.assetURL == "" || rel.AssetName == "" {
		return fmt.Errorf("La versión %s no trae un instalador para este equipo. Ábrela en la página de la versión.", rel.Version)
	}
	want, err := AssetName(rel.Version, u.kind, u.arch)
	if err != nil || want != rel.AssetName || !validAssetName(want) {
		return errors.New("El nombre del instalador no es el esperado; no se descarga.")
	}
	dir := u.versionDir(rel.Version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("No se pudo crear la carpeta de descargas: %w", err)
	}
	dctx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	u.setState(StateVerifying, "", "verifying")
	wantSum, err := u.fetchSums(dctx, rel)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, rel.AssetName)
	// Un archivo ya descargado y verificado se reutiliza.
	if sum, err := SHA256File(dest); err != nil || sum != wantSum {
		_ = os.Remove(dest)
		u.setState(StateDownloading, "", "downloading")
		if err := u.download(dctx, rel, dest, u.setProgress); err != nil {
			return err
		}
		u.setState(StateVerifying, "", "verifying")
		sum, err := SHA256File(dest)
		if err != nil {
			return fmt.Errorf("No se pudo leer el archivo descargado: %w", err)
		}
		if sum != wantSum {
			_ = os.Remove(dest)
			return errors.New("La descarga no coincide con la suma de verificación (SHA-256) publicada en la Release. Se borró el archivo y no se instaló nada. Inténtalo de nuevo; si se repite, no instales esta versión.")
		}
	}
	u.setState(StateReady, "", "ready")

	exe := u.exe
	args := ApplyArgs{
		Kind: string(u.kind), File: dest, SHA256: wantSum, ParentPID: os.Getpid(), Relaunch: exe,
		ParentData: u.dataDir, Minimized: u.minimized != nil && u.minimized(),
	}
	if u.kind == KindPortable {
		args.Target = filepath.Dir(exe)
	}
	u.setState(StateInstalling, "", "installing")
	if err := u.startHelper(exe, BuildApplyArgs(args)); err != nil {
		return fmt.Errorf("No se pudo iniciar el instalador: %w El archivo descargado queda en %s.", err, dir)
	}
	if u.quit != nil {
		u.quit()
	}
	return nil
}

// Cleanup borra descargas de versiones ya instaladas (o más antiguas) y restos de ayudantes viejos.
func (u *Updater) Cleanup() {
	entries, err := os.ReadDir(u.UpdatesDir())
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if v, err := ParseVersion(e.Name()); err != nil || !IsNewer(v.String(), u.current) {
				_ = os.RemoveAll(filepath.Join(u.UpdatesDir(), e.Name()))
			}
		}
	}
	pruneHelperTemp(24 * time.Hour)
}

// ReleasePageURL es la página de la versión nueva (o la última Release si aún no se comprobó). Solo https.
func (u *Updater) ReleasePageURL() string {
	u.mu.Lock()
	rel := u.st.Latest
	u.mu.Unlock()
	if rel != nil {
		if pu, err := url.Parse(rel.URL); err == nil && pu.Scheme == "https" && pu.Host != "" {
			return rel.URL
		}
	}
	return "https://github.com/" + u.repo + "/releases/latest"
}

// DownloadFolder es la carpeta del instalador descargado (la de la versión nueva si existe; si no, updates/).
func (u *Updater) DownloadFolder() string {
	u.mu.Lock()
	rel := u.st.Latest
	u.mu.Unlock()
	if rel != nil {
		if d := u.versionDir(rel.Version); dirExists(d) {
			return d
		}
	}
	return u.UpdatesDir()
}

func dirExists(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
