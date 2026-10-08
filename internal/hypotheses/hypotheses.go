package hypotheses

import (
	"fmt"

	"github.com/zyvorai/kavira/internal/bundle"
)

// Hypothesis is a proposed explanation. Authoritative is always false:
// execution, not the proposer, decides.
type Hypothesis struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Explanation   string   `json:"explanation"`
	Evidence      []string `json:"evidence"`
	Experiment    string   `json:"experiment"`
	Rank          int      `json:"rank"`
	Status        string   `json:"status"`
	Authoritative bool     `json:"authoritative"`
}

func Propose(b bundle.Bundle) []Hypothesis {
	var out []Hypothesis
	rank := 1
	add := func(h Hypothesis) {
		h.Rank = rank
		h.Status = "untested"
		h.Authoritative = false
		rank++
		out = append(out, h)
	}

	switch b.Class {
	case bundle.ClassOOM:
		add(Hypothesis{
			ID:   "resource_contention",
			Name: "Resource contention",
			Explanation: fmt.Sprintf(
				"Captured peak %d bytes against limit %d, with %d restarts. A bounded workload held under that limit should fail the same way if the limit is the cause.",
				b.Evidence.ObservedPeakBytes, b.Evidence.MemoryLimitBytes, b.Evidence.RestartCount,
			),
			Evidence: []string{
				fmt.Sprintf("memory_limit_bytes=%d", b.Evidence.MemoryLimitBytes),
				fmt.Sprintf("observed_peak_bytes=%d", b.Evidence.ObservedPeakBytes),
				fmt.Sprintf("restart_count=%d", b.Evidence.RestartCount),
			},
			Experiment: "Replay the bounded workload under the captured memory limit, holding allocations.",
		})
	case bundle.ClassNetwork:
		n := b.Network
		add(Hypothesis{
			ID:   "network_interruption",
			Name: "Network interruption",
			Explanation: fmt.Sprintf(
				"Captured delay %dms, loss %d%%, timeout %dms, policy_deny=%t. A local dial under those conditions should time out or reset if the path is the cause.",
				n.DelayMs, n.LossPercent, n.TimeoutMs, n.PolicyDeny,
			),
			Evidence: []string{
				fmt.Sprintf("delay_ms=%d", n.DelayMs),
				fmt.Sprintf("loss_percent=%d", n.LossPercent),
				fmt.Sprintf("timeout_ms=%d", n.TimeoutMs),
				fmt.Sprintf("policy_deny=%t", n.PolicyDeny),
			},
			Experiment: "Recreate selected delay, loss, or policy against a local listener. One factor changes in the repair arm.",
		})
	case bundle.ClassConfig:
		add(Hypothesis{
			ID:          "configuration_drift",
			Name:        "Configuration drift",
			Explanation: "Previous and current configs differ. The same workload run against both should diverge if the drift is the cause.",
			Evidence:    []string{"previous config snapshot", "current config snapshot"},
			Experiment:  "Run previous and current configurations against the same bounded workload.",
		})
	}

	add(Hypothesis{
		ID:          "deploy_change",
		Name:        "Deployment change",
		Explanation: "A migration or rollout is in the evidence. Treated as context, not a verdict, until an experiment isolates it.",
		Evidence:    []string{emptyAs(b.Evidence.DeployChange, "no deploy change recorded")},
		Experiment:  "Not run in v0. Declared class owns the experiment.",
	})
	return out
}

func emptyAs(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func Mark(hs []Hypothesis, id, status string) []Hypothesis {
	out := make([]Hypothesis, len(hs))
	copy(out, hs)
	for i := range out {
		if out[i].ID == id {
			out[i].Status = status
		}
	}
	return out
}
