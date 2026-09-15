package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/archive"
	"github.com/volod/arxiv-go/internal/state"
)

// catiaCLIFixture returns an archive with a synthetic CATIA part and a text file, a video archive
// and a CATIA archive path that split creates.
func catiaCLIFixture(t *testing.T) (arc, video, cat string, part []byte) {
	t.Helper()
	arc, video = fixture(t)
	cat = filepath.Join(filepath.Dir(arc), "catia")
	part = []byte("V5_CFV2\x00 invented part body")
	if err := os.MkdirAll(filepath.Join(arc, "cad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(arc, "cad", "fixture-part.CATPart"), part, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(arc, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	return arc, video, cat, part
}

func mapLookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestSplitCatiaCommandIgnoresVideoArchiveEnvironment(t *testing.T) {
	arc, video, cat, part := catiaCLIFixture(t)
	withLockIdentity(t, 500)
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, mapLookup(map[string]string{"ARXGO_VIDEO_ARCHIVE": video}))
	code := run(context.Background(), []string{"split", "--catia", "--archive", arc, "--catia-archive", cat,
		"--min-free", "0"}, e)
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got, err := os.ReadFile(filepath.Join(cat, "cad", "fixture-part.CATPart")); err != nil || !bytes.Equal(got, part) {
		t.Fatalf("CATIA archive copy: %v", err)
	}
	description, err := os.ReadFile(filepath.Join(arc, "cad", "fixture-part.CATPart.md"))
	if err != nil || !bytes.Contains(description, []byte("\ncatia: CATPart | V5_CFV2 | unknown | 0 components\n")) {
		t.Fatalf("description: %s, %v", description, err)
	}
	for _, root := range []string{arc, cat} {
		if _, err := os.Stat(filepath.Join(root, "arxgo-catia.csv")); err != nil {
			t.Fatalf("catia registry in %s: %v", root, err)
		}
		if _, err := os.Stat(filepath.Join(root, "arxgo-videos.csv")); !os.IsNotExist(err) {
			t.Fatalf("video registry in %s: %v", root, err)
		}
	}
	if entries, _ := os.ReadDir(video); len(entries) != 0 {
		t.Fatalf("video archive touched: %v", entries)
	}
	id, err := state.ReadCurrent(arc)
	if err != nil {
		t.Fatal(err)
	}
	var o state.RunOptions
	if err := state.ReadJSON(filepath.Join(state.StateDir(arc), "runs", id, state.OptionsFile), &o); err != nil {
		t.Fatal(err)
	}
	var defining SplitOptions
	if err := json.Unmarshal(o.Defining, &defining); err != nil {
		t.Fatal(err)
	}
	if o.Payload != PayloadCatia || o.CatiaArchive != cat || o.VideoArchive != "" || defining.Payload != PayloadCatia ||
		defining.CatiaArchive != cat || defining.VideoArchive != "" || bytes.Contains(o.Options, []byte(video+`"`)) {
		t.Fatalf("options.json: %+v defining %s options %s", o, o.Defining, o.Options)
	}
}

func TestCatiaUsageErrorsExitBeforeLock(t *testing.T) {
	arc, video, cat, _ := catiaCLIFixture(t)
	if err := os.Mkdir(cat, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"catia with video", []string{"--catia", "--video", "--catia-archive", cat}, nil, "mutually exclusive"},
		{"catia with video archive", []string{"--catia", "--catia-archive", cat, "--video-archive", video}, nil, "--video-archive cannot be used with --catia"},
		{"catia archive without catia", []string{"--video-archive", video, "--catia-archive", cat}, nil, "--catia-archive requires --catia"},
		{"catia without catia archive", []string{"--catia"}, map[string]string{"ARXGO_VIDEO_ARCHIVE": video}, "--catia-archive is required"},
		{"catia archive inside video archive", []string{"--catia", "--catia-archive", filepath.Join(video, "cad")},
			map[string]string{"ARXGO_VIDEO_ARCHIVE": video}, "is inside --video-archive"},
		{"video archive inside catia archive", []string{"--video-archive", filepath.Join(cat, "video")},
			map[string]string{"ARXGO_CATIA_ARCHIVE": cat}, "is inside --catia-archive"},
		{"equal mirror roots", []string{"--catia", "--catia-archive", video}, map[string]string{"ARXGO_VIDEO_ARCHIVE": video}, "same directory"},
		{"catia inside archive", []string{"--catia", "--catia-archive", filepath.Join(arc, "cad")}, nil, "is inside --archive"},
		{"catia with sample", []string{"--catia", "--catia-archive", cat, "--sample", "start"}, nil, "--sample start cannot be used with --catia"},
		{"catia with image from env", []string{"--catia", "--catia-archive", cat}, map[string]string{"ARXGO_IMAGE": "series"}, "ARXGO_IMAGE series cannot be used with --catia"},
		{"catia with publish", []string{"--catia", "--catia-archive", cat, "--publish", "gdrive"}, nil, "option not available in this build"},
		{"both payload variables", []string{"--catia-archive", cat}, map[string]string{"ARXGO_VIDEO": "true", "ARXGO_CATIA": "true"}, "mutually exclusive"},
		{"catia on restore", []string{"--catia", "--video-archive", video}, nil, "flag provided but not defined: --catia"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			op := "split"
			if strings.Contains(tc.name, "restore") {
				op = "restore"
			}
			args := append([]string{op, "--archive", arc, "--min-free", "0"}, tc.args...)
			code := run(context.Background(), args, testEnv(&out, &errOut, mapLookup(tc.env)))
			if code != ExitUsage || !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("exit %d, stderr %s; want 2 with %q", code, errOut.String(), tc.want)
			}
			for _, root := range []string{arc, video, cat} {
				if _, err := os.Stat(state.StateDir(root)); !os.IsNotExist(err) {
					t.Fatalf("state written in %s before validation finished: %v", root, err)
				}
			}
		})
	}
}

