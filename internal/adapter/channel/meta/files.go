package meta

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// OutFile es el archivo que se va a subir, ya leído y validado.
type OutFile struct {
	Data []byte
	Name string
	Mime string
	Size int64
}

// ReadOutgoing lee y valida el archivo de un envío contra los límites de la cuenta (tamaño por tipo).
func ReadOutgoing(out model.Outgoing, caps capability.Set) (OutFile, error) {
	if strings.TrimSpace(out.FilePath) == "" {
		return OutFile{}, fmt.Errorf("falta el archivo que se quiere enviar: %w", port.ErrInvalid)
	}
	st, err := os.Stat(out.FilePath)
	if err != nil || st.IsDir() {
		return OutFile{}, fmt.Errorf("no se encontró el archivo que se quiere enviar: %w", port.ErrNotFound)
	}
	limit := caps.MaxFileSize
	switch out.Kind {
	case model.KindImage:
		limit = caps.MaxImageSize
	case model.KindVideo:
		limit = caps.MaxVideoSize
	}
	if limit > 0 && st.Size() > limit {
		return OutFile{}, fmt.Errorf("el archivo pesa más de %d MB, el máximo de este canal: %w", limit>>20, port.ErrInvalid)
	}
	if st.Size() == 0 {
		return OutFile{}, fmt.Errorf("el archivo está vacío: %w", port.ErrInvalid)
	}
	data, err := os.ReadFile(out.FilePath)
	if err != nil {
		return OutFile{}, fmt.Errorf("no se pudo leer el archivo: %w", port.ErrNotFound)
	}
	name := strings.TrimSpace(out.FileName)
	if name == "" {
		name = filepath.Base(out.FilePath)
	}
	mimeType := strings.TrimSpace(out.Mime)
	if mimeType == "" {
		mimeType = MimeFromName(name)
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	return OutFile{Data: data, Name: name, Mime: mimeType, Size: st.Size()}, nil
}
