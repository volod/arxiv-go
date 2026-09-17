//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/test/fixtures/testmp4"
	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

// genArchive describes the generated archive: which relative paths must become split
// candidates, which must stay, and whether ffmpeg clips were added.
type genArchive struct {
	root             string
	videos           map[string]bool // rel_path of every file split must move
	nonVideos        []string        // media-like files that must stay in the archive
	foreignMD        string          // a non-description <video>.md that split must not overwrite
	dirAtDescription string          // a directory named <video>.md
	ffmpeg           bool
	catia            *genCatia // CATIA files, when the archive has them
}

const customVideoExt = "bik"

// generateArchive builds a multi-level archive of several hundred files under root. Video payloads
// are seeded random bytes, so every video has distinct content and a copy that loses or mixes
// bytes changes its SHA-256.
func generateArchive(t *testing.T, root string, rng *rand.Rand) *genArchive {
	t.Helper()
	g := &genArchive{root: root, videos: map[string]bool{}}
	projects := []string{"projects/2024", "projects/2025/q1", "projects/fixture-\u00e9\u0436", "old/deep/er/tapes", "with space/a,b"}
	n := 0
	for _, dir := range projects {
		for i := 0; i < 6; i++ {
			rel := fmt.Sprintf("%s/clip-%02d.mp4", dir, n)
			g.write(t, rel, mp4Bytes(rng, 256*1024+rng.Intn(1536*1024), i%3 == 0))
			g.videos[rel] = true
			n++
		}
	}
	// Signature-less containers selected by the built-in extension list or --video-extensions.
	for _, rel := range []string{"old/tape.mkv", "old/deep/broadcast.ts", "top.avi", "old/custom." + customVideoExt} {
		g.write(t, rel, randomBytes(rng, 512*1024+rng.Intn(512*1024)))
		g.videos[rel] = true
	}
	g.write(t, "projects/2024/clip-00.mp4.md", []byte("human notes about clip 00\n"))
	g.foreignMD = "projects/2024/clip-00.mp4.md"
	g.dirAtDescription = "top.avi.md"
	mkdirAll(t, filepath.Join(root, filepath.FromSlash(g.dirAtDescription)))
	mkdirAll(t, filepath.Join(root, "empty", "nested"))

	docDirs := []string{"docs", "docs/a", "docs/a/x", "docs/a/x/y/z", "docs/b", "docs/with space", "docs/кирилиця"}
	for _, dir := range docDirs {
		for i := 0; i < 40; i++ {
			rel := fmt.Sprintf("%s/note-%02d.txt", dir, i)
			g.write(t, rel, []byte(fmt.Sprintf("document %s %d %d\n", dir, i, rng.Int63())))
		}
	}
	for i := 0; i < 20; i++ {
		g.write(t, fmt.Sprintf("blobs/data-%02d.bin", i), randomBytes(rng, 1024+rng.Intn(64*1024)))
	}
	g.addFFmpegClips(t)
	return g
}

// addFFmpegClips adds real encoded clips when ffmpeg is available (the pinned build in bin/, else
// PATH). They are optional: CI installs no ffmpeg. ARXGO_TEST_REQUIRE_TOOLS=1 makes a missing
// ffmpeg fail the test instead.
func (g *genArchive) addFFmpegClips(t *testing.T) {
	ffmpeg, err := tooltest.Path("ffmpeg")
	if err != nil {
		if os.Getenv(tooltest.RequireEnv) == "1" {
			t.Fatalf("ffmpeg is required by %s=1: %v", tooltest.RequireEnv, err)
		}
		t.Logf("ffmpeg unavailable, generated archive has no encoded clips: %v", err)
		return
	}
	src := []string{"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25", "-f", "lavfi", "-i", "sine=frequency=440"}
	clips := []struct {
		rel   string
		video bool
		args  []string
	}{
		{"media/camera.mov", true, append(append([]string{}, src...), "-t", "2", "-c:v", "mpeg4", "-c:a", "aac", "-shortest")},
		{"media/deep/interview, take 2.mkv", true, append(append([]string{}, src...), "-t", "2", "-c:v", "mpeg4", "-c:a", "aac", "-shortest")},
		{"media/broadcast.ts", true, append(append([]string{}, src...), "-t", "2", "-c:v", "mpeg2video", "-c:a", "mp2", "-shortest")},
		{"media/song.m4a", false, []string{"-f", "lavfi", "-i", "sine=frequency=550", "-t", "2", "-c:a", "aac"}},
		{"media/frame.png", false, []string{"-f", "lavfi", "-i", "testsrc2=size=320x240", "-frames:v", "1"}},
	}
	for _, c := range clips {
		dst := filepath.Join(g.root, filepath.FromSlash(c.rel))
		mkdirAll(t, filepath.Dir(dst))
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		args := append([]string{"-v", "error", "-y"}, c.args...)
		// ffmpeg picks the muxer from the extension; the file is renamed into place when complete.
		tmp := dst + ".gen" + path.Ext(c.rel)
		out, err := exec.CommandContext(ctx, ffmpeg, append(args, tmp)...).CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("ffmpeg %s: %v\n%s", c.rel, err, out)
		}
		if err := os.Rename(tmp, dst); err != nil {
			t.Fatal(err)
		}
		if c.video {
			g.videos[c.rel] = true
		} else {
			g.nonVideos = append(g.nonVideos, c.rel)
		}
	}
	g.ffmpeg = true
}

