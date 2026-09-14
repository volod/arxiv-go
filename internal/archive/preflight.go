package archive

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/volod/arxiv-go/internal/fsops"
)

// ErrInsufficientSpace is returned when preflight finds a write device without enough free space.
// The cli package maps it to exit code 4.
var ErrInsufficientSpace = errors.New("insufficient free space")

// Role names a path that preflight places on a device.
type Role string

// Preflight roles.
const (
	RoleArchive      Role = "archive"
	RoleVideoArchive Role = "video_archive"
	RoleRegistry     Role = "registry"
)

// Estimates from the preflight table in the integrity specification.
const (
	registryRowBytes      = 256     // scan: per registry row
	mediaMetadataBytes    = 512     // scan: per media row in media mode
	stubBytes             = 4 << 10 // split: per Markdown stub
	videoRegistryRowBytes = 1 << 10 // split: per video, per registry copy
	walBytes              = 2 << 10 // split and restore: WAL records per candidate
)

// Operation names and option values preflight distinguishes. They equal the cli values.
const (
	opScan        = "scan"
	opSplit       = "split"
	opRestore     = "restore"
	transferCopy  = "copy"
	metadataMedia = "media"
)

// Candidates summarizes the work left for the run: for split and restore the remaining video
// candidates, for scan the registry rows to write. Resume passes the remaining candidates only.
type Candidates struct {
	Count     int64 // candidates (scan: registry rows)
	Bytes     int64 // sum of candidate sizes
	Largest   int64 // largest candidate size
	MediaRows int64 // scan in media mode: rows that carry media metadata
	// PreviewBytes is the estimate of the previews split still has to write to the archive device.
	PreviewBytes int64
}

// PreflightOptions are the run options that change the requirement.
type PreflightOptions struct {
	Op       string
	Transfer string // auto or copy; empty means auto
	Metadata string // scan: file or media
	MinFree  int64  // bytes that must remain free on every write device
}

// Device is one filesystem volume with the roles placed on it and its free space.
type Device struct {
	Roles []Role
	Path  string // first role's path; a diagnostic label
	Space fsops.Space
}

// DeviceInfo lists the distinct devices of a run's roots. A role appears on at most one device.
type DeviceInfo struct {
	Devices []Device
}

func (d DeviceInfo) index(r Role) int {
	for i, dev := range d.Devices {
		for _, got := range dev.Roles {
			if got == r {
				return i
			}
		}
	}
	return -1
}

// Need is one estimated component of a device requirement.
type Need struct {
	Name  string
	Bytes int64
}

// DeviceRequirement is the computed requirement for one write device.
type DeviceRequirement struct {
	Roles     []Role
	Path      string
	Needs     []Need
	Required  int64 // sum of Needs
	MinFree   int64
	Available int64 // caller-available bytes, capped at MaxInt64
	Known     bool  // false when the filesystem reports no total size (network share)
	Shortfall int64 // Required + MinFree - Available when positive and Known
}

// Requirement is the preflight result for every write device of a run.
type Requirement struct {
	Op      string
	Devices []DeviceRequirement
}

// Sufficient reports whether every device with known free space can hold the requirement plus
// --min-free.
func (r Requirement) Sufficient() bool {
	for _, d := range r.Devices {
		if d.Shortfall > 0 {
			return false
		}
	}
	return true
}

// Shortfall is the summed shortfall of all devices.
func (r Requirement) Shortfall() int64 {
	var n int64
	for _, d := range r.Devices {
		n = addSat(n, d.Shortfall)
	}
	return n
}

