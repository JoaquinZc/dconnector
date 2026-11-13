// Package paths decide dónde vive cada archivo de datos (02-ARQUITECTURA, 05-ALMACEN §4).
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"dconnector/internal/core/model"
)

const (
	// EnvData: variable de entorno que gana a todo lo demás (para pruebas).
	EnvData = "DCONNECTOR_DATA"
	// PortableMarker: archivo junto al .exe que activa el modo portable.
	PortableMarker = "portable.txt"
	appFolder      = "dConnector"
)

var (
	overrideDir string
	mu          sync.RWMutex
	dataDir     string
	portable    bool
)

// Init resuelve la carpeta de datos y crea la estructura básica. Vacía tmp/.
func Init() error {
	dir, port, err := resolve()
	if err != nil {
		return err
	}
	return InitAt(dir, port)
}

// InitAt fija la carpeta de datos a mano (pruebas y --selftest).
func InitAt(dir string, isPortable bool) error { return initAt(dir, isPortable, true) }

// Resolve devuelve la carpeta de datos que Init usaría, sin crear ni vaciar nada (para tomar un candado
// sobre ella ANTES de que Init vacíe tmp/).
func Resolve() (dir string, isPortable bool, err error) {
	dir, isPortable, err = resolve()
	if err != nil {
		return "", false, err
	}
	abs, aerr := filepath.Abs(dir)
	if aerr != nil {
		return "", false, fmt.Errorf("paths: carpeta de datos: %w", aerr)
	}
	return abs, isPortable, nil
}

// InitKeepTmp es Init sin vaciar tmp/: lo usa quien comparte la carpeta con un servidor que sigue vivo
// (el escritorio con «seguir en segundo plano»), cuyos archivos temporales en curso no deben borrarse.
func InitKeepTmp() error {
	dir, port, err := resolve()
	if err != nil {
		return err
	}
	return initAt(dir, port, false)
}

func initAt(dir string, isPortable, wipeTmp bool) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("paths: carpeta de datos: %w", err)
	}
	mu.Lock()
	dataDir, portable = abs, isPortable
	mu.Unlock()
	for _, d := range []string{abs, LogsDir(), filepath.Join(abs, "accounts"), TmpDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("paths: crear %s: %w", d, err)
		}
	}
	// tmp/ se vacía al arrancar.
	if !wipeTmp {
		return nil
	}
	if entries, err := os.ReadDir(TmpDir()); err == nil {
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(TmpDir(), e.Name()))
		}
	}
	return nil
}

// SetOverride fija la carpeta de datos antes de Init (indicador --data del servidor); gana a la variable de entorno.
func SetOverride(dir string) { mu.Lock(); overrideDir = dir; mu.Unlock() }

func resolve() (string, bool, error) {
	mu.RLock()
	ov := overrideDir
	mu.RUnlock()
	if ov != "" {
		return ov, false, nil
	}
	if v := os.Getenv(EnvData); v != "" {
		return v, false, nil
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(exeDir, PortableMarker)); err == nil {
			return filepath.Join(exeDir, "data"), true, nil
		}
	}
	return defaultDir()
}

// DataDir es la carpeta raíz de datos (vacía si Init no se ha llamado).
func DataDir() string { mu.RLock(); defer mu.RUnlock(); return dataDir }

// IsPortable indica si se está en modo portable.
func IsPortable() bool { mu.RLock(); defer mu.RUnlock(); return portable }

func AppDBPath() string        { return filepath.Join(DataDir(), "app.db") }
func LogsDir() string          { return filepath.Join(DataDir(), "logs") }
func TmpDir() string           { return filepath.Join(DataDir(), "tmp") }
func CampaignMediaDir() string { return filepath.Join(DataDir(), "campaign-media") }
func StatusMediaDir() string   { return filepath.Join(DataDir(), "status-media") }

func AccountDir(id model.AccountID) string {
	return filepath.Join(DataDir(), "accounts", string(id))
}
func AccountDBPath(id model.AccountID) string { return filepath.Join(AccountDir(id), "account.db") }
func MediaDir(id model.AccountID) string      { return filepath.Join(AccountDir(id), "media") }
func AvatarDir(id model.AccountID) string     { return filepath.Join(AccountDir(id), "avatars") }
