// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package compiler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zyvorai/kavira/internal/bundle"
)

// regressScript needs only a POSIX shell and the kavira binary (KAVIRA=/path/to/kavira to override).
const regressScript = `#!/bin/sh
# KAVIRA regression check for %s. Dependency-free: a POSIX shell and the kavira binary.
#   1. the captured condition must still FAIL when executed (the reconstruction is alive), and
#   2. the repaired condition must PASS.
# Exact replay is not claimed. Do not apply the repair to production from this result alone.
set -u
KAVIRA="${KAVIRA:-kavira}"
dir=$(cd "$(dirname "$0")" && pwd)
if ! command -v "$KAVIRA" >/dev/null 2>&1; then
  echo "ERROR: kavira binary not found (set KAVIRA=/path/to/kavira)" >&2
  exit 2
fi
if "$KAVIRA" test -f "$dir/before_test.json" --bundle "$dir/bundle.json" >/dev/null 2>&1; then
  echo "FAIL: the captured condition no longer fails, so this check proves nothing" >&2
  exit 1
fi
if ! "$KAVIRA" test -f "$dir/regression_test.json" --bundle "$dir/bundle.json" >/dev/null 2>&1; then
  echo "FAIL: the repaired condition does not hold" >&2
  exit 1
fi
echo "ok: fails on the captured condition, passes on the repaired one (reconstructed, not replayed)"
`

// RegressionFiles returns a standalone before/after check for a repair that held. There is no export
// when execution did not confirm a repair.
func RegressionFiles(b bundle.Bundle, rep Report) (map[string][]byte, error) {
	if rep.Repair == nil || rep.Verdict != "repair_held" {
		return nil, fmt.Errorf("no regression export for %s (%s): execution did not confirm a repair", rep.ID, rep.Verdict)
	}
	if rep.ID == "." || rep.ID == ".." || rep.ID != filepath.Base(rep.ID) || strings.ContainsAny(rep.ID, `/\`) {
		return nil, fmt.Errorf("bundle id %q is not a safe directory name", rep.ID)
	}
	// The "before" test applies no repair: it re-executes the captured condition and expects zero failures,
	// so it must fail. For a config regression the captured condition is the current snapshot.
	before := RegressionTest{ID: b.ID, Class: b.Class, Repair: map[string]any{}, Expect: rep.Repair.RegressionTest.Expect}
	if b.Class == bundle.ClassConfig {
		for k, v := range b.Configs.Current {
			before.Repair[k] = v
		}
	}
	files := map[string]any{
		"bundle.json":          b,
		"before_test.json":     before,
		"regression_test.json": rep.Repair.RegressionTest,
	}
	out := map[string][]byte{}
	for name, doc := range files {
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		out[name] = append(raw, '\n')
	}
	out["regress.sh"] = []byte(fmt.Sprintf(regressScript, shellComment(rep.ID)))
	return out, nil
}

// shellComment makes an untrusted id safe inside a one-line shell comment.
func shellComment(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// WriteRegression writes RegressionFiles under dir/<id>/regression/.
func WriteRegression(dir string, b bundle.Bundle, rep Report) (string, error) {
	files, err := RegressionFiles(b, rep)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, rep.ID, "regression")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	for name, raw := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dest, name), raw, mode); err != nil {
			return "", err
		}
	}
	return dest, nil
}

// RegressionZIP is the same files as an in-memory zip (for the API).
func RegressionZIP(b bundle.Bundle, rep Report) ([]byte, error) {
	files, err := RegressionFiles(b, rep)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		hdr := &zip.FileHeader{Name: rep.ID + "/regression/" + n, Method: zip.Deflate}
		hdr.SetMode(0o644)
		if strings.HasSuffix(n, ".sh") {
			hdr.SetMode(0o755)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
