package archive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// A rename reporting EXDEV overrides a stale device classification. Size all remaining copies
// against the video archive separately before retrying any transfer.
func preflightSplitFallback(ctx context.Context, s *Session, left Candidates) error {
	oldFS, oldTransfer := s.cfg.FS, s.cfg.Preflight.Transfer
	s.cfg.FS = forcedOtherDevice{oldFS}
	s.cfg.Preflight.Transfer = transferCopy
	defer func() { s.cfg.FS, s.cfg.Preflight.Transfer = oldFS, oldTransfer }()
	_, err := s.Preflight(ctx, left)
	return err
}

type forcedOtherDevice struct{ fsops.Ops }

func (forcedOtherDevice) SameDevice(string, string) (bool, error) { return false, nil }

func placeSplit(ctx context.Context, s *Session, w *state.WAL, rec state.Record, mode string, c SplitConfig) error {
	if mode == state.TransferRename {
		fi, err := os.Lstat(rec.Src)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != rec.Size || !fi.ModTime().Equal(rec.Mtime) {
			return fsops.ErrSourceChanged
		}
		if err := renamePlaced(s, rec.Src, rec.Dst, rec.Size); err != nil {
			return err
		}
		return hitSplit(s.cfg.Crash, "fs:place")
	}
	stage := c.StageCopy
	if stage == nil {
		stage = fsops.StageCopy
	}
	res, err := stage(ctx, rec.Src, rec.Dst, fsops.CopyOptions{
		Verify: c.Verify, ExpectSize: rec.Size, ExpectModTime: rec.Mtime,
	})
	if err != nil {
		return err
	}
	if err := hitSplit(s.cfg.Crash, "fs:copy"); err != nil {
		return err
	}
	if _, err := w.Append(rec.TxID, state.StepCopied, state.Record{}); err != nil {
		return err
	}
	if _, err := w.Append(rec.TxID, state.StepVerified, state.Record{SHA256: res.SHA256}); err != nil {
		return err
	}
	if err := renamePlaced(s, fsops.PartPath(rec.Dst), rec.Dst, rec.Size); err != nil {
		return err
	}
	return hitSplit(s.cfg.Crash, "fs:place")
}

// renamePlaced moves oldpath to dst. A directory-flush failure after the rename is already
// placed: recovery would roll forward, so the run continues from placed instead of aborting.
func renamePlaced(s *Session, oldpath, dst string, size int64) error {
	err := s.cfg.FS.Rename(oldpath, dst)
	if err == nil {
		return nil
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return err
	}
	fi, st := os.Lstat(dst)
	if st != nil || !fi.Mode().IsRegular() || fi.Size() != size {
		return err
	}
	s.Log.Warn("destination placed but directory flush failed; continuing", "dst", dst, "error", err)
	return nil
}

func hitSplit(h state.CrashHook, point string) error {
	if h != nil {
		return h(point)
	}
	return nil
}

func ensureMirrorDirs(s *Session, dst string) error {
	rel, err := filepath.Rel(s.cfg.VideoArchive, filepath.Dir(dst))
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	var chain []string
	for p := rel; p != "."; p = filepath.Dir(p) {
		chain = append(chain, p)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		path := filepath.Join(s.cfg.VideoArchive, chain[i])
		sourceDir := filepath.Join(s.cfg.Archive, chain[i])
		fi, err := os.Stat(sourceDir)
		if err != nil || !fi.IsDir() {
			return fmt.Errorf("source parent is not a directory: %s", sourceDir)
		}
		err = os.Mkdir(path, fi.Mode().Perm())
		created := err == nil
		if errors.Is(err, fs.ErrExist) {
			var got fs.FileInfo
			got, err = os.Lstat(path)
			if err == nil && !got.IsDir() {
				err = fmt.Errorf("destination parent is not a directory: %s", path)
			}
		}
		if err != nil {
			return err
		}
		if !created {
			continue
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(path, fi.Mode().Perm()); err != nil && !errors.Is(err, fs.ErrPermission) && !errors.Is(err, errors.ErrUnsupported) {
				return err
			}
		}
		if err := fsops.SyncDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := hitSplit(s.cfg.Crash, "fs:mkdir"); err != nil {
			return err
		}
	}
	return nil
}
