package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

const ffmpegHelperEnv = "ARXGO_TEST_FFMPEG_HELPER"
const ffmpegHeartbeatEnv = "ARXGO_TEST_FFMPEG_HEARTBEAT"
const ffmpegChildPIDEnv = "ARXGO_TEST_FFMPEG_CHILD_PID"

func runFFmpegHelper(mode string) {
	if mode == "child" {
		for {
			_ = os.WriteFile(os.Getenv(ffmpegHeartbeatEnv), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o600)
			time.Sleep(20 * time.Millisecond)
		}
	}
	if len(os.Args) == 3 && os.Args[1] == "-hide_banner" && os.Args[2] == "-encoders" {
		switch mode {
		case "encoders":
			fmt.Print("Encoders:\n V..... = Video\n A..... = Audio\n V..... libx264 H.264\n A..... aac AAC\n")
		case "encoder-sleep":
			time.Sleep(5 * time.Second)
		default:
			os.Exit(19)
		}
		return
	}
	if len(os.Args) < 8 || os.Args[1] != "-hide_banner" || os.Args[2] != "-nostdin" ||
		os.Args[3] != "-y" || os.Args[4] != "-progress" || os.Args[5] != "pipe:1" ||
		os.Args[6] != "-nostats" {
		os.Exit(20)
	}
	part := os.Args[len(os.Args)-1]
	switch mode {
	case "success":
		_, _ = os.Stdout.Write([]byte("out_time_us=1250000\nspeed=1.5x\npro"))
		_, _ = os.Stdout.Write([]byte("gress=continue\nout_time_us=2000000\nspeed=2.0x\nprogress=end\n"))
		_ = os.WriteFile(part, []byte("preview-data"), 0o600)
	case "failure":
		_ = os.WriteFile(part, []byte("partial"), 0o600)
		fmt.Fprint(os.Stderr, strings.Repeat("x", ffmpegStderrLimit+1024), "final encoder error")
		os.Exit(17)
	case "empty":
		// The reserved part remains empty.
	case "sleep":
		_ = os.WriteFile(part, []byte("partial"), 0o600)
		time.Sleep(5 * time.Second)
	case "tree", "orphan":
		exe, err := os.Executable()
		if err != nil {
			os.Exit(21)
		}
		child := exec.Command(exe, "-test.run=none")
		child.Env = append(os.Environ(), ffmpegHelperEnv+"=child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(22)
		}
		_ = os.WriteFile(os.Getenv(ffmpegChildPIDEnv), []byte(strconv.Itoa(child.Process.Pid)), 0o600)
		_ = os.WriteFile(part, []byte("partial"), 0o600)
		for i := 0; i < 100; i++ {
			if _, err := os.Stat(os.Getenv(ffmpegHeartbeatEnv)); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if mode == "orphan" {
			return
		}
		time.Sleep(5 * time.Second)
	default:
		os.Exit(23)
	}
}

func helperRunner(t *testing.T, mode string) *Runner {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(ffmpegHelperEnv, mode)
	return &Runner{Tools: Toolset{FFmpeg: {Path: exe}}}
}

func TestPreviewPartPathAndTimeout(t *testing.T) {
	if got := PreviewPartPath("folder/sample.mp4"); got != "folder/sample.arxgo-part.mp4" {
		t.Fatal(got)
	}
	if got := previewTimeout(3 * time.Second); got != 60*time.Second {
		t.Fatal(got)
	}
	if got := previewTimeout(20 * time.Second); got != 80*time.Second {
		t.Fatal(got)
	}
}

func TestProgressCollectorIgnoresInvalidValues(t *testing.T) {
	var got []Progress
	p := &progressCollector{callback: func(value Progress) { got = append(got, value) }}
	chunks := []string{
		"out_time_us=-1\nspeed=N/A\nprogress=unknown\n",
		"out_time_us=9999999999999999999\nspeed=Inf\n",
		"progress=continue\n",
		"out_time_us=500000\nspeed=0.75x\nprogress=end\n",
	}
	for _, chunk := range chunks {
		if _, err := p.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || got[0].OutTime != 0 || got[0].Speed != 0 || got[0].End ||
		got[1].OutTime != 500*time.Millisecond || got[1].Speed != .75 || !got[1].End {
		t.Fatalf("parsed progress = %+v", got)
	}
	p.Write([]byte(strings.Repeat("x", progressLineLimit+1) + "\n"))
	if !p.overflow {
		t.Fatal("long progress line was accepted")
	}
}

func TestRunnerProgressAndPublish(t *testing.T) {
	r := helperRunner(t, "success")
	out := filepath.Join(t.TempDir(), "preview.mp4")
	var progress []Progress
	err := r.Run(context.Background(), PreviewCommand{
		Args: []string{"-f", "lavfi"}, Output: out, TotalDuration: 2 * time.Second,
		OnProgress: func(p Progress) { progress = append(progress, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "preview-data" {
		t.Fatalf("preview = %q, %v", data, err)
	}
	if len(progress) != 2 || progress[0].OutTime != 1250*time.Millisecond ||
		progress[0].Speed != 1.5 || progress[0].End ||
		progress[1].OutTime != 2*time.Second || progress[1].Speed != 2 || !progress[1].End {
		t.Fatalf("progress = %+v", progress)
	}
	if _, err := os.Stat(PreviewPartPath(out)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part remains: %v", err)
	}
}

func TestRunnerFailureAndConflicts(t *testing.T) {
	for _, mode := range []string{"failure", "empty", "sleep"} {
		t.Run(mode, func(t *testing.T) {
			r := helperRunner(t, mode)
			r.Timeout = 2 * time.Second
			out := filepath.Join(t.TempDir(), "preview.mp4")
			err := r.Run(context.Background(), PreviewCommand{Output: out})
			if err == nil {
				t.Fatal("expected failure")
			}
			switch mode {
			case "failure":
				if !strings.Contains(err.Error(), "exit status 17") ||
					!strings.Contains(err.Error(), "final encoder error") ||
					strings.Contains(err.Error(), strings.Repeat("x", ffmpegStderrLimit+1024)) {
					t.Fatalf("stderr tail: %v", err)
				}
			case "empty":
				if !strings.Contains(err.Error(), "empty") {
					t.Fatal(err)
				}
			case "sleep":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			}
			for _, path := range []string{out, PreviewPartPath(out)} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unexpected file %s: %v", path, err)
				}
			}
		})
	}
	t.Run("existing final", func(t *testing.T) {
		r := helperRunner(t, "success")
		out := filepath.Join(t.TempDir(), "preview.mp4")
		if err := os.WriteFile(out, []byte("user file"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := r.Run(context.Background(), PreviewCommand{Output: out}); !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(out); string(data) != "user file" {
			t.Fatal("existing preview changed")
		}
	})
	t.Run("existing part", func(t *testing.T) {
		r := helperRunner(t, "success")
		out := filepath.Join(t.TempDir(), "preview.mp4")
		part := PreviewPartPath(out)
		if err := os.WriteFile(part, []byte("user part"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := r.Run(context.Background(), PreviewCommand{Output: out}); !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(part); string(data) != "user part" {
			t.Fatal("existing part changed")
		}
	})
}

func TestRunnerCancellation(t *testing.T) {
	r := helperRunner(t, "sleep")
	out := filepath.Join(t.TempDir(), "preview.mp4")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Run(ctx, PreviewCommand{Output: out}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(PreviewPartPath(out)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part remains: %v", err)
	}
}

func TestRunnerEncoders(t *testing.T) {
	r := helperRunner(t, "encoders")
	got, err := r.Encoders(context.Background())
	if err != nil || !got["libx264"] || !got["aac"] || len(got) != 2 {
		t.Fatalf("encoders = %v, %v", got, err)
	}
	delete(got, "aac")
	t.Setenv(ffmpegHelperEnv, "failure")
	cached, err := r.Encoders(context.Background())
	if err != nil || !cached["aac"] {
		t.Fatalf("cached encoders = %v, %v", cached, err)
	}
}

func TestRunnerEncoderProbeRetryAfterTimeout(t *testing.T) {
	r := helperRunner(t, "encoder-sleep")
	r.EncoderTimeout = 2 * time.Second
	if _, err := r.Encoders(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	t.Setenv(ffmpegHelperEnv, "encoders")
	got, err := r.Encoders(context.Background())
	if err != nil || !got["libx264"] {
		t.Fatalf("retry = %v, %v", got, err)
	}
}

func TestRunnerLiveLavfi(t *testing.T) {
	ffmpeg := tooltest.LookPath(t, "ffmpeg")
	r := &Runner{Tools: Toolset{FFmpeg: {Path: ffmpeg}}}
	out := filepath.Join(t.TempDir(), "lavfi.avi")
	var progress []Progress
	err := r.Run(context.Background(), PreviewCommand{
		Args:   []string{"-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=25", "-t", "1", "-c:v", "mpeg4"},
		Output: out, TotalDuration: time.Second,
		OnProgress: func(p Progress) { progress = append(progress, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() == 0 {
		t.Fatalf("live output: %v, %v", info, err)
	}
	if len(progress) == 0 || !progress[len(progress)-1].End {
		t.Fatalf("live progress: %+v", progress)
	}
	encoders, err := r.Encoders(context.Background())
	if err != nil || !encoders["mpeg4"] {
		t.Fatalf("live encoders: %v, %v", encoders["mpeg4"], err)
	}
}
