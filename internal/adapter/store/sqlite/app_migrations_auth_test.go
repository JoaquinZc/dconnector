package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// La migración de usuarios y tokens es aditiva: no toca las tablas anteriores y las nuevas existen.
func TestMigracionUsuariosYTokensEsAditiva(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	defer db.Close()

	// Una carpeta de 0.2.x: solo las migraciones anteriores a la de usuarios, con datos.
	const authMigration = 2 // índice (0-based) de la migración de usuarios
	require.Equal(t, authMigration+1, len(appMigrations), "la migración de usuarios es la última")
	require.NoError(t, migrate(db, appMigrations[:authMigration]))
	_, err = db.Exec(`INSERT INTO settings(key, value) VALUES('k','"v"')`)
	require.NoError(t, err)
	var before []string
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	require.NoError(t, err)
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		before = append(before, n)
	}
	require.NoError(t, rows.Close())

	// Actualizar a 0.3.0.
	require.NoError(t, migrate(db, appMigrations))
	var v string
	require.NoError(t, db.QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&v))
	require.Equal(t, `"v"`, v)
	for _, tbl := range before {
		var n int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n))
		require.Equal(t, 1, n, tbl)
	}
	for _, tbl := range []string{"users", "auth_tokens"} {
		var n int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n))
		require.Equal(t, 1, n, tbl)
	}

	// Claves foráneas y unicidad sin distinguir mayúsculas.
	_, err = db.Exec(`INSERT INTO users(id,username,password_hash,created_at) VALUES('1','Ana','h',1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users(id,username,password_hash,created_at) VALUES('2','ANA','h',1)`)
	require.Error(t, err)
	_, err = db.Exec(`INSERT INTO auth_tokens(token_hash,user_id,kind,created_at,last_used_at,expires_at,hard_expires_at) VALUES('t','nadie','session',1,1,1,1)`)
	require.Error(t, err)
	// Reaplicar no hace nada.
	require.NoError(t, migrate(db, appMigrations))
}
