package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/covesa/vissr/utils"
)

func TestMain(m *testing.M) {
	utils.InitLog("dds-client-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// ── buildEnvelope ─────────────────────────────────────────────────────────────

// TestBuildEnvelope_Happy: valid JSON request is wrapped in the DDS envelope.
func TestBuildEnvelope_Happy(t *testing.T) {
	request := `{"action":"get","path":"Vehicle.Speed","requestId":"1"}`
	envelope, ok := buildEnvelope("/client/reply/abc", request)
	if !ok {
		t.Fatalf("buildEnvelope returned ok=false for valid request")
	}

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(envelope), &m); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}
	if m["replyTopic"] != "/client/reply/abc" {
		t.Errorf("replyTopic = %v; want \"/client/reply/abc\"", m["replyTopic"])
	}
	req, ok := m["request"].(map[string]interface{})
	if !ok {
		t.Fatalf("request field is not an object: %T", m["request"])
	}
	if req["action"] != "get" {
		t.Errorf("request.action = %v; want \"get\"", req["action"])
	}
}

// TestBuildEnvelope_InvalidJSON: a non-JSON request returns ("", false).
func TestBuildEnvelope_InvalidJSON(t *testing.T) {
	envelope, ok := buildEnvelope("/client/reply/abc", "not json {{}}")
	if ok || envelope != "" {
		t.Errorf("expected ok=false and empty envelope for invalid JSON; got ok=%v envelope=%q", ok, envelope)
	}
}

// TestBuildEnvelope_EmptyObject: an empty JSON object is a valid request.
func TestBuildEnvelope_EmptyObject(t *testing.T) {
	_, ok := buildEnvelope("/reply", "{}")
	if !ok {
		t.Error("buildEnvelope with empty object returned ok=false; want ok=true")
	}
}

// TestBuildEnvelope_ReplyTopicPreserved: the replyTopic string is copied
// verbatim; no extra quoting or escaping is applied.
func TestBuildEnvelope_ReplyTopicPreserved(t *testing.T) {
	topic := "/client/reply/uuid-1234-5678"
	envelope, ok := buildEnvelope(topic, `{"action":"subscribe","path":"Vehicle.ADAS"}`)
	if !ok {
		t.Fatal("buildEnvelope returned ok=false")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(envelope), &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if m["replyTopic"] != topic {
		t.Errorf("replyTopic = %q; want %q", m["replyTopic"], topic)
	}
}

// TestBuildEnvelope_RequestEmbeddedAsObject: the inner request must be an
// embedded JSON object (not a JSON-encoded string), so that the server can
// unmarshal the full envelope in one step.
func TestBuildEnvelope_RequestEmbeddedAsObject(t *testing.T) {
	request := `{"action":"set","path":"Vehicle.Speed","value":"60"}`
	envelope, ok := buildEnvelope("/r", request)
	if !ok {
		t.Fatal("buildEnvelope returned ok=false")
	}
	// Confirm the request field is NOT a string (i.e. not double-encoded).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(envelope), &raw); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	var reqMap map[string]interface{}
	if err := json.Unmarshal(raw["request"], &reqMap); err != nil {
		t.Fatalf("request field not a JSON object (may be double-encoded): %v", err)
	}
}

// Integration-only entry points — NOT unit-tested here:
//
//   main()             — connects to DDS broker, fmt.Scanf interactive loop
//   response goroutine — reads from sub.C() forever (unbounded select)
