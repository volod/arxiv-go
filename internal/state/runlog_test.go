package state

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunLogFanout(t *testing.T) {
	path := filepath.Join(t.TempDir(), LogFile)
	rl, err := OpenRunLog(path, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	var console bytes.Buffer
	log := slog.New(Fanout(slog.NewTextHandler(&console, &slog.HandlerOptions{Level: slog.LevelWarn}), rl.Handler()))
	log = log.With("run_id", "r1")
	log.Debug("dropped everywhere")
	log.Info("file only", "n", 1)
	log.WithGroup("g").Warn("both", "k", "v")
	if err := rl.Close(); err != nil {
		t.Fatal(err)
	}

	if s := console.String(); strings.Contains(s, "file only") || !strings.Contains(s, "msg=both run_id=r1 g.k=v") {
		t.Errorf("console = %q", s)
	}
	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("log lines = %q", lines)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["msg"] != "both" || rec["run_id"] != "r1" || rec["g"].(map[string]any)["k"] != "v" {
		t.Errorf("record = %v", rec)
	}
	ts, err := time.Parse(time.RFC3339Nano, rec["time"].(string))
	if err != nil || ts.Location() != time.UTC || !strings.HasSuffix(rec["time"].(string), "Z") {
		t.Errorf("time %v is not RFC 3339 UTC (%v)", rec["time"], err)
	}
}

func TestRunLogTerminatesTornLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), LogFile)
	if err := os.WriteFile(path, []byte(`{"msg":"complete"}`+"\n"+`{"msg":"to`), 0o644); err != nil {
		t.Fatal(err)
	}
	rl, err := OpenRunLog(path, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	slog.New(rl.Handler()).Info("resumed")
	rl.Close()
	lines := readLines(t, path)
	if len(lines) != 3 || lines[1] != `{"msg":"to` {
		t.Fatalf("lines = %q", lines)
	}
	if !json.Valid([]byte(lines[2])) {
		t.Errorf("appended record merged with torn line: %q", lines[2])
	}
}

func TestFanoutEnabled(t *testing.T) {
	h := Fanout(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError}),
		slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if !h.Enabled(context.Background(), slog.LevelInfo) || h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Enabled must be true when any handler accepts the level")
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
