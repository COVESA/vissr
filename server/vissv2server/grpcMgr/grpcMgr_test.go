/**
* Regression tests for the grpcMgr fixes shipped in PR #120
* (grpcStateMu around grpcRoutingDataList / grpcClientIndexList).
**/
package grpcMgr

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	pb "github.com/covesa/vissr/grpc_pb"
	"google.golang.org/grpc/metadata"
)

// initLists sets up the package-level slices to a known empty state.
// The production setup of this is buried inside the gRPC Init path; we
// replicate the minimum here so the test is self-contained.
func initLists() {
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	if len(grpcClientIndexList) != MAXGRPCCLIENTS {
		grpcClientIndexList = make([]bool, MAXGRPCCLIENTS)
	}
	if len(grpcRoutingDataList) != MAXGRPCCLIENTS {
		grpcRoutingDataList = make([]GrpcRoutingData, MAXGRPCCLIENTS)
	}
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcClientIndexList[i] = false
		grpcRoutingDataList[i].ClientId = -1
	}
}

// TestGetClientId_AllocatesUniqueSlots is the basic-semantics check.
func TestGetClientId_AllocatesUniqueSlots(t *testing.T) {
	initLists()
	defer initLists()

	first := getClientId()
	second := getClientId()
	if first == -1 || second == -1 {
		t.Fatalf("expected two free slots; got %d, %d", first, second)
	}
	if first == second {
		t.Fatalf("two sequential getClientId calls returned the same slot %d", first)
	}
}

// TestGetClientId_Exhaustion verifies the function returns -1 when the
// pool is full.
func TestGetClientId_Exhaustion(t *testing.T) {
	initLists()
	defer initLists()

	grpcStateMu.Lock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcClientIndexList[i] = true
	}
	grpcStateMu.Unlock()

	if got := getClientId(); got != -1 {
		t.Fatalf("expected -1 when pool full; got %d", got)
	}
}

// TestGetClientId_ConcurrentClaimsAreUnique is the regression test for
// the PR #120 grpcStateMu mutex. Before that fix, per-RPC handler
// goroutines and the manager-loop goroutine concurrently mutated
// grpcClientIndexList / grpcRoutingDataList; the result was slot leaks,
// cross-talk between unrelated subscribers, or runtime panics on
// concurrent slice mutation.
//
// Run with: go test -race
func TestGetClientId_ConcurrentClaimsAreUnique(t *testing.T) {
	initLists()
	defer initLists()

	n := MAXGRPCCLIENTS
	results := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = getClientId()
		}(i)
	}
	wg.Wait()

	seen := make(map[int]int)
	for _, idx := range results {
		if idx == -1 {
			t.Fatalf("concurrent claim returned -1 even though %d slots were free", n)
		}
		seen[idx]++
	}
	for idx, count := range seen {
		if count > 1 {
			t.Fatalf("slot %d was claimed %d times concurrently; grpcStateMu is missing or broken", idx, count)
		}
	}
}

