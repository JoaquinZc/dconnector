package updater

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// FlagApply es la bandera oculta que convierte dConnector.exe en el ayudante de instalación.
const FlagApply = "--apply-update"

// ApplyArgs son los parámetros del ayudante.
type ApplyArgs struct {
	// Kind: "msi" o "portable".
	Kind string
	// File: instalador (.msi) o zip descargado y verificado.
	File string
	// SHA256: hash esperado de File; el ayudante lo vuelve a comprobar antes de ejecutar o extraer.
	SHA256 string
	// ParentPID: la aplicación que se cierra (el ayudante espera a que termine).
	ParentPID int
	// Relaunch: ejecutable que se vuelve a abrir al terminar.
	Relaunch string
	// Target: carpeta de destino del portable.
	Target string
	// ParentData: carpeta de datos (de ahí cuelga el registro del ayudante).
	ParentData string
	// Minimized: relanzar con --minimized.
	Minimized bool
}

// IsApplyInvocation dice si la línea de comandos pide el modo ayudante.
func IsApplyInvocation(args []string) bool {
	for _, a := range args {
		if a == FlagApply {
			return true
		}
	}
	return false
}

// BuildApplyArgs arma la línea de comandos del ayudante.
func BuildApplyArgs(a ApplyArgs) []string {
	out := []string{
		FlagApply,
		"--kind", a.Kind,
		"--file", a.File,
		"--sha256", a.SHA256,
		"--parent-pid", strconv.Itoa(a.ParentPID),
		"--relaunch", a.Relaunch,
		"--parent-data", a.ParentData,
	}
	if a.Target != "" {
		out = append(out, "--target", a.Target)
	}
	if a.Minimized {
		out = append(out, "--relaunch-minimized")
	}
	return out
}

// ParseApplyArgs lee y valida la línea de comandos del ayudante.
func ParseApplyArgs(args []string) (ApplyArgs, error) {
	fs := flag.NewFlagSet("apply-update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var a ApplyArgs
	var apply bool
	fs.BoolVar(&apply, "apply-update", false, "")
	fs.StringVar(&a.Kind, "kind", "", "")
	fs.StringVar(&a.File, "file", "", "")
	fs.StringVar(&a.SHA256, "sha256", "", "")
	fs.IntVar(&a.ParentPID, "parent-pid", 0, "")
	fs.StringVar(&a.Relaunch, "relaunch", "", "")
	fs.StringVar(&a.Target, "target", "", "")
	fs.StringVar(&a.ParentData, "parent-data", "", "")
	fs.BoolVar(&a.Minimized, "relaunch-minimized", false, "")
	if err := fs.Parse(args); err != nil {
		return ApplyArgs{}, fmt.Errorf("argumentos no válidos: %w", err)
	}
	if !apply {
		return ApplyArgs{}, errors.New("falta " + FlagApply)
	}
	switch a.Kind {
	case string(KindMSI), string(KindPortable):
	default:
		return ApplyArgs{}, fmt.Errorf("tipo de instalación no válido: %q", a.Kind)
	}
	if !filepath.IsAbs(a.File) || !filepath.IsAbs(a.Relaunch) || !filepath.IsAbs(a.ParentData) {
		return ApplyArgs{}, errors.New("las rutas deben ser absolutas")
	}
	if len(a.SHA256) != 64 {
		return ApplyArgs{}, errors.New("falta la suma SHA-256")
	}
	if a.ParentPID <= 0 {
		return ApplyArgs{}, errors.New("falta el pid de la aplicación")
	}
	base := filepath.Base(a.File)
	if !validAssetName(base) || (a.Kind == "msi") != strings.HasSuffix(base, ".msi") {
		return ApplyArgs{}, errors.New("el archivo no tiene el nombre esperado")
	}
	// Solo se instala lo que está dentro de <datos>/updates.
	rel, err := filepath.Rel(filepath.Join(a.ParentData, "updates"), a.File)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ApplyArgs{}, errors.New("el archivo no está en la carpeta de actualizaciones")
	}
	if a.Kind == "portable" {
		if !filepath.IsAbs(a.Target) {
			return ApplyArgs{}, errors.New("falta la carpeta de destino del portable")
		}
	}
	return a, nil
}
