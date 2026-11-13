package meta

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

type sinkRec struct {
	mu  sync.Mutex
	evs []model.Event
}

func (s *sinkRec) Emit(ev model.Event) {
	s.mu.Lock()
	s.evs = append(s.evs, ev)
	s.mu.Unlock()
}

func (s *sinkRec) last() model.AccountStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.evs) - 1; i >= 0; i-- {
		if s.evs[i].AccountStatus != nil {
			return *s.evs[i].AccountStatus
		}
	}
	return model.AccountStatus{}
}

func (s *sinkRec) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.evs)
}

func newTestCore(t *testing.T, ch model.Channel) (*Core, *sinkRec) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "account.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	core, err := NewCore(ch, port.AdapterDeps{
		Account: model.Account{ID: "fb-test", Channel: ch},
		DB:      db,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	sink := &sinkRec{}
	core.SetSink(sink)
	return core, sink
}

type fakeDriver struct {
	startStep PasswordStep
	startErr  error
	codeStep  PasswordStep
	codeErr   error
	gotUser   string
	gotPass   string
	gotCode   string
	cancelled bool
}

func (d *fakeDriver) Start(_ context.Context, u, p string) (PasswordStep, error) {
	d.gotUser, d.gotPass = u, p
	return d.startStep, d.startErr
}
func (d *fakeDriver) Code(_ context.Context, c string) (PasswordStep, error) {
	d.gotCode = c
	return d.codeStep, d.codeErr
}
func (d *fakeDriver) Cancel() { d.cancelled = true }

func TestPairCookiesFlow(t *testing.T) {
	core, sink := newTestCore(t, model.ChannelMessenger)
	var got map[string]string
	f := NewPairFlow(core, PlatformMessenger, nil, func(_ context.Context, c map[string]string) error { got = c; return nil })

	f.PromptCookies()
	st := sink.last()
	assert.Equal(t, model.StatePairing, st.State)
	assert.Equal(t, model.NeedCookies, st.Need)
	assert.Contains(t, st.Detail, "messenger.com")
	assert.Equal(t, "cookies", f.Stage())

	// Faltan cookies: se queda en el mismo paso, con el motivo, y devuelve ErrInvalid.
	err := f.Submit(context.Background(), model.PairInput{Cookies: "c_user=1000123; datr=d"})
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalid)
	st = sink.last()
	assert.Equal(t, model.NeedCookies, st.Need)
	assert.Contains(t, st.Detail, "xs")
	assert.Nil(t, got)

	// Válidas: se entregan a onReady y la máquina vuelve a reposo.
	require.NoError(t, f.Submit(context.Background(), model.PairInput{Cookies: "c_user=1000123; xs=x; datr=d"}))
	assert.Equal(t, "1000123", got["c_user"])
	assert.Equal(t, "idle", f.Stage())

	// Sin paso en curso y sin cookies: error.
	err = f.Submit(context.Background(), model.PairInput{})
	assert.ErrorIs(t, err, port.ErrInvalid)
}

func TestPairCookiesAcceptedWithoutBegin(t *testing.T) {
	core, _ := newTestCore(t, model.ChannelInstagram)
	called := false
	f := NewPairFlow(core, PlatformInstagram, nil, func(context.Context, map[string]string) error { called = true; return nil })
	require.NoError(t, f.Submit(context.Background(), model.PairInput{Cookies: "sessionid=s; ds_user_id=7; csrftoken=t"}))
	assert.True(t, called)
}

func TestPairOnReadyFailure(t *testing.T) {
	core, sink := newTestCore(t, model.ChannelMessenger)
	f := NewPairFlow(core, PlatformMessenger, nil, func(context.Context, map[string]string) error { return errors.New("disco lleno") })
	err := f.Submit(context.Background(), model.PairInput{Cookies: "c_user=1; xs=x; datr=d"})
	require.Error(t, err)
	assert.Equal(t, model.NeedCookies, sink.last().Need)
}

func TestPairBeginMethods(t *testing.T) {
	core, sink := newTestCore(t, model.ChannelMessenger)
	f := NewPairFlow(core, PlatformMessenger, nil, func(context.Context, map[string]string) error { return nil })
	assert.ErrorIs(t, f.Begin(context.Background(), model.PairRequest{Method: "password_login"}), port.ErrUnsupported, "sin driver")
	assert.ErrorIs(t, f.Begin(context.Background(), model.PairRequest{Method: "qr"}), port.ErrInvalid)
	require.NoError(t, f.Begin(context.Background(), model.PairRequest{Method: "cookies"}))
	assert.Equal(t, model.NeedCookies, sink.last().Need)
	assert.Contains(t, sink.last().Detail, "Inicia sesión")
}

