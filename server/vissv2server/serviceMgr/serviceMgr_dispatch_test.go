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
* Follow-up PR (this PR) adds tests for the subscribe arm, which was
* left inline in #129 with a TODO(testing) note and has now been
* extracted to handleServiceSubscribe. The storage-backend interface
* (StorageBackend) injection is still deferred to a separate PR — it
* is a genuinely separate architectural change touching 5 backends
* and their init paths.
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

// TestHandleServiceSubscribe_EmptyFilterListBugPreserved is a
// regression-pinning test for a PRE-EXISTING bug in the original
// inline arm that handleServiceSubscribe preserves verbatim:
//
//   When UnpackFilter unpacks a non-nil filter into an EMPTY
//   FilterList, the arm sends a "invalid_data" error response on
//   dataChan AND THEN falls through to append the empty-filter
//   subscription, sending a SECOND response on dataChan and
//   incrementing subscriptionId. The arm was missing a `break` after
//   the error send.
//
// If a future fix adds the missing break, THIS TEST WILL FAIL — that
// is intentional. Update the test to reflect the corrected behaviour
// (single error response, list unchanged, id unchanged) at that time.
func TestHandleServiceSubscribe_EmptyFilterListBugPreserved(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 2) // room for both messages
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
		// map[string]interface{}), leaving the FilterList nil — i.e.
		// len() == 0 — and tripping the buggy empty-FilterList path.
		"filter": "this-is-neither-a-map-nor-an-array",
	}
	resp := buildServiceResponseMap(req)
	startId := 100

	updatedList, nextId := handleServiceSubscribe(req, resp, dataChan, []SubscriptionState{}, startId, subChan, clChan, toFeederChan)

	// First message on dataChan should be the invalid_data error.
	select {
	case got := <-dataChan:
		errMap, _ := got["error"].(map[string]interface{})
		if errMap == nil || errMap["reason"] != "invalid_data" {
			t.Errorf("first message was not invalid_data error: %v", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("no first response on dataChan")
	}

	// Second message exists due to the missing-break bug — the arm
	// keeps going and sends responseMap as if the subscription was
	// added. If this select hits the timeout instead, the bug has
	// been fixed; update the test to the corrected behaviour.
	select {
	case got := <-dataChan:
		if got["subscriptionId"] != "100" {
			t.Errorf("second (buggy) message did not carry the new subscriptionId: %v", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("missing second message — bug may have been fixed; update this test to the corrected behaviour")
	}

	// And the list / id reflect the buggy append.
	if len(updatedList) != 1 {
		t.Errorf("subscriptionList len = %d; bug-preserved behaviour appends 1", len(updatedList))
	}
	if nextId != startId+1 {
		t.Errorf("nextId = %d; bug-preserved behaviour increments to %d", nextId, startId+1)
	}
}
