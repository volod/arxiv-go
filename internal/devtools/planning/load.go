package planning

import (
	"io/fs"
	"path"
	"sort"
)

// Repository-relative locations of the planning documents.
const (
	SpecPath     = "docs/openspec/spec.md"
	PlanPath     = "docs/impl/plan.md"
	WorkflowPath = "docs/guide/planning-workflow.md"
	RecordsDir   = "docs/impl/records"
)

// nonRecordFiles live in the records directory but are not task records.
var nonRecordFiles = map[string]bool{"README.md": true, "template.md": true}

// Load reads every planning document from the repository root fsys. Record
// naming errors are returned as lint problems rather than failures.
func Load(fsys fs.FS) (Inputs, []string, error) {
	read := func(p string) (string, error) {
		b, err := fs.ReadFile(fsys, p)
		return string(b), err
	}
	var in Inputs
	spec, err := read(SpecPath)
	if err != nil {
		return in, nil, err
	}
	planText, err := read(PlanPath)
	if err != nil {
		return in, nil, err
	}
	workflow, err := read(WorkflowPath)
	if err != nil {
		return in, nil, err
	}
	in.RecordIndex, err = read(path.Join(RecordsDir, "README.md"))
	if err != nil {
		return in, nil, err
	}
	in.Registry = ParseRegistry(spec)
	in.Plan = ParsePlan(planText)
	in.Groups = ParseGroupTable(workflow)

	entries, err := fs.ReadDir(fsys, RecordsDir)
	if err != nil {
		return in, nil, err
	}
	var problems []string
	for _, e := range entries {
		if e.IsDir() || nonRecordFiles[e.Name()] || path.Ext(e.Name()) != ".md" {
			continue
		}
		r, perr := ParseRecordName(e.Name(), in.Groups)
		if perr != nil {
			problems = append(problems, perr.Error())
			continue
		}
		if r.Body, err = read(path.Join(RecordsDir, e.Name())); err != nil {
			return in, nil, err
		}
		in.Records = append(in.Records, r)
	}
	sort.Slice(in.Records, func(i, j int) bool { return in.Records[i].File < in.Records[j].File })
	return in, problems, nil
}
