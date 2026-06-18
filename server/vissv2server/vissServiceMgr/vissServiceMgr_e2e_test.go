package vissServiceMgr

// End-to-end routing tests for the VISSv3.2 service profile.
//
// These exercise the path that the package-level unit tests deliberately skip
// (see the header of vissServiceMgr_test.go): resolving a full service path
// against a real binary HIM tree loaded into the forest. They reproduce the
// invoke request from client/client-1.0/Javascript/appclient_service_commands.txt
// (requestId 8756) that previously failed with a 400 "The request is malformed."
//
// Root causes covered:
//   1. viss.him registered the tree as "VehicleServices" (plural) while the
//      tree root and the client path use "VehicleService" — so the request
//      never routed to vissServiceMgr.
//   2. HandleInvoke/Monitor/Discover used SetRootNodePointer (which only
//      returns the tree root) instead of walking the full path, so a valid
//      procedure path resolved to the root branch and was rejected as "must
//      address a procedure node".
//   3. validateIoParams required every Input child, including the optional
//      MoveSeat.Credentials parameter.

import (
	"os"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

const serviceTreeBinary = "../forest/VehicleServices.v1.0.binary"

const moveSeatPath = "VehicleService.Seating.Row1.DriverSide.MoveSeat"

// loadVehicleServiceTree loads the bundled HIM service example tree and
// registers it under the "VehicleService" root, matching the (fixed)
// server/vissv2server/viss.him configuration. The test is skipped when the
// binary is unavailable (e.g. a sparse checkout).
func loadVehicleServiceTree(t *testing.T) *utils.Node_t {
	t.Helper()
	if _, err := os.Stat(serviceTreeBinary); err != nil {
		t.Skipf("service tree binary not present (%s): %v", serviceTreeBinary, err)
	}
	root := utils.VSSReadTree(serviceTreeBinary)
	if root == nil {
		t.Fatalf("VSSReadTree(%s) returned nil", serviceTreeBinary)
	}
	utils.DeregisterServiceTree("VehicleService")
	if !utils.RegisterServiceTree("VehicleService", "Vehicle.Car.Service", "1.0.0", root) {
		t.Fatal("RegisterServiceTree(VehicleService) failed")
	}
	t.Cleanup(func() { utils.DeregisterServiceTree("VehicleService") })
	return root
}

// stopServiceGoroutines stops the timeout watchdogs and time-based monitoring
// tickers spawned by HandleInvoke so they do not outlive the test.
func stopServiceGoroutines() {
	mu.Lock()
	defer mu.Unlock()
	for _, inv := range invocations {
		if inv.cancelFn != nil {
			inv.cancelFn()
			inv.cancelFn = nil
		}
	}
	for _, sess := range sessions {
		if sess.cancelTicker != nil {
			sess.cancelTicker()
			sess.cancelTicker = nil
		}
	}
}

func TestResolveServiceNode_FullPathReachesProcedure(t *testing.T) {
	loadVehicleServiceTree(t)

	// SetRootNodePointer alone returns the tree root branch, never the
	// addressed procedure — the bug that broke invoke routing.
	root := utils.SetRootNodePointer(moveSeatPath)
	if root == nil {
		t.Fatal("SetRootNodePointer returned nil for a known root")
	}
	if utils.VSSgetType(root) == utils.PROCEDURE {
		t.Fatal("precondition failed: tree root should be a branch, not a procedure")
	}

	node := resolveServiceNode(moveSeatPath)
	if node == nil {
		t.Fatalf("resolveServiceNode(%q) = nil; want the MoveSeat procedure", moveSeatPath)
	}
	if got := utils.VSSgetType(node); got != utils.PROCEDURE {
		t.Errorf("resolved node type = %q, want PROCEDURE", got)
	}
	if got := utils.VSSgetName(node); got != "MoveSeat" {
		t.Errorf("resolved node name = %q, want MoveSeat", got)
	}
}

func TestResolveServiceNode_BadPathsReturnNil(t *testing.T) {
	loadVehicleServiceTree(t)

	cases := map[string]string{
		"plural root (the viss.him typo)": "VehicleServices.Seating.Row1.DriverSide.MoveSeat",
		"unknown mid segment":             "VehicleService.Nope.MoveSeat",
		"unknown leaf":                    "VehicleService.Seating.Row1.DriverSide.NoSuchProc",
	}
	for name, path := range cases {
		if n := resolveServiceNode(path); n != nil {
			t.Errorf("%s: resolveServiceNode(%q) = %v, want nil", name, path, utils.VSSgetName(n))
		}
	}
}

func TestHandleInvoke_AppclientRequest_RoutesAndReturnsOngoing(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	bc := make(chan map[string]interface{}, 8)
	bcs := []chan map[string]interface{}{bc}

	// Exactly the appclient invoke request (requestId 8756).
	req := map[string]interface{}{
		"action": "invoke",
		"path":   moveSeatPath,
		"input": map[string]interface{}{
			"MovementType": "longitudinal",
			"Position":     "40",
		},
		"filter": map[string]interface{}{
			"variant":   "timebased",
			"parameter": map[string]interface{}{"period": "250"},
		},
		"requestId":   "8756",
		"routerIndex": 0,
	}

	HandleInvoke(req, bcs)

	select {
	case resp := <-bc:
		if e, ok := resp["error"]; ok {
			t.Fatalf("invoke returned an error response: %v", e)
		}
		if resp["action"] != "invoke" {
			t.Errorf("action = %v, want invoke", resp["action"])
		}
		if resp["status"] != string(StatusOngoing) {
			t.Errorf("status = %v, want ONGOING", resp["status"])
		}
		if resp["requestId"] != "8756" {
			t.Errorf("requestId = %v, want 8756", resp["requestId"])
		}
		if _, ok := resp["serviceId"].(string); !ok {
			t.Error("expected a serviceId for the monitoring session")
		}
	default:
		t.Fatal("HandleInvoke produced no response")
	}
}

// TestHandleInvoke_MonitoringEventCarriesRouterId is the regression for the
// follow-up bug found after the path-resolution fix landed: the invoke
// response routed fine (copyRouteFields), but the asynchronous "monitoring"
// events emitted by the timebased ticker carried no RouterId. The transport
// managers could not recover a clientId from them — wsMgr's RemoveInternalData
// sliced response[-2:] and panicked the whole server. The monitoring session
// must now remember the originating RouterId and stamp it onto every event.
func TestHandleInvoke_MonitoringEventCarriesRouterId(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	bc := make(chan map[string]interface{}, 16)
	bcs := []chan map[string]interface{}{bc}

	const routerId = "1?0"
	req := map[string]interface{}{
		"action": "invoke",
		"path":   moveSeatPath,
		"input": map[string]interface{}{
			"MovementType": "longitudinal",
			"Position":     "40",
		},
		"filter": map[string]interface{}{
			"variant":   "timebased",
			"parameter": map[string]interface{}{"period": "50"}, // fast ticker for the test
		},
		"requestId":   "8756",
		"routerIndex": 0,
		"RouterId":    routerId,
	}

	HandleInvoke(req, bcs)

	// The synchronous invoke ACK must carry the RouterId (copyRouteFields).
	select {
	case resp := <-bc:
		if resp["action"] != "invoke" {
			t.Fatalf("first message action = %v, want invoke ACK", resp["action"])
		}
		if resp["RouterId"] != routerId {
			t.Errorf("invoke ACK RouterId = %v, want %q", resp["RouterId"], routerId)
		}
	case <-time.After(time.Second):
		t.Fatal("HandleInvoke produced no invoke ACK")
	}

	// The asynchronous monitoring event(s) from the ticker must also carry it,
	// otherwise the transport manager drops them (or, pre-fix, panicked).
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-bc:
			if ev["action"] != "monitoring" {
				continue
			}
			if ev["RouterId"] != routerId {
				t.Fatalf("monitoring event RouterId = %v, want %q", ev["RouterId"], routerId)
			}
			return // success: routed monitoring event observed
		case <-deadline:
			t.Fatal("no monitoring event observed within 2s")
		}
	}
}

