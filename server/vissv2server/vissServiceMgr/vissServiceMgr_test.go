package vissServiceMgr

import (
	"testing"
	"time"
)

// Tests for the VISSv3.2 service manager.
// Node-handle functions (SetRootNodePointer, VSSgetType, etc.) require a
// live binary tree and are integration-only; they are exercised via the
// vissv2server integration tests. Unit tests here cover pure logic that
// does not touch the tree.

func init() {
	// Seed the global sessions/states maps to a known-empty state.
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}
}

// ---- helpers ---------------------------------------------------------------

func makeChan() chan map[string]interface{} {
	return make(chan map[string]interface{}, 8)
}

// ---- generateServiceId -----------------------------------------------------

func TestGenerateServiceId_Length(t *testing.T) {
	id := generateServiceId()
	if len(id) == 0 {
		t.Fatal("generateServiceId returned empty string")
	}
}

func TestGenerateServiceId_Unique(t *testing.T) {
	ids := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		ids[generateServiceId()] = struct{}{}
	}
	if len(ids) < 900 { // tolerate a small collision rate
		t.Fatalf("generateServiceId produced too many collisions: %d unique from 1000", len(ids))
	}
}

// ---- getTimestamp ----------------------------------------------------------

func TestGetTimestamp_NonEmpty(t *testing.T) {
	ts := getTimestamp()
	if len(ts) == 0 {
		t.Fatal("getTimestamp returned empty string")
	}
}

func TestGetTimestamp_RFC3339(t *testing.T) {
	ts := getTimestamp()
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Fatalf("getTimestamp %q is not RFC3339: %v", ts, err)
	}
}

// ---- getState --------------------------------------------------------------

func TestGetState_CreatesUnknown(t *testing.T) {
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}

	s := getState("Test.Proc")
	if s == nil {
		t.Fatal("getState returned nil")
	}
	if s.status != StatusUnknown {
		t.Fatalf("want UNKNOWN, got %q", s.status)
	}
}

func TestGetState_Idempotent(t *testing.T) {
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}

	s1 := getState("Test.Proc2")
	s1.status = StatusOngoing
	s2 := getState("Test.Proc2")
	if s2.status != StatusOngoing {
		t.Fatalf("second getState should return same object; got %q", s2.status)
	}
}

// ---- extractFilterVariant --------------------------------------------------

func TestExtractFilterVariant_NilIsAll(t *testing.T) {
	if v := extractFilterVariant(nil); v != "all" {
		t.Fatalf("want all, got %q", v)
	}
}

func TestExtractFilterVariant_MapVariant(t *testing.T) {
	f := map[string]interface{}{"variant": "timebased"}
	if v := extractFilterVariant(f); v != "timebased" {
		t.Fatalf("want timebased, got %q", v)
	}
}

func TestExtractFilterVariant_StringJSON(t *testing.T) {
	f := `{"variant":"status"}`
	if v := extractFilterVariant(f); v != "status" {
		t.Fatalf("want status, got %q", v)
	}
}

func TestExtractFilterVariant_None(t *testing.T) {
	f := map[string]interface{}{"variant": "none"}
	if v := extractFilterVariant(f); v != "none" {
		t.Fatalf("want none, got %q", v)
	}
}

func TestExtractFilterVariant_MissingVariantKey(t *testing.T) {
	f := map[string]interface{}{"parameter": "x"}
	if v := extractFilterVariant(f); v != "all" {
		t.Fatalf("want all (fallback), got %q", v)
	}
}

// ---- extractRouterIndex ----------------------------------------------------

func TestExtractRouterIndex_Present(t *testing.T) {
	m := map[string]interface{}{"routerIndex": 3}
	if i := extractRouterIndex(m); i != 3 {
		t.Fatalf("want 3, got %d", i)
	}
}

func TestExtractRouterIndex_Missing(t *testing.T) {
	m := map[string]interface{}{}
	if i := extractRouterIndex(m); i != 0 {
		t.Fatalf("want 0 (default), got %d", i)
	}
}

// ---- copyRouteFields -------------------------------------------------------

