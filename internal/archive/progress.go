package archive

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/volod/arxiv-go/internal/state"
)

// Stats are the live run counters. Workers update them with atomic adds; the progress reporter and
// checkpoints read snapshots.
type Stats struct {
	Files, Bytes                            atomic.Int64
	VideosDone, VideosSkipped, VideosFailed atomic.Int64
	VideoBytes                              atomic.Int64
	ArchiveWritten, ArchiveFreed            atomic.Int64
	VideoArchiveWritten, VideoArchiveFreed  atomic.Int64
	PreviewsDone, PreviewsFailed            atomic.Int64
	TextsDone, TextsFailed                  atomic.Int64
	CatiaDone, CatiaSkipped, CatiaFailed    atomic.Int64
	CatiaBytes                              atomic.Int64
	CatiaArchiveWritten, CatiaArchiveFreed  atomic.Int64
}

// Snapshot returns the current counters.
func (s *Stats) Snapshot() state.Counters {
	return state.Counters{
		Files: s.Files.Load(), Bytes: s.Bytes.Load(),
		VideosDone: s.VideosDone.Load(), VideosSkipped: s.VideosSkipped.Load(), VideosFailed: s.VideosFailed.Load(),
		VideoBytes:     s.VideoBytes.Load(),
		ArchiveWritten: s.ArchiveWritten.Load(), ArchiveFreed: s.ArchiveFreed.Load(),
		VideoWritten: s.VideoArchiveWritten.Load(), VideoFreed: s.VideoArchiveFreed.Load(),
		PreviewsDone: s.PreviewsDone.Load(), PreviewsFailed: s.PreviewsFailed.Load(),
		TextsDone: s.TextsDone.Load(), TextsFailed: s.TextsFailed.Load(),
		CatiaDone: s.CatiaDone.Load(), CatiaSkipped: s.CatiaSkipped.Load(), CatiaFailed: s.CatiaFailed.Load(),
		CatiaBytes:   s.CatiaBytes.Load(),
		CatiaWritten: s.CatiaArchiveWritten.Load(), CatiaFreed: s.CatiaArchiveFreed.Load(),
	}
}

// Restore sets the counters from a checkpoint when a run resumes.
func (s *Stats) Restore(c state.Counters) {
	s.Files.Store(c.Files)
	s.Bytes.Store(c.Bytes)
	s.VideosDone.Store(c.VideosDone)
	s.VideosSkipped.Store(c.VideosSkipped)
	s.VideosFailed.Store(c.VideosFailed)
	s.VideoBytes.Store(c.VideoBytes)
	s.ArchiveWritten.Store(c.ArchiveWritten)
	s.ArchiveFreed.Store(c.ArchiveFreed)
	s.VideoArchiveWritten.Store(c.VideoWritten)
	s.VideoArchiveFreed.Store(c.VideoFreed)
	s.PreviewsDone.Store(c.PreviewsDone)
	s.PreviewsFailed.Store(c.PreviewsFailed)
	s.TextsDone.Store(c.TextsDone)
	s.TextsFailed.Store(c.TextsFailed)
	s.CatiaDone.Store(c.CatiaDone)
	s.CatiaSkipped.Store(c.CatiaSkipped)
	s.CatiaFailed.Store(c.CatiaFailed)
	s.CatiaBytes.Store(c.CatiaBytes)
	s.CatiaArchiveWritten.Store(c.CatiaWritten)
	s.CatiaArchiveFreed.Store(c.CatiaFreed)
}

// Totals are the known work of a phase. A phase with zero Items has an unknown total (scan): its
// progress lines report entries, bytes seen and entries/s without an ETA. Otherwise lines report
// handled payload files and bytes against the totals, byte rate and ETA.
type Totals struct {
	Items int64
	Bytes int64
}

// Progress writes progress lines at most once per interval, plus one at each phase boundary.
type Progress struct {
	log      *slog.Logger
	stats    *Stats
	interval time.Duration
	now      func() time.Time

	// handled reads the payload counters of split and restore phases; nil for scan.
	handled func(state.Counters) payloadTotals

	mu         sync.Mutex
	phase      string
	totals     Totals
	phaseStart time.Time
	base       state.Counters // counters when the phase started
	lastAt     time.Time      // time of the last line
	last       state.Counters // counters at the last line
}

