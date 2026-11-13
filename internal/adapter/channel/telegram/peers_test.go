package telegram

import (
	"strconv"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func TestIDs(t *testing.T) {
	assert.Equal(t, "user:5", userNative(5))
	assert.Equal(t, "chat:6", chatNative(6))
	assert.Equal(t, "channel:7", channelNative(7))

	assert.Equal(t, "user:5", nativeOf(&tg.PeerUser{UserID: 5}))
	assert.Equal(t, "chat:6", nativeOf(&tg.PeerChat{ChatID: 6}))
	assert.Equal(t, "channel:7", nativeOf(&tg.PeerChannel{ChannelID: 7}))
	assert.Equal(t, "", nativeOf(nil))

	cases := map[string]struct {
		k  peerKind
		id int64
		ok bool
	}{
		"user:5":       {kindUser, 5, true},
		"chat:6":       {kindChat, 6, true},
		"channel:7":    {kindChannel, 7, true},
		"channel:-100": {kindNone, 0, false},
		"user:0":       {kindNone, 0, false},
		"user:abc":     {kindNone, 0, false},
		"foo:1":        {kindNone, 0, false},
		"":             {kindNone, 0, false},
		"5":            {kindNone, 0, false},
	}
	for in, want := range cases {
		k, id, ok := parseNative(in)
		assert.Equal(t, want.ok, ok, in)
		assert.Equal(t, want.k, k, in)
		assert.Equal(t, want.id, id, in)
	}
	p, ok := peerFromNative("channel:7")
	require.True(t, ok)
	assert.Equal(t, int64(7), p.(*tg.PeerChannel).ChannelID)
	_, ok = peerFromNative("raro")
	assert.False(t, ok)
	assert.Equal(t, "…4567", shortID("user:1234567"))
	assert.Equal(t, "abc", shortID("abc"))
}

func TestChatKindOf(t *testing.T) {
	v := entitiesView{testEntities()}
	assert.Equal(t, model.ChatDirect, chatKindOf(&tg.PeerUser{UserID: otherUID}, v, selfUID))
	assert.Equal(t, model.ChatSelf, chatKindOf(&tg.PeerUser{UserID: selfUID}, v, selfUID))
	assert.Equal(t, model.ChatDirect, chatKindOf(&tg.PeerUser{UserID: selfUID}, v, 0), "sin conocer mi id es un directo")
	assert.Equal(t, model.ChatGroup, chatKindOf(&tg.PeerChat{ChatID: 7}, v, selfUID))
	assert.Equal(t, model.ChatGroup, chatKindOf(&tg.PeerChannel{ChannelID: 30}, v, selfUID), "supergrupo")
	assert.Equal(t, model.ChatChannel, chatKindOf(&tg.PeerChannel{ChannelID: 20}, v, selfUID), "canal de difusión")
	assert.Equal(t, model.ChatChannel, chatKindOf(&tg.PeerChannel{ChannelID: 99}, v, selfUID), "desconocido")
	giga := entitiesView{tg.Entities{Channels: map[int64]*tg.Channel{5: {ID: 5, Gigagroup: true}}}}
	assert.Equal(t, model.ChatGroup, chatKindOf(&tg.PeerChannel{ChannelID: 5}, giga, 1))
}

func TestChainAndEmptyViews(t *testing.T) {
	v := chainView{emptyView{}, entitiesView{testEntities()}}
	u, ok := v.User(otherUID)
	require.True(t, ok)
	assert.Equal(t, "Ana", u.FirstName)
	_, ok = v.User(555)
	assert.False(t, ok)
	_, ok = v.Chat(7)
	assert.True(t, ok)
	_, ok = v.Channel(20)
	assert.True(t, ok)
	_, ok = (emptyView{}).Chat(1)
	assert.False(t, ok)
	f, ok := v.UserByUsername("LUIS")
	require.True(t, ok, "los @usuarios no distinguen mayúsculas")
	assert.Equal(t, int64(101), f.ID)
	assert.Equal(t, "Ana Pérez", nameOfNative(v, "user:100"))
	assert.Equal(t, "Familia", nameOfNative(v, "chat:7"))
	assert.Equal(t, "Noticias", nameOfNative(v, "channel:20"))
	assert.Equal(t, "", nameOfNative(v, "channel:99"))
	assert.Equal(t, "", nameOfNative(v, "raro"))
}

func TestPeerCacheInputsAndPersistence(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	_, err := a.peers.input(bg, "user:100")
	assert.ErrorIs(t, err, port.ErrNotFound, "sin access_hash no se puede actuar")
	_, err = a.peers.input(bg, "basura")
	assert.ErrorIs(t, err, port.ErrInvalid)

	a.peers.feed(bg, testEntities())

	in, err := a.peers.input(bg, "user:100")
	require.NoError(t, err)
	assert.Equal(t, &tg.InputPeerUser{UserID: otherUID, AccessHash: 11}, in)
	in, err = a.peers.input(bg, "user:1")
	require.NoError(t, err)
	assert.IsType(t, &tg.InputPeerSelf{}, in, "yo soy InputPeerSelf")
	in, err = a.peers.input(bg, "chat:7")
	require.NoError(t, err)
	assert.Equal(t, &tg.InputPeerChat{ChatID: 7}, in, "los grupos no llevan hash")
	in, err = a.peers.input(bg, "channel:20")
	require.NoError(t, err)
	assert.Equal(t, &tg.InputPeerChannel{ChannelID: 20, AccessHash: 21}, in)

	iu, err := a.peers.inputUser(bg, "user:101")
	require.NoError(t, err)
	assert.Equal(t, &tg.InputUser{UserID: 101, AccessHash: 12}, iu)
	iu, _ = a.peers.inputUser(bg, "user:1")
	assert.IsType(t, &tg.InputUserSelf{}, iu)
	_, err = a.peers.inputUser(bg, "chat:7")
	assert.ErrorIs(t, err, port.ErrInvalid)
	ic, err := a.peers.inputChannel(bg, "channel:30")
	require.NoError(t, err)
	assert.Equal(t, &tg.InputChannel{ChannelID: 30, AccessHash: 31}, ic)
	_, err = a.peers.inputChannel(bg, "user:100")
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, err = a.peers.inputChannel(bg, "channel:99")
	assert.ErrorIs(t, err, port.ErrNotFound)

	assert.True(t, a.peers.isMegagroup(bg, 30))
	assert.False(t, a.peers.isMegagroup(bg, 20))

	// Los hashes sobreviven a un reinicio: otra caché sobre la misma base.
	fresh := newPeerCache(a.kv, a.selfID.Load)
	in, err = fresh.input(bg, "user:100")
	require.NoError(t, err)
	assert.Equal(t, &tg.InputPeerUser{UserID: otherUID, AccessHash: 11}, in)
	in, err = fresh.input(bg, "channel:20")
	require.NoError(t, err)
	assert.Equal(t, int64(21), in.(*tg.InputPeerChannel).AccessHash)
	assert.True(t, fresh.isMegagroup(bg, 30), "se recuerda si es supergrupo")
	_, err = fresh.input(bg, "user:555")
	assert.ErrorIs(t, err, port.ErrNotFound)

	// Un usuario «mínimo» no pisa el hash bueno.
	a.peers.feed(bg, tg.Entities{Users: map[int64]*tg.User{otherUID: {ID: otherUID, FirstName: "Ana", Min: true, AccessHash: 999}}})
	in, _ = a.peers.input(bg, "user:100")
	assert.Equal(t, int64(11), in.(*tg.InputPeerUser).AccessHash)

	a.peers.reset()
	_, ok := a.peers.User(otherUID)
	assert.False(t, ok)
}

func TestQuoteCacheIsBounded(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	for i := 1; i <= quoteCacheMax+50; i++ {
		a.local.noteQuote(model.Message{ChatNativeID: "user:100", NativeID: strconv.Itoa(i), Text: "m"})
	}
	_, _, ok := a.local.quoteLookup("user:100", 1)
	assert.False(t, ok, "los más viejos se descartan")
	_, _, ok = a.local.quoteLookup("user:100", quoteCacheMax+50)
	assert.True(t, ok)
	a.local.reset()
	_, _, ok = a.local.quoteLookup("user:100", quoteCacheMax+50)
	assert.False(t, ok)
}

func TestQuoteLookupFromStore(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	createMessagesTable(t, a.deps.DB)
	insertMessage(t, a.deps.DB, "user:100", "7", 0, "image", "", "sent")
	pv, snd, ok := a.local.quoteLookup("user:100", 7)
	require.True(t, ok)
	assert.Equal(t, "📷 Foto", pv)
	assert.Equal(t, "Ana", snd)
	_, _, ok = a.local.quoteLookup("user:100", 8)
	assert.False(t, ok)
}

func TestOldestMessageID(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	assert.Equal(t, 0, a.oldestMessageID(bg, "user:100"), "sin tabla: desde el más reciente")
	createMessagesTable(t, a.deps.DB)
	assert.Equal(t, 0, a.oldestMessageID(bg, "user:100"))
	for _, id := range []string{"120", "9", "55"} {
		insertMessage(t, a.deps.DB, "user:100", id, 0, "text", "x", "sent")
	}
	assert.Equal(t, 9, a.oldestMessageID(bg, "user:100"), "orden numérico, no de texto")
}

func TestPollRecordPersistence(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	rec := recordOfPoll(samplePoll())
	a.local.putPoll(bg, "user:100", 40, 900, rec)
	got, ok := a.local.getPoll(bg, "user:100", 40)
	require.True(t, ok)
	assert.Equal(t, rec, got, "los bytes de las opciones sobreviven al JSON")
	chat, id, ok := a.local.pollRef(bg, 900)
	require.True(t, ok)
	assert.Equal(t, "user:100", chat)
	assert.Equal(t, 40, id)
	_, _, ok = a.local.pollRef(bg, 1)
	assert.False(t, ok)
	_, ok = a.local.getPoll(bg, "user:100", 41)
	assert.False(t, ok)
}
