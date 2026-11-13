package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

const (
	schedulerTick = 15 * time.Second
	// schedulerGiveUp: si pasan 6 h del SendAt sin poder enviar por falta de conexión, falla.
	schedulerGiveUp = 6 * time.Hour
	// msgSchedNotConnected: motivo de fallo cuando se agota la espera.
	msgSchedNotConnected = "La cuenta no estuvo conectada"

	// schedMissedGrace: una ocurrencia de una serie con más atraso que esto se omite en vez de enviarse.
	schedMissedGrace = time.Hour
	// Motivos de una ocurrencia omitida (state "canceled").
	msgSchedMissedClosed  = "Omitido: la app estaba cerrada"
	msgSchedMissedOffline = "Omitido: la cuenta no estaba conectada"
	// schedChatGap: pausa mínima entre dos envíos programados al mismo chat.
	schedChatGap = 1500 * time.Millisecond
	// schedRatePerMinute: tope de envíos programados por minuto en cada cuenta (el resto espera su turno).
	schedRatePerMinute = 20
	// ListForChat con includeDone: enviados, fallidos u omitidos de los últimos 7 días (hasta 50).
	schedRecentWindow = 7 * 24 * time.Hour
	schedRecentMax    = 50
)

// errSchedBusy: la cuenta ya envió el tope del minuto; el programado espera al siguiente tick.
var errSchedBusy = errors.New("programados: tope de envíos por minuto")

// scheduledSeriesStore: métodos opcionales del almacén de programados (aserción de tipo; el de
// SQLite los implementa). Sin ellos el servicio funciona igual con port.AppStore, solo que sin
// la transacción que une "cerrar esta ocurrencia" con "crear la siguiente".
type scheduledSeriesStore interface {
	GetScheduled(ctx context.Context, id string) (model.Scheduled, error) // ErrNotFound
	ListScheduledByChat(ctx context.Context, accountID model.AccountID, chatNativeID string, states []string, since int64) ([]model.Scheduled, error)
	ListScheduledBySeries(ctx context.Context, seriesID string, states []string) ([]model.Scheduled, error)
	// SaveScheduledWithNext guarda cur y, en la misma transacción, next (si no es nil y la serie
	// no tiene ya otra pendiente).
	SaveScheduledWithNext(ctx context.Context, cur model.Scheduled, next *model.Scheduled) error
}

// Scheduler: mensajes y estados programados (06 §9). Cada tick (15 s) envía lo que ya tocó; lo
// que no se puede enviar porque la cuenta no está conectada se reintenta en el siguiente tick
// hasta que pasen 6 h (1 h en las series: pasado eso la ocurrencia se omite y sigue la serie).
type Scheduler struct {
	app port.AppStore
	msg *Messaging
	st  *Statuses
	acc *Accounts

	// Ajustables en pruebas (se fijan antes de Start).
	now    func() time.Time
	tick   time.Duration
	giveUp time.Duration
	loc    *time.Location // zona de las repeticiones (time.Local)
	// sleep espera d (o hasta que ctx termine); lo usa la pausa entre envíos al mismo chat.
	sleep func(ctx context.Context, d time.Duration) error
	// bootAt: arranque de la app (ms). Lo que debía enviarse antes de eso es de cuando estaba cerrada.
	bootAt int64

	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	mu        sync.Mutex      // serializa leer-modificar-guardar un programado
	inFlight  map[string]bool // programados que se están enviando ahora
	rate      map[model.AccountID][]int64
	lastChat  map[string]time.Time
	unsettled map[string]model.Scheduled // enviados cuyo resultado no se pudo guardar
}

func NewScheduler(app port.AppStore, msg *Messaging, st *Statuses, acc *Accounts) *Scheduler {
	return &Scheduler{
		app: app, msg: msg, st: st, acc: acc,
		now: time.Now, tick: schedulerTick, giveUp: schedulerGiveUp, loc: time.Local,
		sleep:     schedSleep,
		bootAt:    time.Now().UnixMilli(),
		inFlight:  map[string]bool{},
		rate:      map[model.AccountID][]int64{},
		lastChat:  map[string]time.Time{},
		unsettled: map[string]model.Scheduled{},
	}
}

func schedSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Start arranca el bucle (tick cada 15 s). Llamarlo dos veces no hace nada.
func (s *Scheduler) Start(ctx context.Context) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	go s.loop(runCtx, s.done)
}

// Stop para el bucle y espera a que termine el tick en curso.
func (s *Scheduler) Stop() {
	s.runMu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.runMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (s *Scheduler) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		s.safeProcess(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.tick):
		}
	}
}

func (s *Scheduler) safeProcess(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("pánico en los programados", "panic", r)
		}
	}()
	s.processDue(ctx)
}

// processDue envía lo que ya tocó (un tick del bucle), por hora de envío y luego de creación.
func (s *Scheduler) processDue(ctx context.Context) {
	due, err := s.app.DueScheduled(ctx, s.now().UnixMilli())
	if err != nil {
		slog.Warn("programados: no se pudieron leer", "err", err)
		return
	}
	for _, it := range due {
		if ctx.Err() != nil {
			return
		}
		err := s.run(ctx, it, false)
		if err != nil && !errors.Is(err, port.ErrNotConnected) && !errors.Is(err, errSchedBusy) && ctx.Err() == nil {
			slog.Warn("programados: falló un envío", "id", it.ID, "err", err)
		}
	}
}

// run envía un programado. Si la cuenta no está conectada devuelve ErrNotConnected y lo deja
// pendiente (o failed si pasaron 6 h del SendAt y `force` es false). Cualquier otro error lo
// deja failed con su texto. Una ocurrencia de serie con más de 1 h de atraso no se envía: se
// omite. Al cerrarse una ocurrencia de serie (enviada, fallida u omitida) se crea la siguiente.
func (s *Scheduler) run(ctx context.Context, it model.Scheduled, force bool) error {
	s.mu.Lock()
	if s.inFlight[it.ID] {
		s.mu.Unlock()
		return nil
	}
	s.inFlight[it.ID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inFlight, it.ID)
		s.mu.Unlock()
	}()

	// Ya se envió pero no se pudo guardar el resultado: se reintenta guardar, no se reenvía.
	s.mu.Lock()
	pending, unsettled := s.unsettled[it.ID]
	s.mu.Unlock()
	if unsettled {
		if err := s.settle(ctx, pending); err != nil {
			return err
		}
		return nil
	}

	// Releer: pudo cancelarse, borrarse o enviarse mientras tanto.
	cur, err := s.get(ctx, it.ID)
	if err != nil {
		return err
	}
	if cur.State != "pending" && !(force && cur.State == "failed") {
		return nil
	}
	it = cur

	now := s.now()
	if it.Recurrence != nil && !force && now.UnixMilli()-it.SendAt > schedMissedGrace.Milliseconds() {
		return s.skipMissed(ctx, it)
	}
	if s.acc.Status(it.AccountID).State != model.StateConnected {
		return s.notConnected(ctx, it, now)
	}
	if !force {
		if err := s.admit(ctx, it, now); err != nil {
			return err
		}
	}
	sendErr := s.send(ctx, it)
	if errors.Is(sendErr, port.ErrNotConnected) {
		return s.notConnected(ctx, it, now)
	}
	if sendErr != nil {
		it.State, it.Error = "failed", sendErr.Error()
	} else {
		it.State, it.Error, it.SentAt = "sent", "", now.UnixMilli()
		s.recordSend(it)
	}
	if err := s.settle(ctx, it); err != nil {
		return err
	}
	return sendErr
}

// skipMissed omite una ocurrencia de serie que llegó demasiado tarde y sigue con la siguiente.
func (s *Scheduler) skipMissed(ctx context.Context, it model.Scheduled) error {
	it.State = "canceled"
	if it.SendAt < s.bootAt {
		it.Error = msgSchedMissedClosed
	} else {
		it.Error = msgSchedMissedOffline
	}
	return s.settle(ctx, it)
}

