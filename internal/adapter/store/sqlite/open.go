// Package sqlite implementa los almacenes de dConnector (port.AppStore y port.AccountStore)
// sobre SQLite (driver modernc.org/sqlite, sin CGO). Ver docs/05-ALMACEN.md.
package sqlite

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Open abre (o crea) una base SQLite con los pragmas del plan: WAL, claves foráneas,
// espera de 10 s por bloqueos y transacciones inmediatas.
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + escapePath(filepath.ToSlash(path)) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)&_txlock=immediate"
	// Dos procesos que abren a la vez una base NUEVA se disputan el cambio a WAL (pragma journal_mode) y
	// SQLite puede contestar «database is locked» sin respetar la espera: se reintenta unos segundos.
	deadline := time.Now().Add(10 * time.Second)
	for {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("almacén: no se pudo abrir %q: %w", filepath.Base(path), err)
		}
		db.SetMaxOpenConns(8)
		db.SetConnMaxIdleTime(5 * time.Minute)
		err = db.Ping()
		if err == nil {
			return db, nil
		}
		_ = db.Close()
		if !isBusy(err) || time.Now().After(deadline) {
			return nil, fmt.Errorf("almacén: no se pudo abrir %q: %w", filepath.Base(path), err)
		}
		time.Sleep(time.Duration(20+rand.IntN(60)) * time.Millisecond)
	}
}

// isBusy dice si el error es «la base está ocupada por otra conexión» (SQLITE_BUSY o SQLITE_LOCKED).
func isBusy(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return strings.Contains(m, "SQLITE_BUSY") || strings.Contains(m, "SQLITE_LOCKED") || strings.Contains(m, "database is locked")
}

// escapePath protege los caracteres que un URI de SQLite interpretaría ('%', '?' y '#').
func escapePath(p string) string {
	r := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")
	return r.Replace(p)
}

// OpenApp abre app.db y aplica sus migraciones.
func OpenApp(path string) (port.AppStore, error) {
	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	if err := migrate(db, appMigrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return newAppStore(db), nil
}

// OpenAccount abre accounts/<id>/account.db, aplica sus migraciones y devuelve el almacén junto con
// el *sql.DB compartido (la librería del canal crea sus propias tablas en el mismo archivo).
func OpenAccount(id model.AccountID, path string) (port.AccountStore, *sql.DB, error) {
	db, err := Open(path)
	if err != nil {
		return nil, nil, err
	}
	if err := migrate(db, accountMigrations); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return newAccountStore(id, db), db, nil
}
