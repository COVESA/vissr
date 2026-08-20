package vissServiceMgr

// End-to-end tests for VISSv3.2 Service resource multiplexing (issue #198):
// invoking/monitoring/discovering a multiplexed procedure (MoveSeat,
// ActivateMassage) addressed via the "resource" filter variant, against the
// real rebuilt HIM-canonical tree (ServiceSpecification-example.binary).
//
// Covers: single-resource selection, multi-resource selection (wait-for-all
// completion semantics), wildcard expansion, "no resource filter" (defaults
// to all resources), and the negative case of two non-resource filter
// variants combined (400).

import (
	"testing"
	"time"
)

// activateMassagePath is the other multiplexed procedure in the example
// tree, used to confirm resource resolution is not MoveSeat-specific.
const activateMassagePath = "VehicleService.Seating.ActivateMassage"

// getCapabilitiesPath is the non-multiplexed procedure in the example tree.
const getCapabilitiesPath = "VehicleService.Seating.GetCapabilities"

func TestResolveResourceInstances_AllFourWhenNoFilter(t *testing.T) {
	loadVehicleServiceTree(t)
	node := resolveServiceNode(moveSeatPath)
	if node == nil {
		t.Fatal("could not resolve MoveSeat procedure")
	}

	keys, errStr := resolveResourceInstances(node, nil)
	if errStr != "" {
		t.Fatalf("unexpected error: %v", errStr)
	}
	want := []string{"Row1.DriverSide", "Row1.PassengerSide", "Row2.DriverSide", "Row2.PassengerSide"}
	if len(keys) != len(want) {
		t.Fatalf("got %v, want %v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Errorf("keys[%d] = %q, want %q (keys=%v)", i, keys[i], k, keys)
		}
	}
}

func TestResolveResourceInstances_SingleSnippetMatch(t *testing.T) {
	loadVehicleServiceTree(t)
	node := resolveServiceNode(moveSeatPath)

	keys, errStr := resolveResourceInstances(node, []string{"Row1.DriverSide"})
	if errStr != "" {
		t.Fatalf("unexpected error: %v", errStr)
	}
	if len(keys) != 1 || keys[0] != "Row1.DriverSide" {
		t.Errorf("keys = %v, want [Row1.DriverSide]", keys)
	}
}

func TestResolveResourceInstances_PrefixSnippetMatchesBothSides(t *testing.T) {
	loadVehicleServiceTree(t)
	node := resolveServiceNode(moveSeatPath)

	keys, errStr := resolveResourceInstances(node, []string{"Row1"})
	if errStr != "" {
		t.Fatalf("unexpected error: %v", errStr)
	}
	want := map[string]bool{"Row1.DriverSide": true, "Row1.PassengerSide": true}
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want 2 entries matching Row1.*", keys)
	}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected key %q in %v", k, keys)
		}
	}
}

func TestResolveResourceInstances_WildcardSegment(t *testing.T) {
	loadVehicleServiceTree(t)
	node := resolveServiceNode(moveSeatPath)

	keys, errStr := resolveResourceInstances(node, []string{"*.DriverSide"})
	if errStr != "" {
		t.Fatalf("unexpected error: %v", errStr)
	}
	want := map[string]bool{"Row1.DriverSide": true, "Row2.DriverSide": true}
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want 2 entries matching *.DriverSide", keys)
	}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected key %q in %v", k, keys)
		}
	}
}

func TestResolveResourceInstances_MultipleSnippetsUnionDeduplicated(t *testing.T) {
	loadVehicleServiceTree(t)
	node := resolveServiceNode(moveSeatPath)

	// "Row1" already covers Row1.DriverSide; adding it explicitly must not
	// produce a duplicate entry in the result.
	keys, errStr := resolveResourceInstances(node, []string{"Row1", "Row1.DriverSide", "Row2.PassengerSide"})
	if errStr != "" {
		t.Fatalf("unexpected error: %v", errStr)
	}
	want := []string{"Row1.DriverSide", "Row1.PassengerSide", "Row2.PassengerSide"}
	if len(keys) != len(want) {
		t.Fatalf("keys = %v, want %v (deduplicated)", keys, want)
	}
}

func TestResolveResourceInstances_NoMatchErrors(t *testing.T) {
	loadVehicleServiceTree(t)
	node := resolveServiceNode(moveSeatPath)

	_, errStr := resolveResourceInstances(node, []string{"Row3.DriverSide"})
	if errStr == "" {
		t.Error("expected an error for a resource snippet matching nothing")
	}
}

