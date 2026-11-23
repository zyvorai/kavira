// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package compiler

import (
	"fmt"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/hypotheses"
	"github.com/zyvorai/kavira/internal/runner"
)

// arm is one one-factor repair experiment. Arms run in order; the first one that held inside its
// latency bound becomes the repair. An arm whose changed condition would leave the bundle limits is not
// run and is not a repair.
type arm struct {
	name, factor, changed string
	invalid               error
	run                   func() runner.Result
	patch                 map[string]any
	bound                 int
}

// settle runs the arms in order, records each as an experiment, and stops at the first that held. Later
// arms are not run: the console and the server seed every fixture at start, and the runners really sleep.
func settle(rep *Report, arms []arm) *arm {
	for i := range arms {
		a := &arms[i]
		if a.invalid != nil {
			continue
		}
		res := a.run()
		rep.Experiments = append(rep.Experiments, exp(a.name, a.factor, a.changed, res))
		if res.Failures == 0 && (a.bound <= 0 || res.P99Ms <= a.bound) {
			return a
		}
	}
	return nil
}

// finish records the verdict for a class built on one-factor arms. A repair package exists only when an
// arm held.
func finish(rep *Report, b bundle.Bundle, held *arm, title, heldSummary, failedSummary string) {
	if held == nil {
		rep.Verdict = "repair_failed"
		rep.Summary = failedSummary
		return
	}
	rep.Repair = &RepairPackage{
		Title: title,
		Patch: held.patch,
		RegressionTest: RegressionTest{
			ID: b.ID, Class: b.Class, Repair: held.patch,
			Expect: Expect{MaxFailures: 0, MaxP99Ms: held.bound},
		},
		Instructions: "Repair was checked on an isolated loopback reconstruction only. Do not apply the patch to production from this result alone; confirm the same factor on the real system first.",
	}
	rep.Verdict = "repair_held"
	rep.Summary = heldSummary + " (arm " + held.name + ": " + held.changed + ")."
}

func notReproduced(rep *Report, id, summary string) {
	rep.Verdict = "not_reproduced"
	rep.Summary = summary + " No repair package."
	rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, id, "not_reproduced")
}

func compileDNS(rep *Report, b bundle.Bundle) {
	d := *b.DNS
	orig := runner.RunDNS(d)
	rep.Experiments = append(rep.Experiments, exp("original", "resolver", "captured latency, NXDOMAIN, timeout, retries, cache", orig))
	rep.Measurements["original_failures"] = orig.Failures
	rep.Measurements["original_p99_ms"] = orig.P99Ms
	if orig.Failures == 0 {
		notReproduced(rep, "dns_resolution", "Every lookup resolved under the captured resolver conditions.")
		return
	}
	rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "dns_resolution", "reproduced")

	patchOf := func(c bundle.DNSCondition) map[string]any {
		return map[string]any{
			"latency_ms": c.LatencyMs, "degraded_after": c.DegradedAfter, "nxdomain_percent": c.NXDomainPercent,
			"timeout_ms": c.TimeoutMs, "retries": c.Retries, "cache": c.Cache,
		}
	}
	mk := func(name, factor, changed string, c bundle.DNSCondition) arm {
		return arm{name: name, factor: factor, changed: changed, invalid: bundle.ValidateDNS(c),
			run: func() runner.Result { return runner.RunDNS(c) }, patch: patchOf(c), bound: c.TimeoutMs + 50}
	}
	var arms []arm
	if d.LatencyMs >= d.TimeoutMs {
		c := d
		c.TimeoutMs = d.LatencyMs*3 + 50
		arms = append(arms, mk("repair_raise_timeout", "timeout_ms", fmt.Sprintf("timeout_ms %d -> %d", d.TimeoutMs, c.TimeoutMs), c))
	}
	if !d.Cache && d.Names < d.Lookups {
		c := d
		c.Cache = true
		arms = append(arms, mk("repair_enable_cache", "cache", "cache false -> true", c))
	}
	if d.NXDomainPercent > 0 {
		c := d
		c.NXDomainPercent = 0
		arms = append(arms, mk("repair_fix_names", "nxdomain_percent", fmt.Sprintf("nxdomain_percent %d -> 0", d.NXDomainPercent), c))
	}
	finish(rep, b, settle(rep, arms), "DNS resolution repair",
		"Lookups failed under the captured resolver conditions. One changed factor held",
		"Lookups failed. None of the one-factor repair arms (longer timeout, cache, fixed names) held.")
}

