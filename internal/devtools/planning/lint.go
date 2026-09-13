package planning

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Required fields per lane, in the order the planning workflow defines them.
var (
	agentFields = []string{
		"Serves", "Agent status", "Dependencies", "User-visible outcome", "Scope boundary",
		"Data and artifact paths", "Execution path", "Acceptance gates", "Documentation target",
		"Review checkpoint",
	}
	humanFields = []string{
		"Serves", "Human status", "Dependencies", "Requested input or decision", "Unblocks",
	}
	agentStatuses = map[string]bool{"CLEAR": true, "RUN NEEDED": true}
	humanStatuses = map[string]bool{"HUMAN-GATED": true, "BLOCKED BY HUMAN": true}
	capStatuses   = map[string]bool{"planned": true, "shipped": true}
)

var (
	recordLinkRe = regexp.MustCompile(`\(records/([0-9]{4}-[a-z0-9-]+)\.md(?:#[^)]*)?\)`)
	recordNameRe = regexp.MustCompile(`^([0-9]{4})-([a-z0-9-]+)\.md$`)
	recordIDRe   = regexp.MustCompile("(?m)^- Id / capability / checkpoint: `([a-z0-9-]+)`")
)

// Record is one task record file under docs/impl/records.
type Record struct {
	File     string
	Sequence string
	Group    string
	TaskID   string
	Body     string
}

// Inputs are the documents a lint run reads.
type Inputs struct {
	Registry    []Capability
	Plan        Plan
	Groups      map[string]string // capability id -> record group abbreviation
	Records     []Record
	RecordIndex string // content of records/README.md
}

// ParseRecordName splits "NNNN-group-task-id.md" using the known group abbreviations.
func ParseRecordName(name string, groups map[string]string) (Record, error) {
	m := recordNameRe.FindStringSubmatch(name)
	if m == nil {
		return Record{}, fmt.Errorf("record %s: name must be NNNN-<group>-<task-id>.md", name)
	}
	abbrevs := make([]string, 0, len(groups))
	for _, a := range groups {
		abbrevs = append(abbrevs, a)
	}
	sort.Slice(abbrevs, func(i, j int) bool { return len(abbrevs[i]) > len(abbrevs[j]) })
	for _, a := range abbrevs {
		if rest, ok := strings.CutPrefix(m[2], a+"-"); ok && rest != "" {
			return Record{File: name, Sequence: m[1], Group: a, TaskID: rest}, nil
		}
	}
	return Record{}, fmt.Errorf("record %s: unknown group; add it to the planning workflow table", name)
}

// Lint returns every integrity problem found; an empty slice means the documents agree.
func Lint(in Inputs) []string {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	order := map[string]int{}
	for i, c := range in.Registry {
		if _, dup := order[c.ID]; dup {
			add("registry: capability %q listed twice", c.ID)
		}
		order[c.ID] = i
		if !capStatuses[c.Status] {
			add("registry: capability %q has status %q, want planned or shipped", c.ID, c.Status)
		}
		if _, ok := in.Groups[c.ID]; !ok {
			add("registry: capability %q has no record group in the planning workflow table", c.ID)
		}
	}
	if len(in.Registry) == 0 {
		add("registry: no capabilities parsed from spec")
	}

	lastByLane := map[string]int{}
	for _, g := range in.Plan.Groups {
		idx, ok := order[g.Capability]
		if !ok {
			add("plan:%d: group %q is not in the registry", g.Line, g.Capability)
			continue
		}
		if prev, seen := lastByLane[g.Lane]; seen && idx <= prev {
			add("plan:%d: group %q is out of registry order in the %s lane", g.Line, g.Capability, g.Lane)
		}
		lastByLane[g.Lane] = idx
	}

	open := map[string]Task{}
	tasksPerCap := map[string]int{}
	for _, t := range in.Plan.Tasks {
		if _, dup := open[t.ID]; dup {
			add("plan:%d: duplicate task id %q", t.Line, t.ID)
		}
		open[t.ID] = t
		tasksPerCap[t.Capability]++
	}

	accepted := map[string]Record{}
	for _, r := range in.Records {
		accepted[r.TaskID] = r
		if m := recordIDRe.FindStringSubmatch(r.Body); m == nil || m[1] != r.TaskID {
			add("record %s: first line of scope must be \"- Id / capability / checkpoint: `%s` ...\"", r.File, r.TaskID)
		}
		if !strings.Contains(in.RecordIndex, "("+r.File+")") {
			add("record %s: not linked from records/README.md", r.File)
		}
		if _, still := open[r.TaskID]; still {
			add("record %s: task %q is still in the plan", r.File, r.TaskID)
		}
	}

	for _, t := range in.Plan.Tasks {
		errs = append(errs, lintTask(t, open, accepted)...)
	}

	for _, c := range in.Registry {
		switch {
		case c.Status == "planned" && tasksPerCap[c.ID] == 0:
			add("registry: planned capability %q has no open tasks", c.ID)
		case c.Status == "shipped" && tasksPerCap[c.ID] > 0:
			add("registry: shipped capability %q still has %d open tasks", c.ID, tasksPerCap[c.ID])
		}
	}

	if cycle := findCycle(in.Plan.Tasks); cycle != nil {
		add("plan: dependency cycle %s", strings.Join(cycle, " -> "))
	}
	return errs
}

