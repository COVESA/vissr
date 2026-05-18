/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the StorageBackend interface + adapter implementations
* introduced as the third follow-up to PR #129. Covers:
*
*   - noneBackend                        Get + Set
*   - redisBackend / memcacheBackend     Set (pushes to toFeeder)
*   - getVehicleData / setVehicleData    dispatch through stateBackend
*   - handleServiceSet with a successful backend
*     (the PR #129 test only covered the failure path because there
*     was no way to inject a working backend without spinning up a
*     real database)
*
* The Get arms of redisBackend, memcacheBackend, sqliteBackend, and
* iotdbBackend all require real database connections to test
* meaningfully, so those are left for integration testing rather than
* unit testing here.
**/
package serviceMgr

import (
	"strings"
	"testing"
	"time"
)

// fakeBackend is a test double implementing StorageBackend. Use it
// to verify that getVehicleData / setVehicleData dispatch correctly
// through the injected backend, and to give handleServiceSet a
// successful Set path that the production-default noneBackend can't
// provide.
type fakeBackend struct {
	getPath        string         // last path passed to Get
	getReturn      string         // what Get should return
	setPath        string         // last path passed to Set
	setValue       string         // last value passed to Set
	setReturn      string         // what Set should return ("" means failure)
}

func (f *fakeBackend) Get(path string) string {
	f.getPath = path
	return f.getReturn
}

func (f *fakeBackend) Set(path, value string) string {
	f.setPath = path
	f.setValue = value
	return f.setReturn
}

// withBackend swaps stateBackend in for the duration of fn and
// restores the prior value on return — important because the
// stateBackend global is process-wide and we run inside `go test`
// alongside the PR-#129 / #130 / #131 dispatch tests.
func withBackend(b StorageBackend, fn func()) {
	prev := stateBackend
	stateBackend = b
	defer func() { stateBackend = prev }()
	fn()
}

// TestNoneBackend_GetReturnsDummyValue exercises noneBackend.Get and
// confirms it returns the canonical {"value":"<N>","ts":"..."} shape
// using the dummyValue counter.
func TestNoneBackend_GetReturnsDummyValue(t *testing.T) {
	dummyValue = 42
	b := newNoneBackend()
	got := b.Get("Vehicle.Speed")
	if !strings.Contains(got, `"value":"42"`) {
		t.Errorf("noneBackend.Get = %q; want it to embed dummyValue=42", got)
	}
	if !strings.Contains(got, `"ts":"`) {
		t.Errorf("noneBackend.Get = %q; want it to embed a ts field", got)
	}
}

// TestNoneBackend_SetReturnsEmpty confirms the documented contract:
// Set is always a no-op-failure (returns "") for the none backend.
// handleServiceSet relies on this empty return to emit the
// service_unavailable response.
func TestNoneBackend_SetReturnsEmpty(t *testing.T) {
	b := newNoneBackend()
	if got := b.Set("Vehicle.Speed", "100"); got != "" {
		t.Errorf("noneBackend.Set = %q; want \"\"", got)
	}
}

