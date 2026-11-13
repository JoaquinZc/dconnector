package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func schedMsg(id, chat, text string, sendAt int64) model.Scheduled {
	return model.Scheduled{
		ID: id, AccountID: "wa-a", Kind: "message", ChatNativeID: chat, ChatTitle: "Ana", SendAt: sendAt, State: "pending",
		Message: &model.Outgoing{Kind: model.KindText, Text: text}, CreatedAt: 100,
	}
}

func TestScheduledRecurrenceRoundTripAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	st, err := OpenApp(path)
	require.NoError(t, err)
	s := st.(*AppStore)
	saveTestAccount(t, s, "wa-a")

	rec := schedMsg("r1", "1@s", "hola", 5000)
	rec.SeriesID = "ser-1"
	rec.Recurrence = &model.Recurrence{
		Frequency: "weekly", Every: 2, Weekdays: []int{1, 5}, EndAt: 9_000_000, MaxCount: 10, Count: 3, FirstAt: 1000,
	}
	custom := schedMsg("r2", "1@s", "cada rato", 6000)
	custom.Recurrence = &model.Recurrence{Frequency: "custom", Every: 15, Unit: "minutes", FirstAt: 6000}
	custom.SeriesID = "ser-2"
	single := schedMsg("one", "1@s", "suelto", 7000)
	for _, it := range []model.Scheduled{rec, custom, single} {
		require.NoError(t, s.SaveScheduled(ctx, it))
	}
	require.NoError(t, s.Close())

	// Reabrir el almacén conserva la regla y la serie.
	st, err = OpenApp(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	s = st.(*AppStore)
	all, err := s.ListScheduled(ctx, 0, 0, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"r1", "r2", "one"}, schedIDs(all))
	require.Equal(t, rec, all[0])
	require.Equal(t, custom, all[1])
	require.Nil(t, all[2].Recurrence)
	require.Equal(t, "", all[2].SeriesID)

	// Un programado que era serie puede volver a ser de una sola vez.
	rec.Recurrence = nil
	require.NoError(t, s.SaveScheduled(ctx, rec))
	got, err := s.GetScheduled(ctx, "r1")
	require.NoError(t, err)
	require.Nil(t, got.Recurrence)
	require.Equal(t, "ser-1", got.SeriesID)

	var raw string
	require.NoError(t, s.db.QueryRow(`SELECT recurrence_json FROM scheduled WHERE id = 'one'`).Scan(&raw))
	require.Equal(t, "", raw) // vacío = una sola vez
}

func TestScheduledGetByChatBySeries(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	saveTestAccount(t, s, "wa-b")

	_, err := s.GetScheduled(ctx, "nada")
	require.ErrorIs(t, err, port.ErrNotFound)

	a1 := schedMsg("a1", "1@s", "uno", 3000)
	a2 := schedMsg("a2", "1@s", "dos", 1000)
	a3 := schedMsg("a3", "1@s", "tres", 2000)
	a3.State = "sent"
	a3.SentAt = 2001
	b1 := schedMsg("b1", "2@s", "otro chat", 1500)
	c1 := schedMsg("c1", "1@s", "otra cuenta", 1500)
	c1.AccountID = "wa-b"
	st := model.Scheduled{ID: "st", AccountID: "wa-a", Kind: "status", SendAt: 1200, State: "pending", CreatedAt: 1,
		Status: &model.OutgoingStatus{Kind: model.KindText, Text: "estado"}}
	a1.SeriesID, a3.SeriesID = "ser", "ser"
	a1.Recurrence = &model.Recurrence{Frequency: "daily", Every: 1, FirstAt: 1000}
	for _, it := range []model.Scheduled{a1, a2, a3, b1, c1, st} {
		require.NoError(t, s.SaveScheduled(ctx, it))
	}

	got, err := s.GetScheduled(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, a1, got)

	l, err := s.ListScheduledByChat(ctx, "wa-a", "1@s", nil, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"a2", "a3", "a1"}, schedIDs(l)) // por hora de envío; sin estados ni otros chats
	l, _ = s.ListScheduledByChat(ctx, "wa-a", "1@s", []string{"pending"}, 0)
	require.Equal(t, []string{"a2", "a1"}, schedIDs(l))
	l, _ = s.ListScheduledByChat(ctx, "wa-a", "1@s", []string{"sent", "failed", "canceled"}, 0)
	require.Equal(t, []string{"a3"}, schedIDs(l))
	l, _ = s.ListScheduledByChat(ctx, "wa-a", "1@s", nil, 2500)
	require.Equal(t, []string{"a1"}, schedIDs(l))
	l, _ = s.ListScheduledByChat(ctx, "wa-b", "1@s", nil, 0)
	require.Equal(t, []string{"c1"}, schedIDs(l))
	l, err = s.ListScheduledByChat(ctx, "wa-a", "nadie", nil, 0)
	require.NoError(t, err)
	require.Empty(t, l)

	l, err = s.ListScheduledBySeries(ctx, "ser", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"a3", "a1"}, schedIDs(l))
	l, _ = s.ListScheduledBySeries(ctx, "ser", []string{"pending"})
	require.Equal(t, []string{"a1"}, schedIDs(l))
	l, _ = s.ListScheduledBySeries(ctx, "", nil)
	require.Empty(t, l)
	l, _ = s.ListScheduledBySeries(ctx, "otra", nil)
	require.Empty(t, l)
}

