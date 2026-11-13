package service

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"dconnector/internal/core/campaign"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Motor de campañas (06 §8): una goroutine por cuenta con campañas en marcha; en cada tick
// (5 s) comprueba ventana, conexión, tandas y límites y, si todo está libre, envía UN mensaje
// de la cola. El reloj (`now`), el tick, la pausa de «escribiendo…» y el azar son campos de
// Campaigns para poder probarlo sin esperas.

// sendState: lo que recuerda el motor de cada cuenta entre un envío y el siguiente.
type sendState struct {
	nextSendAt time.Time // no enviar antes (espera aleatoria entre envíos)
	restUntil  time.Time // descanso de tanda
	batchCount int       // envíos seguidos en la tanda actual
}

// sentRef: un envío ya hecho de una campaña, con lo que se sabe de su respuesta.
type sentRef struct {
	itemID, contactID, chat string
	variant                 int
	abSample                bool
	sentAt                  int64
	msgKey                  model.MessageKey
	replied, optedOut       bool
}

// brakeBase: contadores al reanudar una campaña frenada; el freno solo mira lo posterior.
type brakeBase struct {
	sent, failed, replied, optOut int
	at                            int64
}

// campStats: respuestas y bajas de una campaña. No se guardan en la base (el almacén solo
// calcula enviados, fallidos y saltados): se rehacen una vez por arranque a partir de los
// mensajes y los contactos, y luego los mantienen OnIncoming y OnOptOut.
type campStats struct {
	acc               model.AccountID
	refs              map[string]*sentRef // por id de item
	byChat            map[string]*sentRef
	byContact         map[string]*sentRef
	replied, optedOut int
	firstSent         int64
	base              *brakeBase
}

// ---------------------------------------------------------------------------------------------
// Arranque y parada

// Start arranca el motor: un supervisor que cada tick promueve las campañas ready cuya hora
// llegó y arranca una goroutine por cuenta con campañas running. Llamarlo dos veces no hace nada.
func (c *Campaigns) Start(ctx context.Context) {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = make(chan struct{})
	go c.supervise(runCtx, c.done)
}

// Stop para el motor y espera a las goroutines de las cuentas.
func (c *Campaigns) Stop() {
	c.runMu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel, c.done = nil, nil
	c.runMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	c.wg.Wait()
}

func (c *Campaigns) supervise(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		c.safeSync(ctx)
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-time.After(c.tick):
		}
	}
}

func (c *Campaigns) safeSync(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("pánico en el supervisor de campañas", "panic", r)
		}
	}()
	c.sync(ctx)
}

// sync promueve las ready y arranca los trabajadores que falten.
func (c *Campaigns) sync(ctx context.Context) {
	cs, err := c.app.ListCampaigns(ctx)
	if err != nil {
		slog.Warn("campañas: no se pudieron leer", "err", err)
		return
	}
	now := c.nowMs()
	accs := map[model.AccountID]bool{}
	for _, cp := range cs {
		switch {
		case cp.State == model.CampaignReady && cp.StartAt <= now:
			if _, err := c.transition(ctx, cp.ID, []model.CampaignState{model.CampaignReady}, model.CampaignRunning, "iniciar", ""); err == nil {
				accs[cp.AccountID] = true
			}
		case cp.State == model.CampaignRunning:
			accs[cp.AccountID] = true
		}
	}
	for acc := range accs {
		c.wmu.Lock()
		if c.workers[acc] || ctx.Err() != nil {
			c.wmu.Unlock()
			continue
		}
		c.workers[acc] = true
		c.wg.Add(1)
		c.wmu.Unlock()
		go c.worker(ctx, acc)
	}
}

func (c *Campaigns) worker(ctx context.Context, acc model.AccountID) {
	defer c.wg.Done()
	defer func() {
		c.wmu.Lock()
		delete(c.workers, acc)
		c.wmu.Unlock()
	}()
	for {
		if !c.safeProcess(ctx, acc) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.tick):
		}
	}
}

func (c *Campaigns) safeProcess(ctx context.Context, acc model.AccountID) (active bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("pánico en el motor de campañas", "accountId", string(acc), "panic", r)
			active = true
		}
	}()
	return c.processAccount(ctx, acc)
}

func (c *Campaigns) sendStateFor(acc model.AccountID) *sendState {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	st := c.wstate[acc]
	if st == nil {
		st = &sendState{}
		c.wstate[acc] = st
	}
	return st
}

