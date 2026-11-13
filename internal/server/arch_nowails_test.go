package server

import (
	"os/exec"
	"strings"
	"testing"
)

// El servidor es un binario sin interfaz: ningún paquete suyo (ni sus dependencias transitivas) puede traer
// Wails, la capa de escritorio ni la interfaz de Wails.
func TestServerDoesNotDependOnWailsOrDesktop(t *testing.T) {
	if testing.Short() {
		t.Skip("usa go list")
	}
	roots := []string{
		"dconnector/cmd/dconnector-server",
		"dconnector/internal/server",
		"dconnector/internal/adapter/api/...",
	}
	prohibidos := []string{
		"github.com/wailsapp/",
		"dconnector/internal/adapter/ui/",
		"dconnector/internal/adapter/desktop",
		"dconnector/cmd/dconnector\n",
	}
	args := append([]string{"list", "-deps", "-f", "{{.ImportPath}}"}, roots...)
	out, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("go list falló: %v\n%s", err, out)
	}
	deps := string(out) + "\n"
	for _, p := range prohibidos {
		if strings.Contains(deps, p) {
			for _, l := range strings.Split(deps, "\n") {
				if strings.Contains(l+"\n", p) {
					t.Errorf("el servidor depende de %s", l)
				}
			}
		}
	}
}
