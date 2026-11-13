package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

var bg = context.Background()

func mkChat(native string, kind model.ChatKind, title string, last int64) model.Chat {
	return model.Chat{NativeID: native, Kind: kind, Title: title, LastMessageAt: last, LastPreview: "p" + native}
}

func chatIDs(cs []model.Chat) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.NativeID)
	}
	return out
}

func TestChatsUpsertGetPatch(t *testing.T) {
	s := newTestAccount(t)
	c := model.Chat{
		NativeID: "593@s.whatsapp.net", Kind: model.ChatDirect, Title: "Ana", AvatarURL: "/avatar/wa-test/ana.jpg",
		UnreadCount: 2, LastMessageAt: 100, LastPreview: "hola", LastFromMe: true, Pinned: true, MutedUntil: -1,
		ReadOnly: true, PeerNativeID: "593@s.whatsapp.net",
	}
	require.NoError(t, s.UpsertChat(bg, c))
	got, err := s.GetChat(bg, c.NativeID)
	require.NoError(t, err)
	c.Key = model.NewChatKey(testAcc, c.NativeID)
	c.AccountID = testAcc
	c.Channel = model.ChannelWhatsApp
	require.Equal(t, c, got)

	// Un título o avatar vacío no borra; un chat más antiguo no pisa la vista previa.
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: c.NativeID, Kind: model.ChatDirect, LastMessageAt: 50, LastPreview: "viejo", UnreadCount: 0}))
	got, _ = s.GetChat(bg, c.NativeID)
	require.Equal(t, "Ana", got.Title)
	require.Equal(t, "/avatar/wa-test/ana.jpg", got.AvatarURL)
	require.Equal(t, "hola", got.LastPreview)
	require.Equal(t, int64(100), got.LastMessageAt)
	require.Equal(t, 0, got.UnreadCount) // lo demás sí se reemplaza
	require.False(t, got.Pinned)
	// Uno más nuevo sí.
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: c.NativeID, Kind: model.ChatDirect, LastMessageAt: 200, LastPreview: "nuevo", LastFromMe: false}))
	got, _ = s.GetChat(bg, c.NativeID)
	require.Equal(t, "nuevo", got.LastPreview)
	require.Equal(t, int64(200), got.LastMessageAt)

	// PatchChat toca solo lo pedido.
	require.NoError(t, s.PatchChat(bg, model.Chat{NativeID: c.NativeID, Title: "Ana M.", UnreadCount: 9, Archived: true, MutedUntil: 5},
		[]string{"title", "archived"}))
	got, _ = s.GetChat(bg, c.NativeID)
	require.Equal(t, "Ana M.", got.Title)
	require.True(t, got.Archived)
	require.Equal(t, 0, got.UnreadCount)
	require.Equal(t, int64(0), got.MutedUntil)
	require.NoError(t, s.PatchChat(bg, model.Chat{NativeID: c.NativeID, UnreadCount: 4, Pinned: true, AvatarURL: "/avatar/wa-test/b.jpg"},
		[]string{"unreadCount", "pinned", "avatarUrl"}))
	got, _ = s.GetChat(bg, c.NativeID)
	require.Equal(t, 4, got.UnreadCount)
	require.True(t, got.Pinned)
	require.Equal(t, "/avatar/wa-test/b.jpg", got.AvatarURL)
	require.NoError(t, s.PatchChat(bg, model.Chat{NativeID: c.NativeID}, nil))

	require.ErrorIs(t, s.PatchChat(bg, model.Chat{NativeID: "nada"}, []string{"title"}), port.ErrNotFound)
	require.ErrorIs(t, s.PatchChat(bg, model.Chat{NativeID: c.NativeID}, []string{"no_existe"}), port.ErrInvalid)
	_, err = s.GetChat(bg, "nada")
	require.ErrorIs(t, err, port.ErrNotFound)
	require.ErrorIs(t, s.UpsertChat(bg, model.Chat{}), port.ErrInvalid)
}

