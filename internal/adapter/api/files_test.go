package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/paths"
)

var pngBytes, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+strings.ReplaceAll(filename, `"`, "%22")+`"`)
	h.Set("Content-Type", "application/octet-stream")
	w, err := mw.CreatePart(h)
	require.NoError(t, err)
	_, _ = w.Write(data)
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

func (e *env) upload(token, filename string, data []byte) (int, []byte) {
	e.t.Helper()
	body, ct := multipartBody(e.t, "file", filename, data)
	code, b, _ := e.raw("POST", "/api/v1/files", token, map[string]string{"Content-Type": ct}, body)
	return code, b
}

func (e *env) uploadOK(token, filename string, data []byte) uploadInfo {
	e.t.Helper()
	code, b := e.upload(token, filename, data)
	require.Equal(e.t, 200, code, string(b))
	var info uploadInfo
	require.NoError(e.t, json.Unmarshal(b, &info))
	return info
}

func TestUploadAndDownload(t *testing.T) {
	e := newEnv(t)
	info := e.uploadOK(tokUser, "foto.jpg", pngBytes)
	require.Len(t, info.ID, 26)
	require.Equal(t, "foto.jpg", info.Name)
	require.Equal(t, "image/png", info.Mime, "el tipo sale del contenido, no de la extensión")
	require.EqualValues(t, len(pngBytes), info.Size)

	// Guardado bajo <data>/uploads/<id>/.
	st, err := os.Stat(filepath.Join(paths.DataDir(), "uploads", info.ID))
	require.NoError(t, err)
	require.True(t, st.IsDir())

	code, body, h := e.raw("GET", "/api/v1/files/"+info.ID, tokUser, nil, nil)
	require.Equal(t, 200, code)
	require.Equal(t, pngBytes, body)
	require.Equal(t, "image/png", h.Get("Content-Type"))
	require.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	require.Equal(t, "sandbox", h.Get("Content-Security-Policy"))
	require.Equal(t, "no-store", h.Get("Cache-Control"))

	// Con media-token por ?token=, y en minúsculas (mismo ULID).
	code, body, _ = e.raw("GET", "/api/v1/files/"+strings.ToLower(info.ID)+"?token="+tokMedia, "", nil, nil)
	require.Equal(t, 200, code)
	require.Equal(t, pngBytes, body)

	// Range.
	code, body, _ = e.raw("GET", "/api/v1/files/"+info.ID, tokUser, map[string]string{"Range": "bytes=0-3"}, nil)
	require.Equal(t, http.StatusPartialContent, code)
	require.Equal(t, pngBytes[:4], body)

	// Inexistente (ULID válido) = 404; mal formado = 400.
	code, body, _ = e.raw("GET", "/api/v1/files/01J00000000000000000000000", tokUser, nil, nil)
	require.Equal(t, 404, code)
	c, _ := errCode(t, body)
	require.Equal(t, "not_found", c)
	// POST en /files/{id}: 405.
	code, _, _ = e.raw("POST", "/api/v1/files/"+info.ID, tokUser, nil, nil)
	require.Equal(t, 405, code)
}

func TestUploadMimeByExtensionWhenContentIsGeneric(t *testing.T) {
	e := newEnv(t)
	info := e.uploadOK(tokUser, "datos.csv", []byte("a,b\n1,2\n"))
	require.Equal(t, "text/csv", info.Mime)
	info = e.uploadOK(tokUser, "raro.bin", []byte{0, 1, 2, 3, 4})
	require.Equal(t, "application/octet-stream", info.Mime)
	// Un nombre malicioso nunca es una ruta: se guarda con un nombre limpio dentro de la carpeta del id.
	info = e.uploadOK(tokUser, `..\..\..\evil:.png`, pngBytes)
	require.Equal(t, "evil:.png", info.Name)
	ents, err := os.ReadDir(filepath.Join(paths.DataDir(), "uploads", info.ID))
	require.NoError(t, err)
	for _, en := range ents {
		require.NotContains(t, en.Name(), "..")
		require.NotContains(t, en.Name(), ":")
	}
}

func TestUploadErrors(t *testing.T) {
	e := newEnv(t)
	// Sin multipart.
	code, body, _ := e.raw("POST", "/api/v1/files", tokUser, map[string]string{"Content-Type": "application/json"}, strings.NewReader("{}"))
	require.Equal(t, 400, code)
	c, _ := errCode(t, body)
	require.Equal(t, "invalid", c)
	// Sin el campo file.
	b, ct := multipartBody(t, "otro", "x.png", pngBytes)
	code, body, _ = e.raw("POST", "/api/v1/files", tokUser, map[string]string{"Content-Type": ct}, b)
	require.Equal(t, 400, code, string(body))
	// GET en /files: 405.
	code, _, _ = e.raw("GET", "/api/v1/files", tokUser, nil, nil)
	require.Equal(t, 405, code)

	// Demasiado grande (límite bajado para la prueba).
	old := maxUploadSize
	maxUploadSize = 1024
	t.Cleanup(func() { maxUploadSize = old })
	code, body = e.upload(tokUser, "grande.bin", bytes.Repeat([]byte{7}, 4096))
	require.Equal(t, 413, code, string(body))
	c, _ = errCode(t, body)
	require.Equal(t, "too_large", c)
	// Y no deja restos.
	ents, _ := os.ReadDir(filepath.Join(paths.DataDir(), "uploads"))
	require.Empty(t, ents)
}