func TestCopyRouteFields_CopiesKnownKeys(t *testing.T) {
	src := map[string]interface{}{
		"RouterId":    "1?abc",
		"routerIndex": 2,
		"otherKey":   "shouldNotCopy",
	}
	dst := map[string]interface{}{}
	copyRouteFields(src, dst)
	if dst["RouterId"] != "1?abc" {
		t.Error("RouterId not copied")
	}
	if dst["routerIndex"] != 2 {
		t.Error("routerIndex not copied")
	}
	if _, ok := dst["otherKey"]; ok {
		t.Error("otherKey should not have been copied")
	}
}

// ---- copyMap ---------------------------------------------------------------

func TestCopyMap_Nil(t *testing.T) {
	if copyMap(nil) != nil {
		t.Error("copyMap(nil) should return nil")
	}
}

func TestCopyMap_ShallowCopy(t *testing.T) {
	src := map[string]interface{}{"a": "1", "b": "2"}
	dst := copyMap(src)
	if dst["a"] != "1" || dst["b"] != "2" {
		t.Error("values not copied correctly")
	}
	// Mutation of dst must not affect src.
	dst["a"] = "changed"
	if src["a"] != "1" {
		t.Error("copyMap is not a shallow copy — src was mutated")
	}
}

// ---- isServiceAction (server-level predicate, tested here for parity) ------

func TestIsServiceAction(t *testing.T) {
	for _, a := range []string{"invoke", "monitor", "cancel", "discover"} {
		if !isServiceActionStr(a) {
			t.Errorf("expected %q to be a service action", a)
		}
	}
	for _, a := range []string{"get", "set", "subscribe", "unsubscribe", ""} {
		if isServiceActionStr(a) {
			t.Errorf("expected %q NOT to be a service action", a)
		}
	}
}

// isServiceActionStr mirrors the server-level predicate so we can test it
// without importing the vissv2server package (avoid circular imports).
func isServiceActionStr(action string) bool {
	switch action {
	case "invoke", "monitor", "cancel", "discover":
		return true
	}
	return false
}

// ---- sendServiceError ------------------------------------------------------

