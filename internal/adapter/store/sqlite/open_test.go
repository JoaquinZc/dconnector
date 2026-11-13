package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

func TestOpenPragmasAndMigrationsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	st, err := OpenApp(path)
	require.NoError(t, err)
	app := st.(*AppStore)

	var fk int
	require.NoError(t, app.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk))
	require.Equal(t, 1, fk)
	var mode string
	require.NoError(t, app.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode))
	require.Equal(t, "wal", mode)

	require.NoError(t, app.SetSetting(context.Background(), "k", `"v"`))
	require.NoError(t, st.Close())

	// Reabrir no repite las migraciones ni pierde datos.
	st2, err := OpenApp(path)
	require.NoError(t, err)
	defer st2.Close()
	v, err := st2.GetSetting(context.Background(), "k")
	require.NoError(t, err)
	require.Equal(t, `"v"`, v)
	var n, maxV int
	require.NoError(t, st2.(*AppStore).db.QueryRow(`SELECT COUNT(*), MAX(version) FROM schema_migrations`).Scan(&n, &maxV))
	require.Equal(t, len(appMigrations), n)
	require.Equal(t, len(appMigrations), maxV)
}

func TestMigrateAppliesOnlyNewOnes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, migrate(db, []string{`CREATE TABLE a (id INTEGER)`}))
	require.NoError(t, migrate(db, []string{`CREATE TABLE a (id INTEGER)`, `CREATE TABLE b (id INTEGER)`}))
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n))
	require.Equal(t, 2, n)
	// Una migración que falla se deshace entera.
	err = migrate(db, []string{`CREATE TABLE a (id INTEGER)`, `CREATE TABLE b (id INTEGER)`, `CREATE TABLE c (id INTEGER); CREATE TABLE c (id INTEGER)`})
	require.Error(t, err)
	var cnt int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'c'`).Scan(&cnt))
	require.Equal(t, 0, cnt)
}

func TestOpenAccountSharesDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.db")
	st, db, err := OpenAccount("wa-x", path)
	require.NoError(t, err)
	defer st.Close()
	require.Equal(t, model.AccountID("wa-x"), st.AccountID())

	// La librería del canal puede crear sus tablas en el mismo archivo sin chocar.
	_, err = db.Exec(`CREATE TABLE whatsmeow_device (jid TEXT PRIMARY KEY)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO whatsmeow_device(jid) VALUES('x')`)
	require.NoError(t, err)
	require.NoError(t, st.KVSet(context.Background(), "a", []byte("1")))
}

func TestOpenPathWithSpecialChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "carpeta con espacios #1 100%")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	db, err := Open(filepath.Join(dir, "app.db"))
	require.NoError(t, err)
	defer db.Close()
	var one int
	require.NoError(t, db.QueryRow(`SELECT 1`).Scan(&one))
}

func TestOpenFailsOnBadPath(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "no-existe", "x.db"))
	require.Error(t, err)
}
