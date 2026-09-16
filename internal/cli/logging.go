package cli

import (
	"io"
	"log/slog"
)

// NewLogger builds the console logger for the validated level and format. The JSON Lines run log
// file is added by the run-state task as a second handler.
func NewLogger(w io.Writer, level slog.Level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if format == LogJSON {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
