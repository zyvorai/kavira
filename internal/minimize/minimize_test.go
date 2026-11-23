// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package minimize

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/compiler"
)

func has(keep []int, want ...int) bool {
	m := map[int]bool{}
	for _, k := range keep {
		m[k] = true
	}
	for _, w := range want {
		if !m[w] {
			return false
		}
	}
	return true
}

func TestDDMinTable(t *testing.T) {
	cases := []struct {
		name string
		n    int
		need []int // failure needs all of these
		want []int
	}{
		{"single culprit", 16, []int{11}, []int{11}},
		{"two culprits", 16, []int{3, 12}, []int{3, 12}},
		{"all needed", 5, []int{0, 1, 2, 3, 4}, []int{0, 1, 2, 3, 4}},
		{"one element", 1, []int{0}, []int{0}},
		{"adjacent pair", 9, []int{4, 5}, []int{4, 5}},
	}
	for _, c := range cases {
		calls := 0
		got, complete := DDMin(c.n, func(k []int) bool { calls++; return has(k, c.need...) }, &Budget{})
		if !complete || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v complete=%v want %v", c.name, got, complete, c.want)
		}
		if calls > c.n*c.n+4 {
			t.Errorf("%s: %d oracle calls exceeds O(n^2)", c.name, calls)
		}
	}
}

func TestDDMinBudget(t *testing.T) {
	_, complete := DDMin(64, func(k []int) bool { return has(k, 40) }, &Budget{MaxRuns: 3})
	if complete {
		t.Fatal("must not claim completeness when the budget ran out")
	}
}

func load(t *testing.T, name string) bundle.Bundle {
	t.Helper()
	b, err := bundle.Load(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMinimizeOOMRequests(t *testing.T) {
	b := load(t, "oom.json") // limit 3 MiB, 1 MiB per request, hold: fails at the 4th request
	b.Workload.Requests = 12
	rep, min, err := Minimize(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if min.Workload.Requests != 4 || !rep.OneMinimal || !rep.Verified || rep.ExactReplay {
		t.Fatalf("%+v", rep)
	}
	if compiler.Compile(min).Verdict == "not_reproduced" {
		t.Fatal("minimal bundle must still reproduce")
	}
}

func TestMinimizeConfigKeys(t *testing.T) {
	b := load(t, "config-regression.json")
	prev, cur := map[string]string{}, map[string]string{}
	for k, v := range b.Configs.Previous {
		prev[k] = v
	}
	for k, v := range b.Configs.Current {
		cur[k] = v
	}
	// Noise keys that differ but are irrelevant.
	for _, k := range []string{"log_level", "region", "zone", "owner"} {
		prev[k], cur[k] = "a", "b"
	}
	b.Configs = &bundle.ConfigPair{Previous: prev, Current: cur}
	full := len(bundle.Diff(prev, cur))
	rep, min, err := Minimize(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Minimal) >= full || len(rep.Minimal) == 0 || !rep.Verified {
		t.Fatalf("expected a strict, non-empty reduction of %d: %+v", full, rep)
	}
	for _, noise := range []string{"log_level", "region", "zone", "owner"} {
		for _, m := range rep.Minimal {
			if m == noise {
				t.Fatalf("noise key %s kept: %v", noise, rep.Minimal)
			}
		}
	}
	if min.Configs.Previous["log_level"] != "a" {
		t.Fatal("previous snapshot must be untouched")
	}
}

func TestMinimizeNetworkFactors(t *testing.T) {
	b := load(t, "network-timeout.json")
	n := *b.Network
	n.LossPercent = 50
	n.DelayMs = 40
	n.TimeoutMs = 15
	b.Network = &n
	rep, min, err := Minimize(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Minimal) != 1 || !rep.Verified {
		t.Fatalf("%+v", rep)
	}
	if min.Network.TimeoutMs != 15 {
		t.Fatal("timeout is a threshold, not a removable factor")
	}
}

func TestMinimizeNotReproduced(t *testing.T) {
	b := load(t, "oom.json")
	b.Evidence.MemoryLimitBytes = 64 << 20
	_, _, err := Minimize(b, Options{})
	if !errors.Is(err, ErrNotReproduced) {
		t.Fatalf("err %v", err)
	}
}

func TestMinimizeConfigBaselineFails(t *testing.T) {
	b := load(t, "config-regression.json")
	b.Configs = &bundle.ConfigPair{
		Previous: map[string]string{"batch_size": "100000"},
		Current:  map[string]string{"batch_size": "100001"},
	}
	if _, _, err := Minimize(b, Options{}); err == nil {
		t.Fatal("a failing baseline is not change-induced")
	}
}

func TestMinimizeUnsupportedClass(t *testing.T) {
	if _, _, err := Minimize(bundle.Bundle{ID: "x", Class: "kernel_panic"}, Options{}); err == nil {
		t.Fatal("expected error")
	}
}
