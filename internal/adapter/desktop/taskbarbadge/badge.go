// Package taskbarbadge: el contador de mensajes sin leer sobre el icono de la barra de tareas de Windows,
// como el de WhatsApp Desktop: un círculo rojo con el número («1»… «9» y «9+» desde 10), sin nada cuando no
// hay sin leer. La lógica (etiqueta, dibujo, cola de aplicación) no toca el sistema y se prueba en cualquier
// sistema operativo; lo que habla con Windows (ITaskbarList3.SetOverlayIcon) entra por la interfaz Overlay y
// vive en overlay_windows.go. Fuera de Windows no hace nada.
package taskbarbadge

import (
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	badgeSize = 32 // el icono se dibuja a 32×32; Windows lo ajusta al tamaño de la barra de tareas
	// reapplyEvery: mientras haya sin leer se vuelve a poner la insignia cada tanto, porque Explorer la pierde
	// cuando recrea el botón de la barra de tareas (reinicio de Explorer) y Wails no deja enganchar el mensaje
	// «TaskbarButtonCreated» de la ventana.
	reapplyEvery = 30 * time.Second
)

// badgeColor: rojo vivo visible sobre barras de tareas claras y oscuras.
var badgeColor = color.NRGBA{R: 0xE1, G: 0x1D, B: 0x48, A: 0xFF}

// badgeLabel: el texto de la insignia para n sin leer. "" = sin insignia.
func badgeLabel(n int) string {
	switch {
	case n <= 0:
		return ""
	case n >= 10:
		return "9+"
	}
	return fmt.Sprintf("%d", n)
}

// badgeDescription: el texto accesible que lee el lector de pantalla («3 mensajes sin leer»). "" sin insignia.
func badgeDescription(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 mensaje sin leer"
	}
	return fmt.Sprintf("%d mensajes sin leer", n)
}

var (
	renderMu  sync.Mutex // serializa renderBadge (y con ello faceOfSize)
	faceCache = map[float64]font.Face{}
	fontOnce  sync.Once
	boldFont  *opentype.Font
	fontErr   error
)

// faceOfSize devuelve la fuente Go Bold al tamaño dado (en píxeles; se guarda en caché). Llamar con renderMu tomado.
func faceOfSize(px float64) (font.Face, error) {
	fontOnce.Do(func() { boldFont, fontErr = opentype.Parse(gobold.TTF) })
	if fontErr != nil {
		return nil, fontErr
	}
	if f, ok := faceCache[px]; ok {
		return f, nil
	}
	f, err := opentype.NewFace(boldFont, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	faceCache[px] = f
	return f, nil
}

// renderBadge dibuja la insignia: círculo rojo de 32×32 con el texto en blanco, centrado. Las esquinas quedan
// transparentes. Con etiqueta vacía devuelve nil (sin insignia).
func renderBadge(label string) *image.RGBA {
	if label == "" {
		return nil
	}
	// La fuente (sfnt.Font) y sus caras comparten búferes internos: no son seguras entre goroutines.
	renderMu.Lock()
	defer renderMu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, badgeSize, badgeSize))
	drawDisc(img)

	// El texto más grande que quepa dentro del círculo (con margen), de 24 px hacia abajo.
	const maxWidth = 24 * 64 // 24 px en 26.6
	for px := 24.0; px >= 10; px -= 1 {
		face, err := faceOfSize(px)
		if err != nil {
			slog.Debug("insignia de la barra de tareas: no se pudo cargar la fuente", "error", err)
			return img // sin texto: al menos el círculo
		}
		bounds, _ := font.BoundString(face, label)
		w := bounds.Max.X - bounds.Min.X
		h := bounds.Max.Y - bounds.Min.Y
		if w > maxWidth || h > 20*64 {
			continue
		}
		// El punto de origen del texto (línea base) que deja el rectángulo del texto centrado en el círculo.
		cx := fixed.I(badgeSize)/2 - (bounds.Min.X+bounds.Max.X)/2
		cy := fixed.I(badgeSize)/2 - (bounds.Min.Y+bounds.Max.Y)/2
		d := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(color.White),
			Face: face,
			Dot:  fixed.Point26_6{X: cx, Y: cy},
		}
		d.DrawString(label)
		return img
	}
	return img
}

// drawDisc pinta el círculo rojo con borde suavizado; fuera de él queda transparente.
func drawDisc(img *image.RGBA) {
	const c = float64(badgeSize) / 2
	const r = c - 0.5
	for y := 0; y < badgeSize; y++ {
		for x := 0; x < badgeSize; x++ {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			cover := r + 0.5 - math.Hypot(dx, dy) // 1 dentro, 0 fuera, gradual en el borde
			if cover <= 0 {
				continue
			}
			if cover > 1 {
				cover = 1
			}
			a := uint8(math.Round(cover * 255))
			// image.RGBA guarda el color con el alfa ya multiplicado
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(math.Round(float64(badgeColor.R) * cover)),
				G: uint8(math.Round(float64(badgeColor.G) * cover)),
				B: uint8(math.Round(float64(badgeColor.B) * cover)),
				A: a,
			})
		}
	}
}

