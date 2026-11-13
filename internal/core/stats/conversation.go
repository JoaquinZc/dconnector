package stats

import (
	"sort"
	"time"

	"dconnector/internal/core/model"
)

// Estadísticas de conversaciones: lógica pura portada de
// ../dconnector/src/shared/conversationStats.ts (sin las listas de clientes: bestCustomers,
// latePayers, unhappy) y de ConversationStatsService (resumen, por contacto, grupos).
//
// Convenciones:
//   - Los instantes van en milisegundos (como todo el dominio).
//   - Las duraciones de respuesta van en SEGUNDOS y sus campos llevan el sufijo `Sec`.
//   - Todas las listas salen como `[]`, nunca `null`; los valores que pueden no existir (medianas,
//     variaciones) son punteros y salen como `null`.
//   - El día de la semana del mapa de calor empieza en el lunes (0) y acaba en el domingo (6).

// ConversationGap: sin mensajes durante este tiempo, el siguiente abre una conversación nueva.
const ConversationGap = 8 * time.Hour

const (
	gapMs  = int64(ConversationGap / time.Millisecond)
	hourMs = int64(time.Hour / time.Millisecond)

	// Tamaño de los rankings del servicio.
	topChatsLimit = 10
	groupsLimit   = 50
	peopleLimit   = 30
)

// MessageRow: un mensaje, lo mínimo para las estadísticas (los mismos campos que port.StatRow más
// la cuenta, porque aquí se juntan las filas de varias cuentas). `core/stats` no importa `port`:
// el servicio convierte.
type MessageRow struct {
	AccountID    model.AccountID
	ChatNativeID string
	ChatKind     model.ChatKind
	SenderID     string
	FromMe       bool
	Timestamp    int64 // ms
	Kind         model.ContentKind
	TextLen      int
}

// Group: el mensaje es de un grupo (no cuenta para respuestas ni inicios de conversación).
func (r MessageRow) Group() bool { return r.ChatKind == model.ChatGroup }

// Countable: qué mensajes entran en las estadísticas. Los chats de canal y contigo mismo y los
// avisos del sistema (altas y bajas de grupo…) no cuentan.
func (r MessageRow) Countable() bool {
	if r.Kind == model.KindSystem {
		return false
	}
	return r.ChatKind == model.ChatDirect || r.ChatKind == model.ChatGroup
}

// ChatKey: clave del chat del mensaje.
func (r MessageRow) ChatKey() model.ChatKey { return model.NewChatKey(r.AccountID, r.ChatNativeID) }

type chatID struct {
	acc    model.AccountID
	native string
}

func (r MessageRow) id() chatID { return chatID{r.AccountID, r.ChatNativeID} }

// ---------------------------------------------------------------------------------------------
// Tipos que ve la interfaz (11-INTERFAZ §6.2 y §6.7). Los nombres de campo JSON son un contrato.

// Period: periodo analizado y el anterior de la misma duración.
type Period struct {
	FromMs      int64       `json:"fromMs"`
	ToMs        int64       `json:"toMs"`
	PrevFromMs  int64       `json:"prevFromMs"`
	PrevToMs    int64       `json:"prevToMs"`
	Days        int         `json:"days"`
	Granularity Granularity `json:"granularity"` // "day" | "week" | "month"
}

// ContentMix: mezcla de contenido (audios y notas de voz van juntos en `voice`).
type ContentMix struct {
	Text     int `json:"text"`
	Image    int `json:"image"`
	Video    int `json:"video"`
	Voice    int `json:"voice"`
	Document int `json:"document"`
	Sticker  int `json:"sticker"`
	Other    int `json:"other"`
}

// ResponseStats: tiempos de respuesta (segundos). Solo chats individuales. Se cuentan las 24 h:
// desde el primer mensaje del otro tras tu último mensaje hasta tu siguiente mensaje (y al
// revés para los suyos).
type ResponseStats struct {
	MineMedianSec   *int64   `json:"mineMedianSec"` // null sin respuestas
	MineCount       int      `json:"mineCount"`
	MineUnderHour   *float64 `json:"mineUnderHour"` // 0..1: parte de tus respuestas en menos de 1 h
	TheirsMedianSec *int64   `json:"theirsMedianSec"`
	TheirsCount     int      `json:"theirsCount"`
}

