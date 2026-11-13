// Package auth: usuarios, claves (argon2id), sesiones, tickets, límite de intentos y CLI de usuarios
// (docs/14 §3). Las firmas públicas de este archivo las fijó la costura de la oleada 1.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Options configura el servicio.
type Options struct {
	DB         *sql.DB // app.db
	Log        *slog.Logger
	LocalToken string // DCONNECTOR_LOCAL_TOKEN (vacío = sin modo local)
	TLS        bool   // el servidor sirve HTTPS (afecta a las cookies/cabeceras)

	// Adiciones (opcionales, valor cero = comportamiento del contrato).
	RatePerMinute     int  // peticiones por minuto y token (0 = 3000)
	ListenNonLoopback bool // el servidor escucha fuera de loopback: el token local se desactiva
}

// Service implementa api.Auth y las rutas /api/v1/auth/*.
type Service struct {
	db       *sql.DB
	log      *slog.Logger
	tls      bool
	hasLocal bool
	localSum [32]byte

	now    func() time.Time
	params argonParams
	dummy  string // hash de relleno para igualar el tiempo del login con usuario inexistente

	ipLim, userLim *attemptLimiter
	rate           *rateLimiter
	sem            chan struct{} // limita los argon2 simultáneos (64 MiB cada uno)

	mu         sync.Mutex
	tickets    map[string]ticket
	localMedia map[string]time.Time
	closed     bool
	onRevoke   func(username string)
}

type ticket struct {
	user string
	exp  time.Time
}

type principal struct {
	userID, username, role string
	local                  bool
	kind                   string
	key                    string
}

type ctxKey struct{}

// NewService valida las opciones y comprueba que la migración de app.db está aplicada.
func NewService(o Options) (*Service, error) {
	if o.DB == nil {
		return nil, errors.New("auth: falta la base de datos app.db")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if _, err := countUsers(context.Background(), o.DB); err != nil {
		return nil, fmt.Errorf("auth: la tabla de usuarios no está disponible (¿se aplicó la migración de app.db?): %w", err)
	}
	if o.LocalToken != "" && len(o.LocalToken) < 16 {
		return nil, errors.New("auth: DCONNECTOR_LOCAL_TOKEN es demasiado corto (mínimo 16 caracteres)")
	}
	rate := o.RatePerMinute
	if rate <= 0 {
		rate = 3000 // por minuto y token: la interfaz hace muchas llamadas pequeñas; el tope solo frena abusos
	}
	s := &Service{
		db: o.DB, log: o.Log, tls: o.TLS,
		now:     time.Now,
		params:  defaultParams,
		tickets: map[string]ticket{}, localMedia: map[string]time.Time{},
		sem: make(chan struct{}, 4),
	}
	if o.LocalToken != "" && !o.ListenNonLoopback {
		s.hasLocal = true
		s.localSum = sha256.Sum256([]byte(o.LocalToken))
	}
	s.initLimiters(rate)
	d, err := hashPassword("dconnector-relleno-sin-uso", s.params)
	if err != nil {
		return nil, err
	}
	s.dummy = d
	return s, nil
}

func (s *Service) initLimiters(rate int) {
	clock := func() time.Time { return s.now() }
	s.ipLim = newAttemptLimiter(5, 15*time.Minute, clock)
	s.userLim = newAttemptLimiter(10, 15*time.Minute, clock)
	s.rate = newRateLimiter(rate, clock)
}

// Close libera los tickets en memoria.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.tickets = map[string]ticket{}
	s.localMedia = map[string]time.Time{}
	s.mu.Unlock()
}

// ---- Autenticación de peticiones ----

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

func (s *Service) isLocalToken(r *http.Request, tok string) bool {
	if !s.hasLocal || tok == "" {
		return false
	}
	sum := sha256.Sum256([]byte(tok))
	if subtle.ConstantTimeCompare(sum[:], s.localSum[:]) != 1 {
		return false
	}
	// Solo por loopback y nunca a través de un proxy (que pondría estas cabeceras).
	if !isLoopbackConn(r) || r.Header.Get("X-Forwarded-For") != "" ||
		r.Header.Get("Forwarded") != "" || r.Header.Get("X-Real-IP") != "" {
		return false
	}
	return true
}

