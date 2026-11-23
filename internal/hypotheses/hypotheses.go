// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package hypotheses

import (
	"fmt"

	"github.com/zyvorai/kavira/internal/bundle"
)

// Hypothesis is a proposed explanation. Authoritative is always false:
// execution, not the proposer, decides.
type Hypothesis struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Explanation   string   `json:"explanation"`
	Evidence      []string `json:"evidence"`
	Experiment    string   `json:"experiment"`
	Rank          int      `json:"rank"`
	Status        string   `json:"status"`
	Authoritative bool     `json:"authoritative"`
}

func Propose(b bundle.Bundle) []Hypothesis {
	var out []Hypothesis
	rank := 1
	add := func(h Hypothesis) {
		h.Rank = rank
		h.Status = "untested"
		h.Authoritative = false
		rank++
		out = append(out, h)
	}

	switch b.Class {
	case bundle.ClassOOM:
		add(Hypothesis{
			ID:   "resource_contention",
			Name: "Resource contention",
			Explanation: fmt.Sprintf(
				"Captured peak %d bytes against limit %d, with %d restarts. A bounded workload held under that limit should fail the same way if the limit is the cause.",
				b.Evidence.ObservedPeakBytes, b.Evidence.MemoryLimitBytes, b.Evidence.RestartCount,
			),
			Evidence: []string{
				fmt.Sprintf("memory_limit_bytes=%d", b.Evidence.MemoryLimitBytes),
				fmt.Sprintf("observed_peak_bytes=%d", b.Evidence.ObservedPeakBytes),
				fmt.Sprintf("restart_count=%d", b.Evidence.RestartCount),
			},
			Experiment: "Replay the bounded workload under the captured memory limit, holding allocations.",
		})
	case bundle.ClassNetwork:
		n := b.Network
		add(Hypothesis{
			ID:   "network_interruption",
			Name: "Network interruption",
			Explanation: fmt.Sprintf(
				"Captured delay %dms (jitter %dms), loss %d%%, timeout %dms, policy_deny=%t, bandwidth %dB/s (0 = uncapped), reset after %dB (0 = off), slow close %dms. A local dial under those conditions should time out or reset if the path is the cause.",
				n.DelayMs, n.JitterMs, n.LossPercent, n.TimeoutMs, n.PolicyDeny, n.BandwidthBps, n.ResetAfterBytes, n.SlowCloseMs,
			),
			Evidence: []string{
				fmt.Sprintf("delay_ms=%d", n.DelayMs),
				fmt.Sprintf("loss_percent=%d", n.LossPercent),
				fmt.Sprintf("timeout_ms=%d", n.TimeoutMs),
				fmt.Sprintf("policy_deny=%t", n.PolicyDeny),
			},
			Experiment: "Recreate selected delay, loss, or policy against a local listener. One factor changes in the repair arm.",
		})
	case bundle.ClassDNS:
		d := b.DNS
		add(Hypothesis{
			ID:   "dns_resolution",
			Name: "DNS resolution failure or slow resolver",
			Explanation: fmt.Sprintf(
				"Captured resolver latency %dms (from query %d), NXDOMAIN for %d%% of names, client timeout %dms, %d retries, cache=%t. A stub resolver on loopback with those conditions should fail lookups the same way if resolution is the cause.",
				d.LatencyMs, d.DegradedAfter, d.NXDomainPercent, d.TimeoutMs, d.Retries, d.Cache,
			),
			Evidence: []string{
				fmt.Sprintf("latency_ms=%d", d.LatencyMs),
				fmt.Sprintf("nxdomain_percent=%d", d.NXDomainPercent),
				fmt.Sprintf("timeout_ms=%d", d.TimeoutMs),
				fmt.Sprintf("cache=%t", d.Cache),
			},
			Experiment: "Resolve the captured number of names through a loopback stub resolver. One factor (timeout, cache, or names) changes in the repair arm.",
		})
	case bundle.ClassCert:
		c := b.Cert
		add(Hypothesis{
			ID:   "certificate_expiry",
			Name: "Certificate expired between rotations",
			Explanation: fmt.Sprintf(
				"Captured lifetime %dh, rotation window %dh, check interval %dh. A rotation job that checks every %dh and rotates inside a %dh window can let a certificate expire between checks, which a real handshake at the probe hours should show.",
				c.LifetimeHours, c.RotationWindowHours, c.CheckIntervalHours, c.CheckIntervalHours, c.RotationWindowHours,
			),
			Evidence: []string{
				fmt.Sprintf("lifetime_hours=%d", c.LifetimeHours),
				fmt.Sprintf("rotation_window_hours=%d", c.RotationWindowHours),
				fmt.Sprintf("check_interval_hours=%d", c.CheckIntervalHours),
				fmt.Sprintf("probe_hours=%d entries", len(c.ProbeHours)),
			},
			Experiment: "Generate throwaway certificates on a virtual clock and handshake over loopback TLS at each probe hour.",
		})
	case bundle.ClassPool:
		p := b.Pool
		add(Hypothesis{
			ID:   "pool_exhaustion",
			Name: "Connection pool exhaustion",
			Explanation: fmt.Sprintf(
				"Captured pool size %d, %d clients, %dms per request, acquire timeout %dms, leak_every=%d. A bounded pool in front of a loopback server should run out of connections the same way if the pool is the cause.",
				p.PoolSize, p.Clients, p.ServiceMs, p.AcquireTimeoutMs, p.LeakEvery,
			),
			Evidence: []string{
				fmt.Sprintf("pool_size=%d", p.PoolSize),
				fmt.Sprintf("clients=%d", p.Clients),
				fmt.Sprintf("acquire_timeout_ms=%d", p.AcquireTimeoutMs),
				fmt.Sprintf("leak_every=%d", p.LeakEvery),
			},
			Experiment: "Run the clients against a loopback server through a pool of the captured size. One factor changes in each repair arm.",
		})
	case bundle.ClassRetry:
		r := b.Retry
		add(Hypothesis{
			ID:   "retry_amplification",
			Name: "Retry storm (synchronized retries)",
			Explanation: fmt.Sprintf(
				"Captured %d clients, server capacity %d, backoff %dms (cap %dms), jitter %d%%, %d retries. Clients that retry on the same schedule hit a capacity-limited server in waves; jitter should spread them.",
				r.Clients, r.Capacity, r.BackoffMs, r.BackoffCapMs, r.JitterPercent, r.MaxRetries,
			),
			Evidence: []string{
				fmt.Sprintf("clients=%d", r.Clients),
				fmt.Sprintf("capacity=%d", r.Capacity),
				fmt.Sprintf("jitter_percent=%d", r.JitterPercent),
				fmt.Sprintf("max_retries=%d", r.MaxRetries),
			},
			Experiment: "Start the captured number of clients together against a capacity-limited loopback server. Jitter changes in the repair arm.",
		})
	case bundle.ClassConfig:
		add(Hypothesis{
			ID:          "configuration_drift",
			Name:        "Configuration drift",
			Explanation: "Previous and current configs differ. The same workload run against both should diverge if the drift is the cause.",
			Evidence:    []string{"previous config snapshot", "current config snapshot"},
			Experiment:  "Run previous and current configurations against the same bounded workload.",
		})
	case bundle.ClassDisk:
		add(Hypothesis{
			ID:   "disk_quota",
			Name: "Disk quota exhaustion",
			Explanation: fmt.Sprintf(
				"Captured used %d bytes against volume limit %d, with %d restarts. A bounded write workload under that quota should fail the same way if the quota is the cause.",
				b.Evidence.ObservedUsedBytes, b.Evidence.DiskLimitBytes, b.Evidence.RestartCount,
			),
			Evidence: []string{
				fmt.Sprintf("disk_limit_bytes=%d", b.Evidence.DiskLimitBytes),
				fmt.Sprintf("observed_used_bytes=%d", b.Evidence.ObservedUsedBytes),
				fmt.Sprintf("restart_count=%d", b.Evidence.RestartCount),
			},
			Experiment: "Replay the bounded write workload under the captured disk quota, retaining writes when hold is set.",
		})
	}

	add(Hypothesis{
		ID:          "deploy_change",
		Name:        "Deployment change",
		Explanation: "A migration or rollout is in the evidence. Treated as context, not a verdict, until an experiment isolates it.",
		Evidence:    []string{emptyAs(b.Evidence.DeployChange, "no deploy change recorded")},
		Experiment:  "Not run in v0. Declared class owns the experiment.",
	})
	return out
}

func emptyAs(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func Mark(hs []Hypothesis, id, status string) []Hypothesis {
	out := make([]Hypothesis, len(hs))
	copy(out, hs)
	for i := range out {
		if out[i].ID == id {
			out[i].Status = status
		}
	}
	return out
}
