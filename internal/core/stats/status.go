package stats

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"dconnector/internal/core/model"
)

// Estadísticas de mis estados: lógica pura portada de ../dconnector/src/shared/statusStats.ts
// (reachRatios, loyaltyBuckets, viewsCurve, pctChange, statusLabel). Sin categorías de producto
// ni listas de clientes (D5 de docs/01). Todas las marcas de tiempo son milisegundos.

// LoyaltyBucket: cuántas personas vieron qué parte de mis estados del periodo.
type LoyaltyBucket struct {
	Key    string `json:"key"` // all | most | half | some | few
	Label  string `json:"label"`
	People int    `json:"people"`
}

// CurvePoint: vistas acumuladas hasta `Hour` horas después de publicar (0..24).
type CurvePoint struct {
	Hour  int `json:"hour"`
	Views int `json:"views"`
}

// StatusSummary: los KPIs de un periodo.
type StatusSummary struct {
	Posts int `json:"posts"` // estados publicados
	// Views: impresiones; cada vez que una persona vio un estado.
	Views    int     `json:"views"`
	AvgViews float64 `json:"avgViews"` // vistas medias por estado
	// Reach: alcance; personas distintas que vieron al menos un estado.
	Reach     int `json:"reach"`
	Reactions int `json:"reactions"`
	Replies   int `json:"replies"`
	// Engagement: (reacciones + respuestas) / vistas.
	Engagement float64 `json:"engagement"`
	// Frequency: vistas por persona alcanzada.
	Frequency float64 `json:"frequency"`
	// Loyalty (fidelidad, 0..1): qué parte de mis estados ve en promedio cada persona alcanzada.
	Loyalty float64 `json:"loyalty"`
	// EngagedPeople: personas distintas que reaccionaron.
	EngagedPeople int `json:"engagedPeople"`
}

// StatusChanges: variación porcentual del periodo actual frente al anterior (0,25 = +25 %).
// nil cuando no hay base para comparar (antes 0, ahora algo).
type StatusChanges struct {
	Posts     *float64 `json:"posts"`
	Views     *float64 `json:"views"`
	AvgViews  *float64 `json:"avgViews"`
	Reach     *float64 `json:"reach"`
	Reactions *float64 `json:"reactions"`
	Replies   *float64 `json:"replies"`
}

// StatusDay: la serie por día (o por semana o mes según Period.Granularity) de los estados
// publicados en ese día. Todos los campos son sumables.
type StatusDay struct {
	Day       string `json:"day"` // AAAA-MM-DD; semana = su lunes; mes = AAAA-MM
	Posts     int    `json:"posts"`
	Views     int    `json:"views"`
	Reactions int    `json:"reactions"`
	Replies   int    `json:"replies"`
}

// StatusReachRow: el alcance de cada estado del periodo, del más visto al menos visto.
type StatusReachRow struct {
	Key       string            `json:"key"`
	PostedAt  int64             `json:"postedAt"`
	Kind      model.ContentKind `json:"kind"`
	Label     string            `json:"label"`
	Views     int               `json:"views"`
	Reactions int               `json:"reactions"`
	Replies   int               `json:"replies"`
	// Reach: parte (0..1) de las personas alcanzadas en el periodo que vieron este estado.
	Reach float64 `json:"reach"`
}

// StatusPeriod: el periodo analizado y el anterior con el que se compara.
type StatusPeriod struct {
	FromMs      int64       `json:"fromMs"`
	ToMs        int64       `json:"toMs"`
	PrevFromMs  int64       `json:"prevFromMs"`
	PrevToMs    int64       `json:"prevToMs"`
	Days        int         `json:"days"`
	Granularity Granularity `json:"granularity"`
}

// StatusStats: lo que devuelve Statuses.Stats (06 §10, 11 §6.3).
type StatusStats struct {
	Period   StatusPeriod  `json:"period"`
	Current  StatusSummary `json:"current"`
	Previous StatusSummary `json:"previous"`
	Changes  StatusChanges `json:"changes"`
	// PerDay: estados, vistas, reacciones y respuestas por día de publicación.
	PerDay []StatusDay `json:"perDay"`
	// PerStatus: alcance de cada estado del periodo actual.
	PerStatus []StatusReachRow `json:"perStatus"`
	// Curve: vistas acumuladas por hora desde la publicación, sumadas sobre los estados del
	// periodo actual (dividir entre Current.Posts para la media por estado).
	Curve []CurvePoint `json:"curve"`
	// Loyalty: cuántas personas vieron todos, la mayoría, la mitad… de mis estados.
	Loyalty []LoyaltyBucket `json:"loyalty"`
}

