/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for the pure helper functions in vissv2server.go.
*
* These functions are reachable from the main message-dispatch loop but
* have no goroutine / channel / VSS-tree state of their own, so they
* can be tested in isolation. The goroutine-driven entry points
* (serveRequest, issueServiceRequest, initiateFileTransfer,
* serviceDataSession, transportDataSession) are exercised end-to-end by
* runtest.sh integration; they are not unit-tested here.
*
* See TESTING.md at the repo root for the list of helpers covered by
* this file and the broader test-debt picture.
**/
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractMgrId checks the "mgrId?clientId" parser used by the
// service-side dispatch loop to route responses back to the
// originating transport manager.
func TestExtractMgrId(t *testing.T) {
	cases := map[string]int{
		"0?42":     0,
		"1?0":      1,
		"99?12345": 99,
	}
	for routerId, want := range cases {
		t.Run(routerId, func(t *testing.T) {
			if got := extractMgrId(routerId); got != want {
				t.Fatalf("extractMgrId(%q) = %d; want %d", routerId, got, want)
			}
		})
	}
}

// TestGetPathLen pins down the null-terminator-aware length function
// used when reading paths out of the fixed-size buffers in
// utils.SearchData_t.
func TestGetPathLen(t *testing.T) {
	cases := map[string]int{
		"":              0,
		"Vehicle":       7,
		"Vehicle.Speed": 13,
		"abc\x00xyz":    3, // stops at NUL terminator
		"\x00":          0,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := getPathLen(in); got != want {
				t.Fatalf("getPathLen(%q) = %d; want %d", in, got, want)
			}
		})
	}
}

// TestCountPathSegments verifies the dot-separated segment count.
func TestCountPathSegments(t *testing.T) {
	cases := map[string]int{
		"Vehicle":                  1,
		"Vehicle.Speed":            2,
		"Vehicle.Cabin.Door.Row1":  4,
		"":                         1, // edge: empty path counts as one (no dots)
		"...":                      4,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := countPathSegments(in); got != want {
				t.Fatalf("countPathSegments(%q) = %d; want %d", in, got, want)
			}
		})
	}
}

// TestGetTokenErrorMessage covers every documented error-code index
// plus an unknown-index baseline.
func TestGetTokenErrorMessage(t *testing.T) {
	knownIndices := []int{1, 2, 5, 6, 10, 11, 15, 16, 20, 21, 30, 40, 41, 42, 60}
	for _, idx := range knownIndices {
		t.Run("known", func(t *testing.T) {
			got := getTokenErrorMessage(idx)
			if got == "" {
				t.Fatalf("getTokenErrorMessage(%d) returned empty string", idx)
			}
		})
	}
	// Unknown indices should not panic; they may return empty.
	unknownIndices := []int{-1, 0, 99, 9999}
	for _, idx := range unknownIndices {
		t.Run("unknown", func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("getTokenErrorMessage(%d) panicked: %v", idx, r)
				}
			}()
			_ = getTokenErrorMessage(idx)
		})
	}
}

// TestSetTokenErrorResponse verifies that the helper populates the
// expected fields on errRespMap.
func TestSetTokenErrorResponse(t *testing.T) {
	reqMap := map[string]interface{}{
		"RouterId":  "0?1",
		"action":    "get",
		"path":      "Vehicle.Speed",
		"requestId": "42",
	}
	// Reset the package-level errorResponseMap so the assertion is
	// deterministic. Tests in this package may share it.
	for k := range errorResponseMap {
		delete(errorResponseMap, k)
	}
	setTokenErrorResponse(reqMap, 1) // 1 = "Invalid Access Token"
	// At minimum, the response should now carry some error indication.
	if len(errorResponseMap) == 0 {
		t.Fatalf("setTokenErrorResponse left errorResponseMap empty")
	}
}

// TestSingleToDoubleQuote replaces every single-quote with a
// double-quote. The function mutates an internal string buffer so the
// returned value is the canonical one to check.
func TestSingleToDoubleQuote(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"no quotes":       "no quotes",
		"'hello'":         `"hello"`,
		`{'a':'b'}`:       `{"a":"b"}`,
		"already \"ok\"":  "already \"ok\"",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := singleToDoubleQuote(in); got != want {
				t.Fatalf("singleToDoubleQuote(%q) = %q; want %q", in, got, want)
			}
		})
	}
}

