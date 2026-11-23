// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package bundle

import "fmt"

// Bounds for the incident classes that execute against loopback. Like every other limit here, they hold
// before anything runs, and again for repaired conditions read back from a regression test.
const (
	MaxDNSLookups   = 200
	MaxDNSRetries   = 5
	MaxCertHours    = 87_600 // ten years
	MaxCertProbes   = 50
	MaxPoolSize     = 64
	MaxPoolClients  = 64
	MaxPoolRequests = 50
	MaxPoolService  = 1_000
	MaxRetryClients = 200
	MaxRetryRetries = 10
	MaxRetryService = 500
	MaxBackoffMs    = 5_000
	// MaxClassTotalMs bounds the worst-case wall time any of these runners may spend.
	MaxClassTotalMs = 60_000
	DefaultP99Bound = 5_000
)

// DNSCondition is a stub resolver on loopback plus a client with a timeout, retries and an optional cache.
// Queries are numbered as the stub receives them. From query number degraded_after on, the stub answers
// after latency_ms. Names are numbered; the first nxdomain_percent of them answer NXDOMAIN. Both are
// selected, not random, so a run is repeatable apart from scheduler timing.
type DNSCondition struct {
	LatencyMs       int  `json:"latency_ms"`
	DegradedAfter   int  `json:"degraded_after"`
	NXDomainPercent int  `json:"nxdomain_percent"`
	TimeoutMs       int  `json:"timeout_ms"`
	Retries         int  `json:"retries"`
	Cache           bool `json:"cache"`
	Lookups         int  `json:"lookups"`
	Names           int  `json:"names"`
}

func ValidateDNS(d DNSCondition) error {
	switch {
	case d.Lookups < 1 || d.Lookups > MaxDNSLookups:
		return fmt.Errorf("dns lookups must be 1..%d", MaxDNSLookups)
	case d.Names < 1 || d.Names > d.Lookups:
		return fmt.Errorf("dns names must be 1..lookups")
	case d.TimeoutMs < 1 || d.TimeoutMs > MaxNetMs:
		return fmt.Errorf("dns timeout_ms must be 1..%d", MaxNetMs)
	case d.LatencyMs < 0 || d.LatencyMs > MaxNetMs:
		return fmt.Errorf("dns latency_ms must be 0..%d", MaxNetMs)
	case d.Retries < 0 || d.Retries > MaxDNSRetries:
		return fmt.Errorf("dns retries must be 0..%d", MaxDNSRetries)
	case d.NXDomainPercent < 0 || d.NXDomainPercent > 100:
		return fmt.Errorf("dns nxdomain_percent must be 0..100")
	case d.DegradedAfter < 0 || d.DegradedAfter > MaxDNSLookups*(MaxDNSRetries+1):
		return fmt.Errorf("dns degraded_after out of range")
	}
	if int64(d.Lookups)*int64(d.Retries+1)*int64(d.TimeoutMs) > MaxClassTotalMs {
		return fmt.Errorf("dns lookups * (retries+1) * timeout_ms must be <= %d ms", MaxClassTotalMs)
	}
	return nil
}

// CertCondition is a rotation policy run on a virtual clock measured in hours since the first issue.
// A job checks the certificate every check_interval_hours and reissues it when no more than
// rotation_window_hours remain (rotation_window_hours 0 means no automatic rotation). probe_hours are
// the moments clients handshook. Each probe is a real TLS handshake on loopback whose verifier is set to
// that virtual time, so the standard library, not this code, decides whether the certificate is valid.
type CertCondition struct {
	LifetimeHours       int   `json:"lifetime_hours"`
	RotationWindowHours int   `json:"rotation_window_hours"`
	CheckIntervalHours  int   `json:"check_interval_hours"`
	ProbeHours          []int `json:"probe_hours"`
}

func ValidateCert(c CertCondition) error {
	switch {
	case c.LifetimeHours < 1 || c.LifetimeHours > MaxCertHours:
		return fmt.Errorf("cert lifetime_hours must be 1..%d", MaxCertHours)
	case c.RotationWindowHours < 0 || c.RotationWindowHours > c.LifetimeHours:
		return fmt.Errorf("cert rotation_window_hours must be 0..lifetime_hours")
	case c.RotationWindowHours > 0 && (c.CheckIntervalHours < 1 || c.CheckIntervalHours > MaxCertHours):
		return fmt.Errorf("cert check_interval_hours must be 1..%d when rotation is on", MaxCertHours)
	case len(c.ProbeHours) < 1 || len(c.ProbeHours) > MaxCertProbes:
		return fmt.Errorf("cert probe_hours must have 1..%d entries", MaxCertProbes)
	}
	for _, p := range c.ProbeHours {
		if p < 0 || p > MaxCertHours {
			return fmt.Errorf("cert probe hour must be 0..%d", MaxCertHours)
		}
	}
	return nil
}

// PoolCondition is a bounded connection pool in front of a loopback server. Each of clients goroutines
// issues requests_per_client requests, each holding one connection for service_ms. leak_every N means
// every Nth request (counted across clients) never returns its connection, the classic leak.
type PoolCondition struct {
	PoolSize          int `json:"pool_size"`
	AcquireTimeoutMs  int `json:"acquire_timeout_ms"`
	Clients           int `json:"clients"`
	RequestsPerClient int `json:"requests_per_client"`
	ServiceMs         int `json:"service_ms"`
	LeakEvery         int `json:"leak_every"`
	ExpectedMaxP99Ms  int `json:"expected_max_p99_ms"`
}

