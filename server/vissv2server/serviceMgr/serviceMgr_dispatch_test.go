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
* ServiceMgrInit's dataChan switch.
*
* Originally added in PR #129 covering:
*   - buildServiceResponseMap        shared response-skeleton builder
*   - handleServiceSet               "set" action arm
*   - handleServiceGet               "get" action arm (error paths)
*   - handleServiceUnsubscribe       client-driven "unsubscribe" arm
*   - handleInternalKillSubscriptions kill all subscriptions for a RouterId
*   - handleInternalCancelSubscription AGT-cancelled subscription cleanup
*   - handleUnknownAction            default arm
*
* Follow-up PR A added tests for the subscribe arm, which was left
* inline in #129 with a TODO(testing) note and has now been extracted
* to handleServiceSubscribe.
*
* The Tier-1 bug-fix PR (most recent) closes out four pre-existing
* bugs documented as PRESERVED in #130 / #131:
*   - bug 1: subscribe arm missing break after empty-FilterList
*            error  (test renamed from _BugPreserved to _ReturnsError,
*                    assertions flipped)
*   - bug 2: subscribe arm panicked on malformed JSON path arrays
*            (new TestHandleServiceSubscribe_MalformedPathReturnsError)
*   - bug 3: handleCurveLogNotification indexed past the slice end
*            after remove (new
*            TestHandleCurveLogNotification_CloseSignalRemovesEntryAndReturns)
*   - bug 4: closeClSubId accessed without mcloseClSubId mutex
*            (locking added; -race verifies)
*
* Follow-up PR B extracted the five notification-side arms of
* ServiceMgrInit's select loop and added tests for the four that
* are unit-testable without a live UDS socket:
*   - handleIntervalNotification       <-subscriptionChan
*   - handleCurveLogNotification       <-CLChannel
*   - handleRCTick                     <-subscriptTicker.C
*   - handleFeederNotificationMessage  <-fromFeederRorC
*   (handleFeederRegistration is extracted but not unit-tested -- it
*    calls connectToFeeder which opens a real UDS socket.)
*
* The storage-backend interface (StorageBackend) injection is still
* deferred to a separate PR — it is a genuinely separate architectural
* change touching 5 backends and their init paths.
*
* Same shape as the dispatch tests in PRs #124 (udsMgr+httpMgr), #125
* (feederv4), #126 (wsMgr), #127 (grpcMgr), and #128 (vissv2server core).
**/
package serviceMgr

import (
	"strings"
	"testing"
	"time"
)

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

// ----------------------------------------------------------------------------
// Tests for handleServiceSubscribe (added in the follow-up PR after #129).
//
// The subscribe arm was left inline in PR #129 with a TODO(testing) note.
// These tests cover the extracted handleServiceSubscribe — the clean error
// paths, the happy path with a non-special filter (one that doesn't trip
// subscriptionChan / CLChannel / toFeeder), and a regression-pinning test
// for the pre-existing missing-break bug in the empty-FilterList branch.
// ----------------------------------------------------------------------------

// TestHandleServiceSubscribe_MissingFilterReturnsBadRequest exercises
// the nil-filter short-circuit: no filter field at all on the request,
// so the helper returns immediately with a bad_request error and does
// not touch the subscription list or any side channels.
func TestHandleServiceSubscribe_MissingFilterReturnsBadRequest(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 2)
	subChan := make(chan int, 1)
	clChan := make(chan CLPack, 1)
	toFeederChan := make(chan string, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "subscribe",
		"requestId": "1",
		"path":      "Vehicle.Speed",
		// no filter
	}
	resp := buildServiceResponseMap(req)
	list := []SubscriptionState{}

	updatedList, nextId := handleServiceSubscribe(req, resp, dataChan, list, 99, subChan, clChan, toFeederChan)
	if len(updatedList) != 0 {
		t.Errorf("subscriptionList grew on the error path; len=%d", len(updatedList))
	}
	if nextId != 99 {
		t.Errorf("subscriptionId advanced on the error path; got %d, want 99", nextId)
	}

	select {
	case got := <-dataChan:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response; got %v", got)
		}
		if errMap["reason"] != "bad_request" {
			t.Errorf("error.reason = %v; want bad_request", errMap["reason"])
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceSubscribe did not reply on dataChan")
	}

	// Nothing should reach the side channels on this error path.
	select {
	case <-subChan:
		t.Errorf("subChan was sent to on the error path")
	case <-clChan:
		t.Errorf("clChan was sent to on the error path")
	case <-toFeederChan:
		t.Errorf("toFeederChan was sent to on the error path")
	default:
	}
}

