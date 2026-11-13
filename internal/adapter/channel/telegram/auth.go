package telegram

// Vinculación (08 §3): teléfono → código → contraseña de verificación en dos pasos (si la tiene).
// pairFlow es una máquina de estados sin red: habla con Telegram por la interfaz authAPI, de modo
// que se prueba con un doble.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// authAPI: lo que la vinculación necesita del cliente de Telegram.
type authAPI interface {
	// SendCode pide el código al número. signedIn es true si Telegram autorizó sin pedir código.
	SendCode(ctx context.Context, phone string) (hash string, signedIn bool, err error)
	// SignIn entrega el código. Devuelve auth.ErrPasswordAuthNeeded si hace falta la contraseña y
	// *auth.SignUpRequired si el número no tiene cuenta.
	SignIn(ctx context.Context, phone, code, hash string) error
	// Password entrega la contraseña de verificación en dos pasos.
	Password(ctx context.Context, password string) error
}

// realAuth implementa authAPI sobre *auth.Client de gotd.
type realAuth struct{ c *auth.Client }

func (r realAuth) SendCode(ctx context.Context, phone string) (string, bool, error) {
	sent, err := r.c.SendCode(ctx, phone, auth.SendCodeOptions{})
	if err != nil {
		return "", false, err
	}
	switch s := sent.(type) {
	case *tg.AuthSentCode:
		return s.PhoneCodeHash, false, nil
	case *tg.AuthSentCodeSuccess:
		return "", true, nil
	}
	return "", false, fmt.Errorf("telegram: respuesta inesperada al pedir el código: %T", sent)
}

func (r realAuth) SignIn(ctx context.Context, phone, code, hash string) error {
	_, err := r.c.SignIn(ctx, phone, code, hash)
	return err
}

func (r realAuth) Password(ctx context.Context, password string) error {
	_, err := r.c.Password(ctx, password)
	return err
}

// Textos de estado de la vinculación.
const (
	detailAskCode      = "Escribe el código que Telegram te envió (a la app de Telegram o por SMS)."
	detailAskPassword  = "Esta cuenta tiene verificación en dos pasos. Escribe tu contraseña de Telegram."
	detailBadCode      = "Código incorrecto"
	detailBadPassword  = "Contraseña incorrecta"
	detailCodeExpired  = "El código caducó. Escribe el número otra vez para pedir uno nuevo."
	detailBadPhone     = "El número no es válido. Escríbelo con el código de país, por ejemplo +593991234567."
	detailNoAccount    = "Ese número no tiene cuenta de Telegram. Crea la cuenta desde la app de Telegram y vuelve a vincular."
	detailNeedPhone    = "Escribe el número de teléfono de la cuenta de Telegram."
	detailMissingKeys  = "Falta api_id/api_hash"
	detailConnecting   = "Conectando con Telegram…"
	detailFloodWaitFmt = "Telegram pidió esperar %s antes de volver a intentarlo."
)

// pairFlow guarda el punto en que va la vinculación.
type pairFlow struct {
	mu     sync.Mutex
	api    authAPI
	phone  string
	hash   string
	step   model.PairNeed
	status func(model.AccountStatus) // informa el estado (el adaptador lo emite)
	done   func()                    // se llama una vez, al quedar autorizada la cuenta
}

func newPairFlow(status func(model.AccountStatus), done func()) *pairFlow {
	return &pairFlow{status: status, done: done}
}

// attach asocia el cliente de autenticación y deja el flujo esperando el teléfono.
func (p *pairFlow) attach(api authAPI) {
	p.mu.Lock()
	p.api = api
	p.phone, p.hash = "", ""
	p.step = model.NeedPhone
	p.mu.Unlock()
}

// detach suelta el cliente (la conexión se cerró).
func (p *pairFlow) detach() {
	p.mu.Lock()
	p.api = nil
	p.step = model.NeedNone
	p.mu.Unlock()
}

// current devuelve el paso actual.
func (p *pairFlow) current() model.PairNeed {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.step
}

func (p *pairFlow) pairing(need model.PairNeed, detail string) {
	p.step = need
	if p.status != nil {
		p.status(model.AccountStatus{State: model.StatePairing, Need: need, Detail: detail})
	}
}

func invalid(msg string) error { return fmt.Errorf("telegram: %s: %w", msg, port.ErrInvalid) }

// normalizePhone deja el número como +<dígitos>. Pide al menos 7 dígitos.
func normalizePhone(s string) (string, error) {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if len(digits) < 7 || len(digits) > 15 || strings.HasPrefix(digits, "0") {
		return "", invalid(detailBadPhone)
	}
	return "+" + digits, nil
}

// cleanCode quita espacios y guiones del código que pega el usuario.
func cleanCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// floodErr traduce una espera pedida por Telegram a port.ErrRateLimited.
func floodErr(err error) error {
	if d, ok := tgerr.AsFloodWait(err); ok {
		return fmt.Errorf("telegram: "+detailFloodWaitFmt+": %w", d.Round(time.Second), port.ErrRateLimited)
	}
	return nil
}

// begin arranca la vinculación: pide el código al número.
func (p *pairFlow) begin(ctx context.Context, req model.PairRequest) error {
	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method != "" && method != "login" && method != "phone" {
		return invalid(fmt.Sprintf("método de vinculación %q no admitido", req.Method))
	}
	phone, err := normalizePhone(req.Phone)
	if err != nil {
		return err
	}
	p.mu.Lock()
	api := p.api
	p.mu.Unlock()
	if api == nil {
		return fmt.Errorf("telegram: todavía no hay conexión con Telegram, inténtalo en unos segundos: %w", port.ErrNotConnected)
	}
	hash, signedIn, err := api.SendCode(ctx, phone)
	if err != nil {
		return p.sendCodeError(err)
	}
	if signedIn {
		p.finish()
		return nil
	}
	p.mu.Lock()
	p.phone, p.hash = phone, hash
	p.pairing(model.NeedLoginCode, detailAskCode)
	p.mu.Unlock()
	return nil
}

