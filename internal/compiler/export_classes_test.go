// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The before test fails on the captured condition and the repaired test passes, for every new class, and
// the exported script runs end to end under a plain POSIX shell.
func TestRegressionExportNewClasses(t *testing.T) {
	kavira := os.Getenv("KAVIRA_TEST_BIN")
	for _, c := range classExamples {
		b := load(t, c.file)
		rep := Compile(b)
		files, err := RegressionFiles(b, rep)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		before := RegressionTest{ID: b.ID, Class: b.Class, Repair: map[string]any{}, Expect: rep.Repair.RegressionTest.Expect}
		if _, err := TestRepair(b, before); err == nil {
			t.Errorf("%s: the captured condition must fail", c.file)
		}
		if _, err := TestRepair(b, rep.Repair.RegressionTest); err != nil {
			t.Errorf("%s: the repaired condition must pass: %v", c.file, err)
		}
		if kavira != "" {
			dest := t.TempDir()
			for n, raw := range files {
				_ = os.WriteFile(filepath.Join(dest, n), raw, 0o755)
			}
			cmd := exec.Command("sh", filepath.Join(dest, "regress.sh"))
			cmd.Env = append(os.Environ(), "KAVIRA="+kavira)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s: regress.sh: %v %s", c.file, err, out)
			}
		}
	}
}

// regress.sh must stay POSIX and dependency-free: no bashisms, no tools beyond sh builtins, dirname and
// command -v. A new class must not change it.
func TestRegressScriptStaysPOSIX(t *testing.T) {
	if !strings.HasPrefix(regressScript, "#!/bin/sh\n") {
		t.Fatal("shebang must be /bin/sh")
	}
	for _, bad := range []string{"[[", "]]", "<<<", "function ", "source ", "$'", "==", "local ", "declare ", "echo -e", "<(", "$RANDOM", "&>", "|&", "curl", "wget", "python", "jq", "node"} {
		if strings.Contains(regressScript, bad) {
			t.Errorf("regress.sh contains %q", bad)
		}
	}
	// Every command word that starts a line or follows a pipe/&&/then must be a builtin or dirname.
	allowed := map[string]bool{"set": true, "KAVIRA": true, "dir": true, "if": true, "echo": true, "exit": true, "fi": true, "command": true, "then": true}
	re := regexp.MustCompile(`(?m)^\s*([A-Za-z_]+)`)
	for _, m := range re.FindAllStringSubmatch(regressScript, -1) {
		if !allowed[m[1]] {
			t.Errorf("unexpected command %q in regress.sh", m[1])
		}
	}
	if sh, err := exec.LookPath("sh"); err == nil {
		path := filepath.Join(t.TempDir(), "regress.sh")
		_ = os.WriteFile(path, []byte(strings.Replace(regressScript, "%s", "x", 1)), 0o755)
		if out, err := exec.Command(sh, "-n", path).CombinedOutput(); err != nil {
			t.Fatalf("sh -n: %v %s", err, out)
		}
	}
}
