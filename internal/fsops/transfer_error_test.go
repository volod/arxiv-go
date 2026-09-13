package fsops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableCopyVerificationMismatchLeavesNoDestination(t *testing.T) {
	corrupt := map[string]struct {
		verify VerifyMode
		damage func(part string) error
	}{
		"hash: flipped byte": {VerifyHash, func(part string) error {
			f, err := os.OpenFile(part, os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.WriteAt([]byte{0xff ^ payload(2, 100)[50]}, 50)
			return err
		}},
		"size: truncated": {VerifySize, func(part string) error { return os.Truncate(part, 99) }},
	}
	for name, tc := range corrupt {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			data := payload(2, 100)
			src := sourceFile(t, dir, data, 0o644)
			dst := destination(t, dir)
			_, err := DurableCopy(context.Background(), src, dst, CopyOptions{Verify: tc.verify, beforeVerify: tc.damage})
			var ve *VerifyError
			if !errors.Is(err, ErrVerifyMismatch) || !errors.As(err, &ve) || ve.Mode != tc.verify {
				t.Fatalf("got %v, want %s verification mismatch", err, tc.verify)
			}
			assertMissing(t, dst)
			assertMissing(t, PartPath(dst))
			assertContent(t, src, data)
		})
	}
}

func TestDurableCopyRemovesPartFileOnError(t *testing.T) {
	data := payload(3, 4<<20)
	for _, verify := range []VerifyMode{VerifySize, VerifyHash} {
		t.Run("canceled mid-copy/"+verify.String(), func(t *testing.T) {
			dir := t.TempDir()
			src := sourceFile(t, dir, data, 0o644)
			dst := destination(t, dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := DurableCopy(ctx, src, dst, CopyOptions{Verify: verify, chunk: 1 << 20, Progress: func(n int64) {
				if _, statErr := os.Stat(PartPath(dst)); statErr != nil {
					t.Errorf("part file missing during copy: %v", statErr)
				}
				cancel()
			}})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want context.Canceled", err)
			}
			assertMissing(t, dst)
			assertMissing(t, PartPath(dst))
		})
	}
	t.Run("hook error after sync", func(t *testing.T) {
		dir := t.TempDir()
		src := sourceFile(t, dir, data, 0o644)
		dst := destination(t, dir)
		boom := errors.New("injected")
		_, err := DurableCopy(context.Background(), src, dst, CopyOptions{beforeVerify: func(string) error { return boom }})
		if !errors.Is(err, boom) {
			t.Fatalf("got %v, want injected error", err)
		}
		assertMissing(t, dst)
		assertMissing(t, PartPath(dst))
	})
	t.Run("missing destination directory", func(t *testing.T) {
		dir := t.TempDir()
		src := sourceFile(t, dir, data, 0o644)
		dst := filepath.Join(dir, "missing", "clip.mp4")
		if _, err := DurableCopy(context.Background(), src, dst, CopyOptions{}); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("got %v, want fs.ErrNotExist", err)
		}
		assertMissing(t, filepath.Dir(dst))
	})
	t.Run("source is a directory", func(t *testing.T) {
		dir := t.TempDir()
		dst := destination(t, dir)
		if _, err := DurableCopy(context.Background(), filepath.Dir(dst), filepath.Join(dir, "x"), CopyOptions{}); err == nil {
			t.Fatal("copying a directory succeeded")
		}
		assertMissing(t, PartPath(filepath.Join(dir, "x")))
	})
}

func TestDurableCopyDetectsSourceChanges(t *testing.T) {
	data := payload(7, 3<<20)
	t.Run("differs from expected attributes", func(t *testing.T) {
		dir := t.TempDir()
		src := sourceFile(t, dir, data, 0o644)
		dst := destination(t, dir)
		for _, opts := range []CopyOptions{
			{ExpectSize: int64(len(data)) + 1, ExpectModTime: mtime},
			{ExpectSize: int64(len(data)), ExpectModTime: mtime.Add(time.Second)},
		} {
			if _, err := DurableCopy(context.Background(), src, dst, opts); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("opts %+v: got %v, want ErrSourceChanged", opts, err)
			}
			assertMissing(t, dst)
			assertMissing(t, PartPath(dst))
		}
	})
	for _, verify := range []VerifyMode{VerifySize, VerifyHash} {
		t.Run("modified while copying/"+verify.String(), func(t *testing.T) {
			dir := t.TempDir()
			src := sourceFile(t, dir, data, 0o644)
			dst := destination(t, dir)
			appended := false
			_, err := DurableCopy(context.Background(), src, dst, CopyOptions{Verify: verify, chunk: 1 << 20, Progress: func(int64) {
				if appended {
					return
				}
				appended = true
				f, err := os.OpenFile(src, os.O_WRONLY|os.O_APPEND, 0)
				if err != nil {
					t.Error(err)
					return
				}
				f.Write([]byte("more"))
				f.Close()
			}})
			if !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("got %v, want ErrSourceChanged", err)
			}
			assertMissing(t, dst)
			assertMissing(t, PartPath(dst))
		})
	}
}

func TestDurableCopyOverwriteFailureKeepsDestination(t *testing.T) {
	dir := t.TempDir()
	data := payload(10, 1000)
	src := sourceFile(t, dir, data, 0o644)
	dst := destination(t, dir)
	old := []byte("existing user file")
	writeFile(t, dst, old)
	boom := errors.New("injected")
	_, err := DurableCopy(context.Background(), src, dst, CopyOptions{
		Overwrite:    true,
		beforeVerify: func(string) error { return boom },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want injected error", err)
	}
	assertContent(t, dst, old)
	assertMissing(t, PartPath(dst))
	assertContent(t, src, data)
}

func TestDurableCopyHonorsCanceledContext(t *testing.T) {
	data := payload(11, 2<<20)
	for _, verify := range []VerifyMode{VerifySize, VerifyHash} {
		t.Run(verify.String(), func(t *testing.T) {
			dir := t.TempDir()
			src := sourceFile(t, dir, data, 0o644)
			dst := destination(t, dir)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := DurableCopy(ctx, src, dst, CopyOptions{Verify: verify})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want context.Canceled", err)
			}
			assertMissing(t, dst)
			assertMissing(t, PartPath(dst))
			assertContent(t, src, data)
		})
	}
}