// Overlay pone o quita la insignia en el sistema.
type Overlay interface {
	// Apply pone la insignia (img != nil) con su descripción accesible, o la quita (img == nil).
	Apply(img *image.RGBA, description string) error
	// Close libera lo que Apply dejó vivo (iconos).
	Close()
}

// Badge mantiene la insignia al día. Set y Reapply nunca bloquean: solo anotan lo que se quiere y avisan a
// un único trabajador que habla con el sistema (el sistema puede tardar, porque en Windows toda la llamada va
// por el hilo de interfaz, y quien llama puede ser ese mismo hilo). Las peticiones seguidas se agrupan.
type Badge struct {
	ov Overlay

	mu     sync.Mutex
	count  int
	force  bool // volver a aplicar aunque no haya cambiado
	closed bool

	lastApplied string // etiqueta y descripción aplicadas con éxito la última vez
	haveApplied bool
	lastErr     string

	kick chan struct{}
	stop chan struct{}
}

// Option ajusta el Badge.
type Option func(*Badge, *time.Duration)

// WithReapplyEvery cambia cada cuánto se reaplica la insignia mientras haya sin leer (0 = nunca). Pruebas.
func WithReapplyEvery(d time.Duration) Option {
	return func(_ *Badge, every *time.Duration) { *every = d }
}

// New crea el Badge sobre un Overlay. Con ov nil no hace nada (fuera de Windows, o sin ventana).
func New(ov Overlay, opts ...Option) *Badge {
	b := &Badge{ov: ov, kick: make(chan struct{}, 1), stop: make(chan struct{})}
	every := reapplyEvery
	for _, o := range opts {
		o(b, &every)
	}
	if ov != nil {
		go b.run(every)
	}
	return b
}

// Set anota el total de sin leer y lo aplica (solo si cambió la etiqueta).
func (b *Badge) Set(total int) {
	if b == nil || b.ov == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.count = total
	b.mu.Unlock()
	b.wake()
}

// Reapply vuelve a poner la insignia aunque no haya cambiado (la ventana volvió de la bandeja, ganó el
// foco o Explorer recreó el botón de la barra de tareas).
func (b *Badge) Reapply() {
	if b == nil || b.ov == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.force = true
	b.mu.Unlock()
	b.wake()
}

// Count devuelve el último total anotado.
func (b *Badge) Count() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// Close detiene el trabajador y libera los iconos. No espera al trabajador (si quien llama es el hilo de
// interfaz, esperar a una llamada que necesita ese hilo se bloquearía); es seguro llamarlo más de una vez.
func (b *Badge) Close() {
	if b == nil || b.ov == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()
	close(b.stop)
	b.ov.Close()
}

func (b *Badge) wake() {
	select {
	case b.kick <- struct{}{}:
	default: // ya hay un aviso pendiente: el trabajador leerá el último valor
	}
}

func (b *Badge) run(every time.Duration) {
	var tick <-chan time.Time
	if every > 0 {
		t := time.NewTicker(every)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case <-b.stop:
			return
		case <-b.kick:
			b.apply(false)
		case <-tick:
			b.apply(true)
		}
	}
}

// apply lleva el sistema al estado pedido. periodic: es la reaplicación periódica (solo con sin leer).
func (b *Badge) apply(periodic bool) {
	b.mu.Lock()
	n, force, closed := b.count, b.force, b.closed
	b.force = false
	label := badgeLabel(n)
	key := label + "|" + badgeDescription(n) // la descripción accesible también debe ser exacta (12 → 30)
	unchanged := b.haveApplied && b.lastApplied == key && !force
	b.mu.Unlock()
	if closed || unchanged && !periodic || periodic && n <= 0 {
		return
	}

	var img *image.RGBA
	if label != "" {
		img = renderBadge(label)
	}
	err := b.safeApply(img, badgeDescription(n))

	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		// Se anota una vez por motivo para no llenar el registro si falla cada 30 s.
		if msg := err.Error(); msg != b.lastErr {
			b.lastErr = msg
			slog.Debug("insignia de la barra de tareas: no se pudo aplicar", "error", err)
		}
		b.haveApplied = false // se reintentará en el siguiente cambio o reaplicación
		return
	}
	b.lastErr = ""
	b.lastApplied, b.haveApplied = key, true
}

// safeApply llama al Overlay sin dejar que un pánico (p. ej. la aplicación aún no arrancó) tumbe el trabajador.
func (b *Badge) safeApply(img *image.RGBA, desc string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pánico al poner la insignia: %v", r)
		}
	}()
	return b.ov.Apply(img, desc)
}
