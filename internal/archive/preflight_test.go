package archive

import (
	"bytes"
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
)

const (
	kib = int64(1) << 10
	gib = int64(1) << 30
)

func dev(avail int64, path string, roles ...Role) Device {
	return Device{Roles: roles, Path: path, Space: fsops.Space{Total: 1 << 50, Free: uint64(avail), Available: uint64(avail)}}
}

func devices(ds ...Device) DeviceInfo { return DeviceInfo{Devices: ds} }

var (
	shared   = devices(dev(100*gib, "/a", RoleArchive, RoleVideoArchive))
	separate = devices(dev(100*gib, "/a", RoleArchive), dev(100*gib, "/v", RoleVideoArchive))
	videos3  = Candidates{Count: 3, Bytes: 30 * gib, Largest: 20 * gib}
)

type wantDevice struct {
	path  string
	needs []Need
}

func TestPlanPreflightTable(t *testing.T) {
	tests := []struct {
		name string
		c    Candidates
		o    PreflightOptions
		info DeviceInfo
		want []wantDevice
	}{
		{"scan file mode on archive device", Candidates{Count: 1000, MediaRows: 400}, PreflightOptions{Op: "scan", Metadata: "file"},
			devices(dev(gib, "/a", RoleArchive)),
			[]wantDevice{{"/a", []Need{{"registry", 256000}}}}},
		{"scan media mode adds metadata estimate", Candidates{Count: 1000, MediaRows: 400}, PreflightOptions{Op: "scan", Metadata: "media"},
			devices(dev(gib, "/a", RoleArchive, RoleRegistry)),
			[]wantDevice{{"/a", []Need{{"registry", 256000}, {"media_metadata", 204800}}}}},
		{"scan registry on another device", Candidates{Count: 10}, PreflightOptions{Op: "scan"},
			devices(dev(gib, "/a", RoleArchive), dev(gib, "/r", RoleRegistry)),
			[]wantDevice{{"/r", []Need{{"registry", 2560}}}}},
		{"split same device auto renames", videos3, PreflightOptions{Op: "split", Payload: PayloadVideo, Transfer: "auto"}, shared,
			[]wantDevice{{"/a", []Need{{"descriptions", 12 * kib}, {"video_registry", 6 * kib}, {"wal", 6 * kib}}}}},
		{"split other device auto copies all", videos3, PreflightOptions{Op: "split", Payload: PayloadVideo, Transfer: "auto"}, separate,
			[]wantDevice{
				{"/a", []Need{{"descriptions", 12 * kib}, {"video_registry", 3 * kib}, {"wal", 6 * kib}}},
				{"/v", []Need{{"video_registry", 3 * kib}, {"videos", 30 * gib}}}}},
		{"split other device copy", videos3, PreflightOptions{Op: "split", Payload: PayloadVideo, Transfer: "copy"}, separate,
			[]wantDevice{
				{"/a", []Need{{"descriptions", 12 * kib}, {"video_registry", 3 * kib}, {"wal", 6 * kib}}},
				{"/v", []Need{{"video_registry", 3 * kib}, {"videos", 30 * gib}}}}},
		{"split same device copy needs largest twice", videos3, PreflightOptions{Op: "split", Payload: PayloadVideo, Transfer: "copy"}, shared,
			[]wantDevice{{"/a", []Need{{"descriptions", 12 * kib}, {"video_registry", 6 * kib}, {"wal", 6 * kib}, {"largest_video", 20 * gib}}}}},
		{"split previews hook on archive device", Candidates{Count: 1, Bytes: gib, Largest: gib, PreviewBytes: 5 * kib},
			PreflightOptions{Op: "split", Payload: PayloadVideo}, separate,
			[]wantDevice{
				{"/a", []Need{{"descriptions", 4 * kib}, {"video_registry", kib}, {"wal", 2 * kib}, {"previews", 5 * kib}}},
				{"/v", []Need{{"video_registry", kib}, {"videos", gib}}}}},
		{"restore same device auto is negligible", videos3, PreflightOptions{Op: "restore", Payload: PayloadVideo, Transfer: "auto"}, shared,
			[]wantDevice{{"/a", []Need{{"wal", 6 * kib}}}}},
		{"restore other device auto", videos3, PreflightOptions{Op: "restore", Payload: PayloadVideo, Transfer: "auto"}, separate,
			[]wantDevice{{"/a", []Need{{"wal", 6 * kib}, {"videos", 30 * gib}}}}},
		{"restore same device copy keeps sources", videos3, PreflightOptions{Op: "restore", Payload: PayloadVideo, Transfer: "copy"}, shared,
			[]wantDevice{{"/a", []Need{{"wal", 6 * kib}, {"videos", 30 * gib}}}}},
		{"no candidates still checks write devices", Candidates{}, PreflightOptions{Op: "split", Payload: PayloadVideo}, separate,
			[]wantDevice{{"/a", nil}, {"/v", nil}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Plan(tt.c, tt.o, tt.info)
			var got []wantDevice
			for _, d := range req.Devices {
				got = append(got, wantDevice{d.Path, d.Needs})
				var sum int64
				for _, n := range d.Needs {
					sum += n.Bytes
				}
				if d.Required != sum {
					t.Errorf("%s: required %d, want sum of needs %d", d.Path, d.Required, sum)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("devices = %+v\nwant      %+v", got, tt.want)
			}
			if !req.Sufficient() {
				t.Errorf("want sufficient with 100GiB free: %+v", req)
			}
		})
	}
}

func TestPlanThresholdIsRequiredPlusMinFree(t *testing.T) {
	c := Candidates{Count: 1, Bytes: 10 * gib, Largest: 10 * gib}
	o := PreflightOptions{Op: "restore", Payload: PayloadVideo, Transfer: "copy", MinFree: gib}
	required := 10*gib + 2*kib // videos + wal
	for _, tt := range []struct {
		name      string
		avail     int64
		shortfall int64
	}{
		{"exactly at threshold passes", required + gib, 0},
		{"one byte short fails", required + gib - 1, 1},
		{"min-free is part of the requirement", required, gib},
		{"empty device", 0, required + gib},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := Plan(c, o, devices(dev(tt.avail, "/a", RoleArchive, RoleVideoArchive)))
			d := req.Devices[0]
			if d.Required != required || d.MinFree != gib || d.Available != tt.avail || !d.Known {
				t.Fatalf("device = %+v", d)
			}
			if d.Shortfall != tt.shortfall || req.Shortfall() != tt.shortfall || req.Sufficient() != (tt.shortfall == 0) {
				t.Fatalf("shortfall = %d sufficient = %v, want %d", d.Shortfall, req.Sufficient(), tt.shortfall)
			}
		})
	}
}

