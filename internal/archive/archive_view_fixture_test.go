package archive

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// viewFixture is a generated archive with videos, CATIA files and other files, a video archive and
// a CATIA archive, for the archive view of the file registry.
type viewFixture struct {
	catiaRoots
	registry string
	videos   []string // payload videos a video split moves
	catia    []string // CATIA files a CATIA split moves
	tools    media.Toolset
}

// newViewFixture builds the tree. With ffmpeg on the host, the videos are real clips and the video
// split generates previews, so preview exclusion and ISO metadata columns are exercised.
func newViewFixture(t *testing.T) viewFixture {
	t.Helper()
	r, files := catiaFixture(t)
	f := viewFixture{catiaRoots: r, registry: filepath.Join(r.archive, "arxgo-registry.csv"),
		// zz/ sorts after every other entry, so its preserved row follows the last walked entry.
		videos: []string{"media/clip.mp4", "nested/deep/clip two.mp4", "zz/last.mp4"}}
	for rel := range files {
		f.catia = append(f.catia, rel)
	}
	slices.Sort(f.catia)
	writeScanFile(t, r.archive, "nested/deep/clip two.mp4", videoFixture)
	writeScanFile(t, r.archive, "zz/last.mp4", videoFixture)
	writeScanFile(t, r.archive, "nested/readme.txt", []byte("nested notes\n"))
	ffmpeg, errMpeg := exec.LookPath("ffmpeg")
	ffprobe, errProbe := exec.LookPath("ffprobe")
	if errMpeg == nil && errProbe == nil {
		f.tools = media.Toolset{media.FFmpeg: {Path: ffmpeg}, media.FFprobe: {Path: ffprobe}}
		f.videos = append(f.videos, "media/real.mp4")
		for _, rel := range f.videos {
			f.addVideo(t, rel)
		}
	} else {
		t.Log("ffmpeg or ffprobe missing: the video split runs without previews")
	}
	return f
}

// addVideo writes a video at rel: a real clip when ffmpeg is available, otherwise MP4 bytes that
// only the pure-Go parser reads.
func (f viewFixture) addVideo(t *testing.T, rel string) {
	t.Helper()
	p := filepath.Join(f.archive, filepath.FromSlash(rel))
	if f.tools == nil {
		writeScanFile(t, f.archive, rel, videoFixture)
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	generateClip(t, f.tools[media.FFmpeg].Path, p,
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:v", "mpeg4", "-q:v", "5", "-c:a", "aac")
}

// scanConfig is the scan the split configurations run, so scan and split write the same registry.
func (f viewFixture) scanConfig() ScanConfig {
	return ScanConfig{Root: f.archive, Registry: f.registry, Metadata: "file", LargeThreshold: 1024,
		Workers: 2, Window: 4, Preflight: true}
}

func (f viewFixture) scan(t *testing.T, edit func(*ScanConfig)) (Result, *recorder) {
	t.Helper()
	sc := f.scanConfig()
	if edit != nil {
		edit(&sc)
	}
	res, rec := runScan(t, context.Background(), scanSessionConfig(f.roots, newClock(), 3), sc)
	if res.Status != StatusCompleted {
		t.Fatalf("scan = %v (%v)", res.Status, res.Err)
	}
	return res, rec
}

func (f viewFixture) splitVideo(t *testing.T) {
	t.Helper()
	cfg, c := splitConfig(f.roots, "auto")
	if f.tools != nil {
		c.Preview = media.DefaultPreviewOptions()
		c.Preview.SampleMode, c.Preview.ImageMode, c.Preview.SampleDuration = "start", "start", time.Second
		c.Tools = f.tools
	}
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("video split = %+v: %+v", res, readReport(t, f.archive, res.RunID).Issues)
	}
}

func (f viewFixture) splitCatia(t *testing.T) {
	t.Helper()
	cfg, c := catiaTextConfig(f.catiaRoots, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("catia split = %+v", res)
	}
}

// splitBoth scans, splits videos with previews and CATIA files with text sidecars, and returns
// the registry the CATIA split wrote.
func (f viewFixture) splitBoth(t *testing.T) []byte {
	t.Helper()
	f.scan(t, nil)
	f.splitVideo(t)
	f.splitCatia(t)
	return mustRead(t, f.registry)
}

func (f viewFixture) rows(t *testing.T) map[string]report.RegistryRow {
	t.Helper()
	rows, err := report.LoadRegistry(f.registry)
	if err != nil || rows == nil {
		t.Fatalf("file registry: %v", err)
	}
	return report.RegistryByPath(rows)
}

// modTime is the registry's modification time, to prove an unchanged registry was not rewritten.
func (f viewFixture) modTime(t *testing.T) time.Time {
	t.Helper()
	info, err := os.Stat(f.registry)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

// ownedArtifacts lists the descriptions, previews and text sidecars both payload registries name.
func (f viewFixture) ownedArtifacts(t *testing.T) []string {
	t.Helper()
	var out []string
	if rows, err := report.LoadVideoFile(filepath.Join(f.archive, scanner.VideoRegistryName)); err == nil {
		for _, row := range rows {
			out = append(out, row.DescriptionRelPath)
			if row.Previews != "" {
				out = append(out, strings.Split(row.Previews, ";")...)
			}
		}
	}
	if rows, err := report.LoadCatiaFile(filepath.Join(f.archive, scanner.CatiaRegistryName)); err == nil {
		for _, row := range rows {
			out = append(out, row.DescriptionRelPath, row.TextRelPath)
		}
	}
	return slices.DeleteFunc(out, func(s string) bool { return s == "" })
}

// checkLocations compares rows with want: the same rows with the same values, except the location
// of the rel_paths in moved.
func checkLocations(t *testing.T, stage string, want, got map[string]report.RegistryRow, moved map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d rows, want %d", stage, len(got), len(want))
	}
	for rel, w := range want {
		if loc, ok := moved[rel]; ok {
			w.Location = loc
		}
		if g, ok := got[rel]; !ok {
			t.Errorf("%s: row %s missing", stage, rel)
		} else if !rowsEqual(g, w) {
			t.Errorf("%s: row %s\n got %+v\nwant %+v", stage, rel, g, w)
		}
	}
}

// rowsEqual compares every column of two rows.
func rowsEqual(a, b report.RegistryRow) bool { return reflect.DeepEqual(a, b) }

func locations(rels []string, loc string) map[string]string {
	out := map[string]string{}
	for _, rel := range rels {
		out[rel] = loc
	}
	return out
}

func readStamp(t *testing.T, archive string) state.RegistryStamp {
	t.Helper()
	st, ok := state.ReadRegistryStamp(archive)
	if !ok {
		t.Fatal("registry stamp missing or unreadable")
	}
	return st
}
