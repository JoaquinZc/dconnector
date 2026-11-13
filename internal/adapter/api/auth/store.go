package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	sessionIdle   = 24 * time.Hour
	sessionHard   = 30 * 24 * time.Hour
	mediaTTL      = 15 * time.Minute
	wsTicketTTL   = 30 * time.Second
	maxSessions   = 50
	touchInterval = time.Minute

	kindSession = "session"
	kindMedia   = "media"

	roleAdmin = "admin"
)

var (
	errUserExists = errors.New("Ya existe un usuario con ese nombre.")
	errNoUser     = errors.New("No existe ese usuario.")
	usernameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)
)

type user struct {
	ID, Username, Hash, Role string
	CreatedAt, DisabledAt    int64
}

func validateUsername(name string) error {
	if !usernameRe.MatchString(name) {
		return errors.New("El nombre de usuario debe tener de 1 a 64 caracteres: letras, números, punto, guion, guion bajo o arroba.")
	}
	if strings.EqualFold(name, "local") {
		return errors.New("El nombre «local» está reservado para el modo local.")
	}
	return nil
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func createUser(ctx context.Context, db *sql.DB, username, hash, role string, now time.Time) (user, error) {
	u := user{ID: newID(), Username: username, Hash: hash, Role: role, CreatedAt: ms(now)}
	_, err := db.ExecContext(ctx,
		`INSERT INTO users(id, username, password_hash, role, created_at, disabled_at) VALUES(?,?,?,?,?,0)`,
		u.ID, u.Username, u.Hash, u.Role, u.CreatedAt)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return user{}, errUserExists
		}
		return user{}, fmt.Errorf("auth: no se pudo crear el usuario: %w", err)
	}
	return u, nil
}

func getUser(ctx context.Context, db *sql.DB, username string) (user, error) {
	var u user
	err := db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at, disabled_at FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.Hash, &u.Role, &u.CreatedAt, &u.DisabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return user{}, errNoUser
	}
	return u, err
}

func getUserByID(ctx context.Context, db *sql.DB, id string) (user, error) {
	var u user
	err := db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at, disabled_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.Hash, &u.Role, &u.CreatedAt, &u.DisabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return user{}, errNoUser
	}
	return u, err
}

func listUsers(ctx context.Context, db *sql.DB) ([]user, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, username, password_hash, role, created_at, disabled_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []user
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.ID, &u.Username, &u.Hash, &u.Role, &u.CreatedAt, &u.DisabledAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// setPassword cambia la clave e invalida todos los tokens del usuario, en una transacción.
func setPassword(ctx context.Context, db *sql.DB, userID, hash string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNoUser
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_tokens WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func setDisabled(ctx context.Context, db *sql.DB, userID string, disabled bool, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var at int64
	if disabled {
		at = ms(now)
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, at, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNoUser
	}
	if disabled {
		if _, err := tx.ExecContext(ctx, `DELETE FROM auth_tokens WHERE user_id = ?`, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// issueToken crea un token opaco y guarda solo su SHA-256. Devuelve el token y su caducidad (ms).
func issueToken(ctx context.Context, db *sql.DB, userID, kind string, now time.Time) (string, int64, error) {
	tok, err := randomToken()
	if err != nil {
		return "", 0, err
	}
	var exp, hard time.Time
	if kind == kindMedia {
		exp = now.Add(mediaTTL)
		hard = exp
	} else {
		hard = now.Add(sessionHard)
		exp = now.Add(sessionIdle)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	// Limpieza oportunista: caducados y exceso de sesiones por usuario.
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_tokens WHERE expires_at <= ? OR hard_expires_at <= ?`, ms(now), ms(now)); err != nil {
		return "", 0, err
	}
	if kind == kindSession {
		if _, err := tx.ExecContext(ctx, `DELETE FROM auth_tokens WHERE user_id = ? AND kind = ? AND token_hash IN (
			SELECT token_hash FROM auth_tokens WHERE user_id = ? AND kind = ? ORDER BY created_at DESC LIMIT -1 OFFSET ?)`,
			userID, kindSession, userID, kindSession, maxSessions-1); err != nil {
			return "", 0, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO auth_tokens(token_hash, user_id, kind, created_at, last_used_at, expires_at, hard_expires_at) VALUES(?,?,?,?,?,?,?)`,
		hashToken(tok), userID, kind, ms(now), ms(now), ms(exp), ms(hard)); err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return tok, ms(exp), nil
}

type tokenInfo struct {
	user user
	kind string
	key  string // hash del token
}

// lookupToken valida un token: existe, no caducó (deslizante y tope), el usuario sigue activo.
// Renueva la ventana deslizante de las sesiones (como mucho una escritura por minuto).
func lookupToken(ctx context.Context, db *sql.DB, tok string, now time.Time) (tokenInfo, bool) {
	if tok == "" || len(tok) > 256 {
		return tokenInfo{}, false
	}
	key := hashToken(tok)
	var ti tokenInfo
	var last, exp, hard int64
	err := db.QueryRowContext(ctx,
		`SELECT t.kind, t.last_used_at, t.expires_at, t.hard_expires_at,
		        u.id, u.username, u.role, u.created_at, u.disabled_at
		 FROM auth_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ?`, key).
		Scan(&ti.kind, &last, &exp, &hard, &ti.user.ID, &ti.user.Username, &ti.user.Role, &ti.user.CreatedAt, &ti.user.DisabledAt)
	if err != nil {
		return tokenInfo{}, false
	}
	n := ms(now)
	if n >= exp || n >= hard {
		_, _ = db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE token_hash = ?`, key)
		return tokenInfo{}, false
	}
	if ti.user.DisabledAt != 0 {
		return tokenInfo{}, false
	}
	if ti.kind == kindSession && n-last >= touchInterval.Milliseconds() {
		newExp := n + sessionIdle.Milliseconds()
		if newExp > hard {
			newExp = hard
		}
		_, _ = db.ExecContext(ctx, `UPDATE auth_tokens SET last_used_at = ?, expires_at = ? WHERE token_hash = ?`, n, newExp, key)
	}
	ti.key = key
	return ti, true
}

func deleteUserTokens(ctx context.Context, db *sql.DB, userID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE user_id = ?`, userID)
	return err
}

func countUsers(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}