func TestUploadQuotaPerUser(t *testing.T) {
	e := newEnv(t)
	oldT := maxUploadsTotal
	maxUploadsTotal = 3000
	t.Cleanup(func() { maxUploadsTotal = oldT })
	e.uploadOK(tokUser, "a.bin", bytes.Repeat([]byte{1}, 2000))
	code, body := e.upload(tokUser, "b.bin", bytes.Repeat([]byte{1}, 2000))
	require.Equal(t, 413, code, string(body))
	// Otro usuario tiene su propio cupo.
	e.uploadOK(tokLocal, "c.bin", bytes.Repeat([]byte{1}, 2000))
}

func TestUploadSweep(t *testing.T) {
	e := newEnv(t)
	info := e.uploadOK(tokUser, "a.png", pngBytes)
	e.srv.files.sweep()
	_, err := os.Stat(filepath.Join(paths.DataDir(), "uploads", info.ID))
	require.NoError(t, err, "no caduca antes de 24 h")

	e.srv.files.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	e.srv.files.sweep()
	_, err = os.Stat(filepath.Join(paths.DataDir(), "uploads", info.ID))
	require.True(t, os.IsNotExist(err))
	code, _, _ := e.raw("GET", "/api/v1/files/"+info.ID, tokUser, nil, nil)
	require.Equal(t, 404, code)

	// Restos a medias y sueltos: se limpian al arrancar un servidor nuevo.
	dir := filepath.Join(paths.DataDir(), "uploads")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "huérfano"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suelto.txt"), []byte("x"), 0o600))
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "huérfano"), old, old))
	n, err := New(Config{Core: e.core, Auth: e.auth})
	require.NoError(t, err)
	defer n.Close()
	_, err = os.Stat(filepath.Join(dir, "huérfano"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "suelto.txt"))
	require.True(t, os.IsNotExist(err))
}

// ---------------------------------------------------------------------------------------------
// upload:// en los argumentos de RPC (docs/14 §4.4)

func TestUploadSchemeInMessagingSend(t *testing.T) {
	e := newEnv(t)
	acc, ad := e.newAccount()
	var chat model.Chat
	e.ok(&chat, "Inbox", "OpenChat", string(acc.ID), "593111")
	info := e.uploadOK(tokUser, "foto.png", pngBytes)

	var sent model.Message
	e.ok(&sent, "Messaging", "Send", string(chat.Key), model.Outgoing{Kind: model.KindImage, FilePath: "upload://" + info.ID, Text: "mira"})
	got := ad.Sent()
	require.Len(t, got, 1)
	want := filepath.Join(paths.DataDir(), "uploads", info.ID, "foto.png")
	require.Equal(t, want, got[0].FilePath)
	require.Equal(t, "foto.png", got[0].FileName)
	require.Equal(t, "image/png", got[0].Mime)

	// Rutas que un cliente remoto no puede usar.
	for name, p := range map[string]string{
		"absoluta":       filepath.Join(paths.DataDir(), "app.db"),
		"relativa":       "../app.db",
		"dentro uploads": want,
		"id roto":        "upload://../app.db",
		"id corto":       "upload://abc",
		"otro id":        "upload://01J00000000000000000000000",
		"traversal":      "upload://" + info.ID + "/../../app.db",
	} {
		code, body, _ := e.call(tokUser, "Messaging", "Send", string(chat.Key), model.Outgoing{Kind: model.KindImage, FilePath: p})
		require.Equal(t, 400, code, name+": "+string(body))
		c, _ := errCode(t, body)
		require.Equal(t, "invalid", c, name)
	}
	require.Len(t, ad.Sent(), 1, "ninguna de las rutas rechazadas llegó al canal")

	// En modo local se aceptan rutas absolutas.
	abs := filepath.Join(t.TempDir(), "local.png")
	require.NoError(t, os.WriteFile(abs, pngBytes, 0o644))
	code, body, _ := e.call(tokLocal, "Messaging", "Send", string(chat.Key), model.Outgoing{Kind: model.KindImage, FilePath: abs})
	require.Equal(t, 200, code, string(body))
	require.Equal(t, abs, ad.Sent()[1].FilePath)
	// ...pero no relativas.
	code, _, _ = e.call(tokLocal, "Messaging", "Send", string(chat.Key), model.Outgoing{Kind: model.KindImage, FilePath: "local.png"})
	require.Equal(t, 400, code)
	// Y el local también puede usar upload://.
	info2 := e.uploadOK(tokLocal, "otra.png", pngBytes)
	code, body, _ = e.call(tokLocal, "Messaging", "Send", string(chat.Key), model.Outgoing{Kind: model.KindImage, FilePath: "upload://" + info2.ID})
	require.Equal(t, 200, code, string(body))
}