// TestRedisBackend_SetPushesMessageToFeeder confirms the redis Set
// arm encodes the path+value into a JSON set message and pushes it
// onto the toFeeder channel (which is how redis writes get fanned
// out — see the original arm comment).
func TestRedisBackend_SetPushesMessageToFeeder(t *testing.T) {
	feeder := make(chan string, 1)
	b := newRedisBackend(nil, feeder) // client unused for Set path
	ts := b.Set("Vehicle.Speed", "100")
	if ts == "" {
		t.Fatalf("redisBackend.Set returned empty timestamp on success path")
	}
	select {
	case msg := <-feeder:
		if !strings.Contains(msg, `"path":"Vehicle.Speed"`) {
			t.Errorf("feeder message missing path: %q", msg)
		}
		if !strings.Contains(msg, `"value":"100"`) {
			t.Errorf("feeder message missing value: %q", msg)
		}
		if !strings.Contains(msg, `"action": "set"`) {
			t.Errorf("feeder message missing action=set: %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatalf("redisBackend.Set did not push to feeder")
	}
}

// TestMemcacheBackend_SetPushesMessageToFeeder — same as the redis
// case. The two backends share the Set protocol exactly; the original
// arms in setVehicleData even used `fallthrough` to share the body.
// This test pins that the duplication remains intentional.
func TestMemcacheBackend_SetPushesMessageToFeeder(t *testing.T) {
	feeder := make(chan string, 1)
	b := newMemcacheBackend(nil, feeder)
	ts := b.Set("Vehicle.Speed", "100")
	if ts == "" {
		t.Fatalf("memcacheBackend.Set returned empty timestamp on success path")
	}
	select {
	case msg := <-feeder:
		if !strings.Contains(msg, `"path":"Vehicle.Speed"`) || !strings.Contains(msg, `"value":"100"`) {
			t.Errorf("feeder message malformed: %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatalf("memcacheBackend.Set did not push to feeder")
	}
}

// TestGetVehicleData_DispatchesToStateBackend confirms the thunk-form
// getVehicleData routes through the currently-installed stateBackend
// — the whole point of the refactor.
func TestGetVehicleData_DispatchesToStateBackend(t *testing.T) {
	fake := &fakeBackend{getReturn: `{"value":"sentinel","ts":"2026-01-01T00:00:00Z"}`}
	withBackend(fake, func() {
		got := getVehicleData("Vehicle.Speed")
		if got != fake.getReturn {
			t.Errorf("getVehicleData = %q; want it to passthrough the fake backend's return", got)
		}
		if fake.getPath != "Vehicle.Speed" {
			t.Errorf("fake backend got path = %q; want Vehicle.Speed", fake.getPath)
		}
	})
}

// TestSetVehicleData_DispatchesToStateBackend — same property for the
// Set side.
func TestSetVehicleData_DispatchesToStateBackend(t *testing.T) {
	fake := &fakeBackend{setReturn: "2026-01-01T00:00:00Z"}
	withBackend(fake, func() {
		got := setVehicleData("Vehicle.Speed", "100")
		if got != fake.setReturn {
			t.Errorf("setVehicleData = %q; want %q", got, fake.setReturn)
		}
		if fake.setPath != "Vehicle.Speed" || fake.setValue != "100" {
			t.Errorf("fake backend recorded path=%q, value=%q; want Vehicle.Speed / 100", fake.setPath, fake.setValue)
		}
	})
}

// TestHandleServiceSet_WithSuccessfulBackend exercises the success
// path of handleServiceSet — previously uncovered because the test
// suite (PR #129) had no way to inject a working backend. With the
// interface in place, we substitute a fakeBackend that returns a real
// timestamp on Set and confirm handleServiceSet emits the success
// response rather than the service_unavailable error.
func TestHandleServiceSet_WithSuccessfulBackend(t *testing.T) {
	fake := &fakeBackend{setReturn: "2026-01-01T00:00:00Z"}
	withBackend(fake, func() {
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
			if _, isErr := got["error"]; isErr {
				t.Fatalf("expected success response; got error %v", got)
			}
			if got["ts"] != "2026-01-01T00:00:00Z" {
				t.Errorf("ts = %v; want the backend's returned ts", got["ts"])
			}
		case <-time.After(time.Second):
			t.Fatalf("handleServiceSet did not reply on dataChan")
		}
	})

	// The fakeBackend should have seen the request.
	if fake.setPath != "Vehicle.Speed" {
		t.Errorf("backend got setPath=%q; want Vehicle.Speed", fake.setPath)
	}
	if fake.setValue != "100" {
		t.Errorf("backend got setValue=%q; want 100", fake.setValue)
	}
}

// TestHandleServiceSet_WithFailingBackend pins the existing failure
// behaviour — a backend that returns "" from Set still triggers
// service_unavailable. This duplicates a check from
// serviceMgr_dispatch_test.go but expressed in terms of the new
// interface so the connection between Set returning "" and the
// service_unavailable response is explicit.
func TestHandleServiceSet_WithFailingBackend(t *testing.T) {
	fake := &fakeBackend{setReturn: ""}
	withBackend(fake, func() {
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
			if !ok || errMap["reason"] != "service_unavailable" {
				t.Errorf("expected service_unavailable error; got %v", got)
			}
		case <-time.After(time.Second):
			t.Fatalf("handleServiceSet did not reply on dataChan")
		}
	})
}
