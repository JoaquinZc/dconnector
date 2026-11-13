package updater

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func init() { renameRetryWait = 0 }

func goodZip(t *testing.T, tag string) string {
	t.Helper()
	zp := filepath.Join(t.TempDir(), "p.zip")
	root := "dConnector-0.4.0-portable-x64/"
	if err := buildZip(zp, []zipEntry{
		{root, "", os.ModeDir | 0o755},
		{root + "dConnector.exe", fakeExe("desk-" + tag), 0},
		{root + "dConnector-server.exe", fakeExe("srv-" + tag), 0},
		{root + "portable.txt", "NUEVO-PORTABLE", 0},
		{root + "LEEME.txt", "leeme " + tag, 0},
		{root + "LICENCIAS.txt", "lic " + tag, 0},
	}); err != nil {
		t.Fatal(err)
	}
	return zp
}

func portableDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for p, c := range map[string]string{
		"dConnector.exe":        fakeExe("desk-viejo"),
		"dConnector-server.exe": fakeExe("srv-viejo"),
		"portable.txt":          "marca-original",
		"LEEME.txt":             "leeme viejo",
		"data/app.db":           "datos-importantes",
		"data/accounts/a/x.db":  "cuenta",
	} {
		if err := writeFile(filepath.Join(d, p), c); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func noLeftovers(t *testing.T, d string) {
	t.Helper()
	entries, _ := os.ReadDir(d)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), stagingPrefix) || strings.HasSuffix(e.Name(), oldSuffix) {
			t.Errorf("quedó un resto: %s", e.Name())
		}
	}
}

func TestReplacePortablePreservesDataAndMarker(t *testing.T) {
	d := portableDir(t)
	if err := ReplacePortable(goodZip(t, "nuevo"), d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, filepath.Join(d, "dConnector.exe")), "desk-nuevo") ||
		!strings.Contains(read(t, filepath.Join(d, "dConnector-server.exe")), "srv-nuevo") {
		t.Fatal("los ejecutables no se reemplazaron")
	}
	if read(t, filepath.Join(d, "LEEME.txt")) != "leeme nuevo" {
		t.Fatal("LEEME no se reemplazó")
	}
	if read(t, filepath.Join(d, "portable.txt")) != "marca-original" {
		t.Fatal("portable.txt se tocó")
	}
	if read(t, filepath.Join(d, "data", "app.db")) != "datos-importantes" || read(t, filepath.Join(d, "data", "accounts", "a", "x.db")) != "cuenta" {
		t.Fatal("data\\ se tocó")
	}
	noLeftovers(t, d)
}

func TestReplacePortableRollsBackOnFailure(t *testing.T) {
	d := portableDir(t)
	orig := renameFile
	defer func() { renameFile = orig }()
	renameFile = func(oldp, newp string) error {
		// Falla al colocar dConnector.exe nuevo: antes ya se movieron otros archivos.
		if strings.Contains(oldp, stagingPrefix) && filepath.Base(oldp) == "dConnector.exe" {
			return errors.New("fallo simulado")
		}
		return orig(oldp, newp)
	}
	err := ReplacePortable(goodZip(t, "nuevo"), d)
	if err == nil {
		t.Fatal("debía fallar")
	}
	for p, want := range map[string]string{
		"dConnector.exe": fakeExe("desk-viejo"), "dConnector-server.exe": fakeExe("srv-viejo"),
		"LEEME.txt": "leeme viejo", "portable.txt": "marca-original", "data/app.db": "datos-importantes",
	} {
		if got := read(t, filepath.Join(d, filepath.FromSlash(p))); got != want {
			t.Errorf("%s no volvió a como estaba: %.30q", p, got)
		}
	}
	if _, err := os.Stat(filepath.Join(d, "LICENCIAS.txt")); err == nil {
		t.Error("un archivo nuevo quedó a medias")
	}
	noLeftovers(t, d)
}

func TestReplacePortableRejectsMissingExe(t *testing.T) {
	d := portableDir(t)
	zp := filepath.Join(t.TempDir(), "x.zip")
	_ = buildZip(zp, []zipEntry{{"r/dConnector.exe", fakeExe("a"), 0}})
	if err := ReplacePortable(zp, d); err == nil || !strings.Contains(err.Error(), "dConnector-server.exe") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(read(t, filepath.Join(d, "dConnector.exe")), "desk-viejo") {
		t.Fatal("se tocó el programa")
	}
	noLeftovers(t, d)
}

func TestReplacePortableRejectsNotExecutable(t *testing.T) {
	d := portableDir(t)
	zp := filepath.Join(t.TempDir(), "x.zip")
	_ = buildZip(zp, []zipEntry{
		{"dConnector.exe", strings.Repeat("A", 4000), 0},
		{"dConnector-server.exe", fakeExe("s"), 0},
	})
	if err := ReplacePortable(zp, d); err == nil {
		t.Fatal("un archivo que no es ejecutable debe rechazarse")
	}
}

func TestExtractZipSlip(t *testing.T) {
	for _, name := range []string{`..\evil.txt`, `../evil.txt`, `a/../../evil.txt`, `a\..\..\evil.txt`, `/abs/evil.txt`, `\abs\evil.txt`, `C:\evil.txt`, `C:evil.txt`, `..`} {
		base := t.TempDir()
		dest := filepath.Join(base, "staging")
		zp := filepath.Join(base, "z.zip")
		if err := buildZip(zp, []zipEntry{{name, "mal", 0}, {"ok.txt", "bien", 0}}); err != nil {
			t.Fatal(err)
		}
		_, err := extractZip(zp, dest)
		if !errors.Is(err, ErrUnsafeZip) {
			t.Errorf("%q: err=%v", name, err)
		}
		if _, e := os.Stat(filepath.Join(base, "evil.txt")); e == nil {
			t.Errorf("%q: escribió fuera de la carpeta", name)
		}
	}
}

func TestExtractZipRejectsSymlink(t *testing.T) {
	base := t.TempDir()
	zp := filepath.Join(base, "z.zip")
	_ = buildZip(zp, []zipEntry{{"r/link", "C:\\Windows", os.ModeSymlink | 0o777}, {"r/a.txt", "x", 0}})
	if _, err := extractZip(zp, filepath.Join(base, "s")); !errors.Is(err, ErrUnsafeZip) {
		t.Fatalf("err=%v", err)
	}
}

func TestExtractZipStripsRootAndLimits(t *testing.T) {
	base := t.TempDir()
	zp := filepath.Join(base, "z.zip")
	_ = buildZip(zp, []zipEntry{{"raiz/a.txt", "A", 0}, {"raiz/sub/b.txt", "B", 0}})
	src, err := extractZip(zp, filepath.Join(base, "s"))
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(src, "a.txt")) != "A" || read(t, filepath.Join(src, "sub", "b.txt")) != "B" {
		t.Fatal("no quitó la carpeta raíz")
	}
	// Demasiados archivos.
	var many []zipEntry
	for i := 0; i <= maxZipFiles; i++ {
		many = append(many, zipEntry{"f" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".txt", "x", 0})
	}
	z2 := filepath.Join(base, "m.zip")
	_ = buildZip(z2, many)
	if _, err := extractZip(z2, filepath.Join(base, "s2")); err == nil {
		t.Fatal("demasiados archivos")
	}
}
