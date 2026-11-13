package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migrate aplica, en orden y cada una en su transacción, las migraciones cuyo índice sea mayor o
// igual que la versión actual (la versión de la migración i es i+1). Nunca se edita una migración
// ya escrita: los cambios van en una nueva.
func migrate(db *sql.DB, migrations []string) error {
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER)`); err != nil {
		return fmt.Errorf("almacén: no se pudo preparar schema_migrations: %w", err)
	}
	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("almacén: no se pudo leer la versión del esquema: %w", err)
	}
	for i := current; i < len(migrations); i++ {
		version := i + 1
		err := inTx(ctx, db, func(tx *sql.Tx) error {
			// Otro proceso pudo aplicarla entre la lectura de la versión y esta transacción (que es inmediata).
			var applied int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
				return err
			}
			if applied > 0 {
				return nil
			}
			if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, time.Now().UnixMilli())
			return err
		})
		if err != nil {
			return fmt.Errorf("almacén: falló la migración %d: %w", version, err)
		}
	}
	return nil
}
