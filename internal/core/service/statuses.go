package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/stats"
	"dconnector/internal/paths"
)

const (
	// statusViewsEvery: cada cuánto se refrescan las vistas de mis estados (06 §6).
	statusViewsEvery = 10 * time.Minute
	// statusViewsFirst: espera antes de la primera pasada tras arrancar (las cuentas aún conectan).
	statusViewsFirst = time.Minute
	// statusViewsWindow: se refrescan los estados míos de las últimas 24 h.
	statusViewsWindow = 24 * time.Hour
	// statusViewersTimeout: tope de una consulta de vistas al adaptador.
	statusViewersTimeout = 20 * time.Second
	// statusTTL: vigencia por defecto de un estado publicado si el canal no la informa.
	statusTTL = 24 * time.Hour
)

// Statuses: estados (06 §6).
type Statuses struct {
	acc    *Accounts
	router *Router
	app    port.AppStore

	// Ajustables en pruebas (se fijan antes de Start).
	now          func() time.Time
	loc          *time.Location
	refreshEvery time.Duration
	refreshFirst time.Duration

	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewStatuses(acc *Accounts, r *Router, app port.AppStore) *Statuses {
	return &Statuses{
		acc: acc, router: r, app: app,
		now: time.Now, loc: time.Local,
		refreshEvery: statusViewsEvery, refreshFirst: statusViewsFirst,
	}
}

// ---------------------------------------------------------------------------------------------
// Consulta

// instances: las cuentas pedidas (sin repetir) o, si no se pide ninguna, todas las cargadas.
func (s *Statuses) instances(ids []model.AccountID) ([]*instance, error) {
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

// Authors: una fila por persona con estados vigentes, de las cuentas pedidas (vacío = todas),
// la más reciente primero.
func (s *Statuses) Authors(ctx context.Context, accountIDs []model.AccountID) ([]model.StatusAuthor, error) {
	insts, err := s.instances(accountIDs)
	if err != nil {
		return nil, err
	}
	now := s.now().UnixMilli()
	out := []model.StatusAuthor{}
	for _, inst := range insts {
		rows, err := inst.store.ListStatusAuthors(ctx, now)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			r.AccountID = inst.acc.ID
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.LastAt != b.LastAt {
			return a.LastAt > b.LastAt
		}
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		return a.AuthorID < b.AuthorID
	})
	return out, nil
}

// ByAuthor: los estados vigentes de una persona.
func (s *Statuses) ByAuthor(ctx context.Context, acc model.AccountID, authorID string) ([]model.Status, error) {
	inst, err := s.acc.mustGet(acc)
	if err != nil {
		return nil, err
	}
	if authorID == "" {
		return nil, msgInvalid("falta la persona")
	}
	return inst.store.ListStatuses(ctx, authorID, s.now().UnixMilli())
}

// Mine: mis estados publicados entre from y to (ms); to = 0 significa «hasta ahora».
func (s *Statuses) Mine(ctx context.Context, acc model.AccountID, from, to int64) ([]model.Status, error) {
	inst, err := s.acc.mustGet(acc)
	if err != nil {
		return nil, err
	}
	if to <= 0 {
		to = s.now().Add(time.Hour).UnixMilli()
	}
	return inst.store.ListMyStatuses(ctx, from, to)
}

// ---------------------------------------------------------------------------------------------
// Emisión por el sink de la cuenta

// emitStatus emite el estado por el sink (se guarda con UpsertStatus y se publica como
// status.posted). Si la cuenta no tiene sink, lo guarda directamente.
func (s *Statuses) emitStatus(inst *instance, st model.Status) {
	cp := st
	if sink, ok := s.router.SinkFor(inst.acc.ID); ok {
		sink.Emit(model.Event{Type: model.EvStatusUpsert, At: s.now().UnixMilli(), Status: &cp})
		return
	}
	slog.Warn("no hay enlace de eventos para la cuenta", "accountId", string(inst.acc.ID), "type", string(model.EvStatusUpsert))
	if err := inst.store.UpsertStatus(context.Background(), cp); err != nil {
		slog.Warn("no se pudo guardar el estado", "accountId", string(inst.acc.ID), "err", err)
	}
}

// emitView emite una vista nueva o cambiada (status.viewed).
func (s *Statuses) emitView(inst *instance, v model.StatusView) {
	cp := v
	at := v.At
	if at == 0 {
		at = s.now().UnixMilli()
	}
	if sink, ok := s.router.SinkFor(inst.acc.ID); ok {
		sink.Emit(model.Event{Type: model.EvStatusView, At: at, StatusView: &cp})
		return
	}
	if err := inst.store.UpsertStatusView(context.Background(), cp); err != nil {
		slog.Warn("no se pudo guardar la vista del estado", "accountId", string(inst.acc.ID), "err", err)
	}
}

// ---------------------------------------------------------------------------------------------
// Publicar

// validateOutgoingStatus aplica las capacidades de la cuenta a lo que se pide publicar.
func validateOutgoingStatus(st model.OutgoingStatus, c capability.Set) error {
	var maxSize int64
	switch st.Kind {
	case model.KindText:
		if strings.TrimSpace(st.Text) == "" {
			return msgInvalid("el estado de texto está vacío")
		}
	case model.KindImage:
		maxSize = c.MaxImageSize
	case model.KindVideo:
		maxSize = c.MaxVideoSize
	default:
		return msgInvalid("un estado puede ser de texto, foto o video, no %q", st.Kind)
	}
	if err := checkTextLen(st.Text, c); err != nil {
		return err
	}
	if st.Kind == model.KindText {
		return nil
	}
	if st.FilePath == "" {
		return msgInvalid("falta el archivo")
	}
	fi, err := os.Stat(st.FilePath)
	if err != nil || fi.IsDir() {
		return msgInvalid("no se encontró el archivo")
	}
	if maxSize > 0 && fi.Size() > maxSize {
		return msgInvalid("el archivo pesa %s y el máximo es %s", sizeMB(fi.Size()), sizeMB(maxSize))
	}
	return nil
}

func uniqueStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// resolveAudience valida la audiencia con las capacidades y convierte las listas en PeerIDs: las
// identidades de ESTA cuenta de los miembros de las listas. Con listas, el modo pasa a «allow»
// (salvo que se pida «deny»: «mis contactos excepto estas listas»).
func (s *Statuses) resolveAudience(ctx context.Context, acc model.AccountID, a model.StatusAudience, c capability.Set) (model.StatusAudience, error) {
	mode := a.Mode
	switch mode {
	case "":
		mode = "contacts"
	case "contacts", "allow", "deny":
	default:
		return a, msgInvalid("el modo de audiencia %q no es válido", mode)
	}
	if (mode != "contacts" || len(a.PeerIDs) > 0 || len(a.ListIDs) > 0) && !c.StatusAudience {
		return a, msgUnsupported("este canal usa la privacidad de estados del teléfono: no se puede elegir la audiencia")
	}
	peers := uniqueStrings(a.PeerIDs)
	if len(a.ListIDs) > 0 {
		members, err := s.app.ListMembers(ctx, a.ListIDs)
		if err != nil {
			return a, err
		}
		for _, ct := range members {
			for _, id := range ct.Identities {
				if id.AccountID == acc && id.PeerNativeID != "" {
					peers = append(peers, id.PeerNativeID)
				}
			}
		}
		peers = uniqueStrings(peers)
		if len(peers) == 0 {
			return a, msgInvalid("las listas elegidas no tienen contactos en esta cuenta")
		}
		if mode != "deny" {
			mode = "allow"
		}
	}
	switch mode {
	case "contacts":
		if len(peers) > 0 {
			return a, msgInvalid("para elegir personas hay que usar «solo compartir con» o «excepto»")
		}
	case "allow":
		if len(peers) == 0 {
			return a, msgInvalid("elige al menos una persona o lista para compartir el estado")
		}
	}
	return model.StatusAudience{Mode: mode, PeerIDs: peers, ListIDs: []string{}}, nil
}

// copyStatusMedia copia el archivo a <data>/status-media con un nombre propio (el original puede
// borrarse o cambiar mientras el estado sigue vigente o programado).
func copyStatusMedia(src string) (dst string, size int64, err error) {
	dir := paths.StatusMediaDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("estados: crear carpeta: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(src))
	if len(ext) > 8 {
		ext = ""
	}
	dst = filepath.Join(dir, model.NewID()+ext)
	in, err := os.Open(src)
	if err != nil {
		return "", 0, fmt.Errorf("estados: abrir el archivo: %w", err)
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", 0, fmt.Errorf("estados: copiar el archivo: %w", err)
	}
	size, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", 0, fmt.Errorf("estados: copiar el archivo: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", 0, fmt.Errorf("estados: copiar el archivo: %w", err)
	}
	return dst, size, nil
}

// Post publica un estado: valida con las capacidades de la cuenta, resuelve la audiencia, copia
// el archivo a status-media, lo publica por el adaptador y emite status.posted por el sink.
func (s *Statuses) Post(ctx context.Context, acc model.AccountID, st model.OutgoingStatus) (model.Status, error) {
	inst, err := s.acc.mustGet(acc)
	if err != nil {
		return model.Status{}, err
	}
	ad := inst.getAdapter()
	caps := ad.Capabilities(nil)
	if !caps.PostStatus {
		return model.Status{}, msgUnsupported("esta cuenta no permite publicar estados")
	}
	if err := validateOutgoingStatus(st, caps); err != nil {
		return model.Status{}, err
	}
	aud, err := s.resolveAudience(ctx, acc, st.Audience, caps)
	if err != nil {
		return model.Status{}, err
	}
	st.Audience = aud

	var (
		local string
		size  int64
	)
	if st.Kind != model.KindText {
		orig := st.FilePath
		if st.Mime == "" {
			st.Mime = mime.TypeByExtension(strings.ToLower(filepath.Ext(orig)))
		}
		if local, size, err = copyStatusMedia(orig); err != nil {
			return model.Status{}, err
		}
		st.FilePath = local
	}
	posted, err := ad.PostStatus(ctx, st)
	if err != nil {
		if local != "" {
			os.Remove(local)
		}
		return model.Status{}, err
	}
	posted = s.completePosted(inst, posted, st, local, size)
	if posted.NativeID == "" {
		slog.Warn("el canal publicó el estado sin devolver su id", "accountId", string(acc))
		return posted, nil
	}
	s.emitStatus(inst, posted)
	return posted, nil
}

// completePosted rellena lo que el adaptador no haya puesto del estado recién publicado.
func (s *Statuses) completePosted(inst *instance, p model.Status, out model.OutgoingStatus, local string, size int64) model.Status {
	now := s.now().UnixMilli()
	p.AccountID, p.Channel, p.FromMe = inst.acc.ID, inst.acc.Channel, true
	p.Key = string(inst.acc.ID) + "|" + p.NativeID
	if p.AuthorNativeID == "" {
		p.AuthorNativeID = inst.acc.SelfID
	}
	if p.AuthorName == "" {
		p.AuthorName = inst.acc.SelfName
	}
	if p.PostedAt == 0 {
		p.PostedAt = now
	}
	if p.ExpiresAt == 0 {
		p.ExpiresAt = p.PostedAt + statusTTL.Milliseconds()
	}
	if p.Kind == "" {
		p.Kind = out.Kind
	}
	if p.Text == "" {
		p.Text = out.Text
	}
	if p.BgColor == "" {
		p.BgColor = out.BgColor
	}
	if p.Font == 0 {
		p.Font = out.Font
	}
	if p.Delivery == "" {
		p.Delivery = model.DeliverySent
	}
	if local != "" {
		var md model.Media
		if p.Media != nil {
			md = *p.Media
		}
		if md.ID == "" {
			md.ID = p.NativeID
		}
		if md.Mime == "" {
			md.Mime = out.Mime
		}
		if md.FileName == "" {
			md.FileName = filepath.Base(local)
		}
		if md.Size == 0 {
			md.Size = size
		}
		if md.LocalPath == "" {
			md.LocalPath = local
		}
		p.Media = &md
	}
	p.Media = withMediaURL(inst.acc.ID, p.Media)
	return p
}

// ---------------------------------------------------------------------------------------------
// Buscar un estado

// splitStatusKey separa «<cuenta>|<idNativo>» y comprueba que sea de la cuenta pedida.
func splitStatusKey(acc model.AccountID, key string) (string, error) {
	p := strings.SplitN(key, "|", 2)
	if len(p) != 2 || p[1] == "" {
		return "", msgInvalid("clave de estado no válida")
	}
	if model.AccountID(p[0]) != acc {
		return "", msgInvalid("el estado no es de esta cuenta")
	}
	return p[1], nil
}

// findMine busca uno de mis estados por su id nativo.
func (s *Statuses) findMine(ctx context.Context, inst *instance, native string) (model.Status, bool, error) {
	list, err := inst.store.ListMyStatuses(ctx, 0, s.now().Add(time.Hour).UnixMilli())
	if err != nil {
		return model.Status{}, false, err
	}
	for _, st := range list {
		if st.NativeID == native {
			return st, true, nil
		}
	}
	return model.Status{}, false, nil
}

// findStatus busca un estado (mío o ajeno vigente) por su id nativo.
func (s *Statuses) findStatus(ctx context.Context, inst *instance, native string) (model.Status, error) {
	if st, ok, err := s.findMine(ctx, inst, native); err != nil {
		return model.Status{}, err
	} else if ok {
		return st, nil
	}
	now := s.now().UnixMilli()
	authors, err := inst.store.ListStatusAuthors(ctx, now)
	if err != nil {
		return model.Status{}, err
	}
	for _, a := range authors {
		list, err := inst.store.ListStatuses(ctx, a.AuthorID, now)
		if err != nil {
			return model.Status{}, err
		}
		for _, st := range list {
			if st.NativeID == native {
				return st, nil
			}
		}
	}
	return model.Status{}, fmt.Errorf("%w: estado %s", port.ErrNotFound, native)
}

// ---------------------------------------------------------------------------------------------
// Acciones sobre un estado

// Delete borra uno de mis estados en el canal y lo deja marcado como borrado en local.
func (s *Statuses) Delete(ctx context.Context, acc model.AccountID, statusNativeID string) error {
	inst, err := s.acc.mustGet(acc)
	if err != nil {
		return err
	}
	if statusNativeID == "" {
		return msgInvalid("falta el estado")
	}
	st, found, err := s.findMine(ctx, inst, statusNativeID)
	if err != nil {
		return err
	}
	if err := inst.getAdapter().DeleteStatus(ctx, statusNativeID); err != nil {
		return err
	}
	if found {
		st.DeletedAt = s.now().UnixMilli()
		s.emitStatus(inst, st)
	}
	return nil
}

// MarkViewed marca como visto un estado AJENO: avisa al adaptador y lo deja `Viewed` en local.
func (s *Statuses) MarkViewed(ctx context.Context, acc model.AccountID, statusKey string) error {
	inst, err := s.acc.mustGet(acc)
	if err != nil {
		return err
	}
	native, err := splitStatusKey(acc, statusKey)
	if err != nil {
		return err
	}
	st, err := s.findStatus(ctx, inst, native)
	if err != nil {
		return err
	}
	if st.FromMe {
		return msgInvalid("solo se pueden marcar como vistos los estados de otras personas")
	}
	if st.Viewed {
		return nil
	}
	if err := inst.getAdapter().MarkStatusViewed(ctx, st); err != nil && !errors.Is(err, port.ErrUnsupported) {
		return err
	}
	st.Viewed = true
	s.emitStatus(inst, st)
	return nil
}

// React reacciona a un estado ajeno (si el canal tiene StatusReact).
func (s *Statuses) React(ctx context.Context, acc model.AccountID, statusKey, emoji string) error {
	inst, err := s.acc.mustGet(acc)
	if err != nil {
		return err
	}
	native, err := splitStatusKey(acc, statusKey)
	if err != nil {
		return err
	}
	ad := inst.getAdapter()
	if !ad.Capabilities(nil).StatusReact {
		return msgUnsupported("este canal no permite reaccionar a estados")
	}
	st, err := s.findStatus(ctx, inst, native)
	if err != nil {
		return err
	}
	if st.FromMe {
		return msgInvalid("solo se puede reaccionar a los estados de otras personas")
	}
	return ad.ReactStatus(ctx, st, emoji)
}

// Viewers: quién vio uno de mis estados. Si el canal informa las vistas, se consultan primero y
// las nuevas se guardan y se publican (status.viewed); si la consulta falla, se devuelve lo que
// ya hay en local. La más reciente primero.
func (s *Statuses) Viewers(ctx context.Context, acc model.AccountID, statusNativeID string) ([]model.StatusView, error) {
	inst, err := s.acc.mustGet(acc)
	if err != nil {
		return nil, err
	}
	if statusNativeID == "" {
		return nil, msgInvalid("falta el estado")
	}
	ad := inst.getAdapter()
	if !ad.Capabilities(nil).StatusViewers {
		return s.localViewers(ctx, inst, statusNativeID)
	}
	rctx, cancel := context.WithTimeout(ctx, statusViewersTimeout)
	defer cancel()
	merged, err := s.refreshViewers(rctx, inst, ad, statusNativeID)
	if err != nil {
		if !errors.Is(err, port.ErrUnsupported) && !errors.Is(err, port.ErrNotConnected) {
			slog.Debug("no se pudieron consultar las vistas del estado", "accountId", string(acc), "err", err)
		}
		return s.localViewers(ctx, inst, statusNativeID)
	}
	return merged, nil
}

func sortViews(v []model.StatusView) {
	sort.SliceStable(v, func(i, j int) bool {
		if v[i].At != v[j].At {
			return v[i].At > v[j].At
		}
		return v[i].ViewerID < v[j].ViewerID
	})
}

func (s *Statuses) localViewers(ctx context.Context, inst *instance, native string) ([]model.StatusView, error) {
	views, err := inst.store.ListStatusViews(ctx, string(inst.acc.ID)+"|"+native)
	if err != nil {
		return nil, err
	}
	views = append([]model.StatusView{}, views...)
	sortViews(views)
	return views, nil
}

// refreshViewers pide las vistas al adaptador, emite las nuevas o con otra reacción y devuelve
// la unión de lo que había y lo que llegó.
func (s *Statuses) refreshViewers(ctx context.Context, inst *instance, ad port.ChannelAdapter, native string) ([]model.StatusView, error) {
	fresh, err := ad.StatusViewers(ctx, native)
	if err != nil {
		return nil, err
	}
	key := string(inst.acc.ID) + "|" + native
	known, err := inst.store.ListStatusViews(ctx, key)
	if err != nil {
		return nil, err
	}
	merged := map[string]model.StatusView{}
	for _, v := range known {
		merged[v.ViewerID] = v
	}
	for _, v := range fresh {
		if v.ViewerID == "" {
			continue
		}
		v.StatusKey = key
		old, ok := merged[v.ViewerID]
		if ok {
			if v.At == 0 {
				v.At = old.At
			}
			if old.Reaction == v.Reaction && old.At == v.At && old.ViewerName == v.ViewerName {
				continue
			}
		}
		merged[v.ViewerID] = v
		s.emitView(inst, v)
	}
	out := make([]model.StatusView, 0, len(merged))
	for _, v := range merged {
		out = append(out, v)
	}
	sortViews(out)
	return out, nil
}

// ---------------------------------------------------------------------------------------------
// Refresco periódico de vistas

// Start arranca el refresco de vistas: cada 10 min, para mis estados de las últimas 24 h de las
// cuentas conectadas que informan las vistas. Se detiene con Stop o al cancelar ctx. Llamarlo
// dos veces seguidas no hace nada.
func (s *Statuses) Start(ctx context.Context) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.cancel != nil {
		select {
		case <-s.done: // la ejecución anterior terminó sola (ctx cancelado): se puede volver a arrancar
			s.cancel, s.done = nil, nil
		default:
			return
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	go s.loop(cctx, done)
}

// Stop detiene el refresco y espera a que termine la pasada en curso. Es seguro llamarlo sin
// haber arrancado o varias veces.
func (s *Statuses) Stop() {
	s.runMu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.runMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (s *Statuses) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	delay := s.refreshFirst
	for {
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		s.safeRefresh(ctx)
		delay = s.refreshEvery
	}
}

// safeRefresh: un pánico en una pasada se registra y el bucle sigue (02 §6).
func (s *Statuses) safeRefresh(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("pánico al refrescar las vistas de los estados", "panic", rec)
		}
	}()
	s.refreshViews(ctx)
}