func (p *pairFlow) sendCodeError(err error) error {
	if fe := floodErr(err); fe != nil {
		return fe
	}
	switch {
	case tgerr.Is(err, "PHONE_NUMBER_INVALID", "PHONE_NUMBER_BANNED", "PHONE_NUMBER_FLOOD"):
		p.mu.Lock()
		p.pairing(model.NeedPhone, detailBadPhone)
		p.mu.Unlock()
		return invalid(detailBadPhone)
	case tgerr.Is(err, "API_ID_INVALID", "API_ID_PUBLISHED_FLOOD"):
		p.mu.Lock()
		p.pairing(model.NeedPhone, "El api_id/api_hash no es válido. Revisa los datos de la cuenta.")
		p.mu.Unlock()
		return invalid("el api_id/api_hash no es válido")
	}
	return fmt.Errorf("telegram: no se pudo pedir el código: %w", err)
}

// submit entrega el código o la contraseña, según el paso.
func (p *pairFlow) submit(ctx context.Context, in model.PairInput) error {
	p.mu.Lock()
	api, step, phone, hash := p.api, p.step, p.phone, p.hash
	p.mu.Unlock()
	if api == nil {
		return fmt.Errorf("telegram: todavía no hay conexión con Telegram, inténtalo en unos segundos: %w", port.ErrNotConnected)
	}
	switch step {
	case model.NeedLoginCode:
		code := cleanCode(in.Code)
		if code == "" {
			return invalid("escribe el código que te envió Telegram")
		}
		return p.signIn(ctx, api, phone, code, hash)
	case model.NeedPassword:
		if in.Password == "" {
			return invalid("escribe tu contraseña de Telegram")
		}
		return p.password(ctx, api, in.Password)
	}
	return invalid("primero escribe el número de teléfono")
}

func (p *pairFlow) signIn(ctx context.Context, api authAPI, phone, code, hash string) error {
	err := api.SignIn(ctx, phone, code, hash)
	if err == nil {
		p.finish()
		return nil
	}
	if fe := floodErr(err); fe != nil {
		return fe
	}
	var signUp *auth.SignUpRequired
	switch {
	case errors.Is(err, auth.ErrPasswordAuthNeeded):
		p.mu.Lock()
		p.pairing(model.NeedPassword, detailAskPassword)
		p.mu.Unlock()
		return nil
	case errors.As(err, &signUp):
		p.mu.Lock()
		p.pairing(model.NeedPhone, detailNoAccount)
		p.mu.Unlock()
		return invalid(detailNoAccount)
	case tgerr.Is(err, "PHONE_CODE_INVALID", "PHONE_CODE_EMPTY"):
		p.mu.Lock()
		p.pairing(model.NeedLoginCode, detailBadCode)
		p.mu.Unlock()
		return invalid(detailBadCode)
	case tgerr.Is(err, "PHONE_CODE_EXPIRED"):
		p.mu.Lock()
		p.pairing(model.NeedPhone, detailCodeExpired)
		p.mu.Unlock()
		return invalid(detailCodeExpired)
	}
	return fmt.Errorf("telegram: no se pudo iniciar sesión: %w", err)
}

func (p *pairFlow) password(ctx context.Context, api authAPI, password string) error {
	err := api.Password(ctx, password)
	if err == nil {
		p.finish()
		return nil
	}
	if fe := floodErr(err); fe != nil {
		return fe
	}
	if errors.Is(err, auth.ErrPasswordInvalid) || tgerr.Is(err, "PASSWORD_HASH_INVALID") {
		p.mu.Lock()
		p.pairing(model.NeedPassword, detailBadPassword)
		p.mu.Unlock()
		return invalid(detailBadPassword)
	}
	return fmt.Errorf("telegram: no se pudo comprobar la contraseña: %w", err)
}

// finish cierra la vinculación con éxito: avisa y deja que la sesión siga con el arranque.
func (p *pairFlow) finish() {
	p.mu.Lock()
	p.step = model.NeedNone
	p.phone, p.hash = "", ""
	status, done := p.status, p.done
	p.mu.Unlock()
	if status != nil {
		status(model.AccountStatus{State: model.StateConnecting, Detail: detailConnecting})
	}
	if done != nil {
		done()
	}
}

// ---------------------------------------------------------------------------------------------
// Métodos del adaptador

// BeginPairing arranca la vinculación por teléfono (método "login").
func (a *Adapter) BeginPairing(ctx context.Context, req model.PairRequest) error {
	if !a.hasKeys() {
		a.setStatus(model.AccountStatus{State: model.StatePairing, Need: model.NeedNone, Detail: detailMissingKeys})
		return fmt.Errorf("telegram: %s: %w", detailMissingKeys, port.ErrInvalid)
	}
	if a.live.Load() {
		return nil // la cuenta ya está vinculada y conectada
	}
	if err := a.waitAuthReady(ctx); err != nil {
		return err
	}
	return a.pair.begin(ctx, req)
}

// SubmitPairing entrega el código o la contraseña.
func (a *Adapter) SubmitPairing(ctx context.Context, in model.PairInput) error {
	if !a.hasKeys() {
		return fmt.Errorf("telegram: %s: %w", detailMissingKeys, port.ErrInvalid)
	}
	return a.pair.submit(ctx, in)
}
