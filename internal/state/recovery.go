package state

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/volod/arxiv-go/internal/fsops"
)

// Recovery is the summary of one Recover pass.
type Recovery struct {
	Aborted   int
	Committed int
}

// Observation is the filesystem facts the recovery table needs.
type Observation struct {
	SrcExists  bool
	SrcSize    int64
	DstExists  bool
	DstSize    int64
	PartExists bool
}

// DstMatches reports whether dst is present and has the begin size.
func (o Observation) DstMatches(begin Record) bool {
	return o.DstExists && o.DstSize == begin.Size
}

// Resolver applies operation-specific filesystem effects during recovery. Split and restore
// supply their own; tests use a fake or FSResolver.
type Resolver interface {
	Inspect(tx Tx) (Observation, error)
	DeletePart(tx Tx) error
	WriteStub(tx Tx) error
	RemoveSource(tx Tx) error
}

// Recover walks open transactions in begin-seq order and applies the integrity recovery table:
// unplaced work is aborted; at or after placed, the destination is kept and the rest rolls
// forward. A second call is a no-op when the first succeeded.
func Recover(ctx context.Context, w *WAL, res Resolver, log *slog.Logger) (Recovery, error) {
	if res == nil {
		return Recovery{}, fmt.Errorf("wal: recovery requires a resolver")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	var out Recovery
	for _, tx := range w.OpenTransactions() {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		log.Info("recovering transaction", "txid", tx.Begin.TxID, "rel_path", tx.Begin.RelPath,
			"last_step", tx.Last.Step, "seq", tx.Begin.Seq)
		n, err := recoverOne(w, res, log, tx)
		if err != nil {
			return out, err
		}
		out.Aborted += n.Aborted
		out.Committed += n.Committed
	}
	return out, nil
}

func recoverOne(w *WAL, res Resolver, log *slog.Logger, tx Tx) (Recovery, error) {
	obs, err := res.Inspect(tx)
	if err != nil {
		return Recovery{}, err
	}
	switch tx.Last.Step {
	case StepBegin:
		return recoverBegin(w, res, log, tx, obs)
	case StepCopied, StepVerified:
		return recoverUnplaced(w, res, log, tx, obs)
	case StepPlaced:
		return recoverPlaced(w, res, log, tx, obs)
	case StepStubbed, StepStubRemoved:
		return recoverStubbed(w, res, log, tx, obs)
	case StepSourceRemoved:
		return commitTx(w, log, tx)
	default:
		return Recovery{}, fmt.Errorf("%w: unknown WAL step %q in %s", ErrStateCorrupt, tx.Last.Step, tx.Begin.TxID)
	}
}

func recoverBegin(w *WAL, res Resolver, log *slog.Logger, tx Tx, obs Observation) (Recovery, error) {
	if !obs.SrcExists && obs.DstMatches(tx.Begin) {
		if err := res.DeletePart(tx); err != nil {
			return Recovery{}, err
		}
		log.Info("recovery: same-device rename finished after begin; rolling forward",
			"txid", tx.Begin.TxID, "rel_path", tx.Begin.RelPath)
		if _, err := w.Append(tx.Begin.TxID, StepPlaced, Record{}); err != nil {
			return Recovery{}, err
		}
		tx.Last.Step = StepPlaced
		obs.PartExists = false
		return recoverPlaced(w, res, log, tx, obs)
	}
	if obs.SrcExists {
		return abortTx(w, res, log, tx, "unplaced")
	}
	return Recovery{}, fmt.Errorf("%w: transaction %s lost the file after begin (src and dst missing or dst size %d != %d)",
		ErrStateCorrupt, tx.Begin.TxID, obs.DstSize, tx.Begin.Size)
}

func recoverUnplaced(w *WAL, res Resolver, log *slog.Logger, tx Tx, obs Observation) (Recovery, error) {
	if obs.PartExists {
		return abortTx(w, res, log, tx, "unplaced")
	}
	if obs.DstMatches(tx.Begin) {
		log.Info("recovery: destination already placed; rolling forward",
			"txid", tx.Begin.TxID, "rel_path", tx.Begin.RelPath, "last_step", tx.Last.Step)
		if _, err := w.Append(tx.Begin.TxID, StepPlaced, Record{}); err != nil {
			return Recovery{}, err
		}
		tx.Last.Step = StepPlaced
		return recoverPlaced(w, res, log, tx, obs)
	}
	if obs.SrcExists && !obs.DstExists {
		return abortTx(w, res, log, tx, "unplaced")
	}
	return Recovery{}, fmt.Errorf("%w: transaction %s is unplaced but the source is missing", ErrStateCorrupt, tx.Begin.TxID)
}

func recoverPlaced(w *WAL, res Resolver, log *slog.Logger, tx Tx, obs Observation) (Recovery, error) {
	if !obs.DstMatches(tx.Begin) {
		return Recovery{}, fmt.Errorf("%w: destination missing or wrong size after placed in %s (size %d, want %d, exists %v)",
			ErrStateCorrupt, tx.Begin.TxID, obs.DstSize, tx.Begin.Size, obs.DstExists)
	}
	step := StepStubbed
	if tx.Begin.Op == "restore" {
		step = StepStubRemoved
	}
	// The path is chosen before the effect: a removed stub can no longer be found afterwards.
	path := stubPath(tx.Begin)
	if namer, ok := res.(interface{ StubPath(Tx) string }); ok {
		path = namer.StubPath(tx)
	}
	if err := res.WriteStub(tx); err != nil {
		return Recovery{}, err
	}
	if _, err := w.Append(tx.Begin.TxID, step, Record{Stub: path}); err != nil {
		return Recovery{}, err
	}
	tx.Last.Step = step
	return recoverStubbed(w, res, log, tx, obs)
}

func recoverStubbed(w *WAL, res Resolver, log *slog.Logger, tx Tx, obs Observation) (Recovery, error) {
	obs, err := res.Inspect(tx)
	if err != nil {
		return Recovery{}, err
	}
	if !obs.DstMatches(tx.Begin) {
		return Recovery{}, fmt.Errorf("%w: destination does not verify after %s in %s", ErrStateCorrupt, tx.Last.Step, tx.Begin.TxID)
	}
	if obs.SrcExists {
		if err := res.RemoveSource(tx); err != nil {
			return Recovery{}, err
		}
	}
	return commitTx(w, log, tx)
}

func abortTx(w *WAL, res Resolver, log *slog.Logger, tx Tx, reason string) (Recovery, error) {
	if err := res.DeletePart(tx); err != nil {
		return Recovery{}, err
	}
	if _, err := w.Append(tx.Begin.TxID, StepAborted, Record{Reason: reason}); err != nil {
		return Recovery{}, err
	}
	log.Info("recovery: aborted unplaced transaction", "txid", tx.Begin.TxID, "rel_path", tx.Begin.RelPath, "reason", reason)
	return Recovery{Aborted: 1}, nil
}

func commitTx(w *WAL, log *slog.Logger, tx Tx) (Recovery, error) {
	if _, err := w.Append(tx.Begin.TxID, StepCommit, Record{}); err != nil {
		return Recovery{}, err
	}
	log.Info("recovery: committed transaction", "txid", tx.Begin.TxID, "rel_path", tx.Begin.RelPath)
	return Recovery{Committed: 1}, nil
}

func stubPath(b Record) string {
	if b.Op == "restore" {
		return b.Dst + ".md"
	}
	return b.Src + ".md"
}

// FSResolver implements Resolver with the process filesystem. Stub writing uses Src+".md" for
// split and removes Dst+".md" for restore. Operations replace this with their own resolver.
type FSResolver struct {
	Crash CrashHook
}

func (r FSResolver) hit(point string) error {
	if r.Crash == nil {
		return nil
	}
	return r.Crash(point)
}

// Inspect stats src, dst and the destination part file.
func (r FSResolver) Inspect(tx Tx) (Observation, error) {
	var o Observation
	var err error
	if o.SrcExists, o.SrcSize, err = inspectPath(tx.Begin.Src); err != nil {
		return Observation{}, err
	}
	if o.DstExists, o.DstSize, err = inspectPath(tx.Begin.Dst); err != nil {
		return Observation{}, err
	}
	if o.PartExists, _, err = inspectPath(fsops.PartPath(tx.Begin.Dst)); err != nil {
		return Observation{}, err
	}
	return o, nil
}

// DeletePart removes dst.arxgo-part if it exists.
func (r FSResolver) DeletePart(tx Tx) error {
	if err := removeIfPresent(fsops.PartPath(tx.Begin.Dst)); err != nil {
		return err
	}
	return r.hit("fs:delete_part")
}

// WriteStub writes or removes the Markdown stub for the operation.
func (r FSResolver) WriteStub(tx Tx) error {
	path := stubPath(tx.Begin)
	var err error
	if tx.Begin.Op == "restore" {
		err = removeIfPresent(path)
	} else {
		err = fsops.AtomicWriteFile(path, []byte("rel_path: "+tx.Begin.RelPath+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	return r.hit("fs:stub")
}

// RemoveSource deletes the source file if it is still present.
func (r FSResolver) RemoveSource(tx Tx) error {
	if err := removeIfPresent(tx.Begin.Src); err != nil {
		return err
	}
	return r.hit("fs:source_removed")
}

func inspectPath(path string) (bool, int64, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	return true, fi.Size(), nil
}

func removeIfPresent(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
