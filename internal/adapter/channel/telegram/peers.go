package telegram

// Caché de pares (08 §1, peers.go): id nativo ↔ tg.InputPeerClass. Telegram exige el access_hash de
// usuarios y canales para actuar sobre ellos; se aprende de todas las tg.Entities que llegan y se
// guarda en kv["tg.peer.<nativo>"] para sobrevivir a los reinicios. También recuerda los objetos
// completos vistos (nombres, títulos) mientras el adaptador vive.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gotd/td/tg"

	"dconnector/internal/core/port"
)

const prefPeer = "tg.peer."

// peerRec: lo que se guarda de cada par.
type peerRec struct {
	Hash int64 `json:"h,omitempty"`
	Mega bool  `json:"m,omitempty"` // supergrupo
}

type peerCache struct {
	kv     *kvStore
	selfID func() int64

	mu       sync.RWMutex
	users    map[int64]*tg.User
	chats    map[int64]*tg.Chat
	channels map[int64]*tg.Channel
	recs     map[string]peerRec // nativo → registro (memoria y disco)
	loaded   map[string]bool    // nativos ya buscados en disco
}

func newPeerCache(kv *kvStore, selfID func() int64) *peerCache {
	return &peerCache{
		kv: kv, selfID: selfID,
		users: map[int64]*tg.User{}, chats: map[int64]*tg.Chat{}, channels: map[int64]*tg.Channel{},
		recs: map[string]peerRec{}, loaded: map[string]bool{},
	}
}

var _ peerView = (*peerCache)(nil)

func (p *peerCache) User(id int64) (*tg.User, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	u, ok := p.users[id]
	return u, ok
}

func (p *peerCache) Chat(id int64) (*tg.Chat, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	c, ok := p.chats[id]
	return c, ok
}

func (p *peerCache) Channel(id int64) (*tg.Channel, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	c, ok := p.channels[id]
	return c, ok
}

// feed aprende los pares de unas entidades. Devuelve los usuarios nuevos o cuyo nombre cambió
// respecto a lo que se sabía (para anunciarlos como person.updated).
func (p *peerCache) feed(ctx context.Context, e tg.Entities) (changed []*tg.User) {
	toSave := map[string][]byte{}
	p.mu.Lock()
	for id, u := range e.Users {
		if u == nil {
			continue
		}
		old, had := p.users[id]
		if !u.Min || !had {
			p.users[id] = u
		}
		if !had || userSig(old) != userSig(u) {
			if !u.Min {
				changed = append(changed, u)
			}
		}
		if !u.Min && u.AccessHash != 0 {
			p.noteRec(userNative(id), peerRec{Hash: u.AccessHash}, toSave)
		}
	}
	for id, c := range e.Chats {
		if c != nil {
			p.chats[id] = c
		}
	}
	for id, c := range e.Channels {
		if c == nil {
			continue
		}
		old, had := p.channels[id]
		if !c.Min || !had {
			p.channels[id] = c
		}
		_ = old
		if !c.Min && c.AccessHash != 0 {
			p.noteRec(channelNative(id), peerRec{Hash: c.AccessHash, Mega: c.Megagroup || c.Gigagroup}, toSave)
		}
	}
	p.mu.Unlock()
	if len(toSave) > 0 {
		_ = p.kv.setMany(ctx, toSave)
	}
	return changed
}

// feedLists aprende de las listas crudas de una respuesta (users y chats).
func (p *peerCache) feedLists(ctx context.Context, users []tg.UserClass, chats []tg.ChatClass) []*tg.User {
	return p.feed(ctx, tg.EntitiesFrom(users, chats))
}

// noteRec registra un par si cambió (con el candado tomado).
func (p *peerCache) noteRec(native string, rec peerRec, toSave map[string][]byte) {
	if cur, ok := p.recs[native]; ok && cur == rec {
		return
	}
	p.recs[native] = rec
	b, _ := json.Marshal(rec)
	toSave[prefPeer+native] = b
}

