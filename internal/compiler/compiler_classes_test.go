// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package compiler

import (
	"strings"
	"testing"

	"github.com/zyvorai/kavira/internal/bundle"
)

var classExamples = []struct{ file, class, arm string }{
	{"dns-failure.json", bundle.ClassDNS, "repair_raise_timeout"},
	{"certificate-expiry.json", bundle.ClassCert, "repair_widen_window"},
	{"connection-pool.json", bundle.ClassPool, "repair_raise_pool"},
	{"retry-storm.json", bundle.ClassRetry, "repair_add_jitter"},
}

func TestNewClassesRepairHeld(t *testing.T) {
	for _, c := range classExamples {
		b := load(t, c.file)
		rep := Compile(b)
		if rep.Verdict != "repair_held" || rep.Repair == nil || rep.ExactReplay || rep.Authority != "execution" || rep.Class != c.class {
			t.Fatalf("%s: %s %+v", c.file, rep.Verdict, rep.Summary)
		}
		if !strings.Contains(rep.Summary, c.arm) {
			t.Errorf("%s: summary should name the arm %s: %s", c.file, c.arm, rep.Summary)
		}
		if !strings.Contains(rep.Repair.Instructions, "Do not apply the patch to production") {
			t.Errorf("%s: instructions must forbid applying the patch from this result alone", c.file)
		}
		if rep.Experiments[0].Name != "original" || rep.Experiments[0].Result != "fail" {
			t.Errorf("%s: original arm should fail: %+v", c.file, rep.Experiments[0])
		}
		for _, h := range rep.Hypotheses {
			if h.Authoritative {
				t.Errorf("%s: hypothesis %s claims authority", c.file, h.ID)
			}
		}
		if !strings.Contains(rep.Reproduce, "examples/") || strings.Contains(strings.ToLower(rep.Summary), "replay") {
			t.Errorf("%s: reproduce/summary copy: %q %q", c.file, rep.Reproduce, rep.Summary)
		}
		if _, err := TestRepair(b, rep.Repair.RegressionTest); err != nil {
			t.Errorf("%s: regression test must pass: %v", c.file, err)
		}
	}
}

// A symptom that does not reproduce gets no repair package, for every new class.
func TestNewClassesNotReproduced(t *testing.T) {
	mutate := map[string]func(*bundle.Bundle){
		"dns-failure.json":        func(b *bundle.Bundle) { b.DNS.LatencyMs = 0 },
		"certificate-expiry.json": func(b *bundle.Bundle) { b.Cert.ProbeHours = []int{10, 20} },
		"connection-pool.json":    func(b *bundle.Bundle) { b.Pool.PoolSize = b.Pool.Clients },
		"retry-storm.json":        func(b *bundle.Bundle) { b.Retry.JitterPercent = 100 },
	}
	for _, c := range classExamples {
		b := load(t, c.file)
		mutate[c.file](&b)
		rep := Compile(b)
		if rep.Verdict != "not_reproduced" || rep.Repair != nil {
			t.Errorf("%s: %s repair=%v", c.file, rep.Verdict, rep.Repair)
		}
		if err := WritePackage(t.TempDir(), b, rep); err == nil {
			t.Errorf("%s: a package must not be written for not_reproduced", c.file)
		}
	}
}

// When no one-factor arm holds the verdict is repair_failed and there is no package.
func TestNewClassesRepairFailed(t *testing.T) {
	b := load(t, "dns-failure.json")
	b.DNS.NXDomainPercent = 50 // names that do not exist; a longer timeout or a cache cannot fix them
	b.DNS.LatencyMs = 0
	b.DNS.TimeoutMs = 200
	b.DNS.Cache = true
	b.DNS.Names = 8 // nothing to cache twice
	rep := Compile(b)
	if rep.Verdict != "repair_held" {
		// fix_names applies: the one-factor repair is to correct the names.
		t.Fatalf("fixing the names should hold: %s", rep.Verdict)
	}
	b.DNS.LatencyMs = 500
	b.DNS.TimeoutMs = 20
	b.DNS.NXDomainPercent = 50
	b.DNS.Retries = 0
	rep = Compile(b)
	if rep.Verdict != "repair_failed" || rep.Repair != nil {
		t.Fatalf("two independent causes need two factors, so one-factor arms must fail: %s", rep.Verdict)
	}
}

func TestPoolLeakRepair(t *testing.T) {
	b := load(t, "connection-pool.json")
	b.Pool.PoolSize = 4
	b.Pool.Clients = 4
	b.Pool.RequestsPerClient = 6
	b.Pool.ServiceMs = 5
	b.Pool.AcquireTimeoutMs = 60
	b.Pool.LeakEvery = 3
	rep := Compile(b)
	if rep.Verdict != "repair_held" || rep.Repair.Patch["leak_every"] != 0 {
		t.Fatalf("a leak is repaired by returning connections: %s %v", rep.Verdict, rep.Repair)
	}
}

