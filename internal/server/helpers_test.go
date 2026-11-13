package server

import (
	"io"
	"log/slog"
)

func newTestLogger(a, b io.Writer) *slog.Logger {
	return slog.New(fanout{slog.NewJSONHandler(a, nil), slog.NewJSONHandler(b, nil)})
}