func TestHandleDiscover_BranchPath_ReturnsMetadata(t *testing.T) {
	resetState()
	loadVehicleServiceTree(t)

	bc := make(chan map[string]interface{}, 4)
	req := map[string]interface{}{
		"action":    "discover",
		"path":      "VehicleService.Seating",
		"requestId": "5687",
	}

	HandleDiscover(req, bc)

	select {
	case resp := <-bc:
		if e, ok := resp["error"]; ok {
			t.Fatalf("discover returned an error response: %v", e)
		}
		meta, ok := resp["metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("discover metadata missing/!map: %T", resp["metadata"])
		}
		if len(meta) == 0 {
			t.Error("discover metadata is empty for a populated branch")
		}
	default:
		t.Fatal("HandleDiscover produced no response")
	}
}

func TestValidateInputSignature_OptionalCredentialsMayBeOmitted(t *testing.T) {
	loadVehicleServiceTree(t)

	proc := resolveServiceNode(moveSeatPath)
	if proc == nil {
		t.Fatal("could not resolve MoveSeat procedure")
	}

	// Ulf's input supplies MovementType + Position but omits the optional
	// Credentials parameter.
	ok, missing := validateInputSignature(proc, map[string]interface{}{
		"MovementType": "longitudinal",
		"Position":     "40",
	})
	if !ok {
		t.Errorf("expected valid input (Credentials is optional); missing=%v", missing)
	}
}
