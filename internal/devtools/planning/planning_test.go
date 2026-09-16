package planning

import (
	"strings"
	"testing"
	"testing/fstest"
)

const testSpec = "# Spec\n\n## Capability Registry\n\n" +
	"| # | Capability | Status | Implementation |\n| --- | --- | --- | --- |\n" +
	"| 1 | `alpha` | shipped | x |\n| 2 | `beta` | planned | y |\n\n## After\n"

const testWorkflow = "# Workflow\n\n| Capability id | Group abbrev |\n| --- | --- |\n" +
	"| `alpha` | `al` |\n| `beta` | `be` |\n"

func agentTask(id, deps string) string {
	return "#### " + id + "\n\nDo it.\n\n" +
		"- Serves: `beta` -- [Spec](../openspec/spec.md)\n- Agent status: CLEAR\n" +
		"- Dependencies: " + deps + "\n- User-visible outcome: o\n- Scope boundary: s\n" +
		"- Data and artifact paths: p\n- Execution path:\n  multi-line\n  execution\n- Acceptance gates: g\n" +
		"- Documentation target: d\n- Review checkpoint: none\n\n"
}

func testRepo(plan string) fstest.MapFS {
	return fstest.MapFS{
		SpecPath:                              {Data: []byte(testSpec)},
		WorkflowPath:                          {Data: []byte(testWorkflow)},
		PlanPath:                              {Data: []byte(plan)},
		RecordsDir + "/README.md":             {Data: []byte("| [0001](0001-al-first-task.md) | x |\n")},
		RecordsDir + "/template.md":           {Data: []byte("# Template\n")},
		RecordsDir + "/0001-al-first-task.md": {Data: []byte("# First\n\n- Id / capability / checkpoint: `first-task` / `alpha` / none\n")},
	}
}

func validPlan() string {
	return "# Plan\n\n## Agent Implementation Tasks\n\n### Beta -- `beta`\n\n" +
		agentTask("build-beta", "[First](records/0001-al-first-task.md).") +
		agentTask("finish-beta", "`build-beta`; `wait-human`.") +
		"## Human-Assisted Tasks\n\n### Beta -- `beta`\n\n#### wait-human\n\nDecide.\n\n" +
		"- Serves: `beta` -- x\n- Human status: HUMAN-GATED\n- Dependencies: none.\n" +
		"- Requested input or decision: d\n- Unblocks: `finish-beta`.\n"
}

func lintRepo(t *testing.T, fsys fstest.MapFS) []string {
	t.Helper()
	in, problems, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return append(problems, Lint(in)...)
}

func TestLintAcceptsConsistentDocuments(t *testing.T) {
	if errs := lintRepo(t, testRepo(validPlan())); len(errs) != 0 {
		t.Fatalf("unexpected problems:\n%s", strings.Join(errs, "\n"))
	}
}

func TestParsePlanKeepsMultilineFields(t *testing.T) {
	plan := ParsePlan(validPlan())
	if got := plan.Tasks[0].Fields["Execution path"]; got != "multi-line execution" {
		t.Fatalf("Execution path = %q", got)
	}
}

func TestLintDetectsDefects(t *testing.T) {
	cases := []struct {
		name string
		fsys func() fstest.MapFS
		want string
	}{
		{"unknown dependency", func() fstest.MapFS {
			return testRepo(strings.Replace(validPlan(), "`build-beta`;", "`ghost-task`;", 1))
		}, "dependency `ghost-task` is not an open task"},
		{"missing record", func() fstest.MapFS {
			return testRepo(strings.Replace(validPlan(), "0001-al-first-task", "0009-al-gone", 1))
		}, "0009-al-gone.md does not exist"},
		{"cycle", func() fstest.MapFS {
			return testRepo(strings.Replace(validPlan(), "[First](records/0001-al-first-task.md).", "`finish-beta`.", 1))
		}, "dependency cycle"},
		{"missing field", func() fstest.MapFS {
			return testRepo(strings.Replace(validPlan(), "- Scope boundary: s\n", "", 1))
		}, `missing field "Scope boundary"`},
		{"bad status", func() fstest.MapFS {
			return testRepo(strings.Replace(validPlan(), "Agent status: CLEAR", "Agent status: DONE", 1))
		}, `Agent status "DONE"`},
		{"group not in registry", func() fstest.MapFS {
			return testRepo(strings.Replace(validPlan(), "### Beta -- `beta`", "### Gamma -- `gamma`", 1))
		}, `group "gamma" is not in the registry`},
		{"planned capability without tasks", func() fstest.MapFS {
			return testRepo("# Plan\n\n## Agent Implementation Tasks\n")
		}, `planned capability "beta" has no open tasks`},
		{"record not indexed", func() fstest.MapFS {
			fsys := testRepo(validPlan())
			fsys[RecordsDir+"/README.md"] = &fstest.MapFile{Data: []byte("empty\n")}
			return fsys
		}, "not linked from records/README.md"},
		{"record with unknown group", func() fstest.MapFS {
			fsys := testRepo(validPlan())
			fsys[RecordsDir+"/0002-zz-other.md"] = &fstest.MapFile{Data: []byte("x")}
			return fsys
		}, "unknown group"},
		{"accepted task still planned", func() fstest.MapFS {
			return testRepo(strings.Replace(validPlan(), "#### build-beta", "#### first-task", 1))
		}, `task "first-task" is still in the plan`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := lintRepo(t, tc.fsys())
			if !strings.Contains(strings.Join(errs, "\n"), tc.want) {
				t.Fatalf("want a problem containing %q, got:\n%s", tc.want, strings.Join(errs, "\n"))
			}
		})
	}
}

func TestStatusReportsEligibleWork(t *testing.T) {
	s := ComputeStatus(ParsePlan(validPlan()))
	if s.AgentTasks != 2 || s.HumanTasks != 1 {
		t.Fatalf("counts = %d agent, %d human", s.AgentTasks, s.HumanTasks)
	}
	if len(s.Eligible) != 1 || s.Eligible[0].ID != "build-beta" {
		t.Fatalf("eligible = %+v, want only build-beta", s.Eligible)
	}
	if len(s.WaitingHuman) != 1 || s.WaitingHuman[0].ID != "wait-human" {
		t.Fatalf("waiting human = %+v", s.WaitingHuman)
	}
}

func TestCheckLinks(t *testing.T) {
	fsys := fstest.MapFS{
		"README.md":                           {Data: []byte("[ok](docs/a.md#archive----core) [web](https://x.y) [self](#top)\n# Top\n")},
		"docs/a.md":                           {Data: []byte("# A\n## Archive -- `core`\n[bad](missing.md)\n[anchor](#nope)\n```\n[fenced](ignored.md)\n```\n")},
		".git/config.md":                      {Data: []byte("[x](nowhere.md)")},
		"test/testdata/report/description.md": {Data: []byte("[video](../../../../mnt/nas/video/clip.mp4)\n")},
	}
	errs, err := CheckLinks(fsys)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(errs, "\n")
	for _, want := range []string{`docs/a.md:3: broken link "missing.md"`, `docs/a.md:4: missing anchor "#nope"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
	if len(errs) != 2 {
		t.Errorf("want exactly 2 problems, got:\n%s", joined)
	}
}

func TestSlugMatchesGitHub(t *testing.T) {
	cases := map[string]string{
		"Project foundation -- `project-foundation`": "project-foundation----project-foundation",
		"Run lock":                  "run-lock",
		"File registry CSV":         "file-registry-csv",
		"Video -- Previews (v2.0)!": "video----previews-v20",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
