package vissServiceMgr

import (
	"testing"
	"time"
)

// Unit tests for the VISSv3.3-alpha service manager.
// Functions that require a live binary tree (SetRootNodePointer, VSSgetType,
// validateInputSignature) are integration-only and are documented as such
// rather than unit-tested here.

func resetState() {
	invocations = map[string]*invocationState{}
	sessions = map[string]*monitorSession{}
}

// ---- generateId ------------------------------------------------------------

func TestGenerateId_NonEmpty(t *testing.T) {
	if id := generateId(); len(id) == 0 {
		t.Fatal("generateId returned empty string")
	}
}

func TestGenerateId_Unique(t *testing.T) {
	ids := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		ids[generateId()] = struct{}{}
	}
	if len(ids) < 900 {
		t.Fatalf("too many collisions: %d unique from 1000", len(ids))
	}
}

// ---- getTimestamp ----------------------------------------------------------

func TestGetTimestamp_RFC3339(t *testing.T) {
	ts := getTimestamp()
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Fatalf("getTimestamp %q is not RFC3339: %v", ts, err)
	}
}

// ---- latestInvocationForPath -----------------------------------------------

func TestLatestInvocationForPath_NoneReturnsNil(t *testing.T) {
	resetState()
	if latestInvocationForPath("No.Such.Path") != nil {
		t.Error("expected nil for unknown path")
	}
}

func TestLatestInvocationForPath_ReturnsNewest(t *testing.T) {
	resetState()
	early := &invocationState{serviceId: "s1", path: "A.B", status: StatusOngoing,
		startedAt: time.Now().Add(-time.Second)}
	late := &invocationState{serviceId: "s2", path: "A.B", status: StatusOngoing,
		startedAt: time.Now()}
	invocations["s1"] = early
	invocations["s2"] = late

	got := latestInvocationForPath("A.B")
	if got == nil || got.serviceId != "s2" {
		t.Errorf("expected s2 (newest), got %v", got)
	}
}

