/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* More "tier 2" real-socket WebSocket tests, building on
* ws_e2e_delivery_test.go. These cover behaviour that only shows up with the
* genuine upgrade handler and multiple live connections:
*   - per-client routing isolation (a push for one client must not reach
*     another), and
*   - the max-clients rejection branch of the upgrade handler.
**/
package utils

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Integration-only entry points (not unit-tested here): WsServer.InitClientServer
// and HttpServer.InitClientServer bind fixed ports (:8080 / :8081) and call
// Error.Fatal on exit, so they cannot run under `go test`. The tier-2 tests
// instead mount the handler each returns (via makeappClientHandler) on an
// httptest.Server, which exercises every line of the request/response and
// server-push delivery path that those two functions wrap. The only remaining
// uncovered lines in the delivery functions are write-error and upgrade-error
// branches that require a mid-flight broken socket to reach.

func freeAllWsSlots() {
	WsClientIndexMu.Lock()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	WsClientIndexMu.Unlock()
}

func occupyAllWsSlots() {
	WsClientIndexMu.Lock()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = false
	}
	WsClientIndexMu.Unlock()
}

// newWsTestServer mounts the real app-client upgrade handler on an
// httptest.Server using the supplied per-client channels.
func newWsTestServer(t *testing.T, appClientChannel, clientBackendChannel []chan string) *httptest.Server {
	t.Helper()
	var clientIndex int
	handler := WsChannel{
		clientBackendChannel: clientBackendChannel,
		mgrIndex:             1,
		clientIndex:          &clientIndex,
	}.makeappClientHandler(appClientChannel)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

func dialWs(t *testing.T, srv *httptest.Server) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	dialer := websocket.Dialer{Subprotocols: []string{"VISS-noenc"}}
	return dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
}

// awaitWsConnCleanup closes a WS connection and blocks until its client slot is
// returned to the free list. On close the frontend goroutine emits its shutdown
// handshake (kill-subscriptions + backendTermination) and then calls
// ReturnWsClientIndex; draining the per-client channels lets that complete. Without
// this wait the slot would be freed asynchronously and could re-appear as free
// in the middle of a later test, breaking slot-allocation assumptions.
func awaitWsConnCleanup(conn *websocket.Conn, appChan, backendChan chan string, slot int) {
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-appChan:
			case <-backendChan:
			case <-stop:
				return
			}
		}
	}()
	conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		WsClientIndexMu.Lock()
		free := WsClientIndexList[slot]
		WsClientIndexMu.Unlock()
		if free || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(stop)
}

