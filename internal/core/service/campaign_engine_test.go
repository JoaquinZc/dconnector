package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dconnector/internal/core/campaign"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// ---------------------------------------------------------------------------------------------
// Orden, espera entre envíos y «escribiendo…»

func TestEngineSendsInOrderWithPauses(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	cold := e.contact(acc.ID, "Dani")
	warm := e.warm(acc.ID, "Ana")
	luke := e.lukewarm(acc.ID, "Carla")
	cp := e.save(acc.ID, nil, cold, luke, warm)
	e.launch(cp.ID)
	e.progress() // descarta los avisos del lanzamiento

	// Primer tick: envía el más cálido.
	if !e.tick(acc.ID) {
		t.Fatal("debería seguir en marcha")
	}
	if got := e.sentTexts(acc.ID); len(got) != 1 || got[0] != "Hola Ana, tenemos algo para ti" {
		t.Fatalf("enviados: %q", got)
	}
	got := e.get(cp.ID)
	if got.Totals.Sent != 1 || got.Totals.Queued != 2 {
		t.Fatalf("totales: %+v", got.Totals)
	}
	sent := e.queue(cp.ID, "sent")[0]
	if sent.SentAt != campStart.UnixMilli() || sent.MessageKey == "" || !strings.HasPrefix(string(sent.MessageKey), string(acc.ID)+"|"+peerOf("Ana")+"|") {
		t.Errorf("item enviado: %+v", sent)
	}
	ps := e.progress()
	p, ok := lastProgress(ps, cp.ID)
	if !ok || p.Totals.Sent != 1 || p.State != model.CampaignRunning || p.Waiting != "" || p.NextAt != campStart.Add(time.Second).UnixMilli() {
		t.Fatalf("progreso: %+v", ps)
	}

	// Mismo instante: todavía no toca (espera entre envíos de 1 s).
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("no debía enviar antes de tiempo: %d", n)
	}
	e.clock.Advance(999 * time.Millisecond)
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("faltaba 1 ms: %d", n)
	}
	e.clock.Advance(time.Millisecond)
	e.tick(acc.ID)
	e.clock.Advance(time.Second)
	e.tick(acc.ID)
	texts := e.sentTexts(acc.ID)
	if len(texts) != 3 || texts[1] != "Hola Carla, tenemos algo para ti" || texts[2] != "Hola Dani, tenemos algo para ti" {
		t.Fatalf("orden de envío: %q", texts)
	}
	// Sin nada pendiente, el siguiente tick la termina y el trabajador se retira.
	if e.tick(acc.ID) {
		t.Fatal("ya no debía haber campañas en marcha")
	}
	if e.get(cp.ID).State != model.CampaignDone {
		t.Fatalf("estado: %s", e.get(cp.ID).State)
	}
	if p, _ := lastProgress(e.progress(), cp.ID); p.State != model.CampaignDone || p.Totals.Sent != 3 {
		t.Errorf("último progreso: %+v", p)
	}
}

func TestEngineDelayDependsOnNextRisk(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	warm := e.warm(acc.ID, "Ana")
	warm2 := e.warm(acc.ID, "Beto")
	cold := e.contact(acc.ID, "Dani")
	pacing := campPacing()
	pacing.MinDelaySec, pacing.MaxDelaySec = 10, 30          // con randf = 0,5 → 20 s
	pacing.ColdMinDelaySec, pacing.ColdMaxDelaySec = 60, 120 // → 90 s
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, warm, warm2, cold)
	e.launch(cp.ID)

	e.tick(acc.ID) // Ana; el siguiente (Beto) es conocido → 20 s
	ps := e.progress()
	if p, _ := lastProgress(ps, cp.ID); p.NextAt != campStart.Add(20*time.Second).UnixMilli() {
		t.Fatalf("tras Ana: %+v", p)
	}
	e.clock.Advance(20 * time.Second)
	e.tick(acc.ID) // Beto; el siguiente (Dani) es frío → 90 s
	ps = e.progress()
	if p, _ := lastProgress(ps, cp.ID); p.NextAt != campStart.Add(110*time.Second).UnixMilli() {
		t.Fatalf("tras Beto: %+v", p)
	}
}

func TestEngineTypingBeforeSending(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	pacing := campPacing()
	pacing.TypingMsPerChar = 100
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing; c.Text = "Hola {{nombre}}" }, ana)
	e.launch(cp.ID)
	e.tick(acc.ID)

	// "Hola Ana" = 8 caracteres × 100 ms.
	if s := e.sleeps(); len(s) != 1 || s[0] != 800*time.Millisecond {
		t.Fatalf("pausas: %v", s)
	}
	calls := e.adapter(acc.ID).Calls()
	var typing []string
	for _, c := range calls {
		if strings.HasPrefix(c, "SetTyping:") {
			typing = append(typing, c)
		}
	}
	if len(typing) != 1 || typing[0] != "SetTyping:"+peerOf("Ana")+":true" {
		t.Fatalf("llamadas: %v", calls)
	}
}

