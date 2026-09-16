package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// registryBase is the file registry at the scan's target path, read before the scan replaces it.
type registryBase struct {
	rows map[string]report.RegistryRow
	// stamped: the registry stamp names this path with this size and SHA-256 and the run detects
	// with the stamp's settings, so its rows are the values detection would write.
	stamped bool
}

// loadRegistryBase reads the registry at c.Registry and checks it against the archive's stamp. A
// missing or unreadable registry is no base.
func loadRegistryBase(s *Session, c ScanConfig) *registryBase {
	data, err := os.ReadFile(c.Registry)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.Log.Warn("existing file registry unreadable; not reusing its rows", "registry", c.Registry, "error", err)
		}
		return nil
	}
	rows, err := report.ReadRegistry(bytes.NewReader(data))
	if err != nil {
		s.Log.Warn("existing file registry cannot be parsed; not reusing its rows", "registry", c.Registry, "error", err)
		return nil
	}
	b := &registryBase{rows: report.RegistryByPath(rows)}
	if st, ok := state.ReadRegistryStamp(s.cfg.Archive); ok {
		sum := sha256.Sum256(data)
		b.stamped = sameFilePath(st.Registry, c.Registry) && st.Size == int64(len(data)) &&
			st.SHA256 == hex.EncodeToString(sum[:]) && st.Detect.Equal(detectSettings(s, c))
	}
	return b
}

func sameFilePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

// detectSettings are the run's detection settings as the registry stamp records them.
func detectSettings(s *Session, c ScanConfig) state.DetectSettings {
	metadata := c.Metadata
	if metadata == "" {
		metadata = "file"
	}
	exts := slices.Clone(scanner.BuiltinVideoExtensions)
	for _, e := range c.VideoExtensions {
		if e = strings.ToLower(strings.TrimPrefix(e, ".")); e != "" {
			exts = append(exts, e)
		}
	}
	slices.Sort(exts)
	return state.DetectSettings{Version: s.cfg.Version, Metadata: metadata, VideoExtensions: slices.Compact(exts)}
}

// placedRegistry describes the registry file at its final path after placeRegistry.
type placedRegistry struct {
	outcome string // state.RegistryWritten or state.RegistryUnchanged
	size    int64
	sha256  string
}

// placeRegistry puts a completed part file at final: when final already holds the same bytes the
// part file is removed and final keeps its modification time, otherwise the part file replaces it.
func placeRegistry(part, final string) (placedRegistry, error) {
	p, equal, err := compareRegistry(part, final)
	if err != nil {
		return p, err
	}
	if equal {
		p.outcome = state.RegistryUnchanged
		if err := os.Remove(part); err != nil {
			return p, fmt.Errorf("remove unchanged registry part: %w", err)
		}
		return p, nil
	}
	p.outcome = state.RegistryWritten
	if err := fsops.Replace(part, final); err != nil {
		return p, fmt.Errorf("place registry: %w", err)
	}
	return p, nil
}

// compareRegistry hashes part and compares it with final in one pass.
func compareRegistry(part, final string) (placedRegistry, bool, error) {
	var p placedRegistry
	src, err := os.Open(part)
	if err != nil {
		return p, false, err
	}
	defer src.Close()
	old, err := os.Open(final)
	if err != nil {
		old = nil
	} else {
		defer old.Close()
	}
	h := sha256.New()
	a, b := make([]byte, 1<<20), make([]byte, 1<<20)
	equal := old != nil
	for {
		n, rerr := io.ReadFull(src, a)
		h.Write(a[:n])
		p.size += int64(n)
		if equal {
			m, _ := io.ReadFull(old, b[:n])
			equal = m == n && bytes.Equal(a[:n], b[:n])
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return p, false, rerr
		}
	}
	if equal {
		// final must end where part ends.
		var one [1]byte
		n, _ := old.Read(one[:])
		equal = n == 0
	}
	p.sha256 = hex.EncodeToString(h.Sum(nil))
	return p, equal, nil
}

// writeRegistryStamp records the registry the run placed or found unchanged.
func writeRegistryStamp(s *Session, c ScanConfig, st *state.ScanStats, p placedRegistry) error {
	stamp := state.RegistryStamp{
		Registry: c.Registry, Size: p.size, SHA256: p.sha256, RunID: s.Run.ID, Op: s.cfg.Op,
		ScanStartedAt: st.StartedAt, Detect: detectSettings(s, c),
	}
	if err := state.WriteRegistryStamp(s.cfg.Archive, stamp); err != nil {
		return fmt.Errorf("write registry stamp: %w", err)
	}
	return nil
}

// writeFileIfChanged atomically writes data to path unless path already holds exactly data, so an
// unchanged registry keeps its modification time.
func writeFileIfChanged(fsys fsops.Ops, path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	return fsys.AtomicWriteFile(path, data, 0o644)
}
