// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package runner

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
)

// CertIssue is one certificate in force: issued at hour From of the virtual clock, valid for the
// lifetime. It is exported so tests and docs can show the schedule the rotation policy produced.
type CertIssue struct {
	From int
}

// CertSchedule runs the rotation policy on the virtual clock up to the last probe and returns every
// issue time. The first certificate is issued at hour 0. Every check_interval_hours a job reissues the
// certificate when it has expired or no more than rotation_window_hours remain. A certificate that
// expires between two checks is the failure this class reconstructs. Pure arithmetic: nothing here
// depends on the wall clock.
func CertSchedule(c bundle.CertCondition) []CertIssue {
	issues := []CertIssue{{From: 0}}
	if c.RotationWindowHours <= 0 || c.CheckIntervalHours <= 0 {
		return issues
	}
	last := 0
	for _, p := range c.ProbeHours {
		if p > last {
			last = p
		}
	}
	cur := 0
	for at := c.CheckIntervalHours; at <= last; at += c.CheckIntervalHours {
		if cur+c.LifetimeHours-at <= c.RotationWindowHours {
			cur = at
			issues = append(issues, CertIssue{From: at})
		}
	}
	return issues
}

// certInForce is the index of the latest issue at or before hour t.
func certInForce(issues []CertIssue, t int) int {
	i := sort.Search(len(issues), func(i int) bool { return issues[i].From > t }) - 1
	if i < 0 {
		i = 0
	}
	return i
}

// RunCert generates a throwaway CA and one leaf per certificate in force at some probe (all in memory,
// ECDSA P-256, never written to disk), serves them from a TLS listener on 127.0.0.1, and makes one real
// handshake per probe. The client verifies against a clock set to the probe's virtual time, so the
// standard library decides whether the certificate was expired then. The wall clock plays no part in
// validity; only handshake latency (p99) is measured.
//
// Not modelled: hostname or SAN mismatch, revocation (OCSP/CRL), intermediate chains, clock skew
// between peers, and client-side certificate caching.
func RunCert(c bundle.CertCondition) Result {
	probes := append([]int(nil), c.ProbeHours...)
	issues := CertSchedule(c)
	base := time.Now().Truncate(time.Hour)

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Result{Attempts: len(probes), Failures: len(probes), Detail: err.Error()}
	}
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kavira throwaway CA"},
		NotBefore: base.Add(-24 * time.Hour), NotAfter: base.Add(time.Duration(2*bundle.MaxCertHours) * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return Result{Attempts: len(probes), Failures: len(probes), Detail: err.Error()}
	}
	caCert, _ := x509.ParseCertificate(caDER)
	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	// Only certificates some probe actually meets are generated (at most len(probes)).
	leaves := map[int]*tls.Certificate{}
	for _, p := range probes {
		k := certInForce(issues, p)
		if leaves[k] != nil {
			continue
		}
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return Result{Attempts: len(probes), Failures: len(probes), Detail: err.Error()}
		}
		from := base.Add(time.Duration(issues[k].From) * time.Hour)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(int64(k) + 2), Subject: pkix.Name{CommonName: leafName(k)},
			DNSNames:  []string{leafName(k)},
			NotBefore: from, NotAfter: from.Add(time.Duration(c.LifetimeHours) * time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			return Result{Attempts: len(probes), Failures: len(probes), Detail: err.Error()}
		}
		leaves[k] = &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
			var k int
			if _, err := fmt.Sscanf(h.ServerName, "svc-%d.kavira.local", &k); err != nil || leaves[k] == nil {
				return nil, fmt.Errorf("unknown server name")
			}
			return leaves[k], nil
		},
	})
	if err != nil {
		return Result{Attempts: len(probes), Failures: len(probes), Detail: err.Error()}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				_ = conn.(*tls.Conn).Handshake()
			}()
		}
	}()

	samples := make([]time.Duration, 0, len(probes))
	failures, expired := 0, 0
	for _, p := range probes {
		k := certInForce(issues, p)
		at := base.Add(time.Duration(p) * time.Hour)
		start := time.Now()
		d := net.Dialer{Timeout: 5 * time.Second}
		conn, err := tls.DialWithDialer(&d, "tcp", ln.Addr().String(), &tls.Config{
			RootCAs: pool, ServerName: leafName(k), MinVersion: tls.VersionTLS12,
			Time: func() time.Time { return at },
		})
		samples = append(samples, time.Since(start))
		if err != nil {
			failures++
			if _, ok := err.(*tls.CertificateVerificationError); ok {
				expired++
			}
			continue
		}
		_ = conn.Close()
	}
	_ = ln.Close()
	wg.Wait()
	return Result{
		Failures: failures,
		Attempts: len(probes),
		P99Ms:    int(percentile(samples, 99) / time.Millisecond),
		Detail: fmt.Sprintf("lifetime=%dh window=%dh check=%dh issues=%d probes=%d verify_failures=%d failures=%d/%d",
			c.LifetimeHours, c.RotationWindowHours, c.CheckIntervalHours, len(issues), len(probes), expired, failures, len(probes)),
		Passed: failures == 0,
	}
}

func leafName(k int) string { return fmt.Sprintf("svc-%d.kavira.local", k) }
