package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zyvorai/kavira/internal/api"
	"github.com/zyvorai/kavira/internal/bundle"
	"github.com/zyvorai/kavira/internal/compiler"
)

// version is stamped at build time: -ldflags "-X main.version=1.2.3".
var version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "compile":
		os.Exit(runCompile(os.Args[2:]))
	case "test":
		os.Exit(runTest(os.Args[2:]))
	case "serve":
		os.Exit(runServe(os.Args[2:]))
	case "healthcheck":
		os.Exit(runHealthcheck(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Println("kavira v" + version)
		return
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `kavira — turn an outage into a reproducible test

  kavira compile -f examples/oom.json -o repairs
  kavira test -f repairs/inc-oom-001/regression_test.json
  kavira serve -addr :8080
  kavira serve -addr :8443 -tls-self-signed ~/.kavira/tls -tls-host 203.0.113.7
  kavira healthcheck -url http://127.0.0.1:8080/api/v1/health

Execution decides. The proposer is not a verdict. Exact replay is not claimed.
`)
}

func runCompile(args []string) int {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	file := fs.String("f", "", "evidence bundle")
	out := fs.String("o", "repairs", "repair package directory")
	_ = fs.Parse(args)
	if *file == "" {
		fmt.Fprintln(os.Stderr, "compile: -f is required")
		return 2
	}
	b, err := bundle.Load(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rep := compiler.Compile(b)
	raw, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Println(string(raw))
	if rep.Repair == nil {
		fmt.Fprintf(os.Stderr, "verdict %s: no package written\n", rep.Verdict)
		return 0
	}
	if err := compiler.WritePackage(*out, b, rep); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", filepath.Join(*out, rep.ID))
	return 0
}

func runTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	file := fs.String("f", "", "regression_test.json")
	bundlePath := fs.String("bundle", "", "evidence bundle (default: sibling bundle.json)")
	_ = fs.Parse(args)
	if *file == "" {
		fmt.Fprintln(os.Stderr, "test: -f is required")
		return 2
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var test compiler.RegressionTest
	if err := json.Unmarshal(raw, &test); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	path := *bundlePath
	if path == "" {
		path = filepath.Join(filepath.Dir(*file), "bundle.json")
	}
	b, err := bundle.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	result, err := compiler.TestRepair(b, test)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "regression held")
	return 0
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	cert := fs.String("tls-cert", "", "TLS certificate (PEM); serve HTTPS")
	key := fs.String("tls-key", "", "TLS private key (PEM)")
	self := fs.String("tls-self-signed", "", "directory for a generated, reused self-signed certificate; serve HTTPS")
	hosts := fs.String("tls-host", "", "comma-separated extra hostnames/IPs the self-signed certificate must cover")
	_ = fs.Parse(args)
	opts := api.Options{Addr: *addr, CertFile: *cert, KeyFile: *key, SelfSignedDir: *self}
	if *hosts != "" {
		opts.Hosts = strings.Split(*hosts, ",")
	}
	if err := api.Serve(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// runHealthcheck exists for container HEALTHCHECKs: the runtime image has no shell or curl.
func runHealthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	url := fs.String("url", "http://127.0.0.1:8080/api/v1/health", "health endpoint")
	_ = fs.Parse(args)
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(*url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unhealthy:", res.Status)
		return 1
	}
	return 0
}
