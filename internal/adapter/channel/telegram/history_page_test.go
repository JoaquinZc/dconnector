package telegram

import (
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func historyBatches(sink *sinkRec) []*model.HistoryBatch {
	var out []*model.HistoryBatch
	for _, ev := range sink.all() {
		if ev.Type == model.EvHistoryBatch {
			out = append(out, ev.History)
		}
	}
	return out
}

// Una página de messages.getHistory sale como UN history.batch con sus mensajes y personas.
func TestEmitHistoryPageDeliversMessagesAsOneBatch(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	page := &tg.MessagesMessages{
		Messages: []tg.MessageClass{privMsg(9, "nueve"), privMsg(8, "ocho")},
		Users:    []tg.UserClass{testEntities().Users[otherUID]},
	}
	a.emitHistoryPage(bg, page)

	bs := historyBatches(sink)
	require.Len(t, bs, 1)
	require.Len(t, bs[0].Messages, 2)
	assert.Equal(t, "9", bs[0].Messages[0].NativeID)
	assert.Equal(t, "8", bs[0].Messages[1].NativeID)
	assert.Equal(t, "user:100", bs[0].Messages[0].ChatNativeID)
	assert.Equal(t, -1.0, bs[0].Progress)
	assert.Empty(t, bs[0].Chats, "el historial no pisa las marcas de los chats")
	require.NotEmpty(t, bs[0].Persons)
	assert.Equal(t, "user:100", bs[0].Persons[0].NativeID)
}

// Sin nada que traer, el lote sale vacío: es la señal de «Telegram contestó y no hay más».
func TestEmitHistoryPageEmptyIsAnAnswer(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	a.emitHistoryPage(bg, nil)
	a.emitHistoryPage(bg, &tg.MessagesMessages{})
	bs := historyBatches(sink)
	require.Len(t, bs, 2)
	for _, b := range bs {
		assert.Empty(t, b.Messages)
		assert.Empty(t, b.Chats)
		assert.Empty(t, b.Persons)
	}
}

// Los mensajes de servicio del historial también se guardan; los que no se pueden traducir se omiten.
func TestEmitHistoryPageKeepsServiceMessages(t *testing.T) {
	a, sink := newTestAdapter(t, nil)
	page := &tg.MessagesMessages{
		Messages: []tg.MessageClass{
			privMsg(5, "cinco"),
			&tg.MessageEmpty{ID: 4},
		},
		Users: []tg.UserClass{testEntities().Users[otherUID]},
	}
	a.emitHistoryPage(bg, page)
	bs := historyBatches(sink)
	require.Len(t, bs, 1)
	require.Len(t, bs[0].Messages, 1)
	assert.Equal(t, "5", bs[0].Messages[0].NativeID)
}

// El FLOOD_WAIT largo sale como error de espera con el tiempo, que el servicio de historial respeta.
func TestFloodWaitErrorCarriesTheWait(t *testing.T) {
	var err error = &floodWaitError{wait: 90 * time.Second, action: "pedir mensajes anteriores"}
	assert.ErrorIs(t, err, port.ErrRateLimited)
	var ra interface{ RetryAfter() time.Duration }
	require.True(t, errors.As(err, &ra))
	assert.Equal(t, 90*time.Second, ra.RetryAfter())
	assert.Contains(t, err.Error(), "1m30s")
}

// Sin conexión, RequestHistory avisa que la cuenta no está conectada (antes de mirar el ancla).
func TestRequestHistoryNeedsConnection(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	err := a.RequestHistory(bg, "user:100", &model.MessageRef{ChatNativeID: "user:100", NativeID: "7"}, 50)
	assert.ErrorIs(t, err, port.ErrNotConnected)
}
