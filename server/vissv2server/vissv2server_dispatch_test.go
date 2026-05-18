/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the helpers extracted from serveRequest / issueServiceRequest
* in PR #128 (this PR):
*
*   - isInternalAction       (kill/cancel-subscriptions predicate)
*   - isUnsubscribeRequest   (client-driven unsubscribe predicate)
*   - setErrorAndForward     (dedup of the SetErrorResponse -> backendChan
*                              -> return pattern that appeared 6x inline)
*   - expandPathFilter       (single path vs JSON-array path-filter expansion)
*   - authorizeAccess        (token-verification dispatch logic, minus the
*                              live verifyToken delegation path which needs
*                              the atsChannel goroutine)
*
* Same shape as the dispatch tests in PRs #124 (udsMgr+httpMgr), #125
* (feederv4), #126 (wsMgr), and #127 (grpcMgr).
**/
package main

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises utils.Info / utils.Error and provides a small
// buffered backendChan slice so setErrorAndForward (which is called by
// several of the tests below) doesn't block. We deliberately don't call
// the production initChannels() — its channels are unbuffered, and only
// one transport-manager slot is needed for these tests.
func TestMain(m *testing.M) {
	utils.InitLog("vissv2server-dispatch-test.log", os.TempDir(), false, "error")
	backendChan = []chan map[string]interface{}{
		make(chan map[string]interface{}, 4),
	}
	os.Exit(m.Run())
}

// resetErrorResponseMap clears the shared errorResponseMap between
// tests so leftover keys from a previous case don't taint the next
// assertion. (errorResponseMap is a package-level global; the
// production code mutates it in place under the assumption that only
// one request is being processed at a time.)
func resetErrorResponseMap() {
	for k := range errorResponseMap {
		delete(errorResponseMap, k)
	}
}

// TestIsInternalAction covers the predicate that decides whether a
// request bypasses the validation/auth pipeline and goes straight to
// serviceDataChan.
func TestIsInternalAction(t *testing.T) {
	cases := []struct {
		action string
		want   bool
	}{
		{"internal-killsubscriptions", true},
		{"internal-cancelsubscription", true},
		{"get", false},
		{"set", false},
		{"subscribe", false},
		{"unsubscribe", false},
		{"", false},
		// Defensive: must match the exact action string, not a prefix.
		{"internal-killsubscriptions-extra", false},
	}
	for _, tc := range cases {
		if got := isInternalAction(tc.action); got != tc.want {
			t.Errorf("isInternalAction(%q) = %v; want %v", tc.action, got, tc.want)
		}
	}
}

// TestIsUnsubscribeRequest pins the predicate used by serveRequest to
// short-circuit unsubscribe requests to serviceDataChan.
func TestIsUnsubscribeRequest(t *testing.T) {
	cases := []struct {
		action string
		want   bool
	}{
		{"unsubscribe", true},
		{"subscribe", false},
		{"get", false},
		{"set", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isUnsubscribeRequest(tc.action); got != tc.want {
			t.Errorf("isUnsubscribeRequest(%q) = %v; want %v", tc.action, got, tc.want)
		}
	}
}

// TestExpandPathFilter_SinglePath confirms a bare path string gets the
// rootPath prefix and URL-to-dot translation applied.
func TestExpandPathFilter_SinglePath(t *testing.T) {
	got, ok := expandPathFilter("Speed", "Vehicle")
	if !ok {
		t.Fatalf("expandPathFilter returned ok=false for a valid single path")
	}
	want := []string{"Vehicle.Speed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandPathFilter single-path: got %v, want %v", got, want)
	}
}

// TestExpandPathFilter_PathArray confirms a JSON array parameter is
// expanded into a slice of rooted paths.
func TestExpandPathFilter_PathArray(t *testing.T) {
	got, ok := expandPathFilter(`["Speed","Acceleration.Longitudinal"]`, "Vehicle")
	if !ok {
		t.Fatalf("expandPathFilter returned ok=false for a valid JSON array")
	}
	want := []string{"Vehicle.Speed", "Vehicle.Acceleration.Longitudinal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandPathFilter path-array: got %v, want %v", got, want)
	}
}

// TestExpandPathFilter_MalformedJsonReturnsFalse confirms the helper
// signals failure (ok=false) on broken JSON so the caller can emit
// the appropriate bad-request response.
func TestExpandPathFilter_MalformedJsonReturnsFalse(t *testing.T) {
	got, ok := expandPathFilter(`["Speed","Acceleration"`, "Vehicle") // missing closing bracket
	if ok {
		t.Fatalf("expandPathFilter should have returned ok=false on malformed JSON; got %v", got)
	}
	if got != nil {
		t.Fatalf("expandPathFilter should return nil paths on failure; got %v", got)
	}
}