func TestResolveResourceInstances_NonMultiplexedProcedureReturnsNil(t *testing.T) {
	loadVehicleServiceTree(t)
	node := resolveServiceNode(getCapabilitiesPath)
	if node == nil {
		t.Fatal("could not resolve GetCapabilities procedure")
	}

	keys, errStr := resolveResourceInstances(node, nil)
	if errStr != "" {
		t.Fatalf("unexpected error: %v", errStr)
	}
	if keys != nil {
		t.Errorf("GetCapabilities is not multiplexed; want nil resourceKeys, got %v", keys)
	}
}

func TestResolveResourceInstances_ActivateMassageAlsoMultiplexed(t *testing.T) {
	loadVehicleServiceTree(t)
	node := resolveServiceNode(activateMassagePath)
	if node == nil {
		t.Fatal("could not resolve ActivateMassage procedure")
	}

	keys, errStr := resolveResourceInstances(node, nil)
	if errStr != "" {
		t.Fatalf("unexpected error: %v", errStr)
	}
	if len(keys) != 4 {
		t.Errorf("keys = %v, want 4 resource instances", keys)
	}
}

// ---- HandleInvoke — single resource via filter ------------------------------

func TestHandleInvoke_SingleResourceFilter_MovesAndCompletes(t *testing.T) {
	resetState()
	resetSeatState()
	shrinkStepPeriod(t, 3*time.Millisecond)
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	bc := make(chan map[string]interface{}, 64)
	bcs := []chan map[string]interface{}{bc}
	req := map[string]interface{}{
		"action": "invoke",
		"path":   moveSeatPath,
		"input":  map[string]interface{}{"MovementType": "longitudinal", "Position": "2"},
		"filter": []interface{}{
			map[string]interface{}{"variant": "resource", "parameter": []interface{}{"Row2.PassengerSide"}},
			map[string]interface{}{"variant": "all"},
		},
		"requestId":   "r1",
		"routerIndex": 0,
	}
	HandleInvoke(req, bcs)

	gotSuccess := false
	timeout := time.After(2 * time.Second)
	for !gotSuccess {
		select {
		case msg := <-bc:
			if msg["action"] == "monitoring" && msg["status"] == string(StatusSuccessful) {
				gotSuccess = true
				od, _ := msg["outdata"].(map[string]interface{})
				out, _ := od["output"].(map[string]interface{})
				if out["Position"] != "2" {
					t.Errorf("final Position = %v, want 2", out["Position"])
				}
			}
		case <-timeout:
			t.Fatal("did not reach SUCCESSFUL")
		}
	}
	// A different resource's state must be untouched (independent simulated position).
	seatMu.Lock()
	otherKey := seatKey(resourceScopedPath(moveSeatPath, "Row1.DriverSide"), "longitudinal")
	if v := seatState[otherKey]; v != 0 {
		t.Errorf("unaddressed resource state changed: Row1.DriverSide longitudinal = %d, want 0", v)
	}
	seatMu.Unlock()
}

// ---- HandleInvoke — multi-resource, wait-for-all completion -----------------

// TestHandleInvoke_MultiResourceFilter_WaitsForAllToSucceed exercises the
// confirmed "wait for all" completion semantics (design doc §3.2/§7 item 2):
// the overall invocation status stays ONGOING until every addressed resource
// has reported a terminal status, and is SUCCESSFUL only once all have
// succeeded.
func TestHandleInvoke_MultiResourceFilter_WaitsForAllToSucceed(t *testing.T) {
	resetState()
	resetSeatState()
	shrinkStepPeriod(t, 3*time.Millisecond)
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	// Pre-seed Row1.DriverSide already at the target (immediate), so only
	// Row1.PassengerSide actually needs to drive — this also exercises the
	// "partial-immediate + partial-run" mixed case from §4.2.
	seatMu.Lock()
	seatState[seatKey(resourceScopedPath(moveSeatPath, "Row1.DriverSide"), "longitudinal")] = 5
	seatMu.Unlock()

	bc := make(chan map[string]interface{}, 64)
	bcs := []chan map[string]interface{}{bc}
	req := map[string]interface{}{
		"action": "invoke",
		"path":   moveSeatPath,
		"input":  map[string]interface{}{"MovementType": "longitudinal", "Position": "5"},
		"filter": []interface{}{
			map[string]interface{}{"variant": "resource", "parameter": []interface{}{"Row1.DriverSide", "Row1.PassengerSide"}},
			map[string]interface{}{"variant": "all"},
		},
		"requestId":   "r2",
		"routerIndex": 0,
	}
	HandleInvoke(req, bcs)

	var sawOngoingAfterOneResourceDone bool
	gotOverallSuccess := false
	timeout := time.After(3 * time.Second)
	for !gotOverallSuccess {
		select {
		case msg := <-bc:
			if msg["action"] != "monitoring" {
				continue
			}
			if msg["status"] == string(StatusOngoing) {
				sawOngoingAfterOneResourceDone = true
			}
			if msg["status"] == string(StatusSuccessful) {
				gotOverallSuccess = true
				arr, ok := msg["outdata"].([]map[string]interface{})
				if !ok || len(arr) != 2 {
					t.Fatalf("final outdata = %#v, want a 2-entry per-resource array", msg["outdata"])
				}
			}
		case <-timeout:
			t.Fatal("multi-resource invocation did not reach overall SUCCESSFUL")
		}
	}
	if !sawOngoingAfterOneResourceDone {
		t.Error("expected at least one ONGOING event while only one of two resources had finished (wait-for-all)")
	}

	// Invocation must be cleaned up once both resources are terminal.
	mu.Lock()
	_, alive := invocations["r2"]
	n := len(invocations)
	mu.Unlock()
	if alive || n != 0 {
		t.Errorf("invocation should be removed after all resources succeed; alive=%v n=%d", alive, n)
	}
}