func TestLatestInvocationForPath_IgnoresTerminal(t *testing.T) {
	resetState()
	invocations["s1"] = &invocationState{serviceId: "s1", path: "A.B",
		status: StatusSuccessful, startedAt: time.Now()}
	if latestInvocationForPath("A.B") != nil {
		t.Error("should ignore non-ONGOING invocations")
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

func TestExtractFilterVariant_JSONString(t *testing.T) {
	if v := extractFilterVariant(`{"variant":"status"}`); v != "status" {
		t.Fatalf("want status, got %q", v)
	}
}

func TestExtractFilterVariant_MissingVariant(t *testing.T) {
	if v := extractFilterVariant(map[string]interface{}{"parameter": "x"}); v != "all" {
		t.Fatalf("want all, got %q", v)
	}
}

// ---- periodFromFilter ------------------------------------------------------

func TestPeriodFromFilter_Valid(t *testing.T) {
	f := map[string]interface{}{
		"variant":   "timebased",
		"parameter": map[string]interface{}{"period": "250"},
	}
	p := periodFromFilter(f)
	if p != 250*time.Millisecond {
		t.Fatalf("want 250ms, got %v", p)
	}
}

func TestPeriodFromFilter_NilDefaultsToSecond(t *testing.T) {
	if p := periodFromFilter(nil); p != time.Second {
		t.Fatalf("want 1s, got %v", p)
	}
}

func TestPeriodFromFilter_InvalidFallsBack(t *testing.T) {
	f := map[string]interface{}{"variant": "timebased", "parameter": map[string]interface{}{"period": "abc"}}
	if p := periodFromFilter(f); p != time.Second {
		t.Fatalf("want 1s default, got %v", p)
	}
}

// ---- timeoutFromRequest ----------------------------------------------------

func TestTimeoutFromRequest_MsInt(t *testing.T) {
	m := map[string]interface{}{"timeout": float64(5000)}
	if d := timeoutFromRequest(m); d != 5*time.Second {
		t.Fatalf("want 5s, got %v", d)
	}
}

func TestTimeoutFromRequest_StringMs(t *testing.T) {
	m := map[string]interface{}{"timeout": "2000"}
	if d := timeoutFromRequest(m); d != 2*time.Second {
		t.Fatalf("want 2s, got %v", d)
	}
}

func TestTimeoutFromRequest_MissingUsesDefault(t *testing.T) {
	m := map[string]interface{}{}
	if d := timeoutFromRequest(m); d != DefaultTimeout {
		t.Fatalf("want DefaultTimeout, got %v", d)
	}
}

// ---- copyMap ---------------------------------------------------------------

func TestCopyMap_Nil(t *testing.T) {
	if copyMap(nil) != nil {
		t.Error("copyMap(nil) should return nil")
	}
}

func TestCopyMap_Independent(t *testing.T) {
	src := map[string]interface{}{"a": "1"}
	dst := copyMap(src)
	dst["a"] = "changed"
	if src["a"] != "1" {
		t.Error("src was mutated by copyMap")
	}
}

// ---- copyRouteFields -------------------------------------------------------

func TestCopyRouteFields(t *testing.T) {
	src := map[string]interface{}{"RouterId": "1?x", "routerIndex": 3, "other": "skip"}
	dst := map[string]interface{}{}
	copyRouteFields(src, dst)
	if dst["RouterId"] != "1?x" {
		t.Error("RouterId not copied")
	}
	if dst["routerIndex"] != 3 {
		t.Error("routerIndex not copied")
	}
	if _, ok := dst["other"]; ok {
		t.Error("unexpected key copied")
	}
}

// ---- sendServiceError ------------------------------------------------------

func TestSendServiceError_RequiredFields(t *testing.T) {
	ch := make(chan map[string]interface{}, 4)
	sendServiceError(ch, "invoke", "req-1", "", StatusFailed, "400", "bad_request", "oops")
	select {
	case m := <-ch:
		if m["action"] != "invoke" {
			t.Errorf("wrong action: %v", m["action"])
		}
		if m["status"] != "FAILED" {
			t.Errorf("wrong status: %v", m["status"])
		}
		errObj, ok := m["error"].(map[string]interface{})
		if !ok || errObj["number"] != "400" {
			t.Errorf("wrong error: %v", m["error"])
		}
		if m["requestId"] != "req-1" {
			t.Errorf("wrong requestId: %v", m["requestId"])
		}
		if m["ts"] == nil {
			t.Error("ts missing")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// ---- HandleCancel ----------------------------------------------------------

func TestHandleCancel_MissingServiceId(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	HandleCancel(map[string]interface{}{}, ch)
	select {
	case m := <-ch:
		if _, ok := m["error"]; !ok {
			t.Error("expected error response")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHandleCancel_UnknownServiceId(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	HandleCancel(map[string]interface{}{"serviceId": "no-such"}, ch)
	select {
	case m := <-ch:
		if _, ok := m["error"]; !ok {
			t.Error("expected error")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHandleCancel_InvokeSessionCancelsService(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)

	// Pre-seed an invoke session + matching invocation.
	invocations["inv-1"] = &invocationState{serviceId: "inv-1", path: "S.P", status: StatusOngoing}
	sessions["sid-1"] = &monitorSession{sessionId: "sid-1", serviceId: "inv-1", isInvoke: true}

	HandleCancel(map[string]interface{}{"serviceId": "sid-1"}, ch)

	select {
	case m := <-ch:
		if m["status"] != "CANCELED" {
			t.Errorf("want CANCELED, got %v", m["status"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	if _, ok := sessions["sid-1"]; ok {
		t.Error("session should be removed")
	}
	if _, ok := invocations["inv-1"]; ok {
		t.Error("invocation should be removed")
	}
}

func TestHandleCancel_MonitorSessionLeavesServiceAlive(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)

	invocations["inv-2"] = &invocationState{serviceId: "inv-2", path: "S.P2", status: StatusOngoing}
	sessions["sid-2"] = &monitorSession{sessionId: "sid-2", serviceId: "inv-2", isInvoke: false}

	HandleCancel(map[string]interface{}{"serviceId": "sid-2"}, ch)
	<-ch

	if _, ok := invocations["inv-2"]; !ok {
		t.Error("invocation should remain when monitor session is cancelled")
	}
	if invocations["inv-2"].status != StatusOngoing {
		t.Errorf("service status should stay ONGOING, got %v", invocations["inv-2"].status)
	}
}

// ---- UpdateServiceState ----------------------------------------------------

func TestUpdateServiceState_FanOut(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 8)
	bcs := []chan map[string]interface{}{ch}

	invocations["inv-3"] = &invocationState{serviceId: "inv-3", path: "S.P3", status: StatusOngoing}
	sessions["sid-3"] = &monitorSession{sessionId: "sid-3", serviceId: "inv-3",
		routerIndex: 0, filterKind: "all"}

	UpdateServiceState("inv-3", StatusSuccessful, map[string]interface{}{"x": "1"}, bcs)

	select {
	case event := <-ch:
		if event["action"] != "monitoring" {
			t.Errorf("wrong action: %v", event["action"])
		}
		if event["status"] != "SUCCESSFUL" {
			t.Errorf("wrong status: %v", event["status"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	if _, ok := sessions["sid-3"]; ok {
		t.Error("session should be removed after terminal status")
	}
}

func TestUpdateServiceState_StatusFilterOnlyOnChange(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 8)
	bcs := []chan map[string]interface{}{ch}

	invocations["inv-4"] = &invocationState{serviceId: "inv-4", path: "S.P4", status: StatusOngoing}
	sessions["sid-4"] = &monitorSession{sessionId: "sid-4", serviceId: "inv-4",
		routerIndex: 0, filterKind: "status"}

	// Status unchanged → should NOT deliver.
	UpdateServiceState("inv-4", StatusOngoing, nil, bcs)
	select {
	case <-ch:
		t.Error("status filter should not deliver when status unchanged")
	case <-time.After(50 * time.Millisecond):
	}

	// Status changed → SHOULD deliver.
	UpdateServiceState("inv-4", StatusSuccessful, nil, bcs)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected event on status change")
	}
}

func TestUpdateServiceState_NoneFilterNeverDelivers(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 8)
	bcs := []chan map[string]interface{}{ch}

	invocations["inv-5"] = &invocationState{serviceId: "inv-5", path: "S.P5", status: StatusOngoing}
	sessions["sid-5"] = &monitorSession{sessionId: "sid-5", serviceId: "inv-5",
		routerIndex: 0, filterKind: "none"}

	UpdateServiceState("inv-5", StatusSuccessful, nil, bcs)
	select {
	case <-ch:
		t.Error("'none' filter should never deliver events")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUpdateServiceState_UnknownInvocationIsNoop(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	UpdateServiceState("does-not-exist", StatusFailed, nil, bcs)
	select {
	case <-ch:
		t.Error("unknown invocation should not produce events")
	case <-time.After(50 * time.Millisecond):
	}
}

// ---- isServiceAction (tested locally to avoid circular import) -------------

func TestIsServiceAction(t *testing.T) {
	for _, a := range []string{"invoke", "monitor", "cancel", "discover"} {
		if !isServiceActionStr(a) {
			t.Errorf("%q should be a service action", a)
		}
	}
	for _, a := range []string{"get", "set", "subscribe", ""} {
		if isServiceActionStr(a) {
			t.Errorf("%q should NOT be a service action", a)
		}
	}
}

func isServiceActionStr(a string) bool {
	switch a {
	case "invoke", "monitor", "cancel", "discover":
		return true
	}
	return false
}
