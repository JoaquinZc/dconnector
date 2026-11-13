package updater

import "testing"

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.8", "0.3.1", 1},
		{"0.3.1", "0.3.8", -1},
		{"0.3.8", "0.3.8", 0},
		{"v0.3.8", "0.3.8", 0},
		{"0.10.0", "0.9.9", 1},
		{"1.0.0", "0.99.99", 1},
		{"0.3.8-prueba", "0.3.8", -1}, // el sufijo es más antiguo que la final
		{"0.3.8", "0.3.8-prueba", 1},
		{"0.3.8-prueba", "0.3.7", 1},
		{"0.3.8-rc.2", "0.3.8-rc.10", -1},
		{"0.3.8-rc.1", "0.3.8-rc.1.1", -1},
		{"0.3.8-alfa", "0.3.8-beta", -1},
		{"0.3.8+build5", "0.3.8", 0},
	}
	for _, c := range cases {
		a, err := ParseVersion(c.a)
		if err != nil {
			t.Fatalf("%s: %v", c.a, err)
		}
		b, err := ParseVersion(c.b)
		if err != nil {
			t.Fatalf("%s: %v", c.b, err)
		}
		if got := a.Compare(b); got != c.want {
			t.Errorf("%s vs %s = %d, quería %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseVersionRejects(t *testing.T) {
	for _, s := range []string{"", "dev", "1.2", "a.b.c", "1.2.3/../x", `1.2.3\x`, "1.2.3-", "v1.2.3 ", "1.2.3-a b", "../1.2.3"} {
		if s == "v1.2.3 " { // los espacios de los extremos se recortan
			if _, err := ParseVersion(s); err != nil {
				t.Errorf("%q debería aceptarse tras recortar", s)
			}
			continue
		}
		if _, err := ParseVersion(s); err == nil {
			t.Errorf("%q debería rechazarse", s)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, cur string
		want        bool
	}{
		{"0.3.8", "0.3.1", true},
		{"0.3.8", "0.3.8", false},
		{"0.3.7", "0.3.8", false},
		{"0.3.8", "0.3.8-prueba", true},
		{"0.3.8-prueba", "0.3.8", false},
		{"0.3.8", "dev", false}, // dev nunca se actualiza
		{"basura", "0.3.1", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.cur); got != c.want {
			t.Errorf("IsNewer(%q,%q)=%v", c.latest, c.cur, got)
		}
	}
}

func TestArchName(t *testing.T) {
	if a, ok := ArchName("amd64"); !ok || a != "x64" {
		t.Fatal("amd64")
	}
	if a, ok := ArchName("arm64"); !ok || a != "arm64" {
		t.Fatal("arm64")
	}
	if _, ok := ArchName("386"); ok {
		t.Fatal("386 no se ofrece")
	}
}

func TestDetectKind(t *testing.T) {
	local := t.TempDir()
	msiDir := local + `\Programs\dConnector`
	mk := func(dir string) string {
		if err := mkdirAll(dir); err != nil {
			t.Fatal(err)
		}
		return dir + `\dConnector.exe`
	}
	yes := func() bool { return true }
	no := func() bool { return false }
	if k := DetectKind(mk(msiDir), KindProbe{LocalAppData: local, RegistryInstalled: yes}); k != KindMSI {
		t.Errorf("msi: %s", k)
	}
	if k := DetectKind(msiDir+`\dConnector.exe`, KindProbe{LocalAppData: local, RegistryInstalled: no}); k != KindOther {
		t.Errorf("sin registro no es msi: %s", k)
	}
	other := mk(t.TempDir())
	if k := DetectKind(other, KindProbe{LocalAppData: local, RegistryInstalled: yes}); k != KindOther {
		t.Errorf("otra ruta: %s", k)
	}
	pdir := t.TempDir()
	if err := writeFile(pdir+`\`+PortableMarker, "x"); err != nil {
		t.Fatal(err)
	}
	if k := DetectKind(pdir+`\dConnector.exe`, KindProbe{LocalAppData: local, RegistryInstalled: yes}); k != KindPortable {
		t.Errorf("portable: %s", k)
	}
	if k := DetectKind("", KindProbe{}); k != KindOther {
		t.Errorf("vacío: %s", k)
	}
}
