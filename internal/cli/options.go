package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/scanner"
)

// Enumerated option values from the CLI contract.
const (
	MetadataFile  = "file"
	MetadataMedia = "media"
	TransferAuto  = "auto"
	TransferCopy  = "copy"
	VerifySize    = "size"
	VerifyHash    = "hash"
	PolicyDelete  = "delete" // --descriptions and --previews
	PolicyKeep    = "keep"
	LogText       = "text"
	LogJSON       = "json"
)

// DefaultRegistryName is the registry file created in the archive root unless --registry is set.
const DefaultRegistryName = "arxgo-registry.csv"

// Common holds validated flags shared by every operation. Paths are absolute and cleaned but keep
// the spelling the operator gave; symlinks are resolved only for the nesting check.
type Common struct {
	Archive            string
	VideoArchive       string // empty for scan
	LogLevel           slog.Level
	LogFormat          string
	ProgressInterval   time.Duration
	CheckpointEvery    int
	CheckpointInterval time.Duration
	DryRun             bool
	MinFree            Size
	NewRun             bool
	ForceUnlock        bool
	// Tools are the external tools validated for this run; set after validation, never stored
	// in options.json because a tool path is not part of a run's definition.
	Tools media.Toolset `json:"-"`
}

// ScanSettings holds validated scan flags, used by scan and by split for its scan phase.
type ScanSettings struct {
	LargeThreshold Size
	Registry       string // absolute path
	Metadata       string // MetadataFile or MetadataMedia
	Exclude        []string
	// VideoExtensions are extra extensions (lower case, with a leading dot) that mark a file with
	// an ambiguous signature as video.
	VideoExtensions []string
}

// ScanOptions configures the scan operation.
type ScanOptions struct {
	Common
	ScanSettings
}

// SplitOptions configures the split operation.
type SplitOptions struct {
	Common
	ScanSettings
	Transfer string // TransferAuto or TransferCopy
	Verify   string // VerifySize or VerifyHash
	BaseURL  string // empty, or absolute http(s) URL without a trailing slash
	Preview  media.PreviewOptions
	// CreateVideoArchive is true when the video archive root does not exist yet; its parent does,
	// and the split operation creates it after taking the lock.
	CreateVideoArchive bool
}

// RestoreOptions configures the restore operation.
type RestoreOptions struct {
	Common
	Transfer       string
	Verify         string
	Descriptions   string // PolicyDelete or PolicyKeep
	CreateDirs     bool
	Overwrite      bool
	RegistryUpdate bool
	Previews       string // keep or delete
}

// validator accumulates validation errors so the operator sees all of them at once.
type validator struct{ errs []error }

func (v *validator) addf(format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf(format, args...))
}

func (v *validator) err() error { return errors.Join(v.errs...) }

func buildCommon(op string, s *settings, fsys rootFS, v *validator) (Common, bool) {
	c := Common{
		LogLevel:           parseLevel(s.logLevel),
		LogFormat:          s.logFormat,
		ProgressInterval:   s.progressInterval,
		CheckpointEvery:    s.checkpointEvery,
		CheckpointInterval: s.checkpointInterval,
		DryRun:             s.dryRun,
		MinFree:            s.minFree,
		NewRun:             s.newRun,
		ForceUnlock:        s.forceUnlock,
	}
	if c.ProgressInterval <= 0 {
		v.addf("--progress-interval must be positive, got %s", c.ProgressInterval)
	}
	if c.CheckpointInterval <= 0 {
		v.addf("--checkpoint-interval must be positive, got %s", c.CheckpointInterval)
	}
	if c.CheckpointEvery < 1 {
		v.addf("--checkpoint-every must be at least 1, got %d", c.CheckpointEvery)
	}
	if c.MinFree < 0 {
		v.addf("--min-free must not be negative")
	}
	var videoMissing bool
	c.Archive, c.VideoArchive, videoMissing = checkRoots(op, s, fsys, v)
	return c, videoMissing
}

func buildScan(s *settings, archive string, fsys rootFS, v *validator) ScanSettings {
	sc := ScanSettings{LargeThreshold: s.largeThreshold, Metadata: s.metadata, Exclude: s.exclude}
	if sc.LargeThreshold <= 0 {
		v.addf("--large-threshold must be positive")
	}
	if s.followSymlinks {
		v.addf("%s: option not available in this build", s.explicit["follow-symlinks"])
	}
	for _, g := range sc.Exclude {
		if err := validateGlob(g, runtime.GOOS == "windows"); err != nil {
			v.addf("--exclude %q: %v", g, err)
		}
	}
	exts, err := parseExtensions(s.videoExtensions)
	if err != nil {
		v.addf("--video-extensions %q: %v", s.videoExtensions, err)
	}
	for _, ext := range exts {
		if scanner.IsCatiaExtension(ext) {
			v.addf("--video-extensions %q: CATIA extension %s cannot mark a file as video", s.videoExtensions, ext)
		}
	}
	sc.VideoExtensions = exts
	sc.Registry = s.registry
	if sc.Registry == "" && archive != "" {
		sc.Registry = filepath.Join(archive, DefaultRegistryName)
	} else if sc.Registry != "" {
		abs, err := fsys.abs(sc.Registry)
		if err != nil {
			v.addf("--registry %q: %v", sc.Registry, err)
			return sc
		}
		sc.Registry = abs
		if fi, err := fsys.stat(abs); err == nil && fi.IsDir() {
			v.addf("--registry %q is a directory", abs)
		}
		if fi, err := fsys.stat(filepath.Dir(abs)); err != nil || !fi.IsDir() {
			v.addf("--registry %q: parent directory does not exist", abs)
		}
	}
	return sc
}