func TestUploadSchemeInOtherFileArguments(t *testing.T) {
	e := newEnv(t)
	acc, _ := e.newAccount()
	csv := e.uploadOK(tokUser, "contactos.csv", []byte("nombre,telefono\nAna,+593999111222\n"))

	// Contacts.ImportCSV: el primer argumento es una ruta.
	code, body, _ := e.call(tokUser, "Contacts", "ImportCSV", "upload://"+csv.ID, map[string]any{"name": 0, "phone": 1, "hasHeader": true})
	require.NotEqual(t, 500, code, string(body))
	require.NotContains(t, string(body), paths.DataDir())
	code, body, _ = e.call(tokUser, "Contacts", "ImportCSV", filepath.Join(paths.DataDir(), "app.db"), map[string]any{})
	require.Equal(t, 400, code, string(body))
	code, _, _ = e.call(tokUser, "Contacts", "ImportCSV", "../../etc/passwd", map[string]any{})
	require.Equal(t, 400, code)

	// Campaign.FilePath, OutgoingStatus.FilePath y Scheduled.Message.FilePath se validan igual.
	bad := filepath.Join(paths.DataDir(), "app.db")
	code, _, _ = e.call(tokUser, "Campaigns", "Save", model.Campaign{Name: "c", AccountID: acc.ID, FilePath: bad})
	require.Equal(t, 400, code)
	code, _, _ = e.call(tokUser, "Campaigns", "Review", model.Campaign{Name: "c", AccountID: acc.ID, FilePath: bad})
	require.Equal(t, 400, code)
	code, _, _ = e.call(tokUser, "Statuses", "Post", string(acc.ID), model.OutgoingStatus{Kind: model.KindImage, FilePath: bad})
	require.Equal(t, 400, code)
	code, _, _ = e.call(tokUser, "Scheduled", "Save", model.Scheduled{AccountID: acc.ID, Message: &model.Outgoing{Kind: model.KindImage, FilePath: bad}})
	require.Equal(t, 400, code)
	code, _, _ = e.call(tokUser, "Scheduled", "Update", model.Scheduled{AccountID: acc.ID, Message: &model.Outgoing{Kind: model.KindImage, FilePath: bad}})
	require.Equal(t, 400, code)

	// Una campaña con adjunto subido se guarda y su copia queda en campaign-media.
	img := e.uploadOK(tokUser, "banner.png", pngBytes)
	var saved model.Campaign
	code, body, _ = e.call(tokUser, "Campaigns", "Save", model.Campaign{Name: "con adjunto", AccountID: acc.ID, Text: "hola", FilePath: "upload://" + img.ID})
	require.Equal(t, 200, code, string(body))
	require.NoError(t, json.Unmarshal(body, &saved))
	require.True(t, strings.HasPrefix(saved.FilePath, filepath.Join(paths.DataDir(), "campaign-media")), saved.FilePath)
	require.Equal(t, "banner.png", filepath.Base(saved.FilePath))
	// Al editarla, el cliente reenvía la ruta que le devolvió el servidor: está en una carpeta de medios propia.
	saved.Text = "hola 2"
	code, body, _ = e.call(tokUser, "Campaigns", "Save", saved)
	require.Equal(t, 200, code, string(body))
}

func TestInsideManagedMedia(t *testing.T) {
	newEnv(t)
	d := paths.DataDir()
	for _, sub := range []string{"campaign-media", "status-media", "scheduled-media", "uploads", "tmp"} {
		require.NoError(t, os.MkdirAll(filepath.Join(d, sub), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(d, sub, "f.png"), pngBytes, 0o644))
	}
	require.True(t, insideManagedMedia(filepath.Join(d, "campaign-media", "f.png")))
	require.True(t, insideManagedMedia(filepath.Join(d, "status-media", "f.png")))
	require.True(t, insideManagedMedia(filepath.Join(d, "scheduled-media", "f.png")))
	require.False(t, insideManagedMedia(filepath.Join(d, "uploads", "f.png")))
	require.False(t, insideManagedMedia(filepath.Join(d, "tmp", "f.png")))
	require.False(t, insideManagedMedia(filepath.Join(d, "campaign-media", "..", "app.db")))
	require.False(t, insideManagedMedia(filepath.Join(d, "campaign-media")))
	require.False(t, insideManagedMedia("campaign-media/f.png"))
	require.False(t, insideManagedMedia(""))
}

func TestCleanFileName(t *testing.T) {
	for in, want := range map[string]string{
		"foto.jpg":            "foto.jpg",
		`C:\Users\x\foto.jpg`: "foto.jpg",
		"../../a.png":         "a.png",
		"a:b*c?.png":          "a_b_c_.png",
		"":                    "archivo",
		"...":                 "archivo",
		"CON.txt":             "_CON.txt",
		".hidden":             "hidden",
	} {
		require.Equal(t, want, cleanFileName(in), in)
	}
	long := cleanFileName(strings.Repeat("á", 200) + ".jpg")
	require.LessOrEqual(t, len(long), 120)
	require.True(t, strings.HasSuffix(long, ".jpg"))
}
