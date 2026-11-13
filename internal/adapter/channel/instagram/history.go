package instagram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// maxSyncThreads: tope de hilos que pide SyncChats (09 §3).
const maxSyncThreads = 500

// fetchThread pide la ficha de un hilo (con sus últimos mensajes), la recuerda y la entrega: chat, personas y
// mensajes como history.batch. Es lo que se hace con un chat que aún no se conocía.
func (a *Adapter) fetchThread(ctx context.Context, chat string) error {
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	if chat == "" {
		return fmt.Errorf("falta el identificador del chat: %w", port.ErrInvalid)
	}
	fctx, cancel := context.WithTimeout(ctx, meta.OpTimeout)
	resp, err := cli.GetThread(fctx, slidetypes.MakeGetThreadInfoRequest(chat))
	cancel()
	if err != nil {
		return err
	}
	info := resp.ThreadInfo.AsIGDirectThread
	if info == nil {
		return fmt.Errorf("Instagram no devolvió ese chat: %w", port.ErrNotFound)
	}
	res := a.conv.Thread(info)
	if info.SlideMessages != nil {
		page := a.conv.Page(chat, info.SlideMessages.Edges)
		res.Messages = page.Messages
		for id, u := range page.Avatars {
			res.Avatars[id] = u
		}
	}
	a.afterConvert(res)
	b := res.History(1)
	a.Emit(model.Event{Type: model.EvHistoryBatch, History: &b})
	return nil
}

