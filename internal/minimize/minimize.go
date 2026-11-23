// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package minimize

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/compiler"
	"github.com/zyvorai/kavira/internal/runner"
)

const (
	DefaultMaxRuns = 200
	DefaultBudget  = 20 * time.Second
)

// ErrNotReproduced: there is no failure to minimize. Execution decided, so there is no minimal reproducer.
var ErrNotReproduced = errors.New("the full captured condition did not fail when executed; nothing to minimize")

// Report is what execution found. It never claims the minimal input is the production cause.
type Report struct {
	ID          string   `json:"id"`
	Class       string   `json:"class"`
	Kind        string   `json:"kind"` // config_keys | network_factors | workload_requests
	Original    []string `json:"original"`
	Minimal     []string `json:"minimal"`
	Removed     []string `json:"removed"`
	Runs        int      `json:"runs"`
	OneMinimal  bool     `json:"one_minimal"` // search finished inside the budget
	Verified    bool     `json:"verified"`    // the minimal input failed again when re-executed
	Summary     string   `json:"summary"`
	Authority   string   `json:"authority"`
	ExactReplay bool     `json:"exact_replay"`
	Note        string   `json:"note"`
}

type Options struct {
	MaxRuns int
	Budget  time.Duration
}

// Minimize returns the report and a bundle holding only the minimal failing input.
// The bundle must be compiled again for a verdict: minimizing is not a repair.
func Minimize(b bundle.Bundle, o Options) (Report, bundle.Bundle, error) {
	if o.MaxRuns < 1 || o.MaxRuns > 1000 {
		o.MaxRuns = DefaultMaxRuns
	}
	if o.Budget <= 0 || o.Budget > time.Minute {
		o.Budget = DefaultBudget
	}
	bud := &Budget{MaxRuns: o.MaxRuns, Deadline: time.Now().Add(o.Budget)}
	rep := Report{
		ID: b.ID, Class: b.Class, Authority: compiler.Authority, ExactReplay: false,
		Note: "Minimal means 1-minimal for this reconstruction: removing any one remaining element stopped the failure in this run. It is not a global minimum, and it does not show what caused the production outage. Do not apply a patch to production from this result alone.",
	}
	var (
		names []string
		fails func(keep []int) bool
		build func(keep []int) bundle.Bundle
	)
	switch b.Class {
	case bundle.ClassConfig:
		rep.Kind = "config_keys"
		diff := bundle.Diff(b.Configs.Previous, b.Configs.Current)
		for k := range diff {
			names = append(names, k)
		}
		sort.Strings(names)
		build = func(keep []int) bundle.Bundle {
			nb := b
			cfg := map[string]string{}
			for k, v := range b.Configs.Previous {
				cfg[k] = v
			}
			for _, i := range keep {
				k := names[i]
				if v, ok := b.Configs.Current[k]; ok {
					cfg[k] = v
				} else {
					delete(cfg, k)
				}
			}
			nb.Configs = &bundle.ConfigPair{Previous: b.Configs.Previous, Current: cfg}
			return nb
		}
		fails = func(keep []int) bool { return runner.RunConfig(build(keep).Configs.Current).Failures > 0 }
		if len(names) > 0 && runner.RunConfig(b.Configs.Previous).Failures > 0 {
			return rep, b, errors.New("the previous config already fails the workload; the failure is not change-induced, so a config diff cannot be minimized")
		}
	case bundle.ClassNetwork:
		rep.Kind = "network_factors"
		n := *b.Network
		var apply []func(*bundle.NetworkCondition)
		if n.DelayMs > 0 {
			names = append(names, fmt.Sprintf("delay_ms=%d", n.DelayMs))
			apply = append(apply, func(c *bundle.NetworkCondition) { c.DelayMs = n.DelayMs })
		}
		if n.LossPercent > 0 {
			names = append(names, fmt.Sprintf("loss_percent=%d", n.LossPercent))
			apply = append(apply, func(c *bundle.NetworkCondition) { c.LossPercent = n.LossPercent })
		}
		if n.PolicyDeny {
			names = append(names, "policy_deny=true")
			apply = append(apply, func(c *bundle.NetworkCondition) { c.PolicyDeny = true })
		}
		build = func(keep []int) bundle.Bundle {
			c := n
			c.DelayMs, c.LossPercent, c.PolicyDeny = 0, 0, false
			for _, i := range keep {
				apply[i](&c)
			}
			nb := b
			nb.Network = &c
			return nb
		}
		fails = func(keep []int) bool { return runner.RunNetwork(*build(keep).Network).Failures > 0 }
	case bundle.ClassOOM, bundle.ClassDisk:
		rep.Kind = "workload_requests"
		for i := 0; i < b.Workload.Requests; i++ {
			names = append(names, fmt.Sprintf("request %d", i+1))
		}
		build = func(keep []int) bundle.Bundle {
			nb := b
			nb.Workload.Requests = len(keep)
			return nb
		}
		limit := b.Evidence.MemoryLimitBytes
		if b.Class == bundle.ClassDisk {
			limit = b.Evidence.DiskLimitBytes
		}
		fails = func(keep []int) bool {
			w := build(keep).Workload
			if b.Class == bundle.ClassDisk {
				return runner.RunDisk(w, limit, w.Hold).Failures > 0
			}
			return runner.RunOOM(w, limit, w.Hold).Failures > 0
		}
	default:
		return rep, b, fmt.Errorf("unsupported class %q", b.Class)
	}
	rep.Original = append([]string{}, names...)
	if len(names) == 0 {
		return rep, b, errors.New("no removable factor: nothing in this bundle differs from a clean condition")
	}
	all := make([]int, len(names))
	for i := range all {
		all[i] = i
	}
	bud.Used++
	if !fails(all) {
		rep.Runs = bud.Used
		rep.Summary = "The full captured condition did not fail when executed. Nothing to minimize, and no verdict beyond not_reproduced."
		return rep, b, ErrNotReproduced
	}
	keep, complete := DDMin(len(names), fails, bud)
	rep.Runs = bud.Used
	rep.OneMinimal = complete
	for _, i := range keep {
		rep.Minimal = append(rep.Minimal, names[i])
	}
	kept := map[int]bool{}
	for _, i := range keep {
		kept[i] = true
	}
	for i, n := range names {
		if !kept[i] {
			rep.Removed = append(rep.Removed, n)
		}
	}
	min := build(keep)
	rep.Verified = fails(keep)
	rep.Runs++
	switch {
	case !rep.Verified:
		rep.Summary = "The reduced input did not fail when re-executed: the failure is flaky at this size. Run kavira repeat before trusting any reduction."
	case !complete:
		rep.Summary = fmt.Sprintf("Reduced %d to %d elements before the budget ran out. Not proven 1-minimal.", len(names), len(keep))
	default:
		rep.Summary = fmt.Sprintf("Reduced %d to %d element(s) in %d runs; each remaining element is needed for the failure to reproduce in this reconstruction.", len(names), len(keep), rep.Runs)
	}
	return rep, min, nil
}
