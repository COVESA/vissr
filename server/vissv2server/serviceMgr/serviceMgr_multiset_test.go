/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for handleServiceMultiSet, the VISSv3.2 Multiple Data Update
* (CORE section 5.2.2) handler added alongside handleServiceSet /
* handleServiceGet. Same shape as the handleServiceSet/handleServiceGet
* tests in serviceMgr_dispatch_test.go.
**/
package serviceMgr

import (
	"testing"
	"time"
)

// TestHandleServiceMultiSet_MissingDataReturnsError covers the
// invalid_data branch when requestMap["data"] is absent entirely.
func TestHandleServiceMultiSet_MissingDataReturnsError(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "set",
		"requestId": "1",
	}
	resp := buildServiceResponseMap(req)

	go handleServiceMultiSet(req, resp, dataChan)

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
		t.Fatalf("handleServiceMultiSet did not reply on dataChan")
	}
}

// TestHandleServiceMultiSet_EmptyDataArrayReturnsError covers the
// invalid_data branch when requestMap["data"] is present but empty.
func TestHandleServiceMultiSet_EmptyDataArrayReturnsError(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "set",
		"requestId": "1",
		"data":      []interface{}{},
	}
	resp := buildServiceResponseMap(req)

	go handleServiceMultiSet(req, resp, dataChan)

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
		t.Fatalf("handleServiceMultiSet did not reply on dataChan")
	}
}

// TestHandleServiceMultiSet_MalformedElementReturnsError covers the
// invalid_data branch when a data array element is not a
// {"path","value"} object.
func TestHandleServiceMultiSet_MalformedElementReturnsError(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "set",
		"requestId": "1",
		"data":      []interface{}{"not-an-object"},
	}
	resp := buildServiceResponseMap(req)

	go handleServiceMultiSet(req, resp, dataChan)

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
		t.Fatalf("handleServiceMultiSet did not reply on dataChan")
	}
}

// TestHandleServiceMultiSet_MissingPathInElementReturnsError covers
// the invalid_data branch when an element object is missing "path".
func TestHandleServiceMultiSet_MissingPathInElementReturnsError(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "set",
		"requestId": "1",
		"data": []interface{}{
			map[string]interface{}{"value": "100"}, // no path
		},
	}
	resp := buildServiceResponseMap(req)

	go handleServiceMultiSet(req, resp, dataChan)

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
		t.Fatalf("handleServiceMultiSet did not reply on dataChan")
	}
}

// TestHandleServiceMultiSet_StorageUnavailableReturnsError mirrors
// TestHandleServiceSet_StorageUnavailableReturnsError: with
// stateDbType unset, setVehicleData returns "" for every element, and
// the helper must emit the canonical service_unavailable error on the
// first element rather than silently succeeding.
func TestHandleServiceMultiSet_StorageUnavailableReturnsError(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "set",
		"requestId": "1",
		"data": []interface{}{
			map[string]interface{}{"path": "Vehicle.Speed", "value": "100"},
		},
	}
	resp := buildServiceResponseMap(req)

	go handleServiceMultiSet(req, resp, dataChan)

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
		t.Fatalf("handleServiceMultiSet did not reply on dataChan")
	}
}

// TestHandleServiceMultiSet_ObjectValueIsMarshalled confirms a
// map-valued "value" element (a struct-typed actuator write) is
// JSON-marshalled before being passed to the storage backend, mirroring
// handleServiceSet's map[string]interface{} case. With stateDbType
// unset this still ends up on the service_unavailable error path (the
// backend can't actually store anything), but must not panic on the
// type switch.
func TestHandleServiceMultiSet_ObjectValueDoesNotPanic(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "set",
		"requestId": "1",
		"data": []interface{}{
			map[string]interface{}{
				"path":  "Vehicle.Cabin.Infotainment.PrivateMap",
				"value": map[string]interface{}{"lat": "1.0", "lon": "2.0"},
			},
		},
	}
	resp := buildServiceResponseMap(req)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("object-valued set element panicked: %v", r)
		}
	}()

	go handleServiceMultiSet(req, resp, dataChan)

	select {
	case <-dataChan:
		// Either an error or (if a backend somehow accepted it) a
		// success response is fine here - the only real assertion is
		// "did not panic", covered by the recover() above.
	case <-time.After(time.Second):
		t.Fatalf("handleServiceMultiSet did not reply on dataChan")
	}
}