// SyncChats emite chat.updated de los chats conocidos y pide más páginas de la bandeja (hasta 500).
func (a *Adapter) SyncChats(ctx context.Context) error {
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	known := a.conv.AllChats()
	for i := range known {
		ch := known[i]
		ch.UnreadCount = -1
		a.Emit(model.Event{Type: model.EvChatUpsert, Chat: &ch, ChatKey: ch.Key})
	}
	total := len(known)
	self := a.conv.Self()
	cursor := ""
	// La primera página ya se entregó al abrir la sesión; se sigue con la paginación desde ahí.
	mb, err := cli.GetMailbox(ctx)
	if err != nil {
		return a.mapErr("pedir la bandeja", err)
	}
	if mb == nil || mb.Mailbox == nil {
		return nil
	}
	a.evMu.Lock()
	merged := &Result{Avatars: map[string]string{}}
	for _, e := range mb.Mailbox.ThreadsByFolder.Edges {
		if t := e.Node.AsIGDirectThread; t != nil {
			r := a.conv.Thread(t)
			merged.Chats, merged.Persons = append(merged.Chats, r.Chats...), append(merged.Persons, r.Persons...)
		}
	}
	a.afterConvert(merged)
	if len(merged.Chats) > 0 {
		b := merged.History(-1)
		a.Emit(model.Event{Type: model.EvHistoryBatch, History: &b})
	}
	a.evMu.Unlock()
	total += len(merged.Chats)
	cursor = mb.Mailbox.ThreadsByFolder.PageInfo.EndCursor
	hasMore := mb.Mailbox.ThreadsByFolder.PageInfo.HasNextPage
	for hasMore && cursor != "" && total < maxSyncThreads {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fctx, cancel := context.WithTimeout(ctx, meta.OpTimeout)
		resp, err := cli.PaginateMailbox(fctx, slidetypes.MakePaginateMailboxRequest(self, cursor, "", nil))
		cancel()
		if err != nil {
			return a.mapErr("pedir más chats", err)
		}
		if resp == nil || resp.Mailbox == nil {
			break
		}
		page := &Result{Avatars: map[string]string{}}
		a.evMu.Lock()
		for _, e := range resp.Mailbox.ThreadsByFolder.Edges {
			if t := e.Node.AsIGDirectThread; t != nil {
				r := a.conv.Thread(t)
				page.Chats, page.Persons = append(page.Chats, r.Chats...), append(page.Persons, r.Persons...)
			}
		}
		a.afterConvert(page)
		if len(page.Chats) > 0 {
			b := page.History(-1)
			a.Emit(model.Event{Type: model.EvHistoryBatch, History: &b})
		}
		a.evMu.Unlock()
		total += len(page.Chats)
		cursor = resp.Mailbox.ThreadsByFolder.PageInfo.EndCursor
		hasMore = resp.Mailbox.ThreadsByFolder.PageInfo.HasNextPage && len(page.Chats) > 0
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

// backfillRecent carga los últimos mensajes de los chats más recientes (en la bandeja solo vienen a medias).
func (a *Adapter) backfillRecent(ctx context.Context) {
	chats := a.conv.AllChats()
	if len(chats) > recentBackfill {
		chats = chats[:recentBackfill]
	}
	for _, ch := range chats {
		if ctx.Err() != nil {
			return
		}
		if err := a.fetchThread(ctx, ch.NativeID); err != nil {
			a.Log.Debug("no se pudieron cargar los últimos mensajes de un chat", "chat", meta.ShortID(ch.NativeID), "err", meta.RedactErr(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(700 * time.Millisecond):
		}
	}
}

// RequestHistory pide una página de mensajes más antiguos que before (o que el más antiguo guardado) y los
// entrega como history.batch. Instagram manda 20 por página; count solo orienta. Responde de forma síncrona:
// si la página no trae mensajes (no hay más), emite un lote vacío, que el servicio de historial entiende como
// «contestó y no hay más». Un chat sin ningún mensaje guardado no tiene desde dónde partir (ErrNotFound).
func (a *Adapter) RequestHistory(ctx context.Context, chatNativeID string, before *model.MessageRef, count int) error {
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	if _, err := meta.ParseID(chatNativeID); err != nil {
		return fmt.Errorf("instagram: %w", err)
	}
	refID := ""
	if before != nil {
		refID = before.NativeID
	}
	if refID == "" {
		refID, _ = a.local.Oldest(ctx, chatNativeID)
	}
	if refID == "" {
		return fmt.Errorf("instagram: todavía no hay mensajes de este chat desde los que pedir el historial: %w", port.ErrNotFound)
	}
	refID = strings.SplitN(refID, "#", 2)[0]
	resp, err := cli.PaginateMessages(ctx, &slidetypes.PaginateMessagesRequest{
		OlderThanMessageID: &refID, ThreadID: chatNativeID, FirstN: 20, InitialMessagePageCount: 20,
	})
	if err != nil {
		return a.mapErr("pedir el historial", err)
	}
	th := resp.ThreadInfo.AsIGDirectThread
	if th == nil || th.Messages == nil {
		a.Emit(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{Progress: -1}})
		return nil
	}
	a.evMu.Lock()
	defer a.evMu.Unlock()
	res := a.conv.Page(chatNativeID, th.Messages.Edges)
	a.afterConvert(res)
	b := res.History(1)
	b.Chats, b.Persons = nil, res.Persons
	if len(b.Messages) > 0 || len(b.Persons) > 0 {
		a.Emit(model.Event{Type: model.EvHistoryBatch, History: &b})
	}
	if len(b.Messages) == 0 {
		a.Emit(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{Progress: -1}})
	}
	return nil
}

// ResolvePerson devuelve lo que se sabe de una persona por su id.
func (a *Adapter) ResolvePerson(ctx context.Context, peerNativeID string) (model.Person, error) {
	id, err := meta.ParseID(peerNativeID)
	if err != nil {
		return model.Person{}, fmt.Errorf("instagram: %w", err)
	}
	return a.conv.PersonSnapshot(id), nil
}

// ListPersons devuelve las personas que se conocen por los chats (Instagram no tiene agenda).
func (a *Adapter) ListPersons(ctx context.Context) ([]model.Person, error) {
	return a.conv.KnownPersons(), nil
}
