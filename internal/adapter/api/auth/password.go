package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLen es el mínimo de caracteres de una clave (docs/14 §3).
const MinPasswordLen = 12

const maxPasswordBytes = 1024

type argonParams struct {
	m, t    uint32 // memoria en KiB, pasadas
	p       uint8
	saltLen uint32
	keyLen  uint32
}

// defaultParams: m=64 MiB, t=3, p=2, sal de 16 bytes, hash de 32 (docs/14 §3).
var defaultParams = argonParams{m: 64 * 1024, t: 3, p: 2, saltLen: 16, keyLen: 32}

var errBadHash = errors.New("auth: cadena de clave no válida")

func validatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLen {
		return fmt.Errorf("La clave debe tener al menos %d caracteres.", MinPasswordLen)
	}
	if len(pw) > maxPasswordBytes {
		return errors.New("La clave es demasiado larga.")
	}
	return nil
}

// hashPassword devuelve la cadena PHC: $argon2id$v=19$m=…,t=…,p=…$sal$hash.
func hashPassword(pw string, p argonParams) (string, error) {
	salt := make([]byte, p.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, p.t, p.m, p.p, p.keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.m, p.t, p.p, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func parseHash(s string) (p argonParams, salt, key []byte, err error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, nil, nil, errBadHash
	}
	var v int
	if _, e := fmt.Sscanf(parts[2], "v=%d", &v); e != nil || v != argon2.Version {
		return p, nil, nil, errBadHash
	}
	var par uint32
	if _, e := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.m, &p.t, &par); e != nil || par == 0 || par > 32 {
		return p, nil, nil, errBadHash
	}
	p.p = uint8(par)
	// Cotas: una base manipulada no debe poder agotar la memoria del servidor.
	if p.m < 8 || p.m > 512*1024 || p.t == 0 || p.t > 16 {
		return p, nil, nil, errBadHash
	}
	enc := base64.RawStdEncoding
	if salt, err = enc.DecodeString(parts[4]); err != nil || len(salt) < 8 || len(salt) > 64 {
		return p, nil, nil, errBadHash
	}
	if key, err = enc.DecodeString(parts[5]); err != nil || len(key) < 16 || len(key) > 64 {
		return p, nil, nil, errBadHash
	}
	p.saltLen, p.keyLen = uint32(len(salt)), uint32(len(key))
	return p, salt, key, nil
}

// verifyPassword compara en tiempo constante. needsRehash indica parámetros más débiles que los actuales.
func verifyPassword(pw, enc string, current argonParams) (ok, needsRehash bool) {
	p, salt, key, err := parseHash(enc)
	if err != nil {
		return false, false
	}
	got := argon2.IDKey([]byte(pw), salt, p.t, p.m, p.p, p.keyLen)
	if subtle.ConstantTimeCompare(got, key) != 1 {
		return false, false
	}
	return true, p.m < current.m || p.t < current.t || p.p < current.p
}

const passAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// generatePassword devuelve una clave aleatoria (sin caracteres ambiguos).
func generatePassword(n int) (string, error) {
	max := big.NewInt(int64(len(passAlphabet)))
	b := make([]byte, n)
	for i := range b {
		x, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = passAlphabet[x.Int64()]
	}
	return string(b), nil
}

// randomToken devuelve 32 bytes aleatorios en base64url (sin relleno).
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
