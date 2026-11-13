package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dconnector/internal/buildinfo"
)

func TestUpdateSelftestCheckAgainstFakeAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0","body":"nueva","html_url":"https://github.com/x/y/releases/tag/v99.0.0","assets":[]}`))
	}))
	defer srv.Close()
	old := buildinfo.Version
	buildinfo.Version = "0.1.0"
	defer func() { buildinfo.Version = old }()
	t.Setenv("DCONNECTOR_UPDATE_API", srv.URL)
	t.Setenv("DCONNECTOR_DATA", t.TempDir())
	var out bytes.Buffer
	if err := runUpdateSelftest(&out, "check"); err != nil {
		t.Fatal(err)
	}
	// El binario de pruebas no es un MSI ni un portable: informa pero no instala.
	for _, want := range []string{`"state": "available"`, `"version": "99.0.0"`, `"installKind": "other"`, `"canInstall": false`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("falta %s en:\n%s", want, out.String())
		}
	}
	if err := runUpdateSelftest(&out, "otra"); err == nil {
		t.Fatal("modo no válido")
	}
}
