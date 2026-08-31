/**
* (C) 2026 Ford Motor Company
*
* Tests for the VISSv3.2 Service profile additions to grpcMgr.go: the
* CancelRequest/DiscoverRequest unary stubs, the InvokeRequest/
* MonitorRequest streaming RPCs (via the shared serveServiceStream), and
* the supporting routing/classification helpers (getServiceId,
* updateGrpcServiceRoutingData/getServiceRoutingData,
* isMultipleEventsRequest's invoke/monitor branch, updateRoutingList's
* invoke/monitor/monitoring/cancel branches). Mirrors the style of
* grpcMgr_test.go / grpcMgr_dispatch_test.go.
**/
package grpcMgr

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "github.com/covesa/vissr/grpc_pb"
	"google.golang.org/grpc/metadata"
)

// --------------------------------------------------------------------------
// getServiceId
// --------------------------------------------------------------------------

func TestGetServiceId_HappyPath(t *testing.T) {
	if got := getServiceId(`{"action":"invoke","status":"ONGOING","serviceId":"S1"}`); got != "S1" {
		t.Fatalf("got %q, want S1", got)
	}
}

func TestGetServiceId_MissingOrMalformed(t *testing.T) {
	if got := getServiceId(`{"action":"invoke"}`); got != "" {
		t.Fatalf("missing field: got %q, want empty", got)
	}
	if got := getServiceId(`not json`); got != "" {
		t.Fatalf("malformed JSON: got %q, want empty", got)
	}
}

// --------------------------------------------------------------------------
// updateGrpcServiceRoutingData / getServiceRoutingData
// --------------------------------------------------------------------------

func TestServiceRoutingData_SetAndLookup(t *testing.T) {
	initLists()
	defer initLists()

	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	if !setGrpcRoutingData(id, make(chan string, 1), true) {
		t.Fatalf("setGrpcRoutingData failed")
	}
	updateGrpcServiceRoutingData(id, "svc-ABC")

	if got := getServiceRoutingData("svc-ABC"); got != id {
		t.Fatalf("getServiceRoutingData = %d, want %d", got, id)
	}
	if got := getServiceRoutingData("no-such-service"); got != -1 {
		t.Fatalf("unknown serviceId: got %d, want -1", got)
	}
}

// --------------------------------------------------------------------------
// isMultipleEventsRequest — invoke/monitor branch
// --------------------------------------------------------------------------

func TestIsMultipleEventsRequest_ServiceActions(t *testing.T) {
	cases := []struct {
		req  string
		want bool
	}{
		{`{"action":"invoke","path":"P","requestId":"1"}`, true},
		{`{"action":"monitor","path":"P","requestId":"1"}`, true},
		{`{"action":"cancel","serviceId":"S1"}`, false},
		{`{"action":"discover","path":"P","depth":"0","requestId":"1"}`, false},
	}
	for _, tc := range cases {
		if got := isMultipleEventsRequest(tc.req); got != tc.want {
			t.Errorf("isMultipleEventsRequest(%q) = %v; want %v", tc.req, got, tc.want)
		}
	}
}

// --------------------------------------------------------------------------
// updateRoutingList — invoke/monitor/monitoring/cancel branches
// --------------------------------------------------------------------------

func TestUpdateRoutingList_InvokeOngoingRecordsServiceId(t *testing.T) {
	initLists()
	defer initLists()

	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	ch := make(chan string, 1)
	if !setGrpcRoutingData(id, ch, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}

	resp := `{"action":"invoke","status":"ONGOING","requestId":"1","ts":"T","serviceId":"svc-XYZ"}`
	updateRoutingList(resp, id, true)

	if got := getServiceRoutingData("svc-XYZ"); got != id {
		t.Fatalf("expected serviceId svc-XYZ to map to clientId %d; got %d", id, got)
	}
	// The routing slot itself must still be present (not reset).
	if _, multi := getGrpcRoutingData(id); !multi {
		t.Fatalf("expected routing slot to remain registered as multi-event")
	}
}

