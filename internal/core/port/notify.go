package port

import (
	"context"

	"dconnector/internal/core/model"
)

// NotifyState: lo que el sistema operativo permite hacer con las notificaciones de la aplicación.
type NotifyState string

const (
	// NotifyGranted: el sistema deja mostrar notificaciones.
	NotifyGranted NotifyState = "granted"
	// NotifyDenied: el usuario las apagó (en general o solo para esta aplicación).
	NotifyDenied NotifyState = "denied"
	// NotifyUnknown: no se pudo averiguar (se intenta mostrar igual y se cae al sonido si falla).
	NotifyUnknown NotifyState = "unknown"
	// NotifyUnsupported: este sistema no tiene notificaciones de escritorio que usar.
	NotifyUnsupported NotifyState = "unsupported"
)

// QuietState: si el sistema está suprimiendo los avisos en pantalla (modo «No molestar», pantalla completa,
// presentación). Es independiente del permiso: con el permiso concedido y «No molestar» activo, el aviso llega
// al centro de notificaciones pero no se ve ni suena.
type QuietState string

const (
	// QuietInactive: nada suprime los avisos.
	QuietInactive QuietState = "inactive"
	// QuietActive: el sistema suprime los avisos en pantalla.
	QuietActive QuietState = "active"
	// QuietUnknown: no se pudo averiguar (no se afirma nada).
	QuietUnknown QuietState = "unknown"
)

// QuietCause: qué suprime los avisos cuando QuietState es QuietActive.
type QuietCause string

const (
	QuietCauseNone         QuietCause = ""
	QuietCauseDND          QuietCause = "dnd"          // modo «No molestar» / asistente de concentración
	QuietCauseFullscreen   QuietCause = "fullscreen"   // una aplicación a pantalla completa
	QuietCausePresentation QuietCause = "presentation" // modo presentación
)

// NotifyPermission: el estado del permiso y su motivo, en español, para mostrarlo tal cual. Los campos
// Quiet* son aditivos: dicen si, aun con permiso, el sistema oculta los avisos ahora mismo.
type NotifyPermission struct {
	State  NotifyState `json:"state"`
	Reason string      `json:"reason"`
	// Quiet: «active» | «inactive» | «unknown» ("" = no informado = desconocido).
	Quiet QuietState `json:"quiet,omitempty"`
	// QuietCause: qué la causa (solo con Quiet activo).
	QuietCause QuietCause `json:"quietCause,omitempty"`
	// QuietReason: texto en español con lo que pasa, listo para mostrar (vacío si no hay supresión).
	QuietReason string `json:"quietReason,omitempty"`
}

// Hidden dice si el aviso no se verá en pantalla ahora mismo: permiso bloqueado o no admitido, o el sistema
// en «No molestar» / pantalla completa / presentación.
func (p NotifyPermission) Hidden() bool {
	return p.State == NotifyDenied || p.State == NotifyUnsupported || p.Quiet == QuietActive
}

// Notification: una notificación de escritorio.
type Notification struct {
	// ID identifica la notificación (el mismo id no debe repetirse para mensajes distintos).
	ID string `json:"id"`
	// Title y Body: lo que se ve. El cuerpo ya viene decidido por el servicio (vista previa o texto neutro).
	Title string `json:"title"`
	Body  string `json:"body"`
	// ChatKey: el chat que se abre al pulsar la notificación ("" = ninguno).
	ChatKey model.ChatKey `json:"chatKey"`
	// AvatarPath: ruta local de una imagen pequeña (opcional).
	AvatarPath string `json:"avatarPath,omitempty"`
	// Silent: sin el sonido propio de la notificación.
	Silent bool `json:"silent"`
}

// Notifier: el adaptador de escritorio que muestra notificaciones y suena (en Windows: toast del
// sistema y sonido de mensaje). El núcleo decide cuándo; el adaptador solo ejecuta.
type Notifier interface {
	// Permission lee el permiso actual sin molestar al usuario.
	Permission(ctx context.Context) NotifyPermission
	// RequestPermission pide el permiso: si el sistema no tiene diálogo, lleva al usuario a donde
	// puede encenderlas. Devuelve el estado resultante.
	RequestPermission(ctx context.Context) (NotifyPermission, error)
	// Show muestra la notificación.
	Show(ctx context.Context, n Notification) error
	// PlaySound reproduce el sonido de mensaje del sistema, sin bloquear.
	PlaySound(ctx context.Context) error
	// OpenSystemSettings abre la pantalla de notificaciones del sistema.
	OpenSystemSettings() error
}