// TestHandleInvoke_MultiResourceFilter_AllAlreadyAtTarget exercises the
// "all resources already at target" immediate fast path (§4.2), which must
// respond SUCCESSFUL synchronously with a per-resource outdata array and
// create no invocation/session.
func TestHandleInvoke_MultiResourceFilter_AllAlreadyAtTarget(t *testing.T) {
	resetState()
	resetSeatState()
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	seatMu.Lock()
	seatState[seatKey(resourceScopedPath(moveSeatPath, "Row2.DriverSide"), "vertical")] = 10
	seatState[seatKey(resourceScopedPath(moveSeatPath, "Row2.PassengerSide"), "vertical")] = 10
	seatMu.Unlock()

	bc := make(chan map[string]interface{}, 8)
	bcs := []chan map[string]interface{}{bc}
	req := map[string]interface{}{
		"action": "invoke",
		"path":   moveSeatPath,
		"input":  map[string]interface{}{"MovementType": "vertical", "Position": "10"},
		"filter": []interface{}{
			map[string]interface{}{"variant": "resource", "parameter": []interface{}{"Row2.DriverSide", "Row2.PassengerSide"}},
			map[string]interface{}{"variant": "all"},
		},
		"requestId":   "r3",
		"routerIndex": 0,
	}
	HandleInvoke(req, bcs)

	select {
	case resp := <-bc:
		if resp["status"] != string(StatusSuccessful) {
			t.Errorf("status = %v, want SUCCESSFUL", resp["status"])
		}
		arr, ok := resp["outdata"].([]map[string]interface{})
		if !ok || len(arr) != 2 {
			t.Fatalf("outdata = %#v, want a 2-entry per-resource array", resp["outdata"])
		}
	case <-time.After(time.Second):
		t.Fatal("no response")
	}
	mu.Lock()
	n := len(invocations)
	mu.Unlock()
	if n != 0 {
		t.Errorf("all-immediate multi-resource invoke must not create an invocation, have %d", n)
	}
}

// ---- HandleMonitor — multi-resource outdata array ---------------------------

func TestHandleMonitor_MultiResourceInvocation_ReturnsOutdataArray(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	inv := &invocationState{
		serviceId: "mon-multi",
		path:      moveSeatPath,
		status:    StatusOngoing,
		startedAt: time.Now(),
		resources: []string{"Row1.DriverSide", "Row1.PassengerSide"},
		outdataByResource: map[string]map[string]interface{}{
			"Row1.DriverSide": {"output": map[string]interface{}{"Position": "5"}, "ts": "2026-01-01T00:00:00Z"},
		},
	}
	mu.Lock()
	invocations["mon-multi"] = inv
	mu.Unlock()

	bc := make(chan map[string]interface{}, 4)
	req := map[string]interface{}{
		"action":    "monitor",
		"path":      moveSeatPath,
		"requestId": "m1",
		"filter": []interface{}{
			map[string]interface{}{"variant": "resource", "parameter": []interface{}{"Row1.DriverSide", "Row1.PassengerSide"}},
			map[string]interface{}{"variant": "status"},
		},
		"routerIndex": 0,
	}
	HandleMonitor(req, []chan map[string]interface{}{bc})

	select {
	case resp := <-bc:
		arr, ok := resp["outdata"].([]map[string]interface{})
		if !ok {
			t.Fatalf("outdata = %#v (%T), want []map[string]interface{}", resp["outdata"], resp["outdata"])
		}
		if len(arr) != 1 {
			t.Errorf("outdata array len = %d, want 1 (only Row1.DriverSide reported so far)", len(arr))
		}
	case <-time.After(time.Second):
		t.Fatal("no monitor response")
	}
}

// ---- HandleDiscover — resource filter narrowing (§6) ------------------------

