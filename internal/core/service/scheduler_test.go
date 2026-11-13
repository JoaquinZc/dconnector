package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

type schedEnv struct {
	*campEnv
	s *Scheduler
}

func newSchedEnv(t *testing.T) *schedEnv {
	t.Helper()
	ce := newCampEnv(t)
	st := NewStatuses(ce.accounts, ce.router, ce.app)
	t.Cleanup(st.Stop)
	s := NewScheduler(ce.app, ce.msg, st, ce.accounts)
	s.now = ce.clock.Now
	s.tick = 5 * time.Millisecond
	s.loc, s.bootAt = time.UTC, campStart.UnixMilli()
	s.sleep = func(context.Context, time.Duration) error { return nil } // sin esperas reales
	t.Cleanup(s.Stop)
	return &schedEnv{campEnv: ce, s: s}
}

func (e *schedEnv) message(acc model.AccountID, chat, text string, at time.Time) model.Scheduled {
	e.t.Helper()
	it, err := e.s.Save(context.Background(), model.Scheduled{
		AccountID: acc, Kind: "message", ChatNativeID: chat, ChatTitle: "Ana",
		Message: &model.Outgoing{Kind: model.KindText, Text: text}, SendAt: at.UnixMilli(),
	})
	if err != nil {
		e.t.Fatalf("Save: %v", err)
	}
	return it
}

func (e *schedEnv) find(id string) model.Scheduled {
	e.t.Helper()
	all, err := e.s.List(context.Background(), 0, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, it := range all {
		if it.ID == id {
			return it
		}
	}
	e.t.Fatalf("no está el programado %s", id)
	return model.Scheduled{}
}

func TestSchedulerSaveValidation(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	msg := &model.Outgoing{Kind: model.KindText, Text: "hola"}
	at := campStart.Add(time.Hour).UnixMilli()

	bad := []model.Scheduled{
		{AccountID: acc.ID, Kind: "otro", SendAt: at},
		{AccountID: "wa-nada", Kind: "message", ChatNativeID: "c", Message: msg, SendAt: at},
		{AccountID: acc.ID, Kind: "message", ChatNativeID: "c", Message: msg},
		{AccountID: acc.ID, Kind: "message", Message: msg, SendAt: at},
		{AccountID: acc.ID, Kind: "message", ChatNativeID: "c", SendAt: at},
		{AccountID: acc.ID, Kind: "status", SendAt: at},
	}
	for i, it := range bad {
		if _, err := e.s.Save(ctx, it); err == nil {
			t.Errorf("caso %d: debía fallar", i)
		}
	}
	if _, err := e.s.Save(ctx, bad[1]); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("cuenta inexistente: %v", err)
	}
	it, err := e.s.Save(ctx, model.Scheduled{
		AccountID: acc.ID, Kind: "message", ChatNativeID: "c", Message: msg, SendAt: at,
		State: "sent", Error: "x", Status: &model.OutgoingStatus{Kind: model.KindText},
	})
	if err != nil || it.ID == "" || it.State != "pending" || it.Error != "" || it.CreatedAt == 0 || it.Status != nil {
		t.Fatalf("nuevo: %+v %v", it, err)
	}
	st, err := e.s.Save(ctx, model.Scheduled{AccountID: acc.ID, Kind: "status", ChatNativeID: "ignorado", Status: &model.OutgoingStatus{Kind: model.KindText, Text: "hola"}, SendAt: at})
	if err != nil || st.ChatNativeID != "" || st.Message != nil {
		t.Fatalf("estado: %+v %v", st, err)
	}
	// Editar uno pendiente conserva id y creación.
	it.Message = &model.Outgoing{Kind: model.KindText, Text: "cambiado"}
	out, err := e.s.Save(ctx, it)
	if err != nil || out.ID != it.ID || out.CreatedAt != it.CreatedAt || out.Message.Text != "cambiado" {
		t.Fatalf("editar: %+v %v", out, err)
	}
	if got, _ := e.s.List(ctx, 0, 0); len(got) != 2 {
		t.Errorf("List: %d", len(got))
	}
}

