// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package postmortem

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/compiler"
)

func load(t *testing.T, name string) bundle.Bundle {
	t.Helper()
	b, err := bundle.Load(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRenderHeldStatesOnlyWhatWasDemonstrated(t *testing.T) {
	b := load(t, "oom.json")
	b.Timeline = []bundle.TimelineEvent{{At: "T-10m", Event: "VM migration"}, {At: "T-2m", Event: "first restart"}}
	rep := compiler.Compile(b)
	md := Render(b, rep)
	for _, want := range []string{
		"# Postmortem draft: inc-oom-001", "DRAFT", "Exact replay: no", "## Summary", "## Impact", "## Timeline",
		"| T-10m | VM migration |", "## Root cause", "**Hypothesis:**", "reproduced in the reconstruction",
		"## Mitigation and repair", "Do not apply this patch to production", "## Not established by KAVIRA",
		"## Action items (proposed, not verified)", "## Lessons learned",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{"exact replay: yes", "was replayed", "confirmed root cause", "the root cause is"} {
		if strings.Contains(strings.ToLower(md), strings.ToLower(bad)) {
			t.Errorf("overclaim %q", bad)
		}
	}
}

func TestRenderNotReproducedHasNoRepair(t *testing.T) {
	b := load(t, "oom.json")
	b.Evidence.MemoryLimitBytes = 64 << 20
	b.Workload.Hold = false
	rep := compiler.Compile(b)
	if rep.Repair != nil {
		t.Fatal("setup: expected no repair")
	}
	md := Render(b, rep)
	if strings.Contains(md, "```json") || strings.Contains(md, "memory_limit_bytes\":") {
		t.Fatal("no patch may appear without a held repair")
	}
	if !strings.Contains(md, "**Not demonstrated.**") || !strings.Contains(md, "No repair is stated") {
		t.Fatal("must say nothing was demonstrated")
	}
	if !strings.Contains(md, "The bundle has no timeline") {
		t.Fatal("must not invent a timeline")
	}
}

func TestRenderEscapesHostileText(t *testing.T) {
	b := load(t, "oom.json")
	b.ID = "inc-<img src=x onerror=alert(1)>"
	b.Service = "svc|with|pipes\n# injected heading"
	b.Symptom = "[click](javascript:alert(1)) <script>alert(1)</script>"
	b.Timeline = []bundle.TimelineEvent{{At: "t|1", Event: "line1\n## fake section\n<b>x</b>"}}
	b.Evidence.Notes = []string{"```\n# break out"}
	rep := compiler.Compile(b)
	md := Render(b, rep)
	for _, bad := range []string{"<script", "<img", "<b>", "\n## fake section", "\n# injected heading", "[click](", "\n[click]"} {
		if strings.Contains(md, bad) {
			t.Errorf("unescaped %q in output", bad)
		}
	}
	// Table rows keep exactly three pipes so a cell cannot add columns.
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(l, "| t") && strings.Count(strings.ReplaceAll(l, `\|`, ""), "|") != 3 {
			t.Errorf("table row broken: %q", l)
		}
	}
}
