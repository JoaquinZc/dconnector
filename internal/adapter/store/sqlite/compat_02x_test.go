package sqlite

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

// Compatibilidad de datos 0.2.x → 0.3.0 con una carpeta SINTÉTICA (nunca la real): app.db en el esquema
// anterior a la migración 3, una cuenta falsa (desactivada) y un chat. El servidor REAL 0.3.0 la abre, aplica
// la migración aditiva de usuarios y deja intacto todo lo anterior; un 0.2.x hipotético sigue leyéndola.
func TestCarpeta02xSinteticaLaAbreElServidor030(t *testing.T) {
	if testing.Short() {
		t.Skip("compila y arranca el servidor real")
	}
	ctx := context.Background()
	dir := t.TempDir()
	const acc = model.AccountID("acc-sintetica")

	// 1) La carpeta «0.2.x»: solo las migraciones 1 y 2.
	appPath := filepath.Join(dir, "app.db")
	db, err := Open(appPath)
	require.NoError(t, err)
	require.NoError(t, migrate(db, appMigrations[:2]))
	st := newAppStore(db)
	require.NoError(t, st.SaveAccount(ctx, model.Account{ID: acc, Channel: model.ChannelTelegram, Label: "Cuenta de prueba", Color: "#123456", Enabled: false}))
	require.NoError(t, st.SetSetting(ctx, "k", `"v"`))
	oldDDL := tablesDDL(t, db)
	require.NotContains(t, oldDDL, "users")
	require.NoError(t, db.Close())

	accPath := filepath.Join(dir, "accounts", string(acc), "account.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(accPath), 0o755))
	as, adb, err := OpenAccount(acc, accPath)
	require.NoError(t, err)
	require.NoError(t, as.UpsertChat(ctx, model.Chat{Key: model.ChatKey(string(acc) + "|c1"), AccountID: acc, Channel: model.ChannelTelegram, NativeID: "c1", Kind: model.ChatDirect, Title: "Chat de prueba", PeerNativeID: "c1"}))
	require.NoError(t, adb.Close())

	// 2) El servidor real 0.3.0 la abre (arranca de verdad, sin TLS, en loopback).
	bin := filepath.Join(t.TempDir(), "dConnector-server.exe")
	b := exec.Command("go", "build", "-o", bin, "dconnector/cmd/dconnector-server")
	b.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := b.CombinedOutput(); err != nil {
		t.Fatalf("compilar el servidor: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "--data", dir, "--listen", "127.0.0.1:0", "--no-tls")
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid)).Run(); _ = cmd.Wait() })
	ready := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(out)
		sent := false
		for sc.Scan() {
			if !sent && strings.Contains(sc.Text(), `"event":"listening"`) {
				sent = true
				ready <- true
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(40 * time.Second):
		t.Fatal("el servidor 0.3.0 no arrancó sobre la carpeta 0.2.x")
	}
	require.NoError(t, exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid)).Run())
	_ = cmd.Wait()

	// 3) Resultado: migración 3 aplicada, administrador creado, y lo anterior intacto.
	db, err = Open(appPath)
	require.NoError(t, err)
	defer db.Close()
	var maxV, n int
	require.NoError(t, db.QueryRow(`SELECT MAX(version), COUNT(*) FROM schema_migrations`).Scan(&maxV, &n))
	require.Equal(t, 3, maxV)
	require.Equal(t, 3, n)
	var users int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users))
	require.Equal(t, 1, users, "el primer arranque crea al administrador")
	newDDL := tablesDDL(t, db)
	for name, ddl := range oldDDL {
		require.Equal(t, ddl, newDDL[name], "la tabla %s cambió", name)
	}
	require.Contains(t, newDDL, "users")
	require.Contains(t, newDDL, "auth_tokens")

	// 4) Un 0.2.x hipotético: sus migraciones (1 y 2) no hacen nada y sus lecturas siguen funcionando.
	require.NoError(t, migrate(db, appMigrations[:2]))
	accs, err := newAppStore(db).ListAccounts(ctx)
	require.NoError(t, err)
	require.Len(t, accs, 1)
	require.Equal(t, "Cuenta de prueba", accs[0].Label)
	v, err := newAppStore(db).GetSetting(ctx, "k")
	require.NoError(t, err)
	require.Equal(t, `"v"`, v)
	as2, adb2, err := OpenAccount(acc, accPath)
	require.NoError(t, err)
	defer adb2.Close()
	chats, err := as2.ListChats(ctx, model.ChatFilter{Limit: 50, IncludeEmpty: true})
	require.NoError(t, err)
	require.Len(t, chats, 1)
	require.Equal(t, "Chat de prueba", chats[0].Title)
}

// tablesDDL devuelve el SQL de cada tabla (sin las de sqlite) por nombre.
func tablesDDL(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT name, sql FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var n, s string
		require.NoError(t, rows.Scan(&n, &s))
		m[n] = s
	}
	return m
}
