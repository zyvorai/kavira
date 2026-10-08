package api

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func leaf(t *testing.T, certFile, keyFile string) *x509.Certificate {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSelfSignedCoversHostsAndIsReused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	cert, key, err := EnsureSelfSigned(dir, []string{"203.0.113.7", "kavira.example"})
	if err != nil {
		t.Fatal(err)
	}
	c := leaf(t, cert, key)
	for _, h := range []string{"203.0.113.7", "kavira.example", "localhost", "127.0.0.1", "::1"} {
		if err := c.VerifyHostname(h); err != nil {
			t.Errorf("certificate does not cover %s: %v", h, err)
		}
	}
	if st, _ := os.Stat(key); st.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v, want 0600", st.Mode().Perm())
	}
	before, _ := os.ReadFile(cert)
	if _, _, err := EnsureSelfSigned(dir, []string{"203.0.113.7"}); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(cert); string(after) != string(before) {
		t.Error("certificate regenerated although it still covers every host")
	}
	// A new host forces a new certificate, so a changed IP does not leave a stale cert behind.
	if _, _, err := EnsureSelfSigned(dir, []string{"198.51.100.9"}); err != nil {
		t.Fatal(err)
	}
	if err := leaf(t, cert, key).VerifyHostname("198.51.100.9"); err != nil {
		t.Errorf("new host not covered after regeneration: %v", err)
	}
}

func TestOptionsValidation(t *testing.T) {
	if _, err := (&Options{CertFile: "c.pem"}).resolveTLS(); err == nil {
		t.Error("cert without key accepted")
	}
	if _, err := (&Options{CertFile: "c", KeyFile: "k", SelfSignedDir: t.TempDir()}).resolveTLS(); err == nil {
		t.Error("cert/key together with self-signed accepted")
	}
	on, err := (&Options{}).resolveTLS()
	if err != nil || on {
		t.Errorf("plain options: tls=%v err=%v", on, err)
	}
	o := Options{SelfSignedDir: filepath.Join(t.TempDir(), "x")}
	if on, err := o.resolveTLS(); err != nil || !on || o.CertFile == "" {
		t.Errorf("self-signed: tls=%v err=%v cert=%q", on, err, o.CertFile)
	}
}

func TestServesOverTLSWithSecureCookie(t *testing.T) {
	cert, key, err := EnsureSelfSigned(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	s.secureCookie = true // what Serve does when it terminates TLS
	srv := httptest.NewUnstartedServer(s.Handler())
	pair, _ := tls.LoadX509KeyPair(cert, key)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	res, err := srv.Client().Post(srv.URL+"/api/v1/session", "application/json", strings.NewReader(`{"username":"admin","password":"Admin@321"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login over tls: %d", res.StatusCode)
	}
	cs := res.Cookies()
	if len(cs) == 0 || !cs[0].Secure || !cs[0].HttpOnly {
		t.Fatalf("session cookie not Secure+HttpOnly: %+v", cs)
	}
}
