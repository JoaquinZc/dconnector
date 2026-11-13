package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	buildOnce sync.Once
	exePath   string
	buildErr  error
)

// buildServer compila ./cmd/dconnector-server a una carpeta temporal (una vez por ejecución de pruebas).
func buildServer(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "dc-e2e-bin-")
		if err != nil {
			buildErr = err
			return
		}
		name := "dconnector-server"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		exePath = filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", exePath, "./cmd/dconnector-server")
		cmd.Dir = filepath.Join("..", "..", "..")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = err
			t.Logf("go build: %s", out)
		}
	})
	require.NoError(t, buildErr)
	return exePath
}

// cleanEnv: entorno del proceso hijo, aislado de los datos reales (APPDATA y compañía apuntan a la carpeta temporal).
func cleanEnv(data string, extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		if strings.HasPrefix(k, "DCONNECTOR_") || k == "APPDATA" || k == "LOCALAPPDATA" || k == "XDG_CONFIG_HOME" || k == "XDG_DATA_HOME" || k == "HOME" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "DCONNECTOR_DATA="+data, "APPDATA="+data, "LOCALAPPDATA="+data, "XDG_DATA_HOME="+data, "HOME="+data, "USERPROFILE="+data)
}

// runExe ejecuta el servidor y devuelve salida estándar, de errores y código de salida.
func runExe(t *testing.T, data, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(buildServer(t), args...)
	cmd.Env = cleanEnv(data)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	hideAndGroup(cmd)
	done := make(chan error, 1)
	require.NoError(t, cmd.Start())
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), errb.String(), code
	case <-time.After(90 * time.Second):
		killTree(cmd.Process.Pid)
		t.Fatalf("el ejecutable no terminó: %v", args)
	}
	return "", "", -1
}

func TestExecutableSubcommands(t *testing.T) {
	data := t.TempDir()

	out, _, code := runExe(t, data, "", "--version")
	require.Equal(t, 0, code)
	require.NotEmpty(t, strings.TrimSpace(out))

	out, errOut, code := runExe(t, data, "", "--selftest")
	require.Equal(t, 0, code, out+errOut)
	require.True(t, strings.HasSuffix(strings.TrimSpace(out), "OK"), out)

	// Sin usuarios.
	out, _, code = runExe(t, data, "", "user", "list")
	require.Equal(t, 0, code)
	require.Contains(t, out, "No hay usuarios")

	// Alta con la clave por la entrada estándar; clave corta rechazada; duplicado rechazado; nunca se imprime la clave.
	const pass = "clave-larga-de-bob-123"
	out, errOut, code = runExe(t, data, pass+"\n", "user", "add", "bob", "--password-stdin")
	require.Equal(t, 0, code, out+errOut)
	require.NotContains(t, out+errOut, pass)
	_, errOut, code = runExe(t, data, "corta\n", "user", "add", "ana", "--password-stdin")
	require.NotEqual(t, 0, code)
	require.Contains(t, errOut, "Error")
	_, _, code = runExe(t, data, pass+"\n", "user", "add", "bob", "--password-stdin")
	require.NotEqual(t, 0, code)
	out, _, code = runExe(t, data, "", "user", "list")
	require.Equal(t, 0, code)
	require.Contains(t, out, "bob")
	require.Contains(t, out, "activo")
	out, _, code = runExe(t, data, "", "user", "disable", "bob")
	require.Equal(t, 0, code, out)
	out, _, _ = runExe(t, data, "", "user", "list")
	require.Contains(t, out, "desactivado")
	_, _, code = runExe(t, data, "", "user", "enable", "bob")
	require.Equal(t, 0, code)

	// healthcheck sin servidor: falla; subcomando desconocido: 2.
	_, _, code = runExe(t, data, "", "healthcheck", "--listen", "127.0.0.1:1", "--no-tls")
	require.Equal(t, 1, code)
	_, _, code = runExe(t, data, "", "inventado")
	require.Equal(t, 2, code)
	// La carpeta del usuario real no se tocó: todo quedó en `data`.
	_, err := os.Stat(filepath.Join(data, "app.db"))
	require.NoError(t, err)
}

// exeServer es el ejecutable en marcha como proceso.
type exeServer struct {
	all   *syncBuf // toda la salida estándar
	cmd   *exec.Cmd
	addr  string
	lines chan string
	done  chan struct{} // se cierra al terminar el proceso
	err   error
}

