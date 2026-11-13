//go:build !windows

package wails

// SetAutostart no hace nada fuera de Windows.
func SetAutostart(enabled bool, exePath string, minimized bool) error { return nil }

// IsAutostart siempre es falso fuera de Windows.
func IsAutostart(exePath string, minimized bool) bool { return false }
