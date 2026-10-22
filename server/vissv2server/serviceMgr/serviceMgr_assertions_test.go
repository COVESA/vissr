/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests that the request handlers of the service manager answer a malformed request with an error
* response instead of panicking. The server has no panic recovery, so a panic in the service manager
* ends the whole server. The core hands the handlers requests that it has checked, but the handlers
* must not depend on that: a field that is missing or of another type is an error response.
**/
package serviceMgr

import (
	"testing"
	"time"
)

// noPanic runs f and fails the test if it panics or does not return.
func noPanic(t *testing.T, f func()) {
	t.Helper()
	done := make(chan interface{}, 1)
	go func() {
		defer func() { done <- recover() }()
		f()
	}()
	select {
	case r := <-done:
		if r != nil {
			t.Fatalf("panicked: %v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not return")
	}
}

// isErrorResponse reports whether got is an error response, and returns its reason.
func isErrorResponse(got map[string]interface{}) (bool, string) {
	errMap, ok := got["error"].(map[string]interface{})
	if !ok {
		return false, ""
	}
	reason, _ := errMap["reason"].(string)
	return true, reason
}

func expectError(t *testing.T, dataChan chan map[string]interface{}) {
	t.Helper()
	select {
	case got := <-dataChan:
		if ok, _ := isErrorResponse(got); !ok {
			t.Errorf("want an error response; got %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Error("no response was sent")
	}
}

func expectNothing(t *testing.T, dataChan chan map[string]interface{}) {
	t.Helper()
	select {
	case got := <-dataChan:
		t.Errorf("a response was sent: %v", got)
	default:
	}
}

func newDataChan() chan map[string]interface{} { return make(chan map[string]interface{}, 4) }

func TestHandleServiceSet_MalformedRequestIsAnErrorResponse(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"path missing":                       {"value": "1"},
		"path not a string":                  {"path": 5.0, "value": "1"},
		"path null":                          {"path": nil, "value": "1"},
		"value missing":                      {"path": "Vehicle.X"},
		"value a number":                     {"path": "Vehicle.X", "value": 5.0},
		"value a bool":                       {"path": "Vehicle.X", "value": true},
		"value a list":                       {"path": "Vehicle.X", "value": []interface{}{"a", "b"}},
		"value null":                         {"path": "Vehicle.X", "value": nil},
		"path not a string, value an object": {"path": []interface{}{"a"}, "value": map[string]interface{}{"a": "b"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			resetErrorResponseMap()
			dc := newDataChan()
			noPanic(t, func() { handleServiceSet(req, map[string]interface{}{}, dc) })
			expectError(t, dc)
		})
	}
}

func TestHandleServiceGet_MalformedRequestIsAnErrorResponse(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"path missing":      {},
		"path not a string": {"path": 7.0},
		"path a list":       {"path": []interface{}{"Vehicle.Speed"}},
		"path null":         {"path": nil},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			resetErrorResponseMap()
			dc := newDataChan()
			noPanic(t, func() { handleServiceGet(req, map[string]interface{}{}, dc) })
			expectError(t, dc)
		})
	}
}

func subscribeWith(req map[string]interface{}, dc chan map[string]interface{}) {
	handleServiceSubscribe(req, map[string]interface{}{}, dc, nil, 0, make(chan int, 1), make(chan CLPack, 1), make(chan string, 4))
}

func TestHandleServiceSubscribe_MalformedRequestIsAnErrorResponse(t *testing.T) {
	filter := []interface{}{map[string]interface{}{"variant": "timebased", "parameter": `{"period":"1000"}`}}
	cases := map[string]map[string]interface{}{
		"RouterId missing":      {"path": `"Vehicle.Speed"`, "filter": filter},
		"RouterId not a string": {"RouterId": 5.0, "path": `"Vehicle.Speed"`, "filter": filter},
		"path missing":          {"RouterId": "0?0", "filter": filter},
		"path not a string":     {"RouterId": "0?0", "path": 5.0, "filter": filter},
		"gatingId a number":     {"RouterId": "0?0", "path": `"Vehicle.Speed"`, "filter": filter, "gatingId": 5.0},
		"gatingId a list":       {"RouterId": "0?0", "path": `"Vehicle.Speed"`, "filter": filter, "gatingId": []interface{}{"a"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			resetErrorResponseMap()
			dc := newDataChan()
			noPanic(t, func() { subscribeWith(req, dc) })
			expectError(t, dc)
		})
	}
}

// A subscription whose request was rejected must not be left in the list.
func TestHandleServiceSubscribe_RejectedRequestIsNotRecorded(t *testing.T) {
	resetErrorResponseMap()
	dc := newDataChan()
	var list []SubscriptionState
	noPanic(t, func() {
		list, _ = handleServiceSubscribe(map[string]interface{}{"RouterId": "0?0", "path": `"Vehicle.Speed"`, "gatingId": 5.0,
			"filter": []interface{}{map[string]interface{}{"variant": "timebased", "parameter": `{"period":"1000"}`}}},
			map[string]interface{}{}, dc, nil, 0, make(chan int, 1), make(chan CLPack, 1), make(chan string, 4))
	})
	if len(list) != 0 {
		t.Errorf("a subscription was recorded for a rejected request: %+v", list)
	}
}