func (s *Service) principalFromToken(r *http.Request, tok string) (*principal, bool) {
	if s.isLocalToken(r, tok) {
		return &principal{userID: "local", username: "local", role: roleAdmin, local: true, kind: kindSession, key: "local"}, true
	}
	if s.hasLocal {
		if key, ok := s.lookupLocalMedia(tok); ok {
			return &principal{userID: "local", username: "local", role: roleAdmin, kind: kindMedia, key: key}, true
		}
	}
	ti, ok := lookupToken(r.Context(), s.db, tok, s.now())
	if !ok {
		return nil, false
	}
	return &principal{userID: ti.user.ID, username: ti.user.Username, role: ti.user.Role, kind: ti.kind, key: ti.key}, true
}

func (s *Service) deny(w http.ResponseWriter, status int) {
	if status == http.StatusForbidden {
		WriteError(w, status, "forbidden", "Este token solo sirve para leer medios, avatares y archivos.")
		return
	}
	WriteError(w, http.StatusUnauthorized, "unauthorized", "Falta iniciar sesión o la sesión caducó.")
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request, p *principal, next http.Handler) {
	// El token local es el de la propia aplicación de escritorio, por loopback y en la misma máquina: no se limita
	// (el tope existe para frenar a un cliente remoto descontrolado, no a la interfaz local).
	if p.userID != "local" {
		if ok, wait, first, top := s.rate.allow(p.key, r.URL.Path); !ok {
			if first && s.log != nil {
				// Solo rutas y cuentas, nunca datos: sirve para encontrar qué cliente o pantalla gasta el cupo.
				s.log.Warn("límite de peticiones alcanzado", "usuario", p.username, "rutas_mas_llamadas", top)
			}
			WriteRateLimited(w, wait, "Demasiadas peticiones. Espere un momento e inténtelo de nuevo.")
			return
		}
	}
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
}

// RequireSession exige Authorization: Bearer con un token de sesión o el token local (solo loopback).
// Un media-token da 403.
func (s *Service) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.principalFromToken(r, bearer(r))
		if !ok {
			s.deny(w, http.StatusUnauthorized)
			return
		}
		if p.kind != kindSession {
			s.deny(w, http.StatusForbidden)
			return
		}
		s.serve(w, r, p, next)
	})
}

var mediaPrefixes = []string{"/api/v1/media/", "/api/v1/avatar/", "/api/v1/files/"}

func mediaScope(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	for _, p := range mediaPrefixes {
		if strings.HasPrefix(r.URL.Path, p) {
			return true
		}
	}
	return false
}

// RequireSessionOrMedia además admite un media-token (en Bearer o en ?token=), solo en GET/HEAD de
// /api/v1/media/…, /api/v1/avatar/… y /api/v1/files/…. Un token de sesión en la URL se rechaza.
// Antes de pasar la petición se quita ?token= de la URL para que nada posterior lo registre.
func (s *Service) RequireSessionOrMedia(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		qtok := q.Get("token")
		if qtok != "" {
			r2 := r.Clone(r.Context())
			q.Del("token")
			r2.URL.RawQuery = q.Encode()
			r2.RequestURI = r2.URL.RequestURI()
			r = r2
		}
		tok := bearer(r)
		viaQuery := false
		if tok == "" {
			tok, viaQuery = qtok, true
		}
		p, ok := s.principalFromToken(r, tok)
		if !ok || (viaQuery && p.kind != kindMedia) {
			// Local o sesión en la URL: no se admiten (el token local y los de sesión nunca viajan en URL).
			s.deny(w, http.StatusUnauthorized)
			return
		}
		if p.kind == kindMedia && !mediaScope(r) {
			s.deny(w, http.StatusForbidden)
			return
		}
		s.serve(w, r, p, next)
	})
}

func principalOf(r *http.Request) *principal {
	p, _ := r.Context().Value(ctxKey{}).(*principal)
	return p
}

// Username devuelve el usuario autenticado de una petición ya validada ("" si no hay).
func (s *Service) Username(r *http.Request) string {
	if p := principalOf(r); p != nil {
		return p.username
	}
	return ""
}

// IsLocal dice si la petición se autenticó con el token local.
func (s *Service) IsLocal(r *http.Request) bool {
	p := principalOf(r)
	return p != nil && p.local
}

// ---- Tickets del WebSocket ----