// ---------------------------------------------------------------------------------------------
// Un tick de una cuenta

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func msOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// processAccount hace lo que toca en un tick para las campañas running de la cuenta y dice si
// todavía hay alguna (si no, el trabajador termina). Envía como mucho un mensaje por tick.
func (c *Campaigns) processAccount(ctx context.Context, accID model.AccountID) bool {
	all, err := c.app.ListCampaigns(ctx)
	if err != nil {
		slog.Warn("campañas: no se pudieron leer", "accountId", string(accID), "err", err)
		return true
	}
	var running []model.Campaign
	for _, cp := range all {
		if cp.AccountID == accID && cp.State == model.CampaignRunning {
			running = append(running, cp)
		}
	}
	if len(running) == 0 {
		return false
	}
	sort.Slice(running, func(i, j int) bool { return running[i].CreatedAt < running[j].CreatedAt })

	type liveCampaign struct {
		cp     model.Campaign
		abWait time.Time
	}
	var live []liveCampaign
	for _, cp := range running {
		if c.finishIfEmpty(ctx, cp) {
			continue
		}
		live = append(live, liveCampaign{cp, c.releaseAB(ctx, cp)})
	}
	if len(live) == 0 {
		return false
	}

	now := c.now()
	local := now.In(c.loc)
	connected := c.acc.Status(accID).State == model.StateConnected
	ws := c.sendStateFor(accID)
	dayStart := startOfDay(local)
	nextMidnight := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, local.Location())
	hourly, daily := -1, -1

	sent := false
	for _, lc := range live {
		cp := lc.cp
		if sent {
			continue // ya se envió uno en este tick: las demás esperan al siguiente
		}
		p := c.effectivePacing(ctx, cp)
		if !campaign.InWindow(cp.Window, local) {
			c.report(ctx, cp, "window", campaign.NextWindowStart(cp.Window, local))
			continue
		}
		if !connected {
			c.report(ctx, cp, "disconnected", time.Time{})
			continue
		}
		if now.Before(ws.restUntil) {
			c.report(ctx, cp, "batch_rest", ws.restUntil)
			continue
		}
		if p.HourlyLimit > 0 {
			if hourly < 0 {
				hourly, _ = c.app.CountSentSince(ctx, accID, now.Add(-time.Hour).UnixMilli())
			}
			if hourly >= p.HourlyLimit {
				c.report(ctx, cp, "hourly", time.Time{})
				continue
			}
		}
		if limit := campaign.WarmupLimit(p.DailyLimit, p, c.warmupStart(ctx, accID), now); limit > 0 {
			if daily < 0 {
				daily, _ = c.app.CountSentSince(ctx, accID, dayStart.UnixMilli())
			}
			if daily >= limit {
				c.report(ctx, cp, "daily", nextMidnight)
				continue
			}
		}
		if now.Before(ws.nextSendAt) {
			c.report(ctx, cp, "", ws.nextSendAt)
			continue
		}
		it, nextRisk, coldBlocked, ok := c.nextItem(ctx, cp, p, now, dayStart)
		if !ok {
			switch {
			case coldBlocked:
				c.report(ctx, cp, "daily", nextMidnight)
			default:
				c.report(ctx, cp, "", lc.abWait)
			}
			continue
		}
		c.sendItem(ctx, cp, p, it, nextRisk, ws)
		sent = true
	}
	return true
}

// finishIfEmpty pasa la campaña a done si no queda nada pendiente ni retenido.
func (c *Campaigns) finishIfEmpty(ctx context.Context, cp model.Campaign) bool {
	left, err := c.app.ListQueue(ctx, cp.ID, []string{"pending", "held"}, 1)
	if err != nil || len(left) > 0 {
		return false
	}
	_, err = c.transition(ctx, cp.ID, []model.CampaignState{model.CampaignRunning}, model.CampaignDone, "terminar", "")
	return err == nil
}

// markSkipped deja un item de la cola como saltado, con el motivo.
func (c *Campaigns) markSkipped(ctx context.Context, it model.QueueItem, reason string) {
	it.State, it.Error = "skipped", reason
	if err := c.app.UpdateQueueItem(ctx, it); err != nil {
		slog.Warn("campañas: no se pudo marcar un item como saltado", "campaignId", it.CampaignID, "err", err)
	}
}

