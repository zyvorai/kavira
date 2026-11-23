// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

// Package minimize shrinks a captured incident to the smallest input that still fails when executed,
// using delta debugging (ddmin, Zeller and Hildebrandt 2002). The oracle is always the runner.
package minimize

import (
	"fmt"
	"time"
)

// Budget bounds the oracle: a run count and a wall-clock deadline.
type Budget struct {
	MaxRuns  int
	Deadline time.Time // zero means none
	Used     int
}

func (b *Budget) exhausted() bool {
	if b.MaxRuns > 0 && b.Used >= b.MaxRuns {
		return true
	}
	return !b.Deadline.IsZero() && time.Now().After(b.Deadline)
}

// DDMin reduces the set {0..n-1} to a subset on which fails(subset) is still true. fails must be true for
// the full set (the caller checks) and false for the empty set (never asked). complete is true only when
// the search finished inside the budget; then the result is 1-minimal: removing any single remaining
// element makes fails return false. That is local minimality, not a global minimum.
func DDMin(n int, fails func(keep []int) bool, b *Budget) (keep []int, complete bool) {
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}
	cache := map[string]bool{}
	test := func(s []int) (res bool, ok bool) {
		key := fmt.Sprint(s)
		if v, hit := cache[key]; hit {
			return v, true
		}
		if b.exhausted() {
			return false, false
		}
		b.Used++
		cache[key] = fails(s)
		return cache[key], true
	}
	gran := 2
	for len(items) >= 2 {
		if gran > len(items) {
			gran = len(items)
		}
		chunks := split(items, gran)
		progressed := false
		for _, c := range chunks {
			res, ok := test(c)
			if !ok {
				return items, false
			}
			if res {
				items, gran, progressed = c, 2, true
				break
			}
		}
		if !progressed {
			for i := range chunks {
				comp := complement(chunks, i)
				res, ok := test(comp)
				if !ok {
					return items, false
				}
				if res {
					items, progressed = comp, true
					if gran > 2 {
						gran--
					}
					break
				}
			}
		}
		if !progressed {
			if gran >= len(items) {
				break
			}
			gran *= 2
		}
	}
	return items, true
}

func split(items []int, n int) [][]int {
	out := make([][]int, 0, n)
	size, rem := len(items)/n, len(items)%n
	pos := 0
	for i := 0; i < n; i++ {
		w := size
		if i < rem {
			w++
		}
		out = append(out, items[pos:pos+w])
		pos += w
	}
	return out
}

func complement(chunks [][]int, skip int) []int {
	var out []int
	for i, c := range chunks {
		if i != skip {
			out = append(out, c...)
		}
	}
	return out
}
