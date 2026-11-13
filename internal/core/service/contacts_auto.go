package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Contactos automáticos: cada persona de chat directo de cualquier canal tiene su contacto en la
// lista de Contactos aunque nunca se le haya escrito. Se crean solos (el Router avisa al guardar
// personas y chats, y SyncAll rellena lo que ya estaba guardado), con `Source == "auto"`.
//
// Reglas:
//   - Nombre: libreta > nombre que la persona se puso > título del chat > usuario > teléfono.
//   - Si ya hay un contacto con ese teléfono (otra cuenta u otro canal) se le añade la identidad
//     en lugar de crear otro.
//   - Un contacto manual o importado nunca cambia de nombre: solo se le completa la identidad que
//     falte. En uno automático el nombre se refresca mientras sea el que puso el sistema.
//   - Nunca se toca «no recibe campañas»; etiquetas, notas y listas se conservan.
//   - Un contacto automático que el usuario borra no vuelve a crearse (se recuerda su identidad).

const (
	// settingAutoSkip: ajuste con las identidades («cuenta|id nativo») de contactos automáticos que
	// el usuario borró a mano; la sincronización no los vuelve a crear.
	settingAutoSkip = "contacts_auto_skip"
	// autoSkipMax: tope de identidades recordadas (las más antiguas se olvidan primero).
	autoSkipMax = 5000
	// syncChatsLimit: tope al leer los chats de una cuenta para sincronizar.
	syncChatsLimit = 1_000_000
)

// ContactSync: lo implementa Contacts; el Router lo llama tras guardar personas y chats de una
// cuenta (ver Router.SetContactSync). Devuelve cuántos contactos se crearon.
type ContactSync interface {
	SyncAccount(ctx context.Context, acc model.Account, st port.AccountStore, persons []model.Person, chats []model.Chat) (int, error)
}

// contactBatchSaver: el almacén puede guardar varios contactos en una transacción. Es opcional
// (no está en port.AppStore): sin él se guardan de uno en uno.
type contactBatchSaver interface {
	SaveContacts(ctx context.Context, cs []model.Contact) error
}

// autoContactCleaner: el almacén puede borrar de una vez los contactos automáticos sin identidades.
// Es opcional: sin él se recorren los contactos.
type autoContactCleaner interface {
	DeleteOrphanAutoContacts(ctx context.Context) (int, error)
}

// cleanOrphanAutoContacts borra los contactos automáticos que se quedaron sin identidades (los
// manuales y los importados se conservan) y devuelve cuántos.
func cleanOrphanAutoContacts(ctx context.Context, app port.AppStore) (int, error) {
	if cl, ok := app.(autoContactCleaner); ok {
		return cl.DeleteOrphanAutoContacts(ctx)
	}
	var orphans []string
	for offset := 0; ; offset += contactsPage {
		page, total, err := app.ListContacts(ctx, model.ContactFilter{Offset: offset, Limit: contactsPage})
		if err != nil {
			return 0, err
		}
		for _, ct := range page {
			if ct.Source == model.ContactSourceAuto && len(ct.Identities) == 0 {
				orphans = append(orphans, ct.ID)
			}
		}
		if len(page) == 0 || offset+len(page) >= total {
			break
		}
	}
	for _, id := range orphans {
		if err := app.DeleteContact(ctx, id); err != nil && !errors.Is(err, port.ErrNotFound) {
			return 0, err
		}
	}
	return len(orphans), nil
}

// strongerSource devuelve el origen que más pesa al unir dos contactos: manual > import > auto.
func strongerSource(a, b string) string {
	rank := func(s string) int {
		switch s {
		case model.ContactSourceAuto:
			return 0
		case model.ContactSourceImport:
			return 1
		}
		return 2 // manual (y vacío)
	}
	if rank(b) > rank(a) {
		return b
	}
	if a == "" {
		return model.ContactSourceManual
	}
	return a
}

// ---------------------------------------------------------------------------------------------
// Identidades que el usuario borró

func (c *Contacts) loadAutoSkip(ctx context.Context) []string {
	raw, err := c.app.GetSetting(ctx, settingAutoSkip)
	if err != nil || raw == "" {
		return nil
	}
	var keys []string
	if json.Unmarshal([]byte(raw), &keys) != nil {
		return nil
	}
	return keys
}