// coldSentSince cuenta los envíos a contactos sin conversación que la cuenta hizo desde `since`
// (en todas sus campañas).
func (c *Campaigns) coldSentSince(ctx context.Context, acc model.AccountID, since int64) int {
	cs, err := c.app.ListCampaigns(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, cp := range cs {
		if cp.AccountID != acc {
			continue
		}
		items, err := c.app.ListQueue(ctx, cp.ID, []string{"sent"}, 0)
		if err != nil {
			continue
		}
		for _, it := range items {
			if it.Risk == string(campaign.RiskCold) && it.SentAt >= since {
				n++
			}
		}
	}
	return n
}

// nextItem elige el siguiente item pendiente que se puede enviar ya: ya le toca (SendAfter), no
// es un contacto que se dio de baja (esos se saltan aquí) y, si es frío, no se pasó el límite
// diario de fríos. Devuelve también el riesgo del que sigue (para elegir la pausa).
func (c *Campaigns) nextItem(ctx context.Context, cp model.Campaign, p model.PacingSettings, now, dayStart time.Time) (it model.QueueItem, nextRisk campaign.Risk, coldBlocked, ok bool) {
	items, err := c.app.ListQueue(ctx, cp.ID, []string{"pending"}, 200)
	if err != nil {
		return it, "", false, false
	}
	nowMs := now.UnixMilli()
	cold := -1
	for i, cand := range items {
		if cand.SendAfter > nowMs {
			continue
		}
		if campaign.Risk(cand.Risk) == campaign.RiskCold && p.ColdDailyLimit > 0 {
			if cold < 0 {
				cold = c.coldSentSince(ctx, cp.AccountID, dayStart.UnixMilli())
			}
			if cold >= p.ColdDailyLimit {
				coldBlocked = true
				continue
			}
		}
		ct, err := c.app.GetContact(ctx, cand.ContactID)
		switch {
		case errors.Is(err, port.ErrNotFound):
			c.markSkipped(ctx, cand, "El contacto ya no existe")
			continue
		case err != nil:
			return it, "", false, false
		case ct.OptedOut:
			c.markSkipped(ctx, cand, "Pidió no recibir más mensajes")
			continue
		}
		if i+1 < len(items) {
			nextRisk = campaign.Risk(items[i+1].Risk)
		}
		return cand, nextRisk, false, true
	}
	return it, "", coldBlocked, false
}

var knownMime = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
	".mp4": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm", ".mkv": "video/x-matroska",
	".pdf": "application/pdf",
}

// outgoing arma lo que se envía: solo texto, o el adjunto de la campaña (imagen, video o
// documento según su MIME) con el texto como pie.
func outgoingFor(cp model.Campaign, it model.QueueItem) model.Outgoing {
	if cp.FilePath == "" {
		return model.Outgoing{Kind: model.KindText, Text: it.Text, ClientRef: it.ID}
	}
	ext := strings.ToLower(filepath.Ext(cp.FilePath))
	mt := knownMime[ext]
	if mt == "" {
		mt = mime.TypeByExtension(ext)
	}
	kind := model.KindDocument
	switch {
	case strings.HasPrefix(mt, "image/"):
		kind = model.KindImage
	case strings.HasPrefix(mt, "video/"):
		kind = model.KindVideo
	}
	return model.Outgoing{Kind: kind, Text: it.Text, FilePath: cp.FilePath, FileName: filepath.Base(cp.FilePath), Mime: mt, ClientRef: it.ID}
}

// pauseBetween: espera aleatoria entre MinDelay y MaxDelay (o las de fríos).
func (c *Campaigns) pauseBetween(p model.PacingSettings, cold bool) time.Duration {
	lo, hi := campaign.NormalizeDelayRange(p.MinDelaySec, p.MaxDelaySec)
	if cold {
		lo, hi = campaign.NormalizeDelayRange(p.ColdMinDelaySec, p.ColdMaxDelaySec)
	}
	sec := float64(lo) + c.randf()*float64(hi-lo)
	return time.Duration(sec * float64(time.Second))
}

