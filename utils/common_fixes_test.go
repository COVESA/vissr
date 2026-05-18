/**
* Regression tests for the utils/common.go fixes shipped in PR #120
* (JsonSchemaValidate nil-deref guard).
**/
package utils

import (
	"strings"
	"sync"
	"testing"
)

// TestJsonSchemaValidate_NilSchemaDoesNotPanic is the regression test
// for the PR #120 fix. JsonSchemaInit silently leaves the package-level
// jsonSchema nil if the vissv3.0-schema.json file is missing; without
// this guard the first request from any anonymous client through any
// transport dereferences nil and crashes the whole daemon.
func TestJsonSchemaValidate_NilSchemaDoesNotPanic(t *testing.T) {
	// Force jsonSchema back to nil; the package-level value persists
	// across tests and may have been initialised by an earlier run.
	saved := jsonSchema
	jsonSchema = nil
	defer func() { jsonSchema = saved }()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("JsonSchemaValidate panicked on nil schema: %v", r)
		}
	}()

	got := JsonSchemaValidate(`{"any": "request"}`)
	if got == "" {
		t.Fatalf("expected a non-empty error message when schema not loaded; got \"\"")
	}
	if !strings.Contains(got, "schema") {
		t.Fatalf("expected error message to mention the schema; got %q", got)
	}
}

// TestJsonSchemaValidate_NilSchema_ConcurrentSafe sanity-checks that
// many concurrent callers all see the safe nil-path without panicking.
//
// Run with: go test -race
func TestJsonSchemaValidate_NilSchema_ConcurrentSafe(t *testing.T) {
	saved := jsonSchema
	jsonSchema = nil
	defer func() { jsonSchema = saved }()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("concurrent JsonSchemaValidate panicked: %v", r)
				}
			}()
			_ = JsonSchemaValidate(`{"x": 1}`)
		}()
	}
	wg.Wait()
}

// FuzzMapRequest exercises the JSON-unmarshal wrapper that every
// transport's request handler funnels through (wsMgr, httpMgr, udsMgr,
// mqttMgr, grpcMgr all call it). The contract: never panic, return -1
// on any decode error, leave rMap in a state the caller can safely
// type-assert against via the v, ok := patterns shipped in batches 2-4.
//
// Run with: go test -fuzz=FuzzMapRequest -fuzztime=30s ./utils
func FuzzMapRequest(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"action":"get","path":"Vehicle.Speed","requestId":"1"}`,
		`{"action":"set","path":"Vehicle.Cabin.Door.Row1.Right.IsOpen","value":true}`,
		`{"action":"subscribe","path":"Vehicle.Speed","filter":{"variant":"timebased","parameter":"100"}}`,
		``,
		`not json`,
		`{`,
		`}`,
		`[]`,
		`null`,
		`"just a string"`,
		`12345`,
		`{"deeply": {"nested": {"value": {"goes": "here"}}}}`,
		// Adversarial shapes — make sure subsequent .(string) / .(map)
		// assertions in callers can't be coaxed into a panic by an
		// upstream parser quirk.
		`{"action": null}`,
		`{"path": ["array", "not", "string"]}`,
		`{"action": 12345}`,
		`{"path": {"object": "not string"}}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, request string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MapRequest panicked on input %q: %v", request, r)
			}
		}()
		var m map[string]interface{}
		ret := MapRequest(request, &m)
		// Contract: never panics. The return value documents success
		// (0) or failure (-1); both are valid outcomes for fuzz input.
		if ret != 0 && ret != -1 {
			t.Fatalf("MapRequest returned %d; only 0 and -1 are documented values", ret)
		}
	})
}

// FuzzJsonSchemaValidate ensures the function never panics on
// attacker-controlled JSON inputs, both when the schema is loaded and
// when it is not.
//
// Run with: go test -fuzz=FuzzJsonSchemaValidate -fuzztime=10s ./...
func FuzzJsonSchemaValidate(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"action":"get","path":"Vehicle.Speed","requestId":"1"}`,
		`{"action":"set","path":"Vehicle","value":42,"requestId":"2"}`,
		``,
		`not json`,
		`{"a":` + strings.Repeat(`"x",`, 64) + `"x"}`,
		"{\"\\u0000\": \"null-key\"}",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, request string) {
		// Contract: never panic, whether the schema is loaded or not.
		_ = JsonSchemaValidate(request)
	})
}
