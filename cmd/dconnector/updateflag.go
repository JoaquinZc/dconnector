package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"dconnector/internal/adapter/desktop/connection"
	"dconnector/internal/adapter/desktop/updater"
	"dconnector/internal/buildinfo"
	"dconnector/internal/paths"
)

// newUpdater arma el actualizador con lo de esta máquina. emit y quit los pone quien llama.
func newUpdater(store *connection.Store, exe string, emit func(string, any), quit func(), minimized func() bool) *updater.Updater {
	return updater.New(updater.Options{
		CurrentVersion: buildinfo.Version,
		DataDir:        paths.DataDir(),
		Exe:            exe,
		Prefs:          connection.NewUpdatePrefs(store),
		Emit:           emit,
		Quit:           quit,
		Minimized:      minimized,
	})
}

// runUpdateSelftest es la comprobación sin ventana del actualizador (para pruebas con DCONNECTOR_UPDATE_API):
// «check» consulta la Release y muestra el estado; «apply» además descarga, verifica y lanza el ayudante, y sale.
func runUpdateSelftest(w io.Writer, mode string) error {
	if mode != "check" && mode != "apply" {
		return fmt.Errorf("--update-selftest admite check o apply, no %q", mode)
	}
	if err := paths.InitKeepTmp(); err != nil {
		return err
	}
	store, err := connection.OpenStore(paths.DataDir())
	if err != nil {
		return err
	}
	exe, _ := os.Executable()
	u := newUpdater(store, exe, func(string, any) {}, func() {}, func() bool { return false })
	show := func(label string) {
		b, _ := json.MarshalIndent(u.Status(), "", "  ")
		fmt.Fprintf(w, "%s:\n%s\n", label, b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := u.Check(ctx); err != nil {
		show("estado")
		return err
	}
	show("comprobación")
	if mode == "check" {
		return nil
	}
	if err := u.Apply(ctx); err != nil {
		show("estado")
		return err
	}
	fmt.Fprintln(w, "ayudante lanzado; esta aplicación se cierra")
	return nil
}