// StatusInput: un estado mío con las vistas conocidas.
type StatusInput struct {
	Status model.Status
	Views  []model.StatusView
}

// ReachRatios: frecuencia (vistas por persona) y fidelidad (parte de mis estados que ve cada
// persona, entre 0 y 1).
func ReachRatios(views, people, posts int) (frequency, loyalty float64) {
	if people <= 0 || posts <= 0 {
		return 0, 0
	}
	loyalty = float64(views) / float64(people*posts)
	if loyalty > 1 {
		loyalty = 1
	}
	return float64(views) / float64(people), loyalty
}

// LoyaltyBuckets reparte a las personas según la parte de mis estados que vieron: todos, 75 % o
// más, la mitad o más, la cuarta parte o más, y menos. `seen` = estados que vio cada persona.
func LoyaltyBuckets(seen []int, posts int) []LoyaltyBucket {
	out := []LoyaltyBucket{
		{Key: "all", Label: "Todos"},
		{Key: "most", Label: "75–99 %"},
		{Key: "half", Label: "50–74 %"},
		{Key: "some", Label: "25–49 %"},
		{Key: "few", Label: "Menos del 25 %"},
	}
	if posts <= 0 {
		return out
	}
	for _, n := range seen {
		share := float64(n) / float64(posts)
		i := 4
		switch {
		case share >= 1:
			i = 0
		case share >= 0.75:
			i = 1
		case share >= 0.5:
			i = 2
		case share >= 0.25:
			i = 3
		}
		out[i].People++
	}
	return out
}

