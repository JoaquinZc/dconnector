package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Programados recurrentes, varios por chat y límites de envío.

// txApp: el almacén de campañas con los métodos opcionales de programados (transacción lógica,
// lecturas por chat y por serie). failWithNext hace fallar el siguiente SaveScheduledWithNext.
type txApp struct {
	*campApp
	fmu          sync.Mutex
	failWithNext error
}

func (a *txApp) GetScheduled(ctx context.Context, id string) (model.Scheduled, error) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	it, ok := a.sched[id]
	if !ok {
		return model.Scheduled{}, port.ErrNotFound
	}
	return it, nil
}

func (a *txApp) ListScheduledByChat(ctx context.Context, acc model.AccountID, chat string, states []string, since int64) ([]model.Scheduled, error) {
	all, _ := a.ListScheduled(ctx, since, 0, states)
	out := []model.Scheduled{}
	for _, it := range all {
		if it.Kind == "message" && it.AccountID == acc && it.ChatNativeID == chat {
			out = append(out, it)
		}
	}
	return out, nil
}

func (a *txApp) ListScheduledBySeries(ctx context.Context, seriesID string, states []string) ([]model.Scheduled, error) {
	all, _ := a.ListScheduled(ctx, 0, 0, states)
	out := []model.Scheduled{}
	for _, it := range all {
		if it.SeriesID == seriesID {
			out = append(out, it)
		}
	}
	return out, nil
}

func (a *txApp) SaveScheduledWithNext(ctx context.Context, cur model.Scheduled, next *model.Scheduled) error {
	a.fmu.Lock()
	err := a.failWithNext
	a.failWithNext = nil
	a.fmu.Unlock()
	if err != nil {
		return err
	}
	a.cmu.Lock()
	defer a.cmu.Unlock()
	a.sched[cur.ID] = cur
	if next != nil {
		for _, o := range a.sched {
			if o.SeriesID == next.SeriesID && o.State == "pending" && o.ID != cur.ID {
				return nil
			}
		}
		a.sched[next.ID] = *next
	}
	return nil
}

// newScheduler arma un Scheduler de prueba sobre app: reloj falso, UTC y esperas que solo adelantan el reloj.
func (e *campEnv) newScheduler(app port.AppStore, st *Statuses) *Scheduler {
	s := NewScheduler(app, e.msg, st, e.accounts)
	s.now = e.clock.Now
	s.tick = 5 * time.Millisecond
	s.loc = time.UTC
	s.bootAt = campStart.UnixMilli()
	s.sleep = func(ctx context.Context, d time.Duration) error {
		e.smu.Lock()
		e.slept = append(e.slept, d)
		e.smu.Unlock()
		e.clock.Advance(d)
		return nil
	}
	return s
}

// newSchedEnvTx: como newSchedEnv, pero el Scheduler usa txApp y zona UTC.
func newSchedEnvTx(t *testing.T) (*schedEnv, *txApp) {
	t.Helper()
	ce := newCampEnv(t)
	tx := &txApp{campApp: ce.app}
	st := NewStatuses(ce.accounts, ce.router, tx)
	t.Cleanup(st.Stop)
	e := &schedEnv{campEnv: ce, s: ce.newScheduler(tx, st)}
	t.Cleanup(func() { e.s.Stop() })
	return e, tx
}

func (e *schedEnv) series(acc model.AccountID, chat, text string, first time.Time, r model.Recurrence) model.Scheduled {
	e.t.Helper()
	it, err := e.s.Save(context.Background(), model.Scheduled{
		AccountID: acc, Kind: "message", ChatNativeID: chat, ChatTitle: "Ana",
		Message: &model.Outgoing{Kind: model.KindText, Text: text}, SendAt: first.UnixMilli(), Recurrence: &r,
	})
	if err != nil {
		e.t.Fatalf("Save serie: %v", err)
	}
	return it
}

