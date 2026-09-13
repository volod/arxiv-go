package scanner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func writeFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDetectFiles(t *testing.T) {
	opts := NewDetectOptions(1<<20, nil)
	mp4 := writeFixture(t, "clip.txt", ftypBox("isom", "isom", "mp41"))
	got, err := Detect(mp4, 1<<20, opts)
	want := FileType{MIME: "video/mp4", Type: "mp4", IsBinary: true, IsVideo: true, IsMedia: true, IsLarge: true}
	if err != nil || got != want {
		t.Fatalf("Detect(mp4) = %+v, %v; want %+v", got, err, want)
	}
	empty := writeFixture(t, "empty.mp4", nil)
	if got, err := Detect(empty, 0, opts); err != nil || got != (FileType{MIME: MIMEEmpty}) {
		t.Fatalf("Detect(empty) = %+v, %v", got, err)
	}
}

func TestDetectLargeThreshold(t *testing.T) {
	const threshold = 4096
	p := writeFixture(t, "f.bin", randomBytes(16))
	for _, tt := range []struct {
		size, threshold int64
		want            bool
	}{
		{threshold - 1, threshold, false},
		{threshold, threshold, true},
		{threshold + 1, threshold, true},
		{0, threshold, false},
		{1 << 40, 0, false}, // no threshold configured
	} {
		got, err := Detect(p, tt.size, NewDetectOptions(tt.threshold, nil))
		if err != nil || got.IsLarge != tt.want {
			t.Errorf("size %d threshold %d: IsLarge=%v err=%v, want %v", tt.size, tt.threshold, got.IsLarge, err, tt.want)
		}
	}
	// The flag follows the recorded size, so an exactly-threshold file on disk is large.
	exact := writeFixture(t, "exact.bin", randomBytes(threshold))
	info, _ := os.Stat(exact)
	if got, _ := Detect(exact, info.Size(), NewDetectOptions(threshold, nil)); !got.IsLarge {
		t.Error("file of exactly the threshold size is not large")
	}
}

func TestDetectReadsOnlyTheHead(t *testing.T) {
	data := append(ftypBox("isom", "isom"), randomBytes(1<<20)...)
	p := writeFixture(t, "big.mp4", data)
	head, err := readHead(p, make([]byte, DetectLimit))
	if err != nil || len(head) != DetectLimit {
		t.Fatalf("readHead: %d bytes, %v; want %d", len(head), err, DetectLimit)
	}
	short := writeFixture(t, "short", []byte("abc"))
	if head, err := readHead(short, make([]byte, DetectLimit)); err != nil || string(head) != "abc" {
		t.Fatalf("readHead(short) = %q, %v", head, err)
	}
}

func TestDetectErrors(t *testing.T) {
	opts := NewDetectOptions(1, nil)
	if _, err := Detect(filepath.Join(t.TempDir(), "missing"), 1, opts); err == nil {
		t.Error("missing file: no error")
	}
	if _, err := Detect(t.TempDir(), 1, opts); err == nil {
		t.Error("directory: no error")
	}
	if runtime.GOOS != "linux" {
		return
	}
	if os.Geteuid() == 0 {
		t.Log("running as root bypasses permission checks; unreadable file case skipped")
	} else {
		locked := writeFixture(t, "locked.mp4", ftypBox("isom"))
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
		if ft, err := Detect(locked, 1, opts); err == nil || ft != (FileType{}) {
			t.Errorf("unreadable file: got %+v, %v", ft, err)
		}
	}
}

// A regular file replaced by a FIFO after the walk must fail fast instead of blocking the open.
func TestDetectFIFODoesNotBlock(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("FIFO fixture used on Linux only")
	}
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo not available")
	}
	p := filepath.Join(t.TempDir(), "swapped.mp4")
	if out, err := exec.Command(mkfifo, p).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Detect(p, 1, NewDetectOptions(1, nil))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("FIFO: no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Detect blocked on a FIFO")
	}
}

// The scan runs Detect from a worker pool; pooled buffers must not leak between files.
func TestDetectConcurrent(t *testing.T) {
	opts := NewDetectOptions(1<<20, nil)
	fixtures := map[string]string{
		writeFixture(t, "a.bin", ftypBox("isom", "isom")): "video/mp4",
		writeFixture(t, "b.bin", []byte("short text\n")):  "text/plain",
		writeFixture(t, "c.bin", pngSignature):            "image/png",
		writeFixture(t, "d.bin", wavHeader()):             "audio/wav",
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				for p, want := range fixtures {
					if ft, err := Detect(p, 1, opts); err != nil || ft.MIME != want {
						t.Errorf("%s: got %q, %v; want %q", filepath.Base(p), ft.MIME, err, want)
						return
					}
				}
			}
		})
	}
	wg.Wait()
}
