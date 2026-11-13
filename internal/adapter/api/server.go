package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"dconnector/internal/adapter/api/facade"
	"dconnector/internal/core/model"
)

const (
	apiBase    = "/api/v1"
	connHeader = "X-DConnector-Conn"
)

func newServer(cfg Config) (*Server, error) {
	if cfg.Core == nil {
		return nil, errors.New("api: falta el núcleo (Config.Core)")
	}
	if cfg.Auth == nil {
		return nil, errors.New("api: falta la autenticación (Config.Auth)")
	}
	c := cfg.Core
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: cfg, log: log, ctx: ctx, cancel: cancel}

	d := facade.Deps{
		Accounts: c.Accounts, Inbox: c.Inbox, Messaging: c.Messaging, Statuses: c.Statuses,
		Contacts: c.Contacts, Campaigns: c.Campaigns, Scheduler: c.Scheduler, Stats: c.Stats,
		Destinations: c.Destinations, Settings: c.Settings, History: c.History,
		Notifications:   cfg.Notifications,
		OnSettingsSaved: cfg.OnSettingsSaved,
	}
	if c.Protocol != nil {
		d.DestinationStates = c.Protocol.States
		d.DestinationTest = c.Protocol.Test
	}
	s.facade = facade.New(d)
	s.rpc = buildRPC(s.facade.Services())
	s.files = newUploads(log)
	if c.Messaging != nil {
		s.media = c.Messaging
	}
	s.hub = newHub(ctx, log, cfg.Version, s.facade.ConnClosed)
	if ua, ok := cfg.Auth.(interface{ UserActive(username string) bool }); ok {
		s.hub.active = ua.UserActive
	}
	if rv, ok := cfg.Auth.(Revoker); ok {
		rv.OnRevoke(s.hub.revokeUser)
	}
	s.note = newNotifier(s.hub.broadcast)
	if cfg.Notifications != nil {
		s.SetNotifications(cfg.Notifications)
	}

	// Puente bus -> WebSocket.
	if c.Bus != nil {
		br := &bridge{emit: s.hub.broadcast, b: c.Bus}
		if c.Inbox != nil {
			br.unread = c.Inbox
		}
		if c.Campaigns != nil {
			br.progress = c.Campaigns.Progress()
		}
		if c.History != nil {
			ch, unsub := c.History.Subscribe(64)
			br.history = ch
			s.cleanups = append(s.cleanups, unsub) // se cancela en Close
		}
		s.wg.Add(1)
		go func() { defer s.wg.Done(); br.run(ctx) }()
	}
	if c.Protocol != nil {
		c.Protocol.OnState(func(st model.DestinationState) { s.hub.broadcast(EvtDestinationState, st) })
	}
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.note.watch(ctx) }()

	// Limpieza de subidas: al arrancar y cada 15 minutos.
	s.files.sweep()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(uploadSweepEach)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.files.sweep()
			}
		}
	}()

	s.handler = s.middleware(http.HandlerFunc(s.route))
	return s, nil
}

// route reparte las rutas de /api/v1 (sin ServeMux: este redirigiría las rutas con «..» en lugar de rechazarlas).
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	a := s.cfg.Auth
	switch {
	case p == apiBase+"/health":
		s.handleHealth(w, r)
	case strings.HasPrefix(p, apiBase+"/auth/"):
		if s.cfg.AuthRoutes == nil {
			writeError(w, http.StatusNotFound, codeNotFound, "No existe esa ruta.")
			return
		}
		s.cfg.AuthRoutes.ServeHTTP(w, r)
	case strings.HasPrefix(p, apiBase+"/rpc/"):
		a.RequireSession(http.HandlerFunc(s.handleRPC)).ServeHTTP(w, r)
	case p == apiBase+"/events":
		s.handleEvents(w, r)
	case p == apiBase+"/files":
		a.RequireSession(http.HandlerFunc(s.handleUpload)).ServeHTTP(w, r)
	case strings.HasPrefix(p, apiBase+"/files/"):
		a.RequireSessionOrMedia(http.HandlerFunc(s.handleFileGet)).ServeHTTP(w, r)
	case strings.HasPrefix(p, apiBase+"/media/"):
		a.RequireSessionOrMedia(http.HandlerFunc(s.handleMedia)).ServeHTTP(w, r)
	case strings.HasPrefix(p, apiBase+"/avatar/"):
		a.RequireSessionOrMedia(http.HandlerFunc(s.handleAvatar)).ServeHTTP(w, r)
	default:
		writeError(w, http.StatusNotFound, codeNotFound, "No existe esa ruta.")
	}
}

// gateReady responde 503 si el servidor está arrancando o apagándose.
func (s *Server) gateReady(w http.ResponseWriter) bool {
	if s.cfg.Ready == nil {
		return true
	}
	switch s.cfg.Ready() {
	case "ok":
		return true
	case "stopping":
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, "El servidor se está apagando.")
	default:
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, "El servidor está arrancando. Inténtalo en unos segundos.")
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, codeMethod, "Usa GET.")
		return
	}
	status := "ok"
	if s.cfg.Ready != nil {
		status = s.cfg.Ready()
	}
	code := http.StatusOK
	if status != "ok" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status": status, "version": s.cfg.Version, "apiVersion": apiVersion, "protocol": protocolVersion, "tls": s.cfg.TLS,
	})
}

// originAllowed: el mismo host, wails.localhost o uno de AllowOrigins (docs/14 §5).
func (s *Server) originAllowed(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) || strings.EqualFold(u.Hostname(), "wails.localhost") {
		return true
	}
	o := strings.TrimRight(origin, "/")
	for _, a := range s.cfg.AllowOrigins {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), "/"), o) {
			return true
		}
	}
	return false
}

// middleware: recuperación de pánicos, cabeceras de §8 y CORS.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("pánico en la API", "path", r.URL.Path, "panic", rec)
				writeError(w, http.StatusInternalServerError, codeInternal, genericInternal)
			}
		}()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if s.cfg.TLS {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if origin := r.Header.Get("Origin"); origin != "" && r.URL.Path != apiBase+"/events" {
			if s.originAllowed(r, origin) {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-DConnector-Client, "+connHeader)
				h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
				h.Set("Access-Control-Expose-Headers", "Retry-After, Content-Range, Accept-Ranges, Content-Length")
				h.Set("Access-Control-Max-Age", "600")
			} else if r.Method == http.MethodOptions {
				writeError(w, http.StatusForbidden, codeForbidden, "El origen de la petición no está permitido.")
				return
			}
		}
		if r.Method == http.MethodOptions { // la verificación previa no requiere token
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
