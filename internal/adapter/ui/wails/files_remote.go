package wails

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"dconnector/internal/adapter/desktop/client"
	"dconnector/internal/adapter/desktop/connection"
)

// ClientSource da un cliente del servidor actual (lo cumple *connection.Manager).
type ClientSource interface {
	Client(ctx context.Context) (*client.Client, connection.Info, error)
}

// UploadedFile: ficha de un archivo subido al servidor (docs/14 §6). Para usarlo como adjunto, la interfaz
// manda «upload://<id>» donde el núcleo espera una ruta.
type UploadedFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// Upload sube un archivo local al servidor (modo remoto: Files.PickFiles/SaveTemp/Info no existen allí) y
// devuelve su ficha.
func (s *FilesService) Upload(ctx context.Context, path string) (UploadedFile, error) {
	if s.src == nil {
		return UploadedFile{}, errNotWired
	}
	f, err := os.Open(path)
	if err != nil {
		return UploadedFile{}, fmt.Errorf("no se pudo abrir el archivo: %w", err)
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		return UploadedFile{}, errors.New("eso no es un archivo")
	}
	cl, _, err := s.src.Client(ctx)
	if err != nil {
		return UploadedFile{}, err
	}
	up, err := cl.Upload(ctx, filepath.Base(path), f)
	if err != nil {
		return UploadedFile{}, err
	}
	return UploadedFile(up), nil
}

// SaveFileAs muestra el diálogo «Guardar como» y descarga ahí un medio del servidor. url es la ruta relativa
// del modelo («/media/<cuenta>/<id>»). Devuelve la ruta guardada, o "" si la persona canceló.
func (s *FilesService) SaveFileAs(ctx context.Context, suggestedName, url string) (string, error) {
	if s.src == nil {
		return "", errNotWired
	}
	name := filepath.Base(strings.ReplaceAll(suggestedName, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "archivo"
	}
	dst, err := application.Get().Dialog.SaveFile().SetFilename(name).PromptForSingleSelection()
	if err != nil || dst == "" {
		return "", err
	}
	cl, _, err := s.src.Client(ctx)
	if err != nil {
		return "", err
	}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	out, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("no se pudo crear el archivo: %w", err)
	}
	_, err = cl.Download(dctx, url, out)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}
