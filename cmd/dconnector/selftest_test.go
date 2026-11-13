package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"testing"
)

// TestMain: con DC_FAKE_SERVER=1 este binario hace de dConnector-server falso (--selftest y modo local).
func TestMain(m *testing.M) {
	if os.Getenv("DC_FAKE_SERVER") == "1" {
		for _, a := range os.Args[1:] {
			if a == "--selftest" {
				fmt.Println("OK")
				return
			}
		}
		token := os.Getenv("DCONNECTOR_LOCAL_TOKEN")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"ok","version":"9.9.9","apiVersion":1,"protocol":1,"tls":false}`))
		})
		mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token || token == "" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"username":"local"}`))
		})
		go func() { _ = http.Serve(ln, mux) }()
		fmt.Printf(`{"event":"listening","addr":%q,"tls":false,"version":"9.9.9","pid":%d}`+"\n", ln.Addr().String(), os.Getpid())
		<-sig
		return
	}
	os.Exit(m.Run())
}

func TestSelftestWithoutServerIsAWarning(t *testing.T) {
	t.Setenv("DCONNECTOR_DATA", t.TempDir())
	t.Setenv("DCONNECTOR_SERVER_EXE", "")
	var out bytes.Buffer
	if err := runSelftest(&out, "no-existe-srv.exe", false); err != nil {
		t.Fatalf("sin servidor en desarrollo no debe fallar: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "AVISO") || !strings.Contains(out.String(), "OK (sin servidor)") {
		t.Fatalf("salida: %s", out.String())
	}
	if err := runSelftest(&out, "no-existe-srv.exe", true); err == nil {
		t.Fatal("con --require-server debe fallar")
	}
}

func TestSelftestStartsServer(t *testing.T) {
	t.Setenv("DCONNECTOR_DATA", t.TempDir())
	t.Setenv("DC_FAKE_SERVER", "1")
	var out bytes.Buffer
	if err := runSelftest(&out, os.Args[0], true); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for _, want := range []string{"carpeta de datos: escribible", "servidor --selftest: OK", "health y token propio OK"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("falta %q en:\n%s", want, out.String())
		}
	}
}
