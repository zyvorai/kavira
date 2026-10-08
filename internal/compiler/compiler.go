package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/hypotheses"
	"github.com/zyvorai/kavira/internal/runner"
)

const Authority = "execution"

type Report struct {
	ID           string                  `json:"id"`
	Class        string                  `json:"class"`
	Service      string                  `json:"service"`
	Symptom      string                  `json:"symptom"`
	Verdict      string                  `json:"verdict"`
	Summary      string                  `json:"summary"`
	Authority    string                  `json:"authority"`
	Hypotheses   []hypotheses.Hypothesis `json:"hypotheses"`
	Experiments  []Experiment            `json:"experiments"`
	Repair       *RepairPackage          `json:"repair,omitempty"`
	Measurements map[string]any          `json:"measurements"`
	Reproduce    string                  `json:"reproduce"`
	CompiledAt   string                  `json:"compiled_at"`
	ExactReplay  bool                    `json:"exact_replay"`
}

type Experiment struct {
	Name     string `json:"name"`
	Factor   string `json:"factor"`
	Changed  string `json:"changed"`
	Result   string `json:"result"`
	Failures int    `json:"failures"`
	Attempts int    `json:"attempts"`
	P99Ms    int    `json:"p99_ms"`
	Detail   string `json:"detail"`
}

type RepairPackage struct {
	Title          string         `json:"title"`
	Patch          map[string]any `json:"patch"`
	RegressionTest RegressionTest `json:"regression_test"`
	Instructions   string         `json:"instructions"`
}

type RegressionTest struct {
	ID     string         `json:"id"`
	Class  string         `json:"class"`
	Repair map[string]any `json:"repair"`
	Expect Expect         `json:"expect"`
}

type Expect struct {
	MaxFailures int `json:"max_failures"`
	MaxP99Ms    int `json:"max_p99_ms"`
}

func Compile(b bundle.Bundle) Report {
	hs := hypotheses.Propose(b)
	rep := Report{
		ID:          b.ID,
		Class:       b.Class,
		Service:     b.Service,
		Symptom:     b.Symptom,
		Authority:   Authority,
		Hypotheses:  hs,
		ExactReplay: false,
		CompiledAt:  time.Now().UTC().Format(time.RFC3339),
		Measurements: map[string]any{
			"exact_replay": false,
			"class":        b.Class,
		},
	}
	switch b.Class {
	case bundle.ClassOOM:
		compileOOM(&rep, b)
	case bundle.ClassNetwork:
		compileNetwork(&rep, b)
	case bundle.ClassConfig:
		compileConfig(&rep, b)
	default:
		rep.Verdict = "unsupported"
		rep.Summary = "Class is not in the v0 set."
	}
	rep.Reproduce = reproduce(rep)
	return rep
}

func compileOOM(rep *Report, b bundle.Bundle) {
	limit := b.Evidence.MemoryLimitBytes
	original := runner.RunOOM(b.Workload, limit, b.Workload.Hold)
	rep.Experiments = append(rep.Experiments, exp("original", "memory_limit", "captured limit, hold as captured", original))
	rep.Measurements["original_failures"] = original.Failures
	rep.Measurements["original_p99_ms"] = original.P99Ms
	if original.Failures == 0 {
		rep.Verdict = "not_reproduced"
		rep.Summary = "Bounded workload did not fail under the captured memory limit. No repair package."
		rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "resource_contention", "not_reproduced")
		return
	}
	rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "resource_contention", "reproduced")

	raised := limit
	need := b.Workload.BytesPerRequest * int64(b.Workload.Requests)
	if need > raised {
		raised = need + need/5
	}
	raisedRun := runner.RunOOM(b.Workload, raised, true)
	rep.Experiments = append(rep.Experiments, exp("repair_raise_limit", "memory_limit", fmt.Sprintf("%d -> %d", limit, raised), raisedRun))

	unhold := runner.RunOOM(b.Workload, limit, false)
	rep.Experiments = append(rep.Experiments, exp("repair_drop_hold", "hold", "true -> false", unhold))

	bound := b.Workload.ExpectedMaxP99Ms
	if bound < 1 {
		bound = 2000
	}
	if raisedRun.Failures == 0 && raisedRun.P99Ms <= bound {
		rep.Repair = oomPackage(b, map[string]any{
			"memory_limit_bytes": raised,
			"hold":               true,
		}, bound)
		rep.Verdict = "repair_held"
		rep.Summary = "Failure reproduced under the captured limit. Raising the limit held, and latency stayed inside the bound."
		return
	}
	if unhold.Failures == 0 && unhold.P99Ms <= bound {
		rep.Repair = oomPackage(b, map[string]any{
			"memory_limit_bytes": limit,
			"hold":               false,
			"note":               "do not retain per-request buffers; shrink the batch",
		}, bound)
		rep.Verdict = "repair_held"
		rep.Summary = "Failure reproduced. Dropping the hold (batch shrink) held inside the captured limit."
		return
	}
	rep.Verdict = "repair_failed"
	rep.Summary = "Failure reproduced. Neither raising the limit nor dropping the hold held inside the latency bound."
}

