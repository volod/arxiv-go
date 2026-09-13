package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCheckpointRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), CheckpointFile)
	want := Checkpoint{
		RunID: "20260913T101500Z-1a2b3c4d", Op: "split", Phase: "execute",
		ScanCursor: []string{"projects", "2024", "zeta.pdf"}, RegistryOffset: 81234567, CandidateIndex: 42,
		WALOffset: 18233, Counters: Counters{Files: 1203344, Bytes: 4012345678901, VideosDone: 41, VideosSkipped: 1, VideoBytes: 9},
		ElapsedS: 5234.2, WrittenAt: time.Date(2026, 9, 13, 11, 44, 54, 123, time.UTC),
	}
	if err := WriteCheckpoint(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	want.V = CheckpointVersion
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, want)
	}
}

func TestCheckpointTornPartFileIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), CheckpointFile)
	first := Checkpoint{RunID: "20260913T101500Z-1a2b3c4d", Op: "scan", Phase: "scan", Counters: Counters{Files: 10}}
	if err := WriteCheckpoint(path, first); err != nil {
		t.Fatal(err)
	}
	// A crash in the middle of the next write leaves a torn part file next to the checkpoint.
	part := path + ".arxgo-part"
	if err := os.WriteFile(part, []byte(`{"v":1,"run_id":"20260913T1015`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCheckpoint(path)
	if err != nil || got.Counters.Files != 10 {
		t.Fatalf("ReadCheckpoint with torn part file = %+v, %v", got, err)
	}
	second := first
	second.Counters.Files = 20
	if err := WriteCheckpoint(path, second); err != nil {
		t.Fatalf("write over torn part file: %v", err)
	}
	if got, _ := ReadCheckpoint(path); got.Counters.Files != 20 {
		t.Errorf("files = %d, want 20", got.Counters.Files)
	}
	if _, err := os.Stat(part); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("part file left behind: %v", err)
	}
}

func TestReadCheckpointErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadCheckpoint(filepath.Join(dir, "missing.json")); !errors.Is(err, ErrNoCheckpoint) {
		t.Errorf("missing = %v, want ErrNoCheckpoint", err)
	}
	for name, content := range map[string]string{"torn": `{"v":1,"run_`, "version": `{"v":2,"run_id":"x"}`} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadCheckpoint(path); !errors.Is(err, ErrStateCorrupt) {
			t.Errorf("%s = %v, want ErrStateCorrupt", name, err)
		}
	}
}

func TestThrottle(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	th := NewThrottle(3, 30*time.Second, clock)
	if th.Add(1) || th.Add(1) {
		t.Fatal("due before count or interval")
	}
	if !th.Add(1) {
		t.Fatal("not due at count")
	}
	th.Reset()
	if th.Due() {
		t.Fatal("due right after reset")
	}
	now = now.Add(29 * time.Second)
	if th.Add(1) {
		t.Fatal("due before interval")
	}
	now = now.Add(time.Second)
	if !th.Due() {
		t.Fatal("not due at interval")
	}
	th.Reset()
	if th.Add(2) {
		t.Fatal("count was not reset")
	}
	onlyTime := NewThrottle(0, time.Minute, clock)
	if onlyTime.Add(1 << 40) {
		t.Error("count trigger active with every=0")
	}
}

func TestCountersSub(t *testing.T) {
	a := Counters{Files: 10, Bytes: 100, VideosDone: 3, VideoFreed: 7}
	b := Counters{Files: 4, Bytes: 40, VideosDone: 1, VideoFreed: 2}
	if got := a.Sub(b); got != (Counters{Files: 6, Bytes: 60, VideosDone: 2, VideoFreed: 5}) {
		t.Errorf("Sub = %+v", got)
	}
}
