package stats

import (
	"reflect"
	"time"
)

// Periodos: lógica pura portada de ../dconnector/src/shared/periods.ts (statsBounds, rangeDays,
// granularityFor, bucketKey, bucketize, asStatsRange) y de previousPeriod (finance.ts).
//
// Un periodo es «los últimos N días» (Range.Days > 0, ventana móvil que termina ahora) o un
// rango de fechas locales [From, To) con formato AAAA-MM-DD. Los cortes de día usan la zona
// horaria que se pase (en producción, time.Local); nil equivale a time.Local.

const dayLayout = "2006-01-02"

// defaultDays: periodo por defecto cuando el Range no es válido.
const defaultDays = 30

// Granularity: detalle de los gráficos según la duración del periodo.
type Granularity string

const (
	GranDay   Granularity = "day"
	GranWeek  Granularity = "week"
	GranMonth Granularity = "month"
)

// GranularityFor: por día hasta ~3 meses, por semana hasta poco más de un año, luego por mes.
func GranularityFor(days int) Granularity {
	switch {
	case days <= 92:
		return GranDay
	case days <= 400:
		return GranWeek
	}
	return GranMonth
}

func orLocal(loc *time.Location) *time.Location {
	if loc == nil {
		return time.Local
	}
	return loc
}

func parseDay(s string, loc *time.Location) (time.Time, bool) {
	t, err := time.ParseInLocation(dayLayout, s, orLocal(loc))
	return t, err == nil
}

// NormalizeRange valida un Range (lo que llega de la interfaz o del protocolo): Days > 0 gana;
// si no, hacen falta dos fechas AAAA-MM-DD válidas con From < To; en cualquier otro caso, 30 días.
func NormalizeRange(r Range) Range {
	if r.Days > 0 {
		return Range{Days: r.Days}
	}
	from, okF := parseDay(r.From, time.UTC)
	to, okT := parseDay(r.To, time.UTC)
	if okF && okT && from.Before(to) {
		return Range{From: r.From, To: r.To}
	}
	return Range{Days: defaultDays}
}

// SpanDays: días que abarca el periodo (mínimo 1).
func SpanDays(rng Range) int {
	r := NormalizeRange(rng)
	if r.Days > 0 {
		return r.Days
	}
	from, _ := parseDay(r.From, time.UTC)
	to, _ := parseDay(r.To, time.UTC)
	d := int((to.Sub(from) + 12*time.Hour) / (24 * time.Hour))
	if d < 1 {
		d = 1
	}
	return d
}

type bounds struct {
	from, to         time.Time
	prevFrom, prevTo time.Time
	days             int
}

// previousPeriod: el periodo anterior de la misma duración. Si el periodo son meses completos
// (de día 1 a día 1), el anterior son los mismos meses hacia atrás; si no, los mismos días.
func previousPeriod(start, end time.Time, loc *time.Location) (from, to time.Time) {
	fy, fm, fd := start.Date()
	ty, tm, td := end.Date()
	if fd == 1 && td == 1 {
		months := (ty-fy)*12 + int(tm) - int(fm)
		return time.Date(fy, fm-time.Month(months), 1, 0, 0, 0, 0, loc), start
	}
	a := time.Date(fy, fm, fd, 0, 0, 0, 0, time.UTC)
	b := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	days := int((b.Sub(a) + 12*time.Hour) / (24 * time.Hour))
	return time.Date(fy, fm, fd-days, 0, 0, 0, 0, loc), start
}

