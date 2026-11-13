package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dconnector/internal/core/bus"
	"dconnector/internal/core/campaign"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

// Claves de app_settings que usa Campaigns (06 §3 y §8). `optout_footer_text` y
// `optout_keywords` están en settings.go.
const (
	settingPacingPrefix = "pacing:"            // pacing:<accountId> → JSON de model.PacingSettings
	settingWarmupPrefix = "warmup_started_at:" // warmup_started_at:<accountId> → ms desde epoch, texto
	campaignTick        = 5 * time.Second      // tick del motor
	recentCampaignDays  = 30                   // «campañas recientes» para avisar de textos repetidos
	maxRiskMessages     = 200                  // mensajes que se miran de un chat para sacar su riesgo
	msgCampaignsOnly    = "las campañas solo se envían por cuentas de WhatsApp o Telegram"
)

// Campaigns: campañas y su motor (06 §8). La lógica pura está en core/campaign; el motor (un
// bucle por cuenta con campañas en marcha) está en campaign_engine.go.
type Campaigns struct {
	app      port.AppStore
	acc      *Accounts
	msg      *Messaging
	bus      *bus.Bus
	progress chan CampaignProgress

	// Ajustables en pruebas (se fijan antes de Start).
	now   func() time.Time
	loc   *time.Location
	tick  time.Duration
	sleep func(ctx context.Context, d time.Duration)
	randf func() float64

	mu        sync.Mutex      // serializa leer-modificar-guardar una campaña
	launching map[string]bool // campañas cuya cola se está generando

	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
	wg     sync.WaitGroup

	wmu     sync.Mutex
	workers map[model.AccountID]bool
	wstate  map[model.AccountID]*sendState

	pmu      sync.Mutex
	lastProg map[string]CampaignProgress

	smu   sync.Mutex
	stats map[string]*campStats
}

func NewCampaigns(app port.AppStore, acc *Accounts, msg *Messaging, b *bus.Bus) *Campaigns {
	return &Campaigns{
		app: app, acc: acc, msg: msg, bus: b,
		progress: make(chan CampaignProgress, 256),
		now:      time.Now, loc: time.Local, tick: campaignTick, sleep: sleepCtx, randf: rand.Float64,
		launching: map[string]bool{},
		wake:      make(chan struct{}, 1),
		workers:   map[model.AccountID]bool{},
		wstate:    map[model.AccountID]*sendState{},
		lastProg:  map[string]CampaignProgress{},
		stats:     map[string]*campStats{},
	}
}

var _ IncomingObserver = (*Campaigns)(nil)

func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// Progress: avisos de cambio de campañas (búfer 256, descarta si está lleno).
func (c *Campaigns) Progress() <-chan CampaignProgress { return c.progress }

func (c *Campaigns) nowMs() int64 { return c.now().UnixMilli() }

func (c *Campaigns) kick() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// ---------------------------------------------------------------------------------------------
// Ritmo por cuenta (ajuste pacing:<id>)

// GetPacing devuelve el ritmo de la cuenta; si no hay (o está dañado) el de por defecto.
func (c *Campaigns) GetPacing(ctx context.Context, acc model.AccountID) (model.PacingSettings, error) {
	raw, err := c.app.GetSetting(ctx, settingPacingPrefix+string(acc))
	if err != nil {
		return model.DefaultPacing, err
	}
	p := model.DefaultPacing
	if strings.TrimSpace(raw) == "" {
		return p, nil
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		slog.Warn("ritmo de envío dañado, se usa el de por defecto", "accountId", string(acc))
		return model.DefaultPacing, nil
	}
	return p, nil
}

// SetPacing guarda el ritmo de la cuenta (ajuste pacing:<id>) con los valores corregidos.
func (c *Campaigns) SetPacing(ctx context.Context, acc model.AccountID, p model.PacingSettings) error {
	if _, err := c.app.GetAccount(ctx, acc); err != nil {
		return err
	}
	raw, err := json.Marshal(normalizePacing(p))
	if err != nil {
		return err
	}
	return c.app.SetSetting(ctx, settingPacingPrefix+string(acc), string(raw))
}

