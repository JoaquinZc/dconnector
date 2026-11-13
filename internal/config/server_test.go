package config

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestParseServerDefaultsAndPrecedence(t *testing.T) {
	c, err := ParseServer(nil, env(nil), nil)
	require.NoError(t, err)
	require.Equal(t, DefaultListen, c.Listen)
	require.Equal(t, "admin", c.AdminUser)

	c, err = ParseServer([]string{"--listen", "0.0.0.0:9000"}, env(map[string]string{"DCONNECTOR_LISTEN": "1.2.3.4:1", "DCONNECTOR_VERBOSE": "true"}), nil)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9000", c.Listen) // el indicador gana
	require.True(t, c.Verbose)                 // la variable gana al valor por defecto
}

func TestParseServerValidation(t *testing.T) {
	_, err := ParseServer([]string{"--tls-cert", "a.pem"}, env(nil), nil)
	require.Error(t, err)
	_, err = ParseServer([]string{"--service", "x"}, env(nil), nil)
	require.Error(t, err)
}

func TestParseServerSubcommandArgs(t *testing.T) {
	c, err := ParseServer([]string{"--data", "x", "user", "add", "ana", "--password-stdin"}, env(nil), nil)
	require.NoError(t, err)
	require.Equal(t, "x", c.Data)
	require.Equal(t, []string{"user", "add", "ana", "--password-stdin"}, c.Args)
}

func TestParseServerNoTLSConflictsWithCert(t *testing.T) {
	_, err := ParseServer([]string{"--no-tls", "--tls-cert", "a", "--tls-key", "b"}, env(nil), nil)
	require.Error(t, err)
}

func TestOrigins(t *testing.T) {
	require.Empty(t, Server{}.Origins())
	got := Server{AllowOrigin: " https://a.example/ ,, http://b:8080"}.Origins()
	require.Equal(t, []string{"https://a.example", "http://b:8080"}, got)
}

func TestServiceArgsNeverIncludeSecrets(t *testing.T) {
	c, err := ParseServer([]string{"--data", `C:\d`, "--listen", "0.0.0.0:8443", "--tls-cert", "c.pem", "--tls-key", "k.pem",
		"--admin-user", "root", "--verbose", "--allow-origin", "https://x"},
		env(map[string]string{"DCONNECTOR_LOCAL_TOKEN": "secreto", "DCONNECTOR_ADMIN_PASSWORD": "otro"}), nil)
	require.NoError(t, err)
	a := c.ServiceArgs()
	require.Equal(t, []string{"--data", `C:\d`, "--listen", "0.0.0.0:8443", "--tls-cert", "c.pem", "--tls-key", "k.pem",
		"--verbose", "--admin-user", "root", "--allow-origin", "https://x"}, a)
	for _, s := range a {
		require.NotContains(t, s, "secreto")
		require.NotContains(t, s, "otro")
	}
	// Los argumentos guardados se vuelven a interpretar igual.
	back, err := ParseServer(a, env(nil), nil)
	require.NoError(t, err)
	require.Equal(t, c.Listen, back.Listen)
	require.Equal(t, c.TLSKey, back.TLSKey)
	require.Equal(t, c.AdminUser, back.AdminUser)
}

func TestEnvVariablesAllRecognised(t *testing.T) {
	c, err := ParseServer(nil, env(map[string]string{
		"DCONNECTOR_LISTEN": "0.0.0.0:1", "DCONNECTOR_DATA": "/d", "DCONNECTOR_TLS_CERT": "c", "DCONNECTOR_TLS_KEY": "k",
		"DCONNECTOR_INSECURE": "1", "DCONNECTOR_ADMIN_USER": "root", "DCONNECTOR_ALLOW_ORIGIN": "https://o",
		"DCONNECTOR_LOCAL_TOKEN": "t",
	}), nil)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:1", c.Listen)
	require.Equal(t, "/d", c.Data)
	require.Equal(t, "c", c.TLSCert)
	require.True(t, c.Insecure)
	require.Equal(t, "root", c.AdminUser)
	require.Equal(t, "t", c.LocalToken)
}

func TestHealthcheckAcceptsFlagsAfterSubcommand(t *testing.T) {
	c, err := ParseServer([]string{"healthcheck", "--listen", "127.0.0.1:9", "--no-tls"}, func(string) string { return "" }, io.Discard)
	require.NoError(t, err)
	require.Equal(t, []string{"healthcheck"}, c.Args)
	require.Equal(t, "127.0.0.1:9", c.Listen)
	require.True(t, c.NoTLS)
}

func TestNoAccountsFlagAndEnv(t *testing.T) {
	c, err := ParseServer(nil, func(k string) string {
		if k == "DCONNECTOR_NO_ACCOUNTS" {
			return "1"
		}
		return ""
	}, io.Discard)
	require.NoError(t, err)
	require.True(t, c.NoAccounts)
	c, err = ParseServer([]string{"--no-accounts"}, func(string) string { return "" }, io.Discard)
	require.NoError(t, err)
	require.True(t, c.NoAccounts)
	require.Contains(t, c.ServiceArgs(), "--no-accounts")
	c, _ = ParseServer(nil, func(string) string { return "" }, io.Discard)
	require.False(t, c.NoAccounts)
}