// sendItem envía un item: «escribiendo…», envío, estado del item, espera hasta el siguiente,
// descanso de tanda, calentamiento y chequeo de salud.
func (c *Campaigns) sendItem(ctx context.Context, cp model.Campaign, p model.PacingSettings, it model.QueueItem, nextRisk campaign.Risk, ws *sendState) {
	key := model.NewChatKey(cp.AccountID, it.ChatNativeID)
	if d := time.Duration(campaign.TypingMs(utf8.RuneCountInString(it.Text), p.TypingMsPerChar)) * time.Millisecond; d > 0 {
		_ = c.msg.SetTyping(ctx, key, true) // sin capacidad o con fallo: se envía igual
		c.sleep(ctx, d)
	}
	if ctx.Err() != nil {
		return
	}
	// Mientras «escribía» pudieron pausarla o cancelarla.
	if cur, err := c.app.GetCampaign(ctx, cp.ID); err != nil || cur.State != model.CampaignRunning {
		_ = c.msg.SetTyping(ctx, key, false)
		return
	}
	msg, err := c.msg.Send(ctx, key, outgoingFor(cp, it))
	now := c.now()
	switch {
	case errors.Is(err, port.ErrNotConnected):
		c.report(ctx, cp, "disconnected", time.Time{}) // sigue pendiente: se reintenta cuando conecte
		return
	case errors.Is(err, port.ErrRateLimited):
		// El canal pide ir más despacio: sigue pendiente y se descansa como en una tanda.
		ws.restUntil = now.Add(max(time.Duration(campaign.BatchRestMs(float64(p.BatchRestMinutes), c.randf))*time.Millisecond, time.Minute))
		c.report(ctx, cp, "batch_rest", ws.restUntil)
		return
	case err != nil:
		it.State, it.Error = "failed", err.Error()
		slog.Warn("campañas: falló un envío", "campaignId", cp.ID, "accountId", string(cp.AccountID), "err", err)
	default:
		it.State, it.Error, it.SentAt, it.MessageKey = "sent", "", now.UnixMilli(), msg.Key
		c.markWarmupStart(ctx, cp.AccountID, now)
	}
	if err := c.app.UpdateQueueItem(ctx, it); err != nil {
		slog.Error("campañas: no se pudo guardar el resultado de un envío", "campaignId", cp.ID, "err", err)
	}
	if it.State == "sent" {
		c.recordSent(ctx, cp, it)
	}

	ws.nextSendAt = now.Add(c.pauseBetween(p, nextRisk == campaign.RiskCold))
	ws.batchCount++
	if p.BatchSize > 0 && ws.batchCount >= p.BatchSize {
		ws.restUntil = now.Add(time.Duration(campaign.BatchRestMs(float64(p.BatchRestMinutes), c.randf)) * time.Millisecond)
		ws.batchCount = 0
	}

	if c.checkBrake(ctx, cp.ID) {
		return
	}
	next := ws.nextSendAt
	waiting := ""
	if now.Before(ws.restUntil) {
		waiting, next = "batch_rest", ws.restUntil
	}
	c.report(ctx, cp, waiting, next)
}

// ---------------------------------------------------------------------------------------------
// Freno automático

// health reúne lo que mira el freno: enviados, fallidos, respuestas y bajas (desde la última vez
// que se reanudó) y, solo cuando hace falta, cuántos lo leyeron.
func (c *Campaigns) health(ctx context.Context, cp model.Campaign, p model.PacingSettings) campaign.CampaignHealth {
	st := c.statsFor(ctx, cp)
	nowMs := c.nowMs()
	c.smu.Lock()
	h := campaign.CampaignHealth{Sent: cp.Totals.Sent, Failed: cp.Totals.Failed, Responded: st.replied, OptOuts: st.optedOut}
	from, baseAt := st.firstSent, int64(0)
	if b := st.base; b != nil {
		h.Sent, h.Failed, h.Responded, h.OptOuts = h.Sent-b.sent, h.Failed-b.failed, h.Responded-b.replied, h.OptOuts-b.optOut
		baseAt = b.at
		from = max(from, b.at)
	}
	var refs []*sentRef
	if from > 0 {
		h.MinutesSinceFirst = int((nowMs - from) / 60000)
	}
	needRead := p.BrakeEnabled && h.Sent >= p.BrakeMinSent && h.MinutesSinceFirst >= 120
	if needRead {
		for _, r := range st.refs {
			if r.sentAt >= baseAt {
				rc := *r
				refs = append(refs, &rc)
			}
		}
	}
	c.smu.Unlock()
	for _, r := range refs {
		switch c.deliveryOf(ctx, cp.AccountID, r.msgKey) {
		case model.DeliveryRead, model.DeliveryPlayed:
			h.Read++
			h.Delivered++
		case model.DeliveryDelivered:
			h.Delivered++
		}
	}
	return h
}

