// Package frontend incrusta la interfaz compilada (frontend/dist) en el ejecutable de escritorio.
package frontend

import "embed"

// Assets contiene frontend/dist (se genera con `task frontend`).
//
//go:embed all:dist
var Assets embed.FS
