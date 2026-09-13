package cli

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"github.com/volod/arxiv-go/internal/media"
)

type toolDiscover func(media.Finder, context.Context, []media.Requirement) (media.Toolset, []media.Requirement, error)

// scanNeeds returns the tool needs of the scan settings. Stage-2 preview modes join when their
// flags become available.
func scanNeeds(sc ScanSettings) media.Needs {
	return media.Needs{MetadataMedia: sc.Metadata == MetadataMedia}
}

// requireTools discovers the tools the validated options need before any lock or write. When a
// tool is missing it logs one error per tool naming the unavailable options, prints the platform
// download guidance to stderr and returns ExitMissingTool; ok is true when the run may proceed.
func requireTools(ctx context.Context, e env, log *slog.Logger, needs media.Needs) (media.Toolset, int, bool) {
	reqs := media.Requirements(needs)
	if len(reqs) == 0 {
		return nil, ExitOK, true
	}
	f := e.finder
	f.Log = log
	discover := media.Finder.Discover
	if e.discover != nil {
		discover = e.discover
	}
	found, missing, err := discover(f, ctx, reqs)
	if err != nil {
		log.Error("tool discovery interrupted", "err", err)
		return nil, ExitInterrupted, false
	}
	if len(missing) == 0 {
		return found, ExitOK, true
	}
	goos, goarch := e.platform()
	for _, r := range missing {
		name := media.ExecutableName(r.Tool, goos)
		log.Error("required tool not found", "tool", name, "unavailable", strings.Join(r.Options, ", "))
	}
	fmt.Fprint(e.stderr, media.Guidance(missing, goos, goarch))
	return nil, ExitMissingTool, false
}

// platform returns the GOOS/GOARCH used for executable names and download links.
func (e env) platform() (string, string) {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	if e.finder.GOOS != "" {
		goos = e.finder.GOOS
	}
	if e.goarch != "" {
		goarch = e.goarch
	}
	return goos, goarch
}
