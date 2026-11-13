// Package connection decide a qué servidor habla la app de escritorio (el local que ella lanza o uno remoto),
// guarda esa configuración en <data>/desktop.json (la clave remota solo cifrada con DPAPI) y fija la huella
// del certificado de cada servidor remoto (confianza al primer uso).
package connection

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Modos de conexión.
const (
	ModeLocal  = "local"
	ModeRemote = "remote"
)

// RemoteConfig: el servidor remoto elegido. Password es la clave cifrada (base64 de un bloque DPAPI):
// nunca va en claro.
type RemoteConfig struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
}

// Config es el contenido de desktop.json.
type Config struct {
	Mode string `json:"mode"`
	// KeepRunning: «seguir en segundo plano» (el servidor local sigue vivo al cerrar el escritorio).
	KeepRunning bool         `json:"keepRunningInBackground"`
	Remote      RemoteConfig `json:"remote"`
	// TLSPins: huella SHA-256 (hex en minúsculas) fijada por servidor («host:puerto»).
	TLSPins map[string]string `json:"tlsPins,omitempty"`
	// NotificationsAsked: ya se comprobó el permiso de avisos en el primer arranque.
	NotificationsAsked bool `json:"notificationsAsked,omitempty"`
}

// Store lee y guarda desktop.json.
type Store struct {
	path string
	mu   sync.Mutex
	cfg  Config
}

// OpenStore carga <dataDir>/desktop.json (si no existe o está dañado, empieza con modo local).
func OpenStore(dataDir string) (*Store, error) {
	s := &Store{path: filepath.Join(dataDir, "desktop.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(b, &s.cfg); err != nil {
			// Dañado: se arranca en modo local, pero el archivo no se pierde (puede llevar la clave remota
			// cifrada y las huellas fijadas): queda como desktop.json.dañado para recuperarlo a mano.
			_ = os.Rename(s.path, s.path+".dañado")
			s.cfg = Config{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no se pudo leer la configuración de conexión: %w", err)
	}
	if s.cfg.Mode != ModeRemote {
		s.cfg.Mode = ModeLocal
	}
	return s, nil
}

// Get devuelve una copia de la configuración.
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.copy()
}

func (s *Store) copy() Config {
	c := s.cfg
	if s.cfg.TLSPins != nil {
		c.TLSPins = make(map[string]string, len(s.cfg.TLSPins))
		for k, v := range s.cfg.TLSPins {
			c.TLSPins[k] = v
		}
	}
	return c
}

// Update aplica un cambio y lo guarda de forma atómica (archivo temporal + renombrar).
func (s *Store) Update(mut func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.copy()
	mut(&next)
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("no se pudo guardar la configuración de conexión: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("no se pudo guardar la configuración de conexión: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("no se pudo guardar la configuración de conexión: %w", err)
	}
	s.cfg = next
	return nil
}

// NormalizeURL deja la dirección del servidor como «esquema://host[:puerto]» (sin ruta ni barra final).
// Sin esquema se asume https. http solo se acepta en loopback: la clave no puede viajar sin cifrar.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", userError("Escribe la dirección del servidor.")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", userError("La dirección del servidor no es válida.")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", userError("La dirección del servidor no debe llevar usuario, consulta ni fragmento.")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if ip := net.ParseIP(u.Hostname()); !(u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
			return "", userError("Usa https:// para conectarte a un servidor remoto: la clave no puede viajar sin cifrar.")
		}
	default:
		return "", userError("La dirección del servidor debe empezar por https://.")
	}
	return u.Scheme + "://" + u.Host, nil
}

// HostPort devuelve «host:puerto» (con el puerto por defecto del esquema) de una URL ya normalizada.
func HostPort(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// userError es un mensaje para la persona (con mayúscula y punto final), listo para mostrar tal cual.
type userError string

func (e userError) Error() string { return string(e) }
