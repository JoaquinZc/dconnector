package whatsapp

// Encuestas: whatsmeow solo entrega los votos como hashes sha256 de las opciones, cifrados.
// El adaptador guarda las opciones de cada encuesta y los votos de cada persona para poder emitir
// el estado completo (opciones con sus votantes) en cada message.updated (07 §6, PollUpdateMessage).

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"sort"
	"sync"

	"dconnector/internal/core/model"
)

// pollState: opciones y votos de una encuesta.
type pollState struct {
	Question string
	Options  []string
	Multi    bool
	Votes    map[string][]string // votante (id nativo) → nombres de las opciones elegidas
}

// apply registra el voto de una persona a partir de los hashes de sus opciones elegidas.
// Un voto sin opciones quita el voto anterior. Devuelve false si ningún hash coincide con una
// opción (voto de otra encuesta o de una versión que no se conoce) y no era un voto vacío.
func (p *pollState) apply(voter string, hashes [][]byte) bool {
	byHash := make(map[[32]byte]string, len(p.Options))
	for _, o := range p.Options {
		byHash[sha256.Sum256([]byte(o))] = o
	}
	var chosen []string
	for _, h := range hashes {
		if len(h) != 32 {
			continue
		}
		var k [32]byte
		copy(k[:], h)
		if name, ok := byHash[k]; ok {
			chosen = append(chosen, name)
		}
	}
	if len(chosen) == 0 && len(hashes) > 0 {
		return false
	}
	if p.Votes == nil {
		p.Votes = map[string][]string{}
	}
	if len(chosen) == 0 {
		delete(p.Votes, voter)
	} else {
		p.Votes[voter] = chosen
	}
	return true
}

// setVote fija el voto de una persona con nombres de opción (mi propio voto).
func (p *pollState) setVote(voter string, names []string) {
	if p.Votes == nil {
		p.Votes = map[string][]string{}
	}
	valid := map[string]bool{}
	for _, o := range p.Options {
		valid[o] = true
	}
	var chosen []string
	for _, n := range names {
		if valid[n] {
			chosen = append(chosen, n)
		}
	}
	if len(chosen) == 0 {
		delete(p.Votes, voter)
		return
	}
	p.Votes[voter] = chosen
}

// model devuelve la encuesta del modelo con los votantes de cada opción (ordenados).
func (p *pollState) model() *model.Poll {
	out := &model.Poll{Question: p.Question, Multi: p.Multi}
	for _, o := range p.Options {
		opt := model.PollOption{Name: o, Voters: []string{}}
		for voter, chosen := range p.Votes {
			for _, c := range chosen {
				if c == o {
					opt.Voters = append(opt.Voters, voter)
					break
				}
			}
		}
		sort.Strings(opt.Voters)
		out.Options = append(out.Options, opt)
	}
	return out
}

// pollBook guarda las encuestas en memoria y en wa_polls.
type pollBook struct {
	db *sql.DB

	mu    sync.Mutex
	cache map[string]*pollState
}

func newPollBook(db *sql.DB) *pollBook {
	return &pollBook{db: db, cache: map[string]*pollState{}}
}

func pollKey(chat, id string) string { return chat + "|" + id }

// load devuelve la encuesta (de memoria o de la base) o nil. Debe llamarse con p.mu tomado.
func (p *pollBook) load(ctx context.Context, chat, id string) *pollState {
	k := pollKey(chat, id)
	if st, ok := p.cache[k]; ok {
		return st
	}
	if p.db == nil {
		return nil
	}
	var question, options, votes string
	var multi int
	err := p.db.QueryRowContext(ctx, "SELECT question, options, multi, votes FROM wa_polls WHERE chat = ? AND id = ?", chat, id).
		Scan(&question, &options, &multi, &votes)
	if err != nil {
		return nil
	}
	st := &pollState{Question: question, Multi: multi != 0}
	_ = json.Unmarshal([]byte(options), &st.Options)
	_ = json.Unmarshal([]byte(votes), &st.Votes)
	p.cache[k] = st
	return st
}

func (p *pollBook) save(ctx context.Context, chat, id string, st *pollState) {
	if p.db == nil {
		return
	}
	options, _ := json.Marshal(st.Options)
	votes, _ := json.Marshal(st.Votes)
	multi := 0
	if st.Multi {
		multi = 1
	}
	_, _ = p.db.ExecContext(ctx,
		`INSERT INTO wa_polls(chat, id, question, options, multi, votes) VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(chat, id) DO UPDATE SET question = excluded.question, options = excluded.options,
		   multi = excluded.multi, votes = excluded.votes`,
		chat, id, st.Question, string(options), multi, string(votes))
}

// register anota una encuesta vista (mensaje nuevo, enviado o del historial) y devuelve su estado
// del modelo, con los votos que ya se hubieran acumulado.
func (p *pollBook) register(ctx context.Context, chat, id string, poll *model.Poll) *model.Poll {
	if poll == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.load(ctx, chat, id)
	if st == nil {
		st = &pollState{Question: poll.Question, Multi: poll.Multi}
		for _, o := range poll.Options {
			st.Options = append(st.Options, o.Name)
		}
		p.cache[pollKey(chat, id)] = st
		p.save(ctx, chat, id, st)
	}
	return st.model()
}

// vote aplica el voto de una persona; devuelve la encuesta resultante o false si no se conoce la encuesta.
func (p *pollBook) vote(ctx context.Context, chat, id, voter string, hashes [][]byte) (*model.Poll, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.load(ctx, chat, id)
	if st == nil {
		return nil, false
	}
	if !st.apply(voter, hashes) {
		return nil, false
	}
	p.save(ctx, chat, id, st)
	return st.model(), true
}

// myVote fija mi voto con nombres de opción; devuelve las opciones de la encuesta y la encuesta resultante.
func (p *pollBook) myVote(ctx context.Context, chat, id, me string, names []string) (*model.Poll, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.load(ctx, chat, id)
	if st == nil {
		return nil, false
	}
	st.setVote(me, names)
	p.save(ctx, chat, id, st)
	return st.model(), true
}

// options devuelve las opciones de una encuesta conocida.
func (p *pollBook) options(ctx context.Context, chat, id string) ([]string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.load(ctx, chat, id)
	if st == nil {
		return nil, false
	}
	return append([]string(nil), st.Options...), true
}

func (p *pollBook) reset() {
	p.mu.Lock()
	p.cache = map[string]*pollState{}
	p.mu.Unlock()
}
