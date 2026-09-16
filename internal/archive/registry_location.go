package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

// updateSplitLocations sets the location of the files this split run committed in the file
// registry its scan wrote, reusing the scan's rows without detection, and stamps the result. A scan
// right after the split then finds nothing to change.
func updateSplitLocations(s *Session, c ScanConfig, st *state.ScanStats, committed []string) error {
	if len(committed) == 0 {
		return nil
	}
	rows, err := report.LoadRegistry(c.Registry)
	if err != nil || rows == nil {
		s.Log.Warn("file registry unavailable; location of moved files not updated", "registry", c.Registry, "error", err)
		return nil
	}
	moved := make(map[string]struct{}, len(committed))
	for _, rel := range committed {
		moved[rel] = struct{}{}
	}
	loc, n := locationOf(s.payload.kind), 0
	for i := range rows {
		if _, ok := moved[rows[i].RelPath]; ok && rows[i].Location != loc {
			rows[i].Location = loc
			n++
		}
	}
	placed, err := rewriteRegistry(c.Registry, rows)
	if err != nil {
		return err
	}
	if err := writeRegistryStamp(s, c, st, placed); err != nil {
		return err
	}
	s.Log.Info("updated file registry location", "registry", c.Registry, "moved", n, "registry_outcome", placed.outcome)
	return nil
}

// updateRestoredLocations sets location archive on the file-registry rows of the files the payload
// history now records as restored, in the registry the stamp names while that file still matches
// its stamp. Restore never creates a file registry and adds or removes no row.
func updateRestoredLocations(s *Session) error {
	stamp, ok := state.ReadRegistryStamp(s.cfg.Archive)
	if !ok {
		s.Log.Info("no registry stamp; file registry location not updated")
		return nil
	}
	data, err := os.ReadFile(stamp.Registry)
	sum := sha256.Sum256(data)
	if err != nil || int64(len(data)) != stamp.Size || hex.EncodeToString(sum[:]) != stamp.SHA256 {
		s.Log.Warn("file registry does not match its stamp; location not updated", "registry", stamp.Registry, "error", err)
		return nil
	}
	rows, err := report.ReadRegistry(bytes.NewReader(data))
	if err != nil {
		s.Log.Warn("file registry cannot be parsed; location not updated", "registry", stamp.Registry, "error", err)
		return nil
	}
	history, err := readHistory(s.cfg.Archive, s.payload.kind)
	if err != nil {
		return err
	}
	_, restored := replayMoves(s.payload.kind, history)
	loc, n := locationOf(s.payload.kind), 0
	for i := range rows {
		if _, ok := restored[rows[i].RelPath]; ok && rows[i].Location == loc {
			rows[i].Location = report.LocationArchive
			n++
		}
	}
	if n == 0 {
		return nil
	}
	placed, err := rewriteRegistry(stamp.Registry, rows)
	if err != nil {
		return err
	}
	stamp.Size, stamp.SHA256 = placed.size, placed.sha256
	if err := state.WriteRegistryStamp(s.cfg.Archive, stamp); err != nil {
		return err
	}
	s.Log.Info("updated file registry location", "registry", stamp.Registry, "restored", n)
	return nil
}

// rewriteRegistry writes rows through the registry's part file and places it.
func rewriteRegistry(path string, rows []report.RegistryRow) (placedRegistry, error) {
	part := fsops.PartPath(path)
	w, err := report.CreateRegistry(part)
	if err != nil {
		return placedRegistry{}, err
	}
	for _, row := range rows {
		if err = w.Write(row); err != nil {
			break
		}
	}
	if err = errors.Join(err, w.Close()); err != nil {
		return placedRegistry{}, err
	}
	return placeRegistry(part, path)
}
