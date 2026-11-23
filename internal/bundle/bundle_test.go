// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oom(mut string) string {
	return fmt.Sprintf(`{"id":"x","class":"container_oom","evidence":{"memory_limit_bytes":%s},"workload":{"requests":%s,"bytes_per_request":%s}}`,
		pick(mut, "limit", "1048576"), pick(mut, "requests", "4"), pick(mut, "bytes", "1024"))
}

// pick returns the override from "name=value;name=value" when present.
func pick(mut, name, def string) string {
	for _, kv := range strings.Split(mut, ";") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return def
}

func TestParseHostileBundles(t *testing.T) {
	long := strings.Repeat("a", MaxTextLen+1)
	cases := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"valid oom", oom(""), true},
		{"empty", ``, false},
		{"not json", `<html>`, false},
		{"array", `[]`, false},
		{"null", `null`, false},
		{"trailing garbage", oom("") + `{}`, false},
		{"huge requests", oom("requests=999999999"), false},
		{"huge bytes", oom("bytes=9223372036854775807"), false},
		{"overflowing product", oom("requests=1024;bytes=9007199254740992"), false},
		{"workload over cap", oom("requests=1024;bytes=1048576"), false},
		{"huge limit", oom("limit=9223372036854775807"), false},
		{"negative requests", oom("requests=-1"), false},
		{"id too long", `{"id":"` + strings.Repeat("i", MaxIDLen+1) + `","class":"container_oom"}`, false},
		{"symptom too long", strings.Replace(oom(""), `"id":"x"`, `"id":"x","symptom":"`+long+`"`, 1), false},
		{"path traversal id is data, not a path", strings.Replace(oom(""), `"id":"x"`, `"id":"../../etc/passwd"`, 1), true},
		{"network attempts huge", `{"id":"n","class":"network_timeout","network":{"attempts":100000,"timeout_ms":10}}`, false},
		{"network timeout huge", `{"id":"n","class":"network_timeout","network":{"attempts":1,"timeout_ms":99999999}}`, false},
		{"network delay*attempts huge", `{"id":"n","class":"network_timeout","network":{"attempts":200,"timeout_ms":10,"delay_ms":50000}}`, false},
		{"network loss out of range", `{"id":"n","class":"network_timeout","network":{"attempts":2,"timeout_ms":10,"loss_percent":101}}`, false},
		{"network negative delay", `{"id":"n","class":"network_timeout","network":{"attempts":2,"timeout_ms":10,"delay_ms":-5}}`, false},
		{"network ok", `{"id":"n","class":"network_timeout","network":{"attempts":4,"timeout_ms":15,"delay_ms":40}}`, true},
		{"too many timeline events", `{"id":"x","class":"container_oom","timeline":[` + strings.TrimSuffix(strings.Repeat(`{"at":"a","event":"e"},`, MaxTimeline+1), ",") + `],"evidence":{"memory_limit_bytes":9},"workload":{"requests":1,"bytes_per_request":1}}`, false},
		{"timeline event too long", `{"id":"x","class":"container_oom","timeline":[{"at":"a","event":"` + strings.Repeat("e", 513) + `"}],"evidence":{"memory_limit_bytes":9},"workload":{"requests":1,"bytes_per_request":1}}`, false},
		{"timeline ok", `{"id":"x","class":"container_oom","timeline":[{"at":"T-1","event":"e"}],"evidence":{"memory_limit_bytes":9},"workload":{"requests":1,"bytes_per_request":1}}`, true},
		{"config value too long", `{"id":"c","class":"config_regression","configs":{"previous":{"a":"1"},"current":{"a":"` + long + `"}}}`, false},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.raw))
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestParseTooManyConfigKeys(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"id":"c","class":"config_regression","configs":{"previous":{"a":"1"},"current":{`)
	for i := 0; i <= MaxConfigKeys; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"k%d":"v"`, i)
	}
	sb.WriteString("}}}")
	if _, err := Parse([]byte(sb.String())); err == nil {
		t.Fatal("expected too many keys to be rejected")
	}
}

func TestParseOversizedRaw(t *testing.T) {
	raw := []byte(`{"id":"x","pad":"` + strings.Repeat("A", MaxBundleBytes) + `"}`)
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err %v", err)
	}
}

func TestLoadDoesNotReadPastLimit(t *testing.T) {
	// 4 MiB of padding: Load must reject without needing the whole file.
	p := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(p, []byte(`{"id":"x","pad":"`+strings.Repeat("A", 4<<20)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected oversize error")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestDeeplyNestedJSONIsRejectedNotCrashed(t *testing.T) {
	raw := strings.Repeat("[", 200000) + strings.Repeat("]", 200000)
	if _, err := Parse([]byte(raw)); err == nil {
		t.Fatal("expected error")
	}
}
