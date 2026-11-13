package messenger

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
	"maunium.net/go/mautrix/bridgev2"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/port"
)

// passwordDriver es el inicio de sesión con usuario y contraseña (09 §4): el flujo de la app «Messenger Lite»
// de iOS que ofrece la librería (Bloks). Devuelve las cookies de la sesión como cualquier otro método.
// Todo lo de red está sin probar con cuentas reales (QA-EN-VIVO).
type passwordDriver struct {
	a *Adapter

	mu     sync.Mutex
	client *messagix.Client
	input  map[string]string
	asked  int
}

func newPasswordDriver(a *Adapter) meta.PasswordDriver { return &passwordDriver{a: a} }

func (d *passwordDriver) Start(ctx context.Context, username, password string) (meta.PasswordStep, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	zl := meta.ZerologToSlog(d.a.Log)
	jar := &cookies.Cookies{Platform: types.MessengerLiteIOS}
	jar.UpdateValues(map[cookies.MetaCookieName]string{})
	d.client = messagix.NewClient(jar, zl, &messagix.Config{})
	d.input = map[string]string{"username": username, "password": password}
	d.asked = 0
	return d.proceed(meta.LibContext(ctx, zl))
}

func (d *passwordDriver) Code(ctx context.Context, code string) (meta.PasswordStep, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == nil {
		return meta.PasswordStep{}, fmt.Errorf("el inicio de sesión no está en curso: %w", port.ErrInvalid)
	}
	d.input["otp_code"] = code
	d.input["backup_code"] = code
	return d.proceed(meta.LibContext(ctx, meta.ZerologToSlog(d.a.Log)))
}

func (d *passwordDriver) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client != nil && d.client.MessengerLite != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*meta.StopWait/6)
		_ = d.client.MessengerLite.CancelLoginStep(ctx)
		cancel()
	}
	d.client, d.input = nil, nil
}

// proceed avanza un paso del flujo con lo que se tiene (usuario, contraseña, código).
func (d *passwordDriver) proceed(ctx context.Context) (meta.PasswordStep, error) {
	step, jar, err := d.client.MessengerLite.DoLoginSteps(ctx, d.input)
	if err != nil {
		return meta.PasswordStep{}, loginError(err)
	}
	if step == nil {
		if jar == nil {
			return meta.PasswordStep{}, fmt.Errorf("Messenger no devolvió la sesión: %w", port.ErrInvalid)
		}
		all := jar.GetAll()
		out := make(map[string]string, len(all))
		for k, v := range all {
			out[string(k)] = v
		}
		_ = d.a.KV.Set(ctx, meta.KeyPlatform, []byte(platformLite))
		return meta.PasswordStep{Done: true, Cookies: out}, nil
	}
	if p := step.UserInputParams; p != nil {
		for _, f := range p.Fields {
			switch f.ID {
			case "otp_code", "backup_code":
				return meta.PasswordStep{NeedCode: true}, nil
			case "username", "password":
				d.asked++
				if d.asked > 1 {
					return meta.PasswordStep{}, fmt.Errorf("el usuario o la contraseña no son correctos: %w", port.ErrInvalid)
				}
			}
		}
	}
	// Captcha, elegir otra forma de verificar, aprobar en otro dispositivo…: la interfaz no lo sabe mostrar.
	return meta.PasswordStep{Detail: "Meta pide un paso extra (verificación, captcha o aprobar en otro dispositivo) que dConnector no puede mostrar. Inicia sesión en messenger.com y pega las cookies."}, nil
}

// loginError traduce el error del flujo a un texto en español.
func loginError(err error) error {
	var re bridgev2.RespError
	if errors.As(err, &re) {
		switch re.ErrCode {
		case "FI.MAU.META_CHALLENGE_ERROR", "FI.MAU.META_CONSENT_ERROR", "FI.MAU.META_CHECKPOINT_ERROR":
			return fmt.Errorf("Meta pide verificar tu cuenta en messenger.com. Hazlo en el navegador y pega las cookies: %w", port.ErrInvalid)
		case "FI.MAU.META_PHONE_NUMBER", "FI.MAU.META_MATRIX_ID":
			return fmt.Errorf("escribe tu correo o tu nombre de usuario de Facebook (no el teléfono): %w", port.ErrInvalid)
		case "FI.MAU.META_AFAD_STOPPED":
			return fmt.Errorf("la aprobación venció o se negó; inténtalo de nuevo: %w", port.ErrInvalid)
		case "FI.MAU.META_RATE_LIMITED":
			return fmt.Errorf("Meta limitó los intentos desde esta red; espera unos minutos: %w", port.ErrRateLimited)
		case "FI.MAU.META_MISSING_COOKIES", "FI.MAU.META_TOKEN_EXCHANGE_FAILED", "FI.MAU.META_TOKEN_ERROR":
			return fmt.Errorf("Meta no devolvió la sesión completa; inicia sesión en messenger.com y pega las cookies: %w", port.ErrInvalid)
		}
		return fmt.Errorf("Meta rechazó el inicio de sesión (%s): %w", re.ErrCode, port.ErrInvalid)
	}
	return fmt.Errorf("no se pudo iniciar sesión: %s: %w", meta.Redact(err.Error()), port.ErrInvalid)
}

var _ = bridgev2.LoginStep{}
