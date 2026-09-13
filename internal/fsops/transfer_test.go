package fsops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// mtime has 100 ns resolution so NTFS stores it exactly.
var mtime = time.Date(2024, 5, 1, 10, 22, 3, 123456700, time.UTC)

func sourceFile(t *testing.T, dir string, data []byte, perm fs.FileMode) string {
	t.Helper()
	src := filepath.Join(dir, "archive", "projects", "interview.mp4")
	writeFile(t, src, data)
	if err := os.Chmod(src, perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return src
}

func destination(t *testing.T, dir string) string {
	t.Helper()
	dst := filepath.Join(dir, "video", "projects", "interview.mp4")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	return dst
}

func assertCopied(t *testing.T, dst string, data []byte, perm fs.FileMode) {
	t.Helper()
	assertContent(t, dst, data)
	assertMissing(t, PartPath(dst))
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(mtime) {
		t.Errorf("mtime = %s, want %s", fi.ModTime().Format(time.RFC3339Nano), mtime.Format(time.RFC3339Nano))
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != perm {
		t.Errorf("mode = %v, want %v", fi.Mode().Perm(), perm)
	}
}

func TestDurableCopyPreservesBytesMtimeAndMode(t *testing.T) {
	data := payload(1, 3<<20+517)
	sum := sha256.Sum256(data)
	for _, tc := range []struct {
		name    string
		verify  VerifyMode
		perm    fs.FileMode
		wantSum string
	}{
		{"size", VerifySize, 0o640, ""},
		{"hash", VerifyHash, 0o604, hex.EncodeToString(sum[:])},
		{"read-only source", VerifyHash, 0o444, hex.EncodeToString(sum[:])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := sourceFile(t, dir, data, tc.perm)
			dst := destination(t, dir)
			var progress []int64
			res, err := DurableCopy(context.Background(), src, dst, CopyOptions{
				Verify:        tc.verify,
				ExpectSize:    int64(len(data)),
				ExpectModTime: mtime,
				Progress:      func(n int64) { progress = append(progress, n) },
				chunk:         1 << 20,
			})
			if err != nil {
				t.Fatal(err)
			}
			assertCopied(t, dst, data, tc.perm)
			assertContent(t, src, data)
			if res.Size != int64(len(data)) || !res.ModTime.Equal(mtime) || res.SHA256 != tc.wantSum {
				t.Fatalf("result = %+v", res)
			}
			if len(progress) != 4 || progress[len(progress)-1] != int64(len(data)) {
				t.Fatalf("progress = %v, want 4 calls ending at %d", progress, len(data))
			}
		})
	}
}

func TestDurableCopyEmptyFile(t *testing.T) {
	dir := t.TempDir()
	src := sourceFile(t, dir, nil, 0o644)
	dst := destination(t, dir)
	res, err := DurableCopy(context.Background(), src, dst, CopyOptions{Verify: VerifyHash})
	if err != nil {
		t.Fatal(err)
	}
	assertCopied(t, dst, nil, 0o644)
	if want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; res.SHA256 != want {
		t.Fatalf("SHA256 = %s, want %s", res.SHA256, want)
	}
}

func TestDurableCopyReplacesStalePartFile(t *testing.T) {
	dir := t.TempDir()
	data := payload(4, 1000)
	src := sourceFile(t, dir, data, 0o644)
	dst := destination(t, dir)
	writeFile(t, PartPath(dst), payload(5, 5000))
	if _, err := DurableCopy(context.Background(), src, dst, CopyOptions{Verify: VerifyHash}); err != nil {
		t.Fatal(err)
	}
	assertCopied(t, dst, data, 0o644)
}

func TestDurableCopyNeverOverwritesUnlessAsked(t *testing.T) {
	dir := t.TempDir()
	data := payload(6, 2048)
	src := sourceFile(t, dir, data, 0o644)
	dst := destination(t, dir)
	writeFile(t, dst, []byte("existing user file"))

	_, err := DurableCopy(context.Background(), src, dst, CopyOptions{})
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("got %v, want fs.ErrExist", err)
	}
	assertContent(t, dst, []byte("existing user file"))
	assertMissing(t, PartPath(dst))

	if _, err := DurableCopy(context.Background(), src, dst, CopyOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	assertCopied(t, dst, data, 0o644)
}

func TestDurableCopyAcrossDevices(t *testing.T) {
	dir := t.TempDir()
	shm := shmDir(t, dir)
	data := payload(8, 2<<20+3)
	src := sourceFile(t, dir, data, 0o640)
	dst := filepath.Join(shm, "interview.mp4")
	if err := Rename(src, dst); !IsCrossDevice(err) {
		t.Fatalf("Rename across devices: got %v, want cross-device error", err)
	}
	for _, verify := range []VerifyMode{VerifySize, VerifyHash} {
		os.Remove(dst)
		if _, err := DurableCopy(context.Background(), src, dst, CopyOptions{Verify: verify}); err != nil {
			t.Fatalf("%s: %v", verify, err)
		}
		assertCopied(t, dst, data, 0o640)
	}
	back := filepath.Join(dir, "restored.mp4")
	if _, err := DurableCopy(context.Background(), dst, back, CopyOptions{Verify: VerifyHash}); err != nil {
		t.Fatal(err)
	}
	assertCopied(t, back, data, 0o640)
}

func TestDurableCopyProgressAtExactChunkMultiple(t *testing.T) {
	for _, verify := range []VerifyMode{VerifySize, VerifyHash} {
		dir := t.TempDir()
		data := payload(9, 2<<20)
		src := sourceFile(t, dir, data, 0o644)
		dst := destination(t, dir)
		var progress []int64
		if _, err := DurableCopy(context.Background(), src, dst, CopyOptions{Verify: verify, chunk: 1 << 20,
			Progress: func(n int64) { progress = append(progress, n) }}); err != nil {
			t.Fatal(err)
		}
		if len(progress) != 2 || progress[0] != 1<<20 || progress[1] != 2<<20 {
			t.Fatalf("%s: progress = %v, want [1MiB 2MiB]", verify, progress)
		}
		assertCopied(t, dst, data, 0o644)
	}
}