func TestPlanSharedDeviceSumsRequirementsOnce(t *testing.T) {
	// Split copy across devices needs 7KiB on the archive and 10GiB+1KiB on the video archive.
	// Each alone fits 10GiB+4KiB of free space, so separate devices pass ...
	c := Candidates{Count: 1, Bytes: 10 * gib, Largest: 10 * gib}
	o := PreflightOptions{Op: "split", Payload: PayloadVideo, Transfer: "copy"}
	avail := 10*gib + 4*kib
	if req := Plan(c, o, devices(dev(avail, "/a", RoleArchive), dev(avail, "/v", RoleVideoArchive))); !req.Sufficient() {
		t.Fatalf("separate devices: %+v", req)
	}
	// ... while one device holding both roots is checked once against the summed requirement:
	// descriptions 4KiB + two registry copies 2KiB + wal 2KiB + the largest video once.
	req := Plan(c, o, devices(dev(avail, "/a", RoleArchive, RoleVideoArchive)))
	if len(req.Devices) != 1 || req.Devices[0].Required != 10*gib+8*kib || req.Devices[0].Shortfall != 4*kib {
		t.Fatalf("shared device: %+v", req)
	}
}

func TestPlanUnknownNetworkFreeSpaceDoesNotFail(t *testing.T) {
	info := devices(dev(100*gib, "/a", RoleArchive), Device{Roles: []Role{RoleVideoArchive}, Path: "//nas/video"})
	req := Plan(videos3, PreflightOptions{Op: "split", Payload: PayloadVideo, MinFree: gib}, info)
	v := req.Devices[1]
	if v.Known || v.Shortfall != 0 || v.Required == 0 || !req.Sufficient() {
		t.Fatalf("unknown device = %+v", v)
	}
	var buf bytes.Buffer
	LogRequirement(slog.New(slog.NewTextHandler(&buf, nil)), req)
	if !strings.Contains(buf.String(), `level=WARN msg="free space unknown`) || !strings.Contains(buf.String(), "available=unknown") {
		t.Fatalf("no unknown-space warning:\n%s", buf.String())
	}
}

func TestPlanSaturatesAndIgnoresNegativeInput(t *testing.T) {
	huge := Candidates{Count: math.MaxInt64, Bytes: math.MaxInt64, Largest: -5, PreviewBytes: -1}
	req := Plan(huge, PreflightOptions{Op: "split", Payload: PayloadVideo, MinFree: math.MaxInt64}, separate)
	for _, d := range req.Devices {
		if d.Required != math.MaxInt64 || d.Shortfall <= 0 {
			t.Fatalf("device = %+v, want saturated requirement", d)
		}
	}
	req = Plan(Candidates{Count: -3, Bytes: -1}, PreflightOptions{Op: "restore", Payload: PayloadVideo, Transfer: "copy", MinFree: -1}, shared)
	if d := req.Devices[0]; d.Required != 0 || d.MinFree != 0 || len(d.Needs) != 0 {
		t.Fatalf("negative input: %+v", d)
	}
	big := devices(Device{Roles: []Role{RoleArchive, RoleVideoArchive}, Path: "/a", Space: fsops.Space{Total: math.MaxUint64, Available: math.MaxUint64}})
	if d := Plan(videos3, PreflightOptions{Op: "restore", Payload: PayloadVideo}, big).Devices[0]; d.Available != math.MaxInt64 {
		t.Fatalf("available = %d, want capped", d.Available)
	}
}