func ValidatePool(p PoolCondition) error {
	switch {
	case p.PoolSize < 1 || p.PoolSize > MaxPoolSize:
		return fmt.Errorf("pool pool_size must be 1..%d", MaxPoolSize)
	case p.Clients < 1 || p.Clients > MaxPoolClients:
		return fmt.Errorf("pool clients must be 1..%d", MaxPoolClients)
	case p.RequestsPerClient < 1 || p.RequestsPerClient > MaxPoolRequests:
		return fmt.Errorf("pool requests_per_client must be 1..%d", MaxPoolRequests)
	case p.ServiceMs < 0 || p.ServiceMs > MaxPoolService:
		return fmt.Errorf("pool service_ms must be 0..%d", MaxPoolService)
	case p.AcquireTimeoutMs < 1 || p.AcquireTimeoutMs > MaxNetMs:
		return fmt.Errorf("pool acquire_timeout_ms must be 1..%d", MaxNetMs)
	case p.LeakEvery < 0 || p.LeakEvery > MaxPoolClients*MaxPoolRequests:
		return fmt.Errorf("pool leak_every out of range")
	case p.ExpectedMaxP99Ms < 0 || p.ExpectedMaxP99Ms > MaxNetMs:
		return fmt.Errorf("pool expected_max_p99_ms must be 0..%d", MaxNetMs)
	}
	// Clients run in parallel; one client waits at most acquire_timeout and serves service_ms per request.
	if int64(p.RequestsPerClient)*int64(p.AcquireTimeoutMs+p.ServiceMs) > MaxClassTotalMs {
		return fmt.Errorf("pool requests_per_client * (acquire_timeout_ms + service_ms) must be <= %d ms", MaxClassTotalMs)
	}
	return nil
}

// RetryCondition is a loopback server that serves at most capacity requests at once and rejects the
// rest, and clients that all start together and retry with exponential backoff: delay k is
// min(backoff_cap_ms, backoff_ms * 2^k) (no cap when backoff_cap_ms is 0), shortened by up to
// jitter_percent of itself by a deterministic per-client, per-attempt hash. Not random: no seed.
type RetryCondition struct {
	Clients          int `json:"clients"`
	Capacity         int `json:"capacity"`
	ServiceMs        int `json:"service_ms"`
	BackoffMs        int `json:"backoff_ms"`
	BackoffCapMs     int `json:"backoff_cap_ms"`
	JitterPercent    int `json:"jitter_percent"`
	MaxRetries       int `json:"max_retries"`
	ExpectedMaxP99Ms int `json:"expected_max_p99_ms"`
}

// BackoffDelayMs is the un-jittered delay before retry k (k from 0).
func (r RetryCondition) BackoffDelayMs(k int) int64 {
	d := int64(r.BackoffMs)
	for i := 0; i < k && d < MaxNetMs; i++ {
		d *= 2
	}
	if r.BackoffCapMs > 0 && d > int64(r.BackoffCapMs) {
		d = int64(r.BackoffCapMs)
	}
	if d > MaxNetMs {
		d = MaxNetMs
	}
	return d
}

func ValidateRetry(r RetryCondition) error {
	switch {
	case r.Clients < 1 || r.Clients > MaxRetryClients:
		return fmt.Errorf("retry clients must be 1..%d", MaxRetryClients)
	case r.Capacity < 1 || r.Capacity > MaxRetryClients:
		return fmt.Errorf("retry capacity must be 1..%d", MaxRetryClients)
	case r.ServiceMs < 1 || r.ServiceMs > MaxRetryService:
		return fmt.Errorf("retry service_ms must be 1..%d", MaxRetryService)
	case r.BackoffMs < 1 || r.BackoffMs > MaxBackoffMs:
		return fmt.Errorf("retry backoff_ms must be 1..%d", MaxBackoffMs)
	case r.BackoffCapMs < 0 || r.BackoffCapMs > MaxNetMs:
		return fmt.Errorf("retry backoff_cap_ms must be 0..%d", MaxNetMs)
	case r.JitterPercent < 0 || r.JitterPercent > 100:
		return fmt.Errorf("retry jitter_percent must be 0..100")
	case r.MaxRetries < 0 || r.MaxRetries > MaxRetryRetries:
		return fmt.Errorf("retry max_retries must be 0..%d", MaxRetryRetries)
	case r.ExpectedMaxP99Ms < 0 || r.ExpectedMaxP99Ms > MaxNetMs:
		return fmt.Errorf("retry expected_max_p99_ms must be 0..%d", MaxNetMs)
	}
	total := int64(r.MaxRetries+1) * int64(r.ServiceMs)
	for k := 0; k < r.MaxRetries; k++ {
		total += r.BackoffDelayMs(k)
	}
	if total > MaxClassTotalMs {
		return fmt.Errorf("retry worst-case duration must be <= %d ms", MaxClassTotalMs)
	}
	return nil
}
