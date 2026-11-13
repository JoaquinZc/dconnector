package tlsconf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Una clave propia que quedó legible por otros (copia o restauración) se cierra al arrancar, sin cambiar la huella.
func TestClavePropiaLegibleSeCierra(t *testing.T) {
	if isWindows() {
		t.Skip("los permisos POSIX no existen en Windows")
	}
	dir := t.TempDir()
	_, fp, err := Load(Options{DataDir: dir})
	require.NoError(t, err)
	key := filepath.Join(dir, "tls", ownKeyFile)
	require.NoError(t, os.Chmod(key, 0o644))
	_, fp2, err := Load(Options{DataDir: dir})
	require.NoError(t, err)
	require.Equal(t, fp, fp2, "la huella no cambia")
	st, err := os.Stat(key)
	require.NoError(t, err)
	require.Zero(t, st.Mode().Perm()&0o077)
}
