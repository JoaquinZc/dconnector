//go:build windows

package wails

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue  = "dConnector"
	flagStart = " --minimized"
)

// autostartCommand: la línea de comando del arranque con Windows ("<exe>" y, si se pide, --minimized).
func autostartCommand(exePath string, minimized bool) string {
	cmd := `"` + exePath + `"`
	if minimized {
		cmd += flagStart
	}
	return cmd
}

// SetAutostart escribe o borra HKCU\…\Run\dConnector (11 §4). Con minimized el arranque va
// directo a la bandeja (`--minimized`): es el ajuste «Iniciar minimizado».
func SetAutostart(enabled bool, exePath string, minimized bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("no se pudo abrir el registro de Windows: %w", err)
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("no se pudo quitar el arranque con Windows: %w", err)
		}
		return nil
	}
	if err := k.SetStringValue(runValue, autostartCommand(exePath, minimized)); err != nil {
		return fmt.Errorf("no se pudo activar el arranque con Windows: %w", err)
	}
	return nil
}

// IsAutostart indica si el valor existe y apunta a esta ruta con esa opción.
func IsAutostart(exePath string, minimized bool) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValue)
	return err == nil && v == autostartCommand(exePath, minimized)
}
