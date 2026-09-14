package archive

import (
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

// StubConfig is the archive-side input to the Markdown stub writer.
type StubConfig struct {
	Archive, VideoArchive, BaseURL, Registry, Version string
	Verify                                            fsops.VerifyMode
	FS                                                fsops.Ops
	Crash                                             state.CrashHook
	Now                                               func() time.Time
}

// MarkdownStub implements SplitStubWriter with full front matter, links and metadata.
type MarkdownStub struct {
	cfg  StubConfig
	mu   sync.Mutex
	sha  map[string]string
	rows map[string]report.RegistryRow
}

// NewMarkdownStub returns a stub writer. Nil FS, Crash and Now use production defaults.
func NewMarkdownStub(cfg StubConfig) *MarkdownStub {
	if cfg.FS == nil {
		cfg.FS = fsops.System{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Registry == "" && cfg.Archive != "" {
		cfg.Registry = filepath.Join(cfg.Archive, "arxgo-registry.csv")
	}
	return &MarkdownStub{cfg: cfg, sha: map[string]string{}}
}

func (m *MarkdownStub) RememberSHA256(rel, sum string) {
	if sum == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sha[rel] = sum
}

func (m *MarkdownStub) Path(tx state.Tx) string {
	p, err := report.ChooseStubPath(tx.Begin.Src, tx.Begin.RelPath)
	if err != nil {
		return ""
	}
	return p
}

func (m *MarkdownStub) Write(tx state.Tx) error {
	path, err := report.ChooseStubPath(tx.Begin.Src, tx.Begin.RelPath)
	if err != nil {
		return err
	}
	in, err := m.input(tx, path)
	if err != nil {
		return err
	}
	data, err := report.RenderStub(in)
	if err != nil {
		return err
	}
	if err := m.cfg.FS.AtomicWriteFile(path, data, 0o644); err != nil {
		return err
	}
	return hitCrash(m.cfg.Crash, "fs:stub")
}

func (m *MarkdownStub) input(tx state.Tx, stubAbs string) (report.StubInput, error) {
	reg := m.lookup(tx.Begin.RelPath)
	sum := m.shaOf(tx.Begin.RelPath)
	if sum == "" && m.cfg.Verify == fsops.VerifyHash {
		if h, err := hashFile(tx.Begin.Dst); err == nil {
			sum = hex.EncodeToString(h[:])
		}
	}
	abs := filepath.ToSlash(tx.Begin.Dst)
	in := report.StubInput{
		RelPath:          tx.Begin.RelPath,
		VideoArchivePath: abs,
		URL:              report.ComposeURL(m.cfg.BaseURL, tx.Begin.RelPath),
		FileSize:         tx.Begin.Size,
		FileMIME:         reg.FileMIME,
		SHA256:           sum,
		RunID:            runIDFromTxID(tx.Begin.TxID),
		MovedAt:          m.cfg.Now().UTC(),
		FileName:         path.Base(tx.Begin.RelPath),
		RelLink:          report.RelativeLink(filepath.Dir(stubAbs), tx.Begin.Dst),
		Media:            reg.Metadata.Media,
	}
	if in.FileName == "." || in.FileName == "" {
		in.FileName = filepath.Base(tx.Begin.Src)
	}
	if in.FileSize == 0 {
		if fi, err := os.Lstat(tx.Begin.Dst); err == nil {
			in.FileSize = fi.Size()
		}
	}
	return in, nil
}

func (m *MarkdownStub) shaOf(rel string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sha[rel]
}

func (m *MarkdownStub) lookup(rel string) report.RegistryRow {
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
