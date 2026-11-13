package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"dconnector/internal/buildinfo"
	"dconnector/internal/core/model"
)

func TestHelloHeadersAndAccountFilter(t *testing.T) {
	ts := newTestServer(t, "secreto")
	e := newEnv(t)
	a1, _ := e.newAccount("Ventas")
	a2, _ := e.newAccount("Soporte")

	// Un destino ve las dos cuentas; el otro solo la segunda.
	e.addDest(ts, false, allFilter)
	e.start()
	hello := ts.waitHello()
	h := jsonOf(t, hello.Data)
	if len(h["accounts"].([]any)) != 2 {
		t.Fatalf("el hello debe traer las dos cuentas: %v", h["accounts"])
	}
	first := h["accounts"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "channel", "label", "color", "state", "selfId", "selfName", "selfHandle", "capabilities"} {
		if _, ok := first[k]; !ok {
			t.Errorf("falta %q en la cuenta del hello", k)
		}
	}
	if first["capabilities"].(map[string]any)["sendText"] != true {
		t.Errorf("capabilities mal serializadas: %v", first["capabilities"])
	}
	if h["allowCommands"] != false || h["version"] != buildinfo.Version {
		t.Errorf("hello: %v", h)
	}

	ts.mu.Lock()
	hdr := ts.headers[0]
	ts.mu.Unlock()
	if hdr.Get("Authorization") != "Bearer secreto" || hdr.Get("X-DConnector-Protocol") != "1" ||
		!strings.HasPrefix(hdr.Get("User-Agent"), "dConnector/") || hdr.Get("X-DConnector-Id") != h["instanceId"] {
		t.Errorf("cabeceras: %v", hdr)
	}

	// Con el filtro de cuentas, el hello solo lleva la permitida.
	ts2 := newTestServer(t, "otro")
	e.addDest(ts2, false, model.DestinationFilter{AccountIDs: []model.AccountID{a2.ID}})
	h2 := jsonOf(t, ts2.waitHello().Data)
	accs := h2["accounts"].([]any)
	if len(accs) != 1 || accs[0].(map[string]any)["id"] != string(a2.ID) {
		t.Fatalf("hello filtrado: %v (a1=%s a2=%s)", accs, a1.ID, a2.ID)
	}
	if h2["filter"].(map[string]any)["accountIds"].([]any)[0] != string(a2.ID) {
		t.Fatalf("el hello debe llevar el filtro: %v", h2["filter"])
	}
}

func TestUnauthorizedDoesNotRetryUntilEdited(t *testing.T) {
	ts := newTestServer(t, "bueno")
	ts.setReject(401)
	e := newEnv(t)
	dst := e.addDest(ts, false, allFilter)
	e.start()

	waitFor(t, "estado de error", func() bool { return e.stateOf(dst.ID).State == "error" })
	if st := e.stateOf(dst.ID); st.LastError != "Token rechazado" {
		t.Fatalf("error: %q", st.LastError)
	}
	time.Sleep(500 * time.Millisecond) // con espera de 20 ms habría decenas de intentos
	if n := ts.connCount(); n != 1 {
		t.Fatalf("un 401 no se reintenta: %d intentos", n)
	}

	// Al editar el destino (token nuevo) se vuelve a intentar y ya entra.
	ts.setReject(0)
	dst.Token = "bueno"
	if _, err := e.dsts.Save(context.Background(), dst); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "en línea tras editar", func() bool { return e.stateOf(dst.ID).State == "online" })
	if n := ts.connCount(); n != 2 {
		t.Fatalf("intentos: %d", n)
	}
}

func TestForbiddenIsAlsoTokenRejected(t *testing.T) {
	ts := newTestServer(t, "x")
	ts.setReject(403)
	e := newEnv(t)
	dst := e.addDest(ts, false, allFilter)
	e.start()
	waitFor(t, "error", func() bool { return e.stateOf(dst.ID).LastError == "Token rechazado" })
}

