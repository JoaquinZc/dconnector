package telegram

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram/updates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

var bg = context.Background()

// openTestDB abre un account.db temporal como lo hace el almacén: driver "sqlite" de modernc.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "account.db")) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestKV(t *testing.T) *kvStore {
	t.Helper()
	kv := newKV(openTestDB(t))
	require.NoError(t, kv.ensure(bg))
	return kv
}

func TestKVBasics(t *testing.T) {
	kv := newTestKV(t)
	_, ok, err := kv.get(bg, "tg.a")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, kv.set(bg, "tg.a", []byte("1")))
	require.NoError(t, kv.set(bg, "tg.a", []byte("2"))) // reemplaza
	require.NoError(t, kv.set(bg, "tg.empty", nil))     // un valor vacío no viola NOT NULL
	v, ok, err := kv.get(bg, "tg.a")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []byte("2"), v)

	require.NoError(t, kv.setMany(bg, map[string][]byte{"tg.x.1": []byte("a"), "tg.x.2": []byte("b"), "other": []byte("c")}))
	var keys []string
	require.NoError(t, kv.scanPrefix(bg, "tg.x.", func(k string, _ []byte) error { keys = append(keys, k); return nil }))
	assert.Equal(t, []string{"tg.x.1", "tg.x.2"}, keys)

	require.NoError(t, kv.delPrefix(bg, "tg.x."))
	_, ok, _ = kv.get(bg, "tg.x.1")
	assert.False(t, ok)
	_, ok, _ = kv.get(bg, "other")
	assert.True(t, ok, "lo que no empieza por el prefijo no se toca")

	require.NoError(t, kv.wipe(bg))
	_, ok, _ = kv.get(bg, "tg.a")
	assert.False(t, ok)
	_, ok, _ = kv.get(bg, "other")
	assert.True(t, ok, "wipe solo borra las claves tg.")
	assert.Equal(t, "tg.y", prefixEnd("tg.x"))
}

func TestKVSessionStorage(t *testing.T) {
	kv := newTestKV(t)
	var st session.Storage = &kvSession{kv: kv}

	_, err := st.LoadSession(bg)
	assert.ErrorIs(t, err, session.ErrNotFound)

	require.NoError(t, st.StoreSession(bg, []byte(`{"secreto":1}`)))
	got, err := st.LoadSession(bg)
	require.NoError(t, err)
	assert.Equal(t, []byte(`{"secreto":1}`), got)

	require.NoError(t, st.StoreSession(bg, []byte("nueva")))
	got, _ = st.LoadSession(bg)
	assert.Equal(t, []byte("nueva"), got)

	ks := &kvSession{kv: kv}
	assert.True(t, ks.hasSession(bg))
	require.NoError(t, kv.del(bg, keySession))
	assert.False(t, ks.hasSession(bg))
	_, err = st.LoadSession(bg)
	assert.ErrorIs(t, err, session.ErrNotFound)

	// Una sesión vacía cuenta como ausente.
	require.NoError(t, st.StoreSession(bg, []byte{}))
	_, err = st.LoadSession(bg)
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestKVUpdatesState(t *testing.T) {
	kv := newTestKV(t)
	var s updates.StateStorage = &kvUpdatesState{kv: kv}
	const u, other = int64(1), int64(10)

	_, found, err := s.GetState(bg, u)
	require.NoError(t, err)
	assert.False(t, found)
	// Sin estado, las escrituras parciales fallan (como el almacén en memoria).
	assert.Error(t, s.SetPts(bg, u, 5))
	assert.Error(t, s.SetChannelPts(bg, u, 99, 5))
	assert.Error(t, s.ForEachChannels(bg, u, func(context.Context, int64, int) error { return nil }))
	_, found, _ = s.GetChannelPts(bg, u, 99)
	assert.False(t, found)

	require.NoError(t, s.SetState(bg, u, updates.State{Pts: 1, Qts: 2, Date: 3, Seq: 4}))
	st, found, err := s.GetState(bg, u)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, updates.State{Pts: 1, Qts: 2, Date: 3, Seq: 4}, st)

	require.NoError(t, s.SetPts(bg, u, 11))
	require.NoError(t, s.SetQts(bg, u, 12))
	require.NoError(t, s.SetDate(bg, u, 13))
	require.NoError(t, s.SetSeq(bg, u, 14))
	st, _, _ = s.GetState(bg, u)
	assert.Equal(t, updates.State{Pts: 11, Qts: 12, Date: 13, Seq: 14}, st)
	require.NoError(t, s.SetDateSeq(bg, u, 23, 24))
	st, _, _ = s.GetState(bg, u)
	assert.Equal(t, updates.State{Pts: 11, Qts: 12, Date: 23, Seq: 24}, st)

	// Pts de canales; el usuario 10 comparte prefijo numérico con el 1 y no debe mezclarse.
	require.NoError(t, s.SetState(bg, other, updates.State{Pts: 100}))
	require.NoError(t, s.SetChannelPts(bg, u, 7, 70))
	require.NoError(t, s.SetChannelPts(bg, u, 8, 80))
	require.NoError(t, s.SetChannelPts(bg, other, 9, 90))
	pts, found, err := s.GetChannelPts(bg, u, 7)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 70, pts)
	_, found, _ = s.GetChannelPts(bg, u, 9)
	assert.False(t, found)

	var got []int64
	require.NoError(t, s.ForEachChannels(bg, u, func(_ context.Context, id int64, pts int) error {
		got = append(got, id*1000+int64(pts))
		return nil
	}))
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	assert.Equal(t, []int64{7070, 8080}, got)

	// SetState olvida los canales de ese usuario (y solo de ese).
	require.NoError(t, s.SetState(bg, u, updates.State{Pts: 50}))
	_, found, _ = s.GetChannelPts(bg, u, 7)
	assert.False(t, found)
	pts, found, _ = s.GetChannelPts(bg, other, 9)
	assert.True(t, found)
	assert.Equal(t, 90, pts)
}

func TestKVAccessHasher(t *testing.T) {
	kv := newTestKV(t)
	h := &kvAccessHasher{kv: kv}
	_, found, err := h.GetChannelAccessHash(bg, 1, 2)
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, h.SetChannelAccessHash(bg, 1, 2, 777))
	require.NoError(t, h.SetChannelAccessHash(bg, 1, 2, 777)) // sin cambios: sin escribir
	v, found, err := h.GetChannelAccessHash(bg, 1, 2)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(777), v)

	require.NoError(t, h.SetUserAccessHash(bg, 1, 5, -88))
	uv, found, _ := h.GetUserAccessHash(bg, 1, 5)
	assert.True(t, found)
	assert.Equal(t, int64(-88), uv)
	_, found, _ = h.GetUserAccessHash(bg, 1, 2)
	assert.False(t, found, "usuarios y canales no se mezclan")
}