func (e *schedEnv) mustList() []model.Scheduled {
	e.t.Helper()
	all, err := e.s.List(context.Background(), 0, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return all
}

// rows devuelve todas las filas de una serie por hora de envío.
func (e *schedEnv) rows(seriesID string) []model.Scheduled {
	e.t.Helper()
	var out []model.Scheduled
	for _, it := range e.mustList() {
		if it.SeriesID == seriesID {
			out = append(out, it)
		}
	}
	return out
}

func (e *schedEnv) pending(seriesID string) []model.Scheduled {
	var out []model.Scheduled
	for _, it := range e.rows(seriesID) {
		if it.State == "pending" {
			out = append(out, it)
		}
	}
	return out
}

func (e *schedEnv) setClock(t time.Time) {
	e.clock.mu.Lock()
	e.clock.t = t
	e.clock.mu.Unlock()
}

func TestSchedulerSeriesEachOccurrenceIsItsOwnRow(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "buenos días", first, model.Recurrence{Frequency: "daily", Every: 1})
	if a.SeriesID == "" || a.Recurrence == nil || a.Recurrence.FirstAt != first.UnixMilli() || a.Recurrence.Count != 0 {
		t.Fatalf("serie nueva: %+v %+v", a, a.Recurrence)
	}

	for day := 0; day < 3; day++ {
		e.setClock(first.Add(time.Duration(day) * 24 * time.Hour))
		e.s.processDue(ctx)
	}
	rows := e.rows(a.SeriesID)
	if len(rows) != 4 {
		t.Fatalf("3 enviadas + 1 pendiente, hay %d: %+v", len(rows), rows)
	}
	for i, r := range rows[:3] {
		if r.State != "sent" || r.SendAt != first.Add(time.Duration(i)*24*time.Hour).UnixMilli() || r.Recurrence.Count != i {
			t.Errorf("fila %d: %+v count=%d", i, r, r.Recurrence.Count)
		}
	}
	last := rows[3]
	if last.State != "pending" || last.SendAt != first.Add(72*time.Hour).UnixMilli() || last.Recurrence.Count != 3 ||
		last.Message.Text != "buenos días" || last.SentAt != 0 || last.Error != "" {
		t.Fatalf("pendiente: %+v", last)
	}
	if got := e.sentTexts(acc.ID); len(got) != 3 {
		t.Fatalf("enviados: %q", got)
	}
	// Otro tick sin que pase el tiempo no repite ni duplica la serie.
	e.s.processDue(ctx)
	if n := len(e.rows(a.SeriesID)); n != 4 {
		t.Fatalf("filas tras otro tick: %d", n)
	}
}

func TestSchedulerSeriesWeeklyMultipleDays(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour) // jueves 11:00
	a := e.series(acc.ID, peerOf("Ana"), "reunión", first, model.Recurrence{Frequency: "weekly", Every: 1, Weekdays: []int{1, 3, 5}})
	want := []time.Time{
		first,
		time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC), // viernes
		time.Date(2026, 11, 16, 11, 0, 0, 0, time.UTC), // lunes
		time.Date(2026, 11, 18, 11, 0, 0, 0, time.UTC), // miércoles
	}
	for _, w := range want {
		e.setClock(w)
		e.s.processDue(ctx)
	}
	rows := e.rows(a.SeriesID)
	if len(rows) != 5 {
		t.Fatalf("filas: %d", len(rows))
	}
	for i, w := range want {
		if rows[i].SendAt != w.UnixMilli() || rows[i].State != "sent" {
			t.Errorf("fila %d: %s %s", i, time.UnixMilli(rows[i].SendAt).UTC(), rows[i].State)
		}
	}
	if p := rows[4]; p.State != "pending" || p.SendAt != time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("pendiente: %+v", p)
	}
}

func TestSchedulerSeriesMonthlyDay31(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := time.Date(2027, 1, 31, 8, 0, 0, 0, time.UTC)
	e.setClock(first.Add(-time.Hour))
	a := e.series(acc.ID, peerOf("Ana"), "cuota", first, model.Recurrence{Frequency: "monthly", Every: 1})
	var got []time.Time
	for i := 0; i < 4; i++ {
		p := e.pending(a.SeriesID)
		if len(p) != 1 {
			t.Fatalf("debía haber 1 pendiente, hay %d", len(p))
		}
		got = append(got, time.UnixMilli(p[0].SendAt).UTC())
		e.setClock(time.UnixMilli(p[0].SendAt))
		e.s.processDue(ctx)
	}
	want := []time.Time{first, time.Date(2027, 2, 28, 8, 0, 0, 0, time.UTC), time.Date(2027, 3, 31, 8, 0, 0, 0, time.UTC), time.Date(2027, 4, 30, 8, 0, 0, 0, time.UTC)}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("mes %d: %s, se esperaba %s", i, got[i], want[i])
		}
	}
	if p := e.pending(a.SeriesID); len(p) != 1 || !time.UnixMilli(p[0].SendAt).UTC().Equal(time.Date(2027, 5, 31, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("la siguiente debía volver al 31: %+v", p)
	}
}

