package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

func TestAccountsCreate(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	acc, err := e.accounts.Create(ctx, model.ChannelWhatsApp, "Ventas", "", map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if acc.Color != "#25D366" || acc.Label != "Ventas" || !acc.Enabled || acc.Config["k"] != "v" || acc.CreatedAt == 0 {
		t.Fatalf("cuenta: %+v", acc)
	}
	if len(acc.ID) < 4 || acc.ID[:3] != "wa-" {
		t.Fatalf("id: %s", acc.ID)
	}
	for _, d := range []string{paths.AccountDir(acc.ID), paths.MediaDir(acc.ID), paths.AvatarDir(acc.ID)} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("falta la carpeta %s", d)
		}
	}
	saved, err := e.app.GetAccount(ctx, acc.ID)
	if err != nil || saved.Label != "Ventas" {
		t.Fatalf("no se guardó: %+v %v", saved, err)
	}
	var pacing model.PacingSettings
	raw, _ := e.app.GetSetting(ctx, "pacing:"+string(acc.ID))
	if err := json.Unmarshal([]byte(raw), &pacing); err != nil || pacing != model.DefaultPacing {
		t.Fatalf("pacing:<id> = %q (%v)", raw, err)
	}
	ad := e.adapter(acc.ID)
	if ad.deps.DBPath != paths.AccountDBPath(acc.ID) || ad.deps.MediaDir != paths.MediaDir(acc.ID) || ad.deps.AvatarDir != paths.AvatarDir(acc.ID) {
		t.Fatalf("deps del adaptador: %+v", ad.deps)
	}
	e.waitState(acc.ID, model.StateConnected)

	// Colores: el siguiente de la paleta que no use otra cuenta; las cuentas nuevas no repiten.
	second, _ := e.accounts.Create(ctx, model.ChannelTelegram, "", "", nil)
	if second.Color != "#229ED9" || second.Label != "Telegram" {
		t.Fatalf("segunda cuenta: %+v", second)
	}
	custom, _ := e.accounts.Create(ctx, model.ChannelInstagram, "IG", "#8b5cf6", nil)
	third, _ := e.accounts.Create(ctx, model.ChannelMessenger, "", "", nil)
	if custom.Color != "#8B5CF6" || third.Color != "#F59E0B" {
		t.Fatalf("colores: %s y %s", custom.Color, third.Color)
	}
}

func TestAccountsCreateErrors(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if _, err := e.accounts.Create(ctx, model.Channel("sms"), "", "", nil); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("canal desconocido: %v", err)
	}
	if _, err := e.accounts.Create(ctx, model.ChannelWhatsApp, "", "rojo", nil); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("color inválido: %v", err)
	}
	e.openErr = errors.New("disco lleno")
	if _, err := e.accounts.Create(ctx, model.ChannelWhatsApp, "", "", nil); err == nil {
		t.Fatal("si no abre la base debe fallar")
	}
	if accs, _ := e.app.ListAccounts(ctx); len(accs) != 0 {
		t.Fatalf("no debe quedar cuenta: %+v", accs)
	}
	e.openErr = nil
	e.app.saveAccErr = errors.New("sin espacio")
	if _, err := e.accounts.Create(ctx, model.ChannelWhatsApp, "", "", nil); err == nil {
		t.Fatal("si no guarda la cuenta debe fallar")
	}
	if len(e.accounts.snapshot()) != 0 {
		t.Fatal("no debe quedar instancia cargada")
	}
}

