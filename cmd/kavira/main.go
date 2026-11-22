// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
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
	case "summarize":
		os.Exit(runSummarize(os.Args[2:]))
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
  kavira compile -d examples -o repairs
  kavira test -f repairs/inc-oom-001/regression_test.json
  kavira test -f repairs/inc-oom-001/regression_test.json --format junit
  kavira summarize -f repairs/inc-oom-001/verdict.json
  kavira serve -addr :8080
  kavira serve -addr :8443 -tls-self-signed ~/.kavira/tls -tls-host 203.0.113.7
  kavira healthcheck -url http://127.0.0.1:8080/api/v1/health

Execution decides. The proposer is not a verdict. Exact replay is not claimed.
`)
}

func runCompile(args []string) int {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	file := fs.String("f", "", "evidence bundle")
	dir := fs.String("d", "", "directory of evidence bundles (*.json)")
	out := fs.String("o", "repairs", "repair package directory")
	quiet := fs.Bool("q", false, "print only verdicts (batch-friendly)")
	_ = fs.Parse(args)
	if *file == "" && *dir == "" {
		fmt.Fprintln(os.Stderr, "compile: -f or -d is required")
		return 2
	}
	if *file != "" && *dir != "" {
		fmt.Fprintln(os.Stderr, "compile: use -f or -d, not both")
		return 2
	}
	paths := []string{*file}
	if *dir != "" {
		entries, err := os.ReadDir(*dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		paths = nil
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			paths = append(paths, filepath.Join(*dir, e.Name()))
		}
		if len(paths) == 0 {
			fmt.Fprintln(os.Stderr, "compile: no *.json bundles in", *dir)
			return 1
		}
	}
	exit := 0
	for _, path := range paths {
		code := compileOne(path, *out, *quiet || *dir != "")
		if code > exit {
			exit = code
		}
	}
	return exit
}

func compileOne(path, out string, quiet bool) int {
	b, err := bundle.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, path+":", err)
		return 1
	}
	rep := compiler.Compile(b)
	if quiet {
		fmt.Printf("%s\t%s\t%s\n", rep.ID, rep.Verdict, path)
	} else {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(raw))
	}
	if rep.Repair == nil {
		fmt.Fprintf(os.Stderr, "verdict %s: no package written\n", rep.Verdict)
		if rep.Verdict == "repair_failed" {
			return 1
		}
		return 0
	}
	if err := compiler.WritePackage(out, b, rep); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", filepath.Join(out, rep.ID))
	return 0
}

func runTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	file := fs.String("f", "", "regression_test.json")
	dir := fs.String("d", "", "directory of repair packages (each with regression_test.json)")
	bundlePath := fs.String("bundle", "", "evidence bundle (default: sibling bundle.json)")
	format := fs.String("format", "json", "output format: json, junit, github")
	_ = fs.Parse(args)
	if *file == "" && *dir == "" {
		fmt.Fprintln(os.Stderr, "test: -f or -d is required")
		return 2
	}
	if *file != "" && *dir != "" {
		fmt.Fprintln(os.Stderr, "test: use -f or -d, not both")
		return 2
	}
	type caseResult struct {
		ID     string
		Class  string
		Result any
		Err    error
	}
	var cases []caseResult
	runOne := func(testPath, bPath string) {
		raw, err := os.ReadFile(testPath)
		if err != nil {
			cases = append(cases, caseResult{ID: testPath, Err: err})
			return
		}
		var test compiler.RegressionTest
		if err := json.Unmarshal(raw, &test); err != nil {
			cases = append(cases, caseResult{ID: testPath, Err: err})
			return
		}
		path := bPath
		if path == "" {
			path = filepath.Join(filepath.Dir(testPath), "bundle.json")
		}
		b, err := bundle.Load(path)
		if err != nil {
			cases = append(cases, caseResult{ID: test.ID, Class: test.Class, Err: err})
			return
		}
		result, err := compiler.TestRepair(b, test)
		cases = append(cases, caseResult{ID: test.ID, Class: test.Class, Result: result, Err: err})
	}
	if *file != "" {
		runOne(*file, *bundlePath)
	} else {
		entries, err := os.ReadDir(*dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			tpath := filepath.Join(*dir, e.Name(), "regression_test.json")
			if _, err := os.Stat(tpath); err != nil {
				continue
			}
			runOne(tpath, "")
		}
		if len(cases) == 0 {
			fmt.Fprintln(os.Stderr, "test: no regression_test.json under", *dir)
			return 1
		}
	}
	failed := 0
	for _, c := range cases {
		if c.Err != nil {
			failed++
		}
	}
	switch strings.ToLower(*format) {
	case "json":
		if len(cases) == 1 {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(cases[0].Result)
			if cases[0].Err != nil {
				fmt.Fprintln(os.Stderr, cases[0].Err)
				return 1
			}
			fmt.Fprintln(os.Stderr, "regression held")
			return 0
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		out := make([]map[string]any, 0, len(cases))
		for _, c := range cases {
			m := map[string]any{"id": c.ID, "class": c.Class, "result": c.Result}
			if c.Err != nil {
				m["error"] = c.Err.Error()
			}
			out = append(out, m)
		}
		_ = enc.Encode(out)
	case "junit":
		type tc struct {
			XMLName   xml.Name `xml:"testcase"`
			Name      string   `xml:"name,attr"`
			Classname string   `xml:"classname,attr"`
			Failure   *struct {
				Message string `xml:"message,attr"`
				Body    string `xml:",chardata"`
			} `xml:"failure,omitempty"`
		}
		suite := struct {
			XMLName  xml.Name `xml:"testsuite"`
			Name     string   `xml:"name,attr"`
			Tests    int      `xml:"tests,attr"`
			Failures int      `xml:"failures,attr"`
			Cases    []tc
		}{Name: "kavira", Tests: len(cases), Failures: failed}
		for _, c := range cases {
			item := tc{Name: c.ID, Classname: c.Class}
			if c.Err != nil {
				item.Failure = &struct {
					Message string `xml:"message,attr"`
					Body    string `xml:",chardata"`
				}{Message: c.Err.Error(), Body: c.Err.Error()}
			}
			suite.Cases = append(suite.Cases, item)
		}
		fmt.Print(xml.Header)
		enc := xml.NewEncoder(os.Stdout)
		enc.Indent("", "  ")
		_ = enc.Encode(suite)
		fmt.Println()
	case "github":
		for _, c := range cases {
			if c.Err != nil {
				fmt.Printf("::error title=kavira %s::%s\n", c.ID, c.Err.Error())
			} else {
				fmt.Printf("::notice title=kavira %s::regression held\n", c.ID)
			}
		}
	default:
		fmt.Fprintln(os.Stderr, "test: unknown --format", *format)
		return 2
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func runSummarize(args []string) int {
	fs := flag.NewFlagSet("summarize", flag.ExitOnError)
	file := fs.String("f", "", "verdict.json from a repair package, or a compile report")
	_ = fs.Parse(args)
	var raw []byte
	var err error
	if *file == "" || *file == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(*file)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var rep compiler.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if rep.ID == "" || rep.Verdict == "" {
		fmt.Fprintln(os.Stderr, "summarize: not a compile report (need id and verdict)")
		return 1
	}
	fmt.Print(compiler.Summarize(rep))
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