func startExe(t *testing.T, data string, env []string, args ...string) *exeServer {
	t.Helper()
	cmd := exec.Command(buildServer(t), args...)
	cmd.Env = append(cleanEnv(data), env...)
	hideAndGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var errb syncBuf
	cmd.Stderr = &errb
	require.NoError(t, cmd.Start())
	es := &exeServer{all: &syncBuf{}, cmd: cmd, lines: make(chan string, 64), done: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-es.done:
		default:
			killTree(cmd.Process.Pid) // no se deja ningún proceso vivo
			<-es.done
		}
	})
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			_, _ = es.all.Write([]byte(sc.Text() + "\n"))
			es.lines <- sc.Text()
		}
		es.err = cmd.Wait()
		close(es.done)
	}()
	deadline := time.After(40 * time.Second)
	for es.addr == "" {
		select {
		case l := <-es.lines:
			var ev struct {
				Event, Addr string
				PID         int
				TLS         bool
			}
			if strings.HasPrefix(l, "{") && json.Unmarshal([]byte(l), &ev) == nil && ev.Event == "listening" {
				es.addr = ev.Addr
				require.Equal(t, cmd.Process.Pid, ev.PID)
			}
		case err := <-es.done:
			t.Fatalf("el servidor terminó antes de escuchar: %v\n%s", err, errb.String())
		case <-deadline:
			t.Fatalf("no apareció la línea listening\n%s", errb.String())
		}
	}
	return es
}

// stop pide el apagado como el escritorio y espera la salida con código 0.
func (e *exeServer) stop(t *testing.T) {
	t.Helper()
	require.NoError(t, gracefulSignal(e.cmd.Process.Pid))
	select {
	case <-e.done:
		require.NoError(t, e.err, "el apagado por señal sale con 0")
	case <-time.After(20 * time.Second):
		t.Fatal("el proceso no se apagó con la señal")
	}
}

func TestExecutableRealStartLoginAndSignalShutdown(t *testing.T) {
	data := t.TempDir()
	const pass = "clave-larga-de-bob-123"
	_, errOut, code := runExe(t, data, pass+"\n", "user", "add", "bob", "--password-stdin")
	require.Equal(t, 0, code, errOut)

	es := startExe(t, data, nil, "--listen", "127.0.0.1:0", "--no-tls")
	base := "http://" + es.addr
	c := &http.Client{Timeout: 15 * time.Second}
	require.Eventually(t, func() bool {
		r, err := c.Get(base + "/api/v1/health")
		if err != nil {
			return false
		}
		defer r.Body.Close()
		return r.StatusCode == 200
	}, 30*time.Second, 50*time.Millisecond)

	// Login por HTTP con el usuario creado por la CLI, y una llamada RPC.
	r, err := c.Post(base+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"bob","password":"`+pass+`"}`))
	require.NoError(t, err)
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	require.Equal(t, 200, r.StatusCode, string(body))
	var lr struct{ Token string }
	require.NoError(t, json.Unmarshal(body, &lr))
	req, _ := http.NewRequest("POST", base+"/api/v1/rpc/Settings/Version", strings.NewReader("[]"))
	req.Header.Set("Authorization", "Bearer "+lr.Token)
	r, err = c.Do(req)
	require.NoError(t, err)
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	require.Equal(t, 200, r.StatusCode, string(body))

	// healthcheck del propio ejecutable contra el servidor vivo.
	_, _, hc := runExe(t, data, "", "healthcheck", "--listen", es.addr, "--no-tls")
	require.Equal(t, 0, hc)

	// Un segundo servidor sobre la misma carpeta no debe pisar al primero (SQLite WAL admite lectura, pero el
	// puerto es otro): lo importante es que ninguno queda colgado. Se omite; aquí solo se apaga el primero.
	es.stop(t)
	_, err = c.Get(base + "/api/v1/health")
	require.Error(t, err, "tras el apagado ya no responde")
}

// Primer arranque con el ejecutable: imprime la clave del administrador una vez y la huella TLS con TLS propio.
func TestExecutableFirstRunPrintsAdminPasswordAndFingerprint(t *testing.T) {
	data := t.TempDir()
	es := startExe(t, data, nil, "--listen", "127.0.0.1:0")
	out := es.all.String()
	require.Regexp(t, `Clave: [A-Za-z0-9]{20,}`, out)
	require.Regexp(t, `Huella TLS [(]SHA-256[)]: [0-9A-F:]{95}`, out)
	require.Contains(t, out, `"tls":true`)
	es.stop(t)
	// Segundo arranque: ni clave nueva ni usuario nuevo.
	es2 := startExe(t, data, nil, "--listen", "127.0.0.1:0")
	require.NotContains(t, es2.all.String(), "Clave:")
	es2.stop(t)
}
