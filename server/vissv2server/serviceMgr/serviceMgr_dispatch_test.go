/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the per-message dispatch helpers extracted from
* ServiceMgrInit's dataChan switch in PR #129 (this PR):
*
*   - buildServiceResponseMap        shared response-skeleton builder
*   - handleServiceSet               "set" action arm
*   - handleServiceGet               "get" action arm (error paths)
*   - handleServiceUnsubscribe       client-driven "unsubscribe" arm
*   - handleInternalKillSubscriptions kill all subscriptions for a RouterId
*   - handleInternalCancelSubscription AGT-cancelled subscription cleanup
*   - handleUnknownAction            default arm
*
* The subscribe arm is intentionally left inline in this PR — it mutates
* several locals (subscriptionList, subscriptionId) and sends on three
* channels (subscriptionChan, CLChannel, toFeeder), so extracting it
* cleanly will be a follow-up. The storage-backend interface
* (StorageBackend) injection is also deferred to a future PR — it is a
* genuinely separate architectural change touching 5 backends and their
* init paths, and bundling it here would balloon this diff.
*
* Same shape as the dispatch tests in PRs #124 (udsMgr+httpMgr), #125
* (feederv4), #126 (wsMgr), #127 (grpcMgr), and #128 (vissv2server core).
**/
package serviceMgr

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises utils.Info / utils.Error so the helpers can
// log without nil-deref under test conditions. We deliberately do
// NOT set stateDbType here — the empty value falls through to the
// default arm in setVehicleData/getVehicleData, returning "" without
// touching the live storage backends. That keeps these tests
// self-contained and exercises the service_unavailable / empty-data
// paths.
func TestMain(m *testing.M) {
	utils.InitLog("serviceMgr-dispatch-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// resetErrorResponseMap clears the shared package-level
// errorResponseMap between tests. The production code mutates it in
// place on the assumption that one request is being processed at a
// time, so we restore that assumption per test case.
func resetErrorResponseMap() {
	for k := range errorResponseMap {
		delete(errorResponseMap, k)
	}
}

// TestBuildServiceResponseMap verifies the response skeleton carries
// the four required fields and that the authorization field is only
// set when "handle" is present on the request.
func TestBuildServiceResponseMap(t *testing.T) {
	req := map[string]interface{}{
		"RouterId":  "0?7",
		"action":    "get",
		"requestId": "42",
		"handle":    "tok-handle-xyz",
	}
	resp := buildServiceResponseMap(req)
	if resp["RouterId"] != "0?7" {
		t.Errorf("RouterId = %v; want 0?7", resp["RouterId"])
	}
	if resp["action"] != "get" {
		t.Errorf("action = %v; want get", resp["action"])
	}
	if resp["requestId"] != "42" {
		t.Errorf("requestId = %v; want 42", resp["requestId"])
	}
	if resp["ts"] == nil || resp["ts"].(string) == "" {
		t.Errorf("ts not populated")
	}
	if resp["authorization"] != "tok-handle-xyz" {
		t.Errorf("authorization = %v; want tok-handle-xyz", resp["authorization"])
	}

	// Without a handle, the authorization field must be absent.
	req2 := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "get",
		"requestId": "1",
	}
	resp2 := buildServiceResponseMap(req2)
	if _, present := resp2["authorization"]; present {
		t.Errorf("authorization should be absent when handle is nil; got %v", resp2["authorization"])
	}
}

// TestHandleServiceSet_StorageUnavailableReturnsError exercises the
// service_unavailable path: with stateDbType unset, setVehicleData
// returns the zero string and the helper must emit the canonical
// error response on dataChan.
func TestHandleServiceSet_StorageUnavailableReturnsError(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "set",
		"requestId": "1",
		"path":      "Vehicle.Speed",
		"value":     "100",
	}
	resp := buildServiceResponseMap(req)

	go handleServiceSet(req, resp, dataChan)

	select {
	case got := <-dataChan:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response; got %v", got)
		}
		if errMap["reason"] != "service_unavailable" {
			t.Errorf("error.reason = %v; want service_unavailable", errMap["reason"])
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceSet did not reply on dataChan")
	}
}

