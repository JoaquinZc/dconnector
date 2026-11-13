// Package api: servidor HTTP(S) + WebSocket de dConnector (docs/14-API-SERVIDOR.md).
// Las firmas públicas de este archivo son la costura con los demás paquetes (cmd, auth, escritorio): los
// cambios son solo aditivos.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"dconnector/internal/adapter/api/facade"
	"dconnector/internal/app"
	"dconnector/internal/core/port"
	"dconnector/internal/core/service"
)

// Auth es lo que la API necesita de la seguridad (lo implementa *auth.Service del agente B).
type Auth interface {
	// RequireSession exige un token de sesión (Authorization: Bearer) o el token local por loopback.
	RequireSession(next http.Handler) http.Handler
	// RequireSessionOrMedia además admite ?token=<media-token> (solo para GET de medios, avatares y archivos).
	RequireSessionOrMedia(next http.Handler) http.Handler
	// RedeemWSTicket consume un ticket de un solo uso y devuelve el usuario.
	RedeemWSTicket(ticket string) (username string, ok bool)
	// Username devuelve el usuario autenticado de una petición ya validada ("" si no hay).
	Username(r *http.Request) string
	// IsLocal dice si la petición viene con el token local (modo local, solo loopback): puede usar rutas absolutas.
	IsLocal(r *http.Request) bool
}

// Revoker es una capacidad OPCIONAL de Auth (aditiva): avisa cuando se revocan las sesiones de un usuario
// (logout o cambio de clave) para que la API cierre sus WebSocket con el código 4401 (docs/14 §5).
type Revoker interface {
	// OnRevoke registra la función a la que se avisa con el nombre del usuario.
	OnRevoke(fn func(username string))
}

// Config agrupa lo que necesita el servidor de la API.
type Config struct {
	Core         *app.Core
	Auth         Auth
	AuthRoutes   http.Handler // se monta en /api/v1/auth/ (lo da auth.Service.Handler())
	AllowOrigins []string     // orígenes extra permitidos (CORS y WebSocket)
	Version      string
	TLS          bool
	// Ready devuelve "starting", "ok" o "stopping" (para /health). Nil = siempre "ok".
	Ready func() string

	// Aditivos (todos opcionales).

	// Notifications: el servicio de avisos. Suele crearse después de la API (necesita Server.Notifier()),
	// en cuyo caso se conecta con Server.SetNotifications.
	Notifications *service.Notifications
	// OnSettingsSaved se llama tras guardar ajustes (Settings.Save) con éxito.
	OnSettingsSaved func(service.AppSettings)
	// Log: registro del servidor (por defecto slog.Default()).
	Log *slog.Logger
}

// Server es el manejador HTTP de la API.
type Server struct {
	cfg    Config
	log    *slog.Logger
	facade *facade.Facade
	rpc    map[string]map[string]*rpcMethod
	hub    *hub
	files  *uploads
	media  mediaResolver
	note   *notifier

	handler http.Handler

	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	cleanups []func()
	once     sync.Once
}

// New crea la API y arranca sus tareas de fondo (puente de eventos, limpieza de subidas).
func New(cfg Config) (*Server, error) { return newServer(cfg) }

// Handler devuelve el manejador raíz (todas las rutas /api/v1/…).
func (s *Server) Handler() http.Handler { return s.handler }

// Notifier devuelve el port.Notifier que convierte los avisos del núcleo en eventos `notify` del WebSocket.
func (s *Server) Notifier() port.Notifier { return s.note }

// SetNotifications conecta el servicio de avisos creado con Server.Notifier(): el foco de las conexiones
// (Notifications.SetFocus) se traslada a él y sus decisiones salen como eventos `notify:decision`.
func (s *Server) SetNotifications(n *service.Notifications) {
	s.facade.SetNotifications(n)
	s.note.setService(n)
}

// Close cierra los WebSocket (código 1001) y detiene las tareas de fondo.
func (s *Server) Close() {
	s.once.Do(func() {
		s.hub.closeAll() // 1001 a cada WebSocket antes de cortar los contextos
		s.cancel()
		s.wg.Wait()
		for _, f := range s.cleanups {
			f()
		}
	})
}
