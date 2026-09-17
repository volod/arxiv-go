package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// A split after a scan opens only the files added since, and its descriptions and video registry
// take the metadata of the reused rows. A description writer that read the registry before the scan
// (as recovery does) uses the scan's rows.
func TestSplitReusesScanRows(t *testing.T) {
	t.Parallel()
	r := newRoots(t)
	writeScanFile(t, r.archive, "media/clip.mp4", videoFixture)
	writeScanFile(t, r.archive, "notes.txt", []byte("keep"))
	det := &countingDetector{root: r.archive}
	cfg, c := splitConfig(r, "auto")
	c.Scan.detectFile = det.detect

	sc := c.Scan
	sc.Preflight = true
	if res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 3), sc); res.Status != StatusCompleted {
		t.Fatalf("scan = %v (%v)", res.Status, res.Err)
	}
	wantOpened(t, "scan", det.take(), "media/clip.mp4", "notes.txt")
	c.Descriptions.(*MarkdownDescription).lookup("media/clip.mp4") // caches the registry without the addition

	writeScanFile(t, r.archive, "media/new.mp4", videoFixture)
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	wantOpened(t, "split", det.take(), "media/new.mp4")
	for _, rel := range []string{"media/clip.mp4", "media/new.mp4"} {
		d := string(mustRead(t, filepath.Join(r.archive, filepath.FromSlash(rel)+".md")))
		if !strings.Contains(d, "\nfile_mime: video/mp4\n") || !strings.Contains(d, "\nvideo: ") || !strings.Contains(d, "320x240") {
			t.Errorf("description of %s lacks the scan row metadata:\n%s", rel, d)
		}
	}
	videos, err := report.LoadVideoFile(filepath.Join(r.archive, scanner.VideoRegistryName))
	if err != nil || len(videos) != 2 {
		t.Fatalf("video registry: %d rows (%v)", len(videos), err)
	}
	for _, row := range videos {
		if row.FileMIME != "video/mp4" || row.Metadata.Media == nil || row.Metadata.Media.Width != 320 {
			t.Errorf("video row %s lacks the scan row metadata: %+v", row.RelPath, row)
		}
	}

	// The registry split wrote is the one --redetect writes, and a scan after it opens nothing.
	split := mustRead(t, c.Scan.Registry)
	if res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 3), sc); res.Status != StatusCompleted {
		t.Fatalf("scan after split = %v (%v)", res.Status, res.Err)
	}
	wantOpened(t, "scan after split", det.take())
	sc.Redetect = true
	if res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 3), sc); res.Status != StatusCompleted {
		t.Fatalf("redetect scan = %v (%v)", res.Status, res.Err)
	}
	if got := mustRead(t, c.Scan.Registry); !bytes.Equal(got, split) {
		t.Errorf("split registry differs from the --redetect registry\n--- split\n%s\n--- redetect\n%s", split, got)
	}
}

// A resumed scan continues only while its base registry is the one its checkpoint recorded;
// otherwise it scans again from the start with the base it finds.
func TestScanResumeRestartsOnChangedBase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		change  func(t *testing.T, f *reuseFixture)
		restart bool
	}{
		{"unchanged", func(*testing.T, *reuseFixture) {}, false},
		{"deleted", func(t *testing.T, f *reuseFixture) {
			if err := os.Remove(f.registry); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"edited", func(t *testing.T, f *reuseFixture) {
			if err := os.WriteFile(f.registry, append(mustRead(t, f.registry), '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"replaced by another stamped registry", func(t *testing.T, f *reuseFixture) {
			// The same registry without the notes.txt row, stamped: reusable, but another base.
			var kept [][]byte
			for _, line := range bytes.SplitAfter(mustRead(t, f.registry), []byte("\n")) {
				if !bytes.HasPrefix(line, []byte("notes.txt,")) {
					kept = append(kept, line)
				}
			}
			data := bytes.Join(kept, nil)
			if err := os.WriteFile(f.registry, data, 0o644); err != nil {
				t.Fatal(err)
			}
			st := readStamp(t, f.r.archive)
			sum := sha256.Sum256(data)
			st.Size, st.SHA256 = int64(len(data)), hex.EncodeToString(sum[:])
			if err := state.WriteRegistryStamp(f.r.archive, st); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newReuseFixture(t)
			_, _, want := f.scan(t, nil)
			stamp := readStamp(t, f.r.archive)

			f.clock.Advance(5 * time.Minute)
			cfg := scanSessionConfig(f.r, f.clock, 2)
			cfg.Version = f.version
			ctx, cancel := context.WithCancel(context.Background())
			sc := testScanConfig(f.r)
			sc.detectFile = f.det.detect
			sc.afterEntry = func(n int64) error {
				if n == 8 {
					cancel()
				}
				return nil
			}
			first, _ := runScan(t, ctx, cfg, sc)
			cancel()
			if first.Status != StatusInterrupted {
				t.Fatalf("interrupted scan = %v (%v)", first.Status, first.Err)
			}
			cp := readRunCheckpoint(t, f.r, first.RunID)
			if cp.RegistryOffset == 0 || !state.SameFileID(cp.RegistryBase, &state.FileID{Size: stamp.Size, SHA256: stamp.SHA256}) {
				t.Fatalf("checkpoint base %+v offset %d, want the stamped base", cp.RegistryBase, cp.RegistryOffset)
			}
			wantOpened(t, "interrupted scan", f.det.take())

			tc.change(t, f)
			sc.afterEntry = nil
			res, rec := runScan(t, context.Background(), cfg, sc)
			if res.RunID != first.RunID || res.Status != StatusCompleted {
				t.Fatalf("resume: run %s status %v (%v)", res.RunID, res.Status, res.Err)
			}
			resumed := len(rec.messages("resuming scan")) == 1
			restarted := len(rec.messages("base registry changed since the checkpoint; scanning again from the start")) == 1
			if resumed == tc.restart || restarted != tc.restart {
				t.Errorf("resumed %v, restarted %v; want restart %v", resumed, restarted, tc.restart)
			}
			opened := f.det.take()
			if got := mustRead(t, f.registry); !bytes.Equal(got, want) {
				t.Errorf("registry after resume differs\n--- got\n%s\n--- want\n%s", got, want)
			}
			if tc.name == "replaced by another stamped registry" {
				wantOpened(t, "restart on a stamped base", opened, "notes.txt")
			}
			f.redetect(t, "after resume", want, nil)
		})
	}
}
