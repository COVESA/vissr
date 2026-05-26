/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for the testable pure functions in compress_client:
* pathToUrl, jsonToStructList, retrieveRequest, createListFromFile.
*
* The network-driven entry points (initVissV2WebSocket, getResponse,
* performCommand, performPbCommand, main, the interactive
* displayOptions / readOption) need a running server or a TTY; they
* are exercised by runtest.sh integration and are not unit-tested.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("compress_client-test.log", os.TempDir(), false, "error")
}

// TestPathToUrl converts VSS dot-paths into URL slash-paths.
func TestPathToUrl(t *testing.T) {
	cases := map[string]string{
		"Vehicle":              "/Vehicle",
		"Vehicle.Speed":        "/Vehicle/Speed",
		"Vehicle.Cabin.Door":   "/Vehicle/Cabin/Door",
		"":                     "/",
		"Already/Slashed":      "/Already/Slashed", // no dots, just prepended slash
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := pathToUrl(in); got != want {
				t.Fatalf("pathToUrl(%q) = %q; want %q", in, got, want)
			}
		})
	}
}

// TestJsonToStructList_AcceptsValidJSON. The helper returns the count
// of decoded list items; zero indicates a parse error or wrong shape.
func TestJsonToStructList_AcceptsValidJSON(t *testing.T) {
	valid := `{"request": [{"action":"get","path":"Vehicle.Speed"}]}`
	got := jsonToStructList(valid)
	if got != 1 {
		t.Fatalf("jsonToStructList valid input = %d; want 1", got)
	}
}

func TestJsonToStructList_RejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"not json",
		`{`,
		`{"request": "not an array or object"}`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("jsonToStructList panicked on %q: %v", in, r)
				}
			}()
			if got := jsonToStructList(in); got != 0 {
				t.Fatalf("jsonToStructList(%q) = %d; want 0", in, got)
			}
		})
	}
}

// TestRetrieveRequest marshals a request map back to JSON.
func TestRetrieveRequest(t *testing.T) {
	in := map[string]interface{}{
		"action": "get",
		"path":   "Vehicle.Speed",
	}
	got := retrieveRequest(in)
	if got == "" {
		t.Fatalf("retrieveRequest returned empty for valid input")
	}
	// Must round-trip the action and path.
	if !contains(got, "get") || !contains(got, "Vehicle.Speed") {
		t.Fatalf("retrieveRequest = %q; missing expected fields", got)
	}
}

func TestRetrieveRequest_EmptyInput(t *testing.T) {
	got := retrieveRequest(map[string]interface{}{})
	if got != "{}" {
		t.Fatalf("retrieveRequest empty = %q; want \"{}\"", got)
	}
}

// TestCreateListFromFile reads a JSON file and parses it.
func TestCreateListFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "request.json")
	content := `{"request": [{"action":"get","path":"Vehicle.Speed"}]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := createListFromFile(path); got != 1 {
		t.Fatalf("createListFromFile = %d; want 1", got)
	}
}

func TestCreateListFromFile_MissingFile(t *testing.T) {
	if got := createListFromFile("/nonexistent/path/should/not/exist"); got != 0 {
		t.Fatalf("createListFromFile missing = %d; want 0", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
