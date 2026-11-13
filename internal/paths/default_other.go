//go:build !windows

package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// defaultDir: $XDG_DATA_HOME/dconnector o ~/.local/share/dconnector. En un servidor se usa --data
// (p. ej. /var/lib/dconnector) o DCONNECTOR_DATA.
func defaultDir() (string, bool, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "dconnector"), false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("paths: no se encontró la carpeta personal del usuario (use --data): %w", err)
	}
	return filepath.Join(home, ".local", "share", "dconnector"), false, nil
}
