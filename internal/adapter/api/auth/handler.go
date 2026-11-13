package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const badLogin = "Usuario o clave incorrectos."

// Handler sirve /api/v1/auth/{login,logout,me,password,ws-ticket,media-token}. Acepta que el prefijo
// lo haya quitado o no el montaje (usa lo que sigue a «/auth/»).
func (s *Service) Handler() http.Handler {
	logout := s.RequireSession(http.HandlerFunc(s.handleLogout))
	me := s.RequireSession(http.HandlerFunc(s.handleMe))
	pw := s.RequireSession(http.HandlerFunc(s.handlePassword))
	wst := s.RequireSession(http.HandlerFunc(s.handleWSTicket))
	mt := s.RequireSession(http.HandlerFunc(s.handleMediaToken))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if i := strings.LastIndex(p, "/auth/"); i >= 0 {
			p = p[i+len("/auth/"):]
		}
		p = strings.Trim(p, "/")
		var h http.Handler
		method := http.MethodPost
		switch p {
		case "login":
			h = http.HandlerFunc(s.handleLogin)
		case "logout":
			h = logout
		case "me":
			h, method = me, http.MethodGet
		case "password":
			h = pw
		case "ws-ticket":
			h = wst
		case "media-token":
			h = mt
		default:
			WriteError(w, http.StatusNotFound, "not_found", "No existe esa ruta.")
			return
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			WriteError(w, http.StatusMethodNotAllowed, "invalid", "Método no permitido en esta ruta.")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	// Sin plazo, un cliente que gotea el cuerpo de un login mantiene la conexión abierta para siempre.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(15 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "La petición es demasiado grande.")
			return false
		}
		WriteError(w, http.StatusBadRequest, "invalid", "La petición no es un JSON válido.")
		return false
	}
	return true
}

func normUser(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if utf8.RuneCountInString(name) > 64 {
		name = string([]rune(name)[:64])
	}
	return name
}

func (s *Service) acquire(r *http.Request) bool {
	select {
	case s.sem <- struct{}{}:
		return true
	case <-r.Context().Done():
		return false
	}
}

func (s *Service) release() { <-s.sem }

func userJSON(u user) map[string]string {
	return map[string]string{"id": u.ID, "username": u.Username, "role": u.Role}
}

