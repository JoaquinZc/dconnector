package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInitCreatesTodaysFileAndPurgesOld(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "dconnector-"+time.Now().AddDate(0, 0, -30).Format("2006-01-02")+".log")
	recent := filepath.Join(dir, "dconnector-"+time.Now().AddDate(0, 0, -3).Format("2006-01-02")+".log")
	require.NoError(t, os.WriteFile(old, []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(recent, []byte("x"), 0o644))

	closeFn, err := Init(dir)
	require.NoError(t, err)
	defer closeFn()

	require.NoFileExists(t, old)
	require.FileExists(t, recent)
	require.FileExists(t, filepath.Join(dir, "dconnector-"+time.Now().Format("2006-01-02")+".log"))
}

func TestLast4(t *testing.T) {
	require.Equal(t, "…6789", Last4("123456789"))
	require.Equal(t, "***", Last4("abc"))
}