// rec devuelve el registro de un par, buscándolo en disco si hace falta.
func (p *peerCache) rec(ctx context.Context, native string) (peerRec, bool) {
	p.mu.RLock()
	r, ok := p.recs[native]
	seen := p.loaded[native]
	p.mu.RUnlock()
	if ok || seen {
		return r, ok
	}
	v, found, err := p.kv.get(ctx, prefPeer+native)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		p.loaded[native] = true
	}
	if err != nil || !found {
		return peerRec{}, false
	}
	var rr peerRec
	if json.Unmarshal(v, &rr) != nil {
		return peerRec{}, false
	}
	p.recs[native] = rr
	return rr, true
}

// isMegagroup: si un canal es un supergrupo (lo sabe por el objeto visto o por el registro).
func (p *peerCache) isMegagroup(ctx context.Context, id int64) bool {
	if c, ok := p.Channel(id); ok {
		return c.Megagroup || c.Gigagroup
	}
	r, _ := p.rec(ctx, channelNative(id))
	return r.Mega
}

var errUnknownPeer = fmt.Errorf("Telegram todavía no conoce ese chat; espera a que se sincronicen los chats e inténtalo de nuevo: %w", port.ErrNotFound)

// input devuelve el tg.InputPeerClass de un id nativo.
func (p *peerCache) input(ctx context.Context, native string) (tg.InputPeerClass, error) {
	kind, id, ok := parseNative(native)
	if !ok {
		return nil, fmt.Errorf("telegram: identificador de chat no válido: %w", port.ErrInvalid)
	}
	switch kind {
	case kindChat:
		return &tg.InputPeerChat{ChatID: id}, nil
	case kindUser:
		if p.selfID != nil && p.selfID() == id {
			return &tg.InputPeerSelf{}, nil
		}
		r, ok := p.rec(ctx, native)
		if !ok || r.Hash == 0 {
			return nil, errUnknownPeer
		}
		return &tg.InputPeerUser{UserID: id, AccessHash: r.Hash}, nil
	default:
		r, ok := p.rec(ctx, native)
		if !ok || r.Hash == 0 {
			return nil, errUnknownPeer
		}
		return &tg.InputPeerChannel{ChannelID: id, AccessHash: r.Hash}, nil
	}
}

// inputUser devuelve el InputUser de un id nativo de usuario.
func (p *peerCache) inputUser(ctx context.Context, native string) (tg.InputUserClass, error) {
	kind, id, ok := parseNative(native)
	if !ok || kind != kindUser {
		return nil, fmt.Errorf("telegram: identificador de persona no válido: %w", port.ErrInvalid)
	}
	if p.selfID != nil && p.selfID() == id {
		return &tg.InputUserSelf{}, nil
	}
	r, ok := p.rec(ctx, native)
	if !ok || r.Hash == 0 {
		return nil, errUnknownPeer
	}
	return &tg.InputUser{UserID: id, AccessHash: r.Hash}, nil
}

// inputChannel devuelve el InputChannel de un id nativo de canal.
func (p *peerCache) inputChannel(ctx context.Context, native string) (tg.InputChannelClass, error) {
	kind, id, ok := parseNative(native)
	if !ok || kind != kindChannel {
		return nil, fmt.Errorf("telegram: identificador de canal no válido: %w", port.ErrInvalid)
	}
	r, ok := p.rec(ctx, native)
	if !ok || r.Hash == 0 {
		return nil, errUnknownPeer
	}
	return &tg.InputChannel{ChannelID: id, AccessHash: r.Hash}, nil
}

// userSig: firma de lo que se muestra de un usuario, para detectar cambios.
func userSig(u *tg.User) string {
	if u == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%t|%t", u.FirstName, u.LastName, usernameOf(u), u.Phone, u.Contact, u.Deleted)
}

// reset olvida todo lo recordado en memoria (al cerrar la sesión).
func (p *peerCache) reset() {
	p.mu.Lock()
	p.users = map[int64]*tg.User{}
	p.chats = map[int64]*tg.Chat{}
	p.channels = map[int64]*tg.Channel{}
	p.recs = map[string]peerRec{}
	p.loaded = map[string]bool{}
	p.mu.Unlock()
}
