// Command plancheck is repository tooling for the planning workflow:
//
//	plancheck lint    specification registry, plan and records agree
//	plancheck links   relative Markdown links and anchors resolve
//	plancheck status  open task counts and the next eligible task
//
// It runs from the repository root (or -root DIR) and is not shipped.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/volod/arxiv-go/internal/devtools/planning"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("plancheck", flag.ContinueOnError)
	fl.SetOutput(stderr)
	root := fl.String("root", ".", "repository root")
	if err := fl.Parse(args); err != nil || fl.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: plancheck [-root DIR] lint|links|status")
		return 2
	}
	fsys := os.DirFS(*root)
	switch fl.Arg(0) {
	case "lint":
		in, problems, err := planning.Load(fsys)
		if err != nil {
			fmt.Fprintln(stderr, "plancheck:", err)
			return 1
		}
		return report(stdout, stderr, "lint-spec-plan", append(problems, planning.Lint(in)...))
	case "links":
		problems, err := planning.CheckLinks(fsys)
		if err != nil {
			fmt.Fprintln(stderr, "plancheck:", err)
			return 1
		}
		return report(stdout, stderr, "lint-doc-links", problems)
	case "status":
		in, _, err := planning.Load(fsys)
		if err != nil {
			fmt.Fprintln(stderr, "plancheck:", err)
			return 1
		}
		planning.ComputeStatus(in.Plan).Print(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "plancheck: unknown command %q\n", fl.Arg(0))
		return 2
	}
}

func report(stdout, stderr io.Writer, name string, problems []string) int {
	for _, p := range problems {
		fmt.Fprintln(stderr, p)
	}
	if len(problems) > 0 {
		fmt.Fprintf(stderr, "%s: %d problem(s)\n", name, len(problems))
		return 1
	}
	fmt.Fprintf(stdout, "%s: ok\n", name)
	return 0
}