func TestHandleInternalKillSubscriptions_MalformedMessageIsIgnored(t *testing.T) {
	list := []SubscriptionState{{SubscriptionId: 1, RouterId: "0?0"}}
	for name, req := range map[string]map[string]interface{}{
		"RouterId missing":      {"action": "internal-killsubscriptions"},
		"RouterId not a string": {"action": "internal-killsubscriptions", "RouterId": 3.0},
		"RouterId null":         {"action": "internal-killsubscriptions", "RouterId": nil},
	} {
		t.Run(name, func(t *testing.T) {
			var got []SubscriptionState
			noPanic(t, func() { got = handleInternalKillSubscriptions(req, list) })
			if len(got) != 1 {
				t.Errorf("subscriptions = %d; want the list unchanged (1)", len(got))
			}
		})
	}
}

// Nothing is sent for a kill message, as before: it has no response.
func TestHandleInternalKillSubscriptions_ValidMessageStillRemovesTheSubscriptions(t *testing.T) {
	list := []SubscriptionState{{SubscriptionId: 1, RouterId: "0?0"}, {SubscriptionId: 2, RouterId: "0?1"}, {SubscriptionId: 3, RouterId: "0?0"}}
	got := handleInternalKillSubscriptions(map[string]interface{}{"action": "internal-killsubscriptions", "RouterId": "0?0"}, list)
	if len(got) != 1 || got[0].RouterId != "0?1" {
		t.Errorf("subscriptions after the kill = %+v; want only the one of 0?1", got)
	}
}

func TestHandleInternalCancelSubscription_MalformedMessageIsIgnored(t *testing.T) {
	list := []SubscriptionState{{SubscriptionId: 1, RouterId: "0?0", GatingId: "g1"}}
	for name, req := range map[string]map[string]interface{}{
		"gatingId missing":      {"action": "internal-cancelsubscription"},
		"gatingId not a string": {"action": "internal-cancelsubscription", "gatingId": 5.0},
		"gatingId null":         {"action": "internal-cancelsubscription", "gatingId": nil},
	} {
		t.Run(name, func(t *testing.T) {
			resetErrorResponseMap()
			dc := newDataChan()
			var got []SubscriptionState
			noPanic(t, func() { got = handleInternalCancelSubscription(req, dc, list) })
			if len(got) != 1 {
				t.Errorf("subscriptions = %d; want the list unchanged (1)", len(got))
			}
			expectNothing(t, dc)
		})
	}
}

func TestHandleInternalCancelSubscription_ValidMessageStillCancels(t *testing.T) {
	resetErrorResponseMap()
	list := []SubscriptionState{{SubscriptionId: 4, RouterId: "0?2", GatingId: "g9"}, {SubscriptionId: 5, RouterId: "0?3", GatingId: "g10"}}
	dc := newDataChan()
	got := handleInternalCancelSubscription(map[string]interface{}{"action": "internal-cancelsubscription", "gatingId": "g9"}, dc, list)
	if len(got) != 1 || got[0].RouterId != "0?3" {
		t.Errorf("subscriptions after the cancel = %+v; want only the one of 0?3", got)
	}
	select {
	case resp := <-dc:
		if ok, _ := isErrorResponse(resp); !ok || resp["RouterId"] != "0?2" {
			t.Errorf("response = %v; want an error event for RouterId 0?2", resp)
		}
	case <-time.After(2 * time.Second):
		t.Error("the client was not told that its subscription was cancelled")
	}
}

func TestHandleServiceUnsubscribe_NonStringSubscriptionIdIsAnErrorResponse(t *testing.T) {
	for name, id := range map[string]interface{}{"number": 5.0, "bool": true, "list": []interface{}{"1"}, "null": nil} {
		t.Run(name, func(t *testing.T) {
			resetErrorResponseMap()
			dc := newDataChan()
			noPanic(t, func() {
				handleServiceUnsubscribe(map[string]interface{}{"subscriptionId": id}, map[string]interface{}{}, dc, nil, make(chan string, 2))
			})
			expectError(t, dc)
		})
	}
}

// Valid requests still work.
func TestHandleServiceSet_ValidRequestsStillWork(t *testing.T) {
	for name, value := range map[string]interface{}{"string": "1", "object": map[string]interface{}{"a": "b"}} {
		t.Run(name, func(t *testing.T) {
			resetErrorResponseMap()
			dc := newDataChan()
			noPanic(t, func() {
				handleServiceSet(map[string]interface{}{"path": "Vehicle.X", "value": value}, map[string]interface{}{}, dc)
			})
			select {
			case <-dc: // a response of some kind (success, or service_unavailable when no state storage is configured)
			case <-time.After(2 * time.Second):
				t.Error("no response")
			}
		})
	}
}

// A ticker slot that was allocated but has no ticker must not make deactivateInterval panic.
func TestDeactivateInterval_SlotWithoutTickerDoesNotPanic(t *testing.T) {
	resetTickerGlobals()
	defer resetTickerGlobals()
	if idx := allocateTicker(4242); idx < 0 {
		t.Fatal("no free ticker slot")
	}
	noPanic(t, func() { deactivateInterval(4242) })
}
