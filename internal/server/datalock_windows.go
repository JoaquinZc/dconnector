//go:build windows

package server

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile toma un candado exclusivo sin esperar. El rango bloqueado está lejos del contenido (offset alto)
// para que otros procesos puedan leer el pid escrito en el archivo.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ol := &windows.Overlapped{OffsetHigh: 0x7FFF}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol); err != nil {
		_ = f.Close()
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, errLockHeld
		}
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
		_ = f.Close()
	}, nil
}
