package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// dataLockName es el candado de la carpeta de datos: un solo servidor por carpeta (dos procesos escribiendo
// en el mismo app.db y los mismos account.db corromperían el estado). El sistema operativo lo suelta solo
// si el proceso muere, así que un fallo nunca deja un candado obsoleto.
const dataLockName = "dconnector-server.lock"

// ErrDataDirInUse: otro servidor ya usa la carpeta de datos.
var ErrDataDirInUse = errors.New("Ya hay un servidor usando esta carpeta de datos")

// lockDataDir toma el candado exclusivo de dir (la crea si hace falta). Devuelve una función que lo suelta.
// Si otro proceso lo tiene devuelve un error que envuelve ErrDataDirInUse, con su pid si se pudo leer.
func lockDataDir(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo crear la carpeta de datos %s: %w", dir, err)
	}
	path := filepath.Join(dir, dataLockName)
	unlock, err := lockFile(path)
	if err != nil {
		if errors.Is(err, errLockHeld) {
			who := ""
			if b, rerr := os.ReadFile(path); rerr == nil {
				if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil && pid > 0 {
					who = fmt.Sprintf(" (proceso %d)", pid)
				}
			}
			return nil, fmt.Errorf("%w%s: %s. Cierre el otro servidor o use otra carpeta con --data", ErrDataDirInUse, who, dir)
		}
		return nil, fmt.Errorf("no se pudo bloquear la carpeta de datos %s: %w", dir, err)
	}
	// Anota el pid para el mensaje de quien llegue después (el candado va en una zona del archivo que no
	// se lee, así que leerlo desde otro proceso funciona también en Windows).
	if f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
		_ = f.Close()
	}
	return unlock, nil
}

var errLockHeld = errors.New("candado ocupado")