func TestEngineTypingIsCappedAt8Seconds(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	pacing := campPacing()
	pacing.TypingMsPerChar = 300
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing; c.Text = strings.Repeat("palabra ", 50) }, ana)
	e.launch(cp.ID)
	e.tick(acc.ID)
	if s := e.sleeps(); len(s) != 1 || s[0] != 8*time.Second {
		t.Fatalf("pausas: %v", s)
	}
}

func TestEngineNoTypingWhenZero(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	cp := e.save(acc.ID, nil, ana) // TypingMsPerChar = 0
	e.launch(cp.ID)
	e.tick(acc.ID)
	if len(e.sleeps()) != 0 {
		t.Fatalf("no debía esperar: %v", e.sleeps())
	}
	for _, c := range e.adapter(acc.ID).Calls() {
		if strings.HasPrefix(c, "SetTyping") {
			t.Fatalf("no debía pedir «escribiendo»: %s", c)
		}
	}
}

func TestEngineStopsIfPausedWhileTyping(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	pacing := campPacing()
	pacing.TypingMsPerChar = 100
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, ana)
	e.launch(cp.ID)
	// Mientras «escribe», alguien pausa la campaña.
	e.c.sleep = func(ctx2 context.Context, d time.Duration) { _ = e.c.Pause(ctx, cp.ID) }
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("no debía enviar: %d", n)
	}
	if got := e.get(cp.ID); got.State != model.CampaignPaused || got.Totals.Sent != 0 {
		t.Fatalf("campaña: %+v", got)
	}
	if got := e.queue(cp.ID, "pending"); len(got) != 1 {
		t.Fatalf("debía seguir pendiente: %+v", got)
	}
}

// ---------------------------------------------------------------------------------------------
// Ventana, conexión, límites

func TestEngineWaitsForWindow(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	// Jueves 12 a las 10:00; la ventana abre a las 14:00.
	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.Window = model.SendWindow{FromMinute: 14 * 60, ToMinute: 18 * 60, Weekdays: []int{4}}
	}, ana)
	e.launch(cp.ID)
	e.progress()
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("fuera de la ventana no se envía: %d", n)
	}
	p, ok := lastProgress(e.progress(), cp.ID)
	opens := time.Date(2026, 11, 12, 14, 0, 0, 0, time.UTC).UnixMilli()
	if !ok || p.Waiting != "window" || p.NextAt != opens || p.State != model.CampaignRunning {
		t.Fatalf("progreso: %+v", p)
	}
	e.clock.Advance(4 * time.Hour)
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("en la ventana debía enviar: %d", n)
	}
	// Después de cerrar (18:00) esperaría a la del jueves siguiente.
	cp2 := e.save(acc.ID, func(c *model.Campaign) {
		c.Name = "otra"
		c.Window = model.SendWindow{FromMinute: 14 * 60, ToMinute: 15 * 60, Weekdays: []int{4}}
	}, e.warm(acc.ID, "Beto"))
	e.launch(cp2.ID)
	e.clock.Advance(2 * time.Hour) // 16:00: cerrada
	e.progress()
	e.tick(acc.ID)
	p, _ = lastProgress(e.progress(), cp2.ID)
	if p.Waiting != "window" || p.NextAt != time.Date(2026, 11, 19, 14, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("próxima apertura: %+v", p)
	}
}

func TestEngineWaitsWhileDisconnected(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	cp := e.save(acc.ID, nil, ana)
	e.launch(cp.ID)
	if err := e.accounts.Disconnect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateDisconnected)
	e.progress()
	if !e.tick(acc.ID) {
		t.Fatal("la campaña sigue running aunque no envíe")
	}
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("sin conexión no se envía: %d", n)
	}
	p, _ := lastProgress(e.progress(), cp.ID)
	if p.Waiting != "disconnected" || p.State != model.CampaignRunning {
		t.Fatalf("progreso: %+v", p)
	}
	if err := e.accounts.Connect(ctx, acc.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(acc.ID, model.StateConnected)
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("al conectar debía enviar: %d", n)
	}
}

func TestEngineKeepsItemPendingWhenAdapterIsNotConnected(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	cp := e.save(acc.ID, nil, ana)
	e.launch(cp.ID)
	ad := e.adapter(acc.ID)
	ad.SendErr = port.ErrNotConnected
	e.progress()
	e.tick(acc.ID)
	got := e.get(cp.ID)
	if got.Totals.Failed != 0 || got.Totals.Sent != 0 || got.Totals.Queued != 1 {
		t.Fatalf("no debía fallar: %+v", got.Totals)
	}
	if p, _ := lastProgress(e.progress(), cp.ID); p.Waiting != "disconnected" {
		t.Fatalf("progreso: %+v", p)
	}
	ad.SendErr = nil
	e.tick(acc.ID)
	if e.get(cp.ID).Totals.Sent != 1 {
		t.Fatal("debía enviar al volver la conexión")
	}
}

