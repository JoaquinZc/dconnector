package whatsapp

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types/events"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// historyTables crea las dos tablas de account.db que lee RequestHistory (el resto no hace falta).
func historyTables(t *testing.T, a *Adapter) {
	t.Helper()
	_, err := a.deps.DB.Exec(`
CREATE TABLE messages (
  chat_native_id TEXT NOT NULL, native_id TEXT NOT NULL, sender_native_id TEXT NOT NULL DEFAULT '',
  from_me INTEGER NOT NULL, ts INTEGER NOT NULL, PRIMARY KEY (chat_native_id, native_id));
CREATE TABLE chats (native_id TEXT PRIMARY KEY, last_message_at INTEGER NOT NULL DEFAULT 0);`)
	require.NoError(t, err)
}

func TestHistoryDeviceProps(t *testing.T) {
	// Parte de los valores por defecto de whatsmeow: solo cambian los de la sincronización completa.
	p := &waCompanionReg.DeviceProps{
		RequireFullSync: new(false),
		HistorySyncConfig: &waCompanionReg.DeviceProps_HistorySyncConfig{
			StorageQuotaMb: new(uint32(10240)), SupportGroupHistory: new(true), ThumbnailSyncDaysLimit: new(uint32(60)),
		},
	}
	historyDeviceProps(p)
	assert.True(t, p.GetRequireFullSync())
	c := p.GetHistorySyncConfig()
	assert.Equal(t, uint32(3650), c.GetFullSyncDaysLimit())
	assert.Equal(t, uint32(4096), c.GetFullSyncSizeMbLimit())
	assert.Equal(t, uint32(8192), c.GetStorageQuotaMb())
	assert.True(t, c.GetSupportCallLogHistory())
	assert.True(t, c.GetOnDemandReady())
	assert.True(t, c.GetCompleteOnDemandReady())
	// Lo que no se menciona se conserva.
	assert.True(t, c.GetSupportGroupHistory())
	assert.Equal(t, uint32(60), c.GetThumbnailSyncDaysLimit())

	// Sin configuración previa se crea, y nil no rompe.
	q := &waCompanionReg.DeviceProps{}
	historyDeviceProps(q)
	assert.Equal(t, uint32(3650), q.GetHistorySyncConfig().GetFullSyncDaysLimit())
	historyDeviceProps(nil)
}

func TestSyntheticMessageID(t *testing.T) {
	re := regexp.MustCompile(`^3EB0[0-9A-F]{28}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, err := syntheticMessageID()
		require.NoError(t, err)
		assert.Regexp(t, re, id)
		assert.False(t, seen[id], "los ids no se repiten")
		seen[id] = true
	}
}

func TestHistoryRequestInfo(t *testing.T) {
	a, _ := newTestAdapter(t)
	historyTables(t, a)
	ctx := context.Background()
	_, err := a.deps.DB.Exec(`INSERT INTO messages VALUES (?, 'm-new', 's', 0, 5000), (?, 'm-old', 's', 1, 1000)`, pnPeer, pnPeer)
	require.NoError(t, err)

	// Con mensajes guardados parte del más antiguo; la cantidad se acota a 50.
	info, n, err := a.historyRequestInfo(ctx, pnPeer, nil, 200)
	require.NoError(t, err)
	assert.Equal(t, "m-old", info.ID)
	assert.True(t, info.IsFromMe)
	assert.Equal(t, int64(1000), info.Timestamp.UnixMilli())
	assert.Equal(t, 50, n)
	assert.Equal(t, pnPeer, jidToNative(info.Chat))

	_, n, err = a.historyRequestInfo(ctx, pnPeer, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, historyRequestDefault, n)
	_, n, err = a.historyRequestInfo(ctx, pnPeer, nil, 20)
	require.NoError(t, err)
	assert.Equal(t, 20, n)

	// Un `before` explícito manda sobre lo guardado.
	info, _, err = a.historyRequestInfo(ctx, pnPeer, &model.MessageRef{ChatNativeID: pnPeer, NativeID: "dado", Timestamp: 777}, 10)
	require.NoError(t, err)
	assert.Equal(t, "dado", info.ID)
	assert.Equal(t, int64(777), info.Timestamp.UnixMilli())
}

func TestHistoryRequestInfoSyntheticAnchorForEmptyChat(t *testing.T) {
	a, _ := newTestAdapter(t)
	historyTables(t, a)
	ctx := context.Background()
	const other = "593992222222@s.whatsapp.net"

	// Sin mensajes y con el chat conocido: ancla con la fecha del último mensaje del chat.
	_, err := a.deps.DB.Exec(`INSERT INTO chats VALUES (?, 4242000)`, other)
	require.NoError(t, err)
	info, _, err := a.historyRequestInfo(ctx, other, nil, 50)
	require.NoError(t, err)
	assert.Regexp(t, `^3EB0[0-9A-F]{28}$`, info.ID)
	assert.False(t, info.IsFromMe)
	assert.Equal(t, int64(4242000), info.Timestamp.UnixMilli())

	// Sin mensajes y sin el chat: la fecha es «ahora».
	const unknown = "593993333333@s.whatsapp.net"
	before := time.Now()
	info, _, err = a.historyRequestInfo(ctx, unknown, nil, 50)
	require.NoError(t, err)
	assert.False(t, info.Timestamp.Before(before.Add(-time.Second)))
	assert.Regexp(t, `^3EB0`, info.ID)

	// oldestMessage sigue avisando con su error propio (lo usa historyRequestInfo para decidir).
	_, err = a.oldestMessage(ctx, other)
	assert.ErrorIs(t, err, errNoStoredMessages)
	assert.ErrorIs(t, err, port.ErrInvalid)
}

func TestHistoryRequestInfoRejectsWhatWhatsAppCannotServe(t *testing.T) {
	a, _ := newTestAdapter(t)
	historyTables(t, a)
	ctx := context.Background()
	for _, chat := range []string{"120363000000000000@newsletter", "status@broadcast"} {
		_, _, err := a.historyRequestInfo(ctx, chat, nil, 50)
		assert.ErrorIs(t, err, port.ErrUnsupported, chat)
	}
	_, _, err := a.historyRequestInfo(ctx, "", nil, 50)
	assert.ErrorIs(t, err, port.ErrInvalid)
}

// Una respuesta a una petición por chat que llega vacía se avisa con un lote vacío (el servicio de
// historial lo toma como «el teléfono contestó y no hay más»).
func TestOnDemandEmptyHistorySyncEmitsMarker(t *testing.T) {
	a, sink := newTestAdapter(t)
	a.sink = sink
	require.NoError(t, a.ensureClient(context.Background()))

	a.onHistorySync(&events.HistorySync{Data: &waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_ON_DEMAND.Enum()}})
	var batches []*model.HistoryBatch
	for _, ev := range sink.all() {
		if ev.Type == model.EvHistoryBatch {
			batches = append(batches, ev.History)
		}
	}
	require.Len(t, batches, 1)
	b := batches[0]
	assert.Empty(t, b.Chats)
	assert.Empty(t, b.Messages)
	assert.Empty(t, b.Persons)

	// Otros tipos vacíos (la sincronización inicial) no emiten nada.
	a2, sink2 := newTestAdapter(t)
	a2.sink = sink2
	require.NoError(t, a2.ensureClient(context.Background()))
	a2.onHistorySync(&events.HistorySync{Data: &waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_RECENT.Enum()}})
	for _, ev := range sink2.all() {
		assert.NotEqual(t, model.EvHistoryBatch, ev.Type)
	}
}
