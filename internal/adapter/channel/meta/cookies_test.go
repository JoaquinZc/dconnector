package meta

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"dconnector/internal/core/port"
)

const (
	secretXS = "SECRETO-xs-12345"
	secretIG = "SECRETO-sessionid-98765"
)

func TestParseCookiesJSONArray(t *testing.T) {
	c, err := ParseCookies(`[{"domain":".messenger.com","name":"c_user","value":"1000123"},{"name":"xs","value":"` + secretXS + `"},{"name":"datr","value":"d1"},{"name":"vacia","value":""},{"value":"sinnombre"}]`)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"c_user": "1000123", "xs": secretXS, "datr": "d1"}, c)
}

func TestParseCookiesJSONObject(t *testing.T) {
	c, err := ParseCookies(`{"c_user": "1000123", "xs": "` + secretXS + `", "datr": "d1", "n": 42, "x": null}`)
	require.NoError(t, err)
	assert.Equal(t, "1000123", c["c_user"])
	assert.Equal(t, "42", c["n"])
	_, has := c["x"]
	assert.False(t, has)
}

func TestParseCookiesHeader(t *testing.T) {
	c, err := ParseCookies(`c_user=1000123; xs="` + secretXS + `"; datr=d1;  ; sb=a=b`)
	require.NoError(t, err)
	assert.Equal(t, secretXS, c["xs"], "se quitan las comillas")
	assert.Equal(t, "a=b", c["sb"], "el valor puede llevar «=»")

	c, err = ParseCookies("Cookie: c_user=1; xs=2; datr=3")
	require.NoError(t, err)
	assert.Equal(t, "1", c["c_user"])
	assert.Equal(t, "2", c["xs"])
}

func TestParseCookiesCurl(t *testing.T) {
	c, err := ParseCookies(`curl 'https://www.messenger.com/api' -H 'accept: */*' -H 'cookie: c_user=1000123; xs=` + secretXS + `; datr=d1' --compressed`)
	require.NoError(t, err)
	assert.Equal(t, "1000123", c["c_user"])
	c, err = ParseCookies(`curl 'https://www.instagram.com/' -b 'sessionid=s; ds_user_id=7; csrftoken=t'`)
	require.NoError(t, err)
	assert.Equal(t, "7", c["ds_user_id"])
}

func TestParseCookiesErrors(t *testing.T) {
	for _, in := range []string{"", "   ", "[no es json", "{no es json", "esto no tiene cookies", "[]", "{}"} {
		_, err := ParseCookies(in)
		require.Error(t, err, in)
		assert.ErrorIs(t, err, port.ErrInvalid)
	}
}

func TestMissingCookiesPerPlatform(t *testing.T) {
	assert.Equal(t, []string{"xs", "datr"}, MissingCookies(PlatformMessenger, map[string]string{"c_user": "1"}))
	assert.Empty(t, MissingCookies(PlatformMessenger, map[string]string{"c_user": "1", "xs": "2", "datr": "3"}))
	assert.Equal(t, []string{"sessionid", "ds_user_id", "csrftoken"}, MissingCookies(PlatformInstagram, map[string]string{"c_user": "1", "xs": "2", "datr": "3"}))
	assert.Equal(t, []string{"csrftoken"}, MissingCookies(PlatformInstagram, map[string]string{"sessionid": "s", "ds_user_id": "7", "mid": "m"}))
	assert.Equal(t, []string{"xs"}, MissingCookies(PlatformMessenger, map[string]string{"c_user": "1", "xs": "  ", "datr": "3"}), "un valor en blanco cuenta como faltante")
}

func TestValidateCookies(t *testing.T) {
	c, err := ValidateCookies(PlatformMessenger, "c_user=1000123; xs="+secretXS+"; datr=d1")
	require.NoError(t, err)
	assert.Equal(t, "1000123", UserID(PlatformMessenger, c))

	_, err = ValidateCookies(PlatformMessenger, "c_user=1000123; datr=d1")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Contains(t, err.Error(), "xs", "dice cuál falta")
	assert.NotContains(t, err.Error(), "1000123", "nunca incluye valores")

	_, err = ValidateCookies(PlatformInstagram, "c_user=1000123; xs="+secretXS+"; datr=d1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sessionid")
	assert.Contains(t, err.Error(), "ds_user_id")
	assert.Contains(t, err.Error(), "csrftoken")
	assert.NotContains(t, err.Error(), secretXS)

	_, err = ValidateCookies(PlatformInstagram, "sessionid="+secretIG+"; ds_user_id=abc; csrftoken=t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ds_user_id")
	assert.NotContains(t, err.Error(), secretIG)
	assert.NotContains(t, err.Error(), "abc")

	c, err = ValidateCookies(PlatformInstagram, `[{"name":"sessionid","value":"s"},{"name":"ds_user_id","value":"7"},{"name":"csrftoken","value":"t"},{"name":"mid","value":"m"}]`)
	require.NoError(t, err)
	assert.Equal(t, "7", UserID(PlatformInstagram, c))
	assert.Equal(t, "ds_user_id", UserCookie(PlatformInstagram))
	assert.Equal(t, "c_user", UserCookie(PlatformMessenger))
}

func TestHeaderAndNamesAreStable(t *testing.T) {
	c := map[string]string{"xs": "2", "c_user": "1", "datr": "3"}
	assert.Equal(t, "c_user=1; datr=3; xs=2", Header(c))
	assert.Equal(t, []string{"c_user", "datr", "xs"}, NamesOnly(c))
}

func openKV(t *testing.T) *KV {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "account.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	kv := NewKV(db)
	require.NoError(t, kv.Ensure(context.Background()))
	return kv
}

func TestCookiesPersistInKV(t *testing.T) {
	ctx := context.Background()
	kv := openKV(t)
	got, err := LoadCookies(ctx, kv)
	require.NoError(t, err)
	assert.Nil(t, got, "sin sesión guardada")

	want := map[string]string{"c_user": "1", "xs": secretXS, "datr": "d"}
	require.NoError(t, SaveCookies(ctx, kv, want))
	got, err = LoadCookies(ctx, kv)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// Wipe borra solo lo que empieza por «meta.».
	require.NoError(t, kv.Set(ctx, "tg.session", []byte("x")))
	require.NoError(t, kv.Set(ctx, KeyPlatform, []byte("messenger-lite")))
	require.NoError(t, kv.Wipe(ctx))
	got, _ = LoadCookies(ctx, kv)
	assert.Nil(t, got)
	_, ok, _ := kv.Get(ctx, KeyPlatform)
	assert.False(t, ok)
	_, ok, _ = kv.Get(ctx, "tg.session")
	assert.True(t, ok, "las claves de otros canales no se tocan")

	require.NoError(t, kv.Set(ctx, KeyCookies, []byte("{roto")))
	_, err = LoadCookies(ctx, kv)
	require.Error(t, err)
}