// TestHandleServiceSubscribe_EmptyStringFilterReturnsBadRequest covers
// the same short-circuit when the filter field is present but is the
// empty string (which the original arm explicitly rejected alongside
// nil).
func TestHandleServiceSubscribe_EmptyStringFilterReturnsBadRequest(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 2)
	subChan := make(chan int, 1)
	clChan := make(chan CLPack, 1)
	toFeederChan := make(chan string, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "subscribe",
		"requestId": "1",
		"path":      "Vehicle.Speed",
		"filter":    "",
	}
	resp := buildServiceResponseMap(req)

	updatedList, nextId := handleServiceSubscribe(req, resp, dataChan, []SubscriptionState{}, 7, subChan, clChan, toFeederChan)
	if len(updatedList) != 0 || nextId != 7 {
		t.Errorf("subscription state mutated on the error path; list=%d, id=%d", len(updatedList), nextId)
	}

	select {
	case got := <-dataChan:
		errMap, _ := got["error"].(map[string]interface{})
		if errMap == nil || errMap["reason"] != "bad_request" {
			t.Errorf("expected bad_request error; got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("no response on dataChan")
	}
}

// TestHandleServiceSubscribe_HappyPath exercises the success path
// using a "paths" filter — one that does NOT trip the timebased,
// curvelog, change, or range branches, so the three side channels
// (subChan, clChan, toFeederChan) all stay quiet. We verify:
//   1) the subscription is appended to the list
//   2) subscriptionId is incremented by one
//   3) responseMap["subscriptionId"] is set to the old id (the one the
//      client should use for unsubscribe)
//   4) the response is pushed to dataChan
func TestHandleServiceSubscribe_HappyPath(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 2)
	subChan := make(chan int, 1)
	clChan := make(chan CLPack, 1)
	toFeederChan := make(chan string, 1)
	req := map[string]interface{}{
		"RouterId":  "0?5",
		"action":    "subscribe",
		"requestId": "1",
		"path":      "Vehicle.Speed",
		// A "paths" filter: not timebased / curvelog / range / change,
		// so it triggers none of the side channels.
		"filter": map[string]interface{}{
			"variant":   "paths",
			"parameter": "Vehicle.Speed",
		},
	}
	resp := buildServiceResponseMap(req)
	startId := 42

	updatedList, nextId := handleServiceSubscribe(req, resp, dataChan, []SubscriptionState{}, startId, subChan, clChan, toFeederChan)

	if len(updatedList) != 1 {
		t.Fatalf("subscriptionList len = %d; want 1", len(updatedList))
	}
	if updatedList[0].SubscriptionId != startId {
		t.Errorf("appended SubscriptionId = %d; want %d", updatedList[0].SubscriptionId, startId)
	}
	if updatedList[0].RouterId != "0?5" {
		t.Errorf("appended RouterId = %q; want 0?5", updatedList[0].RouterId)
	}
	if nextId != startId+1 {
		t.Errorf("nextId = %d; want %d (subscriptionId must advance by 1)", nextId, startId+1)
	}

	select {
	case got := <-dataChan:
		if _, isErr := got["error"]; isErr {
			t.Fatalf("happy path produced an error response: %v", got)
		}
		if got["subscriptionId"] != "42" {
			t.Errorf("responseMap.subscriptionId = %v; want \"42\"", got["subscriptionId"])
		}
	case <-time.After(time.Second):
		t.Fatalf("no response on dataChan on happy path")
	}

	// Side channels stay quiet for a "paths" filter.
	select {
	case <-subChan:
		t.Errorf("subChan was sent to on the paths-filter path")
	case <-clChan:
		t.Errorf("clChan was sent to on the paths-filter path")
	case <-toFeederChan:
		t.Errorf("toFeederChan was sent to on the paths-filter path")
	default:
	}
}

