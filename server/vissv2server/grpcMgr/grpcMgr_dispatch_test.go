/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the per-message dispatch helpers extracted from grpcMgr in
* PR #127 (this PR):
*
*   - dispatchGrpcUnaryRequest   (the shared GetRequest/SetRequest/
*                                  UnsubscribeRequest handshake)
*   - classifySubscribeResponse  (error vs kill-message vs normal event)
*   - isMultipleEventsRequest    (subscribe vs unsubscribe vs one-shot)
*   - handleGrpcTransportResponse (response arm of GrpcMgrInit)
*   - handleGrpcNewClientSession (request arm of GrpcMgrInit, including
*                                  the max-clients short-circuit)
*   - extractClientId             (parses "kill subscription clientId:N")
*
* Same shape as the udsMgr / httpMgr / wsMgr dispatch tests landed in
* PRs #124 and #126.
**/
package grpcMgr

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises utils.Info / utils.Error and the package-level
// state used by the helpers (grpcClientIndexList, grpcRoutingDataList)
// so the dispatch functions don't nil-deref under test conditions.
func TestMain(m *testing.M) {
	utils.InitLog("grpcMgr-dispatch-test.log", os.TempDir(), false, "error")
	grpcClientIndexList = make([]bool, MAXGRPCCLIENTS)
	grpcRoutingDataList = make([]GrpcRoutingData, MAXGRPCCLIENTS)
	resetGrpcStateForTest()
	os.Exit(m.Run())
}

// resetGrpcStateForTest puts the package-level routing/client-id
// tables back into a known-clean state. Called by tests that mutate
// them so they don't leak state into each other.
func resetGrpcStateForTest() {
	initClientIdList()
	iniGrpcRoutingDataList()
}

// TestExtractClientId pins the parsing of the "kill subscription
// clientId:N" message the unsubscribe thread sends to the subscribe
// thread. extractClientId is one line but the message format is the
// contract between two goroutines, so a regression here would be
// silent.
func TestExtractClientId(t *testing.T) {
	cases := []struct {
		msg  string
		want int
	}{
		{"kill subscription clientId:5", 5},
		{"kill subscription clientId:42", 42},
		{"kill subscription clientId:0", 0},
		{"x:99", 99},
	}
	for _, tc := range cases {
		if got := extractClientId(tc.msg); got != tc.want {
			t.Errorf("extractClientId(%q) = %d; want %d", tc.msg, got, tc.want)
		}
	}
}

// TestIsMultipleEventsRequest covers the predicate that decides
// whether a request will produce a stream of events (active
// subscribe) versus a one-shot response. The tricky case is
// "unsubscribe" — it contains "subscribe" as a substring and must
// still return false.
func TestIsMultipleEventsRequest(t *testing.T) {
	cases := []struct {
		req  string
		want bool
	}{
		{`{"action":"subscribe","path":"Vehicle.Speed","requestId":"1"}`, true},
		{`{"action":"unsubscribe","subscriptionId":"abc","requestId":"2"}`, false},
		{`{"action":"get","path":"Vehicle.Speed","requestId":"3"}`, false},
		{`{"action":"set","path":"Vehicle.Speed","value":"100","requestId":"4"}`, false},
		{``, false},
	}
	for _, tc := range cases {
		if got := isMultipleEventsRequest(tc.req); got != tc.want {
			t.Errorf("isMultipleEventsRequest(%q) = %v; want %v", tc.req, got, tc.want)
		}
	}
}

// TestClassifySubscribeResponse pins the classification used by the
// streaming SubscribeRequest arm — error short-circuit vs kill-message
// short-circuit vs ordinary subscribe event.
func TestClassifySubscribeResponse(t *testing.T) {
	cases := []struct {
		resp         string
		wantIsError  bool
		wantIsKill   bool
	}{
		{`{"action":"subscribe","error":{"number":"401"},"requestId":"1"}`, true, false},
		{"kill subscription clientId:5", false, true},
		{`{"action":"subscribe","subscriptionId":"abc","ts":"2026-01-01T00:00:00Z"}`, false, false},
		{``, false, false},
	}
	for _, tc := range cases {
		gotErr, gotKill := classifySubscribeResponse(tc.resp)
		if gotErr != tc.wantIsError || gotKill != tc.wantIsKill {
			t.Errorf("classifySubscribeResponse(%q) = (%v, %v); want (%v, %v)",
				tc.resp, gotErr, gotKill, tc.wantIsError, tc.wantIsKill)
		}
	}
}

