package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	selfSignedValidity = 825 * 24 * time.Hour // the longest lifetime Safari/macOS accepts for a user-trusted cert
	renewBefore        = 30 * 24 * time.Hour
)

// EnsureSelfSigned returns cert and key paths inside dir, creating a self-signed ECDSA P-256
// certificate when none exists, when it expires within 30 days, or when it does not cover
// every requested host. localhost, 127.0.0.1 and ::1 are always included. The pair is kept
// on disk so a browser's "accept the risk" decision survives restarts.
func EnsureSelfSigned(dir string, hosts []string) (certFile, keyFile string, err error) {
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	want := hostSet(hosts)
	if covers(certFile, keyFile, want) {
		return certFile, keyFile, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", "", err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "kavira", Organization: []string{"Zyvor AI Labs"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range want {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(keyFile, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return "", "", err
	}
	if err := writePEM(certFile, "CERTIFICATE", der, 0o644); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

func hostSet(hosts []string) []string {
	seen := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h != "" {
			seen[h] = true
		}
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// covers reports whether an existing, loadable pair is still valid for every wanted host.
func covers(certFile, keyFile string, want []string) bool {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || time.Until(leaf.NotAfter) < renewBefore {
		return false
	}
	for _, h := range want {
		if leaf.VerifyHostname(h) != nil {
			return false
		}
	}
	return true
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if err := pem.Encode(f, &pem.Block{Type: typ, Bytes: der}); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Options configures Serve. TLS is on when CertFile and KeyFile are set, or when SelfSignedDir is.
type Options struct {
	Addr          string
	CertFile      string
	KeyFile       string
	SelfSignedDir string   // generate and reuse a self-signed pair here
	Hosts         []string // extra names/IPs the self-signed certificate must cover
}

func (o *Options) resolveTLS() (bool, error) {
	if o.SelfSignedDir != "" {
		if o.CertFile != "" || o.KeyFile != "" {
			return false, errors.New("use either -tls-cert/-tls-key or -tls-self-signed, not both")
		}
		c, k, err := EnsureSelfSigned(o.SelfSignedDir, o.Hosts)
		if err != nil {
			return false, fmt.Errorf("self-signed certificate: %w", err)
		}
		o.CertFile, o.KeyFile = c, k
	}
	if (o.CertFile == "") != (o.KeyFile == "") {
		return false, errors.New("-tls-cert and -tls-key must be set together")
	}
	return o.CertFile != "", nil
}
