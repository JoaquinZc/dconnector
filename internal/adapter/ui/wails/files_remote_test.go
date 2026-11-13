package wails

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"dconnector/internal/adapter/desktop/client"
	"dconnector/internal/adapter/desktop/connection"
)

type fakeSrc struct{ cl *client.Client }

func (f fakeSrc) Client(context.Context) (*client.Client, connection.Info, error) {
	return f.cl, connection.Info{}, nil
}

func TestFilesUploadSendsFile(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, h, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(400)
			return
		}
		b, _ := io.ReadAll(f)
		got = h.Filename + ":" + string(b)
		_, _ = io.WriteString(w, `{"id":"01JX","name":"a.txt","mime":"text/plain","size":3}`)
	}))
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "a.txt")
	_ = os.WriteFile(p, []byte("abc"), 0o600)
	s := NewFilesService(fakeSrc{client.New(client.Config{BaseURL: srv.URL, Token: func() string { return "t" }})})
	up, err := s.Upload(context.Background(), p)
	if err != nil || up.ID != "01JX" || got != "a.txt:abc" {
		t.Fatalf("%+v %v %q", up, err, got)
	}
	if _, err := s.Upload(context.Background(), filepath.Dir(p)); err == nil {
		t.Fatal("una carpeta no se sube")
	}
	if _, err := NewFilesService(nil).Upload(context.Background(), p); err == nil {
		t.Fatal("sin cliente debe fallar")
	}
}
