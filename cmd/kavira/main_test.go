// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ex(name string) string { return filepath.Join("..", "..", "examples", name) }

func TestAnalysisCommands(t *testing.T) {
	dir := t.TempDir()
	notRepro := filepath.Join(dir, "nr.json")
	raw, _ := os.ReadFile(ex("oom.json"))
	_ = os.WriteFile(notRepro, []byte(strings.Replace(string(raw), `"memory_limit_bytes": 3145728`, `"memory_limit_bytes": 67108864`, 1)), 0o644)
	huge := filepath.Join(dir, "huge.json")
	_ = os.WriteFile(huge, []byte(`{"id":"h","class":"container_oom","evidence":{"memory_limit_bytes":1099511627776},"workload":{"requests":1024,"bytes_per_request":1099511627776}}`), 0o644)

	cases := []struct {
		name string
		run  func([]string) int
		args []string
		want int
	}{
		{"repeat ok", runRepeat, []string{"-f", ex("oom.json"), "-n", "3"}, 0},
		{"repeat clean but too few runs is inconclusive", runRepeat, []string{"-f", notRepro, "-n", "3"}, 1},
		{"repeat clean 20 runs is not_reproduced", runRepeat, []string{"-f", notRepro, "-n", "20"}, 0},
		{"repeat missing -f", runRepeat, nil, 2},
		{"repeat hostile bundle", runRepeat, []string{"-f", huge}, 1},
		{"repeat missing file", runRepeat, []string{"-f", filepath.Join(dir, "nope.json")}, 1},
		{"minimize ok", runMinimize, []string{"-f", ex("config-regression.json"), "-o", filepath.Join(dir, "min.json")}, 0},
		{"minimize not reproduced", runMinimize, []string{"-f", notRepro}, 1},
		{"minimize hostile bundle", runMinimize, []string{"-f", huge}, 1},
		{"postmortem ok", runPostmortem, []string{"-f", ex("oom.json"), "-o", filepath.Join(dir, "pm.md")}, 0},
		{"postmortem not reproduced still drafts", runPostmortem, []string{"-f", notRepro, "-o", filepath.Join(dir, "pm2.md")}, 0},
		{"export ok", runExport, []string{"-f", ex("oom.json"), "-o", filepath.Join(dir, "r")}, 0},
		{"export refuses not_reproduced", runExport, []string{"-f", notRepro, "-o", filepath.Join(dir, "r2")}, 1},
	}
	for _, c := range cases {
		if got := c.run(c.args); got != c.want {
			t.Errorf("%s: exit %d want %d", c.name, got, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "r2")); err == nil {
		t.Error("export wrote files for a not_reproduced incident")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "pm2.md")); !strings.Contains(string(b), "Not demonstrated") {
		t.Error("not_reproduced draft must say nothing was demonstrated")
	}
}
