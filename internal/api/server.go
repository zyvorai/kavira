package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/compiler"
)

//go:embed fixtures/*.json
var fixtureFS embed.FS

type Server struct {
	mu       sync.Mutex
	reports  map[string]compiler.Report
	bundles  map[string]bundle.Bundle
	audit    []Audit
	key      []byte
	password string
	// demoGate is true while the documented default password is in use.
	demoGate     bool
	defaultKey   bool
	secureCookie bool
}

const (
	defaultPassword = "Admin@321"
	defaultKey      = "kavira-dev-session"
	sessionTTL      = 12 * time.Hour
	cookieName      = "kavira_session"
)

type Audit struct {
	At      string `json:"at"`
	Action  string `json:"action"`
	ID      string `json:"id"`
	Verdict string `json:"verdict"`
	Prev    string `json:"prev"`
	Hash    string `json:"hash"`
}

func New() *Server {
	key := os.Getenv("KAVIRA_SESSION_KEY")
	usingDefaultKey := key == ""
	if usingDefaultKey {
		key = defaultKey
	}
	password := os.Getenv("KAVIRA_ADMIN_PASSWORD")
	if password == "" {
		password = defaultPassword
	}
	s := &Server{
		reports:      map[string]compiler.Report{},
		bundles:      map[string]bundle.Bundle{},
		key:          []byte(key),
		password:     password,
		demoGate:     password == defaultPassword,
		defaultKey:   usingDefaultKey,
		secureCookie: os.Getenv("KAVIRA_COOKIE_SECURE") == "1",
	}
	s.seed()
	return s
}

func (s *Server) seed() {
	entries, err := fs.ReadDir(fixtureFS, "fixtures")
	if err != nil {
		return
	}
	for _, e := range entries {
		raw, err := fixtureFS.ReadFile("fixtures/" + e.Name())
		if err != nil {
			continue
		}
		b, err := bundle.Parse(raw)
		if err != nil {
			continue
		}
		s.store(b, compiler.Compile(b), "seed")
	}
}

func (s *Server) store(b bundle.Bundle, rep compiler.Report, action string) {
	s.bundles[rep.ID] = b
	s.reports[rep.ID] = rep
	prev := ""
	if n := len(s.audit); n > 0 {
		prev = s.audit[n-1].Hash
	}
	body := prev + "|" + action + "|" + rep.ID + "|" + rep.Verdict
	sum := sha256.Sum256([]byte(body))
	s.audit = append(s.audit, Audit{
		At: time.Now().UTC().Format(time.RFC3339), Action: action, ID: rep.ID,
		Verdict: rep.Verdict, Prev: prev, Hash: hex.EncodeToString(sum[:]),
	})
}

// Listen serves plain HTTP until SIGINT or SIGTERM, then shuts down gracefully.
func Listen(addr string) error { return Serve(Options{Addr: addr}) }

