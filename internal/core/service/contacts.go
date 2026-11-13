package service

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// csvMaxBytes: tope del archivo CSV que se importa.
	csvMaxBytes = 50 << 20
	// contactsPage: tamaño de página al recorrer todos los contactos.
	contactsPage = 500
	// Largo válido de un teléfono E.164 (solo dígitos, sin el +).
	phoneMinDigits = 7
	phoneMaxDigits = 15
)

// Contacts: contactos unificados y listas (06 §7).
type Contacts struct {
	app port.AppStore
	acc *Accounts

	// mu serializa lo que lee y luego escribe (importar, unir, separar…): sin él, dos importaciones
	// a la vez podrían crear dos contactos para el mismo teléfono.
	mu  sync.Mutex
	now func() time.Time
}

func NewContacts(app port.AppStore, acc *Accounts) *Contacts {
	return &Contacts{app: app, acc: acc, now: time.Now}
}

func contactNotFound(what string) error { return fmt.Errorf("%w: %s", port.ErrNotFound, what) }

// ---------------------------------------------------------------------------------------------
// Teléfonos y datos

// NormalizePhoneE164 lleva un teléfono a E.164 con un normalizador simple: solo dígitos y un `+`
// delante. Si no empieza por `+` y tiene 10 dígitos que empiezan por `0`, se asume Ecuador
// (`+593` y el número sin el 0). Un prefijo `00` se toma como `+`. Devuelve "" si lo que queda
// no tiene entre 7 y 15 dígitos.
func NormalizePhoneE164(s string) string {
	s = strings.TrimSpace(s)
	plus := strings.HasPrefix(s, "+")
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if !plus {
		switch {
		case strings.HasPrefix(d, "00") && len(d) > 2:
			d = d[2:]
		case len(d) == 10 && d[0] == '0':
			d = "593" + d[1:]
		}
	}
	if len(d) < phoneMinDigits || len(d) > phoneMaxDigits {
		return ""
	}
	return "+" + d
}

func cleanTags(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		k := strings.ToLower(t)
		if t == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out
}

func unionTags(a, b []string) []string { return cleanTags(append(append([]string{}, a...), b...)) }

// sameIDs: los mismos identificadores (sin importar el orden).
func sameIDs(a, b []string) bool {
	a, b = unionStrings(a, nil), unionStrings(b, nil)
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return false
		}
	}
	return true
}

// sameTags: las mismas etiquetas (sin importar el orden ni las mayúsculas).
func sameTags(a, b []string) bool {
	a, b = cleanTags(a), cleanTags(b)
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, t := range a {
		set[strings.ToLower(t)] = true
	}
	for _, t := range b {
		if !set[strings.ToLower(t)] {
			return false
		}
	}
	return true
}

func identityKey(i model.Identity) string { return string(i.AccountID) + "|" + i.PeerNativeID }

// cleanIdentities: sin repetidas ni vacías, con el teléfono normalizado.
func cleanIdentities(in []model.Identity) []model.Identity {
	out := make([]model.Identity, 0, len(in))
	seen := map[string]bool{}
	for _, it := range in {
		if it.PeerNativeID == "" || it.AccountID == "" || seen[identityKey(it)] {
			continue
		}
		seen[identityKey(it)] = true
		it.Phone = NormalizePhoneE164(it.Phone)
		out = append(out, it)
	}
	return out
}

