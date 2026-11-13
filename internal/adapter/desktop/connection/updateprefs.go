package connection

import (
	"time"
)

// UpdatePrefs expone, sobre el mismo desktop.json, lo que el actualizador recuerda: si busca al abrir y cuándo
// fue la última comprobación. Cumple la interfaz updater.Prefs.
type UpdatePrefs struct{ s *Store }

// NewUpdatePrefs envuelve el almacén de configuración.
func NewUpdatePrefs(s *Store) UpdatePrefs { return UpdatePrefs{s: s} }

// AutoCheck dice si se busca al abrir (por defecto sí).
func (p UpdatePrefs) AutoCheck() bool {
	v := p.s.Get().UpdateAutoCheck
	return v == nil || *v
}

// SetAutoCheck activa o apaga la búsqueda al abrir.
func (p UpdatePrefs) SetAutoCheck(on bool) error {
	return p.s.Update(func(c *Config) { c.UpdateAutoCheck = &on })
}

// LastCheck devuelve la última comprobación (cero si nunca).
func (p UpdatePrefs) LastCheck() time.Time {
	ms := p.s.Get().UpdateLastCheck
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// SetLastCheck guarda la hora de la última comprobación.
func (p UpdatePrefs) SetLastCheck(t time.Time) error {
	return p.s.Update(func(c *Config) { c.UpdateLastCheck = t.UnixMilli() })
}
