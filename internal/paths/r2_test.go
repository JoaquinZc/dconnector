package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitKeepTmpDoesNotClearAndResolveDoesNotCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "datos")
	t.Setenv(EnvData, dir)
	SetOverride("")
	got, port, err := Resolve()
	require.NoError(t, err)
	require.False(t, port)
	require.Equal(t, dir, got)
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err), "Resolve no debe crear nada")

	require.NoError(t, InitAt(dir, false))
	keep := filepath.Join(TmpDir(), "up-en-curso")
	require.NoError(t, os.WriteFile(keep, []byte("x"), 0o600))
	require.NoError(t, InitKeepTmp())
	_, err = os.Stat(keep)
	require.NoError(t, err, "InitKeepTmp no debe vaciar tmp/")
	require.NoError(t, Init())
	_, err = os.Stat(keep)
	require.True(t, os.IsNotExist(err), "Init sigue vaciando tmp/")
}
