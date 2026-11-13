package core_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const modPath = "dconnector"

// Librerías de canal, de almacén y de interfaz que el núcleo no puede importar (02-ARQUITECTURA §3).
var forbiddenExternal = []string{
	"go.mau.fi/whatsmeow",
	"go.mau.fi/mautrix-meta",
	"maunium.net/go/mautrix",
	"github.com/gotd/",
	"github.com/wailsapp/wails",
	"github.com/coder/websocket",
	"modernc.org/sqlite",
	"database/sql/driver",
}

// Qué paquetes internos de dconnector puede importar cada paquete de core.
var allowedInternal = map[string][]string{
	"model":      {},
	"capability": {},
	"port":       {"core/model", "core/capability"},
	"bus":        {"core/model"},
	"campaign":   {"core/model"},
	"stats":      {"core/model"},
	"fake":       {"core/model", "core/capability", "core/port"},
	"service": {"core/model", "core/capability", "core/port", "core/bus", "core/campaign", "core/stats",
		"paths", "logging", "buildinfo"},
}

func TestCoreDoesNotImportAdaptersOrChannelLibraries(t *testing.T) {
	root := "." // el test corre en internal/core
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		isTest := strings.HasSuffix(path, "_test.go")
		pkgDir := filepath.ToSlash(filepath.Dir(path))
		top := strings.SplitN(pkgDir, "/", 2)[0] // model, port, service…
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, modPath+"/internal/adapter") {
				t.Errorf("%s importa un adaptador: %s", path, p)
			}
			for _, bad := range forbiddenExternal {
				if strings.HasPrefix(p, bad) {
					t.Errorf("%s importa una librería de canal/infra: %s", path, p)
				}
			}
			if isTest || !strings.HasPrefix(p, modPath+"/internal/") {
				continue
			}
			rel := strings.TrimPrefix(p, modPath+"/internal/")
			allowed, known := allowedInternal[top]
			if !known {
				continue
			}
			ok := false
			for _, a := range allowed {
				if rel == a || strings.HasPrefix(rel, a+"/") {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("%s (core/%s) no puede importar %s", path, top, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