// Serve runs the console, over HTTPS when opts carries a certificate (or asks for a self-signed one).
func Serve(opts Options) error {
	useTLS, err := opts.resolveTLS()
	if err != nil {
		return err
	}
	s := New()
	if useTLS {
		s.secureCookie = true // never send the session cookie over plain HTTP when we terminate TLS
	}
	scheme, addr := "http", opts.Addr
	if useTLS {
		scheme = "https"
	}
	if s.demoGate {
		fmt.Fprintf(os.Stderr, "kavira console on %s://%s  admin / %s (demo gate; set KAVIRA_ADMIN_PASSWORD)\n", scheme, addr, s.password)
	} else {
		fmt.Fprintf(os.Stderr, "kavira console on %s://%s\n", scheme, addr)
	}
	if strings.HasPrefix(addr, ":") {
		fmt.Fprintf(os.Stderr, "open %s://127.0.0.1%s\n", scheme, addr)
	}
	if useTLS && opts.SelfSignedDir != "" {
		fmt.Fprintf(os.Stderr, "tls: self-signed certificate in %s (browsers will ask you to accept it once)\n", opts.SelfSignedDir)
	}
	if !loopback(addr) && (s.demoGate || s.defaultKey) {
		fmt.Fprintln(os.Stderr, "warning: listening beyond loopback with the default password or session key; set KAVIRA_ADMIN_PASSWORD and KAVIRA_SESSION_KEY")
	}
	if !loopback(addr) && !useTLS {
		fmt.Fprintln(os.Stderr, "warning: serving plain HTTP beyond loopback; use -tls-cert/-tls-key, -tls-self-signed, or a TLS proxy")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if useTLS {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	errc := make(chan error, 1)
	go func() {
		if useTLS {
			errc <- srv.ListenAndServeTLS(opts.CertFile, opts.KeyFile)
		} else {
			errc <- srv.ListenAndServe()
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errc:
		return err
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
		if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// loopback reports whether addr only binds to the local machine.
func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.health)
	mux.HandleFunc("/api/v1/session", s.session)
	mux.HandleFunc("/api/v1/logout", s.logout)
	mux.HandleFunc("/api/v1/incidents", s.auth(s.incidents))
	mux.HandleFunc("/api/v1/incidents/", s.auth(s.incident))
	mux.HandleFunc("/api/v1/compile", s.auth(s.compile))
	mux.HandleFunc("/api/v1/audit", s.auth(s.auditLog))
	mux.HandleFunc("/api/v1/fixtures", s.auth(s.fixtures))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	web, _ := fs.Sub(webFS, "static")
	mux.Handle("/", staticHeaders(http.FileServer(http.FS(web))))
	return secure(mux)
}

// secure sets baseline response headers on every response.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func staticHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "product": "kavira", "exact_replay": false, "demo_gate": s.demoGate})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Lets the console learn whether it is signed in without a 401 in the browser console.
		c, err := r.Cookie(cookieName)
		ok := err == nil && s.valid(c.Value)
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": ok})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !isJSON(r) {
		writeError(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad json")
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(body.Username), []byte("admin"))
	passOK := subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.password))
	if userOK&passOK != 1 {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	exp := time.Now().Add(sessionTTL).Unix()
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.sign("admin", exp), Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.secureCookie, MaxAge: int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": "admin"})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.secureCookie,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func isJSON(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return ct == "application/json" || strings.HasPrefix(ct, "application/json;")
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || !s.valid(c.Value) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Server) sign(user string, exp int64) string {
	payload := fmt.Sprintf("%s|%d", user, exp)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return payload + "|" + hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) valid(token string) bool {
	parts := strings.Split(token, "|")
	if len(parts) != 3 {
		return false
	}
	payload := parts[0] + "|" + parts[1]
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	got, err := hex.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
}

func (s *Server) incidents(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]compiler.Report, 0, len(s.reports))
	for _, rep := range s.reports {
		list = append(list, rep)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"incidents": list})
}

func (s *Server) incident(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/incidents/")
	s.mu.Lock()
	rep, ok := s.reports[id]
	b := s.bundles[id]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "incident not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": rep, "bundle": b})
}

// FixtureInfo describes an embedded example bundle so the console can list it.
type FixtureInfo struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Class   string `json:"class"`
	Service string `json:"service"`
	Symptom string `json:"symptom"`
}

func (s *Server) fixtures(w http.ResponseWriter, r *http.Request) {
	entries, _ := fs.ReadDir(fixtureFS, "fixtures")
	list := []FixtureInfo{}
	for _, e := range entries {
		raw, err := fixtureFS.ReadFile("fixtures/" + e.Name())
		if err != nil {
			continue
		}
		b, err := bundle.Parse(raw)
		if err != nil {
			continue
		}
		list = append(list, FixtureInfo{
			Name: strings.TrimSuffix(e.Name(), ".json"), ID: b.ID, Class: b.Class, Service: b.Service, Symptom: b.Symptom,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"fixtures": list})
}

func (s *Server) compile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !isJSON(r) {
		writeError(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(raw) > 1<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "bundle exceeds 1 MiB")
		return
	}
	var probe struct {
		Fixture string `json:"fixture"`
	}
	_ = json.Unmarshal(raw, &probe)
	if probe.Fixture != "" {
		raw, err = fixtureFS.ReadFile("fixtures/" + path.Base(probe.Fixture) + ".json")
		if err != nil {
			writeError(w, http.StatusBadRequest, "unknown fixture")
			return
		}
	}
	b, err := bundle.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rep := compiler.Compile(b)
	s.mu.Lock()
	s.store(b, rep, "compile")
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"incident": rep, "bundle": b})
}

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"audit": s.audit, "chain_ok": chainOK(s.audit)})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

// chainOK recomputes the hash chain, the same way store() built it.
func chainOK(entries []Audit) bool {
	prev := ""
	for _, e := range entries {
		sum := sha256.Sum256([]byte(prev + "|" + e.Action + "|" + e.ID + "|" + e.Verdict))
		if e.Prev != prev || e.Hash != hex.EncodeToString(sum[:]) {
			return false
		}
		prev = e.Hash
	}
	return true
}
