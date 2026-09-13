package planning

import (
	"fmt"
	"io"
	"strings"
)

// Status summarizes the open plan.
type Status struct {
	AgentTasks int
	HumanTasks int
	// Eligible lists agent tasks whose dependencies are all accepted, in plan order.
	Eligible []Task
	// WaitingHuman lists human tasks whose dependencies are all accepted.
	WaitingHuman []Task
}

// ComputeStatus derives counts and eligibility. A dependency on any open task,
// agent or human, blocks; accepted records never block.
func ComputeStatus(plan Plan) Status {
	open := map[string]bool{}
	for _, t := range plan.Tasks {
		open[t.ID] = true
	}
	var s Status
	for _, t := range plan.Tasks {
		blocked := false
		for _, id := range dependencyIDs(t.Fields["Dependencies"]) {
			if open[id] {
				blocked = true
				break
			}
		}
		if t.Lane == LaneAgent {
			s.AgentTasks++
			if !blocked {
				s.Eligible = append(s.Eligible, t)
			}
		} else {
			s.HumanTasks++
			if !blocked {
				s.WaitingHuman = append(s.WaitingHuman, t)
			}
		}
	}
	return s
}

// Print writes the human-readable status report.
func (s Status) Print(w io.Writer) {
	fmt.Fprintf(w, "open tasks: %d (%d agent, %d human)\n", s.AgentTasks+s.HumanTasks, s.AgentTasks, s.HumanTasks)
	if len(s.Eligible) == 0 {
		fmt.Fprintln(w, "next agent task: none eligible")
	} else {
		next := s.Eligible[0]
		fmt.Fprintf(w, "next agent task: %s (%s, %s)\n", next.ID, next.Capability, next.Fields["Agent status"])
	}
	if len(s.Eligible) > 1 {
		ids := make([]string, 0, len(s.Eligible)-1)
		for _, t := range s.Eligible[1:] {
			ids = append(ids, t.ID)
		}
		fmt.Fprintf(w, "also eligible: %s\n", strings.Join(ids, ", "))
	}
	for _, t := range s.WaitingHuman {
		fmt.Fprintf(w, "human action available: %s (%s)\n", t.ID, t.Fields["Human status"])
	}
}
