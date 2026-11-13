package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	"github.com/rs/zerolog"
)

// slogWriter recibe las líneas JSON de zerolog (el registrador de la librería de Meta) y las pasa a slog.
// Solo deja pasar Warn y Error, y de cada línea solo el mensaje y el error, ya sin ids largos:
// la librería registra, en niveles más bajos, cuerpos de peticiones y mensajes que NUNCA deben salir (02 §7).
type slogWriter struct {
	log *slog.Logger
}

func (w slogWriter) Write(p []byte) (int, error) {
	n := len(p)
	for _, line := range bytes.Split(bytes.TrimSpace(p), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Level   string `json:"level"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		level := slog.LevelWarn
		if rec.Level == "error" || rec.Level == "fatal" || rec.Level == "panic" {
			level = slog.LevelError
		}
		if rec.Message == "" && rec.Error == "" {
			continue
		}
		attrs := []any{"lib", "meta"}
		if rec.Error != "" {
			attrs = append(attrs, "err", Redact(rec.Error))
		}
		w.log.Log(context.Background(), level, Redact(rec.Message), attrs...)
	}
	return n, nil
}

// ZerologToSlog construye el registrador de zerolog que usan las librerías de Meta, volcado en slog.
func ZerologToSlog(log *slog.Logger) zerolog.Logger {
	return zerolog.New(slogWriter{log: log}).Level(zerolog.WarnLevel)
}

// LibContext pone el registrador en el contexto (la librería lo busca con zerolog.Ctx).
func LibContext(ctx context.Context, zl zerolog.Logger) context.Context {
	return zl.WithContext(ctx)
}