// notConnected: se reintenta en el siguiente tick, salvo que ya pasaron 6 h (entonces falla).
func (s *Scheduler) notConnected(ctx context.Context, it model.Scheduled, now time.Time) error {
	if now.Sub(time.UnixMilli(it.SendAt)) >= s.giveUp {
		it.State, it.Error = "failed", msgSchedNotConnected
		if err := s.settle(ctx, it); err != nil {
			return err
		}
	}
	return port.ErrNotConnected
}

// admit aplica el tope de envíos por minuto de la cuenta y la pausa entre envíos al mismo chat.
func (s *Scheduler) admit(ctx context.Context, it model.Scheduled, now time.Time) error {
	s.mu.Lock()
	recent := s.pruneRate(it.AccountID, now.UnixMilli())
	var wait time.Duration
	if len(recent) >= schedRatePerMinute {
		s.mu.Unlock()
		return errSchedBusy
	}
	if it.Kind == "message" {
		if last, ok := s.lastChat[chatGapKey(it)]; ok {
			wait = schedChatGap - now.Sub(last)
		}
	}
	s.mu.Unlock()
	if wait > 0 {
		return s.sleep(ctx, wait)
	}
	return nil
}

func chatGapKey(it model.Scheduled) string { return string(it.AccountID) + "\x00" + it.ChatNativeID }

// pruneRate deja en la ventana de la cuenta solo los envíos del último minuto (con s.mu tomado).
func (s *Scheduler) pruneRate(acc model.AccountID, nowMs int64) []int64 {
	cut := nowMs - time.Minute.Milliseconds()
	w := s.rate[acc]
	i := 0
	for i < len(w) && w[i] <= cut {
		i++
	}
	w = w[i:]
	s.rate[acc] = w
	return w
}

func (s *Scheduler) recordSend(it model.Scheduled) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rate[it.AccountID] = append(s.rate[it.AccountID], now.UnixMilli())
	if it.Kind == "message" {
		s.lastChat[chatGapKey(it)] = now
	}
}

func (s *Scheduler) send(ctx context.Context, it model.Scheduled) error {
	switch it.Kind {
	case "message":
		if it.Message == nil {
			return msgInvalid("el mensaje programado no tiene contenido")
		}
		_, err := s.msg.Send(ctx, model.NewChatKey(it.AccountID, it.ChatNativeID), *it.Message)
		return err
	case "status":
		if it.Status == nil {
			return msgInvalid("el estado programado no tiene contenido")
		}
		_, err := s.st.Post(ctx, it.AccountID, *it.Status)
		return err
	}
	return msgInvalid("tipo de programado %q desconocido", it.Kind)
}

// settle guarda el resultado de un envío (o de una omisión) y, si era una ocurrencia pendiente
// de una serie, crea la siguiente en la misma transacción. Si se borró mientras se enviaba no
// guarda nada; si se canceló mientras se enviaba (p. ej. CancelSeries) registra el resultado
// pero no continúa la serie.
func (s *Scheduler) settle(ctx context.Context, it model.Scheduled) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, err := s.get(ctx, it.ID)
	if errors.Is(err, port.ErrNotFound) {
		delete(s.unsettled, it.ID)
		return nil
	}
	if err != nil {
		return err
	}
	var next *model.Scheduled
	if it.Recurrence != nil && prev.State == "pending" {
		next = s.nextRow(it)
	}
	if err := s.saveWithNext(ctx, it, next); err != nil {
		if it.State == "sent" {
			s.unsettled[it.ID] = it // ya salió: que el siguiente tick solo reintente guardar
		}
		return err
	}
	delete(s.unsettled, it.ID)
	return nil
}

