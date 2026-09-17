package archive

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// CatiaIndexConfig configures the CATIA text index.
type CatiaIndexConfig struct {
	Archive string // absolute archive root
	Out     string // absolute output path
	Lock    state.LockOptions
}

// catiaIndex is the index content read from the archive before anything is written.
type catiaIndex struct {
	header   report.CatiaIndexHeader
	sections []report.CatiaIndexSection
	missing  []report.MissingText
}

// CatiaIndex writes one Markdown document of every CATIA file the CATIA run history records as
// moved, from its owned description and owned text sidecar. It reads only: no run directory, no
// lock and no write other than cfg.Out. A lock held by a live or remote run stops it, because that
// run is changing the history and the sidecars.
func CatiaIndex(ctx context.Context, cfg CatiaIndexConfig, log *slog.Logger) Status {
	locked, err := state.InspectLock(cfg.Archive, cfg.Lock)
	switch {
	case err != nil:
		log.Error("cannot read the run lock", "error", err)
		return StatusFailed
	case locked != nil && (locked.State == state.LockHeld || locked.State == state.LockRemote):
		return StartFailed(ctx, log, locked)
	case locked != nil:
		log.Warn("run lock left by an interrupted run; indexing the recorded history",
			"lock", locked.Path, "state", string(locked.State))
	}
	idx, err := readCatiaIndex(ctx, cfg, log)
	if err == nil {
		err = fsops.AtomicWrite(cfg.Out, 0o644, func(w io.Writer) error { return idx.write(ctx, w, cfg, log) })
	}
	switch {
	case err == nil:
	case ctx.Err() != nil:
		log.Warn("interrupted; CATIA text index not written", "out", cfg.Out)
		return StatusInterrupted
	case errors.Is(err, state.ErrStateCorrupt):
		return StartFailed(ctx, log, err)
	default:
		log.Error("CATIA text index not written", "out", cfg.Out, "error", err)
		return StatusFailed
	}
	h := idx.header
	if h.MissingText > 0 {
		log.Warn("CATIA files without text sidecar listed under Missing text", "missing_text", h.MissingText)
	}
	log.Info("wrote CATIA text index", "out", cfg.Out, "files", h.Files, "components", h.Components,
		"missing_text", h.MissingText)
	return StatusCompleted
}

func readCatiaIndex(ctx context.Context, cfg CatiaIndexConfig, log *slog.Logger) (*catiaIndex, error) {
	history, err := readHistory(cfg.Archive, PayloadCatia)
	if err != nil {
		return nil, err
	}
	texts, err := newTextIndex(cfg.Archive, history)
	if err != nil {
		return nil, err
	}
	idx := &catiaIndex{header: report.CatiaIndexHeader{Archive: cfg.Archive}}
	if len(history) == 0 {
		log.Warn("archive has no CATIA split history; the index lists no files", "archive", cfg.Archive)
	} else {
		idx.header.CatiaArchive = history[len(history)-1].mirror
		idx.header.HistoryAt = newestRecord(history)
	}
	var moved []report.CatiaRow
	for _, row := range replayCatiaRows(nil, history) {
		if row.Status == report.StatusMoved && scanner.LocalRelPath(row.RelPath) {
			moved = append(moved, row)
		}
	}
	slices.SortFunc(moved, func(a, b report.CatiaRow) int {
		return scanner.Compare(scanner.KeyOf(a.RelPath), scanner.KeyOf(b.RelPath))
	})
	for _, row := range moved {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s, missing := indexSection(cfg.Archive, row, texts, log)
		idx.sections = append(idx.sections, s)
		idx.header.Components += len(s.Components)
		if missing != "" {
			idx.missing = append(idx.missing, report.MissingText{RelPath: row.RelPath, Reason: missing})
		}
	}
	idx.header.Files, idx.header.MissingText = len(idx.sections), len(idx.missing)
	return idx, nil
}

// indexSection reads the section of one moved file without its notes, and the Missing text reason
// when it has no readable owned sidecar.
func indexSection(archive string, row report.CatiaRow, texts *eventIndex, log *slog.Logger) (report.CatiaIndexSection, string) {
	rel := row.RelPath
	s := report.CatiaIndexSection{RelPath: rel}
	described := false
	if p, err := ownedDescriptionPath(archive, row.DescriptionRelPath, rel); err == nil && p != "" {
		if d, err := report.ReadDescriptionFile(p); err == nil {
			s.Identity, described = report.TextIdentityOf(d, archiveRelOr(archive, p)), true
		}
	}
	missing := ""
	sidecar := texts.ownedPath(rel)
	if sidecar == "" {
		missing = report.MissingNotRecorded
	} else if text, reason := readIndexText(texts.abs(sidecar), rel, false); reason != "" {
		missing = reason
		if reason == report.MissingUnreadable {
			log.Warn("CATIA text sidecar unreadable", "rel_path", rel, "sidecar", sidecar)
		}
	} else {
		s.Text, s.Truncated, s.Properties, s.Components = sidecar, text.Fields["truncated"], text.Properties, text.Components
		if !described {
			s.Identity = report.TextIdentityOfSidecar(text)
		}
	}
	if !described {
		log.Warn("CATIA index: owned description not found", "rel_path", rel, "identity_from_sidecar", s.Text != "")
	}
	return s, missing
}

// readIndexText reads the owned sidecar of rel at path, or the Missing text reason it cannot be used.
func readIndexText(path, rel string, withNotes bool) (report.CatiaText, string) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return report.CatiaText{}, report.MissingAbsent
	case err != nil:
		return report.CatiaText{}, report.MissingUnreadable
	case !fi.Mode().IsRegular():
		return report.CatiaText{}, report.MissingForeign
	}
	text, err := report.ReadCatiaText(path, rel, withNotes)
	switch {
	case errors.Is(err, report.ErrNotDescription):
		return report.CatiaText{}, report.MissingForeign
	case err != nil:
		return report.CatiaText{}, report.MissingUnreadable
	}
	return text, ""
}

// write streams the document. Each sidecar is read again for its notes: block only, so memory holds
// identities and component lists, not notes.
func (idx *catiaIndex) write(ctx context.Context, w io.Writer, cfg CatiaIndexConfig, log *slog.Logger) error {
	if err := report.WriteCatiaIndexHeader(w, idx.header); err != nil {
		return err
	}
	for _, s := range idx.sections {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.Text != "" {
			text, reason := readIndexText(filepath.Join(cfg.Archive, filepath.FromSlash(s.Text)), s.RelPath, true)
			if reason != "" {
				log.Warn("CATIA text sidecar changed while indexing; notes omitted", "rel_path", s.RelPath, "reason", reason)
			}
			s.Notes = text.Notes
		}
		if err := report.WriteCatiaIndexSection(w, s); err != nil {
			return err
		}
	}
	return report.WriteMissingText(w, idx.missing)
}

// newestRecord is the time of the newest WAL record of the history, so the header names the state
// it reflects and a rerun over the same history writes the same bytes.
func newestRecord(history []runHistory) time.Time {
	var newest time.Time
	for _, run := range history {
		for _, rec := range run.records {
			if rec.TS.After(newest) {
				newest = rec.TS
			}
		}
	}
	return newest
}
