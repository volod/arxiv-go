package archive

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/volod/arxiv-go/internal/fsops"
)

// RolePath is a root or output file that preflight places on a device.
type RolePath struct {
	Role Role
	Path string
}

// ProbeDevices groups paths by device with ops.SameDevice and reads each device's free space once.
// Missing paths (a mirror root that split will create) resolve to their nearest existing
// ancestor inside fsops. Empty paths are skipped.
func ProbeDevices(ops fsops.Ops, paths []RolePath) (DeviceInfo, error) {
	var info DeviceInfo
next:
	for _, p := range paths {
		if p.Path == "" {
			continue
		}
		for i := range info.Devices {
			d := &info.Devices[i]
			same, err := ops.SameDevice(d.Path, p.Path)
			if err != nil {
				return DeviceInfo{}, fmt.Errorf("preflight: compare devices of %s and %s: %w", d.Path, p.Path, err)
			}
			if same {
				d.Roles = append(d.Roles, p.Role)
				continue next
			}
		}
		space, err := ops.FreeSpace(p.Path)
		if err != nil {
			return DeviceInfo{}, fmt.Errorf("preflight: free space of %s: %w", p.Path, err)
		}
		info.Devices = append(info.Devices, Device{Roles: []Role{p.Role}, Path: p.Path, Space: space})
	}
	return info, nil
}

// LogRequirement prints the preflight computation: one line per write device with required,
// available and shortfall, a warning for devices whose free space is unknown, and a summary.
func LogRequirement(log *slog.Logger, req Requirement) {
	for _, d := range req.Devices {
		log.Info("preflight device", d.Attrs()...)
		if !d.Known {
			log.Warn("free space unknown (filesystem reports no total size); continuing",
				"path", d.Path, "required", FormatBytes(d.Required))
		}
	}
	if req.Sufficient() {
		log.Info("preflight passed", "op", req.Op, "devices", len(req.Devices))
		return
	}
	log.Error("preflight failed: insufficient free space", "op", req.Op, "devices", len(req.Devices),
		"shortfall", FormatBytes(req.Shortfall()), "shortfall_bytes", req.Shortfall())
}

// Preflight starts the preflight phase, computes the requirement for the remaining candidates on
// the run's roots, prints it and returns an *InsufficientSpaceError (exit 4) when a device cannot
// hold it plus --min-free. It reads only device and free-space information and mutates nothing
// outside the run directory, so it runs before the first archive mutation and also in --dry-run.
func (s *Session) Preflight(ctx context.Context, c Candidates) (Requirement, error) {
	if err := ctx.Err(); err != nil {
		return Requirement{}, err
	}
	o := s.cfg.Preflight
	o.Op = s.cfg.Op
	totals := Totals{Items: c.Count, Bytes: c.Bytes}
	if o.Op == opScan {
		totals = Totals{} // registry rows are not handled items; report entries like the scan
	}
	if err := s.Phase("preflight", totals); err != nil {
		return Requirement{}, err
	}
	paths := []RolePath{{RoleArchive, s.cfg.Archive}}
	if s.payload != nil {
		o.Payload = s.payload.kind
		paths = append(paths, RolePath{s.payload.role, s.cfg.Payload.Root})
	}
	if o.Op == opScan {
		paths = append(paths, RolePath{RoleRegistry, s.cfg.Registry})
	}
	info, err := ProbeDevices(s.cfg.FS, paths)
	if err != nil {
		return Requirement{}, err
	}
	req := Plan(c, o, info)
	LogRequirement(s.Log, req)
	if !req.Sufficient() {
		return req, &InsufficientSpaceError{Requirement: req}
	}
	return req, nil
}
