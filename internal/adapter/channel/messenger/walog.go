package messenger

import (
	"context"
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"

	"dconnector/internal/adapter/channel/meta"
)

// walogger adapta *slog.Logger a la interfaz de registro de whatsmeow. Todo pasa por meta.Redact:
// nunca salen ids largos ni números completos (02 §7).
type walogger struct {
	log *slog.Logger
	mod string
}

func newWALogger(log *slog.Logger) *walogger { return &walogger{log: log} }

func (l *walogger) logf(level slog.Level, msg string, args ...any) {
	ctx := context.Background()
	if !l.log.Enabled(ctx, level) {
		return
	}
	text := meta.Redact(fmt.Sprintf(msg, args...))
	if l.mod != "" {
		l.log.Log(ctx, level, text, "lib", "e2ee/"+l.mod)
		return
	}
	l.log.Log(ctx, level, text, "lib", "e2ee")
}

func (l *walogger) Warnf(msg string, args ...any)  { l.logf(slog.LevelWarn, msg, args...) }
func (l *walogger) Errorf(msg string, args ...any) { l.logf(slog.LevelError, msg, args...) }
func (l *walogger) Infof(msg string, args ...any)  { l.logf(slog.LevelInfo, msg, args...) }
func (l *walogger) Debugf(msg string, args ...any) { l.logf(slog.LevelDebug, msg, args...) }

func (l *walogger) Sub(module string) waLog.Logger {
	mod := module
	if l.mod != "" {
		mod = l.mod + "/" + module
	}
	return &walogger{log: l.log, mod: mod}
}
