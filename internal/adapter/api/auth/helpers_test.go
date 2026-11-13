package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dconnector/internal/adapter/store/sqlite"
)

const goodPass = "una-clave-larga-123"

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type env struct {
	t     *testing.T
	s     *Service
	db    *sql.DB
	logs  *safeBuf
	clock *time.Time
	mux   *http.ServeMux
	hits  int
}

// newEnv abre un app.db real (SQLite en un directorio temporal) con parámetros de argon2 baratos.
func newEnv(t *testing.T, o Options) *env {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	st, err := sqlite.OpenApp(path) // aplica las migraciones
	require.NoError(t, err)
	require.NoError(t, st.Close())
	db, err := sqlite.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	logs := &safeBuf{}
	o.DB = db
	o.Log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s, err := NewService(o)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	s.params = argonParams{m: 16, t: 1, p: 1, saltLen: 16, keyLen: 32}
	s.dummy, err = hashPassword("x", s.params)
	require.NoError(t, err)
	clock := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }

	e := &env{t: t, s: s, db: db, logs: logs, clock: &clock}
	e.mux = http.NewServeMux()
	e.mux.Handle("/api/v1/auth/", s.Handler())
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hits++
		WriteJSON(w, 200, map[string]any{"user": s.Username(r), "local": s.IsLocal(r), "query": r.URL.RawQuery})
	})
	e.mux.Handle("/api/v1/rpc/", s.RequireSession(ok))
	e.mux.Handle("/api/v1/media/", s.RequireSessionOrMedia(ok))
	e.mux.Handle("/api/v1/avatar/", s.RequireSessionOrMedia(ok))
	e.mux.Handle("/api/v1/files/", s.RequireSessionOrMedia(ok))
	e.mux.Handle("/api/v1/files", s.RequireSessionOrMedia(ok))
	return e
}

func (e *env) advance(d time.Duration) { *e.clock = e.clock.Add(d) }

func (e *env) addUser(name, pass string) {
	e.t.Helper()
	h, err := hashPassword(pass, e.s.params)
	require.NoError(e.t, err)
	_, err = createUser(context.Background(), e.db, name, h, roleAdmin, e.s.now())
	require.NoError(e.t, err)
}

type resp struct {
	*httptest.ResponseRecorder
}

func (r resp) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &m)
	return m
}

func (r resp) errCode() string {
	if m, ok := r.json()["error"].(map[string]any); ok {
		s, _ := m["code"].(string)
		return s
	}
	return ""
}

func (e *env) do(method, target, token, ip string, body any) resp {
	e.t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	r := httptest.NewRequest(method, target, rd)
	if ip == "" {
		ip = "192.0.2.10:5555"
	}
	r.RemoteAddr = ip
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return resp{w}
}

func (e *env) login(user, pass, ip string) resp {
	return e.do("POST", "/api/v1/auth/login", "", ip,
		map[string]string{"username": user, "password": pass})
}

// token inicia sesión y devuelve el token.
func (e *env) token(user, pass string) string {
	e.t.Helper()
	r := e.login(user, pass, "")
	require.Equal(e.t, 200, r.Code, r.Body.String())
	return r.json()["token"].(string)
}

func newReq(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "192.0.2.10:5555"
	return r
}

func newRec() *httptest.ResponseRecorder { return httptest.NewRecorder() }