// recheckBlocked vuelve a mirar los bloqueos justo antes de gastar un argon2. Sin esta segunda mirada, una
// ráfaga de intentos simultáneos pasa toda la comprobación inicial (aún no hay fallos contados) y se evalúa
// entera: el límite de intentos no limitaría nada. Con el semáforo, los que esperan turno ven el bloqueo.
func (s *Service) recheckBlocked(w http.ResponseWriter, ipKey, userKey, msg string) bool {
	if rem, b := s.ipLim.blocked(ipKey); b {
		WriteRateLimited(w, rem, msg)
		return true
	}
	if rem, b := s.userLim.blocked(userKey); b {
		WriteRateLimited(w, rem, msg)
		return true
	}
	return false
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := ClientIP(r)
	ipKey := "ip:" + limiterIP(ip)
	if rem, b := s.ipLim.blocked(ipKey); b {
		WriteRateLimited(w, rem, "Demasiados intentos fallidos. Espere antes de volver a intentarlo.")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Username) == "" || body.Password == "" {
		WriteError(w, http.StatusBadRequest, "invalid", "Escriba el usuario y la clave.")
		return
	}
	ukey := "user:" + normUser(body.Username)
	if rem, b := s.userLim.blocked(ukey); b {
		WriteRateLimited(w, rem, "Demasiados intentos fallidos para este usuario. Espere antes de volver a intentarlo.")
		return
	}
	if !s.acquire(r) {
		return
	}
	if s.recheckBlocked(w, ipKey, ukey, "Demasiados intentos fallidos. Espere antes de volver a intentarlo.") {
		s.release()
		return
	}
	u, err := getUser(r.Context(), s.db, strings.TrimSpace(body.Username))
	hash := s.dummy
	if err == nil {
		hash = u.Hash
	}
	// Siempre se hace un argon2 (con el hash de relleno si no existe) para igualar los tiempos.
	ok, rehash := verifyPassword(body.Password, hash, s.params)
	s.release()
	if err != nil || !ok || u.DisabledAt != 0 {
		s.ipLim.fail(ipKey)
		s.userLim.fail(ukey)
		s.log.Warn("inicio de sesión fallido", "ip", ip)
		WriteError(w, http.StatusUnauthorized, "unauthorized", badLogin)
		return
	}
	if rehash {
		if nh, e := s.hash(body.Password); e == nil {
			_, _ = s.db.ExecContext(r.Context(), `UPDATE users SET password_hash = ? WHERE id = ?`, nh, u.ID)
		}
	}
	tok, exp, err := issueToken(r.Context(), s.db, u.ID, kindSession, s.now())
	if err != nil {
		s.log.Error("no se pudo emitir el token de sesión", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal", "No se pudo iniciar la sesión.")
		return
	}
	s.log.Info("inicio de sesión", "usuario", u.Username, "ip", ip)
	WriteJSON(w, http.StatusOK, map[string]any{"token": tok, "expiresAt": exp, "user": userJSON(u)})
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	if !p.local {
		// Invalida todos los tokens del usuario (sesión y media).
		if err := deleteUserTokens(r.Context(), s.db, p.userID); err != nil {
			s.log.Error("no se pudieron invalidar los tokens", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal", "No se pudo cerrar la sesión.")
			return
		}
		s.revoked(p.username)
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	WriteJSON(w, http.StatusOK, map[string]any{"id": p.userID, "username": p.username, "role": p.role, "local": p.local})
}

// handlePassword cambia la clave propia: {"currentPassword","newPassword"} (también se admite
// «oldPassword»). Invalida todos los tokens y devuelve una sesión nueva para no obligar a volver a entrar.
func (s *Service) handlePassword(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	if p.local {
		WriteError(w, http.StatusForbidden, "forbidden", "El usuario local no tiene clave.")
		return
	}
	var body struct {
		Current string `json:"currentPassword"`
		Old     string `json:"oldPassword"`
		New     string `json:"newPassword"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Current == "" {
		body.Current = body.Old
	}
	ip := ClientIP(r)
	ipKey := "ip:" + limiterIP(ip)
	ukey := "user:" + normUser(p.username)
	if rem, b := s.userLim.blocked(ukey); b {
		WriteRateLimited(w, rem, "Demasiados intentos fallidos. Espere antes de volver a intentarlo.")
		return
	}
	if rem, b := s.ipLim.blocked(ipKey); b {
		WriteRateLimited(w, rem, "Demasiados intentos fallidos. Espere antes de volver a intentarlo.")
		return
	}
	u, err := getUserByID(r.Context(), s.db, p.userID)
	if err != nil {
		s.deny(w, http.StatusUnauthorized)
		return
	}
	if !s.acquire(r) {
		return
	}
	if s.recheckBlocked(w, ipKey, ukey, "Demasiados intentos fallidos. Espere antes de volver a intentarlo.") {
		s.release()
		return
	}
	ok, _ := verifyPassword(body.Current, u.Hash, s.params)
	s.release()
	if !ok {
		s.ipLim.fail(ipKey)
		s.userLim.fail(ukey)
		WriteError(w, http.StatusUnauthorized, "unauthorized", "La clave actual no es correcta.")
		return
	}
	if err := validatePassword(body.New); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if body.New == body.Current {
		WriteError(w, http.StatusBadRequest, "invalid", "La clave nueva debe ser distinta de la actual.")
		return
	}
	h, err := s.hash(body.New)
	if err == nil {
		err = setPassword(r.Context(), s.db, u.ID, h)
	}
	if err != nil {
		s.log.Error("no se pudo cambiar la clave", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal", "No se pudo cambiar la clave.")
		return
	}
	s.revoked(p.username)
	tok, exp, err := issueToken(r.Context(), s.db, u.ID, kindSession, s.now())
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	s.log.Info("clave cambiada", "usuario", u.Username)
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "token": tok, "expiresAt": exp})
}

func (s *Service) handleWSTicket(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	t, exp, err := s.newTicket(p.username)
	if err != nil {
		WriteError(w, http.StatusServiceUnavailable, "unavailable", "No se pudo crear el ticket. Inténtelo de nuevo.")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ticket": t, "expiresAt": exp})
}

func (s *Service) handleMediaToken(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	if p.local {
		// El token local no tiene fila de usuario: el media-token del modo local es un ticket de lectura
		// sin usuario en la base, así que se emite con el usuario de sistema.
		tok, exp, err := s.localMediaToken(r)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal", "No se pudo crear el token de medios.")
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"token": tok, "expiresAt": exp})
		return
	}
	tok, exp, err := issueToken(r.Context(), s.db, p.userID, kindMedia, s.now())
	if err != nil {
		s.log.Error("no se pudo emitir el media-token", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal", "No se pudo crear el token de medios.")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"token": tok, "expiresAt": exp})
}

// localMediaToken emite un media-token para el modo local. Vive en memoria (el usuario local no tiene
// fila en users) y solo sirve para leer medios.
func (s *Service) localMediaToken(r *http.Request) (string, int64, error) {
	tok, err := randomToken()
	if err != nil {
		return "", 0, err
	}
	now := s.now()
	exp := now.Add(mediaTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.localMedia {
		if !t.After(now) {
			delete(s.localMedia, k)
		}
	}
	if len(s.localMedia) >= 10000 {
		return "", 0, errors.New("auth: demasiados tokens de medios")
	}
	s.localMedia[hashToken(tok)] = exp
	return tok, ms(exp), nil
}

func (s *Service) lookupLocalMedia(tok string) (string, bool) {
	key := hashToken(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.localMedia[key]
	if !ok {
		return "", false
	}
	if !exp.After(s.now()) {
		delete(s.localMedia, key)
		return "", false
	}
	return key, true
}