// normalizePacing corrige valores imposibles: negativos a 0, esperas con un mínimo de 1 s y
// ordenadas, calentamiento y freno con sus mínimos de la base (pacingFrom de pacing.ts).
func normalizePacing(p model.PacingSettings) model.PacingSettings {
	nz := func(v int) int { return max(0, v) }
	p.MinDelaySec, p.MaxDelaySec = campaign.NormalizeDelayRange(p.MinDelaySec, p.MaxDelaySec)
	p.ColdMinDelaySec, p.ColdMaxDelaySec = campaign.NormalizeDelayRange(p.ColdMinDelaySec, p.ColdMaxDelaySec)
	p.DailyLimit, p.ColdDailyLimit = nz(p.DailyLimit), nz(p.ColdDailyLimit)
	p.TypingMsPerChar = nz(p.TypingMsPerChar)
	p.BatchSize, p.BatchRestMinutes, p.HourlyLimit = nz(p.BatchSize), nz(p.BatchRestMinutes), nz(p.HourlyLimit)
	p.WarmupStart, p.WarmupDays = max(1, p.WarmupStart), max(1, p.WarmupDays)
	p.BrakeMinSent, p.BrakeOptOuts = max(5, p.BrakeMinSent), max(1, p.BrakeOptOuts)
	p.FreqMax, p.FreqDays = nz(p.FreqMax), nz(p.FreqDays)
	return p
}

// effectivePacing: el ritmo propio de la campaña; si no tiene (todo en cero) el de su cuenta.
func (c *Campaigns) effectivePacing(ctx context.Context, cp model.Campaign) model.PacingSettings {
	if cp.Pacing != (model.PacingSettings{}) {
		return normalizePacing(cp.Pacing)
	}
	p, _ := c.GetPacing(ctx, cp.AccountID)
	return normalizePacing(p)
}

