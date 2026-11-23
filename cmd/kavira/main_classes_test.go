// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package main

import (
	"path/filepath"
	"testing"
)

// The analysis commands accept the new classes and keep their exit-code contract.
func TestAnalysisCommandsNewClasses(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"dns-failure.json", "certificate-expiry.json", "connection-pool.json", "retry-storm.json"} {
		stem := f[:len(f)-5]
		for name, c := range map[string]struct {
			run  func([]string) int
			args []string
		}{
			"repeat":     {runRepeat, []string{"-f", ex(f), "-n", "2"}},
			"postmortem": {runPostmortem, []string{"-f", ex(f), "-o", filepath.Join(dir, stem+".md")}},
			"export":     {runExport, []string{"-f", ex(f), "-o", filepath.Join(dir, "r-"+stem)}},
		} {
			if got := c.run(c.args); got != 0 {
				t.Errorf("%s %s: exit %d", name, f, got)
			}
		}
	}
	// Minimizing the cheap ones; the retry storm one is slow by construction and covered in internal/minimize.
	for _, f := range []string{"dns-failure.json", "certificate-expiry.json"} {
		if got := runMinimize([]string{"-f", ex(f), "-o", filepath.Join(dir, "min-"+f)}); got != 0 {
			t.Errorf("minimize %s: exit %d", f, got)
		}
	}
}