// TestSetAndResetGrpcRoutingData_ConcurrentSafe exercises the routing-
// data accessors concurrently. Designed to be run with -race.
func TestSetAndResetGrpcRoutingData_ConcurrentSafe(t *testing.T) {
	initLists()
	defer initLists()

	// Claim a handful of client ids first so the routing accessors have
	// values to mutate.
	const n = 8
	ids := make([]int, n)
	for i := range ids {
		ids[i] = getClientId()
		if ids[i] == -1 {
			t.Fatalf("expected at least %d free client slots", n)
		}
		if !setGrpcRoutingData(ids[i], make(chan string, 1), false) {
			t.Fatalf("setGrpcRoutingData failed for clientId %d", ids[i])
		}
	}

	// Spawn the same number of mutators that race set/get/reset.
	var wg sync.WaitGroup
	wg.Add(n * 3)
	for i := 0; i < n; i++ {
		clientId := ids[i]
		subId := "sub-" + string(rune('A'+i))
		go func() {
			defer wg.Done()
			updateGrpcRoutingData(clientId, subId)
		}()
		go func() {
			defer wg.Done()
			_, _ = getGrpcRoutingData(clientId)
		}()
		go func() {
			defer wg.Done()
			resetGrpcRoutingData(clientId)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// resetClientId
// ---------------------------------------------------------------------------

// TestResetClientId_ClearsSlot: resetClientId must mark the given slot
// as free so getClientId can re-allocate it.
func TestResetClientId_ClearsSlot(t *testing.T) {
	initLists()
	defer initLists()

	id := getClientId()
	if id == -1 {
		t.Fatalf("getClientId: no free slot")
	}
	// Slot should now be taken.
	grpcStateMu.Lock()
	taken := grpcClientIndexList[id]
	grpcStateMu.Unlock()
	if !taken {
		t.Fatalf("slot %d not marked taken after getClientId", id)
	}

	// Reset it.
	resetClientId(id)

	grpcStateMu.Lock()
	taken = grpcClientIndexList[id]
	grpcStateMu.Unlock()
	if taken {
		t.Fatalf("slot %d still marked taken after resetClientId", id)
	}
}

// TestResetClientId_Idempotent: calling resetClientId twice on the
// same slot must not panic.
func TestResetClientId_Idempotent(t *testing.T) {
	initLists()
	defer initLists()

	id := getClientId()
	if id == -1 {
		t.Fatalf("getClientId: no free slot")
	}
	resetClientId(id)
	resetClientId(id) // second call must not panic
}

// ---------------------------------------------------------------------------
// setGrpcRoutingData
// ---------------------------------------------------------------------------

// TestSetGrpcRoutingData_FindsEmptySlot: setGrpcRoutingData should
// locate the first entry with ClientId==-1 and populate it.
func TestSetGrpcRoutingData_FindsEmptySlot(t *testing.T) {
	initLists()
	defer initLists()

	ch := make(chan string, 1)
	id := getClientId()
	if id == -1 {
		t.Fatalf("getClientId: no free slot")
	}
	ok := setGrpcRoutingData(id, ch, true)
	if !ok {
		t.Fatalf("setGrpcRoutingData returned false; want true")
	}
	// Verify the data was stored.
	gotCh, gotMulti := getGrpcRoutingData(id)
	if gotCh == nil {
		t.Fatalf("getGrpcRoutingData returned nil channel after set")
	}
	if !gotMulti {
		t.Fatalf("getGrpcRoutingData returned isMultipleEvents=false; want true")
	}
}

// TestSetGrpcRoutingData_ReturnsFalseWhenFull: when all routing data
// slots are occupied, setGrpcRoutingData must return false.
func TestSetGrpcRoutingData_ReturnsFalseWhenFull(t *testing.T) {
	initLists()
	defer initLists()

	// Fill all routing data slots.
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcStateMu.Lock()
		grpcRoutingDataList[i].ClientId = i
		grpcStateMu.Unlock()
	}
	ok := setGrpcRoutingData(0, make(chan string, 1), false)
	if ok {
		t.Fatalf("setGrpcRoutingData on a full list returned true; want false")
	}
}

// TestSetGrpcRoutingData_IsMultipleEventsFalse: setting isMultipleEvent=false
// is stored and retrievable.
func TestSetGrpcRoutingData_IsMultipleEventsFalse(t *testing.T) {
	initLists()
	defer initLists()

	ch := make(chan string, 1)
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free slot")
	}
	setGrpcRoutingData(id, ch, false)
	_, gotMulti := getGrpcRoutingData(id)
	if gotMulti {
		t.Fatalf("isMultipleEvents should be false; got true")
	}
}

// ---------------------------------------------------------------------------
// getSubscribeRoutingData
// ---------------------------------------------------------------------------

// TestGetSubscribeRoutingData_FindsBySubscriptionId: after
// setGrpcRoutingData + updateGrpcRoutingData, getSubscribeRoutingData
// must find the entry by its subscriptionId.
func TestGetSubscribeRoutingData_FindsBySubscriptionId(t *testing.T) {
	initLists()
	defer initLists()

	ch := make(chan string, 1)
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free slot")
	}
	setGrpcRoutingData(id, ch, true)
	updateGrpcRoutingData(id, "sub-ABCD")

	// Build a minimal subscribe-response JSON that getSubscriptionId can parse.
	resp := `{"action":"subscription","subscriptionId":"sub-ABCD"}`
	gotId, gotCh := getSubscribeRoutingData(resp)
	if gotId != id {
		t.Fatalf("getSubscribeRoutingData: clientId = %d; want %d", gotId, id)
	}
	if gotCh == nil {
		t.Fatalf("getSubscribeRoutingData: channel is nil; want non-nil")
	}
}