func TestReconnectResendsWithoutLoss(t *testing.T) {
	ts := newTestServer(t, "t")
	// La primera conexión no da ack; después de reconectar, sí.
	ts.setAck(func(conn int, f frameIn) bool { return conn >= 2 })
	e := newEnv(t)
	_, ad := e.newAccount("Ventas")
	dst := e.addDest(ts, false, model.DestinationFilter{Events: []string{"message.received"}})
	e.start()
	ts.waitHello()

	const chat = "c1"
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat(chat, "Ana")})
	for i := 0; i < 3; i++ {
		ad.Inject(model.Event{Type: model.EvMessageNew, Message: incoming(chat, fmt.Sprintf("m%d", i), "hola")})
	}
	waitFor(t, "3 mensajes en la primera conexión", func() bool {
		n := 0
		for _, r := range ts.eventsNamed("message.received") {
			if r.conn == 1 {
				n++
			}
		}
		return n == 3
	})
	if e.pending(dst.ID) != 3 {
		t.Fatalf("sin ack, el outbox conserva los 3 eventos: %d", e.pending(dst.ID))
	}

	ts.closeCurrent()
	waitFor(t, "reenvío y ack tras reconectar", func() bool { return e.pending(dst.ID) == 0 })
	if ts.connCount() != 2 {
		t.Fatalf("conexiones: %d", ts.connCount())
	}
	// Tras deduplicar por id: ni se pierde ni sobra nada; cada evento llegó otra vez en la 2.ª conexión.
	byID := map[string]map[int]int{}
	for _, r := range ts.eventsNamed("message.received") {
		if byID[r.f.ID] == nil {
			byID[r.f.ID] = map[int]int{}
		}
		byID[r.f.ID][r.conn]++
	}
	if len(byID) != 3 {
		t.Fatalf("eventos distintos: %d", len(byID))
	}
	for id, c := range byID {
		if c[1] != 1 || c[2] != 1 {
			t.Fatalf("el evento %s debía llegar una vez por conexión: %v", id, c)
		}
	}
	// El estado vuelve a «online» sin pendientes.
	waitFor(t, "online", func() bool { return e.stateOf(dst.ID).State == "online" })
}

func TestWindowOf200WithoutAck(t *testing.T) {
	ts := newTestServer(t, "t")
	ts.setAck(func(int, frameIn) bool { return false })
	e := newEnv(t)
	dst := e.addDest(ts, false, allFilter)
	ctx := context.Background()
	var all []string
	for i := 0; i < 250; i++ {
		id := fmt.Sprintf("ev-%03d", i)
		all = append(all, id)
		b, _ := json.Marshal(frameOut{V: 1, ID: id, Kind: kindEvent, Name: "account.state", TS: nowMs(), Data: map[string]any{"n": i}})
		if err := e.app.OutboxAdd(ctx, dst.ID, id, b); err != nil {
			t.Fatal(err)
		}
	}
	e.start()
	waitFor(t, "200 enviados", func() bool { return len(ts.eventsNamed("account.state")) >= 200 })
	time.Sleep(300 * time.Millisecond)
	if n := len(ts.eventsNamed("account.state")); n != 200 {
		t.Fatalf("la ventana es de 200 sin ack: se enviaron %d", n)
	}
	// El servidor confirma los 200 primeros y salen los 50 que faltaban.
	ts.send(map[string]any{"v": 1, "id": "ack-1", "kind": "ack", "data": map[string]any{"ids": all[:200]}})
	waitFor(t, "los 50 restantes", func() bool { return len(ts.eventsNamed("account.state")) == 250 })
	ts.send(map[string]any{"v": 1, "id": "ack-2", "kind": "ack", "data": map[string]any{"ids": all[200:]}})
	waitFor(t, "outbox vacío", func() bool { return e.pending(dst.ID) == 0 })
	// En orden de creación.
	for i, r := range ts.eventsNamed("account.state") {
		if r.f.ID != all[i] {
			t.Fatalf("orden roto en %d: %s", i, r.f.ID)
		}
	}
}

