// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package runner

import (
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"sync"
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

// RunDisk reconstructs a volume quota: each request "writes" bytes_per_request against limit.
// When hold is true, writes accumulate (like a log shipper that does not rotate). When false,
// each write is checked alone. This is a bounded stand-in, not a filesystem trace.
func RunDisk(w bundle.Workload, limit int64, hold bool) Result {
	r := RunOOM(w, limit, hold)
	r.Detail = fmt.Sprintf("disk hold=%t failures=%d/%d written_peak=%d limit=%d", hold, r.Failures, r.Attempts, r.PeakBytes, limit)
	return r
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
// Loss is selected, not random: the first lossPercent of attempts are reset. Jitter is a deterministic
// per-attempt hash, not a random draw. The optional Toxiproxy-style arms (bandwidth cap, reset after N
// bytes, slow close, response size) are off at zero.
func RunNetwork(n bundle.NetworkCondition) Result {
	if n.Attempts < 1 {
		n.Attempts = 1
	}
	attempts := n.Attempts
	size := responseSize(n)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{Attempts: attempts, Failures: attempts, Detail: err.Error()}
	}
	closeOnce := sync.OnceFunc(func() { _ = ln.Close() })
	defer closeOnce()

	var handlers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < attempts; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// One goroutine per connection: a slow attempt must not delay the next one.
			handlers.Add(1)
			go func(i int) {
				defer handlers.Done()
				handle(conn, n, i)
			}(i)
		}
	}()

	samples := make([]time.Duration, 0, attempts)
	failures := 0
	timeout := time.Duration(n.TimeoutMs) * time.Millisecond
	dialer := net.Dialer{Timeout: timeout}
	for i := 0; i < attempts; i++ {
		start := time.Now()
		conn, err := dialer.Dial("tcp", ln.Addr().String())
		if err != nil {
			failures++
			samples = append(samples, time.Since(start))
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(timeout))
		buf := make([]byte, size)
		_, err = io.ReadFull(conn, buf)
		if err == nil && n.SlowCloseMs > 0 {
			// A client that waits for the server to close (read until EOF) pays for a slow close.
			_, err = io.Copy(io.Discard, conn)
		}
		_ = conn.Close()
		elapsed := time.Since(start)
		samples = append(samples, elapsed)
		if err != nil || string(buf[:2]) != "ok" {
			failures++
		}
	}
	// Close first: if any dial failed, the accept loop would otherwise wait forever.
	closeOnce()
	<-done
	handlers.Wait()
	p99 := percentile(samples, 99)
	return Result{
		Failures: failures,
		Attempts: attempts,
		P99Ms:    int(p99 / time.Millisecond),
		Detail:   networkDetail(n, failures, attempts),
		Passed:   failures == 0,
	}
}

func networkDetail(n bundle.NetworkCondition, failures, attempts int) string {
	d := fmt.Sprintf("delay=%dms loss=%d%% timeout=%dms policy_deny=%t", n.DelayMs, n.LossPercent, n.TimeoutMs, n.PolicyDeny)
	if n.JitterMs > 0 {
		d += fmt.Sprintf(" jitter=%dms", n.JitterMs)
	}
	if n.BandwidthBps > 0 {
		d += fmt.Sprintf(" bandwidth=%dB/s", n.BandwidthBps)
	}
	if n.ResetAfterBytes > 0 {
		d += fmt.Sprintf(" reset_after=%dB", n.ResetAfterBytes)
	}
	if n.SlowCloseMs > 0 {
		d += fmt.Sprintf(" slow_close=%dms", n.SlowCloseMs)
	}
	if n.ResponseBytes > 0 {
		d += fmt.Sprintf(" response=%dB", n.ResponseBytes)
	}
	return fmt.Sprintf("%s failures=%d/%d", d, failures, attempts)
}

func responseSize(n bundle.NetworkCondition) int {
	if n.ResponseBytes >= 2 {
		return n.ResponseBytes
	}
	return 2
}

func handle(conn net.Conn, n bundle.NetworkCondition, i int) {
	defer conn.Close()
	if n.PolicyDeny {
		return
	}
	if n.LossPercent > 0 && i*100/n.Attempts < n.LossPercent {
		return
	}
	delay := time.Duration(n.DelayMs) * time.Millisecond
	if n.JitterMs > 0 {
		delay += time.Duration(mix(uint64(i), 0)%uint64(n.JitterMs+1)) * time.Millisecond
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	size := responseSize(n)
	body := make([]byte, size)
	copy(body, "ok")
	for j := 2; j < size; j++ {
		body[j] = 'x'
	}
	// A client that gave up must not pin this goroutine: every write has a deadline.
	_ = conn.SetWriteDeadline(time.Now().Add(2*time.Duration(n.TimeoutMs)*time.Millisecond + 100*time.Millisecond))
	if n.ResetAfterBytes > 0 && n.ResetAfterBytes < size {
		_, _ = conn.Write(body[:n.ResetAfterBytes])
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetLinger(0) // close sends RST, like Toxiproxy reset_peer
		}
		return
	}
	if n.BandwidthBps > 0 {
		chunk := n.BandwidthBps / 100
		if chunk < 1 {
			chunk = 1
		}
		if chunk > 4096 {
			chunk = 4096
		}
		for off := 0; off < size; {
			end := off + chunk
			if end > size {
				end = size
			}
			if _, err := conn.Write(body[off:end]); err != nil {
				return
			}
			time.Sleep(time.Duration(int64(end-off) * int64(time.Second) / int64(n.BandwidthBps)))
			off = end
		}
	} else if _, err := conn.Write(body); err != nil {
		return
	}
	if n.SlowCloseMs > 0 {
		// Hold the connection open, but return as soon as the client closes it.
		_ = conn.SetReadDeadline(time.Now().Add(time.Duration(n.SlowCloseMs) * time.Millisecond))
		_, _ = io.Copy(io.Discard, conn)
	}
}

// mix is splitmix64: a deterministic hash used where a random draw would otherwise be needed.
func mix(a, b uint64) uint64 {
	z := a*0x9e3779b97f4a7c15 + b*0xbf58476d1ce4e5b9 + 0x94d049bb133111eb
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// maxConfigBatch bounds the allocation (batch*256KiB) and sleep (batch*2ms) in RunConfig.
const maxConfigBatch = 4096

func RunConfig(cfg map[string]string) Result {
	batch := atoi(cfg["batch_size"], 1)
	timeoutMs := atoi(cfg["timeout_ms"], 1000)
	memMiB := atoi(cfg["memory_limit_mib"], 64)
	limit := int64(memMiB) * 1024 * 1024
	need := int64(batch) * 256 * 1024
	if batch < 1 || memMiB < 1 || batch > maxConfigBatch {
		return Result{
			Failures: 1, Attempts: 1, LimitBytes: limit,
			Detail: fmt.Sprintf("invalid config: batch=%d (1..%d) mem=%dMiB (>=1)", batch, maxConfigBatch, memMiB),
		}
	}
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
