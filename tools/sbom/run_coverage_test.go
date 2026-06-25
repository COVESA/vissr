package main

// Coverage for run() (the testable core extracted from main) and
// goListPackages, which the original suite left at 0%.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoListPackages_ListsModules(t *testing.T) {
	pkgs, err := goListPackages()
	if err != nil {
		t.Fatalf("goListPackages: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("expected at least the main module")
	}
	for _, p := range pkgs {
		if p.Name == "" {
			t.Errorf("package missing Name: %+v", p)
		}
		if !strings.HasPrefix(p.SPDXID, "SPDXRef-Package-") {
			t.Errorf("unexpected SPDXID %q", p.SPDXID)
		}
	}
}

func TestRun_TVToFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "sbom.spdx")
	if err := run("2.3", "tv", out, "vissr", "", &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "SPDXVersion:") || !strings.Contains(s, "SPDXID: SPDXRef-DOCUMENT") {
		t.Errorf("tag-value output missing SPDX header: %.200s", s)
	}
}

func TestRun_JSONToStdout(t *testing.T) {
	var buf bytes.Buffer
	if err := run("3.0.1", "json", "", "vissr", "https://example.com/ns", &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), `"spdxVersion"`) && !strings.Contains(buf.String(), `"SPDXID"`) {
		t.Errorf("json output missing SPDX fields: %.200s", buf.String())
	}
}

func TestRun_UnsupportedVersion(t *testing.T) {
	err := run("9.9", "tv", "", "vissr", "", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unsupported SPDX version") {
		t.Errorf("expected unsupported-version error, got %v", err)
	}
}

func TestRun_UnsupportedFormat(t *testing.T) {
	err := run("2.3", "xml", "", "vissr", "", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("expected unsupported-format error, got %v", err)
	}
}