func TestSchedulerSeriesEndAtAndMaxCount(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)

	// MaxCount 3: salen 3 y no queda pendiente.
	a := e.series(acc.ID, peerOf("Ana"), "tres veces", first, model.Recurrence{Frequency: "daily", Every: 1, MaxCount: 3})
	for d := 0; d < 5; d++ {
		e.setClock(first.Add(time.Duration(d) * 24 * time.Hour))
		e.s.processDue(ctx)
	}
	if rows := e.rows(a.SeriesID); len(rows) != 3 || len(e.pending(a.SeriesID)) != 0 {
		t.Fatalf("MaxCount: %d filas, %d pendientes", len(rows), len(e.pending(a.SeriesID)))
	}

	// EndAt a los 2 días y medio: salen la del día 0, 1 y 2.
	b := e.series(acc.ID, peerOf("Luis"), "hasta el viernes", first.Add(10*24*time.Hour), model.Recurrence{Frequency: "daily", Every: 1, EndAt: first.Add(10*24*time.Hour + 60*time.Hour).UnixMilli()})
	for d := 10; d < 16; d++ {
		e.setClock(first.Add(time.Duration(d) * 24 * time.Hour))
		e.s.processDue(ctx)
	}
	if rows := e.rows(b.SeriesID); len(rows) != 3 || len(e.pending(b.SeriesID)) != 0 {
		t.Fatalf("EndAt: %d filas, %d pendientes", len(rows), len(e.pending(b.SeriesID)))
	}
}

func TestSchedulerSeriesMissedOccurrences(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour) // 11:00
	a := e.series(acc.ID, peerOf("Ana"), "a tiempo", first, model.Recurrence{Frequency: "daily", Every: 1})

	// Con 59 minutos de atraso (la app arrancó después de la hora) se envía.
	e.setClock(first.Add(59 * time.Minute))
	e.s.bootAt = first.Add(30 * time.Minute).UnixMilli()
	e.s.processDue(ctx)
	rows := e.rows(a.SeriesID)
	if rows[0].State != "sent" || len(rows) != 2 {
		t.Fatalf("≤ 1 h de atraso se envía: %+v", rows)
	}

	// La app queda cerrada y vuelve a abrirse 5 h tarde: esa ocurrencia se omite y la siguiente es futura.
	e.setClock(time.UnixMilli(rows[1].SendAt).Add(5 * time.Hour))
	e.s.bootAt = e.clock.Now().UnixMilli()
	e.s.processDue(ctx)
	rows = e.rows(a.SeriesID)
	if len(rows) != 3 || rows[1].State != "canceled" || rows[1].Error != "Omitido: la app estaba cerrada" {
		t.Fatalf("omitida: %+v", rows)
	}
	if rows[2].State != "pending" || rows[2].SendAt <= e.clock.Now().UnixMilli() ||
		rows[2].SendAt != first.Add(48*time.Hour).UnixMilli() || rows[2].Recurrence.Count != 2 {
		t.Fatalf("la siguiente debía ser la de pasado mañana a la misma hora: %+v", rows[2])
	}
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("lo omitido no se envía: %d", n)
	}

	// Una serie con varios días de atraso salta a la próxima futura sin acumular envíos.
	e.setClock(first.Add(10 * 24 * time.Hour).Add(3 * time.Hour))
	e.s.bootAt = e.clock.Now().UnixMilli()
	e.s.processDue(ctx)
	p := e.pending(a.SeriesID)
	if len(p) != 1 || p[0].SendAt != first.Add(11*24*time.Hour).UnixMilli() {
		t.Fatalf("tras 10 días cerrada: %+v", p)
	}
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("sin envíos acumulados: %d", n)
	}

	// Un programado de una sola vez conserva su comportamiento: con el mismo atraso sí se envía.
	one := e.message(acc.ID, peerOf("Ana"), "único", e.clock.Now().Add(-5*time.Hour))
	e.s.processDue(ctx)
	if got := e.find(one.ID); got.State != "sent" {
		t.Fatalf("un solo envío atrasado debía enviarse: %+v", got)
	}
}

func TestSchedulerSeriesOfflineAccountSkipsAfterAnHour(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})
	if err := e.accounts.Disconnect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateDisconnected)

	// Los fallos transitorios reintentan sin crear duplicados ni siguientes.
	for _, d := range []time.Duration{0, 10 * time.Minute, 59 * time.Minute} {
		e.setClock(first.Add(d))
		e.s.processDue(ctx)
		rows := e.rows(a.SeriesID)
		if len(rows) != 1 || rows[0].State != "pending" || rows[0].Error != "" {
			t.Fatalf("a +%v: %+v", d, rows)
		}
	}
	e.setClock(first.Add(61 * time.Minute))
	e.s.processDue(ctx)
	rows := e.rows(a.SeriesID)
	if len(rows) != 2 || rows[0].State != "canceled" || rows[0].Error != "Omitido: la cuenta no estaba conectada" || rows[1].State != "pending" {
		t.Fatalf("pasada la hora: %+v", rows)
	}
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("no se envió nada: %d", n)
	}

	// Se reconecta: la siguiente sale a su hora, una sola vez.
	if err := e.accounts.Connect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateConnected)
	e.setClock(time.UnixMilli(rows[1].SendAt))
	e.s.processDue(ctx)
	e.s.processDue(ctx)
	if got := e.sentTexts(acc.ID); len(got) != 1 {
		t.Fatalf("enviados: %q", got)
	}
	if n := len(e.rows(a.SeriesID)); n != 3 {
		t.Fatalf("filas: %d", n)
	}
}

