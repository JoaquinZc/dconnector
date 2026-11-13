package wails

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"dconnector/internal/adapter/desktop/updater"
)

type fakeUpdater struct {
	st       updater.Status
	why      string
	applied  chan struct{}
	autoSet  []bool
	checkErr error
}

func (f *fakeUpdater) Status() updater.Status { return f.st }
func (f *fakeUpdater) Check(context.Context) (updater.Status, error) {
	return f.st, f.checkErr
}
func (f *fakeUpdater) CanApply() string { return f.why }
func (f *fakeUpdater) Apply(context.Context) error {
	close(f.applied)
	return nil
}
func (f *fakeUpdater) SetAutoCheck(v bool) error { f.autoSet = append(f.autoSet, v); return nil }
func (f *fakeUpdater) ReleasePageURL() string    { return "https://github.com/x/y/releases/latest" }
func (f *fakeUpdater) DownloadFolder() string    { return "dir-descargas" }

func newTestUpdateSvc(f *fakeUpdater) (*UpdateService, *[]string) {
	var opened []string
	s := &UpdateService{u: f,
		openURL:    func(u string) error { opened = append(opened, "url:"+u); return nil },
		openFolder: func(p string) error { opened = append(opened, "dir:"+p); return nil },
	}
	return s, &opened
}

func TestUpdateServiceStatusJSONShape(t *testing.T) {
	f := &fakeUpdater{st: updater.Status{CurrentVersion: "0.3.1", InstallKind: "msi", CanInstall: true, State: "available", Latest: &updater.Release{Version: "0.4.0", AssetName: "a.msi"}}}
	s, _ := newTestUpdateSvc(f)
	got, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"currentVersion", "installKind", "canInstall", "lastCheck", "autoCheck", "latest", "state", "progress"} {
		if _, ok := m[k]; !ok {
			t.Errorf("falta %q en %s", k, b)
		}
	}
	l := m["latest"].(map[string]any)
	for _, k := range []string{"version", "notes", "url", "assetName", "sizeBytes"} {
		if _, ok := l[k]; !ok {
			t.Errorf("falta latest.%s", k)
		}
	}
}

func TestUpdateServiceApply(t *testing.T) {
	f := &fakeUpdater{applied: make(chan struct{}), st: updater.Status{Latest: &updater.Release{Version: "0.4.0"}}}
	s, _ := newTestUpdateSvc(f)
	if err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.applied:
	case <-time.After(2 * time.Second):
		t.Fatal("no se lanzó la actualización")
	}
	f2 := &fakeUpdater{applied: make(chan struct{}), why: "No se puede aquí."}
	s2, _ := newTestUpdateSvc(f2)
	if err := s2.Apply(); err == nil || err.Error() != "No se puede aquí." {
		t.Fatalf("%v", err)
	}
	f3 := &fakeUpdater{applied: make(chan struct{})}
	s3, _ := newTestUpdateSvc(f3)
	if err := s3.Apply(); err == nil || !strings.Contains(err.Error(), "busca") {
		t.Fatalf("%v", err)
	}
}

func TestUpdateServiceOpenersAndAuto(t *testing.T) {
	f := &fakeUpdater{}
	s, opened := newTestUpdateSvc(f)
	if err := s.OpenReleasePage(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAutoCheck(false); err != nil || len(f.autoSet) != 1 || f.autoSet[0] {
		t.Fatal("SetAutoCheck")
	}
	f.checkErr = errors.New("sin red")
	if _, err := s.Check(); err == nil {
		t.Fatal("Check debe devolver el error")
	}
	if len(*opened) != 1 || (*opened)[0] != "url:https://github.com/x/y/releases/latest" {
		t.Fatalf("%v", *opened)
	}
}
