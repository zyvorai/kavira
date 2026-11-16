package compiler

import (
	"fmt"
	"strings"
)

// Summarize returns a human-readable one-pager for a compile report.
// It does not re-run experiments; it only formats what execution already decided.
func Summarize(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "incident  %s\n", rep.ID)
	fmt.Fprintf(&b, "class     %s\n", rep.Class)
	fmt.Fprintf(&b, "service   %s\n", rep.Service)
	fmt.Fprintf(&b, "verdict   %s\n", rep.Verdict)
	fmt.Fprintf(&b, "authority %s\n", rep.Authority)
	fmt.Fprintf(&b, "exact_replay %t\n", rep.ExactReplay)
	if rep.Summary != "" {
		fmt.Fprintf(&b, "\n%s\n", rep.Summary)
	}
	if len(rep.Experiments) > 0 {
		fmt.Fprintf(&b, "\nexperiments\n")
		for _, e := range rep.Experiments {
			fmt.Fprintf(&b, "  - %s [%s] %s → %s (failures=%d/%d p99=%dms)\n",
				e.Name, e.Factor, e.Changed, e.Result, e.Failures, e.Attempts, e.P99Ms)
		}
	}
	if rep.Repair != nil {
		fmt.Fprintf(&b, "\nrepair    %s\n", rep.Repair.Title)
		fmt.Fprintf(&b, "patch     %v\n", rep.Repair.Patch)
		fmt.Fprintf(&b, "retest    kavira test -f <package>/regression_test.json\n")
		fmt.Fprintf(&b, "note      %s\n", rep.Repair.Instructions)
	} else {
		fmt.Fprintf(&b, "\nrepair    (none — execution did not confirm a repair)\n")
	}
	return b.String()
}
