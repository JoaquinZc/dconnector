package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Valores por defecto del origen.
const (
	DefaultRepo = "JoaquinZc/dconnector"
	DefaultAPI  = "https://api.github.com"
	// EnvAPI cambia la URL base de la API SOLO para pruebas (y solo hacia loopback).
	EnvAPI = "DCONNECTOR_UPDATE_API"
	// SumsName es el asset con las sumas SHA-256 de la Release.
	SumsName = "SHA256SUMS"

	maxNotes = 4000
)

// Release es lo que la interfaz muestra de la versión nueva (JSON camelCase).
type Release struct {
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	URL       string `json:"url"`
	AssetName string `json:"assetName"`
	SizeBytes int64  `json:"sizeBytes"`

	assetURL string
	sumsURL  string
}

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Body       string    `json:"body"`
	HTMLURL    string    `json:"html_url"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

// ErrNoRelease: GitHub no tiene una versión publicada utilizable (sin Release o solo borradores/prereleases).
var ErrNoRelease = errors.New("no hay una versión publicada")

// AssetName devuelve el nombre exacto del archivo que se espera en la Release.
func AssetName(version string, kind Kind, arch string) (string, error) {
	if _, err := ParseVersion(version); err != nil {
		return "", err
	}
	switch kind {
	case KindMSI:
		return fmt.Sprintf("dConnector-%s-%s.msi", version, arch), nil
	case KindPortable:
		return fmt.Sprintf("dConnector-%s-portable-%s.zip", version, arch), nil
	}
	return "", errors.New("este tipo de instalación no se actualiza desde la aplicación")
}

var assetNameRE = regexp.MustCompile(`^dConnector-[0-9A-Za-z.+-]{1,80}(-portable)?-(x64|arm64)\.(msi|zip)$`)

// validAssetName: un nombre de archivo simple (sin rutas ni separadores) con la forma esperada.
func validAssetName(name string) bool {
	return assetNameRE.MatchString(name) && !strings.ContainsAny(name, `/\:`) && !strings.Contains(name, "..")
}

// parseRelease interpreta la respuesta de /releases/latest. Devuelve ErrNoRelease si es borrador o
// prerelease. kind/arch eligen el asset (si kind es KindOther no se elige ninguno).
func parseRelease(r io.Reader, kind Kind, arch string) (*Release, error) {
	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(r, 4<<20)).Decode(&gr); err != nil {
		return nil, fmt.Errorf("la respuesta de GitHub no se entiende: %w", err)
	}
	if gr.Draft || gr.Prerelease {
		return nil, ErrNoRelease
	}
	v, err := ParseVersion(gr.TagName)
	if err != nil {
		return nil, fmt.Errorf("la etiqueta de la versión (%q) no es válida", gr.TagName)
	}
	rel := &Release{Version: v.String(), Notes: cleanNotes(gr.Body), URL: gr.HTMLURL}
	if u, err := url.Parse(gr.HTMLURL); err != nil || u.Scheme != "https" {
		rel.URL = ""
	}
	if kind != KindMSI && kind != KindPortable {
		return rel, nil
	}
	want, err := AssetName(rel.Version, kind, arch)
	if err != nil || !validAssetName(want) {
		return rel, nil
	}
	for _, a := range gr.Assets {
		switch a.Name {
		case want:
			rel.AssetName, rel.SizeBytes, rel.assetURL = a.Name, a.Size, a.URL
		case SumsName:
			rel.sumsURL = a.URL
		}
	}
	return rel, nil
}

// cleanNotes deja texto plano: sin caracteres de control y acotado.
func cleanNotes(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if rs := []rune(out); len(rs) > maxNotes {
		out = string(rs[:maxNotes]) + "…"
	}
	return out
}

// fetchLatest consulta la API y devuelve la última Release.
func (u *Updater) fetchLatest(ctx context.Context) (*Release, error) {
	resp, err := u.get(ctx, u.latestURL(), "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 404:
		return nil, ErrNoRelease
	case resp.StatusCode == 403 || resp.StatusCode == 429:
		return nil, errors.New("GitHub limitó las consultas desde esta conexión. Inténtalo de nuevo en unos minutos.")
	case resp.StatusCode != 200:
		return nil, fmt.Errorf("GitHub respondió con un error (%d). Inténtalo de nuevo más tarde.", resp.StatusCode)
	}
	return parseRelease(resp.Body, u.kind, u.arch)
}

func (u *Updater) latestURL() string {
	return strings.TrimRight(u.api, "/") + "/repos/" + u.repo + "/releases/latest"
}