func (s *Service) newTicket(username string) (string, int64, error) {
	tok, err := randomToken()
	if err != nil {
		return "", 0, err
	}
	now := s.now()
	exp := now.Add(wsTicketTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", 0, errors.New("auth: servicio cerrado")
	}
	for k, t := range s.tickets {
		if !t.exp.After(now) {
			delete(s.tickets, k)
		}
	}
	if len(s.tickets) >= 10000 {
		return "", 0, errors.New("auth: demasiados tickets pendientes")
	}
	s.tickets[hashToken(tok)] = ticket{user: username, exp: exp}
	return tok, ms(exp), nil
}

// RedeemWSTicket consume un ticket de un solo uso y devuelve el usuario.
func (s *Service) RedeemWSTicket(tk string) (string, bool) {
	if tk == "" || len(tk) > 256 {
		return "", false
	}
	key := hashToken(tk)
	s.mu.Lock()
	t, ok := s.tickets[key]
	delete(s.tickets, key)
	s.mu.Unlock()
	if !ok || !t.exp.After(s.now()) {
		return "", false
	}
	return t.user, true
}

// ---- Alta del administrador ----

// EnsureAdmin crea el administrador si no hay usuarios. Devuelve la clave generada (vacía si se usó
// DCONNECTOR_ADMIN_PASSWORD o ya había usuarios) y si se creó. La clave generada es para imprimirla UNA
// vez en la salida estándar; este paquete nunca la registra.
func (s *Service) EnsureAdmin(ctx context.Context, username string) (generated string, created bool, err error) {
	if username == "" {
		username = "admin"
	}
	if err := validateUsername(username); err != nil {
		return "", false, err
	}
	n, err := countUsers(ctx, s.db)
	if err != nil {
		return "", false, err
	}
	if n > 0 {
		return "", false, nil
	}
	pw := os.Getenv("DCONNECTOR_ADMIN_PASSWORD")
	fromEnv := pw != ""
	if fromEnv {
		if err := validatePassword(pw); err != nil {
			return "", false, fmt.Errorf("DCONNECTOR_ADMIN_PASSWORD no es válida: %s", strings.TrimSuffix(err.Error(), "."))
		}
	} else if pw, err = generatePassword(24); err != nil {
		return "", false, err
	}
	h, err := s.hash(pw)
	if err != nil {
		return "", false, err
	}
	// Inserción condicionada: si otro proceso creó usuarios entre medias, no se crea nada.
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(id, username, password_hash, role, created_at, disabled_at)
		 SELECT ?, ?, ?, ?, ?, 0 WHERE NOT EXISTS (SELECT 1 FROM users)`,
		newID(), username, h, roleAdmin, ms(s.now()))
	if err != nil {
		return "", false, fmt.Errorf("auth: no se pudo crear el administrador: %w", err)
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return "", false, nil
	}
	s.log.Info("administrador creado", "usuario", username)
	if fromEnv {
		return "", true, nil
	}
	return pw, true, nil
}

func (s *Service) hash(pw string) (string, error) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()
	return hashPassword(pw, s.params)
}

// OnRevoke registra la función a la que se avisa (con el nombre del usuario) cuando se invalidan sus sesiones
// por logout o cambio de clave: la API cierra entonces sus WebSocket con 4401. Implementa api.Revoker.
func (s *Service) OnRevoke(fn func(username string)) {
	s.mu.Lock()
	s.onRevoke = fn
	s.mu.Unlock()
}

// revoked descarta los tickets pendientes del usuario y avisa a quien escucha.
func (s *Service) revoked(username string) {
	s.mu.Lock()
	for k, t := range s.tickets {
		if t.user == username {
			delete(s.tickets, k)
		}
	}
	fn := s.onRevoke
	s.mu.Unlock()
	if fn != nil {
		fn(username)
	}
}

// UserActive dice si el usuario sigue existiendo y activo (el usuario local siempre). La API lo consulta
// de vez en cuando para cerrar los WebSocket de quien se desactivó o borró con la CLI (otro proceso, que no
// puede avisar a este). Un fallo de la base no cuenta como desactivado: no se echa a nadie por un error pasajero.
func (s *Service) UserActive(username string) bool {
	if username == "local" {
		return true
	}
	u, err := getUser(context.Background(), s.db, username)
	if errors.Is(err, errNoUser) {
		return false
	}
	return err != nil || u.DisabledAt == 0
}
