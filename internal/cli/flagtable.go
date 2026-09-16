package cli

import (
	"flag"
	"os"
	"strings"
	"time"
)

// flagTable is the single source of the operator flag contract in
// docs/openspec/stage-1-core/cli.md. Help output follows table order.
var flagTable = []flagDef{
	// Common flags.
	{name: "archive", ops: allOps, group: groupCommon, arg: "PATH", usage: "Root of the main archive (required)", bind: str("")},
	{name: "video-archive", ops: allOps, group: groupCommon, arg: "PATH", usage: "Root of the video archive (required for video split and restore; unused by scan)", bind: str("")},
	{name: "catia-archive", ops: allOps, group: groupCommon, arg: "PATH", usage: "Root of the CATIA archive (required for split and restore --catia; unused by scan)", bind: str("")},
	{name: "log-level", ops: allOps, group: groupCommon, arg: "LEVEL", usage: "Console log level: debug, info, warn, error", bind: enum("info", logLevels)},
	{name: "log-format", ops: allOps, group: groupCommon, arg: "FORMAT", usage: "Console log format: text or json", bind: enum("text", logFormats)},
	{name: "progress-interval", ops: allOps, group: groupCommon, arg: "DURATION", usage: "Minimum interval between progress lines", bind: duration(defaultProgress, func(s *settings) *time.Duration { return &s.progressInterval })},
	{name: "checkpoint-every", ops: allOps, group: groupCommon, arg: "N", usage: "Checkpoint after this many processed files", bind: func(fs *flag.FlagSet, s *settings, name, usage string) {
		fs.IntVar(&s.checkpointEvery, name, defaultEvery, usage)
	}},
	{name: "checkpoint-interval", ops: allOps, group: groupCommon, arg: "DURATION", usage: "... or after this much time, whichever comes first", bind: duration(defaultCkptTime, func(s *settings) *time.Duration { return &s.checkpointInterval })},
	{name: "dry-run", ops: allOps, group: groupCommon, usage: "Validate, scan and preflight; print the plan; mutate nothing except the run log", bind: boolean(false)},
	{name: "min-free", ops: allOps, group: groupCommon, arg: "SIZE", usage: "Free space that must remain on every written device", bind: size(defaultMinFree, func(s *settings) *Size { return &s.minFree })},
	{name: "new-run", ops: allOps, group: groupCommon, usage: "Recover an incomplete previous run, then start a fresh scan", bind: boolean(false)},
	{name: "force-unlock", ops: allOps, group: groupCommon, usage: "Take over a lock whose owner process is not alive", bind: boolean(false)},

	// Scan flags, shared by split for its scan phase.
	{name: "large-threshold", ops: scanSplitOps, group: groupScan, arg: "SIZE", usage: "Files at least this large get is_large=true", bind: size(defaultLarge, func(s *settings) *Size { return &s.largeThreshold })},
	{name: "registry", ops: scanSplitOps, group: groupScan, arg: "PATH", usage: "CSV registry output path (default <archive>/arxgo-registry.csv)", bind: str("")},
	{name: "metadata", ops: scanSplitOps, group: groupScan, arg: "MODE", usage: "file: filesystem plus ISO BMFF media fields; media: also ffprobe for other audio and video", bind: enum(MetadataFile, metadataModes)},
	{name: "video-extensions", ops: scanSplitOps, group: groupScan, arg: "LIST", usage: "Extra comma-separated video extensions used when signature detection is inconclusive (not a CATIA extension)", bind: str("")},
	{name: "exclude", ops: scanSplitOps, group: groupScan, arg: "GLOB", usage: "Relative-path glob excluded from traversal (repeatable; ** for any depth)", bind: func(fs *flag.FlagSet, s *settings, name, usage string) {
		fs.Var(&listValue{target: &s.exclude}, name, usage)
	}},
	{name: "redetect", ops: scanSplitOps, group: groupScan, usage: "Detect every present file instead of reusing unchanged rows of the previous registry", bind: boolean(false)},
	{name: "follow-symlinks", ops: scanSplitOps, group: groupScan, usage: "Reserved; symlinks are recorded but never followed by scan", bind: boolean(false)},

	// Split flags.
	{name: "video", ops: splitOnly, group: groupSplit, usage: "Move video files into --video-archive (the default payload)", bind: boolean(false)},
	{name: "catia", ops: splitOnly, group: groupSplit, usage: "Move CATIA files into --catia-archive instead of videos", bind: boolean(false)},
	{name: "catia-text", ops: splitOnly, group: groupSplit, usage: "With --catia, write a searchable text sidecar of extracted accessible text for moved CATIA files that lack one", bind: boolean(false)},
	{name: "transfer", ops: splitOnly, group: groupSplit, arg: "MODE", usage: "auto: rename on the same device, copy+verify+delete otherwise; copy: always copy+verify+delete", bind: enum(TransferAuto, transferModes)},
	{name: "verify", ops: splitOnly, group: groupSplit, arg: "MODE", usage: "size or hash (SHA-256 while copying and re-read from the destination)", bind: enum(VerifySize, verifyModes)},
	{name: "base-url", ops: splitOnly, group: groupSplit, arg: "URL", usage: "Absolute http(s) URL of the published video or CATIA archive; descriptions link to URL/<rel_path>", bind: str("")},
	{name: "sample", ops: splitOnly, group: groupSplit, arg: "MODE", usage: "Sample clips: none, start, middle, end, series", bind: enum("none", previewModes)},
	{name: "sample-duration", ops: splitOnly, group: groupSplit, arg: "DURATION", usage: "Clip length, or fragment length for series", bind: duration(5*time.Second, func(s *settings) *time.Duration { return &s.sampleDuration })},
	{name: "sample-every", ops: splitOnly, group: groupSplit, arg: "DURATION", usage: "Fragment spacing for series", bind: duration(5*time.Minute, func(s *settings) *time.Duration { return &s.sampleEvery })},
	{name: "sample-resolution", ops: splitOnly, group: groupSplit, arg: "RES", usage: "sd, hd or 4k upper bound", bind: enum("sd", previewSizes)},
	{name: "sample-quality", ops: splitOnly, group: groupSplit, arg: "Q", usage: "low, medium, high", bind: enum("medium", previewQuality)},
	{name: "image", ops: splitOnly, group: groupSplit, arg: "MODE", usage: "Frame images: none, start, middle, end, series", bind: enum("none", previewModes)},
	{name: "image-every", ops: splitOnly, group: groupSplit, arg: "DURATION", usage: "Frame spacing for series", bind: duration(5*time.Minute, func(s *settings) *time.Duration { return &s.imageEvery })},
	{name: "image-resolution", ops: splitOnly, group: groupSplit, arg: "RES", usage: "sd, hd or 4k upper bound", bind: enum("sd", previewSizes)},
	{name: "image-quality", ops: splitOnly, group: groupSplit, arg: "Q", usage: "PNG compression: low, medium, high", bind: enum("medium", previewQuality)},
	{name: "preview-max-items", ops: splitOnly, group: groupSplit, arg: "N", usage: "Cap on fragments per series clip and frames per series", bind: func(fs *flag.FlagSet, s *settings, name, usage string) {
		fs.IntVar(&s.previewMaxItems, name, 100, usage)
	}},
	{name: "publish", ops: splitOnly, group: groupSplit, plannedFeature: "cloud publishing", arg: "TARGET", usage: "Upload target: gdrive or sharepoint", bind: reserved(false)},
	{name: "publish-delete-local", ops: splitOnly, group: groupSplit, plannedFeature: "cloud publishing", usage: "Remove the local video archive copy after a verified upload", bind: reserved(true)},
	{name: "gdrive-drive-id", ops: splitOnly, group: groupSplit, plannedFeature: "cloud publishing", arg: "ID", usage: "Google Shared Drive id", bind: reserved(false)},
	{name: "gdrive-credentials", ops: splitOnly, group: groupSplit, plannedFeature: "cloud publishing", arg: "FILE", usage: "Google service account JSON key", bind: reserved(false)},
	{name: "share", ops: splitOnly, group: groupSplit, plannedFeature: "cloud publishing", arg: "SCOPE", usage: "Sharing link scope for published files", bind: reserved(false)},

	// Restore flags.
	{name: "video", ops: restoreOnly, group: groupRestore, usage: "Return video files from --video-archive (the default payload)", bind: boolean(false)},
	{name: "catia", ops: restoreOnly, group: groupRestore, usage: "Return CATIA files from --catia-archive instead of videos", bind: boolean(false)},
	{name: "transfer", ops: restoreOnly, group: groupRestore, arg: "MODE", usage: "auto: rename on the same device, copy then delete otherwise; copy: copy and keep the video or CATIA archive copy", bind: enum(TransferAuto, transferModes)},
	{name: "verify", ops: restoreOnly, group: groupRestore, arg: "MODE", usage: "size or hash, as for split", bind: enum(VerifySize, verifyModes)},
	{name: "descriptions", ops: restoreOnly, group: groupRestore, arg: "POLICY", usage: "delete or keep the descriptions at restored locations (with --catia also owned text sidecars)", bind: enum(PolicyDelete, policies)},
	{name: "previews", ops: restoreOnly, group: groupRestore, arg: "POLICY", usage: "delete or keep preview files of restored videos (delete is not valid with --catia)", bind: enum(PolicyKeep, policies)},
	{name: "create-dirs", ops: restoreOnly, group: groupRestore, usage: "Recreate a missing parent directory instead of skipping the file", bind: boolean(false)},
	{name: "overwrite", ops: restoreOnly, group: groupRestore, usage: "Replace an existing, different destination file instead of skipping it", bind: boolean(false)},
	{name: "registry-update", ops: restoreOnly, group: groupRestore, usage: "Mark restored rows in arxgo-videos.csv, or arxgo-catia.csv with --catia", bind: boolean(true)},
}

// splitList splits an environment value for a repeatable flag on the platform path list
// separator (":" on Linux, ";" on Windows), dropping empty items.
func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, string(os.PathListSeparator)) {
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
