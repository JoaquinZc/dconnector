package taskbarbadge

import (
	"errors"
	"image"
	"sync"
	"testing"
	"time"
)

func TestBadgeLabel(t *testing.T) {
	cases := map[int]string{-3: "", 0: "", 1: "1", 2: "2", 9: "9", 10: "9+", 11: "9+", 250: "9+"}
	for n, want := range cases {
		if got := badgeLabel(n); got != want {
			t.Errorf("badgeLabel(%d) = %q, quería %q", n, got, want)
		}
	}
}

func TestBadgeDescription(t *testing.T) {
	cases := map[int]string{0: "", -1: "", 1: "1 mensaje sin leer", 3: "3 mensajes sin leer", 25: "25 mensajes sin leer"}
	for n, want := range cases {
		if got := badgeDescription(n); got != want {
			t.Errorf("badgeDescription(%d) = %q, quería %q", n, got, want)
		}
	}
}

// whitePixels cuenta los píxeles casi blancos y opacos, y devuelve su caja.
func whitePixels(img *image.RGBA) (n int, box image.Rectangle) {
	box = image.Rectangle{Min: image.Pt(1<<20, 1<<20), Max: image.Pt(-1, -1)} // vacía a propósito (Rect la reordenaría)
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			c := img.RGBAAt(x, y)
			if c.A == 255 && c.R >= 230 && c.G >= 230 && c.B >= 230 {
				n++
				box.Min.X, box.Min.Y = min(box.Min.X, x), min(box.Min.Y, y)
				box.Max.X, box.Max.Y = max(box.Max.X, x+1), max(box.Max.Y, y+1)
			}
		}
	}
	return n, box
}

func TestRenderBadge(t *testing.T) {
	if renderBadge("") != nil {
		t.Fatal("sin etiqueta no hay imagen")
	}
	for _, label := range []string{"1", "2", "5", "9", "9+"} {
		t.Run(label, func(t *testing.T) {
			img := renderBadge(label)
			if img == nil || img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
				t.Fatalf("debe ser de 32×32: %v", img)
			}
			// esquinas transparentes
			for _, p := range [][2]int{{0, 0}, {31, 0}, {0, 31}, {31, 31}} {
				if a := img.RGBAAt(p[0], p[1]).A; a != 0 {
					t.Fatalf("la esquina %v debe ser transparente (alfa %d)", p, a)
				}
			}
			// el borde del círculo: rojo opaco justo dentro
			if c := img.RGBAAt(16, 1); c.A != 255 || c.R < 200 || c.G > 60 {
				t.Fatalf("el círculo debe ser rojo y opaco: %+v", c)
			}
			// texto blanco dentro del círculo y centrado
			n, box := whitePixels(img)
			if n < 20 {
				t.Fatalf("faltan píxeles blancos del texto: %d", n)
			}
			for y := box.Min.Y; y < box.Max.Y; y++ {
				for x := box.Min.X; x < box.Max.X; x++ {
					if img.RGBAAt(x, y).A == 0 {
						t.Fatalf("el texto se sale del círculo en (%d,%d)", x, y)
					}
				}
			}
			cx, cy := (box.Min.X+box.Max.X)/2, (box.Min.Y+box.Max.Y)/2
			if cx < 14 || cx > 18 || cy < 14 || cy > 18 {
				t.Fatalf("el texto debe estar centrado: centro (%d,%d), caja %v", cx, cy, box)
			}
			if box.Dx() > 26 || box.Dy() > 22 {
				t.Fatalf("el texto no cabe con margen: %v", box)
			}
		})
	}
	// «9+» es más ancho que «1»: se achica para caber, no se corta
	if _, w2 := whitePixels(renderBadge("9+")); w2.Dx() <= 4 {
		t.Fatalf("«9+» debe verse completo: %v", w2)
	}
}

// ---------------------------------------------------------------------------------------------
// Aplicación

type fakeOverlay struct {
	mu     sync.Mutex
	calls  []fakeCall
	err    error
	closed int
	ch     chan struct{}
}

type fakeCall struct {
	has  bool
	desc string
}

func newFakeOverlay() *fakeOverlay { return &fakeOverlay{ch: make(chan struct{}, 64)} }

func (f *fakeOverlay) Apply(img *image.RGBA, desc string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{has: img != nil, desc: desc})
	f.ch <- struct{}{}
	return f.err
}

func (f *fakeOverlay) Close() {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
}

func (f *fakeOverlay) snapshot() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCall(nil), f.calls...)
}

// next espera una llamada más a Apply.
func (f *fakeOverlay) next(t *testing.T) {
	t.Helper()
	select {
	case <-f.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("el overlay no recibió la llamada esperada")
	}
}

func (f *fakeOverlay) quiet(t *testing.T) {
	t.Helper()
	select {
	case <-f.ch:
		t.Fatal("no debía haber otra llamada")
	case <-time.After(80 * time.Millisecond):
	}
}

