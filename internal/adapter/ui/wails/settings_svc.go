package wails

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"dconnector/internal/paths"
)

// SettingsService: lo del lado de esta máquina de «Ajustes». Los ajustes en sí viven en el servidor
// (RPC Settings.Get/Save); abrir las carpetas solo tiene sentido en modo local (docs/14 §4.3).
type SettingsService struct{}

// OpenDataDir abre la carpeta de datos en el Explorador.
func (s *SettingsService) OpenDataDir() error {
	return application.Get().Browser.OpenFile(paths.DataDir())
}

// OpenLogsDir abre la carpeta de registros en el Explorador.
func (s *SettingsService) OpenLogsDir() error {
	return application.Get().Browser.OpenFile(paths.LogsDir())
}
