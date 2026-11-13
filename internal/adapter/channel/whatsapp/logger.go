package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// waLogger adapta *slog.Logger a la interfaz de registro de whatsmeow (07 §2.5).
// Todo lo que la librería escribe pasa por redact: nunca salen números completos (02 §7).
type waLogger struct {
	log *slog.Logger
	mod string
}

func newWALogger(log *slog.Logger) waLog.Logger { return &waLogger{log: log} }

var longDigits = regexp.MustCompile(`\d{7,}`)

// redact reemplaza cualquier racha larga de dígitos (teléfonos, ids numéricos) por «…» + últimos 4.
func redact(s string) string {
	return longDigits.ReplaceAllStringFunc(s, func(m string) string { return "…" + m[len(m)-4:] })
}

func (l *waLogger) logf(level slog.Level, msg string, args ...any) {
	ctx := context.Background()
	// whatsmeow vuelca nodos XML y nombres de contactos en Debug (02 §7): nunca sale, ni con el
	// «Registro detallado» encendido.
	if level < slog.LevelInfo || !l.log.Enabled(ctx, level) {
		return
	}
	text := redact(fmt.Sprintf(msg, args...))
	if l.mod != "" {
		l.log.Log(ctx, level, text, "lib", l.mod)
		return
	}
	l.log.Log(ctx, level, text)
}

func (l *waLogger) Warnf(msg string, args ...any)  { l.logf(slog.LevelWarn, msg, args...) }
func (l *waLogger) Errorf(msg string, args ...any) { l.logf(slog.LevelError, msg, args...) }
func (l *waLogger) Infof(msg string, args ...any)  { l.logf(slog.LevelInfo, msg, args...) }
func (l *waLogger) Debugf(msg string, args ...any) { l.logf(slog.LevelDebug, msg, args...) }

func (l *waLogger) Sub(module string) waLog.Logger {
	mod := module
	if l.mod != "" {
		mod = l.mod + "/" + module
	}
	return &waLogger{log: l.log, mod: mod}
}
