// Package localserver lanza y supervisa el servidor local de dConnector (dConnector-server.exe) que usa la
// app de escritorio en modo local (docs/14 §3 y §8): proceso aparte, oculto, en loopback y con un token propio
// que viaja solo por entorno. Reutiliza un servidor que se dejó en segundo plano y lo apaga al salir.
package localserver

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Nombres y variables del ejecutable del servidor.
const (
	EnvServerExe = "DCONNECTOR_SERVER_EXE"
	EnvToken     = "DCONNECTOR_LOCAL_TOKEN"
)

// ServerExeName es el nombre del servidor junto al ejecutable del escritorio.
func ServerExeName() string {
	if runtime.GOOS == "windows" {
		return "dConnector-server.exe"
	}
	return "dConnector-server"
}

// ErrServerNotFound: no hay ejecutable del servidor.
var ErrServerNotFound = errors.New("no se encontró dConnector-server junto a la aplicación")

// FindServerExe localiza el servidor: la ruta explícita (--server-exe), la variable DCONNECTOR_SERVER_EXE o
// el archivo junto al ejecutable actual.
func FindServerExe(explicit string) (string, error) {
	cands := []string{explicit, os.Getenv(EnvServerExe)}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), ServerExeName()))
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	if explicit != "" {
		return "", fmt.Errorf("%w: %s", ErrServerNotFound, explicit)
	}
	return "", ErrServerNotFound
}

// Phase: en qué punto está el servidor local.
type Phase string

const (
	PhaseStarting   Phase = "starting"
	PhaseReady      Phase = "ready"
	PhaseRestarting Phase = "restarting"
	PhaseStopped    Phase = "stopped"
)

// Status es lo que se informa a quien escucha (la interfaz, la bandeja).
type Status struct {
	Phase   Phase
	Attempt int
	// Err: el motivo (en español) de la última caída o fallo de arranque.
	Err string
	// RetryIn: espera hasta el próximo intento (solo en PhaseRestarting).
	RetryIn time.Duration
	// Adopted: se reutilizó un servidor que ya estaba vivo.
	Adopted bool
}

// Options configura el lanzador. Solo DataDir es obligatorio.
type Options struct {
	// ExePath: ruta del servidor ("" = buscarlo con FindServerExe).
	ExePath string
	DataDir string
	// KeepRunning dice, al salir, si el servidor debe seguir vivo (nil = no).
	KeepRunning func() bool
	// ExtraArgs se añaden a los argumentos fijos (--data, --listen, --no-tls).
	ExtraArgs []string
	// Env: entorno base del proceso hijo (nil = el del proceso actual).
	Env []string
	// StartTimeout: espera máxima a la línea «listening» y a /health (30 s).
	StartTimeout time.Duration
	// StopTimeout: espera al apagado ordenado antes de matar el árbol de procesos (10 s).
	StopTimeout time.Duration
	// Backoff: espera antes del intento n (1…): 1, 2, 4, 8, 16, 30 s por defecto.
	Backoff func(attempt int) time.Duration
	// OnStatus recibe los cambios de estado (se llama en su propia goroutine, en orden).
	OnStatus func(Status)
}

// Launcher lanza, supervisa y apaga el servidor local.
type Launcher struct {
	o Options

	mu      sync.Mutex
	status  Status
	rec     Record // addr/token/pid del servidor listo
	owned   *exec.Cmd
	unbind  func(keepAlive bool) // suelta el objeto de trabajo del proceso propio (Windows)
	changed chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
	stopped bool
	exePath string
}

// New crea el lanzador (no arranca nada).
func New(o Options) *Launcher {
	if o.StartTimeout <= 0 {
		o.StartTimeout = 30 * time.Second
	}
	if o.StopTimeout <= 0 {
		o.StopTimeout = 10 * time.Second
	}
	if o.Backoff == nil {
		o.Backoff = DefaultBackoff
	}
	return &Launcher{o: o, changed: make(chan struct{}), status: Status{Phase: PhaseStopped}}
}

// DefaultBackoff: 1, 2, 4, 8, 16 y 30 s (tope).
func DefaultBackoff(attempt int) time.Duration {
	steps := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(steps) {
		attempt = len(steps)
	}
	return steps[attempt-1]
}

