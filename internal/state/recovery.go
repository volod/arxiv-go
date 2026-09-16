package state

import (
	"context"
	"fmt"
	"log/slog"
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
// supply their own; tests use the filesystem resolver of test/fixtures/crashtest.
type Resolver interface {
	Inspect(tx Tx) (Observation, error)
	DeletePart(tx Tx) error
	WriteDescription(tx Tx) error
	RemoveSource(tx Tx) error
}

// DescribedAnnotator is implemented by a resolver whose described record carries payload metadata
// collected while it wrote the description (the CATIA summary). It is called after WriteDescription.
type DescribedAnnotator interface {
	DescribedCatia(tx Tx) *CatiaSummary
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
	case StepDescribed, StepDescriptionRemoved:
		return recoverDescribed(w, res, log, tx)
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
	step := StepDescribed
	if tx.Begin.Op == "restore" {
		step = StepDescriptionRemoved
	}
	// The path is chosen before the effect: a removed description can no longer be found afterwards.
	path := descriptionPath(tx.Begin)
	if namer, ok := res.(interface{ DescriptionPath(Tx) string }); ok {
		path = namer.DescriptionPath(tx)
	}
	if err := res.WriteDescription(tx); err != nil {
		return Recovery{}, err
	}
	extra := Record{Description: path}
	if annotator, ok := res.(DescribedAnnotator); ok {
		extra.Catia = annotator.DescribedCatia(tx)
	}
	if _, err := w.Append(tx.Begin.TxID, step, extra); err != nil {
		return Recovery{}, err
	}
	tx.Last.Step = step
	return recoverDescribed(w, res, log, tx)
}

func recoverDescribed(w *WAL, res Resolver, log *slog.Logger, tx Tx) (Recovery, error) {
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

func descriptionPath(b Record) string {
	if b.Op == "restore" {
		return b.Dst + ".md"
	}
	return b.Src + ".md"
}
