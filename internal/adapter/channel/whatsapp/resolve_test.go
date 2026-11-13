package whatsapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/port"
)

// ResolvePeer: ids nativos sin red; números con sesión (sin sesión: ErrNotConnected).
func TestResolvePeer(t *testing.T) {
	a, _ := newTestAdapter(t)
	ctx := context.Background()

	// Un JID de teléfono se normaliza sin consultar a WhatsApp.
	id, _, err := a.ResolvePeer(ctx, " 593991234567@s.whatsapp.net ")
	require.NoError(t, err)
	assert.Equal(t, "593991234567@s.whatsapp.net", id)

	// Grupos y canales no son «una persona».
	_, _, err = a.ResolvePeer(ctx, "1203630@g.us")
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, _, err = a.ResolvePeer(ctx, "1203@newsletter")
	assert.ErrorIs(t, err, port.ErrInvalid)

	// Texto que no es número.
	_, _, err = a.ResolvePeer(ctx, "hola")
	assert.ErrorIs(t, err, port.ErrInvalid)

	// Un número necesita la sesión conectada para comprobar «¿está en WhatsApp?».
	_, _, err = a.ResolvePeer(ctx, "+593 99 123 4567")
	assert.ErrorIs(t, err, port.ErrNotConnected)
}
