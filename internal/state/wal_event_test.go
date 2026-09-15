package state

import (
	"errors"
	"os"
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
	p, err := w.BeginEvent(PreviewEvents, "clip.mp4", "/archive/clip-img01.png", 0, false)
	if err != nil || p.V != PreviewWALVersion {
		t.Fatalf("preview begin = %+v, %v", p, err)
	}
	if _, err := w.FinishEvent(PreviewEvents, p.TxID, StepPreviewDone, "", 99, ""); err != nil {
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
	if _, err := w.FinishEvent(PreviewEvents, p.TxID, StepPreviewDone, "", 99, ""); err == nil {
		t.Fatal("duplicate preview outcome accepted")
	}
	rows, err := ReadWALRecords(path)
	if err != nil || len(rows) != 4 || rows[2].V != PreviewWALVersion || rows[3].Size != 99 {
		t.Fatalf("preview records = %+v, %v", rows, err)
	}
}

func TestTextEventsUseSharedEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.jsonl")
	w, err := OpenWAL(path, walTestRun, WALOptions{})
	if err != nil {
		t.Fatal(err)
	}
	begin, err := w.BeginEvent(TextEvents, "cad/fixture.CATPart", "/archive/cad/fixture.CATPart.text.md", 0, false)
	if err != nil || begin.V != EventWALVersion || begin.Step != StepTextBegin {
		t.Fatalf("text begin = %+v, %v", begin, err)
	}
	if _, err := w.FinishEvent(TextEvents, begin.TxID, StepTextDone, "", 80, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := w.FinishEvent(PreviewEvents, begin.TxID, StepPreviewDone, "", 1, ""); err == nil {
		t.Fatal("preview outcome accepted for a text event")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// fixtureEvents is a second sidecar family, registered only in tests, proving that the begin,
// finish and replay rules are shared rather than preview-specific.
var fixtureEvents = EventFamily{Name: "fixture", Begin: "fixture_begin", Done: "fixture_done",
	Failed: "fixture_failed", Delete: "fixture_delete", Deleted: "fixture_deleted"}

func TestEventFamiliesShareMechanismAndStayApart(t *testing.T) {
	saved := eventFamilies
	eventFamilies = []EventFamily{PreviewEvents, fixtureEvents}
	t.Cleanup(func() { eventFamilies = saved })

	path := filepath.Join(t.TempDir(), "wal.jsonl")
	w, err := OpenWAL(path, walTestRun, WALOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := w.BeginEvent(fixtureEvents, "cad/fixture-part.CATPart", "/archive/cad/fixture-part.CATPart.text.md", 0, false)
	if err != nil || gen.V != EventWALVersion || gen.Step != fixtureEvents.Begin {
		t.Fatalf("fixture begin = %+v, %v", gen, err)
	}
	del, err := w.BeginEvent(fixtureEvents, "cad/fixture-part.CATPart", "/archive/cad/old.text.md", 12, true)
	if err != nil || del.Step != fixtureEvents.Delete {
		t.Fatalf("fixture delete = %+v, %v", del, err)
	}
	for _, bad := range []struct {
		f    EventFamily
		txid string
		step Step
	}{
		{PreviewEvents, gen.TxID, StepPreviewDone}, // another family's outcome
		{fixtureEvents, gen.TxID, fixtureEvents.Deleted},
		{fixtureEvents, del.TxID, fixtureEvents.Done},
		{fixtureEvents, gen.TxID, fixtureEvents.Begin},
	} {
		if _, err := w.FinishEvent(bad.f, bad.txid, bad.step, "", 1, ""); err == nil {
			t.Errorf("accepted %s outcome %s for %s", bad.f.Name, bad.step, bad.txid)
		}
	}
	if _, err := w.FinishEvent(fixtureEvents, gen.TxID, fixtureEvents.Done, "", 42, ""); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = OpenWAL(path, walTestRun, WALOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.FinishEvent(fixtureEvents, del.TxID, fixtureEvents.Deleted, "", 12, ""); err != nil {
		t.Fatalf("reopened WAL lost the open fixture delete: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// A preview outcome for a fixture begin is corrupt on replay.
	data := mustReadFile(t, path)
	line := `{"v":2,"txid":"` + walTestRun + `-000009","seq":9,"step":"fixture_begin","ts":"2026-09-15T12:00:00Z","rel_path":"a","dst":"/archive/a.text.md"}` + "\n" +
		`{"v":2,"txid":"` + walTestRun + `-000009","seq":10,"step":"preview_done","ts":"2026-09-15T12:00:00Z","rel_path":"a","dst":"/archive/a.text.md","size":1}` + "\n"
	if err := os.WriteFile(path, append(data, line...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWALRecords(path); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWAL(path, walTestRun, WALOptions{}); !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("mixed-family event accepted on open: %v", err)
	}
}
