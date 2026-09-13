// Command arxgo separates video files from a mixed file archive into a
// mirrored video archive and restores them. Only initialization lives here;
// parsing and behavior belong to internal packages.
package main

import (
	"os"

	"github.com/volod/arxiv-go/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
