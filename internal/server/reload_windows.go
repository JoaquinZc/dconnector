//go:build windows

package server

import "context"

// watchReload no hace nada en Windows (no hay SIGHUP): el certificado externo se relee al reiniciar el servicio.
func watchReload(context.Context) {}
