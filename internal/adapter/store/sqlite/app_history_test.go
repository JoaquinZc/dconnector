package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

func TestHistorySyncSaveListReset(t *testing.T) {
	s := newTestApp(t)
	const other = model.AccountID("tg-other")
	saveTestAccount(t, s, testAcc)
	saveTestAccount(t, s, other)

	rows, err := s.ListHistorySync(bg, testAcc)
	require.NoError(t, err)
	require.Empty(t, rows)

	require.NoError(t, s.SaveHistorySync(bg, model.HistorySyncRow{AccountID: testAcc, ChatNativeID: "c1"}))
	require.NoError(t, s.SaveHistorySync(bg, model.HistorySyncRow{
		AccountID: testAcc, ChatNativeID: "c2", State: model.HistoryDone, Requests: 7, EmptyStreak: 2, Added: 130,
		OldestTs: 1234, LastRequestAt: 5678, DoneAt: 5679, LastError: "nada",
	}))
	require.NoError(t, s.SaveHistorySync(bg, model.HistorySyncRow{AccountID: other, ChatNativeID: "c1", State: model.HistoryFailed}))

	rows, err = s.ListHistorySync(bg, testAcc)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "c1", rows[0].ChatNativeID)
	require.Equal(t, model.HistoryPending, rows[0].State) // el estado vacío es «pending»
	require.Equal(t, model.HistorySyncRow{
		AccountID: testAcc, ChatNativeID: "c2", State: model.HistoryDone, Requests: 7, EmptyStreak: 2, Added: 130,
		OldestTs: 1234, LastRequestAt: 5678, DoneAt: 5679, LastError: "nada",
	}, rows[1])

	// Guardar de nuevo reemplaza la fila.
	require.NoError(t, s.SaveHistorySync(bg, model.HistorySyncRow{AccountID: testAcc, ChatNativeID: "c2", State: model.HistoryPending, Requests: 8}))
	rows, err = s.ListHistorySync(bg, testAcc)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, model.HistoryPending, rows[1].State)
	require.Equal(t, 8, rows[1].Requests)
	require.Zero(t, rows[1].DoneAt)

	// Datos no válidos.
	require.Error(t, s.SaveHistorySync(bg, model.HistorySyncRow{AccountID: testAcc}))
	require.Error(t, s.SaveHistorySync(bg, model.HistorySyncRow{AccountID: testAcc, ChatNativeID: "c3", State: "rara"}))

	// Reiniciar borra solo la cuenta indicada.
	require.NoError(t, s.ResetHistorySync(bg, testAcc))
	rows, err = s.ListHistorySync(bg, testAcc)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = s.ListHistorySync(bg, other)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestHistorySyncCascadesWithAccount(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, testAcc)
	require.NoError(t, s.SaveHistorySync(bg, model.HistorySyncRow{AccountID: testAcc, ChatNativeID: "c1"}))
	require.NoError(t, s.DeleteAccount(bg, testAcc))
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM history_sync`).Scan(&n))
	require.Zero(t, n)
}

func TestChatHistoryStats(t *testing.T) {
	s := newTestAccount(t)

	st, err := s.ChatHistoryStats(bg, "c1")
	require.NoError(t, err)
	require.Zero(t, st.Count)
	require.Empty(t, st.Oldest.NativeID)

	_, err = s.UpsertMessages(bg, []model.Message{
		mkMsg("c1", "m2", "dos", 2000, false),
		mkMsg("c1", "m1", "uno", 1000, true),
		mkMsg("c1", "m3", "tres", 3000, false),
		mkMsg("c2", "x1", "otro chat", 500, false),
	})
	require.NoError(t, err)

	st, err = s.ChatHistoryStats(bg, "c1")
	require.NoError(t, err)
	require.Equal(t, 3, st.Count)
	require.Equal(t, int64(3000), st.NewestTs)
	require.Equal(t, "m1", st.Oldest.NativeID)
	require.Equal(t, "c1", st.Oldest.ChatNativeID)
	require.Equal(t, int64(1000), st.Oldest.Timestamp)
	require.True(t, st.Oldest.FromMe)
	require.Equal(t, "s-m1", st.Oldest.SenderNativeID)

	n, oldest, err := s.HistoryTotals(bg)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Equal(t, int64(500), oldest)
}
