// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package postmortem

import (
	"strings"
	"testing"

	"github.com/zyvorai/kavira/internal/compiler"
)

func TestRenderNewClasses(t *testing.T) {
	for _, f := range []string{"dns-failure.json", "certificate-expiry.json", "connection-pool.json", "retry-storm.json"} {
		b := load(t, f)
		b.Symptom = `<img src=x onerror=alert(1)> | [x](javascript:alert(1))`
		b.Timeline = nil
		rep := compiler.Compile(b)
		md := Render(b, rep)
		for _, want := range []string{"## Root cause", "## Mitigation and repair", "Exact replay: no", "Do not apply this patch to production", "Hypothesis:", "reconstructed"} {
			if !strings.Contains(md, want) {
				t.Errorf("%s: missing %q", f, want)
			}
		}
		if strings.Contains(md, "<img") || strings.Contains(md, "[x](") {
			t.Errorf("%s: bundle text not escaped", f)
		}
		if strings.Contains(strings.ReplaceAll(strings.ToLower(md), "not replayed", ""), "replayed") {
			t.Errorf("%s: claims replay", f)
		}
	}
	// No repair is stated when nothing reproduced.
	b := load(t, "retry-storm.json")
	b.Retry.JitterPercent = 100
	md := Render(b, compiler.Compile(b))
	if !strings.Contains(md, "No repair is stated") || strings.Contains(md, "```json") {
		t.Fatal("not_reproduced must state no repair")
	}
}