// Start busca el servidor y arranca la supervisión en segundo plano. Devuelve ErrServerNotFound (sin
// supervisar) si no hay ejecutable. No espera a que esté listo: use WaitReady.
func (l *Launcher) Start() error {
	exe, err := FindServerExe(l.o.ExePath)
	if err != nil {
		return err
	}
	l.mu.Lock()
	if l.cancel != nil {
		l.mu.Unlock()
		return errors.New("el servidor local ya está arrancado")
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel, l.done, l.exePath, l.stopped = cancel, make(chan struct{}), exe, false
	l.mu.Unlock()
	go l.loop(ctx)
	return nil
}

// Status devuelve el estado actual.
func (l *Launcher) Status() Status { l.mu.Lock(); defer l.mu.Unlock(); return l.status }

// Ready dice si el servidor está listo para recibir llamadas.
func (l *Launcher) Ready() bool { return l.Status().Phase == PhaseReady }

// BaseURL es la dirección base de la API («http://127.0.0.1:51234»), vacía si no está listo.
func (l *Launcher) BaseURL() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.status.Phase != PhaseReady {
		return ""
	}
	return "http://" + l.rec.Addr
}

// Token es el token propio del modo local (vacío si no está listo). No se registra nunca.
func (l *Launcher) Token() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.status.Phase != PhaseReady {
		return ""
	}
	return l.rec.Token
}

// Version es la versión que anunció el servidor.
func (l *Launcher) Version() string { l.mu.Lock(); defer l.mu.Unlock(); return l.rec.Version }

// PID del servidor listo (0 si no lo está).
func (l *Launcher) PID() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.status.Phase != PhaseReady {
		return 0
	}
	return l.rec.PID
}

// WaitReady espera a que el servidor esté listo (o a que ctx termine).
func (l *Launcher) WaitReady(ctx context.Context) error {
	for {
		l.mu.Lock()
		st, ch, stopped := l.status, l.changed, l.stopped
		l.mu.Unlock()
		if st.Phase == PhaseReady {
			return nil
		}
		if stopped {
			return errors.New("el servidor local está detenido")
		}
		select {
		case <-ch:
		case <-ctx.Done():
			msg := "se agotó la espera del servidor local"
			if st.Err != "" {
				msg += ": " + st.Err
			}
			return errors.New(msg)
		}
	}
}

func (l *Launcher) setStatus(s Status, rec *Record) {
	l.mu.Lock()
	l.status = s
	if rec != nil {
		l.rec = *rec
	}
	old := l.changed
	l.changed = make(chan struct{})
	cb := l.o.OnStatus
	l.mu.Unlock()
	close(old)
	if cb != nil {
		cb(s)
	}
}