// refreshViews hace una pasada: por cada cuenta conectada con StatusViewers, las vistas de mis
// estados de las últimas 24 h.
func (s *Statuses) refreshViews(ctx context.Context) {
	now := s.now()
	from, to := now.Add(-statusViewsWindow).UnixMilli(), now.Add(time.Hour).UnixMilli()
	for _, inst := range s.acc.snapshot() {
		if ctx.Err() != nil {
			return
		}
		if inst.getStatus().State != model.StateConnected {
			continue
		}
		ad := inst.getAdapter()
		if !ad.Capabilities(nil).StatusViewers {
			continue
		}
		mine, err := inst.store.ListMyStatuses(ctx, from, to)
		if err != nil {
			slog.Warn("no se pudieron listar mis estados", "accountId", string(inst.acc.ID), "err", err)
			continue
		}
		for _, st := range mine {
			if ctx.Err() != nil {
				return
			}
			if st.DeletedAt > 0 || st.NativeID == "" || (st.ExpiresAt > 0 && st.ExpiresAt < now.UnixMilli()) {
				continue
			}
			rctx, cancel := context.WithTimeout(ctx, statusViewersTimeout)
			_, err := s.refreshViewers(rctx, inst, ad, st.NativeID)
			cancel()
			if err != nil && !errors.Is(err, port.ErrUnsupported) && !errors.Is(err, port.ErrNotConnected) && ctx.Err() == nil {
				slog.Debug("no se pudieron refrescar las vistas", "accountId", string(inst.acc.ID), "err", err)
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Estadísticas

// statusInputs lee mis estados publicados en [from, to) con sus vistas.
func (s *Statuses) statusInputs(ctx context.Context, inst *instance, from, to time.Time) ([]stats.StatusInput, error) {
	list, err := inst.store.ListMyStatuses(ctx, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	out := make([]stats.StatusInput, 0, len(list))
	for _, st := range list {
		if !st.FromMe {
			continue
		}
		views, err := inst.store.ListStatusViews(ctx, st.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, stats.StatusInput{Status: st, Views: views})
	}
	return out, nil
}

// Stats junta mis estados y sus vistas del periodo (y del anterior) y calcula las estadísticas.
// Los cortes de día usan la zona horaria local del equipo.
func (s *Statuses) Stats(ctx context.Context, acc model.AccountID, rng stats.Range) (stats.StatusStats, error) {
	inst, err := s.acc.mustGet(acc)
	if err != nil {
		return stats.StatusStats{}, err
	}
	now := s.now()
	from, to := stats.Bounds(rng, now, s.loc)
	pFrom, pTo := stats.PreviousBounds(rng, now, s.loc)
	cur, err := s.statusInputs(ctx, inst, from, to)
	if err != nil {
		return stats.StatusStats{}, err
	}
	prev, err := s.statusInputs(ctx, inst, pFrom, pTo)
	if err != nil {
		return stats.StatusStats{}, err
	}
	return stats.BuildStatusStats(rng, now, s.loc, cur, prev), nil
}
