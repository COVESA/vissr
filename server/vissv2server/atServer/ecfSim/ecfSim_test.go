/**
* (C) 2026 Matt Jones
*
* Unit tests for ecfSimulator.
*
* The testable pure/logic functions are:
*   extractMessageId — parses messageId from a JSON request string
*   createReply      — builds a consent-reply JSON string
*   dispatchResponse — builds a response JSON string (limited: unchecked assertion)
*
* Integration-only entry points (main, initEcfComm, reDialer, ecfClient,
* ecfReceiver, uiDialogue, prepareCancelRequest) require a live WebSocket
* connection or interactive stdin and are not unit-tested here.
**/
package main

import (
	"strings"
	"testing"
)

// ── extractMessageId ──────────────────────────────────────────────────────────

func TestExtractMessageId_Happy(t *testing.T) {
	req := `{"action":"consent-request","messageId":"abc-123"}`
	got := extractMessageId(req)
	if got != "abc-123" {
		t.Errorf("extractMessageId = %q; want %q", got, "abc-123")
	}
}

func TestExtractMessageId_Missing(t *testing.T) {
	req := `{"action":"get","path":"Vehicle.Speed"}`
	got := extractMessageId(req)
	if got != "" {
		t.Errorf("extractMessageId with no messageId = %q; want empty", got)
	}
}

func TestExtractMessageId_InvalidJSON(t *testing.T) {
	got := extractMessageId("not json")
	if got != "" {
		t.Errorf("extractMessageId invalid JSON = %q; want empty", got)
	}
}

// ── createReply ───────────────────────────────────────────────────────────────

func TestCreateReply_ConsentYes(t *testing.T) {
	req := `{"action":"consent-request","messageId":"xyz"}`
	got := createReply(req, true)
	if !strings.Contains(got, `"consent":"YES"`) {
		t.Errorf("createReply(true) = %q; want consent YES", got)
	}
	if !strings.Contains(got, `"messageId":"xyz"`) {
		t.Errorf("createReply(true) = %q; want messageId xyz", got)
	}
	if !strings.Contains(got, `"action":"consent-reply"`) {
		t.Errorf("createReply(true) = %q; want action consent-reply", got)
	}
}

func TestCreateReply_ConsentNo(t *testing.T) {
	req := `{"action":"consent-request","messageId":"xyz"}`
	got := createReply(req, false)
	if !strings.Contains(got, `"consent":"NO"`) {
		t.Errorf("createReply(false) = %q; want consent NO", got)
	}
}

func TestCreateReply_InvalidJSON(t *testing.T) {
	got := createReply("not json", true)
	if got != "" {
		t.Errorf("createReply invalid JSON = %q; want empty", got)
	}
}

// Integration-only entry points — NOT unit-tested here:
//
//   initEcfComm        — dials a real WebSocket server
//   reDialer           — dials a real WebSocket server (15 retries)
//   ecfClient          — goroutine loop over a real conn
//   ecfReceiver        — goroutine loop over a real conn
//   dispatchResponse   — contains unchecked requestMap["action"].(string) assertion
//   uiDialogue         — reads from interactive stdin
//   prepareCancelRequest — reads from interactive stdin
//   main               — select loop with tickers and WebSocket I/O
