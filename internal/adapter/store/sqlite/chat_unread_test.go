package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

// UnreadCount < 0 significa «sin cambio»: un chat parcial del adaptador no borra el contador.
func TestUpsertChatNegativeUnreadKeepsCounter(t *testing.T) {
	s := newTestAccount(t)
	id := "593@s.whatsapp.net"

	// Chat nuevo con -1: queda en 0.
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: id, Kind: model.ChatDirect, Title: "Ana", UnreadCount: -1}))
	got, err := s.GetChat(bg, id)
	require.NoError(t, err)
	require.Equal(t, 0, got.UnreadCount)

	// Con un valor real se guarda; con -1 se conserva; con 0 explícito se pone a cero.
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: id, Kind: model.ChatDirect, UnreadCount: 4}))
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: id, Kind: model.ChatDirect, Title: "Ana B", UnreadCount: -1}))
	got, _ = s.GetChat(bg, id)
	require.Equal(t, 4, got.UnreadCount)
	require.Equal(t, "Ana B", got.Title)

	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: id, Kind: model.ChatDirect, UnreadCount: 0}))
	got, _ = s.GetChat(bg, id)
	require.Equal(t, 0, got.UnreadCount)
}

func TestUpsertStatusPartialDeleteMerges(t *testing.T) {
	s := newTestAccount(t)
	st := model.Status{NativeID: "s1", AuthorNativeID: "a@s.whatsapp.net", PostedAt: 1000, ExpiresAt: 2000, Kind: model.KindText, Text: "hola"}
	require.NoError(t, s.UpsertStatus(bg, st))
	require.NoError(t, s.UpsertStatus(bg, model.Status{NativeID: "s1", AuthorNativeID: "a@s.whatsapp.net", DeletedAt: 1500}))
	var text string
	var posted, deleted int64
	require.NoError(t, s.db.QueryRow(`SELECT text, posted_at, deleted_at FROM statuses WHERE native_id = 's1'`).Scan(&text, &posted, &deleted))
	require.Equal(t, "hola", text, "el aviso de borrado no pisa el contenido")
	require.EqualValues(t, 1000, posted)
	require.EqualValues(t, 1500, deleted)
}

// PatchChat con unreadCount < 0 («sin cambio») no guarda el valor negativo.
func TestPatchChatNegativeUnreadIsIgnored(t *testing.T) {
	s := newTestAccount(t)
	id := "593@s.whatsapp.net"
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: id, Kind: model.ChatDirect, UnreadCount: 3}))

	require.NoError(t, s.PatchChat(bg, model.Chat{NativeID: id, UnreadCount: -1}, []string{"unreadCount"}))
	got, err := s.GetChat(bg, id)
	require.NoError(t, err)
	require.Equal(t, 3, got.UnreadCount)

	require.NoError(t, s.PatchChat(bg, model.Chat{NativeID: id, UnreadCount: -1, Pinned: true}, []string{"unreadCount", "pinned"}))
	got, _ = s.GetChat(bg, id)
	require.Equal(t, 3, got.UnreadCount)
	require.True(t, got.Pinned)
}