// ConversationSummary: resumen de un conjunto de mensajes.
type ConversationSummary struct {
	Sent          int           `json:"sent"`
	Received      int           `json:"received"`
	Chats         int           `json:"chats"`         // chats con algún mensaje
	StartedByMe   int           `json:"startedByMe"`   // conversaciones iniciadas por ti (≥ 8 h sin hablar)
	StartedByThem int           `json:"startedByThem"` // iniciadas por la otra persona
	Response      ResponseStats `json:"response"`
	Mix           ContentMix    `json:"mix"`
}

// DayCount: enviados y recibidos por día (semana = su lunes y mes = AAAA-MM según la
// granularidad del periodo). Todos los campos numéricos son sumables (Bucketize).
type DayCount struct {
	Day      string `json:"day"` // AAAA-MM-DD
	Sent     int    `json:"sent"`
	Received int    `json:"received"`
}

// ChatStat: totales de un chat para rankings.
type ChatStat struct {
	Chat       model.ChatKey `json:"chat"`
	Total      int           `json:"total"`
	Sent       int           `json:"sent"`
	Received   int           `json:"received"`
	ActiveDays int           `json:"activeDays"`
	Last       int64         `json:"last"` // ms del último mensaje
}

// ChatRank: una fila de «chats más activos». El servicio completa título, avatar, canal y tipo.
type ChatRank struct {
	ChatKey    model.ChatKey   `json:"chatKey"`
	AccountID  model.AccountID `json:"accountId"`
	Channel    model.Channel   `json:"channel"`
	NativeID   string          `json:"nativeId"`
	Kind       model.ChatKind  `json:"kind"`
	Title      string          `json:"title"`
	AvatarURL  string          `json:"avatarUrl"`
	Total      int             `json:"total"`
	Sent       int             `json:"sent"`
	Received   int             `json:"received"`
	ActiveDays int             `json:"activeDays"`
	LastAt     int64           `json:"lastAt"` // ms
}

// ConversationChanges: variación respecto al periodo anterior como fracción (0,25 = +25 %); null
// si no hay base para comparar. En los tiempos de respuesta un valor NEGATIVO es mejor (más rápido).
type ConversationChanges struct {
	Sent            *float64 `json:"sent"`
	Received        *float64 `json:"received"`
	Chats           *float64 `json:"chats"`
	StartedByMe     *float64 `json:"startedByMe"`
	StartedByThem   *float64 `json:"startedByThem"`
	MineMedianSec   *float64 `json:"mineMedianSec"`
	TheirsMedianSec *float64 `json:"theirsMedianSec"`
}

// ConversationOverview: pestaña «Conversaciones» (11 §6.7). `current`, `previous`, `heatmap`,
// `topChats` y `frequent` son de chats individuales; `perDay` incluye también los grupos.
type ConversationOverview struct {
	Period        Period              `json:"period"`
	Current       ConversationSummary `json:"current"`
	Previous      ConversationSummary `json:"previous"`
	Changes       ConversationChanges `json:"changes"`
	GroupSent     int                 `json:"groupSent"` // mensajes de grupos del periodo
	GroupReceived int                 `json:"groupReceived"`
	PerDay        []DayCount          `json:"perDay"`   // todos los días del periodo (agrupados según Period.Granularity)
	Heatmap       [][]int             `json:"heatmap"`  // 7 filas (lunes…domingo) × 24 horas locales
	TopChats      []ChatRank          `json:"topChats"` // hasta 10, por mensajes
	Frequent      []ChatRank          `json:"frequent"` // hasta 10, por días con actividad
}

// ContactConversationStats: estadísticas de un chat (panel de información, 11 §6.2).
// `summary`, `firstAt`, `lastAt` y `activeDays` son de TODO el historial guardado del chat;
// `perDay` es del periodo pedido; `heatmap` es de lo que te escribe (cuándo responde).
type ContactConversationStats struct {
	Period     Period              `json:"period"`
	Summary    ConversationSummary `json:"summary"`
	FirstAt    int64               `json:"firstAt"` // ms; 0 si no hay mensajes
	LastAt     int64               `json:"lastAt"`  // ms; 0 si no hay mensajes
	ActiveDays int                 `json:"activeDays"`
	PerDay     []DayCount          `json:"perDay"`
	Heatmap    [][]int             `json:"heatmap"` // 7 × 24, solo mensajes recibidos
}

