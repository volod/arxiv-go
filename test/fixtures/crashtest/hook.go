// Package crashtest is the crash-injection helper for WAL and recovery tests.
// Production code never imports it. Split and restore tests reuse the hook and
// the fake operation against the generic recovery engine.
package crashtest

import (
	"errors"
	"sync"
)

// ErrCrash is returned (or panicked) when the hook hits the configured point.
var ErrCrash = errors.New("injected crash")

// Hook counts After calls and fails at FailAt. Production WAL/resolver crash
// hooks are this function: hook.After.
type Hook struct {
	mu     sync.Mutex
	FailAt string
	Panic  bool
	Hits   map[string]int
}

// After records point and fails when it equals FailAt.
func (h *Hook) After(point string) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.Hits == nil {
		h.Hits = make(map[string]int)
	}
	h.Hits[point]++
	if h.FailAt == "" || h.FailAt != point {
		return nil
	}
	if h.Panic {
		panic(ErrCrash)
	}
	return ErrCrash
}

// Func returns a state.CrashHook.
func (h *Hook) Func() func(string) error {
	if h == nil {
		return nil
	}
	return h.After
}

// Catch runs fn and converts a panic of ErrCrash into a returned error.
func Catch(fn func() error) (err error) {
	defer func() {
		if x := recover(); x != nil {
			if e, ok := x.(error); ok && errors.Is(e, ErrCrash) {
				err = ErrCrash
				return
			}
			panic(x)
		}
	}()
	return fn()
}

// CopyPoints is every WAL step and filesystem effect on the copy path (split).
var CopyPoints = []string{
	"wal:begin", "fs:copy", "wal:copied", "wal:verified", "fs:place",
	"wal:placed", "fs:description", "wal:described", "fs:source_removed",
	"wal:source_removed", "wal:commit",
}

// RenamePoints is every WAL step and filesystem effect on the same-device rename path (split).
var RenamePoints = []string{
	"wal:begin", "fs:place", "wal:placed", "fs:description", "wal:described", "wal:commit",
}

// RestoreCopyPoints is the copy path with description_removed in place of described.
var RestoreCopyPoints = []string{
	"wal:begin", "fs:copy", "wal:copied", "wal:verified", "fs:place",
	"wal:placed", "fs:description", "wal:description_removed", "fs:source_removed",
	"wal:source_removed", "wal:commit",
}

// RestoreRenamePoints is the rename path with description_removed in place of described.
var RestoreRenamePoints = []string{
	"wal:begin", "fs:place", "wal:placed", "fs:description", "wal:description_removed", "wal:commit",
}