func TestBadgeAppliesChangesOnly(t *testing.T) {
	ov := newFakeOverlay()
	b := New(ov, WithReapplyEvery(0))
	defer b.Close()

	b.Set(3)
	ov.next(t)
	b.Set(3) // misma etiqueta: nada
	ov.quiet(t)
	b.Set(4) // etiqueta distinta («4»)
	ov.next(t)
	b.Set(12) // «9+»
	ov.next(t)
	b.Set(12) // igual
	ov.quiet(t)
	b.Set(30) // sigue «9+», pero la descripción accesible cambia: se reaplica
	ov.next(t)
	b.Set(0)
	ov.next(t)

	got := ov.snapshot()
	want := []fakeCall{{true, "3 mensajes sin leer"}, {true, "4 mensajes sin leer"}, {true, "12 mensajes sin leer"}, {true, "30 mensajes sin leer"}, {false, ""}}
	if len(got) != len(want) {
		t.Fatalf("llamadas: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("llamada %d: %+v, quería %+v", i, got[i], want[i])
		}
	}
	if b.Count() != 0 {
		t.Fatalf("Count: %d", b.Count())
	}
}

func TestBadgeReapplyForcesEvenIfUnchanged(t *testing.T) {
	ov := newFakeOverlay()
	b := New(ov, WithReapplyEvery(0))
	defer b.Close()
	b.Set(2)
	ov.next(t)
	b.Reapply() // la ventana volvió de la bandeja
	ov.next(t)
	b.Set(0)
	ov.next(t)
	b.Reapply() // sin sin leer también se reaplica (quita la insignia que pudiera quedar)
	ov.next(t)
	if got := ov.snapshot(); len(got) != 4 || got[3].has {
		t.Fatalf("llamadas: %+v", got)
	}
}

func TestBadgeRetriesAfterError(t *testing.T) {
	ov := newFakeOverlay()
	ov.err = errors.New("la barra de tareas aún no existe")
	b := New(ov, WithReapplyEvery(0))
	defer b.Close()
	b.Set(1)
	ov.next(t)
	b.Set(1) // el intento anterior falló: aunque sea la misma etiqueta, se reintenta
	ov.next(t)
	ov.mu.Lock()
	ov.err = nil
	ov.mu.Unlock()
	b.Set(1)
	ov.next(t)
	b.Set(1)
	ov.quiet(t) // ya aplicada
}

func TestBadgePeriodicReapplyOnlyWithUnread(t *testing.T) {
	ov := newFakeOverlay()
	b := New(ov, WithReapplyEvery(40*time.Millisecond))
	defer b.Close()
	time.Sleep(120 * time.Millisecond)
	if n := len(ov.snapshot()); n != 0 {
		t.Fatalf("sin sin leer no se reaplica: %d llamadas", n)
	}
	b.Set(5)
	ov.next(t)
	ov.next(t) // la reaplicación periódica
	ov.next(t)
	b.Set(0)
	ov.next(t)
	time.Sleep(150 * time.Millisecond)
	calls := ov.snapshot()
	if last := calls[len(calls)-1]; last.has {
		t.Fatalf("la última debe quitar la insignia: %+v", last)
	}
}

func TestBadgeCloseIsIdempotentAndStops(t *testing.T) {
	ov := newFakeOverlay()
	b := New(ov, WithReapplyEvery(0))
	b.Set(1)
	ov.next(t)
	b.Close()
	b.Close()
	if ov.closed != 1 {
		t.Fatalf("Close del overlay: %d veces", ov.closed)
	}
	b.Set(7)
	b.Reapply()
	ov.quiet(t)
}

func TestBadgeNeverBlocksCaller(t *testing.T) {
	// El overlay tarda: Set/Reapply no deben esperarlo (quien llama puede ser el hilo de interfaz).
	release := make(chan struct{})
	slow := &slowOverlay{release: release}
	b := New(slow, WithReapplyEvery(0))
	defer b.Close()
	done := make(chan struct{})
	go func() {
		for i := 1; i <= 50; i++ {
			b.Set(i)
			b.Reapply()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Set/Reapply se bloquearon esperando al sistema")
	}
	close(release)
}

type slowOverlay struct{ release chan struct{} }

func (s *slowOverlay) Apply(*image.RGBA, string) error { <-s.release; return nil }
func (s *slowOverlay) Close()                          {}

func TestNilBadgeIsHarmless(t *testing.T) {
	var b *Badge
	b.Set(3)
	b.Reapply()
	b.Close()
	if b.Count() != 0 {
		t.Fatal("un Badge nil no cuenta nada")
	}
	nb := New(nil) // fuera de Windows
	nb.Set(3)
	nb.Reapply()
	nb.Close()
}

func TestBadgeSurvivesOverlayPanic(t *testing.T) {
	// P. ej. Wails aún no arrancó y InvokeSync falla con un pánico: el trabajador sigue vivo.
	ov := newFakeOverlay()
	pan := &panicOnce{fakeOverlay: ov}
	b := New(pan, WithReapplyEvery(0))
	defer b.Close()
	b.Set(1)
	ov.next(t)
	b.Set(2)
	ov.next(t)
	if got := ov.snapshot(); len(got) != 2 || got[1].desc != "2 mensajes sin leer" {
		t.Fatalf("llamadas: %+v", got)
	}
}

type panicOnce struct {
	*fakeOverlay
	done bool
}

func (p *panicOnce) Apply(img *image.RGBA, desc string) error {
	if !p.done {
		p.done = true
		p.fakeOverlay.ch <- struct{}{}
		p.fakeOverlay.mu.Lock()
		p.fakeOverlay.calls = append(p.fakeOverlay.calls, fakeCall{has: img != nil, desc: desc})
		p.fakeOverlay.mu.Unlock()
		panic("app sin arrancar")
	}
	return p.fakeOverlay.Apply(img, desc)
}
