//go:build !windows

package taskbarbadge

// NewOverlay fuera de Windows no hay barra de tareas con insignias: devuelve nil y Badge no hace nada.
func NewOverlay(hwnd func() uintptr, invoke func(func() error) error) Overlay { return nil }
