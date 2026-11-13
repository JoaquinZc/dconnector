//go:build windows

package taskbarbadge

import (
	"strings"
	"testing"
)

// El icono se crea de verdad con las funciones de Windows (no se muestra en ningún sitio) y se destruye.
func TestIconFromImageCreatesRealIcons(t *testing.T) {
	for _, l := range []string{"1", "9", "9+"} {
		h, err := iconFromImage(renderBadge(l))
		if err != nil || h == 0 {
			t.Fatalf("icono de %q: %v", l, err)
		}
		destroyIcon(h)
	}
}

func TestWinOverlayWithoutWindowFailsCleanly(t *testing.T) {
	called := 0
	ov := NewOverlay(func() uintptr { return 0 }, func(fn func() error) error { called++; return fn() })
	if ov == nil {
		t.Fatal("en Windows hay Overlay")
	}
	err := ov.Apply(renderBadge("3"), "3 mensajes sin leer")
	if err == nil || !strings.Contains(err.Error(), "ventana principal") || called != 1 {
		t.Fatalf("sin ventana debe fallar sin dejar iconos: %v (invocaciones %d)", err, called)
	}
	ov.Close()
	if err := ov.Apply(nil, ""); err == nil {
		t.Fatal("tras Close no se aplica nada")
	}
	if NewOverlay(nil, nil) != nil {
		t.Fatal("sin hwnd/invoke no hay Overlay")
	}
}
