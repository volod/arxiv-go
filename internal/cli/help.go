package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

const generalUsage = `arxgo - separate video and CATIA files from a file archive and restore them

Usage:
  arxgo [scan]  --archive PATH [flags]    build the CSV file registry (default operation)
  arxgo split   --archive PATH --video-archive PATH [flags]
  arxgo split   --catia --archive PATH --catia-archive PATH [--catia-text] [flags]
  arxgo restore --archive PATH --video-archive PATH [flags]
  arxgo restore --catia --archive PATH --catia-archive PATH [flags]
  arxgo catia-index --archive PATH [--out PATH]
  arxgo version
  arxgo help [operation]

Flags use --name value or --name=value. Every flag may also be set with the environment
variable ARXGO_<NAME> (dashes become underscores), or in an optional .env file next to the
arxgo executable. Precedence: command line, process environment, .env file, default.
Run 'arxgo help <operation>' for the flags of one operation.
`

var opSynopsis = map[string]string{
	OpScan:       "arxgo [scan] --archive PATH [flags]\n\nBuild the CSV registry of every file in the archive.",
	OpSplit:      "arxgo split --archive PATH --video-archive PATH [flags]\n       arxgo split --catia --archive PATH --catia-archive PATH [--catia-text] [flags]\n\nMove video files (or, with --catia, CATIA files) into their mirrored archive and leave descriptions. With --catia-text, also write a searchable text sidecar.",
	OpCatiaIndex: "arxgo catia-index --archive PATH [--out PATH] [flags]\n\nWrite one Markdown document of every moved CATIA file's description fields and text sidecar: properties, components and notes. Reads only; starts no run.",
	OpRestore:    "arxgo restore --archive PATH --video-archive PATH [flags]\n       arxgo restore --catia --archive PATH --catia-archive PATH [flags]\n\nReturn videos (or, with --catia, CATIA files) from their mirrored archive to the main archive.",
}

// writeOpHelp prints the synopsis and flag table of one operation, with defaults and environment
// variable names, followed by flags reserved for planned features.
func writeOpHelp(w io.Writer, op string) {
	fmt.Fprintf(w, "Usage: %s\n", opSynopsis[op])
	fs := newFlagSet(op, &settings{})
	var current group
	var later []string
	for _, d := range flagTable {
		if !d.appliesTo(op) {
			continue
		}
		if d.plannedFeature != "" {
			later = append(later, fmt.Sprintf("--%s (%s)", d.name, d.plannedFeature))
			continue
		}
		if d.group != current {
			current = d.group
			fmt.Fprintf(w, "\n%s:\n", current)
		}
		fmt.Fprintf(w, "  %-32s %s\n", strings.TrimSpace("--"+d.name+" "+d.arg), d.usage)
		fmt.Fprintf(w, "  %-32s %s\n", "", flagDetails(fs.Lookup(d.name), d))
	}
	if len(later) > 0 {
		fmt.Fprintf(w, "\nReserved for planned features (not available in this build):\n")
		for _, line := range wrapItems(later, 94) {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}

func flagDetails(f *flag.Flag, d flagDef) string {
	parts := []string{}
	if f.DefValue != "" && f.DefValue != "[]" {
		parts = append(parts, "default "+f.DefValue)
	}
	parts = append(parts, "env "+EnvName(d.name))
	return "(" + strings.Join(parts, "; ") + ")"
}

// wrapItems joins items with ", " into lines of at most width bytes without splitting an item.
func wrapItems(items []string, width int) []string {
	var lines []string
	line := ""
	for i, item := range items {
		if i < len(items)-1 {
			item += ","
		}
		if line != "" && len(line)+1+len(item) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += item
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
