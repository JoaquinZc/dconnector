package wails

import (
	"context"
	"time"

	"dconnector/internal/adapter/desktop/bridge"
	"dconnector/internal/core/port"
)

// NotifyStatus: el estado de las notificaciones de escritorio que ve la pantalla de Ajustes.
type NotifyStatus struct {
	// State: "granted" | "denied" | "unknown" | "unsupported" (el permiso del sistema operativo).
	State string `json:"state"`
	// Reason: el motivo en español, listo para mostrar.
	Reason string `json:"reason"`
	// Listening: el flujo de eventos del servidor, que trae los avisos, está conectado.
	Listening bool `json:"listening"`
	// Asked: ya se comprobó el permiso en el primer arranque (solo se hace una vez).
	Asked bool `json:"asked"`

	// Quiet: ¿Windows está suprimiendo los avisos en pantalla? "active" | "inactive" | "unknown" (modo
	// «No molestar», pantalla completa o presentación; independiente del permiso).
	Quiet string `json:"quiet"`
	// QuietCause: "dnd" | "fullscreen" | "presentation" | "" (solo con Quiet activo).
	QuietCause string `json:"quietCause"`
	// QuietReason: lo que pasa, en español, listo para mostrar (vacío si no hay supresión).
	QuietReason string `json:"quietReason"`
	// Suppressed: el aviso no se verá en pantalla ahora mismo (permiso bloqueado o no admitido, o Quiet activo).
	Suppressed bool `json:"suppressed"`
	// LastDecision y LastDecisionAt (ms Unix): qué decidió el servidor con el último mensaje y cuándo.
	LastDecision   string `json:"lastDecision"`
	LastDecisionAt int64  `json:"lastDecisionAt"`

	// Solo tras «Probar notificación»: lo que de verdad pasó.
	TestMessage string `json:"testMessage"`
	TestShown   bool   `json:"testShown"`
	TestVisible bool   `json:"testVisible"`
	TestSound   bool   `json:"testSound"`
}

// askedStore: dónde se recuerda que el permiso ya se comprobó (desktop.json).
type askedStore interface {
	NotificationsAsked() bool
	MarkNotificationsAsked() error
}

// streamState: lo que el puente de eventos sabe del servidor (lo cumple *bridge.Bridge).
type streamState interface {
	Listening() bool
	SoundEnabled() bool
	LastDecision() (string, int64)
}

// NotificationsService: solo el lado de Windows de las notificaciones (permiso del sistema, prueba y
// Configuración). Quién avisa de qué lo decide el servidor; el foco del chat (SetFocus) es un RPC del servidor.
type NotificationsService struct {
	n port.Notifier
	b streamState
	a askedStore
}

// NewNotificationsService crea el servicio. Cualquier parte puede ser nil.
func NewNotificationsService(n port.Notifier, b streamState, a askedStore) *NotificationsService {
	return &NotificationsService{n: n, b: b, a: a}
}

func notifyStatusOf(p port.NotifyPermission, listening, asked bool) NotifyStatus {
	quiet := string(p.Quiet)
	if quiet == "" {
		quiet = string(port.QuietUnknown)
	}
	return NotifyStatus{
		State: string(p.State), Reason: p.Reason, Listening: listening, Asked: asked,
		Quiet: quiet, QuietCause: string(p.QuietCause), QuietReason: p.QuietReason, Suppressed: p.Hidden(),
	}
}

func (s *NotificationsService) listening() bool { return s.b != nil && s.b.Listening() }
func (s *NotificationsService) asked() bool     { return s.a != nil && s.a.NotificationsAsked() }

// Status lee el permiso del sistema (sin molestar) y si el flujo de avisos está conectado.
func (s *NotificationsService) Status(ctx context.Context) NotifyStatus {
	if s.n == nil {
		return NotifyStatus{State: string(port.NotifyUnknown), Quiet: string(port.QuietUnknown),
			Reason: "El servicio de notificaciones no está conectado todavía."}
	}
	out := notifyStatusOf(s.n.Permission(ctx), s.listening(), s.asked())
	if s.b != nil {
		out.LastDecision, out.LastDecisionAt = s.b.LastDecision()
	}
	return out
}

// MarkAsked anota que ya se comprobó el permiso en el primer arranque.
func (s *NotificationsService) MarkAsked(ctx context.Context) error {
	if s.a == nil {
		return errNotWired
	}
	return s.a.MarkNotificationsAsked()
}

// RequestPermission pide el permiso: en Windows no hay diálogo, así que si está bloqueado abre la pantalla
// de notificaciones de Configuración. Devuelve el estado.
func (s *NotificationsService) RequestPermission(ctx context.Context) (NotifyStatus, error) {
	if s.n == nil {
		return NotifyStatus{}, errNotWired
	}
	p, err := s.n.RequestPermission(ctx)
	return notifyStatusOf(p, s.listening(), s.asked()), err
}

// SendTest envía la notificación de prueba «Así se verán tus notificaciones», suena (con el sonido activo en
// el servidor) y responde con honestidad: TestMessage dice qué pasó.
func (s *NotificationsService) SendTest(ctx context.Context) (NotifyStatus, error) {
	if s.n == nil {
		return NotifyStatus{}, errNotWired
	}
	sound := true
	if s.b != nil {
		sound = s.b.SoundEnabled()
	}
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r := bridge.SendTest(tctx, s.n, sound)
	out := notifyStatusOf(r.Permission, s.listening(), s.asked())
	out.TestMessage, out.TestShown, out.TestVisible, out.TestSound = r.Message, r.Shown, r.Visible, r.Sound
	return out, nil
}

// OpenSystemSettings abre la pantalla de notificaciones de Windows.
func (s *NotificationsService) OpenSystemSettings() error {
	if s.n == nil {
		return errNotWired
	}
	return s.n.OpenSystemSettings()
}
