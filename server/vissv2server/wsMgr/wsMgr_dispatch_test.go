/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for handleWsClientRequest and handleWsTransportResponse,
* extracted from WsMgrInit's for/select loop in PR #126 (this PR).
* Same shape as the udsMgr dispatch tests landed in PR #124.
**/
package wsMgr

import (
	"os"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises utils.Info / utils.Error and the package-level
// state used by the helpers (wsClientChan, errorResponseMap, dcCache)
// so the dispatch functions don't nil-deref under test conditions.
func TestMain(m *testing.M) {
	utils.InitLog("wsMgr-dispatch-test.log", os.TempDir(), false, "error")
	initChannels()
	initDcCache()
	os.Exit(m.Run())
}

// TestHandleWsClientRequest_KillBypassesValidation: an
// internal-killsubscriptions request skips JSON-schema validation and
// reaches the routing-forward step.
func TestHandleWsClientRequest_KillBypassesValidation(t *testing.T) {
	transportMgrChan := make(chan string, 4)
	killMsg := `{"action":"internal-killsubscriptions"}`

	done := make(chan struct{})
	go func() {
		handleWsClientRequest(killMsg, 0, 0, transportMgrChan)
		close(done)
	}()
	select {
	case forwarded := <-transportMgrChan:
		if !strings.Contains(forwarded, "internal-killsubscriptions") {
			t.Fatalf("forwarded message lost original action: %q", forwarded)
		}
	case <-done:
		t.Fatalf("handler returned without forwarding the killMsg")
	}
	<-done
}

// TestHandleWsClientRequest_DoesNotBlock confirms the function returns
// through one of the two channels (validation error to wsClientChan,
// or forward to transportMgrChan) without blocking forever.
func TestHandleWsClientRequest_DoesNotBlock(t *testing.T) {
	transportMgrChan := make(chan string, 4)
	req := `{"action":"get","path":"Vehicle.Speed","requestId":"42"}`

	done := make(chan struct{})
	go func() {
		handleWsClientRequest(req, 0, 0, transportMgrChan)
		close(done)
	}()
	select {
	case <-wsClientChan[0]:
	case <-transportMgrChan:
	}
	<-done
}

// TestHandleWsTransportResponse_ForwardsToClient verifies that a
// well-formed response with a RouterId reaches wsClientChan[0] via
// checkCompressionResponse + RemoveRoutingForwardResponse.
func TestHandleWsTransportResponse_ForwardsToClient(t *testing.T) {
	transportMgrChan := make(chan string, 4)
	resp := `{"action":"get","path":"Vehicle.Speed","value":"100","RouterId":"0?0"}`

	done := make(chan struct{})
	go func() {
		handleWsTransportResponse(resp, transportMgrChan)
		close(done)
	}()

	select {
	case forwarded := <-wsClientChan[0]:
		if !strings.Contains(forwarded, "Vehicle.Speed") {
			t.Fatalf("client channel received %q; want it to contain Vehicle.Speed", forwarded)
		}
	case <-done:
		t.Fatalf("handler completed without forwarding to wsClientChan[0]")
	}
	<-done
}

// TestHandleWsTransportResponse_ErrorPassesThrough exercises the
// short-circuit branch of checkCompressionResponse.
func TestHandleWsTransportResponse_ErrorPassesThrough(t *testing.T) {
	transportMgrChan := make(chan string, 4)
	resp := `{"action":"get","error":{"number":"401"},"RouterId":"0?0"}`

	done := make(chan struct{})
	go func() {
		handleWsTransportResponse(resp, transportMgrChan)
		close(done)
	}()

	select {
	case forwarded := <-wsClientChan[0]:
		if !strings.Contains(forwarded, "error") {
			t.Fatalf("error response wasn't preserved: %q", forwarded)
		}
	case <-done:
		t.Fatalf("handler completed without forwarding")
	}
	<-done
}
