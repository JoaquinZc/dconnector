// Package ws implementa el protocolo dConnector v1 (docs/PROTOCOLO-v1.md): un cliente WebSocket por
// destino habilitado, con outbox persistente, comandos hacia los servicios del núcleo y
// transferencia de archivos por trozos.
package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"dconnector/internal/core/port"
)

const (
	// protocolVersion: valor de "v" en el sobre y de X-DConnector-Protocol.
	protocolVersion = 1
	// maxFrameSize: tamaño máximo de un frame de texto, en los dos sentidos (10 §1).
	maxFrameSize = 1 << 20
	// maxWindow: eventos enviados sin ack como máximo (10 §4).
	maxWindow = 200
	// chunkSize: trozo de archivo antes de base64 (10 §6).
	chunkSize = 512 << 10
	// maxMediaSize: tope de un archivo por el protocolo (10 §6).
	maxMediaSize = 100 << 20
	// commandTimeout: tope de un comando (10 §5).
	commandTimeout = 120 * time.Second
	// uploadTTL: caducidad de un uploadId (10 §6).
	uploadTTL = 10 * time.Minute
	// maxCommands: comandos que pueden estar en curso a la vez por conexión.
	maxCommands = 16
	// writeTimeout: tope para escribir un frame normal.
	writeTimeout = 20 * time.Second
	// mediaWriteTimeout: tope para escribir un trozo de archivo.
	mediaWriteTimeout = 60 * time.Second
)

// Nombres de comandos y eventos fuera de model.EventType.
const (
	kindHello   = "hello"
	kindWelcome = "welcome"
	kindEvent   = "event"
	kindAck     = "ack"
	kindCommand = "command"
	kindResult  = "result"

	nameMediaChunk = "media.chunk"
)

// Códigos de error del protocolo (10 §5).
const (
	codeUnsupported  = "unsupported"
	codeNotConnected = "not_connected"
	codeNotFound     = "not_found"
	codeInvalid      = "invalid"
	codeRateLimited  = "rate_limited"
	codeForbidden    = "forbidden"
	codeInternal     = "internal"
)

// errForbidden: el comando no está permitido para este destino o cuenta.
var errForbidden = errors.New("no permitido")

// accountRef: el bloque "account" del sobre.
type accountRef struct {
	ID      string `json:"id"`
	Channel string `json:"channel,omitempty"`
	Label   string `json:"label,omitempty"`
}

// frameIn: un frame recibido del servidor (los campos desconocidos se ignoran).
type frameIn struct {
	V       int             `json:"v"`
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	TS      int64           `json:"ts"`
	Account *accountRef     `json:"account"`
	Data    json.RawMessage `json:"data"`
}

// frameOut: un frame que dConnector envía.
type frameOut struct {
	V       int         `json:"v"`
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Name    string      `json:"name,omitempty"`
	TS      int64       `json:"ts"`
	Account *accountRef `json:"account,omitempty"`
	Data    any         `json:"data,omitempty"`
}

func nowMs() int64 { return time.Now().UnixMilli() }

// marshalFrame serializa el frame y comprueba el tamaño máximo.
func marshalFrame(f frameOut) ([]byte, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	if len(b) > maxFrameSize {
		return nil, errFrameTooBig
	}
	return b, nil
}

var errFrameTooBig = errors.New("el frame supera 1 MiB")

// ---- Resultados y errores de comandos ----

type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type resultData struct {
	OK     bool       `json:"ok"`
	Result any        `json:"result,omitempty"`
	Error  *wireError `json:"error,omitempty"`
}

// empty es el resultado `{}` de los comandos que no devuelven nada.
func empty() map[string]any { return map[string]any{} }

// errorCode traduce un error del núcleo al código del protocolo (02 §6).
func errorCode(err error) string {
	switch {
	case errors.Is(err, errForbidden):
		return codeForbidden
	case errors.Is(err, port.ErrUnsupported):
		return codeUnsupported
	case errors.Is(err, port.ErrNotConnected):
		return codeNotConnected
	case errors.Is(err, port.ErrNotFound):
		return codeNotFound
	case errors.Is(err, port.ErrInvalid):
		return codeInvalid
	case errors.Is(err, port.ErrRateLimited):
		return codeRateLimited
	}
	return codeInternal
}

// toWireError arma el error de un `result`. El mensaje del núcleo va en español y sin datos
// personales; los errores internos se recortan.
func toWireError(err error) *wireError {
	msg := err.Error()
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300])
	}
	return &wireError{Code: errorCode(err), Message: msg}
}

func invalidf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", port.ErrInvalid, fmt.Sprintf(format, a...))
}
