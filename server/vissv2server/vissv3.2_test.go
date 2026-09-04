/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the VISSv3.2 features added on this branch:
*
*   - Discovery ("discover" action, CORE section 5.5): isDiscoverRequest,
*     handleDiscoverRequest (Signal Discovery + Forest Discovery), and the
*     preprocessHimYaml helper needed to make himJsonify actually parse
*     the flat viss.him block format.
*   - Multi-signal Get ("get" with a top-level "data" array of paths, CORE
*     section 5.1.1.2): isMultiSignalRequest, handleMultiGetRequest.
*   - Multi-signal Set ("set" with a top-level "data" array of
*     {"path","value"} objects, CORE section 5.2.2): handleMultiSetRequest.
*
* Follows the same style as vissv2server_dispatch_test.go /
* vissv2server_helpers_test.go: build a small in-memory or on-disk VSS
* tree fixture, drive the extracted helper directly, and assert on the
* map/error shape sent to backendChan / serviceDataChan.
**/
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
	"gopkg.in/yaml.v3"
)

// buildDiscoverTestForest writes a minimal binary VSS tree and a
// matching viss.him file to a temp directory, chdir's into it (so
// himJsonify's relative os.ReadFile("viss.him") finds it and
// InitForest's relative "local:" paths resolve), loads the forest via
// utils.InitForest, and returns a cleanup function that restores the
// original working directory and forest state. Callers must defer the
// returned function.
//
// The tree built is:
//
//	Vehicle (branch)
//	  Speed (sensor, float)
//	  Powertrain (branch)
//	    Transmission (branch)
//	      PerformanceMode (actuator, string)
func buildDiscoverTestForest(t *testing.T) func() {
	t.Helper()
	tmp := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	performanceMode := utils.NewSignalNode("PerformanceMode", utils.ACTUATOR, "string", "performance mode", "", "", "")
	speed := utils.NewSignalNode("Speed", utils.SENSOR, "float", "vehicle speed", "", "", "")
	transmission := utils.NewBranchNode("Transmission", performanceMode)
	powertrain := utils.NewBranchNode("Powertrain", transmission)
	root := utils.NewBranchNode("Vehicle", speed, powertrain)

	binPath := filepath.Join(tmp, "vehicle.binary")
	utils.VSSWriteTree(binPath, root)

	himContent := "HIM:\n" +
		"type: branch\n" +
		"description: Defines the set of trees that are managed as one virtual domain.\n" +
		"\n" +
		"HIM.Vehicle:\n" +
		"type: direct\n" +
		"domain: Vehicle.Car.Data\n" +
		"version: 1.0\n" +
		"local: vehicle.binary\n" +
		"description: Test vehicle tree.\n"
	himPath := filepath.Join(tmp, "viss.him")
	if err := os.WriteFile(himPath, []byte(himContent), 0644); err != nil {
		t.Fatalf("WriteFile viss.him failed: %v", err)
	}

	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	if ok := utils.InitForest("viss.him"); !ok {
		os.Chdir(origWd)
		t.Fatalf("InitForest failed")
	}
	return func() {
		os.Chdir(origWd)
	}
}

// ----------------------------------------------------------------------------
// preprocessHimYaml
// ----------------------------------------------------------------------------

func TestPreprocessHimYaml_NestsKeysUnderBlock(t *testing.T) {
	in := "HIM:\n" +
		"type: branch\n" +
		"description: top level\n" +
		"\n" +
		"HIM.Vehicle:\n" +
		"type: direct\n" +
		"domain: Vehicle.Car.Data\n" +
		"local: forest/vss.binary\n" +
		"#a comment line\n" +
		"description: the vehicle tree\n"
	out := preprocessHimYaml([]byte(in))

	var himMap map[string]interface{}
	if err := yaml.Unmarshal(out, &himMap); err != nil {
		t.Fatalf("preprocessHimYaml output did not parse as YAML: %v\noutput:\n%s", err, out)
	}
	him, ok := himMap["HIM"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing/invalid HIM key in %+v", himMap)
	}
	if him["type"] != "branch" {
		t.Errorf("HIM.type = %v; want branch", him["type"])
	}
	vehicle, ok := himMap["HIM.Vehicle"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing/invalid HIM.Vehicle key in %+v", himMap)
	}
	if vehicle["domain"] != "Vehicle.Car.Data" {
		t.Errorf("HIM.Vehicle.domain = %v; want Vehicle.Car.Data", vehicle["domain"])
	}
	if vehicle["local"] != "forest/vss.binary" {
		t.Errorf("HIM.Vehicle.local = %v; want forest/vss.binary", vehicle["local"])
	}
	if _, present := vehicle["#a comment line"]; present {
		t.Errorf("comment line leaked into parsed map: %+v", vehicle)
	}
}

