package meta

// Acceso al almacén clave-valor del account.db (tabla kv, 05-ALMACEN). El adaptador no recibe el
// AccountStore: usa la tabla por SQL directo con claves prefijadas «meta.» (09 §2):
//
//	meta.cookies            cookies de la sesión (JSON {nombre: valor})
//	meta.platform           plataforma de la sesión de Messenger ("messenger" | "messenger-lite")
//	meta.session            sesión nativa de la librería (inicio de sesión con contraseña)
//	meta.self               id propio (numérico)
//	meta.seq                cursor de Instagram (seq_id y marca de tiempo, JSON)
//	meta.igthread.<chat>    ids largo y de equipo de un hilo de Instagram (JSON)
//	meta.e2ee.<chat>        JID cifrado de un chat de Messenger (E2EE)

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const kvSchema = `CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value BLOB NOT NULL)`

// KVPrefix es el prefijo de todas las claves del adaptador.
const KVPrefix = "meta."

// Claves fijas.
const (
	KeyCookies  = KVPrefix + "cookies"
	KeyPlatform = KVPrefix + "platform"
	KeySession  = KVPrefix + "session"
	KeySelf     = KVPrefix + "self"
	KeySeq      = KVPrefix + "seq"
)

// KV es el acceso a la tabla kv del account.db.
type KV struct {
	db *sql.DB
}

// NewKV envuelve la base de la cuenta.
func NewKV(db *sql.DB) *KV { return &KV{db: db} }

// Ensure crea la tabla si el almacén aún no lo hizo (en producción ya existe).
func (k *KV) Ensure(ctx context.Context) error {
	if _, err := k.db.ExecContext(ctx, kvSchema); err != nil {
		return fmt.Errorf("crear tabla kv: %w", err)
	}
	return nil
}

// Get devuelve el valor de la clave; ok=false si no existe o está vacío.
func (k *KV) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var v []byte
	err := k.db.QueryRowContext(ctx, "SELECT value FROM kv WHERE key = ?", key).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return v, len(v) > 0, nil
}

const kvUpsert = "INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"

// Set guarda (o reemplaza) una clave.
func (k *KV) Set(ctx context.Context, key string, value []byte) error {
	if value == nil {
		value = []byte{}
	}
	_, err := k.db.ExecContext(ctx, kvUpsert, key, value)
	return err
}

// Del borra una clave.
func (k *KV) Del(ctx context.Context, key string) error {
	_, err := k.db.ExecContext(ctx, "DELETE FROM kv WHERE key = ?", key)
	return err
}

func prefixEnd(prefix string) string {
	if prefix == "" {
		return ""
	}
	b := []byte(prefix)
	b[len(b)-1]++
	return string(b)
}

// DelPrefix borra todas las claves que empiezan por prefix.
func (k *KV) DelPrefix(ctx context.Context, prefix string) error {
	_, err := k.db.ExecContext(ctx, "DELETE FROM kv WHERE key >= ? AND key < ?", prefix, prefixEnd(prefix))
	return err
}

// Wipe borra todo lo que guarda el adaptador (al cerrar la sesión).
func (k *KV) Wipe(ctx context.Context) error { return k.DelPrefix(ctx, KVPrefix) }