func TestScheduledSameTimeKeepsInsertionOrder(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	// Mismo send_at y mismo created_at: salen en el orden en que se guardaron, no por id.
	ids := []string{"z", "m", "a", "k", "b"}
	for _, id := range ids {
		require.NoError(t, s.SaveScheduled(ctx, schedMsg(id, "1@s", id, 1000)))
	}
	due, err := s.DueScheduled(ctx, 1000)
	require.NoError(t, err)
	require.Equal(t, ids, schedIDs(due))
	all, _ := s.ListScheduled(ctx, 0, 0, nil)
	require.Equal(t, ids, schedIDs(all))
	l, _ := s.ListScheduledByChat(ctx, "wa-a", "1@s", nil, 0)
	require.Equal(t, ids, schedIDs(l))
	// Actualizar uno no le cambia el lugar.
	upd := schedMsg("z", "1@s", "z", 1000)
	upd.State, upd.SentAt = "sent", 1001
	require.NoError(t, s.SaveScheduled(ctx, upd))
	all, _ = s.ListScheduled(ctx, 0, 0, nil)
	require.Equal(t, ids, schedIDs(all))
}

func TestScheduledSaveWithNextIsAtomicAndIdempotent(t *testing.T) {
	s := newTestApp(t)
	saveTestAccount(t, s, "wa-a")
	rec := func(id string, at int64, count int) model.Scheduled {
		it := schedMsg(id, "1@s", "hola", at)
		it.SeriesID = "ser"
		it.Recurrence = &model.Recurrence{Frequency: "daily", Every: 1, FirstAt: 1000, Count: count}
		return it
	}
	cur := rec("o1", 1000, 0)
	require.NoError(t, s.SaveScheduled(ctx, cur))

	// Cierra la actual y crea la siguiente.
	cur.State, cur.SentAt = "sent", 1001
	next := rec("o2", 2000, 1)
	require.NoError(t, s.SaveScheduledWithNext(ctx, cur, &next))
	l, _ := s.ListScheduledBySeries(ctx, "ser", nil)
	require.Equal(t, []string{"o1", "o2"}, schedIDs(l))
	require.Equal(t, "sent", l[0].State)
	require.Equal(t, "pending", l[1].State)

	// Repetir el cierre no duplica la serie (ya hay una pendiente).
	dup := rec("o3", 2000, 1)
	require.NoError(t, s.SaveScheduledWithNext(ctx, cur, &dup))
	l, _ = s.ListScheduledBySeries(ctx, "ser", nil)
	require.Equal(t, []string{"o1", "o2"}, schedIDs(l))

	// next nil: solo guarda la actual (la serie terminó).
	end := l[1]
	end.State, end.SentAt = "sent", 2001
	require.NoError(t, s.SaveScheduledWithNext(ctx, end, nil))
	l, _ = s.ListScheduledBySeries(ctx, "ser", []string{"pending"})
	require.Empty(t, l)

	// Si no se puede guardar la siguiente, tampoco se guarda el cierre de la actual (transacción).
	cur2 := rec("p1", 3000, 2)
	require.NoError(t, s.SaveScheduled(ctx, cur2))
	cur2.State = "sent"
	bad := rec("p2", 4000, 3)
	bad.Message = nil // inválida: sin contenido
	require.Error(t, s.SaveScheduledWithNext(ctx, cur2, &bad))
	got, err := s.GetScheduled(ctx, "p1")
	require.NoError(t, err)
	require.Equal(t, "pending", got.State)
	_, err = s.GetScheduled(ctx, "p2")
	require.ErrorIs(t, err, port.ErrNotFound)

	// Una actual inválida no guarda nada.
	cur2.Message = nil
	require.Error(t, s.SaveScheduledWithNext(ctx, cur2, nil))
}

// El servicio (service.scheduledSeriesStore) pide estos métodos por aserción de tipo: si cambia
// una firma aquí, el almacén dejaría de usarse en silencio. Debe coincidir con esa interfaz.
var _ interface {
	GetScheduled(ctx context.Context, id string) (model.Scheduled, error)
	ListScheduledByChat(ctx context.Context, accountID model.AccountID, chatNativeID string, states []string, since int64) ([]model.Scheduled, error)
	ListScheduledBySeries(ctx context.Context, seriesID string, states []string) ([]model.Scheduled, error)
	SaveScheduledWithNext(ctx context.Context, cur model.Scheduled, next *model.Scheduled) error
} = (*AppStore)(nil)
