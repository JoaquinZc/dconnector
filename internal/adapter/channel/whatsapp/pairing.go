package whatsapp

// Vinculación (07 §4): por QR y por código de 8 caracteres.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// pairFirstCodeWait: cuánto se espera al primer elemento del canal de QR (el socket listo).
	pairFirstCodeWait = 30 * time.Second
	// pairCodeClientName: nombre que WhatsApp muestra para el dispositivo vinculado por código.
	pairCodeClientName = "Chrome (Windows)"
)

func (a *Adapter) cancelPairing() {
	a.pairMu.Lock()
	if a.pairCancel != nil {
		a.pairCancel()
		a.pairCancel = nil
	}
	a.pairMu.Unlock()
}

// BeginPairing arranca la vinculación por QR ("qr") o por código ("phone").
func (a *Adapter) BeginPairing(ctx context.Context, req model.PairRequest) error {
	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method == "" {
		method = "qr"
	}
	if method != "qr" && method != "phone" {
		return fmt.Errorf("whatsapp: método de vinculación %q no admitido: %w", req.Method, port.ErrInvalid)
	}
	phone := ""
	if method == "phone" {
		phone = nonDigits.ReplaceAllString(req.Phone, "")
		if len(phone) < 8 {
			return fmt.Errorf("whatsapp: escribe el número con el código de país, por ejemplo 593991234567: %w", port.ErrInvalid)
		}
		if strings.HasPrefix(phone, "0") {
			return fmt.Errorf("whatsapp: el número debe llevar el código de país y no empezar con 0: %w", port.ErrInvalid)
		}
	}

	run := a.runContext()
	if err := a.ensureClient(run); err != nil {
		return err
	}
	cli := a.getClient()
	// Sesión conservada (otra sesión la tomó o la versión quedó vieja): basta con reconectar.
	if cli.Store.ID != nil {
		return a.connect(run, cli)
	}
	// Reintento: se descarta el intento anterior.
	a.cancelPairing()
	if cli.IsConnected() {
		cli.Disconnect()
	}

	pairCtx, cancel := context.WithCancel(run)
	a.pairMu.Lock()
	a.pairCancel = cancel
	a.pairMu.Unlock()

	// GetQRChannel va ANTES de Connect (07 §4).
	qrChan, err := cli.GetQRChannel(pairCtx)
	if err != nil {
		cancel()
		if errors.Is(err, whatsmeow.ErrQRStoreContainsID) || errors.Is(err, whatsmeow.ErrQRAlreadyConnected) {
			return a.connect(run, cli)
		}
		return a.mapErr("preparar la vinculación", err)
	}
	if err := cli.ConnectContext(pairCtx); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
		cancel()
		a.log.Warn("no se pudo abrir la conexión para vincular", "err", redact(err.Error()))
		a.setStatus(model.AccountStatus{State: model.StateError, Detail: "No se pudo conectar con WhatsApp. Revisa tu conexión a internet."})
		return fmt.Errorf("whatsapp: no se pudo conectar con WhatsApp: %w", port.ErrNotConnected)
	}

	if method == "qr" {
		go a.consumeQR(pairCtx, qrChan, false)
		return nil
	}

	// Por código: se espera al primer elemento del canal (socket listo) y se pide el código.
	var first whatsmeow.QRChannelItem
	select {
	case first = <-qrChan:
	case <-time.After(pairFirstCodeWait):
		cancel()
		return fmt.Errorf("whatsapp: WhatsApp tardó demasiado en responder, inténtalo de nuevo: %w", port.ErrNotConnected)
	case <-pairCtx.Done():
		return pairCtx.Err()
	}
	if first.Event != whatsmeow.QRChannelEventCode {
		cancel()
		return a.pairingFailure(first)
	}
	code, err := cli.PairPhone(pairCtx, phone, true, whatsmeow.PairClientChrome, pairCodeClientName)
	if err != nil {
		cancel()
		switch {
		case errors.Is(err, whatsmeow.ErrPhoneNumberTooShort), errors.Is(err, whatsmeow.ErrPhoneNumberIsNotInternational):
			return fmt.Errorf("whatsapp: el número no es válido, escríbelo con el código de país: %w", port.ErrInvalid)
		}
		return a.mapErr("pedir el código de vinculación", err)
	}
	a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedEnterCode, PairCode: code})
	go a.consumeQR(pairCtx, qrChan, true)
	return nil
}

// SubmitPairing no se usa en WhatsApp: el QR y el código no necesitan entrada del usuario.
func (a *Adapter) SubmitPairing(ctx context.Context, in model.PairInput) error {
	return port.ErrUnsupported
}

// consumeQR lee el canal de QR hasta que se cierra. En modo código los QR se ignoran.
func (a *Adapter) consumeQR(ctx context.Context, ch <-chan whatsmeow.QRChannelItem, phoneMode bool) {
	defer a.recoverLoop("pairing")
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-ch:
			if !ok {
				return
			}
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				if !phoneMode {
					a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedScanQR, QRCode: item.Code})
				}
			case whatsmeow.QRChannelSuccess.Event:
				// El ID del dispositivo ya quedó en whatsmeow_device (PairSuccess lo guarda).
				a.log.Info("vinculación aceptada")
				a.setStatus(model.AccountStatus{State: model.StateConnecting})
				return
			default:
				_ = a.pairingFailure(item)
				return
			}
		}
	}
}

// pairingFailure informa el fallo de la vinculación y devuelve el error equivalente.
func (a *Adapter) pairingFailure(item whatsmeow.QRChannelItem) error {
	detail := ""
	switch item.Event {
	case whatsmeow.QRChannelTimeout.Event:
		detail = "El código QR caducó. Vuelve a intentarlo."
	case whatsmeow.QRChannelClientOutdated.Event:
		detail = "WhatsApp rechazó esta versión de dConnector por estar desactualizada. Actualiza la aplicación."
	case whatsmeow.QRChannelScannedWithoutMultidevice.Event:
		detail = "Tu teléfono escaneó el código sin tener activados los dispositivos vinculados. Actualiza WhatsApp en el teléfono e inténtalo de nuevo."
	case whatsmeow.QRChannelErrUnexpectedEvent.Event:
		detail = "La cuenta ya estaba vinculada. Cierra la sesión y vuelve a intentarlo."
	case whatsmeow.QRChannelEventPasskeyRequest, whatsmeow.QRChannelEventPasskeyResponse:
		detail = "WhatsApp pidió una llave de acceso (passkey), que dConnector todavía no admite."
	default:
		detail = "No se pudo vincular la cuenta. Vuelve a intentarlo."
		if item.Error != nil {
			a.log.Warn("error al vincular", "err", redact(item.Error.Error()))
		}
	}
	a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedNone, Detail: detail})
	return fmt.Errorf("whatsapp: %s: %w", detail, port.ErrInvalid)
}