func TestWelcomeEventsReduceWhatIsSent(t *testing.T) {
	ts := newTestServer(t, "t")
	ts.setWelcome(map[string]any{"name": "Mi bot", "events": []string{"message.received"}})
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	dst := e.addDest(ts, false, allFilter)
	ctx := context.Background()
	// Un account.state ya guardado antes de conectar, y luego un mensaje.
	b, _ := json.Marshal(frameOut{V: 1, ID: "old-state", Kind: kindEvent, Name: "account.state", TS: nowMs(), Data: map[string]any{}})
	if err := e.app.OutboxAdd(ctx, dst.ID, "old-state", b); err != nil {
		t.Fatal(err)
	}
	e.start()
	ts.waitHello()
	waitFor(t, "outbox sin lo que el servidor no quiere", func() bool { return e.pending(dst.ID) == 0 })
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat("c1", "Ana")})
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: incoming("c1", "m1", "hola")})
	ts.waitEvent("message.received", nil)
	waitFor(t, "ack del mensaje", func() bool { return e.pending(dst.ID) == 0 })
	for _, r := range ts.snapshot() {
		if r.f.Kind == kindEvent && r.f.Name != "message.received" {
			t.Fatalf("el servidor pidió solo message.received y llegó %s (cuenta %s)", r.f.Name, acc.ID)
		}
	}
}

func TestPingTimeoutReconnects(t *testing.T) {
	// Servidor que acepta y no lee nunca: no puede contestar al ping.
	ts := newTestServer(t, "t")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	ts.custom = func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		<-block // sin leer: no procesa el ping del cliente
	}
	e := newEnv(t)
	e.mgr.pingEvery, e.mgr.pingTimeout = 50*time.Millisecond, 100*time.Millisecond
	dst := e.addDest(ts, false, allFilter)
	e.start()
	waitFor(t, "al menos dos conexiones por falta de pong", func() bool { return ts.connCount() >= 2 })
	_ = dst
}