func (g *genArchive) write(t *testing.T, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(g.root, filepath.FromSlash(rel))
	mkdirAll(t, filepath.Dir(p))
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Distinct past mtimes with a sub-second part: a transfer that loses precision is detected.
	mt := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(len(rel)*7919+len(data)) * time.Second).Add(123456789)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func mp4Bytes(rng *rand.Rand, payload int, moovAtEnd bool) []byte {
	b := testmp4.File(testmp4.Options{
		Tracks:    []testmp4.Track{{Kind: "vide", Codec: "avc1", Width: 1280, Height: 720}, {Kind: "soun", Codec: "mp4a"}},
		MoovAtEnd: moovAtEnd,
		MdatBytes: payload,
	})
	end := mdatEnd(b)
	rng.Read(b[end-payload : end])
	return b
}

// mdatEnd returns the end offset of the top-level mdat box.
func mdatEnd(b []byte) int {
	for off := 0; off+8 <= len(b); {
		size := int(uint32(b[off])<<24 | uint32(b[off+1])<<16 | uint32(b[off+2])<<8 | uint32(b[off+3]))
		if string(b[off+4:off+8]) == "mdat" {
			return off + size
		}
		off += size
	}
	return len(b)
}

func randomBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	rng.Read(b)
	return b
}

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// manifestEntry is one file or directory of an archive tree outside arxgo's own outputs.
type manifestEntry struct {
	Size   int64
	MTime  int64
	SHA256 string
	Dir    bool
}

type manifest map[string]manifestEntry

// isArxgoOutput reports the paths arxgo owns in an archive root: its state directory and the
// registries and summaries it writes there.
func isArxgoOutput(rel string) bool {
	if rel == ".arxgo" || strings.HasPrefix(rel, ".arxgo/") {
		return true
	}
	return !strings.Contains(rel, "/") && (rel == "arxgo-registry.csv" || rel == "arxgo-catia-text.md" ||
		strings.HasPrefix(rel, "arxgo-videos.") || strings.HasPrefix(rel, "arxgo-catia."))
}

// takeManifest records path, size, mtime (nanoseconds) and SHA-256 of every file, and every
// directory, below root.
func takeManifest(t *testing.T, root string) manifest {
	t.Helper()
	m := manifest{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isArxgoOutput(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			m[rel] = manifestEntry{Dir: true}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sum, err := fileSHA256(p)
		if err != nil {
			return err
		}
		m[rel] = manifestEntry{Size: info.Size(), MTime: info.ModTime().UnixNano(), SHA256: sum}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// diffManifests lists every difference between two manifests, sorted by path.
func diffManifests(want, got manifest) []string {
	var out []string
	for rel, w := range want {
		g, ok := got[rel]
		switch {
		case !ok:
			out = append(out, "missing "+rel)
		case g != w:
			out = append(out, fmt.Sprintf("changed %s: want %+v, got %+v", rel, w, g))
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			out = append(out, "unexpected "+rel)
		}
	}
	sort.Strings(out)
	return out
}

func (m manifest) files() int {
	n := 0
	for _, e := range m {
		if !e.Dir {
			n++
		}
	}
	return n
}
