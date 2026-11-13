// Package integration prueba el escritorio contra el servidor REAL compilado (dConnector-server): el ciclo
// del lanzador y el cliente en modo remoto con TLS autofirmado. Compila el servidor una vez; con -short se
// omite. Todo corre en carpetas temporales: nunca toca la carpeta de datos real.
package integration

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var serverExe string

func TestMain(m *testing.M) {
	flag.Parse()
	code := 1
	func() {
		if testing.Short() {
			code = 0
			return
		}
		dir, err := os.MkdirTemp("", "dc-integration-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		defer os.RemoveAll(dir)
		name := "dConnector-server"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		serverExe = filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", serverExe, "dconnector/cmd/dconnector-server")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "no se pudo compilar el servidor: %v\n%s\n", err, out)
			return
		}
		code = m.Run()
	}()
	os.Exit(code)
}

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integración con el servidor real")
	}
}
