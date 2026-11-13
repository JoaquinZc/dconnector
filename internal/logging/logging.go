// Package logging configura slog en JSON a archivo diario (02-ARQUITECTURA §7).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const keepDays = 14

var (
	mu    sync.Mutex
	level slog.LevelVar
	file  *os.File
)

// Init abre el archivo del día en dir, borra los de más de 14 días y fija el
// registro por defecto de slog. Devuelve el cierre del archivo.
func Init(dir string) (func() error, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: %w", err)
	}
	purgeOld(dir, time.Now())
	name := filepath.Join(dir, "dconnector-"+time.Now().Format("2006-01-02")+".log")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("logging: abrir %s: %w", name, err)
	}
	mu.Lock()
	file = f
	mu.Unlock()
	level.Set(slog.LevelInfo)
	slog.SetDefault(New(f))
	return Close, nil
}

// New crea un registrador JSON sobre w, con el nivel compartido (SetVerbose).
func New(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: &level}))
}

// SetVerbose activa DEBUG (ajuste «Registro detallado»).
func SetVerbose(v bool) {
	if v {
		level.Set(slog.LevelDebug)
	} else {
		level.Set(slog.LevelInfo)
	}
}

// Close cierra el archivo de registro.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if file == nil {
		return nil
	}
	err := file.Close()
	file = nil
	return err
}

// ForChannel devuelve un registrador con el atributo channel.
func ForChannel(l *slog.Logger, channel string) *slog.Logger {
	if l == nil {
		l = slog.Default()
	}
	return l.With("channel", channel)
}

// Last4 abrevia un id nativo a sus últimos 4 caracteres (nunca se registra completo).
func Last4(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	return "…" + string(r[len(r)-4:])
}

func purgeOld(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	limit := now.AddDate(0, 0, -keepDays)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, "dconnector-") || !strings.HasSuffix(n, ".log") {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(n, "dconnector-"), ".log")
		t, err := time.ParseInLocation("2006-01-02", day, time.Local)
		if err == nil && t.Before(limit) {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
}
