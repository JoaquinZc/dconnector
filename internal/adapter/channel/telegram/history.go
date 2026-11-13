package telegram

// Historial y chats (08 §7).

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// maxDialogs: tope de chats que se sincronizan en v1 (08 §7).
	maxDialogs = 1000
	// dialogBatch: tamaño de página al pedir chats y de cada history.batch.
	dialogBatch = 100
	// historyDefault y historyMax: cuántos mensajes se piden por RequestHistory.
	historyDefault = 50
	historyMax     = 100
)

// entitiesOf pasa las entidades de un diálogo a tg.Entities.
func entitiesOf(e interface {
	Users() map[int64]*tg.User
	Chats() map[int64]*tg.Chat
	Channels() map[int64]*tg.Channel
}) tg.Entities {
	return tg.Entities{Users: e.Users(), Chats: e.Chats(), Channels: e.Channels()}
}

// dialogChat arma el Chat de un diálogo (08 §7): marcas, no leídos y último mensaje.
func (a *Adapter) dialogChat(c converter, dlg *tg.Dialog, last tg.NotEmptyMessage, now time.Time) (model.Chat, []model.Message, bool) {
	native := nativeOf(dlg.Peer)
	if native == "" {
		return model.Chat{}, nil, false
	}
	marks := chatMarks{Archived: dlg.FolderID == 1, Pinned: dlg.Pinned, Muted: muteMs(dlg.NotifySettings.MuteUntil, now)}
	a.local.setMarks(native, marks)
	ch, ok := a.chatShell(native, c.v())
	if !ok {
		// Sin título conocido (par que Telegram no describió): se emite igual con un nombre genérico.
		ch.Title = "Chat de Telegram"
		if ch.NativeID == "" {
			return model.Chat{}, nil, false
		}
	}
	ch.UnreadCount = dlg.UnreadCount
	if ch.UnreadCount == 0 && dlg.UnreadMark {
		ch.UnreadCount = 1
	}
	var msgs []model.Message
	switch m := last.(type) {
	case *tg.Message:
		msg := c.message(m)
		msgs = append(msgs, msg)
		ch.LastMessageAt, ch.LastPreview, ch.LastFromMe = msg.Timestamp, model.Preview(msg), msg.FromMe
		a.rememberPollQuiet(msg.ChatNativeID, m)
	case *tg.MessageService:
		if msg, ok := c.service(m); ok {
			msgs = append(msgs, msg)
			ch.LastMessageAt, ch.LastPreview, ch.LastFromMe = msg.Timestamp, model.Preview(msg), msg.FromMe
		}
	}
	return ch, msgs, true
}

// rememberPollQuiet guarda las opciones de una encuesta del historial sin contexto de petición.
func (a *Adapter) rememberPollQuiet(chat string, m *tg.Message) {
	ctx, cancel := context.WithTimeout(a.runContext(), opTimeout)
	defer cancel()
	a.rememberPoll(ctx, chat, m)
}

// SyncChats pide los chats (hasta 1.000) y los emite en lotes de history.batch con el último
// mensaje de cada uno, sin sumar no leídos (08 §7).
func (a *Adapter) SyncChats(ctx context.Context) error {
	api, err := a.liveAPI()
	if err != nil {
		return err
	}
	var chats []model.Chat
	var msgs []model.Message
	persons := map[int64]model.Person{}
	total := 0
	flush := func(progress float64) {
		if len(chats) == 0 && len(msgs) == 0 && len(persons) == 0 {
			return
		}
		batch := model.HistoryBatch{Chats: chats, Messages: msgs, Progress: progress}
		for _, p := range persons {
			batch.Persons = append(batch.Persons, p)
		}
		a.local.indexMsgChats(ctx, msgs)
		a.emit(model.Event{Type: model.EvHistoryBatch, History: &batch})
		chats, msgs, persons = nil, nil, map[int64]model.Person{}
	}
	for _, folder := range []int{0, 1} {
		if total >= maxDialogs {
			break
		}
		iter := query.GetDialogs(api).BatchSize(dialogBatch).FolderID(folder).Iter()
		for iter.Next(ctx) {
			el := iter.Value()
			dlg, ok := el.Dialog.(*tg.Dialog)
			if !ok { // DialogFolder: la carpeta de archivados, que se recorre aparte
				continue
			}
			ents := entitiesOf(el.Entities)
			for _, u := range a.peers.feed(ctx, ents) {
				a.local.noteUserSig(u)
				persons[u.ID] = personOf(a.acc.ID, u)
			}
			c := a.conv(&ents)
			ch, cm, ok := a.dialogChat(c, dlg, el.Last, time.Now())
			if !ok {
				continue
			}
			chats = append(chats, ch)
			msgs = append(msgs, cm...)
			for _, m := range cm {
				a.local.noteQuote(m)
			}
			a.queueAvatar(ch.NativeID)
			total++
			if len(chats) >= dialogBatch {
				flush(-1)
			}
			if total >= maxDialogs {
				break
			}
		}
		if err := iter.Err(); err != nil {
			flush(-1)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return a.mapErr("sincronizar los chats", err)
		}
	}
	flush(1)
	a.local.markSynced()
	a.log.Info("chats sincronizados", "total", total)
	return nil
}