func TestListChatsRules(t *testing.T) {
	s := newTestAccount(t)
	chats := []model.Chat{
		mkChat("a", model.ChatDirect, "Ana López", 100),
		mkChat("b", model.ChatGroup, "Grupo Familia", 300),
		mkChat("c", model.ChatDirect, "Beto", 200),
		mkChat("d", model.ChatGroup, "Archivado grupo", 400),
		mkChat("e", model.ChatDirect, "Fijado", 50),
		mkChat("f", model.ChatDirect, "100% real_1", 10),
	}
	chats[2].UnreadCount = 3
	chats[3].Archived = true
	chats[4].Pinned = true
	for _, c := range chats {
		require.NoError(t, s.UpsertChat(bg, c))
	}

	l, err := s.ListChats(bg, model.ChatFilter{})
	require.NoError(t, err)
	require.Equal(t, []string{"e", "b", "c", "a", "f"}, chatIDs(l)) // fijados primero; archivados fuera
	require.Equal(t, model.ChannelWhatsApp, l[0].Channel)

	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "all"})
	require.Equal(t, []string{"e", "b", "c", "a", "f"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "unread"})
	require.Equal(t, []string{"c"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "groups"})
	require.Equal(t, []string{"d", "b"}, chatIDs(l)) // literal: kind='group', incluidos archivados
	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "archived"})
	require.Equal(t, []string{"d"}, chatIDs(l))

	// Texto: contiene, sin distinguir mayúsculas; los comodines del usuario no actúan como tales.
	l, _ = s.ListChats(bg, model.ChatFilter{Query: "ANA"})
	require.Equal(t, []string{"a"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Query: "100%"})
	require.Equal(t, []string{"f"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Query: "real_"})
	require.Equal(t, []string{"f"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Query: "%"})
	require.Equal(t, []string{"f"}, chatIDs(l))

	// Paginación por LastMessageAt y límite.
	l, _ = s.ListChats(bg, model.ChatFilter{Limit: 2})
	require.Equal(t, []string{"e", "b"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Before: 200, Limit: 10})
	require.Equal(t, []string{"e", "a", "f"}, chatIDs(l))

	// Filtros de cuenta y de canal.
	l, _ = s.ListChats(bg, model.ChatFilter{AccountIDs: []model.AccountID{"wa-otra"}})
	require.Empty(t, l)
	l, _ = s.ListChats(bg, model.ChatFilter{AccountIDs: []model.AccountID{"wa-otra", testAcc}, Channels: []model.Channel{model.ChannelWhatsApp}})
	require.Len(t, l, 5)
	l, _ = s.ListChats(bg, model.ChatFilter{Channels: []model.Channel{model.ChannelTelegram}})
	require.Empty(t, l)

	// Más de 60 por defecto.
	s2 := newTestAccount(t)
	for i := 0; i < 70; i++ {
		require.NoError(t, s2.UpsertChat(bg, mkChat(fmt.Sprintf("c%02d", i), model.ChatDirect, "x", int64(i+1))))
	}
	l, _ = s2.ListChats(bg, model.ChatFilter{})
	require.Len(t, l, 60)
	require.Equal(t, "c69", l[0].NativeID)
}

func TestPersons(t *testing.T) {
	s := newTestAccount(t)
	require.NoError(t, s.UpsertPerson(bg, model.Person{NativeID: "1@s", PushName: "Anita", Phone: "+593991", AvatarURL: "/avatar/wa-test/1.jpg"}))
	require.NoError(t, s.UpsertPerson(bg, model.Person{NativeID: "2@s", Name: "Beto Ruiz", Username: "beto"}))
	require.NoError(t, s.UpsertPerson(bg, model.Person{NativeID: "3@s"}))
	// Completar sin borrar.
	require.NoError(t, s.UpsertPerson(bg, model.Person{NativeID: "1@s", Name: "Ana"}))
	p, err := s.GetPerson(bg, "1@s")
	require.NoError(t, err)
	require.Equal(t, model.Person{AccountID: testAcc, NativeID: "1@s", Name: "Ana", PushName: "Anita", Phone: "+593991", AvatarURL: "/avatar/wa-test/1.jpg"}, p)
	_, err = s.GetPerson(bg, "zz")
	require.ErrorIs(t, err, port.ErrNotFound)
	require.ErrorIs(t, s.UpsertPerson(bg, model.Person{}), port.ErrInvalid)

	l, err := s.ListPersons(bg, "", 0)
	require.NoError(t, err)
	require.Len(t, l, 3)
	require.Equal(t, "3@s", l[0].NativeID) // por nombre visible: "3@s", Ana, Beto Ruiz
	require.Equal(t, "1@s", l[1].NativeID)
	require.Equal(t, "2@s", l[2].NativeID)
	l, _ = s.ListPersons(bg, "beto", 0)
	require.Len(t, l, 1)
	l, _ = s.ListPersons(bg, "5939", 0)
	require.Len(t, l, 1)
	l, _ = s.ListPersons(bg, "", 2)
	require.Len(t, l, 2)
}

func TestKV(t *testing.T) {
	s := newTestAccount(t)
	v, err := s.KVGet(bg, "session")
	require.NoError(t, err)
	require.Nil(t, v)
	require.NoError(t, s.KVSet(bg, "session", []byte{0, 1, 2, 255}))
	v, _ = s.KVGet(bg, "session")
	require.Equal(t, []byte{0, 1, 2, 255}, v)
	require.NoError(t, s.KVSet(bg, "session", []byte("nuevo")))
	v, _ = s.KVGet(bg, "session")
	require.Equal(t, []byte("nuevo"), v)
	require.NoError(t, s.KVSet(bg, "vacio", nil))
	v, err = s.KVGet(bg, "vacio")
	require.NoError(t, err)
	require.NotNil(t, v)
	require.Empty(t, v)
	require.Error(t, s.KVSet(bg, "", []byte("x")))
}

func TestReopenAccountKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.db")
	st, _, err := OpenAccount(testAcc, path)
	require.NoError(t, err)
	_, err = st.UpsertMessage(bg, mkMsg("c", "m1", "hola mundo", 100, false))
	require.NoError(t, err)
	require.NoError(t, st.Close())

	st2, _, err := OpenAccount(testAcc, path)
	require.NoError(t, err)
	defer st2.Close()
	got, err := st2.SearchMessages(bg, "mundo", "", 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
}