// nextRow arma la siguiente ocurrencia de la serie de cur (nil si la serie terminó). La fecha es
// la siguiente a la de cur y a ahora: si hubo atraso no se acumulan envíos de golpe.
func (s *Scheduler) nextRow(cur model.Scheduled) *model.Scheduled {
	r := cur.Recurrence
	if r == nil {
		return nil
	}
	nr := *r
	nr.Weekdays = append([]int(nil), r.Weekdays...)
	nr.Count++
	after := s.now().UnixMilli()
	if cur.SendAt > after {
		after = cur.SendAt
	}
	at, ok := nextOccurrenceIn(nr, nr.FirstAt, after, s.loc)
	if !ok {
		return nil
	}
	n := cur
	n.ID, n.SendAt, n.State, n.SentAt, n.Error = model.NewID(), at, "pending", 0, ""
	n.CreatedAt = s.now().UnixMilli()
	n.Recurrence = &nr
	if cur.Message != nil {
		m := *cur.Message
		n.Message = &m
	}
	if cur.Status != nil {
		st := *cur.Status
		n.Status = &st
	}
	return &n
}

// saveWithNext guarda cur y next juntos (en una transacción si el almacén la ofrece). Nunca
// deja dos pendientes en la misma serie: si ya hay otra, next se descarta.
func (s *Scheduler) saveWithNext(ctx context.Context, cur model.Scheduled, next *model.Scheduled) error {
	if st, ok := s.app.(scheduledSeriesStore); ok {
		return st.SaveScheduledWithNext(ctx, cur, next)
	}
	if err := s.app.SaveScheduled(ctx, cur); err != nil {
		return err
	}
	if next == nil {
		return nil
	}
	others, err := s.bySeries(ctx, next.SeriesID, []string{"pending"})
	if err != nil {
		return err
	}
	for _, o := range others {
		if o.ID != cur.ID {
			return nil
		}
	}
	return s.app.SaveScheduled(ctx, *next)
}

// ---------------------------------------------------------------------------------------------
// Lecturas (con el almacén opcional si lo ofrece)

func (s *Scheduler) get(ctx context.Context, id string) (model.Scheduled, error) {
	if st, ok := s.app.(scheduledSeriesStore); ok {
		return st.GetScheduled(ctx, id)
	}
	all, err := s.app.ListScheduled(ctx, 0, 0, nil)
	if err != nil {
		return model.Scheduled{}, err
	}
	for _, it := range all {
		if it.ID == id {
			return it, nil
		}
	}
	return model.Scheduled{}, fmt.Errorf("%w: programado %s", port.ErrNotFound, id)
}