func TestDNSCacheRepairWhenTimeoutCannotBeRaised(t *testing.T) {
	b := load(t, "dns-failure.json")
	// 20 s of latency: the raised timeout would leave the bounded limits, so only the cache arm runs to a result.
	b.DNS.LatencyMs = 20000
	b.DNS.TimeoutMs = 60
	b.DNS.Retries = 0
	b.DNS.DegradedAfter = 2
	b.DNS.Names = 2
	b.DNS.Lookups = 6
	rep := Compile(b)
	if rep.Verdict != "repair_held" || rep.Repair.Patch["cache"] != true {
		t.Fatalf("%s %s", rep.Verdict, rep.Summary)
	}
	for _, e := range rep.Experiments {
		if e.Name == "repair_raise_timeout" {
			t.Fatal("an arm that leaves the bounded limits must not run")
		}
	}
}

// A regression file is untrusted like a bundle: a repaired condition that leaves the limits is refused.
func TestTestRepairRejectsHostilePatches(t *testing.T) {
	cases := []struct {
		file  string
		patch map[string]any
	}{
		{"dns-failure.json", map[string]any{"retries": 1e9}},
		{"dns-failure.json", map[string]any{"timeout_ms": -5}},
		{"certificate-expiry.json", map[string]any{"lifetime_hours": 1e12}},
		{"certificate-expiry.json", map[string]any{"rotation_window_hours": 99999, "check_interval_hours": 1}},
		{"connection-pool.json", map[string]any{"pool_size": 1e9}},
		{"connection-pool.json", map[string]any{"acquire_timeout_ms": 1e9}},
		{"retry-storm.json", map[string]any{"jitter_percent": 1000}},
		{"retry-storm.json", map[string]any{"backoff_ms": 1e9}},
		{"network-timeout.json", map[string]any{"bandwidth_bps": -1}},
		{"network-timeout.json", map[string]any{"response_bytes": 1e12}},
	}
	for _, c := range cases {
		b := load(t, c.file)
		test := RegressionTest{ID: b.ID, Class: b.Class, Repair: c.patch, Expect: Expect{MaxFailures: 0, MaxP99Ms: 5000}}
		if _, err := TestRepair(b, test); err == nil {
			t.Errorf("%s %v: accepted", c.file, c.patch)
		}
	}
	// Class mismatch and unknown class still refused.
	b := load(t, "dns-failure.json")
	if _, err := TestRepair(b, RegressionTest{Class: bundle.ClassPool}); err == nil {
		t.Error("class mismatch accepted")
	}
	b.Class = "kernel_panic"
	if _, err := TestRepair(b, RegressionTest{Class: "kernel_panic"}); err == nil {
		t.Error("unknown class accepted")
	}
}

func TestNetworkArmRepairs(t *testing.T) {
	base := func() bundle.Bundle { return load(t, "network-timeout.json") }
	cases := []struct {
		name  string
		mut   func(n *bundle.NetworkCondition)
		patch string
	}{
		{"reset after bytes", func(n *bundle.NetworkCondition) {
			n.DelayMs, n.TimeoutMs, n.ResponseBytes, n.ResetAfterBytes = 0, 300, 500, 50
		}, "reset_after_bytes 50 -> 0"},
		{"bandwidth cap", func(n *bundle.NetworkCondition) {
			n.DelayMs, n.TimeoutMs, n.ResponseBytes, n.BandwidthBps = 0, 100, 1000, 4000
		}, "timeout_ms 100 ->"},
		{"slow close", func(n *bundle.NetworkCondition) {
			n.DelayMs, n.TimeoutMs, n.SlowCloseMs = 0, 40, 200
		}, "timeout_ms 40 ->"},
	}
	for _, c := range cases {
		b := base()
		n := *b.Network
		n.Attempts = 2
		c.mut(&n)
		b.Network = &n
		rep := Compile(b)
		if rep.Verdict != "repair_held" {
			t.Errorf("%s: %s %s", c.name, rep.Verdict, rep.Summary)
			continue
		}
		var changed string
		for _, e := range rep.Experiments {
			if e.Name == "repair" {
				changed = e.Changed
			}
		}
		if !strings.Contains(changed, c.patch) {
			t.Errorf("%s: changed %q want %q", c.name, changed, c.patch)
		}
		if _, err := TestRepair(b, rep.Repair.RegressionTest); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
