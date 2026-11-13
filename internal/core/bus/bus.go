// Package bus: pub/sub en memoria (06-SERVICIOS §1). Publicar nunca bloquea.
package bus

import (
	"sync"

	"dconnector/internal/core/model"
)

// DefaultBuffer es el tamaño de búfer habitual de un suscriptor.
const DefaultBuffer = 1024

type sub struct {
	name string
	ch   chan model.Event
}

// Bus reparte eventos a los suscriptores. Un suscriptor lento pierde eventos
// (se cuentan en Dropped) pero no frena a los demás.
type Bus struct {
	mu      sync.RWMutex
	subs    map[int]*sub
	next    int
	dropMu  sync.Mutex
	dropped map[string]int64
}

func New() *Bus {
	return &Bus{subs: map[int]*sub{}, dropped: map[string]int64{}}
}

// Publish entrega ev a todos los suscriptores sin bloquear: si el búfer de
// alguno está lleno, se descarta para ese suscriptor.
func (b *Bus) Publish(ev model.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		select {
		case s.ch <- ev:
		default:
			b.dropMu.Lock()
			b.dropped[s.name]++
			b.dropMu.Unlock()
		}
	}
}

// Subscribe devuelve el canal del suscriptor y la función que lo cancela
// (cancelar cierra el canal; es seguro llamarla más de una vez).
func (b *Bus) Subscribe(name string, buf int) (<-chan model.Event, func()) {
	if buf <= 0 {
		buf = DefaultBuffer
	}
	s := &sub{name: name, ch: make(chan model.Event, buf)}
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = s
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			close(s.ch)
		})
	}
	return s.ch, cancel
}

// Dropped devuelve cuántos eventos se descartaron por suscriptor (por nombre).
func (b *Bus) Dropped() map[string]int64 {
	b.dropMu.Lock()
	defer b.dropMu.Unlock()
	out := make(map[string]int64, len(b.dropped))
	for k, v := range b.dropped {
		out[k] = v
	}
	return out
}
