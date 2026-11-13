package sqlite

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

// ListChats solo lista conversaciones con actividad (salvo IncludeEmpty): la libreta de WhatsApp aparece
// como chats directos vacíos y no debe llenar la lista.
func TestListChatsOnlyWithActivity(t *testing.T) {
	s := newTestAccount(t)
	pinnedEmpty := mkChat("pin", model.ChatDirect, "Fijado vacío", 0)
	pinnedEmpty.Pinned = true
	archivedEmpty := mkChat("arc", model.ChatDirect, "Archivado vacío", 0)
	archivedEmpty.Archived = true
	unreadEmpty := mkChat("unr", model.ChatDirect, "Vacío con no leídos", 0)
	unreadEmpty.UnreadCount = 1
	for _, c := range []model.Chat{
		mkChat("act", model.ChatDirect, "Ana activa", 100),
		mkChat("vac", model.ChatDirect, "Ana vacía", 0),
		pinnedEmpty, archivedEmpty, unreadEmpty,
		mkChat("grp", model.ChatGroup, "Grupo vacío", 0),
		mkChat("can", model.ChatChannel, "Difusión vacía", 0),
		mkChat("me0", model.ChatSelf, "Tú sin actividad", 0),
	} {
		require.NoError(t, s.UpsertChat(bg, c))
	}

	l, err := s.ListChats(bg, model.ChatFilter{})
	require.NoError(t, err)
	require.Equal(t, []string{"pin", "act", "can", "grp"}, chatIDs(l)) // fijado primero; recientes; luego por id
	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "groups"})
	require.Equal(t, []string{"grp"}, chatIDs(l)) // «groups» es kind='group' (literal)
	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "unread"})
	require.Empty(t, chatIDs(l), "un directo vacío no sale ni en «No leídos»")
	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "archived"})
	require.Empty(t, chatIDs(l))

	// Buscar por título a alguien sin actividad no lo muestra (esa búsqueda es la de «Nuevo chat»).
	l, _ = s.ListChats(bg, model.ChatFilter{Query: "Ana"})
	require.Equal(t, []string{"act"}, chatIDs(l))

	// IncludeEmpty lo trae todo.
	l, _ = s.ListChats(bg, model.ChatFilter{IncludeEmpty: true})
	require.ElementsMatch(t, []string{"pin", "act", "vac", "unr", "grp", "can", "me0"}, chatIDs(l)) // archivados fuera
	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "unread", IncludeEmpty: true})
	require.Equal(t, []string{"unr"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Tab: "archived", IncludeEmpty: true})
	require.Equal(t, []string{"arc"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Query: "Ana", IncludeEmpty: true})
	require.ElementsMatch(t, []string{"act", "vac"}, chatIDs(l))

	// El self con actividad sí sale.
	require.NoError(t, s.UpsertChat(bg, mkChat("me0", model.ChatSelf, "Tú", 150)))
	l, _ = s.ListChats(bg, model.ChatFilter{})
	require.Equal(t, []string{"pin", "me0", "act", "can", "grp"}, chatIDs(l))
}

// El filtro va en el SQL: la paginación no se llena de chats vacíos.
func TestListChatsPaginationSkipsEmpty(t *testing.T) {
	s := newTestAccount(t)
	for i := 0; i < 70; i++ {
		require.NoError(t, s.UpsertChat(bg, mkChat(fmt.Sprintf("v%02d", i), model.ChatDirect, fmt.Sprintf("Vacío %d", i), 0)))
	}
	for i := 1; i <= 5; i++ {
		require.NoError(t, s.UpsertChat(bg, mkChat(fmt.Sprintf("a%d", i), model.ChatDirect, fmt.Sprintf("Activo %d", i), int64(i*10))))
	}
	l, err := s.ListChats(bg, model.ChatFilter{Limit: 3})
	require.NoError(t, err)
	require.Equal(t, []string{"a5", "a4", "a3"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{Limit: 3, Before: 30})
	require.Equal(t, []string{"a2", "a1"}, chatIDs(l))
	l, _ = s.ListChats(bg, model.ChatFilter{})
	require.Len(t, l, 5)
	l, _ = s.ListChats(bg, model.ChatFilter{IncludeEmpty: true, Limit: 200})
	require.Len(t, l, 75)
}

// Un chat vacío (creado por «Nuevo chat») pasa a la lista con su primer mensaje.
func TestEmptyChatAppearsAfterFirstMessage(t *testing.T) {
	s := newTestAccount(t)
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: "n1", Kind: model.ChatDirect, Title: "Nuevo", PeerNativeID: "n1"}))
	l, _ := s.ListChats(bg, model.ChatFilter{})
	require.Empty(t, l)
	_, err := s.UpsertMessage(bg, model.Message{
		AccountID: testAcc, NativeID: "m1", ChatNativeID: "n1", FromMe: true, Timestamp: 500, Kind: model.KindText, Text: "hola",
		Delivery: model.DeliverySent,
	})
	require.NoError(t, err)
	l, _ = s.ListChats(bg, model.ChatFilter{})
	require.Equal(t, []string{"n1"}, chatIDs(l))
	require.Equal(t, int64(500), l[0].LastMessageAt)
	require.Equal(t, "Nuevo", l[0].Title)
}
