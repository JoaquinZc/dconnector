package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"dconnector/internal/adapter/store/sqlite"
	"dconnector/internal/buildinfo"
	"dconnector/internal/core/model"
	"dconnector/internal/logging"
	"dconnector/internal/paths"
)

// RunSelftest es la prueba automática de los paquetes portable y MSI (T16): sin ventana,
// resuelve la carpeta de datos, abre app.db, aplica migraciones, crea y borra una cuenta
// falsa con su account.db, imprime OK y sale con código 0. Cualquier fallo devuelve error.
func RunSelftest(w io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	fmt.Fprintln(w, "dConnector", buildinfo.Version)

	if err := paths.Init(); err != nil {
		return fmt.Errorf("carpeta de datos: %w", err)
	}
	fmt.Fprintln(w, "datos:", paths.DataDir(), "portable:", paths.IsPortable())

	closeLog, err := logging.Init(paths.LogsDir())
	if err != nil {
		return fmt.Errorf("registro: %w", err)
	}
	defer closeLog()

	app, err := sqlite.OpenApp(paths.AppDBPath())
	if err != nil {
		return fmt.Errorf("abrir app.db: %w", err)
	}
	defer app.Close()
	if _, err := os.Stat(paths.AppDBPath()); err != nil {
		return fmt.Errorf("app.db no existe: %w", err)
	}
	fmt.Fprintln(w, "app.db: abierta y con migraciones")

	// Cuenta falsa: se guarda, se abre su account.db, se escribe un chat y se borra todo.
	id := model.NewAccountID(model.ChannelWhatsApp)
	acc := model.Account{
		ID: id, Channel: model.ChannelWhatsApp, Label: "selftest", Color: "#25D366",
		Enabled: false, CreatedAt: time.Now().UnixMilli(), Config: map[string]string{},
	}
	if err := app.SaveAccount(ctx, acc); err != nil {
		return fmt.Errorf("guardar la cuenta falsa: %w", err)
	}
	st, _, err := OpenAccountStore(id)
	if err != nil {
		return fmt.Errorf("abrir account.db: %w", err)
	}
	chat := model.Chat{
		Key: model.NewChatKey(id, "selftest@s.whatsapp.net"), AccountID: id, Channel: model.ChannelWhatsApp,
		NativeID: "selftest@s.whatsapp.net", Kind: model.ChatDirect, Title: "Selftest",
	}
	if err := st.UpsertChat(ctx, chat); err != nil {
		_ = st.Close()
		return fmt.Errorf("escribir en account.db: %w", err)
	}
	got, err := st.GetChat(ctx, chat.NativeID)
	if err != nil || got.Title != "Selftest" {
		_ = st.Close()
		return fmt.Errorf("leer de account.db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.AccountDir(id), "account.db")); err != nil {
		_ = st.Close()
		return fmt.Errorf("account.db no existe: %w", err)
	}
	fmt.Fprintln(w, "account.db: abierta y con migraciones")
	if err := st.Close(); err != nil {
		return fmt.Errorf("cerrar account.db: %w", err)
	}
	if err := app.DeleteAccount(ctx, id); err != nil {
		return fmt.Errorf("borrar la cuenta falsa: %w", err)
	}
	if err := os.RemoveAll(paths.AccountDir(id)); err != nil {
		return fmt.Errorf("borrar la carpeta de la cuenta: %w", err)
	}
	if _, err := app.GetAccount(ctx, id); err == nil {
		return fmt.Errorf("la cuenta falsa sigue existiendo")
	}
	fmt.Fprintln(w, "OK")
	return nil
}
