package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Proceso auxiliar: toma el candado de la carpeta, avisa con «locked» y lo mantiene hasta que se cierre su
// entrada estándar (así la prueba controla cuándo muere).
func TestMain(m *testing.M) {
	if dir := os.Getenv("DC_LOCK_HELPER_DIR"); dir != "" {
		unlock, err := lockDataDir(dir)
		if err != nil {
			os.Stdout.WriteString("error: " + err.Error() + "\n")
			os.Exit(3)
		}
		os.Stdout.WriteString("locked\n")
		_, _ = io.Copy(io.Discard, os.Stdin)
		unlock()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func startLockHelper(t *testing.T, dir string) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	cmd.Env = append(os.Environ(), "DC_LOCK_HELPER_DIR="+dir)
	in, err := cmd.StdinPipe()
	require.NoError(t, err)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	return cmd, in, bufio.NewReader(out)
}

func TestLockDataDirExcludesSecondInProcess(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockDataDir(dir)
	require.NoError(t, err)
	_, err = lockDataDir(dir)
	require.ErrorIs(t, err, ErrDataDirInUse)
	require.Contains(t, err.Error(), "Ya hay un servidor usando esta carpeta de datos")
	require.Contains(t, err.Error(), "proceso")
	unlock()
	unlock2, err := lockDataDir(dir)
	require.NoError(t, err, "tras soltar, se puede volver a tomar")
	unlock2()
}

func TestLockDataDirTwoProcessesAndStaleAfterKill(t *testing.T) {
	dir := t.TempDir()
	cmd, in, out := startLockHelper(t, dir)
	line, err := out.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked", strings.TrimSpace(line))

	_, err = lockDataDir(dir)
	require.ErrorIs(t, err, ErrDataDirInUse, "otro proceso lo tiene")

	// Muerte a mala fuerza: el candado no queda obsoleto.
	require.NoError(t, cmd.Process.Kill())
	_ = in.Close()
	_ = cmd.Wait()
	var unlock func()
	require.Eventually(t, func() bool {
		unlock, err = lockDataDir(dir)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "el candado sigue ocupado tras matar al dueño: %v", err)
	unlock()
}

func TestRunRefusesDataDirInUse(t *testing.T) {
	cfg := localCfg(t)
	cmd, in, out := startLockHelper(t, cfg.Data)
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Wait() })
	line, err := out.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked", strings.TrimSpace(line))

	// Un archivo temporal del primer servidor no se toca si llega un segundo.
	tmp := cfg.Data + string(os.PathSeparator) + "tmp"
	require.NoError(t, os.MkdirAll(tmp, 0o755))
	keep := tmp + string(os.PathSeparator) + "up-en-curso"
	require.NoError(t, os.WriteFile(keep, []byte("x"), 0o600))

	err = Run(context.Background(), cfg, io.Discard, io.Discard)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrDataDirInUse), err.Error())
	_, statErr := os.Stat(keep)
	require.NoError(t, statErr, "el segundo servidor borró tmp/ del primero")
}
