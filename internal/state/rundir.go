package state

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
)

// DirName is the reserved state directory in each root.
const DirName = ".arxgo"

// Files inside a run directory.
const (
	OptionsFile      = "options.json"
	CheckpointFile   = "checkpoint.json"
	WALFile          = "wal.jsonl"
	CandidatesFile   = "candidates.jsonl"
	ScanRegistryFile = "scan-registry.csv" // restore: scan of the mirror root stays in the run dir
	ReportFile       = "report.json"
	LogFile          = "run.log.jsonl"
)

const (
	lockName    = "lock"
	currentName = "current"
	runsName    = "runs"
)

// ErrStateCorrupt marks run state that cannot be trusted without operator action.
var ErrStateCorrupt = errors.New("run state is corrupt")

// StateDir returns root/.arxgo.
func StateDir(root string) string { return filepath.Join(root, DirName) }

var runIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

// NewRunID returns a run id of the form YYYYMMDDTHHMMSSZ-<8 hex>.
func NewRunID(now time.Time, random io.Reader) (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", fmt.Errorf("run id: %w", err)
	}
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:]), nil
}

// ValidRunID reports whether id has the run id shape, so that it is safe to use as a path segment.
func ValidRunID(id string) bool { return runIDPattern.MatchString(id) }

// RunDir is one run's state directory, root/.arxgo/runs/<id>.
type RunDir struct {
	Root string
	ID   string
	Path string
}

// File returns the path of a file inside the run directory.
func (r RunDir) File(name string) string { return filepath.Join(r.Path, name) }

// Complete reports whether the run wrote its final report. An incomplete run is resumable.
func (r RunDir) Complete() (bool, error) {
	_, err := os.Stat(r.File(ReportFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func runDirOf(root, id string) RunDir {
	return RunDir{Root: root, ID: id, Path: filepath.Join(StateDir(root), runsName, id)}
}

// CreateRunDir creates a new run directory named by a fresh run id and makes its entry durable.
// On the unlikely collision with an existing id it draws another.
func CreateRunDir(root string, now time.Time, random io.Reader) (RunDir, error) {
	runs := filepath.Join(StateDir(root), runsName)
	if err := mkdirDurable(runs); err != nil {
		return RunDir{}, err
	}
	for attempt := 0; ; attempt++ {
		id, err := NewRunID(now, random)
		if err != nil {
			return RunDir{}, err
		}
		rd := runDirOf(root, id)
		err = os.Mkdir(rd.Path, 0o755)
		if err == nil {
			return rd, fsops.SyncDir(runs)
		}
		if !errors.Is(err, fs.ErrExist) || attempt == 8 {
			return RunDir{}, err
		}
	}
}

// OpenRunDir returns an existing run directory.
func OpenRunDir(root, id string) (RunDir, error) {
	if !ValidRunID(id) {
		return RunDir{}, fmt.Errorf("%w: invalid run id %q", ErrStateCorrupt, id)
	}
	rd := runDirOf(root, id)
	fi, err := os.Stat(rd.Path)
	if err != nil {
		return RunDir{}, err
	}
	if !fi.IsDir() {
		return RunDir{}, fmt.Errorf("%w: %s is not a directory", ErrStateCorrupt, rd.Path)
	}
	return rd, nil
}

// ReadCurrent returns the run id named by root/.arxgo/current, or "" when there is none.
func ReadCurrent(root string) (string, error) {
	path := filepath.Join(StateDir(root), currentName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if !ValidRunID(id) {
		return "", fmt.Errorf("%w: %s holds %q, not a run id", ErrStateCorrupt, path, id)
	}
	return id, nil
}

// WriteCurrent atomically points root/.arxgo/current at run id. A plain file is used instead of a
// symlink so the layout works on Windows and network shares.
func WriteCurrent(root, id string) error {
	if !ValidRunID(id) {
		return fmt.Errorf("invalid run id %q", id)
	}
	if err := mkdirDurable(StateDir(root)); err != nil {
		return err
	}
	return fsops.AtomicWriteFile(filepath.Join(StateDir(root), currentName), []byte(id+"\n"), 0o644)
}

// RunOptions is the content of options.json: the validated options that define a run.
type RunOptions struct {
	V         int       `json:"v"`
	RunID     string    `json:"run_id"`
	Op        string    `json:"op"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Archive   string    `json:"archive"`
	// Payload is the kind split and restore move ("video" or "catia"); scan has none. The mirror
	// root of that kind is stored in VideoArchive or CatiaArchive.
	Payload      string `json:"payload,omitempty"`
	VideoArchive string `json:"video_archive,omitempty"`
	CatiaArchive string `json:"catia_archive,omitempty"`
	DryRun       bool   `json:"dry_run,omitempty"`
	// SidecarCleanup is set for restore runs only: true when the run deletes the owned post-commit
	// sidecars of the files it restores. Nil (scan, split, or a restore written by an earlier
	// build) means false.
	SidecarCleanup *bool `json:"sidecar_cleanup,omitempty"`
	// Defining holds the options that must match for a later process to resume this run.
	Defining json.RawMessage `json:"defining"`
	// Options holds every validated option, including runtime-only ones such as the log level.
	Options json.RawMessage `json:"options"`
}

// SameDefinition reports whether a resumed run would use the same defining options.
func (o RunOptions) SameDefinition(op string, defining json.RawMessage) bool {
	return o.Op == op && bytes.Equal(compactJSON(o.Defining), compactJSON(defining))
}

func compactJSON(raw json.RawMessage) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}

// WriteJSON atomically writes v as indented JSON followed by a newline.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return fsops.AtomicWriteFile(path, append(data, '\n'), 0o644)
}

// ReadJSON decodes the JSON file at path into v. A decode failure wraps ErrStateCorrupt.
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrStateCorrupt, path, err)
	}
	return nil
}

// mkdirDurable creates dir and any missing parents, flushing each parent whose entries changed.
func mkdirDurable(dir string) error {
	if fi, err := os.Stat(dir); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	}
	parent := filepath.Dir(dir)
	if parent != dir {
		if err := mkdirDurable(parent); err != nil {
			return err
		}
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return fsops.SyncDir(parent)
}

// ListRunIDs returns the ids of run directories under root/.arxgo/runs, sorted.
func ListRunIDs(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(StateDir(root), runsName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() || !ValidRunID(e.Name()) {
			continue
		}
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids, nil
}
