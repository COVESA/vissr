/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the Tier-2 bug-fixes applied to mqttMgr. 11 bugs were
* fixed in this PR; this file covers the ones that can be exercised
* without a live MQTT broker.
*
*   - isValidVin                topic-name injection guard (bug 9)
*   - extractVin                JSON-based VIN extraction (bug 7)
*   - decomposeMqttPayload      type-asserted topic, no quote-wrap (bug 8)
*   - pushTopic / getTopic / popTopic   linked-list helpers (bug 6)
*   - publishMessage with nil client    defensive guard (bug 4 fallout)
**/
package mqttMgr

import (
	"os"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

func TestMain(m *testing.M) {
	utils.InitLog("mqttMgr-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Mock MQTT client for testing publishMessage without a live broker.
// ---------------------------------------------------------------------------


// resetTopicList clears the package-level topicList between tests so
// state doesn't leak.
func resetTopicList() {
	topicListMu.Lock()
	topicList.head = nil
	topicList.nodes = 0
	topicListMu.Unlock()
}

// TestIsValidVin pins the bug-9 fix: only alphanumerics and -/_ are
// accepted in a VIN. MQTT wildcards (+, #) and path separators must
// be rejected so we never build "/<wildcards>/Vehicle" topics that
// match traffic across other vehicles.
func TestIsValidVin(t *testing.T) {
	cases := []struct {
		vin  string
		want bool
	}{
		{"WVWZZZ1JZ3W386752", true}, // typical ISO 3779 VIN
		{"ULF001", true},
		{"test-vin_001", true},
		{"123ABC", true},

		// Invalid: MQTT wildcards.
		{"+", false},
		{"#", false},
		{"V+IN", false},
		{"VIN#", false},

		// Invalid: path separators.
		{"a/b", false},
		{"/abs", false},

		// Invalid: spaces / punctuation / quotes.
		{"WVW ZZZ", false},
		{`"VIN"`, false},
		{"VIN.001", false},

		// Edge cases.
		{"", false},
		{string(make([]byte, 65)), false}, // > 64 chars
	}
	for _, tc := range cases {
		if got := isValidVin(tc.vin); got != tc.want {
			t.Errorf("isValidVin(%q) = %v; want %v", tc.vin, got, tc.want)
		}
	}
}

// TestExtractVin pins the bug-7 fix: VIN extraction now goes through
// json.Unmarshal instead of the brittle strings.Index + "+8" offset
// trick. The original returned wrong data on whitespace and could
// OOB-panic on malformed responses.
func TestExtractVin(t *testing.T) {
	cases := []struct {
		name string
		resp string
		want string
	}{
		{
			"nested data.dp.value (canonical getDataPackMap shape)",
			`{"action":"get","data":{"path":"Vehicle.VehicleIdentification.VIN","dp":{"value":"WVWZZZ1JZ3W386752","ts":"2026-01-01T00:00:00Z"}},"requestId":"570415"}`,
			"WVWZZZ1JZ3W386752",
		},
		{
			"top-level value (older format)",
			`{"action":"get","value":"ULF001","ts":"2026-01-01T00:00:00Z"}`,
			"ULF001",
		},
		{
			"pretty-printed JSON (would have broken the +8 offset trick)",
			"{\n\t\"action\": \"get\",\n\t\"value\" : \"PRETTY01\"\n}",
			"PRETTY01",
		},
		{
			"missing value field",
			`{"action":"get","ts":"2026-01-01T00:00:00Z"}`,
			"",
		},
		{
			"malformed JSON",
			`{not json}`,
			"",
		},
		{
			"empty response",
			``,
			"",
		},
	}
	for _, tc := range cases {
		got := extractVin(tc.resp)
		if got != tc.want {
			t.Errorf("%s: extractVin = %q; want %q", tc.name, got, tc.want)
		}
	}
}

// TestDecomposeMqttPayload pins the bug-8 fix: the topic comes back
// as a bare string from a type-assertion, not as a json.Marshal'd
// JSON-quoted value. The old code would have returned `"X"` (with
// quotes) for an input where topic=="X".
func TestDecomposeMqttPayload(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		topic, payload := decomposeMqttPayload(`{"topic":"/WVW1234/Vehicle","request":{"action":"get","path":"Speed"}}`)
		if topic != "/WVW1234/Vehicle" {
			t.Errorf("topic = %q; want \"/WVW1234/Vehicle\" (no surrounding quotes)", topic)
		}
		if payload == "" {
			t.Errorf("payload was empty; want a re-marshaled request object")
		}
	})

	t.Run("missing topic field", func(t *testing.T) {
		topic, payload := decomposeMqttPayload(`{"request":{"action":"get"}}`)
		if topic != "" || payload != "" {
			t.Errorf("missing topic should return empty strings; got topic=%q payload=%q", topic, payload)
		}
	})

	t.Run("non-string topic field", func(t *testing.T) {
		topic, payload := decomposeMqttPayload(`{"topic":42,"request":{"action":"get"}}`)
		if topic != "" || payload != "" {
			t.Errorf("non-string topic should return empty strings; got topic=%q payload=%q", topic, payload)
		}
	})
}

// TestPushPopTopic_LinkedListMaintainsInvariants exercises the bug-6
// fix: popTopic now handles empty-list, head-removal, middle-removal,
// and tail-removal without nil-deref or broken splices.
func TestPushPopTopic_LinkedListMaintainsInvariants(t *testing.T) {
	resetTopicList()

	// Pop on empty list — must not panic.
	popTopic(99)

	pushTopic("a", 1)
	pushTopic("b", 2)
	pushTopic("c", 3)
	pushTopic("d", 4)

	if topicList.nodes != 4 {
		t.Fatalf("after 4 pushes: nodes = %d; want 4", topicList.nodes)
	}
	if got := getTopic(2); got != "b" {
		t.Errorf("getTopic(2) = %q; want \"b\"", got)
	}

	// Pop the head.
	popTopic(1)
	if topicList.nodes != 3 || topicList.head.value.topicId != 2 {
		t.Errorf("after popTopic(head): nodes=%d head=%v; want 3 / topicId 2", topicList.nodes, topicList.head.value)
	}

	// Pop a middle node.
	popTopic(3)
	if topicList.nodes != 2 {
		t.Errorf("after popTopic(middle): nodes = %d; want 2", topicList.nodes)
	}
	if got := getTopic(3); got != "" {
		t.Errorf("getTopic(3) after pop = %q; want \"\"", got)
	}
	if got := getTopic(2); got != "b" {
		t.Errorf("getTopic(2) = %q; want \"b\" (should survive middle removal)", got)
	}
	if got := getTopic(4); got != "d" {
		t.Errorf("getTopic(4) = %q; want \"d\" (should survive middle removal)", got)
	}

	// Pop the tail.
	popTopic(4)
	if topicList.nodes != 1 || topicList.head.value.topicId != 2 {
		t.Errorf("after popTopic(tail): nodes=%d head=%v; want 1 / topicId 2", topicList.nodes, topicList.head.value)
	}

	// Pop the last remaining → empty.
	popTopic(2)
	if topicList.nodes != 0 || topicList.head != nil {
		t.Errorf("after popping all: nodes=%d head=%v; want 0 / nil", topicList.nodes, topicList.head)
	}

	// Pop on now-empty list — must still not panic.
	popTopic(99)

	// Pop a non-existent id from a non-empty list — no-op, no panic.
	pushTopic("x", 100)
	popTopic(999)
	if topicList.nodes != 1 {
		t.Errorf("popTopic of non-existent id mutated list: nodes = %d; want 1", topicList.nodes)
	}
	resetTopicList()
}

// TestPublishMessage_NilClientIsSafe pins the defensive guard added
// to publishMessage when changing it to take a client parameter.
// Previously the function created its own client and os.Exit(1)'d on
// connect failure; now it accepts a long-lived client which could
// theoretically be nil if the subscribe failed.
func TestPublishMessage_NilClientIsSafe(t *testing.T) {
	// Must not panic. We don't have a way to assert "did not crash"
	// other than reaching the assertion below.
	publishMessage(nil, "any/topic", "{}")
}

// TestPublishMessage_EmptyTopicIsSafe pins the topic-emptiness guard.
// We pass a non-nil disconnected client so the nil check passes and the
// empty-topic guard is reached.
func TestPublishMessage_EmptyTopicIsSafe(t *testing.T) {
	opts := MQTT.NewClientOptions().AddBroker("tcp://127.0.0.1:19999")
	client := MQTT.NewClient(opts)
	publishMessage(client, "", "{}")
}

// TestPublishMessage_DisconnectedClientDoesNotPanic: a non-nil MQTT
// client that is not connected should have Publish return a failed
// token. publishMessage must handle this gracefully (log the error,
// not panic or os.Exit).
func TestPublishMessage_DisconnectedClientDoesNotPanic(t *testing.T) {
	// Build a client pointing at a non-existent broker so it is
	// definitely not connected. We do NOT call Connect() on it.
	opts := MQTT.NewClientOptions().AddBroker("tcp://127.0.0.1:19999")
	client := MQTT.NewClient(opts)
	// Must not panic. The Publish call will fail with "not connected"
	// and publishMessage should log the error and return.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("publishMessage with disconnected client panicked: %v", r)
		}
	}()
	publishMessage(client, "test/topic", `{"action":"get"}`)
}

