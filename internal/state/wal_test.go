package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const walTestRun = "20260913T101500Z-1a2b3c4d"

func openTestWAL(t *testing.T, dir string) *WAL {
	t.Helper()
	w, err := OpenWAL(filepath.Join(dir, WALFile), walTestRun, WALOptions{
		Now: func() time.Time { return testNow.UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestWALBeginIsOneJSONLineWithContractFields(t *testing.T) {
	w := openTestWAL(t, t.TempDir())
	mtime := time.Date(2024, 5, 1, 10, 22, 3, 0, time.UTC)
	rec, err := w.Begin(Begin{
		Op: "split", RelPath: "projects/2024/interview.mp4",
		Src:  "/data/archive/projects/2024/interview.mp4",
		Dst:  "/mnt/nas/video/projects/2024/interview.mp4",
		Size: 734003200, Mtime: mtime, Transfer: TransferCopy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.TxID != walTestRun+"-000001" || rec.Seq != 1 || rec.Step != StepBegin || rec.V != 1 {
		t.Fatalf("record = %+v", rec)
	}
	data, err := os.ReadFile(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte("\n")) != 1 || bytes.Contains(data, []byte("\n\n")) {
		t.Fatalf("want one JSON line, got %q", data)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"v", "txid", "seq", "step", "ts", "op", "rel_path", "src", "dst", "size", "mtime", "transfer"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing field %s in %v", k, m)
		}
	}
}

func TestWALLaterStepsOmitBeginPayload(t *testing.T) {
	w := openTestWAL(t, t.TempDir())
	begin, err := w.Begin(Begin{Op: "split", RelPath: "a.mp4", Src: "s", Dst: "d", Size: 1, Transfer: TransferCopy})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(begin.TxID, StepVerified, Record{SHA256: "abc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(begin.TxID, StepAborted, Record{Reason: "unplaced"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(mustReadFile(t, w.Path()))), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d", len(lines))
	}
	var verified, aborted map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &verified); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[2]), &aborted); err != nil {
		t.Fatal(err)
	}
	if verified["sha256"] != "abc" || verified["src"] != nil || verified["rel_path"] != nil {
		t.Errorf("verified = %v", verified)
	}
	if aborted["reason"] != "unplaced" || aborted["step"] != "aborted" {
		t.Errorf("aborted = %v", aborted)
	}
}

func TestWALTornFinalLineIsTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, WALFile)
	good := `{"v":1,"txid":"` + walTestRun + `-000001","seq":1,"step":"begin","ts":"2026-09-13T07:15:00Z","op":"split","rel_path":"a.mp4","src":"s","dst":"d","size":3}` + "\n"
	if err := os.WriteFile(path, []byte(good+`{"v":1,"txid":"`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := openTestWAL(t, dir)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != good {
		t.Fatalf("after open:\n%s\nwant:\n%s", got, good)
	}
	if n := len(w.OpenTransactions()); n != 1 {
		t.Fatalf("open txs = %d", n)
	}
	rec, err := w.Begin(Begin{Op: "split", RelPath: "b.mp4", Src: "s2", Dst: "d2", Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Seq != 2 || rec.TxID != walTestRun+"-000002" {
		t.Fatalf("resumed seq/txid = %d %s", rec.Seq, rec.TxID)
	}
}

func TestWALTornFinalLineWithNewlineIsTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, WALFile)
	good := `{"v":1,"txid":"` + walTestRun + `-000001","seq":1,"step":"begin","ts":"2026-09-13T07:15:00Z","op":"split","rel_path":"a.mp4","src":"s","dst":"d","size":3}` + "\n"
	if err := os.WriteFile(path, []byte(good+"not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = openTestWAL(t, dir)
	got := mustReadFile(t, path)
	if string(got) != good {
		t.Fatalf("after open:\n%s\nwant:\n%s", got, good)
	}
}

func TestWALCorruptMiddleLineIsStateCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, WALFile)
	line := `{"v":1,"txid":"` + walTestRun + `-000001","seq":1,"step":"begin","ts":"2026-09-13T07:15:00Z","op":"split","rel_path":"a.mp4","src":"s","dst":"d","size":3}` + "\n"
	if err := os.WriteFile(path, []byte(line+"this is not json\n"+line), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := OpenWAL(path, walTestRun, WALOptions{})
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("err = %v, want ErrStateCorrupt", err)
	}
}

func TestWALUnsupportedVersionIsCorruptEvenOnLastLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, WALFile)
	if err := os.WriteFile(path, []byte(`{"v":2,"txid":"`+walTestRun+`-000001","seq":1,"step":"begin"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := OpenWAL(path, walTestRun, WALOptions{})
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("err = %v, want ErrStateCorrupt", err)
	}
}

func TestWALCopiedAndVerifiedSkipFsyncPolicy(t *testing.T) {
	if stepNeedsFsync(StepCopied) || stepNeedsFsync(StepVerified) {
		t.Fatal("copied/verified must not require fsync")
	}
	for _, s := range []Step{StepBegin, StepPlaced, StepStubbed, StepStubRemoved, StepSourceRemoved, StepCommit, StepAborted} {
		if !stepNeedsFsync(s) {
			t.Errorf("%s should fsync", s)
		}
	}
}

func TestWALOpenTransactionsAreSeqOrdered(t *testing.T) {
	w := openTestWAL(t, t.TempDir())
	a, err := w.Begin(Begin{Op: "split", RelPath: "a.mp4", Src: "sa", Dst: "da", Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.Begin(Begin{Op: "split", RelPath: "b.mp4", Src: "sb", Dst: "db", Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(a.TxID, StepCopied, Record{}); err != nil {
		t.Fatal(err)
	}
	open := w.OpenTransactions()
	if len(open) != 2 || open[0].Begin.TxID != a.TxID || open[1].Begin.TxID != b.TxID {
		t.Fatalf("open = %+v", open)
	}
	if open[0].Last.Step != StepCopied || open[1].Last.Step != StepBegin {
		t.Fatalf("last steps = %s, %s", open[0].Last.Step, open[1].Last.Step)
	}
}

func TestWALRejectsAppendAfterCommit(t *testing.T) {
	w := openTestWAL(t, t.TempDir())
	rec, err := w.Begin(Begin{Op: "split", RelPath: "a.mp4", Src: "s", Dst: "d", Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(rec.TxID, StepCommit, Record{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(rec.TxID, StepAborted, Record{}); err == nil {
		t.Fatal("append after commit succeeded")
	}
	if !w.Committed().Has("a.mp4") || w.Committed().Has("missing") {
		t.Fatal("committed set")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
