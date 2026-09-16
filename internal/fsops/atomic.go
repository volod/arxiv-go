package fsops

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
)

// AtomicWriteFile replaces path with data so that readers and a crash see
// either the previous content or the complete new content, never a partial
// file. See AtomicWrite.
func AtomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	return AtomicWrite(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// AtomicWrite streams content from write into path+PartSuffix, fsyncs it,
// renames it over path and flushes the directory. perm (subject to the
// umask) applies to the new file. When write or any step fails, the part
// file is removed and path keeps its previous content. A leftover part file
// from a crash is replaced.
func AtomicWrite(path string, perm fs.FileMode, write func(io.Writer) error) (err error) {
	part := PartPath(path)
	if err := os.Remove(part); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		if f != nil {
			f.Close()
		}
		if err != nil {
			// Harmless when a rename moved the part file before a flush failed.
			os.Remove(part)
		}
	}()
	bw := bufio.NewWriterSize(f, 256<<10)
	if err = write(bw); err != nil {
		return err
	}
	if err = bw.Flush(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	err = f.Close()
	f = nil
	if err != nil {
		return err
	}
	return Replace(part, path)
}
