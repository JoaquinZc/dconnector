package telegram

// Almacenes persistentes de gotd sobre la tabla kv del account.db (08 §1):
//   - kvSession: session.Storage (la sesión MTProto, dato muy sensible).
//   - kvUpdatesState: updates.StateStorage (pts, qts, date, seq y pts de cada canal).
//   - kvAccessHasher: updates.ChannelAccessHasher y updates.UserAccessHasher.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram/updates"
)

const (
	keySession = "tg.session"
	prefState  = "tg.state."
	prefChPts  = "tg.chpts."
	prefHash   = "tg.hash."
	prefUHash  = "tg.uhash."
)

// ---------------------------------------------------------------------------------------------
// Sesión

// kvSession implementa session.Storage sobre kv["tg.session"].
type kvSession struct {
	kv *kvStore
}

var _ session.Storage = (*kvSession)(nil)

func (s *kvSession) LoadSession(ctx context.Context) ([]byte, error) {
	v, ok, err := s.kv.get(ctx, keySession)
	if err != nil {
		return nil, err
	}
	if !ok || len(v) == 0 {
		return nil, session.ErrNotFound
	}
	return v, nil
}

func (s *kvSession) StoreSession(ctx context.Context, data []byte) error {
	return s.kv.set(ctx, keySession, data)
}

// hasSession dice si hay una sesión guardada.
func (s *kvSession) hasSession(ctx context.Context) bool {
	v, ok, err := s.kv.get(ctx, keySession)
	return err == nil && ok && len(v) > 0
}

// ---------------------------------------------------------------------------------------------
// Estado de actualizaciones

// kvUpdatesState implementa updates.StateStorage.
type kvUpdatesState struct {
	kv *kvStore
	mu sync.Mutex
}

var _ updates.StateStorage = (*kvUpdatesState)(nil)

var errStateNotFound = errors.New("estado de actualizaciones no encontrado")

func stateKey(userID int64) string { return prefState + strconv.FormatInt(userID, 10) }

func chPtsKey(userID, channelID int64) string {
	return prefChPts + strconv.FormatInt(userID, 10) + "." + strconv.FormatInt(channelID, 10)
}

func (s *kvUpdatesState) load(ctx context.Context, userID int64) (updates.State, bool, error) {
	v, ok, err := s.kv.get(ctx, stateKey(userID))
	if err != nil || !ok {
		return updates.State{}, false, err
	}
	var st updates.State
	if err := json.Unmarshal(v, &st); err != nil {
		return updates.State{}, false, fmt.Errorf("estado dañado: %w", err)
	}
	return st, true, nil
}

func (s *kvUpdatesState) save(ctx context.Context, userID int64, st updates.State) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.kv.set(ctx, stateKey(userID), b)
}

func (s *kvUpdatesState) GetState(ctx context.Context, userID int64) (updates.State, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(ctx, userID)
}

// SetState reemplaza el estado y olvida los pts de canales (como el almacén en memoria de gotd).
func (s *kvUpdatesState) SetState(ctx context.Context, userID int64, st updates.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.save(ctx, userID, st); err != nil {
		return err
	}
	return s.kv.delPrefix(ctx, prefChPts+strconv.FormatInt(userID, 10)+".")
}

func (s *kvUpdatesState) update(ctx context.Context, userID int64, f func(*updates.State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok, err := s.load(ctx, userID)
	if err != nil {
		return err
	}
	if !ok {
		return errStateNotFound
	}
	f(&st)
	return s.save(ctx, userID, st)
}

func (s *kvUpdatesState) SetPts(ctx context.Context, userID int64, pts int) error {
	return s.update(ctx, userID, func(st *updates.State) { st.Pts = pts })
}

func (s *kvUpdatesState) SetQts(ctx context.Context, userID int64, qts int) error {
	return s.update(ctx, userID, func(st *updates.State) { st.Qts = qts })
}

func (s *kvUpdatesState) SetDate(ctx context.Context, userID int64, date int) error {
	return s.update(ctx, userID, func(st *updates.State) { st.Date = date })
}

func (s *kvUpdatesState) SetSeq(ctx context.Context, userID int64, seq int) error {
	return s.update(ctx, userID, func(st *updates.State) { st.Seq = seq })
}

func (s *kvUpdatesState) SetDateSeq(ctx context.Context, userID int64, date, seq int) error {
	return s.update(ctx, userID, func(st *updates.State) { st.Date, st.Seq = date, seq })
}

func (s *kvUpdatesState) GetChannelPts(ctx context.Context, userID, channelID int64) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok, err := s.load(ctx, userID); err != nil || !ok {
		return 0, false, err
	}
	v, ok, err := s.kv.get(ctx, chPtsKey(userID, channelID))
	if err != nil || !ok {
		return 0, false, err
	}
	pts, err := strconv.Atoi(string(v))
	if err != nil {
		return 0, false, nil
	}
	return pts, true, nil
}