func (c *Campaigns) deliveryOf(ctx context.Context, acc model.AccountID, key model.MessageKey) model.DeliveryState {
	_, chat, native := key.Split()
	store, err := c.acc.Store(acc)
	if err != nil || chat == "" {
		return ""
	}
	m, err := store.GetMessage(ctx, chat, native)
	if err != nil {
		return ""
	}
	return m.Delivery
}

// checkBrake evalúa la salud de la campaña y, si hay motivo, la pasa a braked. Dice si frenó.
func (c *Campaigns) checkBrake(ctx context.Context, id string) bool {
	cp, err := c.app.GetCampaign(ctx, id)
	if err != nil || cp.State != model.CampaignRunning {
		return false
	}
	c.decorate(ctx, &cp)
	p := c.effectivePacing(ctx, cp)
	reason := campaign.BrakeReason(c.health(ctx, cp, p), p)
	if reason == "" {
		return false
	}
	if _, err := c.transition(ctx, id, []model.CampaignState{model.CampaignRunning}, model.CampaignBraked, "frenar", reason); err != nil {
		return false
	}
	slog.Warn("campaña frenada automáticamente", "campaignId", id, "accountId", string(cp.AccountID))
	return true
}

// resetBrakeBase: al reanudar una campaña, el freno solo mira lo que pase desde ahora.
func (c *Campaigns) resetBrakeBase(ctx context.Context, cp model.Campaign) {
	if cp.Totals.Sent == 0 && cp.Totals.Failed == 0 {
		return
	}
	st := c.statsFor(ctx, cp)
	c.smu.Lock()
	st.base = &brakeBase{sent: cp.Totals.Sent, failed: cp.Totals.Failed, replied: st.replied, optOut: st.optedOut, at: c.nowMs()}
	c.smu.Unlock()
}

// ---------------------------------------------------------------------------------------------
// «Prueba y gana» (A/B)

// releaseAB: cuando la muestra ya se envió y pasaron WaitHours desde su último envío, elige las
// variantes ganadoras, repersonaliza lo retenido (held) con ellas y lo pasa a pending. Devuelve
// cuándo se hará (cero si no hay nada que esperar).
func (c *Campaigns) releaseAB(ctx context.Context, cp model.Campaign) time.Time {
	if !cp.ABEnabled {
		return time.Time{}
	}
	if held, err := c.app.ListQueue(ctx, cp.ID, []string{"held"}, 1); err != nil || len(held) == 0 {
		return time.Time{}
	}
	items, err := c.app.ListQueue(ctx, cp.ID, nil, 0)
	if err != nil {
		return time.Time{}
	}
	var sample, held []model.QueueItem
	var lastSent int64
	for _, it := range items {
		switch {
		case it.State == "held":
			held = append(held, it)
		case it.ABSample:
			if it.State == "pending" {
				return time.Time{} // la muestra aún no terminó de enviarse
			}
			sample = append(sample, it)
			if it.State == "sent" {
				lastSent = max(lastSent, it.SentAt)
			}
		}
	}
	cfg := campaign.NormalizeABConfig(campaign.ABDefaults)
	now := c.now()
	if lastSent > 0 {
		if at := time.UnixMilli(lastSent).Add(time.Duration(cfg.WaitHours) * time.Hour); now.Before(at) {
			return at
		}
	}

	texts := campaignTexts(cp)
	if len(texts) == 0 {
		texts = []string{""}
	}
	st := c.statsFor(ctx, cp)
	perVariant := map[int]*campaign.VariantStat{}
	for _, it := range sample {
		if it.State != "sent" {
			continue
		}
		vs := perVariant[it.Variant]
		if vs == nil {
			vs = &campaign.VariantStat{Variant: it.Variant}
			perVariant[it.Variant] = vs
		}
		vs.Sent++
		c.smu.Lock()
		ref := st.refs[it.ID]
		var replied, optedOut bool
		if ref != nil {
			replied, optedOut = ref.replied, ref.optedOut
		}
		c.smu.Unlock()
		if replied {
			vs.Responded++
		}
		if optedOut {
			vs.OptOuts++
		}
		switch c.deliveryOf(ctx, cp.AccountID, it.MessageKey) {
		case model.DeliveryRead, model.DeliveryPlayed:
			vs.Read++
		}
	}
	stats := make([]campaign.VariantStat, 0, len(perVariant))
	for _, vs := range perVariant {
		stats = append(stats, *vs)
	}
	winners := campaign.PickWinners(stats, min(cfg.Winners, len(texts)))
	if len(winners) == 0 {
		winners = []int{0}
	}

	ids := make([]string, len(held))
	for i, it := range held {
		ids[i] = it.ID
	}
	footer := ""
	if cp.OptOutFooter {
		footer = c.footerText(ctx)
	}
	for _, a := range campaign.AssignVariants(ids, len(winners), cp.ID+"|ab") {
		var it model.QueueItem
		for _, h := range held {
			if h.ID == a.ID {
				it = h
				break
			}
		}
		v := winners[a.Variant]
		if v >= len(texts) {
			v = 0
		}
		it.Variant, it.ABSample, it.State = v, false, "pending"
		ct, err := c.app.GetContact(ctx, it.ContactID)
		switch {
		case err == nil && ct.OptedOut:
			it.State, it.Error = "skipped", "Pidió no recibir más mensajes"
		case err == nil:
			it.Text = personalize(cp.ID, texts[v], ct, footer)
		}
		if err := c.app.UpdateQueueItem(ctx, it); err != nil {
			slog.Warn("campañas: no se pudo liberar un item retenido", "campaignId", cp.ID, "err", err)
		}
	}
	return time.Time{}
}

