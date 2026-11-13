package bridge

import (
	"context"
	"log/slog"

	"dconnector/internal/core/port"
)

const (
	notifyTestID    = "dconnector-test"
	notifyTestTitle = "dConnector"
	notifyTestBody  = "Así se verán tus notificaciones"
)

// TestResult: lo que de verdad pasó con la notificación de prueba (no solo «se envió»).
type TestResult struct {
	Permission port.NotifyPermission
	Shown      bool
	Visible    bool
	Sound      bool
	Message    string
}

// SendTest envía la notificación de prueba y suena (con el sonido encendido) y dice con honestidad qué
// pasó. Ignora el interruptor «Notificaciones»: sirve para comprobar si Windows las deja pasar.
func SendTest(ctx context.Context, nt port.Notifier, soundOn bool) TestResult {
	perm := nt.Permission(ctx)
	res := TestResult{Permission: perm}
	var showErr error
	if perm.State == port.NotifyGranted || perm.State == port.NotifyUnknown {
		showErr = nt.Show(ctx, port.Notification{ID: notifyTestID, Title: notifyTestTitle, Body: notifyTestBody, Silent: true})
		if showErr != nil {
			slog.Warn("notificaciones: la prueba no se pudo mostrar", "error", showErr)
		}
		res.Shown = showErr == nil
	}
	if soundOn && nt.PlaySound(ctx) == nil {
		res.Sound = true
	}
	res.Visible = res.Shown && perm.Quiet != port.QuietActive
	res.Message = testMessage(perm, res.Shown, showErr, soundOn, res.Sound)
	return res
}

func testMessage(perm port.NotifyPermission, shown bool, showErr error, soundOn, played bool) string {
	tail := func() string {
		switch {
		case played:
			return ", pero sonará el aviso."
		case soundOn:
			return " y no se pudo reproducir el sonido."
		}
		return " y el sonido está apagado en Ajustes."
	}
	switch {
	case showErr != nil:
		return "No se pudo mostrar la notificación de prueba (" + showErr.Error() + "): no la verás en pantalla" + tail()
	case !shown && perm.State == port.NotifyUnsupported:
		return "Este sistema no tiene notificaciones de escritorio" + tail()
	case !shown:
		return perm.Reason + " No verás el aviso en pantalla" + tail()
	case perm.Quiet == port.QuietActive:
		lead := "Windows está en «No molestar»"
		switch perm.QuietCause {
		case port.QuietCauseFullscreen:
			lead = "Hay una aplicación a pantalla completa"
		case port.QuietCausePresentation:
			lead = "Windows está en modo presentación"
		}
		return lead + ": no verás el aviso en pantalla" + tail() + " Está en el centro de notificaciones."
	case perm.Quiet != port.QuietInactive:
		return "Aviso enviado. No se pudo comprobar si Windows está en «No molestar»: si no lo ves, revisa esa opción."
	}
	return "Aviso enviado"
}
