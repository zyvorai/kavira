// Copyright 2026 Zyvor AI Labs · https://zyvor.dev
// SPDX-License-Identifier: LicenseRef-Zyvor-Production-2.0

package compiler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zyvorai/kavira/internal/bundle"
)

// PackageZIP builds an in-memory zip of the same files WritePackage would write.
// Returns an error when there is no repair package (execution did not confirm a repair).
func PackageZIP(b bundle.Bundle, rep Report) ([]byte, error) {
	if rep.Repair == nil {
		return nil, fmt.Errorf("no repair package for %s (%s)", rep.ID, rep.Verdict)
	}
	if rep.ID == "." || rep.ID == ".." || rep.ID != filepath.Base(rep.ID) || strings.ContainsAny(rep.ID, `/\`) {
		return nil, fmt.Errorf("bundle id %q is not a safe directory name", rep.ID)
	}
	files := map[string]any{
		"verdict.json":         rep,
		"patch.json":           rep.Repair.Patch,
		"regression_test.json": rep.Repair.RegressionTest,
		"measurements.json":    rep.Measurements,
		"bundle.json":          b,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, doc := range files {
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		w, err := zw.Create(rep.ID + "/" + name)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := w.Write(append(raw, '\n')); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	body := fmt.Sprintf("# Reproduce %s\n\n%s\n%s\n\nExact replay: no.\n", rep.ID, rep.Summary, rep.Repair.Instructions)
	w, err := zw.Create(rep.ID + "/REPRODUCE.md")
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	if _, err := w.Write([]byte(body)); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