func TestSendServiceError_HasRequiredFields(t *testing.T) {
	ch := makeChan()
	sendServiceError(ch, "invoke", "req-1", "", StatusFailed,
		"400", "bad_request", "test error")
	select {
	case m := <-ch:
		if m["action"] != "invoke" {
			t.Errorf("wrong action: %v", m["action"])
		}
		if m["status"] != "FAILED" {
			t.Errorf("wrong status: %v", m["status"])
		}
		errObj, ok := m["error"].(map[string]interface{})
		if !ok {
			t.Fatal("error field missing or wrong type")
		}
		if errObj["number"] != "400" {
			t.Errorf("wrong error number: %v", errObj["number"])
		}
		if m["requestId"] != "req-1" {
			t.Errorf("wrong requestId: %v", m["requestId"])
		}
		if _, ok := m["ts"]; !ok {
			t.Error("ts field missing")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for error response")
	}
}

func TestSendServiceError_NoRequestIdWhenEmpty(t *testing.T) {
	ch := makeChan()
	sendServiceError(ch, "cancel", "", "svc-42", StatusFailed,
		"400", "bad_request", "not found")
	select {
	case m := <-ch:
		if _, ok := m["requestId"]; ok {
			t.Error("requestId should be absent when empty")
		}
		if m["serviceId"] != "svc-42" {
			t.Errorf("wrong serviceId: %v", m["serviceId"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// ---- HandleCancel (pure-state paths) ---------------------------------------

func TestHandleCancel_MissingServiceId(t *testing.T) {
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}
	ch := makeChan()

	HandleCancel(map[string]interface{}{}, ch)

	select {
	case m := <-ch:
		errObj, ok := m["error"].(map[string]interface{})
		if !ok {
			t.Fatal("expected error response")
		}
		if errObj["reason"] != "bad_request" {
			t.Errorf("wrong reason: %v", errObj["reason"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestHandleCancel_UnknownServiceId(t *testing.T) {
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}
	ch := makeChan()

	HandleCancel(map[string]interface{}{"serviceId": "no-such-id"}, ch)

	select {
	case m := <-ch:
		if _, ok := m["error"]; !ok {
			t.Error("expected error response for unknown serviceId")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestHandleCancel_InvokeSession_CancelsService(t *testing.T) {
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}
	ch := makeChan()

	// Pre-seed an invoke session.
	sessions["sid-1"] = &serviceSession{
		serviceId: "sid-1",
		path:      "Svc.Proc",
		isInvoke:  true,
		status:    StatusOngoing,
	}
	states["Svc.Proc"] = &serviceState{status: StatusOngoing}

	HandleCancel(map[string]interface{}{"serviceId": "sid-1"}, ch)

	select {
	case m := <-ch:
		if m["status"] != "CANCELED" {
			t.Errorf("want CANCELED, got %v", m["status"])
		}
		if m["serviceId"] != "sid-1" {
			t.Errorf("wrong serviceId: %v", m["serviceId"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	// Session must be removed.
	if _, ok := sessions["sid-1"]; ok {
		t.Error("session should have been removed after cancel")
	}
	// Service state must be CANCELED.
	if states["Svc.Proc"].status != StatusCanceled {
		t.Errorf("service state should be CANCELED, got %v", states["Svc.Proc"].status)
	}
}

func TestHandleCancel_MonitorSession_ServiceUnaffected(t *testing.T) {
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}
	ch := makeChan()

	sessions["sid-2"] = &serviceSession{
		serviceId: "sid-2",
		path:      "Svc.Proc2",
		isInvoke:  false, // monitor session
		status:    StatusOngoing,
	}
	states["Svc.Proc2"] = &serviceState{status: StatusOngoing}

	HandleCancel(map[string]interface{}{"serviceId": "sid-2"}, ch)

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	// Monitor cancel must NOT change service state.
	if states["Svc.Proc2"].status != StatusOngoing {
		t.Errorf("monitor cancel should not change service state; got %v", states["Svc.Proc2"].status)
	}
}

// ---- UpdateServiceState ----------------------------------------------------

func TestUpdateServiceState_FansOutToSessions(t *testing.T) {
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}

	ch := makeChan()
	backendChans := []chan map[string]interface{}{ch}

	sessions["s1"] = &serviceSession{
		serviceId:   "s1",
		path:        "Svc.Fan",
		routerIndex: 0,
		status:      StatusOngoing,
	}
	states["Svc.Fan"] = &serviceState{status: StatusOngoing}

	outdata := map[string]interface{}{"Position": "42"}
	UpdateServiceState("Svc.Fan", StatusSuccessful, outdata, backendChans)

	select {
	case event := <-ch:
		if event["action"] != "monitoring" {
			t.Errorf("want 'monitoring' action, got %v", event["action"])
		}
		if event["status"] != "SUCCESSFUL" {
			t.Errorf("want SUCCESSFUL, got %v", event["status"])
		}
		if event["serviceId"] != "s1" {
			t.Errorf("wrong serviceId: %v", event["serviceId"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fan-out event")
	}

	// Session removed on terminal status.
	if _, ok := sessions["s1"]; ok {
		t.Error("session should be removed after terminal status")
	}
	// State updated.
	if states["Svc.Fan"].status != StatusSuccessful {
		t.Errorf("state not updated; got %v", states["Svc.Fan"].status)
	}
}

func TestUpdateServiceState_OngoingKeepsSessions(t *testing.T) {
	sessions = map[string]*serviceSession{}
	states = map[string]*serviceState{}

	ch := makeChan()
	backendChans := []chan map[string]interface{}{ch}

	sessions["s2"] = &serviceSession{
		serviceId:   "s2",
		path:        "Svc.Keep",
		routerIndex: 0,
		status:      StatusOngoing,
	}
	states["Svc.Keep"] = &serviceState{status: StatusOngoing}

	UpdateServiceState("Svc.Keep", StatusOngoing, nil, backendChans)

	<-ch // consume the event

	// Session must remain for further updates.
	if _, ok := sessions["s2"]; !ok {
		t.Error("session should remain while status is ONGOING")
	}
}
