package fsops

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestAtomicWriteFileCreatesAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := AtomicWriteFile(path, []byte(`{"v":1,"phase":"scan"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertContent(t, path, []byte(`{"v":1,"phase":"scan"}`))
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v (%v), want 0600", fi.Mode().Perm(), err)
		}
	}
	if err := AtomicWriteFile(path, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertContent(t, path, []byte(`{"v":1}`))
	assertMissing(t, PartPath(path))
}

func TestAtomicWriteFailureKeepsPreviousContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arxgo-videos.csv")
	old := []byte("rel_path,size\nclip.mp4,1\n")
	writeFile(t, path, old)
	boom := errors.New("injected")
	err := AtomicWrite(path, 0o644, func(w io.Writer) error {
		w.Write(bytes.Repeat([]byte("partial row\n"), 100000))
		// Mid-write, readers of path still see the complete old content.
		assertContent(t, path, old)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want injected error", err)
	}
	assertContent(t, path, old)
	assertMissing(t, PartPath(path))
}

func TestAtomicWriteReplacesStalePartFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	writeFile(t, PartPath(path), []byte("torn"))
	if err := AtomicWriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertContent(t, path, []byte("{}"))
	assertMissing(t, PartPath(path))
}

func TestAtomicWriteNeverExposesPartialContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	versions := [][]byte{bytes.Repeat([]byte("A"), 1<<20), bytes.Repeat([]byte("B"), 1<<20+1)}
	if err := AtomicWriteFile(path, versions[0], 0o644); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var reads, partial int
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			got, err := os.ReadFile(path)
			if runtime.GOOS == "windows" {
				// Go opens files without FILE_SHARE_DELETE, so an open reader
				// blocks the replacement; leave gaps for the writer's retries.
				time.Sleep(2 * time.Millisecond)
			}
			if err != nil {
				// Windows may refuse to open a file that is being replaced.
				if runtime.GOOS != "windows" {
					partial++
				}
				continue
			}
			reads++
			if !bytes.Equal(got, versions[0]) && !bytes.Equal(got, versions[1]) {
				partial++
			}
		}
	}()
	for i := range 60 {
		if err := AtomicWriteFile(path, versions[i%2], 0o644); err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if partial != 0 {
		t.Fatalf("%d of %d reads saw partial content or a missing file", partial, reads)
	}
}