// floodWaitError: Telegram pidió esperar antes de seguir (FLOOD_WAIT que gotd no absorbió por ser largo).
// Cuenta como port.ErrRateLimited y RetryAfter dice cuánto; el servicio de historial espera ese tiempo y repite.
type floodWaitError struct {
	wait   time.Duration
	action string
}

func (e *floodWaitError) Error() string {
	return fmt.Sprintf("telegram: Telegram pidió esperar %s antes de %s", e.wait.Round(time.Second), e.action)
}
func (e *floodWaitError) Unwrap() error             { return port.ErrRateLimited }
func (e *floodWaitError) RetryAfter() time.Duration { return e.wait }

// RequestHistory pide una página de mensajes anteriores a `before` (o al más antiguo que se conozca) con
// messages.getHistory (offset_id = id de ese mensaje: devuelve los que tienen id menor, de más reciente a
// más antiguo) y los emite en un history.batch por el mismo camino que cualquier otro historial. Responde
// de forma síncrona: cuando vuelve, el lote ya salió. Si Telegram no devuelve nada, emite un lote vacío
// («contestó y no hay más»). Un FLOOD_WAIT largo se devuelve como floodWaitError.
func (a *Adapter) RequestHistory(ctx context.Context, chatNativeID string, before *model.MessageRef, count int) error {
	api, peer, err := a.sendTarget(ctx, chatNativeID)
	if err != nil {
		return err
	}
	if count <= 0 {
		count = historyDefault
	}
	if count > historyMax {
		count = historyMax
	}
	offset := 0
	switch {
	case before != nil && before.NativeID != "":
		offset, err = strconv.Atoi(before.NativeID)
		if err != nil || offset <= 0 {
			return fmt.Errorf("telegram: identificador de mensaje no válido: %w", port.ErrInvalid)
		}
	default:
		offset = a.oldestMessageID(ctx, chatNativeID)
	}
	res, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, OffsetID: offset, Limit: count})
	if err != nil {
		if d, ok := tgerr.AsFloodWait(err); ok {
			return &floodWaitError{wait: d, action: "pedir mensajes anteriores"}
		}
		return a.mapErr("pedir mensajes anteriores", err)
	}
	mod, ok := res.AsModified()
	if !ok {
		a.emitHistoryPage(ctx, nil)
		return nil
	}
	a.emitHistoryPage(ctx, mod)
	return nil
}

// emitHistoryPage convierte una página de messages.getHistory y la emite como history.batch. Con mod == nil
// o sin mensajes emite un lote vacío, que el servicio de historial entiende como «no hay más».
func (a *Adapter) emitHistoryPage(ctx context.Context, mod tg.ModifiedMessagesMessages) {
	var msgs []model.Message
	var persons []model.Person
	if mod != nil {
		ents := tg.EntitiesFrom(mod.GetUsers(), mod.GetChats())
		for _, u := range a.peers.feed(ctx, ents) {
			persons = append(persons, personOf(a.acc.ID, u))
			a.local.noteUserSig(u)
		}
		c := a.conv(&ents)
		for _, mc := range mod.GetMessages() {
			switch m := mc.(type) {
			case *tg.Message:
				msg := c.message(m)
				a.rememberPoll(ctx, msg.ChatNativeID, m)
				msgs = append(msgs, msg)
			case *tg.MessageService:
				if msg, ok := c.service(m); ok {
					msgs = append(msgs, msg)
				}
			}
		}
	}
	a.local.indexMsgChats(ctx, msgs)
	a.emit(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{Messages: msgs, Persons: persons, Progress: -1}})
}

// oldestMessageID: id del mensaje más antiguo que el almacén tiene de un chat (0 = desde el más reciente).
func (a *Adapter) oldestMessageID(ctx context.Context, chat string) int {
	var id string
	err := a.deps.DB.QueryRowContext(ctx,
		"SELECT native_id FROM messages WHERE chat_native_id = ? ORDER BY CAST(native_id AS INTEGER) ASC LIMIT 1", chat).Scan(&id)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(id)
	return n
}
