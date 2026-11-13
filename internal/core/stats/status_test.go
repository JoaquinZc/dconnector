package stats

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"dconnector/internal/core/model"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestReachRatios(t *testing.T) {
	f, l := ReachRatios(0, 0, 0)
	if f != 0 || l != 0 {
		t.Fatalf("sin datos: %v %v", f, l)
	}
	f, l = ReachRatios(10, 5, 0)
	if f != 0 || l != 0 {
		t.Fatalf("sin estados: %v %v", f, l)
	}
	// 12 vistas, 4 personas, 6 estados: 3 vistas por persona; cada una ve la mitad.
	f, l = ReachRatios(12, 4, 6)
	if !near(f, 3) || !near(l, 0.5) {
		t.Fatalf("ratios: %v %v", f, l)
	}
	// La fidelidad se acota a 1 (el contador del canal puede ir por delante de las personas).
	_, l = ReachRatios(100, 2, 3)
	if l != 1 {
		t.Fatalf("fidelidad sin acotar: %v", l)
	}
}

func TestLoyaltyBuckets(t *testing.T) {
	// 8 estados: 8 = todos, 6 = 75 %, 4 = 50 %, 2 = 25 %, 1 = menos, 0 = menos.
	b := LoyaltyBuckets([]int{8, 8, 6, 5, 4, 2, 1, 0}, 8)
	got := map[string]int{}
	var keys []string
	for _, x := range b {
		got[x.Key] = x.People
		keys = append(keys, x.Key)
	}
	if !reflect.DeepEqual(keys, []string{"all", "most", "half", "some", "few"}) {
		t.Fatalf("claves: %v", keys)
	}
	want := map[string]int{"all": 2, "most": 1, "half": 2, "some": 1, "few": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reparto: %v", got)
	}
	if b[1].Label != "75–99 %" || b[4].Label != "Menos del 25 %" {
		t.Fatalf("etiquetas: %+v", b)
	}
	// Sin estados no hay reparto, pero los cinco tramos existen.
	if e := LoyaltyBuckets([]int{3}, 0); len(e) != 5 || e[0].People != 0 {
		t.Fatalf("sin estados: %+v", e)
	}
}

func TestViewsCurve(t *testing.T) {
	const h = int64(3_600_000)
	posted := int64(1_000_000)
	c := ViewsCurve(posted, []int64{posted + 2*h, posted + 30*60_000, posted + 25*h, posted + 2*h})
	if len(c) != 25 || c[0].Hour != 0 || c[24].Hour != 24 {
		t.Fatalf("forma: %d puntos", len(c))
	}
	checks := map[int]int{0: 0, 1: 1, 2: 3, 23: 3, 24: 3}
	for hour, want := range checks {
		if c[hour].Views != want {
			t.Errorf("hora %d: %d vistas, esperaba %d", hour, c[hour].Views, want)
		}
	}
	// Una vista justo en el límite cuenta; la entrada no se reordena.
	in := []int64{posted + 3*h, posted}
	if c := ViewsCurve(posted, in); c[0].Views != 1 || c[3].Views != 2 {
		t.Fatalf("límites: %+v", c[:4])
	}
	if in[0] != posted+3*h {
		t.Fatal("ViewsCurve reordenó la entrada")
	}
	if c := ViewsCurve(posted, nil); c[24].Views != 0 {
		t.Fatalf("sin vistas: %+v", c[24])
	}
}

func TestPctChange(t *testing.T) {
	if v := PctChange(15, 10); v == nil || !near(*v, 0.5) {
		t.Fatalf("subida: %v", v)
	}
	if v := PctChange(5, 10); v == nil || !near(*v, -0.5) {
		t.Fatalf("bajada: %v", v)
	}
	if v := PctChange(0, 0); v == nil || *v != 0 {
		t.Fatalf("0 contra 0: %v", v)
	}
	if v := PctChange(3, 0); v != nil {
		t.Fatalf("sin base debe ser nil: %v", *v)
	}
	if v := PctChange(0, 4); v == nil || !near(*v, -1) {
		t.Fatalf("a cero: %v", v)
	}
}

