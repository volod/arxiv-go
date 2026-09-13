package state

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
)

// RunLog is the append-only JSON Lines log of a run.
type RunLog struct {
	f       *os.File
	handler slog.Handler
}

// OpenRunLog opens or appends to path. Records at level and above are written as one JSON object
// per line with UTC times. A torn final line left by a crash is terminated first so the next
// record starts on its own line.
func OpenRunLog(path string, level slog.Level) (*RunLog, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := terminateLastLine(f); err != nil {
		f.Close()
		return nil, err
	}
	h := slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	})
	return &RunLog{f: f, handler: h}, nil
}

func terminateLastLine(f *os.File) error {
	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return err
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, fi.Size()-1); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	_, err = f.Write([]byte{'\n'})
	return err
}

// Handler returns the JSON handler writing to the log file.
func (l *RunLog) Handler() slog.Handler { return l.handler }

// Sync flushes the log file to stable storage.
func (l *RunLog) Sync() error { return l.f.Sync() }

// Close flushes and closes the log file.
func (l *RunLog) Close() error {
	err := l.f.Sync()
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Fanout returns a handler that passes each record to every handler enabled for its level, so one
// logger writes to the console and the run log with independent levels and formats.
func Fanout(handlers ...slog.Handler) slog.Handler { return fanout(handlers) }

type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range f {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}
