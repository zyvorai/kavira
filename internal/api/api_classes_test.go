// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package api

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every new class goes through the API like the first four: compile a fixture, then the analysis routes.
func TestNewClassFixturesThroughTheAPI(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	ck := login(t, srv.URL)
	for _, c := range []struct{ fixture, id string }{
		{"dns-failure", "inc-dns-001"}, {"certificate-expiry", "inc-cert-001"},
		{"connection-pool", "inc-pool-001"}, {"retry-storm", "inc-retry-001"},
	} {
		if res := do(t, "POST", srv.URL+"/api/v1/compile", "application/json", `{"fixture":"`+c.fixture+`"}`, ck); res.StatusCode != 200 {
			t.Fatalf("%s: compile %d", c.fixture, res.StatusCode)
		}
		base := srv.URL + "/api/v1/incidents/" + c.id
		res := do(t, "GET", base, "", "", ck)
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 || !strings.Contains(string(body), `"repair_held"`) || !strings.Contains(string(body), `"exact_replay":false`) && !strings.Contains(string(body), `"exact_replay": false`) {
			t.Errorf("%s: %d %.200s", c.fixture, res.StatusCode, body)
		}
		for path, want := range map[string]string{
			"/postmortem.md":  "# Postmortem draft: " + c.id,
			"/confidence?n=2": `"reproduced"`,
			"/regression.zip": "PK",
			"/package.zip":    "PK",
		} {
			res := do(t, "GET", base+path, "", "", ck)
			b, _ := io.ReadAll(res.Body)
			if res.StatusCode != 200 || !strings.Contains(string(b), want) {
				t.Errorf("%s%s: %d", c.fixture, path, res.StatusCode)
			}
		}
	}
}

func TestCompileRejectsHostileNewClassBundles(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	ck := login(t, srv.URL)
	cases := []struct{ name, body string }{
		{"dns bomb", `{"id":"d","class":"dns_failure","dns":{"timeout_ms":60000,"lookups":200,"names":1,"retries":5}}`},
		{"dns missing section", `{"id":"d","class":"dns_failure"}`},
		{"cert huge lifetime", `{"id":"c","class":"certificate_expiry","cert":{"lifetime_hours":999999999,"probe_hours":[1]}}`},
		{"pool bomb", `{"id":"p","class":"connection_pool_exhaustion","pool":{"pool_size":1,"clients":64,"requests_per_client":50,"service_ms":1000,"acquire_timeout_ms":60000}}`},
		{"retry bomb", `{"id":"r","class":"retry_storm","retry":{"clients":200,"capacity":1,"service_ms":500,"backoff_ms":5000,"max_retries":10}}`},
		{"network response bomb", `{"id":"n","class":"network_timeout","network":{"attempts":1,"timeout_ms":10,"response_bytes":2000000000}}`},
	}
	for _, c := range cases {
		if res := do(t, "POST", srv.URL+"/api/v1/compile", "application/json", c.body, ck); res.StatusCode != 400 {
			t.Errorf("%s: %d want 400", c.name, res.StatusCode)
		}
	}
}