// ViewsCurve: vistas acumuladas por hora (0..24) a partir de la hora de publicación y de la
// hora de cada vista (ms).
func ViewsCurve(postedAt int64, viewTimes []int64) []CurvePoint {
	sorted := append([]int64(nil), viewTimes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	out := make([]CurvePoint, 0, 25)
	for h := 0; h <= 24; h++ {
		limit := postedAt + int64(h)*3_600_000
		n := sort.Search(len(sorted), func(i int) bool { return sorted[i] > limit })
		out = append(out, CurvePoint{Hour: h, Views: n})
	}
	return out
}

// PctChange: variación porcentual como fracción (0,25 = +25 %). nil si no hay base para
// comparar (antes 0 y ahora algo); 0 si ambos son 0.
func PctChange(current, previous float64) *float64 {
	if previous == 0 {
		if current != 0 {
			return nil
		}
		z := 0.0
		return &z
	}
	v := (current - previous) / previous
	return &v
}

// StatusLabel: etiqueta corta de un estado para gráficos y listas.
func StatusLabel(kind model.ContentKind, text string) string {
	t := strings.Join(strings.Fields(text), " ")
	if t != "" {
		if utf8.RuneCountInString(t) > 40 {
			r := []rune(t)
			return string(r[:40]) + "…"
		}
		return t
	}
	switch kind {
	case model.KindVideo:
		return "Video"
	case model.KindImage:
		return "Foto"
	}
	return "Estado"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// statusAgg: lo calculado sobre un conjunto de estados.
type statusAgg struct {
	summary   StatusSummary
	perStatus []StatusReachRow
	seen      []int // estados distintos que vio cada persona alcanzada
	curve     []CurvePoint
	days      map[string]*StatusDay
}

// aggregate calcula los KPIs de un conjunto de estados míos. Las vistas de un estado son el
// mayor entre el contador del canal y las filas de vistas conocidas (las filas pueden ir por
// detrás del contador); lo mismo con las reacciones.
func aggregate(in []StatusInput, loc *time.Location) statusAgg {
	agg := statusAgg{curve: make([]CurvePoint, 25), days: map[string]*StatusDay{}}
	for h := range agg.curve {
		agg.curve[h].Hour = h
	}
	seenBy := map[string]int{}
	reacted := map[string]struct{}{}
	for _, it := range in {
		st := it.Status
		if !st.FromMe {
			continue
		}
		viewers := map[string]model.StatusView{}
		for _, v := range it.Views {
			if v.ViewerID == "" {
				continue
			}
			if old, ok := viewers[v.ViewerID]; ok && old.Reaction != "" && v.Reaction == "" {
				continue
			}
			viewers[v.ViewerID] = v
		}
		reactions := 0
		times := make([]int64, 0, len(viewers))
		for id, v := range viewers {
			seenBy[id]++
			if v.Reaction != "" {
				reactions++
				reacted[id] = struct{}{}
			}
			if v.At > 0 {
				times = append(times, v.At)
			}
		}
		views := maxInt(st.ViewCount, len(viewers))
		reactions = maxInt(st.ReactionCount, reactions)
		replies := st.ReplyCount

		agg.summary.Posts++
		agg.summary.Views += views
		agg.summary.Reactions += reactions
		agg.summary.Replies += replies
		agg.perStatus = append(agg.perStatus, StatusReachRow{
			Key: st.Key, PostedAt: st.PostedAt, Kind: st.Kind, Label: StatusLabel(st.Kind, st.Text),
			Views: views, Reactions: reactions, Replies: replies,
		})
		for _, p := range ViewsCurve(st.PostedAt, times) {
			agg.curve[p.Hour].Views += p.Views
		}
		day := DayKey(st.PostedAt, loc)
		d := agg.days[day]
		if d == nil {
			d = &StatusDay{Day: day}
			agg.days[day] = d
		}
		d.Posts++
		d.Views += views
		d.Reactions += reactions
		d.Replies += replies
	}
	s := &agg.summary
	s.Reach = len(seenBy)
	s.EngagedPeople = len(reacted)
	if s.Posts > 0 {
		s.AvgViews = float64(s.Views) / float64(s.Posts)
	}
	if s.Views > 0 {
		s.Engagement = float64(s.Reactions+s.Replies) / float64(s.Views)
	}
	s.Frequency, s.Loyalty = ReachRatios(s.Views, s.Reach, s.Posts)
	agg.seen = make([]int, 0, len(seenBy))
	for _, n := range seenBy {
		agg.seen = append(agg.seen, n)
	}
	for i := range agg.perStatus {
		if s.Reach > 0 {
			r := float64(agg.perStatus[i].Views) / float64(s.Reach)
			if r > 1 {
				r = 1
			}
			agg.perStatus[i].Reach = r
		}
	}
	sort.SliceStable(agg.perStatus, func(i, j int) bool {
		a, b := agg.perStatus[i], agg.perStatus[j]
		if a.Views != b.Views {
			return a.Views > b.Views
		}
		return a.PostedAt > b.PostedAt
	})
	return agg
}

// BuildStatusStats arma las estadísticas de mis estados: `cur` son los estados publicados en el
// periodo actual (Bounds) y `prev` los del anterior (PreviousBounds), cada uno con sus vistas.
// `now` y `loc` fijan los límites y los cortes de día (loc nil = time.Local).
func BuildStatusStats(rng Range, now time.Time, loc *time.Location, cur, prev []StatusInput) StatusStats {
	loc = orLocal(loc)
	b := computeBounds(rng, now, loc)
	gran := GranularityFor(b.days)

	c := aggregate(cur, loc)
	p := aggregate(prev, loc)

	days := make([]StatusDay, 0, len(c.days))
	for _, d := range c.days {
		days = append(days, *d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Day < days[j].Day })

	perStatus := c.perStatus
	if perStatus == nil {
		perStatus = []StatusReachRow{}
	}
	return StatusStats{
		Period: StatusPeriod{
			FromMs: b.from.UnixMilli(), ToMs: b.to.UnixMilli(),
			PrevFromMs: b.prevFrom.UnixMilli(), PrevToMs: b.prevTo.UnixMilli(),
			Days: b.days, Granularity: gran,
		},
		Current:  c.summary,
		Previous: p.summary,
		Changes: StatusChanges{
			Posts:     PctChange(float64(c.summary.Posts), float64(p.summary.Posts)),
			Views:     PctChange(float64(c.summary.Views), float64(p.summary.Views)),
			AvgViews:  PctChange(c.summary.AvgViews, p.summary.AvgViews),
			Reach:     PctChange(float64(c.summary.Reach), float64(p.summary.Reach)),
			Reactions: PctChange(float64(c.summary.Reactions), float64(p.summary.Reactions)),
			Replies:   PctChange(float64(c.summary.Replies), float64(p.summary.Replies)),
		},
		PerDay:    Bucketize(days, gran),
		PerStatus: perStatus,
		Curve:     c.curve,
		Loyalty:   LoyaltyBuckets(c.seen, c.summary.Posts),
	}
}
