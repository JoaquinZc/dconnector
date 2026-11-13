package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dconnector/internal/core/port"
	"dconnector/internal/paths"
)

// Códigos de error de docs/14 §2.
const (
	codeInvalid      = "invalid"
	codeUnauthorized = "unauthorized"
	codeForbidden    = "forbidden"
	codeNotFound     = "not_found"
	codeNotConnected = "not_connected"
	codeTooLarge     = "too_large"
	codeUnsupported  = "unsupported"
	codeRateLimited  = "rate_limited"
	codeInternal     = "internal"
	codeUnavailable  = "unavailable"
	codeNotAvailable = "not_available"
	codeMethod       = "method_not_allowed"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, genericInternal)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{errorDetail{Code: code, Message: msg}})
}

const genericInternal = "Ocurrió un error inesperado en el servidor. Inténtalo de nuevo."

// retryAfterer lo cumplen los errores que saben cuánto esperar (como en service/history).
type retryAfterer interface{ RetryAfter() time.Duration }

// writeServiceError traduce un error de un servicio del núcleo a su código HTTP (docs/14 §2). El mensaje de
// los errores conocidos se conserva (ya está en español); el de los demás es genérico y el detalle va al registro.
func writeServiceError(w http.ResponseWriter, log *slog.Logger, where string, err error) {
	switch {
	case errors.Is(err, port.ErrInvalid):
		writeError(w, http.StatusBadRequest, codeInvalid, safeMessage(err))
	case errors.Is(err, port.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, safeMessage(err))
	case errors.Is(err, port.ErrNotConnected):
		writeError(w, http.StatusConflict, codeNotConnected, safeMessage(err))
	case errors.Is(err, port.ErrUnsupported):
		writeError(w, http.StatusUnprocessableEntity, codeUnsupported, safeMessage(err))
	case errors.Is(err, port.ErrRateLimited):
		secs := 5
		var ra retryAfterer
		if errors.As(err, &ra) && ra.RetryAfter() > 0 {
			secs = int((ra.RetryAfter() + time.Second - 1) / time.Second)
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeError(w, http.StatusTooManyRequests, codeRateLimited, safeMessage(err))
	default:
		log.Error("error interno", "where", where, "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, genericInternal)
	}
}

// safeMessage: el mensaje del error sin rutas de la carpeta de datos del servidor.
func safeMessage(err error) string {
	msg := err.Error()
	if d := paths.DataDir(); d != "" {
		msg = strings.ReplaceAll(msg, d, "<datos>")
	}
	return msg
}