func TestPairPasswordFlowWithCode(t *testing.T) {
	core, sink := newTestCore(t, model.ChannelInstagram)
	drv := &fakeDriver{startStep: PasswordStep{NeedCode: true, Detail: "Escribe el código de tu app."}}
	var got map[string]string
	f := NewPairFlow(core, PlatformInstagram, drv, func(_ context.Context, c map[string]string) error { got = c; return nil })

	require.NoError(t, f.Begin(context.Background(), model.PairRequest{Method: "password_login"}))
	assert.Equal(t, model.NeedPassword, sink.last().Need)
	assert.Equal(t, "password", f.Stage())

	// Faltan datos.
	err := f.Submit(context.Background(), model.PairInput{Username: "ana"})
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Equal(t, model.NeedPassword, sink.last().Need)

	// Usuario y contraseña → pide el código.
	require.NoError(t, f.Submit(context.Background(), model.PairInput{Username: " ana ", Password: "clave"}))
	assert.Equal(t, "ana", drv.gotUser)
	assert.Equal(t, "clave", drv.gotPass)
	assert.Equal(t, model.NeedLoginCode, sink.last().Need)
	assert.Equal(t, "Escribe el código de tu app.", sink.last().Detail)
	assert.Equal(t, "code", f.Stage())

	// Código incorrecto: sigue pidiendo el código y devuelve el error.
	drv.codeErr = errors.New("el código no es correcto: " + port.ErrInvalid.Error())
	err = f.Submit(context.Background(), model.PairInput{Code: "000000"})
	require.Error(t, err)
	assert.Equal(t, model.NeedLoginCode, sink.last().Need)
	assert.Equal(t, "code", f.Stage())

	// Código bueno: sesión lista.
	drv.codeErr = nil
	drv.codeStep = PasswordStep{Done: true, Cookies: map[string]string{"sessionid": "s", "ds_user_id": "7", "csrftoken": "t"}}
	require.NoError(t, f.Submit(context.Background(), model.PairInput{Code: " 123456 "}))
	assert.Equal(t, "123456", drv.gotCode)
	assert.Equal(t, "7", got["ds_user_id"])
	assert.Equal(t, "idle", f.Stage())
}

func TestPairPasswordDoneWithoutCompleteSessionFallsBackToCookies(t *testing.T) {
	core, sink := newTestCore(t, model.ChannelMessenger)
	drv := &fakeDriver{startStep: PasswordStep{Done: true, Cookies: map[string]string{"c_user": "1"}}}
	called := false
	f := NewPairFlow(core, PlatformMessenger, drv, func(context.Context, map[string]string) error { called = true; return nil })
	require.NoError(t, f.Begin(context.Background(), model.PairRequest{Method: "password_login"}))
	err := f.Submit(context.Background(), model.PairInput{Username: "a", Password: "b"})
	require.Error(t, err)
	assert.False(t, called)
	assert.Equal(t, model.NeedCookies, sink.last().Need)
	assert.Contains(t, sink.last().Detail, "xs")
	assert.Equal(t, "cookies", f.Stage())
}

func TestPairPasswordUnsupportedStepFallsBackToCookies(t *testing.T) {
	core, sink := newTestCore(t, model.ChannelMessenger)
	drv := &fakeDriver{startStep: PasswordStep{Detail: "Meta pide un captcha."}}
	f := NewPairFlow(core, PlatformMessenger, drv, func(context.Context, map[string]string) error { return nil })
	require.NoError(t, f.Begin(context.Background(), model.PairRequest{Method: "password_login"}))
	err := f.Submit(context.Background(), model.PairInput{Username: "a", Password: "b"})
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Equal(t, model.NeedCookies, sink.last().Need)
	assert.Equal(t, "Meta pide un captcha.", sink.last().Detail)
}

func TestPairPasswordDriverErrorKeepsStep(t *testing.T) {
	core, sink := newTestCore(t, model.ChannelMessenger)
	drv := &fakeDriver{startErr: errors.New("el usuario o la contraseña no son correctos: " + port.ErrInvalid.Error())}
	f := NewPairFlow(core, PlatformMessenger, drv, func(context.Context, map[string]string) error { return nil })
	require.NoError(t, f.Begin(context.Background(), model.PairRequest{Method: "password_login"}))
	err := f.Submit(context.Background(), model.PairInput{Username: "a", Password: "b"})
	require.Error(t, err)
	assert.Equal(t, model.NeedPassword, sink.last().Need)
	assert.Contains(t, sink.last().Detail, "contraseña")
	f.Reset()
	assert.True(t, drv.cancelled)
	assert.Equal(t, "idle", f.Stage())
}

func TestSetStatusDeduplicates(t *testing.T) {
	core, sink := newTestCore(t, model.ChannelMessenger)
	core.SetStatus(model.AccountStatus{State: model.StateConnecting})
	core.SetStatus(model.AccountStatus{State: model.StateConnecting})
	assert.Equal(t, 1, sink.count())
	core.SetStatus(model.AccountStatus{State: model.StateConnected})
	assert.Equal(t, 2, sink.count())
	assert.Equal(t, model.AccountID("fb-test"), sink.last().AccountID)
	assert.NotZero(t, sink.last().Since)
}