func TestUpdateRoutingList_InvokeTerminalResetsClientId(t *testing.T) {
	initLists()
	defer initLists()

	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	ch := make(chan string, 1)
	if !setGrpcRoutingData(id, ch, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}

	resp := `{"action":"invoke","status":"SUCCESSFUL","requestId":"1","ts":"T"}`
	updateRoutingList(resp, id, true)

	if ch2, _ := getGrpcRoutingData(id); ch2 != nil {
		t.Fatalf("expected routing slot to be reset after terminal invoke response")
	}
}

func TestUpdateRoutingList_MonitoringEventOngoingKeepsSlot(t *testing.T) {
	initLists()
	defer initLists()

	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	ch := make(chan string, 1)
	if !setGrpcRoutingData(id, ch, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}

	resp := `{"action":"monitoring","path":"P","serviceId":"S1","status":"ONGOING","ts":"T"}`
	updateRoutingList(resp, id, true)

	if _, multi := getGrpcRoutingData(id); !multi {
		t.Fatalf("expected routing slot to remain registered while ONGOING")
	}
}

func TestUpdateRoutingList_MonitoringEventTerminalResets(t *testing.T) {
	initLists()
	defer initLists()

	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	ch := make(chan string, 1)
	if !setGrpcRoutingData(id, ch, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}

	resp := `{"action":"monitoring","path":"P","serviceId":"S1","status":"FAILED","ts":"T"}`
	updateRoutingList(resp, id, true)

	if ch2, _ := getGrpcRoutingData(id); ch2 != nil {
		t.Fatalf("expected routing slot to be reset after terminal monitoring event")
	}
}

func TestUpdateRoutingList_CancelAlwaysResets(t *testing.T) {
	initLists()
	defer initLists()

	// isMultipleEvent=true path: mirrors serveServiceStream's disconnect
	// handler reusing the invoke/monitor session's own slot.
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	if !setGrpcRoutingData(id, make(chan string, 1), true) {
		t.Fatalf("setGrpcRoutingData failed")
	}
	updateRoutingList(`{"action":"cancel","status":"CANCELED","serviceId":"S1","ts":"T"}`, id, true)
	if ch, _ := getGrpcRoutingData(id); ch != nil {
		t.Fatalf("expected routing slot to be reset after cancel ack (isMultipleEvent=true)")
	}

	// isMultipleEvent=false path: an ordinary client-issued cancel.
	id2 := getClientId()
	if id2 == -1 {
		t.Fatalf("no free client slot")
	}
	if !setGrpcRoutingData(id2, make(chan string, 1), false) {
		t.Fatalf("setGrpcRoutingData failed")
	}
	updateRoutingList(`{"action":"cancel","status":"FAILED","serviceId":"","ts":"T"}`, id2, false)
	if ch, _ := getGrpcRoutingData(id2); ch != nil {
		t.Fatalf("expected routing slot to be reset after cancel ack (isMultipleEvent=false)")
	}
}

func TestUpdateRoutingList_DiscoverResetsClientId(t *testing.T) {
	initLists()
	defer initLists()

	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	if !setGrpcRoutingData(id, make(chan string, 1), false) {
		t.Fatalf("setGrpcRoutingData failed")
	}
	updateRoutingList(`{"action":"discover","metadata":{},"requestId":"1","ts":"T"}`, id, false)
	if ch, _ := getGrpcRoutingData(id); ch != nil {
		t.Fatalf("expected routing slot to be reset after discover response")
	}
}

// --------------------------------------------------------------------------
// CancelRequest / DiscoverRequest unary stubs
// --------------------------------------------------------------------------

func TestCancelRequest_ForwardsAndResponds(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"cancel","status":"CANCELED","serviceId":"S1","ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.CancelRequestMessage{ServiceId: "S1"}
	resp, err := srv.CancelRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("CancelRequest returned error: %v", err)
	}
	if resp.GetStatus() != pb.ServiceStatus_CANCELED || resp.GetServiceId() != "S1" {
		t.Fatalf("got %+v", resp)
	}
}

