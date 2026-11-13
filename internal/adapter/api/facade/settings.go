package facade

import (
	"context"

	"dconnector/internal/buildinfo"
	"dconnector/internal/core/service"
	"dconnector/internal/paths"
)

// Settings: ajustes y versión (sin OpenDataDir/OpenLogsDir: son de escritorio, docs/14 §4.3).
type Settings struct {
	s       *service.Settings
	onSaved func(service.AppSettings)
}

func (s *Settings) Get(ctx context.Context) (service.AppSettings, error) {
	return s.s.All(ctx)
}

func (s *Settings) Save(ctx context.Context, v service.AppSettings) error {
	if err := s.s.Save(ctx, v); err != nil {
		return err
	}
	if s.onSaved != nil {
		s.onSaved(v)
	}
	return nil
}

// DataDir devuelve la carpeta de datos del servidor (informativo).
func (s *Settings) DataDir() string { return paths.DataDir() }

// Version devuelve la versión del servidor.
func (s *Settings) Version() string { return buildinfo.Version }
