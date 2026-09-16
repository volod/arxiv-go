package archive

import (
	"path/filepath"

	"github.com/volod/arxiv-go/internal/report"
)

// movedDescriptions maps the rel_path of each CATIA file earlier runs moved to the archive-relative
// description path their described records name; the latest moved record wins.
func movedDescriptions(history []runHistory) map[string]string {
	hints := map[string]string{}
	for _, run := range history {
		split, _ := splitEvents(run)
		for rel, a := range split {
			if a.status == report.StatusMoved && a.description != "" {
				hints[rel] = a.description
			}
		}
	}
	return hints
}

// identity is the identity block a sidecar of rel repeats from the owned description. Without a
// readable owned description the block holds only file_name, and the run log says why.
func (t *splitTexts) identity(rel string) report.TextIdentity {
	path, err := t.descriptionPath(rel)
	if err != nil || path == "" {
		t.s.Log.Warn("text sidecar has no description identity: owned description not found",
			"rel_path", rel, "error", err)
		return report.TextIdentity{}
	}
	d, err := report.ReadDescriptionFile(path)
	if err != nil {
		t.s.Log.Warn("text sidecar has no description identity: description unreadable",
			"rel_path", rel, "description", archiveRelOr(t.s.cfg.Archive, path), "error", err)
		return report.TextIdentity{}
	}
	return report.TextIdentityOf(d, archiveRelOr(t.s.cfg.Archive, path))
}

// descriptionPath is the owned description of rel: the one the WAL recorded when it is still owned,
// otherwise the one the description naming rule finds next to the file.
func (t *splitTexts) descriptionPath(rel string) (string, error) {
	archive := t.s.cfg.Archive
	if hint, ok := t.hints[rel]; ok {
		p := filepath.Join(archive, filepath.FromSlash(hint))
		if occ, err := report.InspectDescription(p, rel); err == nil && occ == report.DescriptionOwned {
			return p, nil
		}
	}
	return report.FindDescriptionPath(filepath.Join(archive, filepath.FromSlash(rel)), rel)
}