func oomPackage(b bundle.Bundle, patch map[string]any, bound int) *RepairPackage {
	return &RepairPackage{
		Title: "Container OOM repair",
		Patch: patch,
		RegressionTest: RegressionTest{
			ID: b.ID, Class: b.Class, Repair: patch,
			Expect: Expect{MaxFailures: 0, MaxP99Ms: bound},
		},
		Instructions: "Re-run kavira test against this package. Do not apply the patch to production from this result alone.",
	}
}

func compileNetwork(rep *Report, b bundle.Bundle) {
	n := *b.Network
	original := runner.RunNetwork(n)
	rep.Experiments = append(rep.Experiments, exp("original", "path", "captured delay, loss, timeout, policy", original))
	rep.Measurements["original_failures"] = original.Failures
	rep.Measurements["original_p99_ms"] = original.P99Ms
	if original.Failures == 0 {
		rep.Verdict = "not_reproduced"
		rep.Summary = "Dial succeeded under captured network conditions. No repair package."
		rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "network_interruption", "not_reproduced")
		return
	}
	rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "network_interruption", "reproduced")

	repaired := n
	changed := ""
	if n.PolicyDeny {
		repaired.PolicyDeny = false
		changed = "policy_deny true -> false"
	} else if n.DelayMs >= n.TimeoutMs {
		repaired.TimeoutMs = n.DelayMs*3 + 50
		changed = fmt.Sprintf("timeout_ms %d -> %d", n.TimeoutMs, repaired.TimeoutMs)
	} else if n.LossPercent > 0 {
		repaired.LossPercent = 0
		changed = fmt.Sprintf("loss_percent %d -> 0", n.LossPercent)
	} else {
		repaired.TimeoutMs = n.TimeoutMs * 4
		changed = fmt.Sprintf("timeout_ms %d -> %d", n.TimeoutMs, repaired.TimeoutMs)
	}
	repairRun := runner.RunNetwork(repaired)
	rep.Experiments = append(rep.Experiments, exp("repair", "one factor", changed, repairRun))
	bound := n.TimeoutMs * 4
	if repaired.TimeoutMs > bound {
		bound = repaired.TimeoutMs + 50
	}
	if repairRun.Failures == 0 {
		patch := map[string]any{
			"delay_ms": repaired.DelayMs, "loss_percent": repaired.LossPercent,
			"timeout_ms": repaired.TimeoutMs, "policy_deny": repaired.PolicyDeny,
		}
		rep.Repair = &RepairPackage{
			Title: "Network timeout repair",
			Patch: patch,
			RegressionTest: RegressionTest{
				ID: b.ID, Class: b.Class, Repair: patch,
				Expect: Expect{MaxFailures: 0, MaxP99Ms: bound},
			},
			Instructions: "Repair was checked inside the isolated listener only. Confirm the same factor on the real path before merging.",
		}
		rep.Verdict = "repair_held"
		rep.Summary = "Timeouts reproduced. Changing one factor held, with connectivity restored."
		return
	}
	rep.Verdict = "repair_failed"
	rep.Summary = "Timeouts reproduced. The one-factor repair did not restore connectivity."
}

func compileConfig(rep *Report, b bundle.Bundle) {
	prev := runner.RunConfig(b.Configs.Previous)
	curr := runner.RunConfig(b.Configs.Current)
	rep.Experiments = append(rep.Experiments, exp("previous", "config", "previous snapshot", prev))
	rep.Experiments = append(rep.Experiments, exp("current", "config", "current snapshot", curr))
	rep.Measurements["previous_failures"] = prev.Failures
	rep.Measurements["current_failures"] = curr.Failures
	rep.Measurements["previous_p99_ms"] = prev.P99Ms
	rep.Measurements["current_p99_ms"] = curr.P99Ms
	if curr.Passed || curr.Failures == 0 {
		rep.Verdict = "not_reproduced"
		rep.Summary = "Current config did not fail the bounded workload. No repair package."
		rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "configuration_drift", "not_reproduced")
		return
	}
	rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "configuration_drift", "reproduced")
	reverted := runner.RunConfig(b.Configs.Previous)
	rep.Experiments = append(rep.Experiments, exp("repair_revert", "config", "current -> previous", reverted))
	bound := prev.P99Ms * 2
	if bound < 20 {
		bound = 20
	}
	if reverted.Failures == 0 && reverted.P99Ms <= bound+5 {
		patch := map[string]any{}
		for k, v := range b.Configs.Previous {
			patch[k] = v
		}
		rep.Repair = &RepairPackage{
			Title: "Configuration regression repair",
			Patch: patch,
			RegressionTest: RegressionTest{
				ID: b.ID, Class: b.Class, Repair: patch,
				Expect: Expect{MaxFailures: 0, MaxP99Ms: bound + 5},
			},
			Instructions: "Revert drifted keys to the previous snapshot and keep this regression test on the workload.",
		}
		rep.Verdict = "repair_held"
		rep.Summary = "Current config failed the same workload. Reverting to the previous snapshot held."
		return
	}
	rep.Verdict = "repair_failed"
	rep.Summary = "Current config failed. Reverting to the previous snapshot did not hold."
}

