package bus

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

func ev(id string) model.Event { return model.Event{ID: id, Type: model.EvMessageNew} }

func TestOrderPerSubscriber(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("a", 16)
	defer cancel()
	for i := 0; i < 10; i++ {
		b.Publish(ev(fmt.Sprint(i)))
	}
	for i := 0; i < 10; i++ {
		require.Equal(t, fmt.Sprint(i), (<-ch).ID)
	}
}

func TestSlowSubscriberDropsWithoutBlocking(t *testing.T) {
	b := New()
	slow, cancelSlow := b.Subscribe("slow", 2)
	defer cancelSlow()
	fast, cancelFast := b.Subscribe("fast", 100)
	defer cancelFast()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			b.Publish(ev(fmt.Sprint(i)))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish se bloqueó con un suscriptor lleno")
	}
	require.Len(t, fast, 50, "el rápido recibe todo")
	require.Len(t, slow, 2)
	require.EqualValues(t, 48, b.Dropped()["slow"])
	require.EqualValues(t, 0, b.Dropped()["fast"])
}

func TestCancelClosesChannelAndIsIdempotent(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("a", 4)
	b.Publish(ev("1"))
	cancel()
	cancel()
	require.Equal(t, "1", (<-ch).ID)
	_, ok := <-ch
	require.False(t, ok, "canal cerrado tras cancelar")
	b.Publish(ev("2")) // no debe entrar en pánico
}

func TestDefaultBuffer(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("a", 0)
	defer cancel()
	require.Equal(t, DefaultBuffer, cap(ch))
}
