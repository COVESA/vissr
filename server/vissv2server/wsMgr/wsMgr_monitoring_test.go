/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Regression tests for the routing of VISSv3.2 service "monitoring" events.
*
* Background: a WS app-client talks to the hub over two per-client channels.
* wsClientChan is a synchronous request/response rendezvous — the frontend
* goroutine only reads it in the window between sending a request and receiving
* that request's response (forwardWsRequest). clientBackendChan is the
* asynchronous write-pump channel that backendWSAppSession drains continuously.
*
* Service monitoring events are unsolicited server pushes that arrive long
* after the invoke handshake has completed, when the frontend is parked on
* conn.ReadMessage() and is NOT reading wsClientChan. Routing them to the
* synchronous wsClientChan therefore blocked the hub on an unbuffered send,
* which backed up the transport channel and produced the "server hub: Event
* dropped" stream Ulf reported after PR #187. They must go to the async
* clientBackendChan, like subscription notifications.
**/
package wsMgr

import (
	"strings"
	"testing"
	"time"
)

// A monitoring event (with a RouterId so RemoveInternalData yields a valid
// clientId) must be delivered to the asynchronous clientBackendChan and never
// to the synchronous wsClientChan.
func TestRemoveRoutingForwardResponse_MonitoringRoutedToAsyncChannel(t *testing.T) {
	// Buffer the async channel so the non-blocking send always lands without a
	// racing reader, then restore the package global.
	saved := clientBackendChan[0]
	clientBackendChan[0] = make(chan string, 4)
	defer func() { clientBackendChan[0] = saved }()

	// Exact shape from Ulf's log, with the internal RouterId prefix.
	resp := `{"RouterId":"0?0","action":"monitoring","path":"VehicleService.Seating.Row1.DriverSide.MoveSeat","serviceId":"860818","status":"ONGOING","ts":"2026-06-17T14:29:37Z"}`

	RemoveRoutingForwardResponse(resp, nil)

	select {
	case got := <-clientBackendChan[0]:
		if !strings.Contains(got, "\"action\":\"monitoring\"") {
			t.Fatalf("async channel got %q; want the monitoring event", got)
		}
		if strings.Contains(got, "RouterId") {
			t.Fatalf("internal RouterId leaked to client: %q", got)
		}
	default:
		t.Fatal("monitoring event was not routed to the asynchronous clientBackendChan")
	}

	// It must NOT have been sent to the synchronous request/response channel.
	select {
	case leaked := <-wsClientChan[0]:
		t.Fatalf("monitoring event leaked onto synchronous wsClientChan: %q", leaked)
	default:
	}
}

// Reproduces Ulf's wedge directly: with NO reader on the synchronous
// wsClientChan (the frontend is parked on conn.ReadMessage after the invoke
// handshake), a stream of monitoring events must still be delivered via the
// async channel and must never block the caller (the hub). On the pre-fix code
// the first event blocked forever on the unbuffered wsClientChan send, the hub
// stopped draining respChan, and transportDataSession logged "server hub: Event
// dropped".
func TestRemoveRoutingForwardResponse_NoHubWedgeOnMonitoringStream(t *testing.T) {
	const events = 50
	saved := clientBackendChan[0]
	clientBackendChan[0] = make(chan string, events)
	defer func() { clientBackendChan[0] = saved }()

	// Intentionally NO reader on wsClientChan[0].
	done := make(chan struct{})
	go func() {
		for i := 0; i < events; i++ {
			resp := `{"RouterId":"0?0","action":"monitoring","path":"VehicleService.Seating.Row1.DriverSide.MoveSeat","serviceId":"860818","status":"ONGOING","ts":"2026-06-17T14:29:37Z"}`
			RemoveRoutingForwardResponse(resp, nil)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hub wedged: RemoveRoutingForwardResponse blocked delivering a monitoring event")
	}

	if got := len(clientBackendChan[0]); got != events {
		t.Fatalf("delivered %d/%d monitoring events to the async channel", got, events)
	}
}

// Sanity check that the routing predicate stays correct for the messages that
// must keep using each path.
func TestIsAsyncServerPush(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"monitoring event", `{"action":"monitoring","status":"ONGOING"}`, true},
		{"subscription notification", `{"action":"subscribe","subscription":"246","data":{}}`, true},
		{"invoke ACK (solicited)", `{"action":"invoke","status":"ONGOING","requestId":"8756"}`, false},
		{"monitor ACK (solicited)", `{"action":"monitor","status":"ONGOING","requestId":"8756"}`, false},
		{"subscribe ACK (solicited)", `{"action":"subscribe","subscriptionId":"246","requestId":"1"}`, false},
		{"get response", `{"action":"get","data":{"path":"Vehicle.Speed","dp":{"value":"5"}}}`, false},
	}
	for _, c := range cases {
		if got := isAsyncServerPush(c.msg); got != c.want {
			t.Errorf("%s: isAsyncServerPush=%v; want %v", c.name, got, c.want)
		}
	}
}
