package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"dconnector/internal/core/model"
)

const defaultContactLimit = 100

const contactCols = `c.id, c.name, c.phone, c.notes, c.tags_json, c.opted_out, c.created_at, c.updated_at, c.source`

func scanContactRow(sc scanner) (model.Contact, error) {
	var (
		c    model.Contact
		tags string
		opt  int
	)
	if err := sc.Scan(&c.ID, &c.Name, &c.Phone, &c.Notes, &tags, &opt, &c.CreatedAt, &c.UpdatedAt, &c.Source); err != nil {
		return model.Contact{}, err
	}
	c.Tags = decodeStrings(tags)
	c.OptedOut = opt != 0
	c.Identities = []model.Identity{}
	c.ListIDs = []string{}
	return c, nil
}

// fillContacts completa identidades y listas de los contactos (en una consulta por cada tipo).
func fillContacts(ctx context.Context, q dbtx, cs []model.Contact) error {
	if len(cs) == 0 {
		return nil
	}
	idx := make(map[string]int, len(cs))
	ids := make([]string, 0, len(cs))
	for i, c := range cs {
		idx[c.ID] = i
		ids = append(ids, c.ID)
	}
	for _, part := range chunks(ids, 400) {
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		rows, err := q.QueryContext(ctx,
			`SELECT contact_id, account_id, channel, peer_native_id, display_name, phone
			   FROM contact_identities WHERE contact_id IN (`+placeholders(len(part))+`)
			   ORDER BY account_id, peer_native_id`, args...)
		if err != nil {
			return fmt.Errorf("almacén: no se pudieron leer las identidades: %w", err)
		}
		for rows.Next() {
			var (
				cid, ch string
				it      model.Identity
			)
			if err := rows.Scan(&cid, &it.AccountID, &ch, &it.PeerNativeID, &it.DisplayName, &it.Phone); err != nil {
				rows.Close()
				return err
			}
			it.Channel = model.Channel(ch)
			i := idx[cid]
			cs[i].Identities = append(cs[i].Identities, it)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		rows, err = q.QueryContext(ctx,
			`SELECT contact_id, list_id FROM list_members WHERE contact_id IN (`+placeholders(len(part))+`)
			   ORDER BY list_id`, args...)
		if err != nil {
			return fmt.Errorf("almacén: no se pudieron leer las listas del contacto: %w", err)
		}
		for rows.Next() {
			var cid, lid string
			if err := rows.Scan(&cid, &lid); err != nil {
				rows.Close()
				return err
			}
			i := idx[cid]
			cs[i].ListIDs = append(cs[i].ListIDs, lid)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

// queryContacts ejecuta una consulta que devuelve las columnas de contactCols y completa el resto.
func (s *AppStore) queryContacts(ctx context.Context, query string, args ...any) ([]model.Contact, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer los contactos: %w", err)
	}
	out := []model.Contact{}
	for rows.Next() {
		c, err := scanContactRow(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("almacén: contacto ilegible: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := fillContacts(ctx, s.db, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListContacts: filtro por texto (nombre, teléfono, notas, identidades), lista y etiqueta.
// Orden alfabético; devuelve también el total sin paginar.
func (s *AppStore) ListContacts(ctx context.Context, f model.ContactFilter) ([]model.Contact, int, error) {
	where := []string{"1 = 1"}
	var args []any
	if q := strings.TrimSpace(f.Query); q != "" {
		p := likePattern(q)
		where = append(where, `(c.name LIKE ? ESCAPE '\' OR c.phone LIKE ? ESCAPE '\' OR c.notes LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM contact_identities i WHERE i.contact_id = c.id
			           AND (i.display_name LIKE ? ESCAPE '\' OR i.phone LIKE ? ESCAPE '\' OR i.peer_native_id LIKE ? ESCAPE '\')))`)
		args = append(args, p, p, p, p, p, p)
	}
	if f.ListID != "" {
		where = append(where, `EXISTS (SELECT 1 FROM list_members m WHERE m.contact_id = c.id AND m.list_id = ?)`)
		args = append(args, f.ListID)
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM json_each(c.tags_json) t WHERE t.value = ? COLLATE NOCASE)`)
		args = append(args, f.Tag)
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contacts c WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("almacén: no se pudieron contar los contactos: %w", err)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultContactLimit
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	qargs := append(append([]any{}, args...), limit, offset)
	out, err := s.queryContacts(ctx,
		`SELECT `+contactCols+` FROM contacts c WHERE `+cond+` ORDER BY c.name COLLATE NOCASE, c.id LIMIT ? OFFSET ?`, qargs...)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// GetContact devuelve un contacto con identidades y listas (ErrNotFound si no existe).
func (s *AppStore) GetContact(ctx context.Context, id string) (model.Contact, error) {
	cs, err := s.queryContacts(ctx, `SELECT `+contactCols+` FROM contacts c WHERE c.id = ?`, id)
	if err != nil {
		return model.Contact{}, err
	}
	if len(cs) == 0 {
		return model.Contact{}, notFound("contacto " + id)
	}
	return cs[0], nil
}

// FindContactByIdentity busca el contacto que tiene esa identidad (ErrNotFound si no hay).
func (s *AppStore) FindContactByIdentity(ctx context.Context, acc model.AccountID, peerNativeID string) (model.Contact, error) {
	cs, err := s.queryContacts(ctx,
		`SELECT `+contactCols+` FROM contacts c
		   JOIN contact_identities i ON i.contact_id = c.id
		  WHERE i.account_id = ? AND i.peer_native_id = ?`, string(acc), peerNativeID)
	if err != nil {
		return model.Contact{}, err
	}
	if len(cs) == 0 {
		return model.Contact{}, notFound("contacto de " + peerNativeID)
	}
	return cs[0], nil
}

// FindContactsByPhone devuelve los contactos cuyo teléfono (o el de alguna identidad) coincide exactamente.
func (s *AppStore) FindContactsByPhone(ctx context.Context, phone string) ([]model.Contact, error) {
	if phone == "" {
		return []model.Contact{}, nil
	}
	return s.queryContacts(ctx,
		`SELECT `+contactCols+` FROM contacts c
		  WHERE c.phone = ?
		     OR EXISTS (SELECT 1 FROM contact_identities i WHERE i.contact_id = c.id AND i.phone = ?)
		  ORDER BY c.created_at, c.id`, phone, phone)
}

// SaveContact crea o actualiza el contacto y reemplaza sus identidades, etiquetas y listas.
// Una identidad (cuenta + id nativo) que ya perteneciera a otro contacto pasa a este.
func (s *AppStore) SaveContact(ctx context.Context, c model.Contact) error {
	if c.ID == "" {
		return invalid("el contacto no tiene id")
	}
	return inTx(ctx, s.db, func(tx *sql.Tx) error { return saveContactTx(ctx, tx, c) })
}

// SaveContacts guarda varios contactos en una sola transacción (misma semántica que SaveContact).
// No está en el puerto: Contacts lo usa por aserción de tipo para los lotes de contactos
// automáticos, que pueden traer cientos de personas.
func (s *AppStore) SaveContacts(ctx context.Context, cs []model.Contact) error {
	for _, c := range cs {
		if c.ID == "" {
			return invalid("el contacto no tiene id")
		}
	}
	if len(cs) == 0 {
		return nil
	}
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, c := range cs {
			if err := saveContactTx(ctx, tx, c); err != nil {
				return err
			}
		}
		return nil
	})
}

func saveContactTx(ctx context.Context, tx *sql.Tx, c model.Contact) error {
	now := nowMs()
	created := c.CreatedAt
	if created == 0 {
		created = now
	}
	updated := c.UpdatedAt
	if updated == 0 {
		updated = now
	}
	source := c.Source
	if source == "" {
		source = model.ContactSourceManual
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO contacts(id, name, phone, notes, tags_json, opted_out, created_at, updated_at, source)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name, phone = excluded.phone, notes = excluded.notes, tags_json = excluded.tags_json,
  opted_out = excluded.opted_out, updated_at = excluded.updated_at, source = excluded.source`,
		c.ID, c.Name, c.Phone, c.Notes, encodeStrings(c.Tags), b2i(c.OptedOut), created, updated, source)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar el contacto: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM contact_identities WHERE contact_id = ?`, c.ID); err != nil {
		return fmt.Errorf("almacén: no se pudieron reemplazar las identidades: %w", err)
	}
	for _, it := range c.Identities {
		ch := it.Channel
		if ch == "" {
			ch = channelOf(it.AccountID)
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO contact_identities(contact_id, account_id, channel, peer_native_id, display_name, phone)
VALUES(?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, peer_native_id) DO UPDATE SET
  contact_id = excluded.contact_id, channel = excluded.channel,
  display_name = excluded.display_name, phone = excluded.phone`,
			c.ID, string(it.AccountID), string(ch), it.PeerNativeID, it.DisplayName, it.Phone)
		if err != nil {
			return fmt.Errorf("almacén: no se pudo guardar la identidad: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM list_members WHERE contact_id = ?`, c.ID); err != nil {
		return fmt.Errorf("almacén: no se pudieron reemplazar las listas: %w", err)
	}
	for _, lid := range c.ListIDs {
		// Una lista que ya no existe se ignora en silencio.
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO list_members(list_id, contact_id)
			 SELECT id, ? FROM lists WHERE id = ?`, c.ID, lid); err != nil {
			return fmt.Errorf("almacén: no se pudo añadir a la lista: %w", err)
		}
	}
	return nil
}

// DeleteOrphanAutoContacts borra los contactos automáticos que ya no tienen ninguna identidad (por
// ejemplo, tras eliminar la cuenta de la que salieron) y devuelve cuántos. Los manuales y los
// importados se conservan aunque se queden sin identidades. No está en el puerto: Accounts lo
// usa por aserción de tipo.
func (s *AppStore) DeleteOrphanAutoContacts(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM contacts WHERE source = 'auto'
   AND NOT EXISTS (SELECT 1 FROM contact_identities i WHERE i.contact_id = contacts.id)`)
	if err != nil {
		return 0, fmt.Errorf("almacén: no se pudieron limpiar los contactos automáticos: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteContact borra el contacto (sus identidades y membresías caen en cascada).
func (s *AppStore) DeleteContact(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM contacts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("almacén: no se pudo borrar el contacto: %w", err)
	}
	return nil
}

// ---- Listas ----

// ListLists devuelve las listas por nombre, con el número de miembros.
func (s *AppStore) ListLists(ctx context.Context) ([]model.ContactList, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT l.id, l.name, l.color, (SELECT COUNT(*) FROM list_members m WHERE m.list_id = l.id)
  FROM lists l ORDER BY l.name COLLATE NOCASE, l.created_at, l.id`)
	if err != nil {
		return nil, fmt.Errorf("almacén: no se pudieron leer las listas: %w", err)
	}
	defer rows.Close()
	out := []model.ContactList{}
	for rows.Next() {
		var l model.ContactList
		if err := rows.Scan(&l.ID, &l.Name, &l.Color, &l.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SaveList crea o actualiza una lista (los miembros no se tocan).
func (s *AppStore) SaveList(ctx context.Context, l model.ContactList) error {
	if l.ID == "" {
		return invalid("la lista no tiene id")
	}
	color := l.Color
	if color == "" {
		color = "#64748b"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO lists(id, name, color, created_at) VALUES(?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name = excluded.name, color = excluded.color`,
		l.ID, l.Name, color, nowMs())
	if err != nil {
		return fmt.Errorf("almacén: no se pudo guardar la lista: %w", err)
	}
	return nil
}

// DeleteList borra la lista y sus membresías.
func (s *AppStore) DeleteList(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM lists WHERE id = ?`, id); err != nil {
		return fmt.Errorf("almacén: no se pudo borrar la lista: %w", err)
	}
	return nil
}

// SetListMembers añade y quita miembros de una lista en una transacción. Los contactos que no
// existen se ignoran; quitar uno que no estaba no es un error.
func (s *AppStore) SetListMembers(ctx context.Context, listID string, add, remove []string) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM lists WHERE id = ?`, listID).Scan(&one); err != nil {
			if isNoRows(err) {
				return notFound("lista " + listID)
			}
			return err
		}
		for _, cid := range remove {
			if _, err := tx.ExecContext(ctx, `DELETE FROM list_members WHERE list_id = ? AND contact_id = ?`, listID, cid); err != nil {
				return fmt.Errorf("almacén: no se pudo quitar de la lista: %w", err)
			}
		}
		for _, cid := range add {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO list_members(list_id, contact_id)
				 SELECT ?, id FROM contacts WHERE id = ?`, listID, cid); err != nil {
				return fmt.Errorf("almacén: no se pudo añadir a la lista: %w", err)
			}
		}
		return nil
	})
}

// ListMembers devuelve los contactos que están en alguna de las listas, sin repetir.
func (s *AppStore) ListMembers(ctx context.Context, listIDs []string) ([]model.Contact, error) {
	if len(listIDs) == 0 {
		return []model.Contact{}, nil
	}
	args := make([]any, len(listIDs))
	for i, id := range listIDs {
		args[i] = id
	}
	return s.queryContacts(ctx,
		`SELECT `+contactCols+` FROM contacts c
		  WHERE c.id IN (SELECT contact_id FROM list_members WHERE list_id IN (`+placeholders(len(listIDs))+`))
		  ORDER BY c.name COLLATE NOCASE, c.id`, args...)
}
