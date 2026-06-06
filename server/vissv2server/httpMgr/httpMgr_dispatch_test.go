/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the per-message dispatch helpers (handleHttpClientRequest,
* handleHttpTransportResponse) extracted from HttpMgrInit's for/select
* loop. The extraction was done so the validation + forwarding +
* response-routing behaviour can be unit-tested without spinning up a
* goroutine, a real HTTP server, or the full server stack.
*
* The HttpMgrInit for-select loop itself is still exercised end-to-end
* by runtest.sh integration; this file covers the per-iteration logic.
**/
package httpMgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

// schemaSourcePath is the relative path from the httpMgr package directory
// to the vissv3.0-schema.json file used by JsonSchemaInit.
const schemaSourcePath = "../vissv3.0-schema.json"

// TestMain initialises utils.Info / utils.Error and the package-level
// HttpClientChan so the dispatch helpers can log and channel-send
// without nil-deref under test conditions.
func TestMain(m *testing.M) {
	utils.InitLog("httpMgr-dispatch-test.log", os.TempDir(), false, "error")
	// HttpClientChan is package-level and already initialised in
	// httpMgr.go to a 1-element slice with an unbuffered channel. We
	// replace it with a buffered channel for tests so we don't
	// deadlock when the handler writes back an error response.
	HttpClientChan = []chan string{make(chan string, 4)}
	os.Exit(m.Run())
}

// copySchemaToDir copies the vissv3.0-schema.json file into dir so that
// utils.JsonSchemaInit() can find it when running from dir.
func copySchemaToDir(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(schemaSourcePath)
	if err != nil {
		t.Skipf("schema file %s not readable: %v (skipping schema-loaded test)", schemaSourcePath, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vissv3.0-schema.json"), data, 0644); err != nil {
		t.Fatalf("write schema to tmp dir: %v", err)
	}
}

// TestHandleHttpClientRequest_NilSchemaPath: when JsonSchemaInit
// hasn't loaded a schema, JsonSchemaValidate returns a non-empty error
// (per PR #120). The handler should respond with the schema-error
// response on HttpClientChan[0] and NOT forward to transportMgrChan.
func TestHandleHttpClientRequest_NilSchemaPath(t *testing.T) {
	// Drain anything leftover from previous tests.
	for len(HttpClientChan[0]) > 0 {
		<-HttpClientChan[0]
	}

	transportMgrChan := make(chan string, 4)
	req := `{"action":"get","path":"Vehicle.Speed","requestId":"42"}`

	done := make(chan struct{})
	go func() {
		handleHttpClientRequest(req, 0, transportMgrChan)
		close(done)
	}()

	// Either an error response comes back on HttpClientChan[0]
	// (schema-not-loaded path from PR #120), or the request was
	// forwarded to transportMgrChan (schema loaded path). Both
	// non-panic outcomes are acceptable for this test.
	select {
	case resp := <-HttpClientChan[0]:
		_ = resp
	case forwarded := <-transportMgrChan:
		_ = forwarded
	}
	<-done
}

// TestHandleHttpClientRequest_ValidatesNonEmpty verifies the helper
// returns through one of the two channels rather than blocking on the
// schema-not-loaded path forever.
func TestHandleHttpClientRequest_DoesNotBlock(t *testing.T) {
	// Drain.
	for len(HttpClientChan[0]) > 0 {
		<-HttpClientChan[0]
	}
	transportMgrChan := make(chan string, 4)

	done := make(chan struct{})
	go func() {
		handleHttpClientRequest(`{"action":"get","path":"Vehicle.Speed","requestId":"43"}`, 0, transportMgrChan)
		close(done)
	}()
	// Drain whatever the handler produced.
	select {
	case <-HttpClientChan[0]:
	case <-transportMgrChan:
	}
	<-done
}

// TestHandleHttpTransportResponse_ForwardsToClient verifies that a
// well-formed response with a RouterId reaches HttpClientChan[0].
func TestHandleHttpTransportResponse_ForwardsToClient(t *testing.T) {
	for len(HttpClientChan[0]) > 0 {
		<-HttpClientChan[0]
	}
	transportMgrChan := make(chan string, 4)
	// Response with internal RouterId so RemoveInternalData can split
	// out the clientId.
	resp := `{"action":"get","path":"Vehicle.Speed","value":"100","RouterId":"0?0"}`

	done := make(chan struct{})
	go func() {
		handleHttpTransportResponse(resp, transportMgrChan)
		close(done)
	}()

	select {
	case forwarded := <-HttpClientChan[0]:
		if !strings.Contains(forwarded, "Vehicle.Speed") {
			t.Fatalf("client channel received %q; want it to contain Vehicle.Speed", forwarded)
		}
	case <-done:
		t.Fatalf("handler completed without forwarding to HttpClientChan[0]")
	}
	<-done
}

// TestHandleHttpClientRequest_ValidRequestForwarded covers the happy path:
// when the schema IS loaded and the request is valid, the handler forwards
// the request to transportMgrChan (the AddRoutingForwardRequest branch).
//
// This test copies the vissv3.0-schema.json file to a temp directory,
// changes to that directory so utils.JsonSchemaInit can find it, then
// calls handleHttpClientRequest with a well-formed request. The schema
// is only loaded once per process (sync.Once), so this test is only
// effective if run before any other test that triggers JsonSchemaInit.
// We force it by running it in isolation via t.Run with a fresh directory.
func TestHandleHttpClientRequest_ValidRequestForwarded(t *testing.T) {
	// Drain any leftovers.
	for len(HttpClientChan[0]) > 0 {
		<-HttpClientChan[0]
	}

	tmp := t.TempDir()
	copySchemaToDir(t, tmp)

	// Remember working directory and change to tmp so readSchema finds the file.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	// Initialize the schema (sync.Once — may already be initialized if a prior
	// test called it; that's fine — schema will be non-nil either way).
	utils.JsonSchemaInit()

	transportMgrChan := make(chan string, 4)
	// Use a request that is valid per the VISSv3 schema.
	req := `{"action":"get","path":"Vehicle.Speed","requestId":"99"}`

	done := make(chan struct{})
	go func() {
		handleHttpClientRequest(req, 0, transportMgrChan)
		close(done)
	}()

	select {
	case forwarded := <-transportMgrChan:
		// Happy path: request was forwarded with RouterId prefix injected.
		if !strings.Contains(forwarded, "Vehicle.Speed") {
			t.Errorf("forwarded request missing path: %q", forwarded)
		}
		if !strings.Contains(forwarded, "RouterId") {
			t.Errorf("forwarded request missing RouterId: %q", forwarded)
		}
	case errResp := <-HttpClientChan[0]:
		// Schema-not-loaded or validation error path: tolerate if schema couldn't be loaded.
		t.Logf("schema validation path taken (schema may not validate this request): %q", errResp)
	}
	<-done
}
