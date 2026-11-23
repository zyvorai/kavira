// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package bundle

import (
	"strings"
	"testing"
)

// Hostile and malformed bundles for the loopback classes. Every one must be refused before anything runs.
func TestParseHostileClassBundles(t *testing.T) {
	dns := func(f string) string {
		return `{"id":"d","class":"dns_failure","dns":{` + f + `}}`
	}
	okDNS := `"latency_ms":10,"timeout_ms":50,"retries":1,"lookups":4,"names":2`
	cert := func(f string) string { return `{"id":"c","class":"certificate_expiry","cert":{` + f + `}}` }
	pool := func(f string) string { return `{"id":"p","class":"connection_pool_exhaustion","pool":{` + f + `}}` }
	okPool := `"pool_size":2,"acquire_timeout_ms":50,"clients":4,"requests_per_client":2,"service_ms":10`
	retry := func(f string) string { return `{"id":"r","class":"retry_storm","retry":{` + f + `}}` }
	okRetry := `"clients":10,"capacity":2,"service_ms":10,"backoff_ms":20,"max_retries":3`

	cases := []struct {
		name, raw string
		wantErr   string // empty means valid
	}{
		{"dns ok", dns(okDNS), ""},
		{"dns missing section", `{"id":"d","class":"dns_failure"}`, "requires a dns condition"},
		{"dns zero lookups", dns(`"timeout_ms":5,"lookups":0,"names":1`), "lookups"},
		{"dns too many lookups", dns(`"timeout_ms":5,"lookups":100000,"names":1`), "lookups"},
		{"dns names above lookups", dns(`"timeout_ms":5,"lookups":2,"names":3`), "names"},
		{"dns no timeout", dns(`"lookups":2,"names":1`), "timeout_ms"},
		{"dns negative latency", dns(`"latency_ms":-1,"timeout_ms":5,"lookups":2,"names":1`), "latency_ms"},
		{"dns retries bomb", dns(`"timeout_ms":5,"lookups":2,"names":1,"retries":1000000`), "retries"},
		{"dns nxdomain above 100", dns(`"timeout_ms":5,"lookups":2,"names":1,"nxdomain_percent":101`), "nxdomain"},
		{"dns time bomb", dns(`"timeout_ms":60000,"lookups":200,"names":1,"retries":5`), "<="},
		{"cert ok", cert(`"lifetime_hours":100,"rotation_window_hours":10,"check_interval_hours":5,"probe_hours":[1,2]`), ""},
		{"cert no rotation ok", cert(`"lifetime_hours":100,"probe_hours":[1]`), ""},
		{"cert missing section", `{"id":"c","class":"certificate_expiry"}`, "requires a cert condition"},
		{"cert zero lifetime", cert(`"lifetime_hours":0,"probe_hours":[1]`), "lifetime"},
		{"cert huge lifetime", cert(`"lifetime_hours":99999999,"probe_hours":[1]`), "lifetime"},
		{"cert window above lifetime", cert(`"lifetime_hours":10,"rotation_window_hours":11,"check_interval_hours":1,"probe_hours":[1]`), "rotation_window"},
		{"cert rotation without interval", cert(`"lifetime_hours":10,"rotation_window_hours":2,"probe_hours":[1]`), "check_interval"},
		{"cert no probes", cert(`"lifetime_hours":10,"probe_hours":[]`), "probe_hours"},
		{"cert negative probe", cert(`"lifetime_hours":10,"probe_hours":[-1]`), "probe hour"},
		{"cert too many probes", cert(`"lifetime_hours":10,"probe_hours":[` + strings.Repeat("1,", MaxCertProbes) + `1]`), "probe_hours"},
		{"pool ok", pool(okPool), ""},
		{"pool zero size", pool(`"pool_size":0,"acquire_timeout_ms":5,"clients":1,"requests_per_client":1`), "pool_size"},
		{"pool huge size", pool(`"pool_size":100000,"acquire_timeout_ms":5,"clients":1,"requests_per_client":1`), "pool_size"},
		{"pool huge clients", pool(`"pool_size":1,"acquire_timeout_ms":5,"clients":100000,"requests_per_client":1`), "clients"},
		{"pool huge requests", pool(`"pool_size":1,"acquire_timeout_ms":5,"clients":1,"requests_per_client":100000`), "requests_per_client"},
		{"pool negative service", pool(`"pool_size":1,"acquire_timeout_ms":5,"clients":1,"requests_per_client":1,"service_ms":-5`), "service_ms"},
		{"pool time bomb", pool(`"pool_size":1,"acquire_timeout_ms":60000,"clients":1,"requests_per_client":50,"service_ms":1000`), "<="},
		{"pool bad leak", pool(`"pool_size":1,"acquire_timeout_ms":5,"clients":1,"requests_per_client":1,"leak_every":-2`), "leak_every"},
		{"retry ok", retry(okRetry), ""},
		{"retry huge clients", retry(`"clients":100000,"capacity":1,"service_ms":1,"backoff_ms":1`), "clients"},
		{"retry zero capacity", retry(`"clients":2,"capacity":0,"service_ms":1,"backoff_ms":1`), "capacity"},
		{"retry zero backoff", retry(`"clients":2,"capacity":1,"service_ms":1,"backoff_ms":0`), "backoff_ms"},
		{"retry jitter above 100", retry(`"clients":2,"capacity":1,"service_ms":1,"backoff_ms":1,"jitter_percent":500`), "jitter"},
		{"retry retries bomb", retry(`"clients":2,"capacity":1,"service_ms":1,"backoff_ms":1,"max_retries":1000000`), "max_retries"},
		{"retry exponential bomb", retry(`"clients":2,"capacity":1,"service_ms":500,"backoff_ms":5000,"max_retries":10`), "worst-case"},
		{"network response bomb", `{"id":"n","class":"network_timeout","network":{"attempts":1,"timeout_ms":10,"response_bytes":2147483647}}`, "response_bytes"},
		{"network response one byte", `{"id":"n","class":"network_timeout","network":{"attempts":1,"timeout_ms":10,"response_bytes":1}}`, "response_bytes"},
		{"network negative bandwidth", `{"id":"n","class":"network_timeout","network":{"attempts":1,"timeout_ms":10,"bandwidth_bps":-1}}`, "bandwidth"},
		{"network slow trickle bomb", `{"id":"n","class":"network_timeout","network":{"attempts":200,"timeout_ms":10,"bandwidth_bps":1,"response_bytes":1048576}}`, "<="},
		{"network slow close bomb", `{"id":"n","class":"network_timeout","network":{"attempts":200,"timeout_ms":10,"slow_close_ms":60000}}`, "<="},
		{"network jitter bomb", `{"id":"n","class":"network_timeout","network":{"attempts":1,"timeout_ms":10,"jitter_ms":99999999}}`, "jitter"},
		{"network arms ok", `{"id":"n","class":"network_timeout","network":{"attempts":3,"timeout_ms":100,"jitter_ms":5,"bandwidth_bps":10000,"response_bytes":100,"reset_after_bytes":50,"slow_close_ms":10}}`, ""},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.raw))
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: want error containing %q, got %v", c.name, c.wantErr, err)
		}
	}
}

func TestBackoffDelayIsBoundedAndCapped(t *testing.T) {
	r := RetryCondition{BackoffMs: 100, MaxRetries: 10}
	for _, c := range []struct {
		k, cap int
		want   int64
	}{{0, 0, 100}, {1, 0, 200}, {3, 0, 800}, {3, 300, 300}, {10, 0, 60_000}, {200, 0, 60_000}} {
		r.BackoffCapMs = c.cap
		if got := r.BackoffDelayMs(c.k); got != c.want {
			t.Errorf("k=%d cap=%d: got %d want %d", c.k, c.cap, got, c.want)
		}
	}
}

func TestClassesListMatchesValidate(t *testing.T) {
	for _, c := range Classes {
		_, err := Parse([]byte(`{"id":"x","class":"` + c + `"}`))
		if err != nil && strings.Contains(err.Error(), "unsupported class") {
			t.Errorf("%s is listed but not accepted", c)
		}
	}
}
