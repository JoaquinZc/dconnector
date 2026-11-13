package service

import (
	"context"
	"errors"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/stats"
)

// Stats: estadísticas de conversaciones (06 §10). Las filas salen de AccountStore.MessageStats de
// cada cuenta y se juntan antes de calcular (la lógica pura está en core/stats).
type Stats struct {
	acc *Accounts

	// Ajustables en pruebas.
	now func() time.Time
	loc *time.Location
}

func NewStats(acc *Accounts) *Stats { return &Stats{acc: acc, now: time.Now, loc: time.Local} }

// instances: las cuentas pedidas (sin repetir) o, si no se pide ninguna, todas las cargadas.
func (s *Stats) instances(ids []model.AccountID) ([]*instance, error) {
	if len(ids) == 0 {
		return s.acc.snapshot(), nil
	}
	seen := map[model.AccountID]bool{}
	out := make([]*instance, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		inst, err := s.acc.mustGet(id)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, nil
}

// statRowsToMessageRows convierte las filas del almacén al tipo de core/stats. Con `onlyCountable` se
// descartan los avisos del sistema y los chats de canal o contigo mismo.
func statRowsToMessageRows(acc model.AccountID, in []port.StatRow, onlyCountable bool) []stats.MessageRow {
	out := make([]stats.MessageRow, 0, len(in))
	for _, r := range in {
		m := stats.MessageRow{
			AccountID: acc, ChatNativeID: r.ChatNativeID, ChatKind: r.ChatKind, SenderID: r.SenderID,
			FromMe: r.FromMe, Timestamp: r.Timestamp, Kind: r.Kind, TextLen: r.TextLen,
		}
		if onlyCountable {
			if !m.Countable() {
				continue
			}
		} else if m.Kind == model.KindSystem {
			continue
		}
		out = append(out, m)
	}
	return out
}

// collect junta las filas de las cuentas pedidas entre from y to (ms).
func (s *Stats) collect(ctx context.Context, accountIDs []model.AccountID, from, to int64) ([]stats.MessageRow, map[model.AccountID]*instance, error) {
	insts, err := s.instances(accountIDs)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[model.AccountID]*instance, len(insts))
	var rows []stats.MessageRow
	for _, inst := range insts {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		got, err := inst.store.MessageStats(ctx, from, to, nil)
		if err != nil {
			return nil, nil, err
		}
		byID[inst.acc.ID] = inst
		rows = append(rows, statRowsToMessageRows(inst.acc.ID, got, true)...)
	}
	return rows, byID, nil
}

// statsChatInfo completa título, avatar, tipo y canal de un chat desde su almacén.
func statsChatInfo(ctx context.Context, inst *instance, native string) (title, avatar string, kind model.ChatKind, ok bool) {
	if inst == nil {
		return native, "", "", false
	}
	c, err := inst.store.GetChat(ctx, native)
	if err != nil {
		return native, "", "", false
	}
	title = c.Title
	if title == "" {
		title = native
	}
	return title, c.AvatarURL, c.Kind, true
}

// Conversations: la pestaña «Conversaciones» del periodo, comparada con el anterior.
func (s *Stats) Conversations(ctx context.Context, accountIDs []model.AccountID, rng stats.Range) (stats.ConversationOverview, error) {
	now := s.now()
	pFrom, _ := stats.PreviousBounds(rng, now, s.loc)
	_, to := stats.Bounds(rng, now, s.loc)
	rows, byID, err := s.collect(ctx, accountIDs, pFrom.UnixMilli(), to.UnixMilli())
	if err != nil {
		return stats.ConversationOverview{}, err
	}
	out := stats.BuildConversationOverview(rng, now, s.loc, rows)
	for _, list := range [][]stats.ChatRank{out.TopChats, out.Frequent} {
		for i := range list {
			r := &list[i]
			inst := byID[r.AccountID]
			if inst != nil {
				r.Channel = inst.acc.Channel
			}
			title, avatar, kind, _ := statsChatInfo(ctx, inst, r.NativeID)
			r.Title, r.AvatarURL, r.Kind = title, avatar, kind
		}
	}
	return out, nil
}

// Chat: las estadísticas de un chat (todo su historial guardado; el periodo acota el gráfico).
func (s *Stats) Chat(ctx context.Context, key model.ChatKey, rng stats.Range) (stats.ContactConversationStats, error) {
	accID, native := key.Split()
	if accID == "" || native == "" {
		return stats.ContactConversationStats{}, msgInvalid("la clave del chat no es válida")
	}
	inst, err := s.acc.mustGet(accID)
	if err != nil {
		return stats.ContactConversationStats{}, err
	}
	if _, err := inst.store.GetChat(ctx, native); err != nil {
		return stats.ContactConversationStats{}, err
	}
	got, err := inst.store.MessageStats(ctx, 0, 0, []string{native})
	if err != nil {
		return stats.ContactConversationStats{}, err
	}
	return stats.BuildChatStats(rng, s.now(), s.loc, statRowsToMessageRows(accID, got, false)), nil
}

// Groups: la pestaña «Grupos»: grupos y personas más activas del periodo.
func (s *Stats) Groups(ctx context.Context, accountIDs []model.AccountID, rng stats.Range) (stats.GroupsOverview, error) {
	now := s.now()
	from, to := stats.Bounds(rng, now, s.loc)
	rows, byID, err := s.collect(ctx, accountIDs, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return stats.GroupsOverview{}, err
	}
	out := stats.BuildGroupsOverview(rng, now, s.loc, rows)
	for i := range out.Groups {
		g := &out.Groups[i]
		inst := byID[g.AccountID]
		if inst != nil {
			g.Channel = inst.acc.Channel
		}
		g.Title, g.AvatarURL, _, _ = statsChatInfo(ctx, inst, g.NativeID)
	}
	for i := range out.People {
		p := &out.People[i]
		inst := byID[p.AccountID]
		p.Name = p.SenderID
		if inst == nil {
			continue
		}
		p.Channel = inst.acc.Channel
		if per, err := inst.store.GetPerson(ctx, p.SenderID); err == nil {
			p.Name, p.AvatarURL = per.Display(), per.AvatarURL
		} else if !errors.Is(err, port.ErrNotFound) && ctx.Err() != nil {
			return stats.GroupsOverview{}, ctx.Err()
		}
	}
	return out, nil
}
