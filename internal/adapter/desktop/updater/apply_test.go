package updater

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func goodArgs(t *testing.T, kind string) (ApplyArgs, string) {
	t.Helper()
	data := t.TempDir()
	name := "dConnector-0.4.0-x64.msi"
	if kind == "portable" {
		name = "dConnector-0.4.0-portable-x64.zip"
	}
	file := filepath.Join(data, "updates", "0.4.0", name)
	if err := writeFile(file, "contenido"); err != nil {
		t.Fatal(err)
	}
	sum, _ := SHA256File(file)
	inst := t.TempDir()
	a := ApplyArgs{Kind: kind, File: file, SHA256: sum, ParentPID: 4242, Relaunch: filepath.Join(inst, "dConnector.exe"), ParentData: data}
	if kind == "portable" {
		a.Target = inst
	}
	return a, data
}

func TestApplyArgsRoundTrip(t *testing.T) {
	for _, kind := range []string{"msi", "portable"} {
		a, _ := goodArgs(t, kind)
		a.Minimized = true
		got, err := ParseApplyArgs(BuildApplyArgs(a))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, a) {
			t.Fatalf("%+v != %+v", got, a)
		}
	}
	if !IsApplyInvocation([]string{"--x", FlagApply}) || IsApplyInvocation([]string{"--minimized"}) {
		t.Fatal("IsApplyInvocation")
	}
}

func TestApplyArgsRejectsBad(t *testing.T) {
	good, data := goodArgs(t, "msi")
	mut := map[string]func(a *ApplyArgs){
		"tipo":              func(a *ApplyArgs) { a.Kind = "exe" },
		"ruta relativa":     func(a *ApplyArgs) { a.File = `updates\x.msi` },
		"fuera de updates":  func(a *ApplyArgs) { a.File = filepath.Join(data, "otra", "dConnector-0.4.0-x64.msi") },
		"escapa de updates": func(a *ApplyArgs) { a.File = filepath.Join(data, "updates", "..", "dConnector-0.4.0-x64.msi") },
		"nombre raro":       func(a *ApplyArgs) { a.File = filepath.Join(data, "updates", "0.4.0", "calc.exe") },
		"tipo y extensión": func(a *ApplyArgs) {
			a.File = filepath.Join(data, "updates", "0.4.0", "dConnector-0.4.0-portable-x64.zip")
		},
		"sin hash":          func(a *ApplyArgs) { a.SHA256 = "" },
		"sin pid":           func(a *ApplyArgs) { a.ParentPID = 0 },
		"sin datos":         func(a *ApplyArgs) { a.ParentData = "" },
		"relanzar relativo": func(a *ApplyArgs) { a.Relaunch = "dConnector.exe" },
	}
	for name, m := range mut {
		a := good
		m(&a)
		if _, err := ParseApplyArgs(BuildApplyArgs(a)); err == nil {
			t.Errorf("%s: debía rechazarse", name)
		}
	}
	if _, err := ParseApplyArgs([]string{"--kind", "msi"}); err == nil {
		t.Error("sin --apply-update")
	}
	if _, err := ParseApplyArgs([]string{FlagApply, "--desconocida"}); err == nil {
		t.Error("bandera desconocida")
	}
	p, _ := goodArgs(t, "portable")
	p.Target = ""
	if _, err := ParseApplyArgs(BuildApplyArgs(p)); err == nil {
		t.Error("portable sin destino")
	}
}

type recorder struct {
	calls    []string
	msgs     []string
	folders  []string
	relaunch [][]string
	msiCode  int
	msiErr   error
	parent   error
	servers  error
	relErr   error
}

func (r *recorder) hooks() hooks {
	return hooks{
		waitParent:  func(int, time.Duration) error { r.calls = append(r.calls, "parent"); return r.parent },
		waitServers: func(string, time.Duration) error { r.calls = append(r.calls, "servers"); return r.servers },
		runMSI: func(msi, log string) (int, error) {
			r.calls = append(r.calls, "msi")
			return r.msiCode, r.msiErr
		},
		relaunch: func(exe string, args []string) error {
			r.calls = append(r.calls, "relaunch")
			r.relaunch = append(r.relaunch, append([]string{exe}, args...))
			return r.relErr
		},
		message:    func(_, text string) { r.msgs = append(r.msgs, text) },
		openFolder: func(p string) { r.folders = append(r.folders, p) },
		now:        func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) },
	}
}

func TestRunApplyMSISuccess(t *testing.T) {
	a, data := goodArgs(t, "msi")
	a.Minimized = true
	r := &recorder{}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitOK {
		t.Fatalf("código %d, mensajes %v", code, r.msgs)
	}
	if strings.Join(r.calls, ",") != "parent,servers,msi,relaunch" {
		t.Fatalf("orden: %v", r.calls)
	}
	if len(r.msgs) != 0 {
		t.Fatalf("no debía avisar: %v", r.msgs)
	}
	if !reflect.DeepEqual(r.relaunch[0], []string{a.Relaunch, "--minimized"}) {
		t.Fatalf("%v", r.relaunch)
	}
	logs, _ := filepath.Glob(filepath.Join(data, "logs", "desktop", "update-*.log"))
	if len(logs) != 1 {
		t.Fatalf("registro: %v", logs)
	}
}