func TestStatusLabel(t *testing.T) {
	if got := StatusLabel(model.KindText, "  Hola   mundo \n"); got != "Hola mundo" {
		t.Fatalf("texto: %q", got)
	}
	long := strings.Repeat("á", 50)
	if got := StatusLabel(model.KindText, long); got != strings.Repeat("á", 40)+"…" {
		t.Fatalf("recorte: %q", got)
	}
	if StatusLabel(model.KindVideo, "") != "Video" || StatusLabel(model.KindImage, " ") != "Foto" || StatusLabel(model.KindText, "") != "Estado" {
		t.Fatal("etiquetas por tipo")
	}
}

func ms(y int, m time.Month, d, h, min int) int64 {
	return time.Date(y, m, d, h, min, 0, 0, testLoc).UnixMilli()
}

func mine(key string, postedAt int64, kind model.ContentKind) model.Status {
	return model.Status{Key: key, NativeID: key, FromMe: true, PostedAt: postedAt, Kind: kind, Text: "estado " + key}
}

func TestBuildStatusStats(t *testing.T) {
	const hour = int64(3_600_000)
	s1 := mine("s1", ms(2026, 10, 5, 9, 0), model.KindText)
	s1.ViewCount = 3 // el canal dice 3; solo se conocen 2 filas
	s2 := mine("s2", ms(2026, 10, 6, 9, 0), model.KindImage)
	s2.ReplyCount = 2
	other := mine("x", ms(2026, 10, 6, 9, 0), model.KindText)
	other.FromMe = false // ajeno: se ignora
	other.ViewCount = 99

	cur := []StatusInput{
		{Status: s1, Views: []model.StatusView{
			{StatusKey: "s1", ViewerID: "ana", At: s1.PostedAt + hour, Reaction: "❤️"},
			{StatusKey: "s1", ViewerID: "beto", At: s1.PostedAt + 30*hour},
			{StatusKey: "s1", ViewerID: "", At: s1.PostedAt}, // sin id: se ignora
		}},
		{Status: s2, Views: []model.StatusView{
			{StatusKey: "s2", ViewerID: "ana", At: s2.PostedAt + 2*hour},
		}},
		{Status: other},
	}
	p1 := mine("p1", ms(2026, 9, 30, 10, 0), model.KindVideo)
	prev := []StatusInput{{Status: p1, Views: []model.StatusView{{StatusKey: "p1", ViewerID: "ana", At: p1.PostedAt + hour}}}}

	st := BuildStatusStats(Range{Days: 7}, testNow, testLoc, cur, prev)

	c := st.Current
	if c.Posts != 2 || c.Views != 4 || c.Reach != 2 || c.Reactions != 1 || c.Replies != 2 || c.EngagedPeople != 1 {
		t.Fatalf("KPIs: %+v", c)
	}
	if !near(c.AvgViews, 2) || !near(c.Engagement, 0.75) || !near(c.Frequency, 2) || !near(c.Loyalty, 1) {
		t.Fatalf("ratios: %+v", c)
	}
	if p := st.Previous; p.Posts != 1 || p.Views != 1 || p.Reach != 1 {
		t.Fatalf("anterior: %+v", p)
	}
	if st.Changes.Posts == nil || !near(*st.Changes.Posts, 1) || st.Changes.Views == nil || !near(*st.Changes.Views, 3) {
		t.Fatalf("cambios: %+v", st.Changes)
	}
	if st.Changes.Reactions != nil { // antes 0, ahora 1: sin base
		t.Fatalf("reacciones sin base: %v", *st.Changes.Reactions)
	}

	if len(st.PerStatus) != 2 || st.PerStatus[0].Key != "s1" || st.PerStatus[0].Views != 3 || st.PerStatus[1].Key != "s2" {
		t.Fatalf("por estado: %+v", st.PerStatus)
	}
	if st.PerStatus[0].Reach != 1 || !near(st.PerStatus[1].Reach, 0.5) {
		t.Fatalf("alcance por estado: %+v", st.PerStatus)
	}
	if st.PerStatus[0].Label != "estado s1" || st.PerStatus[1].Kind != model.KindImage {
		t.Fatalf("etiqueta o tipo: %+v", st.PerStatus)
	}

	if st.Period.Days != 7 || st.Period.Granularity != GranDay ||
		st.Period.ToMs != testNow.UnixMilli() || st.Period.PrevToMs != st.Period.FromMs {
		t.Fatalf("periodo: %+v", st.Period)
	}

	wantDays := []StatusDay{
		{Day: "2026-10-05", Posts: 1, Views: 3, Reactions: 1},
		{Day: "2026-10-06", Posts: 1, Views: 1, Replies: 2},
	}
	if !reflect.DeepEqual(st.PerDay, wantDays) {
		t.Fatalf("por día: %+v", st.PerDay)
	}

	// Curva: s1 → ana +1 h, beto +30 h; s2 → ana +2 h.
	curve := map[int]int{0: 0, 1: 1, 2: 2, 24: 2}
	if len(st.Curve) != 25 {
		t.Fatalf("curva: %d puntos", len(st.Curve))
	}
	for h, want := range curve {
		if st.Curve[h].Views != want {
			t.Errorf("curva hora %d: %d, esperaba %d", h, st.Curve[h].Views, want)
		}
	}

	// Fidelidad: ana vio los 2 estados (todos); beto 1 de 2 (la mitad).
	got := map[string]int{}
	for _, b := range st.Loyalty {
		got[b.Key] = b.People
	}
	if got["all"] != 1 || got["half"] != 1 || got["most"] != 0 || got["some"] != 0 || got["few"] != 0 {
		t.Fatalf("fidelidad: %+v", st.Loyalty)
	}
}