// rememberDeletedAuto recuerda las identidades de un contacto automático borrado a mano. Quien
// llama tiene `mu`. Un fallo solo se registra: borrar el contacto es lo importante.
func (c *Contacts) rememberDeletedAuto(ctx context.Context, ct model.Contact) {
	if ct.Source != model.ContactSourceAuto || len(ct.Identities) == 0 {
		return
	}
	keys := c.loadAutoSkip(ctx)
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		seen[k] = true
	}
	for _, it := range ct.Identities {
		if k := identityKey(it); !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	if len(keys) > autoSkipMax {
		keys = keys[len(keys)-autoSkipMax:]
	}
	b, err := json.Marshal(keys)
	if err != nil {
		return
	}
	if err := c.app.SetSetting(ctx, settingAutoSkip, string(b)); err != nil {
		slog.Warn("no se pudo recordar el contacto automático borrado", "err", err)
	}
}

// ---------------------------------------------------------------------------------------------
// Candidatos

// autoCand: lo que se sabe de una persona de chat directo para crear o completar su contacto.
type autoCand struct {
	native string // id nativo de la otra persona
	name   string // nombre «de verdad» (libreta, apodo, título del chat, usuario); "" si no hay
	phone  string // E.164 o ""
}

// nonPersonID: ids nativos que nunca son una persona (grupos, canales y estados de WhatsApp).
func nonPersonID(native string) bool {
	return strings.HasSuffix(native, "@g.us") || strings.HasSuffix(native, "@newsletter") ||
		strings.HasSuffix(native, "@broadcast")
}

// looksLikePhone: un título que es solo un número («+593 99 123 4567»).
func looksLikePhone(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9') && !strings.ContainsRune("+-()  ", r) {
			return false
		}
	}
	return true
}

type peerInfo struct {
	person *model.Person
	chat   *model.Chat
}

// buildCandidates arma los candidatos de una cuenta a partir de personas y chats. Califica una
// persona con nombre de libreta o con chat directo (los desconocidos de un grupo, no). Con `lookup`
// completa lo que falte consultando el almacén de la cuenta (para eventos sueltos); sin él se fía de
// lo recibido (SyncAll ya trae todas las personas y todos los chats).
func buildCandidates(ctx context.Context, acc model.Account, st port.AccountStore, persons []model.Person, chats []model.Chat, lookup bool) []autoCand {
	infos := map[string]*peerInfo{}
	var order []string
	get := func(native string) *peerInfo {
		in := infos[native]
		if in == nil {
			in = &peerInfo{}
			infos[native] = in
			order = append(order, native)
		}
		return in
	}
	for i := range persons {
		p := persons[i]
		if p.NativeID == "" || p.NativeID == acc.SelfID || nonPersonID(p.NativeID) {
			continue
		}
		get(p.NativeID).person = &p
	}
	for i := range chats {
		ch := chats[i]
		if ch.Kind != model.ChatDirect || ch.NativeID == "" || ch.ContactID != "" {
			continue // los demás tipos no son personas; con ContactID, el Router ya lo resolvió
		}
		peer := ch.PeerNativeID
		if peer == "" {
			peer = ch.NativeID
		}
		if peer == acc.SelfID || nonPersonID(peer) {
			continue
		}
		in := get(peer)
		if in.chat == nil {
			in.chat = &ch
		}
	}

	out := make([]autoCand, 0, len(order))
	for _, native := range order {
		in := infos[native]
		var p model.Person
		if in.person != nil {
			p = *in.person
		}
		if lookup && st != nil && (p.Name == "" || p.Phone == "") {
			if stored, err := st.GetPerson(ctx, native); err == nil {
				if p.Name == "" {
					p.Name = stored.Name
				}
				if p.PushName == "" {
					p.PushName = stored.PushName
				}
				if p.Phone == "" {
					p.Phone = stored.Phone
				}
				if p.Username == "" {
					p.Username = stored.Username
				}
			}
		}
		if in.chat == nil && lookup && st != nil && p.Name == "" {
			if ch, err := st.GetChat(ctx, native); err == nil && ch.Kind == model.ChatDirect {
				in.chat = &ch
			}
		}
		// Sin nombre de libreta ni chat directo: alguien que solo habló en un grupo.
		if p.Name == "" && in.chat == nil {
			continue
		}
		cd := autoCand{native: native, phone: NormalizePhoneE164(p.Phone)}
		title := ""
		if in.chat != nil {
			title = strings.TrimSpace(in.chat.Title)
			if looksLikePhone(title) {
				if cd.phone == "" {
					cd.phone = NormalizePhoneE164(title)
				}
				title = ""
			}
		}
		username := ""
		if p.Username != "" {
			username = "@" + strings.TrimPrefix(p.Username, "@")
		}
		for _, n := range []string{p.Name, p.PushName, title, username} {
			if n = strings.TrimSpace(n); n != "" {
				cd.name = n
				break
			}
		}
		if cd.name == "" && cd.phone == "" {
			continue // nada que mostrar
		}
		out = append(out, cd)
	}
	return out
}

