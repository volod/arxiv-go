package crashtest

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// Payload is the fake video body used by tests.
const Payload = "fake-video-bytes"

// Op is a fake split or restore transaction used to exercise recovery.
type Op struct {
	RelPath string
	Src     string
	Dst     string
	Size    int64
	Mtime   time.Time
	Rename  bool
	Restore bool
	Hook    *Hook
}

// Layout is one pair of roots plus a relative video path.
type Layout struct {
	Archive, Video, RelPath string
	Src, Dst, Stub, Part    string
}

// NewLayout builds archive and video roots under dir and returns absolute paths.
func NewLayout(dir, relPath string) Layout {
	l := Layout{
		Archive: filepath.Join(dir, "archive"),
		Video:   filepath.Join(dir, "video"),
		RelPath: relPath,
	}
	l.Src = filepath.Join(l.Archive, filepath.FromSlash(relPath))
	l.Dst = filepath.Join(l.Video, filepath.FromSlash(relPath))
	l.Stub = l.Src + ".md"
	l.Part = fsops.PartPath(l.Dst)
	return l
}

// SplitOp returns a split fake with the source already written.
func SplitOp(l Layout, rename bool, h *Hook) (*Op, error) {
	if err := os.MkdirAll(filepath.Dir(l.Src), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(l.Dst), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(l.Src, []byte(Payload), 0o644); err != nil {
		return nil, err
	}
	st, err := os.Stat(l.Src)
	if err != nil {
		return nil, err
	}
	return &Op{
		RelPath: l.RelPath, Src: l.Src, Dst: l.Dst, Size: st.Size(), Mtime: st.ModTime(),
		Rename: rename, Hook: h,
	}, nil
}

// RestoreOp returns a restore fake: the video is in the video archive and a stub is in the archive.
func RestoreOp(l Layout, rename bool, h *Hook) (*Op, error) {
	src := l.Dst
	dst := l.Src
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(src, []byte(Payload), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(dst+".md", []byte("stub\n"), 0o644); err != nil {
		return nil, err
	}
	st, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	return &Op{
		RelPath: l.RelPath, Src: src, Dst: dst, Size: st.Size(), Mtime: st.ModTime(),
		Rename: rename, Restore: true, Hook: h,
	}, nil
}

func (o *Op) begin() state.Begin {
	op := "split"
	transfer := state.TransferCopy
	if o.Restore {
		op = "restore"
	}
	if o.Rename {
		transfer = state.TransferRename
	}
	return state.Begin{
		Op: op, RelPath: o.RelPath, Src: o.Src, Dst: o.Dst,
		Size: o.Size, Mtime: o.Mtime, Transfer: transfer,
	}
}

func (o *Op) hit(point string) error {
	if o.Hook == nil {
		return nil
	}
	return o.Hook.After(point)
}

// Execute runs one transaction unless rel_path is already committed.
func (o *Op) Execute(ctx context.Context, w *state.WAL) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.Committed().Has(o.RelPath) {
		return nil
	}
	rec, err := w.Begin(o.begin())
	if err != nil {
		return err
	}
	txid := rec.TxID
	if o.Rename {
		if err := o.placeRename(); err != nil {
			return err
		}
	} else if err := o.copyVerifyPlace(txid, w); err != nil {
		return err
	}
	if _, err := w.Append(txid, state.StepPlaced, state.Record{}); err != nil {
		return err
	}
	if err := o.writeOrRemoveStub(); err != nil {
		return err
	}
	stubStep := state.StepStubbed
	if o.Restore {
		stubStep = state.StepStubRemoved
	}
	if _, err := w.Append(txid, stubStep, state.Record{Stub: o.stubPath()}); err != nil {
		return err
	}
	if !o.Rename {
		if err := os.Remove(o.Src); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := o.hit("fs:source_removed"); err != nil {
			return err
		}
		if _, err := w.Append(txid, state.StepSourceRemoved, state.Record{}); err != nil {
			return err
		}
	}
	_, err = w.Append(txid, state.StepCommit, state.Record{})
	return err
}

func (o *Op) copyVerifyPlace(txid string, w *state.WAL) error {
	part := fsops.PartPath(o.Dst)
	if err := os.MkdirAll(filepath.Dir(o.Dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(part, []byte(Payload), 0o644); err != nil {
		return err
	}
	if err := o.hit("fs:copy"); err != nil {
		return err
	}
	if _, err := w.Append(txid, state.StepCopied, state.Record{}); err != nil {
		return err
	}
	if _, err := w.Append(txid, state.StepVerified, state.Record{}); err != nil {
		return err
	}
	if err := os.Rename(part, o.Dst); err != nil {
		return err
	}
	return o.hit("fs:place")
}

func (o *Op) placeRename() error {
	if err := os.MkdirAll(filepath.Dir(o.Dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(o.Src, o.Dst); err != nil {
		return err
	}
	return o.hit("fs:place")
}

func (o *Op) stubPath() string {
	if o.Restore {
		return o.Dst + ".md"
	}
	return o.Src + ".md"
}

func (o *Op) writeOrRemoveStub() error {
	path := o.stubPath()
	if o.Restore {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if err := os.WriteFile(path, []byte("rel_path: "+o.RelPath+"\n"), 0o644); err != nil {
		return err
	}
	return o.hit("fs:stub")
}

// FinalOK reports whether the tree matches an uninterrupted successful run.
func (o *Op) FinalOK() error {
	if _, err := os.Stat(o.Src); !os.IsNotExist(err) {
		return errOr("source still present", err)
	}
	st, err := os.Stat(o.Dst)
	if err != nil {
		return err
	}
	if st.Size() != o.Size {
		return errOr("dst size", nil)
	}
	data, err := os.ReadFile(o.Dst)
	if err != nil {
		return err
	}
	if string(data) != Payload {
		return errOr("dst bytes", nil)
	}
	if _, err := os.Stat(fsops.PartPath(o.Dst)); !os.IsNotExist(err) {
		return errOr("part file remains", err)
	}
	_, stubErr := os.Stat(o.stubPath())
	if o.Restore {
		if !os.IsNotExist(stubErr) {
			return errOr("stub still present", stubErr)
		}
		return nil
	}
	return stubErr
}

func errOr(msg string, err error) error {
	if err != nil {
		return err
	}
	return &opError{msg}
}

type opError struct{ s string }

func (e *opError) Error() string { return e.s }
