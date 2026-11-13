package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/paths"
)

// Argumentos hostiles contra TODOS los métodos del catálogo: ninguno debe provocar un pánico (que la capa
// de reflexión convertiría en 500) ni dejar el servidor sin responder.
func TestRPCArgumentosHostilesNoProvocanPanico(t *testing.T) {
	e := newEnv(t)
	var names []string
	for svc, ms := range e.srv.rpc {
		for name := range ms {
			if isDesktopOnly(svc, name) {
				continue
			}
			names = append(names, svc+"/"+name)
		}
	}
	sort.Strings(names)
	require.NotEmpty(t, names)
	for _, n := range names {
		svc, name, _ := strings.Cut(n, "/")
		m := e.srv.rpc[svc][name]
		variants := map[string]string{
			"nulls":   "[" + strings.TrimSuffix(strings.Repeat("null,", len(m.in)), ",") + "]",
			"strings": "[" + strings.TrimSuffix(strings.Repeat(`"\u0000x",`, len(m.in)), ",") + "]",
			"numbers": "[" + strings.TrimSuffix(strings.Repeat("-9223372036854775808,", len(m.in)), ",") + "]",
			"objects": "[" + strings.TrimSuffix(strings.Repeat(`{"filePath":"","steps":[null],"x":{}},`, len(m.in)), ",") + "]",
			"arrays":  "[" + strings.TrimSuffix(strings.Repeat("[null,[],{},1,\"a\"],", len(m.in)), ",") + "]",
		}
		for vn, body := range variants {
			code, resp, _ := e.raw("POST", "/api/v1/rpc/"+n, tokUser, nil, strings.NewReader(body))
			require.NotEqual(t, 500, code, "%s con %s: %s", n, vn, resp)
		}
	}
	code, _, _ := e.raw("GET", "/api/v1/health", "", nil, nil)
	require.Equal(t, 200, code)
}

// El mapa de rutas es exacto: mayúsculas, dobles barras, punto y coma o %2f no saltan a otro método ni
// a uno de escritorio.
func TestRPCRutasNoSeSaltanPorVariaciones(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{
		"/api/v1/rpc/settings/version", "/api/v1/rpc/Settings/version", "/api/v1/rpc//Settings/Version",
		"/api/v1/rpc/Settings//Version", "/api/v1/rpc/Settings/Version/", "/api/v1/rpc/Settings;x/Version",
		"/api/v1/rpc/Settings/Version%00", "/api/v1/RPC/Settings/Version",
		"/api/v1/rpc/Files/PickFiles", "/api/v1/rpc/files/PickFiles", "/api/v1/rpc/Files%2fPickFiles",
		"/api/v1/rpc/Settings/OpenDataDir", "/api/v1/rpc/Settings/openDataDir", "/api/v1/rpc/Settings/Version/../OpenDataDir",
		"/api/v1/rpc/Settings/%2e%2e/Settings/Version",
	} {
		code, body, _ := e.raw("POST", p, tokUser, nil, strings.NewReader("[]"))
		require.Contains(t, []int{404, 400}, code, "%s -> %d %s", p, code, body)
	}
	// Un método no exportado del catálogo no existe.
	for _, ms := range e.srv.rpc {
		for name := range ms {
			require.True(t, name[0] >= 'A' && name[0] <= 'Z', name)
		}
	}
}

func TestCuerpoRPCGrandeYAnidado(t *testing.T) {
	e := newEnv(t)
	big := "[" + strings.Repeat("0,", 600000) + "0]" // > 1 MiB
	code, _, _ := e.raw("POST", "/api/v1/rpc/Inbox/UnreadTotals", tokUser, nil, strings.NewReader(big))
	require.Equal(t, 413, code)
	deep := strings.Repeat("[", 200000) + strings.Repeat("]", 200000)
	code, _, _ = e.raw("POST", "/api/v1/rpc/Inbox/GetChat", tokUser, nil, strings.NewReader(deep))
	require.Equal(t, 400, code)
}

func TestCleanFileNameNombresReservadosConExtensiones(t *testing.T) {
	for _, in := range []string{"CON.tar.gz", "nul.txt.exe", "con .txt", "Aux.", "COM1.a.b", "lpt0.txt", "COM¹.txt", "LPT³", "prn.x"} {
		got := cleanFileName(in)
		require.True(t, strings.HasPrefix(got, "_"), "%q -> %q", in, got)
		require.False(t, isWinReserved(got), "%q -> %q", in, got)
	}
	for _, in := range []string{"console.txt", "nulo.txt", "a.con", "comm1.txt", "informe.pdf"} {
		require.Equal(t, in, cleanFileName(in))
	}
}

func TestRutasRemotasUNCyFlujosNTFS(t *testing.T) {
	for _, p := range []string{`\\host\share\a.png`, `//host/share/a.png`, `\\?\C:\Windows\win.ini`, `\\.\pipe\x`, `\/host/x`, "a\x00b"} {
		require.True(t, unsafeRemotePath(p, false), p)
		require.True(t, unsafeRemotePath(p, true), p)
		require.False(t, insideManagedMedia(p), p)
	}
	require.True(t, unsafeRemotePath(`C:\data\campaign-media\a.png:secreto`, true))
	require.True(t, unsafeRemotePath(`C:\data\a.png::$DATA`, true))
	require.False(t, unsafeRemotePath(`C:\data\campaign-media\a.png`, true))
	require.False(t, unsafeRemotePath(`/var/lib/x/a:b.png`, false))
}