// ---------------------------------------------------------------------------------------------
// Índice de contactos en memoria

// contactIndex: contactos cargados para decidir sin una consulta por persona. Con `full` están
// todos cargados (SyncAll); sin él se consulta la base la primera vez que se pregunta por una
// identidad o un teléfono. Los cambios se guardan juntos al final (flush).
type contactIndex struct {
	c           *Contacts
	full        bool
	byID        map[string]*model.Contact
	byIdent     map[string]string   // «cuenta|id nativo» -> id de contacto
	byPhone     map[string][]string // teléfono -> ids de contacto
	missed      map[string]bool     // identidades que se sabe que no existen (modo lazy)
	phoneLoaded map[string]bool
	dirty       map[string]bool
	fresh       map[string]bool // creados en este lote
	order       []string        // ids modificados, en orden
	skip        map[string]bool // identidades de contactos automáticos borrados a mano
	skipLoaded  bool
}

func (c *Contacts) newIndex() *contactIndex {
	return &contactIndex{
		c: c, byID: map[string]*model.Contact{}, byIdent: map[string]string{}, byPhone: map[string][]string{},
		missed: map[string]bool{}, phoneLoaded: map[string]bool{}, dirty: map[string]bool{}, fresh: map[string]bool{},
	}
}

// loadAll carga todos los contactos (por páginas).
func (ix *contactIndex) loadAll(ctx context.Context) error {
	for offset := 0; ; offset += contactsPage {
		page, total, err := ix.c.app.ListContacts(ctx, model.ContactFilter{Offset: offset, Limit: contactsPage})
		if err != nil {
			return err
		}
		for _, ct := range page {
			ix.add(ct)
		}
		if len(page) == 0 || offset+len(page) >= total {
			break
		}
	}
	ix.full = true
	return nil
}

// add mete un contacto en el índice (si ya estaba, devuelve el que había).
func (ix *contactIndex) add(ct model.Contact) *model.Contact {
	if cur, ok := ix.byID[ct.ID]; ok {
		return cur
	}
	p := &ct
	ix.byID[ct.ID] = p
	for _, it := range ct.Identities {
		ix.indexIdentity(p, it)
	}
	ix.indexPhone(p, ct.Phone)
	return p
}

func (ix *contactIndex) indexIdentity(ct *model.Contact, it model.Identity) {
	ix.byIdent[identityKey(it)] = ct.ID
	delete(ix.missed, identityKey(it))
	ix.indexPhone(ct, it.Phone)
}

func (ix *contactIndex) indexPhone(ct *model.Contact, phone string) {
	if phone == "" {
		return
	}
	for _, id := range ix.byPhone[phone] {
		if id == ct.ID {
			return
		}
	}
	ix.byPhone[phone] = append(ix.byPhone[phone], ct.ID)
}