func TestSchedulerListRange(t *testing.T) {
	e := newSchedEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.message(acc.ID, "c", "uno", campStart.Add(1*time.Hour))
	e.message(acc.ID, "c", "dos", campStart.Add(3*time.Hour))
	e.message(acc.ID, "c", "tres", campStart.Add(5*time.Hour))
	got, err := e.s.List(context.Background(), campStart.Add(2*time.Hour).UnixMilli(), campStart.Add(4*time.Hour).UnixMilli())
	if err != nil || len(got) != 1 || got[0].Message.Text != "dos" {
		t.Fatalf("rango: %+v %v", got, err)
	}
}

func TestSchedulerSendsDueMessages(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	due := e.message(acc.ID, peerOf("Ana"), "ya toca", campStart.Add(-time.Minute))
	later := e.message(acc.ID, peerOf("Ana"), "más tarde", campStart.Add(time.Hour))

	e.s.processDue(ctx)
	if got := e.sentTexts(acc.ID); len(got) != 1 || got[0] != "ya toca" {
		t.Fatalf("enviados: %q", got)
	}
	if got := e.find(due.ID); got.State != "sent" || got.SentAt != campStart.UnixMilli() || got.Error != "" {
		t.Fatalf("enviado: %+v", got)
	}
	if got := e.find(later.ID); got.State != "pending" {
		t.Fatalf("aún no toca: %+v", got)
	}
	// Ya enviado: otro tick no lo repite.
	e.s.processDue(ctx)
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("no debía repetirse: %d", n)
	}
	e.clock.Advance(2 * time.Hour)
	e.s.processDue(ctx)
	if got := e.sentTexts(acc.ID); len(got) != 2 || got[1] != "más tarde" {
		t.Fatalf("enviados: %q", got)
	}
}

func TestSchedulerPostsStatus(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	it, err := e.s.Save(ctx, model.Scheduled{
		AccountID: acc.ID, Kind: "status", SendAt: campStart.Add(-time.Second).UnixMilli(),
		Status: &model.OutgoingStatus{Kind: model.KindText, Text: "Buen día", BgColor: "#25D366"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.s.processDue(ctx)
	if got := e.find(it.ID); got.State != "sent" {
		t.Fatalf("estado: %+v", got)
	}
	var posted bool
	for _, c := range e.adapter(acc.ID).Calls() {
		posted = posted || c == "PostStatus:text"
	}
	if !posted {
		t.Fatalf("debía publicar el estado: %v", e.adapter(acc.ID).Calls())
	}
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("un estado no es un mensaje: %d", n)
	}
}

func TestSchedulerRetriesWhenNotConnectedAndFailsAfterSixHours(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	it := e.message(acc.ID, peerOf("Ana"), "hola", campStart)
	if err := e.accounts.Disconnect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateDisconnected)

	e.s.processDue(ctx)
	if got := e.find(it.ID); got.State != "pending" || got.Error != "" {
		t.Fatalf("debía seguir pendiente: %+v", got)
	}
	e.clock.Advance(5*time.Hour + 59*time.Minute)
	e.s.processDue(ctx)
	if got := e.find(it.ID); got.State != "pending" {
		t.Fatalf("a las 5 h 59 min aún se espera: %+v", got)
	}
	e.clock.Advance(time.Minute)
	e.s.processDue(ctx)
	got := e.find(it.ID)
	if got.State != "failed" || got.Error != "La cuenta no estuvo conectada" {
		t.Fatalf("a las 6 h debía fallar: %+v", got)
	}
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("no se envió nada: %d", n)
	}

	// Corregirlo lo vuelve a pendiente y, con la cuenta conectada, sale.
	if err := e.accounts.Connect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateConnected)
	got.SendAt = e.clock.Now().UnixMilli()
	fixed, err := e.s.Save(ctx, got)
	if err != nil || fixed.State != "pending" || fixed.Error != "" {
		t.Fatalf("corregir: %+v %v", fixed, err)
	}
	e.s.processDue(ctx)
	if got := e.find(it.ID); got.State != "sent" {
		t.Fatalf("tras corregir: %+v", got)
	}
}

