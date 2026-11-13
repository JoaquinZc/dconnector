package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// dbtx: lo que comparten *sql.DB y *sql.Tx.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanner: lo que comparten *sql.Row y *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func nowMs() int64 { return time.Now().UnixMilli() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func notFound(what string) error { return fmt.Errorf("%s: %w", what, port.ErrNotFound) }

func invalid(msg string) error { return fmt.Errorf("%s: %w", msg, port.ErrInvalid) }

// inTx ejecuta fn en una transacción (inmediata, por el DSN) y la confirma si no hay error.
func inTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("almacén: no se pudo iniciar la transacción: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("almacén: no se pudo confirmar la transacción: %w", err)
	}
	return nil
}

// likePattern devuelve "%q%" con los comodines de q escapados (usar con ESCAPE '\').
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// placeholders devuelve "?,?,?" con n signos.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// chunks parte s en trozos de hasta size elementos.
func chunks[T any](s []T, size int) [][]T {
	var out [][]T
	for len(s) > size {
		out = append(out, s[:size])
		s = s[size:]
	}
	if len(s) > 0 {
		out = append(out, s)
	}
	return out
}

func encodeJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("almacén: no se pudo serializar: %w", err)
	}
	return string(b), nil
}

func decodeJSON(s string, v any) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(s), v); err != nil {
		return fmt.Errorf("almacén: JSON dañado: %w", err)
	}
	return nil
}

// encodeStrings guarda una lista como JSON ("[]" si es nula).
func encodeStrings(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// decodeStrings lee una lista JSON; nunca devuelve nil.
func decodeStrings(s string) []string {
	out := []string{}
	if strings.TrimSpace(s) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

// channelOf deduce el canal del prefijo del id de cuenta ("wa-…", "tg-…", "fb-…", "ig-…").
func channelOf(id model.AccountID) model.Channel {
	switch {
	case strings.HasPrefix(string(id), "wa-"):
		return model.ChannelWhatsApp
	case strings.HasPrefix(string(id), "tg-"):
		return model.ChannelTelegram
	case strings.HasPrefix(string(id), "fb-"):
		return model.ChannelMessenger
	case strings.HasPrefix(string(id), "ig-"):
		return model.ChannelInstagram
	}
	return ""
}

// deliveryRank: orden de entrega pending < sent < delivered < read < played.
func deliveryRank(d model.DeliveryState) int {
	switch d {
	case model.DeliveryPending:
		return 0
	case model.DeliverySent:
		return 1
	case model.DeliveryDelivered:
		return 2
	case model.DeliveryRead:
		return 3
	case model.DeliveryPlayed:
		return 4
	}
	return -1
}

// mergeDelivery decide el estado resultante al recibir `next` sobre `cur`:
// nunca baja de nivel; "failed" solo pisa a "pending"; cualquier otro estado reemplaza a "failed".
func mergeDelivery(cur, next model.DeliveryState) model.DeliveryState {
	switch {
	case next == "":
		return cur
	case cur == "":
		return next
	case next == model.DeliveryFailed:
		if cur == model.DeliveryPending {
			return next
		}
		return cur
	case cur == model.DeliveryFailed:
		return next
	case deliveryRank(next) > deliveryRank(cur):
		return next
	}
	return cur
}

func mediaURL(acc model.AccountID, mediaID string) string {
	if mediaID == "" {
		return ""
	}
	return "/media/" + string(acc) + "/" + mediaID
}

func avatarURL(acc model.AccountID, file string) string {
	if file == "" {
		return ""
	}
	return "/avatar/" + string(acc) + "/" + file
}

// avatarFile extrae el nombre de archivo de una URL "/avatar/<cuenta>/<archivo>" (o lo devuelve
// tal cual si ya es solo el nombre).
func avatarFile(acc model.AccountID, url string) string {
	if url == "" {
		return ""
	}
	url = strings.TrimPrefix(url, "/avatar/"+string(acc)+"/")
	if i := strings.LastIndexAny(url, "/\\"); i >= 0 {
		url = url[i+1:]
	}
	return url
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
