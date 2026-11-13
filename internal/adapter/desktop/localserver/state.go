package localserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var errLocked = errors.New("otro dConnector está arrancando el servidor local")

const (
	runDirName    = "run"
	stateFileName = "localserver.json"
	lockFileName  = "localserver.lock"
)

// Record es lo que se guarda en <data>/run/localserver.json para reutilizar un servidor que sigue vivo
// (opción «seguir en segundo plano»). Contiene el token propio: la carpeta es solo del usuario.
type Record struct {
	PID       int    `json:"pid"`
	Addr      string `json:"addr"`
	Token     string `json:"token"`
	Version   string `json:"version"`
	Exe       string `json:"exe"`
	StartedAt int64  `json:"startedAt"`
}

// RunDir es la carpeta con ACL restringida donde viven el registro y el candado.
func RunDir(dataDir string) string { return filepath.Join(dataDir, runDirName) }

func statePath(dataDir string) string { return filepath.Join(RunDir(dataDir), stateFileName) }

// ensureRunDir crea la carpeta y la deja accesible solo al usuario actual.
func ensureRunDir(dataDir string) error {
	dir := RunDir(dataDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("no se pudo crear la carpeta del servidor local: %w", err)
	}
	if err := restrictDir(dir); err != nil {
		return fmt.Errorf("no se pudo restringir el acceso a la carpeta del servidor local: %w", err)
	}
	return nil
}

func readRecord(dataDir string) (Record, bool) {
	b, err := os.ReadFile(statePath(dataDir))
	if err != nil {
		return Record{}, false
	}
	var r Record
	if json.Unmarshal(b, &r) != nil || r.Addr == "" || r.Token == "" || r.PID <= 0 {
		return Record{}, false
	}
	return r, true
}

func writeRecord(dataDir string, r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp := statePath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, statePath(dataDir))
}

func removeRecord(dataDir string) { _ = os.Remove(statePath(dataDir)) }

// acquireStartLock espera hasta wait por el candado de arranque (un solo servidor por carpeta de datos).
func acquireStartLock(dataDir string, wait time.Duration) (func(), error) {
	path := filepath.Join(RunDir(dataDir), lockFileName)
	deadline := time.Now().Add(wait)
	for {
		unlock, err := lockFile(path)
		if err == nil {
			return unlock, nil
		}
		if !errors.Is(err, errLocked) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, errLocked
		}
		time.Sleep(100 * time.Millisecond)
	}
}
