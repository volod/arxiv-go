// Package cli owns command-line parsing, flag validation and exit codes.
//
// The full operation contract is specified in docs/openspec/stage-1-core/cli.md
// and implemented by the plan task implement-cli-contract. This scaffold only
// fixes the command identity, the operation names and the exit-code table.
package cli

import (
	"fmt"
	"io"
)

// Exit codes are part of the operator contract; see the CLI specification.
const (
	ExitOK               = 0
	ExitFailure          = 1
	ExitUsage            = 2
	ExitMissingTool      = 3
	ExitInsufficientDisk = 4
	ExitLocked           = 5
	ExitPartial          = 6
	ExitNotImplemented   = 70
)

// Operation names accepted as the first argument. Scan is the default.
const (
	OpScan    = "scan"
	OpSplit   = "split"
	OpRestore = "restore"
)

// version is overridden at build time with -ldflags "-X .../internal/cli.version=...".
var version = "dev"

// Version returns the build version string.
func Version() string { return version }

const usage = `arxgo - separate video files from a file archive and restore them

Usage:
  arxgo [scan] --archive PATH [flags]          build the CSV file registry (default)
  arxgo split  --archive PATH --video-archive PATH [flags]
  arxgo restore --archive PATH --video-archive PATH [flags]
  arxgo version
  arxgo help

See docs/openspec/stage-1-core/cli.md for the full flag contract.
`

// Run executes one command and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	op := OpScan
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		op = args[0]
	}
	switch op {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	case "version":
		fmt.Fprintf(stdout, "arxgo %s\n", version)
		return ExitOK
	case OpScan, OpSplit, OpRestore:
		fmt.Fprintf(stderr, "arxgo: operation %q is not implemented yet\n", op)
		return ExitNotImplemented
	default:
		fmt.Fprintf(stderr, "arxgo: unknown operation %q\n\n%s", op, usage)
		return ExitUsage
	}
}
