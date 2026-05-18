/**
* Tests for filetransfer_client.
* Regression coverage for the client-side path-traversal fix in PR #121.
**/
package main

import (
	"strings"
	"testing"
)

// TestSafeServerFilename_AcceptsPlainFilenames is the happy-path check.
func TestSafeServerFilename_AcceptsPlainFilenames(t *testing.T) {
	cases := []string{
		"upload.txt",
		"snapshot.bin",
		"capture-2026-05-16.json",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := safeServerFilename(name)
			if err != nil {
				t.Fatalf("safeServerFilename(%q) returned error: %v; expected nil", name, err)
			}
			if got != name {
				t.Fatalf("safeServerFilename(%q) returned %q; expected the same value", name, got)
			}
		})
	}
}

// TestSafeServerFilename_RejectsUnsafe is the regression test for the
// PR #121 client-side path-traversal fix. A compromised or MITM'd
// server could otherwise return a 'name' that escapes the client's
// working directory.
func TestSafeServerFilename_RejectsUnsafe(t *testing.T) {
	cases := []struct {
		name   string
		reason string
	}{
		{"", "empty"},
		{"../etc/passwd", "parent-directory"},
		{"..", "parent-directory"},
		{"../../etc/cron.d/owned", "parent-directory"},
		{"foo/bar", "path separators"},
		{"/etc/passwd", "path separators"},
		{"./../escape", "parent-directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeServerFilename(tc.name)
			if err == nil {
				t.Fatalf("safeServerFilename(%q) returned %q with nil error; expected error containing %q", tc.name, got, tc.reason)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("safeServerFilename(%q) error %q; expected substring %q", tc.name, err.Error(), tc.reason)
			}
		})
	}
}

// FuzzSafeServerFilename ensures the helper never panics on attacker-
// controlled input and never accepts a name that violates its contract.
//
// Run with: go test -fuzz=FuzzSafeServerFilename -fuzztime=10s ./...
func FuzzSafeServerFilename(f *testing.F) {
	seeds := []string{
		"upload.txt", "", "..", "../etc/passwd", "/abs", "foo/bar",
		"a/../b", "\x00", "a\nb", strings.Repeat("a", 1024),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		got, err := safeServerFilename(name)
		if err != nil {
			return
		}
		// If accepted, contract: same string back, non-empty, no
		// separators, no parent-directory marker.
		if got != name {
			t.Fatalf("accepted name %q but returned %q (helper must not mutate)", name, got)
		}
		if name == "" {
			t.Fatalf("accepted empty name")
		}
		if strings.Contains(name, "..") {
			t.Fatalf("accepted %q (contains ..)", name)
		}
		if strings.ContainsAny(name, "/\\") {
			t.Fatalf("accepted %q (contains path separator)", name)
		}
	})
}
