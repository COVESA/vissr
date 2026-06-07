package ddsMgr

import (
	"encoding/json"
	"testing"
	"time"

	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/mock"
	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("ddsMgr-test-log.txt", "/tmp", false, "error")
}

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

// ── getVehicleTopic (env-var fast path) ───────────────────────────────────────

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

// ── DdsMgrInit integration (mock) ─────────────────────────────────────────────

func TestDdsMgrInit_InvalidVin_ExitsCleanly(t *testing.T) {
	newMockParticipant(t)
	t.Setenv("DDS_VIN", "BAD/VIN")

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

// TestAddRoutingAndForward verifies that addRoutingAndForward injects the
// correct RouterId prefix into the request and sends it on the channel.
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

// TestDdsMgrInit_StartsWithValidVin verifies DdsMgrInit subscribes and
// enters its event loop without error when given a valid VIN.
// Full end-to-end routing is covered by the unit tests above; the full
// DdsMgrInit loop requires a running server core (transportDataSession)
// to act as the other end of transportMgrChan and is an integration-only path.
func TestDdsMgrInit_StartsWithValidVin(t *testing.T) {
	newMockParticipant(t)
	t.Setenv("DDS_VIN", "STARTTEST01")

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