func exp(name, factor, changed string, r runner.Result) Experiment {
	result := "fail"
	if r.Passed {
		result = "pass"
	}
	return Experiment{
		Name: name, Factor: factor, Changed: changed, Result: result,
		Failures: r.Failures, Attempts: r.Attempts, P99Ms: r.P99Ms, Detail: r.Detail,
	}
}

func reproduce(rep Report) string {
	return fmt.Sprintf("kavira compile -f examples/%s.json\n# verdict=%s authority=%s exact_replay=%t\n", fileStem(rep.Class), rep.Verdict, rep.Authority, rep.ExactReplay)
}

func fileStem(class string) string {
	switch class {
	case bundle.ClassOOM:
		return "oom"
	case bundle.ClassNetwork:
		return "network-timeout"
	case bundle.ClassConfig:
		return "config-regression"
	default:
		return class
	}
}

func WritePackage(dir string, b bundle.Bundle, rep Report) error {
	if rep.Repair == nil {
		return fmt.Errorf("no repair package for %s (%s)", rep.ID, rep.Verdict)
	}
	dest := filepath.Join(dir, rep.ID)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	files := map[string]any{
		"verdict.json":         rep,
		"patch.json":           rep.Repair.Patch,
		"regression_test.json": rep.Repair.RegressionTest,
		"measurements.json":    rep.Measurements,
		"bundle.json":          b,
	}
	for name, doc := range files {
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, name), append(raw, '\n'), 0o644); err != nil {
			return err
		}
	}
	body := fmt.Sprintf("# Reproduce %s\n\n%s\n%s\n\nExact replay: no.\n", rep.ID, rep.Summary, rep.Repair.Instructions)
	return os.WriteFile(filepath.Join(dest, "REPRODUCE.md"), []byte(body), 0o644)
}

// TestRepair re-executes a regression test. It does not trust the stored verdict.
func TestRepair(b bundle.Bundle, test RegressionTest) (runner.Result, error) {
	if test.Class != b.Class {
		return runner.Result{}, fmt.Errorf("regression class %s does not match bundle %s", test.Class, b.Class)
	}
	var result runner.Result
	switch b.Class {
	case bundle.ClassOOM:
		limit := b.Evidence.MemoryLimitBytes
		hold := b.Workload.Hold
		if v, ok := test.Repair["memory_limit_bytes"]; ok {
			limit = asInt64(v)
		}
		if v, ok := test.Repair["hold"]; ok {
			hold, _ = v.(bool)
		}
		result = runner.RunOOM(b.Workload, limit, hold)
	case bundle.ClassNetwork:
		n := *b.Network
		if v, ok := asInt(test.Repair["timeout_ms"]); ok {
			n.TimeoutMs = v
		}
		if v, ok := asInt(test.Repair["loss_percent"]); ok {
			n.LossPercent = v
		}
		if v, ok := asInt(test.Repair["delay_ms"]); ok {
			n.DelayMs = v
		}
		if v, ok := test.Repair["policy_deny"].(bool); ok {
			n.PolicyDeny = v
		}
		result = runner.RunNetwork(n)
	case bundle.ClassConfig:
		cfg := map[string]string{}
		for k, v := range test.Repair {
			cfg[k] = fmt.Sprint(v)
		}
		result = runner.RunConfig(cfg)
	default:
		return runner.Result{}, fmt.Errorf("unsupported class %s", b.Class)
	}
	if result.Failures > test.Expect.MaxFailures {
		return result, fmt.Errorf("failures %d > max %d", result.Failures, test.Expect.MaxFailures)
	}
	if test.Expect.MaxP99Ms > 0 && result.P99Ms > test.Expect.MaxP99Ms {
		return result, fmt.Errorf("p99 %dms > max %dms", result.P99Ms, test.Expect.MaxP99Ms)
	}
	return result, nil
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func asInt(v any) (int, bool) {
	if v == nil {
		return 0, false
	}
	return int(asInt64(v)), true
}