// ---------------------------------------------------------------------------------------------
// Respuestas y bajas (observador de entrantes)

func (c *Campaigns) dropStats(id string) {
	c.smu.Lock()
	delete(c.stats, id)
	c.smu.Unlock()
}

// loadStats rehace las respuestas y bajas de una campaña a partir de su cola, los mensajes del
// chat y los contactos.
func (c *Campaigns) loadStats(ctx context.Context, cp model.Campaign) *campStats {
	st := &campStats{
		acc: cp.AccountID, refs: map[string]*sentRef{}, byChat: map[string]*sentRef{}, byContact: map[string]*sentRef{},
	}
	items, err := c.app.ListQueue(ctx, cp.ID, []string{"sent"}, 0)
	if err != nil {
		return st
	}
	store, _ := c.acc.Store(cp.AccountID)
	for _, it := range items {
		ref := &sentRef{
			itemID: it.ID, contactID: it.ContactID, chat: it.ChatNativeID, variant: it.Variant,
			abSample: it.ABSample, sentAt: it.SentAt, msgKey: it.MessageKey,
		}
		if store != nil {
			if page, err := store.ListMessages(ctx, it.ChatNativeID, 0, maxRiskMessages); err == nil {
				for _, m := range page.Messages {
					if !m.FromMe && m.Timestamp >= it.SentAt {
						ref.replied = true
						break
					}
				}
			}
		}
		if ct, err := c.app.GetContact(ctx, it.ContactID); err == nil && ct.OptedOut {
			ref.optedOut = true
		}
		st.add(ref)
	}
	return st
}

// add registra un envío (y sus contadores); si ya estaba no hace nada.
func (st *campStats) add(ref *sentRef) {
	if _, ok := st.refs[ref.itemID]; ok {
		return
	}
	st.refs[ref.itemID] = ref
	st.byChat[ref.chat] = ref
	st.byContact[ref.contactID] = ref
	if ref.replied {
		st.replied++
	}
	if ref.optedOut {
		st.optedOut++
	}
	if st.firstSent == 0 || ref.sentAt < st.firstSent {
		st.firstSent = ref.sentAt
	}
}

func (c *Campaigns) statsFor(ctx context.Context, cp model.Campaign) *campStats {
	c.smu.Lock()
	st := c.stats[cp.ID]
	c.smu.Unlock()
	if st != nil {
		return st
	}
	loaded := c.loadStats(ctx, cp)
	c.smu.Lock()
	defer c.smu.Unlock()
	if cur := c.stats[cp.ID]; cur != nil {
		return cur
	}
	c.stats[cp.ID] = loaded
	return loaded
}

