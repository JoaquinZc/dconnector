package messenger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/socket"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// maxSyncThreads: tope de hilos que pide SyncChats (09 §3).
const maxSyncThreads = 500

// syncGroups: grupos de sincronización de hilos de Messenger (1 = bandeja; 95 = solicitudes y cifrados).
var syncGroups = []int64{1, 95}

// SyncChats emite chat.updated de los chats conocidos y pide más páginas de hilos (hasta 500).
func (a *Adapter) SyncChats(ctx context.Context) error {
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	// Primero lo que ya se sabe.
	known := a.conv.AllChats()
	for i := range known {
		ch := known[i]
		a.Emit(model.Event{Type: model.EvChatUpsert, Chat: &ch, ChatKey: ch.Key})
	}
	total := len(known)
	for _, group := range syncGroups {
		for total < maxSyncThreads {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fctx, cancel := context.WithTimeout(ctx, meta.OpTimeout)
			ks, tbl, err := cli.FetchMoreThreads(fctx, group)
			cancel()
			if err != nil {
				return a.mapErr("pedir más chats", err)
			}
			if ks == nil || tbl == nil {
				break // no hay más
			}
			a.tableMu.Lock()
			res := a.conv.Convert(ctx, tbl)
			a.afterConvert(res)
			res.Typing = nil
			for _, b := range res.HistoryBatches(500) {
				batch := b
				a.Emit(model.Event{Type: model.EvHistoryBatch, History: &batch})
			}
			a.tableMu.Unlock()
			if len(res.Chats) == 0 {
				break
			}
			total += len(res.Chats)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	return nil
}

// RequestHistory pide una página de mensajes más antiguos que before (o que el más antiguo guardado) y los
// entrega como history.batch. Messenger decide cuántos manda por página (≈20): count solo orienta. Responde
// de forma síncrona: si la página no trae mensajes (no hay más), emite un lote vacío, que el servicio de
// historial entiende como «contestó y no hay más». Los chats cifrados no admiten historial (ErrUnsupported)
// y un chat sin ningún mensaje guardado no tiene desde dónde partir (ErrNotFound).
func (a *Adapter) RequestHistory(ctx context.Context, chatNativeID string, before *model.MessageRef, count int) error {
	if _, ok := a.e2eeChat(chatNativeID); ok {
		return fmt.Errorf("messenger: el historial de los chats cifrados no se puede pedir: %w", port.ErrUnsupported)
	}
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	threadID, err := meta.ParseID(chatNativeID)
	if err != nil {
		return fmt.Errorf("messenger: %w", err)
	}
	refID, refTS := "", int64(0)
	if before != nil {
		refID, refTS = before.NativeID, before.Timestamp
	}
	if refID == "" {
		refID, refTS = a.local.Oldest(ctx, chatNativeID)
	}
	if refID == "" {
		return fmt.Errorf("messenger: todavía no hay mensajes de este chat desde los que pedir el historial: %w", port.ErrNotFound)
	}
	refID = strings.SplitN(refID, "#", 2)[0]
	tbl, err := cli.ExecuteTasks(ctx, &socket.FetchMessagesTask{
		ThreadKey: threadID, Direction: 0, ReferenceTimestampMs: refTS, ReferenceMessageId: refID, SyncGroup: 1,
	})
	if err != nil {
		return a.mapErr("pedir el historial", err)
	}
	a.tableMu.Lock()
	defer a.tableMu.Unlock()
	res := a.conv.Convert(ctx, tbl)
	a.afterConvert(res)
	res.Typing = nil
	// El historial no es «nuevo»: solo mensajes (y lo que traiga) como lote, sin chats que pisen marcas.
	res.Chats = nil
	for _, b := range res.HistoryBatches(500) {
		batch := b
		a.Emit(model.Event{Type: model.EvHistoryBatch, History: &batch})
	}
	if len(res.Messages) == 0 {
		a.Emit(model.Event{Type: model.EvHistoryBatch, History: &model.HistoryBatch{Progress: -1}})
	}
	return nil
}

// ResolvePerson devuelve lo que se sabe de una persona por su id.
func (a *Adapter) ResolvePerson(ctx context.Context, peerNativeID string) (model.Person, error) {
	id, err := meta.ParseID(peerNativeID)
	if err != nil {
		return model.Person{}, fmt.Errorf("messenger: %w", err)
	}
	p := a.conv.PersonSnapshot(id)
	if p.PushName == "" {
		if ch, ok := a.conv.ChatSnapshot(id); ok && ch.Title != "" {
			p.PushName = ch.Title
		}
	}
	return p, nil
}

// ListPersons pide los contactos a Messenger y devuelve los que se conocen (para importar a Contactos).
func (a *Adapter) ListPersons(ctx context.Context) ([]model.Person, error) {
	cli, err := a.liveClient()
	if err != nil {
		return nil, err
	}
	tbl, err := cli.ExecuteTasks(ctx, &socket.GetContactsTask{Limit: 1000})
	if err != nil {
		return nil, a.mapErr("pedir los contactos", err)
	}
	a.tableMu.Lock()
	res := a.conv.Convert(ctx, tbl)
	a.tableMu.Unlock()
	self := a.SelfID()
	out := make([]model.Person, 0, len(res.Persons))
	seen := map[string]bool{}
	for _, p := range res.Persons {
		if p.NativeID == "" || p.NativeID == self || seen[p.NativeID] {
			continue
		}
		seen[p.NativeID] = true
		out = append(out, p)
	}
	return out, nil
}
