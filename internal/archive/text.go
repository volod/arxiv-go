package archive

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/volod/arxiv-go/internal/catia"
	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

const catiaTextNeedCap = 1 << 20

// textIdentityReserve is the preflight estimate of a sidecar's header beyond its extracted text: the
// archive root, file name, the identity block repeated from the description and the timestamps.
const textIdentityReserve = 4 << 10

// extractCatia is the text-sidecar extractor; tests replace it to inject failures.
var extractCatia = catia.ExtractPath

func newTextIndex(archive string, history []runHistory) (*eventIndex, error) {
	return newEventIndex(state.TextEvents, archive, fsops.PartPath, history)
}

// splitTexts generates owned CATIA text sidecars after a move commits, and catches up files
// earlier CATIA splits moved that still lack one.
type splitTexts struct {
	s       *Session
	w       *state.WAL
	idx     *eventIndex
	ctx     context.Context
	earlier []string          // CATIA files earlier runs already moved
	hints   map[string]string // rel_path -> description rel_path recorded by earlier runs
}

func startTextSidecars(ctx context.Context, v *catiaSplit, w *state.WAL) (postCommit, error) {
	t := &splitTexts{s: v.s, w: w, idx: v.idx, ctx: ctx, earlier: v.movedRels(),
		hints: movedDescriptions(v.history)}
	if err := t.catchUp(w.Committed()); err != nil {
		return nil, t.stop(err)
	}
	return t, nil
}

func (v *catiaSplit) movedRels() []string {
	seen := map[string]struct{}{}
	add := func(rel string) {
		if scanner.LocalRelPath(rel) {
			seen[rel] = struct{}{}
		}
	}
	for _, row := range v.rows {
		if row.Status == report.StatusMoved {
			add(row.RelPath)
		}
	}
	for _, run := range v.history {
		split, _ := splitEvents(run)
		for rel, a := range split {
			if a.status == report.StatusMoved {
				add(rel)
			}
		}
	}
	return sortedKeys(seen)
}

func (t *splitTexts) catchUp(committed *state.CommittedSet) error {
	need := map[string]struct{}{}
	for _, rel := range t.earlier {
		need[rel] = struct{}{}
	}
	for _, rel := range committed.Paths() {
		need[rel] = struct{}{}
	}
	for _, rel := range sortedKeys(need) {
		if err := t.ctx.Err(); err != nil {
			return err
		}
		if err := t.generate(rel); err != nil {
			return err
		}
	}
	return nil
}

func (t *splitTexts) committed(rel string) error { return t.generate(rel) }

func (t *splitTexts) stop(err error) error { return err }

func (t *splitTexts) generate(rel string) error {
	if !scanner.LocalRelPath(rel) {
		return nil
	}
	src := filepath.Join(t.s.cfg.Payload.Root, filepath.FromSlash(rel))
	if _, err := os.Lstat(src); err != nil {
		return nil
	}
	for _, p := range t.idx.owned.sorted(rel) {
		if t.idx.published(rel, p) {
			return nil
		}
	}
	path, retry, err := t.choosePath(rel)
	if err != nil {
		t.fail(rel, "text sidecar naming: "+err.Error())
		return nil
	}
	occ, err := report.InspectTextSidecar(path, rel)
	if err != nil {
		return err
	}
	switch occ {
	case report.DescriptionOwned:
		return t.adopt(rel, path)
	case report.DescriptionForeign:
		t.fail(rel, "text sidecar output occupied: "+archiveRelOr(t.s.cfg.Archive, path))
		return nil
	}
	if retry {
		t.s.Log.Warn("text sidecar path occupied; using fallback",
			"rel_path", rel, "sidecar", archiveRelOr(t.s.cfg.Archive, path))
	}
	begin, err := t.w.BeginEvent(t.idx.family, rel, path, 0, false)
	if err != nil {
		return err
	}
	if err := t.ctx.Err(); err != nil {
		return err
	}
	info := extractCatia(t.ctx, src)
	if err := t.ctx.Err(); err != nil {
		return err
	}
	if info.TextFailed || info.Err != nil {
		reason := "CATIA text extraction failed"
		if info.ErrorKind != "" {
			reason += ": " + info.ErrorKind
		}
		return t.finishFailed(begin.TxID, rel, reason)
	}
	body := report.RenderCatiaText(report.CatiaTextInput{
		RelPath: rel, Archive: t.s.cfg.Archive, Identity: t.identity(rel), ExtractedAt: t.s.cfg.Now(), Info: info,
	})
	if err := publishSidecar(t.s.cfg.FS, t.s.cfg.Crash, path, body); err != nil {
		if err := t.ctx.Err(); err != nil {
			return err
		}
		if errors.Is(err, fs.ErrExist) {
			return t.finishFailed(begin.TxID, rel, "text sidecar output occupied: "+archiveRelOr(t.s.cfg.Archive, path))
		}
		return err
	}
	return t.done(rel, path, begin.TxID)
}

