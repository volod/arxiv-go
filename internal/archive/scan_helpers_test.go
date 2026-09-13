package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/state"
)

// fixtureTime is the modification time given to every generated fixture entry.
var fixtureTime = time.Date(2024, 5, 1, 10, 22, 3, 0, time.UTC)

const fixtureThreshold = 64 // --large-threshold used by the scan fixtures

func ftypHead(major string, compatible ...string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(16+4*len(compatible)))
	b.WriteString("ftyp" + major)
	_ = binary.Write(&b, binary.BigEndian, uint32(0x200))
	for _, c := range compatible {
		b.WriteString(c)
	}
	_ = binary.Write(&b, binary.BigEndian, uint32(8))
	b.WriteString("mdat")
	return b.Bytes()
}

// writeScanFile creates a file below root with fixed mode and modification time.
func writeScanFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, fixtureTime, fixtureTime); err != nil {
		t.Fatal(err)
	}
}

// buildEdgeCaseTree generates the registry edge-case fixture of
// docs/openspec/stage-1-core/registry.md#edge-cases below root.
func buildEdgeCaseTree(t *testing.T, root string) {
	t.Helper()
	video := append(ftypHead("isom", "isom", "avc1", "mp41"), make([]byte, 40)...)
	files := map[string][]byte{
		"empty.txt":                   nil,
		"empty.mp4":                   nil,
		"notes.txt":                   []byte("plain text notes\n"),
		"name with spaces.txt":        []byte("spaces\n"),
		"comma,name.txt":              []byte("comma\n"),
		`quote"name.txt`:              []byte("quote\n"),
		"new\nline.txt":               []byte("newline\n"),
		"unicode-файл-é.txt":          []byte("unicode\n"),
		" leading-space.txt":          []byte("leading space\n"),
		"lies/video-named.txt":        video,
		"lies/text-named.mp4":         []byte("this is text, not a video\n"),
		"media/audio-only.mp4":        ftypHead("mp42", "mp42", "isom"),
		"media/song.m4a":              ftypHead("M4A ", "M4A ", "mp42", "isom"),
		"media/tape.dat":              bytes.Repeat([]byte{0x00, 0xFE, 0x42}, 20),
		"media/Clip.MTS":              bytes.Repeat([]byte{0x00, 0xFE, 0x13, 0x37}, 20),
		"a/b":                         []byte("walk order a/b\n"),
		"a-b/x":                       []byte("walk order a-b/x\n"),
		"size/at-threshold.bin":       bytes.Repeat([]byte{0xFF, 0x00}, fixtureThreshold/2),
		"size/below-threshold.bin":    bytes.Repeat([]byte{0xFF, 0x00}, fixtureThreshold/2)[:fixtureThreshold-1],
		"deep/l1/l2/l3/l4/deep.mp4":   video,
		"deep/l1/l2/l3/l4/z-last.txt": []byte("deep text\n"),
		"deep/l1/l2/sibling.txt":      []byte("sibling\n"),
		"arxgo-registry.csv":          []byte("old registry\n"),
		"arxgo-videos.csv":            []byte("reserved\n"),
		"arxgo-videos.md":             []byte("reserved\n"),
		"sub/left.arxgo-part":         []byte("reserved part\n"),
		"sub/arxgo-registry.csv":      []byte("not reserved below the root\n"),
		".hidden":                     []byte("hidden file\n"),
	}
	for rel, data := range files {
		writeScanFile(t, root, rel, data)
	}
	if err := os.MkdirAll(filepath.Join(root, ".arxgo", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "links"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../notes.txt?a=1&b=<2>", filepath.Join(root, "links", "notes-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Directory modification times change while the tree is built; fix them last.
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			err = os.Chtimes(p, fixtureTime, fixtureTime)
		}
		return err
	})
}

// scanConfig returns a session configuration for a scan of r.archive writing the default registry.
func scanSessionConfig(r roots, clock *fakeClock, every int) Config {
	cfg := testConfig(r, clock, 100)
	cfg.Op, cfg.VideoArchive = opScan, ""
	cfg.Registry = filepath.Join(r.archive, "arxgo-registry.csv")
	cfg.CheckpointEvery = every
	cfg.Preflight = PreflightOptions{MinFree: 0}
	return cfg
}

func testScanConfig(r roots) ScanConfig {
	return ScanConfig{
		Root: r.archive, Registry: filepath.Join(r.archive, "arxgo-registry.csv"),
		LargeThreshold: fixtureThreshold, VideoExtensions: []string{".DAT"}, Preflight: true,
		Workers: 3, Window: 4,
	}
}

// runScan starts a session, scans and finishes, returning the result.
func runScan(t *testing.T, ctx context.Context, cfg Config, sc ScanConfig) (Result, *recorder) {
	t.Helper()
	rec := &recorder{}
	cfg.Console = rec
	s, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, err = Scan(ctx, s, sc)
	return s.Finish(ctx, err), rec
}

// scanManifest lists every path below root except the root and excluded top-level names, with
// mode, modification time and a content hash.
func scanManifest(t *testing.T, root string, skipTop ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		for _, s := range skipTop {
			if rel == s || strings.HasPrefix(rel, s+"/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		v := info.Mode().String() + " " + info.ModTime().UTC().String()
		if info.Mode().IsRegular() {
			if data, err := os.ReadFile(p); err == nil {
				sum := sha256.Sum256(data)
				v += " " + hex.EncodeToString(sum[:])
			} else {
				v += " unreadable"
			}
		}
		out[rel] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func readReport(t *testing.T, archive, runID string) state.Report {
	t.Helper()
	var r state.Report
	if err := state.ReadJSON(filepath.Join(state.StateDir(archive), "runs", runID, state.ReportFile), &r); err != nil {
		t.Fatal(err)
	}
	return r
}
