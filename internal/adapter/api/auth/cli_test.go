package auth

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func runCLI(e *env, stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := RunUserCLI(args, e.db, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCLIUsuarios(t *testing.T) {
	e := newEnv(t, Options{})
	// En el CLI se usan los parámetros reales (cuestan ~100 ms cada uno).
	code, out, errOut := runCLI(e, "clave-del-cli-123\n", "user", "add", "carla", "--password-stdin")
	require.Equal(t, 0, code, errOut)
	require.Contains(t, out, "carla")
	require.NotContains(t, out+errOut, "clave-del-cli-123")
	var h string
	require.NoError(t, e.db.QueryRow(`SELECT password_hash FROM users WHERE username='carla'`).Scan(&h))
	require.True(t, strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$"))

	// El e.s.params del entorno es barato, pero verifyPassword lee los parámetros de la cadena.
	require.Equal(t, 200, e.login("carla", "clave-del-cli-123", "").Code)

	// Duplicado, nombre inválido, clave corta.
	code, _, errOut = runCLI(e, "clave-del-cli-123\n", "add", "CARLA", "--password-stdin")
	require.Equal(t, 1, code)
	require.Contains(t, errOut, "Ya existe")
	code, _, _ = runCLI(e, "clave-del-cli-123\n", "add", "local", "--password-stdin")
	require.Equal(t, 1, code)
	code, _, errOut = runCLI(e, "corta\n", "add", "dani", "--password-stdin")
	require.Equal(t, 1, code)
	require.Contains(t, errOut, "12 caracteres")
	require.NotContains(t, errOut, "corta")
	code, _, _ = runCLI(e, "", "add", "dani", "--password-stdin")
	require.Equal(t, 1, code)

	tok := e.token("carla", "clave-del-cli-123")
	code, _, errOut = runCLI(e, "otra-clave-del-cli-9\r\n", "passwd", "carla", "--password-stdin")
	require.Equal(t, 0, code, errOut)
	require.Equal(t, 401, e.do("GET", "/api/v1/auth/me", tok, "", nil).Code, "passwd cierra las sesiones")
	require.Equal(t, 200, e.login("carla", "otra-clave-del-cli-9", "").Code)
	code, _, _ = runCLI(e, "otra-clave-del-cli-9\n", "passwd", "nadie", "--password-stdin")
	require.Equal(t, 1, code)

	code, out, _ = runCLI(e, "", "list")
	require.Equal(t, 0, code)
	require.Contains(t, out, "carla")
	require.Contains(t, out, "activo")
	require.NotContains(t, out, "argon2")

	tok = e.token("carla", "otra-clave-del-cli-9")
	code, _, _ = runCLI(e, "", "disable", "carla")
	require.Equal(t, 0, code)
	require.Equal(t, 401, e.do("GET", "/api/v1/auth/me", tok, "", nil).Code)
	require.Equal(t, 401, e.login("carla", "otra-clave-del-cli-9", "").Code)
	_, out, _ = runCLI(e, "", "list")
	require.Contains(t, out, "desactivado")
	code, _, _ = runCLI(e, "", "enable", "carla")
	require.Equal(t, 0, code)
	require.Equal(t, 200, e.login("carla", "otra-clave-del-cli-9", "").Code)

	// Uso.
	for _, a := range [][]string{{}, {"nada"}, {"add"}, {"disable"}, {"add", "x", "--raro"}} {
		code, _, _ = runCLI(e, "", a...)
		require.Equal(t, 2, code, a)
	}
}