func (ix *contactIndex) findByIdentity(ctx context.Context, acc model.AccountID, native string) (*model.Contact, error) {
	key := string(acc) + "|" + native
	if id, ok := ix.byIdent[key]; ok {
		return ix.byID[id], nil
	}
	if ix.full || ix.missed[key] {
		return nil, nil
	}
	ct, err := ix.c.app.FindContactByIdentity(ctx, acc, native)
	if errors.Is(err, port.ErrNotFound) {
		ix.missed[key] = true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := ix.add(ct)
	ix.byIdent[key] = p.ID
	return p, nil
}

// findByPhone devuelve el contacto más antiguo que tenga ese teléfono (propio o de una identidad).
func (ix *contactIndex) findByPhone(ctx context.Context, phone string) (*model.Contact, error) {
	if phone == "" {
		return nil, nil
	}
	if !ix.full && !ix.phoneLoaded[phone] {
		found, err := ix.c.app.FindContactsByPhone(ctx, phone)
		if err != nil {
			return nil, err
		}
		for _, ct := range found {
			p := ix.add(ct)
			ix.indexPhone(p, phone)
		}
		ix.phoneLoaded[phone] = true
	}
	var best *model.Contact
	for _, id := range ix.byPhone[phone] {
		ct := ix.byID[id]
		if ct == nil {
			continue
		}
		if best == nil || ct.CreatedAt < best.CreatedAt || (ct.CreatedAt == best.CreatedAt && ct.ID < best.ID) {
			best = ct
		}
	}
	return best, nil
}

func (ix *contactIndex) markDirty(ct *model.Contact) {
	if !ix.dirty[ct.ID] {
		ix.dirty[ct.ID] = true
		ix.order = append(ix.order, ct.ID)
	}
}

func (ix *contactIndex) isSkipped(ctx context.Context, key string) bool {
	if !ix.skipLoaded {
		ix.skip = map[string]bool{}
		for _, k := range ix.c.loadAutoSkip(ctx) {
			ix.skip[k] = true
		}
		ix.skipLoaded = true
	}
	return ix.skip[key]
}

// flush guarda los contactos creados o cambiados, juntos si el almacén lo permite. A los que ya
// existían se les vuelven a leer etiquetas, notas, listas y «no recibe campañas» justo antes de
// guardar: la sincronización nunca los cambia y, si el usuario los tocó mientras tanto, no se pisan.
func (ix *contactIndex) flush(ctx context.Context) error {
	if len(ix.order) == 0 {
		return nil
	}
	now := ix.c.now().UnixMilli()
	out := make([]model.Contact, 0, len(ix.order))
	for _, id := range ix.order {
		ct := *ix.byID[id]
		if !ix.fresh[id] {
			cur, err := ix.c.app.GetContact(ctx, id)
			if errors.Is(err, port.ErrNotFound) {
				continue // lo borraron mientras tanto: no se resucita
			}
			if err != nil {
				return err
			}
			ct.ListIDs, ct.Tags, ct.Notes, ct.OptedOut = cur.ListIDs, cur.Tags, cur.Notes, cur.OptedOut
		}
		ct.UpdatedAt = now
		out = append(out, ct)
	}
	ix.dirty, ix.order = map[string]bool{}, nil
	if bs, ok := ix.c.app.(contactBatchSaver); ok {
		return bs.SaveContacts(ctx, out)
	}
	for _, ct := range out {
		if err := ix.c.app.SaveContact(ctx, ct); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Aplicar candidatos

// applyCandidates crea o completa el contacto de cada candidato. Quien llama tiene `mu`. Devuelve
// cuántos contactos nuevos se crearon.
func (c *Contacts) applyCandidates(ctx context.Context, ix *contactIndex, acc model.Account, cands []autoCand) (int, error) {
	created := 0
	for _, cd := range cands {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		ct, err := ix.findByIdentity(ctx, acc.ID, cd.native)
		if err != nil {
			return created, err
		}
		if ct != nil {
			if refreshAutoIdentity(ct, acc.ID, cd) {
				ix.markDirty(ct)
			}
			continue
		}
		if ix.isSkipped(ctx, string(acc.ID)+"|"+cd.native) {
			continue
		}
		id := model.Identity{
			AccountID: acc.ID, Channel: acc.Channel, PeerNativeID: cd.native, DisplayName: cd.name, Phone: cd.phone,
		}
		other, err := ix.findByPhone(ctx, cd.phone)
		if err != nil {
			return created, err
		}
		if other != nil {
			phoneOnly := other.Name == other.Phone
			other.Identities = append(other.Identities, id)
			ix.indexIdentity(other, id)
			if other.Phone == "" {
				other.Phone = cd.phone
			}
			switch {
			case cd.name != "" && (other.Name == "" || (other.Source == model.ContactSourceAuto && phoneOnly)):
				other.Name = cd.name
			case other.Name == "":
				other.Name = cd.phone
			}
			ix.markDirty(other)
			continue
		}
		now := c.now().UnixMilli()
		name := cd.name
		if name == "" {
			name = cd.phone
		}
		ct = ix.add(model.Contact{
			ID: model.NewID(), Name: name, Phone: cd.phone, Tags: []string{}, ListIDs: []string{},
			Identities: []model.Identity{id}, Source: model.ContactSourceAuto, CreatedAt: now, UpdatedAt: now,
		})
		ix.fresh[ct.ID] = true
		ix.markDirty(ct)
		created++
	}
	return created, nil
}

// refreshAutoIdentity completa un contacto que ya tiene la identidad del candidato. En uno manual o
// importado nunca cambia el nombre ni pisa lo que ya hay: solo rellena lo que falta. En uno
// automático refresca el nombre si estaba vacío, era solo el teléfono o era el que esta misma
// identidad traía (el sistema lo puso; si el usuario lo hubiera editado, ya sería manual).
func refreshAutoIdentity(ct *model.Contact, acc model.AccountID, cd autoCand) bool {
	auto := ct.Source == model.ContactSourceAuto
	changed := false
	// La misma persona puede estar en dos cuentas con el mismo id nativo: se busca por cuenta e id.
	mine := func(it model.Identity) bool { return it.AccountID == acc && it.PeerNativeID == cd.native }
	prevName := ""
	for _, it := range ct.Identities {
		if mine(it) {
			prevName = it.DisplayName
			break
		}
	}
	if auto && cd.name != "" && ct.Name != cd.name && (ct.Name == "" || ct.Name == ct.Phone || ct.Name == prevName) {
		ct.Name, changed = cd.name, true
	}
	if ct.Phone == "" && cd.phone != "" {
		ct.Phone, changed = cd.phone, true
	}
	for i := range ct.Identities {
		it := &ct.Identities[i]
		if !mine(*it) {
			continue
		}
		if cd.name != "" && it.DisplayName != cd.name && (auto || it.DisplayName == "") {
			it.DisplayName, changed = cd.name, true
		}
		if cd.phone != "" && it.Phone != cd.phone && (auto || it.Phone == "") {
			it.Phone, changed = cd.phone, true
		}
	}
	return changed
}

// ---------------------------------------------------------------------------------------------
// Entradas públicas

// SyncAccount crea o completa los contactos de las personas y chats de una cuenta (lo llama el
// Router al guardarlos; ver ContactSync). Solo hace consultas por persona o chat, y guarda todo en
// pocas transacciones. Devuelve cuántos contactos nuevos se crearon.
func (c *Contacts) SyncAccount(ctx context.Context, acc model.Account, st port.AccountStore, persons []model.Person, chats []model.Chat) (int, error) {
	if acc.ID == "" {
		return 0, nil
	}
	cands := buildCandidates(ctx, acc, st, persons, chats, true)
	if len(cands) == 0 {
		return 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ix := c.newIndex()
	created, err := c.applyCandidates(ctx, ix, acc, cands)
	if err != nil {
		return created, err
	}
	if err := ix.flush(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

// SyncAll rellena los contactos con lo que ya está guardado en cada cuenta cargada: sus personas y
// sus chats directos. Es idempotente y barato cuando no hay nada nuevo (no escribe nada). Devuelve
// cuántos contactos nuevos se crearon. main.go lo llama al arrancar, tras Accounts.StartAll.
func (c *Contacts) SyncAll(ctx context.Context) (int, error) {
	if c.acc == nil {
		return 0, nil
	}
	type accData struct {
		acc   model.Account
		cands []autoCand
	}
	var data []accData
	for _, inst := range c.acc.snapshot() {
		inst.mu.Lock()
		acc := inst.acc
		inst.mu.Unlock()
		persons, err := inst.store.ListPersons(ctx, "", 0)
		if err != nil {
			slog.Warn("contactos: no se pudieron leer las personas de la cuenta", "accountId", string(acc.ID), "err", err)
			continue
		}
		var chats []model.Chat
		for _, tab := range []string{"all", "archived"} {
			cs, err := inst.store.ListChats(ctx, model.ChatFilter{Tab: tab, Limit: syncChatsLimit, IncludeEmpty: true})
			if err != nil {
				slog.Warn("contactos: no se pudieron leer los chats de la cuenta", "accountId", string(acc.ID), "err", err)
				break
			}
			chats = append(chats, cs...)
		}
		if cands := buildCandidates(ctx, acc, inst.store, persons, chats, false); len(cands) > 0 {
			data = append(data, accData{acc: acc, cands: cands})
		}
	}
	if len(data) == 0 {
		return 0, nil
	}
	sort.SliceStable(data, func(i, j int) bool { return data[i].acc.ID < data[j].acc.ID })

	c.mu.Lock()
	defer c.mu.Unlock()
	ix := c.newIndex()
	if err := ix.loadAll(ctx); err != nil {
		return 0, err
	}
	created := 0
	for _, d := range data {
		n, err := c.applyCandidates(ctx, ix, d.acc, d.cands)
		created += n
		if err != nil {
			return created, err
		}
	}
	if err := ix.flush(ctx); err != nil {
		return 0, err
	}
	return created, nil
}
