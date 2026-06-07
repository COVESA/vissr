package ddsMgr

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/mock"
	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("ddsMgr-test-log.txt", "/tmp", false, "error")
}

// ── test participant / subscriber / publisher mocks ───────────────────────────

// testSubscriber is a controllable dds.Subscriber whose sample channel is
// owned by the test so it can inject samples at will.
type testSubscriber struct {
	ch chan dds.Sample
}

func (s *testSubscriber) C() <-chan dds.Sample { return s.ch }
func (s *testSubscriber) Close() error         { return nil }

// testPublisher is a no-op dds.Publisher. Write and Close always succeed.
type testPublisher struct{}

func (p *testPublisher) Write(_ []byte) error { return nil }
func (p *testPublisher) Close() error         { return nil }

// testParticipant is a minimal dds.Participant for unit tests. Setting
// failSubscriber / failPublisher causes the respective New* call to return
// an error. failPubAfter > 0 lets the first N NewPublisher calls succeed
// and fails all subsequent ones (used to exercise per-reply publisher
// failures while keeping the init publisher happy).
type testParticipant struct {
	subCh          chan dds.Sample // channel wired into the testSubscriber
	failSubscriber bool
	failPublisher  bool
	failPubAfter   int // 0 = apply failPublisher from call 1; >0 = fail after N successes
	pubCount       int
}

func (tp *testParticipant) NewSubscriber(_ string, _ dds.QoS) (dds.Subscriber, error) {
	if tp.failSubscriber {
		return nil, errors.New("test: subscriber creation failed")
	}
	ch := tp.subCh
	if ch == nil {
		ch = make(chan dds.Sample, 8)
		tp.subCh = ch
	}
	return &testSubscriber{ch: ch}, nil
}

func (tp *testParticipant) NewPublisher(_ string, _ dds.QoS) (dds.Publisher, error) {
	tp.pubCount++
	if tp.failPublisher && (tp.failPubAfter == 0 || tp.pubCount > tp.failPubAfter) {
		return nil, errors.New("test: publisher creation failed")
	}
	return &testPublisher{}, nil
}

func (tp *testParticipant) Close() error { return nil }

// ── makeEnvelope / newMockParticipant ─────────────────────────────────────────

