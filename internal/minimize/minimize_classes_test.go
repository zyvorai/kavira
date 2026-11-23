// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package minimize

import (
	"errors"
	"testing"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/compiler"
)

func TestMinimizeDNSFactors(t *testing.T) {
	b := load(t, "dns-failure.json")
	d := *b.DNS
	d.NXDomainPercent = 25 // a second, harmless-looking factor: with a 30ms timeout the latency alone fails
	b.DNS = &d
	rep, min, err := Minimize(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kind != "dns_factors" || len(rep.Minimal) != 1 || rep.Minimal[0] != "latency_ms=60" || !rep.Verified || min.DNS.NXDomainPercent != 0 {
		t.Fatalf("%+v", rep)
	}
	if compiler.Compile(min).Verdict == "not_reproduced" {
		t.Fatal("minimal bundle must still reproduce")
	}
}

func TestMinimizeCertProbes(t *testing.T) {
	b := load(t, "certificate-expiry.json") // probes 705 and 715 fail, the rest verify
	rep, min, err := Minimize(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kind != "probe_hours" || len(rep.Minimal) != 1 || len(min.Cert.ProbeHours) != 1 || !rep.OneMinimal || !rep.Verified {
		t.Fatalf("%+v", rep)
	}
	if h := min.Cert.ProbeHours[0]; h != 705 && h != 715 {
		t.Fatalf("kept a probe that verifies: %d", h)
	}
	if len(rep.Removed) != 5 {
		t.Fatalf("removed %v", rep.Removed)
	}
}

func TestMinimizePoolClients(t *testing.T) {
	b := load(t, "connection-pool.json")
	b.Pool.RequestsPerClient = 2
	rep, min, err := Minimize(b, Options{MaxRuns: 60})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kind != "clients" || min.Pool.Clients >= b.Pool.Clients || min.Pool.Clients <= b.Pool.PoolSize || !rep.Verified {
		t.Fatalf("%+v pool clients %d", rep, min.Pool.Clients)
	}
}

func TestMinimizeRetryClientsKeepsTheBudgetHonest(t *testing.T) {
	b := load(t, "retry-storm.json")
	rep, min, err := Minimize(b, Options{MaxRuns: 6})
	if err != nil {
		t.Fatal(err)
	}
	if rep.OneMinimal {
		t.Fatal("6 oracle runs cannot prove 1-minimality over 24 clients")
	}
	if min.Retry.Clients > b.Retry.Clients || rep.ExactReplay {
		t.Fatalf("%+v", rep)
	}
}

func TestMinimizeNetworkArms(t *testing.T) {
	b := load(t, "network-timeout.json")
	n := bundle.NetworkCondition{Attempts: 2, TimeoutMs: 200, DelayMs: 10, JitterMs: 5, BandwidthBps: 2000, ResponseBytes: 1000, SlowCloseMs: 20}
	b.Network = &n
	rep, min, err := Minimize(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Minimal) != 1 || rep.Minimal[0] != "bandwidth_bps=2000" || min.Network.ResponseBytes != 1000 || min.Network.TimeoutMs != 200 {
		t.Fatalf("only the bandwidth cap matters: %+v", rep)
	}
	if rep.Removed == nil {
		t.Fatal("removed must be a list, not null")
	}
}

func TestMinimizeNewClassNotReproduced(t *testing.T) {
	b := load(t, "dns-failure.json")
	b.DNS.LatencyMs = 0
	if _, _, err := Minimize(b, Options{}); err == nil {
		t.Fatal("a resolver with no latency and no NXDOMAIN has no removable factor")
	}
	c := load(t, "certificate-expiry.json")
	c.Cert.ProbeHours = []int{10}
	if _, _, err := Minimize(c, Options{}); !errors.Is(err, ErrNotReproduced) {
		t.Fatalf("err %v", err)
	}
}
