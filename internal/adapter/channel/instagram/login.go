package instagram

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.mau.fi/mautrix-meta/pkg/instameow"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/util/exhttp"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/port"
)

// passwordDriver es el inicio de sesión web con usuario y contraseña (09 §4) que ofrece instameow:
// CreateInstagramWebSession y, si Instagram lo pide, CompleteInstagramWebSessionTwoFactor.
// Los pasos que exigen un navegador (captcha, verificación de identidad, aceptar cookies) no se pueden
// mostrar: el usuario vuelve a «Pegar cookies». Todo lo de red está sin probar con cuentas reales.
type passwordDriver struct {
	a *Adapter

	mu     sync.Mutex
	client *instameow.Client
	jar    *cookies.Cookies
}

func newPasswordDriver(a *Adapter) meta.PasswordDriver { return &passwordDriver{a: a} }

func (d *passwordDriver) Start(ctx context.Context, username, password string) (meta.PasswordStep, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	zl := meta.ZerologToSlog(d.a.Log)
	d.jar = &cookies.Cookies{Platform: types.Instagram}
	d.jar.UpdateValues(map[cookies.MetaCookieName]string{})
	d.client = instameow.NewClient(instameow.ClientParams{Cookies: d.jar, Log: zl, Settings: exhttp.ClientSettings{}})
	challenge, err := d.client.CreateInstagramWebSession(meta.LibContext(ctx, zl), username, password)
	if err != nil {
		return meta.PasswordStep{}, loginError(err)
	}
	if challenge == nil {
		return d.done(), nil
	}
	if challenge.AuthPlatform || (!challenge.TOTP && !challenge.SMS && !challenge.WhatsApp) {
		return meta.PasswordStep{Detail: "Instagram pide un paso extra (verificación de identidad o captcha) que dConnector no puede mostrar. Inicia sesión en instagram.com y pega las cookies."}, nil
	}
	detail := "Escribe el código de verificación en dos pasos de Instagram."
	switch {
	case challenge.TOTP:
		detail = "Escribe el código de tu app de autenticación."
	case challenge.SMS:
		detail = "Escribe el código que Instagram te envió por SMS."
	case challenge.WhatsApp:
		detail = "Escribe el código que Instagram te envió por WhatsApp."
	}
	return meta.PasswordStep{NeedCode: true, Detail: detail}, nil
}

func (d *passwordDriver) Code(ctx context.Context, code string) (meta.PasswordStep, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == nil {
		return meta.PasswordStep{}, fmt.Errorf("el inicio de sesión no está en curso: %w", port.ErrInvalid)
	}
	zl := meta.ZerologToSlog(d.a.Log)
	if err := d.client.CompleteInstagramWebSessionTwoFactor(meta.LibContext(ctx, zl), code); err != nil {
		if errors.Is(err, instameow.ErrInstagramWebTwoFactorCodeResent) {
			return meta.PasswordStep{NeedCode: true, Detail: "El código no valió; Instagram te envió uno nuevo por SMS."}, nil
		}
		return meta.PasswordStep{}, loginError(err)
	}
	return d.done(), nil
}

func (d *passwordDriver) done() meta.PasswordStep {
	all := d.jar.GetAll()
	out := make(map[string]string, len(all))
	for k, v := range all {
		out[string(k)] = v
	}
	return meta.PasswordStep{Done: true, Cookies: out}
}

func (d *passwordDriver) Cancel() {
	d.mu.Lock()
	d.client, d.jar = nil, nil
	d.mu.Unlock()
}

// loginError traduce el error del inicio de sesión a un texto en español.
func loginError(err error) error {
	switch {
	case errors.Is(err, instameow.ErrInstagramWebCredentialsRejected):
		return fmt.Errorf("el usuario o la contraseña no son correctos: %w", port.ErrInvalid)
	case errors.Is(err, instameow.ErrInstagramWebTwoFactorCodeRejected):
		return fmt.Errorf("el código no es correcto: %w", port.ErrInvalid)
	case errors.Is(err, instameow.ErrInstagramWebAccountPendingDeletion):
		return fmt.Errorf("esa cuenta de Instagram está pendiente de eliminación: %w", port.ErrInvalid)
	case errors.Is(err, instameow.ErrInstagramWebCheckpointUnsupported), errors.Is(err, instameow.ErrInstagramWebCheckpointRequestFailed),
		errors.Is(err, httpclient.ErrChallengeRequired), errors.Is(err, httpclient.ErrCheckpointRequired), errors.Is(err, httpclient.ErrConsentRequired):
		return fmt.Errorf("Instagram pide verificar tu cuenta en instagram.com; hazlo en el navegador y pega las cookies: %w", port.ErrInvalid)
	case errors.Is(err, httpclient.ErrRateLimited):
		return fmt.Errorf("Instagram limitó los intentos desde esta red; espera unos minutos: %w", port.ErrRateLimited)
	case errors.Is(err, instameow.ErrInstagramWebLoginRejected):
		return fmt.Errorf("Instagram rechazó el inicio de sesión; prueba con las cookies: %w", port.ErrInvalid)
	}
	return fmt.Errorf("no se pudo iniciar sesión: %s: %w", meta.Redact(err.Error()), port.ErrInvalid)
}
