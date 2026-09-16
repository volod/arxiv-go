package fsops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"runtime"
	"time"
)

// VerifyMode selects how DurableCopy checks the written file.
type VerifyMode int

const (
	// VerifySize compares the durable part file size with the source size.
	VerifySize VerifyMode = iota
	// VerifyHash also hashes the source stream while copying and re-reads
	// the part file to compare SHA-256 digests.
	VerifyHash
)

func (m VerifyMode) String() string {
	if m == VerifyHash {
		return "hash"
	}
	return "size"
}

const (
	defaultChunk  = 32 << 20 // bytes between cancellation checks and progress calls
	copyBufferLen = 1 << 20
)

var (
	// ErrSourceChanged reports that the source size or modification time
	// differs from the expected values or changed while it was copied.
	ErrSourceChanged = errors.New("fsops: source changed during copy")
	// ErrVerifyMismatch matches every *VerifyError.
	ErrVerifyMismatch = errors.New("fsops: copy verification mismatch")
)

// VerifyError describes a destination that does not match its source.
type VerifyError struct {
	Path string
	Mode VerifyMode
	Want string
	Got  string
}

func (e *VerifyError) Error() string {
	return fmt.Sprintf("fsops: %s verification of %s failed: want %s, got %s", e.Mode, e.Path, e.Want, e.Got)
}

// Is makes errors.Is(err, ErrVerifyMismatch) true.
func (e *VerifyError) Is(target error) bool { return target == ErrVerifyMismatch }

// CopyOptions configures DurableCopy.
type CopyOptions struct {
	Verify VerifyMode
	// Overwrite replaces an existing destination atomically (restore
	// --overwrite). Without it an existing destination is never touched.
	Overwrite bool
	// ExpectSize and ExpectModTime, when ExpectModTime is non-zero, are the
	// source attributes recorded before the copy (the WAL begin record).
	// A difference yields ErrSourceChanged.
	ExpectSize    int64
	ExpectModTime time.Time
	// Progress, when set, receives the cumulative bytes copied.
	Progress func(copied int64)

	chunk        int64                   // test override of defaultChunk
	beforeVerify func(part string) error // test hook after the data is durable
}

// CopyResult describes a completed copy.
type CopyResult struct {
	Size    int64
	ModTime time.Time
	// SHA256 is the lowercase hex digest with VerifyHash, empty otherwise.
	SHA256 string
}

// DurableCopy copies the regular file src to dst so that dst either does not
// exist or is complete and verified, even across a crash:
//
//  1. stream src into dst+PartSuffix (checking ctx between chunks),
//  2. fsync, check that src did not change, preserve its modification time
//     and (except on Windows) permission bits,
//  3. verify size or SHA-256 against the durable part file,
//  4. rename the part file to dst and flush the directory.
//
// The parent directory of dst must exist. On any error the part file is
// removed and dst is left as it was. Without Overwrite an existing dst
// yields an error matching fs.ErrExist. A canceled ctx returns before any
// destination file is created.
func DurableCopy(ctx context.Context, src, dst string, opts CopyOptions) (res CopyResult, err error) {
	res, err = StageCopy(ctx, src, dst, opts)
	if err != nil {
		return res, err
	}
	part := PartPath(dst)
	if opts.Overwrite {
		err = Replace(part, dst)
	} else {
		err = Rename(part, dst)
	}
	var le *os.LinkError
	if err != nil && errors.As(err, &le) {
		// The rename failed; dst is untouched.
		_ = os.Remove(part)
		return CopyResult{}, err
	}
	// A non-LinkError means dst is in place but the directory flush failed.
	return res, err
}

