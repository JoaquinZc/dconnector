package winnotify

import "dconnector/internal/core/port"

// Valores de SHQueryUserNotificationState (shellapi.h).
const (
	// UserNotPresent: protector de pantalla, equipo bloqueado o sesión de otro usuario activa.
	UserNotPresent = 1
	// UserBusy: aplicación a pantalla completa (ventana que cubre todo el monitor).
	UserBusy = 2
	// UserD3DFullScreen: aplicación Direct3D a pantalla completa exclusiva (juego).
	UserD3DFullScreen = 3
	// UserPresentationMode: modo presentación.
	UserPresentationMode = 4
	// UserAcceptsNotifications: el usuario acepta avisos.
	UserAcceptsNotifications = 5
	// UserQuietTime: horas de silencio.
	UserQuietTime = 6
	// UserApp: aplicación de la Tienda a pantalla completa.
	UserApp = 7
)

// QuietReader: lecturas del sistema para saber si Windows está suprimiendo los avisos. Entra por una
// interfaz pequeña para probar la lógica con dobles (la implementación real vive en quiet_windows.go).
type QuietReader interface {
	// DoNotDisturb devuelve el perfil activo del modo «No molestar» / asistente de concentración: 0 = ninguno
	// (desactivado); cualquier otro valor = activado («solo prioritarios», «solo alarmas»).
	DoNotDisturb() (profile uint32, err error)
	// UserState devuelve el resultado de SHQueryUserNotificationState (constantes User*). Por sí sola NO
	// detecta «No molestar» (devuelve «acepta avisos» con él activado): solo pantalla completa, presentación…
	UserState() (state int, err error)
}

const (
	quietDNDReason  = "Windows tiene activado «No molestar»: los avisos no aparecen en pantalla (se guardan en el centro de notificaciones). Los sonidos de dConnector siguen sonando."
	quietFullReason = "Hay una aplicación a pantalla completa: Windows no muestra los avisos en pantalla (se guardan en el centro de notificaciones). Los sonidos de dConnector siguen sonando."
	quietPresReason = "Windows está en modo presentación: los avisos no aparecen en pantalla (se guardan en el centro de notificaciones). Los sonidos de dConnector siguen sonando."
)

// quietStatus combina las dos lecturas:
//   - «No molestar» (estado WNF del shell) distinto de 0 → activo por «No molestar». Es la única vía que lo
//     detecta de verdad: SHQueryUserNotificationState devuelve «acepta avisos» con él activado.
//   - SHQueryUserNotificationState: pantalla completa (2, 3, 7), presentación (4) y horas de silencio (6) →
//     activo; «no presente» (1: bloqueado/protector) no se cuenta como supresión.
//   - Solo si las dos lecturas funcionan y ninguna suprime → «inactivo». Si una falla y la otra no encontró
//     nada, «desconocido»: no se afirma nada que no se haya comprobado.
func quietStatus(r QuietReader) (port.QuietState, port.QuietCause, string) {
	if r == nil {
		return port.QuietUnknown, port.QuietCauseNone, ""
	}
	profile, dndErr := r.DoNotDisturb()
	state, userErr := r.UserState()

	if dndErr == nil && profile != 0 {
		return port.QuietActive, port.QuietCauseDND, quietDNDReason
	}
	if userErr == nil {
		switch state {
		case UserBusy, UserD3DFullScreen, UserApp:
			return port.QuietActive, port.QuietCauseFullscreen, quietFullReason
		case UserPresentationMode:
			return port.QuietActive, port.QuietCausePresentation, quietPresReason
		case UserQuietTime:
			return port.QuietActive, port.QuietCauseDND, quietDNDReason
		}
	}
	if dndErr != nil || userErr != nil {
		return port.QuietUnknown, port.QuietCauseNone, ""
	}
	return port.QuietInactive, port.QuietCauseNone, ""
}
