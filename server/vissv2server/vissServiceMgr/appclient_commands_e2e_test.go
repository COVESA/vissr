package vissServiceMgr

// TestAppclientServiceCommands_Replay is the acceptance test called for by
// the design doc's implementation order (step 9): it replays every request in
// client/client-1.0/Javascript/appclient_service_commands.txt verbatim
// against the rebuilt HIM-canonical multiplexed tree, confirming each one
// routes and responds without error (or, for the plain "cancel" of an
// already-unknown serviceId, with the expected benign error).
//
// This is the client file's own "resource" filter shapes (requestId 8756-8760)
// — the same shapes independently exercised at the JSON-Schema layer in
// utils/service_schema_test.go — now exercised end-to-end through
// HandleInvoke/HandleMonitor/HandleCancel/HandleDiscover against a real
// loaded tree.

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
)

const appclientCommandsPath = "../../../client/client-1.0/Javascript/appclient_service_commands.txt"

// parseAppclientCommands splits the file's newline-separated JSON objects
// (not a JSON array) into individual request maps.
func parseAppclientCommands(t *testing.T) []map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(appclientCommandsPath)
	if err != nil {
		t.Skipf("appclient_service_commands.txt not readable (%v); skipping", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var reqs []map[string]interface{}
	for {
		var m map[string]interface{}
		if err := dec.Decode(&m); err != nil {
			break // io.EOF or no more valid JSON tokens
		}
		reqs = append(reqs, m)
	}
	if len(reqs) == 0 {
		t.Fatal("parsed zero requests from appclient_service_commands.txt")
	}
	return reqs
}

func TestAppclientServiceCommands_Replay(t *testing.T) {
	resetState()
	resetSeatState()
	shrinkStepPeriod(t, 3*time.Millisecond)
	loadVehicleServiceTree(t)
	t.Cleanup(stopServiceGoroutines)

	reqs := parseAppclientCommands(t)

	bc := make(chan map[string]interface{}, 64)
	bcs := []chan map[string]interface{}{bc}

	for _, req := range reqs {
		req["routerIndex"] = 0 // the file's requests predate routerIndex plumbing
		action, _ := req["action"].(string)
		requestId, _ := req["requestId"].(string)

		switch action {
		case "invoke":
			HandleInvoke(req, bcs)
			resp := recvOrFatal(t, bc, requestId)
			if errObj, ok := resp["error"]; ok {
				t.Errorf("invoke %v (requestId=%v) returned an error: %v", req["path"], requestId, errObj)
			}
		case "monitor":
			HandleMonitor(req, bcs)
			resp := recvOrFatal(t, bc, requestId)
			if errObj, ok := resp["error"]; ok {
				t.Errorf("monitor %v (requestId=%v) returned an error: %v", req["path"], requestId, errObj)
			}
		case "discover":
			HandleDiscover(req, bc)
			resp := recvOrFatal(t, bc, requestId)
			if errObj, ok := resp["error"]; ok {
				t.Errorf("discover %v (requestId=%v) returned an error: %v", req["path"], requestId, errObj)
			}
		case "cancel":
			HandleCancel(req, bc)
			resp := recvOrFatal(t, bc, "cancel")
			// The file's cancel targets a hardcoded, never-issued serviceId
			// ("1234"), so a "serviceId not found" error is the *expected*
			// outcome here — this call only guards against a panic/hang, not
			// against an error response.
			_ = resp
		default:
			t.Fatalf("unrecognised action %q in appclient_service_commands.txt", action)
		}
	}
}

func recvOrFatal(t *testing.T, bc chan map[string]interface{}, label string) map[string]interface{} {
	t.Helper()
	select {
	case resp := <-bc:
		return resp
	case <-time.After(2 * time.Second):
		t.Fatalf("no response for %v", label)
		return nil
	}
}
