package archive

import (
	"path/filepath"
	"strings"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
)

// writeRestoreOutputs marks restored rows in both registry copies by replaying every run's
// transactions, so restores of an earlier interrupted or --registry-update=false run count too.
// Restore never creates a registry.
func writeRestoreOutputs(s *Session, c RestoreConfig, existing []report.VideoRow) error {
	if err := s.Phase(phaseReport, Totals{}); err != nil {
		return err
	}
	if existing == nil {
		s.Log.Info("no video registry to update")
		return nil
	}
	rows, err := replayArchive(s, existing, nil)
	if err != nil {
		return err
	}
	for i := range rows {
		if !scanner.LocalRelPath(rows[i].RelPath) {
			continue
		}
		if rows[i].Status == report.StatusRestored && (rows[i].URL == "" || strings.HasPrefix(rows[i].URL, "file:")) {
			rows[i].URL = report.FileURL(filepath.ToSlash(filepath.Join(s.cfg.Archive, filepath.FromSlash(rows[i].RelPath))))
		}
	}
	var b strings.Builder
	if err := report.WriteVideoCSV(&b, rows); err != nil {
		return err
	}
	retire := !c.KeepDescriptions && !report.HasMoved(rows)
	if err := writeRestoredRegistry(s, scanner.VideoRegistryName, []byte(b.String()), retire); err != nil {
		return err
	}
	s.Log.Info("updated video registry", "videos", len(rows), "retired", retire)
	return nil
}

// writeRestoredRegistry atomically writes data as the payload registry name in both roots and,
// with retire, renames each copy to <stem>.restored-<run-id>.csv.
func writeRestoredRegistry(s *Session, name string, data []byte, retire bool) error {
	retired := strings.TrimSuffix(name, ".csv") + ".restored-" + s.Run.ID + ".csv"
	for _, csvPath := range registryPaths(s, name) {
		if err := s.cfg.FS.AtomicWriteFile(csvPath, data, 0o644); err != nil {
			return err
		}
		if !retire {
			continue
		}
		if err := s.cfg.FS.Replace(csvPath, filepath.Join(filepath.Dir(csvPath), retired)); err != nil {
			return err
		}
	}
	return nil
}
