// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-1.0

package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyvorai/kavira/internal/bundle"
)

func load(t *testing.T, name string) bundle.Bundle {
	t.Helper()
	b, err := bundle.Load(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestOOMRepairHeld(t *testing.T) {
	rep := Compile(load(t, "oom.json"))
	if rep.Verdict != "repair_held" {
		t.Fatalf("verdict %s: %s", rep.Verdict, rep.Summary)
	}
	if rep.ExactReplay {
		t.Fatal("exact replay must stay false")
	}
	if rep.Repair == nil {
		t.Fatal("missing repair package")
	}
	if _, err := TestRepair(load(t, "oom.json"), rep.Repair.RegressionTest); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkRepairHeld(t *testing.T) {
	rep := Compile(load(t, "network-timeout.json"))
	if rep.Verdict != "repair_held" {
		t.Fatalf("verdict %s: %s", rep.Verdict, rep.Summary)
	}
	if _, err := TestRepair(load(t, "network-timeout.json"), rep.Repair.RegressionTest); err != nil {
		t.Fatal(err)
	}
}

func TestConfigRepairHeld(t *testing.T) {
	rep := Compile(load(t, "config-regression.json"))
	if rep.Verdict != "repair_held" {
		t.Fatalf("verdict %s: %s", rep.Verdict, rep.Summary)
	}
	if _, err := TestRepair(load(t, "config-regression.json"), rep.Repair.RegressionTest); err != nil {
		t.Fatal(err)
	}
}

func TestDiskRepairHeld(t *testing.T) {
	rep := Compile(load(t, "disk-pressure.json"))
	if rep.Verdict != "repair_held" {
		t.Fatalf("verdict %s: %s", rep.Verdict, rep.Summary)
	}
	if rep.ExactReplay {
		t.Fatal("exact replay must stay false")
	}
	if rep.Repair == nil {
		t.Fatal("missing repair package")
	}
	if _, err := TestRepair(load(t, "disk-pressure.json"), rep.Repair.RegressionTest); err != nil {
		t.Fatal(err)
	}
	raw, err := PackageZIP(load(t, "disk-pressure.json"), rep)
	if err != nil || len(raw) < 100 {
		t.Fatalf("package zip: len=%d err=%v", len(raw), err)
	}
	sum := Summarize(rep)
	if !strings.Contains(sum, "repair_held") || !strings.Contains(sum, "inc-disk-001") {
		t.Fatalf("summarize: %s", sum)
	}
}

func TestUnsupportedClass(t *testing.T) {
	_, err := bundle.Parse([]byte(`{"id":"x","class":"kernel_panic"}`))
	if err == nil {
		t.Fatal("expected unsupported class")
	}
}

func TestNotReproduced(t *testing.T) {
	b := load(t, "oom.json")
	b.Evidence.MemoryLimitBytes = 64 << 20
	b.Workload.Hold = false
	rep := Compile(b)
	if rep.Verdict != "not_reproduced" {
		t.Fatalf("verdict %s", rep.Verdict)
	}
	if rep.Repair != nil {
		t.Fatal("did not want a repair package")
	}
}

func TestWritePackageRejectsUnsafeID(t *testing.T) {
	b := load(t, "oom.json")
	rep := Compile(b)
	if rep.Repair == nil {
		t.Fatal("missing repair package")
	}
	root := t.TempDir()
	for _, id := range []string{"../escape", "a/b", "..", ".", `a\b`} {
		rep.ID = id
		if err := WritePackage(filepath.Join(root, "out"), b, rep); err == nil {
			t.Fatalf("id %q accepted", id)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); err == nil {
		t.Fatal("wrote outside output dir")
	}
	rep.ID = "ok-1"
	if err := WritePackage(filepath.Join(root, "out"), b, rep); err != nil {
		t.Fatal(err)
	}
}

func TestAsIntRejectsNonNumeric(t *testing.T) {
	for _, v := range []any{nil, "500", true} {
		if _, ok := asInt(v); ok {
			t.Fatalf("asInt(%v) accepted", v)
		}
	}
	if n, ok := asInt(float64(7)); !ok || n != 7 {
		t.Fatalf("got %d %v", n, ok)
	}
}
