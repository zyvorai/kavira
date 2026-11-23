// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package bundle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Hard limits. A bundle is untrusted input and the runner really allocates, sleeps and dials, so every
// quantity it controls is bounded before anything executes.
const (
	MaxBundleBytes   = 1 << 20 // raw JSON size, matches the API limit
	MaxTextLen       = 4096    // id, service, symptom, notes, config values
	MaxIDLen         = 128
	MaxNotes         = 64
	MaxTimeline      = 100
	MaxConfigKeys    = 256
	MaxConfigKeyLen  = 256
	MaxRequests      = 1024
	MaxWorkloadBytes = 512 << 20 // requests * bytes_per_request
	MaxLimitBytes    = 1 << 40   // a captured limit may be large; only the workload allocates
	MaxNetAttempts   = 200
	MaxNetMs         = 60_000
	MaxNetTotalMs    = 60_000 // attempts * worst per-attempt time
	MaxBandwidthBps  = 1 << 30
	MaxResponseBytes = 1 << 20
)

const (
	ClassOOM     = "container_oom"
	ClassNetwork = "network_timeout"
	ClassConfig  = "config_regression"
	ClassDisk    = "disk_pressure"
	ClassDNS     = "dns_failure"
	ClassCert    = "certificate_expiry"
	ClassPool    = "connection_pool_exhaustion"
	ClassRetry   = "retry_storm"
)

// Classes lists every supported class in display order.
var Classes = []string{ClassOOM, ClassNetwork, ClassConfig, ClassDisk, ClassDNS, ClassCert, ClassPool, ClassRetry}

// Bundle is captured production evidence for one supported incident.
// It is not a recording. Runners rebuild only the conditions named here.
type Bundle struct {
	ID       string            `json:"id"`
	Class    string            `json:"class"`
	Service  string            `json:"service"`
	Symptom  string            `json:"symptom"`
	Evidence Evidence          `json:"evidence"`
	Workload Workload          `json:"workload"`
	Network  *NetworkCondition `json:"network,omitempty"`
	Configs  *ConfigPair       `json:"configs,omitempty"`
	DNS      *DNSCondition     `json:"dns,omitempty"`
	Cert     *CertCondition    `json:"cert,omitempty"`
	Pool     *PoolCondition    `json:"pool,omitempty"`
	Retry    *RetryCondition   `json:"retry,omitempty"`
	// Timeline is optional, human-captured, and used only by the postmortem draft. It is kept in the
	// order supplied and never parsed as time: KAVIRA does not verify it.
	Timeline []TimelineEvent `json:"timeline,omitempty"`
}

// TimelineEvent is one captured moment. At is free text (for example an RFC 3339 time or "T-5m").
type TimelineEvent struct {
	At    string `json:"at"`
	Event string `json:"event"`
}

type Evidence struct {
	MemoryLimitBytes  int64    `json:"memory_limit_bytes,omitempty"`
	ObservedPeakBytes int64    `json:"observed_peak_bytes,omitempty"`
	DiskLimitBytes    int64    `json:"disk_limit_bytes,omitempty"`
	ObservedUsedBytes int64    `json:"observed_used_bytes,omitempty"`
	RestartCount      int      `json:"restart_count,omitempty"`
	P99Ms             int      `json:"p99_ms,omitempty"`
	DeployChange      string   `json:"deploy_change,omitempty"`
	Notes             []string `json:"notes,omitempty"`
}

type Workload struct {
	Name             string `json:"name"`
	Requests         int    `json:"requests"`
	BytesPerRequest  int64  `json:"bytes_per_request"`
	Hold             bool   `json:"hold"`
	ExpectedMaxP99Ms int    `json:"expected_max_p99_ms"`
}

type NetworkCondition struct {
	DelayMs     int  `json:"delay_ms"`
	LossPercent int  `json:"loss_percent"`
	TimeoutMs   int  `json:"timeout_ms"`
	PolicyDeny  bool `json:"policy_deny"`
	Attempts    int  `json:"attempts"`
	// Toxiproxy-style arms. All optional; zero means the arm is off, so older bundles are unchanged.
	JitterMs        int `json:"jitter_ms,omitempty"`         // 0..jitter_ms added to delay, selected deterministically per attempt
	BandwidthBps    int `json:"bandwidth_bps,omitempty"`     // response paced at this many bytes per second
	ResetAfterBytes int `json:"reset_after_bytes,omitempty"` // connection reset after this many response bytes
	SlowCloseMs     int `json:"slow_close_ms,omitempty"`     // server holds the connection open this long after the response
	ResponseBytes   int `json:"response_bytes,omitempty"`    // response size (default 2, "ok")
}