func TestSchedulerSeriesAdapterNotConnectedRetriesWithoutDuplicates(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})
	e.setClock(first)
	e.adapter(acc.ID).SendErr = port.ErrNotConnected
	for i := 0; i < 3; i++ {
		e.s.processDue(ctx)
	}
	if rows := e.rows(a.SeriesID); len(rows) != 1 || rows[0].State != "pending" {
		t.Fatalf("transitorio: %+v", rows)
	}
	e.adapter(acc.ID).SendErr = nil
	e.s.processDue(ctx)
	e.s.processDue(ctx)
	if rows := e.rows(a.SeriesID); len(rows) != 2 || rows[0].State != "sent" || rows[1].State != "pending" {
		t.Fatalf("tras reconectar: %+v", rows)
	}
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("un solo envío: %d", n)
	}
}

func TestSchedulerSeriesFailedSendStillContinues(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})
	e.adapter(acc.ID).SendErr = errors.New("chat bloqueado")
	e.setClock(first)
	e.s.processDue(ctx)
	rows := e.rows(a.SeriesID)
	if len(rows) != 2 || rows[0].State != "failed" || !strings.Contains(rows[0].Error, "chat bloqueado") ||
		rows[1].State != "pending" || rows[1].Recurrence.Count != 1 {
		t.Fatalf("falla: %+v", rows)
	}
	// Corregir la fallida (la serie ya siguió): se reintenta suelta y no duplica la serie.
	e.adapter(acc.ID).SendErr = nil
	fixed, err := e.s.Save(ctx, rows[0])
	if err != nil || fixed.Recurrence != nil || fixed.State != "pending" {
		t.Fatalf("corregir: %+v %v", fixed, err)
	}
	e.s.processDue(ctx)
	if got := e.find(rows[0].ID); got.State != "sent" {
		t.Fatalf("reintento: %+v", got)
	}
	if p := e.pending(a.SeriesID); len(p) != 1 || p[0].ID != rows[1].ID {
		t.Fatalf("la serie debía seguir con una sola pendiente: %+v", p)
	}
}

func TestSchedulerSeriesCancelOneAndCancelSeries(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})

	// Cancelar una ocurrencia cancela solo esa: la serie sigue con la siguiente.
	if err := e.s.Cancel(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	rows := e.rows(a.SeriesID)
	if len(rows) != 2 || rows[0].State != "canceled" || rows[1].State != "pending" ||
		rows[1].SendAt != first.Add(24*time.Hour).UnixMilli() || rows[1].Recurrence.Count != 1 {
		t.Fatalf("tras cancelar una: %+v", rows)
	}
	// Borrar la pendiente también equivale a saltarla.
	if err := e.s.Delete(ctx, rows[1].ID); err != nil {
		t.Fatal(err)
	}
	rows = e.rows(a.SeriesID)
	if len(rows) != 2 || rows[1].State != "pending" || rows[1].SendAt != first.Add(48*time.Hour).UnixMilli() {
		t.Fatalf("tras borrar la pendiente: %+v", rows)
	}
	// CancelSeries cancela las pendientes y la serie ya no genera más.
	if err := e.s.CancelSeries(ctx, a.SeriesID); err != nil {
		t.Fatal(err)
	}
	if p := e.pending(a.SeriesID); len(p) != 0 {
		t.Fatalf("pendientes tras CancelSeries: %+v", p)
	}
	e.setClock(first.Add(10 * 24 * time.Hour))
	e.s.processDue(ctx)
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("una serie cancelada no envía: %d", n)
	}
	if err := e.s.CancelSeries(ctx, "no-existe"); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("serie inexistente: %v", err)
	}

	// DeleteSeries borra todo, historial incluido.
	if err := e.s.DeleteSeries(ctx, a.SeriesID); err != nil {
		t.Fatal(err)
	}
	if n := len(e.rows(a.SeriesID)); n != 0 {
		t.Fatalf("filas tras DeleteSeries: %d", n)
	}
	if err := e.s.DeleteSeries(ctx, a.SeriesID); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("borrar dos veces: %v", err)
	}
}

