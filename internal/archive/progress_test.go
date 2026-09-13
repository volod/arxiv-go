package archive

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestProgressThrottleHonorsInterval(t *testing.T) {
	clock, rec, stats := newClock(), &recorder{}, &Stats{}
	p := NewProgress(slog.New(rec), stats, 10*time.Second, clock.Now)
	if p.Tick() {
		t.Fatal("line before any phase")
	}
	p.StartPhase("execute", Totals{Items: 100, Bytes: 1000 << 20})

	clock.Advance(9 * time.Second)
	if p.Tick() {
		t.Fatal("line before the interval")
	}
	stats.VideosDone.Add(9)
	stats.VideosSkipped.Add(1)
	stats.VideoBytes.Add(100 << 20)
	clock.Advance(time.Second)
	if !p.Tick() {
		t.Fatal("no line at the interval")
	}
	for range 5 {
		clock.Advance(time.Second)
		if p.Tick() {
			t.Fatal("line within the interval after a line")
		}
	}
	lines := rec.messages("progress")
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	want := map[string]string{"phase": "execute", "done": "10/100", "bytes": "100.0MiB/1000.0MiB",
		"rate": "10.0MiB/s", "eta": "1m30s", "skipped": "1", "failed": "0"}
	for k, v := range want {
		if lines[0][k] != v {
			t.Errorf("%s = %q, want %q (line %v)", k, lines[0][k], v, lines[0])
		}
	}

	// The rate covers the window since the previous line; the ETA uses the phase average.
	stats.VideoBytes.Add(300 << 20)
	stats.VideosDone.Add(30)
	clock.Advance(5 * time.Second) // 10s since the previous line, 20s into the phase
	if !p.Tick() {
		t.Fatal("no second line")
	}
	second := rec.messages("progress")[1]
	if second["rate"] != "30.0MiB/s" || second["eta"] != "30s" || second["done"] != "40/100" {
		t.Errorf("second line = %v", second)
	}
}

func TestProgressPhaseBoundaryLines(t *testing.T) {
	clock, rec, stats := newClock(), &recorder{}, &Stats{}
	p := NewProgress(slog.New(rec), stats, time.Hour, clock.Now)
	p.StartPhase("scan", Totals{})
	stats.Files.Add(5000)
	stats.Bytes.Add(3 << 30)
	clock.Advance(2 * time.Second)
	p.StartPhase("execute", Totals{Items: 2, Bytes: 10})
	stats.VideosDone.Add(2)
	stats.VideoBytes.Add(10)
	p.Flush()

	lines := rec.messages("progress")
	if len(lines) != 2 {
		t.Fatalf("boundary lines = %v", lines)
	}
	scan := lines[0]
	if scan["phase"] != "scan" || scan["entries"] != "5000" || scan["bytes"] != "3.0GiB" || scan["rate"] != "2500 entries/s" {
		t.Errorf("scan line = %v", scan)
	}
	if _, ok := scan["eta"]; ok {
		t.Error("scan line has an ETA although the total is unknown")
	}
	if exec := lines[1]; exec["phase"] != "execute" || exec["done"] != "2/2" || exec["eta"] != "0s" {
		t.Errorf("execute line = %v", exec)
	}
}

func TestProgressRunReadsTicks(t *testing.T) {
	clock, rec, stats := newClock(), &recorder{}, &Stats{}
	p := NewProgress(slog.New(rec), stats, time.Second, clock.Now)
	p.StartPhase("scan", Totals{})
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx, ticks); close(done) }()
	for i := range 6 {
		if i%2 == 0 {
			clock.Advance(time.Second)
		}
		ticks <- time.Time{}
	}
	cancel()
	<-done
	if n := len(rec.messages("progress")); n != 3 {
		t.Errorf("lines = %d, want 3 (one per elapsed interval)", n)
	}
}

func TestProgressUnknownETA(t *testing.T) {
	clock, rec, stats := newClock(), &recorder{}, &Stats{}
	p := NewProgress(slog.New(rec), stats, time.Second, clock.Now)
	p.StartPhase("execute", Totals{Items: 3})
	clock.Advance(time.Second)
	p.Tick()
	stats.VideosDone.Add(1)
	clock.Advance(time.Second)
	p.Tick()
	lines := rec.messages("progress")
	if lines[0]["eta"] != "unknown" || lines[1]["eta"] != "4s" {
		t.Errorf("eta = %q then %q, want unknown then 4s (items-based)", lines[0]["eta"], lines[1]["eta"])
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0B", 1023: "1023B", 1024: "1.0KiB", 812_400 << 20: "793.4GiB",
		3 << 40: "3.0TiB", 182 << 20: "182.0MiB", 1 << 62: "4.0EiB"} {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0s", 1400 * time.Millisecond: "1s", 42 * time.Second: "42s",
		5*time.Minute + 12*time.Second: "5m12s", 3*time.Hour + 41*time.Minute + 20*time.Second: "3h41m", 50 * time.Hour: "50h00m"} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