func TestEngineRateLimitedRests(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	cp := e.save(acc.ID, nil, ana)
	e.launch(cp.ID)
	ad := e.adapter(acc.ID)
	ad.SendErr = port.ErrRateLimited
	e.progress()
	e.tick(acc.ID)
	got := e.get(cp.ID)
	if got.Totals.Failed != 0 || got.Totals.Queued != 1 {
		t.Fatalf("no debía fallar: %+v", got.Totals)
	}
	p, _ := lastProgress(e.progress(), cp.ID)
	if p.Waiting != "batch_rest" || p.NextAt < campStart.Add(time.Minute).UnixMilli() {
		t.Fatalf("progreso: %+v", p)
	}
}

func TestEngineHourlyLimit(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 5; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%d", i)))
	}
	pacing := campPacing()
	pacing.HourlyLimit = 2
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, cs...)
	e.launch(cp.ID)
	for i := 0; i < 4; i++ {
		e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	if n := len(e.sentTexts(acc.ID)); n != 2 {
		t.Fatalf("límite por hora: se enviaron %d", n)
	}
	if p, _ := lastProgress(e.progress(), cp.ID); p.Waiting != "hourly" {
		t.Fatalf("progreso: %+v", p)
	}
	e.clock.Advance(time.Hour)
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 3 {
		t.Fatalf("pasada la hora debía seguir: %d", n)
	}
}

func TestEngineDailyLimit(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 5; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%d", i)))
	}
	pacing := campPacing()
	pacing.DailyLimit = 3
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, cs...)
	e.launch(cp.ID)
	for i := 0; i < 6; i++ {
		e.tick(acc.ID)
		e.clock.Advance(time.Minute)
	}
	if n := len(e.sentTexts(acc.ID)); n != 3 {
		t.Fatalf("límite diario: se enviaron %d", n)
	}
	p, _ := lastProgress(e.progress(), cp.ID)
	if p.Waiting != "daily" || p.NextAt != time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("progreso: %+v", p)
	}
	// Al día siguiente sigue.
	e.clock.Advance(24 * time.Hour)
	e.run(acc.ID, time.Minute, 10)
	if n := len(e.sentTexts(acc.ID)); n != 5 {
		t.Fatalf("al día siguiente debía terminar: %d", n)
	}
}

func TestEngineWarmupLimitGrowsAndIsSavedOnFirstSend(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 8; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%d", i)))
	}
	pacing := campPacing()
	pacing.DailyLimit, pacing.WarmupEnabled, pacing.WarmupStart, pacing.WarmupDays = 5, true, 1, 2
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, cs...)
	e.launch(cp.ID)

	if v, _ := e.testEnv.app.GetSetting(ctx, "warmup_started_at:"+string(acc.ID)); v != "" {
		t.Fatalf("aún no hay envíos: %q", v)
	}
	// Día 0: límite = 1.
	for i := 0; i < 4; i++ {
		e.tick(acc.ID)
		e.clock.Advance(time.Minute)
	}
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("día 0: se enviaron %d", n)
	}
	if v, _ := e.testEnv.app.GetSetting(ctx, "warmup_started_at:"+string(acc.ID)); v != fmt.Sprint(campStart.UnixMilli()) {
		t.Fatalf("warmup_started_at = %q", v)
	}
	// Día 1 (a las 10:05 + 24 h → día 1 del calentamiento): 1 + 4·1/2 = 3 por día; ya van 0 hoy.
	e.clock.Advance(24 * time.Hour)
	for i := 0; i < 6; i++ {
		e.tick(acc.ID)
		e.clock.Advance(time.Minute)
	}
	if n := len(e.sentTexts(acc.ID)); n != 1+3 {
		t.Fatalf("día 1: en total %d", n)
	}
	// Día 2 en adelante: el límite completo (5).
	e.clock.Advance(24 * time.Hour)
	e.run(acc.ID, time.Minute, 20)
	if n := len(e.sentTexts(acc.ID)); n != 8 {
		t.Fatalf("día 2: en total %d", n)
	}
	// El inicio del calentamiento no se pisa.
	if v, _ := e.testEnv.app.GetSetting(ctx, "warmup_started_at:"+string(acc.ID)); v != fmt.Sprint(campStart.UnixMilli()) {
		t.Fatalf("warmup_started_at cambió: %q", v)
	}
}

