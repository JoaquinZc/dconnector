//go:build windows

package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// defaultDir: %APPDATA%\dConnector (igual que en 0.2.x: los datos existentes siguen funcionando).
func defaultDir() (string, bool, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", false, fmt.Errorf("paths: no se encontró la carpeta de configuración del usuario: %w", err)
	}
	return filepath.Join(cfg, appFolder), false, nil
}