// TestHandleServiceGet_MalformedPathArray covers the invalid_data
// branch when unpackPaths can't decode a "looks like an array but
// isn't" path string.
func TestHandleServiceGet_MalformedPathArray(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "get",
		"requestId": "1",
		"path":      `["Vehicle.Speed`, // open bracket but no close
	}
	resp := buildServiceResponseMap(req)

	go handleServiceGet(req, resp, dataChan)

	select {
	case got := <-dataChan:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response; got %v", got)
		}
		if errMap["reason"] != "invalid_data" {
			t.Errorf("error.reason = %v; want invalid_data", errMap["reason"])
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceGet did not reply on dataChan")
	}
}

// TestHandleServiceGet_SinglePathSucceeds confirms the happy path
// resolves a single path without hitting the error arms. With
// stateDbType unset, getVehicleData returns "" and the data pack ends
// up with a nil "dp" value, but the helper still emits a success
// response (not an error response). This pins the contract that the
// "no data available" case is NOT treated as an error here — the
// caller has to interpret a missing dp themselves.
func TestHandleServiceGet_SinglePathSucceeds(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "get",
		"requestId": "1",
		"path":      "Vehicle.Speed", // single path, not an array
	}
	resp := buildServiceResponseMap(req)

	go handleServiceGet(req, resp, dataChan)

	select {
	case got := <-dataChan:
		if _, isErr := got["error"]; isErr {
			t.Fatalf("expected success response; got error %v", got)
		}
		if got["action"] != "get" {
			t.Errorf("action = %v; want get", got["action"])
		}
		// "data" key should be present even when empty.
		if _, present := got["data"]; !present {
			t.Errorf("data key missing from success response")
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceGet did not reply on dataChan")
	}
}

// TestHandleServiceUnsubscribe_MissingSubscriptionId hits the
// invalid_data branch when the request has no subscriptionId field.
func TestHandleServiceUnsubscribe_MissingSubscriptionId(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	toFeederChan := make(chan string, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "unsubscribe",
		"requestId": "1",
		// no subscriptionId
	}
	resp := buildServiceResponseMap(req)
	subscriptionList := []SubscriptionState{}

	updated := handleServiceUnsubscribe(req, resp, dataChan, subscriptionList, toFeederChan)
	if len(updated) != 0 {
		t.Errorf("subscriptionList length = %d; want 0", len(updated))
	}

	select {
	case got := <-dataChan:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response; got %v", got)
		}
		if errMap["reason"] != "invalid_data" {
			t.Errorf("error.reason = %v; want invalid_data", errMap["reason"])
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceUnsubscribe did not reply on dataChan")
	}

	// toFeeder should NOT have been sent to on the error path.
	select {
	case got := <-toFeederChan:
		t.Errorf("toFeederChan should be empty on error path; got %q", got)
	default:
	}
}

// TestHandleServiceUnsubscribe_NotFoundOnList covers the case where
// the subscriptionId is well-formed but no matching subscription is
// in the list — deactivateSubscription returns status=-1 and the
// helper falls through to the invalid_data error.
func TestHandleServiceUnsubscribe_NotFoundOnList(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	toFeederChan := make(chan string, 1)
	req := map[string]interface{}{
		"RouterId":       "0?0",
		"action":         "unsubscribe",
		"requestId":      "1",
		"subscriptionId": "9999",
	}
	resp := buildServiceResponseMap(req)
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 1, RouterId: "0?0"},
	}

	updated := handleServiceUnsubscribe(req, resp, dataChan, subscriptionList, toFeederChan)
	if len(updated) != 1 {
		t.Errorf("subscriptionList length = %d; want 1 (unchanged)", len(updated))
	}

	select {
	case got := <-dataChan:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response; got %v", got)
		}
		if errMap["reason"] != "invalid_data" {
			t.Errorf("error.reason = %v; want invalid_data", errMap["reason"])
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceUnsubscribe did not reply on dataChan")
	}
}

