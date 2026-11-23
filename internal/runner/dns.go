// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package runner

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
)

// DNS wire format, only what a stub resolver and a stub client need (RFC 1035 section 4): a 12 byte
// header, one question, and for an answer one A record that points back at the question name.
const (
	dnsRcodeOK       = 0
	dnsRcodeNXDomain = 3
	dnsMaxPacket     = 512
)

// RunDNS starts a stub resolver on 127.0.0.1 (UDP) and resolves bundle.Lookups names through a client with
// a per-attempt timeout, retries and an optional cache.
//
// Selected, not random: the stub numbers the queries it receives and answers late from query number
// degraded_after onward; the first nxdomain_percent of the distinct names answer NXDOMAIN. Timing still
// comes from the clock, so the lookup that sits near a timeout boundary is the flaky one.
// With cache set, a name that answered once is served locally afterwards (negative answers too), so a
// cached name never reaches the degraded resolver. A cache only helps for names resolved before the
// resolver degraded.
func RunDNS(d bundle.DNSCondition) Result {
	if d.Lookups < 1 {
		d.Lookups = 1
	}
	if d.Names < 1 || d.Names > d.Lookups {
		d.Names = d.Lookups
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return Result{Attempts: d.Lookups, Failures: d.Lookups, Detail: err.Error()}
	}
	var (
		queries atomic.Int64
		served  sync.WaitGroup
		mu      sync.Mutex
		timers  []*time.Timer
	)
	served.Add(1)
	go func() {
		defer served.Done()
		buf := make([]byte, dnsMaxPacket)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			q := queries.Add(1) - 1
			resp, ok := dnsAnswer(buf[:n], d)
			if !ok {
				continue // malformed: a real stub ignores it too
			}
			delay := time.Duration(0)
			if q >= int64(d.DegradedAfter) {
				delay = time.Duration(d.LatencyMs) * time.Millisecond
			}
			t := time.AfterFunc(delay, func() { _, _ = pc.WriteTo(resp, from) })
			mu.Lock()
			timers = append(timers, t)
			mu.Unlock()
		}
	}()
	addr := pc.LocalAddr().String()

	cache := map[int]bool{} // name index -> answered (true) or NXDOMAIN (false)
	samples := make([]time.Duration, 0, d.Lookups)
	failures, hits := 0, 0
	timeout := time.Duration(d.TimeoutMs) * time.Millisecond
	for i := 0; i < d.Lookups; i++ {
		name := i % d.Names
		start := time.Now()
		var ok bool
		if v, cached := cache[name]; cached && d.Cache {
			ok = v
			hits++
		} else {
			rcode, err := dnsLookup(addr, name, timeout, d.Retries, uint16(i))
			ok = err == nil && rcode == dnsRcodeOK
			if err == nil && d.Cache {
				cache[name] = ok
			}
		}
		samples = append(samples, time.Since(start))
		if !ok {
			failures++
		}
	}
	_ = pc.Close()
	served.Wait()
	// A late answer nobody is waiting for must not hold the run open: stop every timer still pending.
	mu.Lock()
	for _, t := range timers {
		t.Stop()
	}
	mu.Unlock()
	return Result{
		Failures: failures,
		Attempts: d.Lookups,
		P99Ms:    int(percentile(samples, 99) / time.Millisecond),
		Detail: fmt.Sprintf("latency=%dms degraded_after=%d nxdomain=%d%% timeout=%dms retries=%d cache=%t names=%d queries=%d cache_hits=%d failures=%d/%d",
			d.LatencyMs, d.DegradedAfter, d.NXDomainPercent, d.TimeoutMs, d.Retries, d.Cache, d.Names, queries.Load(), hits, failures, d.Lookups),
		Passed: failures == 0,
	}
}

var errDNSTimeout = errors.New("resolver timed out")

// dnsLookup sends one query per attempt from a fresh socket (a late answer to an earlier attempt then
// cannot be mistaken for the current one) and returns the response code.
func dnsLookup(addr string, name int, timeout time.Duration, retries int, id uint16) (int, error) {
	q := dnsQuery(id, fmt.Sprintf("host-%d.svc.local", name))
	for attempt := 0; attempt <= retries; attempt++ {
		c, err := net.Dial("udp", addr)
		if err != nil {
			return 0, err
		}
		_ = c.SetDeadline(time.Now().Add(timeout))
		if _, err := c.Write(q); err != nil {
			_ = c.Close()
			return 0, err
		}
		buf := make([]byte, dnsMaxPacket)
		n, err := c.Read(buf)
		_ = c.Close()
		if err != nil {
			continue // timeout: retry, if any are left
		}
		if n < 12 || binary.BigEndian.Uint16(buf[0:2]) != id || buf[2]&0x80 == 0 {
			continue
		}
		return int(buf[3] & 0x0f), nil
	}
	return 0, errDNSTimeout
}

func dnsQuery(id uint16, name string) []byte {
	b := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(b[0:2], id)
	b[2] = 0x01 // RD
	binary.BigEndian.PutUint16(b[4:6], 1)
	b = appendName(b, name)
	return append(b, 0, 1, 0, 1) // QTYPE A, QCLASS IN
}

func appendName(b []byte, name string) []byte {
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			b = append(b, byte(i-start))
			b = append(b, name[start:i]...)
			start = i + 1
		}
	}
	return append(b, 0)
}

// dnsAnswer parses a query and builds the response the stub would send. It rejects anything that is not
// exactly one plain question, so a hostile packet cannot make it read out of range.
func dnsAnswer(q []byte, d bundle.DNSCondition) ([]byte, bool) {
	if len(q) < 17 || binary.BigEndian.Uint16(q[4:6]) != 1 {
		return nil, false
	}
	i := 12
	var label []byte
	for {
		if i >= len(q) {
			return nil, false
		}
		l := int(q[i])
		if l == 0 {
			i++
			break
		}
		if l&0xc0 != 0 || i+1+l > len(q) {
			return nil, false
		}
		label = append(label, q[i+1:i+1+l]...)
		label = append(label, '.')
		i += 1 + l
	}
	if i+4 > len(q) {
		return nil, false
	}
	end := i + 4
	var idx int
	if _, err := fmt.Sscanf(string(label), "host-%d.", &idx); err != nil {
		return nil, false
	}
	resp := make([]byte, 0, end+16)
	resp = append(resp, q[:end]...)
	resp[2] = 0x81 // QR, RD
	resp[3] = 0x80 // RA
	names := d.Names
	if names < 1 {
		names = 1
	}
	if idx*100/names < d.NXDomainPercent {
		resp[3] |= dnsRcodeNXDomain
		binary.BigEndian.PutUint16(resp[6:8], 0)
		return resp, true
	}
	binary.BigEndian.PutUint16(resp[6:8], 1)
	resp = append(resp, 0xc0, 12, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4, 127, 0, 0, 1)
	return resp, true
}
