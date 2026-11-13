package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/stats"
)

// cvStore: AccountStore con chats, personas y mensajes en memoria. MessageStats devuelve las
// filas ordenadas por hora, igual que el almacén real, con los límites inclusivos.
type cvStore struct {
	port.AccountStore
	id      model.AccountID
	chats   map[string]model.Chat
	persons map[string]model.Person
	rows    []port.StatRow
	calls   []cvCall
	err     error
}

type cvCall struct {
	from, to int64
	chats    []string
}

func (s *cvStore) GetChat(ctx context.Context, nativeID string) (model.Chat, error) {
	c, ok := s.chats[nativeID]
	if !ok {
		return model.Chat{}, port.ErrNotFound
	}
	return c, nil
}

func (s *cvStore) GetPerson(ctx context.Context, nativeID string) (model.Person, error) {
	p, ok := s.persons[nativeID]
	if !ok {
		return model.Person{}, port.ErrNotFound
	}
	return p, nil
}

func (s *cvStore) MessageStats(ctx context.Context, from, to int64, chatNativeIDs []string) ([]port.StatRow, error) {
	s.calls = append(s.calls, cvCall{from, to, chatNativeIDs})
	if s.err != nil {
		return nil, s.err
	}
	var out []port.StatRow
	for _, r := range s.rows {
		if r.Timestamp < from || (to > 0 && r.Timestamp > to) {
			continue
		}
		if len(chatNativeIDs) > 0 {
			ok := false
			for _, c := range chatNativeIDs {
				ok = ok || c == r.ChatNativeID
			}
			if !ok {
				continue
			}
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out, nil
}

type cvEnv struct {
	svc    *Stats
	stores map[model.AccountID]*cvStore
	now    time.Time
	loc    *time.Location
}

func newCvEnv(t *testing.T) *cvEnv {
	t.Helper()
	loc := time.FixedZone("EC", -5*3600)
	e := &cvEnv{stores: map[model.AccountID]*cvStore{}, loc: loc, now: time.Date(2026, 10, 9, 15, 0, 0, 0, loc)}
	accounts := &Accounts{instances: map[model.AccountID]*instance{}}
	for _, a := range []model.Account{
		{ID: "wa-1", Channel: model.ChannelWhatsApp},
		{ID: "tg-1", Channel: model.ChannelTelegram},
	} {
		st := &cvStore{id: a.ID, chats: map[string]model.Chat{}, persons: map[string]model.Person{}}
		e.stores[a.ID] = st
		accounts.instances[a.ID] = &instance{acc: a, store: st}
	}
	e.svc = NewStats(accounts)
	e.svc.now = func() time.Time { return e.now }
	e.svc.loc = loc
	return e
}

// msg agrega una fila a la cuenta: `daysAgo` días antes de `now`, a la hora local `hour`.
func (e *cvEnv) msg(acc model.AccountID, chat string, kind model.ChatKind, from string, me bool, daysAgo, hour int, ck model.ContentKind) {
	d := e.now.AddDate(0, 0, -daysAgo)
	ts := time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, e.loc).UnixMilli()
	if ck == "" {
		ck = model.KindText
	}
	e.stores[acc].rows = append(e.stores[acc].rows, port.StatRow{
		ChatNativeID: chat, ChatKind: kind, SenderID: from, FromMe: me, Timestamp: ts, Kind: ck, TextLen: 5,
	})
}

func TestStatsConversationsJoinsAccounts(t *testing.T) {
	e := newCvEnv(t)
	ctx := context.Background()
	wa, tg := e.stores["wa-1"], e.stores["tg-1"]
	wa.chats["ana"] = model.Chat{NativeID: "ana", Kind: model.ChatDirect, Title: "Ana", AvatarURL: "/avatar/wa-1/ana.jpg"}
	tg.chats["ana"] = model.Chat{NativeID: "ana", Kind: model.ChatDirect, Title: "Ana en Telegram"} // mismo id, otra cuenta
	// WhatsApp: Ana escribe y respondo (periodo actual); Ana también escribió en el periodo anterior.
	e.msg("wa-1", "ana", model.ChatDirect, "ana", false, 1, 9, "")
	e.msg("wa-1", "ana", model.ChatDirect, "me", true, 1, 11, "")
	e.msg("wa-1", "ana", model.ChatDirect, "ana", false, 10, 9, "")
	// Telegram: otro chat con el mismo id nativo; no debe mezclarse con el de WhatsApp.
	e.msg("tg-1", "ana", model.ChatDirect, "ana", false, 2, 8, "")
	e.msg("tg-1", "ana", model.ChatDirect, "me", true, 2, 9, model.KindImage)
	// No cuentan: aviso del sistema, canal, contigo mismo.
	e.msg("wa-1", "ana", model.ChatDirect, "", false, 1, 12, model.KindSystem)
	e.msg("tg-1", "canal", model.ChatChannel, "x", false, 1, 12, "")
	e.msg("tg-1", "yo", model.ChatSelf, "me", true, 1, 12, "")
	// Grupo.
	e.msg("wa-1", "g1", model.ChatGroup, "p1", false, 1, 8, "")

	o, err := e.svc.Conversations(ctx, nil, stats.Range{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if o.Current.Sent != 2 || o.Current.Received != 2 || o.Current.Chats != 2 {
		t.Fatalf("actual: %+v", o.Current)
	}
	if o.Previous.Received != 1 || o.Previous.Chats != 1 {
		t.Fatalf("anterior: %+v", o.Previous)
	}
	if o.GroupReceived != 1 || o.GroupSent != 0 {
		t.Fatalf("grupos: %d %d", o.GroupSent, o.GroupReceived)
	}
	if o.Current.Mix.Image != 1 || o.Current.Mix.Text != 3 {
		t.Fatalf("mezcla: %+v", o.Current.Mix)
	}
	if len(o.TopChats) != 2 {
		t.Fatalf("top: %+v", o.TopChats)
	}
	byAcc := map[model.AccountID]stats.ChatRank{}
	for _, r := range o.TopChats {
		byAcc[r.AccountID] = r
	}
	if r := byAcc["wa-1"]; r.Title != "Ana" || r.Channel != model.ChannelWhatsApp || r.AvatarURL != "/avatar/wa-1/ana.jpg" ||
		r.Kind != model.ChatDirect || r.ChatKey != "wa-1|ana" || r.Total != 2 || r.Sent != 1 {
		t.Fatalf("fila WhatsApp: %+v", r)
	}
	if r := byAcc["tg-1"]; r.Title != "Ana en Telegram" || r.Channel != model.ChannelTelegram {
		t.Fatalf("fila Telegram: %+v", r)
	}
	// Se pidió el periodo actual y el anterior de una vez: desde el inicio del anterior.
	pFrom, _ := stats.PreviousBounds(stats.Range{Days: 7}, e.now, e.loc)
	if len(wa.calls) != 1 || wa.calls[0].from != pFrom.UnixMilli() || wa.calls[0].to != e.now.UnixMilli() {
		t.Fatalf("consulta al almacén: %+v", wa.calls)
	}

	// Solo una cuenta.
	o, err = e.svc.Conversations(ctx, []model.AccountID{"tg-1", "tg-1"}, stats.Range{Days: 7})
	if err != nil || o.Current.Chats != 1 || o.Current.Sent != 1 || len(tg.calls) != 2 || len(wa.calls) != 1 {
		t.Fatalf("una cuenta: %+v %v", o.Current, err)
	}
	if _, err := e.svc.Conversations(ctx, []model.AccountID{"xx-1"}, stats.Range{Days: 7}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
	tg.err = errors.New("base rota")
	if _, err := e.svc.Conversations(ctx, nil, stats.Range{Days: 7}); err == nil {
		t.Fatal("el error del almacén debe subir")
	}
	tg.err = nil
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.svc.Conversations(cctx, nil, stats.Range{Days: 7}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelado: %v", err)
	}
}

func TestStatsConversationsUnknownChatFallsBackToNativeID(t *testing.T) {
	e := newCvEnv(t)
	e.msg("wa-1", "desconocido", model.ChatDirect, "x", false, 1, 9, "")
	o, err := e.svc.Conversations(context.Background(), nil, stats.Range{Days: 7})
	if err != nil || len(o.TopChats) != 1 || o.TopChats[0].Title != "desconocido" {
		t.Fatalf("%+v %v", o.TopChats, err)
	}
}

func TestStatsConversationsEmptyIsValidJSON(t *testing.T) {
	e := newCvEnv(t)
	o, err := e.svc.Conversations(context.Background(), nil, stats.Range{Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(o)
	s := string(b)
	if strings.Contains(s, `"topChats":null`) || strings.Contains(s, `"perDay":null`) || !strings.Contains(s, `"topChats":[]`) {
		t.Fatalf("json: %s", s)
	}
	if o.Period.Days != 30 || len(o.PerDay) != 30 {
		t.Fatalf("periodo: %+v, días %d", o.Period, len(o.PerDay))
	}
}

func TestStatsChat(t *testing.T) {
	e := newCvEnv(t)
	ctx := context.Background()
	wa := e.stores["wa-1"]
	wa.chats["ana"] = model.Chat{NativeID: "ana", Kind: model.ChatDirect, Title: "Ana"}
	wa.chats["beto"] = model.Chat{NativeID: "beto", Kind: model.ChatDirect, Title: "Beto"}
	e.msg("wa-1", "ana", model.ChatDirect, "ana", false, 100, 9, "") // fuera del periodo, dentro del historial
	e.msg("wa-1", "ana", model.ChatDirect, "ana", false, 1, 9, "")
	e.msg("wa-1", "ana", model.ChatDirect, "me", true, 1, 10, "")
	e.msg("wa-1", "beto", model.ChatDirect, "beto", false, 1, 9, "")
	e.msg("wa-1", "ana", model.ChatDirect, "", false, 1, 12, model.KindSystem)

	got, err := e.svc.Chat(ctx, model.NewChatKey("wa-1", "ana"), stats.Range{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Sent != 1 || got.Summary.Received != 2 || got.Summary.Chats != 1 || got.ActiveDays != 2 {
		t.Fatalf("resumen: %+v", got)
	}
	if got.FirstAt == 0 || got.LastAt <= got.FirstAt || len(got.PerDay) != 7 {
		t.Fatalf("primero/último/por día: %+v", got)
	}
	// Solo se pidió ese chat, con historial completo.
	last := wa.calls[len(wa.calls)-1]
	if last.from != 0 || last.to != 0 || len(last.chats) != 1 || last.chats[0] != "ana" {
		t.Fatalf("consulta: %+v", last)
	}

	if _, err := e.svc.Chat(ctx, model.NewChatKey("wa-1", "nadie"), stats.Range{Days: 7}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("chat desconocido: %v", err)
	}
	if _, err := e.svc.Chat(ctx, model.NewChatKey("xx-1", "ana"), stats.Range{Days: 7}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
	if _, err := e.svc.Chat(ctx, "sin-barra", stats.Range{Days: 7}); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("clave inválida: %v", err)
	}
	// Chat sin mensajes: todo en cero y listas vacías.
	wa.chats["vacio"] = model.Chat{NativeID: "vacio", Kind: model.ChatDirect}
	empty, err := e.svc.Chat(ctx, model.NewChatKey("wa-1", "vacio"), stats.Range{Days: 7})
	if err != nil || empty.FirstAt != 0 || empty.PerDay == nil || len(empty.PerDay) != 7 {
		t.Fatalf("vacío: %+v %v", empty, err)
	}
}

func TestStatsGroups(t *testing.T) {
	e := newCvEnv(t)
	ctx := context.Background()
	wa, tg := e.stores["wa-1"], e.stores["tg-1"]
	wa.chats["g1"] = model.Chat{NativeID: "g1", Kind: model.ChatGroup, Title: "Vecinos", AvatarURL: "/avatar/wa-1/g1.jpg"}
	tg.chats["g2"] = model.Chat{NativeID: "g2", Kind: model.ChatGroup, Title: "Equipo"}
	wa.persons["ana"] = model.Person{NativeID: "ana", Name: "Ana", AvatarURL: "/avatar/wa-1/ana.jpg"}
	tg.persons["u:9"] = model.Person{NativeID: "u:9", Username: "beto"}
	e.msg("wa-1", "g1", model.ChatGroup, "ana", false, 1, 9, "")
	e.msg("wa-1", "g1", model.ChatGroup, "ana", false, 1, 10, "")
	e.msg("wa-1", "g1", model.ChatGroup, "me", true, 2, 10, "")
	e.msg("wa-1", "g1", model.ChatGroup, "desconocida", false, 2, 11, "")
	e.msg("tg-1", "g2", model.ChatGroup, "u:9", false, 3, 9, "")
	e.msg("tg-1", "g2", model.ChatGroup, "u:9", false, 3, 9, model.KindImage)
	e.msg("tg-1", "g2", model.ChatGroup, "", false, 3, 10, model.KindSystem) // alta de un miembro: no cuenta
	e.msg("wa-1", "ana", model.ChatDirect, "ana", false, 1, 9, "")           // individual: no es de grupo

	o, err := e.svc.Groups(ctx, nil, stats.Range{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if o.GroupSent != 1 || o.GroupReceived != 5 {
		t.Fatalf("totales: %d %d", o.GroupSent, o.GroupReceived)
	}
	if len(o.Groups) != 2 || o.Groups[0].Title != "Vecinos" || o.Groups[0].Channel != model.ChannelWhatsApp ||
		o.Groups[0].AvatarURL != "/avatar/wa-1/g1.jpg" || o.Groups[0].Total != 4 || o.Groups[0].Mine != 1 || o.Groups[0].Authors != 2 {
		t.Fatalf("grupos: %+v", o.Groups)
	}
	if o.Groups[1].Title != "Equipo" || o.Groups[1].Channel != model.ChannelTelegram {
		t.Fatalf("grupo 2: %+v", o.Groups[1])
	}
	names := map[string]stats.PersonRow{}
	for _, p := range o.People {
		names[p.SenderID] = p
	}
	if len(o.People) != 3 || names["ana"].Name != "Ana" || names["ana"].Total != 2 || names["ana"].AvatarURL == "" ||
		names["u:9"].Name != "@beto" || names["u:9"].Channel != model.ChannelTelegram ||
		names["desconocida"].Name != "desconocida" || names["desconocida"].Channel != model.ChannelWhatsApp {
		t.Fatalf("personas: %+v", o.People)
	}
	if _, err := e.svc.Groups(ctx, []model.AccountID{"xx-1"}, stats.Range{Days: 7}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cuenta desconocida: %v", err)
	}
	only, _ := e.svc.Groups(ctx, []model.AccountID{"tg-1"}, stats.Range{Days: 7})
	if len(only.Groups) != 1 || only.Groups[0].Title != "Equipo" {
		t.Fatalf("una cuenta: %+v", only.Groups)
	}
}

func TestStatsGroupsWithCustomRange(t *testing.T) {
	e := newCvEnv(t)
	e.msg("wa-1", "g1", model.ChatGroup, "ana", false, 1, 9, "")  // 8 de octubre
	e.msg("wa-1", "g1", model.ChatGroup, "ana", false, 20, 9, "") // 19 de septiembre
	o, err := e.svc.Groups(context.Background(), nil, stats.Range{From: "2026-10-01", To: "2026-10-09"})
	if err != nil || o.GroupReceived != 1 || o.Period.Days != 8 {
		t.Fatalf("%+v %v", o, err)
	}
}
