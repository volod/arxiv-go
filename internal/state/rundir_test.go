package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 13, 10, 15, 0, 0, time.FixedZone("EEST", 3*3600))

func TestNewRunID(t *testing.T) {
	id, err := NewRunID(testNow, bytes.NewReader([]byte{0x1a, 0x2b, 0x3c, 0x4d}))
	if err != nil {
		t.Fatal(err)
	}
	if id != "20260913T071500Z-1a2b3c4d" {
		t.Errorf("id = %q (must use UTC)", id)
	}
	if !ValidRunID(id) {
		t.Error("generated id is not valid")
	}
	for _, bad := range []string{"", "../../etc", "20260913T071500Z-1A2B3C4D", "20260913T071500Z-1a2b3c4d/..", "20260913T071500-1a2b3c4d"} {
		if ValidRunID(bad) {
			t.Errorf("ValidRunID(%q) = true", bad)
		}
	}
	if _, err := NewRunID(testNow, bytes.NewReader(nil)); err == nil {
		t.Error("short random source accepted")
	}
}

func TestCreateRunDirRetriesOnCollision(t *testing.T) {
	root := t.TempDir()
	same := bytes.Repeat([]byte{0xaa, 0xbb, 0xcc, 0xdd}, 2)
	first, err := CreateRunDir(root, testNow, bytes.NewReader(same[:4]))
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateRunDir(root, testNow, bytes.NewReader(append(same[:4], 1, 2, 3, 4)))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || second.ID != "20260913T071500Z-01020304" {
		t.Errorf("ids = %q, %q", first.ID, second.ID)
	}
	want := filepath.Join(root, ".arxgo", "runs", second.ID)
	if second.Path != want || second.File(ReportFile) != filepath.Join(want, "report.json") {
		t.Errorf("path = %q, want %q", second.Path, want)
	}
	if fi, err := os.Stat(second.Path); err != nil || !fi.IsDir() {
		t.Errorf("run dir not created: %v", err)
	}
	done, err := second.Complete()
	if err != nil || done {
		t.Errorf("new run Complete = %v, %v", done, err)
	}
	if err := WriteReport(second.File(ReportFile), Report{RunID: second.ID}); err != nil {
		t.Fatal(err)
	}
	if done, _ := second.Complete(); !done {
		t.Error("run with report is not complete")
	}
	if _, err := OpenRunDir(root, second.ID); err != nil {
		t.Errorf("OpenRunDir: %v", err)
	}
	if _, err := OpenRunDir(root, "../x"); !errors.Is(err, ErrStateCorrupt) {
		t.Errorf("OpenRunDir with bad id = %v", err)
	}
}

func TestCurrentPointer(t *testing.T) {
	root := t.TempDir()
	if id, err := ReadCurrent(root); err != nil || id != "" {
		t.Fatalf("missing current = %q, %v", id, err)
	}
	const id = "20260913T071500Z-1a2b3c4d"
	if err := WriteCurrent(root, id); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadCurrent(root); err != nil || got != id {
		t.Fatalf("ReadCurrent = %q, %v", got, err)
	}
	if err := WriteCurrent(root, "not-an-id"); err == nil {
		t.Error("invalid id written")
	}
	if err := os.WriteFile(filepath.Join(root, ".arxgo", "current"), []byte("../../etc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCurrent(root); !errors.Is(err, ErrStateCorrupt) {
		t.Errorf("corrupt current = %v, want ErrStateCorrupt", err)
	}
}

func TestRunOptionsSameDefinition(t *testing.T) {
	o := RunOptions{Op: "split", Defining: []byte(`{"Archive": "/a", "Verify": "size"}`)}
	if !o.SameDefinition("split", []byte(`{"Archive":"/a","Verify":"size"}`)) {
		t.Error("formatting difference treated as a different definition")
	}
	if o.SameDefinition("split", []byte(`{"Archive":"/a","Verify":"hash"}`)) {
		t.Error("different option treated as same")
	}
	if o.SameDefinition("scan", []byte(`{"Archive":"/a","Verify":"size"}`)) {
		t.Error("different operation treated as same")
	}
}

func TestReadJSONCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	var o RunOptions
	if err := ReadJSON(path, &o); !errors.Is(err, ErrStateCorrupt) || !strings.Contains(err.Error(), path) {
		t.Errorf("ReadJSON = %v", err)
	}
}
