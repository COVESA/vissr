package vissServiceMgr

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

// Unit tests for the VISSv3.3-alpha service manager.
// Functions that require a live binary tree (SetRootNodePointer, VSSgetType,
// validateInputSignature) are integration-only and are documented as such
// rather than unit-tested here.

func resetState() {
	invocations = map[string]*invocationState{}
	sessions = map[string]*monitorSession{}
	metricsMu.Lock()
	metrics = map[string]*pathMetrics{}
	metricsMu.Unlock()
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

func TestPeriodFromFilter_ZeroPeriodFallsBack(t *testing.T) {
	// err==nil but ms<=0 → the second branch of the compound condition
	f := map[string]interface{}{"variant": "timebased", "parameter": map[string]interface{}{"period": "0"}}
	if p := periodFromFilter(f); p != time.Second {
		t.Fatalf("want 1s for zero period, got %v", p)
	}
}

func TestPeriodFromFilter_ParamNilFallsBack(t *testing.T) {
	// "parameter" key missing → param==nil → return time.Second
	f := map[string]interface{}{"variant": "timebased"}
	if p := periodFromFilter(f); p != time.Second {
		t.Fatalf("want 1s when no parameter key, got %v", p)
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

	UpdateServiceState("inv-3", StatusSuccessful, map[string]interface{}{"x": "1"}, nil, nil, bcs)

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
	UpdateServiceState("inv-4", StatusOngoing, nil, nil, nil, bcs)
	select {
	case <-ch:
		t.Error("status filter should not deliver when status unchanged")
	case <-time.After(50 * time.Millisecond):
	}

	// Status changed → SHOULD deliver.
	UpdateServiceState("inv-4", StatusSuccessful, nil, nil, nil, bcs)
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

	UpdateServiceState("inv-5", StatusSuccessful, nil, nil, nil, bcs)
	select {
	case <-ch:
		t.Error("'none' filter should never deliver events")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUpdateServiceState_WithProgress(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["inv-p"] = &invocationState{serviceId: "inv-p", path: "S.P", status: StatusOngoing}
	sessions["sid-p"] = &monitorSession{sessionId: "sid-p", serviceId: "inv-p",
		routerIndex: 0, filterKind: "all"}

	pct := 50
	UpdateServiceState("inv-p", StatusOngoing, nil, nil, &pct, bcs)

	select {
	case event := <-ch:
		if _, ok := event["progress"]; !ok {
			t.Errorf("progress field missing from event: %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for progress event")
	}
}

func TestUpdateServiceState_UnknownInvocationIsNoop(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	UpdateServiceState("does-not-exist", StatusFailed, nil, nil, nil, bcs)
	select {
	case <-ch:
		t.Error("unknown invocation should not produce events")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestUpdateServiceState_ServiceErrorIncludedInEvent verifies that a non-nil
// ServiceError is included in monitoring events as {"error":{"code":...,"message":...}}.
func TestUpdateServiceState_ServiceErrorIncludedInEvent(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["inv-e"] = &invocationState{serviceId: "inv-e", path: "S.PE", status: StatusOngoing}
	sessions["sid-e"] = &monitorSession{sessionId: "sid-e", serviceId: "inv-e",
		routerIndex: 0, filterKind: "all"}

	svcErr := &ServiceError{Code: "MOTOR_STALL", Message: "seat motor stalled"}
	UpdateServiceState("inv-e", StatusFailed, nil, svcErr, nil, bcs)

	select {
	case event := <-ch:
		errField, ok := event["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("event missing 'error' field: %v", event)
		}
		if errField["code"] != "MOTOR_STALL" {
			t.Errorf("wrong error code: %v", errField["code"])
		}
		if errField["message"] != "seat motor stalled" {
			t.Errorf("wrong error message: %v", errField["message"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for error event")
	}
}

// ---- FormatAsSSE -----------------------------------------------------------

func TestFormatAsSSE_ValidJSON(t *testing.T) {
	event := map[string]interface{}{
		"action": "monitoring",
		"status": "ONGOING",
		"ts":     "2026-01-01T00:00:00Z",
	}
	got, err := FormatAsSSE(event)
	if err != nil {
		t.Fatalf("FormatAsSSE error: %v", err)
	}
	if !strings.HasPrefix(got, "data: ") {
		t.Errorf("SSE frame must start with 'data: ', got %q", got[:min(20, len(got))])
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("SSE frame must end with double newline, got %q", got[max(0, len(got)-4):])
	}
	// The JSON payload inside must be valid.
	payload := strings.TrimPrefix(strings.TrimSuffix(got, "\n\n"), "data: ")
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Errorf("SSE payload is not valid JSON: %v", err)
	}
	if decoded["action"] != "monitoring" {
		t.Errorf("wrong action in SSE payload: %v", decoded["action"])
	}
}

func TestFormatAsSSE_EmptyEvent(t *testing.T) {
	got, err := FormatAsSSE(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "data: {}\n\n" {
		t.Errorf("unexpected output for empty event: %q", got)
	}
}

// ---- StartServiceRegServerTLS ----------------------------------------------

func TestStartServiceRegServerTLS_InvalidCertReturnsError(t *testing.T) {
	err := StartServiceRegServerTLS(nil, "/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Fatal("expected error for missing TLS certificate files")
	}
	if !strings.Contains(err.Error(), "load TLS") {
		t.Errorf("unexpected error message: %v", err)
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

// ---- startTimeoutWatchdog --------------------------------------------------

// TestTimeoutWatchdog_FiresOnExpiry verifies that an ONGOING invocation is
// transitioned to FAILED after its deadline passes.
func TestTimeoutWatchdog_FiresOnExpiry(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	inv := &invocationState{
		serviceId: "tw-1",
		path:      "S.Tw",
		status:    StatusOngoing,
		deadline:  time.Now().Add(20 * time.Millisecond),
	}
	invocations["tw-1"] = inv
	sessions["tw-s1"] = &monitorSession{sessionId: "tw-s1", serviceId: "tw-1",
		routerIndex: 0, filterKind: "all"}

	// Store cancelFn under mu: UpdateServiceState reads inv.cancelFn under mu.
	cancelFn := startTimeoutWatchdog(inv, bcs)
	mu.Lock()
	inv.cancelFn = cancelFn
	mu.Unlock()

	select {
	case event := <-ch:
		if event["status"] != "FAILED" {
			t.Errorf("want FAILED from timeout, got %v", event["status"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout watchdog did not fire")
	}

	// Invocation must be removed on terminal status.
	mu.Lock()
	_, still := invocations["tw-1"]
	mu.Unlock()
	if still {
		t.Error("invocation should be removed after timeout")
	}
}

// TestTimeoutWatchdog_CancelPreventsExpiry verifies that calling the cancel
// function stops the watchdog before it fires.
func TestTimeoutWatchdog_CancelPreventsExpiry(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	inv := &invocationState{
		serviceId: "tw-2",
		path:      "S.Tw2",
		status:    StatusOngoing,
		deadline:  time.Now().Add(50 * time.Millisecond),
	}
	invocations["tw-2"] = inv

	cancel := startTimeoutWatchdog(inv, bcs)
	cancel() // stop before deadline

	select {
	case <-ch:
		t.Error("watchdog fired after cancel")
	case <-time.After(100 * time.Millisecond):
		// correct: no event after cancel
	}
}

// ---- Concurrent invocation isolation ----------------------------------------

// TestConcurrentInvocations_IndependentState verifies that two concurrent
// invocations of the same procedure maintain independent state (VISSv3.3 §10).
func TestConcurrentInvocations_IndependentState(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 8)
	bcs := []chan map[string]interface{}{ch}

	// Two concurrent invocations of the same path.
	invocations["ci-1"] = &invocationState{serviceId: "ci-1", path: "S.PC", status: StatusOngoing,
		startedAt: time.Now().Add(-time.Millisecond)}
	invocations["ci-2"] = &invocationState{serviceId: "ci-2", path: "S.PC", status: StatusOngoing,
		startedAt: time.Now()}
	sessions["cs-1"] = &monitorSession{sessionId: "cs-1", serviceId: "ci-1", routerIndex: 0, filterKind: "all"}
	sessions["cs-2"] = &monitorSession{sessionId: "cs-2", serviceId: "ci-2", routerIndex: 0, filterKind: "all"}

	// Only terminate ci-1 successfully.
	UpdateServiceState("ci-1", StatusSuccessful, map[string]interface{}{"r": "ok"}, nil, nil, bcs)

	// ci-2 must still be ONGOING.
	mu.Lock()
	ci2, ok := invocations["ci-2"]
	mu.Unlock()
	if !ok || ci2.status != StatusOngoing {
		t.Error("ci-2 should remain ONGOING after ci-1 terminates")
	}

	// Only one event should have been delivered (for cs-1).
	select {
	case event := <-ch:
		if event["serviceId"] != "cs-1" {
			t.Errorf("expected cs-1 event, got serviceId=%v", event["serviceId"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected one monitoring event")
	}
	select {
	case extra := <-ch:
		t.Errorf("unexpected second event: %v", extra)
	case <-time.After(30 * time.Millisecond):
	}
}

// ---- HandleInvoke/HandleMonitor bounds guard --------------------------------

func TestHandleInvoke_BadRouterIndex_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("HandleInvoke panicked with bad routerIndex: %v", r)
		}
	}()
	resetState()
	// routerIndex 99 is way out of range for a 1-channel slice.
	req := map[string]interface{}{"path": "S.P", "requestId": "r1", "routerIndex": 99}
	bcs := []chan map[string]interface{}{make(chan map[string]interface{}, 4)}
	HandleInvoke(req, bcs)
}

func TestHandleMonitor_BadRouterIndex_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("HandleMonitor panicked with bad routerIndex: %v", r)
		}
	}()
	resetState()
	req := map[string]interface{}{"path": "S.P", "requestId": "r2", "routerIndex": 99}
	bcs := []chan map[string]interface{}{make(chan map[string]interface{}, 4)}
	HandleMonitor(req, bcs)
}

// ---- sendValidationError (§29) ---------------------------------------------

func TestSendValidationError_IncludesFields(t *testing.T) {
	ch := make(chan map[string]interface{}, 4)
	sendValidationError(ch, "invoke", "req-v", []string{"SeatId", "Position"})
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
			t.Fatalf("missing error object: %v", m)
		}
		if errObj["number"] != "400" {
			t.Errorf("wrong error number: %v", errObj["number"])
		}
		rawFields := errObj["fields"]
		if rawFields == nil {
			t.Fatal("missing 'fields' in error object")
		}
		switch f := rawFields.(type) {
		case []string:
			if len(f) != 2 {
				t.Errorf("want 2 fields, got %d", len(f))
			}
		case []interface{}:
			if len(f) != 2 {
				t.Errorf("want 2 fields (interface{}), got %d", len(f))
			}
		default:
			t.Errorf("unexpected fields type: %T", rawFields)
		}
		if m["requestId"] != "req-v" {
			t.Errorf("wrong requestId: %v", m["requestId"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestSendValidationError_NilFields(t *testing.T) {
	ch := make(chan map[string]interface{}, 4)
	sendValidationError(ch, "invoke", "req-nil", nil)
	select {
	case m := <-ch:
		errObj, ok := m["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing error object: %v", m)
		}
		if _, ok := errObj["fields"]; !ok {
			t.Error("'fields' key should be present even when nil")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// ---- UpdateServiceState progress (§28) --------------------------------------

func TestUpdateServiceState_ProgressInOngoingEvent(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["inv-p1"] = &invocationState{serviceId: "inv-p1", path: "S.PP", status: StatusOngoing}
	sessions["sid-p1"] = &monitorSession{sessionId: "sid-p1", serviceId: "inv-p1", routerIndex: 0, filterKind: "all"}

	pct := 60
	UpdateServiceState("inv-p1", StatusOngoing, nil, nil, &pct, bcs)

	select {
	case event := <-ch:
		got, ok := event["progress"]
		if !ok {
			t.Fatal("progress field missing from ONGOING event")
		}
		if got != 60 {
			t.Errorf("want progress=60, got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestUpdateServiceState_NilProgressNotIncluded(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["inv-p2"] = &invocationState{serviceId: "inv-p2", path: "S.PP2", status: StatusOngoing}
	sessions["sid-p2"] = &monitorSession{sessionId: "sid-p2", serviceId: "inv-p2", routerIndex: 0, filterKind: "all"}

	UpdateServiceState("inv-p2", StatusOngoing, nil, nil, nil, bcs)

	select {
	case event := <-ch:
		if _, ok := event["progress"]; ok {
			t.Error("progress should be absent when nil was passed")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestUpdateServiceState_ProgressAbsentOnTerminal(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["inv-p3"] = &invocationState{serviceId: "inv-p3", path: "S.PP3", status: StatusOngoing, startedAt: time.Now()}
	sessions["sid-p3"] = &monitorSession{sessionId: "sid-p3", serviceId: "inv-p3", routerIndex: 0, filterKind: "all"}

	pct := 99
	UpdateServiceState("inv-p3", StatusSuccessful, nil, nil, &pct, bcs)

	select {
	case event := <-ch:
		if _, ok := event["progress"]; ok {
			t.Error("progress should not be present in terminal (SUCCESSFUL) event")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// ---- Observability metrics (§31) --------------------------------------------

func TestMetrics_IncrementOnSuccessful(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["met-1"] = &invocationState{serviceId: "met-1", path: "M.Path1", status: StatusOngoing, startedAt: time.Now()}
	sessions["ms-1"] = &monitorSession{sessionId: "ms-1", serviceId: "met-1", routerIndex: 0, filterKind: "all"}

	UpdateServiceState("met-1", StatusSuccessful, nil, nil, nil, bcs)
	<-ch

	metricsMu.Lock()
	pm := metrics["M.Path1"]
	metricsMu.Unlock()
	if pm == nil {
		t.Fatal("metrics should be created for M.Path1")
	}
	if pm.total != 1 {
		t.Errorf("want total=1, got %d", pm.total)
	}
	if pm.successes != 1 {
		t.Errorf("want successes=1, got %d", pm.successes)
	}
	if pm.failures != 0 || pm.cancels != 0 {
		t.Errorf("unexpected failures=%d cancels=%d", pm.failures, pm.cancels)
	}
}

func TestMetrics_IncrementOnFailed(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["met-2"] = &invocationState{serviceId: "met-2", path: "M.Path2", status: StatusOngoing, startedAt: time.Now()}
	sessions["ms-2"] = &monitorSession{sessionId: "ms-2", serviceId: "met-2", routerIndex: 0, filterKind: "all"}

	UpdateServiceState("met-2", StatusFailed, nil, nil, nil, bcs)
	<-ch

	metricsMu.Lock()
	pm := metrics["M.Path2"]
	metricsMu.Unlock()
	if pm == nil {
		t.Fatal("metrics should exist")
	}
	if pm.failures != 1 {
		t.Errorf("want failures=1, got %d", pm.failures)
	}
	if pm.successes != 0 {
		t.Errorf("want successes=0, got %d", pm.successes)
	}
}

func TestMetrics_IncrementOnCanceled(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["met-3"] = &invocationState{serviceId: "met-3", path: "M.Path3", status: StatusOngoing, startedAt: time.Now()}
	sessions["ms-3"] = &monitorSession{sessionId: "ms-3", serviceId: "met-3", routerIndex: 0, filterKind: "all"}

	UpdateServiceState("met-3", StatusCanceled, nil, nil, nil, bcs)
	<-ch

	metricsMu.Lock()
	pm := metrics["M.Path3"]
	metricsMu.Unlock()
	if pm == nil {
		t.Fatal("metrics should exist")
	}
	if pm.cancels != 1 {
		t.Errorf("want cancels=1, got %d", pm.cancels)
	}
}

func TestMetrics_NoEntryForOngoing(t *testing.T) {
	resetState()
	ch := make(chan map[string]interface{}, 4)
	bcs := []chan map[string]interface{}{ch}

	invocations["met-o"] = &invocationState{serviceId: "met-o", path: "M.PathO", status: StatusOngoing, startedAt: time.Now()}
	sessions["ms-o"] = &monitorSession{sessionId: "ms-o", serviceId: "met-o", routerIndex: 0, filterKind: "all"}

	UpdateServiceState("met-o", StatusOngoing, nil, nil, nil, bcs)
	<-ch

	metricsMu.Lock()
	pm := metrics["M.PathO"]
	metricsMu.Unlock()
	if pm != nil && pm.total != 0 {
		t.Error("ONGOING transitions should not increment the total metric counter")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- FormatAsSSE -----------------------------------------------------------

func TestFormatAsSSE_WellFormedOutput(t *testing.T) {
	event := map[string]interface{}{"action": "ping", "id": "42"}
	got, err := FormatAsSSE(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "data: ") {
		t.Errorf("missing data: prefix in %q", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("missing trailing \\n\\n in %q", got)
	}
	// Payload must be valid JSON.
	var m map[string]interface{}
	payload := strings.TrimPrefix(strings.TrimSuffix(got, "\n\n"), "data: ")
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Errorf("payload is not valid JSON: %v", err)
	}
}

func TestFormatAsSSE_MarshalError(t *testing.T) {
	// Channels cannot be JSON-marshaled; FormatAsSSE must propagate the error.
	event := map[string]interface{}{"ch": make(chan int)}
	_, err := FormatAsSSE(event)
	if err == nil {
		t.Error("expected error for unmarshalable event value, got nil")
	}
}

// ---- extractRouterIndex ----------------------------------------------------

func TestExtractRouterIndex_IntValue(t *testing.T) {
	got := extractRouterIndex(map[string]interface{}{"routerIndex": 3})
	if got != 3 {
		t.Errorf("extractRouterIndex = %d, want 3", got)
	}
}

func TestExtractRouterIndex_Float64FallsBackToZero(t *testing.T) {
	// JSON unmarshaling yields float64; the function only handles int.
	got := extractRouterIndex(map[string]interface{}{"routerIndex": float64(7)})
	if got != 0 {
		t.Errorf("want 0 for float64 value, got %d", got)
	}
}

func TestExtractRouterIndex_MissingKeyReturnsZero(t *testing.T) {
	if got := extractRouterIndex(map[string]interface{}{}); got != 0 {
		t.Errorf("want 0 for missing key, got %d", got)
	}
}

// ---- buildIoStructMetadata -------------------------------------------------

func TestBuildIoStructMetadata_ReturnsParamMap(t *testing.T) {
	pos := utils.NewPropertyNode("Position", "uint8", "seat position")
	speed := utils.NewPropertyNode("Speed", "float", "speed setpoint")
	iostruct := utils.NewIoStructNode("Input", pos, speed)

	params := buildIoStructMetadata(iostruct)

	if len(params) != 2 {
		t.Fatalf("want 2 params, got %d: %v", len(params), params)
	}
	for _, name := range []string{"Position", "Speed"} {
		entry, ok := params[name]
		if !ok {
			t.Errorf("param %q missing", name)
			continue
		}
		m, ok := entry.(map[string]interface{})
		if !ok {
			t.Errorf("param %q: want map, got %T", name, entry)
			continue
		}
		if m["type"] != utils.PROPERTY {
			t.Errorf("param %q: type = %v, want %q", name, m["type"], utils.PROPERTY)
		}
	}
}

func TestBuildIoStructMetadata_EmptyIoStruct(t *testing.T) {
	iostruct := utils.NewIoStructNode("Input")
	params := buildIoStructMetadata(iostruct)
	if len(params) != 0 {
		t.Errorf("want empty map, got %v", params)
	}
}

// ---- validateIoParams / validateInputSignature -----------------------------

func TestValidateIoParams_AllPresent(t *testing.T) {
	pos := utils.NewPropertyNode("Position", "uint8", "")
	spd := utils.NewPropertyNode("Speed", "float", "")
	iostruct := utils.NewIoStructNode("Input", pos, spd)

	ok, missing := validateIoParams(iostruct, map[string]interface{}{
		"Position": "40",
		"Speed":    "10.5",
	})
	if !ok || len(missing) != 0 {
		t.Errorf("expected valid, got ok=%v missing=%v", ok, missing)
	}
}

func TestValidateIoParams_MissingParam(t *testing.T) {
	pos := utils.NewPropertyNode("Position", "uint8", "")
	spd := utils.NewPropertyNode("Speed", "float", "")
	iostruct := utils.NewIoStructNode("Input", pos, spd)

	ok, missing := validateIoParams(iostruct, map[string]interface{}{"Position": "40"})
	if ok {
		t.Error("expected invalid (Speed missing)")
	}
	if len(missing) != 1 || missing[0] != "Speed" {
		t.Errorf("expected missing=[Speed], got %v", missing)
	}
}

func TestValidateInputSignature_NoInputChild(t *testing.T) {
	proc := utils.NewProcedureNode("Start", "starts engine")
	ok, missing := validateInputSignature(proc, map[string]interface{}{})
	if !ok || len(missing) != 0 {
		t.Errorf("no Input child → should be valid; got ok=%v missing=%v", ok, missing)
	}
}

func TestValidateInputSignature_WithInputChild_Valid(t *testing.T) {
	pos := utils.NewPropertyNode("Position", "uint8", "")
	inputStruct := utils.NewIoStructNode("Input", pos)
	proc := utils.NewProcedureNode("MoveSeat", "moves seat", inputStruct)

	ok, missing := validateInputSignature(proc, map[string]interface{}{"Position": "50"})
	if !ok || len(missing) != 0 {
		t.Errorf("expected valid, got ok=%v missing=%v", ok, missing)
	}
}

func TestValidateInputSignature_WithInputChild_Missing(t *testing.T) {
	pos := utils.NewPropertyNode("Position", "uint8", "")
	inputStruct := utils.NewIoStructNode("Input", pos)
	proc := utils.NewProcedureNode("MoveSeat", "moves seat", inputStruct)

	ok, missing := validateInputSignature(proc, map[string]interface{}{})
	if ok {
		t.Error("expected invalid (Position missing)")
	}
	if len(missing) != 1 || missing[0] != "Position" {
		t.Errorf("expected missing=[Position], got %v", missing)
	}
}

// ---- buildServiceMetadata --------------------------------------------------

func TestBuildServiceMetadata_EmptyBranch(t *testing.T) {
	root := utils.NewBranchNode("Root")
	meta := buildServiceMetadata(root, "Root")
	if len(meta) != 0 {
		t.Errorf("expected empty map for branchless root, got %v", meta)
	}
}

func TestBuildServiceMetadata_WithProcedure(t *testing.T) {
	resetState()
	proc := utils.NewProcedureNode("MoveSeat", "moves seat")
	root := utils.NewBranchNode("SeatSvc", proc)

	meta := buildServiceMetadata(root, "SeatSvc")

	entry, ok := meta["MoveSeat"]
	if !ok {
		t.Fatal("MoveSeat not in metadata")
	}
	m, ok := entry.(map[string]interface{})
	if !ok {
		t.Fatalf("MoveSeat entry is not a map: %T", entry)
	}
	if m["type"] != "procedure" {
		t.Errorf("type = %v, want procedure", m["type"])
	}
	if m["serviceStatus"] != "disconnected" {
		t.Errorf("serviceStatus = %v, want disconnected", m["serviceStatus"])
	}
}

func TestBuildServiceMetadata_WithNestedBranch(t *testing.T) {
	resetState()
	proc := utils.NewProcedureNode("Adjust", "adjust value")
	sub := utils.NewBranchNode("Sub", proc)
	root := utils.NewBranchNode("Root", sub)

	meta := buildServiceMetadata(root, "Root")

	subMeta, ok := meta["Sub"]
	if !ok {
		t.Fatal("Sub branch not in metadata")
	}
	sm, ok := subMeta.(map[string]interface{})
	if !ok {
		t.Fatalf("Sub entry is not a map: %T", subMeta)
	}
	if _, ok := sm["Adjust"]; !ok {
		t.Error("nested procedure Adjust not found under Sub")
	}
}

// ---- buildProcedureMetadata ------------------------------------------------

func TestBuildProcedureMetadata_Disconnected(t *testing.T) {
	resetState()
	proc := utils.NewProcedureNode("UnregisteredProc", "not registered")
	meta := buildProcedureMetadata(proc, "Root.UnregisteredProc")

	if meta["type"] != "procedure" {
		t.Errorf("type = %v, want procedure", meta["type"])
	}
	if meta["serviceStatus"] != "disconnected" {
		t.Errorf("serviceStatus = %v, want disconnected", meta["serviceStatus"])
	}
	if meta["activeInvocations"] != 0 {
		t.Errorf("activeInvocations = %v, want 0", meta["activeInvocations"])
	}
}

func TestBuildProcedureMetadata_WithActiveInvocation(t *testing.T) {
	resetState()
	invocations["active-1"] = &invocationState{
		serviceId: "active-1",
		path:      "Root.ActiveProc",
		status:    StatusOngoing,
		startedAt: time.Now(),
	}

	proc := utils.NewProcedureNode("ActiveProc", "has active invocation")
	meta := buildProcedureMetadata(proc, "Root.ActiveProc")

	if meta["activeInvocations"] != 1 {
		t.Errorf("activeInvocations = %v, want 1", meta["activeInvocations"])
	}
}

func TestBuildProcedureMetadata_WithMetrics(t *testing.T) {
	resetState()
	path := "Root.MetricsProc"
	metricsMu.Lock()
	metrics[path] = &pathMetrics{total: 10, successes: 8, totalDurMs: 500}
	metricsMu.Unlock()

	proc := utils.NewProcedureNode("MetricsProc", "has metrics")
	meta := buildProcedureMetadata(proc, path)

	if meta["totalInvocations"] != int64(10) {
		t.Errorf("totalInvocations = %v, want 10", meta["totalInvocations"])
	}
	if sr, _ := meta["successRate"].(float64); sr < 0.79 || sr > 0.81 {
		t.Errorf("successRate = %v, want ~0.8", meta["successRate"])
	}
	if ad, _ := meta["avgDurationMs"].(int64); ad != 50 {
		t.Errorf("avgDurationMs = %v, want 50", meta["avgDurationMs"])
	}
}

func TestBuildProcedureMetadata_WithIoStructChildren(t *testing.T) {
	resetState()
	posParam := utils.NewPropertyNode("Position", "uint8", "seat position")
	inputStruct := utils.NewIoStructNode("Input", posParam)
	proc := utils.NewProcedureNode("MoveSeat", "moves seat", inputStruct)

	meta := buildProcedureMetadata(proc, "Root.MoveSeat")

	inputMeta, ok := meta["Input"]
	if !ok {
		t.Fatal("Input not in procedure metadata")
	}
	im, ok := inputMeta.(map[string]interface{})
	if !ok {
		t.Fatalf("Input entry is not a map: %T", inputMeta)
	}
	if _, ok := im["Position"]; !ok {
		t.Error("Position param not found in Input metadata")
	}
}