// warmupStart: cuándo se hizo el primer envío de campaña de la cuenta (cero si aún no).
func (c *Campaigns) warmupStart(ctx context.Context, acc model.AccountID) time.Time {
	raw, _ := c.app.GetSetting(ctx, settingWarmupPrefix+string(acc))
	var ms int64
	if _, err := fmt.Sscan(strings.TrimSpace(raw), &ms); err != nil || ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// markWarmupStart fija warmup_started_at:<id> al primer envío de campaña de la cuenta.
func (c *Campaigns) markWarmupStart(ctx context.Context, acc model.AccountID, at time.Time) {
	if !c.warmupStart(ctx, acc).IsZero() {
		return
	}
	if err := c.app.SetSetting(ctx, settingWarmupPrefix+string(acc), fmt.Sprint(at.UnixMilli())); err != nil {
		slog.Warn("no se pudo guardar el inicio del calentamiento", "accountId", string(acc), "err", err)
	}
}

// ---------------------------------------------------------------------------------------------
// Lectura

func (c *Campaigns) List(ctx context.Context) ([]model.Campaign, error) {
	cs, err := c.app.ListCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	for i := range cs {
		c.decorate(ctx, &cs[i])
	}
	return cs, nil
}

func (c *Campaigns) Get(ctx context.Context, id string) (model.Campaign, error) {
	cp, err := c.app.GetCampaign(ctx, id)
	if err != nil {
		return model.Campaign{}, err
	}
	c.decorate(ctx, &cp)
	return cp, nil
}

func (c *Campaigns) Queue(ctx context.Context, id string, states []string, limit int) ([]model.QueueItem, error) {
	if _, err := c.app.GetCampaign(ctx, id); err != nil {
		return nil, err
	}
	return c.app.ListQueue(ctx, id, states, limit)
}

// ---------------------------------------------------------------------------------------------
// Guardar

// accountFor valida la cuenta de la campaña: debe existir y ser de WhatsApp o Telegram.
func (c *Campaigns) accountFor(ctx context.Context, id model.AccountID) (model.Account, error) {
	if id == "" {
		return model.Account{}, msgInvalid("falta la cuenta desde la que se enviará la campaña")
	}
	a, err := c.app.GetAccount(ctx, id)
	if err != nil {
		return model.Account{}, err
	}
	if a.Channel != model.ChannelWhatsApp && a.Channel != model.ChannelTelegram {
		return model.Account{}, msgUnsupported(msgCampaignsOnly)
	}
	return a, nil
}

func validateWindow(w model.SendWindow) error {
	if w.FromMinute < 0 || w.FromMinute > 1440 || w.ToMinute < 0 || w.ToMinute > 1440 {
		return msgInvalid("la ventana horaria debe estar entre las 00:00 y las 24:00")
	}
	for _, d := range w.Weekdays {
		if d < 0 || d > 6 {
			return msgInvalid("los días de la ventana van de 0 (domingo) a 6 (sábado)")
		}
	}
	return nil
}

func cleanStrings(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Save crea o actualiza una campaña. Solo se puede guardar en draft, ready o paused; una campaña
// ready vuelve a draft (su cola se genera de nuevo al lanzarla). El adjunto se copia a
// <data>/campaign-media/<id>/. El estado, el motivo de freno y las fechas no los cambia quien
// llama.
func (c *Campaigns) Save(ctx context.Context, cp model.Campaign) (model.Campaign, error) {
	if _, err := c.accountFor(ctx, cp.AccountID); err != nil {
		return model.Campaign{}, err
	}
	if strings.TrimSpace(cp.Name) == "" {
		return model.Campaign{}, msgInvalid("la campaña necesita un nombre")
	}
	if err := validateWindow(cp.Window); err != nil {
		return model.Campaign{}, err
	}
	if len(cp.Variants) > campaign.MaxVariants-1 {
		return model.Campaign{}, msgInvalid("una campaña admite como máximo %d variantes", campaign.MaxVariants)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowMs()
	var prev model.Campaign
	isNew := false
	if cp.ID == "" {
		cp.ID, isNew = model.NewID(), true
	} else {
		var err error
		prev, err = c.app.GetCampaign(ctx, cp.ID)
		switch {
		case errors.Is(err, port.ErrNotFound):
			isNew = true
		case err != nil:
			return model.Campaign{}, err
		}
	}
	if isNew {
		cp.State, cp.CreatedAt, cp.BrakeReason = model.CampaignDraft, now, ""
	} else {
		switch prev.State {
		case model.CampaignDraft, model.CampaignReady, model.CampaignPaused:
		default:
			return model.Campaign{}, msgInvalid("una campaña %s ya no se puede editar", stateLabel(prev.State))
		}
		cp.State, cp.CreatedAt, cp.BrakeReason = prev.State, prev.CreatedAt, prev.BrakeReason
		if prev.State == model.CampaignReady {
			cp.State = model.CampaignDraft
		}
	}

	cp.Text = strings.TrimSpace(cp.Text)
	variants := make([]string, 0, len(cp.Variants))
	for _, v := range cp.Variants {
		if v = strings.TrimSpace(v); v != "" {
			variants = append(variants, v)
		}
	}
	cp.Variants = variants
	cp.ListIDs, cp.ContactIDs = cleanStrings(cp.ListIDs), cleanStrings(cp.ContactIDs)
	if cp.Window.Weekdays == nil {
		cp.Window.Weekdays = []int{}
	}
	if cp.Pacing == (model.PacingSettings{}) {
		p, _ := c.GetPacing(ctx, cp.AccountID)
		cp.Pacing = p
	}
	cp.Pacing = normalizePacing(cp.Pacing)
	cp.UpdatedAt = now

	if err := c.storeAttachment(&cp, prev.FilePath); err != nil {
		return model.Campaign{}, err
	}
	if err := c.app.SaveCampaign(ctx, cp); err != nil {
		return model.Campaign{}, err
	}
	out, err := c.app.GetCampaign(ctx, cp.ID)
	if err != nil {
		return model.Campaign{}, err
	}
	c.decorate(ctx, &out)
	c.emit(out, "", 0)
	return out, nil
}

func stateLabel(s model.CampaignState) string {
	switch s {
	case model.CampaignRunning:
		return "en marcha"
	case model.CampaignBraked:
		return "frenada"
	case model.CampaignDone:
		return "terminada"
	case model.CampaignCanceled:
		return "cancelada"
	}
	return string(s)
}

// mediaDirOf: carpeta del adjunto de una campaña.
func mediaDirOf(id string) string { return filepath.Join(paths.CampaignMediaDir(), id) }

func isInside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// storeAttachment copia el adjunto a la carpeta de la campaña (el original puede borrarse o
// cambiar) y quita la copia anterior si ya no se usa.
func (c *Campaigns) storeAttachment(cp *model.Campaign, prevPath string) error {
	dir := mediaDirOf(cp.ID)
	if cp.FilePath == "" {
		if prevPath != "" && isInside(prevPath, dir) {
			_ = os.Remove(prevPath)
		}
		return nil
	}
	fi, err := os.Stat(cp.FilePath)
	if err != nil || fi.IsDir() {
		return msgInvalid("no se encontró el archivo adjunto")
	}
	if isInside(cp.FilePath, dir) {
		return nil // ya es la copia de esta campaña
	}
	dst, err := copyFileInto(cp.FilePath, dir, filepath.Base(cp.FilePath))
	if err != nil {
		return fmt.Errorf("campañas: copiar el adjunto: %w", err)
	}
	if prevPath != "" && prevPath != dst && isInside(prevPath, dir) {
		_ = os.Remove(prevPath)
	}
	cp.FilePath = dst
	return nil
}

// copyFileInto copia src a dir/name (crea la carpeta; escribe un .part y lo renombra).
func copyFileInto(src, dir, name string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

// ---------------------------------------------------------------------------------------------
// Estados: pausar, reanudar, cancelar, borrar

// transition cambia el estado de una campaña si el actual es uno de `from`.
func (c *Campaigns) transition(ctx context.Context, id string, from []model.CampaignState, to model.CampaignState, action string, brake string) (model.Campaign, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp, err := c.app.GetCampaign(ctx, id)
	if err != nil {
		return model.Campaign{}, err
	}
	ok := false
	for _, s := range from {
		ok = ok || cp.State == s
	}
	if !ok {
		return model.Campaign{}, msgInvalid("no se puede %s una campaña %s", action, stateLabel(cp.State))
	}
	cp.State, cp.BrakeReason, cp.UpdatedAt = to, brake, c.nowMs()
	if err := c.app.SaveCampaign(ctx, cp); err != nil {
		return model.Campaign{}, err
	}
	c.decorate(ctx, &cp)
	c.emit(cp, "", 0)
	return cp, nil
}

func (c *Campaigns) Pause(ctx context.Context, id string) error {
	_, err := c.transition(ctx, id, []model.CampaignState{model.CampaignRunning, model.CampaignReady}, model.CampaignPaused, "pausar", "")
	return err
}

// Resume reanuda una campaña pausada o frenada (y quita el freno: el chequeo de salud vuelve a
// empezar desde este momento).
func (c *Campaigns) Resume(ctx context.Context, id string) error {
	cp, err := c.transition(ctx, id, []model.CampaignState{model.CampaignPaused, model.CampaignBraked}, model.CampaignRunning, "reanudar", "")
	if err != nil {
		return err
	}
	c.resetBrakeBase(ctx, cp)
	c.kick()
	return nil
}

func (c *Campaigns) Cancel(ctx context.Context, id string) error {
	_, err := c.transition(ctx, id,
		[]model.CampaignState{model.CampaignDraft, model.CampaignReady, model.CampaignRunning, model.CampaignPaused, model.CampaignBraked},
		model.CampaignCanceled, "cancelar", "")
	return err
}

// Delete borra la campaña, su cola y su adjunto. Una campaña en marcha no se borra: primero hay
// que pausarla o cancelarla.
func (c *Campaigns) Delete(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp, err := c.app.GetCampaign(ctx, id)
	if err != nil {
		return err
	}
	if cp.State == model.CampaignRunning {
		return msgInvalid("pausa o cancela la campaña antes de borrarla")
	}
	if err := c.app.DeleteCampaign(ctx, id); err != nil {
		return err
	}
	_ = os.RemoveAll(mediaDirOf(id))
	c.smu.Lock()
	delete(c.stats, id)
	c.smu.Unlock()
	c.pmu.Lock()
	delete(c.lastProg, id)
	c.pmu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------------------------
// Destinatarios y revisión

// campaignTexts: el texto principal (A) y las variantes B…E no vacías, en ese orden.
func campaignTexts(cp model.Campaign) []string {
	texts := []string{}
	if strings.TrimSpace(cp.Text) != "" {
		texts = append(texts, cp.Text)
	}
	for _, v := range cp.Variants {
		if strings.TrimSpace(v) != "" {
			texts = append(texts, v)
		}
	}
	if len(texts) > campaign.MaxVariants {
		texts = texts[:campaign.MaxVariants]
	}
	return texts
}

type recipient struct {
	contact model.Contact
	peer    string // id nativo del chat directo en la cuenta de la campaña
	risk    campaign.Risk
}

type skippedRecipient struct {
	contact model.Contact
	peer    string
	kind    string // optedOut | noIdentity | frequency
	reason  string
}

type launchPlan struct {
	acc        model.Account
	accErr     error // la cuenta no sirve para lanzar (no existe, no es WhatsApp/Telegram o no está cargada)
	pacing     model.PacingSettings
	texts      []string
	recipients []recipient
	skips      []skippedRecipient
}

// members: contactos únicos de las listas y de los contactos sueltos.
func (c *Campaigns) members(ctx context.Context, cp model.Campaign) ([]model.Contact, error) {
	seen := map[string]bool{}
	out := []model.Contact{}
	if len(cp.ListIDs) > 0 {
		ms, err := c.app.ListMembers(ctx, cp.ListIDs)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if !seen[m.ID] {
				seen[m.ID] = true
				out = append(out, m)
			}
		}
	}
	for _, id := range cp.ContactIDs {
		if seen[id] {
			continue
		}
		ct, err := c.app.GetContact(ctx, id)
		if errors.Is(err, port.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		seen[id] = true
		out = append(out, ct)
	}
	return out, nil
}

// sentCounts: cuántas campañas (distintas de `except`) recibió cada contacto desde `since`.
func (c *Campaigns) sentCounts(ctx context.Context, except string, since int64) (map[string]int, error) {
	cs, err := c.app.ListCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, o := range cs {
		if o.ID == except {
			continue
		}
		items, err := c.app.ListQueue(ctx, o.ID, []string{"sent"}, 0)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.SentAt >= since {
				counts[it.ContactID]++
			}
		}
	}
	return counts, nil
}

// riskOf: warm si hay algún mensaje entrante suyo en el chat directo; lukewarm si solo hay
// mensajes míos; cold si no hay chat (o está vacío).
func riskOf(ctx context.Context, st port.AccountStore, peer string) campaign.Risk {
	if _, err := st.GetChat(ctx, peer); err != nil {
		return campaign.RiskCold
	}
	page, err := st.ListMessages(ctx, peer, 0, maxRiskMessages)
	if err != nil || len(page.Messages) == 0 {
		return campaign.RiskCold
	}
	for _, m := range page.Messages {
		if !m.FromMe {
			return campaign.RiskWarm
		}
	}
	return campaign.RiskLukewarm
}

func identityIn(ct model.Contact, acc model.AccountID) (model.Identity, bool) {
	for _, id := range ct.Identities {
		if id.AccountID == acc && id.PeerNativeID != "" {
			return id, true
		}
	}
	return model.Identity{}, false
}

// buildPlan reúne destinatarios (con su riesgo) y los saltados. Los errores de cuenta se
// devuelven aparte para que la revisión los muestre en rojo en vez de fallar.
func (c *Campaigns) buildPlan(ctx context.Context, cp model.Campaign) (*launchPlan, error) {
	plan := &launchPlan{pacing: c.effectivePacing(ctx, cp), texts: campaignTexts(cp)}
	acc, accErr := c.accountFor(ctx, cp.AccountID)
	if accErr != nil {
		plan.accErr = accErr
		return plan, nil
	}
	plan.acc = acc
	store, err := c.acc.Store(cp.AccountID)
	if err != nil {
		plan.accErr = fmt.Errorf("%w: la cuenta no está cargada", port.ErrNotFound)
		return plan, nil
	}
	contacts, err := c.members(ctx, cp)
	if err != nil {
		return nil, err
	}
	sort.Slice(contacts, func(i, j int) bool { return contacts[i].ID < contacts[j].ID })

	var recent map[string]int
	if p := plan.pacing; p.FreqMax > 0 && p.FreqDays > 0 {
		since := c.now().Add(-time.Duration(p.FreqDays) * 24 * time.Hour).UnixMilli()
		if recent, err = c.sentCounts(ctx, cp.ID, since); err != nil {
			return nil, err
		}
	}
	for _, ct := range contacts {
		ident, hasIdent := identityIn(ct, cp.AccountID)
		switch {
		case ct.OptedOut:
			plan.skips = append(plan.skips, skippedRecipient{ct, ident.PeerNativeID, "optedOut", "Pidió no recibir más mensajes"})
		case !hasIdent:
			plan.skips = append(plan.skips, skippedRecipient{ct, "", "noIdentity", "No tiene datos en la cuenta de la campaña"})
		case recent != nil && recent[ct.ID] >= plan.pacing.FreqMax:
			plan.skips = append(plan.skips, skippedRecipient{ct, ident.PeerNativeID, "frequency",
				fmt.Sprintf("Ya recibió %d campañas en los últimos %d días", recent[ct.ID], plan.pacing.FreqDays)})
		default:
			plan.recipients = append(plan.recipients, recipient{ct, ident.PeerNativeID, riskOf(ctx, store, ident.PeerNativeID)})
		}
	}
	return plan, nil
}

// otherTexts: textos de campañas recientes (para avisar si el nuevo se les parece mucho).
func (c *Campaigns) otherTexts(ctx context.Context, except string) []string {
	cs, err := c.app.ListCampaigns(ctx)
	if err != nil {
		return nil
	}
	since := c.now().Add(-recentCampaignDays * 24 * time.Hour).UnixMilli()
	out := []string{}
	for _, o := range cs {
		if o.ID == except || o.CreatedAt < since {
			continue
		}
		out = append(out, campaignTexts(o)...)
	}
	return out
}

// Review revisa una campaña (guardada o no) antes de lanzarla: destinatarios, riesgo, mensaje,
// variantes y ritmo. Level «red» impide lanzar.
func (c *Campaigns) Review(ctx context.Context, cp model.Campaign) (CampaignReview, error) {
	rev, _, err := c.review(ctx, cp)
	return rev, err
}

func (c *Campaigns) review(ctx context.Context, cp model.Campaign) (CampaignReview, *launchPlan, error) {
	rev := CampaignReview{
		Risks:    map[string]int{string(campaign.RiskWarm): 0, string(campaign.RiskLukewarm): 0, string(campaign.RiskCold): 0},
		Items:    []campaign.ReviewItem{},
		Warnings: []string{},
		Skipped:  map[string]int{"optedOut": 0, "noIdentity": 0, "frequency": 0},
	}
	plan, err := c.buildPlan(ctx, cp)
	if err != nil {
		return rev, nil, err
	}
	accErr := plan.accErr
	add := func(level, code, msg string) {
		rev.Items = append(rev.Items, campaign.ReviewItem{Level: level, Code: code, Message: msg})
	}

	switch {
	case accErr == nil:
	case errors.Is(accErr, port.ErrUnsupported):
		add(campaign.LevelRed, "account", "Las campañas solo se pueden enviar por cuentas de WhatsApp o Telegram.")
	case errors.Is(accErr, port.ErrNotFound):
		add(campaign.LevelRed, "account", "La cuenta de la campaña no existe o no está cargada.")
	default:
		add(campaign.LevelRed, "account", "Elige la cuenta desde la que se enviará la campaña.")
	}

	texts := plan.texts
	if len(texts) == 0 && cp.FilePath == "" {
		add(campaign.LevelRed, "empty", "El mensaje está vacío: escribe un texto o adjunta un archivo.")
	}
	others := c.otherTexts(ctx, cp.ID)
	for i, t := range texts {
		for _, it := range campaign.ReviewMessage(t, others) {
			if len(texts) > 1 {
				it.Message = fmt.Sprintf("Mensaje %s: %s", campaign.VariantLetters[i], it.Message)
			}
			rev.Items = append(rev.Items, it)
		}
	}
	if len(texts) > 1 {
		if m := campaign.MaxSimilarity(texts); m.Pair != nil {
			if level := campaign.SimilarityLevel(m.Value); level != campaign.LevelGreen {
				add(level, "similar", fmt.Sprintf("Los mensajes %s y %s se parecen un %d %%: cámbialos más para que no sean casi iguales.",
					campaign.VariantLetters[m.Pair[0]], campaign.VariantLetters[m.Pair[1]], m.Value))
			}
		}
	}

	rev.Recipients = len(plan.recipients)
	counts := map[campaign.Risk]int{}
	for _, r := range plan.recipients {
		counts[r.risk]++
	}
	for _, k := range campaign.RiskOrder {
		rev.Risks[string(k)] = counts[k]
	}
	for _, s := range plan.skips {
		rev.Skipped[s.kind]++
	}
	if accErr == nil && rev.Recipients == 0 {
		add(campaign.LevelRed, "recipients", "No hay destinatarios: ninguna persona de la selección puede recibir la campaña.")
	}
	rev.Required = campaign.RequiredVariants(rev.Recipients)
	if rev.Required > 0 && campaign.DistinctTexts(texts) < rev.Required {
		add(campaign.LevelRed, "variants", fmt.Sprintf(
			"Con %d destinatarios hacen falta al menos %d mensajes distintos (variantes u opciones {a|b}); tienes %d.",
			rev.Recipients, rev.Required, campaign.DistinctTexts(texts)))
	}

	avg := 0
	if len(texts) > 0 {
		sum := 0
		for _, t := range texts {
			sum += utf8.RuneCountInString(t)
		}
		avg = sum / len(texts)
	}
	sending := campaign.SendingFromPacing(plan.pacing)
	rev.Estimate = campaign.EstimateOutreach(counts, avg, sending)
	rev.Warnings = campaign.OutreachWarnings(counts, sending)
	rev.Level = campaign.WorstLevel(rev.Items)
	return rev, plan, nil
}

// ---------------------------------------------------------------------------------------------
// Lanzar

// footerText: el pie de baja (ajuste optout_footer_text).
func (c *Campaigns) footerText(ctx context.Context) string {
	v, err := c.app.GetSetting(ctx, SettingOptOutFooter)
	if err != nil || strings.TrimSpace(v) == "" {
		return defaultsOptOutFooter
	}
	return strings.TrimSpace(v)
}

// personalize rellena la plantilla de una variante para un contacto, resuelve sus opciones
// {a|b} con un generador sembrado (campaña + contacto) y añade el pie de baja.
func personalize(campaignID, template string, ct model.Contact, footer string) string {
	tc := campaign.TemplateContact{FullName: ct.Name, PhoneNumber: ct.Phone}
	text := campaign.Spin(campaign.RenderTemplate(template, tc, nil).Text, campaign.SeededRandom(campaignID+"|"+ct.ID))
	if footer != "" {
		if text != "" {
			text += "\n\n"
		}
		text += footer
	}
	return text
}

// Launch genera la cola de la campaña (06 §8, los 6 pasos) y la deja en ready (si StartAt es
// futuro) o running. Solo desde draft o ready; una revisión en rojo impide lanzar.
func (c *Campaigns) Launch(ctx context.Context, id string) error {
	c.mu.Lock()
	if c.launching[id] {
		c.mu.Unlock()
		return msgInvalid("la campaña ya se está lanzando")
	}
	c.launching[id] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.launching, id)
		c.mu.Unlock()
	}()

	cp, err := c.app.GetCampaign(ctx, id)
	if err != nil {
		return err
	}
	if cp.State != model.CampaignDraft && cp.State != model.CampaignReady {
		return msgInvalid("no se puede lanzar una campaña %s", stateLabel(cp.State))
	}
	rev, plan, err := c.review(ctx, cp)
	if err != nil {
		return err
	}
	if rev.Level == campaign.LevelRed {
		why := "la revisión tiene problemas graves"
		for _, it := range rev.Items {
			if it.Level == campaign.LevelRed {
				why = it.Message
				break
			}
		}
		return msgInvalid("no se puede lanzar: %s", why)
	}

	items := c.buildQueue(ctx, cp, plan)

	c.mu.Lock()
	defer c.mu.Unlock()
	cur, err := c.app.GetCampaign(ctx, id) // pudo cambiar mientras se generaba la cola
	if err != nil {
		return err
	}
	if cur.State != model.CampaignDraft && cur.State != model.CampaignReady {
		return msgInvalid("la campaña cambió de estado mientras se lanzaba")
	}
	if err := c.app.ReplaceQueue(ctx, id, items); err != nil {
		return err
	}
	cur.State, cur.BrakeReason, cur.UpdatedAt = model.CampaignRunning, "", c.nowMs()
	if cur.StartAt > cur.UpdatedAt {
		cur.State = model.CampaignReady
	}
	if cur.Pacing == (model.PacingSettings{}) {
		cur.Pacing = plan.pacing
	}
	if err := c.app.SaveCampaign(ctx, cur); err != nil {
		return err
	}
	c.dropStats(id)
	out, err := c.app.GetCampaign(ctx, id)
	if err != nil {
		return err
	}
	c.decorate(ctx, &out)
	c.emit(out, "", 0)
	c.kick()
	return nil
}

// buildQueue ordena y personaliza a los destinatarios: variantes repartidas (semilla = id de la
// campaña), muestra A/B si está activo, orden warm → lukewarm → cold y dentro de cada uno
// aleatorio sembrado; al final los saltados con su motivo.
func (c *Campaigns) buildQueue(ctx context.Context, cp model.Campaign, plan *launchPlan) []model.QueueItem {
	texts := plan.texts
	if len(texts) == 0 {
		texts = []string{""} // solo adjunto
	}
	ids := make([]string, len(plan.recipients))
	byID := map[string]recipient{}
	for i, r := range plan.recipients {
		ids[i] = r.contact.ID
		byID[r.contact.ID] = r
	}
	assigned := campaign.AssignVariants(ids, len(texts), cp.ID)
	variantOf := make(map[string]int, len(assigned))
	for _, a := range assigned {
		variantOf[a.ID] = a.Variant
	}
	var sample map[string]bool
	if cp.ABEnabled && len(texts) >= 2 {
		sample = campaign.ABSample(assigned, campaign.ABDefaults.SamplePct)
	}
	footer := ""
	if cp.OptOutFooter {
		footer = c.footerText(ctx)
	}

	// Orden: riesgo y, dentro de cada uno, un número sembrado por destinatario.
	rnd := campaign.SeededRandom(cp.ID + "|order")
	keys := make(map[string]float64, len(ids))
	for _, id := range ids { // ids ya están ordenados por id de contacto
		keys[id] = rnd()
	}
	ordered := append([]string(nil), ids...)
	sort.SliceStable(ordered, func(i, j int) bool {
		ri, rj := campaign.RiskRank(byID[ordered[i]].risk), campaign.RiskRank(byID[ordered[j]].risk)
		if ri != rj {
			return ri < rj
		}
		return keys[ordered[i]] < keys[ordered[j]]
	})

	items := make([]model.QueueItem, 0, len(ordered)+len(plan.skips))
	for _, id := range ordered {
		r := byID[id]
		v := variantOf[id]
		state := "pending"
		if sample != nil && !sample[id] {
			state = "held"
		}
		items = append(items, model.QueueItem{
			ID: model.NewID(), CampaignID: cp.ID, ContactID: id, ChatNativeID: r.peer, Risk: string(r.risk),
			Variant: v, ABSample: sample[id], Text: personalize(cp.ID, texts[v], r.contact, footer), State: state,
		})
	}
	for _, s := range plan.skips {
		items = append(items, model.QueueItem{
			ID: model.NewID(), CampaignID: cp.ID, ContactID: s.contact.ID, ChatNativeID: s.peer,
			State: "skipped", Error: s.reason,
		})
	}
	return items
}
