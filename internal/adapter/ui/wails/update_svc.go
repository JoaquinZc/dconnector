package wails

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"dconnector/internal/adapter/desktop/updater"
)

// UpdateStatus es el estado de las actualizaciones que ve la interfaz (versión instalada, tipo de instalación,
// versión nueva, estado y progreso). Eventos: «update:state» (UpdateStatus completo) y «update:progress»
// ({bytes,total,phase}).
type UpdateStatus = updater.Status

// updateAPI es lo que UpdateService necesita del actualizador.
type updateAPI interface {
	Status() updater.Status
	Check(ctx context.Context) (updater.Status, error)
	CanApply() string
	Apply(ctx context.Context) error
	SetAutoCheck(bool) error
	ReleasePageURL() string
	DownloadFolder() string
}

// UpdateService: «actualizar con un botón». La descarga y la instalación las hace internal/adapter/desktop/updater.
type UpdateService struct {
	u          updateAPI
	openURL    func(string) error
	openFolder func(string) error
}

// NewUpdateService crea el servicio sobre el actualizador.
func NewUpdateService(u updateAPI) *UpdateService {
	return &UpdateService{
		u:          u,
		openURL:    func(s string) error { return application.Get().Browser.OpenURL(s) },
		openFolder: func(p string) error { return application.Get().Browser.OpenFile(p) },
	}
}

// Status devuelve el estado actual (sin consultar la red).
func (s *UpdateService) Status() (UpdateStatus, error) { return s.u.Status(), nil }

// Check consulta GitHub ahora (botón «Buscar actualizaciones»). Un fallo se devuelve como error en español y
// queda también en el estado.
func (s *UpdateService) Check() (UpdateStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.u.Check(ctx)
}

// Apply descarga la versión nueva, verifica su SHA-256, cierra la aplicación (y el servidor local), instala
// encima y vuelve a abrirla. Devuelve en seguida; el avance llega por «update:state» y «update:progress», y un
// fallo deja el estado en «error» con el motivo.
func (s *UpdateService) Apply() error {
	if why := s.u.CanApply(); why != "" {
		return errors.New(why)
	}
	if st := s.u.Status(); st.Latest == nil {
		return errors.New("Primero busca actualizaciones.")
	}
	go func() { _ = s.u.Apply(context.Background()) }()
	return nil
}

// SetAutoCheck activa o apaga la búsqueda automática al abrir.
func (s *UpdateService) SetAutoCheck(on bool) error { return s.u.SetAutoCheck(on) }

// OpenReleasePage abre en el navegador la página de la versión.
func (s *UpdateService) OpenReleasePage() error { return s.openURL(s.u.ReleasePageURL()) }

// OpenUpdatesFolder abre la carpeta donde queda el instalador descargado.
func (s *UpdateService) OpenUpdatesFolder() error {
	dir := s.u.DownloadFolder()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return s.openFolder(dir)
}
