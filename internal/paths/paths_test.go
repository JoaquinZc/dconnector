package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitAtCreatesStructureAndClearsTmp(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tmp", "x.bin"), []byte("x"), 0o644))

	require.NoError(t, InitAt(dir, false))
	require.DirExists(t, LogsDir())
	require.DirExists(t, filepath.Join(DataDir(), "accounts"))
	require.NoFileExists(t, filepath.Join(TmpDir(), "x.bin"))
	require.Equal(t, filepath.Join(DataDir(), "app.db"), AppDBPath())
	require.Equal(t, filepath.Join(DataDir(), "accounts", "wa-1", "account.db"), AccountDBPath("wa-1"))
	require.Equal(t, filepath.Join(DataDir(), "accounts", "wa-1", "media"), MediaDir("wa-1"))
	require.Equal(t, filepath.Join(DataDir(), "accounts", "wa-1", "avatars"), AvatarDir("wa-1"))
	require.False(t, IsPortable())
}

func TestEnvOverridesEverything(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvData, dir)
	require.NoError(t, Init())
	abs, _ := filepath.Abs(dir)
	require.Equal(t, abs, DataDir())
}