func TestSchedulerCancelSeriesWhileSendingDoesNotContinue(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})
	e.setClock(first)
	cur := e.find(a.ID) // copia «vieja» (pendiente) que tenía el tick
	if err := e.s.CancelSeries(ctx, a.SeriesID); err != nil {
		t.Fatal(err)
	}
	// run relee y ve «canceled»: no envía ni continúa.
	if err := e.s.run(ctx, cur, false); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter(acc.ID).Sent()) != 0 || len(e.rows(a.SeriesID)) != 1 {
		t.Fatalf("no debía enviar ni continuar: %d %d", len(e.adapter(acc.ID).Sent()), len(e.rows(a.SeriesID)))
	}
	// Si se cancela entre el envío y el guardado, settle registra el envío pero no crea la siguiente.
	sent := e.find(a.ID)
	sent.State, sent.SentAt = "sent", first.UnixMilli()
	if err := e.s.settle(ctx, sent); err != nil {
		t.Fatal(err)
	}
	if rows := e.rows(a.SeriesID); len(rows) != 1 || rows[0].State != "sent" {
		t.Fatalf("settle tras cancelar: %+v", rows)
	}
}

func TestSchedulerSeriesNeverDuplicatesPending(t *testing.T) {
	for _, withTx := range []bool{true, false} {
		name := "sin transacción"
		if withTx {
			name = "con transacción"
		}
		t.Run(name, func(t *testing.T) {
			var e *schedEnv
			if withTx {
				e, _ = newSchedEnvTx(t)
			} else {
				e = newSchedEnv(t)
				e.s.loc, e.s.bootAt = time.UTC, campStart.UnixMilli()
			}
			ctx := context.Background()
			acc := e.newAccount(model.ChannelWhatsApp, "")
			first := campStart.Add(time.Hour)
			a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})
			// Resto de una caída: la siguiente ya existe antes de cerrar la actual.
			orphan := a
			orphan.ID, orphan.SendAt = model.NewID(), first.Add(24*time.Hour).UnixMilli()
			if err := e.app.SaveScheduled(ctx, orphan); err != nil {
				t.Fatal(err)
			}
			e.setClock(first)
			e.s.processDue(ctx)
			rows := e.rows(a.SeriesID)
			if len(rows) != 2 || len(e.pending(a.SeriesID)) != 1 || e.find(a.ID).State != "sent" {
				t.Fatalf("no debía duplicar: %+v", rows)
			}
		})
	}
}

func TestSchedulerSettleFailureDoesNotResend(t *testing.T) {
	e, tx := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})
	e.setClock(first)
	tx.failWithNext = errors.New("disco lleno")
	e.s.processDue(ctx)
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("enviados: %d", n)
	}
	if got := e.find(a.ID); got.State != "pending" {
		t.Fatalf("no se pudo guardar: %+v", got)
	}
	// Siguiente tick: solo reintenta guardar; no reenvía y ahora sí crea la siguiente.
	e.s.processDue(ctx)
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("no debía reenviarse: %d", n)
	}
	if rows := e.rows(a.SeriesID); len(rows) != 2 || rows[0].State != "sent" || rows[1].State != "pending" {
		t.Fatalf("tras reintentar: %+v", rows)
	}
}

func TestSchedulerSeriesResumesAfterRestart(t *testing.T) {
	e, tx := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})
	e.setClock(first)
	e.s.processDue(ctx)

	// «Reinicio»: otro Scheduler sobre el mismo almacén, 20 min tarde.
	st2 := NewStatuses(e.accounts, e.router, tx)
	t.Cleanup(st2.Stop)
	s2 := e.newScheduler(tx, st2)
	s2.bootAt = first.Add(24*time.Hour + 10*time.Minute).UnixMilli()
	e.setClock(first.Add(24*time.Hour + 20*time.Minute))
	s2.processDue(ctx)
	e.s = s2
	rows := e.rows(a.SeriesID)
	if len(rows) != 3 || rows[1].State != "sent" || rows[2].State != "pending" || rows[2].Recurrence.Count != 2 {
		t.Fatalf("tras reiniciar: %+v", rows)
	}
	if rows[2].SendAt != first.Add(48*time.Hour).UnixMilli() {
		t.Fatalf("conserva la hora de pared: %s", time.UnixMilli(rows[2].SendAt).UTC())
	}
	if got := e.sentTexts(acc.ID); len(got) != 2 {
		t.Fatalf("enviados: %q", got)
	}
}

func TestSchedulerSeveralToSameChatKeepOrderAndPause(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	at := campStart.Add(-time.Minute)
	for _, txt := range []string{"uno", "dos", "tres"} { // misma hora, mismo chat
		e.message(acc.ID, peerOf("Ana"), txt, at)
	}
	e.message(acc.ID, peerOf("Luis"), "otro chat", at.Add(time.Second))
	e.s.processDue(ctx)
	got := e.sentTexts(acc.ID)
	if len(got) != 4 || got[0] != "uno" || got[1] != "dos" || got[2] != "tres" || got[3] != "otro chat" {
		t.Fatalf("orden de envío: %q", got)
	}
	sl := e.sleeps()
	if len(sl) != 2 || sl[0] != 1500*time.Millisecond || sl[1] != 1500*time.Millisecond {
		t.Fatalf("pausas entre envíos al mismo chat (el otro chat no espera): %v", sl)
	}
}

