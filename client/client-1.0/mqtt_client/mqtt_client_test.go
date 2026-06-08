/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
* Coverage tests for the unit-testable pure functions in mqtt_client:
* getBrokerSocket and buildPublishPayload.
*
* The network-driven entry points (mqttSubscribe, publishMessage,
* subscribeVissV2Response, publishVissV2Request, main) require a running
* MQTT broker or a TTY and are not unit-tested here.
**/
package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("mqtt_client-test.log", os.TempDir(), false, "error")
}

// ── getBrokerSocket ───────────────────────────────────────────────────────────

func TestGetBrokerSocket_Insecure(t *testing.T) {
	got := getBrokerSocket(false)
	if !strings.HasPrefix(got, "tcp://") {
		t.Errorf("insecure socket = %q; want tcp:// prefix", got)
	}
	if !strings.Contains(got, ":1883") {
		t.Errorf("insecure socket = %q; want port 1883", got)
	}
}

func TestGetBrokerSocket_Secure(t *testing.T) {
	got := getBrokerSocket(true)
	if !strings.HasPrefix(got, "ssl://") {
		t.Errorf("secure socket = %q; want ssl:// prefix", got)
	}
	if !strings.Contains(got, ":8883") {
		t.Errorf("secure socket = %q; want port 8883", got)
	}
}

// ── buildPublishPayload ───────────────────────────────────────────────────────

// TestBuildPublishPayload_Happy: valid request and topic produce a JSON envelope.
func TestBuildPublishPayload_Happy(t *testing.T) {
	request := `{"action":"get","path":"Vehicle.Speed","requestId":"1"}`
	payload, ok := buildPublishPayload("/my/reply/topic", request)
	if !ok {
		t.Fatalf("buildPublishPayload ok=false for valid inputs")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	if m["topic"] != "/my/reply/topic" {
		t.Errorf("topic = %v; want \"/my/reply/topic\"", m["topic"])
	}
	req, ok := m["request"].(map[string]interface{})
	if !ok {
		t.Fatalf("request field is not an object: %T", m["request"])
	}
	if req["action"] != "get" {
		t.Errorf("request.action = %v; want \"get\"", req["action"])
	}
}

// TestBuildPublishPayload_InvalidJSON: non-JSON request returns ("", false).
func TestBuildPublishPayload_InvalidJSON(t *testing.T) {
	payload, ok := buildPublishPayload("/topic", "not-json{}")
	if ok || payload != "" {
		t.Errorf("expected ok=false for invalid JSON; got ok=%v payload=%q", ok, payload)
	}
}

// TestBuildPublishPayload_EmptyTopic: empty reply topic returns ("", false).
func TestBuildPublishPayload_EmptyTopic(t *testing.T) {
	payload, ok := buildPublishPayload("", `{"action":"get"}`)
	if ok || payload != "" {
		t.Errorf("expected ok=false for empty topic; got ok=%v payload=%q", ok, payload)
	}
}

// TestBuildPublishPayload_RequestEmbeddedAsObject: the request field must be
// an embedded JSON object (not double-encoded as a string).
func TestBuildPublishPayload_RequestEmbeddedAsObject(t *testing.T) {
	request := `{"action":"set","path":"Vehicle.Speed","value":"60"}`
	payload, ok := buildPublishPayload("/r", request)
	if !ok {
		t.Fatal("buildPublishPayload returned ok=false")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	var reqMap map[string]interface{}
	if err := json.Unmarshal(raw["request"], &reqMap); err != nil {
		t.Fatalf("request field not a JSON object (may be double-encoded): %v", err)
	}
}

// Integration-only entry points — NOT unit-tested here:
//
//   mqttSubscribe          — connects to a real MQTT broker
//   publishMessage         — connects to a real MQTT broker
//   subscribeVissV2Response — calls mqttSubscribe
//   publishVissV2Request   — calls publishMessage
//   main                   — interactive TTY loop
