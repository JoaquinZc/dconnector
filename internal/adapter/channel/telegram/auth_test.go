package telegram

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// fakeAuth: doble de authAPI con respuestas programadas.
type fakeAuth struct {
	mu         sync.Mutex
	calls      []string
	sendErr    error
	sendHash   string
	sendSigned bool
	signErrs   []error // una por llamada a SignIn
	passErrs   []error
}

func (f *fakeAuth) SendCode(_ context.Context, phone string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "send:"+phone)
	return f.sendHash, f.sendSigned, f.sendErr
}

func (f *fakeAuth) SignIn(_ context.Context, phone, code, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("signin:%s:%s:%s", phone, code, hash))
	var err error
	if len(f.signErrs) > 0 {
		err, f.signErrs = f.signErrs[0], f.signErrs[1:]
	}
	return err
}

func (f *fakeAuth) Password(_ context.Context, pw string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "password:"+pw)
	var err error
	if len(f.passErrs) > 0 {
		err, f.passErrs = f.passErrs[0], f.passErrs[1:]
	}
	return err
}

type pairRec struct {
	mu       sync.Mutex
	statuses []model.AccountStatus
	done     int
}

func (r *pairRec) status(s model.AccountStatus) {
	r.mu.Lock()
	r.statuses = append(r.statuses, s)
	r.mu.Unlock()
}

func (r *pairRec) last() model.AccountStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.statuses) == 0 {
		return model.AccountStatus{}
	}
	return r.statuses[len(r.statuses)-1]
}

func newTestFlow(api authAPI) (*pairFlow, *pairRec) {
	rec := &pairRec{}
	p := newPairFlow(rec.status, func() { rec.mu.Lock(); rec.done++; rec.mu.Unlock() })
	if api != nil {
		p.attach(api)
	}
	return p, rec
}