type ConfigPair struct {
	Previous map[string]string `json:"previous"`
	Current  map[string]string `json:"current"`
}

func Load(path string) (Bundle, error) {
	f, err := os.Open(path)
	if err != nil {
		return Bundle{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxBundleBytes+1))
	if err != nil {
		return Bundle{}, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (Bundle, error) {
	if len(raw) > MaxBundleBytes {
		return Bundle{}, fmt.Errorf("bundle exceeds %d bytes", MaxBundleBytes)
	}
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return Bundle{}, err
	}
	if err := b.Validate(); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

func (b Bundle) Validate() error {
	if b.ID == "" {
		return fmt.Errorf("bundle id is required")
	}
	if err := b.validateText(); err != nil {
		return err
	}
	switch b.Class {
	case ClassOOM, ClassNetwork, ClassConfig, ClassDisk, ClassDNS, ClassCert, ClassPool, ClassRetry:
	default:
		return fmt.Errorf("unsupported class %q", b.Class)
	}
	if b.Class == ClassOOM {
		if b.Workload.Requests < 1 {
			return fmt.Errorf("oom workload requests must be >= 1")
		}
		if b.Workload.BytesPerRequest < 1 {
			return fmt.Errorf("oom workload bytes_per_request must be >= 1")
		}
		if b.Evidence.MemoryLimitBytes < 1 {
			return fmt.Errorf("oom evidence memory_limit_bytes must be >= 1")
		}
		if err := b.validateWorkload(b.Evidence.MemoryLimitBytes); err != nil {
			return err
		}
	}
	if b.Class == ClassDisk {
		if b.Workload.Requests < 1 {
			return fmt.Errorf("disk workload requests must be >= 1")
		}
		if b.Workload.BytesPerRequest < 1 {
			return fmt.Errorf("disk workload bytes_per_request must be >= 1")
		}
		if b.Evidence.DiskLimitBytes < 1 {
			return fmt.Errorf("disk evidence disk_limit_bytes must be >= 1")
		}
		if err := b.validateWorkload(b.Evidence.DiskLimitBytes); err != nil {
			return err
		}
	}
	if b.Class == ClassNetwork {
		if b.Network == nil {
			return fmt.Errorf("network bundle requires network conditions")
		}
		if b.Network.Attempts < 1 {
			return fmt.Errorf("network attempts must be >= 1")
		}
		if b.Network.TimeoutMs < 1 {
			return fmt.Errorf("network timeout_ms must be >= 1")
		}
		if err := ValidateNetwork(*b.Network); err != nil {
			return err
		}
	}
	switch b.Class {
	case ClassDNS:
		if b.DNS == nil {
			return fmt.Errorf("dns bundle requires a dns condition")
		}
		return ValidateDNS(*b.DNS)
	case ClassCert:
		if b.Cert == nil {
			return fmt.Errorf("certificate bundle requires a cert condition")
		}
		return ValidateCert(*b.Cert)
	case ClassPool:
		if b.Pool == nil {
			return fmt.Errorf("pool bundle requires a pool condition")
		}
		return ValidatePool(*b.Pool)
	case ClassRetry:
		if b.Retry == nil {
			return fmt.Errorf("retry bundle requires a retry condition")
		}
		return ValidateRetry(*b.Retry)
	}
	if b.Class == ClassConfig {
		if b.Configs == nil || len(b.Configs.Previous) == 0 || len(b.Configs.Current) == 0 {
			return fmt.Errorf("config bundle requires previous and current configs")
		}
		for _, m := range []map[string]string{b.Configs.Previous, b.Configs.Current} {
			if len(m) > MaxConfigKeys {
				return fmt.Errorf("config has more than %d keys", MaxConfigKeys)
			}
			for k, v := range m {
				if len(k) > MaxConfigKeyLen || len(v) > MaxTextLen {
					return fmt.Errorf("config key or value is too long")
				}
			}
		}
	}
	return nil
}

func (b Bundle) validateText() error {
	if len(b.ID) > MaxIDLen {
		return fmt.Errorf("bundle id is longer than %d bytes", MaxIDLen)
	}
	for _, s := range []string{b.Service, b.Symptom, b.Evidence.DeployChange} {
		if len(s) > MaxTextLen {
			return fmt.Errorf("a text field is longer than %d bytes", MaxTextLen)
		}
	}
	if len(b.Evidence.Notes) > MaxNotes {
		return fmt.Errorf("more than %d evidence notes", MaxNotes)
	}
	for _, n := range b.Evidence.Notes {
		if len(n) > MaxTextLen {
			return fmt.Errorf("an evidence note is longer than %d bytes", MaxTextLen)
		}
	}
	if len(b.Timeline) > MaxTimeline {
		return fmt.Errorf("more than %d timeline events", MaxTimeline)
	}
	for _, e := range b.Timeline {
		if len(e.At) > 64 || len(e.Event) > 512 {
			return fmt.Errorf("a timeline entry is too long (at <= 64, event <= 512 bytes)")
		}
	}
	return nil
}

// validateWorkload bounds what the memory/disk runner will really allocate.
func (b Bundle) validateWorkload(limit int64) error {
	w := b.Workload
	if limit > MaxLimitBytes {
		return fmt.Errorf("limit exceeds %d bytes", int64(MaxLimitBytes))
	}
	if w.Requests > MaxRequests {
		return fmt.Errorf("workload requests must be <= %d", MaxRequests)
	}
	if w.BytesPerRequest > MaxWorkloadBytes || w.BytesPerRequest*int64(w.Requests) > MaxWorkloadBytes {
		return fmt.Errorf("workload requests * bytes_per_request must be <= %d bytes", int64(MaxWorkloadBytes))
	}
	return nil
}

// ValidateNetwork bounds a network condition (also applied to repaired conditions read from a regression test).
func ValidateNetwork(n NetworkCondition) error {
	if n.Attempts < 1 || n.Attempts > MaxNetAttempts {
		return fmt.Errorf("network attempts must be 1..%d", MaxNetAttempts)
	}
	if n.TimeoutMs < 1 || n.TimeoutMs > MaxNetMs {
		return fmt.Errorf("network timeout_ms must be 1..%d", MaxNetMs)
	}
	if n.DelayMs < 0 || n.DelayMs > MaxNetMs {
		return fmt.Errorf("network delay_ms must be 0..%d", MaxNetMs)
	}
	if n.LossPercent < 0 || n.LossPercent > 100 {
		return fmt.Errorf("network loss_percent must be 0..100")
	}
	if n.JitterMs < 0 || n.JitterMs > MaxNetMs {
		return fmt.Errorf("network jitter_ms must be 0..%d", MaxNetMs)
	}
	if n.SlowCloseMs < 0 || n.SlowCloseMs > MaxNetMs {
		return fmt.Errorf("network slow_close_ms must be 0..%d", MaxNetMs)
	}
	if n.BandwidthBps < 0 || n.BandwidthBps > MaxBandwidthBps {
		return fmt.Errorf("network bandwidth_bps must be 0..%d", MaxBandwidthBps)
	}
	if n.ResetAfterBytes < 0 || n.ResetAfterBytes > MaxResponseBytes {
		return fmt.Errorf("network reset_after_bytes must be 0..%d", MaxResponseBytes)
	}
	if n.ResponseBytes < 0 || n.ResponseBytes > MaxResponseBytes {
		return fmt.Errorf("network response_bytes must be 0..%d", MaxResponseBytes)
	}
	if n.ResponseBytes != 0 && n.ResponseBytes < 2 {
		return fmt.Errorf("network response_bytes must be 0 (default) or >= 2")
	}
	// Server-side worst case per attempt: delay, jitter, paced transfer and slow close all run in sequence.
	worst := int64(n.DelayMs) + int64(n.JitterMs) + int64(n.SlowCloseMs)
	if n.BandwidthBps > 0 {
		worst += TransferMs(n)
	}
	if int64(n.TimeoutMs) > worst {
		worst = int64(n.TimeoutMs)
	}
	if worst*int64(n.Attempts) > MaxNetTotalMs {
		return fmt.Errorf("network attempts * max(timeout_ms, delay+jitter+transfer+slow_close) must be <= %d ms", MaxNetTotalMs)
	}
	return nil
}

func Diff(prev, curr map[string]string) map[string][2]string {
	out := map[string][2]string{}
	keys := map[string]struct{}{}
	for k := range prev {
		keys[k] = struct{}{}
	}
	for k := range curr {
		keys[k] = struct{}{}
	}
	for k := range keys {
		if prev[k] != curr[k] {
			out[k] = [2]string{prev[k], curr[k]}
		}
	}
	return out
}

// TransferMs is how long a paced response takes: response_bytes at bandwidth_bps (0 when uncapped).
func TransferMs(n NetworkCondition) int64 {
	if n.BandwidthBps <= 0 {
		return 0
	}
	size := n.ResponseBytes
	if size == 0 {
		size = 2
	}
	return int64(size) * 1000 / int64(n.BandwidthBps)
}
