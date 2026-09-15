package state

import "fmt"

// BeginPreview records the intended archive path before a preview is generated or deleted.
// Preview events are independent of the already committed video transaction, including when a
// later split catches up a video moved by an earlier run. Dst is the absolute preview path.
func (w *WAL) BeginPreview(rel, dst string, size int64, deleting bool) (Record, error) {
	w.mu.Lock()
	w.nextTx++
	step := StepPreviewBegin
	if deleting {
		step = StepPreviewDelete
	}
	rec, err := w.appendLocked(Record{TxID: formatTxID(w.runID, w.nextTx), Step: step,
		RelPath: rel, Dst: dst, Size: size})
	w.mu.Unlock()
	if err != nil {
		return rec, err
	}
	return rec, w.hit("wal:" + string(step))
}

// FinishPreview closes a preview event. A successful generation records its exact byte size;
// a failed attempt records a reason. Deletion uses StepPreviewDeleted.
func (w *WAL) FinishPreview(txid string, step Step, dst string, size int64, reason string) (Record, error) {
	w.mu.Lock()
	begin, ok := w.previews[txid]
	if !ok || (step != StepPreviewDone && step != StepPreviewFailed && step != StepPreviewDeleted) {
		w.mu.Unlock()
		return Record{}, fmt.Errorf("wal: invalid preview outcome for %s", txid)
	}
	if begin.Step == StepPreviewDelete && step != StepPreviewDeleted ||
		begin.Step == StepPreviewBegin && step == StepPreviewDeleted {
		w.mu.Unlock()
		return Record{}, fmt.Errorf("wal: invalid preview outcome %s for %s", step, txid)
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