func buildScanOptions(s *settings, fsys rootFS) (ScanOptions, error) {
	v := &validator{}
	common, _ := buildCommon(OpScan, s, fsys, v)
	o := ScanOptions{Common: common}
	o.ScanSettings = buildScan(s, o.Archive, fsys, v)
	return o, v.err()
}

func buildSplitOptions(s *settings, fsys rootFS) (SplitOptions, error) {
	v := &validator{}
	common, videoMissing := buildCommon(OpSplit, s, fsys, v)
	o := SplitOptions{Common: common, CreateVideoArchive: videoMissing}
	o.ScanSettings = buildScan(s, o.Archive, fsys, v)
	o.Transfer, o.Verify = s.transfer, s.verify
	o.Preview = media.PreviewOptions{SampleMode: s.sampleMode, ImageMode: s.imageMode,
		SampleDuration: s.sampleDuration, SampleEvery: s.sampleEvery, ImageEvery: s.imageEvery,
		SampleResolution: s.sampleResolution, ImageResolution: s.imageResolution,
		SampleQuality: s.sampleQuality, ImageQuality: s.imageQuality, MaxItems: s.previewMaxItems}
	if o.Preview.SampleDuration <= 0 {
		v.addf("--sample-duration must be positive")
	}
	if o.Preview.SampleEvery <= 0 {
		v.addf("--sample-every must be positive")
	}
	if o.Preview.ImageEvery <= 0 {
		v.addf("--image-every must be positive")
	}
	if o.Preview.MaxItems < 1 {
		v.addf("--preview-max-items must be at least 1")
	}
	if s.baseURL != "" {
		u, err := validateBaseURL(s.baseURL)
		if err != nil {
			v.addf("--base-url %q: %v", s.baseURL, err)
		}
		o.BaseURL = u
	}
	return o, v.err()
}

func buildRestoreOptions(s *settings, fsys rootFS) (RestoreOptions, error) {
	v := &validator{}
	common, _ := buildCommon(OpRestore, s, fsys, v)
	o := RestoreOptions{
		Common:         common,
		Transfer:       s.transfer,
		Verify:         s.verify,
		Descriptions:   s.descriptions,
		CreateDirs:     s.createDirs,
		Overwrite:      s.overwrite,
		RegistryUpdate: s.registryUpdate,
		Previews:       s.previews,
	}
	return o, v.err()
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	_ = l.UnmarshalText([]byte(s)) // the enum flag already restricted the value
	return l
}

// validateBaseURL requires an absolute http or https URL with a host and no credentials, query
// or fragment, because descriptions append /<rel_path> to it. It returns the URL without trailing slashes.
func validateBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("not a valid URL")
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return "", errors.New("scheme must be http or https")
	case u.Host == "" || u.Hostname() == "":
		return "", errors.New("host is required")
	case u.User != nil:
		return "", errors.New("credentials are not allowed in the URL")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return "", errors.New("query and fragment are not allowed")
	}
	return strings.TrimRight(raw, "/"), nil
}

// parseExtensions normalizes a comma-separated extension list to lower-case ".ext" items.
func parseExtensions(list string) ([]string, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	var out []string
	seen := map[string]bool{}
	for _, item := range strings.Split(list, ",") {
		ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(item), "."))
		if ext == "" {
			return nil, errors.New("empty extension")
		}
		for _, r := range ext {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				return nil, fmt.Errorf("extension %q may contain only letters, digits, '_' and '-'", item)
			}
		}
		if !seen[ext] {
			seen[ext] = true
			out = append(out, "."+ext)
		}
	}
	return out, nil
}

// validateGlob checks a relative slash-path glob with the walker's grammar: path.Match syntax per
// segment, "**" for any depth, no absolute or parent-directory patterns. On Windows a backslash is
// rejected: it would be a path.Match escape, not the separator the operator probably meant.
func validateGlob(g string, windows bool) error {
	if strings.HasPrefix(g, "/") || filepath.IsAbs(g) || filepath.VolumeName(g) != "" {
		return errors.New("must be relative to the archive root")
	}
	if windows && strings.Contains(g, `\`) {
		return errors.New(`use "/" between segments; "\" is a glob escape (match a literal "[" with "[[]")`)
	}
	_, err := scanner.CompileGlob(g)
	return err
}
