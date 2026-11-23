// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package compiler

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyvorai/kavira/internal/bundle"
)

// Every class: the "before" test fails on the captured condition and the repaired test passes,
// through the same TestRepair the CLI uses.
func TestRegressionBeforeFailsAfterPasses(t *testing.T) {
	for _, name := range []string{"oom.json", "network-timeout.json", "config-regression.json", "disk-pressure.json", "dns-failure.json", "certificate-expiry.json", "connection-pool.json", "retry-storm.json"} {
		b := load(t, name)
		rep := Compile(b)
		files, err := RegressionFiles(b, rep)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, f := range []string{"bundle.json", "before_test.json", "regression_test.json", "regress.sh"} {
			if len(files[f]) == 0 {
				t.Fatalf("%s: missing %s", name, f)
			}
		}
		before := RegressionTest{ID: b.ID, Class: b.Class, Repair: map[string]any{}, Expect: rep.Repair.RegressionTest.Expect}
		if b.Class == bundle.ClassConfig {
			for k, v := range b.Configs.Current {
				before.Repair[k] = v
			}
		}
		if _, err := TestRepair(b, before); err == nil {
			t.Errorf("%s: the captured condition must fail", name)
		}
		if _, err := TestRepair(b, rep.Repair.RegressionTest); err != nil {
			t.Errorf("%s: the repaired condition must pass: %v", name, err)
		}
	}
}

func TestRegressionRefusedWithoutHeldRepair(t *testing.T) {
	b := load(t, "oom.json")
	b.Evidence.MemoryLimitBytes = 64 << 20
	b.Workload.Hold = false
	rep := Compile(b)
	if _, err := RegressionFiles(b, rep); err == nil {
		t.Fatal("no export for not_reproduced")
	}
	if _, err := RegressionZIP(b, rep); err == nil {
		t.Fatal("no zip for not_reproduced")
	}
	rep = Compile(load(t, "oom.json"))
	rep.Verdict = "repair_failed"
	if _, err := RegressionFiles(load(t, "oom.json"), rep); err == nil {
		t.Fatal("no export unless the repair held")
	}
}

func TestRegressionRejectsUnsafeID(t *testing.T) {
	b := load(t, "oom.json")
	rep := Compile(b)
	for _, id := range []string{"../escape", "a/b", "..", `a\b`} {
		rep.ID = id
		if _, err := RegressionFiles(b, rep); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
}

func TestRegressionScriptCommentIsOneLine(t *testing.T) {
	b := load(t, "oom.json")
	rep := Compile(b)
	rep.ID = "ok\n$(touch /tmp/pwned)"
	// An id with a newline is not a safe directory name either way; shellComment is defense in depth.
	if got := shellComment(rep.ID); strings.Contains(got, "\n") {
		t.Fatalf("newline survived: %q", got)
	}
}

func TestRegressionWriteAndZip(t *testing.T) {
	b := load(t, "disk-pressure.json")
	rep := Compile(b)
	dir := t.TempDir()
	dest, err := WriteRegression(dir, b, rep)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dest, "regress.sh"))
	if err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("regress.sh must be executable: %v", err)
	}
	raw, err := RegressionZIP(b, rep)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(zr.File) != 4 {
		t.Fatalf("zip: %v files=%d", err, len(zr.File))
	}
	for _, f := range zr.File {
		if strings.Contains(f.Name, "..") || !strings.HasPrefix(f.Name, rep.ID+"/regression/") {
			t.Errorf("bad zip entry %q", f.Name)
		}
		rc, _ := f.Open()
		_, _ = io.Copy(io.Discard, rc)
		rc.Close()
	}
}

// The script itself runs with a real kavira binary when go and sh are available.
func TestRegressionScriptEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	bin := filepath.Join(t.TempDir(), "kavira")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/kavira")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build kavira: %v %s", err, out)
	}
	b := load(t, "oom.json")
	dest, err := WriteRegression(t.TempDir(), b, Compile(b))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, filepath.Join(dest, "regress.sh"))
	cmd.Env = append(os.Environ(), "KAVIRA="+bin)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok:") {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	// Break the repair: the script must fail.
	tp := filepath.Join(dest, "regression_test.json")
	raw, _ := os.ReadFile(tp)
	broken := strings.NewReplacer(`"memory_limit_bytes": `, `"memory_limit_bytes": 1, "x": `).Replace(string(raw))
	_ = os.WriteFile(tp, []byte(broken), 0o644)
	cmd = exec.Command(sh, filepath.Join(dest, "regress.sh"))
	cmd.Env = append(os.Environ(), "KAVIRA="+bin)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("a broken repair must fail the script: %s", out)
	}
}

func TestRegressionScriptWithoutBinaryIsAnErrorNotAPass(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	b := load(t, "oom.json")
	dest, err := WriteRegression(t.TempDir(), b, Compile(b))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, filepath.Join(dest, "regress.sh"))
	cmd.Env = append(os.Environ(), "KAVIRA=/nonexistent/kavira")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "not found") {
		t.Fatalf("missing binary must be exit 2, got %v %s", err, out)
	}
}

// examples/analysis holds bundles for minimize, repeat and postmortem; they must stay valid and reproduce.
func TestAnalysisExamplesCompile(t *testing.T) {
	for _, name := range []string{"config-noisy.json", "oom-burst.json"} {
		b, err := bundle.Load(filepath.Join("..", "..", "examples", "analysis", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if rep := Compile(b); rep.Verdict != "repair_held" || rep.ExactReplay {
			t.Errorf("%s: %s", name, rep.Verdict)
		}
	}
}
