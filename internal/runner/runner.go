package runner

import (
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
)

// Result is what execution measured. Latency comes from the clock.
// Failure counts come from the captured condition, not from a model score.
type Result struct {
	Failures   int    `json:"failures"`
	Attempts   int    `json:"attempts"`
	PeakBytes  int64  `json:"peak_bytes"`
	LimitBytes int64  `json:"limit_bytes"`
	P99Ms      int    `json:"p99_ms"`
	Detail     string `json:"detail"`
	Passed     bool   `json:"passed"`
}

func RunOOM(w bundle.Workload, limit int64, hold bool) Result {
	if w.Requests < 1 {
		w.Requests = 1
	}
	samples := make([]time.Duration, 0, w.Requests)
	var held [][]byte
	var used, peak int64
	failures := 0
	for i := 0; i < w.Requests; i++ {
		start := time.Now()
		need := w.BytesPerRequest
		if hold && used+need > limit || !hold && need > limit {
			failures++
			held = nil
			used = 0
			samples = append(samples, time.Since(start))
			continue
		}
		buf := make([]byte, need)
		buf[0] = byte(i)
		buf[len(buf)-1] = 1
		if hold {
			held = append(held, buf)
			used += need
		}
		if used > peak {
			peak = used
		}
		if !hold && need > peak {
			peak = need
		}
		samples = append(samples, time.Since(start))
	}
	p99 := percentile(samples, 99)
	return Result{
		Failures:   failures,
		Attempts:   w.Requests,
		PeakBytes:  peak,
		LimitBytes: limit,
		P99Ms:      int(p99 / time.Millisecond),
		Detail:     fmt.Sprintf("hold=%t failures=%d/%d peak=%d limit=%d", hold, failures, w.Requests, peak, limit),
		Passed:     failures == 0,
	}
}

// RunNetwork dials a local listener under the captured conditions.
// Loss is selected, not random: the first lossPercent of attempts are reset.
func RunNetwork(n bundle.NetworkCondition) Result {
	attempts := n.Attempts
	if attempts < 1 {
		attempts = 1
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{Attempts: attempts, Failures: attempts, Detail: err.Error()}
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < attempts; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			handle(conn, n, i)
		}
	}()

	samples := make([]time.Duration, 0, attempts)
	failures := 0
	dialer := net.Dialer{Timeout: time.Duration(n.TimeoutMs) * time.Millisecond}
	for i := 0; i < attempts; i++ {
		start := time.Now()
		conn, err := dialer.Dial("tcp", ln.Addr().String())
		if err != nil {
			failures++
			samples = append(samples, time.Since(start))
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(time.Duration(n.TimeoutMs) * time.Millisecond))
		buf := make([]byte, 2)
		_, err = io.ReadFull(conn, buf)
		_ = conn.Close()
		elapsed := time.Since(start)
		samples = append(samples, elapsed)
		if err != nil || string(buf) != "ok" {
			failures++
		}
	}
	<-done
	p99 := percentile(samples, 99)
	return Result{
		Failures: failures,
		Attempts: attempts,
		P99Ms:    int(p99 / time.Millisecond),
		Detail:   fmt.Sprintf("delay=%dms loss=%d%% timeout=%dms policy_deny=%t failures=%d/%d", n.DelayMs, n.LossPercent, n.TimeoutMs, n.PolicyDeny, failures, attempts),
		Passed:   failures == 0,
	}
}

func handle(conn net.Conn, n bundle.NetworkCondition, i int) {
	defer conn.Close()
	if n.PolicyDeny {
		return
	}
	if n.LossPercent > 0 && i*100/n.Attempts < n.LossPercent {
		return
	}
	if n.DelayMs > 0 {
		time.Sleep(time.Duration(n.DelayMs) * time.Millisecond)
	}
	_, _ = conn.Write([]byte("ok"))
}

func RunConfig(cfg map[string]string) Result {
	batch := atoi(cfg["batch_size"], 1)
	timeoutMs := atoi(cfg["timeout_ms"], 1000)
	memMiB := atoi(cfg["memory_limit_mib"], 64)
	limit := int64(memMiB) * 1024 * 1024
	need := int64(batch) * 256 * 1024
	start := time.Now()
	if need > limit {
		return Result{
			Failures: 1, Attempts: 1, PeakBytes: need, LimitBytes: limit, P99Ms: 0,
			Detail: fmt.Sprintf("allocation %d exceeds limit %d (batch=%d mem=%dMiB)", need, limit, batch, memMiB),
		}
	}
	buf := make([]byte, need)
	buf[0] = 1
	// Bounded stand-in for captured work, proportional to batch. Not a production trace.
	time.Sleep(time.Duration(batch*2) * time.Millisecond)
	elapsed := time.Since(start)
	failed := elapsed > time.Duration(timeoutMs)*time.Millisecond
	failures := 0
	if failed {
		failures = 1
	}
	return Result{
		Failures: failures, Attempts: 1, PeakBytes: need, LimitBytes: limit,
		P99Ms:  int(elapsed / time.Millisecond),
		Detail: fmt.Sprintf("batch=%d timeout=%dms mem=%dMiB elapsed=%s", batch, timeoutMs, memMiB, elapsed.Round(time.Millisecond)),
		Passed: !failed,
	}
}

func atoi(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func percentile(samples []time.Duration, p int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), samples...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := (len(cp) - 1) * p / 100
	return cp[idx]
}
