package meta

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// PasswordStep es lo que devuelve cada paso del inicio de sesión con usuario y contraseña.
type PasswordStep struct {
	Done     bool              // la sesión quedó lista
	Cookies  map[string]string // las cookies de la sesión (si Done)
	NeedCode bool              // falta el código de verificación en dos pasos
	Detail   string            // texto para el usuario, en español (opcional)
}

// PasswordDriver es la parte de red del inicio de sesión con contraseña (09 §4). Cada plataforma
// pone el suyo; la máquina de estados (PairFlow) no sabe nada de red y se prueba con un doble.
type PasswordDriver interface {
	// Start recibe usuario y contraseña.
	Start(ctx context.Context, username, password string) (PasswordStep, error)
	// Code entrega el código de verificación pedido por el paso anterior.
	Code(ctx context.Context, code string) (PasswordStep, error)
	// Cancel suelta lo que el driver tenga en curso.
	Cancel()
}

type pairStage int

const (
	stageIdle pairStage = iota
	stageCookies
	stagePassword
	stageCode
)

// PairFlow es la máquina de estados de la vinculación de una cuenta de Meta: cookies o contraseña.
type PairFlow struct {
	core     *Core
	platform Platform
	name     string // «Messenger» | «Instagram»
	driver   PasswordDriver
	onReady  func(ctx context.Context, cookies map[string]string) error

	mu    sync.Mutex
	stage pairStage
}

// NewPairFlow crea la máquina. driver puede ser nil (sin inicio de sesión con contraseña).
// onReady recibe las cookies ya validadas: debe guardarlas y arrancar la conexión.
func NewPairFlow(core *Core, p Platform, driver PasswordDriver, onReady func(ctx context.Context, cookies map[string]string) error) *PairFlow {
	name := "Messenger"
	if p == PlatformInstagram {
		name = "Instagram"
	}
	return &PairFlow{core: core, platform: p, name: name, driver: driver, onReady: onReady}
}

// Textos fijos de la interfaz (en español).
func (f *PairFlow) cookiesDetail() string {
	site := "messenger.com"
	if f.platform == PlatformInstagram {
		site = "instagram.com"
	}
	return fmt.Sprintf("Inicia sesión en %s en tu navegador y pega aquí las cookies de la sesión (JSON o texto «nombre=valor; …»).", site)
}

func (f *PairFlow) passwordDetail() string {
	return fmt.Sprintf("Escribe tu usuario y tu contraseña de %s.", f.name)
}

const codeDetail = "Escribe el código de verificación en dos pasos que te enviaron."

// PromptCookies deja la cuenta esperando las cookies (lo usa Start cuando no hay sesión).
func (f *PairFlow) PromptCookies() {
	f.mu.Lock()
	f.stage = stageCookies
	f.mu.Unlock()
	f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedCookies, Detail: f.cookiesDetail()})
}

// Reset vuelve la máquina a reposo y suelta el driver.
func (f *PairFlow) Reset() {
	f.mu.Lock()
	f.stage = stageIdle
	f.mu.Unlock()
	if f.driver != nil {
		f.driver.Cancel()
	}
}

// Stage informa del paso actual (para pruebas): "idle" | "cookies" | "password" | "code".
func (f *PairFlow) Stage() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.stage {
	case stageCookies:
		return "cookies"
	case stagePassword:
		return "password"
	case stageCode:
		return "code"
	}
	return "idle"
}

// Begin arranca un método de vinculación: "cookies" o "password_login".
func (f *PairFlow) Begin(ctx context.Context, req model.PairRequest) error {
	switch req.Method {
	case "cookies", "":
		f.PromptCookies()
		return nil
	case "password_login":
		if f.driver == nil {
			return fmt.Errorf("%s: el inicio de sesión con contraseña no está disponible; usa «Pegar cookies»: %w", f.name, port.ErrUnsupported)
		}
		f.mu.Lock()
		f.stage = stagePassword
		f.mu.Unlock()
		f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedPassword, Detail: f.passwordDetail()})
		return nil
	default:
		return fmt.Errorf("%s: método de vinculación desconocido: %w", f.name, port.ErrInvalid)
	}
}

// Submit entrega lo que pidió el paso actual: cookies, usuario+contraseña o código.
func (f *PairFlow) Submit(ctx context.Context, in model.PairInput) error {
	f.mu.Lock()
	stage := f.stage
	f.mu.Unlock()
	// Cookies pegadas: valen en cualquier momento, aunque la interfaz no haya llamado a Begin.
	if strings.TrimSpace(in.Cookies) != "" {
		return f.submitCookies(ctx, in.Cookies)
	}
	switch stage {
	case stagePassword:
		return f.submitPassword(ctx, in)
	case stageCode:
		return f.submitCode(ctx, in)
	}
	return fmt.Errorf("%s: no hay ningún paso de vinculación en curso: %w", f.name, port.ErrInvalid)
}