func compileCert(rep *Report, b bundle.Bundle) {
	c := *b.Cert
	orig := runner.RunCert(c)
	rep.Experiments = append(rep.Experiments, exp("original", "rotation_policy", "captured lifetime, rotation window, check interval", orig))
	rep.Measurements["original_failures"] = orig.Failures
	rep.Measurements["original_p99_ms"] = orig.P99Ms
	rep.Measurements["certificates_issued"] = len(runner.CertSchedule(c))
	if orig.Failures == 0 {
		notReproduced(rep, "certificate_expiry", "Every handshake verified at its probe hour under the captured rotation policy.")
		return
	}
	rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "certificate_expiry", "reproduced")

	patchOf := func(c bundle.CertCondition) map[string]any {
		return map[string]any{
			"lifetime_hours": c.LifetimeHours, "rotation_window_hours": c.RotationWindowHours,
			"check_interval_hours": c.CheckIntervalHours,
		}
	}
	mk := func(name, factor, changed string, n bundle.CertCondition) arm {
		return arm{name: name, factor: factor, changed: changed, invalid: bundle.ValidateCert(n),
			run: func() runner.Result { return runner.RunCert(n) }, patch: patchOf(n), bound: 2000}
	}
	var arms []arm
	switch {
	case c.RotationWindowHours == 0:
		n := c
		n.RotationWindowHours = max(1, c.LifetimeHours/3)
		n.CheckIntervalHours = max(1, min(n.RotationWindowHours, c.LifetimeHours/12))
		arms = append(arms, mk("repair_enable_rotation", "rotation_window_hours",
			fmt.Sprintf("rotation off -> window %dh, check every %dh", n.RotationWindowHours, n.CheckIntervalHours), n))
	default:
		if w := min(c.LifetimeHours, 2*c.CheckIntervalHours); w > c.RotationWindowHours {
			n := c
			n.RotationWindowHours = w
			arms = append(arms, mk("repair_widen_window", "rotation_window_hours", fmt.Sprintf("rotation_window_hours %d -> %d", c.RotationWindowHours, w), n))
		}
		if c.CheckIntervalHours > c.RotationWindowHours {
			n := c
			n.CheckIntervalHours = c.RotationWindowHours
			arms = append(arms, mk("repair_shorter_interval", "check_interval_hours", fmt.Sprintf("check_interval_hours %d -> %d", c.CheckIntervalHours, n.CheckIntervalHours), n))
		}
	}
	finish(rep, b, settle(rep, arms), "Certificate rotation repair",
		"Handshakes failed at the captured probe hours. One changed rotation factor held",
		"Handshakes failed. No one-factor rotation change (wider window, shorter interval, enabling rotation) held.")
}

func compilePool(rep *Report, b bundle.Bundle) {
	p := *b.Pool
	bound := p.ExpectedMaxP99Ms
	if bound < 1 {
		bound = 2000
	}
	orig := runner.RunPool(p)
	rep.Experiments = append(rep.Experiments, exp("original", "pool", "captured pool size, acquire timeout, clients, leak", orig))
	rep.Measurements["original_failures"] = orig.Failures
	rep.Measurements["original_p99_ms"] = orig.P99Ms
	if orig.Failures == 0 {
		notReproduced(rep, "pool_exhaustion", "Every request got a connection under the captured pool settings.")
		return
	}
	rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "pool_exhaustion", "reproduced")

	patchOf := func(c bundle.PoolCondition) map[string]any {
		return map[string]any{
			"pool_size": c.PoolSize, "acquire_timeout_ms": c.AcquireTimeoutMs, "leak_every": c.LeakEvery,
		}
	}
	mk := func(name, factor, changed string, c bundle.PoolCondition) arm {
		return arm{name: name, factor: factor, changed: changed, invalid: bundle.ValidatePool(c),
			run: func() runner.Result { return runner.RunPool(c) }, patch: patchOf(c), bound: bound}
	}
	var arms []arm
	if p.LeakEvery > 0 {
		c := p
		c.LeakEvery = 0
		arms = append(arms, mk("repair_fix_leak", "leak_every", fmt.Sprintf("leak_every %d -> 0 (connections returned)", p.LeakEvery), c))
	}
	if p.PoolSize < p.Clients {
		c := p
		c.PoolSize = p.Clients
		arms = append(arms, mk("repair_raise_pool", "pool_size", fmt.Sprintf("pool_size %d -> %d", p.PoolSize, c.PoolSize), c))
		c2 := p
		waves := (p.Clients + p.PoolSize - 1) / p.PoolSize
		c2.AcquireTimeoutMs = waves*p.ServiceMs*2 + 100
		if c2.AcquireTimeoutMs > p.AcquireTimeoutMs {
			arms = append(arms, mk("repair_raise_acquire_timeout", "acquire_timeout_ms", fmt.Sprintf("acquire_timeout_ms %d -> %d", p.AcquireTimeoutMs, c2.AcquireTimeoutMs), c2))
		}
	}
	finish(rep, b, settle(rep, arms), "Connection pool repair",
		"Requests failed to acquire a connection under the captured pool settings. One changed factor held",
		"Requests failed. None of the one-factor repair arms (fix the leak, larger pool, longer acquire timeout) held inside the latency bound.")
}

