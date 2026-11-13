package ws

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Alcance de contactos de un destino (model.ContactScope): de qué contactos recibe eventos y sobre
// cuáles puede actuar. Un chat se vincula a un contacto por identidad (cuenta + el otro lado del
// chat directo) o, si no, por Chat.ContactID; el contacto pertenece a listas. La pertenencia se
// guarda en una caché en memoria con TTL corto para no consultar la base en cada evento.

const (
	// scopeCacheTTL: cuánto se recuerda la pertenencia a listas de una persona y la lista de listas
	// que existen. Un cambio de listas tarda como mucho esto en notarse.
	scopeCacheTTL = 30 * time.Second
	// scopeCacheMax: tope de personas recordadas; al pasarlo se vacía la caché.
	scopeCacheMax = 4096
)

type peerEntry struct {
	ids []string
	at  time.Time
}

// scopeCache: la caché del alcance de contactos. Su valor cero sirve.
type scopeCache struct {
	mu      sync.Mutex
	now     func() time.Time // ajustable en pruebas
	listsAt time.Time
	lists   map[string]struct{}
	peers   map[string]peerEntry
}

func (c *scopeCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// resetScopeCache olvida todo lo recordado (cambió un destino o las listas).
func (m *Manager) resetScopeCache() {
	m.scope.mu.Lock()
	m.scope.lists, m.scope.peers = nil, nil
	m.scope.listsAt = time.Time{}
	m.scope.mu.Unlock()
}

// existingLists devuelve los ids de las listas que existen (con caché).
func (m *Manager) existingLists(ctx context.Context) (map[string]struct{}, error) {
	c := &m.scope
	c.mu.Lock()
	if c.lists != nil && c.clock().Sub(c.listsAt) < scopeCacheTTL {
		l := c.lists
		c.mu.Unlock()
		return l, nil
	}
	c.mu.Unlock()
	all, err := m.app.ListLists(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(all))
	for _, l := range all {
		set[l.ID] = struct{}{}
	}
	c.mu.Lock()
	c.lists, c.listsAt = set, c.clock()
	c.mu.Unlock()
	return set, nil
}

// peerLists devuelve las listas (que existen) del contacto de una persona: se busca por identidad
// y, si no hay, por contactID. Sin contacto devuelve nil. Un error de la base no se recuerda.
func (m *Manager) peerLists(ctx context.Context, acc model.AccountID, peer, contactID string) ([]string, error) {
	key := string(acc) + "\x00" + peer + "\x00" + contactID
	c := &m.scope
	c.mu.Lock()
	if e, ok := c.peers[key]; ok && c.clock().Sub(e.at) < scopeCacheTTL {
		c.mu.Unlock()
		return e.ids, nil
	}
	c.mu.Unlock()

	var (
		ct    model.Contact
		found bool
	)
	if peer != "" {
		got, err := m.app.FindContactByIdentity(ctx, acc, peer)
		switch {
		case err == nil:
			ct, found = got, true
		case !errors.Is(err, port.ErrNotFound):
			return nil, err
		}
	}
	if !found && contactID != "" {
		got, err := m.app.GetContact(ctx, contactID)
		switch {
		case err == nil:
			ct, found = got, true
		case !errors.Is(err, port.ErrNotFound):
			return nil, err
		}
	}
	var ids []string
	if found && len(ct.ListIDs) > 0 {
		exist, err := m.existingLists(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range ct.ListIDs {
			if _, ok := exist[id]; ok {
				ids = append(ids, id)
			}
		}
	}
	c.mu.Lock()
	if c.peers == nil || len(c.peers) >= scopeCacheMax {
		c.peers = make(map[string]peerEntry)
	}
	c.peers[key] = peerEntry{ids: ids, at: c.clock()}
	c.mu.Unlock()
	return ids, nil
}

// chatContactLists devuelve las listas del contacto vinculado a un chat. chat puede ser nil (chat
// que aún no está en el almacén: se busca por su id nativo). Los grupos, los canales y el chat
// contigo mismo no son contactos: sin listas.
func (m *Manager) chatContactLists(ctx context.Context, acc model.AccountID, native string, chat *model.Chat) ([]string, error) {
	peer, contactID := native, ""
	if chat != nil {
		switch chat.Kind {
		case model.ChatGroup, model.ChatChannel, model.ChatSelf:
			return nil, nil
		}
		if chat.PeerNativeID != "" {
			peer = chat.PeerNativeID
		}
		contactID = chat.ContactID
	}
	return m.peerLists(ctx, acc, peer, contactID)
}

// storedChat lee un chat del almacén de la cuenta; nil si no existe o no se puede leer.
func (m *Manager) storedChat(ctx context.Context, acc model.AccountID, native string) *model.Chat {
	st, err := m.svc.Accounts.Store(acc)
	if err != nil {
		return nil
	}
	c, err := st.GetChat(ctx, native)
	if err != nil {
		return nil
	}
	return &c
}

// contactAllowed: el contacto pasa el alcance (por sus listas, solo las que existen).
func (m *Manager) contactAllowed(ctx context.Context, cs model.ContactScope, ct model.Contact) (bool, error) {
	if !cs.Restricts() {
		return true, nil
	}
	exist, err := m.existingLists(ctx)
	if err != nil {
		return false, err
	}
	var ids []string
	for _, id := range ct.ListIDs {
		if _, ok := exist[id]; ok {
			ids = append(ids, id)
		}
	}
	return cs.Allows(ids), nil
}

// errOutOfScope: el chat o el contacto queda fuera del alcance del destino.
func errOutOfScope(what string) error {
	return fmt.Errorf("%w: %s no pasa el filtro de contactos de este destino", errForbidden, what)
}

// checkChatScope comprueba que un chat pase el alcance de contactos del destino (los comandos
// sobre un chat fuera de alcance reciben `forbidden`, como con el filtro de cuentas).
func (s *session) checkChatScope(ctx context.Context, acc model.AccountID, native string) error {
	cs := s.c.dst.Filter.ContactScope
	if !cs.Restricts() {
		return nil
	}
	ids, err := s.m.chatContactLists(ctx, acc, native, s.m.storedChat(ctx, acc, native))
	if err != nil {
		return fmt.Errorf("no se pudo comprobar el filtro de contactos: %w", err)
	}
	if !cs.Allows(ids) {
		return errOutOfScope("el chat")
	}
	return nil
}

// contactScopeAllows decide si un evento pasa el alcance de contactos. account.state y
// account.capabilities no llevan datos de contactos y siempre pasan; person.updated y los estados
// se evalúan por la persona; el resto, por el chat. Si no se puede consultar, no pasa.
func (l *lookups) contactScopeAllows(cs model.ContactScope, ev model.Event) bool {
	var (
		ids []string
		err error
	)
	switch ev.Type {
	case model.EvAccountStatus, model.EvCapabilities:
		return true
	case model.EvPerson:
		if ev.Person == nil {
			return false
		}
		ids, err = l.m.peerLists(l.ctx, ev.AccountID, ev.Person.NativeID, "")
	case model.EvStatusUpsert:
		if ev.Status == nil {
			return false
		}
		ids, err = l.m.peerLists(l.ctx, ev.AccountID, ev.Status.AuthorNativeID, "")
	case model.EvStatusView:
		if ev.StatusView == nil {
			return false
		}
		ids, err = l.m.peerLists(l.ctx, ev.AccountID, ev.StatusView.ViewerID, "")
	default:
		acc, native, _ := eventChat(ev)
		if native == "" {
			break // sin chat: sin contacto
		}
		chat := ev.Chat
		if ev.Type != model.EvChatUpsert || chat == nil {
			chat = l.chat(acc, native)
		}
		ids, err = l.m.chatContactLists(l.ctx, acc, native, chat)
	}
	if err != nil {
		l.m.log.Warn("no se pudo comprobar el filtro de contactos; el evento no se entrega", "err", err)
		return false
	}
	return cs.Allows(ids)
}