// NewProgress returns a reporter logging at info level.
func NewProgress(log *slog.Logger, stats *Stats, interval time.Duration, now func() time.Time) *Progress {
	return &Progress{log: log, stats: stats, interval: interval, now: now}
}

// StartPhase ends the current phase with a boundary line and starts measuring the next one.
func (p *Progress) StartPhase(name string, totals Totals) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase != "" {
		p.emit()
	}
	now, c := p.now(), p.stats.Snapshot()
	p.phase, p.totals, p.phaseStart, p.base, p.lastAt, p.last = name, totals, now, c, now, c
}

// Flush writes a boundary line for the current phase, for the end of a run.
func (p *Progress) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase != "" {
		p.emit()
	}
}

// Tick writes a line when at least the interval has passed since the previous line and reports
// whether it did.
func (p *Progress) Tick() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase == "" || p.now().Sub(p.lastAt) < p.interval {
		return false
	}
	p.emit()
	return true
}

// Run calls Tick for every value from ticks until ctx is done or ticks is closed.
func (p *Progress) Run(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
			p.Tick()
		}
	}
}

// emit logs one line; p.mu is held.
func (p *Progress) emit() {
	now, c := p.now(), p.stats.Snapshot()
	window := now.Sub(p.lastAt)
	cur, prev := p.measure(c), p.measure(p.last)
	if window <= 0 {
		// A boundary line right after the previous line: use the phase average.
		window, prev = now.Sub(p.phaseStart), p.measure(p.base)
	}
	attrs := []any{"phase", p.phase}
	if p.totals.Items == 0 {
		attrs = append(attrs,
			"entries", c.Files,
			"bytes", FormatBytes(c.Bytes),
			"rate", fmt.Sprintf("%.0f entries/s", perSecond(cur.items-prev.items, window)))
	} else {
		base := p.measure(p.base)
		rate := perSecond(cur.bytes-prev.bytes, window)
		attrs = append(attrs,
			"done", fmt.Sprintf("%d/%d", cur.items, p.totals.Items),
			"bytes", FormatBytes(cur.bytes)+"/"+FormatBytes(p.totals.Bytes),
			"rate", FormatBytes(int64(rate))+"/s",
			"eta", eta(p.totals, cur, base, now.Sub(p.phaseStart)),
			"skipped", p.payloadOf(c).skipped,
			"failed", p.payloadOf(c).failed)
	}
	p.log.Info("progress", attrs...)
	p.lastAt, p.last = now, c
}

type measured struct{ items, bytes int64 }

// measure picks the counters a phase reports: entries for scans, handled payload files otherwise.
func (p *Progress) measure(c state.Counters) measured {
	if p.totals.Items == 0 {
		return measured{items: c.Files, bytes: c.Bytes}
	}
	t := p.payloadOf(c)
	return measured{items: t.done + t.skipped + t.failed, bytes: t.bytes}
}

// payloadOf returns the payload counters of c; a reporter without a payload reads the video ones.
func (p *Progress) payloadOf(c state.Counters) payloadTotals {
	if p.handled == nil {
		return videoPayload.totals(c)
	}
	return p.handled(c)
}

func perSecond(n int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / d.Seconds()
}

// eta extrapolates the phase average rate over the remaining bytes, or items when the byte total
// is unknown.
func eta(t Totals, cur, base measured, elapsed time.Duration) string {
	doneBytes, doneItems := cur.bytes-base.bytes, cur.items-base.items
	var remaining float64
	switch {
	case cur.items >= t.Items && (t.Bytes == 0 || cur.bytes >= t.Bytes):
		return "0s"
	case t.Bytes > 0 && doneBytes > 0:
		remaining = float64(t.Bytes-cur.bytes) / float64(doneBytes)
	case doneItems > 0:
		remaining = float64(t.Items-cur.items) / float64(doneItems)
	default:
		return "unknown"
	}
	return FormatDuration(time.Duration(remaining * float64(elapsed)))
}

// FormatBytes formats a byte count with binary units, for example 812.4GiB.
func FormatBytes(n int64) string {
	const units = "KMGTPE"
	if n < 1024 && n > -1024 {
		return fmt.Sprintf("%dB", n)
	}
	v, i := float64(n)/1024, 0
	for (v >= 1024 || v <= -1024) && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f%ciB", v, units[i])
}

// FormatDuration formats a duration compactly: 3h41m, 5m12s or 42s.
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int64(d.Hours()), int64(d.Minutes())%60, int64(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