// TestHandleInternalKillSubscriptions_RemovesMatching seeds a
// subscription list with two entries for the same RouterId and one
// for a different RouterId, then verifies the kill loop removes both
// of the matching entries while leaving the unrelated one alone.
func TestHandleInternalKillSubscriptions_RemovesMatching(t *testing.T) {
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 1, RouterId: "0?5"},
		{SubscriptionId: 2, RouterId: "0?5"},
		{SubscriptionId: 3, RouterId: "0?9"},
	}
	req := map[string]interface{}{
		"action":   "internal-killsubscriptions",
		"RouterId": "0?5",
	}

	updated := handleInternalKillSubscriptions(req, subscriptionList)

	// The two 0?5 entries should be gone; 0?9 should survive.
	for _, s := range updated {
		if s.RouterId == "0?5" {
			t.Errorf("found leftover RouterId=0?5 entry: subId=%d", s.SubscriptionId)
		}
	}
	foundOther := false
	for _, s := range updated {
		if s.RouterId == "0?9" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Errorf("RouterId=0?9 entry was removed; should have survived")
	}
}

// TestHandleInternalKillSubscriptions_NoMatch confirms that when no
// entries match the request RouterId, the list is returned unchanged.
func TestHandleInternalKillSubscriptions_NoMatch(t *testing.T) {
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 1, RouterId: "0?9"},
	}
	req := map[string]interface{}{
		"action":   "internal-killsubscriptions",
		"RouterId": "0?5",
	}
	updated := handleInternalKillSubscriptions(req, subscriptionList)
	if len(updated) != 1 || updated[0].RouterId != "0?9" {
		t.Errorf("list mutated unexpectedly; got %v", updated)
	}
}

// TestHandleInternalCancelSubscription_GatingIdFound seeds a
// subscription with a matching gatingId, then confirms the helper:
//   1) rewrites requestMap into a synthetic "subscription" error event
//   2) pushes it to dataChan with the expired_token error number
//   3) removes the subscription from the list
func TestHandleInternalCancelSubscription_GatingIdFound(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 7, RouterId: "0?3", GatingId: "gate-abc"},
	}
	req := map[string]interface{}{
		"action":   "internal-cancelsubscription",
		"gatingId": "gate-abc",
	}

	updated := handleInternalCancelSubscription(req, dataChan, subscriptionList)
	if len(updated) != 0 {
		t.Errorf("subscriptionList length = %d; want 0 (entry should have been removed)", len(updated))
	}

	select {
	case got := <-dataChan:
		if got["action"] != "subscription" {
			t.Errorf("rewritten action = %v; want subscription", got["action"])
		}
		if got["RouterId"] != "0?3" {
			t.Errorf("rewritten RouterId = %v; want 0?3", got["RouterId"])
		}
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error sub-map; got %v", got)
		}
		if desc, _ := errMap["description"].(string); !strings.Contains(desc, "Token expired") {
			t.Errorf("description = %q; want it to mention Token expired", desc)
		}
	case <-time.After(time.Second):
		t.Fatalf("handleInternalCancelSubscription did not reply on dataChan")
	}
}

// TestHandleInternalCancelSubscription_GatingIdNotFound confirms a
// non-matching gatingId is a no-op: the list is unchanged and nothing
// is pushed onto dataChan.
func TestHandleInternalCancelSubscription_GatingIdNotFound(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 7, RouterId: "0?3", GatingId: "gate-abc"},
	}
	req := map[string]interface{}{
		"action":   "internal-cancelsubscription",
		"gatingId": "gate-not-here",
	}

	updated := handleInternalCancelSubscription(req, dataChan, subscriptionList)
	if len(updated) != 1 {
		t.Errorf("subscriptionList length = %d; want 1 (unchanged)", len(updated))
	}

	select {
	case got := <-dataChan:
		t.Errorf("dataChan should be empty when gatingId not found; got %v", got)
	default:
	}
}

// TestHandleUnknownAction confirms the default arm emits the
// invalid_data response with "Unknown action" as the description.
func TestHandleUnknownAction(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "completely-made-up-action",
		"requestId": "1",
	}

	handleUnknownAction(req, dataChan)

	select {
	case got := <-dataChan:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response; got %v", got)
		}
		if errMap["reason"] != "invalid_data" {
			t.Errorf("error.reason = %v; want invalid_data", errMap["reason"])
		}
		if desc, _ := errMap["description"].(string); desc != "Unknown action" {
			t.Errorf("description = %q; want exactly \"Unknown action\"", desc)
		}
	case <-time.After(time.Second):
		t.Fatalf("handleUnknownAction did not reply on dataChan")
	}
}