func TestEngineColdDailyLimit(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	warm := e.warm(acc.ID, "Ana")
	cold1, cold2, cold3 := e.contact(acc.ID, "Dani"), e.contact(acc.ID, "Eva"), e.contact(acc.ID, "Fran")
	pacing := campPacing()
	pacing.ColdDailyLimit = 2
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, warm, cold1, cold2, cold3)
	e.launch(cp.ID)
	for i := 0; i < 8; i++ {
		e.tick(acc.ID)
		e.clock.Advance(time.Minute)
	}
	if n := len(e.sentTexts(acc.ID)); n != 3 { // 1 conocido + 2 fríos
		t.Fatalf("se enviaron %d: %q", n, e.sentTexts(acc.ID))
	}
	p, _ := lastProgress(e.progress(), cp.ID)
	if p.Waiting != "daily" {
		t.Fatalf("progreso: %+v", p)
	}
	e.clock.Advance(24 * time.Hour)
	e.run(acc.ID, time.Minute, 10)
	if n := len(e.sentTexts(acc.ID)); n != 4 {
		t.Fatalf("al día siguiente: %d", n)
	}
}

func TestEngineBatchRest(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 5; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%d", i)))
	}
	pacing := campPacing()
	pacing.BatchSize, pacing.BatchRestMinutes = 2, 10 // randf = 0,5 → exactamente 10 min
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, cs...)
	e.launch(cp.ID)

	e.tick(acc.ID)
	e.clock.Advance(2 * time.Second)
	e.tick(acc.ID) // segundo de la tanda: empieza el descanso
	restEnds := e.clock.Now().Add(-0 * time.Second).Add(10 * time.Minute).UnixMilli()
	p, _ := lastProgress(e.progress(), cp.ID)
	if p.Waiting != "batch_rest" || p.NextAt != restEnds {
		t.Fatalf("progreso: %+v (esperaba %d)", p, restEnds)
	}
	e.clock.Advance(9 * time.Minute)
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 2 {
		t.Fatalf("durante el descanso no se envía: %d", n)
	}
	e.clock.Advance(time.Minute + time.Second)
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 3 {
		t.Fatalf("tras el descanso se sigue: %d", n)
	}
	e.progress()
	e.clock.Advance(2 * time.Second)
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 4 { // la nueva tanda lleva 2: este es el segundo
		t.Fatalf("nueva tanda: %d", n)
	}
	if p, _ := lastProgress(e.progress(), cp.ID); p.Waiting != "batch_rest" {
		t.Fatalf("segunda tanda completa: %+v", p)
	}
}

// ---------------------------------------------------------------------------------------------
// Fallos, bajas, freno

func TestEngineFailedSendIsRecorded(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana, beto := e.warm(acc.ID, "Ana"), e.warm(acc.ID, "Beto")
	cp := e.save(acc.ID, nil, ana, beto)
	e.launch(cp.ID)
	ad := e.adapter(acc.ID)
	ad.SendErr = errors.New("número no registrado")
	e.tick(acc.ID)
	ad.SendErr = nil
	e.clock.Advance(2 * time.Second)
	e.tick(acc.ID)
	got := e.get(cp.ID)
	if got.Totals.Failed != 1 || got.Totals.Sent != 1 || got.Totals.Queued != 0 {
		t.Fatalf("totales: %+v", got.Totals)
	}
	failed := e.queue(cp.ID, "failed")
	if len(failed) != 1 || !strings.Contains(failed[0].Error, "número no registrado") || failed[0].MessageKey != "" {
		t.Fatalf("item fallido: %+v", failed)
	}
}

