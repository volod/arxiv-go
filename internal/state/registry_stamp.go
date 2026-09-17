package state

import (
	"path/filepath"
	"slices"
	"time"
)

// RegistryStampVersion is the "v" field of registry.json.
const RegistryStampVersion = 1

// RegistryStampFile is the registry stamp in the archive's state directory.
const RegistryStampFile = "registry.json"

// RegistryStamp identifies the last file registry arxgo wrote or found unchanged for an archive; see
// docs/openspec/stage-1-core/contracts.md#registry-stamp.
type RegistryStamp struct {
	V        int    `json:"v"`
	Registry string `json:"registry"` // absolute registry path
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	RunID    string `json:"run_id"`
	Op       string `json:"op"`
	// ScanStartedAt is the start of the scan phase of the run's first process, to the second.
	ScanStartedAt time.Time      `json:"scan_started_at"`
	Detect        DetectSettings `json:"detect"`
}

// DetectSettings are the options that decide a registry row's detected values.
type DetectSettings struct {
	Version         string   `json:"version"`
	Metadata        string   `json:"metadata"`
	VideoExtensions []string `json:"video_extensions"` // effective list: lower case, sorted, unique, no dot
}

// Equal reports whether two runs detect the same values for the same file.
func (d DetectSettings) Equal(o DetectSettings) bool {
	return d.Version == o.Version && d.Metadata == o.Metadata && slices.Equal(d.VideoExtensions, o.VideoExtensions)
}

// RegistryStampPath returns <root>/.arxgo/registry.json.
func RegistryStampPath(root string) string {
	return filepath.Join(StateDir(root), RegistryStampFile)
}

// ReadRegistryStamp reads the stamp of root. ok is false when the stamp is missing, cannot be
// decoded or has another version; such a stamp is ignored and replaced by the next write.
func ReadRegistryStamp(root string) (RegistryStamp, bool) {
	var st RegistryStamp
	if err := ReadJSON(RegistryStampPath(root), &st); err != nil || st.V != RegistryStampVersion {
		return RegistryStamp{}, false
	}
	return st, true
}

// WriteRegistryStamp atomically writes the stamp of root.
func WriteRegistryStamp(root string, st RegistryStamp) error {
	st.V = RegistryStampVersion
	st.ScanStartedAt = st.ScanStartedAt.UTC().Truncate(time.Second)
	if st.Detect.VideoExtensions == nil {
		st.Detect.VideoExtensions = []string{}
	}
	return WriteJSON(RegistryStampPath(root), st)
}