func TestPreflightReportFormatIsStable(t *testing.T) {
	info := devices(
		dev(5*gib, "/data/archive", RoleArchive),
		Device{Roles: []Role{RoleVideoArchive}, Path: "/mnt/video", Space: fsops.Space{Total: 40 << 30, Available: 30 << 30}},
	)
	req := Plan(Candidates{Count: 2, Bytes: 31 * gib, Largest: 30 * gib}, PreflightOptions{Op: "split", Payload: PayloadVideo, MinFree: gib}, info)
	var buf bytes.Buffer
	noTime := func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}
	LogRequirement(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: noTime})), req)
	want := `level=INFO msg="preflight device" roles=archive path=/data/archive required=14.0KiB min_free=1.0GiB available=5.0GiB shortfall=0B needs="descriptions=8.0KiB video_registry=2.0KiB wal=4.0KiB" required_bytes=14336 min_free_bytes=1073741824 available_bytes=5368709120 shortfall_bytes=0
level=INFO msg="preflight device" roles=video_archive path=/mnt/video required=31.0GiB min_free=1.0GiB available=30.0GiB shortfall=2.0GiB needs="video_registry=2.0KiB videos=31.0GiB" required_bytes=33285998592 min_free_bytes=1073741824 available_bytes=32212254720 shortfall_bytes=2147485696
level=ERROR msg="preflight failed: insufficient free space" op=split devices=2 shortfall=2.0GiB shortfall_bytes=2147485696
`
	if buf.String() != want {
		t.Fatalf("report:\n%s\nwant:\n%s", buf.String(), want)
	}
	err := error(&InsufficientSpaceError{Requirement: req})
	if !errors.Is(err, ErrInsufficientSpace) || err.Error() != "insufficient free space: /mnt/video short by 2.0GiB" {
		t.Fatalf("error = %v", err)
	}
}

// fakeFS reports devices by path prefix and fixed free space, and counts free-space queries.
type fakeFS struct {
	fsops.System
	device     func(path string) string
	space      map[string]fsops.Space
	spaceCalls int
	err        error
}

func (f *fakeFS) SameDevice(a, b string) (bool, error) {
	return f.device(a) == f.device(b), f.err
}

func (f *fakeFS) FreeSpace(path string) (fsops.Space, error) {
	f.spaceCalls++
	return f.space[f.device(path)], f.err
}

func TestProbeDevicesGroupsRootsAndQueriesEachDeviceOnce(t *testing.T) {
	f := &fakeFS{
		device: func(p string) string { return strings.SplitN(strings.TrimPrefix(p, "/"), "/", 2)[0] },
		space:  map[string]fsops.Space{"data": {Total: 9, Available: 7}, "nas": {Total: 5, Available: 1}},
	}
	info, err := ProbeDevices(f, []RolePath{{RoleArchive, "/data/archive"}, {RoleVideoArchive, "/nas/video"}, {RoleRegistry, "/data/reg.csv"}, {RoleRegistry, ""}})
	if err != nil {
		t.Fatal(err)
	}
	want := DeviceInfo{Devices: []Device{
		{Roles: []Role{RoleArchive, RoleRegistry}, Path: "/data/archive", Space: fsops.Space{Total: 9, Available: 7}},
		{Roles: []Role{RoleVideoArchive}, Path: "/nas/video", Space: fsops.Space{Total: 5, Available: 1}},
	}}
	if !reflect.DeepEqual(info, want) || f.spaceCalls != 2 {
		t.Fatalf("info = %+v (%d free-space calls)", info, f.spaceCalls)
	}
	f.err = errors.New("boom")
	if _, err := ProbeDevices(f, []RolePath{{RoleArchive, "/data/a"}}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v", err)
	}
}

func TestProbeDevicesRealSystemSharesTempDevice(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive")
	if err := os.Mkdir(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := ProbeDevices(fsops.System{}, []RolePath{{RoleArchive, archive}, {RoleVideoArchive, filepath.Join(dir, "missing", "video")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Devices) != 1 || len(info.Devices[0].Roles) != 2 || info.Devices[0].Space.Total == 0 {
		t.Fatalf("info = %+v", info)
	}
}