func TestEngineBrakesOnFailureRateAndResumeClearsIt(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 30; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%02d", i)))
	}
	pacing := campPacing()
	pacing.BrakeEnabled, pacing.BrakeFailRate, pacing.BrakeMinSent, pacing.BrakeUnreadRate, pacing.BrakeOptOuts = true, 30, 30, 95, 3
	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.Pacing = pacing
		c.Text = "{Hola|Buenas|Qué tal} {{nombre}}"
	}, cs...)
	e.launch(cp.ID)
	e.adapter(acc.ID).SendErr = errors.New("rechazado")

	var stopped bool
	for i := 0; i < 20 && !stopped; i++ {
		stopped = !e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	got := e.get(cp.ID)
	if got.State != model.CampaignBraked || !strings.Contains(got.BrakeReason, "Freno automático: fallaron 10 de 10") {
		t.Fatalf("campaña: %s %q", got.State, got.BrakeReason)
	}
	if got.Totals.Failed != 10 {
		t.Fatalf("debía frenar al décimo intento: %+v", got.Totals)
	}
	if p, _ := lastProgress(e.progress(), cp.ID); p.State != model.CampaignBraked || p.BrakeReason == "" {
		t.Fatalf("progreso: %+v", p)
	}
	// Frenada no envía más.
	if e.tick(acc.ID) {
		t.Fatal("una campaña frenada no cuenta como en marcha")
	}
	// Reanudar quita el freno y vuelve a contar desde cero: no frena otra vez de inmediato.
	if err := e.c.Resume(ctx, cp.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.get(cp.ID); got.State != model.CampaignRunning || got.BrakeReason != "" {
		t.Fatalf("tras reanudar: %s %q", got.State, got.BrakeReason)
	}
	for i := 0; i < 5; i++ {
		e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	if got := e.get(cp.ID); got.State != model.CampaignRunning || got.Totals.Failed != 15 {
		t.Fatalf("no debía frenar con 5 intentos nuevos: %s %+v", got.State, got.Totals)
	}
	// Con 10 intentos nuevos fallidos vuelve a frenar.
	for i := 0; i < 5; i++ {
		e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	if got := e.get(cp.ID); got.State != model.CampaignBraked || got.Totals.Failed != 20 {
		t.Fatalf("debía frenar otra vez: %s %+v", got.State, got.Totals)
	}
}

func TestEngineBrakesWhenNobodyReads(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 8; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%d", i)))
	}
	pacing := campPacing()
	pacing.BrakeEnabled, pacing.BrakeMinSent, pacing.BrakeUnreadRate, pacing.BrakeFailRate, pacing.BrakeOptOuts = true, 5, 95, 30, 3
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, cs...)
	e.launch(cp.ID)
	for i := 0; i < 5; i++ {
		e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	if got := e.get(cp.ID); got.State != model.CampaignRunning {
		t.Fatalf("aún es pronto: %s", got.State)
	}
	e.clock.Advance(3 * time.Hour) // pasan más de 2 h desde el primer envío y nadie leyó
	e.tick(acc.ID)
	got := e.get(cp.ID)
	if got.State != model.CampaignBraked || !strings.Contains(got.BrakeReason, "casi nadie lo leyó") {
		t.Fatalf("campaña: %s %q", got.State, got.BrakeReason)
	}
}

func TestEngineDoesNotBrakeWhenMessagesAreRead(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 8; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%d", i)))
	}
	pacing := campPacing()
	pacing.BrakeEnabled, pacing.BrakeMinSent, pacing.BrakeUnreadRate, pacing.BrakeFailRate, pacing.BrakeOptOuts = true, 5, 95, 30, 3
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, cs...)
	e.launch(cp.ID)
	for i := 0; i < 5; i++ {
		e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	// Todos los enviados quedan leídos: los recibos llegan al almacén de la cuenta.
	store := e.store(acc.ID)
	for _, it := range e.queue(cp.ID, "sent") {
		eventually(t, "mensaje guardado", func() bool {
			_, chat, native := it.MessageKey.Split()
			_, err := store.GetMessage(context.Background(), chat, native)
			return err == nil
		})
		_ = store.ApplyReceipt(context.Background(), model.ReceiptEvent{MessageKeys: []model.MessageKey{it.MessageKey}, Delivery: model.DeliveryRead})
	}
	e.clock.Advance(3 * time.Hour)
	e.tick(acc.ID)
	if got := e.get(cp.ID); got.State != model.CampaignRunning {
		t.Fatalf("con lecturas no debía frenar: %s %q", got.State, got.BrakeReason)
	}
}

func TestEngineSkipsContactsWhoOptedOutAfterLaunch(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana, beto := e.warm(acc.ID, "Ana"), e.warm(acc.ID, "Beto")
	cp := e.save(acc.ID, nil, ana, beto)
	e.launch(cp.ID)
	// Ana pide la baja después del lanzamiento y antes de su turno.
	ana.OptedOut = true
	_ = e.testEnv.app.SaveContact(ctx, ana)
	e.run(acc.ID, 2*time.Second, 10)
	if got := e.sentTexts(acc.ID); len(got) != 1 || !strings.Contains(got[0], "Beto") {
		t.Fatalf("enviados: %q", got)
	}
	skipped := e.queue(cp.ID, "skipped")
	if len(skipped) != 1 || skipped[0].ContactID != ana.ID || skipped[0].Error == "" {
		t.Fatalf("saltados: %+v", skipped)
	}
	if e.get(cp.ID).State != model.CampaignDone {
		t.Fatal("debía terminar")
	}
	// Un contacto borrado también se salta.
	cp2 := e.save(acc.ID, func(c *model.Campaign) { c.Name = "otra" }, beto)
	e.launch(cp2.ID)
	_ = e.testEnv.app.DeleteContact(ctx, beto.ID)
	e.run(acc.ID, 2*time.Second, 10)
	if got := e.queue(cp2.ID, "skipped"); len(got) != 1 {
		t.Fatalf("contacto borrado: %+v", got)
	}
}

func TestEngineRepliesAndOptOuts(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 6; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%d", i)))
	}
	pacing := campPacing()
	pacing.BrakeEnabled, pacing.BrakeMinSent, pacing.BrakeUnreadRate, pacing.BrakeFailRate, pacing.BrakeOptOuts = true, 30, 95, 30, 3
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Pacing = pacing }, cs...)
	e.launch(cp.ID)
	for i := 0; i < 4; i++ {
		e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	sent := e.queue(cp.ID, "sent")
	if len(sent) != 4 {
		t.Fatalf("enviados: %d", len(sent))
	}
	e.progress()

	// Un chat al que no se envió nada no cuenta, ni un mensaje anterior al envío.
	unsent := e.queue(cp.ID, "pending")[0].ChatNativeID
	e.c.OnIncoming(acc.ID, unsent, campStart.Add(time.Hour).UnixMilli())
	e.c.OnIncoming(acc.ID, sent[0].ChatNativeID, sent[0].SentAt-1)
	if got := e.get(cp.ID); got.Totals.Replied != 0 {
		t.Fatalf("no debía contar: %+v", got.Totals)
	}
	// La primera respuesta cuenta; las siguientes de la misma persona no.
	e.c.OnIncoming(acc.ID, sent[0].ChatNativeID, sent[0].SentAt+5)
	e.c.OnIncoming(acc.ID, sent[0].ChatNativeID, sent[0].SentAt+9)
	e.c.OnIncoming(acc.ID, sent[1].ChatNativeID, sent[1].SentAt+1)
	if got := e.get(cp.ID); got.Totals.Replied != 2 {
		t.Fatalf("respondidos: %+v", got.Totals)
	}
	if p, ok := lastProgress(e.progress(), cp.ID); !ok || p.Totals.Replied != 2 {
		t.Fatalf("progreso: %+v", p)
	}
	// Bajas: la misma persona dos veces cuenta una; sin contacto no cuenta.
	e.c.OnOptOut(acc.ID, sent[0].ContactID)
	e.c.OnOptOut(acc.ID, sent[0].ContactID)
	e.c.OnOptOut(acc.ID, "")
	e.c.OnOptOut(acc.ID, "ct-que-no-recibio")
	got := e.get(cp.ID)
	if got.Totals.OptOut != 1 || got.State != model.CampaignRunning {
		t.Fatalf("bajas: %+v %s", got.Totals, got.State)
	}
	// Con 3 bajas se frena sola.
	e.c.OnOptOut(acc.ID, sent[1].ContactID)
	e.c.OnOptOut(acc.ID, sent[2].ContactID)
	got = e.get(cp.ID)
	if got.State != model.CampaignBraked || !strings.Contains(got.BrakeReason, "3 personas") || got.Totals.OptOut != 3 {
		t.Fatalf("freno por bajas: %s %q %+v", got.State, got.BrakeReason, got.Totals)
	}
	_ = ctx
}

