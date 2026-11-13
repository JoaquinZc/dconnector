package updater

import (
	"archive/zip"
	"bytes"
	"net/url"
	"os"
	"path/filepath"
)

func mkdirAll(p string) error { return os.MkdirAll(filepath.FromSlash(p), 0o755) }

func writeFile(p, s string) error {
	p = filepath.FromSlash(p)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(s), 0o644)
}

// fakeExe: contenido que pasa la comprobación de ejecutable (MZ y tamaño).
func fakeExe(tag string) string {
	return "MZ" + tag + string(bytes.Repeat([]byte{'x'}, 2048))
}

type zipEntry struct {
	name string
	body string
	mode os.FileMode
}

func buildZip(path string, entries []zipEntry) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func parseURL(s string) (*url.URL, error) { return url.Parse(s) }