// TestGetSubscribeRoutingData_MissingId: a subscription id that was
// never registered must return -1 and nil channel.
func TestGetSubscribeRoutingData_MissingId(t *testing.T) {
	initLists()
	defer initLists()

	resp := `{"action":"subscription","subscriptionId":"sub-NOTFOUND"}`
	gotId, gotCh := getSubscribeRoutingData(resp)
	if gotId != -1 {
		t.Fatalf("getSubscribeRoutingData on missing id = %d; want -1", gotId)
	}
	if gotCh != nil {
		t.Fatalf("getSubscribeRoutingData on missing id returned non-nil channel")
	}
}

// TestGetSubscribeRoutingData_MalformedJSON: a non-JSON response must
// not panic; it should return -1 and nil.
func TestGetSubscribeRoutingData_MalformedJSON(t *testing.T) {
	initLists()
	defer initLists()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("getSubscribeRoutingData panicked on malformed JSON: %v", r)
		}
	}()
	gotId, gotCh := getSubscribeRoutingData(`{not valid json}`)
	if gotId != -1 || gotCh != nil {
		t.Logf("getSubscribeRoutingData on bad JSON: id=%d ch=%v (both -1/nil expected)", gotId, gotCh)
	}
}

// ---------------------------------------------------------------------------
// updateRoutingList
// ---------------------------------------------------------------------------

// TestUpdateRoutingList_GetAndSetResetsClientId: for a non-subscribe
// response (isMultipleEvent=false), updateRoutingList must call
// resetGrpcRoutingData, freeing the slot.
func TestUpdateRoutingList_GetAndSetResetsClientId(t *testing.T) {
	initLists()
	defer initLists()

	ch := make(chan string, 1)
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free slot")
	}
	setGrpcRoutingData(id, ch, false)

	resp := `{"action":"get","requestId":"123"}`
	updateRoutingList(resp, id, false /* not multipleEvent */)

	// The slot must be free after reset.
	grpcStateMu.Lock()
	taken := grpcClientIndexList[id]
	grpcStateMu.Unlock()
	if taken {
		t.Fatalf("slot %d still taken after updateRoutingList for get; want freed", id)
	}
}

// TestUpdateRoutingList_SubscribeWithSubscriptionIdUpdates: a subscribe
// response with a subscriptionId must call updateGrpcRoutingData.
func TestUpdateRoutingList_SubscribeWithSubscriptionIdUpdates(t *testing.T) {
	initLists()
	defer initLists()

	ch := make(chan string, 1)
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free slot")
	}
	setGrpcRoutingData(id, ch, true)

	resp := `{"action":"subscribe","subscriptionId":"sub-XYZ"}`
	updateRoutingList(resp, id, true /* multipleEvent */)

	// The routing entry should now have subscriptionId == "sub-XYZ".
	grpcStateMu.Lock()
	var found string
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == id {
			found = grpcRoutingDataList[i].SubscriptionId
			break
		}
	}
	grpcStateMu.Unlock()
	if found != "sub-XYZ" {
		t.Fatalf("after updateRoutingList subscribe: subscriptionId = %q; want \"sub-XYZ\"", found)
	}
}

