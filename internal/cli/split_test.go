package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/archive"
	"github.com/volod/arxiv-go/test/fixtures/testmp4"
)

func TestSplitCommandMovesVideo(t *testing.T) {
	archive, video := fixture(t)
	body := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1"}}, MdatBytes: 512})
	src, dst := filepath.Join(archive, "clip.mp4"), filepath.Join(video, "clip.mp4")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"split", "--archive", archive, "--video-archive", video,
		"--metadata", "file", "--verify", "hash", "--min-free", "0"}, testEnv(&out, &errOut, noProcessEnv))
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("video at mirror: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	description, err := os.ReadFile(src + ".md")
	if err != nil || !bytes.HasPrefix(description, []byte("arxgo: clip.mp4\n")) {
		t.Fatalf("description: %s, %v", description, err)
	}
	for _, root := range []string{archive, video} {
		if _, err := os.Stat(filepath.Join(root, "arxgo-videos.csv")); err != nil {
			t.Fatalf("video registry in %s: %v", root, err)
		}
	}
}

func TestRestoreCommandRoundTrip(t *testing.T) {
	archive, video := fixture(t)
	body := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1"}}, MdatBytes: 512})
	src, dst := filepath.Join(archive, "clip.mp4"), filepath.Join(video, "clip.mp4")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	code := run(context.Background(), []string{"split", "--archive", archive, "--video-archive", video,
		"--metadata", "file", "--min-free", "0"}, e)
	if code != ExitOK {
		t.Fatalf("split exit %d: %s", code, errOut.String())
	}
	errOut.Reset()
	code = run(context.Background(), []string{"restore", "--archive", archive, "--video-archive", video,
		"--min-free", "0"}, e)
	if code != ExitOK {
		t.Fatalf("restore exit %d: %s", code, errOut.String())
	}
	got, err := os.ReadFile(src)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("restored video: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("video archive copy remains: %v", err)
	}
	if _, err := os.Stat(src + ".md"); !os.IsNotExist(err) {
		t.Fatalf("description remains: %v", err)
	}
}

func TestNewRunRecoversInterruptedSplitFromItsOptions(t *testing.T) {
	archiveRoot, video := fixture(t)
	body := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1"}}, MdatBytes: 512})
	src, dst := filepath.Join(archiveRoot, "clip.mp4"), filepath.Join(video, "clip.mp4")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
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
	args := []string{"split", "--archive", archiveRoot, "--video-archive", video, "--metadata", "file",
		"--min-free", "0", "--base-url", "https://cdn.example.com/v"}
	if code := run(context.Background(), args, e); code != ExitFailure {
		t.Fatalf("crashed split exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(src + ".md"); !os.IsNotExist(err) {
		t.Fatalf("description before recovery: %v", err)
	}
	sessionHooks = identity
	errOut.Reset()
	// Other defining options and --new-run: the interrupted run is recovered with its own options.
	next := []string{"split", "--archive", archiveRoot, "--video-archive", video, "--metadata", "file",
		"--min-free", "0", "--verify", "hash", "--new-run"}
	if code := run(context.Background(), next, e); code != ExitOK {
		t.Fatalf("new run exit %d: %s", code, errOut.String())
	}
	description, err := os.ReadFile(src + ".md")
	if err != nil || !bytes.Contains(description, []byte("https://cdn.example.com/v/clip.mp4")) {
		t.Fatalf("recovered description lacks the interrupted run's base URL: %s, %v", description, err)
	}
	if got, err := os.ReadFile(dst); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("video at mirror: %v", err)
	}
	if !strings.Contains(errOut.String(), "recovering the interrupted run") {
		t.Fatalf("recovery not logged: %s", errOut.String())
	}
}
