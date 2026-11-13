package stats

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"dconnector/internal/core/model"
)

// Lunes 5 de octubre de 2026, 10:00 locales (testLoc, definida en periods_test.go).
var t0 = time.Date(2026, 10, 5, 10, 0, 0, 0, testLoc)

type rowOpt func(*MessageRow)

func group() rowOpt                   { return func(r *MessageRow) { r.ChatKind = model.ChatGroup } }
func kind(k model.ContentKind) rowOpt { return func(r *MessageRow) { r.Kind = k } }
func sender(id string) rowOpt         { return func(r *MessageRow) { r.SenderID = id } }

// row: mensaje de `chat` en la cuenta a1, `minutes` después de t0.
func row(chat string, me bool, minutes int, opts ...rowOpt) MessageRow {
	r := MessageRow{
		AccountID: "a1", ChatNativeID: chat, ChatKind: model.ChatDirect, FromMe: me,
		Timestamp: t0.Add(time.Duration(minutes) * time.Minute).UnixMilli(), Kind: model.KindText,
	}
	if !me {
		r.SenderID = chat
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func TestSummarize(t *testing.T) {
	rows := []MessageRow{
		// Chat A: te escriben, respondes a los 30 min; tú escribes y responden a las 2 h
		row("A", false, 0), row("A", false, 5), row("A", true, 30), row("A", true, 31), row("A", false, 151),
		// 10 h después: conversación nueva iniciada por ti, sin respuesta
		row("A", true, 151+600),
		// Chat B: escribes tú, responden en 3 min; respondes en 3 h; nota de voz
		row("B", true, 0), row("B", false, 3, kind(model.KindVoice)), row("B", true, 183, kind(model.KindImage)),
		// Grupo: no cuenta para respuestas ni inicios
		row("G", false, 0, group()), row("G", true, 1, group()),
	}
	s := Summarize(rows)
	if s.Sent != 6 || s.Received != 5 || s.Chats != 3 {
		t.Fatalf("totales: %+v", s)
	}
	// Tuyas: A 30 min, A 600 min, B 180 min -> mediana 180 min.
	if s.Response.MineCount != 3 || s.Response.MineMedianSec == nil || *s.Response.MineMedianSec != 180*60 {
		t.Fatalf("mías: %+v", s.Response)
	}
	if s.Response.MineUnderHour == nil || *s.Response.MineUnderHour < 0.333 || *s.Response.MineUnderHour > 0.334 {
		t.Fatalf("bajo una hora: %v", s.Response.MineUnderHour)
	}
	// Suyas: A 121 min (desde tu primer mensaje de la tanda), B 3 min -> mediana (121+3)/2 min.
	want := int64((121*60 + 3*60 + 1) / 2)
	if s.Response.TheirsCount != 2 || s.Response.TheirsMedianSec == nil || *s.Response.TheirsMedianSec != want {
		t.Fatalf("suyas: %+v (quería %d)", s.Response, want)
	}
	if s.StartedByThem != 1 || s.StartedByMe != 2 {
		t.Fatalf("inicios: me=%d ellos=%d", s.StartedByMe, s.StartedByThem)
	}
	if want := (ContentMix{Text: 9, Image: 1, Voice: 1}); s.Mix != want {
		t.Fatalf("mezcla: %+v", s.Mix)
	}
}

func TestSummarizeOrdersByChatAndTime(t *testing.T) {
	// Las filas del almacén vienen por hora, mezclando chats; el resultado no puede cambiar.
	sorted := []MessageRow{row("A", false, 0), row("A", true, 30), row("B", true, 0), row("B", false, 3)}
	mixed := []MessageRow{sorted[2], sorted[0], sorted[3], sorted[1]}
	if !reflect.DeepEqual(Summarize(sorted), Summarize(mixed)) {
		t.Fatalf("depende del orden:\n%+v\n%+v", Summarize(sorted), Summarize(mixed))
	}
	if Summarize(mixed).Chats != 2 {
		t.Fatal("chats")
	}
	// Dos cuentas con el mismo id de chat son chats distintos.
	b := row("A", false, 0)
	b.AccountID = "a2"
	if got := Summarize([]MessageRow{row("A", false, 0), b}).Chats; got != 2 {
		t.Fatalf("chats entre cuentas: %d", got)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	s := Summarize(nil)
	if s.Sent != 0 || s.Response.MineMedianSec != nil || s.Response.MineUnderHour != nil || s.Response.TheirsMedianSec != nil {
		t.Fatalf("vacío: %+v", s)
	}
}

func TestSummarizeConversationGapBoundary(t *testing.T) {
	// Exactamente 8 h sin hablar ya es conversación nueva; 7 h 59 min no.
	rows := []MessageRow{row("A", false, 0), row("A", false, 8*60)}
	if s := Summarize(rows); s.StartedByThem != 2 {
		t.Fatalf("8 h: %+v", s)
	}
	rows = []MessageRow{row("A", false, 0), row("A", false, 8*60-1)}
	if s := Summarize(rows); s.StartedByThem != 1 {
		t.Fatalf("7 h 59: %+v", s)
	}
}

func TestPerDayFillsEmptyDays(t *testing.T) {
	rows := []MessageRow{row("A", true, 0), row("A", false, 60), row("A", false, 24*60+5)}
	days := PerDay(rows, t0, t0.Add(48*time.Hour), testLoc)
	var keys []string
	for _, d := range days {
		keys = append(keys, d.Day)
	}
	if !reflect.DeepEqual(keys, []string{"2026-10-05", "2026-10-06", "2026-10-07"}) {
		t.Fatalf("días: %v", keys)
	}
	if days[0].Sent != 1 || days[0].Received != 1 || days[1].Received != 1 || days[2].Sent+days[2].Received != 0 {
		t.Fatalf("conteos: %+v", days)
	}
}

func TestPerDayToIsExclusiveAtMidnight(t *testing.T) {
	days := PerDay(nil, day("2026-10-05"), day("2026-10-08"), testLoc)
	if len(days) != 3 || days[2].Day != "2026-10-07" {
		t.Fatalf("días: %+v", days)
	}
	if got := PerDay(nil, t0, t0, testLoc); got == nil || len(got) != 0 {
		t.Fatalf("vacío debe ser []: %#v", got)
	}
}

func TestHeatmap(t *testing.T) {
	rows := []MessageRow{row("A", true, 0), row("A", false, 60), row("A", false, 24*60+5)}
	g := Heatmap(rows, HeatAll, testLoc)
	if len(g) != 7 || len(g[0]) != 24 {
		t.Fatalf("forma %dx%d", len(g), len(g[0]))
	}
	if g[0][10] != 1 || g[0][11] != 1 || g[1][10] != 1 {
		t.Fatalf("lunes = 0, hora local: %v %v", g[0], g[1])
	}
	if Heatmap(rows, HeatSent, testLoc)[0][10] != 1 || Heatmap(rows, HeatReceived, testLoc)[0][10] != 0 {
		t.Fatal("sent/received")
	}
	// Domingo = 6.
	sun := row("A", false, 6*24*60) // domingo 11 oct, 10:00
	if Heatmap([]MessageRow{sun}, HeatAll, testLoc)[6][10] != 1 {
		t.Fatal("domingo")
	}
}

func TestPerChat(t *testing.T) {
	rows := []MessageRow{row("A", true, 0), row("A", false, 60), row("A", false, 24*60+5)}
	got := PerChat(rows, testLoc)
	want := []ChatStat{{Chat: "a1|A", Total: 3, Sent: 1, Received: 2, ActiveDays: 2, Last: t0.Add((24*60 + 5) * time.Minute).UnixMilli()}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestMedian(t *testing.T) {
	if _, ok := Median(nil); ok {
		t.Fatal("vacío")
	}
	if m, _ := Median([]int64{5, 1, 3}); m != 3 {
		t.Fatal(m)
	}
	if m, _ := Median([]int64{1, 2, 3, 4}); m != 3 { // round(2.5) = 3
		t.Fatal(m)
	}
	in := []int64{3, 1, 2}
	Median(in)
	if !reflect.DeepEqual(in, []int64{3, 1, 2}) {
		t.Fatal("no debe ordenar la entrada")
	}
}

func TestMessageRowFilters(t *testing.T) {
	cases := []struct {
		kind model.ChatKind
		k    model.ContentKind
		want bool
	}{
		{model.ChatDirect, model.KindText, true},
		{model.ChatGroup, model.KindImage, true},
		{model.ChatGroup, model.KindSystem, false},
		{model.ChatChannel, model.KindText, false},
		{model.ChatSelf, model.KindText, false},
	}
	for _, c := range cases {
		if got := (MessageRow{ChatKind: c.kind, Kind: c.k}).Countable(); got != c.want {
			t.Errorf("%s/%s: %v", c.kind, c.k, got)
		}
	}
}

func TestConversationOverview(t *testing.T) {
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, testLoc)
	rng := Range{Days: 7}
	at := func(chat string, me bool, daysAgo int, hour int, opts ...rowOpt) MessageRow {
		r := row(chat, me, 0, opts...)
		d := now.AddDate(0, 0, -daysAgo)
		r.Timestamp = time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, testLoc).UnixMilli()
		return r
	}
	rows := []MessageRow{
		// periodo actual (7 días: 3 al 9 de octubre; ahora = 9 a las 15:00)
		at("A", false, 0, 9), at("A", true, 0, 10), at("A", false, 2, 9), at("A", true, 2, 12),
		at("B", false, 1, 20),
		at("G", false, 1, 8, group(), sender("p1")), at("G", true, 1, 9, group()),
		// anterior (26 de sep al 2 de oct)
		at("A", false, 9, 9), at("A", true, 9, 10),
	}
	o := BuildConversationOverview(rng, now, testLoc, rows)
	if o.Current.Sent != 2 || o.Current.Received != 3 || o.Current.Chats != 2 {
		t.Fatalf("actual: %+v", o.Current)
	}
	if o.Previous.Sent != 1 || o.Previous.Received != 1 {
		t.Fatalf("anterior: %+v", o.Previous)
	}
	if o.GroupSent != 1 || o.GroupReceived != 1 {
		t.Fatalf("grupos: %d %d", o.GroupSent, o.GroupReceived)
	}
	if o.Changes.Sent == nil || *o.Changes.Sent != 1 { // 1 -> 2 = +100 %
		t.Fatalf("variación enviados: %v", o.Changes.Sent)
	}
	if o.Changes.Chats == nil || *o.Changes.Chats != 1 { // 1 -> 2
		t.Fatalf("variación chats: %v", o.Changes.Chats)
	}
	if o.Period.Days != 7 || o.Period.Granularity != GranDay || o.Period.ToMs != now.UnixMilli() {
		t.Fatalf("periodo: %+v", o.Period)
	}
	if len(o.PerDay) != 7 || o.PerDay[0].Day != "2026-10-03" || o.PerDay[6].Day != "2026-10-09" {
		t.Fatalf("por día: %+v", o.PerDay)
	}
	// Los grupos entran en el gráfico por día pero no en el mapa de calor.
	var heat int
	for _, r := range o.Heatmap {
		for _, v := range r {
			heat += v
		}
	}
	if heat != 5 {
		t.Fatalf("mapa de calor: %d", heat)
	}
	var sent, recv int
	for _, d := range o.PerDay {
		sent, recv = sent+d.Sent, recv+d.Received
	}
	if sent != 3 || recv != 4 {
		t.Fatalf("suma por día: %d %d", sent, recv)
	}
	if len(o.TopChats) != 2 || o.TopChats[0].ChatKey != "a1|A" || o.TopChats[0].Total != 4 || o.TopChats[0].AccountID != "a1" || o.TopChats[0].NativeID != "A" {
		t.Fatalf("top: %+v", o.TopChats)
	}
	if len(o.Frequent) != 2 || o.Frequent[0].ActiveDays != 2 {
		t.Fatalf("frecuentes: %+v", o.Frequent)
	}
	// Tiempos: tus respuestas del actual son 1 h y 3 h; las del anterior, 1 h.
	if o.Current.Response.MineMedianSec == nil || o.Previous.Response.MineMedianSec == nil {
		t.Fatalf("respuestas: %+v / %+v", o.Current.Response, o.Previous.Response)
	}
	if o.Changes.MineMedianSec == nil || *o.Changes.MineMedianSec != 1 { // 7200 s frente a 3600 s: +100 %
		t.Fatalf("variación tiempos: %v", o.Changes.MineMedianSec)
	}
}

func TestConversationOverviewEmptyJSON(t *testing.T) {
	o := BuildConversationOverview(Range{Days: 7}, testNow, testLoc, nil)
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, bad := range []string{`"topChats":null`, `"frequent":null`, `"perDay":null`, `"heatmap":null`} {
		if strings.Contains(s, bad) {
			t.Fatalf("lista null: %s", bad)
		}
	}
	for _, want := range []string{`"mineMedianSec":null`, `"mineUnderHour":null`, `"sent":0`, `"granularity":"day"`, `"topChats":[]`} {
		if !strings.Contains(s, want) {
			t.Fatalf("falta %s en %s", want, s)
		}
	}
	// 0 -> 0 no es «sin base»: es 0 %.
	if o.Changes.Sent == nil || *o.Changes.Sent != 0 {
		t.Fatalf("0 contra 0: %v", o.Changes.Sent)
	}
}

func TestConversationOverviewGroupsPerWeek(t *testing.T) {
	o := BuildConversationOverview(Range{Days: 120}, testNow, testLoc, []MessageRow{row("A", true, 0), row("A", false, 60)})
	if o.Period.Granularity != GranWeek {
		t.Fatalf("granularidad %s", o.Period.Granularity)
	}
	var total int
	for _, d := range o.PerDay {
		total += d.Sent + d.Received
		if d.Day != BucketKey(d.Day, GranWeek) {
			t.Fatalf("la clave %s no es un lunes", d.Day)
		}
	}
	if total != 2 {
		t.Fatalf("total %d", total)
	}
}

func TestBuildChatStats(t *testing.T) {
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, testLoc)
	old := row("A", false, 0)
	old.Timestamp = time.Date(2026, 1, 5, 10, 0, 0, 0, testLoc).UnixMilli() // fuera del periodo
	recent1 := row("A", true, 0)
	recent2 := row("A", false, 60)
	s := BuildChatStats(Range{Days: 7}, now, testLoc, []MessageRow{old, recent1, recent2})
	if s.Summary.Sent != 1 || s.Summary.Received != 2 {
		t.Fatalf("resumen de todo el historial: %+v", s.Summary)
	}
	if s.FirstAt != old.Timestamp || s.LastAt != recent2.Timestamp || s.ActiveDays != 2 {
		t.Fatalf("primero/último/días: %d %d %d", s.FirstAt, s.LastAt, s.ActiveDays)
	}
	if len(s.PerDay) != 7 {
		t.Fatalf("por día: %d", len(s.PerDay))
	}
	var n int
	for _, d := range s.PerDay {
		n += d.Sent + d.Received
	}
	if n != 2 {
		t.Fatalf("mensajes del periodo: %d", n)
	}
	// El mapa de calor es de lo recibido (de todo el historial).
	var heat int
	for _, r := range s.Heatmap {
		for _, v := range r {
			heat += v
		}
	}
	if heat != 2 {
		t.Fatalf("mapa de calor: %d", heat)
	}

	empty := BuildChatStats(Range{Days: 7}, now, testLoc, nil)
	if empty.FirstAt != 0 || empty.LastAt != 0 || empty.PerDay == nil || len(empty.PerDay) != 7 {
		t.Fatalf("vacío: %+v", empty)
	}
	b, _ := json.Marshal(empty)
	if !strings.Contains(string(b), `"mineMedianSec":null`) {
		t.Fatalf("json: %s", b)
	}
}

func TestBuildGroupsOverview(t *testing.T) {
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, testLoc)
	at := func(chat string, me bool, daysAgo int, who string) MessageRow {
		r := row(chat, me, 0, group(), sender(who))
		d := now.AddDate(0, 0, -daysAgo)
		r.Timestamp = time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, testLoc).UnixMilli()
		return r
	}
	rows := []MessageRow{
		at("G1", false, 0, "ana"), at("G1", false, 0, "ana"), at("G1", false, 1, "beto"), at("G1", true, 1, ""),
		at("G2", false, 2, "ana"), at("G2", false, 2, "carla"),
		at("G3", false, 30, "dani"), // fuera del periodo
		row("A", false, 0),          // chat individual: no es un grupo
	}
	g := BuildGroupsOverview(Range{Days: 7}, now, testLoc, rows)
	if g.GroupSent != 1 || g.GroupReceived != 5 {
		t.Fatalf("totales: %d %d", g.GroupSent, g.GroupReceived)
	}
	if len(g.Groups) != 2 || g.Groups[0].ChatKey != "a1|G1" || g.Groups[0].Total != 4 || g.Groups[0].Mine != 1 ||
		g.Groups[0].Authors != 2 || g.Groups[0].ActiveDays != 2 {
		t.Fatalf("grupos: %+v", g.Groups)
	}
	if g.Groups[0].AccountID != "a1" || g.Groups[0].NativeID != "G1" {
		t.Fatalf("ids: %+v", g.Groups[0])
	}
	if len(g.People) != 3 || g.People[0].SenderID != "ana" || g.People[0].Total != 3 || g.People[0].Groups != 2 {
		t.Fatalf("personas: %+v", g.People)
	}
	for _, p := range g.People {
		if p.SenderID == "" || p.SenderID == "dani" {
			t.Fatalf("persona que no cuenta: %+v", p)
		}
	}
	empty := BuildGroupsOverview(Range{Days: 7}, now, testLoc, nil)
	b, _ := json.Marshal(empty)
	if !strings.Contains(string(b), `"groups":[]`) || !strings.Contains(string(b), `"people":[]`) {
		t.Fatalf("json: %s", b)
	}
}

func TestBuildGroupsOverviewLimits(t *testing.T) {
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, testLoc)
	var rows []MessageRow
	for i := 0; i < 60; i++ {
		r := row("G"+string(rune('A'+i%26))+string(rune('a'+i/26)), false, 0, group(), sender("p"+string(rune('A'+i%26))+string(rune('a'+i/26))))
		r.Timestamp = now.Add(-time.Hour).UnixMilli()
		rows = append(rows, r)
	}
	g := BuildGroupsOverview(Range{Days: 7}, now, testLoc, rows)
	if len(g.Groups) != groupsLimit || len(g.People) != peopleLimit {
		t.Fatalf("límites: %d %d", len(g.Groups), len(g.People))
	}
}
