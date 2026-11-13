package messenger

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"

	"dconnector/internal/core/port"
)

// El historial de los chats cifrados no se puede pedir: error claro, sin tocar la red.
func TestRequestHistoryEncryptedChatIsUnsupported(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.a.KV.Ensure(ctx))
	f.a.rememberE2EEChat("500", types.NewJID("500", types.MessengerServer))

	err := f.a.RequestHistory(ctx, "500", nil, 20)
	assert.ErrorIs(t, err, port.ErrUnsupported)
	assert.Contains(t, err.Error(), "cifrados")
}
