// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package runner

import (
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
)

// RunRetry reconstructs a retry storm (thundering herd). A loopback server serves at most capacity
// requests at once and answers "no" immediately to the rest. clients start together, and each retries
// a rejected request after an exponential backoff, up to max_retries times. A client that is still
// rejected after its last retry is a failure. With no jitter every client retries at the same instant,
// so each wave admits at most capacity clients; jitter spreads the waves.
//
// Jitter is deterministic: delay * (1 - jitter_percent/100 * u), with u in [0,1) a hash of the client
// and the attempt. There is no random source and no seed. Timing-dependent: real sleeps, so a client
// that lands next to a service-time boundary can flip. Failure counts are exact for the unjittered
// case when service_ms is well below backoff_ms.
func RunRetry(r bundle.RetryCondition) Result {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{Attempts: r.Clients, Failures: r.Clients, Detail: err.Error()}
	}
	var (
		inflight atomic.Int64
		rejected atomic.Int64
		srv      sync.WaitGroup
	)
	srv.Add(1)
	go func() {
		defer srv.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			srv.Add(1)
			go func() {
				defer srv.Done()
				defer conn.Close()
				if inflight.Add(1) > int64(r.Capacity) {
					inflight.Add(-1)
					rejected.Add(1)
					_, _ = conn.Write([]byte("no"))
					return
				}
				time.Sleep(time.Duration(r.ServiceMs) * time.Millisecond)
				_, _ = conn.Write([]byte("ok"))
				inflight.Add(-1)
			}()
		}
	}()

	var (
		wg       sync.WaitGroup
		failures atomic.Int64
		attempts atomic.Int64
		sMu      sync.Mutex
		samples  = make([]time.Duration, 0, r.Clients)
	)
	start := make(chan struct{})
	for c := 0; c < r.Clients; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			t0 := time.Now()
			ok := false
			for k := 0; k <= r.MaxRetries && !ok; k++ {
				if k > 0 {
					d := float64(r.BackoffDelayMs(k - 1))
					u := float64(mix(uint64(c), uint64(k))>>11) / (1 << 53)
					d *= 1 - float64(r.JitterPercent)/100*u
					time.Sleep(time.Duration(d * float64(time.Millisecond)))
				}
				attempts.Add(1)
				ok = tryOnce(ln.Addr().String(), time.Duration(r.ServiceMs)*time.Millisecond+5*time.Second)
			}
			if !ok {
				failures.Add(1)
			}
			sMu.Lock()
			samples = append(samples, time.Since(t0))
			sMu.Unlock()
		}()
	}
	close(start)
	wg.Wait()
	_ = ln.Close()
	srv.Wait()
	f := int(failures.Load())
	return Result{
		Failures: f,
		Attempts: r.Clients,
		P99Ms:    int(percentile(samples, 99) / time.Millisecond),
		Detail: fmt.Sprintf("clients=%d capacity=%d service=%dms backoff=%dms cap=%dms jitter=%d%% max_retries=%d requests_sent=%d rejected=%d gave_up=%d/%d",
			r.Clients, r.Capacity, r.ServiceMs, r.BackoffMs, r.BackoffCapMs, r.JitterPercent, r.MaxRetries, attempts.Load(), rejected.Load(), f, r.Clients),
		Passed: f == 0,
	}
}

func tryOnce(addr string, limit time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, limit)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(limit))
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return false
	}
	return string(buf) == "ok"
}
