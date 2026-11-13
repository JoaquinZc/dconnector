package facade

import (
	"context"

	"dconnector/internal/adapter/protocol/ws"
	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

func chatKeyOf(s string) model.ChatKey { return model.ChatKey(s) }

// Destinations: destinos del protocolo y su estado en vivo.
type Destinations struct {
	d      *service.Destinations
	states func() []model.DestinationState
	test   func(ctx context.Context, id string) error
}

func (s *Destinations) List(ctx context.Context) ([]model.Destination, error) {
	return s.d.List(ctx)
}

func (s *Destinations) Save(ctx context.Context, dst model.Destination) (model.Destination, error) {
	return s.d.Save(ctx, dst)
}

func (s *Destinations) Delete(ctx context.Context, id string) error {
	return s.d.Delete(ctx, id)
}

// States: estado en vivo de cada destino (vacío mientras no haya manager).
func (s *Destinations) States() []model.DestinationState {
	if s.states == nil {
		return []model.DestinationState{}
	}
	return s.states()
}

// ProtocolDoc devuelve el documento completo del protocolo v1 (Markdown), embebido en el
// programa: la interfaz lo muestra y permite copiarlo.
func (s *Destinations) ProtocolDoc() string {
	return ws.ProtocolDoc()
}

// Test conecta, espera a que el hello se envíe y cierra.
func (s *Destinations) Test(ctx context.Context, id string) error {
	if s.test == nil {
		return errNotWired
	}
	return s.test(ctx, id)
}
