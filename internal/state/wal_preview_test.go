package state

import (
	"path/filepath"
	"testing"
)

func TestPreviewEventsSurviveWALReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.jsonl")
	w, err := OpenWAL(path, walTestRun, WALOptions{})
	if err != nil {
		t.Fatal(err)
	}
	begin, err := w.Begin(Begin{Op: "split", RelPath: "clip.mp4", Src: "source", Dst: "video", Size: 9})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(begin.TxID, StepCommit, Record{}); err != nil {
		t.Fatal(err)
	}
	p, err := w.BeginPreview("clip.mp4", "/archive/clip-img01.png", 0, false)
	if err != nil || p.V != PreviewWALVersion {
		t.Fatalf("preview begin = %+v, %v", p, err)
	}
	if _, err := w.FinishPreview(p.TxID, StepPreviewDone, "", 99, ""); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = OpenWAL(path, walTestRun, WALOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if len(w.OpenTransactions()) != 0 || !w.Committed().Has("clip.mp4") {
		t.Fatal("preview events changed committed transaction state")
	}
	if _, err := w.FinishPreview(p.TxID, StepPreviewDone, "", 99, ""); err == nil {
		t.Fatal("duplicate preview outcome accepted")
	}
	rows, err := ReadWALRecords(path)
	if err != nil || len(rows) != 4 || rows[2].V != PreviewWALVersion || rows[3].Size != 99 {
		t.Fatalf("preview records = %+v, %v", rows, err)
	}
}
