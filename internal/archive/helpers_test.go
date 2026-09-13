package archive

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// clocks counts newClock calls. Each clock starts one minute after the previous one, so runs
// started from successive configurations have distinct, ordered start times (as real runs do);
// the video registry replays runs in start order.
var clocks atomic.Int64

func newClock() *fakeClock {
	start := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	return &fakeClock{t: start.Add(time.Duration(clocks.Add(1)) * time.Minute)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// recorder is a slog handler that keeps records as flat string maps.
type recorder struct {
	mu      sync.Mutex
	records []map[string]string
	attrs   []slog.Attr
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	m := map[string]string{"msg": rec.Message, "level": rec.Level.String()}
	for _, a := range r.attrs {
		m[a.Key] = a.Value.String()
	}
	rec.Attrs(func(a slog.Attr) bool {
		m[a.Key] = fmt.Sprint(a.Value.Any())
		return true
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, m)
	return nil
}

func (r *recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &recorder{attrs: append(append([]slog.Attr{}, r.attrs...), attrs...)}
}

func (r *recorder) WithGroup(string) slog.Handler { return r }

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (r *recorder) messages(msg string) []map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]string
	for _, m := range r.records {
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}