func TestEngineStatsSurviveRestart(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana, beto := e.warm(acc.ID, "Ana"), e.warm(acc.ID, "Beto")
	cp := e.save(acc.ID, nil, ana, beto)
	e.launch(cp.ID)
	e.run(acc.ID, 2*time.Second, 10)
	sent := e.queue(cp.ID, "sent")
	if len(sent) != 2 {
		t.Fatal("debían enviarse los dos")
	}
	// Ana respondió (mensaje entrante posterior al envío) y Beto se dio de baja.
	e.seedMessage(acc.ID, sent[0].ChatNativeID, "resp1", sent[0].SentAt+10, false, "gracias")
	beto.OptedOut = true
	_ = e.testEnv.app.SaveContact(context.Background(), beto)

	// Un Campaigns nuevo (como tras reiniciar la app) rehace los totales desde los mensajes y contactos.
	fresh := NewCampaigns(e.app, e.accounts, e.msg, e.bus)
	fresh.now, fresh.loc = e.clock.Now, time.UTC
	got, err := fresh.Get(context.Background(), cp.ID)
	if err != nil || got.Totals.Sent != 2 || got.Totals.Replied != 1 || got.Totals.OptOut != 1 {
		t.Fatalf("totales tras reiniciar: %+v %v", got.Totals, err)
	}
}

// ---------------------------------------------------------------------------------------------
// Adjuntos

func TestEngineSendsAttachmentByMime(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	cases := []struct {
		file string
		kind model.ContentKind
		mime string
	}{
		{"foto.png", model.KindImage, "image/png"},
		{"clip.MP4", model.KindVideo, "video/mp4"},
		{"lista.pdf", model.KindDocument, "application/pdf"},
		{"datos.xyz", model.KindDocument, ""},
	}
	for i, tc := range cases {
		name := fmt.Sprintf("P%d", i)
		ct := e.warm(acc.ID, name)
		src := filepath.Join(t.TempDir(), tc.file)
		if err := os.WriteFile(src, []byte("contenido"), 0o644); err != nil {
			t.Fatal(err)
		}
		cp := e.save(acc.ID, func(c *model.Campaign) { c.Name = tc.file; c.FilePath = src; c.Text = "Mira {{nombre}}" }, ct)
		e.launch(cp.ID)
		e.run(acc.ID, 2*time.Second, 10)
		sent := e.adapter(acc.ID).Sent()
		out := sent[len(sent)-1]
		if out.Kind != tc.kind || out.Text != "Mira "+name || out.FileName != strings.ToLower(tc.file) && out.FileName != tc.file {
			t.Errorf("%s: %+v", tc.file, out)
		}
		if tc.mime != "" && out.Mime != tc.mime {
			t.Errorf("%s: mime %q", tc.file, out.Mime)
		}
		if filepath.Dir(out.FilePath) != filepath.Join(mediaDirOf(cp.ID)) {
			t.Errorf("%s: debía enviarse la copia de la campaña: %s", tc.file, out.FilePath)
		}
		if got := e.get(cp.ID); got.Totals.Sent != 1 {
			t.Errorf("%s: totales %+v", tc.file, got.Totals)
		}
	}
}

