package api

import (
	"context"
	"log/slog"
	"time"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

// Nombres de los eventos (docs/14 §5.1): los mismos que emite ui/wails/events.go.
const (
	EvtAccountStatus    = "account:status"
	EvtAccountCaps      = "account:caps"
	EvtChatUpsert       = "chat:upsert"
	EvtChatRemoved      = "chat:removed"
	EvtMessageUpsert    = "message:upsert"
	EvtMessageUpdate    = "message:update"
	EvtReceipt          = "receipt"
	EvtTyping           = "typing"
	EvtPersonUpsert     = "person:upsert"
	EvtStatusUpsert     = "status:upsert"
	EvtStatusView       = "status:view"
	EvtHistoryProgress  = "history:progress"
	EvtHistorySync      = "history:sync"
	EvtCampaignProgress = "campaign:progress"
	EvtDestinationState = "destination:state"
	EvtUnreadTotal      = "unread:total"
	EvtHello            = "hello"
	EvtNotify           = "notify"
	EvtNotifyDecision   = "notify:decision"
)

const (
	batchEvery   = 100 * time.Millisecond
	unreadEvery  = time.Second
	historyEvery = time.Second
	busBuffer    = 4096
)

type unreadTotals interface {
	UnreadTotals(ctx context.Context) (map[model.AccountID]int, error)
}

// bridge reemite a los WebSocket los eventos del bus y los avisos de campañas e historial. Es la copia de
// ui/wails.Bridge con otra salida (emit).
type bridge struct {
	emit     func(name string, data any)
	b        *bus.Bus
	unread   unreadTotals
	progress <-chan service.CampaignProgress
	history  <-chan service.HistoryStatus
}

// run bloquea hasta que ctx se cancela. Si un pánico corta el bucle, se registra y se reinicia.
func (br *bridge) run(ctx context.Context) {
	for ctx.Err() == nil {
		if !br.runGuarded(ctx) {
			return
		}
		slog.Error("pánico en el puente de eventos: se reinicia")
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (br *bridge) runGuarded(ctx context.Context) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("pánico recuperado", "where", "puente de eventos", "panic", r)
			panicked = true
		}
	}()
	br.loop(ctx)
	return false
}

// loop: los mensajes y chats nuevos se agrupan cada 100 ms en arrays; el total de no leídos se recalcula
// como mucho una vez por segundo tras un chat:upsert.
func (br *bridge) loop(ctx context.Context) {
	ch, cancel := br.b.Subscribe("api", busBuffer)
	defer cancel()

	var (
		chats       = map[model.ChatKey]model.Chat{}
		chatOrder   []model.ChatKey
		msgs        []model.Message
		unreadDirty bool
		lastUnread  time.Time
	)
	tick := time.NewTicker(batchEvery)
	defer tick.Stop()

	hist := br.history
	histLast := map[model.AccountID]time.Time{}
	histPending := map[model.AccountID]service.HistoryStatus{}
	emitHistory := func(st service.HistoryStatus) {
		histLast[st.AccountID] = time.Now()
		delete(histPending, st.AccountID)
		br.emit(EvtHistorySync, st)
	}

	flush := func() {
		if len(chatOrder) > 0 {
			out := make([]model.Chat, 0, len(chatOrder))
			for _, k := range chatOrder {
				out = append(out, chats[k])
			}
			chats, chatOrder = map[model.ChatKey]model.Chat{}, nil
			br.emit(EvtChatUpsert, out)
		}
		if len(msgs) > 0 {
			out := msgs
			msgs = nil
			br.emit(EvtMessageUpsert, out)
		}
		for acc, st := range histPending {
			if time.Since(histLast[acc]) >= historyEvery {
				emitHistory(st)
			}
		}
		if unreadDirty && time.Since(lastUnread) >= unreadEvery {
			unreadDirty = false
			lastUnread = time.Now()
			br.emitUnread(ctx)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			flush()
		case p, ok := <-br.progress:
			if ok {
				br.emit(EvtCampaignProgress, p)
			}
		case st, ok := <-hist:
			if !ok {
				hist = nil
				break
			}
			if !st.Running || time.Since(histLast[st.AccountID]) >= historyEvery {
				emitHistory(st)
			} else {
				histPending[st.AccountID] = st
			}
		case ev, ok := <-ch:
			if !ok {
				return
			}
			switch ev.Type {
			case model.EvChatUpsert:
				if ev.Chat != nil {
					if _, seen := chats[ev.Chat.Key]; !seen {
						chatOrder = append(chatOrder, ev.Chat.Key)
					}
					chats[ev.Chat.Key] = *ev.Chat
					unreadDirty = true
				}
			case model.EvMessageNew, model.EvMessageSent:
				if ev.Message != nil {
					msgs = append(msgs, *ev.Message)
				}
			default:
				br.relay(ev)
			}
		}
	}
}

// relay reenvía de inmediato los eventos que no se agrupan.
func (br *bridge) relay(ev model.Event) {
	switch ev.Type {
	case model.EvAccountStatus:
		if ev.AccountStatus != nil {
			br.emit(EvtAccountStatus, *ev.AccountStatus)
		}
	case model.EvCapabilities:
		br.emit(EvtAccountCaps, map[string]any{"accountId": string(ev.AccountID), "capabilities": ev.Capabilities})
	case model.EvChatRemoved:
		br.emit(EvtChatRemoved, map[string]any{"chatKey": string(ev.ChatKey)})
	case model.EvMessageUpdate, model.EvReaction:
		if ev.Message != nil {
			br.emit(EvtMessageUpdate, *ev.Message)
		}
	case model.EvReceipt:
		if ev.Receipt != nil {
			br.emit(EvtReceipt, *ev.Receipt)
		}
	case model.EvTyping:
		if ev.Typing != nil {
			br.emit(EvtTyping, *ev.Typing)
		}
	case model.EvPerson:
		if ev.Person != nil {
			br.emit(EvtPersonUpsert, *ev.Person)
		}
	case model.EvStatusUpsert:
		if ev.Status != nil {
			br.emit(EvtStatusUpsert, *ev.Status)
		}
	case model.EvStatusView:
		if ev.StatusView != nil {
			br.emit(EvtStatusView, *ev.StatusView)
		}
	case model.EvHistoryBatch:
		// Sin los datos: la interfaz recarga la lista al terminar.
		p := -1.0
		if ev.History != nil {
			p = ev.History.Progress
		}
		br.emit(EvtHistoryProgress, map[string]any{"accountId": string(ev.AccountID), "progress": p})
	}
}

func (br *bridge) emitUnread(ctx context.Context) {
	if br.unread == nil {
		return
	}
	m, err := br.unread.UnreadTotals(ctx)
	if err != nil {
		slog.Debug("no se pudo calcular el total de no leídos", "error", err)
		return
	}
	by := make(map[string]int, len(m))
	total := 0
	for k, v := range m {
		if string(k) == "total" {
			continue
		}
		by[string(k)] = v
		total += v
	}
	br.emit(EvtUnreadTotal, map[string]any{"total": total, "byAccount": by})
}
