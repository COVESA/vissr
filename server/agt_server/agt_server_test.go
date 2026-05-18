/**
* Regression tests for the agt_server fixes shipped in PR #119
* (jtiCacheMu race fix; MaxBytesReader on body).
**/
package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises utils.Info / utils.Error so the agt_server
// handler can log without nil-deref under test conditions.
func TestMain(m *testing.M) {
	utils.InitLog("agtServer-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// TestAgtServerHandler_RejectsOversizedBody is the regression test for
// the PR #119 MaxBytesReader on /agts. Before the fix, the AGT endpoint
// (which is reachable pre-auth — its purpose is to issue access-grant
// tokens) used io.ReadAll on the bare req.Body and any anonymous peer
// could OOM the daemon by sending a giant or chunked body.
func TestAgtServerHandler_RejectsOversizedBody(t *testing.T) {
	// The handler does serverChannel <- bodyBytes then waits on the
	// channel. The oversize path returns early without sending, so the
	// channel buffer can be tiny.
	handler := makeAgtServerHandler(make(chan string, 4))

	// 256 KiB — well over the 64 KiB MaxBytesReader cap.
	body := bytes.NewReader(make([]byte, 256*1024))
	req := httptest.NewRequest("POST", "/agts", body)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected %d (Request Entity Too Large); got %d: %s",
			http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	}
}

// TestAgtServerHandler_RejectsWrongPath sanity-checks the early-404
// path (orthogonal to body-size, but exercises the same closure).
func TestAgtServerHandler_RejectsWrongPath(t *testing.T) {
	handler := makeAgtServerHandler(make(chan string, 4))
	req := httptest.NewRequest("POST", "/wrong-path", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong path; got %d", rec.Code)
	}
}

// TestAddCheckJti_AcceptsNewRejectsReplay is the basic-semantics check
// on the jti replay cache (mirrors the equivalent test in atServer).
func TestAddCheckJti_AcceptsNewRejectsReplay(t *testing.T) {
	jtiCacheMu.Lock()
	jtiCache = nil
	jtiCacheMu.Unlock()

	if !addCheckJti("jti-1") {
		t.Fatalf("first addCheckJti must accept a new jti")
	}
	if addCheckJti("jti-1") {
		t.Fatalf("second addCheckJti must reject a replayed jti")
	}
	if !addCheckJti("jti-2") {
		t.Fatalf("a different jti must be accepted")
	}
}

// TestAddCheckJti_ConcurrentSafe is the regression test for the PR #119
// jtiCacheMu mutex. Before that fix, two concurrent AGT requests racing
// the map would abort the daemon with "concurrent map read and map
// write" — deterministic remote crash from any anonymous network peer.
//
// Run with: go test -race
func TestAddCheckJti_ConcurrentSafe(t *testing.T) {
	jtiCacheMu.Lock()
	jtiCache = nil
	jtiCacheMu.Unlock()

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	results := make([]bool, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				results[i] = addCheckJti("shared-jti")
			} else {
				results[i] = addCheckJti("jti-other")
			}
		}(i)
	}
	wg.Wait()

	acceptedShared := 0
	for i, ok := range results {
		if i%2 == 0 && ok {
			acceptedShared++
		}
	}
	if acceptedShared != 1 {
		t.Fatalf("expected exactly 1 acceptance of shared-jti across %d concurrent calls; got %d", n/2, acceptedShared)
	}
}