// TestHandleServiceSubscribe_EmptyFilterListReturnsError verifies the
// corrected behaviour after fixing bug 1 (the missing `break` /
// `return` after the empty-FilterList error response). The function
// must now send exactly one invalid_data error to dataChan and leave
// the subscription list and subscriptionId untouched.
//
// This test previously pinned the BUG-PRESERVED behaviour where the
// arm fell through and sent a second message. See the Tier-1 bug-fix
// PR for the fix.
func TestHandleServiceSubscribe_EmptyFilterListReturnsError(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 2) // size 2 catches any erroneous second message
	subChan := make(chan int, 1)
	clChan := make(chan CLPack, 1)
	toFeederChan := make(chan string, 1)
	req := map[string]interface{}{
		"RouterId":  "0?5",
		"action":    "subscribe",
		"requestId": "1",
		"path":      "Vehicle.Speed",
		// A non-empty string filter hits the default arm of
		// utils.UnpackFilter (which only branches on []interface{} or
		// map[string]interface{}), leaving the FilterList empty.
		"filter": "this-is-neither-a-map-nor-an-array",
	}
	resp := buildServiceResponseMap(req)
	startId := 100

	updatedList, nextId := handleServiceSubscribe(req, resp, dataChan, []SubscriptionState{}, startId, subChan, clChan, toFeederChan)

	// Exactly one message — the invalid_data error.
	select {
	case got := <-dataChan:
		errMap, _ := got["error"].(map[string]interface{})
		if errMap == nil || errMap["reason"] != "invalid_data" {
			t.Errorf("first message was not invalid_data error: %v", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("no response on dataChan")
	}

	// No second message: the bug-preserving fall-through is gone.
	select {
	case got := <-dataChan:
		t.Errorf("a second message was sent on dataChan; fix has regressed: %v", got)
	default:
	}

	// List and id must be unchanged.
	if len(updatedList) != 0 {
		t.Errorf("subscriptionList len = %d; want 0 (no append after error)", len(updatedList))
	}
	if nextId != startId {
		t.Errorf("nextId = %d; want %d (no increment after error)", nextId, startId)
	}
}

// TestHandleServiceSubscribe_MalformedPathReturnsError exercises the
// bug-2 fix: a path string that looks like a JSON array but isn't
// valid JSON would previously make subscriptionState.Path = nil and
// then panic at Path[0]. Now the helper short-circuits with a
// bad_request response before any Path[0] access.
func TestHandleServiceSubscribe_MalformedPathReturnsError(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 2)
	subChan := make(chan int, 1)
	clChan := make(chan CLPack, 1)
	toFeederChan := make(chan string, 1)
	req := map[string]interface{}{
		"RouterId":  "0?5",
		"action":    "subscribe",
		"requestId": "1",
		"path":      `["Vehicle.Speed`, // opens an array but never closes it
		"filter": map[string]interface{}{
			"variant":   "paths",
			"parameter": "Vehicle.Speed",
		},
	}
	resp := buildServiceResponseMap(req)
	startId := 7

	// Must not panic. The previous code dereferenced Path[0] on a nil
	// slice immediately after unpackPaths.
	updatedList, nextId := handleServiceSubscribe(req, resp, dataChan, []SubscriptionState{}, startId, subChan, clChan, toFeederChan)

	select {
	case got := <-dataChan:
		errMap, _ := got["error"].(map[string]interface{})
		if errMap == nil || errMap["reason"] != "bad_request" {
			t.Errorf("expected bad_request error; got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("no response on dataChan")
	}

	if len(updatedList) != 0 {
		t.Errorf("subscriptionList grew on malformed-path error path; len=%d", len(updatedList))
	}
	if nextId != startId {
		t.Errorf("nextId advanced on the error path; got %d, want %d", nextId, startId)
	}
}

// ----------------------------------------------------------------------------
// Tests for the notification-arm handlers (follow-up PR B to #129):
//
//   - handleIntervalNotification       <-subscriptionChan arm
//   - handleCurveLogNotification       <-CLChannel arm
//   - handleRCTick                     <-subscriptTicker.C arm
//   - handleFeederNotificationMessage  <-fromFeederRorC arm
//
// (handleFeederRegistration is not unit-tested -- it calls
// connectToFeeder which opens a real UDS socket, and mocking that
// requires more plumbing than the value it delivers here.)
// ----------------------------------------------------------------------------

// TestHandleIntervalNotification_UnknownIdLogsAndReturns covers the
// "subscription has been removed but the interval timer fired anyway"
// race: the handler should log and return without touching backendChan.
func TestHandleIntervalNotification_UnknownIdLogsAndReturns(t *testing.T) {
	backChan := make(chan map[string]interface{}, 1)
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 1},
	}
	handleIntervalNotification(9999, subscriptionList, backChan)
	select {
	case got := <-backChan:
		t.Fatalf("backChan should be empty for unknown id; got %v", got)
	default:
	}
}