func TestDiscoverRequest_ForwardsAndResponds(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"discover","metadata":{"MoveSeat":{"type":"procedure"}},"requestId":"1","ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.DiscoverRequestMessage{Path: "VehicleService.Seating", Depth: "0", RequestId: "1"}
	resp, err := srv.DiscoverRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("DiscoverRequest returned error: %v", err)
	}
	if resp.GetMetadata() == nil {
		t.Fatalf("got %+v", resp)
	}
}

// --------------------------------------------------------------------------
// InvokeRequest / MonitorRequest — full RPC path (thin wrappers around
// serveServiceStream; see TestServeServiceStream_* below for the shared
// control-flow coverage)
// --------------------------------------------------------------------------

// mockInvokeStream is a minimal VISS_InvokeRequestServer implementation,
// mirroring mockSubscribeStream.
type mockInvokeStream struct {
	ctx   context.Context
	sends []*pb.InvokeStreamMessage
}

func (m *mockInvokeStream) Send(msg *pb.InvokeStreamMessage) error {
	m.sends = append(m.sends, msg)
	return nil
}
func (m *mockInvokeStream) Context() context.Context       { return m.ctx }
func (m *mockInvokeStream) SetHeader(_ metadata.MD) error  { return nil }
func (m *mockInvokeStream) SendHeader(_ metadata.MD) error { return nil }
func (m *mockInvokeStream) SetTrailer(_ metadata.MD)       {}
func (m *mockInvokeStream) SendMsg(_ interface{}) error    { return nil }
func (m *mockInvokeStream) RecvMsg(_ interface{}) error    { return nil }

