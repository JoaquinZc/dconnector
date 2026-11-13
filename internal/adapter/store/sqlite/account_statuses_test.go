package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func mkStatus(native, author string, posted, expires int64, fromMe bool) model.Status {
	return model.Status{
		NativeID: native, AuthorNativeID: author, AuthorName: "Autor " + author, FromMe: fromMe,
		PostedAt: posted, ExpiresAt: expires, Kind: model.KindText, Text: "estado " + native, BgColor: "#112233", Font: 2,
		Delivery: model.DeliverySent,
	}
}

func statusIDs(ss []model.Status) []string {
	out := []string{}
	for _, s := range ss {
		out = append(out, s.NativeID)
	}
	return out
}

func TestStatusesAuthorsAndLists(t *testing.T) {
	s := newTestAccount(t)
	const now = int64(10_000)
	require.NoError(t, s.UpsertPerson(bg, model.Person{NativeID: "ana", Name: "Ana (agenda)", AvatarURL: "/avatar/wa-test/ana.jpg"}))
	in := []model.Status{
		mkStatus("s1", "ana", 1000, 20_000, false),
		mkStatus("s2", "ana", 2000, 20_000, false),
		mkStatus("s3", "beto", 3000, 20_000, false),
		mkStatus("old", "beto", 100, 5_000, false), // vencido
		mkStatus("mine", "yo", 4000, 20_000, true),
		mkStatus("gone", "carla", 5000, 20_000, false),
	}
	in[5].DeletedAt = 6000
	for _, st := range in {
		require.NoError(t, s.UpsertStatus(bg, st))
	}
	// Ana vio el primero.
	st1 := in[0]
	st1.Viewed = true
	require.NoError(t, s.UpsertStatus(bg, st1))

	authors, err := s.ListStatusAuthors(bg, now)
	require.NoError(t, err)
	require.Len(t, authors, 3) // ana, beto, yo (carla borrado, vencido fuera)
	// Primero quienes tienen sin ver (beto 1 sin ver, ana 1 sin ver), luego el resto; por reciente.
	require.Equal(t, "beto", authors[0].AuthorID)
	require.Equal(t, 1, authors[0].Count)
	require.Equal(t, 1, authors[0].Unviewed)
	require.Equal(t, "ana", authors[1].AuthorID)
	require.Equal(t, 2, authors[1].Count)
	require.Equal(t, 1, authors[1].Unviewed)
	require.Equal(t, int64(2000), authors[1].LastAt)
	require.Equal(t, "Ana (agenda)", authors[1].AuthorName) // el nombre de la persona gana al del estado
	require.Equal(t, "Autor beto", authors[0].AuthorName)
	require.Equal(t, "/avatar/wa-test/ana.jpg", authors[1].AvatarURL)
	require.Equal(t, testAcc, authors[1].AccountID)
	require.Equal(t, "yo", authors[2].AuthorID)
	require.Equal(t, 0, authors[2].Unviewed) // lo mío no cuenta como sin ver

	// Estados de una persona: vigentes, del más antiguo al más reciente.
	l, err := s.ListStatuses(bg, "ana", now)
	require.NoError(t, err)
	require.Equal(t, []string{"s1", "s2"}, statusIDs(l))
	require.True(t, l[0].Viewed)
	require.False(t, l[1].Viewed)
	require.Equal(t, testAcc+"|s1", model.AccountID(l[0].Key))
	require.Equal(t, model.ChannelWhatsApp, l[0].Channel)
	l, _ = s.ListStatuses(bg, "beto", now)
	require.Equal(t, []string{"s3"}, statusIDs(l))
	l, _ = s.ListStatuses(bg, "beto", 50) // en el pasado ve también el vencido
	require.Equal(t, []string{"old", "s3"}, statusIDs(l))
	l, _ = s.ListStatuses(bg, "carla", now)
	require.Empty(t, l)

	// Los míos, con rango, vencidos o no.
	require.NoError(t, s.UpsertStatus(bg, mkStatus("mine2", "yo", 500, 600, true)))
	mine, err := s.ListMyStatuses(bg, 0, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"mine", "mine2"}, statusIDs(mine))
	mine, _ = s.ListMyStatuses(bg, 1000, 5000)
	require.Equal(t, []string{"mine"}, statusIDs(mine))
	mine, _ = s.ListMyStatuses(bg, 5001, 0)
	require.NotNil(t, mine)
	require.Empty(t, mine)
}

