package sqlite

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

// Escrituras y lecturas simultáneas (varios hilos del Router, la interfaz y el protocolo) no deben
// dar "database is locked" ni perder filas.
func TestConcurrentWritersAndReaders(t *testing.T) {
	s := newTestAccount(t)
	const writers, perWriter = 6, 40
	var wg sync.WaitGroup
	errs := make(chan error, 256)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				m := mkMsg(fmt.Sprintf("c%d", w%3), fmt.Sprintf("w%d-%d", w, i), "mensaje concurrente", int64(1000+i), false)
				if _, err := s.UpsertMessage(bg, m); err != nil {
					errs <- err
					return
				}
				if _, err := s.ApplyReaction(bg, model.ReactionEvent{MessageKey: mkey(m.ChatNativeID, m.NativeID),
					Reaction: model.Reaction{SenderNativeID: "x", Emoji: "👍"}}); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				if _, err := s.ListChats(bg, model.ChatFilter{}); err != nil {
					errs <- err
					return
				}
				if _, err := s.SearchMessages(bg, "concurrente", "", 20); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n))
	require.Equal(t, writers*perWriter, n)
	require.Equal(t, writers*perWriter, ftsCount(t, s))
}

func TestConcurrentOutbox(t *testing.T) {
	s := newTestApp(t)
	require.NoError(t, s.SaveDestination(ctx, model.Destination{ID: "d", Name: "d", URL: "u", Token: "t"}))
	var wg sync.WaitGroup
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				require.NoError(t, s.OutboxAdd(ctx, "d", fmt.Sprintf("e%d-%d", w, i), []byte("{}")))
				_, err := s.OutboxPending(ctx, "d", 5)
				require.NoError(t, err)
			}
		}(w)
	}
	wg.Wait()
	n, err := s.OutboxCount(ctx, "d")
	require.NoError(t, err)
	require.Equal(t, 200, n)
}