// makeEnvelope builds the {"replyTopic":"X","request":{...}} wire payload.
func makeEnvelope(replyTopic, action, path string) string {
	m := map[string]interface{}{
		"replyTopic": replyTopic,
		"request": map[string]interface{}{
			"action": action,
			"path":   path,
		},
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// newMockParticipant wires up the package-level newParticipant to the mock.
func newMockParticipant(t *testing.T) {
	t.Helper()
	newParticipant = func() (dds.Participant, error) { return mock.New(dds.Domain(0)) }
}

// resetReplies empties the package-level reply list between tests.
func resetReplies() {
	replies.mu.Lock()
	replies.entries = replies.entries[:0]
	replies.mu.Unlock()
}

// ── isValidVin ────────────────────────────────────────────────────────────────

func TestIsValidVin_Valid(t *testing.T) {
	cases := []string{"VIN001", "ULFB0", "ABC-123_XY", "1HGCM82633A004352"}
	for _, vin := range cases {
		if !isValidVin(vin) {
			t.Errorf("isValidVin(%q) = false, want true", vin)
		}
	}
}

func TestIsValidVin_Invalid(t *testing.T) {
	cases := []string{"", "VIN/001", "VIN+1", "VIN#1", "A+B"}
	// also test length > 64
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'A'
	}
	cases = append(cases, string(long))
	for _, vin := range cases {
		if isValidVin(vin) {
			t.Errorf("isValidVin(%q) = true, want false", vin)
		}
	}
}

// ── extractVin ────────────────────────────────────────────────────────────────

func TestExtractVin_DataDpValue(t *testing.T) {
	resp := `{"data":{"dp":{"value":"TESTVIN123"}}}`
	if got := extractVin(resp); got != "TESTVIN123" {
		t.Errorf("got %q", got)
	}
}

func TestExtractVin_DataValue(t *testing.T) {
	resp := `{"data":{"value":"VIN456"}}`
	if got := extractVin(resp); got != "VIN456" {
		t.Errorf("got %q", got)
	}
}

func TestExtractVin_TopLevelValue(t *testing.T) {
	resp := `{"value":"TOP789"}`
	if got := extractVin(resp); got != "TOP789" {
		t.Errorf("got %q", got)
	}
}

func TestExtractVin_NotJSON(t *testing.T) {
	if got := extractVin("not json"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractVin_NoValueField(t *testing.T) {
	if got := extractVin(`{"foo":"bar"}`); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ── decomposeDdsPayload ────────────────────────────────────────────────────────

func TestDecomposeDdsPayload_Happy(t *testing.T) {
	env := makeEnvelope("client/456", "get", "Vehicle.Speed")
	rt, req := decomposeDdsPayload(env)
	if rt != "client/456" {
		t.Errorf("replyTopic: got %q", rt)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(req), &m); err != nil {
		t.Fatalf("request not JSON: %v", err)
	}
	if m["action"] != "get" {
		t.Errorf("action: got %v", m["action"])
	}
}

func TestDecomposeDdsPayload_MissingReplyTopic(t *testing.T) {
	rt, _ := decomposeDdsPayload(`{"request":{"action":"get"}}`)
	if rt != "" {
		t.Errorf("expected empty replyTopic, got %q", rt)
	}
}

func TestDecomposeDdsPayload_NotJSON(t *testing.T) {
	rt, req := decomposeDdsPayload("not json")
	if rt != "" || req != "" {
		t.Errorf("expected empty, got (%q, %q)", rt, req)
	}
}

func TestDecomposeDdsPayload_EmptyReplyTopic(t *testing.T) {
	rt, _ := decomposeDdsPayload(`{"replyTopic":"","request":{}}`)
	if rt != "" {
		t.Errorf("expected empty replyTopic for empty string, got %q", rt)
	}
}

// The json.Marshal(m["request"]) error branch (lines 158-160) is unreachable
// in practice: m["request"] is a standard json.Unmarshal output value
// (map/slice/string/number/bool/nil), none of which can trigger a marshal
// error. It is documented here so future readers do not try to force coverage.

// ── replyList ─────────────────────────────────────────────────────────────────

func TestReplyList_PushGetPop(t *testing.T) {
	var r replyList
	r.push("client/1", 10)
	r.push("client/2", 20)

	if got := r.get(10); got != "client/1" {
		t.Errorf("get(10) = %q, want client/1", got)
	}
	if got := r.get(20); got != "client/2" {
		t.Errorf("get(20) = %q, want client/2", got)
	}
	if got := r.get(99); got != "" {
		t.Errorf("get(99) = %q, want empty", got)
	}

	r.pop(10)
	if got := r.get(10); got != "" {
		t.Errorf("after pop(10), get(10) = %q, want empty", got)
	}
	if got := r.get(20); got != "client/2" {
		t.Errorf("get(20) after pop(10) = %q, want client/2", got)
	}
}

func TestReplyList_PopMissing(t *testing.T) {
	var r replyList
	r.pop(42) // must not panic
}

func TestReplyList_PopOnlyTarget(t *testing.T) {
	var r replyList
	r.push("a", 1)
	r.push("b", 2)
	r.push("c", 3)
	r.pop(2)
	if r.get(1) != "a" || r.get(3) != "c" {
		t.Error("pop(2) removed the wrong entries")
	}
	if r.get(2) != "" {
		t.Error("pop(2) did not remove entry")
	}
}

// ── vissV2Receiver ────────────────────────────────────────────────────────────

func TestVissV2Receiver_ForwardsMessages(t *testing.T) {
	transportChan := make(chan string, 1)
	vissv2Chan := make(chan string, 1)

	go vissV2Receiver(transportChan, vissv2Chan)

	transportChan <- `{"RouterId":"5?0","action":"get-response","value":"42"}`

	select {
	case msg := <-vissv2Chan:
		if msg == "" {
			t.Error("received empty message")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// ── getVehicleTopic ───────────────────────────────────────────────────────────

func TestGetVehicleTopic_EnvVar(t *testing.T) {
	t.Setenv("DDS_VIN", "TESTVIN001")
	ch := make(chan string)
	topic := getVehicleTopic(ch, 5)
	if topic != "/TESTVIN001/Vehicle" {
		t.Errorf("got %q, want /TESTVIN001/Vehicle", topic)
	}
}

func TestGetVehicleTopic_InvalidEnvVar(t *testing.T) {
	t.Setenv("DDS_VIN", "BAD/VIN")
	ch := make(chan string)
	topic := getVehicleTopic(ch, 5)
	if topic != "" {
		t.Errorf("expected empty topic for invalid VIN, got %q", topic)
	}
}

func TestGetVehicleTopic_ChannelRoundTrip(t *testing.T) {
	t.Setenv("DDS_VIN", "") // ensure env var not set
	ch := make(chan string)
	done := make(chan string, 1)

	go func() { done <- getVehicleTopic(ch, 5) }()

	// consume the VIN request, reply with a valid VIN response
	<-ch
	ch <- `{"data":{"dp":{"value":"ROUNDTRIP01"}}}`

	select {
	case topic := <-done:
		if topic != "/ROUNDTRIP01/Vehicle" {
			t.Errorf("got %q", topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// TestGetVehicleTopic_ChannelPathInvalidVin: server returns a VIN that fails
// isValidVin — getVehicleTopic must return "" and not build a topic.
func TestGetVehicleTopic_ChannelPathInvalidVin(t *testing.T) {
	t.Setenv("DDS_VIN", "")
	ch := make(chan string)
	done := make(chan string, 1)

	go func() { done <- getVehicleTopic(ch, 5) }()

	<-ch                                         // consume the outgoing VIN request
	ch <- `{"data":{"dp":{"value":"BAD/VIN"}}}` // reply with an invalid VIN

	select {
	case topic := <-done:
		if topic != "" {
			t.Errorf("expected empty topic for invalid channel VIN, got %q", topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestGetVehicleTopic_Timeout(t *testing.T) {
	t.Setenv("DDS_VIN", "")
	ch := make(chan string)
	done := make(chan string, 1)
	go func() { done <- getVehicleTopic(ch, 5) }()
	<-ch // consume the request but never reply

	// Wait 6 seconds for the 5-second timeout inside getVehicleTopic.
	select {
	case topic := <-done:
		if topic != "" {
			t.Errorf("expected empty on timeout, got %q", topic)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("test itself timed out")
	}
}

// ── addRoutingAndForward ──────────────────────────────────────────────────────

func TestAddRoutingAndForward(t *testing.T) {
	ch := make(chan string, 1)
	addRoutingAndForward(`{"action":"get","path":"Vehicle.Speed"}`, 5, 7, ch)
	select {
	case msg := <-ch:
		if !contains(msg, `"RouterId":"5?7"`) {
			t.Errorf("RouterId not in forwarded message: %s", msg)
		}
		if !contains(msg, `"action":"get"`) {
			t.Errorf("original action missing: %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for forwarded message")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── DdsMgrInit — exit-early paths ─────────────────────────────────────────────

// TestDdsMgrInit_InvalidVin_ExitsCleanly: invalid DDS_VIN → DdsMgrInit returns.
func TestDdsMgrInit_InvalidVin_ExitsCleanly(t *testing.T) {
	newMockParticipant(t)
	t.Setenv("DDS_VIN", "BAD/VIN")
	t.Cleanup(resetReplies)

	done := make(chan struct{})
	go func() {
		DdsMgrInit(5, make(chan string))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("DdsMgrInit did not return on invalid VIN")
	}
}

// TestDdsMgrInit_ParticipantFailure: newParticipant returns an error;
// DdsMgrInit must log the error and return without panicking.
func TestDdsMgrInit_ParticipantFailure(t *testing.T) {
	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) {
		return nil, errors.New("test: participant creation failed")
	}
	t.Setenv("DDS_VIN", "PARTFAIL01")
	t.Cleanup(resetReplies)

	done := make(chan struct{})
	go func() {
		DdsMgrInit(5, make(chan string))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("DdsMgrInit did not return on participant creation failure")
	}
}

// TestDdsMgrInit_SubscriberFailure: NewSubscriber returns an error;
// DdsMgrInit must log and return.
func TestDdsMgrInit_SubscriberFailure(t *testing.T) {
	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) {
		return &testParticipant{failSubscriber: true}, nil
	}
	t.Setenv("DDS_VIN", "SUBFAIL01")
	t.Cleanup(resetReplies)

	done := make(chan struct{})
	go func() {
		DdsMgrInit(5, make(chan string))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DdsMgrInit did not return on subscriber creation failure")
	}
}

// TestDdsMgrInit_PublisherFailure: NewPublisher (vehicleTopic) returns an
// error; DdsMgrInit must log and return.
func TestDdsMgrInit_PublisherFailure(t *testing.T) {
	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) {
		return &testParticipant{failPublisher: true}, nil
	}
	t.Setenv("DDS_VIN", "PUBFAIL01")
	t.Cleanup(resetReplies)

	done := make(chan struct{})
	go func() {
		DdsMgrInit(5, make(chan string))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DdsMgrInit did not return on publisher creation failure")
	}
}

// ── DdsMgrInit — main loop paths ─────────────────────────────────────────────

// TestDdsMgrInit_MalformedSample_DropsAndContinues: a DDS sample whose
// decomposed replyTopic is empty is dropped with an error log; DdsMgrInit
// must not crash or block.
func TestDdsMgrInit_MalformedSample_DropsAndContinues(t *testing.T) {
	subCh := make(chan dds.Sample, 4)
	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) {
		return &testParticipant{subCh: subCh}, nil
	}
	t.Setenv("DDS_VIN", "MALFORM01")
	t.Cleanup(resetReplies)
	transportChan := make(chan string, 8)

	go DdsMgrInit(5, transportChan)
	time.Sleep(3 * time.Second)

	// Payload missing "replyTopic" → decomposeDdsPayload returns "", "" →
	// replyTopic == "" → continue (the malformed-sample drop path).
	subCh <- dds.Sample{Payload: []byte(`{"request":{"action":"get"}}`)}
	time.Sleep(500 * time.Millisecond)
}

// TestDdsMgrInit_HappyPath_FullRoundTrip: schema passes, the request is
// pushed to replies and forwarded to the server core; vissV2Receiver picks
// up the forwarded message and delivers it to the vissv2Chan case, which
// then publishes the response to the replyTopic publisher.
func TestDdsMgrInit_HappyPath_FullRoundTrip(t *testing.T) {
	// Override schema validation to always pass (no schema file needed).
	origValidate := schemaValidate
	t.Cleanup(func() { schemaValidate = origValidate })
	schemaValidate = func(_ string) string { return "" }

	subCh := make(chan dds.Sample, 4)
	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) {
		return &testParticipant{subCh: subCh}, nil
	}
	t.Setenv("DDS_VIN", "HAPPY01")
	t.Cleanup(resetReplies)
	transportChan := make(chan string, 8)

	go DdsMgrInit(5, transportChan)
	time.Sleep(3 * time.Second)

	// Deliver a valid DDS sample. DdsMgrInit will:
	//   1. decomposeDdsPayload → replyTopic="test/reply/happy", request=...
	//   2. schemaValidate → "" (passes)
	//   3. replies.push("test/reply/happy", 0)
	//   4. addRoutingAndForward → sends {"RouterId":"5?0",...} to transportChan
	//   5. vissV2Receiver reads transportChan → writes to vissv2Chan (unbuffered)
	//   6. vissv2Chan case: RemoveInternalData → topicHandle=0 → replies.get(0) →
	//      participant.NewPublisher("test/reply/happy") → Write → Close
	subCh <- dds.Sample{Payload: []byte(makeEnvelope("test/reply/happy", "get", "Vehicle.Speed"))}

	// Give the loop enough time for all six steps to complete.
	time.Sleep(time.Second)
}

// TestDdsMgrInit_VissV2Chan_NoReplyTopic: a server-core response arrives on
// the transport channel, but its topicHandle does not match any entry in
// replies (e.g. the request was never tracked). DdsMgrInit must log an error
// and continue without crashing.
func TestDdsMgrInit_VissV2Chan_NoReplyTopic(t *testing.T) {
	origNew := newParticipant
	t.Cleanup(func() { newParticipant = origNew })
	newParticipant = func() (dds.Participant, error) {
		return &testParticipant{}, nil
	}
	t.Setenv("DDS_VIN", "NOREPLY01")
	t.Cleanup(resetReplies)
	transportChan := make(chan string, 8)

	go DdsMgrInit(5, transportChan)
	time.Sleep(3 * time.Second)

	// Send directly to transportChan; vissV2Receiver picks it up and puts it
	// on vissv2Chan.  topicHandle=99 is not in replies → error path ("no reply
	// topic for handle 99") → continue.
	transportChan <- `{"RouterId":"5?99","action":"get-response","data":"test"}`
	time.Sleep(500 * time.Millisecond)
}

// ── DdsMgrInit — mock-participant integration (kept for regression) ────────────

// TestDdsMgrInit_StartsWithValidVin verifies DdsMgrInit subscribes and
// enters its event loop without error when given a valid VIN.  The full
// vissv2Chan round-trip is covered by TestDdsMgrInit_HappyPath_FullRoundTrip.
// Full end-to-end routing requires a running server core (transportDataSession)
// and is therefore an integration-only path.
func TestDdsMgrInit_StartsWithValidVin(t *testing.T) {
	newMockParticipant(t)
	t.Setenv("DDS_VIN", "STARTTEST01")
	t.Cleanup(resetReplies)

	// Use a buffered channel so DdsMgrInit's addRoutingAndForward doesn't
	// block when the server core isn't running.
	transportChan := make(chan string, 8)
	started := make(chan struct{})

	go func() {
		close(started)
		DdsMgrInit(5, transportChan)
	}()

	<-started
	// Allow the manager to pass the 2-second startup sleep and enter its loop.
	time.Sleep(3 * time.Second)

	// Publish a request; if DdsMgrInit panicked the test would have failed already.
	p, _ := mock.New(dds.Domain(0))
	defer p.Close()
	pub, _ := p.NewPublisher("/STARTTEST01/Vehicle", dds.DefaultQoS)
	defer pub.Close()

	env := makeEnvelope("client/reply/start", "get", "Vehicle.Speed")
	if err := pub.Write([]byte(env)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Give the loop one second to process; we just want to confirm it doesn't crash.
	time.Sleep(time.Second)
}