// TestInvokeRequest_SynchronousResponse drives a single non-ONGOING
// response through the real InvokeRequest RPC handler end to end (proto in,
// proto out via stream.Send), confirming the pb<->JSON plumbing wired up in
// this PR (InvokeRequestPbToJson/InvokeStreamJsonToPb) works together.
func TestInvokeRequest_SynchronousResponse(t *testing.T) {
	initLists()
	defer initLists()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &mockInvokeStream{ctx: ctx}
	in := &pb.InvokeRequestMessage{Path: "VehicleService.Seating.GetCapabilities", RequestId: "1"}

	srv := &Server{}
	done := make(chan error, 1)
	go func() {
		done <- srv.InvokeRequest(in, stream)
	}()

	select {
	case req := <-grpcClientChan[0]:
		if !strings.Contains(req.VssReq, `"action":"invoke"`) || !strings.Contains(req.VssReq, `"path":"VehicleService.Seating.GetCapabilities"`) {
			t.Fatalf("forwarded request = %q; missing expected fields", req.VssReq)
		}
		req.GrpcRespChan <- `{"action":"invoke","path":"VehicleService.Seating.GetCapabilities","status":"SUCCESSFUL","requestId":"1","ts":"T"}`
	case <-time.After(2 * time.Second):
		t.Fatalf("InvokeRequest did not forward initial request")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("InvokeRequest returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("InvokeRequest did not return within 2s")
	}

	if len(stream.sends) != 1 {
		t.Fatalf("expected 1 stream.Send call; got %d", len(stream.sends))
	}
	resp := stream.sends[0].GetResponse()
	if resp == nil || resp.GetStatus() != pb.ServiceStatus_SUCCESSFUL {
		t.Fatalf("got %+v", stream.sends[0])
	}
}

// mockMonitorStream mirrors mockInvokeStream for MonitorRequest.
type mockMonitorStream struct {
	ctx   context.Context
	sends []*pb.MonitorStreamMessage
}

func (m *mockMonitorStream) Send(msg *pb.MonitorStreamMessage) error {
	m.sends = append(m.sends, msg)
	return nil
}
func (m *mockMonitorStream) Context() context.Context       { return m.ctx }
func (m *mockMonitorStream) SetHeader(_ metadata.MD) error  { return nil }
func (m *mockMonitorStream) SendHeader(_ metadata.MD) error { return nil }
func (m *mockMonitorStream) SetTrailer(_ metadata.MD)       {}
func (m *mockMonitorStream) SendMsg(_ interface{}) error    { return nil }
func (m *mockMonitorStream) RecvMsg(_ interface{}) error    { return nil }

func TestMonitorRequest_SynchronousResponse(t *testing.T) {
	initLists()
	defer initLists()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &mockMonitorStream{ctx: ctx}
	in := &pb.MonitorRequestMessage{Path: "VehicleService.Seating.MoveSeat", RequestId: "m1"}

	srv := &Server{}
	done := make(chan error, 1)
	go func() {
		done <- srv.MonitorRequest(in, stream)
	}()

	select {
	case req := <-grpcClientChan[0]:
		if !strings.Contains(req.VssReq, `"action":"monitor"`) {
			t.Fatalf("forwarded request = %q; missing action=monitor", req.VssReq)
		}
		req.GrpcRespChan <- `{"action":"monitor","path":"P","status":"UNKNOWN","requestId":"m1","ts":"T"}`
	case <-time.After(2 * time.Second):
		t.Fatalf("MonitorRequest did not forward initial request")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("MonitorRequest returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("MonitorRequest did not return within 2s")
	}

	if len(stream.sends) != 1 || stream.sends[0].GetResponse().GetStatus() != pb.ServiceStatus_UNKNOWN {
		t.Fatalf("got %+v", stream.sends)
	}
}

// --------------------------------------------------------------------------
// serveServiceStream — shared control flow underlying InvokeRequest/
// MonitorRequest. Exercised directly (rather than via the pb.VISSClient
// wrappers above) so tests can retain a reference to grpcResponseChan
// across the initial ACK and a follow-up event, mirroring how the real
// manager hub delivers both messages onto the same channel over the
// session's lifetime.
// --------------------------------------------------------------------------

func TestServeServiceStream_OngoingThenTerminalEvent(t *testing.T) {
	initLists()
	defer initLists()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sent []string
	sendFn := func(vssResp string) error {
		sent = append(sent, vssResp)
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- serveServiceStream(`{"action":"invoke","path":"P","requestId":"1"}`, ctx, sendFn)
	}()

	var grpcRespChan chan string
	select {
	case req := <-grpcClientChan[0]:
		grpcRespChan = req.GrpcRespChan
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not forward initial request")
	}

	// Register the serviceId against a real routing slot so
	// getServiceRoutingData resolves a clientId (mirrors what
	// handleGrpcNewClientSession + updateRoutingList would do via the real
	// manager hub loop).
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	if !setGrpcRoutingData(id, grpcRespChan, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}
	updateGrpcServiceRoutingData(id, "svc-1")

	grpcRespChan <- `{"action":"invoke","status":"ONGOING","requestId":"1","ts":"T","serviceId":"svc-1"}`
	time.Sleep(50 * time.Millisecond)

	select {
	case err := <-done:
		t.Fatalf("serveServiceStream returned early (err=%v) after ONGOING ack; expected it to keep streaming", err)
	default:
	}

	grpcRespChan <- `{"action":"monitoring","path":"P","serviceId":"svc-1","status":"SUCCESSFUL","ts":"T"}`

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveServiceStream returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not return within 2s after terminal event")
	}

	if len(sent) != 2 {
		t.Fatalf("expected 2 sendFn calls (ack+event); got %d: %+v", len(sent), sent)
	}
}

// TestServeServiceStream_SynchronousOnly covers a non-ONGOING immediate
// response (e.g. GetCapabilities' SUCCESSFUL reply, or a validation
// FAILED): serveServiceStream must send it once and return nil without
// waiting for further events.
func TestServeServiceStream_SynchronousOnly(t *testing.T) {
	initLists()
	defer initLists()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sent []string
	sendFn := func(vssResp string) error {
		sent = append(sent, vssResp)
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- serveServiceStream(`{"action":"invoke","path":"P","requestId":"1"}`, ctx, sendFn)
	}()

	select {
	case req := <-grpcClientChan[0]:
		req.GrpcRespChan <- `{"action":"invoke","status":"SUCCESSFUL","requestId":"1","ts":"T"}`
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not forward initial request")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveServiceStream returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not return within 2s")
	}

	if len(sent) != 1 {
		t.Fatalf("expected exactly 1 sendFn call; got %d: %+v", len(sent), sent)
	}
}