func TestStatusUpsertMonotonicAndMedia(t *testing.T) {
	s := newTestAccount(t)
	st := mkStatus("s1", "ana", 1000, 90_000, false)
	st.Kind = model.KindImage
	st.Media = &model.Media{ID: "stm", Mime: "image/jpeg", Handle: []byte{9}}
	st.Viewed = true
	st.Delivery = model.DeliveryRead
	require.NoError(t, s.UpsertStatus(bg, st))
	require.NoError(t, s.SetMediaPath(bg, "stm", "/tmp/stm.jpg"))

	// Volver a llegar sin visto, sin medio y con entrega menor no retrocede nada.
	again := mkStatus("s1", "ana", 1000, 90_000, false)
	again.Kind = model.KindImage
	again.AuthorName = ""
	again.Delivery = model.DeliverySent
	require.NoError(t, s.UpsertStatus(bg, again))
	got, err := s.ListStatuses(bg, "ana", 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.True(t, got[0].Viewed)
	require.Equal(t, model.DeliveryRead, got[0].Delivery)
	require.Equal(t, "Autor ana", got[0].AuthorName)
	require.NotNil(t, got[0].Media)
	require.Equal(t, "/tmp/stm.jpg", got[0].Media.LocalPath)
	require.Equal(t, "/media/wa-test/stm", got[0].Media.URL)
	md, chat, err := s.GetMedia(bg, "stm")
	require.NoError(t, err)
	require.Equal(t, "status@broadcast", chat)
	require.Equal(t, []byte{9}, md.Handle)

	// Borrado se conserva.
	del := again
	del.DeletedAt = 7777
	require.NoError(t, s.UpsertStatus(bg, del))
	require.NoError(t, s.UpsertStatus(bg, again))
	var d int64
	require.NoError(t, s.db.QueryRow(`SELECT deleted_at FROM statuses WHERE native_id = 's1'`).Scan(&d))
	require.Equal(t, int64(7777), d)

	// Medio sin id: se genera; con la clave en vez del id nativo.
	m := mkStatus("", "ana", 2000, 90_000, false)
	m.Key = string(testAcc) + "|s2"
	m.Kind = model.KindVideo
	m.Media = &model.Media{Mime: "video/mp4"}
	require.NoError(t, s.UpsertStatus(bg, m))
	l, _ := s.ListStatuses(bg, "ana", 0)
	require.Equal(t, []string{"s2"}, statusIDs(l))
	require.NotEmpty(t, l[0].Media.ID)
	require.ErrorIs(t, s.UpsertStatus(bg, model.Status{}), port.ErrInvalid)
}

func TestStatusViewsAndCounts(t *testing.T) {
	s := newTestAccount(t)
	require.NoError(t, s.UpsertStatus(bg, mkStatus("s1", "yo", 1000, 90_000, true)))
	key := string(testAcc) + "|s1"

	require.NoError(t, s.UpsertStatusView(bg, model.StatusView{StatusKey: key, ViewerID: "ana", ViewerName: "Ana", At: 200}))
	require.NoError(t, s.UpsertStatusView(bg, model.StatusView{StatusKey: key, ViewerID: "beto", At: 100}))
	require.NoError(t, s.UpsertStatusView(bg, model.StatusView{StatusKey: key, ViewerID: "ana", At: 300, Reaction: "🔥"}))
	// Una vista posterior no quita la reacción ni la primera hora.
	require.NoError(t, s.UpsertStatusView(bg, model.StatusView{StatusKey: key, ViewerID: "ana", At: 400}))
	// También acepta el id nativo sin cuenta.
	require.NoError(t, s.UpsertStatusView(bg, model.StatusView{StatusKey: "s1", ViewerID: "carla", At: 50, Reaction: "👍"}))
	require.Error(t, s.UpsertStatusView(bg, model.StatusView{ViewerID: "x"}))

	v, err := s.ListStatusViews(bg, key)
	require.NoError(t, err)
	require.Len(t, v, 3)
	require.Equal(t, "ana", v[0].ViewerID) // la más reciente primero (por hora de primera vista)
	require.Equal(t, "Ana", v[0].ViewerName)
	require.Equal(t, "🔥", v[0].Reaction)
	require.Equal(t, int64(200), v[0].At)
	require.Equal(t, key, v[0].StatusKey)
	v, _ = s.ListStatusViews(bg, "no-hay")
	require.NotNil(t, v)
	require.Empty(t, v)

	// Contadores en el estado: vistas, reacciones y respuestas (mensajes con fromStatusId).
	reply := mkMsg("ana", "r1", "me encantó", 1500, false)
	reply.FromStatusID = "s1"
	reply2 := mkMsg("beto", "r2", "yo también", 1600, false)
	reply2.FromStatusID = key
	_, _ = s.UpsertMessage(bg, reply)
	_, _ = s.UpsertMessage(bg, reply2)
	_, _ = s.UpsertMessage(bg, mkMsg("ana", "r3", "otra cosa", 1700, false))
	mine, err := s.ListMyStatuses(bg, 0, 0)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Equal(t, 3, mine[0].ViewCount)
	require.Equal(t, 2, mine[0].ReactionCount)
	require.Equal(t, 2, mine[0].ReplyCount)
	require.Equal(t, key, mine[0].Key)
}