func TestTestMethod(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	e.newAccount("Ventas")
	// El destino no está habilitado: Test funciona igual y sin Start.
	d, err := e.dsts.Save(context.Background(), model.Destination{Name: "x", URL: ts.wsURL(), Token: "t", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Test(context.Background(), d.ID); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if jsonOf(t, ts.waitHello().Data)["protocol"].(float64) != 1 {
		t.Fatal("el servidor debía recibir el hello")
	}

	// Token malo.
	bad, _ := e.dsts.Save(context.Background(), model.Destination{Name: "malo", URL: ts.wsURL(), Token: "otro"})
	if err := e.mgr.Test(context.Background(), bad.ID); err == nil || err.Error() != "Token rechazado" {
		t.Fatalf("Test con token malo: %v", err)
	}
	// Nadie escucha.
	ts.srv.Close()
	down, _ := e.dsts.Save(context.Background(), model.Destination{Name: "caído", URL: ts.wsURL(), Token: "t"})
	err = e.mgr.Test(context.Background(), down.ID)
	if err == nil || !strings.HasPrefix(err.Error(), "no se pudo conectar") {
		t.Fatalf("Test sin servidor: %v", err)
	}
	if strings.Contains(err.Error(), "ws://") {
		t.Fatalf("el error no debe mostrar la dirección: %v", err)
	}
	// Destino inexistente.
	if err := e.mgr.Test(context.Background(), "no-existe"); err == nil {
		t.Fatal("un destino inexistente debe fallar")
	}
}

func TestStopClosesEverythingQuickly(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	e.addDest(ts, false, allFilter)
	e.start()
	ts.waitHello()
	began := time.Now()
	e.mgr.Stop()
	if time.Since(began) > 5*time.Second {
		t.Fatal("Stop tardó más de 5 s")
	}
	// Tras Stop, un cambio de destinos ya no arranca nada.
	n := ts.connCount()
	time.Sleep(100 * time.Millisecond)
	if ts.connCount() != n {
		t.Fatal("no debe reconectar tras Stop")
	}
}

func TestOnStateReportsChanges(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	var states atomic.Value
	var count atomic.Int32
	e.mgr.OnState(func(st model.DestinationState) {
		states.Store(st)
		count.Add(1)
	})
	dst := e.addDest(ts, false, allFilter)
	e.start()
	waitFor(t, "aviso online", func() bool {
		st, _ := states.Load().(model.DestinationState)
		return st.DestinationID == dst.ID && st.State == "online" && st.ConnectedAt > 0
	})
	// Deshabilitar el destino avisa «off».
	dst.Enabled = false
	if _, err := e.dsts.Save(context.Background(), dst); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "aviso off", func() bool {
		st, _ := states.Load().(model.DestinationState)
		return st.State == "off"
	})
}

// ---------------------------------------------------------------------------------------------
// Retry-After (10 §1)

// busy503 hace que el servidor de prueba responda 503 con la cabecera `Retry-After` dada (vacía: sin ella)
// y anote la hora de cada intento.
func busy503(ts *testServer, retryAfter string) func() []time.Time {
	var mu sync.Mutex
	var at []time.Time
	ts.custom = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		at = append(at, time.Now())
		mu.Unlock()
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	return func() []time.Time {
		mu.Lock()
		defer mu.Unlock()
		return append([]time.Time(nil), at...)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	date := func(d time.Duration) string { return now.Add(d).UTC().Format(http.TimeFormat) }
	cases := []struct {
		name, in string
		want     time.Duration
	}{
		{"ausente", "", 0},
		{"segundos", "120", 120 * time.Second},
		{"con espacios", " 30 ", 30 * time.Second},
		{"cero", "0", 0},
		{"negativo", "-5", 0},
		{"con signo", "+5", 0},
		{"decimal", "1.5", 0},
		{"texto", "pronto", 0},
		{"una hora exacta", "3600", time.Hour},
		{"más de una hora se recorta", "7200", time.Hour},
		{"enorme se recorta", "99999999999999999999", time.Hour},
		{"fecha HTTP futura", date(90 * time.Second), 90 * time.Second},
		{"fecha HTTP lejana se recorta", date(48 * time.Hour), time.Hour},
		{"fecha HTTP pasada", date(-time.Minute), 0},
		{"fecha rota", "Tue, 99 Foo 2026", 0},
	}
	for _, c := range cases {
		h := http.Header{}
		if c.in != "" {
			h.Set("Retry-After", c.in)
		}
		if got := parseRetryAfter(h, now); got != c.want {
			t.Errorf("%s (%q): %v, quería %v", c.name, c.in, got, c.want)
		}
	}
}

func TestRetryWaitIsAtLeastWhatTheServerAsked(t *testing.T) {
	m := &Manager{backoffMin: time.Second, backoffMax: 60 * time.Second}
	for i := 0; i < 500; i++ {
		// 120 s pedidos: nunca menos de 120 s (aunque el intento sea alto) ni más de 144 s.
		if w := m.retryWait(i%12, 120*time.Second); w < 120*time.Second || w > 144*time.Second {
			t.Fatalf("120 s: %v", w)
		}
		// Más de 1 h: se recorta a 1 h exacta.
		if w := m.retryWait(3, 5*time.Hour); w != time.Hour {
			t.Fatalf("5 h: %v", w)
		}
		// Un Retry-After menor que la espera creciente no la acorta.
		if w := m.retryWait(20, 5*time.Second); w < 48*time.Second || w > 72*time.Second {
			t.Fatalf("5 s con intento alto: %v", w)
		}
		// Sin Retry-After: la espera de siempre (1 s ±20 % en el primer intento).
		if w := m.retryWait(0, 0); w < 800*time.Millisecond || w > 1200*time.Millisecond {
			t.Fatalf("sin Retry-After: %v", w)
		}
	}
}

func TestWaitText(t *testing.T) {
	for in, want := range map[time.Duration]string{
		0:                               "1 s",
		30 * time.Second:                "30 s",
		90 * time.Second:                "1 min 30 s",
		2 * time.Minute:                 "2 min",
		125 * time.Second:               "2 min", // más de 2 min: sin segundos
		10*time.Minute + 40*time.Second: "11 min",
		31 * time.Minute:                "31 min",
		time.Hour:                       "1 hora",
		59*time.Minute + 45*time.Second: "1 hora",
	} {
		if got := waitText(in); got != want {
			t.Errorf("%v: %q, quería %q", in, got, want)
		}
	}
}

func TestRetryAfterDelaysTheNextAttempt(t *testing.T) {
	ts := newTestServer(t, "t")
	attempts := busy503(ts, "1")
	e := newEnv(t) // espera creciente de 20–100 ms: sin Retry-After habría decenas de intentos
	dst := e.addDest(ts, false, allFilter)
	e.start()

	waitFor(t, "estado de error", func() bool { return e.stateOf(dst.ID).State == "error" })
	st := e.stateOf(dst.ID)
	if !strings.Contains(st.LastError, "El servidor pidió esperar") || !strings.Contains(st.LastError, "503") ||
		!strings.Contains(st.LastError, "se reintentará en") {
		t.Fatalf("mensaje: %q", st.LastError)
	}
	time.Sleep(600 * time.Millisecond)
	if n := ts.connCount(); n != 1 {
		t.Fatalf("el servidor pidió 1 s y hubo %d intentos a los 600 ms", n)
	}
	waitFor(t, "segundo intento", func() bool { return ts.connCount() >= 2 })
	at := attempts()
	if gap := at[1].Sub(at[0]); gap < time.Second {
		t.Fatalf("el segundo intento llegó a los %v, antes del 1 s pedido", gap)
	}
}

func TestHTTP503WithoutRetryAfterBehavesAsBefore(t *testing.T) {
	for _, header := range []string{"", "pronto", "-5", "0"} {
		ts := newTestServer(t, "t")
		busy503(ts, header)
		e := newEnv(t)
		dst := e.addDest(ts, false, allFilter)
		e.start()
		waitFor(t, "varios intentos con "+header, func() bool { return ts.connCount() >= 3 })
		if got := e.stateOf(dst.ID).LastError; got != "el servidor respondió con el código HTTP 503" {
			t.Fatalf("Retry-After %q: mensaje %q", header, got)
		}
	}
}

func TestEditingTheDestinationWakesUpALongWait(t *testing.T) {
	ts := newTestServer(t, "t")
	busy503(ts, "3600")
	e := newEnv(t)
	dst := e.addDest(ts, false, allFilter)
	e.start()
	waitFor(t, "estado de error", func() bool { return strings.Contains(e.stateOf(dst.ID).LastError, "1 hora") })

	// El servidor se enciende y el usuario edita el destino: entra al instante, sin esperar la hora.
	ts.mu.Lock()
	ts.custom = nil
	ts.mu.Unlock()
	dst.Name = "editado"
	began := time.Now()
	if _, err := e.dsts.Save(context.Background(), dst); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "en línea tras editar", func() bool { return e.stateOf(dst.ID).State == "online" })
	if d := time.Since(began); d > 3*time.Second {
		t.Fatalf("tardó %v en reconectar tras editar", d)
	}
}

func TestStopDuringLongWaitReturnsQuickly(t *testing.T) {
	ts := newTestServer(t, "t")
	busy503(ts, "3600")
	e := newEnv(t)
	dst := e.addDest(ts, false, allFilter)
	e.start()
	waitFor(t, "espera larga", func() bool { return strings.Contains(e.stateOf(dst.ID).LastError, "1 hora") })
	began := time.Now()
	e.mgr.Stop()
	if d := time.Since(began); d > 3*time.Second {
		t.Fatalf("Stop tardó %v", d)
	}
}

func TestSavingUnchangedDestinationAlsoWakesUpALongWait(t *testing.T) {
	ts := newTestServer(t, "t")
	busy503(ts, "3600")
	e := newEnv(t)
	dst := e.addDest(ts, false, allFilter)
	e.start()
	waitFor(t, "espera larga", func() bool { return strings.Contains(e.stateOf(dst.ID).LastError, "1 hora") })

	ts.mu.Lock()
	ts.custom = nil
	ts.mu.Unlock()
	if _, err := e.dsts.Save(context.Background(), dst); err != nil { // guardar sin cambiar nada
		t.Fatal(err)
	}
	waitFor(t, "en línea tras guardar", func() bool { return e.stateOf(dst.ID).State == "online" })
}