// TestExtractNoScopeElementsLevel1 walks the top-level shape produced
// by the AT server's noscope response.
func TestExtractNoScopeElementsLevel1(t *testing.T) {
	t.Run("string value", func(t *testing.T) {
		input := map[string]interface{}{"signals": "Vehicle.Secret"}
		list, count := extractNoScopeElementsLevel1(input)
		if count != 1 || len(list) != 1 {
			t.Fatalf("expected single-element list; got count=%d list=%v", count, list)
		}
		if list[0] != "Vehicle.Secret" {
			t.Fatalf("expected list[0] = Vehicle.Secret; got %q", list[0])
		}
	})
	t.Run("array value", func(t *testing.T) {
		input := map[string]interface{}{
			"signals": []interface{}{"Vehicle.A", "Vehicle.B", "Vehicle.C"},
		}
		_, count := extractNoScopeElementsLevel1(input)
		if count != 3 {
			t.Fatalf("expected count=3; got %d", count)
		}
	})
	t.Run("empty map", func(t *testing.T) {
		_, count := extractNoScopeElementsLevel1(map[string]interface{}{})
		if count != 0 {
			t.Fatalf("expected count=0; got %d", count)
		}
	})
}

// TestExtractNoScopeElementsLevel2 walks an interface{} array of
// signal paths.
func TestExtractNoScopeElementsLevel2(t *testing.T) {
	in := []interface{}{"A", "B", "C"}
	list, count := extractNoScopeElementsLevel2(in)
	if count != 3 || len(list) != 3 {
		t.Fatalf("expected count=3 len=3; got count=%d len=%d", count, len(list))
	}
	for i, want := range []string{"A", "B", "C"} {
		if list[i] != want {
			t.Fatalf("list[%d] = %q; want %q", i, list[i], want)
		}
	}
}

// TestGetTokenContext_AbsentReturnsEmpty covers the safe-default branch.
func TestGetTokenContext_AbsentReturnsEmpty(t *testing.T) {
	if got := getTokenContext(map[string]interface{}{}); got != "" {
		t.Fatalf("getTokenContext on empty map = %q; want \"\"", got)
	}
	if got := getTokenContext(map[string]interface{}{"authorization": nil}); got != "" {
		t.Fatalf("getTokenContext with nil authorization = %q; want \"\"", got)
	}
}

// TestRemoveLocalProperty deletes "local" keys from nested maps used
// by the HIM (Host Interface Mapping) loader.
func TestRemoveLocalProperty(t *testing.T) {
	in := map[string]interface{}{
		"section1": map[string]interface{}{
			"local":      "should be removed",
			"production": "should remain",
		},
		"section2": map[string]interface{}{
			"foo": "bar",
		},
	}
	out := removeLocalProperty(in)
	s1, _ := out["section1"].(map[string]interface{})
	if _, ok := s1["local"]; ok {
		t.Fatalf("local key was not removed from section1")
	}
	if _, ok := s1["production"]; !ok {
		t.Fatalf("production key was incorrectly removed from section1")
	}
	s2, _ := out["section2"].(map[string]interface{})
	if _, ok := s2["foo"]; !ok {
		t.Fatalf("section2.foo was incorrectly removed")
	}
}

// TestGetInternalFileName documents the hardcoded mapping used for
// FileTransfer uploads. Currently only "Vehicle.UploadFile" maps to a
// known name; all others fall back to "upload.txt".
func TestGetInternalFileName(t *testing.T) {
	cases := map[string]struct{ path, name string }{
		// Known path: returns the actual filename.
		"Vehicle.UploadFile": {"", "upload.txt"},
		// Unknown paths now return ("", "") — security fix: previously
		// returned ("", "upload.txt") for any input, which was a path-
		// injection hazard (any attacker-controlled path silently mapped to
		// the real upload file).
		"Vehicle.UnknownPath": {"", ""},
		"": {"", ""},
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			path, name := getInternalFileName(in)
			if path != want.path || name != want.name {
				t.Fatalf("getInternalFileName(%q) = (%q, %q); want (%q, %q)",
					in, path, name, want.path, want.name)
			}
		})
	}
}

