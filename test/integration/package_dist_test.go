package integration

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestPackageDist(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("release packaging is gated on Linux")
	}
	for _, name := range []string{"bash", "tar", "zip", "unzip", "sha256sum"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("%s is required for release packaging: %v", name, err)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	bin := filepath.Join(work, "bin")
	dist := filepath.Join(work, "dist")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	var lock strings.Builder
	for _, platform := range []string{"linux", "windows"} {
		ext := ""
		if platform == "windows" {
			ext = ".exe"
		}
		for _, tool := range []string{"arxgo", "ffmpeg", "ffprobe"} {
			body := []byte(platform + ":" + tool + "\n")
			if err := os.WriteFile(filepath.Join(bin, tool+ext), body, 0o755); err != nil {
				t.Fatal(err)
			}
			if tool != "arxgo" {
				fmt.Fprintf(&lock, "%s_amd64_%s_sha256=%x\n", platform, tool, sha256.Sum256(body))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(bin, ".env"), []byte("private fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(work, "ffmpeg.lock")
	if err := os.WriteFile(lockPath, []byte(lock.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	packageCmd := func() *exec.Cmd {
		return exec.Command("bash", filepath.Join(root, "scripts", "package-dist.sh"),
			"test", bin, dist, lockPath)
	}
	if out, err := packageCmd().CombinedOutput(); err != nil {
		t.Fatalf("package: %v: %s", err, out)
	}
	for _, platform := range []string{"linux", "windows"} {
		ext, archive := "", filepath.Join(dist, "arxgo-test-"+platform+"-amd64.tar.gz")
		if platform == "windows" {
			ext = ".exe"
			archive = filepath.Join(dist, "arxgo-test-windows-amd64.zip")
		}
		extract := filepath.Join(work, platform)
		if err := os.Mkdir(extract, 0o755); err != nil {
			t.Fatal(err)
		}
		var cmd *exec.Cmd
		if platform == "linux" {
			cmd = exec.Command("tar", "-xzf", archive, "-C", extract)
		} else {
			cmd = exec.Command("unzip", "-q", archive, "-d", extract)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("extract %s: %v: %s", platform, err, out)
		}
		var got []string
		err := filepath.WalkDir(extract, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(extract, path)
			got = append(got, filepath.ToSlash(rel))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{".env.example", "LICENSES/FFmpeg-SOURCE.txt", "LICENSES/GPL-3.0.txt",
			"LICENSES/arxgo-MIT.txt", "SHA256SUMS", "arxgo" + ext, "ffmpeg" + ext,
			"ffprobe" + ext, "manual-" + platform + ".md"}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s files: got %v, want %v", platform, got, want)
		}
		verify := exec.Command("sha256sum", "-c", "SHA256SUMS")
		verify.Dir = extract
		if out, err := verify.CombinedOutput(); err != nil {
			t.Errorf("%s checksums: %v: %s", platform, err, out)
		}
		notice, err := os.ReadFile(filepath.Join(extract, "LICENSES", "FFmpeg-SOURCE.txt"))
		provenance := "mwader/static-ffmpeg"
		if platform == "windows" {
			provenance = "GyanD/codexffmpeg"
		}
		if err != nil || !strings.Contains(string(notice), "FFmpeg "+pinnedFFmpegVersion(t)+" GPL v3") ||
			!strings.Contains(string(notice), provenance) {
			t.Errorf("%s licence notice: %v: %s", platform, err, notice)
		}
	}
	outer := exec.Command("sha256sum", "-c", "SHA256SUMS")
	outer.Dir = dist
	if out, err := outer.CombinedOutput(); err != nil {
		t.Fatalf("archive checksums: %v: %s", err, out)
	}

	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := packageCmd().CombinedOutput(); err == nil || !strings.Contains(string(out), "checksum mismatch") {
		t.Fatalf("tampered binary must fail before packaging: %v: %s", err, out)
	}
}

// pinnedFFmpegVersion reads the approved version from packaging/ffmpeg.lock, which the source
// notices must name.
func pinnedFFmpegVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "ffmpeg.lock"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "version="); ok {
			return v
		}
	}
	t.Fatal("ffmpeg.lock has no version")
	return ""
}