func TestSchedulerRateLimitPerAccountPerMinute(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	other := e.newAccount(model.ChannelWhatsApp, "")
	at := campStart.Add(-time.Minute)
	for i := 0; i < 25; i++ {
		e.message(acc.ID, peerOf(fmt.Sprintf("c%02d", i)), fmt.Sprintf("m%02d", i), at)
	}
	e.message(other.ID, peerOf("ana"), "de la otra cuenta", at)
	e.s.processDue(ctx)
	if n := len(e.sentTexts(acc.ID)); n != 20 {
		t.Fatalf("primer minuto: %d envíos, se esperaban 20", n)
	}
	if n := len(e.sentTexts(other.ID)); n != 1 {
		t.Fatalf("el tope es por cuenta: %d", n)
	}
	pend := 0
	for _, it := range e.mustList() {
		if it.State == "pending" {
			pend++
		}
	}
	if pend != 5 {
		t.Fatalf("pendientes en cola: %d", pend)
	}
	e.s.processDue(ctx)
	if n := len(e.sentTexts(acc.ID)); n != 20 {
		t.Fatalf("sin pasar el minuto no sale más: %d", n)
	}
	e.clock.Advance(61 * time.Second)
	e.s.processDue(ctx)
	if got := e.sentTexts(acc.ID); len(got) != 25 || got[20] != "m20" || got[24] != "m24" {
		t.Fatalf("tras el minuto salen los 5 restantes en orden: %q", got)
	}
}