// TestHandleIntervalNotification_KnownIdSendsToBackend exercises the
// happy path: the subscription is in the list, the handler builds a
// well-shaped subscription event and pushes it to backendChan.
func TestHandleIntervalNotification_KnownIdSendsToBackend(t *testing.T) {
	backChan := make(chan map[string]interface{}, 1)
	subscriptionList := []SubscriptionState{
		{
			SubscriptionId: 7,
			RouterId:       "0?3",
			Path:           []string{"Vehicle.Speed"},
		},
	}
	handleIntervalNotification(7, subscriptionList, backChan)
	select {
	case got := <-backChan:
		if got["action"] != "subscription" {
			t.Errorf("action = %v; want subscription", got["action"])
		}
		if got["subscriptionId"] != "7" {
			t.Errorf("subscriptionId = %v; want \"7\"", got["subscriptionId"])
		}
		if got["RouterId"] != "0?3" {
			t.Errorf("RouterId = %v; want 0?3", got["RouterId"])
		}
		if _, present := got["data"]; !present {
			t.Errorf("data key missing from notification event")
		}
	case <-time.After(time.Second):
		t.Fatalf("handleIntervalNotification did not push to backChan")
	}
}

// TestHandleIntervalNotification_DropOnBackpressure exercises the
// non-blocking select branch: when backendChan is full, the handler
// drops the event rather than block. This pins the original arm's
// drop-on-overflow behaviour.
func TestHandleIntervalNotification_DropOnBackpressure(t *testing.T) {
	backChan := make(chan map[string]interface{}, 1)
	backChan <- map[string]interface{}{"stale": "event"} // fill the buffer
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 7, RouterId: "0?3", Path: []string{"Vehicle.Speed"}},
	}

	done := make(chan struct{})
	go func() {
		handleIntervalNotification(7, subscriptionList, backChan)
		close(done)
	}()
	select {
	case <-done:
		// Good: the handler returned without blocking even though the
		// channel was full.
	case <-time.After(time.Second):
		t.Fatalf("handleIntervalNotification blocked instead of dropping")
	}

	// The buffer still holds the original stale event — the new one
	// was dropped on the floor.
	select {
	case got := <-backChan:
		if got["stale"] != "event" {
			t.Errorf("backChan contained the new event; should have dropped it")
		}
	default:
		t.Fatalf("backChan was unexpectedly drained")
	}
}

// TestHandleCurveLogNotification_UnknownIdResetsCloseSignal covers the
// "subscription is no longer on the list when the close signal
// arrives" path: closeClSubId is cleared and the handler returns
// without touching backendChan.
func TestHandleCurveLogNotification_UnknownIdResetsCloseSignal(t *testing.T) {
	backChan := make(chan map[string]interface{}, 1)
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 1, SubscriptionThreads: 3},
	}
	closeClSubId = 9999 // pretend a close was pending for an id we won't find

	updated := handleCurveLogNotification(CLPack{SubscriptionId: 9999, DataPack: `{"value":"x","ts":"y"}`}, subscriptionList, backChan)

	if closeClSubId != -1 {
		t.Errorf("closeClSubId = %d; want -1 (should be cleared on unknown id)", closeClSubId)
	}
	if len(updated) != 1 {
		t.Errorf("subscriptionList mutated unexpectedly; len=%d, want 1", len(updated))
	}
	select {
	case got := <-backChan:
		t.Fatalf("backChan should be empty on unknown-id path; got %v", got)
	default:
	}
}

// TestHandleCurveLogNotification_DecrementsThreadsAndForwards exercises
// the happy path: the subscription is in the list, this is NOT the
// close signal for it, so the threads counter is decremented and the
// data pack is forwarded to backendChan.
func TestHandleCurveLogNotification_DecrementsThreadsAndForwards(t *testing.T) {
	backChan := make(chan map[string]interface{}, 1)
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 5, RouterId: "0?2", SubscriptionThreads: 3},
	}
	closeClSubId = -1 // no pending close

	updated := handleCurveLogNotification(
		CLPack{SubscriptionId: 5, DataPack: `{"value":"42","ts":"2026-01-01T00:00:00Z"}`},
		subscriptionList, backChan,
	)

	if updated[0].SubscriptionThreads != 2 {
		t.Errorf("SubscriptionThreads = %d; want 2 (decremented from 3)", updated[0].SubscriptionThreads)
	}
	select {
	case got := <-backChan:
		if got["subscriptionId"] != "5" {
			t.Errorf("subscriptionId = %v; want \"5\"", got["subscriptionId"])
		}
		if got["RouterId"] != "0?2" {
			t.Errorf("RouterId = %v; want 0?2", got["RouterId"])
		}
	case <-time.After(time.Second):
		t.Fatalf("handleCurveLogNotification did not push to backChan")
	}
}

