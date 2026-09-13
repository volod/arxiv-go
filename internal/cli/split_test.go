package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

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
	stub, err := os.ReadFile(src + ".md")
	if err != nil || !bytes.Contains(stub, []byte("rel_path: clip.mp4")) {
		t.Fatalf("placeholder stub: %s, %v", stub, err)
	}
}