func lintTask(t Task, open map[string]Task, accepted map[string]Record) []string {
	var errs []string
	add := func(format string, a ...any) {
		errs = append(errs, fmt.Sprintf("plan:%d: task %q: ", t.Line, t.ID)+fmt.Sprintf(format, a...))
	}
	required, statusField, statuses := agentFields, "Agent status", agentStatuses
	if t.Lane == LaneHuman {
		required, statusField, statuses = humanFields, "Human status", humanStatuses
	}
	if t.Capability == "" {
		add("not under a capability group heading")
	}
	for _, f := range required {
		if strings.TrimSpace(t.Fields[f]) == "" {
			add("missing field %q", f)
		}
	}
	if s := t.Fields[statusField]; s != "" && !statuses[s] {
		add("%s %q is not valid for the %s lane", statusField, s, t.Lane)
	}
	if serves := t.Fields["Serves"]; serves != "" && !strings.HasPrefix(serves, "`"+t.Capability+"`") {
		add("Serves must start with `%s`", t.Capability)
	}
	for _, id := range dependencyIDs(t.Fields["Dependencies"]) {
		if _, ok := open[id]; !ok {
			add("dependency `%s` is not an open task (link accepted work as records/...)", id)
		}
	}
	for _, m := range recordLinkRe.FindAllStringSubmatch(t.Fields["Dependencies"], -1) {
		if !recordExists(m[1], accepted) {
			add("dependency record %s.md does not exist", m[1])
		}
	}
	if t.Lane == LaneHuman {
		for _, id := range dependencyIDs(t.Fields["Unblocks"]) {
			if _, ok := open[id]; !ok {
				add("unblocks `%s`, which is not an open task", id)
			}
		}
	}
	return errs
}

// dependencyIDs returns backticked task ids in a field, ignoring link targets.
func dependencyIDs(field string) []string {
	stripped := recordLinkRe.ReplaceAllString(field, "")
	var ids []string
	for _, m := range backtickRe.FindAllStringSubmatch(stripped, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

func recordExists(stem string, accepted map[string]Record) bool {
	for _, r := range accepted {
		if r.File == stem+".md" {
			return true
		}
	}
	return false
}

func findCycle(tasks []Task) []string {
	deps := map[string][]string{}
	for _, t := range tasks {
		deps[t.ID] = dependencyIDs(t.Fields["Dependencies"])
	}
	const (
		unvisited = iota
		visiting
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) []string
	visit = func(id string) []string {
		switch state[id] {
		case visiting:
			for i, s := range stack {
				if s == id {
					return append(append([]string{}, stack[i:]...), id)
				}
			}
		case done:
			return nil
		}
		state[id] = visiting
		stack = append(stack, id)
		for _, d := range deps[id] {
			if _, known := deps[d]; known {
				if c := visit(d); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
		return nil
	}
	for _, t := range tasks {
		if c := visit(t.ID); c != nil {
			return c
		}
	}
	return nil
}