// GroupRow: un grupo en el ranking. El servicio completa título, avatar y canal.
type GroupRow struct {
	ChatKey    model.ChatKey   `json:"chatKey"`
	AccountID  model.AccountID `json:"accountId"`
	Channel    model.Channel   `json:"channel"`
	NativeID   string          `json:"nativeId"`
	Title      string          `json:"title"`
	AvatarURL  string          `json:"avatarUrl"`
	Total      int             `json:"total"`      // mensajes en el periodo
	Mine       int             `json:"mine"`       // los tuyos
	Authors    int             `json:"authors"`    // personas distintas que escribieron
	ActiveDays int             `json:"activeDays"` // días con mensajes
	LastAt     int64           `json:"lastAt"`     // ms
}

// PersonRow: una persona activa en los grupos (sin contarte a ti). El servicio completa el nombre.
type PersonRow struct {
	AccountID model.AccountID `json:"accountId"`
	Channel   model.Channel   `json:"channel"`
	SenderID  string          `json:"senderId"` // id nativo de la persona en esa cuenta
	Name      string          `json:"name"`
	AvatarURL string          `json:"avatarUrl"`
	Total     int             `json:"total"`  // mensajes en grupos en el periodo
	Groups    int             `json:"groups"` // en cuántos grupos escribió
	LastAt    int64           `json:"lastAt"` // ms
}

// GroupsOverview: pestaña «Grupos» (11 §6.7).
type GroupsOverview struct {
	Period        Period      `json:"period"`
	GroupSent     int         `json:"groupSent"`
	GroupReceived int         `json:"groupReceived"`
	Groups        []GroupRow  `json:"groups"` // hasta 50, por mensajes
	People        []PersonRow `json:"people"` // hasta 30, por mensajes
}

// ---------------------------------------------------------------------------------------------
// Utilidades

// Median: mediana de enteros; en una cantidad par es el promedio de los dos centrales redondeado
// hacia arriba (como Math.round). ok = false si no hay valores.
func Median(values []int64) (m int64, ok bool) {
	if len(values) == 0 {
		return 0, false
	}
	s := append([]int64(nil), values...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid], true
	}
	a, b := s[mid-1], s[mid]
	sum := a + b
	if sum >= 0 {
		return (sum + 1) / 2, true
	}
	return sum / 2, true // Math.round redondea los .5 negativos hacia arriba (hacia cero)
}

func medianPtr(values []int64) *int64 {
	if m, ok := Median(values); ok {
		return &m
	}
	return nil
}

func addToMix(mix *ContentMix, k model.ContentKind) {
	switch k {
	case model.KindText:
		mix.Text++
	case model.KindImage:
		mix.Image++
	case model.KindVideo:
		mix.Video++
	case model.KindVoice, model.KindAudio:
		mix.Voice++
	case model.KindDocument:
		mix.Document++
	case model.KindSticker:
		mix.Sticker++
	default:
		mix.Other++
	}
}

// sortedByChat devuelve las filas ordenadas por chat y hora (la misma si ya lo están).
func sortedByChat(rows []MessageRow) []MessageRow {
	less := func(a, b MessageRow) bool {
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		if a.ChatNativeID != b.ChatNativeID {
			return a.ChatNativeID < b.ChatNativeID
		}
		return a.Timestamp < b.Timestamp
	}
	ordered := true
	for i := 1; i < len(rows); i++ {
		if less(rows[i], rows[i-1]) {
			ordered = false
			break
		}
	}
	if ordered {
		return rows
	}
	cp := append([]MessageRow(nil), rows...)
	sort.SliceStable(cp, func(i, j int) bool { return less(cp[i], cp[j]) })
	return cp
}