func TestAccountsListOrderAndViews(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	a := e.newAccount(model.ChannelWhatsApp, "A")
	time.Sleep(2 * time.Millisecond)
	b := e.newAccount(model.ChannelTelegram, "B")
	time.Sleep(2 * time.Millisecond)
	c := e.newAccount(model.ChannelInstagram, "C")

	ids := func() []model.AccountID {
		views, err := e.accounts.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var out []model.AccountID
		for _, v := range views {
			out = append(out, v.Account.ID)
		}
		return out
	}
	want := func(got []model.AccountID, exp ...model.AccountID) {
		t.Helper()
		if len(got) != len(exp) {
			t.Fatalf("orden %v, quería %v", got, exp)
		}
		for i := range exp {
			if got[i] != exp[i] {
				t.Fatalf("orden %v, quería %v", got, exp)
			}
		}
	}
	want(ids(), a.ID, b.ID, c.ID) // sin orden guardado: por CreatedAt

	if err := e.accounts.Reorder(ctx, []model.AccountID{c.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	want(ids(), c.ID, a.ID, b.ID) // las que no están en la lista, al final
	raw, _ := e.app.GetSetting(ctx, SettingAccountOrder)
	if raw != `["`+string(c.ID)+`","`+string(a.ID)+`"]` {
		t.Fatalf("account_order = %s", raw)
	}

	// no leídos y capacidades
	e.seedChat(a.ID, "c1", "x", 1)
	_ = e.store(a.ID).UpsertChat(ctx, model.Chat{NativeID: "c1", Kind: model.ChatDirect, UnreadCount: 3, LastMessageAt: 1})
	_ = e.store(a.ID).UpsertChat(ctx, model.Chat{NativeID: "c2", Kind: model.ChatDirect, UnreadCount: 4, LastMessageAt: 2})
	views, _ := e.accounts.List(ctx)
	for _, v := range views {
		if v.Account.ID == a.ID && v.Unread != 7 {
			t.Fatalf("no leídos = %d, quería 7", v.Unread)
		}
		if v.Status.State != model.StateConnected || !v.Capabilities.SendText {
			t.Fatalf("vista: %+v", v)
		}
	}
}

func TestAccountsUpdateEnabled(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "A")

	if err := e.accounts.Update(ctx, acc.ID, "Nuevo", "#ef4444", false); err != nil {
		t.Fatal(err)
	}
	saved, _ := e.app.GetAccount(ctx, acc.ID)
	if saved.Label != "Nuevo" || saved.Color != "#EF4444" || saved.Enabled {
		t.Fatalf("no se guardó: %+v", saved)
	}
	e.waitState(acc.ID, model.StateDisconnected)

	if err := e.accounts.Update(ctx, acc.ID, "", "", true); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateConnected)
	if saved, _ = e.app.GetAccount(ctx, acc.ID); saved.Label != "Nuevo" || !saved.Enabled {
		t.Fatalf("al reactivar se conserva el nombre: %+v", saved)
	}
	if err := e.accounts.Update(ctx, acc.ID, "", "azul", true); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("color inválido: %v", err)
	}
	if err := e.accounts.Update(ctx, "wa-nope", "x", "", true); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
}

func TestAccountsConnectDisconnect(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "A")
	if err := e.accounts.Disconnect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	if st := e.accounts.Status(acc.ID).State; st != model.StateDisconnected {
		t.Fatalf("estado tras Disconnect: %s", st)
	}
	if _, err := e.accounts.Adapter(acc.ID); err != nil {
		t.Fatal("el adaptador sigue cargado")
	}
	if err := e.accounts.Connect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateConnected)
	if n := e.adapter(acc.ID).starts(); n != 2 {
		t.Fatalf("Start llamado %d veces, quería 2", n)
	}
	// Connect repetido no vuelve a arrancar
	_ = e.accounts.Connect(ctx, acc.ID)
	time.Sleep(30 * time.Millisecond)
	if n := e.adapter(acc.ID).starts(); n != 2 {
		t.Fatalf("Connect con la cuenta corriendo no debe arrancar otra vez (%d)", n)
	}
	if err := e.accounts.Connect(ctx, "wa-nope"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
}

func TestAccountsRetryWithBackoff(t *testing.T) {
	e := newTestEnv(t)
	e.configure = func(a *testAdapter) { a.startErrs = 3 }
	ctx := context.Background()
	acc, err := e.accounts.Create(ctx, model.ChannelTelegram, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateConnected)
	if n := e.adapter(acc.ID).starts(); n != 4 {
		t.Fatalf("Start llamado %d veces, quería 4 (3 fallos y 1 éxito)", n)
	}
	// Los fallos se publicaron como estado «error» con texto en español.
	var errs int
	for _, ev := range e.eventsSince(0) {
		if ev.Type == model.EvAccountStatus && ev.AccountStatus.State == model.StateError {
			errs++
			if ev.AccountStatus.Detail == "" {
				t.Fatal("el error debe llevar Detail")
			}
		}
	}
	if errs != 3 {
		t.Fatalf("eventos de error = %d, quería 3", errs)
	}
}