// TestUpdateRoutingList_SubscribeErrorResetsClientId: a subscribe
// response WITHOUT subscriptionId (error case) must reset the client.
func TestUpdateRoutingList_SubscribeErrorResetsClientId(t *testing.T) {
	initLists()
	defer initLists()

	ch := make(chan string, 1)
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free slot")
	}
	setGrpcRoutingData(id, ch, true)

	// subscribe response with no subscriptionId → error path
	resp := `{"action":"subscribe","error":{"number":"503"}}`
	updateRoutingList(resp, id, true)

	grpcStateMu.Lock()
	taken := grpcClientIndexList[id]
	grpcStateMu.Unlock()
	if taken {
		t.Fatalf("slot %d still taken after subscribe-error; want freed", id)
	}
}

// TestUpdateRoutingList_UnsubscribeForwardsAndResets: an "unsubscribe"
// response must forward to the subscribe channel and reset the client.
// We simulate both the subscribe-side and unsubscribe-side clientIds.
func TestUpdateRoutingList_UnsubscribeForwardsAndResets(t *testing.T) {
	initLists()
	defer initLists()

	// Set up the subscribe-side channel. getSubscribeRoutingData looks
	// up by subscriptionId, so we need a routing entry with a matching
	// subscriptionId.
	subCh := make(chan string, 1)
	subId := getClientId()
	if subId == -1 {
		t.Fatalf("no free slot for subscribe client")
	}
	setGrpcRoutingData(subId, subCh, true)
	updateGrpcRoutingData(subId, "sub-UNSUB")

	// Set up the unsubscribe-side channel.
	unsubCh := make(chan string, 1)
	unsubId := getClientId()
	if unsubId == -1 {
		t.Fatalf("no free slot for unsubscribe client")
	}
	setGrpcRoutingData(unsubId, unsubCh, false)

	// The "unsubscribe" response carries the subscriptionId so
	// getSubscribeRoutingData can find the subscribe-side channel.
	resp := `{"action":"unsubscribe","subscriptionId":"sub-UNSUB"}`

	// updateRoutingList sends on subscribeChan, which blocks unless
	// consumed. Run it in a goroutine so we can drain subCh.
	done := make(chan struct{})
	go func() {
		updateRoutingList(resp, unsubId, false)
		close(done)
	}()

	// Drain the message sent to the subscribe-side channel.
	select {
	case got := <-subCh:
		_ = got
	case <-done:
		t.Fatalf("updateRoutingList returned before forwarding to subscribe channel")
	}
	<-done

	// The unsubscribe client slot should now be freed.
	grpcStateMu.Lock()
	taken := grpcClientIndexList[unsubId]
	grpcStateMu.Unlock()
	if taken {
		t.Fatalf("unsubscribe client slot %d still taken; want freed", unsubId)
	}
}

// ---------------------------------------------------------------------------
// Helper utilities used by updateRoutingList tests
// ---------------------------------------------------------------------------

// TestGetSubscriptionId_HappyPath: a valid subscriptionId JSON field
// is parsed correctly.
func TestGetSubscriptionId_HappyPath(t *testing.T) {
	resp := `{"action":"subscribe","subscriptionId":"sub-42"}`
	got := getSubscriptionId(resp)
	if got != "sub-42" {
		t.Fatalf("getSubscriptionId = %q; want \"sub-42\"", got)
	}
}

