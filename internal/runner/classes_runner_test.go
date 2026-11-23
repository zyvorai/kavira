// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package runner

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
)

// Timing notes. Pure arithmetic parts (certificate schedule, NXDOMAIN selection, which lookup is slow) are
// exact. Anything that sleeps (resolver latency, service time, backoff) is timing-dependent, so these
// tests keep every expected pass or fail at least several multiples of the scheduler noise away from a
// boundary.

func TestDNSTable(t *testing.T) {
	cases := []struct {
		name         string
		c            bundle.DNSCondition
		failures     int
		cacheHitsMin int
	}{
		{"healthy", bundle.DNSCondition{TimeoutMs: 100, Lookups: 6, Names: 3}, 0, 0},
		{"slow resolver beyond timeout, no retry gain", bundle.DNSCondition{LatencyMs: 80, TimeoutMs: 20, Retries: 1, Lookups: 4, Names: 4}, 4, 0},
		{"slow resolver inside timeout", bundle.DNSCondition{LatencyMs: 10, TimeoutMs: 200, Lookups: 4, Names: 4}, 0, 0},
		{"degraded from the first query, no retries", bundle.DNSCondition{LatencyMs: 80, DegradedAfter: 0, TimeoutMs: 20, Retries: 0, Lookups: 2, Names: 2}, 2, 0},
		{"nxdomain half of names", bundle.DNSCondition{NXDomainPercent: 50, TimeoutMs: 200, Lookups: 4, Names: 4}, 2, 0},
		{"nxdomain is cached negatively but still a failure", bundle.DNSCondition{NXDomainPercent: 50, TimeoutMs: 200, Lookups: 8, Names: 4, Cache: true}, 4, 4},
		{"cache serves names resolved before degradation", bundle.DNSCondition{LatencyMs: 80, DegradedAfter: 2, TimeoutMs: 20, Lookups: 6, Names: 2, Cache: true}, 0, 4},
		{"no cache, same degradation", bundle.DNSCondition{LatencyMs: 80, DegradedAfter: 2, TimeoutMs: 20, Lookups: 6, Names: 2}, 4, 0},
	}
	for _, c := range cases {
		res := RunDNS(c.c)
		if res.Failures != c.failures || res.Attempts != c.c.Lookups || res.Passed != (c.failures == 0) {
			t.Errorf("%s: got %+v want %d failures", c.name, res, c.failures)
		}
		if c.cacheHitsMin > 0 && !strings.Contains(res.Detail, "cache_hits=") {
			t.Errorf("%s: detail lacks cache hits: %s", c.name, res.Detail)
		}
	}
}

func TestDNSLateAnswerIsNotMistakenForALaterAttempt(t *testing.T) {
	// Every attempt is slower than the timeout. Each attempt uses a fresh socket, so the late answer to
	// attempt 1 cannot satisfy attempt 2: the lookup must fail, not succeed on a stale reply.
	res := RunDNS(bundle.DNSCondition{LatencyMs: 60, TimeoutMs: 25, Retries: 2, Lookups: 1, Names: 1})
	if res.Failures != 1 {
		t.Fatalf("every attempt is slow, so the lookup must fail: %+v", res)
	}
}

func TestDNSDoesNotHangOnLongLatency(t *testing.T) {
	done := make(chan Result, 1)
	go func() {
		done <- RunDNS(bundle.DNSCondition{LatencyMs: 60_000, TimeoutMs: 20, Lookups: 2, Names: 2})
	}()
	select {
	case res := <-done:
		if res.Failures != 2 {
			t.Fatalf("%+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunDNS waited for a 60 s late answer nobody needed")
	}
}

func TestDNSAnswerRejectsHostilePackets(t *testing.T) {
	d := bundle.DNSCondition{Lookups: 2, Names: 2}
	good := dnsQuery(7, "host-1.svc.local")
	if resp, ok := dnsAnswer(good, d); !ok || binary.BigEndian.Uint16(resp[0:2]) != 7 || resp[2]&0x80 == 0 {
		t.Fatalf("good query rejected: %v %v", ok, resp)
	}
	cases := map[string][]byte{
		"empty":               nil,
		"short header":        {1, 2, 3},
		"two questions":       func() []byte { q := append([]byte(nil), good...); q[5] = 2; return q }(),
		"label past end":      append(append([]byte(nil), good[:12]...), 200, 'a'),
		"compression pointer": append(append([]byte(nil), good[:12]...), 0xc0, 12, 0, 1, 0, 1),
		"no terminator":       append(append([]byte(nil), good[:12]...), 3, 'a', 'b', 'c'),
		"missing qtype":       good[:len(good)-4],
		"not our name":        dnsQuery(1, "example.com"),
		"all ones": func() []byte {
			q := make([]byte, 600)
			for i := range q {
				q[i] = 0xff
			}
			return q
		}(),
		"huge label count short": append(append([]byte(nil), good[:12]...), 63),
	}
	for name, q := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic %v", name, r)
				}
			}()
			if _, ok := dnsAnswer(q, d); ok {
				t.Errorf("%s: accepted", name)
			}
		}()
	}
}

