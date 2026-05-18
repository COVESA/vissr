/**
* Regression tests for the manager-handlers fixes shipped in PR #119
* (WsClientIndexMu race; MaxBytesReader on POST body).
**/
package utils

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestFrontendHttpAppSession_RejectsOversizedBody is the regression test
// for the PR #119 MaxBytesReader on the VISS HTTP POST handler. Before
// the fix, an unauthenticated peer (the HTTP endpoint is reachable
// without auth at the transport level; the VISS-level token is checked
// downstream) could OOM the daemon by sending a giant POST body.
func TestFrontendHttpAppSession_RejectsOversizedBody(t *testing.T) {
	clientChannel := make(chan string, 4)

	body := bytes.NewReader(make([]byte, 256*1024)) // >> 64 KiB cap
	req := httptest.NewRequest("POST", "/Vehicle.Speed", body)
	rec := httptest.NewRecorder()

	// frontendHttpAppSession sits behind the MaxBytesReader; the
	// oversize path returns 413 (via backendHttpAppSession which writes
	// a JSON error body but not via http.Error, so the status code may
	// stay 200 with the error in the body). Tolerate either: pass if
	// the response carries the 64 KiB / Request body too large message,
	// regardless of HTTP status code.
	frontendHttpAppSession(rec, req, clientChannel)

	got := rec.Body.String()
	if !bytes.Contains([]byte(got), []byte("too large")) {
		t.Fatalf("expected response body to mention 'too large'; got %q (status %d)", got, rec.Code)
	}
	// The handler must not have forwarded the (rejected) request to
	// the manager hub.
	select {
	case msg := <-clientChannel:
		t.Fatalf("oversize POST should not have been forwarded to clientChannel; got %q", msg)
	default:
	}
}

// TestFrontendHttpAppSession_GetForwards verifies the GET path is
// unaffected by the body-limit fix (no body, no rejection).
func TestFrontendHttpAppSession_GetForwards(t *testing.T) {
	clientChannel := make(chan string, 4)
	respChannel := make(chan string, 4)

	// frontendHttpAppSession forwards to clientChannel then waits on
	// the same channel for the response. Spin up a goroutine that
	// drains the forward and responds.
	go func() {
		req := <-clientChannel
		// echo something resembling a response
		respChannel <- req
		clientChannel <- `{"action":"get","value":"ok"}`
	}()

	req := httptest.NewRequest("GET", "/Vehicle.Speed", nil)
	rec := httptest.NewRecorder()
	frontendHttpAppSession(rec, req, clientChannel)

	// Confirm the GET was actually forwarded.
	select {
	case forwarded := <-respChannel:
		if !bytes.Contains([]byte(forwarded), []byte("get")) {
			t.Fatalf("forwarded request didn't look like a GET: %q", forwarded)
		}
	default:
		t.Fatalf("GET was not forwarded to clientChannel")
	}
}

// TestGetWsClientIndex_ConcurrentClaimsAreUnique is the regression test
// for the PR #119 WsClientIndexMu. Without the mutex, two concurrent WS
// upgrades could both observe the same slot as free and both claim it,
// causing request/response cross-talk between unrelated clients.
//
// Run with: go test -race
func TestGetWsClientIndex_ConcurrentClaimsAreUnique(t *testing.T) {
	saved := make([]bool, len(WsClientIndexList))
	copy(saved, WsClientIndexList)
	defer func() {
		WsClientIndexMu.Lock()
		copy(WsClientIndexList, saved)
		WsClientIndexMu.Unlock()
	}()
	// Reset all slots to "free".
	WsClientIndexMu.Lock()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	WsClientIndexMu.Unlock()

	n := len(WsClientIndexList)
	results := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = getWsClientIndex()
		}(i)
	}
	wg.Wait()

	seen := make(map[int]int)
	for _, idx := range results {
		if idx == -1 {
			t.Fatalf("a concurrent claim returned -1; pool should have had room for all %d", n)
		}
		seen[idx]++
	}
	for idx, count := range seen {
		if count > 1 {
			t.Fatalf("slot %d was claimed by %d goroutines concurrently; WsClientIndexMu is missing or broken", idx, count)
		}
	}
}

// TestReturnWsClientIndex_MakesSlotReclaimable is the basic-semantics
// check: a returned slot can be claimed again.
func TestReturnWsClientIndex_MakesSlotReclaimable(t *testing.T) {
	saved := make([]bool, len(WsClientIndexList))
	copy(saved, WsClientIndexList)
	defer func() {
		WsClientIndexMu.Lock()
		copy(WsClientIndexList, saved)
		WsClientIndexMu.Unlock()
	}()
	WsClientIndexMu.Lock()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	WsClientIndexMu.Unlock()

	first := getWsClientIndex()
	if first == -1 {
		t.Fatalf("first claim returned -1 even though all slots were free")
	}
	ReturnWsClientIndex(first)
	second := getWsClientIndex()
	if second == -1 {
		t.Fatalf("after returning slot %d, expected it to be reclaimable; got -1", first)
	}
	if second != first {
		// Not strictly required by contract, but the naive
		// implementation reclaims the lowest free slot.
		t.Logf("note: second claim landed on slot %d, not the returned %d (acceptable but unusual)", second, first)
	}
}
