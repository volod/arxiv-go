// Package planning checks the agreement between the specification registry,
// the forward plan and the task records, and reports the next eligible task.
// It is repository tooling and is never linked into the arxgo binary.
package planning

import (
	"bufio"
	"regexp"
	"strings"
)

// Lane names of the plan's top-level sections.
const (
	LaneAgent = "agent"
	LaneHuman = "human"
)

// Section headings that open each lane in plan.md.
const (
	agentLaneHeading = "## Agent Implementation Tasks"
	humanLaneHeading = "## Human-Assisted Tasks"
)

// Task is one "#### task-id" block of the plan.
type Task struct {
	ID         string
	Capability string
	Lane       string
	Line       int
	Fields     map[string]string
}

// Group is one "### Title -- `capability`" heading inside a lane.
type Group struct {
	Capability string
	Lane       string
	Line       int
}

// Plan is the parsed forward plan.
type Plan struct {
	Groups []Group
	Tasks  []Task
}

// Capability is one row of the specification registry.
type Capability struct {
	ID     string
	Status string
}

var (
	groupHeadingRe = regexp.MustCompile("^### .+ -- `([a-z0-9-]+)`\\s*$")
	taskHeadingRe  = regexp.MustCompile(`^#### ([a-z0-9-]+)(\s+\(optional\))?\s*$`)
	fieldRe        = regexp.MustCompile(`^- ([A-Z][A-Za-z -]+): ?(.*)$`)
	backtickRe     = regexp.MustCompile("`([a-z0-9][a-z0-9-]*)`")
)

// ParsePlan reads plan.md content.
func ParsePlan(text string) Plan {
	var plan Plan
	lane := ""
	capability := ""
	var cur *Task
	field := ""
	flush := func() {
		if cur != nil {
			plan.Tasks = append(plan.Tasks, *cur)
		}
		cur, field = nil, ""
	}
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "## "):
			flush()
			capability = ""
			switch strings.TrimSpace(line) {
			case agentLaneHeading:
				lane = LaneAgent
			case humanLaneHeading:
				lane = LaneHuman
			default:
				lane = ""
			}
		case strings.HasPrefix(line, "### "):
			flush()
			capability = ""
			if m := groupHeadingRe.FindStringSubmatch(line); m != nil && lane != "" {
				capability = m[1]
				plan.Groups = append(plan.Groups, Group{Capability: capability, Lane: lane, Line: n})
			}
		case strings.HasPrefix(line, "#### "):
			flush()
			if m := taskHeadingRe.FindStringSubmatch(line); m != nil && lane != "" {
				cur = &Task{ID: m[1], Capability: capability, Lane: lane, Line: n, Fields: map[string]string{}}
			}
		case cur != nil:
			if m := fieldRe.FindStringSubmatch(line); m != nil {
				field = m[1]
				cur.Fields[field] = strings.TrimSpace(m[2])
			} else if field != "" && strings.TrimSpace(line) != "" {
				cur.Fields[field] = strings.TrimSpace(cur.Fields[field] + " " + strings.TrimSpace(line))
			} else if strings.TrimSpace(line) == "" {
				field = ""
			}
		}
	}
	flush()
	return plan
}

// ParseRegistry reads the "## Capability Registry" table of spec.md. The
// capability id is the first backticked cell; status is the column headed
// "Status".
func ParseRegistry(text string) []Capability {
	var caps []Capability
	in := false
	statusCol := -1
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.TrimSpace(line) == "## Capability Registry"
			continue
		}
		if !in || !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitRow(line)
		if statusCol < 0 {
			for i, c := range cells {
				if c == "Status" {
					statusCol = i
				}
			}
			continue
		}
		if statusCol >= len(cells) {
			continue
		}
		for _, c := range cells {
			if m := backtickRe.FindStringSubmatch(c); m != nil && m[0] == c {
				caps = append(caps, Capability{ID: m[1], Status: cells[statusCol]})
				break
			}
		}
	}
	return caps
}

// ParseGroupTable reads the capability-to-record-group table from the
// planning workflow: rows of the form "| `capability` | `abbrev` |".
func ParseGroupTable(text string) map[string]string {
	groups := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitRow(line)
		if len(cells) != 2 {
			continue
		}
		c := backtickRe.FindStringSubmatch(cells[0])
		a := backtickRe.FindStringSubmatch(cells[1])
		if c != nil && a != nil {
			groups[c[1]] = a[1]
		}
	}
	return groups
}

func splitRow(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