// TestHandleCurveLogNotification_CloseSignalRemovesEntryAndReturns
// exercises the bug-3 / bug-4 fix path: when closeClSubId matches and
// SubscriptionThreads decrements to 0, the helper must remove the
// entry AND return without further indexing into the slice (the old
// code accessed subscriptionList[index] after the remove, which
// panics when the removed entry was the last one). Also confirms
// closeClSubId is cleared and that the post-fix run does not send a
// stray notification for the just-removed subscription.
//
// Run with -race to verify the mcloseClSubId mutex pairing for bug 4.
func TestHandleCurveLogNotification_CloseSignalRemovesEntryAndReturns(t *testing.T) {
	backChan := make(chan map[string]interface{}, 1)
	// Two subs in the list. The one at index 1 (last) is the close
	// target — that is the index position where the old swap-with-last
	// + truncate would leave `index` past the end.
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 1, RouterId: "0?1", SubscriptionThreads: 2},
		{SubscriptionId: 5, RouterId: "0?2", SubscriptionThreads: 1},
	}
	closeClSubId = 5 // pending close for subscriptionId 5

	updated := handleCurveLogNotification(
		CLPack{SubscriptionId: 5, DataPack: `{"value":"42","ts":"2026-01-01T00:00:00Z"}`},
		subscriptionList, backChan,
	)

	// The matching subscription should be gone.
	if len(updated) != 1 {
		t.Fatalf("subscriptionList len = %d; want 1 after remove", len(updated))
	}
	if updated[0].SubscriptionId == 5 {
		t.Errorf("subscriptionId 5 still present after close signal")
	}
	// closeClSubId should be cleared.
	if closeClSubId != -1 {
		t.Errorf("closeClSubId = %d; want -1 after close-signal handling", closeClSubId)
	}
	// No notification event for the just-removed entry.
	select {
	case got := <-backChan:
		t.Errorf("backChan should be empty after removing the subscription; got %v", got)
	default:
	}
}

// TestHandleRCTick_FeederActiveStopsTicker covers the path taken when
// the feeder is producing range/change notifications itself: the
// poll-ticker is stopped and the subscription list is returned
// unchanged.
func TestHandleRCTick_FeederActiveStopsTicker(t *testing.T) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop() // double-Stop is safe
	backChan := make(chan map[string]interface{}, 1)
	subscriptionList := []SubscriptionState{
		{SubscriptionId: 1},
	}

	updated := handleRCTick(true, ticker, subscriptionList, backChan)
	if len(updated) != 1 {
		t.Errorf("subscriptionList mutated on feeder-active path; len=%d", len(updated))
	}
	// We can't directly assert "ticker is stopped" — the time.Ticker
	// API doesn't expose state. But the call should have returned
	// promptly and not panicked, which is what matters for behaviour.
	select {
	case got := <-backChan:
		t.Errorf("backChan should be empty on feeder-active path; got %v", got)
	default:
	}
}

// TestHandleRCTick_FeederInactivePollsRC covers the path taken when
// the feeder is NOT providing notifications: the handler delegates to
// checkRCFilterAndIssueMessages, which is a no-op on an empty
// subscription list (so this test just confirms the call returns
// cleanly with the list unchanged).
func TestHandleRCTick_FeederInactivePollsRC(t *testing.T) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	backChan := make(chan map[string]interface{}, 1)
	updated := handleRCTick(false, ticker, []SubscriptionState{}, backChan)
	if updated == nil || len(updated) != 0 {
		t.Errorf("subscriptionList should be returned (possibly unchanged) on feeder-inactive path; got %v", updated)
	}
}

// TestHandleFeederNotificationMessage_SubscribeOkFlipsNotificationOn
// covers the feeder telling us "I support range/change notifications"
// via a subscribe response with status=ok. The handler should flip the
// feederNotification flag to true.
func TestHandleFeederNotificationMessage_SubscribeOkFlipsNotificationOn(t *testing.T) {
	backChan := make(chan map[string]interface{}, 1)
	msg := `{"action":"subscribe","status":"ok"}`
	updatedFlag, _ := handleFeederNotificationMessage(msg, false, []SubscriptionState{}, backChan)
	if updatedFlag != true {
		t.Errorf("feederNotification flag = %v; want true after subscribe ok", updatedFlag)
	}
}

// TestHandleFeederNotificationMessage_MalformedMessagePreservesFlag
// covers the defensive arm of decodeFeederMessage: an unparseable
// message leaves the flag unchanged.
func TestHandleFeederNotificationMessage_MalformedMessagePreservesFlag(t *testing.T) {
	backChan := make(chan map[string]interface{}, 1)
	msg := "not-json-at-all"
	updatedFlag, _ := handleFeederNotificationMessage(msg, true, []SubscriptionState{}, backChan)
	if updatedFlag != true {
		t.Errorf("feederNotification flag = %v; want it to stay true on malformed input", updatedFlag)
	}
}
