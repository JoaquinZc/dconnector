package ws

import (
	"context"
	"encoding/json"
	"time"

	"dconnector/internal/core/model"
)

// sendTicket empareja un comando message.send/reply (con clientRef) con el evento message.sent que
// provoca, para que el servidor lo reciba en data.clientRef (10 §4). Solo hay un ticket por chat a
// la vez: los comandos con clientRef al mismo chat se encadenan.
type sendTicket struct {
	chat      model.ChatKey
	clientRef string
	key       model.MessageKey // se fija cuando Send devuelve el mensaje
	consumed  chan struct{}    // se cierra cuando el evento ya salió hacia el outbox
	released  chan struct{}    // se cierra cuando el comando termina con el ticket
	once      bool
}

// beginSend reserva el ticket del chat (espera si otro comando con clientRef lo tiene).
func (m *Manager) beginSend(ctx context.Context, chat model.ChatKey, clientRef string) (*sendTicket, error) {
	for {
		m.tmu.Lock()
		cur, busy := m.tickets[chat]
		if !busy {
			t := &sendTicket{
				chat: chat, clientRef: clientRef,
				consumed: make(chan struct{}), released: make(chan struct{}),
			}
			m.tickets[chat] = t
			m.tmu.Unlock()
			return t, nil
		}
		wait := cur.released
		m.tmu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// endSend libera el ticket. Si el envío salió bien, espera (poco) a que su evento message.sent pase
// por Enqueue, para que el siguiente comando al mismo chat no se lo lleve.
func (m *Manager) endSend(ctx context.Context, t *sendTicket, sent bool) {
	if sent {
		select {
		case <-t.consumed:
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
		}
	}
	m.tmu.Lock()
	if m.tickets[t.chat] == t {
		delete(m.tickets, t.chat)
	}
	m.tmu.Unlock()
	close(t.released)
}

func (m *Manager) setTicketKey(t *sendTicket, key model.MessageKey) {
	m.tmu.Lock()
	t.key = key
	m.tmu.Unlock()
}

// consumeTicket devuelve el clientRef pendiente para un mensaje enviado, si lo hay.
func (m *Manager) consumeTicket(msg *model.Message) string {
	if msg == nil || !msg.FromMe {
		return ""
	}
	m.tmu.Lock()
	defer m.tmu.Unlock()
	t := m.tickets[model.NewChatKey(msg.AccountID, msg.ChatNativeID)]
	if t == nil || t.once {
		return ""
	}
	if t.key != "" && t.key != msg.Key {
		return ""
	}
	t.once = true
	close(t.consumed)
	return t.clientRef
}

// Enqueue lo llama el Router de forma síncrona por cada evento. Guarda el frame en el outbox de
// cada destino habilitado cuyo filtro lo acepte y despierta a su cliente. Nunca toca la red.
func (m *Manager) Enqueue(ctx context.Context, ev model.Event) {
	if !protocolEvents[ev.Type] {
		return
	}
	name := string(ev.Type)
	clientRef := ""
	if ev.Type == model.EvMessageSent {
		clientRef = m.consumeTicket(ev.Message)
	}
	dsts := m.enabledDestinations(ctx)
	if len(dsts) == 0 {
		return
	}
	lk := &lookups{m: m, ctx: ctx}
	var targets []model.Destination
	for _, d := range dsts {
		if accepts(d.Filter, name, ev, lk) {
			targets = append(targets, d)
		}
	}
	if len(targets) == 0 {
		return
	}
	if ev.ID == "" {
		ev.ID = model.NewID()
	}
	data, ok := eventData(ev, clientRef, lk)
	if !ok {
		return
	}
	at := ev.At
	if at == 0 {
		at = nowMs()
	}
	frame := frameOut{
		V: protocolVersion, ID: ev.ID, Kind: kindEvent, Name: name, TS: at,
		Account: m.accountRef(ctx, ev.AccountID, ev.Channel), Data: data,
	}
	payload, err := marshalFrame(frame)
	if err == errFrameTooBig {
		frame.Data = shrinkData(data)
		payload, err = marshalFrame(frame)
	}
	if err != nil {
		m.log.Warn("evento descartado: no cabe en un frame", "type", name, "eventId", ev.ID, "err", err)
		return
	}
	for _, d := range targets {
		if err := m.app.OutboxAdd(ctx, d.ID, ev.ID, payload); err != nil {
			m.log.Error("no se pudo guardar el evento en el outbox", "destinationId", d.ID, "type", name, "err", err)
			continue
		}
		m.markDirty(d.ID)
		m.signalClient(d.ID)
	}
}

// flush envía lo pendiente del outbox, de lo más antiguo a lo más nuevo, sin pasar de la ventana
// de eventos sin ack (10 §4).
func (s *session) flush() error {
	m, id := s.m, s.c.dst.ID
	for {
		s.applyAcks()
		s.mu.Lock()
		inflight := len(s.inflight)
		s.mu.Unlock()
		free := maxWindow - inflight
		if free <= 0 {
			return nil
		}
		// El almacén devuelve también lo ya enviado sin ack: se pide de más y se salta.
		rows, err := m.app.OutboxPending(s.ctx, id, inflight+free)
		if err != nil {
			if s.ctx.Err() == nil {
				s.c.log.Error("no se pudo leer el outbox", "err", err)
			}
			return nil
		}
		sent := 0
		var skipped []string
		for _, r := range rows {
			s.mu.Lock()
			_, already := s.inflight[r.EventID]
			s.mu.Unlock()
			if already {
				continue
			}
			if sent >= free {
				break
			}
			if len(r.Payload) == 0 || !s.wantsFrame(r.Payload) {
				skipped = append(skipped, r.EventID)
				continue
			}
			if err := s.writeRaw(r.Payload, writeTimeout); err != nil {
				return err
			}
			s.mu.Lock()
			s.inflight[r.EventID] = struct{}{}
			s.mu.Unlock()
			sent++
		}
		if len(skipped) > 0 {
			// Eventos que el servidor no quiere (welcome.events): se dan por entregados.
			if err := m.app.OutboxAck(s.ctx, id, skipped); err != nil {
				s.c.log.Error("no se pudo limpiar el outbox", "err", err)
			}
		}
		m.markDirty(id)
		if sent < free && len(skipped) == 0 {
			return nil
		}
		if sent == 0 && len(skipped) == 0 {
			return nil
		}
	}
}

// wantsFrame aplica welcome.events al nombre del evento guardado en el frame.
func (s *session) wantsFrame(payload []byte) bool {
	want := s.wantedEvents()
	if want == nil {
		return true
	}
	var h struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(payload, &h); err != nil {
		return false
	}
	return want[h.Name]
}
