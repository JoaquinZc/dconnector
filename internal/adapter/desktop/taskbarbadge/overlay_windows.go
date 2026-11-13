//go:build windows

package taskbarbadge

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// NewOverlay crea el Overlay de Windows: ITaskbarList3.SetOverlayIcon sobre la ventana principal.
//
//   - hwnd devuelve el HWND de la ventana principal de la aplicación (no «la ventana con foco»: con la
//     aplicación en segundo plano o la ventana oculta en la bandeja no hay ventana actual y el contador no
//     aparecería nunca).
//   - invoke ejecuta una función en el hilo de interfaz de Wails (application.InvokeSyncWithError): COM de
//     la barra de tareas es de apartamento único y debe llamarse desde ese hilo.
func NewOverlay(hwnd func() uintptr, invoke func(func() error) error) Overlay {
	if hwnd == nil || invoke == nil {
		return nil
	}
	return &winOverlay{hwnd: hwnd, invoke: invoke}
}

type winOverlay struct {
	hwnd   func() uintptr
	invoke func(func() error) error

	mu     sync.Mutex
	cur    w32.HICON // el icono que está puesto ahora (se destruye al reemplazarlo o al cerrar)
	closed bool
}

// Apply pone la insignia (img != nil) o la quita (img == nil).
func (o *winOverlay) Apply(img *image.RGBA, description string) error {
	o.mu.Lock()
	closed := o.closed
	o.mu.Unlock()
	if closed {
		return errors.New("la insignia ya está cerrada")
	}

	var icon w32.HICON
	if img != nil {
		var err error
		if icon, err = iconFromImage(img); err != nil {
			return err
		}
	}
	var desc *uint16
	if description != "" {
		d, err := windows.UTF16PtrFromString(description)
		if err != nil {
			destroyIcon(icon)
			return err
		}
		desc = d
	}

	// COM de la barra de tareas se crea y se libera en cada aplicación, dentro del hilo de interfaz: así
	// nada queda vivo si Explorer se reinicia, y no hay objeto COM que liberar al cerrar.
	err := o.invoke(func() error {
		hwnd := o.hwnd()
		if hwnd == 0 {
			return errors.New("la ventana principal todavía no existe")
		}
		tb, err := w32.NewTaskbarList3()
		if err != nil {
			return fmt.Errorf("barra de tareas: %w", err)
		}
		defer tb.Release()
		return tb.SetOverlayIcon(w32.HWND(hwnd), icon, desc)
	})
	if err != nil {
		destroyIcon(icon)
		return err
	}

	// El icono nuevo ya está puesto: el anterior se destruye. Se conserva el actual hasta el siguiente
	// cambio o hasta Close.
	o.mu.Lock()
	old, stillOpen := o.cur, !o.closed
	if stillOpen {
		o.cur = icon
	}
	o.mu.Unlock()
	destroyIcon(old)
	if !stillOpen {
		destroyIcon(icon)
	}
	return nil
}

// Close destruye el icono que quedó puesto.
func (o *winOverlay) Close() {
	o.mu.Lock()
	o.closed = true
	old := o.cur
	o.cur = 0
	o.mu.Unlock()
	destroyIcon(old)
}

// iconFromImage crea un HICON desde la imagen (PNG → icono de tamaño pequeño del sistema).
func iconFromImage(img *image.RGBA) (w32.HICON, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return 0, fmt.Errorf("insignia: no se pudo codificar: %w", err)
	}
	icon, err := w32.CreateSmallHIconFromImage(buf.Bytes())
	if err != nil || icon == 0 {
		if err == nil {
			err = errors.New("el sistema devolvió un icono vacío")
		}
		return 0, fmt.Errorf("insignia: no se pudo crear el icono: %w", err)
	}
	return icon, nil
}

func destroyIcon(h w32.HICON) {
	if h != 0 {
		w32.DestroyIcon(h)
	}
}
