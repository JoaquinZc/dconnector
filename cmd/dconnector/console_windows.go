//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachConsole conecta la salida estándar a la consola del proceso padre. Con
// -H windowsgui el .exe no tiene consola propia y --version/--selftest no
// imprimirían nada (docs/12 §3).
func attachConsole() {
	const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS (-1)
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if r, _, _ := proc.Call(attachParentProcess); r == 0 {
		return // sin consola padre (p. ej. lanzado desde el Explorador)
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
	}
}
