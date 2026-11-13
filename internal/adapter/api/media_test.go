package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/paths"
)

func TestMediaServedWithRange(t *testing.T) {
	e := newEnv(t)
	acc, ad := e.newAccount()
	ad.SetMedia("m1", []byte("0123456789"))
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: &model.Message{
		NativeID: "m1", ChatNativeID: "593999", SenderNativeID: "593999", Timestamp: time.Now().UnixMilli(),
		Kind: model.KindAudio, Media: &model.Media{ID: "m1", Mime: "audio/ogg", FileName: "n.ogg"},
	}, Chat: &model.Chat{NativeID: "593999", Kind: model.ChatDirect, Title: "Ana"}})
	require.Eventually(t, func() bool {
		var cs []model.Chat
		e.ok(&cs, "Inbox", "ListChats", model.ChatFilter{})
		return len(cs) == 1
	}, 5*time.Second, 20*time.Millisecond)

	base := "/api/v1/media/" + string(acc.ID) + "/m1"
	code, body, h := e.raw("GET", base, tokUser, nil, nil)
	require.Equal(t, 200, code, string(body))
	require.Equal(t, "0123456789", string(body))
	require.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	require.Equal(t, "sandbox", h.Get("Content-Security-Policy"))
	require.Equal(t, "private, max-age=3600", h.Get("Cache-Control"))

	code, body, h = e.raw("GET", base, tokUser, map[string]string{"Range": "bytes=2-4"}, nil)
	require.Equal(t, http.StatusPartialContent, code)
	require.Equal(t, "234", string(body))
	require.Equal(t, "bytes 2-4/10", h.Get("Content-Range"))

	// Con media-token.
	code, body, _ = e.raw("GET", base+"?token="+tokMedia, "", nil, nil)
	require.Equal(t, 200, code)
	require.Equal(t, "0123456789", string(body))

	// Un medio que no existe, 404 JSON; una cuenta que no existe, 404.
	code, body, _ = e.raw("GET", "/api/v1/media/"+string(acc.ID)+"/otro", tokUser, nil, nil)
	require.Equal(t, 404, code)
	c, _ := errCode(t, body)
	require.Equal(t, "not_found", c)
	code, _, _ = e.raw("GET", "/api/v1/media/nada/m1", tokUser, nil, nil)
	require.Equal(t, 404, code)

	// Segmentos peligrosos: 400.
	for _, p := range []string{
		"/api/v1/media/" + string(acc.ID) + "/..",
		"/api/v1/media/..//m1",
		"/api/v1/media/" + string(acc.ID) + "/%2e%2e",
		"/api/v1/media/" + string(acc.ID) + "/..%2f..%2fapp.db",
		"/api/v1/media/" + string(acc.ID) + "/a%5c..%5cb",
		"/api/v1/media/" + string(acc.ID) + "/a/b",
		"/api/v1/media/" + string(acc.ID) + "/",
		"/api/v1/media/" + string(acc.ID),
		"/api/v1/media/" + string(acc.ID) + "/a%00b",
		"/api/v1/media/%2e%2e/m1",
	} {
		code, body, _ = e.raw("GET", p, tokUser, nil, nil)
		require.Equal(t, 400, code, p+": "+string(body))
		c, _ = errCode(t, body)
		require.Equal(t, "invalid", c, p)
	}
	code, _, _ = e.raw("POST", base, tokUser, nil, nil)
	require.Equal(t, 405, code)
}

func TestAvatar(t *testing.T) {
	e := newEnv(t)
	acc := model.AccountID("wa-01jtest")
	dir := paths.AvatarDir(acc)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("0123456789"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(paths.DataDir(), "secreto.txt"), []byte("no"), 0o644))

	code, body, h := e.raw("GET", "/api/v1/avatar/wa-01jtest/a.jpg", tokUser, nil, nil)
	require.Equal(t, 200, code)
	require.Equal(t, "0123456789", string(body))
	require.Equal(t, "sandbox", h.Get("Content-Security-Policy"))
	code, body, _ = e.raw("GET", "/api/v1/avatar/wa-01jtest/a.jpg", tokUser, map[string]string{"Range": "bytes=1-2"}, nil)
	require.Equal(t, http.StatusPartialContent, code)
	require.Equal(t, "12", string(body))
	code, _, _ = e.raw("GET", "/api/v1/avatar/wa-01jtest/a.jpg?token="+tokMedia, "", nil, nil)
	require.Equal(t, 200, code)

	code, _, _ = e.raw("GET", "/api/v1/avatar/wa-01jtest/nada.jpg", tokUser, nil, nil)
	require.Equal(t, 404, code)
	// Un directorio no se sirve.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	code, _, _ = e.raw("GET", "/api/v1/avatar/wa-01jtest/sub", tokUser, nil, nil)
	require.Equal(t, 404, code)

	for _, p := range []string{
		"/api/v1/avatar/wa-01jtest/..%2f..%2fsecreto.txt",
		"/api/v1/avatar/wa-01jtest/..",
		"/api/v1/avatar/..%2f../secreto.txt",
		"/api/v1/avatar/wa-01jtest/%2e%2e%2fsecreto.txt",
		"/api/v1/avatar/wa-01jtest/..%5csecreto.txt",
		"/api/v1/avatar/wa-01jtest/a.jpg/x",
	} {
		code, body, _ = e.raw("GET", p, tokUser, nil, nil)
		require.Equal(t, 400, code, p+": "+string(body))
	}
}

// Un medio llamado index.html no debe provocar la redirección de ServeFile.
func TestMediaIndexHTMLNoRedirect(t *testing.T) {
	e := newEnv(t)
	acc := model.AccountID("wa-01jtest")
	dir := paths.AvatarDir(acc)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<script>alert(1)</script>"), 0o644))
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1/avatar/wa-01jtest/index.html", nil)
	req.Header.Set("Authorization", "Bearer "+tokUser)
	resp, err := cl.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "sandbox", resp.Header.Get("Content-Security-Policy"))
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
}