func TestAccountsRetryOnAdapterError(t *testing.T) {
	e := newTestEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)
	ad.Inject(model.Event{Type: model.EvAccountStatus, AccountStatus: &model.AccountStatus{State: model.StateError, Detail: "se cayó"}})
	eventually(t, "reintento tras error del adaptador", func() bool { return ad.starts() >= 2 })
	e.waitState(acc.ID, model.StateConnected)
}

func TestAccountsLoggedOutDoesNotRetryUntilPairing(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ad := e.adapter(acc.ID)

	if err := e.accounts.Logout(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateLoggedOut)
	time.Sleep(100 * time.Millisecond) // más que varios reintentos
	if n := ad.starts(); n != 1 {
		t.Fatalf("tras logged_out no se reintenta (Start = %d)", n)
	}
	if st := e.accounts.Status(acc.ID).State; st != model.StateLoggedOut {
		t.Fatalf("estado: %s", st)
	}

	if err := e.accounts.BeginPairing(ctx, acc.ID, model.PairRequest{Method: "qr"}); err != nil {
		t.Fatal(err)
	}
	if n := ad.starts(); n != 2 {
		t.Fatalf("BeginPairing relanza la cuenta (Start = %d)", n)
	}
	found := false
	for _, c := range ad.Calls() {
		if c == "BeginPairing:qr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("el adaptador no recibió BeginPairing: %v", ad.Calls())
	}
	e.waitState(acc.ID, model.StatePairing)

	if err := e.accounts.SubmitPairing(ctx, acc.ID, model.PairInput{Code: "123"}); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateConnected)
}

func TestAccountsDelete(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "A")
	other := e.newAccount(model.ChannelTelegram, "B")
	_ = e.app.SetSetting(ctx, "warmup_started_at:"+string(acc.ID), "123")
	_ = e.accounts.Reorder(ctx, []model.AccountID{acc.ID, other.ID})
	st := e.store(acc.ID)

	if err := e.accounts.Delete(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.AccountDir(acc.ID)); !os.IsNotExist(err) {
		t.Fatalf("la carpeta debe borrarse: %v", err)
	}
	if _, err := e.app.GetAccount(ctx, acc.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("la cuenta debe borrarse de app.db: %v", err)
	}
	for _, k := range []string{"pacing:", "warmup_started_at:"} {
		if v, _ := e.app.GetSetting(ctx, k+string(acc.ID)); v != "" {
			t.Fatalf("%s no se borró: %q", k, v)
		}
	}
	if _, ok := e.router.SinkFor(acc.ID); ok {
		t.Fatal("el sink debe soltarse")
	}
	if !st.isClosed() {
		t.Fatal("la base de la cuenta debe cerrarse")
	}
	if _, err := e.accounts.Adapter(acc.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("Adapter tras borrar: %v", err)
	}
	if raw, _ := e.app.GetSetting(ctx, SettingAccountOrder); raw != `["`+string(other.ID)+`"]` {
		t.Fatalf("account_order = %s", raw)
	}
	calls := e.adapter(acc.ID).Calls()
	if len(calls) == 0 || calls[0] != "Logout" {
		t.Fatalf("Delete debe intentar Logout primero: %v", calls)
	}
	// la otra sigue viva
	if e.accounts.Status(other.ID).State != model.StateConnected {
		t.Fatal("la otra cuenta debe seguir conectada")
	}
	if err := e.accounts.Delete(ctx, "wa-nope"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("borrar una cuenta que no existe: %v", err)
	}
}

