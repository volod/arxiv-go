package fsops

import (
	"context"
	"errors"
	"hash"
	"io"
	"os"
)

// copyChunks streams src into dst in chunks, checking ctx and reporting
// progress between chunks. Without a hash, io.CopyN lets *os.File use
// copy_file_range or sendfile on Linux. With a hash, the digest is computed
// on another goroutine while the data is written.
func copyChunks(ctx context.Context, dst *os.File, src *os.File, h hash.Hash, opts CopyOptions) (int64, error) {
	chunk := opts.chunk
	if chunk <= 0 {
		chunk = defaultChunk
	}
	if h != nil {
		return copyHashing(ctx, dst, src, h, chunk, opts.Progress)
	}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := io.CopyN(dst, src, chunk)
		total += n
		if err != nil && !errors.Is(err, io.EOF) {
			return total, err
		}
		if opts.Progress != nil && n > 0 {
			opts.Progress(total)
		}
		if n < chunk {
			return total, nil
		}
	}
}

// copyHashing reads src into a small ring of buffers; each buffer is handed
// to a hashing goroutine and written to dst concurrently, then returned to
// the ring once hashed. Hashing order equals read order.
func copyHashing(ctx context.Context, dst io.Writer, src io.Reader, h hash.Hash, chunk int64, progress func(int64)) (total int64, err error) {
	const ring = 4
	free := make(chan []byte, ring)
	for range ring {
		free <- make([]byte, copyBufferLen)
	}
	work := make(chan []byte, ring)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for b := range work {
			h.Write(b)
			free <- b[:cap(b)]
		}
	}()
	defer func() {
		close(work)
		<-done
	}()
	nextCheck, reported := chunk, int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		var buf []byte
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case buf = <-free:
		}
		n, rerr := io.ReadFull(src, buf)
		if n > 0 {
			work <- buf[:n]
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		} else {
			free <- buf
		}
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			if progress != nil && total != reported {
				progress(total)
			}
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
		if total >= nextCheck {
			nextCheck += chunk
			if progress != nil {
				progress(total)
				reported = total
			}
			if err := ctx.Err(); err != nil {
				return total, err
			}
		}
	}
}
