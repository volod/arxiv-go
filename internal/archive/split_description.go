package archive

import (
	"context"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/volod/arxiv-go/internal/catia"
	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

// DescriptionConfig is the archive-side input to the description writer. Mirror is the payload's
// mirror root; Payload selects the description kind (a CATIA description reads the placed file's
// accessible metadata).
type DescriptionConfig struct {
	Archive, Mirror, BaseURL, Registry, Version string
	Payload                                     PayloadKind
	Verify                                      fsops.VerifyMode
	FS                                          fsops.Ops
	Crash                                       state.CrashHook
	Now                                         func() time.Time
	Log                                         *slog.Logger // extraction warnings; nil discards them
	Ctx                                         context.Context
}

// MarkdownDescription implements SplitDescriptionWriter with full front matter, links and metadata.
type MarkdownDescription struct {
	cfg  DescriptionConfig
	mu   sync.Mutex
	sha  map[string]string
	rows map[string]report.RegistryRow
	// catia holds the summary of each CATIA description written until the described record takes it.
	catia map[string]*state.CatiaSummary
}

// NewMarkdownDescription returns a description writer. Nil FS and Crash use production defaults. A
// nil Now takes the session clock when split attaches the writer, and the wall clock before that.
func NewMarkdownDescription(cfg DescriptionConfig) *MarkdownDescription {
	if cfg.FS == nil {
		cfg.FS = fsops.System{}
	}
	if cfg.Registry == "" && cfg.Archive != "" {
		cfg.Registry = filepath.Join(cfg.Archive, "arxgo-registry.csv")
	}
	return &MarkdownDescription{cfg: cfg, sha: map[string]string{}, catia: map[string]*state.CatiaSummary{}}
}

func (m *MarkdownDescription) RememberSHA256(rel, sum string) {
	if sum == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sha[rel] = sum
}

func (m *MarkdownDescription) Path(tx state.Tx) string {
	p, err := report.ChooseDescriptionPath(tx.Begin.Src, tx.Begin.RelPath)
	if err != nil {
		return ""
	}
	return p
}

func (m *MarkdownDescription) Write(tx state.Tx) error {
	path, err := report.ChooseDescriptionPath(tx.Begin.Src, tx.Begin.RelPath)
	if err != nil {
		return err
	}
	in := m.input(tx)
	if m.cfg.Payload == PayloadCatia {
		info := m.extractCatia(tx)
		in.Catia, in.Media = &info, nil
		m.mu.Lock()
		m.catia[tx.Begin.RelPath] = catiaSummary(info)
		m.mu.Unlock()
	}
	if err := m.cfg.FS.AtomicWriteFile(path, report.RenderDescription(in), 0o644); err != nil {
		return err
	}
	return hitCrash(m.cfg.Crash, "fs:description")
}

// Catia returns and forgets the CATIA summary of the last description written for rel; nil for a
// video description.
func (m *MarkdownDescription) Catia(rel string) *state.CatiaSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	sum := m.catia[rel]
	delete(m.catia, rel)
	return sum
}

// extractCatia runs the metadata pass on the placed destination. A failure is logged with its kind
// and yields empty values; it never fails the move.
func (m *MarkdownDescription) extractCatia(tx state.Tx) catia.Info {
	m.mu.Lock()
	ctx, log := m.cfg.Ctx, m.cfg.Log
	m.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	info := catia.ExtractPath(ctx, tx.Begin.Dst)
	if info.ErrorKind != "" {
		if log == nil {
			log = slog.New(slog.DiscardHandler)
		}
		log.Warn("CATIA metadata extraction failed; description has empty values",
			"rel_path", tx.Begin.RelPath, "error_kind", info.ErrorKind, "error", info.Err)
	}
	return info
}

// catiaSummary is the part of the extracted metadata that the described record and registry keep.
func catiaSummary(info catia.Info) *state.CatiaSummary {
	release := info.Release
	if release == catia.ReleaseUnknown {
		release = ""
	}
	format := info.Format
	if format == "" {
		format = catia.FormatUnknown
	}
	return &state.CatiaSummary{Kind: info.Kind, Format: format, Release: release, Components: len(info.Components)}
}

func (m *MarkdownDescription) input(tx state.Tx) report.DescriptionInput {
	reg := m.lookup(tx.Begin.RelPath)
	sum := m.shaOf(tx.Begin.RelPath)
	if sum == "" && m.cfg.Verify == fsops.VerifyHash {
		if h, err := hashFile(tx.Begin.Dst); err == nil {
			sum = hex.EncodeToString(h[:])
		}
	}
	in := report.DescriptionInput{
		RelPath: tx.Begin.RelPath, Archive: m.cfg.Archive, FileSize: tx.Begin.Size, FileMIME: reg.FileMIME, SHA256: sum,
		Modified: tx.Begin.Mtime, MovedAt: m.now(), MovedTo: tx.Begin.Dst,
		URL:   report.ComposeURL(m.cfg.BaseURL, tx.Begin.RelPath),
		Media: reg.Metadata.Media,
	}
	if in.FileSize == 0 {
		if fi, err := os.Lstat(tx.Begin.Dst); err == nil {
			in.FileSize = fi.Size()
		}
	}
	return in
}

func (m *MarkdownDescription) now() time.Time {
	if m.cfg.Now == nil {
		return time.Now()
	}
	return m.cfg.Now()
}

func (m *MarkdownDescription) shaOf(rel string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sha[rel]
}

func (m *MarkdownDescription) lookup(rel string) report.RegistryRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		rows, err := report.LoadRegistry(m.cfg.Registry)
		if err != nil {
			m.rows = map[string]report.RegistryRow{}
		} else {
			m.rows = report.RegistryByPath(rows)
		}
	}
	return m.rows[rel]
}

func runIDFromTxID(txid string) string {
	i := lastHyphen(txid)
	if i <= 0 {
		return txid
	}
	return txid[:i]
}

func lastHyphen(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '-' {
			return i
		}
	}
	return -1
}
