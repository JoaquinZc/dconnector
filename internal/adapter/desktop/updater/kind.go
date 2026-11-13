package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Kind es de dónde corre el ejecutable.
type Kind string

const (
	// KindMSI: instalado con el MSI por usuario (%LOCALAPPDATA%\Programs\dConnector).
	KindMSI Kind = "msi"
	// KindPortable: carpeta portable (hay portable.txt junto al ejecutable).
	KindPortable Kind = "portable"
	// KindOther: desarrollo, bin\ u otra ubicación: solo se informa.
	KindOther Kind = "other"
)

// PortableMarker es el archivo que marca una carpeta portable (internal/paths).
const PortableMarker = "portable.txt"

// ArchName traduce GOARCH al nombre que llevan los archivos de la Release. 386 no se ofrece.
func ArchName(goarch string) (string, bool) {
	switch goarch {
	case "amd64":
		return "x64", true
	case "arm64":
		return "arm64", true
	}
	return "", false
}

// KindProbe agrupa lo que se mira del sistema (sustituible en pruebas).
type KindProbe struct {
	// LocalAppData es %LOCALAPPDATA%.
	LocalAppData string
	// RegistryInstalled dice si existe HKCU\Software\Darkmelon\dConnector\installed.
	RegistryInstalled func() bool
}

// DetectKind decide el tipo de instalación a partir de la ruta del ejecutable.
func DetectKind(exe string, p KindProbe) Kind {
	if exe == "" {
		return KindOther
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, PortableMarker)); err == nil {
		return KindPortable
	}
	if p.LocalAppData != "" && p.RegistryInstalled != nil {
		want := filepath.Join(p.LocalAppData, "Programs", "dConnector")
		if sameDir(dir, want) && p.RegistryInstalled() {
			return KindMSI
		}
	}
	return KindOther
}

// SystemKind es DetectKind con el sistema real.
func SystemKind(exe string) Kind {
	return DetectKind(exe, KindProbe{LocalAppData: os.Getenv("LOCALAPPDATA"), RegistryInstalled: registryInstalled})
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	if r, err := filepath.EvalSymlinks(b); err == nil {
		b = r
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
