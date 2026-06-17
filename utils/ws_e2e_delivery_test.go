/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* End-to-end WebSocket delivery test ("tier 2"). Unlike the per-function
* dispatch tests, this one drives the REAL per-client WS machinery over a REAL
* socket: it mounts the actual app-client handler (makeappClientHandler ->
* Upgrader.Upgrade -> frontendWSAppSession + backendWSAppSession) on an
* httptest.Server and talks to it with a gorilla/websocket client — the same
* path a browser app-client (i.e. the way Ulf tests manually) exercises.
*
* It covers both delivery directions:
*   1. the synchronous request/response handshake (forwardWsRequest), and
*   2. the asynchronous server-push path (a message placed directly on
*      clientBackendChannel), which is how subscription notifications and
*      VISSv3.2 service "monitoring" events reach the client.
*
* The async-push leg is the regression coverage for the class of bug that was
* invisible to channel-only unit tests: a monitoring event must travel all the
* way out of a real socket, not merely onto a channel.
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

func TestWsClientServer_RealSocketDeliversSyncResponseAndAsyncPush(t *testing.T) {
	slots := len(WsClientIndexList)
	appClientChannel := make([]chan string, slots)
	clientBackendChannel := make([]chan string, slots)
	for i := 0; i < slots; i++ {
		appClientChannel[i] = make(chan string)
		clientBackendChannel[i] = make(chan string)
	}

	// Free the slot list so the (only) client connecting lands on slot 0
	// deterministically, independent of any earlier test.
	WsClientIndexMu.Lock()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	WsClientIndexMu.Unlock()
	const clientId = 0

	var clientIndex int
	handler := WsChannel{
		clientBackendChannel: clientBackendChannel,
		mgrIndex:             1,
		clientIndex:          &clientIndex,
	}.makeappClientHandler(appClientChannel)

	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dialer := websocket.Dialer{Subprotocols: []string{"VISS-noenc"}} // encoding = NONE (text frames)
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	// Close the connection and wait for its slot to be returned at test end, so
	// a lingering frontend goroutine can't free the slot during a later test.
	t.Cleanup(func() {
		awaitWsConnCleanup(conn, appClientChannel[clientId], clientBackendChannel[clientId], clientId)
	})

	// --- 1. Synchronous request/response over the real socket ---------------
	// Emulate the server core: read the request the frontend forwards, then send
	// the response back on the SAME per-client channel (forwardWsRequest's
	// rendezvous), which the frontend then pushes to the write-pump.
	go func() {
		<-appClientChannel[clientId] // request forwarded by frontendWSAppSession
		appClientChannel[clientId] <- `{"action":"get","requestId":"42","data":{"path":"Vehicle.Speed","dp":{"value":"5"}}}`
	}()

	if err := conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"action":"get","path":"Vehicle.Speed","requestId":"42"}`)); err != nil {
		t.Fatalf("client write: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, msg, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read sync response: %v", err)
	} else if !strings.Contains(string(msg), `"requestId":"42"`) {
		t.Fatalf("sync response = %q; want requestId 42", msg)
	}

	// --- 2. Asynchronous server-push stream over the real socket ------------
	// A monitoring event placed directly on clientBackendChannel must be written
	// out to the client socket with no request/response handshake. Push a stream
	// to mirror the timebased ticker and confirm none are lost.
	const events = 10
	event := `{"action":"monitoring","path":"VehicleService.Seating.Row1.DriverSide.MoveSeat","serviceId":"860818","status":"ONGOING","ts":"2026-06-17T14:29:37Z"}`
	go func() {
		for i := 0; i < events; i++ {
			clientBackendChannel[clientId] <- event
		}
	}()

	for i := 0; i < events; i++ {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read async push %d/%d: %v", i+1, events, err)
		}
		if !strings.Contains(string(msg), `"action":"monitoring"`) {
			t.Fatalf("async push %d = %q; want a monitoring event", i+1, msg)
		}
	}
}
