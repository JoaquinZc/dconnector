package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

func previewMsg(chat, id string, ts int64, text string, fromMe bool) model.Message {
	return model.Message{
		ChatNativeID: chat, NativeID: id, Timestamp: ts, Kind: model.KindText, Text: text, FromMe: fromMe,
		Delivery: model.DeliverySent,
	}
}

// El historial de WhatsApp trae la marca de tiempo del chat mayor que la de su último mensaje: la
// vista previa debe quedar con el último mensaje guardado aunque ninguno «sea el más reciente».
func TestHistoryBatchFillsEmptyPreviewWhenChatTimestampIsAhead(t *testing.T) {
	s := newTestAccount(t)
	chat := "593@s.whatsapp.net"
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: chat, Kind: model.ChatDirect, Title: "Ana", LastMessageAt: 9_000_000}))

	// Lote desordenado: el más reciente no va el último.
	n, err := s.UpsertMessages(bg, []model.Message{
		previewMsg(chat, "m2", 2000, "segundo", true),
		previewMsg(chat, "m3", 3000, "tercero\n  con   espacios", false),
		previewMsg(chat, "m1", 1000, "primero", false),
	})
	require.NoError(t, err)
	require.Equal(t, 3, n)

	got, err := s.GetChat(bg, chat)
	require.NoError(t, err)
	require.Equal(t, "tercero con espacios", got.LastPreview)
	require.False(t, got.LastFromMe)
	require.EqualValues(t, 9_000_000, got.LastMessageAt, "la marca de tiempo mayor del chat se conserva")
	require.Equal(t, 0, got.UnreadCount, "el histórico nunca suma no leídos")
}

func TestHistoryInMultipleBatchesKeepsLatestPreview(t *testing.T) {
	s := newTestAccount(t)
	chat := "593@s.whatsapp.net"
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 9_000_000}))

	_, err := s.UpsertMessages(bg, []model.Message{previewMsg(chat, "m1", 1000, "viejo", false)})
	require.NoError(t, err)
	got, _ := s.GetChat(bg, chat)
	require.Equal(t, "viejo", got.LastPreview)

	// Un mensaje más antiguo que el último y con vista previa ya existente: no cambia nada.
	_, err = s.UpsertMessages(bg, []model.Message{previewMsg(chat, "m0", 500, "antiguo", true)})
	require.NoError(t, err)
	got, _ = s.GetChat(bg, chat)
	require.Equal(t, "viejo", got.LastPreview)
	require.False(t, got.LastFromMe)
}

func TestSingleMessageFillsEmptyPreviewAndSkipsSystem(t *testing.T) {
	s := newTestAccount(t)
	chat := "593@s.whatsapp.net"
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 9_000_000}))

	// Un mensaje de sistema no rellena la vista previa.
	_, err := s.UpsertMessage(bg, model.Message{
		ChatNativeID: chat, NativeID: "s1", Timestamp: 1000, Kind: model.KindSystem, Text: "cambió el número",
	})
	require.NoError(t, err)
	got, _ := s.GetChat(bg, chat)
	require.Equal(t, "", got.LastPreview)

	_, err = s.UpsertMessage(bg, previewMsg(chat, "m1", 2000, "hola", true))
	require.NoError(t, err)
	got, _ = s.GetChat(bg, chat)
	require.Equal(t, "hola", got.LastPreview)
	require.True(t, got.LastFromMe)

	// Un mensaje más antiguo con la vista previa ya puesta no la cambia.
	_, err = s.UpsertMessage(bg, previewMsg(chat, "m0", 1500, "antes", false))
	require.NoError(t, err)
	got, _ = s.GetChat(bg, chat)
	require.Equal(t, "hola", got.LastPreview)
}

func TestChatWithoutMessagesKeepsEmptyPreview(t *testing.T) {
	s := newTestAccount(t)
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: "a@s.whatsapp.net", Kind: model.ChatDirect, LastMessageAt: 5000}))
	// Un mensaje de otro chat no inventa nada en este.
	_, err := s.UpsertMessages(bg, []model.Message{previewMsg("b@s.whatsapp.net", "m1", 100, "hola", false)})
	require.NoError(t, err)
	got, err := s.GetChat(bg, "a@s.whatsapp.net")
	require.NoError(t, err)
	require.Equal(t, "", got.LastPreview)
}

func TestMediaMessageFillsPreview(t *testing.T) {
	s := newTestAccount(t)
	chat := "593@s.whatsapp.net"
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 9_000_000}))
	_, err := s.UpsertMessages(bg, []model.Message{{
		ChatNativeID: chat, NativeID: "m1", Timestamp: 1000, Kind: model.KindImage, Delivery: model.DeliverySent,
		Media: &model.Media{Mime: "image/jpeg"},
	}})
	require.NoError(t, err)
	got, _ := s.GetChat(bg, chat)
	require.Equal(t, "📷 Foto", got.LastPreview)
}

// Un chat parcial (sin vista previa o con una marca de tiempo menor) no borra lo guardado.
func TestUpsertChatKeepsStoredPreviewAndNewestTimestamp(t *testing.T) {
	s := newTestAccount(t)
	chat := "593@s.whatsapp.net"
	require.NoError(t, s.UpsertChat(bg, model.Chat{
		NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 5000, LastPreview: "hola", LastFromMe: true,
	}))

	// Más reciente pero sin vista previa: la vista previa se conserva; la marca de tiempo sube.
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 7000, UnreadCount: -1}))
	got, _ := s.GetChat(bg, chat)
	require.Equal(t, "hola", got.LastPreview)
	require.True(t, got.LastFromMe)
	require.EqualValues(t, 7000, got.LastMessageAt)

	// Más antiguo, con otra vista previa: se conserva todo lo guardado.
	require.NoError(t, s.UpsertChat(bg, model.Chat{
		NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 1000, LastPreview: "viejo", UnreadCount: -1,
	}))
	got, _ = s.GetChat(bg, chat)
	require.Equal(t, "hola", got.LastPreview)
	require.EqualValues(t, 7000, got.LastMessageAt)

	// Más reciente con vista previa: reemplaza.
	require.NoError(t, s.UpsertChat(bg, model.Chat{
		NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 8000, LastPreview: "nuevo", LastFromMe: false, UnreadCount: -1,
	}))
	got, _ = s.GetChat(bg, chat)
	require.Equal(t, "nuevo", got.LastPreview)
	require.False(t, got.LastFromMe)
	require.EqualValues(t, 8000, got.LastMessageAt)
}

// Si el chat estaba sin vista previa, la del chat entrante (aunque su marca de tiempo no sea mayor)
// la rellena.
func TestUpsertChatFillsEmptyStoredPreview(t *testing.T) {
	s := newTestAccount(t)
	chat := "593@s.whatsapp.net"
	require.NoError(t, s.UpsertChat(bg, model.Chat{NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 9000}))
	require.NoError(t, s.UpsertChat(bg, model.Chat{
		NativeID: chat, Kind: model.ChatDirect, LastMessageAt: 4000, LastPreview: "hola", UnreadCount: -1,
	}))
	got, _ := s.GetChat(bg, chat)
	require.Equal(t, "hola", got.LastPreview)
	require.EqualValues(t, 9000, got.LastMessageAt)
}
