package state

import "fmt"

// EventWALVersion is the "v" of post-commit sidecar event records.
const EventWALVersion = 2

// PreviewWALVersion is the version of preview event records.
const PreviewWALVersion = EventWALVersion

// EventFamily names the steps of one kind of post-commit sidecar event: generation surrounded by
// Begin and Done or Failed, and restore cleanup surrounded by Delete and Deleted. Events of a family
// are independent of the committed transaction of the file they serve, including when a later run
// catches up a file moved by an earlier run.
type EventFamily struct {
	Name                                 string
	Begin, Done, Failed, Delete, Deleted Step
}

// PreviewEvents are the stage-2 preview generation and cleanup events.
var PreviewEvents = EventFamily{Name: "preview",
	Begin: StepPreviewBegin, Done: StepPreviewDone, Failed: StepPreviewFailed,
	Delete: StepPreviewDelete, Deleted: StepPreviewDeleted}

// eventFamilies lists every family a WAL accepts.
var eventFamilies = []EventFamily{PreviewEvents}

// Has reports whether step belongs to the family.
func (f EventFamily) Has(step Step) bool {
	switch step {
	case f.Begin, f.Done, f.Failed, f.Delete, f.Deleted:
		return step != ""
	}
	return false
}

// Opens reports whether step starts an event of the family.
func (f EventFamily) Opens(step Step) bool {
	return step != "" && (step == f.Begin || step == f.Delete)
}

// familyOf returns the family of an event step.
func familyOf(step Step) (EventFamily, bool) {
	for _, f := range eventFamilies {
		if f.Has(step) {
			return f, true
		}
	}
	return EventFamily{}, false
}

func isEventStep(step Step) bool {
	_, ok := familyOf(step)
	return ok
}

// BeginEvent records the intended archive path before a sidecar of family f is generated or,
// with deleting, removed. rel is the owning file and dst the absolute sidecar path; size is the
// recorded size of a sidecar to delete.
func (w *WAL) BeginEvent(f EventFamily, rel, dst string, size int64, deleting bool) (Record, error) {
	step := f.Begin
	if deleting {
		step = f.Delete
	}
	if step == "" {
		return Record{}, fmt.Errorf("wal: %s events have no begin step", f.Name)
	}
	w.mu.Lock()
	w.nextTx++
	rec, err := w.appendLocked(Record{TxID: formatTxID(w.runID, w.nextTx), Step: step,
		RelPath: rel, Dst: dst, Size: size})
	w.mu.Unlock()
	if err != nil {
		return rec, err
	}
	return rec, w.hit("wal:" + string(step))
}

// FinishEvent closes an open event of family f. A successful generation (f.Done) records its exact
// byte size, a failed attempt (f.Failed) a reason; a deletion closes with f.Deleted. An empty dst
// repeats the path of the begin record.
func (w *WAL) FinishEvent(f EventFamily, txid string, step Step, dst string, size int64, reason string) (Record, error) {
	w.mu.Lock()
	begin, ok := w.events[txid]
	valid := ok && f.Has(step) && !f.Opens(step) && f.Opens(begin.Step) &&
		(begin.Step == f.Delete) == (step == f.Deleted)
	if !valid {
		w.mu.Unlock()
		return Record{}, fmt.Errorf("wal: invalid %s outcome %s for %s", f.Name, step, txid)
	}
	if dst == "" {
		dst = begin.Dst
	}
	rec, err := w.appendLocked(Record{TxID: txid, Step: step, RelPath: begin.RelPath,
		Dst: dst, Size: size, Reason: reason})
	w.mu.Unlock()
	if err != nil {
		return rec, err
	}
	return rec, w.hit("wal:" + string(step))
}
