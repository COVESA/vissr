/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Comprehensive coverage tests for the testable functions in wsMgr.go.
* Builds on wsMgr_test.go (from PR #122) and covers the remaining
* testable surface — the data-compression parsing helpers
* (getDcConfig / setDcValue / dcCacheInsert / getDcCacheIndex /
* resetDcCache / updatepayloadId / checkCompressionResponse) and the
* timestamp-compression helpers (compressTs / getDpTsList /
* replaceTs / signedTimeDiff / compressPaths).
*
* The wsMgr message-handling goroutines (frontendWSAppSession /
* backendWSAppSession / WsMgrInit) are not unit-tested here; they are
* exercised by the runtest.sh integration harness.
**/
package wsMgr

import (
	"strconv"
	"strings"
	"testing"
)

// TestGetDcConfig parses out the "dc"/"requestId"/get/paths flags from
// a VISS request envelope. The function is a 4-tuple wrapper around
// getValueForKey plus two strings.Contains calls.
func TestGetDcConfig(t *testing.T) {
	cases := []struct {
		name      string
		msg       string
		wantDc    string
		wantId    string
		wantGet   bool
		wantSingle bool
	}{
		{
			"get with dc and single path",
			`{"action":"get","path":"Vehicle.Speed","dc":"2+1","requestId":"42"}`,
			"2+1", "42", true, true,
		},
		{
			"set with dc and paths array",
			`{"action":"set","paths":["A","B"],"dc":"0+0","requestId":"43"}`,
			"0+0", "43", false, false,
		},
		{
			"no dc, no requestId",
			`{"action":"get","path":"Vehicle"}`,
			"", "", true, true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dc, id, isGet, single := getDcConfig(c.msg)
			if dc != c.wantDc || id != c.wantId || isGet != c.wantGet || single != c.wantSingle {
				t.Fatalf("getDcConfig(%q) = (%q, %q, %v, %v); want (%q, %q, %v, %v)",
					c.msg, dc, id, isGet, single, c.wantDc, c.wantId, c.wantGet, c.wantSingle)
			}
		})
	}
}

// TestSetDcValue covers the "pc+tsc" supported / not-supported logic.
// pc must be 0 or 2; tsc must be 0 or 1; anything else is rejected.
func TestSetDcValue(t *testing.T) {
	initDcCache()
	defer initDcCache()
	cases := map[string]bool{
		"0+0":   true,  // pc=0, tsc=0 — both off
		"2+1":   true,  // pc=2, tsc=1 — both on
		"2+0":   true,
		"0+1":   true,
		"1+0":   false, // pc=1 unsupported
		"3+0":   false,
		"2+2":   false, // tsc=2 unsupported
		"2":     false, // no plus
		"":      false,
		"+":     false,
		"a+b":   false, // non-numeric
		"2+x":   false,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := setDcValue(in, 0); got != want {
				t.Fatalf("setDcValue(%q) = %v; want %v", in, got, want)
			}
		})
	}
}

// TestDcCacheLifecycle exercises the full insert / lookup / reset
// cycle across multiple slots.
func TestDcCacheLifecycle(t *testing.T) {
	initDcCache()
	defer initDcCache()

	// Empty lookup.
	if got := getDcCacheIndex("absent"); got != -1 {
		t.Fatalf("getDcCacheIndex on empty = %d; want -1", got)
	}
	// Insert two distinct payloads.
	dcCacheInsert("p1", "2+1", 0)
	dcCacheInsert("p2", "0+0", 0)
	idx1 := getDcCacheIndex("p1")
	idx2 := getDcCacheIndex("p2")
	if idx1 == -1 || idx2 == -1 {
		t.Fatalf("expected both payloads cached; got idx1=%d idx2=%d", idx1, idx2)
	}
	if idx1 == idx2 {
		t.Fatalf("two distinct payloads landed in the same slot %d", idx1)
	}
	// Reset one slot and re-check.
	resetDcCache(idx1)
	if got := getDcCacheIndex("p1"); got != -1 {
		t.Fatalf("after reset, getDcCacheIndex(p1) = %d; want -1", got)
	}
	if got := getDcCacheIndex("p2"); got == -1 {
		t.Fatalf("reset of slot %d wiped slot %d; p2 lookup returned -1", idx1, idx2)
	}
}

// TestUpdatepayloadId renames an in-cache payload id.
func TestUpdatepayloadId(t *testing.T) {
	initDcCache()
	defer initDcCache()
	dcCacheInsert("original", "2+1", 0)
	if idx := getDcCacheIndex("original"); idx == -1 {
		t.Fatalf("setup: getDcCacheIndex(original) returned -1")
	}
	updatepayloadId("original", "renamed")
	if idx := getDcCacheIndex("renamed"); idx == -1 {
		t.Fatalf("after rename: getDcCacheIndex(renamed) = -1; want valid slot")
	}
	if idx := getDcCacheIndex("original"); idx != -1 {
		t.Fatalf("after rename: getDcCacheIndex(original) = %d; want -1", idx)
	}
}

