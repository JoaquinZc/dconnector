package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func env(map[string]string) func(string) string { return func(string) string { return "" } }

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 0, run([]string{"--version"}, env(nil), strings.NewReader(""), &out, &errOut))
	require.NotEmpty(t, strings.TrimSpace(out.String()))
}

func TestVersionToFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "v.txt")
	var out, errOut bytes.Buffer
	require.Equal(t, 0, run([]string{"--version", "--out", f}, env(nil), strings.NewReader(""), &out, &errOut))
	b, err := os.ReadFile(f)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(b)))
}

func TestBadFlagAndSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 2, run([]string{"--nada"}, env(nil), strings.NewReader(""), &out, &errOut))
	errOut.Reset()
	require.Equal(t, 2, run([]string{"raro"}, env(nil), strings.NewReader(""), &out, &errOut))
	require.Contains(t, errOut.String(), "subcomando desconocido")
}

func TestHealthcheckSubcommandFailsWithoutServer(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 1, run([]string{"--listen", "127.0.0.1:1", "healthcheck"}, env(nil), strings.NewReader(""), &out, &errOut))
}
