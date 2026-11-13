package stats

import (
	"reflect"
	"testing"
	"time"
)

// Zona fija (Ecuador, UTC-5, sin horario de verano) para que las pruebas no dependan del equipo.
var testLoc = time.FixedZone("EC", -5*3600)

// Ahora: 9 de octubre de 2026 (viernes), 15:00 locales.
var testNow = time.Date(2026, 10, 9, 15, 0, 0, 0, testLoc)

func day(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, testLoc)
	if err != nil {
		panic(err)
	}
	return t
}

func TestGranularityFor(t *testing.T) {
	got := []Granularity{GranularityFor(30), GranularityFor(92), GranularityFor(93), GranularityFor(180), GranularityFor(365), GranularityFor(400), GranularityFor(730)}
	want := []Granularity{GranDay, GranDay, GranWeek, GranWeek, GranWeek, GranWeek, GranMonth}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestBucketKey(t *testing.T) {
	cases := []struct {
		day  string
		g    Granularity
		want string
	}{
		{"2026-10-09", GranDay, "2026-10-09"},
		{"2026-10-09", GranWeek, "2026-10-05"}, // viernes -> lunes
		{"2026-10-05", GranWeek, "2026-10-05"}, // lunes -> él mismo
		{"2026-10-11", GranWeek, "2026-10-05"}, // domingo -> lunes anterior
		{"2026-10-09", GranMonth, "2026-10"},
		{"2026-01-01", GranWeek, "2025-12-29"}, // cruza de año
		{"basura", GranWeek, "basura"},
	}
	for _, c := range cases {
		if got := BucketKey(c.day, c.g); got != c.want {
			t.Errorf("BucketKey(%q,%s) = %q, want %q", c.day, c.g, got, c.want)
		}
	}
}

type dayRow struct {
	Day      string
	Sent     int
	Received int
	Rate     float64
	Note     string
}

func TestBucketize(t *testing.T) {
	rows := []dayRow{
		{Day: "2026-10-05", Sent: 1, Received: 2, Rate: 0.5, Note: "a"},
		{Day: "2026-10-07", Sent: 3, Received: 0, Rate: 0.25, Note: "b"},
		{Day: "2026-10-12", Sent: 1, Received: 1},
	}
	week := Bucketize(rows, GranWeek)
	want := []dayRow{
		{Day: "2026-10-05", Sent: 4, Received: 2, Rate: 0.75, Note: "a"},
		{Day: "2026-10-12", Sent: 1, Received: 1},
	}
	if !reflect.DeepEqual(week, want) {
		t.Fatalf("semana: %+v", week)
	}
	month := Bucketize(rows, GranMonth)
	if len(month) != 1 || month[0].Day != "2026-10" || month[0].Sent != 5 || month[0].Received != 3 {
		t.Fatalf("mes: %+v", month)
	}
	// No toca la entrada.
	if rows[0].Sent != 1 || rows[0].Day != "2026-10-05" {
		t.Fatalf("la entrada cambió: %+v", rows[0])
	}
	// Por día devuelve igual; un tipo sin Day también.
	if got := Bucketize(rows, GranDay); !reflect.DeepEqual(got, rows) {
		t.Fatalf("día: %+v", got)
	}
	nums := []int{1, 2}
	if got := Bucketize(nums, GranWeek); !reflect.DeepEqual(got, nums) {
		t.Fatalf("tipo sin Day: %+v", got)
	}
	if got := Bucketize([]dayRow(nil), GranWeek); len(got) != 0 {
		t.Fatalf("vacío: %+v", got)
	}
}

func TestNormalizeRange(t *testing.T) {
	cases := []struct {
		in, want Range
	}{
		{Range{Days: 90}, Range{Days: 90}},
		{Range{Days: 7, From: "2026-01-01", To: "2026-02-01"}, Range{Days: 7}},
		{Range{From: "2026-01-01", To: "2026-02-01"}, Range{From: "2026-01-01", To: "2026-02-01"}},
		{Range{From: "2026-02-01", To: "2026-01-01"}, Range{Days: 30}},
		{Range{From: "2026-02-01", To: "2026-02-01"}, Range{Days: 30}},
		{Range{From: "2026-2-1", To: "2026-03-01"}, Range{Days: 30}},
		{Range{From: "x", To: "y"}, Range{Days: 30}},
		{Range{}, Range{Days: 30}},
		{Range{Days: -3}, Range{Days: 30}},
	}
	for _, c := range cases {
		if got := NormalizeRange(c.in); got != c.want {
			t.Errorf("NormalizeRange(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSpanDays(t *testing.T) {
	if got := SpanDays(Range{From: "2025-01-01", To: "2026-01-01"}); got != 365 {
		t.Fatalf("año: %d", got)
	}
	if got := SpanDays(Range{From: "2024-01-01", To: "2025-01-01"}); got != 366 {
		t.Fatalf("año bisiesto: %d", got)
	}
	if got := SpanDays(Range{Days: 45}); got != 45 {
		t.Fatalf("días: %d", got)
	}
	if got := SpanDays(Range{}); got != 30 {
		t.Fatalf("por defecto: %d", got)
	}
}

func TestBoundsLastDays(t *testing.T) {
	from, to := Bounds(Range{Days: 30}, testNow, testLoc)
	if !to.Equal(testNow) || !from.Equal(testNow.Add(-30*24*time.Hour)) {
		t.Fatalf("actual: %v .. %v", from, to)
	}
	pf, pt := PreviousBounds(Range{Days: 30}, testNow, testLoc)
	if !pt.Equal(from) || !pf.Equal(testNow.Add(-60*24*time.Hour)) {
		t.Fatalf("anterior: %v .. %v", pf, pt)
	}
	// Un Range vacío vale 30 días.
	f2, t2 := Bounds(Range{}, testNow, testLoc)
	if !f2.Equal(from) || !t2.Equal(to) {
		t.Fatalf("vacío: %v .. %v", f2, t2)
	}
}

func TestBoundsFullYearCompareWithPreviousYear(t *testing.T) {
	rng := Range{From: "2025-01-01", To: "2026-01-01"}
	from, to := Bounds(rng, testNow, testLoc)
	if !from.Equal(day("2025-01-01")) || !to.Equal(day("2026-01-01")) {
		t.Fatalf("actual: %v .. %v", from, to)
	}
	pf, pt := PreviousBounds(rng, testNow, testLoc)
	// 2024 es bisiesto: el año anterior va completo, sin recortarlo.
	if !pf.Equal(day("2024-01-01")) || !pt.Equal(day("2025-01-01")) {
		t.Fatalf("anterior: %v .. %v", pf, pt)
	}
}

func TestBoundsMonthInProgress(t *testing.T) {
	rng := Range{From: "2026-10-01", To: "2026-11-01"}
	from, to := Bounds(rng, testNow, testLoc)
	if !to.Equal(testNow) || !from.Equal(day("2026-10-01")) {
		t.Fatalf("actual: %v .. %v", from, to)
	}
	pf, pt := PreviousBounds(rng, testNow, testLoc)
	if !pf.Equal(day("2026-09-01")) {
		t.Fatalf("desde anterior: %v", pf)
	}
	// Mismo tramo transcurrido que el mes en curso.
	if pt.Sub(pf) != to.Sub(from) {
		t.Fatalf("el tramo anterior mide %v y el actual %v", pt.Sub(pf), to.Sub(from))
	}
}

func TestBoundsSevenDayRange(t *testing.T) {
	// Rango no alineado a meses: el anterior son los 7 días previos, recortados al tramo en curso.
	rng := Range{From: "2026-10-03", To: "2026-10-10"}
	from, to := Bounds(rng, testNow, testLoc)
	if !from.Equal(day("2026-10-03")) || !to.Equal(testNow) {
		t.Fatalf("actual: %v .. %v", from, to)
	}
	pf, pt := PreviousBounds(rng, testNow, testLoc)
	if !pf.Equal(day("2026-09-26")) || pt.Sub(pf) != to.Sub(from) {
		t.Fatalf("anterior: %v .. %v", pf, pt)
	}
	// Ya terminado: el anterior va completo.
	done := Range{From: "2026-09-01", To: "2026-09-08"}
	pf, pt = PreviousBounds(done, testNow, testLoc)
	if !pf.Equal(day("2026-08-25")) || !pt.Equal(day("2026-09-01")) {
		t.Fatalf("anterior completo: %v .. %v", pf, pt)
	}
}

func TestBoundsMultiMonthCalendar(t *testing.T) {
	// Un trimestre completo se compara con el trimestre anterior.
	rng := Range{From: "2026-04-01", To: "2026-07-01"}
	pf, pt := PreviousBounds(rng, testNow, testLoc)
	if !pf.Equal(day("2026-01-01")) || !pt.Equal(day("2026-04-01")) {
		t.Fatalf("anterior: %v .. %v", pf, pt)
	}
}

func TestBoundsFuturePeriodDoesNotGoNegative(t *testing.T) {
	rng := Range{From: "2027-01-01", To: "2027-02-01"}
	from, to := Bounds(rng, testNow, testLoc)
	if to.Before(from) {
		t.Fatalf("to < from: %v .. %v", from, to)
	}
	pf, pt := PreviousBounds(rng, testNow, testLoc)
	if pt.Before(pf) {
		t.Fatalf("prevTo < prevFrom: %v .. %v", pf, pt)
	}
}

func TestDayKey(t *testing.T) {
	// 2026-10-10 02:00 UTC es 2026-10-09 21:00 en Ecuador.
	ms := time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC).UnixMilli()
	if got := DayKey(ms, testLoc); got != "2026-10-09" {
		t.Fatalf("DayKey = %s", got)
	}
	if got := DayKey(ms, time.UTC); got != "2026-10-10" {
		t.Fatalf("DayKey UTC = %s", got)
	}
}