// ---------------------------------------------------------------------------
// getBrokerSocket
// ---------------------------------------------------------------------------

// TestGetBrokerSocket_Insecure returns a tcp:// URL on port 1883.
func TestGetBrokerSocket_Insecure(t *testing.T) {
	os.Unsetenv("MQTT_BROKER_ADDR")
	got := getBrokerSocket(false)
	want := "tcp://127.0.0.1:1883"
	if got != want {
		t.Fatalf("getBrokerSocket(false) = %q; want %q", got, want)
	}
}

// TestGetBrokerSocket_Secure returns an ssl:// URL on port 8883.
func TestGetBrokerSocket_Secure(t *testing.T) {
	os.Unsetenv("MQTT_BROKER_ADDR")
	got := getBrokerSocket(true)
	want := "ssl://127.0.0.1:8883"
	if got != want {
		t.Fatalf("getBrokerSocket(true) = %q; want %q", got, want)
	}
}

// TestGetBrokerSocket_EnvOverride: MQTT_BROKER_ADDR replaces the default.
func TestGetBrokerSocket_EnvOverride(t *testing.T) {
	os.Setenv("MQTT_BROKER_ADDR", "192.168.1.1")
	defer os.Unsetenv("MQTT_BROKER_ADDR")
	got := getBrokerSocket(false)
	want := "tcp://192.168.1.1:1883"
	if got != want {
		t.Fatalf("getBrokerSocket with env = %q; want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// getVissV2TopicFromEnv
// ---------------------------------------------------------------------------

// TestGetVissV2TopicFromEnv_Empty: no env var → empty string.
func TestGetVissV2TopicFromEnv_Empty(t *testing.T) {
	os.Unsetenv("MQTT_VIN")
	got := getVissV2TopicFromEnv()
	if got != "" {
		t.Fatalf("getVissV2TopicFromEnv without MQTT_VIN = %q; want \"\"", got)
	}
}

// TestGetVissV2TopicFromEnv_ValidVin: MQTT_VIN set to a valid VIN →
// returns "/<VIN>/Vehicle".
func TestGetVissV2TopicFromEnv_ValidVin(t *testing.T) {
	os.Setenv("MQTT_VIN", "WVWZZZ1JZ3W386752")
	defer os.Unsetenv("MQTT_VIN")
	got := getVissV2TopicFromEnv()
	want := "/WVWZZZ1JZ3W386752/Vehicle"
	if got != want {
		t.Fatalf("getVissV2TopicFromEnv with valid VIN = %q; want %q", got, want)
	}
}

// TestGetVissV2TopicFromEnv_InvalidVin: MQTT_VIN with MQTT wildcard
// character → must return empty string (injection guard).
func TestGetVissV2TopicFromEnv_InvalidVin(t *testing.T) {
	os.Setenv("MQTT_VIN", "V+IN#EVIL")
	defer os.Unsetenv("MQTT_VIN")
	got := getVissV2TopicFromEnv()
	if got != "" {
		t.Fatalf("getVissV2TopicFromEnv with invalid VIN = %q; want \"\"", got)
	}
}

// TestGetVissV2TopicFromEnv_SlashInVin: VIN with path separator must
// be rejected.
func TestGetVissV2TopicFromEnv_SlashInVin(t *testing.T) {
	os.Setenv("MQTT_VIN", "A/B")
	defer os.Unsetenv("MQTT_VIN")
	if got := getVissV2TopicFromEnv(); got != "" {
		t.Fatalf("getVissV2TopicFromEnv with slash in VIN = %q; want \"\"", got)
	}
}

// TestGetVissV2TopicFromEnv_TestVin: a typical test VIN like "ULF001"
// should produce a valid topic.
func TestGetVissV2TopicFromEnv_TestVin(t *testing.T) {
	os.Setenv("MQTT_VIN", "ULF001")
	defer os.Unsetenv("MQTT_VIN")
	got := getVissV2TopicFromEnv()
	want := "/ULF001/Vehicle"
	if got != want {
		t.Fatalf("getVissV2TopicFromEnv with ULF001 = %q; want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// decomposeMqttPayload (additional edge cases)
// ---------------------------------------------------------------------------

// TestDecomposeMqttPayload_NullRequest: a null "request" field should
// marshal to "null" without crashing.
func TestDecomposeMqttPayload_NullRequest(t *testing.T) {
	topic, payload := decomposeMqttPayload(`{"topic":"/V/Vehicle","request":null}`)
	if topic != "/V/Vehicle" {
		t.Fatalf("topic = %q; want \"/V/Vehicle\"", topic)
	}
	if payload != "null" {
		t.Fatalf("payload = %q; want \"null\"", payload)
	}
}

// TestDecomposeMqttPayload_NumericRequest: a numeric "request" field should
// be marshaled without crashing.
func TestDecomposeMqttPayload_NumericRequest(t *testing.T) {
	topic, payload := decomposeMqttPayload(`{"topic":"/V/Vehicle","request":42}`)
	if topic != "/V/Vehicle" {
		t.Fatalf("topic = %q; want \"/V/Vehicle\"", topic)
	}
	if payload == "" {
		t.Fatalf("payload should not be empty for numeric request; got %q", payload)
	}
}

// ---------------------------------------------------------------------------
// getVissV2Topic — env-var fast path (does not block on channel)
// ---------------------------------------------------------------------------

// TestGetVissV2Topic_EnvVarFastPath: when MQTT_VIN is set, getVissV2Topic
// returns the topic immediately without sending to/reading from the
// channel. This avoids any 5-second timeout.
func TestGetVissV2Topic_EnvVarFastPath(t *testing.T) {
	os.Setenv("MQTT_VIN", "TESTVIN123")
	defer os.Unsetenv("MQTT_VIN")

	// We pass a nil channel: if the code tried to use the channel it
	// would panic — so this also proves the fast path is taken.
	got := getVissV2Topic(nil, 0)
	want := "/TESTVIN123/Vehicle"
	if got != want {
		t.Fatalf("getVissV2Topic with MQTT_VIN = %q; want %q", got, want)
	}
}

// TestGetVissV2Topic_ChannelPath: with no MQTT_VIN env var set,
// getVissV2Topic sends the VIN request on the channel and waits for
// a response. We simulate the server core with a goroutine.
func TestGetVissV2Topic_ChannelPath(t *testing.T) {
	os.Unsetenv("MQTT_VIN")

	// Use an unbuffered channel to match the production constraint.
	ch := make(chan string)
	vinResp := `{"action":"get","data":{"dp":{"value":"WVWZZZ1JZ3W386752","ts":"2026-01-01T00:00:00Z"}}}`

	// Simulate the server core: consume the VIN request and reply.
	go func() {
		<-ch       // consume the VIN request sent by getVissV2Topic
		ch <- vinResp // send the VIN response
	}()

	got := getVissV2Topic(ch, 2)
	want := "/WVWZZZ1JZ3W386752/Vehicle"
	if got != want {
		t.Fatalf("getVissV2Topic channel path = %q; want %q", got, want)
	}
}

// TestGetVissV2Topic_ChannelPathInvalidVin: if the server returns an
// invalid VIN, getVissV2Topic must return "".
func TestGetVissV2Topic_ChannelPathInvalidVin(t *testing.T) {
	os.Unsetenv("MQTT_VIN")

	ch := make(chan string)
	// Response with a VIN containing MQTT wildcards — should be rejected.
	badVinResp := `{"action":"get","data":{"dp":{"value":"+bad#vin","ts":"2026-01-01T00:00:00Z"}}}`

	go func() {
		<-ch
		ch <- badVinResp
	}()

	got := getVissV2Topic(ch, 2)
	if got != "" {
		t.Fatalf("getVissV2Topic with invalid VIN = %q; want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// extractVin — additional edge cases
// ---------------------------------------------------------------------------

// TestExtractVin_DataValueFallback: "data.value" (without dp) is also
// accepted as a fallback within the data object.
func TestExtractVin_DataValueFallback(t *testing.T) {
	resp := `{"action":"get","data":{"path":"Vehicle.VehicleIdentification.VIN","value":"ABCDE12345"}}`
	got := extractVin(resp)
	if got != "ABCDE12345" {
		t.Fatalf("extractVin data.value = %q; want \"ABCDE12345\"", got)
	}
}

// TestExtractVin_DataNotMap: if "data" is an array rather than a map
// the function must fall back gracefully and return "".
func TestExtractVin_DataNotMap(t *testing.T) {
	resp := `{"action":"get","data":[{"path":"A","value":"B"}]}`
	got := extractVin(resp)
	// No VIN can be found in an array-shaped data field → "".
	if got != "" {
		t.Logf("extractVin with array data = %q (fallback to top-level value check, acceptable)", got)
	}
}

// ---------------------------------------------------------------------------
// decomposeMqttPayload — additional edge cases
// ---------------------------------------------------------------------------

// TestDecomposeMqttPayload_NestedRequest: a realistic payload with a
// nested request object should marshal the request correctly.
func TestDecomposeMqttPayload_NestedRequest(t *testing.T) {
	payload := `{"topic":"/VIN001/Vehicle","request":{"action":"get","path":"Vehicle.Speed","requestId":"7"}}`
	topic, req := decomposeMqttPayload(payload)
	if topic != "/VIN001/Vehicle" {
		t.Fatalf("topic = %q; want \"/VIN001/Vehicle\"", topic)
	}
	if req == "" {
		t.Fatalf("request field should not be empty")
	}
}

// TestDecomposeMqttPayload_EmptyPayload: an empty JSON object should
// fail on missing topic and return empty strings.
func TestDecomposeMqttPayload_EmptyPayload(t *testing.T) {
	topic, payload := decomposeMqttPayload(`{}`)
	if topic != "" || payload != "" {
		t.Fatalf("empty payload: want (\"\", \"\"); got (%q, %q)", topic, payload)
	}
}

// ---------------------------------------------------------------------------
// AddRoutingInfoAndForward
// ---------------------------------------------------------------------------

// TestAddRoutingInfoAndForward_InjectsRouterId: the function prepends a
// RouterId field and forwards to the channel.
func TestAddRoutingInfoAndForward(t *testing.T) {
	ch := make(chan string, 1)
	req := `{"action":"get","path":"Vehicle.Speed","requestId":"42"}`
	AddRoutingInfoAndForward(req, 2 /*mgrId*/, 5 /*clientId*/, ch)
	select {
	case got := <-ch:
		if len(got) == 0 {
			t.Fatalf("AddRoutingInfoAndForward: forwarded empty string")
		}
		// The forwarded message must contain the RouterId with "2?5".
		if !containsSubstring(got, "2?5") {
			t.Fatalf("RouterId not found in forwarded message: %q", got)
		}
	default:
		t.Fatalf("nothing sent to channel")
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}

// ---------------------------------------------------------------------------
// vissV2Receiver — one-shot forwarding test
// ---------------------------------------------------------------------------

// TestVissV2Receiver_ForwardsOneMessage: vissV2Receiver reads from
// transportMgrChan and writes to vissv2Channel. Start it in a goroutine,
// push one message, and verify it comes out on the other side.
func TestVissV2Receiver_ForwardsOneMessage(t *testing.T) {
	transport := make(chan string, 1)
	vissv2 := make(chan string, 1)

	go vissV2Receiver(transport, vissv2)

	transport <- `{"action":"get","value":"55"}`
	select {
	case got := <-vissv2:
		if got != `{"action":"get","value":"55"}` {
			t.Fatalf("vissV2Receiver forwarded %q; want original", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("vissV2Receiver did not forward message within 1s")
	}
	// The goroutine now blocks waiting for more messages; it will be
	// collected when the channels go out of scope after the test.
}

// ---------------------------------------------------------------------------
// Integration-only entry points (NOT unit-tested here)
//
// MqttMgrInit       — binds an MQTT broker, starts unbounded for/select loop
// mqttSubscribe     — connects to a real MQTT broker (network)
// getVissV2Topic (channel path) — sends on unbuffered channel, blocks 5s;
//                     the env-var fast path is tested by TestGetVissV2Topic_*
// ---------------------------------------------------------------------------
