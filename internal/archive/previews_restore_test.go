package archive

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreDeletesOnlyRecordedPreviewsLive(t *testing.T) {
	cfg, c, r, src := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	unowned := filepath.Join(r.archive, "clip-img99.png")
	if err := os.WriteFile(unowned, []byte("unowned"), 0o644); err != nil {
		t.Fatal(err)
	}
	rcfg, rc := restoreConfig(r, "copy")
	rc.DeletePreviews = true
	if got := runRestore(t, rcfg, rc); got.Status != StatusCompleted {
		t.Fatalf("restore: %+v", got)
	}
	if !exists(src) || exists(filepath.Join(r.archive, "clip-smpl01.mp4")) || exists(filepath.Join(r.archive, "clip-img01.png")) || !exists(unowned) {
		t.Fatal("restore preview cleanup did not delete exactly the recorded previews")
	}
}

func TestRestoreKeepsChangedPreviewLive(t *testing.T) {
	cfg, c, r, src := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	preview := filepath.Join(r.archive, "clip-img01.png")
	changed := append(mustRead(t, preview), 'x')
	if err := os.WriteFile(preview, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	rcfg, rc := restoreConfig(r, "copy")
	rc.DeletePreviews = true
	if got := runRestore(t, rcfg, rc); got.Status != StatusPartial {
		t.Fatalf("changed preview restore: %+v", got)
	}
	if !exists(src) || !bytes.Equal(mustRead(t, preview), changed) {
		t.Fatal("restore deleted or altered the changed preview")
	}
}

// A crash after the restore commit and before the preview deletion is finished by the resumed run
// (regression: the committed video was skipped and its previews kept).
func TestRestoreCrashAfterCommitDeletesPreviewsLive(t *testing.T) {
	for _, point := range []string{"wal:commit", "wal:preview_delete", "wal:preview_deleted"} {
		t.Run(point, func(t *testing.T) {
			cfg, c, r, src := previewFixture(t)
			if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
				t.Fatalf("split: %+v", got)
			}
			rcfg, rc := restoreConfig(r, "rename")
			rc.DeletePreviews, rc.KeepSource = true, false
			fired := false
			attachRestoreRecoverer(&rcfg, &rc, func(p string) error {
				if p == point && !fired {
					fired = true
					return errors.New("injected crash")
				}
				return nil
			})
			if got := runRestore(t, rcfg, rc); got.Status != StatusFailed || !fired {
				t.Fatalf("crashed restore: %+v", got)
			}
			attachRestoreRecoverer(&rcfg, &rc, nil)
			if got := runRestore(t, rcfg, rc); got.Status != StatusCompleted {
				t.Fatalf("resumed restore: %+v", got)
			}
			if !exists(src) || exists(filepath.Join(r.archive, "clip-img01.png")) || exists(filepath.Join(r.archive, "clip-smpl01.mp4")) {
				t.Fatal("previews left after the resumed restore")
			}
		})
	}
}

// Split, restore keeping previews, then split again: the new description lists the kept previews
// (regression: every preview was already recorded, so the new description was never updated).
func TestResplitAfterRestoreLinksKeptPreviewsLive(t *testing.T) {
	cfg, c, r, src := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	rcfg, rc := restoreConfig(r, "rename")
	rc.KeepSource = false
	if got := runRestore(t, rcfg, rc); got.Status != StatusCompleted {
		t.Fatalf("restore: %+v", got)
	}
	cfg2, c2 := nextSplit(r, c)
	if got := runSplit(t, cfg2, c2); got.Status != StatusCompleted {
		t.Fatalf("second split: %+v", got)
	}
	description := mustRead(t, src+".md")
	if !bytes.Contains(description, []byte("clip-img01.png")) || !bytes.Contains(description, []byte("clip-smpl01.mp4")) {
		t.Fatalf("description lacks the kept previews:\n%s", description)
	}
}

// A kept description loses its preview section when restore deletes the previews.
func TestRestoreKeptDescriptionDropsDeletedPreviewsLive(t *testing.T) {
	cfg, c, r, src := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	rcfg, rc := restoreConfig(r, "copy")
	rc.DeletePreviews, rc.KeepDescriptions = true, true
	attachRestoreRecoverer(&rcfg, &rc, nil)
	if got := runRestore(t, rcfg, rc); got.Status != StatusCompleted {
		t.Fatalf("restore: %+v", got)
	}
	if description := mustRead(t, src+".md"); bytes.Contains(description, []byte("clip-img01.png")) || !bytes.HasPrefix(description, []byte("arxgo: clip.mp4\n")) {
		t.Fatalf("kept description:\n%s", description)
	}
}