func TestCertScheduleTable(t *testing.T) {
	cases := []struct {
		name string
		c    bundle.CertCondition
		want []int
	}{
		{"no rotation", bundle.CertCondition{LifetimeHours: 100, ProbeHours: []int{500}}, []int{0}},
		{"window wider than interval rotates before expiry", bundle.CertCondition{LifetimeHours: 100, RotationWindowHours: 30, CheckIntervalHours: 20, ProbeHours: []int{300}}, []int{0, 80, 160, 240}},
		{"window narrower than interval misses expiry", bundle.CertCondition{LifetimeHours: 100, RotationWindowHours: 5, CheckIntervalHours: 30, ProbeHours: []int{200}}, []int{0, 120}},
	}
	for _, c := range cases {
		var got []int
		for _, i := range CertSchedule(c.c) {
			got = append(got, i.From)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestRunCertTable(t *testing.T) {
	cases := []struct {
		name     string
		c        bundle.CertCondition
		failures int
	}{
		{"fresh certificate", bundle.CertCondition{LifetimeHours: 24, ProbeHours: []int{0, 1, 23}}, 0},
		{"exactly at expiry is still valid", bundle.CertCondition{LifetimeHours: 24, ProbeHours: []int{24}}, 0},
		{"one hour after expiry", bundle.CertCondition{LifetimeHours: 24, ProbeHours: []int{25}}, 1},
		{"no rotation, three late probes", bundle.CertCondition{LifetimeHours: 24, ProbeHours: []int{10, 30, 40, 50}}, 3},
		{"rotation gap", bundle.CertCondition{LifetimeHours: 700, RotationWindowHours: 24, CheckIntervalHours: 72, ProbeHours: []int{100, 705, 715, 730}}, 2},
		{"wide window closes the gap", bundle.CertCondition{LifetimeHours: 700, RotationWindowHours: 144, CheckIntervalHours: 72, ProbeHours: []int{100, 705, 715, 730}}, 0},
	}
	for _, c := range cases {
		res := RunCert(c.c)
		if res.Failures != c.failures || res.Attempts != len(c.c.ProbeHours) {
			t.Errorf("%s: got %+v want %d failures", c.name, res, c.failures)
		}
	}
}

func TestPoolTable(t *testing.T) {
	cases := []struct {
		name     string
		p        bundle.PoolCondition
		failures func(int) bool
	}{
		{"pool as large as the herd", bundle.PoolCondition{PoolSize: 6, AcquireTimeoutMs: 400, Clients: 6, RequestsPerClient: 3, ServiceMs: 10}, func(f int) bool { return f == 0 }},
		{"undersized pool, impatient acquire", bundle.PoolCondition{PoolSize: 1, AcquireTimeoutMs: 15, Clients: 6, RequestsPerClient: 2, ServiceMs: 40}, func(f int) bool { return f > 0 }},
		{"undersized pool, patient acquire", bundle.PoolCondition{PoolSize: 2, AcquireTimeoutMs: 1500, Clients: 6, RequestsPerClient: 2, ServiceMs: 10}, func(f int) bool { return f == 0 }},
		{"leak drains the pool", bundle.PoolCondition{PoolSize: 2, AcquireTimeoutMs: 30, Clients: 2, RequestsPerClient: 6, ServiceMs: 5, LeakEvery: 2}, func(f int) bool { return f >= 6 }},
		{"zero service time", bundle.PoolCondition{PoolSize: 3, AcquireTimeoutMs: 500, Clients: 3, RequestsPerClient: 5}, func(f int) bool { return f == 0 }},
	}
	for _, c := range cases {
		res := RunPool(c.p)
		if !c.failures(res.Failures) || res.Attempts != c.p.Clients*c.p.RequestsPerClient || res.Passed != (res.Failures == 0) {
			t.Errorf("%s: %+v", c.name, res)
		}
	}
}

func TestRetryJitterSpreadsTheHerd(t *testing.T) {
	storm := bundle.RetryCondition{Clients: 24, Capacity: 4, ServiceMs: 20, BackoffMs: 40, MaxRetries: 4}
	res := RunRetry(storm)
	// Without jitter every wave admits exactly Capacity clients: 5 waves * 4 = 20 served, 4 give up.
	if res.Failures != 4 {
		t.Fatalf("unjittered herd: want exactly 4 to give up, got %+v", res)
	}
	storm.JitterPercent = 100
	if res := RunRetry(storm); res.Failures != 0 {
		t.Fatalf("jitter should spread the retries: %+v", res)
	}
	storm.JitterPercent = 0
	storm.MaxRetries = 6
	if res := RunRetry(storm); res.Failures != 0 {
		t.Fatalf("enough synchronized waves also serve everyone: %+v", res)
	}
}

func TestRetryNoRetriesServesOnlyCapacity(t *testing.T) {
	res := RunRetry(bundle.RetryCondition{Clients: 10, Capacity: 3, ServiceMs: 30, BackoffMs: 10, MaxRetries: 0})
	if res.Failures != 7 {
		t.Fatalf("%+v", res)
	}
}

func TestNetworkArms(t *testing.T) {
	base := bundle.NetworkCondition{Attempts: 2, TimeoutMs: 300}
	with := func(f func(*bundle.NetworkCondition)) bundle.NetworkCondition { n := base; f(&n); return n }
	cases := []struct {
		name string
		n    bundle.NetworkCondition
		fail int
	}{
		{"clean", base, 0},
		{"jitter inside timeout", with(func(n *bundle.NetworkCondition) { n.JitterMs = 20 }), 0},
		{"delay plus jitter beyond timeout", with(func(n *bundle.NetworkCondition) { n.TimeoutMs = 20; n.DelayMs = 100; n.JitterMs = 50 }), 2},
		{"bandwidth fast enough", with(func(n *bundle.NetworkCondition) { n.ResponseBytes = 1000; n.BandwidthBps = 100000 }), 0},
		{"bandwidth cap too slow", with(func(n *bundle.NetworkCondition) { n.ResponseBytes = 1000; n.BandwidthBps = 1000 }), 2},
		{"reset after some bytes", with(func(n *bundle.NetworkCondition) { n.ResponseBytes = 1000; n.ResetAfterBytes = 100 }), 2},
		{"reset threshold beyond response is a no-op", with(func(n *bundle.NetworkCondition) { n.ResponseBytes = 100; n.ResetAfterBytes = 100 }), 0},
		{"slow close under a waiting client", with(func(n *bundle.NetworkCondition) { n.TimeoutMs = 60; n.SlowCloseMs = 500 }), 2},
		{"slow close within timeout", with(func(n *bundle.NetworkCondition) { n.SlowCloseMs = 30 }), 0},
		{"large response, no cap", with(func(n *bundle.NetworkCondition) { n.ResponseBytes = 1 << 20 }), 0},
	}
	for _, c := range cases {
		done := make(chan Result, 1)
		go func() { done <- RunNetwork(c.n) }()
		select {
		case res := <-done:
			if res.Failures != c.fail {
				t.Errorf("%s: got %+v want %d failures", c.name, res, c.fail)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: RunNetwork hung", c.name)
		}
	}
}

func TestNetworkHandlerDoesNotDivideByZero(t *testing.T) {
	// A caller that skipped validation (Attempts 0) with loss set used to panic in the handler.
	res := RunNetwork(bundle.NetworkCondition{TimeoutMs: 50, LossPercent: 50})
	if res.Attempts != 1 {
		t.Fatalf("%+v", res)
	}
}
