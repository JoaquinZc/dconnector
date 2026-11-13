package updater

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxZipFiles    = 200
	maxZipTotal    = 600 << 20
	maxZipEntry    = 300 << 20
	stagingPrefix  = ".dconnector-update-"
	oldSuffix      = ".old"
	exeDesktop     = "dConnector.exe"
	exeServer      = "dConnector-server.exe"
	renameAttempts = 12
)

// renameFile y removeFile se sustituyen en pruebas para provocar fallos a mitad del reemplazo.
var (
	renameFile = os.Rename
	removeFile = os.Remove
)

// ErrUnsafeZip: el zip trae una ruta que se sale de la carpeta (zip-slip), un enlace o algo no permitido.
var ErrUnsafeZip = errors.New("el zip trae rutas no permitidas")

// cleanZipName valida y normaliza el nombre de una entrada: relativo, sin «..», sin letra de unidad ni ruta absoluta.
func cleanZipName(name string) (string, error) {
	n := strings.ReplaceAll(name, `\`, "/")
	if n == "" || strings.HasPrefix(n, "/") || strings.ContainsRune(n, ':') || strings.ContainsRune(n, 0) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeZip, name)
	}
	for _, seg := range strings.Split(n, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: %q", ErrUnsafeZip, name)
		}
	}
	c := path.Clean(n)
	if c == "." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%w: %q", ErrUnsafeZip, name)
	}
	return c, nil
}

// extractZip extrae zipPath en dest (que debe estar vacía o no existir) con protección contra zip-slip, enlaces,
// bombas de descompresión y demasiados archivos. Si todas las entradas comparten una carpeta raíz, se quita.
// Devuelve la carpeta que contiene los archivos del programa.
func extractZip(zipPath, dest string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("el zip descargado no se puede abrir: %w", err)
	}
	defer zr.Close()
	if len(zr.File) == 0 || len(zr.File) > maxZipFiles {
		return "", errors.New("el zip no tiene el contenido esperado")
	}
	type item struct {
		f    *zip.File
		name string
	}
	var items []item
	var total uint64
	for _, f := range zr.File {
		name, err := cleanZipName(f.Name)
		if err != nil {
			return "", err
		}
		if f.Mode()&fs.ModeSymlink != 0 || f.Mode()&(fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeIrregular) != 0 {
			return "", fmt.Errorf("%w: enlace o archivo especial %q", ErrUnsafeZip, f.Name)
		}
		if f.UncompressedSize64 > maxZipEntry {
			return "", errors.New("el zip trae un archivo demasiado grande")
		}
		total += f.UncompressedSize64
		if total > maxZipTotal {
			return "", errors.New("el zip es demasiado grande al descomprimirlo")
		}
		items = append(items, item{f, name})
	}
	// Carpeta raíz común.
	root := strings.SplitN(items[0].name, "/", 2)[0]
	nested := false
	for _, it := range items {
		if it.name == root {
			continue
		}
		if !strings.HasPrefix(it.name, root+"/") {
			root = ""
			break
		}
		nested = true
	}
	if !nested {
		root = ""
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	for _, it := range items {
		rel := it.name
		if root != "" {
			rel = strings.TrimPrefix(strings.TrimPrefix(rel, root), "/")
			if rel == "" {
				continue
			}
		}
		out := filepath.Join(absDest, filepath.FromSlash(rel))
		if r, err := filepath.Rel(absDest, out); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
			return "", fmt.Errorf("%w: %q", ErrUnsafeZip, it.f.Name)
		}
		if it.f.FileInfo().IsDir() || strings.HasSuffix(it.f.Name, "/") || strings.HasSuffix(it.f.Name, `\`) {
			if err := os.MkdirAll(out, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return "", err
		}
		if err := writeZipEntry(it.f, out); err != nil {
			return "", err
		}
	}
	return absDest, nil
}

func writeZipEntry(f *zip.File, out string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("no se pudo leer %s del zip: %w", f.Name, err)
	}
	defer rc.Close()
	w, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("no se pudo extraer %s: %w", f.Name, err)
	}
	n, err := io.Copy(w, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("no se pudo extraer %s: %w", f.Name, err)
	}
	if uint64(n) != f.UncompressedSize64 {
		return fmt.Errorf("%w: tamaño distinto del declarado en %q", ErrUnsafeZip, f.Name)
	}
	return nil
}

// checkProgramDir exige que la carpeta extraída tenga los dos ejecutables como archivos normales de Windows (MZ).
func checkProgramDir(dir string) error {
	for _, n := range []string{exeDesktop, exeServer} {
		p := filepath.Join(dir, n)
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() || st.Size() < 1024 {
			return fmt.Errorf("el zip no trae %s", n)
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		var mz [2]byte
		_, rerr := io.ReadFull(f, mz[:])
		f.Close()
		if rerr != nil || mz != [2]byte{'M', 'Z'} {
			return fmt.Errorf("%s no es un ejecutable de Windows", n)
		}
	}
	return nil
}

func retryFS(op func() error) error {
	var err error
	for i := 0; i < renameAttempts; i++ {
		if err = op(); err == nil {
			return nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return err
		}
		time.Sleep(renameRetryWait)
	}
	return err
}

// ReplacePortable aplica el zip sobre la carpeta del portable: extrae a una carpeta de preparación DENTRO del
// destino (mismo volumen), comprueba los ejecutables y reemplaza los archivos del programa renombrando los
// anteriores a «.old»; si algo falla, deshace lo hecho. No toca data\ ni portable.txt.
func ReplacePortable(zipPath, target string) error {
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		return fmt.Errorf("la carpeta del portable no existe: %s", target)
	}
	staging := filepath.Join(target, fmt.Sprintf("%s%d", stagingPrefix, time.Now().UnixNano()))
	defer os.RemoveAll(staging)
	src, err := extractZip(zipPath, staging)
	if err != nil {
		return err
	}
	if err := checkProgramDir(src); err != nil {
		return err
	}
	var files []string // rutas relativas
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if strings.EqualFold(top, "data") || strings.EqualFold(rel, PortableMarker) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)

	type undo struct {
		dst, old string
		hadOld   bool
	}
	var done []undo
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			u := done[i]
			_ = retryFS(func() error { return removeFile(u.dst) })
			if u.hadOld {
				_ = retryFS(func() error { return renameFile(u.old, u.dst) })
			}
		}
	}
	for _, rel := range files {
		dst := filepath.Join(target, rel)
		old := dst + oldSuffix
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			rollback()
			return err
		}
		_ = os.Remove(old) // resto de una actualización anterior
		u := undo{dst: dst, old: old}
		if _, err := os.Lstat(dst); err == nil {
			if err := retryFS(func() error { return renameFile(dst, old) }); err != nil {
				rollback()
				return fmt.Errorf("no se pudo reemplazar %s (¿sigue abierto o lo bloquea el antivirus?): %w", rel, err)
			}
			u.hadOld = true
		}
		done = append(done, u)
		if err := retryFS(func() error { return renameFile(filepath.Join(src, rel), dst) }); err != nil {
			rollback()
			return fmt.Errorf("no se pudo colocar %s: %w", rel, err)
		}
	}
	for _, u := range done {
		if u.hadOld {
			_ = removeFile(u.old) // si está bloqueado queda como .old y se limpia en la próxima
		}
	}
	return nil
}

// renameRetryWait: espera entre reintentos de renombrado (un antivirus puede tener el archivo un instante).
var renameRetryWait = 400 * time.Millisecond