// ----------------------------------------------------------------------------
// isDiscoverRequest / isMultiSignalRequest predicates
// ----------------------------------------------------------------------------

func TestIsDiscoverRequest(t *testing.T) {
	cases := []struct {
		action string
		want   bool
	}{
		{"discover", true},
		{"get", false},
		{"set", false},
		{"subscribe", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isDiscoverRequest(tc.action); got != tc.want {
			t.Errorf("isDiscoverRequest(%q) = %v; want %v", tc.action, got, tc.want)
		}
	}
}

func TestIsMultiSignalRequest(t *testing.T) {
	cases := []struct {
		name       string
		action     string
		requestMap map[string]interface{}
		want       bool
	}{
		{
			"multi-get: data array, no path",
			"get",
			map[string]interface{}{"data": []interface{}{"Vehicle.Speed"}},
			true,
		},
		{
			"multi-set: data array, no path",
			"set",
			map[string]interface{}{"data": []interface{}{map[string]interface{}{"path": "Vehicle.Speed", "value": "1"}}},
			true,
		},
		{
			"single get: path present, no data",
			"get",
			map[string]interface{}{"path": "Vehicle.Speed"},
			false,
		},
		{
			"path and data both present: single-path wins (path takes precedence)",
			"get",
			map[string]interface{}{"path": "Vehicle.Speed", "data": []interface{}{"Vehicle.Speed"}},
			false,
		},
		{
			"data present but not an array",
			"get",
			map[string]interface{}{"data": "not-an-array"},
			false,
		},
		{
			"discover action is never multi-signal",
			"discover",
			map[string]interface{}{"data": []interface{}{"Vehicle.Speed"}},
			false,
		},
		{
			"subscribe action is never multi-signal",
			"subscribe",
			map[string]interface{}{"data": []interface{}{"Vehicle.Speed"}},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMultiSignalRequest(tc.action, tc.requestMap); got != tc.want {
				t.Errorf("isMultiSignalRequest(%q, %+v) = %v; want %v", tc.action, tc.requestMap, got, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// handleDiscoverRequest - error paths that don't need a live forest
// ----------------------------------------------------------------------------

func TestHandleDiscoverRequest_MissingPath(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "discover", "depth": "0"}
	go handleDiscoverRequest(req, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for missing path; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestHandleDiscoverRequest_MissingDepth(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "discover", "path": "Vehicle"}
	go handleDiscoverRequest(req, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for missing depth; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestHandleDiscoverRequest_NonNumericDepth(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "discover", "path": "Vehicle", "depth": "not-a-number"}
	go handleDiscoverRequest(req, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for non-numeric depth; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestHandleDiscoverRequest_UnknownTreeRootReturnsBadRequest(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "discover", "path": "NoSuchTree", "depth": "0"}
	go handleDiscoverRequest(req, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for unknown tree root; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

// ----------------------------------------------------------------------------
// handleDiscoverRequest - Signal Discovery + Forest Discovery, live forest
// ----------------------------------------------------------------------------

func TestHandleDiscoverRequest_SignalDiscovery(t *testing.T) {
	cleanup := buildDiscoverTestForest(t)
	defer cleanup()
	initChannels()

	// Signal Discovery goes through synthesizeJsonTree, which (like the
	// pre-existing "metadata" filter variant) unconditionally consults
	// the ATS for a no-scope list via atsChannel[0]/atsChannelMu. Fake a
	// minimal ATS response so the call doesn't block forever.
	go func() {
		<-atsChannel[0]
		atsChannel[0] <- `{"paths":[]}`
	}()

	req := map[string]interface{}{"action": "discover", "path": "Vehicle.Powertrain", "depth": "0", "requestId": "1"}
	go handleDiscoverRequest(req, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] != nil {
			t.Fatalf("unexpected error response: %+v", got)
		}
		if _, present := got["path"]; present {
			t.Errorf("response must not retain the request's path field: %+v", got)
		}
		if _, present := got["depth"]; present {
			t.Errorf("response must not retain the request's depth field: %+v", got)
		}
		metadata, ok := got["metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("metadata field missing or not an object (double-encoded-string regression?): %+v", got)
		}
		if _, present := metadata["Powertrain"]; !present {
			t.Errorf("expected metadata to describe the Powertrain subtree: %+v", metadata)
		}
		if got["ts"] == nil || got["ts"] == "" {
			t.Errorf("expected non-empty ts in response: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not produce a response")
	}
}

func TestHandleDiscoverRequest_ForestDiscoveryWholeForest(t *testing.T) {
	cleanup := buildDiscoverTestForest(t)
	defer cleanup()
	initChannels()

	req := map[string]interface{}{"action": "discover", "path": "HIM", "depth": "0", "requestId": "2"}
	go handleDiscoverRequest(req, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] != nil {
			t.Fatalf("unexpected error response: %+v", got)
		}
		metadata, ok := got["metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("metadata field missing or not an object: %+v", got)
		}
		vehicleEntry, ok := metadata["HIM.Vehicle"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected forest metadata to contain a HIM.Vehicle entry: %+v", metadata)
		}
		if _, present := vehicleEntry["local"]; present {
			t.Errorf(`the "local" property must be excluded from forest discovery metadata: %+v`, vehicleEntry)
		}
		if vehicleEntry["domain"] != "Vehicle.Car.Data" {
			t.Errorf("HIM.Vehicle.domain = %v; want Vehicle.Car.Data", vehicleEntry["domain"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not produce a response")
	}
}

func TestHandleDiscoverRequest_ForestDiscoverySingleTree(t *testing.T) {
	cleanup := buildDiscoverTestForest(t)
	defer cleanup()
	initChannels()

	// A path of "HIM.<treename>" also performs Forest Discovery (CORE
	// section 5.5: "This request must have a path starting with the
	// root node name HIM which may be appended with one dot delimited
	// segment name to address a specific tree in the forest.")
	req := map[string]interface{}{"action": "discover", "path": "HIM.Vehicle", "depth": "0", "requestId": "3"}
	go handleDiscoverRequest(req, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] != nil {
			t.Fatalf("unexpected error response: %+v", got)
		}
		if _, ok := got["metadata"].(map[string]interface{}); !ok {
			t.Fatalf("metadata field missing or not an object: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not produce a response")
	}
}

// ----------------------------------------------------------------------------
// handleMultiGetRequest
// ----------------------------------------------------------------------------

func TestHandleMultiGetRequest_MissingDataReturnsError(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "get", "requestId": "1"}
	go handleMultiGetRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for missing data; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestHandleMultiGetRequest_EmptyDataArrayReturnsError(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "get", "data": []interface{}{}, "requestId": "1"}
	go handleMultiGetRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for empty data array; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestHandleMultiGetRequest_NonStringElementReturnsError(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "get", "data": []interface{}{42}, "requestId": "1"}
	go handleMultiGetRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for non-string data element; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestHandleMultiGetRequest_UnknownPathReturnsUnavailableData(t *testing.T) {
	cleanup := buildDiscoverTestForest(t)
	defer cleanup()
	initChannels()

	req := map[string]interface{}{"action": "get", "data": []interface{}{"Vehicle.Speed", "NoSuchTree.Foo"}, "requestId": "1"}
	go handleMultiGetRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response for unresolvable second path; got %+v", got)
		}
		if errMap["reason"] != "unavailable_data" {
			t.Errorf("error.reason = %v; want unavailable_data", errMap["reason"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not produce a response")
	}
}

func TestHandleMultiGetRequest_ResolvesAndForwardsToServiceMgr(t *testing.T) {
	cleanup := buildDiscoverTestForest(t)
	defer cleanup()
	initChannels()

	req := map[string]interface{}{
		"action":    "get",
		"data":      []interface{}{"Vehicle.Speed", "Vehicle.Powertrain.Transmission.PerformanceMode"},
		"requestId": "1",
	}
	go handleMultiGetRequest(req, 0, 0)
	select {
	case got := <-serviceDataChan[0]:
		if _, present := got["data"]; present {
			t.Errorf("data field should have been replaced by a resolved path array: %+v", got)
		}
		pathStr, ok := got["path"].(string)
		if !ok {
			t.Fatalf("expected requestMap[\"path\"] to be a resolved JSON array string; got %+v", got)
		}
		var paths []string
		if err := json.Unmarshal([]byte(pathStr), &paths); err != nil {
			t.Fatalf("resolved path field is not a JSON array of strings: %q, err=%v", pathStr, err)
		}
		if len(paths) != 2 {
			t.Fatalf("expected 2 resolved paths; got %v", paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not forward to serviceDataChan")
	}
}

// ----------------------------------------------------------------------------
// handleMultiSetRequest
// ----------------------------------------------------------------------------

func TestHandleMultiSetRequest_MissingDataReturnsError(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "set", "requestId": "1"}
	go handleMultiSetRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for missing data; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestHandleMultiSetRequest_ElementMissingValueReturnsError(t *testing.T) {
	cleanup := buildDiscoverTestForest(t)
	defer cleanup()
	initChannels()

	req := map[string]interface{}{
		"action": "set",
		"data": []interface{}{
			map[string]interface{}{"path": "Vehicle.Powertrain.Transmission.PerformanceMode"}, // no value
		},
		"requestId": "1",
	}
	go handleMultiSetRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for element missing value; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestHandleMultiSetRequest_SensorRejectedAsReadOnly(t *testing.T) {
	cleanup := buildDiscoverTestForest(t)
	defer cleanup()
	initChannels()

	req := map[string]interface{}{
		"action": "set",
		"data": []interface{}{
			map[string]interface{}{"path": "Vehicle.Speed", "value": "10"}, // Speed is a sensor, not an actuator
		},
		"requestId": "1",
	}
	go handleMultiSetRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response for sensor write; got %+v", got)
		}
		if errMap["reason"] != "invalid_data" {
			t.Errorf("error.reason = %v; want invalid_data", errMap["reason"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not produce a response")
	}
}

func TestHandleMultiSetRequest_ResolvesAndForwardsToServiceMgr(t *testing.T) {
	cleanup := buildDiscoverTestForest(t)
	defer cleanup()
	initChannels()

	req := map[string]interface{}{
		"action": "set",
		"data": []interface{}{
			map[string]interface{}{"path": "Vehicle.Powertrain.Transmission.PerformanceMode", "value": "sport"},
		},
		"requestId": "1",
	}
	go handleMultiSetRequest(req, 0, 0)
	select {
	case got := <-serviceDataChan[0]:
		dataArr, ok := got["data"].([]interface{})
		if !ok || len(dataArr) != 1 {
			t.Fatalf("expected resolved data array of length 1; got %+v", got)
		}
		elem, ok := dataArr[0].(map[string]interface{})
		if !ok {
			t.Fatalf("resolved data element is not a map: %+v", dataArr[0])
		}
		if elem["path"] != "Vehicle.Powertrain.Transmission.PerformanceMode" {
			t.Errorf("resolved path = %v; want Vehicle.Powertrain.Transmission.PerformanceMode", elem["path"])
		}
		if elem["value"] != "sport" {
			t.Errorf("resolved value = %v; want sport", elem["value"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not forward to serviceDataChan")
	}
}

// ----------------------------------------------------------------------------
// serveRequest dispatch - discover and multi-signal requests are routed
// to the right handler instead of falling through to issueServiceRequest.
// ----------------------------------------------------------------------------

func TestServeRequest_DiscoverRoutesToHandleDiscoverRequest(t *testing.T) {
	initChannels()
	// Missing depth -> handleDiscoverRequest's own validation fires and
	// forwards an error directly on backendChan, never touching
	// serviceDataChan. This proves serveRequest dispatched to the
	// discover handler and not issueServiceRequest (which would have
	// produced a different error, "missing/invalid path", middle of a
	// different code path, for a request with a path already set).
	req := map[string]interface{}{"action": "discover", "path": "Vehicle"}
	go serveRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response; got %+v", got)
		}
		desc, _ := errMap["description"].(string)
		if desc != "missing/invalid depth" {
			t.Errorf("error.description = %q; want %q (proves discover dispatch, not issueServiceRequest)", desc, "missing/invalid depth")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

func TestServeRequest_MultiGetRoutesToHandleMultiSignalRequest(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "get", "data": []interface{}{}}
	go serveRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response for empty multi-get data array; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}
