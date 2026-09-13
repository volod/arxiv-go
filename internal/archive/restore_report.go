package archive

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

func writeRestoreOutputs(s *Session, c RestoreConfig, w *state.WAL) error {
	if err := s.Phase(phaseReport, Totals{}); err != nil {
		return err
	}
	existing, err := loadVideoRegistry(s.cfg.Archive, s.cfg.VideoArchive)
	if err != nil {
		return err
	}
	if existing == nil {
		s.Log.Info("no video registry to update")
		return nil
	}
	rows := report.MarkRestored(existing, w.Committed().Paths(), s.Run.ID)
	var b strings.Builder
	if err := report.WriteVideoCSV(&b, rows); err != nil {
		return err
	}
	csvBuf := []byte(b.String())
	mdBuf, err := report.RenderSummary(report.SummaryInput{
		Generated:    s.cfg.Now().UTC(),
		Version:      s.cfg.Version,
		RunIDs:       report.UniqueRunIDs(rows),
		Archive:      s.cfg.Archive,
		VideoArchive: s.cfg.VideoArchive,
		Rows:         rows,
	})
	if err != nil {
		return err
	}
	retire := !c.KeepStubs && !report.HasMoved(rows)
	for _, root := range []string{s.cfg.Archive, s.cfg.VideoArchive} {
		if root == "" {
			continue
		}
		csvPath := filepath.Join(root, scanner.VideoRegistryName)
		mdPath := filepath.Join(root, scanner.VideoSummaryName)
		if err := s.cfg.FS.AtomicWriteFile(csvPath, csvBuf, 0o644); err != nil {
			return err
		}
		if err := s.cfg.FS.AtomicWriteFile(mdPath, mdBuf, 0o644); err != nil {
			return err
		}
		if !retire {
			continue
		}
		stamp := fmt.Sprintf("arxgo-videos.restored-%s", s.Run.ID)
		if err := s.cfg.FS.Replace(csvPath, filepath.Join(root, stamp+".csv")); err != nil {
			return err
		}
		if err := s.cfg.FS.Replace(mdPath, filepath.Join(root, stamp+".md")); err != nil {
			return err
		}
	}
	s.Log.Info("updated video registry", "videos", len(rows), "retired", retire)
	return nil
}
