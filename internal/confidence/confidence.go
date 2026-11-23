// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

// Package confidence repeats the whole compile pipeline and reports how often the failure reproduced
// and how often the repair held, with a Wilson score interval, instead of one yes/no.
//
// There is deliberately no random seed: the runners contain no random source (OOM and disk are
// arithmetic, network loss is selected, config is clock-bounded). What varies between runs is wall-clock
// timing, so a seed would only pretend to control it.
package confidence

import (
	"fmt"
	"math"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/compiler"
)

const (
	DefaultRuns = 20
	MaxRuns     = 100
	// MinCleanRuns is how many runs must ALL stay clean before "not_reproduced" is reported.
	// With 20 clean runs the 95% Wilson upper bound on the reproduction rate is about 16%.
	MinCleanRuns = 20
	z95          = 1.959963984540054
)

// Tally is successes out of trials with a 95% Wilson score interval.
type Tally struct {
	Successes  int     `json:"successes"`
	Trials     int     `json:"trials"`
	Rate       float64 `json:"rate"`
	WilsonLow  float64 `json:"wilson_low"`
	WilsonHigh float64 `json:"wilson_high"`
}

type Report struct {
	ID        string `json:"id"`
	Class     string `json:"class"`
	Requested int    `json:"requested_runs"`
	Completed int    `json:"completed_runs"`
	Truncated bool   `json:"truncated"`
	// Reproduced counts runs where the symptom reproduced; RepairHeld counts, among those, runs where the repair held.
	Reproduced Tally `json:"reproduced"`
	RepairHeld Tally `json:"repair_held"`
	// Reproduction is reproduced | flaky | not_reproduced | inconclusive.
	Reproduction string         `json:"reproduction"`
	Repair       string         `json:"repair"` // held_every_run | mixed | not_demonstrated | none
	Verdicts     map[string]int `json:"verdicts"`
	Summary      string         `json:"summary"`
	Authority    string         `json:"authority"`
	ExactReplay  bool           `json:"exact_replay"`
	Note         string         `json:"note"`
}

// Wilson returns the 95% Wilson score interval for k successes in n trials. n == 0 gives [0, 1].
func Wilson(k, n int) (low, high float64) {
	if n <= 0 {
		return 0, 1
	}
	p := float64(k) / float64(n)
	nf := float64(n)
	den := 1 + z95*z95/nf
	centre := (p + z95*z95/(2*nf)) / den
	half := z95 * math.Sqrt(p*(1-p)/nf+z95*z95/(4*nf*nf)) / den
	low, high = centre-half, centre+half
	if low < 0 {
		low = 0
	}
	if high > 1 {
		high = 1
	}
	// Exact endpoints at the extremes (floating point leaves dust).
	if k == 0 {
		low = 0
	}
	if k == n {
		high = 1
	}
	return low, high
}

func tally(k, n int) Tally {
	t := Tally{Successes: k, Trials: n}
	if n > 0 {
		t.Rate = float64(k) / float64(n)
	}
	t.WilsonLow, t.WilsonHigh = Wilson(k, n)
	return t
}

// Run compiles b up to runs times (clamped to 1..MaxRuns), stopping early at the deadline.
func Run(b bundle.Bundle, runs int, budget time.Duration) Report {
	return RunWith(b, runs, budget, compiler.Compile)
}

// RunWith is Run with an injectable compile function, for tests.
func RunWith(b bundle.Bundle, runs int, budget time.Duration, compile func(bundle.Bundle) compiler.Report) Report {
	if runs < 1 {
		runs = DefaultRuns
	}
	if runs > MaxRuns {
		runs = MaxRuns
	}
	rep := Report{
		ID: b.ID, Class: b.Class, Requested: runs, Verdicts: map[string]int{},
		Authority: compiler.Authority, ExactReplay: false,
		Note: "Each run is a fresh reconstruction under the captured conditions. This measures how stable the reconstruction is, not how often production fails. Do not apply a patch to production from this result alone.",
	}
	var deadline time.Time
	if budget > 0 {
		deadline = time.Now().Add(budget)
	}
	reproduced, held := 0, 0
	for i := 0; i < runs; i++ {
		if !deadline.IsZero() && i > 0 && time.Now().After(deadline) {
			rep.Truncated = true
			break
		}
		r := compile(b)
		rep.Completed++
		rep.Verdicts[r.Verdict]++
		switch r.Verdict {
		case "repair_held":
			reproduced++
			held++
		case "repair_failed":
			reproduced++
		}
	}
	rep.Reproduced = tally(reproduced, rep.Completed)
	rep.RepairHeld = tally(held, reproduced)
	rep.Reproduction = classify(reproduced, rep.Completed)
	switch {
	case reproduced == 0:
		rep.Repair = "none"
	case held == reproduced:
		rep.Repair = "held_every_run"
	case held == 0:
		rep.Repair = "not_demonstrated"
	default:
		rep.Repair = "mixed"
	}
	rep.Summary = summary(rep)
	return rep
}

func classify(reproduced, completed int) string {
	switch {
	case completed == 0:
		return "inconclusive"
	case reproduced == 0 && completed >= MinCleanRuns:
		return "not_reproduced"
	case reproduced == 0:
		return "inconclusive"
	case reproduced == completed:
		return "reproduced"
	default:
		return "flaky"
	}
}

func pct(x float64) string { return fmt.Sprintf("%.0f%%", x*100) }

func summary(r Report) string {
	re := r.Reproduced
	span := fmt.Sprintf("%d/%d runs reproduced the symptom (95%% Wilson interval %s to %s)", re.Successes, re.Trials, pct(re.WilsonLow), pct(re.WilsonHigh))
	switch r.Reproduction {
	case "not_reproduced":
		return fmt.Sprintf("%s. Clean in all %d runs, so not_reproduced stands for this reconstruction. No repair package.", span, r.Completed)
	case "inconclusive":
		return fmt.Sprintf("%s. Fewer than %d clean runs: not enough to call it not_reproduced. Run more.", span, MinCleanRuns)
	case "flaky":
		return fmt.Sprintf("%s. The reconstruction is flaky: a single verdict would have been luck. Repair: %s.", span, r.Repair)
	}
	s := fmt.Sprintf("%s. Stable in this reconstruction.", span)
	if r.Repair == "held_every_run" {
		s += fmt.Sprintf(" The repair held in %d/%d reproduced runs (lower bound %s).", r.RepairHeld.Successes, r.RepairHeld.Trials, pct(r.RepairHeld.WilsonLow))
	} else {
		s += fmt.Sprintf(" Repair: %s (%d/%d held).", r.Repair, r.RepairHeld.Successes, r.RepairHeld.Trials)
	}
	if r.Truncated {
		s += " Stopped early at the time budget."
	}
	return s
}
