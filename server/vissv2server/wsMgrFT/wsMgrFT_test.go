/**
* Tests for wsMgrFT.
* Regression coverage for the path-traversal and session-allocator fixes
* in PR #119.
**/
package wsMgrFT

import (
	"strings"
	"sync"
	"testing"
)

// TestValidateTransferName_AcceptsPlainFilenames is the happy-path check
// for the helper that gates os.Create / os.Open in initFtSession.
func TestValidateTransferName_AcceptsPlainFilenames(t *testing.T) {
	cases := []string{
		"upload.txt",
		"a.bin",
		"vehicle-log-2026-05-16.json",
		"snapshot",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateTransferName(name); err != nil {
				t.Fatalf("validateTransferName(%q) returned error: %v; expected nil", name, err)
			}
		})
	}
}

// TestValidateTransferName_RejectsUnsafe is the regression test for the
// PR #119 path-traversal fix. Every case below was reachable from a VISS
// 'set' on a FileDescriptor actuator and would otherwise have driven
// os.Create("./" + name) to escape the working directory.
func TestValidateTransferName_RejectsUnsafe(t *testing.T) {
	cases := []struct {
		name   string
		reason string // substring that must appear in the error
	}{
		{"", "empty"},
		{"../etc/passwd", "parent-directory"},
		{"..", "parent-directory"},
		{"../../etc/cron.d/owned", "parent-directory"},
		{"foo/bar", "path separators"},
		{"/etc/passwd", "path separators"},
		{"sub/dir/file.txt", "path separators"},
		// Edge case: a leading "./" survives filepath.Base on some
		// platforms; the explicit ".." substring check catches it.
		{"./../escape", "parent-directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTransferName(tc.name)
			if err == nil {
				t.Fatalf("validateTransferName(%q) returned nil; expected error containing %q", tc.name, tc.reason)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("validateTransferName(%q) error %q; expected substring %q", tc.name, err.Error(), tc.reason)
			}
		})
	}
}

// FuzzValidateTransferName runs the helper against pseudo-random inputs
// to ensure it never panics and that any name it accepts is safe by
// construction (a plain filename with no path separators and no parent-
// directory references).
//
// Run with: go test -fuzz=FuzzValidateTransferName -fuzztime=10s ./...
func FuzzValidateTransferName(f *testing.F) {
	seeds := []string{
		"upload.txt", "", "..", "../etc/passwd", "/abs", "foo/bar",
		"a/../b", "\x00", "a\nb", "very-long-" + strings.Repeat("a", 1024),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		err := validateTransferName(name)
		if err != nil {
			return // rejected; nothing more to check
		}
		// If accepted, the name must satisfy the contract every caller
		// of initFtSession relies on.
		if name == "" {
			t.Fatalf("validateTransferName accepted empty string")
		}
		if strings.Contains(name, "..") {
			t.Fatalf("validateTransferName accepted %q (contains ..)", name)
		}
		if strings.ContainsAny(name, "/\\") {
			t.Fatalf("validateTransferName accepted %q (contains path separator)", name)
		}
	})
}

// TestGetDataSessionIndex_ClaimsSlot is the regression test for the PR
// #119 fix that added the missing claim step to getDataSessionIndex.
// Before the fix, the function never set sessionList[i] = true and
// therefore always returned 0, so concurrent FT data sessions all
// shared clientChannel[0].
func TestGetDataSessionIndex_ClaimsSlot(t *testing.T) {
	// Reset shared state to a known baseline; tests in this package
	// share the package-level sessionList.
	for i := range sessionList {
		sessionList[i] = false
	}
	first := getDataSessionIndex()
	if first != 0 {
		t.Fatalf("first claim returned %d; expected 0", first)
	}
	second := getDataSessionIndex()
	if second == first {
		t.Fatalf("second claim returned the same slot %d; the claim step is broken", second)
	}
	if second != 1 {
		t.Fatalf("second claim returned %d; expected 1 (slot 0 was taken)", second)
	}
	// Returning the first slot makes it claimable again.
	returnDataSessionIndex(first)
	third := getDataSessionIndex()
	if third != 0 {
		t.Fatalf("after return, expected slot 0 to be reclaimed; got %d", third)
	}
	// Cleanup so other tests start from a known baseline.
	for i := range sessionList {
		sessionList[i] = false
	}
}

// TestGetDataSessionIndex_PoolExhaustion verifies the function returns
// -1 (and does not panic or wedge) when every slot is taken.
func TestGetDataSessionIndex_PoolExhaustion(t *testing.T) {
	for i := range sessionList {
		sessionList[i] = true
	}
	defer func() {
		for i := range sessionList {
			sessionList[i] = false
		}
	}()
	if got := getDataSessionIndex(); got != -1 {
		t.Fatalf("expected -1 when pool full; got %d", got)
	}
}

// TestSessionListMu_ConcurrentClaimsAreUnique is the regression test
// for the sessionListMu added in PR #119. Without the mutex, two
// concurrent claims could observe the same free slot and both return
// it, causing cross-talk between unrelated WS data sessions.
//
// Run with: go test -race
func TestSessionListMu_ConcurrentClaimsAreUnique(t *testing.T) {
	for i := range sessionList {
		sessionList[i] = false
	}
	defer func() {
		for i := range sessionList {
			sessionList[i] = false
		}
	}()

	n := MAXSESSIONS
	results := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = getDataSessionIndex()
		}(i)
	}
	wg.Wait()

	seen := make(map[int]int)
	for _, idx := range results {
		if idx == -1 {
			t.Fatalf("concurrent claim returned -1; pool should have had room for all %d", n)
		}
		seen[idx]++
	}
	for idx, count := range seen {
		if count > 1 {
			t.Fatalf("slot %d was claimed by %d goroutines concurrently; mutex is missing or broken", idx, count)
		}
	}
}