func TestPayloadFlagsResolveAsOneSetting(t *testing.T) {
	arc, video, cat, _ := catiaCLIFixture(t)
	cases := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"default video", []string{"--video-archive", video}, nil, PayloadVideo},
		{"catia from environment", nil, map[string]string{"ARXGO_CATIA": "true", "ARXGO_CATIA_ARCHIVE": cat}, PayloadCatia},
		{"video flag overrides catia variable", []string{"--video", "--video-archive", video},
			map[string]string{"ARXGO_CATIA": "true", "ARXGO_CATIA_ARCHIVE": cat}, PayloadVideo},
		{"catia flag overrides video variable", []string{"--catia", "--catia-archive", cat},
			map[string]string{"ARXGO_VIDEO": "true", "ARXGO_VIDEO_ARCHIVE": video}, PayloadCatia},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := parseFlags(OpSplit, append([]string{"--archive", arc}, tc.args...), mapEnv(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			o, err := buildSplitOptions(s, osRootFS())
			if err != nil {
				t.Fatal(err)
			}
			if o.Payload != tc.want {
				t.Fatalf("payload = %q, want %q", o.Payload, tc.want)
			}
			if p := payloadOf(o.Common); (tc.want == PayloadCatia) != (p.Root == cat) || o.CreateMirror != (tc.want == PayloadCatia) {
				t.Fatalf("mirror %+v create %v", p, o.CreateMirror)
			}
		})
	}
}

func TestScanAcceptsAndIgnoresCatiaArchive(t *testing.T) {
	arc, _, _, _ := catiaCLIFixture(t)
	o, err := parseScan(t, []string{"--archive", arc, "--catia-archive", filepath.Join(arc, "inside")}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if o.Payload != "" || o.CatiaArchive != "" || o.VideoArchive != "" {
		t.Fatalf("scan kept payload settings: %+v", o.Common)
	}
}

// An interrupted CATIA split is rolled forward, with a CATIA description, by a later video split
// command that names no CATIA archive.
func TestVideoSplitCommandRecoversInterruptedCatiaSplit(t *testing.T) {
	arc, video, cat, part := catiaCLIFixture(t)
	withLockIdentity(t, 500)
	identity := sessionHooks
	sessionHooks = func(cfg *archive.Config) {
		identity(cfg)
		cfg.Crash = func(point string) error {
			if point == "wal:placed" {
				return errors.New("injected crash")
			}
			return nil
		}
	}
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	if code := run(context.Background(), []string{"split", "--catia", "--archive", arc, "--catia-archive", cat,
		"--min-free", "0", "--base-url", "https://cdn.example.com/cad"}, e); code != ExitFailure {
		t.Fatalf("crashed CATIA split exit %d: %s", code, errOut.String())
	}
	src := filepath.Join(arc, "cad", "fixture-part.CATPart")
	if _, err := os.Stat(src + ".md"); !os.IsNotExist(err) {
		t.Fatalf("description before recovery: %v", err)
	}
	sessionHooks = identity
	errOut.Reset()
	if code := run(context.Background(), []string{"split", "--archive", arc, "--video-archive", video,
		"--min-free", "0"}, e); code != ExitOK {
		t.Fatalf("video split exit %d: %s", code, errOut.String())
	}
	description, err := os.ReadFile(src + ".md")
	if err != nil || !bytes.Contains(description, []byte("\ncatia: CATPart | V5_CFV2 |")) ||
		!bytes.Contains(description, []byte("https://cdn.example.com/cad/cad/fixture-part.CATPart")) {
		t.Fatalf("recovered description: %s, %v", description, err)
	}
	if got, err := os.ReadFile(filepath.Join(cat, "cad", "fixture-part.CATPart")); err != nil || !bytes.Equal(got, part) {
		t.Fatalf("CATIA archive copy: %v", err)
	}
	if _, err := os.Stat(state.LockPath(cat)); !os.IsNotExist(err) {
		t.Fatalf("CATIA archive lock left behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(video, "cad")); !os.IsNotExist(err) {
		t.Fatalf("video archive received CATIA content: %v", err)
	}
}