func TestAccountsStartAllStopAll(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	on := model.Account{ID: "wa-on", Channel: model.ChannelWhatsApp, Label: "On", Color: "#25D366", Enabled: true, CreatedAt: 1}
	off := model.Account{ID: "tg-off", Channel: model.ChannelTelegram, Label: "Off", Color: "#229ED9", Enabled: false, CreatedAt: 2}
	bad := model.Account{ID: "ig-bad", Channel: model.ChannelInstagram, Label: "Bad", Enabled: true, CreatedAt: 3}
	for _, a := range []model.Account{on, off, bad} {
		_ = e.app.SaveAccount(ctx, a)
	}
	e.configure = nil
	// ig-bad falla al abrir su base
	origOpen := e.accounts.open
	e.accounts.open = func(id model.AccountID) (port.AccountStore, *sql.DB, error) {
		if id == bad.ID {
			return nil, nil, errors.New("base corrupta")
		}
		return origOpen(id)
	}

	if err := e.accounts.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	e.waitState(on.ID, model.StateConnected)
	if st := e.accounts.Status(off.ID).State; st != model.StateDisconnected {
		t.Fatalf("la deshabilitada no conecta: %s", st)
	}
	if _, err := e.accounts.Store(off.ID); err != nil {
		t.Fatal("la deshabilitada queda cargada (para ver sus chats)")
	}
	if st := e.accounts.Status(bad.ID); st.State != model.StateError || st.Detail == "" {
		t.Fatalf("la que no abre queda en error: %+v", st)
	}

	// StartAll repetido no duplica
	if err := e.accounts.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.adapter(on.ID).starts(); n != 1 {
		t.Fatalf("Start = %d, quería 1", n)
	}

	stores := []*memAccountStore{e.store(on.ID), e.store(off.ID)}
	if err := e.accounts.StopAll(ctx); err != nil {
		t.Fatal(err)
	}
	for _, s := range stores {
		if !s.isClosed() {
			t.Fatal("StopAll cierra las bases de cuenta")
		}
	}
	if _, err := e.accounts.Adapter(on.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatal("StopAll descarga las instancias")
	}
}

func TestAccountsSetConfigRebuildsAdapter(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc, _ := e.accounts.Create(ctx, model.ChannelTelegram, "T", "", nil)
	e.waitState(acc.ID, model.StateConnected)
	first := e.adapter(acc.ID)

	if err := e.accounts.SetConfig(ctx, acc.ID, map[string]string{"api_id": "1", "api_hash": "h"}); err != nil {
		t.Fatal(err)
	}
	second := e.adapter(acc.ID)
	if second == first || second.deps.Account.Config["api_id"] != "1" {
		t.Fatalf("el adaptador debe reconstruirse con la config nueva: %+v", second.deps.Account.Config)
	}
	e.waitState(acc.ID, model.StateConnected)
	saved, _ := e.app.GetAccount(ctx, acc.ID)
	if saved.Config["api_hash"] != "h" {
		t.Fatalf("config guardada: %+v", saved.Config)
	}
	cur, _ := e.accounts.Adapter(acc.ID)
	if cur != second {
		t.Fatal("Adapter debe devolver el nuevo")
	}
}

func TestAccountsCapabilitiesAndLookups(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, "c1", "Ana", 1)
	_ = e.store(acc.ID).UpsertChat(ctx, model.Chat{NativeID: "ro", Kind: model.ChatChannel, ReadOnly: true})

	caps, err := e.accounts.Capabilities(ctx, acc.ID, "")
	if err != nil || !caps.SendText {
		t.Fatalf("capacidades de cuenta: %+v %v", caps, err)
	}
	caps, _ = e.accounts.Capabilities(ctx, acc.ID, "ro")
	if caps.SendText {
		t.Fatal("un chat de solo lectura no envía")
	}
	caps, _ = e.accounts.Capabilities(ctx, acc.ID, "desconocido")
	if !caps.SendText {
		t.Fatal("un chat desconocido usa las capacidades de la cuenta")
	}
	if _, err := e.accounts.Capabilities(ctx, "wa-nope", ""); !errors.Is(err, port.ErrNotFound) {
		t.Fatal(err)
	}
	if st, err := e.accounts.Store(acc.ID); err != nil || st.AccountID() != acc.ID {
		t.Fatalf("Store: %v", err)
	}
	if got := e.accounts.Status("wa-nope"); got.State != model.StateDisconnected {
		t.Fatalf("estado de una desconocida: %+v", got)
	}
}

func TestPalette(t *testing.T) {
	if got := nextPaletteColor(nil); got != "#25D366" {
		t.Fatal(got)
	}
	if got := nextPaletteColor([]string{"#25d366", "#229ED9"}); got != "#8B5CF6" {
		t.Fatalf("no distingue mayúsculas: %s", got)
	}
	all := append([]string(nil), accountPalette...)
	if got := nextPaletteColor(all); got != accountPalette[0] {
		t.Fatalf("con todos usados vuelve a empezar: %s", got)
	}
	if _, err := normalizeColor("#12345"); err == nil {
		t.Fatal("color corto")
	}
}