func TestBuildStatusStatsEmptyAndJSON(t *testing.T) {
	st := BuildStatusStats(Range{Days: 30}, testNow, testLoc, nil, nil)
	if st.Current.Posts != 0 || st.PerDay == nil || st.PerStatus == nil || len(st.Curve) != 25 || len(st.Loyalty) != 5 {
		t.Fatalf("vacío: %+v", st)
	}
	if st.Changes.Posts == nil || *st.Changes.Posts != 0 {
		t.Fatalf("0 contra 0: %+v", st.Changes)
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"period"`, `"fromMs"`, `"granularity":"day"`, `"current"`, `"avgViews"`, `"reach"`,
		`"engagedPeople"`, `"previous"`, `"changes"`, `"perDay":[]`, `"perStatus":[]`, `"curve"`, `"loyalty"`} {
		if !strings.Contains(s, want) {
			t.Errorf("falta %s en %s", want, s)
		}
	}
}

func TestBuildStatusStatsWeeklyGranularity(t *testing.T) {
	// Un periodo de 180 días se agrupa por semana (lunes).
	a := mine("a", ms(2026, 10, 5, 9, 0), model.KindText) // lunes
	b := mine("b", ms(2026, 10, 7, 9, 0), model.KindText) // miércoles de la misma semana
	c := mine("c", ms(2026, 10, 12, 9, 0), model.KindText)
	a.ViewCount, b.ViewCount, c.ViewCount = 1, 2, 4
	st := BuildStatusStats(Range{Days: 180}, testNow, testLoc,
		[]StatusInput{{Status: a}, {Status: b}, {Status: c}}, nil)
	if st.Period.Granularity != GranWeek {
		t.Fatalf("granularidad: %s", st.Period.Granularity)
	}
	want := []StatusDay{
		{Day: "2026-10-05", Posts: 2, Views: 3},
		{Day: "2026-10-12", Posts: 1, Views: 4},
	}
	if !reflect.DeepEqual(st.PerDay, want) {
		t.Fatalf("por semana: %+v", st.PerDay)
	}
}