func TestSchedulerListForChat(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := peerOf("Ana")
	p1 := e.message(acc.ID, ana, "dos", campStart.Add(2*time.Hour))
	p2 := e.message(acc.ID, ana, "uno", campStart.Add(time.Hour))
	e.message(acc.ID, peerOf("Luis"), "de otro chat", campStart.Add(time.Hour))
	e.series(acc.ID, ana, "serie", campStart.Add(3*time.Hour), model.Recurrence{Frequency: "daily", Every: 1})
	if _, err := e.s.Save(ctx, model.Scheduled{AccountID: acc.ID, Kind: "status", SendAt: campStart.Add(time.Hour).UnixMilli(), Status: &model.OutgoingStatus{Kind: model.KindText, Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	// Uno enviado hace 10 días, uno enviado hace poco y uno cancelado.
	closed := func(text string, at time.Time, state string) {
		it := e.message(acc.ID, ana, text, at)
		it.State, it.SentAt = state, at.UnixMilli()
		if err := e.app.SaveScheduled(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	closed("viejo", campStart.Add(-10*24*time.Hour), "sent")
	closed("reciente", campStart.Add(-time.Hour), "sent")
	closed("cancelado", campStart.Add(-2*time.Hour), "canceled")

	texts := func(l []model.Scheduled) []string {
		var out []string
		for _, it := range l {
			out = append(out, it.Message.Text)
		}
		return out
	}
	got, err := e.s.ListForChat(ctx, acc.ID, ana, false)
	if err != nil {
		t.Fatal(err)
	}
	if w := texts(got); len(w) != 3 || w[0] != "uno" || w[1] != "dos" || w[2] != "serie" {
		t.Fatalf("solo pendientes: %q", w)
	}
	if got[0].ID != p2.ID || got[1].ID != p1.ID {
		t.Fatalf("ids: %v", got)
	}
	got, err = e.s.ListForChat(ctx, acc.ID, ana, true)
	if err != nil {
		t.Fatal(err)
	}
	if w := texts(got); len(w) != 5 || w[0] != "cancelado" || w[1] != "reciente" || w[2] != "uno" {
		t.Fatalf("con recientes: %q", w)
	}
	if got, _ := e.s.ListForChat(ctx, acc.ID, peerOf("Nadie"), true); len(got) != 0 {
		t.Fatalf("chat sin programados: %v", got)
	}
}

func TestSchedulerListForChatWithoutOptionalStore(t *testing.T) {
	e := newSchedEnv(t) // campApp no ofrece los métodos opcionales: se filtra en memoria
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.message(acc.ID, peerOf("Ana"), "uno", campStart.Add(time.Hour))
	e.message(acc.ID, peerOf("Luis"), "otro", campStart.Add(time.Hour))
	got, err := e.s.ListForChat(ctx, acc.ID, peerOf("Ana"), false)
	if err != nil || len(got) != 1 || got[0].Message.Text != "uno" {
		t.Fatalf("ListForChat: %+v %v", got, err)
	}
}

func TestSchedulerSaveRecurrenceValidation(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	mk := func(r model.Recurrence, at time.Time) error {
		_, err := e.s.Save(ctx, model.Scheduled{
			AccountID: acc.ID, Kind: "message", ChatNativeID: "c", SendAt: at.UnixMilli(),
			Message: &model.Outgoing{Kind: model.KindText, Text: "x"}, Recurrence: &r,
		})
		return err
	}
	bad := []model.Recurrence{
		{Frequency: "nada", Every: 1},
		{Frequency: "daily", Every: 400},
		{Frequency: "custom", Every: 2, Unit: "minutes"},
		{Frequency: "daily", Every: 1, MaxCount: 5000},
		{Frequency: "daily", Every: 1, EndAt: first.UnixMilli()}, // EndAt debe ser posterior al primer envío
	}
	for i, r := range bad {
		if err := mk(r, first); !errors.Is(err, port.ErrInvalid) {
			t.Errorf("caso %d: %v", i, err)
		}
	}
	if err := mk(model.Recurrence{Frequency: "daily", Every: 1}, campStart.Add(-3*time.Hour)); !errors.Is(err, port.ErrInvalid) {
		t.Errorf("primer envío en el pasado: %v", err)
	}
	// Los estados también pueden repetirse.
	st, err := e.s.Save(ctx, model.Scheduled{
		AccountID: acc.ID, Kind: "status", SendAt: first.UnixMilli(),
		Status: &model.OutgoingStatus{Kind: model.KindText, Text: "Buen día"}, Recurrence: &model.Recurrence{Frequency: "daily"},
	})
	if err != nil || st.SeriesID == "" || st.Recurrence.Every != 1 {
		t.Fatalf("estado recurrente: %+v %v", st, err)
	}
	e.setClock(first)
	e.s.processDue(ctx)
	if rows := e.rows(st.SeriesID); len(rows) != 2 || rows[0].State != "sent" || rows[1].Status == nil || rows[1].Status.Text != "Buen día" {
		t.Fatalf("serie de estados: %+v", rows)
	}
	// El contador y la ancla los lleva el servicio, no el cliente.
	c, err := e.s.Save(ctx, model.Scheduled{
		AccountID: acc.ID, Kind: "message", ChatNativeID: "c", SendAt: first.UnixMilli(),
		Message:    &model.Outgoing{Kind: model.KindText, Text: "x"},
		Recurrence: &model.Recurrence{Frequency: "daily", Every: 1, Count: 9, FirstAt: 5},
	})
	if err != nil || c.Recurrence.Count != 0 || c.Recurrence.FirstAt != first.UnixMilli() {
		t.Fatalf("Count/FirstAt del cliente: %+v %v", c.Recurrence, err)
	}
}

func TestSchedulerUpdateAndUpdateSeries(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "original", first, model.Recurrence{Frequency: "daily", Every: 1})

	if _, err := e.s.Update(ctx, model.Scheduled{}); !errors.Is(err, port.ErrInvalid) {
		t.Errorf("Update sin id: %v", err)
	}
	ghost := a
	ghost.ID = "no-existe"
	if _, err := e.s.Update(ctx, ghost); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("Update inexistente: %v", err)
	}

	// Cambiar el texto de la pendiente: se propaga a las siguientes.
	edit := a
	edit.Message = &model.Outgoing{Kind: model.KindText, Text: "cambiado"}
	up, err := e.s.Update(ctx, edit)
	if err != nil || up.Message.Text != "cambiado" || up.Recurrence.FirstAt != first.UnixMilli() || up.SeriesID != a.SeriesID {
		t.Fatalf("Update: %+v %v", up, err)
	}
	e.setClock(first)
	e.s.processDue(ctx)
	if got := e.sentTexts(acc.ID); len(got) != 1 || got[0] != "cambiado" {
		t.Fatalf("enviado: %q", got)
	}
	if p := e.pending(a.SeriesID); len(p) != 1 || p[0].Message.Text != "cambiado" {
		t.Fatalf("la siguiente hereda el texto: %+v", p)
	}

	// UpdateSeries: cambia la hora y la regla de la pendiente; reancla en la nueva fecha.
	newAt := first.Add(24*time.Hour + 2*time.Hour) // 13:00 del día siguiente
	us, err := e.s.UpdateSeries(ctx, a.SeriesID, model.Scheduled{
		AccountID: acc.ID, Kind: "message", ChatNativeID: peerOf("Ana"), SendAt: newAt.UnixMilli(),
		Message:    &model.Outgoing{Kind: model.KindText, Text: "nuevo"},
		Recurrence: &model.Recurrence{Frequency: "weekly", Every: 1},
	})
	if err != nil || us.SeriesID != a.SeriesID || us.Recurrence.FirstAt != newAt.UnixMilli() || us.Recurrence.Count != 1 {
		t.Fatalf("UpdateSeries: %+v %v", us, err)
	}
	if p := e.pending(a.SeriesID); len(p) != 1 {
		t.Fatalf("sigue una sola pendiente: %+v", p)
	}
	e.setClock(newAt)
	e.s.processDue(ctx)
	if p := e.pending(a.SeriesID); len(p) != 1 || p[0].SendAt != newAt.Add(7*24*time.Hour).UnixMilli() {
		t.Fatalf("semanal desde la nueva hora: %+v", p)
	}

	// Quitar la repetición: la ocurrencia pendiente sale y la serie termina.
	lastAt := newAt.Add(7 * 24 * time.Hour)
	_, err = e.s.UpdateSeries(ctx, a.SeriesID, model.Scheduled{
		AccountID: acc.ID, Kind: "message", ChatNativeID: peerOf("Ana"), SendAt: lastAt.UnixMilli(),
		Message: &model.Outgoing{Kind: model.KindText, Text: "última"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.setClock(lastAt)
	e.s.processDue(ctx)
	if p := e.pending(a.SeriesID); len(p) != 0 {
		t.Fatalf("sin repetición no hay siguiente: %+v", p)
	}
	if _, err := e.s.UpdateSeries(ctx, a.SeriesID, model.Scheduled{}); !errors.Is(err, port.ErrInvalid) {
		t.Errorf("serie sin pendientes: %v", err)
	}
	// Una pendiente suelta puede volverse serie.
	one := e.message(acc.ID, peerOf("Luis"), "suelto", lastAt.Add(10*time.Hour))
	one.Recurrence = &model.Recurrence{Frequency: "weekly", Every: 1}
	made, err := e.s.Update(ctx, one)
	if err != nil || made.SeriesID == "" || made.Recurrence.FirstAt != one.SendAt {
		t.Fatalf("volverla serie: %+v %v", made, err)
	}
}

func TestSchedulerSeriesAttachmentIsSharedAndCleaned(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	src := filepath.Join(t.TempDir(), "afiche.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := campStart.Add(time.Hour)
	a, err := e.s.Save(ctx, model.Scheduled{
		AccountID: acc.ID, Kind: "message", ChatNativeID: peerOf("Ana"), SendAt: first.UnixMilli(),
		Message:    &model.Outgoing{Kind: model.KindImage, Text: "mira", FilePath: src, Mime: "image/png"},
		Recurrence: &model.Recurrence{Frequency: "daily", Every: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(a.Message.FilePath) != filepath.Join(scheduledMediaDir(), a.SeriesID) {
		t.Fatalf("la copia va a la carpeta de la serie: %s", a.Message.FilePath)
	}
	os.Remove(src)
	for d := 0; d < 3; d++ {
		e.setClock(first.Add(time.Duration(d) * 24 * time.Hour))
		e.s.processDue(ctx)
	}
	sent := e.adapter(acc.ID).Sent()
	if len(sent) != 3 {
		t.Fatalf("enviados: %d", len(sent))
	}
	for _, m := range sent {
		if m.FilePath != a.Message.FilePath {
			t.Fatalf("todas las ocurrencias usan la misma copia: %s", m.FilePath)
		}
	}
	// Borrar una ocurrencia suelta no toca la copia de la serie; borrar la serie sí.
	if err := e.s.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Message.FilePath); err != nil {
		t.Fatalf("la copia de la serie debía seguir: %v", err)
	}
	if err := e.s.DeleteSeries(ctx, a.SeriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Message.FilePath); !os.IsNotExist(err) {
		t.Fatal("la copia debía borrarse con la serie")
	}
}

func TestSchedulerSendNowOnSeriesOccurrenceContinuesSeries(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	first := campStart.Add(5 * time.Hour)
	a := e.series(acc.ID, peerOf("Ana"), "hola", first, model.Recurrence{Frequency: "daily", Every: 1})
	if err := e.s.SendNow(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	rows := e.rows(a.SeriesID)
	if len(rows) != 2 || rows[0].State != "sent" || rows[1].State != "pending" || rows[1].SendAt != first.Add(24*time.Hour).UnixMilli() {
		t.Fatalf("SendNow: %+v", rows)
	}
}

func TestSchedulerPreviewMatchesSeries(t *testing.T) {
	e, _ := newSchedEnvTx(t)
	first := time.Date(2027, 1, 31, 8, 0, 0, 0, time.UTC).UnixMilli()
	got := e.s.Preview(model.Recurrence{Frequency: "monthly", Every: 1}, first, 3)
	if len(got) != 3 || got[0] != first || got[1] != time.Date(2027, 2, 28, 8, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("Preview: %v", fmtAll(got, time.UTC))
	}
	if e.s.Preview(model.Recurrence{Frequency: "x"}, first, 3) != nil {
		t.Error("regla inválida: sin fechas")
	}
}