func TestEngineAttachmentOnlyCampaign(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	src := filepath.Join(t.TempDir(), "afiche.jpg")
	os.WriteFile(src, []byte("jpg"), 0o644)
	cp := e.save(acc.ID, func(c *model.Campaign) { c.Text = ""; c.FilePath = src }, ana)
	e.launch(cp.ID)
	e.run(acc.ID, 2*time.Second, 10)
	sent := e.adapter(acc.ID).Sent()
	if len(sent) != 1 || sent[0].Kind != model.KindImage || sent[0].Text != "" {
		t.Fatalf("enviado: %+v", sent)
	}
}

// ---------------------------------------------------------------------------------------------
// Prueba y gana (A/B)

func TestEngineABTestPicksWinners(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 9; i++ {
		cs = append(cs, e.contact(acc.ID, fmt.Sprintf("P%d", i)))
	}
	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.ABEnabled = true
		c.Text = "AAA hola {{nombre}}, te cuento algo"
		c.Variants = []string{"BBB buenas {{nombre}}, mira esto", "CCC qué tal {{nombre}}, hay novedades"}
	}, cs...)
	e.launch(cp.ID)

	// 9 destinatarios, 3 variantes → 3 por variante; muestra del 20 % = 1 de cada una.
	if pend, held := e.queue(cp.ID, "pending"), e.queue(cp.ID, "held"); len(pend) != 3 || len(held) != 6 {
		t.Fatalf("pendientes %d, retenidos %d", len(pend), len(held))
	}
	for _, it := range e.queue(cp.ID, "pending") {
		if !it.ABSample {
			t.Fatalf("lo pendiente es la muestra: %+v", it)
		}
	}
	// Se envía la muestra y la campaña espera (sigue running, con retenidos).
	for i := 0; i < 6; i++ {
		e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	if got := e.get(cp.ID); got.State != model.CampaignRunning || got.Totals.Sent != 3 || got.Totals.Queued != 6 {
		t.Fatalf("tras la muestra: %s %+v", got.State, got.Totals)
	}
	if n := len(e.sentTexts(acc.ID)); n != 3 {
		t.Fatalf("solo la muestra: %d", n)
	}
	e.clock.Advance(3 * time.Hour) // faltan 4 h desde el último de la muestra
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 3 || len(e.queue(cp.ID, "held")) != 6 {
		t.Fatalf("antes de las 4 h no se libera nada: %d enviados", n)
	}
	if p, _ := lastProgress(e.progress(), cp.ID); p.Waiting != "" || p.NextAt == 0 {
		t.Fatalf("debía avisar cuándo se decide: %+v", p)
	}

	// Las variantes B y C reciben respuestas; la A, ninguna → ganan B y C.
	var winnersWant = map[int]bool{1: true, 2: true}
	for _, it := range e.queue(cp.ID, "sent") {
		if winnersWant[it.Variant] {
			e.c.OnIncoming(acc.ID, it.ChatNativeID, it.SentAt+1)
		}
	}
	e.clock.Advance(time.Hour + time.Minute)
	e.tick(acc.ID)
	if held := e.queue(cp.ID, "held"); len(held) != 0 {
		t.Fatalf("debían liberarse los retenidos: %d", len(held))
	}
	rest := e.queue(cp.ID, "pending")
	sentCount := len(e.queue(cp.ID, "sent"))
	if len(rest)+sentCount != 9 {
		t.Fatalf("pendientes %d + enviados %d", len(rest), sentCount)
	}
	perVariant := map[int]int{}
	for _, it := range e.queue(cp.ID) {
		if it.ABSample {
			continue
		}
		if !winnersWant[it.Variant] {
			t.Errorf("salió una variante perdedora: %+v", it)
		}
		perVariant[it.Variant]++
		letter := strings.Repeat(campaign.VariantLetters[it.Variant], 3)
		if !strings.HasPrefix(it.Text, letter+" ") {
			t.Errorf("el texto no se repersonalizó (variante %d): %q", it.Variant, it.Text)
		}
	}
	if perVariant[1] != 3 || perVariant[2] != 3 {
		t.Errorf("reparto entre ganadoras: %v", perVariant)
	}
	e.run(acc.ID, 2*time.Second, 30)
	if got := e.get(cp.ID); got.State != model.CampaignDone || got.Totals.Sent != 9 {
		t.Fatalf("final: %s %+v", got.State, got.Totals)
	}
}