func computeBounds(rng Range, now time.Time, loc *time.Location) bounds {
	loc = orLocal(loc)
	r := NormalizeRange(rng)
	if r.Days > 0 {
		span := time.Duration(r.Days) * 24 * time.Hour
		return bounds{
			from: now.Add(-span), to: now,
			prevFrom: now.Add(-2 * span), prevTo: now.Add(-span),
			days: r.Days,
		}
	}
	start, _ := parseDay(r.From, loc)
	end, _ := parseDay(r.To, loc)
	to := end
	if now.Before(end) {
		to = now
	}
	if to.Before(start) { // periodo que aún no empieza
		to = start
	}
	pf, pt := previousPeriod(start, end, loc)
	prevTo := pt
	if end.After(now) {
		// Periodo en curso: el anterior se compara por el mismo tramo transcurrido.
		if cand := pf.Add(to.Sub(start)); cand.Before(prevTo) {
			prevTo = cand
		}
	}
	return bounds{from: start, to: to, prevFrom: pf, prevTo: prevTo, days: SpanDays(r)}
}

// Bounds: límites [from, to) del periodo actual. Si el periodo llega al futuro (este mes), `to`
// se corta en «ahora».
func Bounds(rng Range, now time.Time, loc *time.Location) (from, to time.Time) {
	b := computeBounds(rng, now, loc)
	return b.from, b.to
}

// PreviousBounds: límites del periodo anterior de la misma duración (año con año; si el actual
// está en curso, el mismo tramo transcurrido).
func PreviousBounds(rng Range, now time.Time, loc *time.Location) (from, to time.Time) {
	b := computeBounds(rng, now, loc)
	return b.prevFrom, b.prevTo
}

// DayKey: el día local (AAAA-MM-DD) de una marca de tiempo en milisegundos.
func DayKey(ms int64, loc *time.Location) string {
	return time.UnixMilli(ms).In(orLocal(loc)).Format(dayLayout)
}

// BucketKey: clave del grupo de un día local según la granularidad (semana = su lunes;
// mes = AAAA-MM). Un día mal formado se devuelve tal cual.
func BucketKey(day string, g Granularity) string {
	switch g {
	case GranMonth:
		if len(day) >= 7 {
			return day[:7]
		}
		return day
	case GranWeek:
		d, ok := parseDay(day, time.UTC)
		if !ok {
			return day
		}
		back := (int(d.Weekday()) + 6) % 7 // lunes = 0
		return d.AddDate(0, 0, -back).Format(dayLayout)
	}
	return day
}

// Bucketize suma una serie diaria en semanas o meses. T debe ser una estructura con un campo
// string `Day`; se suman todos los campos numéricos exportados y `Day` pasa a ser la clave del
// grupo. Respeta el orden de aparición. Con granularidad diaria (o un T que no sirva) devuelve
// las filas tal cual.
func Bucketize[T any](rows []T, g Granularity) []T {
	if g == GranDay || g == "" {
		return rows
	}
	t := reflect.TypeOf((*T)(nil)).Elem()
	if t.Kind() != reflect.Struct {
		return rows
	}
	if f, ok := t.FieldByName("Day"); !ok || f.Type.Kind() != reflect.String || !f.IsExported() {
		return rows
	}
	index := map[string]int{}
	out := make([]T, 0, len(rows))
	for _, r := range rows {
		rv := reflect.ValueOf(r)
		key := BucketKey(rv.FieldByName("Day").String(), g)
		if i, ok := index[key]; ok {
			addNumeric(reflect.ValueOf(&out[i]).Elem(), rv)
			continue
		}
		cp := r
		reflect.ValueOf(&cp).Elem().FieldByName("Day").SetString(key)
		index[key] = len(out)
		out = append(out, cp)
	}
	return out
}

func addNumeric(dst, src reflect.Value) {
	for i := 0; i < dst.NumField(); i++ {
		f := dst.Type().Field(i)
		if f.Name == "Day" || !f.IsExported() {
			continue
		}
		d, s := dst.Field(i), src.Field(i)
		switch d.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			d.SetInt(d.Int() + s.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			d.SetUint(d.Uint() + s.Uint())
		case reflect.Float32, reflect.Float64:
			d.SetFloat(d.Float() + s.Float())
		}
	}
}
