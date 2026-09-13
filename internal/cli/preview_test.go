package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewFlagsValidateAndReachOptions(t *testing.T) {
	archive, video := fixture(t)
	s, err := parseFlags(OpSplit, []string{"--archive", archive, "--video-archive", video,
		"--sample", "series", "--sample-duration", "2.5s", "--sample-every", "30s",
		"--sample-resolution", "hd", "--sample-quality", "high",
		"--image", "end", "--image-every", "45s", "--image-resolution", "4k",
		"--image-quality", "low", "--preview-max-items", "7"}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	o, err := buildSplitOptions(s, osRootFS())
	if err != nil {
		t.Fatal(err)
	}
	p := o.Preview
	if p.SampleMode != "series" || p.ImageMode != "end" || p.SampleDuration != 2500*time.Millisecond ||
		p.SampleEvery != 30*time.Second || p.ImageEvery != 45*time.Second || p.SampleResolution != "hd" ||
		p.ImageResolution != "4k" || p.SampleQuality != "high" || p.ImageQuality != "low" || p.MaxItems != 7 {
		t.Errorf("preview options = %+v", p)
	}

	envSettings, err := parseFlags(OpSplit, []string{"--archive", archive, "--video-archive", video},
		mapEnv(map[string]string{"ARXGO_SAMPLE": "start", "ARXGO_PREVIEW_MAX_ITEMS": "3"}))
	if err != nil {
		t.Fatal(err)
	}
	envOptions, err := buildSplitOptions(envSettings, osRootFS())
	if err != nil || envOptions.Preview.SampleMode != "start" || envOptions.Preview.MaxItems != 3 {
		t.Errorf("environment preview options = %+v, %v", envOptions.Preview, err)
	}
}

func TestPreviewFlagErrorsAndNoMutation(t *testing.T) {
	archive, video := fixture(t)
	for _, tc := range []struct{ flag, value, want string }{
		{"--sample", "invalid", "must be one of"},
		{"--sample-resolution", "8k", "must be one of"},
		{"--image-quality", "lossless", "must be one of"},
		{"--sample-duration", "0s", "must be positive"},
		{"--sample-every", "-1s", "must be positive"},
		{"--image-every", "0s", "must be positive"},
		{"--preview-max-items", "0", "at least 1"},
	} {
		s, err := parseFlags(OpSplit, []string{"--archive", archive, "--video-archive", video, tc.flag, tc.value}, noEnv)
		if err == nil {
			_, err = buildSplitOptions(s, osRootFS())
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s: %v", tc.flag, tc.value, err)
		}
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"split", "--archive", archive, "--video-archive", video, "--sample", "start"}, testEnv(&out, &errOut, noProcessEnv))
	if code != ExitNotImplemented || !strings.Contains(errOut.String(), "preview generation is not available") {
		t.Fatalf("active mode exit = %d, stderr %q", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(archive, ".arxgo")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("preview request created run state: %v", err)
	}
}