func (s *kvUpdatesState) SetChannelPts(ctx context.Context, userID, channelID int64, pts int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok, err := s.load(ctx, userID); err != nil {
		return err
	} else if !ok {
		return errStateNotFound
	}
	return s.kv.set(ctx, chPtsKey(userID, channelID), []byte(strconv.Itoa(pts)))
}

func (s *kvUpdatesState) ForEachChannels(ctx context.Context, userID int64, f func(ctx context.Context, channelID int64, pts int) error) error {
	s.mu.Lock()
	if _, ok, err := s.load(ctx, userID); err != nil {
		s.mu.Unlock()
		return err
	} else if !ok {
		s.mu.Unlock()
		return errStateNotFound
	}
	type item struct {
		id  int64
		pts int
	}
	var items []item
	prefix := prefChPts + strconv.FormatInt(userID, 10) + "."
	err := s.kv.scanPrefix(ctx, prefix, func(key string, v []byte) error {
		id, perr := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64)
		pts, qerr := strconv.Atoi(string(v))
		if perr == nil && qerr == nil {
			items = append(items, item{id, pts})
		}
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := f(ctx, it.id, it.pts); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Access hashes para el gestor de huecos

// kvAccessHasher implementa updates.ChannelAccessHasher y updates.UserAccessHasher.
type kvAccessHasher struct {
	kv *kvStore
}

var (
	_ updates.ChannelAccessHasher = (*kvAccessHasher)(nil)
	_ updates.UserAccessHasher    = (*kvAccessHasher)(nil)
)

func hashKey(prefix string, userID, id int64) string {
	return prefix + strconv.FormatInt(userID, 10) + "." + strconv.FormatInt(id, 10)
}

func (h *kvAccessHasher) get(ctx context.Context, key string) (int64, bool, error) {
	v, ok, err := h.kv.get(ctx, key)
	if err != nil || !ok {
		return 0, false, err
	}
	n, err := strconv.ParseInt(string(v), 10, 64)
	if err != nil {
		return 0, false, nil
	}
	return n, true, nil
}

func (h *kvAccessHasher) set(ctx context.Context, key string, hash int64) error {
	if old, ok, err := h.get(ctx, key); err == nil && ok && old == hash {
		return nil // sin escritura si no cambió
	}
	return h.kv.set(ctx, key, []byte(strconv.FormatInt(hash, 10)))
}

func (h *kvAccessHasher) SetChannelAccessHash(ctx context.Context, userID, channelID, accessHash int64) error {
	return h.set(ctx, hashKey(prefHash, userID, channelID), accessHash)
}

func (h *kvAccessHasher) GetChannelAccessHash(ctx context.Context, userID, channelID int64) (int64, bool, error) {
	return h.get(ctx, hashKey(prefHash, userID, channelID))
}

func (h *kvAccessHasher) SetUserAccessHash(ctx context.Context, userID, targetUserID, accessHash int64) error {
	return h.set(ctx, hashKey(prefUHash, userID, targetUserID), accessHash)
}

func (h *kvAccessHasher) GetUserAccessHash(ctx context.Context, userID, targetUserID int64) (int64, bool, error) {
	return h.get(ctx, hashKey(prefUHash, userID, targetUserID))
}
