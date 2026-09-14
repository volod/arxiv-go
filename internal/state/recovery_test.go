package state

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
)

type recovEnv struct {
	dir, src, dst, part, stub string
	w                         *WAL
	res                       fsResolver
}

func setupRecov(t *testing.T, op, transfer string, size int) recovEnv {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "archive", "clip.mp4")
	dst := filepath.Join(dir, "video", "clip.mp4")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("v"), size)
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	w := openTestWAL(t, dir)
	begin, err := w.Begin(Begin{
		Op: op, RelPath: "clip.mp4", Src: src, Dst: dst, Size: int64(size), Transfer: transfer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if begin.RelPath != "clip.mp4" {
		t.Fatal(begin)
	}
	return recovEnv{
		dir: dir, src: src, dst: dst, part: fsops.PartPath(dst), stub: src + ".md",
		w: w, res: fsResolver{},
	}
}

func (e recovEnv) recover(t *testing.T) (Recovery, error) {
	t.Helper()
	return Recover(context.Background(), e.w, e.res, nil)
}

func (e recovEnv) append(t *testing.T, step Step) {
	t.Helper()
	tx := e.w.OpenTransactions()[0]
	if _, err := e.w.Append(tx.Begin.TxID, step, Record{}); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRecoveryTable(t *testing.T) {
	t.Run("begin part may exist dst absent aborts", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		if err := os.WriteFile(e.part, []byte("part"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := e.recover(t)
		if err != nil || got.Aborted != 1 || got.Committed != 0 {
			t.Fatalf("got %+v err %v", got, err)
		}
		if !exists(e.src) || exists(e.part) || exists(e.dst) {
			t.Fatalf("src=%v part=%v dst=%v", exists(e.src), exists(e.part), exists(e.dst))
		}
		if n := len(e.w.OpenTransactions()); n != 0 {
			t.Fatalf("still open: %+v", e.w.OpenTransactions())
		}
	})

	t.Run("begin same-device rename rolls forward", func(t *testing.T) {
		e := setupRecov(t, "split", TransferRename, 4)
		if err := os.Rename(e.src, e.dst); err != nil {
			t.Fatal(err)
		}
		got, err := e.recover(t)
		if err != nil || got.Committed != 1 {
			t.Fatalf("got %+v err %v", got, err)
		}
		if exists(e.src) || !exists(e.dst) || !exists(e.stub) {
			t.Fatalf("src=%v dst=%v stub=%v", exists(e.src), exists(e.dst), exists(e.stub))
		}
		if !e.w.Committed().Has("clip.mp4") {
			t.Fatal("not committed")
		}
	})

	t.Run("copied part present aborts", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		if err := os.WriteFile(e.part, []byte("vvvv"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.append(t, StepCopied)
		got, err := e.recover(t)
		if err != nil || got.Aborted != 1 {
			t.Fatalf("got %+v err %v", got, err)
		}
		if !exists(e.src) || exists(e.part) || exists(e.dst) {
			t.Fatal("abort should drop the part and keep the source")
		}
	})

	t.Run("verified part present aborts", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		if err := os.WriteFile(e.part, []byte("vvvv"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.append(t, StepCopied)
		e.append(t, StepVerified)
		got, err := e.recover(t)
		if err != nil || got.Aborted != 1 {
			t.Fatalf("got %+v err %v", got, err)
		}
		if !exists(e.src) || exists(e.part) {
			t.Fatal("verified abort")
		}
	})

	t.Run("placed dst present rolls forward", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		if err := os.Rename(e.src, e.dst); err != nil {
			t.Fatal(err)
		}
		e.append(t, StepPlaced)
		got, err := e.recover(t)
		if err != nil || got.Committed != 1 {
			t.Fatalf("got %+v err %v", got, err)
		}
		if exists(e.src) || !exists(e.dst) || !exists(e.stub) {
			t.Fatal("placed roll-forward")
		}
	})

	t.Run("placed dst missing is corrupt", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		e.append(t, StepPlaced)
		_, err := e.recover(t)
		if !errors.Is(err, ErrStateCorrupt) {
			t.Fatalf("err = %v", err)
		}
		if !exists(e.src) || exists(e.dst) {
			t.Fatal("must leave files")
		}
	})

	t.Run("placed dst wrong size is corrupt", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		if err := os.WriteFile(e.dst, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.append(t, StepPlaced)
		_, err := e.recover(t)
		if !errors.Is(err, ErrStateCorrupt) {
			t.Fatalf("err = %v", err)
		}
		if !exists(e.src) || !exists(e.dst) {
			t.Fatal("must leave files")
		}
	})

	t.Run("stubbed removes source and commits", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		body, _ := os.ReadFile(e.src)
		if err := os.WriteFile(e.dst, body, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(e.stub, []byte("rel_path: clip.mp4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.append(t, StepPlaced)
		e.append(t, StepStubbed)
		got, err := e.recover(t)
		if err != nil || got.Committed != 1 {
			t.Fatalf("got %+v err %v", got, err)
		}
		if exists(e.src) || !exists(e.dst) {
			t.Fatal("stubbed")
		}
	})

	t.Run("source_removed commits", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		if err := os.Rename(e.src, e.dst); err != nil {
			t.Fatal(err)
		}
		e.append(t, StepPlaced)
		e.append(t, StepStubbed)
		e.append(t, StepSourceRemoved)
		got, err := e.recover(t)
		if err != nil || got.Committed != 1 {
			t.Fatalf("got %+v err %v", got, err)
		}
	})

	t.Run("stub_removed restore rolls forward", func(t *testing.T) {
		e := setupRecov(t, "restore", TransferCopy, 4)
		body, _ := os.ReadFile(e.src)
		if err := os.WriteFile(e.dst, body, 0o644); err != nil {
			t.Fatal(err)
		}
		e.append(t, StepPlaced)
		e.append(t, StepStubRemoved)
		got, err := e.recover(t)
		if err != nil || got.Committed != 1 {
			t.Fatalf("got %+v err %v", got, err)
		}
		if exists(e.src) || !exists(e.dst) {
			t.Fatalf("restore stub_removed src=%v dst=%v", exists(e.src), exists(e.dst))
		}
	})

	t.Run("copied dst already placed rolls forward", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		if err := os.Rename(e.src, e.dst); err != nil {
			t.Fatal(err)
		}
		e.append(t, StepCopied)
		got, err := e.recover(t)
		if err != nil || got.Committed != 1 {
			t.Fatalf("got %+v err %v", got, err)
		}
		if exists(e.src) || !exists(e.dst) || !exists(e.stub) {
			t.Fatal("copied-with-dst")
		}
	})

	t.Run("begin lost file is corrupt", func(t *testing.T) {
		e := setupRecov(t, "split", TransferCopy, 4)
		if err := os.Remove(e.src); err != nil {
			t.Fatal(err)
		}
		_, err := e.recover(t)
		if !errors.Is(err, ErrStateCorrupt) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRecoveryIsIdempotent(t *testing.T) {
	e := setupRecov(t, "split", TransferRename, 8)
	if err := os.Rename(e.src, e.dst); err != nil {
		t.Fatal(err)
	}
	if _, err := e.recover(t); err != nil {
		t.Fatal(err)
	}
	before := mustReadFile(t, e.w.Path())
	src, dst, stub := exists(e.src), exists(e.dst), exists(e.stub)
	got, err := e.recover(t)
	if err != nil || got.Aborted != 0 || got.Committed != 0 {
		t.Fatalf("second recover %+v err %v", got, err)
	}
	after := mustReadFile(t, e.w.Path())
	if !bytes.Equal(before, after) {
		t.Fatalf("WAL changed\n%s\n%s", before, after)
	}
	if exists(e.src) != src || exists(e.dst) != dst || exists(e.stub) != stub {
		t.Fatal("filesystem changed")
	}
}

func TestRecoverySeqOrder(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir)
	var paths []string
	for _, name := range []string{"a.mp4", "b.mp4"} {
		src := filepath.Join(dir, name)
		dst := filepath.Join(dir, "out", name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(src, []byte("xx"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Begin(Begin{Op: "split", RelPath: name, Src: src, Dst: dst, Size: 2, Transfer: TransferCopy}); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, src)
	}
	got, err := Recover(context.Background(), w, fsResolver{}, nil)
	if err != nil || got.Aborted != 2 {
		t.Fatalf("got %+v err %v", got, err)
	}
	for _, p := range paths {
		if !exists(p) {
			t.Fatalf("lost %s", p)
		}
	}
}