// Summarize resume un conjunto de mensajes. No hace falta que vengan ordenados (se ordenan por
// chat y hora). Respuestas e inicios de conversación solo cuentan en chats individuales; los
// grupos suman a enviados, recibidos, chats y mezcla.
func Summarize(rows []MessageRow) ConversationSummary {
	var out ConversationSummary
	rows = sortedByChat(rows)

	var mine, theirs []int64 // segundos
	var (
		cur       chatID
		have      bool
		prevT     int64
		hasPrev   bool
		waitingMe = int64(-1) // primer mensaje suyo aún sin responder
		waitingTh = int64(-1) // primer mensaje tuyo aún sin respuesta
	)
	for _, m := range rows {
		if m.FromMe {
			out.Sent++
		} else {
			out.Received++
		}
		addToMix(&out.Mix, m.Kind)
		if id := m.id(); !have || id != cur {
			cur, have = id, true
			out.Chats++
			prevT, hasPrev = 0, false
			waitingMe, waitingTh = -1, -1
		}
		if !m.Group() {
			if !hasPrev || m.Timestamp-prevT >= gapMs {
				if m.FromMe {
					out.StartedByMe++
				} else {
					out.StartedByThem++
				}
			}
			if m.FromMe {
				if waitingMe >= 0 {
					mine = append(mine, (m.Timestamp-waitingMe)/1000)
					waitingMe = -1
				}
				if waitingTh < 0 {
					waitingTh = m.Timestamp
				}
			} else {
				if waitingTh >= 0 {
					theirs = append(theirs, (m.Timestamp-waitingTh)/1000)
					waitingTh = -1
				}
				if waitingMe < 0 {
					waitingMe = m.Timestamp
				}
			}
		}
		prevT, hasPrev = m.Timestamp, true
	}
	out.Response = ResponseStats{
		MineMedianSec: medianPtr(mine), MineCount: len(mine),
		TheirsMedianSec: medianPtr(theirs), TheirsCount: len(theirs),
	}
	if len(mine) > 0 {
		under := 0
		for _, d := range mine {
			if d*1000 < hourMs {
				under++
			}
		}
		f := float64(under) / float64(len(mine))
		out.Response.MineUnderHour = &f
	}
	return out
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// PerDay: enviados y recibidos por día local, con TODOS los días entre from y to (aunque no haya
// mensajes). `to` es exclusivo: si cae justo a medianoche, ese día no entra. Los mensajes fuera
// de esos días se ignoran.
func PerDay(rows []MessageRow, from, to time.Time, loc *time.Location) []DayCount {
	loc = orLocal(loc)
	out := []DayCount{}
	if !to.After(from) {
		return out
	}
	last := DayKey(to.Add(-time.Millisecond).UnixMilli(), loc)
	index := map[string]int{}
	for d := startOfDay(from, loc); ; d = d.AddDate(0, 0, 1) {
		k := d.Format(dayLayout)
		if k > last || len(out) >= 4000 {
			break
		}
		index[k] = len(out)
		out = append(out, DayCount{Day: k})
	}
	for _, m := range rows {
		i, ok := index[DayKey(m.Timestamp, loc)]
		if !ok {
			continue
		}
		if m.FromMe {
			out[i].Sent++
		} else {
			out[i].Received++
		}
	}
	return out
}

// HeatSide: qué mensajes cuenta el mapa de calor.
type HeatSide string

const (
	HeatAll      HeatSide = "all"
	HeatSent     HeatSide = "sent"
	HeatReceived HeatSide = "received"
)

// Heatmap: [día de la semana 0 = lunes … 6 = domingo][hora local 0–23] → mensajes.
func Heatmap(rows []MessageRow, which HeatSide, loc *time.Location) [][]int {
	loc = orLocal(loc)
	grid := make([][]int, 7)
	for i := range grid {
		grid[i] = make([]int, 24)
	}
	for _, m := range rows {
		if (which == HeatSent && !m.FromMe) || (which == HeatReceived && m.FromMe) {
			continue
		}
		t := time.UnixMilli(m.Timestamp).In(loc)
		grid[(int(t.Weekday())+6)%7][t.Hour()]++
	}
	return grid
}

// PerChat: totales por chat (mensajes, enviados, recibidos, días activos, último), en el orden en
// que aparece cada chat por primera vez.
func PerChat(rows []MessageRow, loc *time.Location) []ChatStat {
	loc = orLocal(loc)
	type agg struct {
		stat ChatStat
		days map[string]struct{}
	}
	index := map[chatID]int{}
	aggs := []*agg{}
	for _, m := range rows {
		id := m.id()
		i, ok := index[id]
		if !ok {
			i = len(aggs)
			index[id] = i
			aggs = append(aggs, &agg{stat: ChatStat{Chat: m.ChatKey()}, days: map[string]struct{}{}})
		}
		a := aggs[i]
		a.stat.Total++
		if m.FromMe {
			a.stat.Sent++
		} else {
			a.stat.Received++
		}
		a.days[DayKey(m.Timestamp, loc)] = struct{}{}
		if m.Timestamp > a.stat.Last {
			a.stat.Last = m.Timestamp
		}
	}
	out := make([]ChatStat, len(aggs))
	for i, a := range aggs {
		a.stat.ActiveDays = len(a.days)
		out[i] = a.stat
	}
	return out
}

// ---------------------------------------------------------------------------------------------
// Armado de lo que devuelve el servicio

func periodOf(rng Range, now time.Time, loc *time.Location) (Period, time.Time, time.Time, time.Time, time.Time) {
	from, to := Bounds(rng, now, loc)
	pFrom, pTo := PreviousBounds(rng, now, loc)
	days := SpanDays(rng)
	return Period{
		FromMs: from.UnixMilli(), ToMs: to.UnixMilli(),
		PrevFromMs: pFrom.UnixMilli(), PrevToMs: pTo.UnixMilli(),
		Days: days, Granularity: GranularityFor(days),
	}, from, to, pFrom, pTo
}

// perDayFrom: primer instante para PerDay. En una ventana móvil de N días se muestran N días
// (el día parcial de hace N días no se cuenta), como hace D'Ventas.
func perDayFrom(rng Range, from time.Time) time.Time {
	if NormalizeRange(rng).Days > 0 {
		return from.Add(24 * time.Hour)
	}
	return from
}

func between(rows []MessageRow, from, to time.Time) []MessageRow {
	f, t := from.UnixMilli(), to.UnixMilli()
	out := make([]MessageRow, 0, len(rows))
	for _, m := range rows {
		if m.Timestamp >= f && m.Timestamp < t {
			out = append(out, m)
		}
	}
	return out
}

func individual(rows []MessageRow) []MessageRow {
	out := make([]MessageRow, 0, len(rows))
	for _, m := range rows {
		if !m.Group() {
			out = append(out, m)
		}
	}
	return out
}

func pctChangeInt(cur, prev int) *float64 { return PctChange(float64(cur), float64(prev)) }

func pctChangeMedian(cur, prev *int64) *float64 {
	if cur == nil || prev == nil {
		return nil
	}
	return PctChange(float64(*cur), float64(*prev))
}

func rankOf(s ChatStat) ChatRank {
	acc, native := s.Chat.Split()
	return ChatRank{
		ChatKey: s.Chat, AccountID: acc, NativeID: native,
		Total: s.Total, Sent: s.Sent, Received: s.Received, ActiveDays: s.ActiveDays, LastAt: s.Last,
	}
}

func sortRanks(in []ChatStat, byDays bool) []ChatRank {
	cp := append([]ChatStat(nil), in...)
	sort.SliceStable(cp, func(i, j int) bool {
		a, b := cp[i], cp[j]
		if byDays && a.ActiveDays != b.ActiveDays {
			return a.ActiveDays > b.ActiveDays
		}
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Chat < b.Chat
	})
	if len(cp) > topChatsLimit {
		cp = cp[:topChatsLimit]
	}
	out := make([]ChatRank, len(cp))
	for i, s := range cp {
		out[i] = rankOf(s)
	}
	return out
}

// BuildConversationOverview arma la pestaña «Conversaciones». `rows` son los mensajes (ya
// filtrados con Countable) del periodo actual y del anterior; cada uno se reparte por su hora.
func BuildConversationOverview(rng Range, now time.Time, loc *time.Location, rows []MessageRow) ConversationOverview {
	loc = orLocal(loc)
	period, from, to, pFrom, pTo := periodOf(rng, now, loc)
	cur := between(rows, from, to)
	prev := between(rows, pFrom, pTo)
	curInd, prevInd := individual(cur), individual(prev)

	out := ConversationOverview{
		Period:   period,
		Current:  Summarize(curInd),
		Previous: Summarize(prevInd),
		PerDay:   Bucketize(PerDay(cur, perDayFrom(rng, from), to, loc), period.Granularity),
		Heatmap:  Heatmap(curInd, HeatAll, loc),
	}
	for _, m := range cur {
		if m.Group() {
			if m.FromMe {
				out.GroupSent++
			} else {
				out.GroupReceived++
			}
		}
	}
	c, p := out.Current, out.Previous
	out.Changes = ConversationChanges{
		Sent: pctChangeInt(c.Sent, p.Sent), Received: pctChangeInt(c.Received, p.Received),
		Chats:           pctChangeInt(c.Chats, p.Chats),
		StartedByMe:     pctChangeInt(c.StartedByMe, p.StartedByMe),
		StartedByThem:   pctChangeInt(c.StartedByThem, p.StartedByThem),
		MineMedianSec:   pctChangeMedian(c.Response.MineMedianSec, p.Response.MineMedianSec),
		TheirsMedianSec: pctChangeMedian(c.Response.TheirsMedianSec, p.Response.TheirsMedianSec),
	}
	ranked := PerChat(curInd, loc)
	out.TopChats = sortRanks(ranked, false)
	out.Frequent = sortRanks(ranked, true)
	return out
}

// BuildChatStats arma las estadísticas de un chat. `rows` son TODOS los mensajes guardados del
// chat; el periodo solo acota `perDay`.
func BuildChatStats(rng Range, now time.Time, loc *time.Location, rows []MessageRow) ContactConversationStats {
	loc = orLocal(loc)
	period, from, to, _, _ := periodOf(rng, now, loc)
	out := ContactConversationStats{
		Period:  period,
		Summary: Summarize(rows),
		Heatmap: Heatmap(rows, HeatReceived, loc),
	}
	days := map[string]struct{}{}
	for _, m := range rows {
		days[DayKey(m.Timestamp, loc)] = struct{}{}
		if out.FirstAt == 0 || m.Timestamp < out.FirstAt {
			out.FirstAt = m.Timestamp
		}
		if m.Timestamp > out.LastAt {
			out.LastAt = m.Timestamp
		}
	}
	out.ActiveDays = len(days)
	out.PerDay = Bucketize(PerDay(between(rows, from, to), perDayFrom(rng, from), to, loc), period.Granularity)
	return out
}

// BuildGroupsOverview arma la pestaña «Grupos» con los mensajes de grupo del periodo. Quien
// escribe eres tú (`FromMe`) o personas con SenderID; las tuyas no cuentan en `people`.
func BuildGroupsOverview(rng Range, now time.Time, loc *time.Location, rows []MessageRow) GroupsOverview {
	loc = orLocal(loc)
	period, from, to, _, _ := periodOf(rng, now, loc)
	cur := between(rows, from, to)

	type gAgg struct {
		row     GroupRow
		days    map[string]struct{}
		authors map[string]struct{}
	}
	type pKey struct {
		acc model.AccountID
		id  string
	}
	type pAgg struct {
		row    PersonRow
		groups map[chatID]struct{}
	}
	out := GroupsOverview{Period: period, Groups: []GroupRow{}, People: []PersonRow{}}
	gIndex := map[chatID]*gAgg{}
	gOrder := []*gAgg{}
	pIndex := map[pKey]*pAgg{}
	pOrder := []*pAgg{}
	for _, m := range cur {
		if !m.Group() {
			continue
		}
		if m.FromMe {
			out.GroupSent++
		} else {
			out.GroupReceived++
		}
		id := m.id()
		g, ok := gIndex[id]
		if !ok {
			g = &gAgg{
				row:  GroupRow{ChatKey: m.ChatKey(), AccountID: m.AccountID, NativeID: m.ChatNativeID},
				days: map[string]struct{}{}, authors: map[string]struct{}{},
			}
			gIndex[id] = g
			gOrder = append(gOrder, g)
		}
		g.row.Total++
		if m.FromMe {
			g.row.Mine++
		}
		g.days[DayKey(m.Timestamp, loc)] = struct{}{}
		if m.Timestamp > g.row.LastAt {
			g.row.LastAt = m.Timestamp
		}
		if m.FromMe || m.SenderID == "" {
			continue
		}
		g.authors[m.SenderID] = struct{}{}
		pk := pKey{m.AccountID, m.SenderID}
		p, ok := pIndex[pk]
		if !ok {
			p = &pAgg{
				row:    PersonRow{AccountID: m.AccountID, SenderID: m.SenderID},
				groups: map[chatID]struct{}{},
			}
			pIndex[pk] = p
			pOrder = append(pOrder, p)
		}
		p.row.Total++
		p.groups[id] = struct{}{}
		if m.Timestamp > p.row.LastAt {
			p.row.LastAt = m.Timestamp
		}
	}
	for _, g := range gOrder {
		g.row.Authors = len(g.authors)
		g.row.ActiveDays = len(g.days)
		out.Groups = append(out.Groups, g.row)
	}
	for _, p := range pOrder {
		p.row.Groups = len(p.groups)
		out.People = append(out.People, p.row)
	}
	sort.SliceStable(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.ChatKey < b.ChatKey
	})
	sort.SliceStable(out.People, func(i, j int) bool {
		a, b := out.People[i], out.People[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		return a.SenderID < b.SenderID
	})
	if len(out.Groups) > groupsLimit {
		out.Groups = out.Groups[:groupsLimit]
	}
	if len(out.People) > peopleLimit {
		out.People = out.People[:peopleLimit]
	}
	return out
}