// StageCopy writes and verifies dst.arxgo-part without placing dst. The caller must record its
// write-ahead steps, then use Rename or Replace to place the part. An error removes the part.
func StageCopy(ctx context.Context, src, dst string, opts CopyOptions) (res CopyResult, err error) {
	if err := ctx.Err(); err != nil {
		return res, err
	}
	if !opts.Overwrite {
		if _, err := os.Lstat(dst); err == nil {
			return res, &os.PathError{Op: "copy", Path: dst, Err: fs.ErrExist}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return res, err
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return res, err
	}
	defer in.Close()
	before, err := in.Stat()
	if err != nil {
		return res, err
	}
	if !before.Mode().IsRegular() {
		return res, &os.PathError{Op: "copy", Path: src, Err: errors.New("not a regular file")}
	}
	if !opts.ExpectModTime.IsZero() && !sameAttrs(before, opts.ExpectSize, opts.ExpectModTime) {
		return res, fmt.Errorf("%w: %s is %d bytes at %s, expected %d bytes at %s", ErrSourceChanged,
			src, before.Size(), before.ModTime().Format(time.RFC3339Nano),
			opts.ExpectSize, opts.ExpectModTime.Format(time.RFC3339Nano))
	}

	part := PartPath(dst)
	if err := os.Remove(part); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	out, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return res, err
	}
	defer func() {
		if out != nil {
			out.Close()
		}
		if err != nil {
			os.Remove(part)
		}
	}()

	var h hash.Hash
	if opts.Verify == VerifyHash {
		h = sha256.New()
	}
	n, err := copyChunks(ctx, out, in, h, opts)
	if err != nil {
		return res, err
	}
	if err = out.Sync(); err != nil {
		return res, err
	}
	err = out.Close()
	out = nil
	if err != nil {
		return res, err
	}
	after, err := os.Stat(src)
	if err != nil {
		return res, err
	}
	if n != before.Size() || !sameAttrs(after, before.Size(), before.ModTime()) {
		return res, fmt.Errorf("%w: %s copied %d of %d bytes, now %d bytes at %s", ErrSourceChanged,
			src, n, before.Size(), after.Size(), after.ModTime().Format(time.RFC3339Nano))
	}

	// Set the time on the closed file: Windows may update the last write
	// time when a handle that wrote data is closed.
	if err = os.Chtimes(part, time.Time{}, before.ModTime()); err != nil {
		return res, err
	}
	if opts.beforeVerify != nil {
		if err = opts.beforeVerify(part); err != nil {
			return res, err
		}
	}
	if err = finishPart(part, before, h, opts.Verify); err != nil {
		return res, err
	}
	res = CopyResult{Size: n, ModTime: before.ModTime()}
	if h != nil {
		res.SHA256 = hex.EncodeToString(h.Sum(nil))
	}
	return res, nil
}

// finishPart verifies the part file against the source, applies the source
// permission bits and flushes the file metadata (including the time set by
// Chtimes). The handle is opened for writing because Windows requires write
// access to flush.
func finishPart(part string, src fs.FileInfo, h hash.Hash, mode VerifyMode) error {
	f, err := os.OpenFile(part, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() != src.Size() {
		return &VerifyError{Path: part, Mode: VerifySize,
			Want: fmt.Sprint(src.Size()), Got: fmt.Sprint(fi.Size())}
	}
	if mode == VerifyHash {
		want := hex.EncodeToString(h.Sum(nil))
		rh := sha256.New()
		// Hide File.WriteTo so the 1 MiB buffer is used for the re-read.
		if _, err := io.CopyBuffer(rh, struct{ io.Reader }{f}, make([]byte, copyBufferLen)); err != nil {
			return err
		}
		if got := hex.EncodeToString(rh.Sum(nil)); got != want {
			return &VerifyError{Path: part, Mode: VerifyHash, Want: want, Got: got}
		}
	}
	if runtime.GOOS != "windows" {
		// Filesystems without Unix modes (CIFS without permission support,
		// exFAT) reject chmod; the copy is still valid there.
		err := f.Chmod(src.Mode().Perm())
		if err != nil && !errors.Is(err, fs.ErrPermission) && !errors.Is(err, errors.ErrUnsupported) {
			return err
		}
	}
	return f.Sync()
}

func sameAttrs(fi fs.FileInfo, size int64, mtime time.Time) bool {
	return fi.Size() == size && fi.ModTime().Equal(mtime)
}