// wsConfirmOnSlot completes one request/response handshake against the given
// per-client hub channel, proving the just-connected client is bound to that
// slot before we connect the next one (the upgrade handler assigns slots in
// connection order from the free list).
func wsConfirmOnSlot(t *testing.T, conn *websocket.Conn, hubChan chan string, reqId string) {
	t.Helper()
	go func() {
		<-hubChan // request forwarded by this client's frontend goroutine
		hubChan <- `{"action":"get","requestId":"` + reqId + `","data":{}}`
	}()
	if err := conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"action":"get","path":"Vehicle.Speed","requestId":"`+reqId+`"}`)); err != nil {
		t.Fatalf("client write: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("handshake read (reqId %s): %v", reqId, err)
	}
	if !strings.Contains(string(msg), `"requestId":"`+reqId+`"`) {
		t.Fatalf("handshake got %q; want requestId %s", msg, reqId)
	}
}

// Two clients connected at once: an async push placed on one client's backend
// channel must be delivered to that client's socket only.
func TestWsClientServer_RealSocketMultiClientRoutingIsolation(t *testing.T) {
	slots := len(WsClientIndexList)
	appClientChannel := make([]chan string, slots)
	clientBackendChannel := make([]chan string, slots)
	for i := 0; i < slots; i++ {
		appClientChannel[i] = make(chan string)
		clientBackendChannel[i] = make(chan string)
	}
	freeAllWsSlots()
	srv := newWsTestServer(t, appClientChannel, clientBackendChannel)

	// Client A connects first -> slot 0; confirm before connecting B.
	connA, _, err := dialWs(t, srv)
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	t.Cleanup(func() { awaitWsConnCleanup(connA, appClientChannel[0], clientBackendChannel[0], 0) })
	wsConfirmOnSlot(t, connA, appClientChannel[0], "A")

	// Client B connects second -> slot 1.
	connB, _, err := dialWs(t, srv)
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	t.Cleanup(func() { awaitWsConnCleanup(connB, appClientChannel[1], clientBackendChannel[1], 1) })
	wsConfirmOnSlot(t, connB, appClientChannel[1], "B")

	// Push a distinct monitoring event onto each client's backend channel.
	// (We assert isolation by content rather than by a read-timeout: in
	// gorilla/websocket an exceeded read deadline corrupts the connection, so a
	// "should not arrive" check can't reuse the socket afterwards.)
	go func() { clientBackendChannel[0] <- `{"action":"monitoring","serviceId":"for-A","status":"ONGOING"}` }()
	go func() { clientBackendChannel[1] <- `{"action":"monitoring","serviceId":"for-B","status":"ONGOING"}` }()

	connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, msg, err := connA.ReadMessage(); err != nil {
		t.Fatalf("client A did not receive its event: %v", err)
	} else if s := string(msg); !strings.Contains(s, "for-A") || strings.Contains(s, "for-B") {
		t.Fatalf("client A got %q; want only the for-A event", s)
	}

	connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, msg, err := connB.ReadMessage(); err != nil {
		t.Fatalf("client B did not receive its event: %v", err)
	} else if s := string(msg); !strings.Contains(s, "for-B") || strings.Contains(s, "for-A") {
		t.Fatalf("client B got %q; want only the for-B event", s)
	}
}

// With every client slot occupied, getWsClientIndex returns -1 and the upgrade
// handler must reject the connection (no WebSocket upgrade), exercising the
// max-clients branch.
func TestWsClientServer_RealSocketMaxClientsRejected(t *testing.T) {
	slots := len(WsClientIndexList)
	appClientChannel := make([]chan string, slots)
	clientBackendChannel := make([]chan string, slots)
	for i := 0; i < slots; i++ {
		appClientChannel[i] = make(chan string)
		clientBackendChannel[i] = make(chan string)
	}
	occupyAllWsSlots()
	defer freeAllWsSlots() // restore for any later test
	srv := newWsTestServer(t, appClientChannel, clientBackendChannel)

	conn, resp, err := dialWs(t, srv)
	if err == nil {
		conn.Close()
		t.Fatalf("dial succeeded but should have been rejected (all slots occupied)")
	}
	if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("connection was upgraded despite no free client slot")
	}
}

// A plain (non-upgrade) HTTP request to the WS handler must not be upgraded —
// it falls through to the "client must set up a Websocket session" branch.
func TestWsClientServer_RealSocketNonWebsocketRequestRejected(t *testing.T) {
	slots := len(WsClientIndexList)
	appClientChannel := make([]chan string, slots)
	clientBackendChannel := make([]chan string, slots)
	for i := 0; i < slots; i++ {
		appClientChannel[i] = make(chan string)
		clientBackendChannel[i] = make(chan string)
	}
	freeAllWsSlots()
	defer freeAllWsSlots() // this path allocates a slot without a frontend to return it
	srv := newWsTestServer(t, appClientChannel, clientBackendChannel)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("plain GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("plain HTTP request was upgraded to a websocket")
	}
}

// Connecting with the VISS-protoenc subprotocol selects PROTOBUF encoding in the
// upgrade handler. We only need the handshake to land on that branch.
func TestWsClientServer_RealSocketProtobufSubprotocolAccepted(t *testing.T) {
	slots := len(WsClientIndexList)
	appClientChannel := make([]chan string, slots)
	clientBackendChannel := make([]chan string, slots)
	for i := 0; i < slots; i++ {
		appClientChannel[i] = make(chan string)
		clientBackendChannel[i] = make(chan string)
	}
	freeAllWsSlots()
	srv := newWsTestServer(t, appClientChannel, clientBackendChannel)

	dialer := websocket.Dialer{Subprotocols: []string{"VISS-protoenc"}}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("protoenc dial: %v", err)
	}
	if got := conn.Subprotocol(); got != "VISS-protoenc" {
		t.Fatalf("negotiated subprotocol = %q; want VISS-protoenc", got)
	}
	_ = resp
	t.Cleanup(func() { awaitWsConnCleanup(conn, appClientChannel[0], clientBackendChannel[0], 0) })
}