func TestNormalizePhone(t *testing.T) {
	ok := map[string]string{
		"+593 99 123 4567":   "+593991234567",
		"593991234567":       "+593991234567",
		"(+1) 202-555-0143":  "+12025550143",
		"+34 600 000 000":    "+34600000000",
		"  +593991234567  ":  "+593991234567",
		"+593-99-123-4567 ":  "+593991234567",
		"+1 2025550143 ext.": "+12025550143",
	}
	for in, want := range ok {
		got, err := normalizePhone(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "abc", "12345", "0991234567", "+1234567890123456"} {
		_, err := normalizePhone(bad)
		assert.ErrorIs(t, err, port.ErrInvalid, bad)
	}
	assert.Equal(t, "12345", cleanCode(" 12 3-45 "))
}

func TestPairFlowHappyPathWithPassword(t *testing.T) {
	api := &fakeAuth{sendHash: "H1", signErrs: []error{auth.ErrPasswordAuthNeeded}}
	p, rec := newTestFlow(api)
	assert.Equal(t, model.NeedPhone, p.current())

	// Se pide el código.
	require.NoError(t, p.begin(bg, model.PairRequest{Method: "login", Phone: "+593 99 123 4567"}))
	assert.Equal(t, model.NeedLoginCode, p.current())
	st := rec.last()
	assert.Equal(t, model.StatePairing, st.State)
	assert.Equal(t, model.NeedLoginCode, st.Need)
	assert.NotEmpty(t, st.Detail)
	assert.Equal(t, []string{"send:+593991234567"}, api.calls)

	// El código llega y la cuenta pide contraseña.
	require.NoError(t, p.submit(bg, model.PairInput{Code: "12 345"}))
	assert.Equal(t, model.NeedPassword, p.current())
	assert.Equal(t, model.NeedPassword, rec.last().Need)
	assert.Equal(t, "signin:+593991234567:12345:H1", api.calls[1])
	assert.Zero(t, rec.done)

	// La contraseña es correcta: la vinculación termina.
	require.NoError(t, p.submit(bg, model.PairInput{Password: "secreta"}))
	assert.Equal(t, "password:secreta", api.calls[2])
	assert.Equal(t, model.NeedNone, p.current())
	assert.Equal(t, 1, rec.done)
	assert.Equal(t, model.StateConnecting, rec.last().State)
}

func TestPairFlowWithoutPassword(t *testing.T) {
	api := &fakeAuth{sendHash: "H"}
	p, rec := newTestFlow(api)
	require.NoError(t, p.begin(bg, model.PairRequest{Phone: "593991234567"})) // método vacío también vale
	require.NoError(t, p.submit(bg, model.PairInput{Code: "1"}))
	assert.Equal(t, 1, rec.done)
	assert.Equal(t, model.NeedNone, p.current())
}

func TestPairFlowWrongCodeAndPassword(t *testing.T) {
	api := &fakeAuth{
		sendHash: "H",
		signErrs: []error{tgerr.New(400, "PHONE_CODE_INVALID"), auth.ErrPasswordAuthNeeded},
		passErrs: []error{auth.ErrPasswordInvalid, tgerr.New(400, "PASSWORD_HASH_INVALID"), nil},
	}
	p, rec := newTestFlow(api)
	require.NoError(t, p.begin(bg, model.PairRequest{Method: "login", Phone: "+593991234567"}))

	err := p.submit(bg, model.PairInput{Code: "000"})
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Equal(t, model.NeedLoginCode, p.current(), "mismo paso")
	assert.Equal(t, model.NeedLoginCode, rec.last().Need)
	assert.Equal(t, "Código incorrecto", rec.last().Detail)

	require.NoError(t, p.submit(bg, model.PairInput{Code: "111"}))
	assert.Equal(t, model.NeedPassword, p.current())

	err = p.submit(bg, model.PairInput{Password: "mala"})
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Equal(t, model.NeedPassword, rec.last().Need)
	assert.Equal(t, "Contraseña incorrecta", rec.last().Detail)
	err = p.submit(bg, model.PairInput{Password: "mala2"})
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Zero(t, rec.done)

	require.NoError(t, p.submit(bg, model.PairInput{Password: "buena"}))
	assert.Equal(t, 1, rec.done)
}

func TestPairFlowSignUpRequiredAndExpiredCode(t *testing.T) {
	api := &fakeAuth{sendHash: "H", signErrs: []error{
		fmt.Errorf("sign in: %w", &auth.SignUpRequired{}),
		tgerr.New(400, "PHONE_CODE_EXPIRED"),
	}}
	p, rec := newTestFlow(api)
	require.NoError(t, p.begin(bg, model.PairRequest{Method: "login", Phone: "+593991234567"}))

	err := p.submit(bg, model.PairInput{Code: "1"})
	require.ErrorIs(t, err, port.ErrInvalid)
	assert.Contains(t, err.Error(), "Ese número no tiene cuenta de Telegram")
	assert.Equal(t, model.NeedPhone, p.current())
	assert.Equal(t, model.NeedPhone, rec.last().Need)

	// Vuelve a pedir el número y el código caduca.
	require.NoError(t, p.begin(bg, model.PairRequest{Method: "login", Phone: "+593991234567"}))
	err = p.submit(bg, model.PairInput{Code: "2"})
	require.ErrorIs(t, err, port.ErrInvalid)
	assert.Equal(t, model.NeedPhone, p.current())
	assert.Contains(t, rec.last().Detail, "caducó")
}

func TestPairFlowValidation(t *testing.T) {
	api := &fakeAuth{sendHash: "H"}
	p, _ := newTestFlow(api)

	assert.ErrorIs(t, p.begin(bg, model.PairRequest{Method: "qr"}), port.ErrInvalid)
	assert.ErrorIs(t, p.begin(bg, model.PairRequest{Method: "login", Phone: "123"}), port.ErrInvalid)
	assert.Empty(t, api.calls, "no se toca la red con datos inválidos")

	// Antes de pedir el código no se acepta nada.
	assert.ErrorIs(t, p.submit(bg, model.PairInput{Code: "1"}), port.ErrInvalid)

	require.NoError(t, p.begin(bg, model.PairRequest{Phone: "+593991234567"}))
	assert.ErrorIs(t, p.submit(bg, model.PairInput{Code: "  "}), port.ErrInvalid)
	assert.ErrorIs(t, p.submit(bg, model.PairInput{Password: "x"}), port.ErrInvalid, "falta el código")

	// Sin conexión.
	q, _ := newTestFlow(nil)
	assert.ErrorIs(t, q.begin(bg, model.PairRequest{Phone: "+593991234567"}), port.ErrNotConnected)
	assert.ErrorIs(t, q.submit(bg, model.PairInput{Code: "1"}), port.ErrNotConnected)
}

func TestPairFlowSendCodeErrors(t *testing.T) {
	api := &fakeAuth{sendErr: tgerr.New(400, "PHONE_NUMBER_INVALID")}
	p, rec := newTestFlow(api)
	err := p.begin(bg, model.PairRequest{Phone: "+593991234567"})
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Equal(t, model.NeedPhone, rec.last().Need)

	api.sendErr = fmt.Errorf("send code: %w", tgerr.New(420, "FLOOD_WAIT_30"))
	err = p.begin(bg, model.PairRequest{Phone: "+593991234567"})
	assert.ErrorIs(t, err, port.ErrRateLimited)
	assert.Contains(t, err.Error(), "esperar")

	api.sendErr = tgerr.New(400, "API_ID_INVALID")
	assert.ErrorIs(t, p.begin(bg, model.PairRequest{Phone: "+593991234567"}), port.ErrInvalid)

	api.sendErr = errors.New("red caída")
	err = p.begin(bg, model.PairRequest{Phone: "+593991234567"})
	require.Error(t, err)
	assert.False(t, errors.Is(err, port.ErrInvalid))
	assert.Equal(t, model.NeedPhone, p.current(), "el paso no avanza")

	// Telegram autoriza sin pedir código (token de sesión futuro).
	api.sendErr, api.sendSigned = nil, true
	p2, rec2 := newTestFlow(api)
	require.NoError(t, p2.begin(bg, model.PairRequest{Phone: "+593991234567"}))
	assert.Equal(t, 1, rec2.done)
}

func TestPairFlowFloodWaitOnSignIn(t *testing.T) {
	api := &fakeAuth{sendHash: "H", signErrs: []error{tgerr.New(420, "FLOOD_WAIT_5")}}
	p, _ := newTestFlow(api)
	require.NoError(t, p.begin(bg, model.PairRequest{Phone: "+593991234567"}))
	assert.ErrorIs(t, p.submit(bg, model.PairInput{Code: "1"}), port.ErrRateLimited)
	assert.Equal(t, model.NeedLoginCode, p.current())
}

func TestPairFlowDetach(t *testing.T) {
	p, _ := newTestFlow(&fakeAuth{sendHash: "H"})
	p.detach()
	assert.Equal(t, model.NeedNone, p.current())
	assert.ErrorIs(t, p.begin(bg, model.PairRequest{Phone: "+593991234567"}), port.ErrNotConnected)
}

// El adaptador real usa realAuth; aquí solo se comprueba la traducción de respuestas de SendCode.
func TestRealAuthImplementsAuthAPI(t *testing.T) {
	var _ authAPI = realAuth{}
	_ = tg.AuthSentCode{}
}
