package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
)

// Uploaded es la ficha que devuelve POST /api/v1/files (docs/14 §6). El id se usa como «upload://<id>».
type Uploaded struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// Upload sube un archivo (multipart, campo «file») sin cargarlo entero en memoria.
func (c *Client) Upload(ctx context.Context, name string, r io.Reader) (Uploaded, error) {
	var up Uploaded
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, strings.NewReplacer(`"`, "_", "\r", "", "\n", "").Replace(name)))
		h.Set("Content-Type", "application/octet-stream")
		part, err := mw.CreatePart(h)
		if err == nil {
			_, err = io.Copy(part, r)
		}
		if err == nil {
			err = mw.Close()
		}
		_ = pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/v1/files", pr)
	if err != nil {
		_ = pr.Close()
		return up, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token())
	req.Header.Set("X-DConnector-Client", c.cfg.ClientName)
	resp, err := c.http.Do(req)
	if err != nil {
		_ = pr.Close()
		return up, connError(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return up, apiError(resp, body)
	}
	if err := json.Unmarshal(body, &up); err != nil || up.ID == "" {
		return up, errors.New("respuesta inesperada del servidor al subir el archivo")
	}
	return up, nil
}

// Download copia en w el contenido de un recurso de solo lectura del servidor: «/media/…», «/avatar/…» o
// «/files/…» (las rutas relativas de los modelos; se les antepone /api/v1).
func (c *Client) Download(ctx context.Context, relPath string, w io.Writer) (int64, error) {
	rel := strings.TrimSpace(relPath)
	if u, err := url.Parse(rel); err != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" {
		return 0, errors.New("la ruta del archivo no es válida")
	}
	if strings.Contains(rel, "..") || !(strings.HasPrefix(rel, "/media/") || strings.HasPrefix(rel, "/avatar/") || strings.HasPrefix(rel, "/files/")) {
		return 0, errors.New("la ruta del archivo no es válida")
	}
	resp, err := c.do(ctx, http.MethodGet, "/api/v1"+rel, nil, true, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return 0, apiError(resp, body)
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return n, fmt.Errorf("se cortó la descarga: %w", err)
	}
	return n, nil
}
