package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionAndIncidents(t *testing.T) {
	s := New()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/v1/incidents")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}

	res, err = http.Post(srv.URL+"/api/v1/session", "application/json", strings.NewReader(`{"username":"admin","password":"Admin@321"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login %d", res.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/incidents", nil)
	for _, c := range res.Cookies() {
		req.AddCookie(c)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Incidents []struct {
			ID      string `json:"id"`
			Verdict string `json:"verdict"`
		} `json:"incidents"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Incidents) != 3 {
		t.Fatalf("seeded %d", len(body.Incidents))
	}
	for _, inc := range body.Incidents {
		if inc.Verdict != "repair_held" {
			t.Fatalf("%s verdict %s", inc.ID, inc.Verdict)
		}
	}
}

func login(t *testing.T, base string) []*http.Cookie {
	t.Helper()
	res, err := http.Post(base+"/api/v1/session", "application/json", strings.NewReader(`{"username":"admin","password":"Admin@321"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login %d", res.StatusCode)
	}
	return res.Cookies()
}

func do(t *testing.T, method, url, ctype, body string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestLoginRejectsBadInput(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	cases := []struct {
		name, method, ctype, body string
		want                      int
	}{
		{"wrong password", "POST", "application/json", `{"username":"admin","password":"nope"}`, 401},
		{"wrong user", "POST", "application/json", `{"username":"root","password":"Admin@321"}`, 401},
		{"bad json", "POST", "application/json", `{`, 400},
		{"not json", "POST", "text/plain", `{}`, 415},
		{"put", "PUT", "", ``, 405},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := do(t, c.method, srv.URL+"/api/v1/session", c.ctype, c.body, nil)
			if res.StatusCode != c.want {
				t.Fatalf("status %d, want %d", res.StatusCode, c.want)
			}
			if len(res.Cookies()) != 0 {
				t.Fatal("cookie set on failure")
			}
		})
	}
}

func TestHealthAndHeaders(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	res := do(t, "GET", srv.URL+"/api/v1/health", "", "", nil)
	var h struct {
		OK        bool `json:"ok"`
		DemoGate  bool `json:"demo_gate"`
		ExactPlay bool `json:"exact_replay"`
	}
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		t.Fatal(err)
	}
	if !h.OK || !h.DemoGate || h.ExactPlay {
		t.Fatalf("health %+v", h)
	}
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	} {
		if got := res.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("missing CSP")
	}
}

func TestStaticAssetsServed(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	for _, p := range []string{"/", "/styles.css", "/theme.js", "/js/main.js", "/js/api.js", "/js/pages.js", "/js/ui.js", "/js/router.js"} {
		if res := do(t, "GET", srv.URL+p, "", "", nil); res.StatusCode != 200 {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
}

func TestCookieTamperAndLogout(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	cookies := login(t, srv.URL)
	bad := []*http.Cookie{{Name: cookies[0].Name, Value: cookies[0].Value + "0"}}
	if res := do(t, "GET", srv.URL+"/api/v1/audit", "", "", bad); res.StatusCode != 401 {
		t.Fatalf("tampered cookie: %d", res.StatusCode)
	}
	if res := do(t, "GET", srv.URL+"/api/v1/logout", "", "", cookies); res.StatusCode != 405 {
		t.Fatalf("logout GET: %d", res.StatusCode)
	}
	res := do(t, "POST", srv.URL+"/api/v1/logout", "application/json", "{}", cookies)
	if res.StatusCode != 200 || res.Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout %d %+v", res.StatusCode, res.Cookies())
	}
}

func TestCompileAndIncidentLookup(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	ck := login(t, srv.URL)
	post := func(ctype, body string) *http.Response {
		return do(t, "POST", srv.URL+"/api/v1/compile", ctype, body, ck)
	}
	if res := post("application/json", `{"fixture":"oom"}`); res.StatusCode != 200 {
		t.Errorf("fixture: %d", res.StatusCode)
	}
	if res := post("application/json", `{"fixture":"../nope"}`); res.StatusCode != 400 {
		t.Errorf("unknown fixture: %d", res.StatusCode)
	}
	if res := post("application/json", `{`); res.StatusCode != 400 {
		t.Errorf("bad json: %d", res.StatusCode)
	}
	if res := post("text/plain", `{}`); res.StatusCode != 415 {
		t.Errorf("content type: %d", res.StatusCode)
	}
	if res := post("application/json", `{"pad":"`+strings.Repeat("a", 1<<20)+`"}`); res.StatusCode != 413 {
		t.Errorf("oversize: %d", res.StatusCode)
	}
	if res := do(t, "GET", srv.URL+"/api/v1/compile", "", "", ck); res.StatusCode != 405 {
		t.Errorf("get compile: %d", res.StatusCode)
	}
	if res := do(t, "GET", srv.URL+"/api/v1/incidents/inc-missing", "", "", ck); res.StatusCode != 404 {
		t.Errorf("missing incident: %d", res.StatusCode)
	}
	if res := do(t, "GET", srv.URL+"/api/v1/incidents/inc-oom-001", "", "", ck); res.StatusCode != 200 {
		t.Errorf("incident by id: %d", res.StatusCode)
	}
	if res := do(t, "GET", srv.URL+"/api/v1/nope", "", "", ck); res.StatusCode != 404 {
		t.Errorf("unknown api: %d", res.StatusCode)
	}
}

func TestIncidentsSortedAndAuditChain(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	ck := login(t, srv.URL)
	var inc struct {
		Incidents []struct {
			ID string `json:"id"`
		} `json:"incidents"`
	}
	json.NewDecoder(do(t, "GET", srv.URL+"/api/v1/incidents", "", "", ck).Body).Decode(&inc)
	for i := 1; i < len(inc.Incidents); i++ {
		if inc.Incidents[i-1].ID > inc.Incidents[i].ID {
			t.Fatalf("unsorted: %v", inc.Incidents)
		}
	}
	var a struct {
		Audit []Audit `json:"audit"`
	}
	json.NewDecoder(do(t, "GET", srv.URL+"/api/v1/audit", "", "", ck).Body).Decode(&a)
	if len(a.Audit) != 3 {
		t.Fatalf("audit %d", len(a.Audit))
	}
	for i, e := range a.Audit {
		prev := ""
		if i > 0 {
			prev = a.Audit[i-1].Hash
		}
		sum := sha256.Sum256([]byte(prev + "|" + e.Action + "|" + e.ID + "|" + e.Verdict))
		if e.Prev != prev || e.Hash != hex.EncodeToString(sum[:]) {
			t.Fatalf("chain broken at %d", i)
		}
	}
}

func TestFixturesMatchExamples(t *testing.T) {
	entries, err := os.ReadDir("fixtures")
	if err != nil || len(entries) == 0 {
		t.Fatal("no fixtures", err)
	}
	for _, e := range entries {
		want, err := os.ReadFile(filepath.Join("..", "..", "examples", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join("fixtures", e.Name()))
		if !bytes.Equal(got, want) {
			t.Errorf("fixtures/%s drifted from examples/%s", e.Name(), e.Name())
		}
	}
}

func TestLoopback(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1:8080": true, "localhost:1": true, "[::1]:80": true, ":8080": false, "0.0.0.0:80": false} {
		if got := loopback(addr); got != want {
			t.Errorf("loopback(%q) = %v", addr, got)
		}
	}
}

func TestFixturesListAndChainFlag(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	ck := login(t, srv.URL)
	var f struct {
		Fixtures []FixtureInfo `json:"fixtures"`
	}
	if err := json.NewDecoder(do(t, "GET", srv.URL+"/api/v1/fixtures", "", "", ck).Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if len(f.Fixtures) != 3 || f.Fixtures[0].Name == "" || f.Fixtures[0].Class == "" || f.Fixtures[0].Symptom == "" {
		t.Fatalf("fixtures %+v", f.Fixtures)
	}
	var a struct {
		ChainOK bool `json:"chain_ok"`
	}
	json.NewDecoder(do(t, "GET", srv.URL+"/api/v1/audit", "", "", ck).Body).Decode(&a)
	if !a.ChainOK {
		t.Fatal("chain_ok false")
	}
	if chainOK([]Audit{{Action: "x", ID: "y", Verdict: "z", Hash: "bad"}}) {
		t.Fatal("tampered chain accepted")
	}
}

func TestSessionProbe(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	probe := func(ck []*http.Cookie) bool {
		res := do(t, "GET", srv.URL+"/api/v1/session", "", "", ck)
		if res.StatusCode != 200 {
			t.Fatalf("probe status %d", res.StatusCode)
		}
		var b struct {
			Authenticated bool `json:"authenticated"`
		}
		json.NewDecoder(res.Body).Decode(&b)
		return b.Authenticated
	}
	if probe(nil) {
		t.Fatal("anonymous probe reported authenticated")
	}
	if !probe(login(t, srv.URL)) {
		t.Fatal("signed-in probe reported anonymous")
	}
}

func TestBrandAssetsAndMeta(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	for _, p := range []string{"/favicon.svg", "/zyvor-mark.svg", "/apple-touch-icon.png"} {
		if res := do(t, "GET", srv.URL+p, "", "", nil); res.StatusCode != 200 {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
	res := do(t, "GET", srv.URL+"/", "", "", nil)
	var b bytes.Buffer
	b.ReadFrom(res.Body)
	for _, want := range []string{`property="og:image"`, `name="twitter:card"`, `rel="icon"`, `name="description"`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("index.html missing %s", want)
		}
	}
}