func (l *Launcher) loop(ctx context.Context) {
	defer close(l.done)
	attempt := 0
	lastErr := ""
	for ctx.Err() == nil {
		ph := PhaseStarting
		if attempt > 0 {
			ph = PhaseRestarting
		}
		l.setStatus(Status{Phase: ph, Attempt: attempt, Err: lastErr}, nil)
		began := time.Now()
		err := l.runOnce(ctx, attempt)
		if ctx.Err() != nil {
			return
		}
		if time.Since(began) > time.Minute {
			attempt = 0
		}
		attempt++
		lastErr = "el servidor local se detuvo"
		if err != nil {
			lastErr = err.Error()
		}
		delay := l.o.Backoff(attempt)
		slog.Warn("servidor local caído; se reintenta", "intento", attempt, "espera", delay, "motivo", lastErr)
		l.setStatus(Status{Phase: PhaseRestarting, Attempt: attempt, Err: lastErr, RetryIn: delay}, nil)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}

// runOnce adopta o lanza un servidor y espera a que desaparezca (o a que ctx termine).
func (l *Launcher) runOnce(ctx context.Context, attempt int) error {
	if err := ensureRunDir(l.o.DataDir); err != nil {
		return err
	}
	unlock, err := acquireStartLock(l.o.DataDir, l.o.StartTimeout)
	if err != nil {
		return err
	}
	unlocked := false
	release := func() {
		if !unlocked {
			unlocked = true
			unlock()
		}
	}
	defer release()

	if rec, ok := readRecord(l.o.DataDir); ok && isServerProcess(rec.PID, rec.Exe) {
		if v, perr := probe(ctx, rec.Addr, rec.Token, 3*time.Second); perr == nil {
			rec.Version = v
			slog.Info("servidor local reutilizado", "pid", rec.PID, "addr", rec.Addr)
			l.mu.Lock()
			l.owned = nil
			l.mu.Unlock()
			l.setStatus(Status{Phase: PhaseReady, Attempt: attempt, Adopted: true}, &rec)
			release()
			return l.watchAdopted(ctx, rec)
		}
	}
	removeRecord(l.o.DataDir)

	token, err := newToken()
	if err != nil {
		return err
	}
	cmd := l.command(token)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errTail tailBuffer
	cmd.Stderr = &errTail
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("no se pudo arrancar el servidor local: %w", err)
	}
	slog.Info("servidor local lanzado", "pid", cmd.Process.Pid, "exe", l.exePath)
	// Si el escritorio muere a mala fuerza, Windows se lleva al servidor (no queda huérfano).
	unbind := bindLifetime(cmd.Process.Pid)
	handedOff := false
	defer func() {
		if !handedOff {
			unbind(false)
		}
	}()
	exited := make(chan error, 1)
	lines := make(chan listening, 1)
	go func() {
		// Lee la salida: solo importa la línea JSON «listening» (las demás no se registran: pueden llevar la
		// clave de un primer arranque) y después se vacía para que el servidor nunca se bloquee escribiendo.
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		sent := false
		for sc.Scan() {
			if sent {
				continue
			}
			if ev, ok := parseListening(sc.Bytes()); ok {
				sent = true
				lines <- ev
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()
	go func() { exited <- cmd.Wait() }()

	fail := func(msg string) error {
		killTree(cmd.Process.Pid)
		<-exited
		if t := errTail.String(); t != "" {
			msg += ": " + t
		}
		return errors.New(msg)
	}
	deadline := time.Now().Add(l.o.StartTimeout)
	timer := time.NewTimer(l.o.StartTimeout)
	defer timer.Stop()
	var ev listening
	select {
	case ev = <-lines:
	case err := <-exited:
		return exitError("el servidor local se cerró al arrancar", err, errTail.String())
	case <-timer.C:
		return fail("el servidor local no avisó que estaba listo a tiempo")
	case <-ctx.Done():
		l.terminate(cmd.Process.Pid, exited)
		return ctx.Err()
	}
	if err := waitHealth(ctx, ev.Addr, token, max(time.Until(deadline), 2*time.Second)); err != nil {
		if ctx.Err() != nil {
			l.terminate(cmd.Process.Pid, exited)
			return ctx.Err()
		}
		return fail("el servidor local no respondió")
	}
	rec := Record{PID: cmd.Process.Pid, Addr: ev.Addr, Token: token, Version: ev.Version, Exe: l.exePath, StartedAt: time.Now().UnixMilli()}
	if err := writeRecord(l.o.DataDir, rec); err != nil {
		slog.Warn("no se pudo guardar localserver.json", "error", err)
	}
	l.mu.Lock()
	l.owned, l.unbind = cmd, unbind
	l.mu.Unlock()
	handedOff = true
	l.setStatus(Status{Phase: PhaseReady, Attempt: attempt}, &rec)
	release()

	select {
	case err := <-exited:
		l.mu.Lock()
		l.owned, l.unbind = nil, nil
		l.mu.Unlock()
		unbind(false)
		removeRecord(l.o.DataDir)
		return exitError("el servidor local se detuvo", err, errTail.String())
	case <-ctx.Done():
		// Stop decide qué hacer con el proceso (apagarlo o dejarlo vivo).
		return ctx.Err()
	}
}

func exitError(prefix string, err error, tail string) error {
	msg := prefix
	if err != nil {
		msg += " (" + err.Error() + ")"
	}
	if tail != "" {
		msg += ": " + tail
	}
	return errors.New(msg)
}

// watchAdopted vigila un servidor que no lanzamos nosotros: cae si el proceso muere o deja de responder.
func (l *Launcher) watchAdopted(ctx context.Context, rec Record) error {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	fails := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if !isServerProcess(rec.PID, rec.Exe) {
				removeRecord(l.o.DataDir)
				return errors.New("el servidor local que estaba en segundo plano se cerró")
			}
			if _, err := probe(ctx, rec.Addr, "", 3*time.Second); err != nil {
				if fails++; fails >= 3 {
					return errors.New("el servidor local dejó de responder")
				}
				continue
			}
			fails = 0
		}
	}
}

func (l *Launcher) command(token string) *exec.Cmd {
	args := append([]string{"--data", l.o.DataDir, "--listen", "127.0.0.1:0", "--no-tls"}, l.o.ExtraArgs...)
	cmd := exec.Command(l.exePath, args...)
	cmd.Dir = filepath.Dir(l.exePath)
	base := l.o.Env
	if base == nil {
		base = os.Environ()
	}
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if !strings.HasPrefix(strings.ToUpper(kv), EnvToken+"=") {
			env = append(env, kv)
		}
	}
	// El token solo viaja por entorno (nunca por argumento).
	cmd.Env = append(env, EnvToken+"="+token)
	hideAndGroup(cmd)
	return cmd
}

