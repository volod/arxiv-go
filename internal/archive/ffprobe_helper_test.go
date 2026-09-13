package archive

import (
	"os"
	"path/filepath"
	"testing"
)

const ffprobeScanHelperEnv = "ARXGO_TEST_SCAN_FFPROBE_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(ffprobeScanHelperEnv) != "" {
		name := "avi.json"
		if filepath.Base(os.Args[len(os.Args)-1]) == "audio-only.avi" {
			name = "ogg.json"
		}
		data, err := os.ReadFile(filepath.Join("..", "media", "testdata", "ffprobe", name))
		if err != nil {
			os.Exit(20)
		}
		_, _ = os.Stdout.Write(data)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
