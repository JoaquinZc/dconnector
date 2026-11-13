package wails

import (
	"context"
	"time"

	"dconnector/internal/adapter/desktop/connection"
)

// ConnectionInfo: cómo habla la interfaz con el servidor. En modo local Token es el token propio del servidor
// local; en remoto, el token de sesión (el escritorio hace el login). TLSFingerprint es la huella SHA-256
// fijada del servidor remoto («AB:CD:…»), vacía si no hay.
type ConnectionInfo struct {
	// Mode: "local" | "remote".
	Mode           string `json:"mode"`
	BaseURL        string `json:"baseUrl"`
	Token          string `json:"token"`
	User           string `json:"user"`
	TLSFingerprint string `json:"tlsFingerprint"`
	ServerVersion  string `json:"serverVersion"`
}

// connectionAPI: lo que DesktopService necesita del administrador de conexión (*connection.Manager).
type connectionAPI interface {
	Info(ctx context.Context) (connection.Info, error)
	SaveRemote(ctx context.Context, url, user, password string) error
	UseLocal(ctx context.Context) error
	KeepRunning() bool
	SetKeepRunning(bool) error
	TrustTLS(fingerprint string) error
}

// DesktopService: el enlace de la interfaz con la app de escritorio (conexión al servidor local o remoto).
type DesktopService struct{ c connectionAPI }

// NewDesktopService crea el servicio.
func NewDesktopService(c connectionAPI) *DesktopService { return &DesktopService{c: c} }

// ConnectionInfo devuelve la dirección y el token del servidor. En modo local espera a que el servidor esté
// listo; si no lo está o no se puede entrar, devuelve un error en español listo para mostrar. La interfaz
// vuelve a llamarla cuando recibe el evento «desktop:connection» o un 401.
func (s *DesktopService) ConnectionInfo() (ConnectionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	i, err := s.c.Info(ctx)
	if err != nil {
		return ConnectionInfo{}, err
	}
	return ConnectionInfo(i), nil
}

// SaveRemote prueba /health y el login contra el servidor remoto y, si van bien, guarda la conexión (la
// clave cifrada con DPAPI) y cambia a modo remoto. Un certificado sin confiar devuelve un error con su huella
// («Huella SHA-256: AB:CD:…»): confírmala con TrustTLS y vuelve a llamar.
func (s *DesktopService) SaveRemote(url, user, password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.c.SaveRemote(ctx, url, user, password)
}

// UseLocal vuelve al servidor local y lo arranca si hace falta.
func (s *DesktopService) UseLocal() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.c.UseLocal(ctx)
}

// KeepRunningInBackground dice si el servidor local sigue vivo al cerrar el escritorio.
func (s *DesktopService) KeepRunningInBackground() bool { return s.c.KeepRunning() }

// SetKeepRunningInBackground activa o apaga «seguir en segundo plano».
func (s *DesktopService) SetKeepRunningInBackground(v bool) error { return s.c.SetKeepRunning(v) }

// TrustTLS fija la huella del certificado que presentó el servidor remoto (confianza al primer uso). La huella
// debe ser la mostrada por el último intento de conexión; si el certificado cambia más adelante, la conexión
// falla con un error claro hasta que se confirme la huella nueva.
func (s *DesktopService) TrustTLS(fingerprint string) error { return s.c.TrustTLS(fingerprint) }
