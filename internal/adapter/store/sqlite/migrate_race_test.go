package sqlite

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Dos manejadores que migran la MISMA base a la vez (el servidor y `user add`, o dos arranques seguidos) no
// deben fallar ni aplicar una migración dos veces.
func TestMigrateConcurrentOpenersAreSafe(t *testing.T) {
	for round := 0; round < 20; round++ {
		path := filepath.Join(t.TempDir(), "app.db")
		const n = 6
		errs := make([]error, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				db, err := Open(path)
				if err != nil {
					errs[i] = err
					return
				}
				defer db.Close()
				<-start
				errs[i] = migrate(db, appMigrations)
			}(i)
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			require.NoError(t, err, "ronda %d, abridor %d", round, i)
		}
		db, err := Open(path)
		require.NoError(t, err)
		var cnt, max int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*), MAX(version) FROM schema_migrations`).Scan(&cnt, &max))
		require.Equal(t, len(appMigrations), cnt)
		require.Equal(t, len(appMigrations), max)
		_ = db.Close()
	}
}