func (t *splitTexts) choosePath(rel string) (path string, fallback bool, err error) {
	for _, p := range t.idx.generating.sorted(rel) {
		return t.idx.abs(p), p != rel+".text.md", nil
	}
	srcAbs := filepath.Join(t.s.cfg.Archive, filepath.FromSlash(rel))
	path, err = report.ChooseTextSidecarPath(srcAbs, rel)
	return path, path != srcAbs+".text.md", err
}

func (t *splitTexts) adopt(rel, path string) error {
	if t.idx.published(rel, archiveRelOr(t.s.cfg.Archive, path)) {
		return nil
	}
	begin, err := t.w.BeginEvent(t.idx.family, rel, path, 0, false)
	if err != nil {
		return err
	}
	return t.done(rel, path, begin.TxID)
}

func (t *splitTexts) done(rel, path, txid string) error {
	size, ok := nonEmptyFileSize(path)
	if !ok {
		return errors.New("published text sidecar is missing: " + path)
	}
	if _, err := t.w.FinishEvent(t.idx.family, txid, t.idx.family.Done, "", size, ""); err != nil {
		return err
	}
	sidecar := archiveRelOr(t.s.cfg.Archive, path)
	t.idx.remember(rel, sidecar, size)
	t.idx.generating.remove(rel, sidecar)
	t.s.Stats.TextsDone.Add(1)
	t.s.Stats.ArchiveWritten.Add(size)
	t.s.Log.Info("text sidecar written", "rel_path", rel, "sidecar", sidecar, "bytes", size)
	return nil
}

func (t *splitTexts) finishFailed(txid, rel, reason string) error {
	if _, err := t.w.FinishEvent(t.idx.family, txid, t.idx.family.Failed, "", 0, reason); err != nil {
		return err
	}
	t.fail(rel, reason)
	return nil
}

func (t *splitTexts) fail(rel, reason string) {
	t.s.Issue(state.IssueFailed, rel, reason)
	t.s.Stats.TextsFailed.Add(1)
}

func publishSidecar(ops fsops.Ops, crash state.CrashHook, path string, data []byte) error {
	part := fsops.PartPath(path)
	if err := os.WriteFile(part, data, 0o644); err != nil {
		return err
	}
	f, err := os.Open(part)
	if err != nil {
		os.Remove(part)
		return err
	}
	syncErr := f.Sync()
	_ = f.Close()
	if syncErr != nil {
		os.Remove(part)
		return syncErr
	}
	if err := hitCrash(crash, "fs:text_part"); err != nil {
		return err
	}
	if err := ops.Rename(part, path); err != nil {
		_ = os.Remove(part)
		return err
	}
	return hitCrash(crash, "fs:text_sidecar")
}

func archiveRelOr(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func estimateTextBytes(v *catiaSplit) (int64, error) {
	need := map[string]int64{}
	add := func(rel string, size int64) {
		if !scanner.LocalRelPath(rel) {
			return
		}
		if _, ok := need[rel]; ok {
			return
		}
		for _, p := range v.idx.owned.sorted(rel) {
			if v.idx.published(rel, p) {
				return
			}
		}
		need[rel] = textNeed(size)
	}
	for _, row := range v.rows {
		if row.Status != report.StatusMoved {
			continue
		}
		fi, err := os.Lstat(filepath.Join(v.s.cfg.Payload.Root, filepath.FromSlash(row.RelPath)))
		if err != nil {
			continue
		}
		add(row.RelPath, fi.Size())
	}
	err := ReadCandidates(v.s.Run.File(state.CandidatesFile), func(c Candidate) error {
		add(c.RelPath, c.Size)
		return nil
	})
	if err != nil {
		return 0, err
	}
	var n int64
	for _, b := range need {
		n = addSat(n, b)
	}
	return n, nil
}

// textNeed is the preflight estimate of one sidecar: the extracted text is bounded by the file
// size, the header by textIdentityReserve, and the whole sidecar by the 1 MiB cap.
func textNeed(size int64) int64 {
	if size <= 0 {
		return 0
	}
	return min(addSat(size, textIdentityReserve), catiaTextNeedCap)
}
