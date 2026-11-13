package wails

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"dconnector/internal/core/model"
	"dconnector/internal/paths"
)

// PickedFile: archivo elegido o creado desde la interfaz.
type PickedFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// FilesService: diálogos de archivos y archivos temporales.
type FilesService struct {
	// src da el cliente del servidor para Upload y SaveFileAs (modo remoto); nil = no disponible.
	src ClientSource
}

// NewFilesService crea el servicio de archivos. src puede ser nil.
func NewFilesService(src ClientSource) *FilesService { return &FilesService{src: src} }

// PickFiles abre el diálogo de Wails. kind: "media"|"document"|"sticker"|"csv"|"any".
// Devuelve lista vacía si el usuario cancela.
func (s *FilesService) PickFiles(ctx context.Context, kind string) ([]PickedFile, error) {
	d := application.Get().Dialog.OpenFile().CanChooseFiles(true).SetTitle("Elegir archivos")
	switch kind {
	case "media":
		d.AddFilter("Fotos y vídeos", "*.jpg;*.jpeg;*.png;*.webp;*.gif;*.mp4;*.mov;*.webm;*.mkv;*.3gp")
	case "sticker":
		d.AddFilter("Stickers", "*.webp;*.png")
	case "csv":
		d.AddFilter("Archivos CSV", "*.csv;*.txt")
	}
	picked, err := d.PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}
	out := make([]PickedFile, 0, len(picked))
	for _, p := range picked {
		f, err := s.Info(ctx, p)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// SaveTemp escribe en <data>/tmp un archivo recibido en base64 (notas de voz, imágenes pegadas).
func (s *FilesService) SaveTemp(ctx context.Context, name string, b64 string) (PickedFile, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return PickedFile{}, fmt.Errorf("el archivo no es válido: %w", err)
	}
	clean := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || clean == "/" || clean == "" {
		clean = "archivo"
	}
	if err := os.MkdirAll(paths.TmpDir(), 0o755); err != nil {
		return PickedFile{}, fmt.Errorf("no se pudo preparar la carpeta temporal: %w", err)
	}
	dst := filepath.Join(paths.TmpDir(), model.NewID()+"-"+clean)
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return PickedFile{}, fmt.Errorf("no se pudo guardar el archivo: %w", err)
	}
	return s.Info(ctx, dst)
}

// Info describe un archivo del disco.
func (s *FilesService) Info(ctx context.Context, path string) (PickedFile, error) {
	st, err := os.Stat(path)
	if err != nil {
		return PickedFile{}, fmt.Errorf("no se encontró el archivo: %w", err)
	}
	m := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if m == "" {
		m = "application/octet-stream"
	}
	return PickedFile{Path: path, Name: filepath.Base(path), Mime: m, Size: st.Size()}, nil
}