// terminate apaga un proceso propio: señal ordenada y, si no responde en StopTimeout, mata el árbol.
func (l *Launcher) terminate(pid int, exited <-chan error) {
	if err := gracefulSignal(pid); err != nil {
		slog.Debug("no se pudo enviar la señal de apagado", "error", err)
	}
	select {
	case <-exited:
	case <-time.After(l.o.StopTimeout):
		slog.Warn("el servidor local no se apagó a tiempo; se termina el proceso", "pid", pid)
		killTree(pid)
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
		}
	}
}

// Stop detiene la supervisión y, salvo que «seguir en segundo plano» esté activo, apaga el servidor.
// Con la opción activa el servidor sigue vivo y localserver.json queda para reutilizarlo.
func (l *Launcher) Stop() {
	l.mu.Lock()
	cancel, done := l.cancel, l.done
	l.stopped = true
	l.cancel = nil
	l.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	// El estado se lee DESPUÉS de que el bucle terminó: si se leyera antes, un servidor que quedó listo justo
	// mientras se cancelaba pasaría por «no propio» y se quedaría vivo aunque no se pidiera.
	l.mu.Lock()
	owned, unbind, rec := l.owned, l.unbind, l.rec
	adopted := l.status.Phase == PhaseReady && l.status.Adopted
	l.owned, l.unbind = nil, nil
	l.mu.Unlock()
	keep := l.o.KeepRunning != nil && l.o.KeepRunning()
	switch {
	case owned != nil && keep:
		slog.Info("el servidor local sigue en segundo plano", "pid", rec.PID)
		if unbind != nil {
			unbind(true) // sobrevive al cierre del escritorio
		}
	case owned != nil:
		// cmd.Wait ya corre en su goroutina: se espera por el pid.
		l.stopPID(rec.PID)
		if unbind != nil {
			unbind(false)
		}
		removeRecord(l.o.DataDir)
	case adopted && !keep:
		l.stopPID(rec.PID)
		removeRecord(l.o.DataDir)
	}
	l.setStatus(Status{Phase: PhaseStopped}, nil)
}

// stopPID: señal ordenada y espera a que el proceso desaparezca; si pasa StopTimeout, mata el árbol.
func (l *Launcher) stopPID(pid int) {
	// Nunca se señala ni se mata un pid que ya no es nuestro servidor (el número pudo reutilizarse).
	if !isServerProcess(pid, l.exePath) {
		slog.Info("el servidor local ya no está; no se toca el pid", "pid", pid)
		return
	}
	if err := gracefulSignal(pid); err != nil {
		slog.Debug("no se pudo enviar la señal de apagado", "error", err)
	}
	deadline := time.Now().Add(l.o.StopTimeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	slog.Warn("el servidor local no se apagó a tiempo; se termina el proceso", "pid", pid)
	killTree(pid)
}

type listening struct {
	Event   string `json:"event"`
	Addr    string `json:"addr"`
	TLS     bool   `json:"tls"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

func parseListening(line []byte) (listening, bool) {
	var ev listening
	if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &ev) != nil || ev.Event != "listening" {
		return ev, false
	}
	host, port, err := net.SplitHostPort(ev.Addr)
	if err != nil || port == "" {
		return ev, false
	}
	// Solo loopback: el token propio nunca debe salir a otra interfaz.
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() || ev.TLS {
		return ev, false
	}
	return ev, true
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("no se pudo generar el token local: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

var probeClient = &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}

// probe comprueba /health (y, con token, que el token propio sea aceptado). Devuelve la versión.
func probe(ctx context.Context, addr, token string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/v1/health", nil)
	resp, err := probeClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var h struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&h)
	if resp.StatusCode != http.StatusOK || h.Status != "ok" {
		return "", fmt.Errorf("el servidor respondió %d (%s)", resp.StatusCode, h.Status)
	}
	if token != "" {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r2, err := probeClient.Do(req)
		if err != nil {
			return "", err
		}
		_ = r2.Body.Close()
		if r2.StatusCode != http.StatusOK {
			return "", fmt.Errorf("el servidor no aceptó el token local (%d)", r2.StatusCode)
		}
	}
	return h.Version, nil
}

// waitHealth espera a que /health diga «ok» y el token propio valga.
func waitHealth(ctx context.Context, addr, token string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	var last error
	for {
		_, err := probe(ctx, addr, token, 2*time.Second)
		if err == nil {
			return nil
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return last
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// tailBuffer guarda los últimos bytes escritos (la cola de stderr del servidor, para explicar un fallo).
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const tailMax = 1024

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailMax {
		t.buf = t.buf[len(t.buf)-tailMax:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}