func TestEngineABTestSkipsOptedOutHeldContacts(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 6; i++ {
		cs = append(cs, e.contact(acc.ID, fmt.Sprintf("P%d", i)))
	}
	cp := e.save(acc.ID, func(c *model.Campaign) {
		c.ABEnabled = true
		c.Text = "AAA {{nombre}} hola uno"
		c.Variants = []string{"BBB {{nombre}} buenas dos"}
	}, cs...)
	e.launch(cp.ID)
	held := e.queue(cp.ID, "held")
	if len(held) == 0 {
		t.Fatal("debía haber retenidos")
	}
	victim, _ := e.testEnv.app.GetContact(ctx, held[0].ContactID)
	victim.OptedOut = true
	_ = e.testEnv.app.SaveContact(ctx, victim)
	for i := 0; i < 4; i++ {
		e.tick(acc.ID)
		e.clock.Advance(2 * time.Second)
	}
	e.clock.Advance(5 * time.Hour)
	e.run(acc.ID, 2*time.Second, 30)
	for _, it := range e.queue(cp.ID, "sent") {
		if it.ContactID == victim.ID {
			t.Fatal("no debía enviarse a quien se dio de baja")
		}
	}
	if got := e.get(cp.ID); got.State != model.CampaignDone {
		t.Fatalf("estado: %s", got.State)
	}
}

// ---------------------------------------------------------------------------------------------
// Dos campañas en la misma cuenta, progreso, arranque y parada

func TestEngineSendsOneMessageAtATimePerAccount(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	a1, a2 := e.warm(acc.ID, "Ana"), e.warm(acc.ID, "Beto")
	cp1 := e.save(acc.ID, func(c *model.Campaign) { c.Name = "uno" }, a1)
	e.clock.Advance(time.Second)
	cp2 := e.save(acc.ID, func(c *model.Campaign) { c.Name = "dos" }, a2)
	e.launch(cp1.ID)
	e.launch(cp2.ID)
	e.tick(acc.ID)
	if n := len(e.sentTexts(acc.ID)); n != 1 {
		t.Fatalf("un envío por tick y cuenta: %d", n)
	}
	e.clock.Advance(2 * time.Second)
	e.tick(acc.ID)
	if got := e.sentTexts(acc.ID); len(got) != 2 || !strings.Contains(got[0], "Ana") || !strings.Contains(got[1], "Beto") {
		t.Fatalf("orden entre campañas: %q", got)
	}
}

func TestEngineProgressChannelDropsWhenFull(t *testing.T) {
	e := newCampEnv(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			e.c.emit(model.Campaign{ID: fmt.Sprintf("c%d", i), State: model.CampaignRunning}, "", 0)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("emit se bloqueó con el búfer lleno")
	}
	if n := len(e.progress()); n != 256 {
		t.Fatalf("el búfer es de 256, había %d", n)
	}
	// Un aviso igual al anterior no se repite.
	cp := model.Campaign{ID: "igual", State: model.CampaignRunning}
	e.c.emit(cp, "window", 5)
	e.c.emit(cp, "window", 5)
	e.c.emit(cp, "window", 6)
	if n := len(e.progress()); n != 2 {
		t.Fatalf("avisos: %d", n)
	}
}

func TestEngineStartStopRunsToCompletion(t *testing.T) {
	e := newCampEnv(t)
	acc := e.newAccount(model.ChannelWhatsApp, "")
	var cs []model.Contact
	for i := 0; i < 4; i++ {
		cs = append(cs, e.warm(acc.ID, fmt.Sprintf("P%d", i)))
	}
	cp := e.save(acc.ID, nil, cs...)
	e.c.Start(context.Background())
	e.c.Start(context.Background()) // la segunda vez no hace nada
	e.launch(cp.ID)

	// El reloj de prueba avanza solo mientras el motor trabaja.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
				e.clock.Advance(time.Minute)
			}
		}
	}()
	defer close(stop)
	eventually(t, "campaña terminada", func() bool { return e.get(cp.ID).State == model.CampaignDone })
	if got := e.get(cp.ID); got.Totals.Sent != 4 {
		t.Fatalf("totales: %+v", got.Totals)
	}
	e.c.Stop()
	e.c.Stop() // parar dos veces tampoco hace nada
	// Se puede volver a arrancar.
	e.c.Start(context.Background())
	e.c.Stop()
}

func TestEnginePausedCampaignDoesNotSend(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	ana := e.warm(acc.ID, "Ana")
	cp := e.save(acc.ID, nil, ana)
	e.launch(cp.ID)
	if err := e.c.Pause(ctx, cp.ID); err != nil {
		t.Fatal(err)
	}
	if e.tick(acc.ID) {
		t.Fatal("pausada no es «en marcha»")
	}
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("pausada no envía: %d", n)
	}
	// Con el motor arrancado tampoco.
	e.c.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	e.c.Stop()
	if n := len(e.sentTexts(acc.ID)); n != 0 {
		t.Fatalf("pausada no envía: %d", n)
	}
}