func compileRetry(rep *Report, b bundle.Bundle) {
	r := *b.Retry
	bound := r.ExpectedMaxP99Ms
	if bound < 1 {
		bound = bundle.DefaultP99Bound
	}
	orig := runner.RunRetry(r)
	rep.Experiments = append(rep.Experiments, exp("original", "retry_policy", "captured backoff, jitter, retries", orig))
	rep.Measurements["original_failures"] = orig.Failures
	rep.Measurements["original_p99_ms"] = orig.P99Ms
	if orig.Failures == 0 {
		notReproduced(rep, "retry_amplification", "Every client was served within its retries under the captured retry policy.")
		return
	}
	rep.Hypotheses = hypotheses.Mark(rep.Hypotheses, "retry_amplification", "reproduced")

	patchOf := func(c bundle.RetryCondition) map[string]any {
		return map[string]any{
			"backoff_ms": c.BackoffMs, "backoff_cap_ms": c.BackoffCapMs, "jitter_percent": c.JitterPercent, "max_retries": c.MaxRetries,
		}
	}
	mk := func(name, factor, changed string, c bundle.RetryCondition) arm {
		return arm{name: name, factor: factor, changed: changed, invalid: bundle.ValidateRetry(c),
			run: func() runner.Result { return runner.RunRetry(c) }, patch: patchOf(c), bound: bound}
	}
	var arms []arm
	if r.JitterPercent < 100 {
		c := r
		c.JitterPercent = 100
		arms = append(arms, mk("repair_add_jitter", "jitter_percent", fmt.Sprintf("jitter_percent %d -> 100", r.JitterPercent), c))
		c2 := c
		if c2.BackoffCapMs == 0 {
			c2.BackoffCapMs = r.BackoffMs * 4
		}
		arms = append(arms, mk("repair_jitter_and_cap", "jitter_percent+backoff_cap_ms",
			fmt.Sprintf("jitter_percent %d -> 100, backoff_cap_ms %d -> %d", r.JitterPercent, r.BackoffCapMs, c2.BackoffCapMs), c2))
	}
	finish(rep, b, settle(rep, arms), "Retry policy repair",
		"Clients gave up under the captured retry policy. One changed factor held",
		"Clients gave up. Neither jitter nor jitter with a cap held inside the latency bound.")
}

// testClassRepair re-executes the class-specific part of a regression test. The patch overrides only the
// factors it names; everything else comes from the bundle, and the resulting condition is validated again
// because a regression file is as untrusted as a bundle.
func testClassRepair(b bundle.Bundle, test RegressionTest) (runner.Result, bool, error) {
	switch b.Class {
	case bundle.ClassDNS:
		d := *b.DNS
		setInt(test.Repair, "latency_ms", &d.LatencyMs)
		setInt(test.Repair, "degraded_after", &d.DegradedAfter)
		setInt(test.Repair, "nxdomain_percent", &d.NXDomainPercent)
		setInt(test.Repair, "timeout_ms", &d.TimeoutMs)
		setInt(test.Repair, "retries", &d.Retries)
		if v, ok := test.Repair["cache"].(bool); ok {
			d.Cache = v
		}
		if err := bundle.ValidateDNS(d); err != nil {
			return runner.Result{}, true, err
		}
		return runner.RunDNS(d), true, nil
	case bundle.ClassCert:
		c := *b.Cert
		c.ProbeHours = append([]int(nil), c.ProbeHours...)
		setInt(test.Repair, "lifetime_hours", &c.LifetimeHours)
		setInt(test.Repair, "rotation_window_hours", &c.RotationWindowHours)
		setInt(test.Repair, "check_interval_hours", &c.CheckIntervalHours)
		if err := bundle.ValidateCert(c); err != nil {
			return runner.Result{}, true, err
		}
		return runner.RunCert(c), true, nil
	case bundle.ClassPool:
		p := *b.Pool
		setInt(test.Repair, "pool_size", &p.PoolSize)
		setInt(test.Repair, "acquire_timeout_ms", &p.AcquireTimeoutMs)
		setInt(test.Repair, "leak_every", &p.LeakEvery)
		if err := bundle.ValidatePool(p); err != nil {
			return runner.Result{}, true, err
		}
		return runner.RunPool(p), true, nil
	case bundle.ClassRetry:
		r := *b.Retry
		setInt(test.Repair, "backoff_ms", &r.BackoffMs)
		setInt(test.Repair, "backoff_cap_ms", &r.BackoffCapMs)
		setInt(test.Repair, "jitter_percent", &r.JitterPercent)
		setInt(test.Repair, "max_retries", &r.MaxRetries)
		if err := bundle.ValidateRetry(r); err != nil {
			return runner.Result{}, true, err
		}
		return runner.RunRetry(r), true, nil
	}
	return runner.Result{}, false, nil
}

func setInt(m map[string]any, key string, dst *int) {
	if v, ok := asInt(m[key]); ok {
		*dst = v
	}
}