// TestServeServiceStream_DisconnectForwardsCancel confirms that when the
// context is cancelled while a session is ONGOING, serveServiceStream
// forwards a "cancel" request for the session's serviceId via grpcMgrChan.
func TestServeServiceStream_DisconnectForwardsCancel(t *testing.T) {
	initLists()
	defer initLists()

	testMgrChan := make(chan string, 4)
	origMgrChan := grpcMgrChan
	grpcMgrChan = testMgrChan
	defer func() { grpcMgrChan = origMgrChan }()

	ctx, cancel := context.WithCancel(context.Background())
	sendFn := func(vssResp string) error { return nil }

	done := make(chan error, 1)
	go func() {
		done <- serveServiceStream(`{"action":"monitor","path":"P","requestId":"1"}`, ctx, sendFn)
	}()

	var grpcRespChan chan string
	select {
	case req := <-grpcClientChan[0]:
		grpcRespChan = req.GrpcRespChan
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not forward initial request")
	}

	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	if !setGrpcRoutingData(id, grpcRespChan, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}
	updateGrpcServiceRoutingData(id, "svc-2")

	grpcRespChan <- `{"action":"monitor","status":"ONGOING","requestId":"1","ts":"T","serviceId":"svc-2"}`
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveServiceStream returned error on disconnect: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not return within 2s after disconnect")
	}

	select {
	case fwd := <-testMgrChan:
		if !strings.Contains(fwd, `"action":"cancel"`) || !strings.Contains(fwd, `"serviceId":"svc-2"`) {
			t.Fatalf("forwarded cancel request = %q; missing expected fields", fwd)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected a cancel request forwarded on grpcMgrChan after disconnect")
	}
}

// TestServeServiceStream_DisconnectBeforeOngoing_NoCancelForwarded confirms
// that disconnecting before any ONGOING ack has been seen (clientId still
// unresolved) does not forward a spurious cancel.
func TestServeServiceStream_DisconnectBeforeOngoing_NoCancelForwarded(t *testing.T) {
	initLists()
	defer initLists()

	testMgrChan := make(chan string, 4)
	origMgrChan := grpcMgrChan
	grpcMgrChan = testMgrChan
	defer func() { grpcMgrChan = origMgrChan }()

	ctx, cancel := context.WithCancel(context.Background())
	sendFn := func(vssResp string) error { return nil }

	done := make(chan error, 1)
	go func() {
		done <- serveServiceStream(`{"action":"invoke","path":"P","requestId":"1"}`, ctx, sendFn)
	}()

	select {
	case <-grpcClientChan[0]:
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not forward initial request")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveServiceStream returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not return within 2s")
	}

	select {
	case fwd := <-testMgrChan:
		t.Fatalf("expected no cancel forwarded before any ONGOING ack; got %q", fwd)
	default:
	}
}

// TestServeServiceStream_SendErrorReturnsError mirrors
// TestSubscribeRequest_SendErrorReturnsError for the shared service-stream
// helper.
func TestServeServiceStream_SendErrorReturnsError(t *testing.T) {
	initLists()
	defer initLists()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wantErr := fmt.Errorf("send failed")
	sendFn := func(vssResp string) error { return wantErr }

	done := make(chan error, 1)
	go func() {
		done <- serveServiceStream(`{"action":"invoke","path":"P","requestId":"1"}`, ctx, sendFn)
	}()

	select {
	case req := <-grpcClientChan[0]:
		req.GrpcRespChan <- `{"action":"invoke","status":"ONGOING","requestId":"1","ts":"T","serviceId":"svc-3"}`
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not forward initial request")
	}

	select {
	case err := <-done:
		if err != wantErr {
			t.Fatalf("serveServiceStream returned %v; want %v", err, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("serveServiceStream did not return within 2s on send-error path")
	}
}