// Un campo FilePath dentro de un mapa también se resuelve (antes se saltaba).
func TestWalkFilePathsRecorreMapas(t *testing.T) {
	e := newEnv(t)
	v := map[string][]model.Outgoing{"a": {{Kind: model.KindDocument, FilePath: "/etc/passwd"}}}
	require.Error(t, e.srv.walkFilePaths(reflect.ValueOf(v), false))
	info := e.uploadOK(tokUser, "x.txt", []byte("hola"))
	v = map[string][]model.Outgoing{"a": {{Kind: model.KindDocument, FilePath: "upload://" + info.ID}}}
	require.NoError(t, e.srv.walkFilePaths(reflect.ValueOf(v), false))
	require.True(t, strings.HasPrefix(v["a"][0].FilePath, paths.DataDir()), v["a"][0].FilePath)
	require.Equal(t, "x.txt", v["a"][0].FileName)
}

// Subidas simultáneas de un mismo usuario no pueden superar el cupo entre todas.
func TestCupoDeSubidasConcurrentes(t *testing.T) {
	newEnv(t)
	oldT := maxUploadsTotal
	maxUploadsTotal = 3000
	t.Cleanup(func() { maxUploadsTotal = oldT })
	u := newUploads(slogDiscard())

	pr, pw := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	var errA error
	go func() { // A: lleva 100 bytes y se queda esperando el resto
		defer wg.Done()
		_, errA = u.store("ana", "a.bin", pr)
	}()
	_, _ = pw.Write(bytes.Repeat([]byte{1}, 100))
	time.Sleep(100 * time.Millisecond)

	// B llega mientras A sigue en curso: A puede ocupar todo el cupo, así que B no cabe.
	_, errB := u.store("ana", "b.bin", bytes.NewReader(bytes.Repeat([]byte{2}, 2500)))
	require.ErrorAs(t, errB, new(quotaError))
	// Otro usuario no se ve afectado.
	_, errC := u.store("bea", "c.bin", bytes.NewReader(bytes.Repeat([]byte{2}, 2500)))
	require.NoError(t, errC)

	_ = pw.Close()
	wg.Wait()
	require.NoError(t, errA)
	require.Empty(t, u.reserved["ana"], "la reserva se libera al terminar")
	// Con A ya guardada (100 bytes) caben 2900 más, no 3000.
	_, errD := u.store("ana", "d.bin", bytes.NewReader(bytes.Repeat([]byte{3}, 2950)))
	require.ErrorAs(t, errD, new(quotaError))
	_, errE := u.store("ana", "e.bin", bytes.NewReader(bytes.Repeat([]byte{3}, 2900)))
	require.NoError(t, errE)
}

func TestWebSocketSeCierraSiElUsuarioYaNoEstaActivo(t *testing.T) {
	e := newEnv(t)
	var off sync.Mutex
	active := true
	e.srv.hub.active = func(string) bool { off.Lock(); defer off.Unlock(); return active }
	e.srv.hub.pingEvery, e.srv.hub.pongWait = 50*time.Millisecond, time.Second
	c, _ := e.connect()
	off.Lock()
	active = false
	off.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			require.Equal(t, websocket.StatusCode(4401), websocket.CloseStatus(err), err.Error())
			return
		}
	}
}

// /files y /media no salen de su carpeta por enlaces simbólicos ni con nombres raros.
func TestAvatarYMediaNoSalenDeSuCarpeta(t *testing.T) {
	e := newEnv(t)
	secret := filepath.Join(e.data, "secreto.txt")
	require.NoError(t, os.WriteFile(secret, []byte("no leer"), 0o600))
	dir := paths.AvatarDir("acc1")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	if err := os.Symlink(secret, filepath.Join(dir, "enlace.jpg")); err == nil {
		code, body, _ := e.raw("GET", "/api/v1/avatar/acc1/enlace.jpg", tokUser, nil, nil)
		require.Equal(t, 404, code, string(body))
	}
	for _, p := range []string{
		"/api/v1/avatar/acc1/..%5csecreto.txt", "/api/v1/avatar/..%2f/secreto.txt", "/api/v1/avatar/acc1/%2e%2e/x",
		"/api/v1/avatar/acc1/a:b", "/api/v1/media/acc1/..", "/api/v1/media/..%5c/x", "/api/v1/media/a/b/c",
	} {
		code, body, _ := e.raw("GET", p, tokUser, nil, nil)
		require.Contains(t, []int{400, 404}, code, "%s: %s", p, body)
		require.NotContains(t, string(body), "no leer")
	}
}

func TestSubidaMultipartConVariasPartesSoloGuardaLaPrimera(t *testing.T) {
	e := newEnv(t)
	b := new(bytes.Buffer)
	b.WriteString("--X\r\nContent-Disposition: form-data; name=\"otro\"\r\n\r\nzzz\r\n")
	b.WriteString("--X\r\nContent-Disposition: form-data; name=\"file\"; filename=\"../../../evil.sh\"\r\n\r\nuno\r\n")
	b.WriteString("--X\r\nContent-Disposition: form-data; name=\"file\"; filename=\"dos.txt\"\r\n\r\ndos\r\n--X--\r\n")
	code, body, _ := e.raw("POST", "/api/v1/files", tokUser, map[string]string{"Content-Type": "multipart/form-data; boundary=X"}, b)
	require.Equal(t, 200, code, string(body))
	var info uploadInfo
	require.NoError(t, json.Unmarshal(body, &info))
	require.Equal(t, "evil.sh", info.Name)
	require.EqualValues(t, 3, info.Size)
	ents, _ := os.ReadDir(filepath.Join(paths.DataDir(), "uploads"))
	require.Len(t, ents, 1)
	_, err := os.Stat(filepath.Join(e.data, "evil.sh"))
	require.True(t, os.IsNotExist(err))
}
