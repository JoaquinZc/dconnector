//go:build !windows

package winnotify

// New crea el adaptador fuera de Windows: no hay notificaciones de escritorio que usar, así que el
// permiso es «no admitido» y no se muestra ni suena nada. svc se ignora (en Windows es el servicio de
// notificaciones de Wails).
func New(appID string, svc any) *Notifier {
	return NewWithDeps(Deps{AppID: appID})
}
