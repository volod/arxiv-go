package state

import (
	"errors"
	"io/fs"
	"os"

	"github.com/volod/arxiv-go/internal/fsops"
)

// fsResolver implements Resolver with the process filesystem for the recovery table tests: it
// writes Src+".md" for split and removes Dst+".md" for restore.
type fsResolver struct {
	Crash CrashHook
}

func (r fsResolver) hit(point string) error {
	if r.Crash == nil {
		return nil
	}
	return r.Crash(point)
}

// Inspect stats src, dst and the destination part file.
func (r fsResolver) Inspect(tx Tx) (Observation, error) {
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
func (r fsResolver) DeletePart(tx Tx) error {
	if err := removeIfPresent(fsops.PartPath(tx.Begin.Dst)); err != nil {
		return err
	}
	return r.hit("fs:delete_part")
}

// WriteDescription writes or removes the video description for the operation.
func (r fsResolver) WriteDescription(tx Tx) error {
	path := descriptionPath(tx.Begin)
	var err error
	if tx.Begin.Op == "restore" {
		err = removeIfPresent(path)
	} else {
		err = fsops.AtomicWriteFile(path, []byte("rel_path: "+tx.Begin.RelPath+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	return r.hit("fs:description")
}

// RemoveSource deletes the source file if it is still present.
func (r fsResolver) RemoveSource(tx Tx) error {
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
