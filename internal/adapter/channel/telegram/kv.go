package telegram

// Acceso al almacén clave-valor del account.db (tabla kv, 05-ALMACEN). El adaptador no recibe el
// AccountStore: usa la tabla por SQL directo con claves prefijadas «tg.» (08 §1):
//
//	tg.session                      sesión de gotd
//	tg.state.<usuario>              pts/qts/date/seq (JSON)
//	tg.chpts.<usuario>.<canal>      pts de cada canal
//	tg.hash.<usuario>.<canal>       access_hash de canales para el gestor de huecos
//	tg.uhash.<usuario>.<otro>       access_hash de usuarios para el gestor de huecos
//	tg.peer.<nativo>                access_hash de cada par (usuario o canal) visto
//	tg.poll.<chat>.<mensaje>        opciones de cada encuesta (para votar)
//	tg.pollid.<id>                  de qué mensaje es cada encuesta
//	tg.msgchat.<id>                 en qué chat está cada mensaje de un privado/grupo (para borrados)

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const kvSchema = `CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value BLOB NOT NULL)`

// kvPrefix es el prefijo de todas las claves del adaptador.
const kvPrefix = "tg."

type kvStore struct {
	db *sql.DB
}

func newKV(db *sql.DB) *kvStore { return &kvStore{db: db} }

// ensure crea la tabla si el almacén aún no lo hizo (en producción ya existe).
func (k *kvStore) ensure(ctx context.Context) error {
	if _, err := k.db.ExecContext(ctx, kvSchema); err != nil {
		return fmt.Errorf("crear tabla kv: %w", err)
	}
	return nil
}

func (k *kvStore) get(ctx context.Context, key string) ([]byte, bool, error) {
	var v []byte
	err := k.db.QueryRowContext(ctx, "SELECT value FROM kv WHERE key = ?", key).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return v, true, nil
}

const kvUpsert = "INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"

func (k *kvStore) set(ctx context.Context, key string, value []byte) error {
	if value == nil {
		value = []byte{}
	}
	_, err := k.db.ExecContext(ctx, kvUpsert, key, value)
	return err
}

// setMany guarda varias claves en una sola transacción.
func (k *kvStore) setMany(ctx context.Context, kv map[string][]byte) error {
	if len(kv) == 0 {
		return nil
	}
	tx, err := k.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for key, v := range kv {
		if v == nil {
			v = []byte{}
		}
		if _, err := tx.ExecContext(ctx, kvUpsert, key, v); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (k *kvStore) del(ctx context.Context, key string) error {
	_, err := k.db.ExecContext(ctx, "DELETE FROM kv WHERE key = ?", key)
	return err
}

// prefixEnd devuelve el primer texto mayor que todos los que empiezan por prefix.
func prefixEnd(prefix string) string {
	if prefix == "" {
		return ""
	}
	b := []byte(prefix)
	b[len(b)-1]++
	return string(b)
}

func (k *kvStore) delPrefix(ctx context.Context, prefix string) error {
	_, err := k.db.ExecContext(ctx, "DELETE FROM kv WHERE key >= ? AND key < ?", prefix, prefixEnd(prefix))
	return err
}

// scanPrefix recorre las claves que empiezan por prefix, en orden.
func (k *kvStore) scanPrefix(ctx context.Context, prefix string, f func(key string, value []byte) error) error {
	rows, err := k.db.QueryContext(ctx, "SELECT key, value FROM kv WHERE key >= ? AND key < ? ORDER BY key",
		prefix, prefixEnd(prefix))
	if err != nil {
		return err
	}
	type row struct {
		key string
		val []byte
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.key, &r.val); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, r := range all {
		if err := f(r.key, r.val); err != nil {
			return err
		}
	}
	return nil
}

// wipe borra todo lo que guarda el adaptador (al cerrar la sesión).
func (k *kvStore) wipe(ctx context.Context) error {
	return k.delPrefix(ctx, kvPrefix)
}
