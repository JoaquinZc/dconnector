// Package wails: servicios con enlaces, bandeja y arranque de la ventana de escritorio. Desde 0.3.0 la
// interfaz habla directo con el servidor por HTTP/WebSocket: aquí solo queda lo que depende de esta máquina
// (conexión, diálogos de archivos, carpetas, avisos y bandeja). Las fachadas del núcleo viven ahora en
// internal/adapter/api/facade y internal/adapter/api (sin dependencia de Wails).
package wails

import "errors"

// EvtOpenChat: evento de escritorio que pide a la interfaz abrir un chat (lo emite la bandeja al pulsar un aviso).
const EvtOpenChat = "ui:open-chat"

var errNotWired = errors.New("el servicio no está conectado todavía")
