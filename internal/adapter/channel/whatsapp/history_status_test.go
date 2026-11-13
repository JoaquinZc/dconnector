package whatsapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
)

// statusParse imita client.ParseWebMessage para status@broadcast: el autor es el participante del mensaje.
func statusParse(chat types.JID, w *waWeb.WebMessageInfo) (*events.Message, error) {
	info := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: jPeer, IsGroup: false},
		ID:            w.GetKey().GetID(), PushName: "Ana P", Timestamp: time.Unix(int64(w.GetMessageTimestamp()), 0),
	}
	return &events.Message{Info: info, Message: w.GetMessage()}, nil
}

func TestStatusesFromHistory(t *testing.T) {
	c := testConv() // «ahora» = testTs
	nowSec := uint64(testTs / 1000)
	old := nowSec - 25*3600 // vencido (24 h de vigencia)

	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), Caption: proto.String("foto")}}
	data := &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_INITIAL_STATUS_V3.Enum(),
		StatusV3Messages: []*waWeb.WebMessageInfo{
			webMsg("S1", false, nowSec-60, txt("hola a todos")).Message,
			webMsg("S2", false, nowSec-120, img).Message,
			webMsg("S3", false, old, txt("ya venció")).Message,
			webMsg("S1", false, nowSec-60, txt("hola a todos")).Message,                                     // repetido
			webMsg("S4", false, nowSec-30, &waE2E.Message{ContactMessage: &waE2E.ContactMessage{}}).Message, // no es texto/foto/vídeo
		},
		Conversations: []*waHistorySync.Conversation{{
			ID:       proto.String("status@broadcast"),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("S5", false, nowSec-10, txt("desde la conversación"))},
		}},
	}

	got := c.statusesFromHistory(data, statusParse)
	require.Len(t, got, 3)
	byID := map[string]model.Status{}
	for _, s := range got {
		byID[s.NativeID] = s
	}
	assert.Equal(t, model.KindText, byID["S1"].Kind)
	assert.Equal(t, "hola a todos", byID["S1"].Text)
	assert.Equal(t, jidToNative(jPeer), byID["S1"].AuthorNativeID)
	assert.False(t, byID["S1"].FromMe)
	assert.Equal(t, model.KindImage, byID["S2"].Kind)
	assert.Equal(t, "desde la conversación", byID["S5"].Text)
	assert.Greater(t, byID["S1"].ExpiresAt, testTs, "vigente: vence después de «ahora»")
	assert.NotContains(t, byID, "S3")
	assert.NotContains(t, byID, "S4")
}

func TestStatusesFromHistoryEmpty(t *testing.T) {
	c := testConv()
	assert.Empty(t, c.statusesFromHistory(nil, statusParse))
	assert.Empty(t, c.statusesFromHistory(&waHistorySync.HistorySync{}, statusParse))
	assert.Empty(t, c.statusesFromHistory(&waHistorySync.HistorySync{StatusV3Messages: []*waWeb.WebMessageInfo{
		webMsg("X", false, 1, txt("a")).Message,
	}}, nil))
}

func TestMessageKindName(t *testing.T) {
	assert.Equal(t, "vacío", messageKindName(nil))
	assert.Equal(t, "vacío", messageKindName(&waE2E.Message{}))
	assert.Equal(t, "conversation", messageKindName(txt("hola")))
	got := messageKindName(&waE2E.Message{
		ImageMessage:       &waE2E.ImageMessage{},
		MessageContextInfo: &waE2E.MessageContextInfo{},
	})
	assert.Equal(t, "imageMessage,messageContextInfo", got)
}