func TestSchedulerAdapterNotConnectedCountsAsNotConnected(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	it := e.message(acc.ID, peerOf("Ana"), "hola", campStart)
	ad := e.adapter(acc.ID)
	ad.SendErr = port.ErrNotConnected
	e.s.processDue(ctx)
	if got := e.find(it.ID); got.State != "pending" {
		t.Fatalf("estado: %+v", got)
	}
	ad.SendErr = nil
	e.s.processDue(ctx)
	if got := e.find(it.ID); got.State != "sent" {
		t.Fatalf("estado: %+v", got)
	}
}

func TestSchedulerSendErrorFailsWithMessage(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	it := e.message(acc.ID, peerOf("Ana"), "hola", campStart)
	e.adapter(acc.ID).SendErr = errors.New("chat bloqueado")
	e.s.processDue(ctx)
	got := e.find(it.ID)
	if got.State != "failed" || !strings.Contains(got.Error, "chat bloqueado") {
		t.Fatalf("estado: %+v", got)
	}
	// Un fallido no se reintenta solo.
	e.adapter(acc.ID).SendErr = nil
	e.s.processDue(ctx)
	if got := e.find(it.ID); got.State != "failed" {
		t.Fatalf("no debía reintentarse: %+v", got)
	}
	// Una capacidad que falta también deja el motivo.
	caps := e.adapter(acc.ID).Capabilities(nil)
	caps.SendText = false
	e.adapter(acc.ID).SetCapabilities(caps)
	again, err := e.s.Save(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	e.s.processDue(ctx)
	if got := e.find(again.ID); got.State != "failed" || !strings.Contains(got.Error, "no admitido") {
		t.Fatalf("sin capacidad: %+v", got)
	}
}

func TestSchedulerCancelDeleteSendNow(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	a := e.message(acc.ID, peerOf("Ana"), "a", campStart.Add(time.Hour))
	b := e.message(acc.ID, peerOf("Ana"), "b", campStart.Add(time.Hour))
	c := e.message(acc.ID, peerOf("Ana"), "c", campStart.Add(time.Hour))

	if err := e.s.Cancel(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.find(a.ID); got.State != "canceled" {
		t.Fatalf("cancelado: %+v", got)
	}
	e.clock.Advance(2 * time.Hour)
	e.s.processDue(ctx)
	if got := e.sentTexts(acc.ID); len(got) != 2 || got[0] == "a" || got[1] == "a" {
		t.Fatalf("lo cancelado no sale: %q", got)
	}
	if err := e.s.Cancel(ctx, b.ID); !errors.Is(err, port.ErrInvalid) {
		t.Errorf("cancelar uno enviado: %v", err)
	}
	if _, err := e.s.Save(ctx, e.find(a.ID)); !errors.Is(err, port.ErrInvalid) {
		t.Errorf("editar uno cancelado: %v", err)
	}
	if _, err := e.s.Save(ctx, e.find(b.ID)); !errors.Is(err, port.ErrInvalid) {
		t.Errorf("editar uno enviado: %v", err)
	}
	if err := e.s.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Delete(ctx, c.ID); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("borrar dos veces: %v", err)
	}
	if err := e.s.Cancel(ctx, "no-existe"); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("cancelar inexistente: %v", err)
	}

	// SendNow: sale ya, sin esperar su hora.
	d := e.message(acc.ID, peerOf("Ana"), "ahora", campStart.Add(10*time.Hour))
	if err := e.s.SendNow(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.find(d.ID); got.State != "sent" {
		t.Fatalf("SendNow: %+v", got)
	}
	if err := e.s.SendNow(ctx, d.ID); !errors.Is(err, port.ErrInvalid) {
		t.Errorf("SendNow de uno enviado: %v", err)
	}
	if err := e.s.SendNow(ctx, "no-existe"); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("SendNow inexistente: %v", err)
	}
	// SendNow con error devuelve el error y deja failed; luego se puede reenviar.
	f := e.message(acc.ID, peerOf("Ana"), "falla", campStart.Add(10*time.Hour))
	e.adapter(acc.ID).SendErr = errors.New("rechazado")
	if err := e.s.SendNow(ctx, f.ID); err == nil || !strings.Contains(err.Error(), "rechazado") {
		t.Fatalf("SendNow con error: %v", err)
	}
	if got := e.find(f.ID); got.State != "failed" {
		t.Fatalf("estado: %+v", got)
	}
	e.adapter(acc.ID).SendErr = nil
	if err := e.s.SendNow(ctx, f.ID); err != nil {
		t.Fatalf("reenviar un fallido: %v", err)
	}
	if got := e.find(f.ID); got.State != "sent" {
		t.Fatalf("estado: %+v", got)
	}
	// Sin conexión devuelve ErrNotConnected y sigue pendiente.
	g := e.message(acc.ID, peerOf("Ana"), "sin red", campStart.Add(10*time.Hour))
	if err := e.accounts.Disconnect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateDisconnected)
	if err := e.s.SendNow(ctx, g.ID); !errors.Is(err, port.ErrNotConnected) {
		t.Fatalf("SendNow sin conexión: %v", err)
	}
	if got := e.find(g.ID); got.State != "pending" {
		t.Fatalf("estado: %+v", got)
	}
}

