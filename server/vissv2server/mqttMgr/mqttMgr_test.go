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

	"github.com/covesa/vissr/utils"
)

func TestMain(m *testing.M) {
	utils.InitLog("mqttMgr-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

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
func TestPublishMessage_EmptyTopicIsSafe(t *testing.T) {
	publishMessage(nil, "", "{}")
}