// Plan computes the free space each write device needs for the remaining work. It is pure: device
// grouping and free space come from info. Roles missing from info are ignored.
func Plan(c Candidates, o PreflightOptions, info DeviceInfo) Requirement {
	req := Requirement{Op: o.Op}
	needs := make([][]Need, len(info.Devices))
	written := make([]bool, len(info.Devices))
	add := func(role Role, name string, bytes int64) {
		i := info.index(role)
		if i < 0 {
			return
		}
		written[i] = true
		if bytes > 0 {
			needs[i] = append(needs[i], Need{name, bytes})
		}
	}
	count, largest := nonNeg(c.Count), nonNeg(c.Largest)
	shared := info.index(RoleArchive) >= 0 && info.index(RoleArchive) == info.index(RoleVideoArchive)
	copying := o.Transfer == transferCopy

	switch o.Op {
	case opScan:
		role := RoleRegistry
		if info.index(role) < 0 {
			role = RoleArchive
		}
		add(role, "registry", mulSat(count, registryRowBytes))
		if o.Metadata == metadataMedia {
			add(role, "media_metadata", mulSat(nonNeg(c.MediaRows), mediaMetadataBytes))
		}
	case opSplit:
		add(RoleArchive, "stubs", mulSat(count, stubBytes))
		add(RoleArchive, "video_registry", mulSat(count, videoRegistryRowBytes))
		add(RoleVideoArchive, "video_registry", mulSat(count, videoRegistryRowBytes))
		add(RoleArchive, "wal", mulSat(count, walBytes))
		add(RoleArchive, "previews", nonNeg(c.PreviewBytes))
		switch {
		case !shared:
			add(RoleVideoArchive, "videos", nonNeg(c.Bytes))
		case copying:
			// Sources are removed one by one after their copy commits.
			add(RoleVideoArchive, "largest_video", largest)
		}
	case opRestore:
		add(RoleArchive, "wal", mulSat(count, walBytes))
		if !shared || copying {
			add(RoleArchive, "videos", nonNeg(c.Bytes))
		}
	}

	for i, dev := range info.Devices {
		if !written[i] {
			continue
		}
		d := DeviceRequirement{
			Roles: dev.Roles, Path: dev.Path, Needs: mergeNeeds(needs[i]), MinFree: nonNeg(o.MinFree),
			Available: capInt64(dev.Space.Available), Known: dev.Space.Total > 0,
		}
		for _, n := range d.Needs {
			d.Required = addSat(d.Required, n.Bytes)
		}
		if d.Known {
			d.Shortfall = nonNeg(addSat(d.Required, d.MinFree) - d.Available)
		}
		req.Devices = append(req.Devices, d)
	}
	return req
}

// mergeNeeds sums components with the same name, keeping first-seen order (two registry copies on
// a shared device become one entry).
func mergeNeeds(in []Need) []Need {
	var out []Need
next:
	for _, n := range in {
		for i := range out {
			if out[i].Name == n.Name {
				out[i].Bytes = addSat(out[i].Bytes, n.Bytes)
				continue next
			}
		}
		out = append(out, n)
	}
	return out
}

// Attrs returns the slog attributes of one device line in the preflight report. Byte counts are
// given both exactly and in binary units so the report is readable and machine-checkable.
func (d DeviceRequirement) Attrs() []any {
	roles := make([]string, len(d.Roles))
	for i, r := range d.Roles {
		roles[i] = string(r)
	}
	needs := make([]string, len(d.Needs))
	for i, n := range d.Needs {
		needs[i] = n.Name + "=" + FormatBytes(n.Bytes)
	}
	attrs := []any{"roles", strings.Join(roles, ","), "path", d.Path,
		"required", FormatBytes(d.Required), "min_free", FormatBytes(d.MinFree)}
	if d.Known {
		attrs = append(attrs, "available", FormatBytes(d.Available), "shortfall", FormatBytes(d.Shortfall))
	} else {
		attrs = append(attrs, "available", "unknown")
	}
	attrs = append(attrs, "needs", strings.Join(needs, " "),
		"required_bytes", d.Required, "min_free_bytes", d.MinFree)
	if d.Known {
		attrs = append(attrs, "available_bytes", d.Available, "shortfall_bytes", d.Shortfall)
	}
	return attrs
}

// InsufficientSpaceError carries the failing requirement; it matches ErrInsufficientSpace.
type InsufficientSpaceError struct {
	Requirement Requirement
}

func (e *InsufficientSpaceError) Error() string {
	var parts []string
	for _, d := range e.Requirement.Devices {
		if d.Shortfall > 0 {
			parts = append(parts, fmt.Sprintf("%s short by %s", d.Path, FormatBytes(d.Shortfall)))
		}
	}
	return ErrInsufficientSpace.Error() + ": " + strings.Join(parts, "; ")
}

// Is reports whether target is ErrInsufficientSpace.
func (e *InsufficientSpaceError) Is(target error) bool { return target == ErrInsufficientSpace }

func nonNeg(n int64) int64 { return max(n, 0) }

func capInt64(n uint64) int64 {
	if n > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(n)
}

func addSat(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

func mulSat(n, unit int64) int64 {
	if n > 0 && n > math.MaxInt64/unit {
		return math.MaxInt64
	}
	return n * unit
}