func TestHandleDiscover_ResourceFilter_NarrowsToOneInstance(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)

	bc := make(chan map[string]interface{}, 4)
	req := map[string]interface{}{
		"action":    "discover",
		"path":      moveSeatPath,
		"requestId": "d-res-1",
		"filter":    map[string]interface{}{"variant": "resource", "parameter": []interface{}{"Row1.DriverSide"}},
	}
	HandleDiscover(req, bc)

	select {
	case resp := <-bc:
		if e, ok := resp["error"]; ok {
			t.Fatalf("unexpected error: %v", e)
		}
		meta, ok := resp["metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("metadata missing/!map: %T", resp["metadata"])
		}
		if _, ok := meta["Row1.DriverSide"]; !ok {
			t.Errorf("metadata missing narrowed resource key, got keys: %v", metaKeys(meta))
		}
		if _, ok := meta["Row1.PassengerSide"]; ok {
			t.Error("metadata should not include unaddressed resource Row1.PassengerSide")
		}
		if _, ok := meta["Version"]; ok {
			t.Error("metadata should not include the singleton Version node when narrowed by resource")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHandleDiscover_ResourceFilterOnBranchPath_Rejected(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)

	bc := make(chan map[string]interface{}, 4)
	req := map[string]interface{}{
		"action":    "discover",
		"path":      "VehicleService.Seating",
		"requestId": "d-res-2",
		"filter":    map[string]interface{}{"variant": "resource", "parameter": []interface{}{"Row1"}},
	}
	HandleDiscover(req, bc)

	select {
	case resp := <-bc:
		if _, ok := resp["error"]; !ok {
			t.Error("expected an error: resource filter must not address a branch node")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHandleDiscover_UnmultiplexedProcedureIgnoresNoFilter(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)

	bc := make(chan map[string]interface{}, 4)
	req := map[string]interface{}{
		"action":    "discover",
		"path":      getCapabilitiesPath,
		"requestId": "d-gc",
	}
	HandleDiscover(req, bc)

	select {
	case resp := <-bc:
		if e, ok := resp["error"]; ok {
			t.Fatalf("unexpected error: %v", e)
		}
		meta, ok := resp["metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("metadata missing/!map: %T", resp["metadata"])
		}
		if _, ok := meta["Output"]; !ok {
			t.Errorf("GetCapabilities metadata missing Output iostruct, got keys: %v", metaKeys(meta))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func metaKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ---- HandleInvoke — negative filter combination (400) -----------------------

// TestHandleInvoke_TwoNonResourceFiltersCombined_Rejected exercises the
// negative case from §8 item 8: an array combining two non-resource filter
// variants is not a valid "Multiple Filters" combination and must be
// rejected with a 400, independent of the JSON-schema-level rejection
// already covered in utils/service_schema_test.go (this is the Go-side
// vissServiceMgr enforcement, reached after schema validation).
func TestHandleInvoke_TwoNonResourceFiltersCombined_Rejected(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	bc := make(chan map[string]interface{}, 4)
	req := map[string]interface{}{
		"action": "invoke",
		"path":   moveSeatPath,
		"input":  map[string]interface{}{"MovementType": "longitudinal", "Position": "5"},
		"filter": []interface{}{
			map[string]interface{}{"variant": "status"},
			map[string]interface{}{"variant": "all"},
		},
		"requestId":   "bad-combo",
		"routerIndex": 0,
	}
	HandleInvoke(req, []chan map[string]interface{}{bc})

	select {
	case resp := <-bc:
		errObj, ok := resp["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected an error response, got %v", resp)
		}
		if errObj["number"] != "400" {
			t.Errorf("error number = %v, want 400", errObj["number"])
		}
	case <-time.After(time.Second):
		t.Fatal("no response")
	}
	mu.Lock()
	n := len(invocations)
	mu.Unlock()
	if n != 0 {
		t.Errorf("rejected filter combination must not create an invocation, have %d", n)
	}
}

// TestHandleInvoke_ResourceFilterNoMatch_Rejected confirms an unmatched
// resource snippet against a real multiplexed procedure is a 400, not a
// silent empty-resource invocation.
func TestHandleInvoke_ResourceFilterNoMatch_Rejected(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	bc := make(chan map[string]interface{}, 4)
	req := map[string]interface{}{
		"action":      "invoke",
		"path":        moveSeatPath,
		"input":       map[string]interface{}{"MovementType": "longitudinal", "Position": "5"},
		"filter":      map[string]interface{}{"variant": "resource", "parameter": []interface{}{"Row9.DriverSide"}},
		"requestId":   "no-match",
		"routerIndex": 0,
	}
	HandleInvoke(req, []chan map[string]interface{}{bc})

	select {
	case resp := <-bc:
		if _, ok := resp["error"]; !ok {
			t.Errorf("expected an error for an unmatched resource filter, got %v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("no response")
	}
}