func TestSchedulerCopiesAttachment(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	src := filepath.Join(t.TempDir(), "afiche.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	it, err := e.s.Save(ctx, model.Scheduled{
		AccountID: acc.ID, Kind: "message", ChatNativeID: peerOf("Ana"), SendAt: campStart.UnixMilli(),
		Message: &model.Outgoing{Kind: model.KindImage, Text: "mira", FilePath: src, Mime: "image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	copyPath := it.Message.FilePath
	if copyPath == src || filepath.Dir(copyPath) != filepath.Join(scheduledMediaDir(), it.ID) {
		t.Fatalf("debía copiarse: %s", copyPath)
	}
	os.Remove(src) // el original ya no está a la hora de envío
	e.s.processDue(ctx)
	sent := e.adapter(acc.ID).Sent()
	if len(sent) != 1 || sent[0].FilePath != copyPath || sent[0].Kind != model.KindImage {
		t.Fatalf("enviado: %+v", sent)
	}
	if got := e.find(it.ID); got.State != "sent" {
		t.Fatalf("estado: %+v", got)
	}
	// Editar sin cambiar el archivo no lo copia otra vez.
	out, err := e.s.Save(ctx, it)
	if err == nil {
		t.Fatalf("uno enviado no se edita: %+v", out)
	}
	if err := e.s.Delete(ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Error("la copia debía borrarse con el programado")
	}
	// Un adjunto inexistente se rechaza al guardar.
	if _, err := e.s.Save(ctx, model.Scheduled{
		AccountID: acc.ID, Kind: "message", ChatNativeID: "c", SendAt: 1,
		Message: &model.Outgoing{Kind: model.KindImage, FilePath: filepath.Join(t.TempDir(), "no.png")},
	}); !errors.Is(err, port.ErrInvalid) {
		t.Errorf("adjunto inexistente: %v", err)
	}
}

func TestSchedulerStartStop(t *testing.T) {
	e := newSchedEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	it := e.message(acc.ID, peerOf("Ana"), "desde el bucle", campStart)
	e.s.Start(ctx)
	e.s.Start(ctx) // la segunda vez no hace nada
	eventually(t, "programado enviado por el bucle", func() bool { return e.find(it.ID).State == "sent" })
	e.s.Stop()
	e.s.Stop()
	// Tras parar, lo nuevo no sale solo.
	late := e.message(acc.ID, peerOf("Ana"), "tarde", campStart)
	time.Sleep(30 * time.Millisecond)
	if got := e.find(late.ID); got.State != "pending" {
		t.Fatalf("parado no envía: %+v", got)
	}
	e.s.Start(ctx)
	eventually(t, "reanudado", func() bool { return e.find(late.ID).State == "sent" })
	e.s.Stop()
}