func TestRunApplyMSIAcceptsRebootCode(t *testing.T) {
	a, _ := goodArgs(t, "msi")
	r := &recorder{msiCode: 3010}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitOK || len(r.msgs) != 0 {
		t.Fatalf("3010 es éxito: %d %v", code, r.msgs)
	}
	if !reflect.DeepEqual(r.relaunch[0], []string{a.Relaunch}) {
		t.Fatalf("sin --minimized si no estaba minimizada: %v", r.relaunch)
	}
}

func TestRunApplyMSIFailureExplainsAndOpensFolder(t *testing.T) {
	a, data := goodArgs(t, "msi")
	r := &recorder{msiCode: 1603}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitFailed {
		t.Fatalf("código %d", code)
	}
	if len(r.msgs) != 1 || !strings.Contains(r.msgs[0], "1603") || !strings.Contains(r.msgs[0], a.File) || !strings.Contains(r.msgs[0], "Smart App Control") || !strings.Contains(r.msgs[0], filepath.Join(data, "logs")) {
		t.Fatalf("mensaje: %v", r.msgs)
	}
	if !reflect.DeepEqual(r.folders, []string{filepath.Dir(a.File)}) {
		t.Fatalf("carpeta: %v", r.folders)
	}
	if len(r.relaunch) != 1 {
		t.Fatal("debe intentar abrir la versión anterior")
	}
}

func TestRunApplyRelaunchFailureWarns(t *testing.T) {
	a, _ := goodArgs(t, "msi")
	r := &recorder{relErr: errors.New("Windows bloqueó el programa (acceso denegado)")}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitFailed {
		t.Fatalf("código %d", code)
	}
	if len(r.msgs) != 1 || !strings.Contains(r.msgs[0], "no se pudo volver a abrir") || !strings.Contains(r.msgs[0], "Causa probable") || len(r.folders) != 1 {
		t.Fatalf("%v %v", r.msgs, r.folders)
	}
}

func TestRunApplyRunMSIError(t *testing.T) {
	a, _ := goodArgs(t, "msi")
	r := &recorder{msiErr: errors.New("fork/exec msiexec.exe: Access is denied.")}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitFailed || len(r.msgs) != 1 || !strings.Contains(r.msgs[0], "Causa probable") {
		t.Fatalf("%d %v", code, r.msgs)
	}
}

func TestRunApplyParentStillAlive(t *testing.T) {
	a, _ := goodArgs(t, "msi")
	r := &recorder{parent: errTimeout}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitFailed {
		t.Fatal(code)
	}
	if strings.Contains(strings.Join(r.calls, ","), "msi") || len(r.relaunch) != 0 {
		t.Fatalf("no debe instalar ni relanzar: %v", r.calls)
	}
}

func TestRunApplyHashChangedAfterDownload(t *testing.T) {
	a, _ := goodArgs(t, "msi")
	if err := os.WriteFile(a.File, []byte("cambiado"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitFailed {
		t.Fatal(code)
	}
	if strings.Contains(strings.Join(r.calls, ","), "msi") {
		t.Fatal("ejecutó un instalador que no coincide")
	}
	if _, err := os.Stat(a.File); err == nil {
		t.Fatal("no borró el archivo alterado")
	}
}

func TestRunApplyPortable(t *testing.T) {
	a, _ := goodArgs(t, "portable")
	zp := goodZip(t, "nuevo")
	b, _ := os.ReadFile(zp)
	if err := os.WriteFile(a.File, b, 0o644); err != nil {
		t.Fatal(err)
	}
	a.SHA256, _ = SHA256File(a.File)
	for p, c := range map[string]string{"dConnector.exe": fakeExe("viejo"), "dConnector-server.exe": fakeExe("v"), "portable.txt": "marca", "data/app.db": "datos"} {
		_ = writeFile(filepath.Join(a.Target, p), c)
	}
	r := &recorder{}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitOK {
		t.Fatalf("%d %v", code, r.msgs)
	}
	if !strings.Contains(read(t, filepath.Join(a.Target, "dConnector.exe")), "desk-nuevo") || read(t, filepath.Join(a.Target, "data", "app.db")) != "datos" || read(t, filepath.Join(a.Target, "portable.txt")) != "marca" {
		t.Fatal("el portable no quedó bien")
	}
	if strings.Join(r.calls, ",") != "parent,servers,relaunch" {
		t.Fatalf("%v", r.calls)
	}
}

func TestRunApplyPortableFailureKeepsOldAndRelaunches(t *testing.T) {
	a, _ := goodArgs(t, "portable")
	// El «zip» no es un zip: el hash coincide pero no se puede extraer.
	_ = os.WriteFile(a.File, []byte("no es un zip"), 0o644)
	a.SHA256, _ = SHA256File(a.File)
	_ = writeFile(filepath.Join(a.Target, "dConnector.exe"), fakeExe("viejo"))
	r := &recorder{}
	if code := runApply(BuildApplyArgs(a), r.hooks()); code != ExitFailed {
		t.Fatal(code)
	}
	if !strings.Contains(read(t, filepath.Join(a.Target, "dConnector.exe")), "viejo") {
		t.Fatal("se tocó el programa")
	}
	if len(r.msgs) != 1 || len(r.relaunch) != 1 {
		t.Fatalf("%v %v", r.msgs, r.relaunch)
	}
}

func TestMsiCodeText(t *testing.T) {
	for _, c := range []int{1602, 1618, 1638, 1603} {
		if msiCodeText(c) == "" {
			t.Fatal(c)
		}
	}
}
