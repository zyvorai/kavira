// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package confidence

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/compiler"
)

func TestWilsonKnownValues(t *testing.T) {
	cases := []struct {
		k, n   int
		lo, hi float64
		tol    float64
	}{
		{0, 20, 0, 0.1611, 0.001}, // rule of three-ish: ~16% upper bound
		{20, 20, 0.8389, 1, 0.001},
		{5, 10, 0.2366, 0.7634, 0.001},
		{1, 10, 0.0179, 0.4041, 0.001},
		{0, 0, 0, 1, 0},
	}
	for _, c := range cases {
		lo, hi := Wilson(c.k, c.n)
		if math.Abs(lo-c.lo) > c.tol || math.Abs(hi-c.hi) > c.tol {
			t.Errorf("Wilson(%d,%d) = %.4f,%.4f want %.4f,%.4f", c.k, c.n, lo, hi, c.lo, c.hi)
		}
		if lo < 0 || hi > 1 || lo > hi {
			t.Errorf("Wilson(%d,%d) out of range: %v %v", c.k, c.n, lo, hi)
		}
	}
}

func fake(verdicts ...string) func(bundle.Bundle) compiler.Report {
	i := 0
	return func(bundle.Bundle) compiler.Report {
		v := verdicts[i%len(verdicts)]
		i++
		return compiler.Report{Verdict: v}
	}
}

func TestClassification(t *testing.T) {
	cases := []struct {
		name     string
		runs     int
		verdicts []string
		want     string
		repair   string
	}{
		{"all clean enough", 20, []string{"not_reproduced"}, "not_reproduced", "none"},
		{"clean but too few", 5, []string{"not_reproduced"}, "inconclusive", "none"},
		{"always reproduced, repair held", 10, []string{"repair_held"}, "reproduced", "held_every_run"},
		{"always reproduced, repair failed", 10, []string{"repair_failed"}, "reproduced", "not_demonstrated"},
		{"flaky", 10, []string{"repair_held", "not_reproduced"}, "flaky", "held_every_run"},
		{"mixed repair", 10, []string{"repair_held", "repair_failed"}, "reproduced", "mixed"},
		{"runs clamped low", 0, []string{"repair_held"}, "reproduced", "held_every_run"},
	}
	for _, c := range cases {
		r := RunWith(bundle.Bundle{ID: "x"}, c.runs, 0, fake(c.verdicts...))
		if r.Reproduction != c.want || r.Repair != c.repair {
			t.Errorf("%s: got %s/%s want %s/%s (%s)", c.name, r.Reproduction, r.Repair, c.want, c.repair, r.Summary)
		}
		if r.ExactReplay || r.Authority != "execution" {
			t.Errorf("%s: boundary fields wrong", c.name)
		}
	}
}

func TestRunsCapped(t *testing.T) {
	r := RunWith(bundle.Bundle{ID: "x"}, 1_000_000, 0, fake("repair_held"))
	if r.Completed != MaxRuns || r.Requested != MaxRuns {
		t.Fatalf("completed %d requested %d", r.Completed, r.Requested)
	}
}

func TestBudgetTruncates(t *testing.T) {
	slow := func(bundle.Bundle) compiler.Report {
		time.Sleep(15 * time.Millisecond)
		return compiler.Report{Verdict: "repair_held"}
	}
	r := RunWith(bundle.Bundle{ID: "x"}, 100, 40*time.Millisecond, slow)
	if !r.Truncated || r.Completed < 1 || r.Completed >= 100 {
		t.Fatalf("truncated=%v completed=%d", r.Truncated, r.Completed)
	}
}

func TestRealFixtureIsStable(t *testing.T) {
	b, err := bundle.Load(filepath.Join("..", "..", "examples", "oom.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := Run(b, 5, 10*time.Second)
	if r.Reproduction != "reproduced" || r.Repair != "held_every_run" || r.Reproduced.Successes != 5 {
		t.Fatalf("%+v", r)
	}
}