func unionStrings(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := map[string]bool{}
	for _, v := range append(append([]string{}, a...), b...) {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func appendNote(base, extra string) string {
	base, extra = strings.TrimSpace(base), strings.TrimSpace(extra)
	switch {
	case extra == "" || strings.Contains(base, extra):
		return base
	case base == "":
		return extra
	}
	return base + "\n\n" + extra
}

// normalizeContact deja el contacto listo para guardar (sin tocar identificador ni fechas).
func normalizeContact(ct model.Contact) (model.Contact, error) {
	ct.Name = strings.TrimSpace(ct.Name)
	if p := strings.TrimSpace(ct.Phone); p != "" {
		ct.Phone = NormalizePhoneE164(p)
		if ct.Phone == "" {
			return ct, msgInvalid("el teléfono %q no es válido", p)
		}
	} else {
		ct.Phone = ""
	}
	ct.Identities = cleanIdentities(ct.Identities)
	ct.Tags = cleanTags(ct.Tags)
	ct.ListIDs = unionStrings(ct.ListIDs, nil)
	ct.Notes = strings.TrimSpace(ct.Notes)
	if ct.Name == "" {
		ct.Name = ct.Phone
	}
	if ct.Name == "" {
		for _, it := range ct.Identities {
			if it.DisplayName != "" {
				ct.Name = it.DisplayName
				break
			}
		}
	}
	if ct.Name == "" && len(ct.Identities) == 0 {
		return ct, msgInvalid("el contacto necesita un nombre o un teléfono")
	}
	return ct, nil
}

// ---------------------------------------------------------------------------------------------
// Contactos

func (c *Contacts) List(ctx context.Context, f model.ContactFilter) ([]model.Contact, int, error) {
	out, total, err := c.app.ListContacts(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	if out == nil {
		out = []model.Contact{}
	}
	return out, total, nil
}

func (c *Contacts) Get(ctx context.Context, id string) (model.Contact, error) {
	if id == "" {
		return model.Contact{}, msgInvalid("falta el contacto")
	}
	return c.app.GetContact(ctx, id)
}

// Save crea (id vacío) o actualiza un contacto. Normaliza el teléfono a E.164 y limpia etiquetas,
// identidades y listas. Una identidad que pertenecía a otro contacto pasa a este.
func (c *Contacts) Save(ctx context.Context, ct model.Contact) (model.Contact, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ct, err := normalizeContact(ct)
	if err != nil {
		return model.Contact{}, err
	}
	now := c.now().UnixMilli()
	if ct.ID == "" {
		ct.ID, ct.CreatedAt = model.NewID(), now
		ct.Source = model.ContactSourceManual
	} else {
		cur, err := c.app.GetContact(ctx, ct.ID)
		if err != nil {
			return model.Contact{}, err
		}
		ct.CreatedAt = cur.CreatedAt
		// El origen lo manda el almacén, no quien llama. Un contacto automático que el usuario
		// edita (nombre, teléfono, etiquetas o notas) pasa a manual: ya es suyo y la
		// sincronización no vuelve a tocarlo ni se borra al quitar la cuenta.
		ct.Source = cur.Source
		if cur.Source == model.ContactSourceAuto &&
			(ct.Name != cur.Name || ct.Phone != cur.Phone || ct.Notes != cur.Notes ||
				!sameTags(ct.Tags, cur.Tags) || !sameIDs(ct.ListIDs, cur.ListIDs)) {
			ct.Source = model.ContactSourceManual
		}
	}
	ct.UpdatedAt = now
	if err := c.app.SaveContact(ctx, ct); err != nil {
		return model.Contact{}, err
	}
	return c.app.GetContact(ctx, ct.ID)
}

func (c *Contacts) Delete(ctx context.Context, id string) error {
	if id == "" {
		return msgInvalid("falta el contacto")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Un contacto automático borrado a mano no debe volver a crearse en la siguiente sincronización.
	if ct, err := c.app.GetContact(ctx, id); err == nil {
		c.rememberDeletedAuto(ctx, ct)
	}
	return c.app.DeleteContact(ctx, id)
}

// instanceOf devuelve la cuenta cargada (ErrNotFound si no existe).
func (c *Contacts) instanceOf(id model.AccountID) (*instance, error) {
	if c.acc == nil {
		return nil, contactNotFound("cuenta " + string(id))
	}
	return c.acc.mustGet(id)
}

// FromChat crea o devuelve el contacto del chat directo. Si el teléfono de la persona ya es de
// otro contacto, se le añade la identidad en lugar de crear uno nuevo.
func (c *Contacts) FromChat(ctx context.Context, key model.ChatKey) (model.Contact, error) {
	accID, native := key.Split()
	if accID == "" || native == "" {
		return model.Contact{}, msgInvalid("la clave del chat no es válida")
	}
	inst, err := c.instanceOf(accID)
	if err != nil {
		return model.Contact{}, err
	}
	chat, err := inst.store.GetChat(ctx, native)
	if err != nil {
		return model.Contact{}, err
	}
	if chat.Kind != model.ChatDirect {
		return model.Contact{}, msgInvalid("solo los chats individuales tienen contacto")
	}
	peer := chat.PeerNativeID
	if peer == "" {
		peer = native
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ct, err := c.app.FindContactByIdentity(ctx, accID, peer); err == nil {
		return ct, nil
	} else if !errors.Is(err, port.ErrNotFound) {
		return model.Contact{}, err
	}
	person, perr := inst.store.GetPerson(ctx, peer)
	if perr != nil && !errors.Is(perr, port.ErrNotFound) {
		return model.Contact{}, perr
	}
	display := chat.Title
	if display == "" && perr == nil {
		display = person.Display()
	}
	phone := ""
	if perr == nil {
		phone = NormalizePhoneE164(person.Phone)
	}
	id := model.Identity{
		AccountID: accID, Channel: inst.acc.Channel, PeerNativeID: peer, DisplayName: display, Phone: phone,
	}
	ct, _, err := c.attachIdentity(ctx, id, display, model.ContactSourceManual)
	return ct, err
}

// attachIdentity añade la identidad al contacto que tenga su teléfono (el más antiguo) o crea uno
// nuevo. Devuelve el contacto y si fue creado. Quien llama tiene `mu`.
func (c *Contacts) attachIdentity(ctx context.Context, id model.Identity, name, source string) (model.Contact, bool, error) {
	now := c.now().UnixMilli()
	if id.Phone != "" {
		found, err := c.app.FindContactsByPhone(ctx, id.Phone)
		if err != nil {
			return model.Contact{}, false, err
		}
		if len(found) > 0 {
			ct := found[0]
			ct.Identities = cleanIdentities(append(append([]model.Identity{}, ct.Identities...), id))
			if ct.Name == "" {
				ct.Name = name
			}
			if ct.Phone == "" {
				ct.Phone = id.Phone
			}
			ct.UpdatedAt = now
			if err := c.app.SaveContact(ctx, ct); err != nil {
				return model.Contact{}, false, err
			}
			got, err := c.app.GetContact(ctx, ct.ID)
			return got, false, err
		}
	}
	if name == "" {
		name = id.Phone
	}
	if name == "" {
		name = id.PeerNativeID
	}
	ct := model.Contact{
		ID: model.NewID(), Name: name, Phone: id.Phone, Tags: []string{}, ListIDs: []string{},
		Identities: cleanIdentities([]model.Identity{id}), Source: source, CreatedAt: now, UpdatedAt: now,
	}
	if err := c.app.SaveContact(ctx, ct); err != nil {
		return model.Contact{}, false, err
	}
	got, err := c.app.GetContact(ctx, ct.ID)
	return got, true, err
}

// Merge une `mergeID` dentro de `keepID`: identidades, etiquetas, listas y notas. El contacto
// absorbido se borra. El resultado conserva «no recibe campañas» si alguno de los dos lo tenía.
func (c *Contacts) Merge(ctx context.Context, keepID, mergeID string) (model.Contact, error) {
	if keepID == "" || mergeID == "" {
		return model.Contact{}, msgInvalid("faltan los contactos a unir")
	}
	if keepID == mergeID {
		return model.Contact{}, msgInvalid("no se puede unir un contacto consigo mismo")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	keep, err := c.app.GetContact(ctx, keepID)
	if err != nil {
		return model.Contact{}, err
	}
	gone, err := c.app.GetContact(ctx, mergeID)
	if err != nil {
		return model.Contact{}, err
	}
	keep.Identities = cleanIdentities(append(append([]model.Identity{}, keep.Identities...), gone.Identities...))
	keep.Tags = unionTags(keep.Tags, gone.Tags)
	keep.ListIDs = unionStrings(keep.ListIDs, gone.ListIDs)
	keep.Notes = appendNote(keep.Notes, gone.Notes)
	if keep.Name == "" {
		keep.Name = gone.Name
	}
	if keep.Phone == "" {
		keep.Phone = NormalizePhoneE164(gone.Phone)
	}
	keep.OptedOut = keep.OptedOut || gone.OptedOut
	keep.Source = strongerSource(keep.Source, gone.Source)
	if gone.CreatedAt != 0 && (keep.CreatedAt == 0 || gone.CreatedAt < keep.CreatedAt) {
		keep.CreatedAt = gone.CreatedAt
	}
	keep.UpdatedAt = c.now().UnixMilli()
	if err := c.app.SaveContact(ctx, keep); err != nil {
		return model.Contact{}, err
	}
	if err := c.app.DeleteContact(ctx, mergeID); err != nil {
		return model.Contact{}, err
	}
	return c.app.GetContact(ctx, keepID)
}

// SplitIdentity separa una identidad de un contacto y la pasa a un contacto nuevo, que es lo que
// devuelve. El nuevo hereda «no recibe campañas». No se puede separar la única identidad.
func (c *Contacts) SplitIdentity(ctx context.Context, contactID string, acc model.AccountID, peer string) (model.Contact, error) {
	if contactID == "" || acc == "" || peer == "" {
		return model.Contact{}, msgInvalid("falta la identidad a separar")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, err := c.app.GetContact(ctx, contactID)
	if err != nil {
		return model.Contact{}, err
	}
	var moved *model.Identity
	rest := make([]model.Identity, 0, len(cur.Identities))
	for i := range cur.Identities {
		it := cur.Identities[i]
		if it.AccountID == acc && it.PeerNativeID == peer {
			moved = &it
			continue
		}
		rest = append(rest, it)
	}
	if moved == nil {
		return model.Contact{}, contactNotFound("la identidad del contacto")
	}
	if len(rest) == 0 {
		return model.Contact{}, msgInvalid("no se puede separar la única identidad del contacto")
	}
	now := c.now().UnixMilli()
	name := moved.DisplayName
	if name == "" {
		name = moved.Phone
	}
	if name == "" {
		name = moved.PeerNativeID
	}
	fresh := model.Contact{
		ID: model.NewID(), Name: name, Phone: moved.Phone, Tags: []string{}, ListIDs: []string{},
		Identities: []model.Identity{*moved}, OptedOut: cur.OptedOut, Source: model.ContactSourceManual,
		CreatedAt: now, UpdatedAt: now,
	}
	// Primero el nuevo (la identidad cambia de dueño), luego el original ya sin ella.
	if err := c.app.SaveContact(ctx, fresh); err != nil {
		return model.Contact{}, err
	}
	cur.Identities = rest
	cur.UpdatedAt = now
	if err := c.app.SaveContact(ctx, cur); err != nil {
		return model.Contact{}, err
	}
	return c.app.GetContact(ctx, fresh.ID)
}

// ImportFromAccount importa la agenda de la cuenta (ListPersons del adaptador). Por cada persona
// con teléfono: si ya hay un contacto con ese teléfono se le añade la identidad, si no se crea
// uno. Sin teléfono (Telegram por usuario, Meta) se crea un contacto con la identidad sola. Una
// persona cuya identidad ya está en algún contacto se omite (o cuenta como actualizada si cambió
// su nombre o teléfono). Volver a importar no duplica.
func (c *Contacts) ImportFromAccount(ctx context.Context, acc model.AccountID) (ImportResult, error) {
	var res ImportResult
	if acc == "" {
		return res, msgInvalid("falta la cuenta")
	}
	inst, err := c.instanceOf(acc)
	if err != nil {
		return res, err
	}
	persons, err := inst.getAdapter().ListPersons(ctx)
	if err != nil {
		return res, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range persons {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if p.NativeID == "" || (inst.acc.SelfID != "" && p.NativeID == inst.acc.SelfID) {
			res.Skipped++
			continue
		}
		phone := NormalizePhoneE164(p.Phone)
		display := p.Display()
		id := model.Identity{
			AccountID: acc, Channel: inst.acc.Channel, PeerNativeID: p.NativeID, DisplayName: display, Phone: phone,
		}
		cur, err := c.app.FindContactByIdentity(ctx, acc, p.NativeID)
		switch {
		case err == nil:
			changed := false
			for i := range cur.Identities {
				it := &cur.Identities[i]
				if it.AccountID != acc || it.PeerNativeID != p.NativeID {
					continue
				}
				if it.DisplayName != display || (phone != "" && it.Phone != phone) {
					it.DisplayName = display
					if phone != "" {
						it.Phone = phone
					}
					changed = true
				}
			}
			if cur.Phone == "" && phone != "" {
				cur.Phone, changed = phone, true
			}
			if !changed {
				res.Skipped++
				continue
			}
			cur.UpdatedAt = c.now().UnixMilli()
			if err := c.app.SaveContact(ctx, cur); err != nil {
				return res, err
			}
			res.Updated++
		case errors.Is(err, port.ErrNotFound):
			_, created, err := c.attachIdentity(ctx, id, display, model.ContactSourceImport)
			if err != nil {
				return res, err
			}
			if created {
				res.Created++
			} else {
				res.Updated++
			}
		default:
			return res, err
		}
	}
	return res, nil
}

// SuggestMerges devuelve pares [conservar, absorber] de contactos que comparten algún teléfono
// E.164 (el propio o el de una identidad). Conserva el más antiguo; si hay más de dos con el mismo
// teléfono, todos se emparejan con el más antiguo.
func (c *Contacts) SuggestMerges(ctx context.Context) ([][2]string, error) {
	all := []model.Contact{}
	for offset := 0; ; offset += contactsPage {
		page, total, err := c.app.ListContacts(ctx, model.ContactFilter{Offset: offset, Limit: contactsPage})
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) == 0 || offset+len(page) >= total {
			break
		}
	}
	byID := make(map[string]model.Contact, len(all))
	byPhone := map[string][]string{}
	for _, ct := range all {
		byID[ct.ID] = ct
		phones := map[string]bool{}
		if p := NormalizePhoneE164(ct.Phone); p != "" {
			phones[p] = true
		}
		for _, it := range ct.Identities {
			if p := NormalizePhoneE164(it.Phone); p != "" {
				phones[p] = true
			}
		}
		for p := range phones {
			byPhone[p] = append(byPhone[p], ct.ID)
		}
	}
	older := func(a, b string) bool {
		x, y := byID[a], byID[b]
		if x.CreatedAt != y.CreatedAt {
			return x.CreatedAt < y.CreatedAt
		}
		return a < b
	}
	seen := map[[2]string]bool{}
	out := [][2]string{}
	phones := make([]string, 0, len(byPhone))
	for p := range byPhone {
		phones = append(phones, p)
	}
	sort.Strings(phones)
	for _, p := range phones {
		ids := byPhone[p]
		if len(ids) < 2 {
			continue
		}
		sort.Slice(ids, func(i, j int) bool { return older(ids[i], ids[j]) })
		for _, other := range ids[1:] {
			pair := [2]string{ids[0], other}
			if !seen[pair] {
				seen[pair] = true
				out = append(out, pair)
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------------------------
// Listas

func (c *Contacts) Lists(ctx context.Context) ([]model.ContactList, error) {
	out, err := c.app.ListLists(ctx)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.ContactList{}
	}
	return out, nil
}

// SaveList crea (id vacío) o renombra y recolorea una lista. Los miembros no se tocan.
func (c *Contacts) SaveList(ctx context.Context, l model.ContactList) (model.ContactList, error) {
	l.Name = strings.TrimSpace(l.Name)
	if l.Name == "" {
		return model.ContactList{}, msgInvalid("la lista necesita un nombre")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	all, err := c.app.ListLists(ctx)
	if err != nil {
		return model.ContactList{}, err
	}
	if l.ID == "" {
		l.ID = model.NewID()
	} else {
		found := false
		for _, x := range all {
			if x.ID == l.ID {
				found = true
			}
		}
		if !found {
			return model.ContactList{}, contactNotFound("lista " + l.ID)
		}
	}
	for _, x := range all {
		if x.ID != l.ID && strings.EqualFold(x.Name, l.Name) {
			return model.ContactList{}, msgInvalid("ya hay una lista llamada %q", l.Name)
		}
	}
	if err := c.app.SaveList(ctx, l); err != nil {
		return model.ContactList{}, err
	}
	all, err = c.app.ListLists(ctx)
	if err != nil {
		return model.ContactList{}, err
	}
	for _, x := range all {
		if x.ID == l.ID {
			return x, nil
		}
	}
	return l, nil
}

func (c *Contacts) DeleteList(ctx context.Context, id string) error {
	if id == "" {
		return msgInvalid("falta la lista")
	}
	return c.app.DeleteList(ctx, id)
}

// SetListMembers añade y quita contactos de una lista (ErrNotFound si la lista no existe).
func (c *Contacts) SetListMembers(ctx context.Context, listID string, add, remove []string) error {
	if listID == "" {
		return msgInvalid("falta la lista")
	}
	add = unionStrings(add, nil)
	if err := c.app.SetListMembers(ctx, listID, add, unionStrings(remove, nil)); err != nil {
		return err
	}
	c.keepAutoContacts(ctx, add)
	return nil
}

// keepAutoContacts pasa a manual los contactos automáticos que el usuario acaba de meter en una
// lista: ya los eligió, así que no se borran al quitar la cuenta de la que salieron. Es de mejor
// esfuerzo: un fallo no deshace la lista.
func (c *Contacts) keepAutoContacts(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		ct, err := c.app.GetContact(ctx, id)
		if err != nil || ct.Source != model.ContactSourceAuto {
			continue
		}
		ct.Source = model.ContactSourceManual
		ct.UpdatedAt = c.now().UnixMilli()
		if err := c.app.SaveContact(ctx, ct); err != nil {
			slog.Warn("contactos: no se pudo marcar el contacto como manual", "err", err)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// CSV

// decodeCSVText: UTF-8 (sin la marca BOM) o, si el archivo no es UTF-8 válido, Latin-1 (lo que
// guarda Excel en español).
func decodeCSVText(b []byte) string {
	b = []byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")) // BOM UTF-8
	if utf8.Valid(b) {
		return string(b)
	}
	rs := make([]rune, len(b))
	for i, x := range b {
		rs[i] = rune(x)
	}
	return string(rs)
}

// detectDelimiter mira la primera línea: `;` si hay más punto y coma que comas, si no `,`.
func detectDelimiter(text string) rune {
	line := text
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		line = text[:i]
	}
	if strings.Count(line, ";") > strings.Count(line, ",") {
		return ';'
	}
	return ','
}

func csvCell(rec []string, idx int) string {
	if idx < 0 || idx >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[idx])
}

func splitTags(s string) []string {
	return cleanTags(strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '|' }))
}

// ImportCSV importa contactos de un archivo CSV (delimitador `;` o `,` detectado en la primera
// línea; tolera BOM UTF-8). Hace falta la columna del teléfono: las filas sin un teléfono válido
// se omiten. Si el teléfono ya es de un contacto, se completan su nombre, etiquetas y notas; si
// no cambia nada, la fila cuenta como omitida. Con ListID, todos los contactos de las filas
// válidas se añaden a esa lista.
func (c *Contacts) ImportCSV(ctx context.Context, path string, m CSVMapping) (ImportResult, error) {
	var res ImportResult
	if strings.TrimSpace(path) == "" {
		return res, msgInvalid("falta el archivo")
	}
	if m.Phone < 0 {
		return res, msgInvalid("elige la columna del teléfono")
	}
	f, err := os.Open(path)
	if err != nil {
		return res, msgInvalid("no se pudo abrir el archivo: %v", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, csvMaxBytes+1))
	if err != nil {
		return res, fmt.Errorf("contactos: leer el archivo: %w", err)
	}
	if len(raw) > csvMaxBytes {
		return res, msgInvalid("el archivo es demasiado grande")
	}
	text := decodeCSVText(raw)

	c.mu.Lock()
	defer c.mu.Unlock()
	if m.ListID != "" {
		lists, err := c.app.ListLists(ctx)
		if err != nil {
			return res, err
		}
		found := false
		for _, l := range lists {
			if l.ID == m.ListID {
				found = true
			}
		}
		if !found {
			return res, contactNotFound("lista " + m.ListID)
		}
	}

	r := csv.NewReader(strings.NewReader(text))
	r.Comma = detectDelimiter(text)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	var members []string
	first := true
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				if !(first && m.HasHeader) {
					res.Skipped++
				}
				first = false
				continue
			}
			return res, fmt.Errorf("contactos: leer el CSV: %w", err)
		}
		if first {
			first = false
			if m.HasHeader {
				continue
			}
		}
		phone := NormalizePhoneE164(csvCell(rec, m.Phone))
		if phone == "" {
			res.Skipped++
			continue
		}
		name := csvCell(rec, m.Name)
		tags := splitTags(csvCell(rec, m.Tags))
		notes := csvCell(rec, m.Notes)
		id, outcome, err := c.upsertFromCSV(ctx, phone, name, tags, notes)
		if err != nil {
			return res, err
		}
		switch outcome {
		case csvCreated:
			res.Created++
		case csvUpdated:
			res.Updated++
		default:
			res.Skipped++
		}
		members = append(members, id)
	}
	if m.ListID != "" && len(members) > 0 {
		if err := c.app.SetListMembers(ctx, m.ListID, unionStrings(members, nil), nil); err != nil {
			return res, err
		}
	}
	return res, nil
}

type csvOutcome int

const (
	csvSkipped csvOutcome = iota
	csvCreated
	csvUpdated
)

func (c *Contacts) upsertFromCSV(ctx context.Context, phone, name string, tags []string, notes string) (string, csvOutcome, error) {
	now := c.now().UnixMilli()
	found, err := c.app.FindContactsByPhone(ctx, phone)
	if err != nil {
		return "", csvSkipped, err
	}
	if len(found) == 0 {
		if name == "" {
			name = phone
		}
		ct := model.Contact{
			ID: model.NewID(), Name: name, Phone: phone, Notes: notes, Tags: tags, ListIDs: []string{},
			Identities: []model.Identity{}, Source: model.ContactSourceImport, CreatedAt: now, UpdatedAt: now,
		}
		if err := c.app.SaveContact(ctx, ct); err != nil {
			return "", csvSkipped, err
		}
		return ct.ID, csvCreated, nil
	}
	ct := found[0]
	changed := false
	if (ct.Name == "" || ct.Name == ct.Phone) && name != "" && name != ct.Name {
		ct.Name, changed = name, true
	}
	if merged := unionTags(ct.Tags, tags); len(merged) != len(cleanTags(ct.Tags)) {
		ct.Tags, changed = merged, true
	}
	if n := appendNote(ct.Notes, notes); n != strings.TrimSpace(ct.Notes) {
		ct.Notes, changed = n, true
	}
	if ct.Phone == "" {
		ct.Phone, changed = phone, true
	}
	if ct.Source == model.ContactSourceAuto {
		// El archivo nombra a este contacto: deja de ser solo automático.
		ct.Source, changed = model.ContactSourceImport, true
	}
	if !changed {
		return ct.ID, csvSkipped, nil
	}
	ct.UpdatedAt = now
	if err := c.app.SaveContact(ctx, ct); err != nil {
		return "", csvSkipped, err
	}
	return ct.ID, csvUpdated, nil
}