// TestDispatchGrpcUnaryRequest verifies the shared
// GetRequest/SetRequest/UnsubscribeRequest handshake: the helper
// pushes the request onto grpcClientChan[0] and waits for the hub to
// reply on the per-call response channel. We simulate the hub with a
// goroutine that reads the request and writes a canned reply.
func TestDispatchGrpcUnaryRequest(t *testing.T) {
	resetGrpcStateForTest()

	hubDone := make(chan struct{})
	go func() {
		req := <-grpcClientChan[0]
		if req.VssReq != "the-payload" {
			t.Errorf("hub got %q; want %q", req.VssReq, "the-payload")
		}
		req.GrpcRespChan <- "the-response"
		close(hubDone)
	}()

	resultCh := make(chan string, 1)
	go func() { resultCh <- dispatchGrpcUnaryRequest("the-payload") }()

	select {
	case got := <-resultCh:
		if got != "the-response" {
			t.Fatalf("dispatchGrpcUnaryRequest returned %q; want %q", got, "the-response")
		}
	case <-time.After(time.Second):
		t.Fatalf("dispatchGrpcUnaryRequest did not return within 1s")
	}
	<-hubDone
}

// TestHandleGrpcNewClientSession_AllocatesClientAndForwards exercises
// the happy path: a clientId is available, routing data is set, and
// the request is forwarded to the transport manager.
func TestHandleGrpcNewClientSession_AllocatesClientAndForwards(t *testing.T) {
	resetGrpcStateForTest()
	transportMgrChan := make(chan string, 4)
	respChan := make(chan string, 1)
	req := GrpcRequestMessage{
		VssReq:       `{"action":"get","path":"Vehicle.Speed","requestId":"7"}`,
		GrpcRespChan: respChan,
	}

	handleGrpcNewClientSession(req, 0, transportMgrChan)

	// We should see exactly one message forwarded to the transport
	// manager, and respChan should not have received a max-clients
	// error.
	select {
	case forwarded := <-transportMgrChan:
		if !strings.Contains(forwarded, "Vehicle.Speed") {
			t.Fatalf("forwarded message lost the original payload: %q", forwarded)
		}
	case <-time.After(time.Second):
		t.Fatalf("nothing forwarded to transportMgrChan")
	}
	select {
	case errResp := <-respChan:
		t.Fatalf("respChan should be empty on the happy path; got %q", errResp)
	default:
	}
}

// TestHandleGrpcNewClientSession_MaxClientsReturnsError fills every
// slot in grpcClientIndexList so getClientId returns -1, then
// confirms the helper pushes the canned max-clients error back to the
// caller instead of forwarding.
func TestHandleGrpcNewClientSession_MaxClientsReturnsError(t *testing.T) {
	resetGrpcStateForTest()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcClientIndexList[i] = true
	}
	transportMgrChan := make(chan string, 4)
	respChan := make(chan string, 1)
	req := GrpcRequestMessage{
		VssReq:       `{"action":"get","path":"Vehicle.Speed","requestId":"7"}`,
		GrpcRespChan: respChan,
	}

	handleGrpcNewClientSession(req, 0, transportMgrChan)

	select {
	case errResp := <-respChan:
		if !strings.Contains(errResp, "max_client_sessions") {
			t.Fatalf("expected max_client_sessions error; got %q", errResp)
		}
	case <-time.After(time.Second):
		t.Fatalf("max-clients path did not push error to respChan")
	}
	select {
	case forwarded := <-transportMgrChan:
		t.Fatalf("nothing should be forwarded on max-clients path; got %q", forwarded)
	default:
	}
}

// TestHandleGrpcTransportResponse_RoutesToRegisteredClient seeds the
// routing table with a known clientId, sends a response carrying that
// clientId via the RouterId field, and confirms the trimmed response
// arrives on the registered client's channel.
func TestHandleGrpcTransportResponse_RoutesToRegisteredClient(t *testing.T) {
	resetGrpcStateForTest()
	clientId := getClientId()
	if clientId == -1 {
		t.Fatalf("getClientId returned -1 after reset; routing table likely full")
	}
	respChan := make(chan string, 1)
	if !setGrpcRoutingData(clientId, respChan, false) {
		t.Fatalf("setGrpcRoutingData refused to register clientId=%d", clientId)
	}

	// utils.RemoveInternalData expects "RouterId":"mgrId?clientId" with
	// a comma immediately following the close quote.
	response := `{"action":"get","RouterId":"0?` +
		strconv.Itoa(clientId) + `","value":"100"}`

	handleGrpcTransportResponse(response)

	select {
	case trimmed := <-respChan:
		if !strings.Contains(trimmed, `"value":"100"`) {
			t.Fatalf("trimmed response lost the value payload: %q", trimmed)
		}
		if strings.Contains(trimmed, "RouterId") {
			t.Fatalf("RouterId was not stripped from response: %q", trimmed)
		}
	case <-time.After(time.Second):
		t.Fatalf("registered client did not receive routed response")
	}
}

// TestHandleGrpcTransportResponse_UnknownClientDoesNotPanic covers the
// "missing routing data" branch — RemoveRoutingForwardResponse should
// log an error and return, not crash.
func TestHandleGrpcTransportResponse_UnknownClientDoesNotPanic(t *testing.T) {
	resetGrpcStateForTest()
	// clientId 49 is in range but not registered.
	response := `{"action":"get","RouterId":"0?49","value":"100"}`
	// Must not panic.
	handleGrpcTransportResponse(response)
}

