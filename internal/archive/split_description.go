package archive

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

// DescriptionConfig is the archive-side input to the video description writer.
type DescriptionConfig struct {
	Archive, VideoArchive, BaseURL, Registry, Version string
	Verify                                            fsops.VerifyMode
	FS                                                fsops.Ops
	Crash                                             state.CrashHook
	Now                                               func() time.Time
}

// MarkdownDescription implements SplitDescriptionWriter with full front matter, links and metadata.
type MarkdownDescription struct {
	cfg  DescriptionConfig
	mu   sync.Mutex
	sha  map[string]string
	rows map[string]report.RegistryRow
}

// NewMarkdownDescription returns a description writer. Nil FS, Crash and Now use production defaults.
func NewMarkdownDescription(cfg DescriptionConfig) *MarkdownDescription {
	if cfg.FS == nil {
		cfg.FS = fsops.System{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Registry == "" && cfg.Archive != "" {
		cfg.Registry = filepath.Join(cfg.Archive, "arxgo-registry.csv")
	}
	return &MarkdownDescription{cfg: cfg, sha: map[string]string{}}
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
	if err := m.cfg.FS.AtomicWriteFile(path, report.RenderDescription(m.input(tx)), 0o644); err != nil {
		return err
	}
	return hitCrash(m.cfg.Crash, "fs:description")
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
		RelPath: tx.Begin.RelPath, FileSize: tx.Begin.Size, FileMIME: reg.FileMIME, SHA256: sum,
		Modified: tx.Begin.Mtime, MovedAt: m.cfg.Now(), MovedTo: tx.Begin.Dst,
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