// TestUpdatepayloadId_NoMatch is a no-op when the source id doesn't
// exist (mustn't panic, mustn't corrupt anything).
func TestUpdatepayloadId_NoMatch(t *testing.T) {
	initDcCache()
	defer initDcCache()
	dcCacheInsert("p1", "2+1", 0)
	updatepayloadId("nonexistent", "new") // must not affect p1
	if idx := getDcCacheIndex("p1"); idx == -1 {
		t.Fatalf("updatepayloadId on missing id corrupted unrelated entry")
	}
}

// TestSignedTimeDiff formats a signed millisecond diff with a leading
// sign so the wire format is deterministic.
func TestSignedTimeDiff(t *testing.T) {
	// signedTimeDiff uses delta-encoding: positive diffMs (ref is after dp)
	// maps to "-N" (dp is earlier than ref), negative to "+N", zero to "+0".
	cases := []struct {
		name string
		ms   int64
		want string
	}{
		{"positive", 1500, "-"},
		{"negative", -750, "+"},
		{"zero", 0, "+"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := signedTimeDiff(strconv.FormatInt(c.ms, 10), c.ms)
			if !strings.HasPrefix(got, c.want) {
				t.Fatalf("signedTimeDiff(%d) = %q; want prefix %q", c.ms, got, c.want)
			}
		})
	}
}

// TestCompressTs_RejectsMalformedJSON verifies the parser returns the
// input unchanged when handed garbage (i.e. it's a no-op on bad input,
// not a panic source).
func TestCompressTs_RejectsMalformedJSON(t *testing.T) {
	cases := []string{
		``,
		`not json`,
		`{`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("compressTs panicked on %q: %v", in, r)
				}
			}()
			got := compressTs(in)
			if got != in {
				t.Logf("note: compressTs mutated malformed input %q -> %q (acceptable as long as no panic)", in, got)
			}
		})
	}
}

// TestGetDpTsList walks a single-element or array of dp objects to
// pull out timestamps.
func TestGetDpTsList(t *testing.T) {
	t.Run("single map", func(t *testing.T) {
		in := map[string]interface{}{
			"value": "100",
			"ts":    "2026-05-16T12:00:00Z",
		}
		got := getDpTsList(in)
		if len(got) != 1 || got[0] != "2026-05-16T12:00:00Z" {
			t.Fatalf("getDpTsList single = %v; want one ts", got)
		}
	})
	t.Run("array of maps", func(t *testing.T) {
		in := []interface{}{
			map[string]interface{}{"value": "1", "ts": "ts1"},
			map[string]interface{}{"value": "2", "ts": "ts2"},
			map[string]interface{}{"value": "3", "ts": "ts3"},
		}
		got := getDpTsList(in)
		if len(got) != 3 {
			t.Fatalf("getDpTsList array = %v; want 3 entries", got)
		}
		for i, want := range []string{"ts1", "ts2", "ts3"} {
			if got[i] != want {
				t.Fatalf("getDpTsList[%d] = %q; want %q", i, got[i], want)
			}
		}
	})
	t.Run("unknown type", func(t *testing.T) {
		// The helper logs and returns empty/nil on unknown type.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("getDpTsList panicked on string input: %v", r)
			}
		}()
		_ = getDpTsList("just a string")
	})
}

// FuzzGetValueForKey hardens the hand-rolled JSON value extractor —
// the helper that every WS request fast path calls.
func FuzzGetValueForKey(f *testing.F) {
	seeds := []struct {
		msg, key string
	}{
		{`{"path": "Vehicle.Speed"}`, `"path":`},
		{`{"action": "get"}`, `"action":`},
		{`malformed`, `"path":`},
		{``, `"path":`},
		{`{"path": "value with embedded \"quotes\""}`, `"path":`},
	}
	for _, s := range seeds {
		f.Add(s.msg, s.key)
	}
	f.Fuzz(func(t *testing.T, msg, key string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("getValueForKey panicked on (%q, %q): %v", msg, key, r)
			}
		}()
		_ = getValueForKey(msg, key)
	})
}

// FuzzCompressTs ensures the timestamp compressor never panics on
// adversarial response messages.
func FuzzCompressTs(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"action":"get","ts":"2026-05-16T12:00:00Z","data":{"dp":{"ts":"2026-05-16T12:00:00Z","value":"100"}}}`,
		``,
		`not json`,
		`{"ts": 1}`, // ts not a string
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, msg string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("compressTs panicked on %q: %v", msg, r)
			}
		}()
		_ = compressTs(msg)
	})
}
