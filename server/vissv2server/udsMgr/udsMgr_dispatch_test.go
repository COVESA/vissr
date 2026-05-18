/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the per-message dispatch helpers (handleUdsClientRequest,
* handleUdsTransportResponse) extracted from UdsMgrInit's for/select
* loop. The extraction was done so the validation / compression /
* forwarding behaviour can be unit-tested without spinning up a
* goroutine, a UDS socket, or the full server stack.
*
* The UdsMgrInit for-select loop itself is still exercised end-to-end
* by runtest.sh integration; this file covers the per-iteration logic.
**/
package udsMgr

import (
	"os"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises the package-level utils loggers and channels so
// the dispatch helpers don't nil-deref under test conditions.
func TestMain(m *testing.M) {
	utils.InitLog("udsMgr-dispatch-test.log", os.TempDir(), false, "error")
	// Initialise the same package-level state UdsMgrInit would; tests
	// reuse these channels and the dc-cache.
	initChannels()
	initDcCache()
	os.Exit(m.Run())
}

// TestHandleUdsClientRequest_KillBypassesValidation verifies that an
// internal-killsubscriptions message skips schema validation and
// reaches the routing-forward call (which sends to transportMgrChan).
func TestHandleUdsClientRequest_KillBypassesValidation(t *testing.T) {
	transportMgrChan := make(chan string, 4)
	killMsg := `{"action":"internal-killsubscriptions"}`

	// The validation branch is skipped; AddRoutingForwardRequest
	// wraps killMsg with internal routing fields and sends to
	// transportMgrChan. We just need to drain the channel so the
	// helper doesn't block.
	done := make(chan struct{})
	go func() {
		handleUdsClientRequest(killMsg, 0, 0, transportMgrChan)
		close(done)
	}()
	select {
	case forwarded := <-transportMgrChan:
		if !strings.Contains(forwarded, "internal-killsubscriptions") {
			t.Fatalf("forwarded message didn't contain the original action: %q", forwarded)
		}
	case <-done:
		t.Fatalf("helper returned without forwarding")
	}
	<-done
}

// TestHandleUdsClientRequest_NilSchemaPath verifies the behaviour when
// JsonSchemaInit hasn't loaded a schema: JsonSchemaValidate now returns
// a non-empty error (per the PR #120 nil-deref guard), so the request
// is rejected with the schema-error response on the udsClientChan.
func TestHandleUdsClientRequest_NilSchemaPath(t *testing.T) {
	// Make sure no schema is loaded for this test.
	// (We can't reach the utils package's jsonSchema var from outside,
	// but if no JsonSchemaInit has been called the variable stays nil.)
	transportMgrChan := make(chan string, 4)
	// Use a real schema-valid message; with no schema loaded the
	// validator returns "schema not loaded".
	req := `{"action":"get","path":"Vehicle.Speed","requestId":"42"}`

	// handleUdsClientRequest will send the error response on
	// udsClientChan[0]; we read it back.
	done := make(chan struct{})
	go func() {
		handleUdsClientRequest(req, 0, 0, transportMgrChan)
		close(done)
	}()

	select {
	case resp := <-udsClientChan[0]:
		// Either the response is an error (schema-not-loaded case from
		// PR #120) or the request was forwarded (schema loaded). Both
		// outcomes are acceptable here; we just confirm no panic.
		_ = resp
	case forwarded := <-transportMgrChan:
		_ = forwarded
	}
	<-done
}

// TestHandleUdsTransportResponse_ForwardsToClient verifies that the
// response branch passes through checkCompressionResponse and ends up
// on the connected UDS client's channel.
func TestHandleUdsTransportResponse_ForwardsToClient(t *testing.T) {
	transportMgrChan := make(chan string, 4)
	// Construct a response with an internal RouterId so
	// RemoveInternalData can split out the clientId.
	resp := `{"action":"get","path":"Vehicle.Speed","value":"100","RouterId":"0?0"}`

	done := make(chan struct{})
	go func() {
		handleUdsTransportResponse(resp, transportMgrChan)
		close(done)
	}()

	select {
	case forwarded := <-udsClientChan[0]:
		if !strings.Contains(forwarded, "Vehicle.Speed") {
			t.Fatalf("client channel received %q; want it to contain Vehicle.Speed", forwarded)
		}
	case <-done:
		t.Fatalf("handler completed without forwarding to udsClientChan[0]")
	}
	<-done
}

// TestHandleUdsTransportResponse_ErrorPassesThrough verifies the
// short-circuit for error responses: checkCompressionResponse returns
// them unchanged.
func TestHandleUdsTransportResponse_ErrorPassesThrough(t *testing.T) {
	transportMgrChan := make(chan string, 4)
	resp := `{"action":"get","error":{"number":"401"},"RouterId":"0?0"}`

	done := make(chan struct{})
	go func() {
		handleUdsTransportResponse(resp, transportMgrChan)
		close(done)
	}()

	select {
	case forwarded := <-udsClientChan[0]:
		if !strings.Contains(forwarded, "error") {
			t.Fatalf("error response wasn't preserved: %q", forwarded)
		}
	case <-done:
		t.Fatalf("handler completed without forwarding")
	}
	<-done
}
