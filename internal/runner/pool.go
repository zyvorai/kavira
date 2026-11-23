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

// RunPool reconstructs connection-pool exhaustion: a pool of at most pool_size real loopback connections
// in front of a server that takes service_ms per request. clients goroutines each issue
// requests_per_client requests. A request that cannot get a connection within acquire_timeout_ms fails
// ("pool exhausted"), which is how an undersized pool or a connection leak shows up.
//
// With leak_every N, every Nth request (numbered across clients in the order they reach the pool) keeps
// its connection forever, so the pool shrinks by one each time. Which request is the Nth depends on
// goroutine scheduling, so the leak count is exact but which client suffers it is not.
//
// Timing-dependent: a request waits for service_ms-sized slots, so a run that sits near
// acquire_timeout_ms is flaky. Failures are counted per request, never sampled.
func RunPool(p bundle.PoolCondition) Result {
	total := p.Clients * p.RequestsPerClient
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{Attempts: total, Failures: total, Detail: err.Error()}
	}
	var srv sync.WaitGroup
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
				one := make([]byte, 1)
				for {
					if _, err := io.ReadFull(conn, one); err != nil {
						return
					}
					time.Sleep(time.Duration(p.ServiceMs) * time.Millisecond)
					if _, err := conn.Write(one); err != nil {
						return
					}
				}
			}()
		}
	}()

	var (
		mu      sync.Mutex
		all     []net.Conn // every connection ever opened, closed at the end
		created int
		idle    = make(chan net.Conn, p.PoolSize)
		reqNo   atomic.Int64
		leaked  atomic.Int64
	)
	acquire := func(timeout time.Duration) (net.Conn, error) {
		select {
		case c := <-idle:
			return c, nil
		default:
		}
		mu.Lock()
		if created < p.PoolSize {
			created++
			mu.Unlock()
			c, err := net.Dial("tcp", ln.Addr().String())
			if err == nil {
				mu.Lock()
				all = append(all, c)
				mu.Unlock()
			}
			return c, err
		}
		mu.Unlock()
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case c := <-idle:
			return c, nil
		case <-t.C:
			return nil, fmt.Errorf("pool exhausted")
		}
	}

	var (
		wg       sync.WaitGroup
		failures atomic.Int64
		sMu      sync.Mutex
		samples  = make([]time.Duration, 0, total)
	)
	timeout := time.Duration(p.AcquireTimeoutMs) * time.Millisecond
	io2 := time.Duration(p.ServiceMs)*time.Millisecond + 5*time.Second
	start := make(chan struct{})
	for c := 0; c < p.Clients; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			one := []byte{1}
			for r := 0; r < p.RequestsPerClient; r++ {
				t0 := time.Now()
				conn, err := acquire(timeout)
				ok := err == nil
				if ok {
					_ = conn.SetDeadline(time.Now().Add(io2))
					if _, err := conn.Write(one); err == nil {
						_, err = io.ReadFull(conn, one)
						ok = err == nil
					} else {
						ok = false
					}
					n := reqNo.Add(1)
					if p.LeakEvery > 0 && n%int64(p.LeakEvery) == 0 {
						leaked.Add(1) // never returned: the slot is gone for the rest of the run
					} else if ok {
						idle <- conn
					} else {
						// A broken connection frees its slot so the pool can open a new one.
						_ = conn.Close()
						mu.Lock()
						created--
						mu.Unlock()
					}
				}
				if !ok {
					failures.Add(1)
				}
				sMu.Lock()
				samples = append(samples, time.Since(t0))
				sMu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	_ = ln.Close()
	mu.Lock()
	for _, c := range all {
		_ = c.Close()
	}
	mu.Unlock()
	srv.Wait()
	f := int(failures.Load())
	return Result{
		Failures: f,
		Attempts: total,
		P99Ms:    int(percentile(samples, 99) / time.Millisecond),
		Detail: fmt.Sprintf("pool=%d clients=%d requests=%d service=%dms acquire_timeout=%dms leak_every=%d leaked=%d failures=%d/%d",
			p.PoolSize, p.Clients, p.RequestsPerClient, p.ServiceMs, p.AcquireTimeoutMs, p.LeakEvery, leaked.Load(), f, total),
		Passed: f == 0,
	}
}
