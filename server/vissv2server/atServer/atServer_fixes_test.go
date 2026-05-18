/**
* Regression tests for the atServer fixes shipped across the
* stability/security batches (ef639f0, #119, #120, #121).
**/
package atServer

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/covesa/vissr/utils"
)

// init initialises the package-level utils loggers before tests that
// exercise paths logging via utils.Error / utils.Info. (The existing
// access_control_test.go in this package does not have its own
// TestMain, so we initialise via an init() function instead of fighting
// over which file owns TestMain.)
func init() {
	utils.InitLog("atServer-test.log", os.TempDir(), false, "error")
}

// TestAtServerHandler_RejectsOversizedBody is the regression test for
// the PR #119 MaxBytesReader on /ats. The AT endpoint is reachable
// pre-auth (it issues short-term access tokens) so an anonymous peer
// could OOM the daemon via a giant body before the fix.
func TestAtServerHandler_RejectsOversizedBody(t *testing.T) {
	handler := makeAtServerHandler(make(chan string, 4))

	body := bytes.NewReader(make([]byte, 256*1024)) // >> 64 KiB cap
	req := httptest.NewRequest("POST", "/ats", body)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected %d (Request Entity Too Large); got %d: %s",
			http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	}
}

// TestExtractKeyValue_ValidKey is the happy-path check for the safe-
// type-assert fix in PR #119.
func TestExtractKeyValue_ValidKey(t *testing.T) {
	input := `{"sessionId": "abc123", "consent": "ok"}`
	if got := extractKeyValue("sessionId", input); got != "abc123" {
		t.Fatalf("extractKeyValue: got %q; want %q", got, "abc123")
	}
	if got := extractKeyValue("consent", input); got != "ok" {
		t.Fatalf("extractKeyValue: got %q; want %q", got, "ok")
	}
}

// TestExtractKeyValue_MissingKey is the regression test for the PR #119
// fix that converted inputMap[key].(string) from an unguarded panic
// into a safe v, ok := check. Before the fix, this scenario panicked
// the central atServer event loop and tore down the daemon.
func TestExtractKeyValue_MissingKey(t *testing.T) {
	input := `{"sessionId": "abc123"}`
	if got := extractKeyValue("consent", input); got != "" {
		t.Fatalf("extractKeyValue with missing key: got %q; want \"\"", got)
	}
}

// TestExtractKeyValue_NonStringValue verifies the helper rejects non-
// string values without panicking.
func TestExtractKeyValue_NonStringValue(t *testing.T) {
	cases := []string{
		`{"sessionId": 12345}`,           // number
		`{"sessionId": null}`,            // null
		`{"sessionId": ["arr"]}`,         // array
		`{"sessionId": {"nested": "x"}}`, // object
		`{"sessionId": true}`,            // bool
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("extractKeyValue panicked on %q: %v", input, r)
				}
			}()
			if got := extractKeyValue("sessionId", input); got != "" {
				t.Fatalf("extractKeyValue on %q: got %q; want \"\"", input, got)
			}
		})
	}
}

// TestExtractKeyValue_MalformedJSON verifies the helper returns "" on
// non-JSON input.
func TestExtractKeyValue_MalformedJSON(t *testing.T) {
	cases := []string{
		"",
		"not json",
		"{bad}",
		`{"sessionId":`,
		"\x00\x01\x02",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if got := extractKeyValue("sessionId", input); got != "" {
				t.Fatalf("extractKeyValue on %q: got %q; want \"\"", input, got)
			}
		})
	}
}

// FuzzExtractKeyValue ensures the function never panics on attacker-
// controlled input — it was reachable from an anonymous POST to /ats
// before the PR #119 fix.
//
// Run with: go test -fuzz=FuzzExtractKeyValue -fuzztime=10s ./...
func FuzzExtractKeyValue(f *testing.F) {
	seeds := []struct {
		key   string
		input string
	}{
		{"sessionId", `{"sessionId": "abc"}`},
		{"sessionId", `{"sessionId": 1}`},
		{"sessionId", `{"sessionId": null}`},
		{"sessionId", `{}`},
		{"sessionId", ""},
		{"sessionId", `{"sessionId": ["arr"]}`},
		{"", `{"": "empty key"}`},
		{"a", `not json`},
	}
	for _, seed := range seeds {
		f.Add(seed.key, seed.input)
	}
	f.Fuzz(func(t *testing.T, key, input string) {
		// Contract: never panic. Return value can be anything.
		_ = extractKeyValue(key, input)
	})
}

// TestAddCheckJti_AcceptsNewRejectsReplay is the basic-semantics
// check on the jti replay-cache.
func TestAddCheckJti_AcceptsNewRejectsReplay(t *testing.T) {
	// Reset the package-level cache; tests in this package share it.
	jtiCacheMu.Lock()
	jtiCache = nil
	jtiCacheMu.Unlock()

	if !addCheckJti("jti-1") {
		t.Fatalf("first addCheckJti must accept new jti")
	}
	if addCheckJti("jti-1") {
		t.Fatalf("second addCheckJti must reject replay of same jti")
	}
	if !addCheckJti("jti-2") {
		t.Fatalf("different jti must be accepted")
	}
}

// TestAddCheckJti_ConcurrentSafe is the regression test for the PR #119
// jtiCacheMu mutex. Before that fix, two concurrent AGT requests with
// overlapping JTIs would race the map and the Go runtime would abort
// the process with "concurrent map read and map write".
//
// Run with: go test -race
func TestAddCheckJti_ConcurrentSafe(t *testing.T) {
	jtiCacheMu.Lock()
	jtiCache = nil
	jtiCacheMu.Unlock()

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	results := make([]bool, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			// Mix unique jtis and overlapping ones to exercise both
			// add and read-hit paths concurrently.
			if i%2 == 0 {
				results[i] = addCheckJti("shared-jti")
			} else {
				results[i] = addCheckJti("jti-")
			}
		}(i)
	}
	wg.Wait()
	// Exactly one goroutine each should have observed the "first time"
	// for "shared-jti" and "jti-" respectively. We don't make a stricter
	// claim than non-panic + at-least-one-accepted, since the race
	// detector is the primary signal here.
	acceptedShared := 0
	for i, ok := range results {
		if i%2 == 0 && ok {
			acceptedShared++
		}
	}
	if acceptedShared != 1 {
		t.Fatalf("expected exactly 1 acceptance of shared-jti across %d concurrent calls; got %d", n/2, acceptedShared)
	}
}