// TestCalculateHash verifies the SHA-1 hash computation matches the
// stdlib implementation when applied to a real temp file.
func TestCalculateHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hash-input.bin")
	payload := []byte("the quick brown fox jumps over the lazy dog")
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	expectedSum := sha1.Sum(payload)
	expected := hex.EncodeToString(expectedSum[:])

	got := calculateHash(path)
	if got != expected {
		t.Fatalf("calculateHash(%q) = %q; want %q", path, got, expected)
	}
}

// TestCalculateHash_MissingFile must return empty (not panic).
func TestCalculateHash_MissingFile(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("calculateHash panicked on missing file: %v", r)
		}
	}()
	if got := calculateHash("/nonexistent/path/that/should/not/exist"); got != "" {
		t.Fatalf("calculateHash on missing file = %q; want \"\"", got)
	}
}

// TestGetRangeBoundary extracts the boundary value from a single
// filter-parameter object.
func TestGetRangeBoundary(t *testing.T) {
	cases := map[string]string{
		`single boundary`: `100`,
		`with logic-op`:   `200`,
		`missing key`:     ``,
	}
	inputs := map[string]map[string]interface{}{
		`single boundary`: {"boundary": "100"},
		`with logic-op`:   {"logic-op": "gt", "boundary": "200"},
		`missing key`:     {"logic-op": "gt"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if got := getRangeBoundary(inputs[name]); got != want {
				t.Fatalf("getRangeBoundary(%v) = %q; want %q", inputs[name], got, want)
			}
		})
	}
}

// TestGetRangeBoundaries handles both shapes the filter parser
// produces: a single object (one boundary) and an array of objects
// (two boundaries).
func TestGetRangeBoundaries(t *testing.T) {
	t.Run("single map", func(t *testing.T) {
		in := map[string]interface{}{
			"logic-op": "gt",
			"boundary": "100",
		}
		a, b := getRangeBoundaries(in)
		if a != "100" || b != "" {
			t.Fatalf("getRangeBoundaries(single) = (%q, %q); want (%q, %q)", a, b, "100", "")
		}
	})
	t.Run("array two", func(t *testing.T) {
		in := []interface{}{
			map[string]interface{}{"boundary": "10"},
			map[string]interface{}{"boundary": "20"},
		}
		a, b := getRangeBoundaries(in)
		if a != "10" || b != "20" {
			t.Fatalf("getRangeBoundaries(array) = (%q, %q); want (%q, %q)", a, b, "10", "20")
		}
	})
	t.Run("array overflow", func(t *testing.T) {
		// Three-element array: the loop log-warns and breaks. First two
		// elements should still be extracted.
		in := []interface{}{
			map[string]interface{}{"boundary": "1"},
			map[string]interface{}{"boundary": "2"},
			map[string]interface{}{"boundary": "3"},
		}
		a, b := getRangeBoundaries(in)
		if a != "1" || b != "2" {
			t.Fatalf("getRangeBoundaries(overflow) = (%q, %q); want (%q, %q)", a, b, "1", "2")
		}
	})
	t.Run("unknown type", func(t *testing.T) {
		// Helper logs an info message but must not panic.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("getRangeBoundaries panicked on unknown type: %v", r)
			}
		}()
		a, b := getRangeBoundaries("string-not-a-map-or-array")
		if a != "" || b != "" {
			t.Fatalf("expected empty boundaries for unknown type; got (%q, %q)", a, b)
		}
	})
}

// FuzzGetRangeBoundaries makes sure the helper never panics on
// adversarial filter-parameter shapes.
func FuzzGetRangeBoundaries(f *testing.F) {
	seeds := []string{
		"single",
		"array",
		"three-elem",
		"unknown-type",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, mode string) {
		// Construct different attacker-controlled shapes based on the
		// fuzzed mode key — keeps the fuzz coverage broad without
		// requiring the fuzzer to invent interface{} JSON shapes.
		var in interface{}
		switch {
		case strings.Contains(mode, "string"):
			in = mode
		case strings.Contains(mode, "array"):
			in = []interface{}{map[string]interface{}{"boundary": mode}}
		case strings.Contains(mode, "null"):
			in = nil
		default:
			in = map[string]interface{}{"boundary": mode}
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("getRangeBoundaries panicked on %v: %v", in, r)
			}
		}()
		_, _ = getRangeBoundaries(in)
	})
}
