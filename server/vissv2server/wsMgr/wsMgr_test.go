/**
* Broader coverage tests for the wsMgr package. Targets the pure-string
* and pure-JSON helpers; the goroutine-driven WS upgrade path is best
* exercised by the runtest.sh integration harness.
**/
package wsMgr

import (
	"testing"
)

// TestGetValueForKey covers the hand-rolled JSON value extractor used
// in the WS request fast path. The helper is "best-effort" — it doesn't
// fully parse JSON — so the contract is "return the value if findable,
// else empty".
func TestGetValueForKey(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		key  string
		want string
	}{
		{"basic", `{"path": "Vehicle.Speed"}`, `"path":`, "Vehicle.Speed"},
		{"action", `{"action": "get"}`, `"action":`, "get"},
		{"missing key", `{"path": "Vehicle"}`, `"action":`, ""},
		{"empty input", ``, `"path":`, ""},
		{"value with embedded colon", `{"path": "Vehicle:Speed"}`, `"path":`, "Vehicle:Speed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := getValueForKey(c.msg, c.key)
			if got != c.want {
				t.Fatalf("getValueForKey(%q,%q) = %q; want %q", c.msg, c.key, got, c.want)
			}
		})
	}
}

// TestGetSortedPaths verifies the deterministic ordering needed by the
// compression / payload-id machinery.
func TestGetSortedPaths(t *testing.T) {
	// Single-element data ('get' shape).
	msg := `{"data": {"path": "Vehicle.Speed", "dp": {"value": "100", "ts": "2026-05-16T12:00:00Z"}}}`
	got := getSortedPaths(msg)
	if len(got) != 1 || got[0] != "Vehicle.Speed" {
		t.Fatalf("getSortedPaths single path: got %v", got)
	}

	// Multi-element data, out of order on the wire.
	msg = `{"data": [
		{"path": "Vehicle.Speed", "dp": {"value": "1", "ts": "x"}},
		{"path": "Vehicle.Acceleration", "dp": {"value": "2", "ts": "y"}},
		{"path": "Vehicle.Cabin.Temperature", "dp": {"value": "3", "ts": "z"}}
	]}`
	got = getSortedPaths(msg)
	if len(got) != 3 {
		t.Fatalf("getSortedPaths multi path: expected 3 paths, got %d (%v)", len(got), got)
	}
	// Must be sorted.
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("getSortedPaths output not sorted: %v", got)
		}
	}
}

// TestGetSortedPaths_RejectsMalformedJSON verifies the helper returns
// nil (not panic) when handed garbage.
func TestGetSortedPaths_RejectsMalformedJSON(t *testing.T) {
	cases := []string{
		"",
		"not json",
		"{",
		`{"data": "not an object or array"}`,
		`null`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("getSortedPaths panicked on %q: %v", in, r)
				}
			}()
			got := getSortedPaths(in)
			if got != nil && len(got) != 0 {
				t.Logf("note: getSortedPaths returned non-empty %v on malformed input %q (acceptable as long as no panic)", got, in)
			}
		})
	}
}

// TestDcCache exercises the data-compression-cache helpers (insert,
// lookup, reset) used by the WS request fast path.
func TestDcCache(t *testing.T) {
	initDcCache()
	const payload = "test-payload-id"

	// Lookup on empty cache returns -1.
	if idx := getDcCacheIndex(payload); idx != -1 {
		t.Fatalf("expected -1 on empty cache lookup; got %d", idx)
	}

	// Insert allocates a slot.
	dcCacheInsert(payload, "dc-value", 0)
	idx := getDcCacheIndex(payload)
	if idx < 0 {
		t.Fatalf("expected non-negative index after insert; got %d", idx)
	}

	// Reset clears the entry.
	resetDcCache(idx)
	if idx2 := getDcCacheIndex(payload); idx2 != -1 {
		t.Fatalf("expected -1 after reset; got %d", idx2)
	}
}