func (f *PairFlow) submitCookies(ctx context.Context, text string) error {
	c, err := ValidateCookies(f.platform, text)
	if err != nil {
		msg := userText(err)
		f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedCookies, Detail: capitalize(msg)})
		f.core.Log.Info("cookies rechazadas", "reason", Redact(msg))
		return fmt.Errorf("%s: %w", f.name, err)
	}
	return f.ready(ctx, c)
}

func (f *PairFlow) ready(ctx context.Context, c map[string]string) error {
	f.core.Log.Info("sesión lista", "cookies", NamesOnly(c))
	if err := f.onReady(ctx, c); err != nil {
		f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedCookies, Detail: "No se pudo guardar la sesión. Inténtalo de nuevo."})
		return fmt.Errorf("%s: %w", f.name, err)
	}
	f.mu.Lock()
	f.stage = stageIdle
	f.mu.Unlock()
	return nil
}

func (f *PairFlow) submitPassword(ctx context.Context, in model.PairInput) error {
	user, pass := strings.TrimSpace(in.Username), in.Password
	if user == "" || pass == "" {
		msg := "Escribe el usuario y la contraseña."
		f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedPassword, Detail: msg})
		return fmt.Errorf("%s: %s: %w", f.name, strings.ToLower(msg[:1])+msg[1:], port.ErrInvalid)
	}
	step, err := f.driver.Start(ctx, user, pass)
	return f.afterStep(ctx, step, err, model.NeedPassword)
}

func (f *PairFlow) submitCode(ctx context.Context, in model.PairInput) error {
	code := strings.TrimSpace(in.Code)
	if code == "" {
		msg := "Escribe el código de verificación."
		f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedLoginCode, Detail: msg})
		return fmt.Errorf("%s: %s: %w", f.name, strings.ToLower(msg[:1])+msg[1:], port.ErrInvalid)
	}
	step, err := f.driver.Code(ctx, code)
	return f.afterStep(ctx, step, err, model.NeedLoginCode)
}

// afterStep traduce el resultado de un paso del driver al estado de la cuenta.
func (f *PairFlow) afterStep(ctx context.Context, step PasswordStep, err error, sameNeed model.PairNeed) error {
	if err != nil {
		msg := userText(err)
		f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: sameNeed, Detail: capitalize(msg)})
		f.core.Log.Info("el inicio de sesión con contraseña no avanzó", "reason", Redact(msg))
		if errors.Is(err, port.ErrInvalid) || errors.Is(err, port.ErrUnsupported) || errors.Is(err, port.ErrRateLimited) {
			return fmt.Errorf("%s: %w", f.name, err)
		}
		return fmt.Errorf("%s: %s: %w", f.name, msg, port.ErrInvalid)
	}
	switch {
	case step.Done:
		if missing := MissingCookies(f.platform, step.Cookies); len(missing) > 0 {
			f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedCookies,
				Detail: fmt.Sprintf("El inicio de sesión no devolvió la sesión completa (faltan: %s). Pega las cookies a mano.", strings.Join(missing, ", "))})
			f.mu.Lock()
			f.stage = stageCookies
			f.mu.Unlock()
			return fmt.Errorf("%s: el inicio de sesión no devolvió la sesión completa: %w", f.name, port.ErrInvalid)
		}
		return f.ready(ctx, step.Cookies)
	case step.NeedCode:
		f.mu.Lock()
		f.stage = stageCode
		f.mu.Unlock()
		detail := step.Detail
		if detail == "" {
			detail = codeDetail
		}
		f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedLoginCode, Detail: detail})
		return nil
	default:
		// Un paso que la interfaz no sabe mostrar: se explica y se vuelve a las cookies.
		detail := step.Detail
		if detail == "" {
			detail = "Meta pide un paso extra que dConnector no puede mostrar. Inicia sesión en el navegador y pega las cookies."
		}
		f.mu.Lock()
		f.stage = stageCookies
		f.mu.Unlock()
		f.core.SetStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedCookies, Detail: detail})
		return fmt.Errorf("%s: %s: %w", f.name, detail, port.ErrInvalid)
	}
}

// userText saca el texto del error sin el prefijo de dominio («datos no válidos»).
func userText(err error) string {
	s := err.Error()
	for _, suffix := range []string{
		": " + port.ErrInvalid.Error(), ": " + port.ErrUnsupported.Error(), ": " + port.ErrRateLimited.Error(),
	} {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] = r[0] - 'a' + 'A'
	}
	out := string(r)
	if !strings.HasSuffix(out, ".") {
		out += "."
	}
	return out
}