// recordSent anota un envío recién hecho.
func (c *Campaigns) recordSent(ctx context.Context, cp model.Campaign, it model.QueueItem) {
	st := c.statsFor(ctx, cp)
	c.smu.Lock()
	st.add(&sentRef{
		itemID: it.ID, contactID: it.ContactID, chat: it.ChatNativeID, variant: it.Variant,
		abSample: it.ABSample, sentAt: it.SentAt, msgKey: it.MessageKey,
	})
	c.smu.Unlock()
}

// decorate rellena Replied y OptOut de los totales (el almacén no los guarda).
func (c *Campaigns) decorate(ctx context.Context, cp *model.Campaign) {
	if cp.Totals.Sent == 0 {
		return
	}
	st := c.statsFor(ctx, *cp)
	c.smu.Lock()
	cp.Totals.Replied, cp.Totals.OptOut = st.replied, st.optedOut
	c.smu.Unlock()
}

// observed: campañas de la cuenta que ya enviaron algo y cuyas respuestas aún cuentan.
func (c *Campaigns) observed(ctx context.Context, acc model.AccountID) []model.Campaign {
	cs, err := c.app.ListCampaigns(ctx)
	if err != nil {
		return nil
	}
	var out []model.Campaign
	for _, cp := range cs {
		if cp.AccountID != acc || cp.Totals.Sent == 0 {
			continue
		}
		switch cp.State {
		case model.CampaignRunning, model.CampaignPaused, model.CampaignBraked, model.CampaignDone:
			out = append(out, cp)
		}
	}
	return out
}

// OnIncoming: llegó un mensaje de un chat directo. Si es de alguien a quien esta campaña le
// envió algo y es su primera respuesta, cuenta como respondido.
func (c *Campaigns) OnIncoming(acc model.AccountID, chatNativeID string, ts int64) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("pánico en Campaigns.OnIncoming", "panic", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, cp := range c.observed(ctx, acc) {
		st := c.statsFor(ctx, cp)
		c.smu.Lock()
		ref := st.byChat[chatNativeID]
		changed := ref != nil && !ref.replied && ts >= ref.sentAt
		if changed {
			ref.replied = true
			st.replied++
		}
		c.smu.Unlock()
		if changed {
			c.decorate(ctx, &cp)
			c.emit(cp, "", 0)
		}
	}
}

// OnOptOut: un contacto pidió la baja. Cuenta en las campañas que le enviaron algo, y puede
// frenar una campaña en marcha (varias bajas seguidas).
func (c *Campaigns) OnOptOut(acc model.AccountID, contactID string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("pánico en Campaigns.OnOptOut", "panic", r)
		}
	}()
	if contactID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, cp := range c.observed(ctx, acc) {
		st := c.statsFor(ctx, cp)
		c.smu.Lock()
		ref := st.byContact[contactID]
		changed := ref != nil && !ref.optedOut
		if changed {
			ref.optedOut = true
			st.optedOut++
		}
		c.smu.Unlock()
		if !changed {
			continue
		}
		c.decorate(ctx, &cp)
		c.emit(cp, "", 0)
		if cp.State == model.CampaignRunning {
			c.checkBrake(ctx, cp.ID)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Avisos de progreso

// emit avisa de un cambio de la campaña por el canal propio (no por el bus). Solo avisa si algo
// cambió respecto al último aviso, y descarta si el búfer está lleno.
func (c *Campaigns) emit(cp model.Campaign, waiting string, nextAt int64) {
	p := CampaignProgress{
		CampaignID: cp.ID, State: cp.State, Totals: cp.Totals, NextAt: nextAt, Waiting: waiting, BrakeReason: cp.BrakeReason,
	}
	c.pmu.Lock()
	if last, ok := c.lastProg[cp.ID]; ok && last == p {
		c.pmu.Unlock()
		return
	}
	c.lastProg[cp.ID] = p
	c.pmu.Unlock()
	select {
	case c.progress <- p:
	default:
	}
}

// report avisa del estado actual de una campaña en marcha y por qué espera.
func (c *Campaigns) report(ctx context.Context, cp model.Campaign, waiting string, next time.Time) {
	if cur, err := c.app.GetCampaign(ctx, cp.ID); err == nil {
		cp = cur
	}
	c.decorate(ctx, &cp)
	c.emit(cp, waiting, msOrZero(next))
}
