/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for decodeWsRequestPayload, forwardWsRequest, and
* encodeWsResponsePayload — the three small helpers extracted from
* frontendWSAppSession / backendWSAppSession in PR #126 (this PR).
*
* These cover the encoding-dispatch (PROTOBUF vs JSON passthrough) and
* the per-message request/response handshake without needing a live
* WebSocket connection. They mirror the udsMgr / httpMgr / wsMgr
* dispatch tests landed in PRs #124 and #125.
**/
package utils

import (
	"testing"

	"github.com/gorilla/websocket"
)

// TestDecodeWsRequestPayload_JsonPassthrough confirms that with the
// default (non-PROTOBUF) encoding the helper returns the raw bytes
// unchanged as a string — i.e. the manager hub gets exactly what the
// WS client wrote.
func TestDecodeWsRequestPayload_JsonPassthrough(t *testing.T) {
	msg := []byte(`{"action":"get","path":"Vehicle.Speed","requestId":"42"}`)

	got := decodeWsRequestPayload(msg, NONE)

	if got != string(msg) {
		t.Fatalf("JSON passthrough mangled the payload:\n got:  %q\n want: %q", got, string(msg))
	}
}

// TestDecodeWsRequestPayload_ProtobufBranchIsTaken verifies that the
// PROTOBUF arm routes to ProtobufToJson rather than the passthrough.
// ProtobufToJson returns "" when handed bytes that are not a valid
// pb.ProtobufMessage, which is a reliable signal that the helper
// dispatched to it (the passthrough would have returned the literal
// string instead).
func TestDecodeWsRequestPayload_ProtobufBranchIsTaken(t *testing.T) {
	msg := []byte("definitely-not-protobuf")

	got := decodeWsRequestPayload(msg, PROTOBUF)

	if got == string(msg) {
		t.Fatalf("PROTOBUF branch fell through to passthrough; got %q", got)
	}
}

// TestForwardWsRequest_PerformsHandshake exercises the
// send-payload → receive-response → push-to-backend sequence using
// buffered channels. We pre-load the response on clientChannel so the
// helper's receive completes without needing a separate goroutine for
// the hub.
func TestForwardWsRequest_PerformsHandshake(t *testing.T) {
	clientChannel := make(chan string, 2)
	clientBackendChannel := make(chan string, 1)

	// Pre-seed the response so the helper's <-clientChannel returns
	// immediately after its send.
	clientChannel <- "fake-hub-response"

	done := make(chan struct{})
	go func() {
		forwardWsRequest("the-payload", clientChannel, clientBackendChannel)
		close(done)
	}()

	select {
	case got := <-clientBackendChannel:
		if got != "fake-hub-response" {
			t.Fatalf("backend got %q; want the response we pre-seeded", got)
		}
	case <-done:
		t.Fatalf("forwardWsRequest returned without pushing to backend channel")
	}
	<-done

	// The helper should have pushed the payload to clientChannel
	// before reading. That send is what the manager hub would
	// normally pick up, so it should still be queued.
	select {
	case forwarded := <-clientChannel:
		if forwarded != "the-payload" {
			t.Fatalf("clientChannel got %q; want %q", forwarded, "the-payload")
		}
	default:
		t.Fatalf("payload was never sent on clientChannel")
	}
}

// TestEncodeWsResponsePayload_JsonReturnsTextMessage confirms the
// default (non-PROTOBUF) arm returns gorilla's TextMessage frame type
// and the raw bytes of the input string.
func TestEncodeWsResponsePayload_JsonReturnsTextMessage(t *testing.T) {
	message := `{"action":"get","path":"Vehicle.Speed","value":"100"}`

	mt, payload := encodeWsResponsePayload(message, NONE)

	if mt != websocket.TextMessage {
		t.Fatalf("JSON arm returned message type %d; want websocket.TextMessage (%d)", mt, websocket.TextMessage)
	}
	if string(payload) != message {
		t.Fatalf("JSON arm mangled the payload:\n got:  %q\n want: %q", string(payload), message)
	}
}

// TestEncodeWsResponsePayload_ProtobufReturnsBinaryMessage exercises
// the PROTOBUF arm and confirms gorilla's BinaryMessage frame type is
// returned. We don't pin the serialised bytes themselves because the
// proto schema may change; the dispatch is what matters here.
func TestEncodeWsResponsePayload_ProtobufReturnsBinaryMessage(t *testing.T) {
	message := `{"action":"get","path":"Vehicle.Speed","value":"100","requestId":"42"}`

	mt, _ := encodeWsResponsePayload(message, PROTOBUF)

	if mt != websocket.BinaryMessage {
		t.Fatalf("PROTOBUF arm returned message type %d; want websocket.BinaryMessage (%d)", mt, websocket.BinaryMessage)
	}
}
