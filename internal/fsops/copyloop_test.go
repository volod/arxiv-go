package fsops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
	"time"
)

func TestCopyHashingAgreesWithSHA256(t *testing.T) {
	data := payload(12, 3<<20+99)
	h := sha256.New()
	var dst bytes.Buffer
	n, err := copyHashing(context.Background(), &dst, bytes.NewReader(data), h, 1<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(data)) || !bytes.Equal(dst.Bytes(), data) {
		t.Fatalf("copied %d bytes (dest %d), want %d", n, dst.Len(), len(data))
	}
	sum := sha256.Sum256(data)
	if got := h.Sum(nil); !bytes.Equal(got, sum[:]) {
		t.Fatalf("digest = %x, want %x", got, sum)
	}
}

func TestCopyHashingHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := copyHashing(ctx, io.Discard, bytes.NewReader(payload(13, 2<<20)), sha256.New(), 1<<20, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if n != 0 {
		t.Fatalf("copied %d bytes after cancel", n)
	}
}

func TestCopyHashingSlowWriterMatches(t *testing.T) {
	data := payload(14, 2<<20+7)
	h := sha256.New()
	var dst bytes.Buffer
	n, err := copyHashing(context.Background(), delayWriter{w: &dst, d: time.Millisecond}, bytes.NewReader(data), h, 1<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(data)) || !bytes.Equal(dst.Bytes(), data) {
		t.Fatalf("copied %d bytes (dest %d), want %d", n, dst.Len(), len(data))
	}
	sum := sha256.Sum256(data)
	if got := h.Sum(nil); !bytes.Equal(got, sum[:]) {
		t.Fatalf("digest = %x, want %x", got, sum)
	}
}

type delayWriter struct {
	w io.Writer
	d time.Duration
}

func (s delayWriter) Write(p []byte) (int, error) {
	time.Sleep(s.d)
	return s.w.Write(p)
}