// TestSetErrorAndForward verifies the helper populates
// errorResponseMap with the right error metadata for the given index
// and pushes it onto backendChan[tDChanIndex]. The alt-description
// case also verifies that the override message replaces the default
// for the chosen index.
func TestSetErrorAndForward(t *testing.T) {
	cases := []struct {
		name        string
		errorIndex  int
		altDesc     string // alternate description override; "" means use default
		wantNumber  string
		wantReason  string
		wantDescIs  string // expected exact description; "" means just non-empty
	}{
		{"bad_request", 0, "", "400", "bad_request", ""},
		{"invalid_data", 1, "", "400", "invalid_data", ""},
		{"unavailable_data", 6, "", "404", "unavailable_data", ""},
		{"service_unavailable", 7, "", "503", "service_unavailable", ""},
		{"alt_description", 1, "Forbidden write", "400", "invalid_data", "Forbidden write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetErrorResponseMap()
			// Drain any leftover messages on backendChan[0].
			drainBackendChan()

			req := map[string]interface{}{
				"action":    "get",
				"requestId": "42",
				"RouterId":  "0?0",
			}
			setErrorAndForward(req, 0, tc.errorIndex, tc.altDesc)

			select {
			case got := <-backendChan[0]:
				errMap, ok := got["error"].(map[string]interface{})
				if !ok {
					t.Fatalf("forwarded map missing error sub-map; got %v", got)
				}
				if errMap["number"] != tc.wantNumber {
					t.Errorf("error.number = %v; want %v", errMap["number"], tc.wantNumber)
				}
				if errMap["reason"] != tc.wantReason {
					t.Errorf("error.reason = %v; want %v", errMap["reason"], tc.wantReason)
				}
				desc, _ := errMap["description"].(string)
				if tc.wantDescIs != "" && desc != tc.wantDescIs {
					t.Errorf("error.description = %q; want exactly %q", desc, tc.wantDescIs)
				}
				if tc.wantDescIs == "" && desc == "" {
					t.Errorf("error.description was empty")
				}
				// Forwarded action/requestId/RouterId should come from req.
				if got["action"] != "get" {
					t.Errorf("forwarded action = %v; want get", got["action"])
				}
				if got["requestId"] != "42" {
					t.Errorf("forwarded requestId = %v; want 42", got["requestId"])
				}
				if got["RouterId"] != "0?0" {
					t.Errorf("forwarded RouterId = %v; want 0?0", got["RouterId"])
				}
			case <-time.After(time.Second):
				t.Fatalf("nothing forwarded to backendChan[0]")
			}
		})
	}
}

// drainBackendChan empties backendChan[0] without blocking. Used by
// tests that may have left a message behind from a previous case
// running under t.Run.
func drainBackendChan() {
	for {
		select {
		case <-backendChan[0]:
		default:
			return
		}
	}
}

// TestAuthorizeAccess_NoAuthorizationHeader covers the "auth required
// but missing" branch — returns errorCode 2 immediately.
func TestAuthorizeAccess_NoAuthorizationHeader(t *testing.T) {
	req := map[string]interface{}{
		"action":    "set",
		"requestId": "1",
		// no "authorization" key
	}
	errorCode, handle, gatingId := authorizeAccess(req, "Vehicle.Speed", 1)
	if errorCode != 2 || handle != "" || gatingId != "" {
		t.Fatalf("no-auth-header: got (%d, %q, %q); want (2, \"\", \"\")", errorCode, handle, gatingId)
	}
}

// TestAuthorizeAccess_GetWithWriteOnlyResourceSkipsCheck covers the
// "skip the check entirely" branch. Per the original inline comment
// in issueServiceRequest, validation%10 == 1 means the resource is
// write-only — so a get/subscribe against it doesn't need the auth
// path to run, and the helper short-circuits with errorCode 0.
func TestAuthorizeAccess_GetWithWriteOnlyResourceSkipsCheck(t *testing.T) {
	req := map[string]interface{}{
		"action":        "get",
		"authorization": "some-token",
	}
	errorCode, handle, gatingId := authorizeAccess(req, "Vehicle.Speed", 1) // maxValidation%10 == 1
	if errorCode != 0 || handle != "" || gatingId != "" {
		t.Fatalf("get-against-write-only: got (%d, %q, %q); want (0, \"\", \"\")", errorCode, handle, gatingId)
	}
}

// TestAuthorizeAccess_NonStringAuthorizationFails covers the
// type-assertion branch — when the authorization header is present
// but not a string, the helper returns errorCode 1 without trying to
// hit the ats goroutine.
func TestAuthorizeAccess_NonStringAuthorizationFails(t *testing.T) {
	req := map[string]interface{}{
		"action":        "set", // forces the auth-check path
		"authorization": 42,    // non-string
	}
	errorCode, handle, gatingId := authorizeAccess(req, "Vehicle.Speed", 1)
	if errorCode != 1 || handle != "" || gatingId != "" {
		t.Fatalf("non-string-auth: got (%d, %q, %q); want (1, \"\", \"\")", errorCode, handle, gatingId)
	}
}
