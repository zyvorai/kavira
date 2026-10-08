package compiler

import (
	"path/filepath"
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