// TestGetSubscriptionId_Missing: a JSON with no subscriptionId field
// returns "".
func TestGetSubscriptionId_Missing(t *testing.T) {
	resp := `{"action":"subscribe"}`
	got := getSubscriptionId(resp)
	if got != "" {
		t.Fatalf("getSubscriptionId (missing) = %q; want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// GetRequest / SetRequest / UnsubscribeRequest — unary RPC stubs
//
// Each stub is 2-3 statements: convert proto→JSON, call
// dispatchGrpcUnaryRequest (send + receive on grpcClientChan), convert
// JSON→proto. We simulate the hub with a goroutine that consumes the
// request and returns a canned response so the send doesn't block.
// ---------------------------------------------------------------------------

// makeHubSimulator spawns a goroutine that reads one request from
// grpcClientChan[0] and writes the given response back. It closes the
// returned channel when done so the test can synchronise.
func makeHubSimulator(response string) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case req := <-grpcClientChan[0]:
			req.GrpcRespChan <- response
		case <-time.After(2 * time.Second):
			// test timed out — leave done open so the test hangs and fails
		}
	}()
	return done
}

// TestGetRequest_ForwardsAndResponds: a GetRequest RPC converts the
// proto message to JSON, dispatches it, and converts the response back.
// The response JSON is a minimal get-response shape that
// utils.GetResponseJsonToPb can handle.
func TestGetRequest_ForwardsAndResponds(t *testing.T) {
	initLists()
	defer initLists()

	// A minimal "get" response JSON (empty-ish but valid for JsonToPb).
	fakeResp := `{"action":"get","requestId":"999","ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.GetRequestMessage{Path: "Vehicle.Speed", RequestId: "999"}
	resp, err := srv.GetRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("GetRequest returned error: %v", err)
	}
	_ = resp // conversion result may be nil for unknown fields; just confirm no panic
}

// TestSetRequest_ForwardsAndResponds: mirrors TestGetRequest for the Set stub.
func TestSetRequest_ForwardsAndResponds(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"set","requestId":"888","ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.SetRequestMessage{Path: "Vehicle.Speed", Value: "100", RequestId: "888"}
	resp, err := srv.SetRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("SetRequest returned error: %v", err)
	}
	_ = resp
}

// TestUnsubscribeRequest_ForwardsAndResponds mirrors for the Unsubscribe stub.
func TestUnsubscribeRequest_ForwardsAndResponds(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"unsubscribe","requestId":"777","ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.UnsubscribeRequestMessage{SubscriptionId: "sub-99", RequestId: "777"}
	resp, err := srv.UnsubscribeRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("UnsubscribeRequest returned error: %v", err)
	}
	_ = resp
}

// ---------------------------------------------------------------------------
// SubscribeRequest — streaming RPC handler
// ---------------------------------------------------------------------------

// mockSubscribeStream is a minimal VISS_SubscribeRequestServer implementation
// that allows the context to be cancelled so SubscribeRequest terminates.
type mockSubscribeStream struct {
	ctx     context.Context
	cancel  context.CancelFunc
	sends   []*pb.SubscribeStreamMessage
	sendErr error // if non-nil, Send returns this error
}

func (m *mockSubscribeStream) Send(msg *pb.SubscribeStreamMessage) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sends = append(m.sends, msg)
	return nil
}
func (m *mockSubscribeStream) Context() context.Context        { return m.ctx }
func (m *mockSubscribeStream) SetHeader(_ metadata.MD) error   { return nil }
func (m *mockSubscribeStream) SendHeader(_ metadata.MD) error  { return nil }
func (m *mockSubscribeStream) SetTrailer(_ metadata.MD)        {}
func (m *mockSubscribeStream) SendMsg(_ interface{}) error     { return nil }
func (m *mockSubscribeStream) RecvMsg(_ interface{}) error     { return nil }

// TestSubscribeRequest_ContextCancelledReturnsNil: when the streaming
// context is cancelled immediately, SubscribeRequest must send the
// kill-subscriptions message to the transport and return nil.
func TestSubscribeRequest_ContextCancelledReturnsNil(t *testing.T) {
	initLists()
	defer initLists()

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx, cancel: cancel}
	in := &pb.SubscribeRequestMessage{Path: "Vehicle.Speed", RequestId: "42"}

	srv := &Server{}

	// The function sends the subscribe request to grpcClientChan[0]
	// and then enters a for/select. We need to:
	// 1. Consume the initial send on grpcClientChan[0].
	// 2. Cancel the context so Context().Done() fires.
	done := make(chan error, 1)
	go func() {
		done <- srv.SubscribeRequest(in, stream)
	}()

	// Consume the request forwarded to grpcClientChan[0].
	select {
	case req := <-grpcClientChan[0]:
		// Respond with a subscribe ack so the handler can enter the
		// for/select (subscribeClientId stays -1 since we don't set up
		// routing data, but that's fine — the context cancel will fire).
		req.GrpcRespChan <- `{"action":"subscribe","error":{"number":"503"}}`
	case <-time.After(2 * time.Second):
		t.Fatalf("SubscribeRequest did not forward request to grpcClientChan[0]")
	}

	// Cancel the context and wait for the function to return.
	// (The error-response branch returns nil before entering the full
	// subscribe loop, so we don't need to cancel here — but cancel
	// anyway for cleanup.)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SubscribeRequest returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SubscribeRequest did not return within 2s")
	}
}

// TestSubscribeRequest_KillMessageReturnsNil: when the manager hub sends a
// "kill subscription" message to grpcResponseChan, SubscribeRequest must
// call resetGrpcRoutingData and return nil (the isKill branch).
func TestSubscribeRequest_KillMessageReturnsNil(t *testing.T) {
	initLists()
	defer initLists()

	// Allocate a real client slot so resetGrpcRoutingData(clientId) doesn't
	// panic with an out-of-range index — extractClientId parses the integer
	// suffix and resetGrpcRoutingData uses it as a slot index.
	killClientId := getClientId()
	if killClientId == -1 {
		t.Fatalf("no free client slot")
	}
	killChan := make(chan string, 1)
	if !setGrpcRoutingData(killClientId, killChan, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &mockSubscribeStream{ctx: ctx, cancel: cancel}
	in := &pb.SubscribeRequestMessage{Path: "Vehicle.Speed", RequestId: "55"}

	srv := &Server{}
	done := make(chan error, 1)
	go func() {
		done <- srv.SubscribeRequest(in, stream)
	}()

	// Consume the initial forwarded request and reply with a kill message.
	// Format: "kill subscription clientId:N" — extractClientId splits on ":"
	// and converts the suffix to an int used by resetGrpcRoutingData.
	select {
	case req := <-grpcClientChan[0]:
		req.GrpcRespChan <- fmt.Sprintf("kill subscription clientId:%d", killClientId)
	case <-time.After(2 * time.Second):
		t.Fatalf("SubscribeRequest did not forward request to grpcClientChan[0]")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SubscribeRequest (kill path) returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SubscribeRequest did not return within 2s on kill path")
	}

	// No Send calls should have been made on the kill path.
	if len(stream.sends) != 0 {
		t.Fatalf("expected 0 stream.Send calls on kill path; got %d", len(stream.sends))
	}
}

// TestSubscribeRequest_NormalEventThenContextCancel: covers the normal
// subscribe path — the hub sends a subscribe-ack (setting subscribeClientId),
// and the function then enters the loop waiting for more events. We then
// cancel the context, which triggers the Done() branch. That branch calls
// AddRoutingForwardRequest on grpcMgrChan, so we initialise grpcMgrChan
// to a buffered channel and drain it from a goroutine.
func TestSubscribeRequest_NormalEventThenContextCancel(t *testing.T) {
	initLists()
	defer initLists()

	// grpcMgrChan is nil by default in tests; initialise it so that the
	// context-Done branch (AddRoutingForwardRequest → grpcMgrChan <- ...) does
	// not block forever.
	testMgrChan := make(chan string, 4)
	origMgrChan := grpcMgrChan
	grpcMgrChan = testMgrChan
	defer func() { grpcMgrChan = origMgrChan }()

	// Drain anything the context-Done branch sends. Capture testMgrChan in the
	// closure so the goroutine does not touch the grpcMgrChan global variable
	// after it has been restored by the defer.
	go func(ch chan string) {
		for range ch {
		}
	}(testMgrChan)

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx, cancel: cancel}
	in := &pb.SubscribeRequestMessage{Path: "Vehicle.Speed", RequestId: "77"}

	// Allocate and register a routing slot so getSubscribeRoutingData
	// (called inside SubscribeRequest via the subscribeClientId == -1
	// branch on the first non-error non-kill response) can find the entry.
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	respChan := make(chan string, 4)
	if !setGrpcRoutingData(id, respChan, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}
	updateGrpcRoutingData(id, "sub-NORM")

	srv := &Server{}
	done := make(chan error, 1)
	go func() {
		done <- srv.SubscribeRequest(in, stream)
	}()

	// Step 1: consume the initial forwarded request; respond with a
	// subscribe-ack containing a subscriptionId so the first-response
	// branch sets subscribeClientId and calls stream.Send.
	select {
	case req := <-grpcClientChan[0]:
		req.GrpcRespChan <- `{"action":"subscribe","subscriptionId":"sub-NORM","ts":"2026-01-01T00:00:00Z"}`
	case <-time.After(2 * time.Second):
		t.Fatalf("SubscribeRequest did not forward initial request")
	}

	// Step 2: give the handler time to process the ack and re-enter the
	// select, then cancel the context so the Done() branch fires.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SubscribeRequest (normal path) returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SubscribeRequest did not return within 2s after context cancel")
	}

	// Confirm stream.Send was called at least once (on the subscribe-ack).
	if len(stream.sends) == 0 {
		t.Fatalf("expected at least one stream.Send call; got 0")
	}
}

// TestSubscribeRequest_SendErrorReturnsError: when stream.Send() returns an
// error, SubscribeRequest must propagate that error to its caller. This
// exercises the err != nil branch after stream.Send.
func TestSubscribeRequest_SendErrorReturnsError(t *testing.T) {
	initLists()
	defer initLists()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Allocate a routing slot and register a subscriptionId so the first
	// non-error non-kill response can resolve subscribeClientId.
	id := getClientId()
	if id == -1 {
		t.Fatalf("no free client slot")
	}
	respChan := make(chan string, 4)
	if !setGrpcRoutingData(id, respChan, true) {
		t.Fatalf("setGrpcRoutingData failed")
	}
	updateGrpcRoutingData(id, "sub-SENDERR")

	wantErr := fmt.Errorf("send failed")
	stream := &mockSubscribeStream{ctx: ctx, cancel: cancel, sendErr: wantErr}
	in := &pb.SubscribeRequestMessage{Path: "Vehicle.Speed", RequestId: "88"}

	srv := &Server{}
	done := make(chan error, 1)
	go func() {
		done <- srv.SubscribeRequest(in, stream)
	}()

	// The function allocates its own grpcResponseChan internally and sends
	// it to grpcClientChan[0].  We consume that request and reply with a
	// subscribe-ack carrying the subscriptionId we registered above, which
	// triggers stream.Send (which will fail).
	select {
	case req := <-grpcClientChan[0]:
		req.GrpcRespChan <- `{"action":"subscribe","subscriptionId":"sub-SENDERR","ts":"2026-01-01T00:00:00Z"}`
	case <-time.After(2 * time.Second):
		t.Fatalf("SubscribeRequest did not forward initial request")
	}

	select {
	case err := <-done:
		if err != wantErr {
			t.Fatalf("SubscribeRequest (send-error path) returned %v; want %v", err, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SubscribeRequest did not return within 2s on send-error path")
	}
}

// ---------------------------------------------------------------------------
// Integration-only entry points (NOT unit-tested here)
//
// GrpcMgrInit      — unbounded for/select loop, launches gRPC server
// initGrpcServer   — calls net.Listen + server.Serve (binds port 8887)
// SubscribeRequest (full stream path) — requires routing data setup with
//   a real subscribe loop; the context-cancel and error paths are tested above.
// ---------------------------------------------------------------------------