func (s *Scheduler) bySeries(ctx context.Context, seriesID string, states []string) ([]model.Scheduled, error) {
	if seriesID == "" {
		return nil, nil
	}
	if st, ok := s.app.(scheduledSeriesStore); ok {
		return st.ListScheduledBySeries(ctx, seriesID, states)
	}
	all, err := s.app.ListScheduled(ctx, 0, 0, states)
	if err != nil {
		return nil, err
	}
	out := []model.Scheduled{}
	for _, it := range all {
		if it.SeriesID == seriesID {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *Scheduler) byChat(ctx context.Context, acc model.AccountID, chat string, states []string, since int64) ([]model.Scheduled, error) {
	if st, ok := s.app.(scheduledSeriesStore); ok {
		return st.ListScheduledByChat(ctx, acc, chat, states, since)
	}
	all, err := s.app.ListScheduled(ctx, since, 0, states)
	if err != nil {
		return nil, err
	}
	out := []model.Scheduled{}
	for _, it := range all {
		if it.Kind == "message" && it.AccountID == acc && it.ChatNativeID == chat {
			out = append(out, it)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------------------------
// API

// List devuelve los programados con SendAt entre from y to (<= 0 deja ese extremo abierto).
func (s *Scheduler) List(ctx context.Context, from, to int64) ([]model.Scheduled, error) {
	return s.app.ListScheduled(ctx, from, to, nil)
}

// ListForChat devuelve los pendientes de un chat por hora de envío y, con includeDone, también
// los enviados, fallidos u omitidos de los últimos 7 días (hasta 50), todo en una lista ordenada
// por SendAt y creación. Se pueden programar varios mensajes al mismo chat, incluso a la misma hora.
func (s *Scheduler) ListForChat(ctx context.Context, accountID model.AccountID, chatNativeID string, includeDone bool) ([]model.Scheduled, error) {
	out, err := s.byChat(ctx, accountID, chatNativeID, []string{"pending"}, 0)
	if err != nil {
		return nil, err
	}
	if includeDone {
		since := s.now().Add(-schedRecentWindow).UnixMilli()
		done, err := s.byChat(ctx, accountID, chatNativeID, []string{"sent", "failed", "canceled"}, since)
		if err != nil {
			return nil, err
		}
		if len(done) > schedRecentMax {
			done = done[len(done)-schedRecentMax:]
		}
		out = append(out, done...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SendAt != out[j].SendAt {
			return out[i].SendAt < out[j].SendAt
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out, nil
}

// scheduledMediaDir: copia de los archivos de los mensajes programados (el original puede
// borrarse antes de la hora de envío). Los de estados los copia Statuses al publicar.
func scheduledMediaDir() string { return filepath.Join(paths.DataDir(), "scheduled-media") }

// schedMediaKey: carpeta del adjunto dentro de scheduledMediaDir. Las series comparten una sola
// copia (la de su serie) para no duplicar el archivo en cada ocurrencia.
func schedMediaKey(it model.Scheduled) string {
	if it.Recurrence != nil && it.SeriesID != "" {
		return it.SeriesID
	}
	return it.ID
}

func sameRule(a, b model.Recurrence) bool {
	if a.Frequency != b.Frequency || a.Every != b.Every || a.Unit != b.Unit || len(a.Weekdays) != len(b.Weekdays) {
		return false
	}
	for i := range a.Weekdays {
		if a.Weekdays[i] != b.Weekdays[i] {
			return false
		}
	}
	return true
}

// Save guarda un programado como pending (corrige uno failed volviéndolo a pending). No se
// puede editar uno ya enviado ni cancelado. Con Recurrence crea una serie (el primer envío es
// SendAt); al editar una ocurrencia pendiente, cambiar la hora o la regla reancla la serie en el
// nuevo SendAt y lo demás (texto, adjunto, fin) se propaga a las ocurrencias siguientes.
func (s *Scheduler) Save(ctx context.Context, it model.Scheduled) (model.Scheduled, error) {
	if it.Kind != "message" && it.Kind != "status" {
		return model.Scheduled{}, msgInvalid("el programado debe ser un mensaje o un estado")
	}
	if _, err := s.acc.Store(it.AccountID); err != nil {
		return model.Scheduled{}, err
	}
	if it.SendAt <= 0 {
		return model.Scheduled{}, msgInvalid("falta la fecha y hora de envío")
	}
	switch it.Kind {
	case "message":
		if strings.TrimSpace(it.ChatNativeID) == "" {
			return model.Scheduled{}, msgInvalid("falta el chat al que se enviará el mensaje")
		}
		if it.Message == nil {
			return model.Scheduled{}, msgInvalid("falta el contenido del mensaje")
		}
		it.Status = nil
	case "status":
		if it.Status == nil {
			return model.Scheduled{}, msgInvalid("falta el contenido del estado")
		}
		it.Message, it.ChatNativeID = nil, ""
	}
	var rec *model.Recurrence
	if it.Recurrence != nil {
		r := normalizeRecurrence(*it.Recurrence)
		rec = &r
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UnixMilli()
	var prev *model.Scheduled
	if it.ID == "" {
		it.ID, it.CreatedAt = model.NewID(), now
	} else if p, err := s.get(ctx, it.ID); err == nil {
		switch p.State {
		case "pending", "failed":
		default:
			return model.Scheduled{}, msgInvalid("un programado %s ya no se puede editar", schedStateLabel(p.State))
		}
		prev = &p
		it.CreatedAt = p.CreatedAt
	} else if !errors.Is(err, port.ErrNotFound) {
		return model.Scheduled{}, err
	} else if it.CreatedAt == 0 {
		it.CreatedAt = now
	}
	it.State, it.Error, it.SentAt = "pending", "", 0
	if err := s.applyRecurrence(ctx, &it, rec, prev, now); err != nil {
		return model.Scheduled{}, err
	}

	if it.Kind == "message" {
		if m := *it.Message; m.FilePath != "" {
			if fi, err := os.Stat(m.FilePath); err != nil || fi.IsDir() {
				return model.Scheduled{}, msgInvalid("no se encontró el archivo adjunto")
			}
			if !isInside(m.FilePath, scheduledMediaDir()) {
				dst, err := copyFileInto(m.FilePath, filepath.Join(scheduledMediaDir(), schedMediaKey(it)), filepath.Base(m.FilePath))
				if err != nil {
					return model.Scheduled{}, fmt.Errorf("programados: copiar el adjunto: %w", err)
				}
				m.FilePath = dst
				it.Message = &m
			}
		}
	}
	if err := s.app.SaveScheduled(ctx, it); err != nil {
		return model.Scheduled{}, err
	}
	return it, nil
}

// applyRecurrence fija Recurrence y SeriesID de un programado que se guarda (con s.mu tomado).
func (s *Scheduler) applyRecurrence(ctx context.Context, it *model.Scheduled, rec *model.Recurrence, prev *model.Scheduled, now int64) error {
	if rec == nil {
		it.Recurrence = nil
		it.SeriesID = ""
		if prev != nil {
			it.SeriesID = prev.SeriesID // sigue en el historial de su serie, pero ya no continúa
		}
		return nil
	}
	switch {
	case prev == nil:
		if it.SendAt < now-schedMissedGrace.Milliseconds() {
			return msgInvalid("el primer envío de una repetición no puede estar en el pasado")
		}
		rec.FirstAt, rec.Count = it.SendAt, 0
		it.SeriesID = model.NewID()
	case prev.Recurrence == nil:
		rec.FirstAt, rec.Count = it.SendAt, 0
		it.SeriesID = prev.SeriesID
		if it.SeriesID == "" {
			it.SeriesID = model.NewID()
		}
	default:
		it.SeriesID = prev.SeriesID
		rec.Count = prev.Recurrence.Count
		if it.SendAt != prev.SendAt || !sameRule(*rec, *prev.Recurrence) {
			rec.FirstAt = it.SendAt
		} else {
			rec.FirstAt = prev.Recurrence.FirstAt
		}
		if prev.State == "failed" {
			// Si la serie ya siguió con otra ocurrencia, esta se reintenta suelta.
			others, err := s.bySeries(ctx, prev.SeriesID, []string{"pending"})
			if err != nil {
				return err
			}
			for _, o := range others {
				if o.ID != it.ID {
					it.Recurrence = nil
					return nil
				}
			}
		}
	}
	if err := ValidateRecurrence(*rec, rec.FirstAt); err != nil {
		return err
	}
	it.Recurrence = rec
	return nil
}

func schedStateLabel(s string) string {
	switch s {
	case "sent":
		return "enviado"
	case "canceled":
		return "cancelado"
	}
	return s
}

// Update cambia un programado que ya existe (texto, adjunto, hora, chat o repetición): es Save
// pero exige que exista. Devuelve ErrNotFound si no está.
func (s *Scheduler) Update(ctx context.Context, it model.Scheduled) (model.Scheduled, error) {
	if it.ID == "" {
		return model.Scheduled{}, msgInvalid("falta el programado que se quiere cambiar")
	}
	if _, err := s.get(ctx, it.ID); err != nil {
		return model.Scheduled{}, err
	}
	return s.Save(ctx, it)
}

// UpdateSeries cambia la ocurrencia pendiente de una serie (texto, hora y/o regla); lo cambiado se
// propaga a las siguientes. it lleva el contenido completo (como Save); su ID se ignora. Un it sin
// Recurrence deja la serie sin repetición a partir de esa ocurrencia.
func (s *Scheduler) UpdateSeries(ctx context.Context, seriesID string, it model.Scheduled) (model.Scheduled, error) {
	pend, err := s.bySeries(ctx, seriesID, []string{"pending"})
	if err != nil {
		return model.Scheduled{}, err
	}
	if len(pend) == 0 {
		return model.Scheduled{}, msgInvalid("la serie ya no tiene ocurrencias pendientes")
	}
	it.ID = pend[0].ID
	if it.AccountID == "" {
		it.AccountID = pend[0].AccountID
	}
	return s.Save(ctx, it)
}

// Cancel cancela un programado pendiente (o fallido). Si es una ocurrencia de una serie solo
// cancela esa: la serie sigue con la siguiente (CancelSeries la detiene toda).
func (s *Scheduler) Cancel(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if it.State != "pending" && it.State != "failed" {
		return msgInvalid("un programado %s no se puede cancelar", schedStateLabel(it.State))
	}
	var next *model.Scheduled
	if it.State == "pending" && it.Recurrence != nil {
		next = s.nextRow(it)
	}
	it.State = "canceled"
	return s.saveWithNext(ctx, it, next)
}

// CancelSeries cancela las ocurrencias pendientes de toda la serie (las ya enviadas quedan en el
// historial). ErrNotFound si la serie no existe.
func (s *Scheduler) CancelSeries(ctx context.Context, seriesID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.bySeries(ctx, seriesID, nil)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return fmt.Errorf("%w: serie %s", port.ErrNotFound, seriesID)
	}
	for _, it := range all {
		if it.State != "pending" {
			continue
		}
		it.State = "canceled"
		if err := s.app.SaveScheduled(ctx, it); err != nil {
			return err
		}
	}
	return nil
}

// Delete borra un programado (en cualquier estado) y su copia del adjunto. Borrar la ocurrencia
// pendiente de una serie equivale a saltarla: la serie sigue con la siguiente (DeleteSeries la borra toda).
func (s *Scheduler) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if it.State == "pending" && it.Recurrence != nil {
		if next := s.nextRow(it); next != nil {
			it.State = "canceled"
			if err := s.saveWithNext(ctx, it, next); err != nil {
				return err
			}
		}
	}
	if err := s.app.DeleteScheduled(ctx, id); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(scheduledMediaDir(), id))
	return nil
}

// DeleteSeries borra todas las ocurrencias de una serie (historial incluido) y su adjunto.
func (s *Scheduler) DeleteSeries(ctx context.Context, seriesID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.bySeries(ctx, seriesID, nil)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return fmt.Errorf("%w: serie %s", port.ErrNotFound, seriesID)
	}
	for _, it := range all {
		if err := s.app.DeleteScheduled(ctx, it.ID); err != nil {
			return err
		}
		_ = os.RemoveAll(filepath.Join(scheduledMediaDir(), it.ID))
	}
	_ = os.RemoveAll(filepath.Join(scheduledMediaDir(), seriesID))
	return nil
}

// SendNow envía ya un programado pendiente o fallido, sin esperar su hora. Si falla devuelve el
// error (y queda failed); si la cuenta no está conectada devuelve ErrNotConnected y sigue
// pendiente. Enviar ya una ocurrencia de serie la cierra y la serie sigue con la siguiente.
func (s *Scheduler) SendNow(ctx context.Context, id string) error {
	it, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if it.State != "pending" && it.State != "failed" {
		return msgInvalid("un programado %s no se puede enviar", schedStateLabel(it.State))
	}
	if s.acc.Status(it.AccountID).State != model.StateConnected {
		return port.ErrNotConnected
	}
	return s.run(ctx, it, true)
}
