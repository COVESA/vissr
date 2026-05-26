/**
* (C) 2026 Matt Jones / Ford
*
* Regression tests for the WsClientIndex race and manager-handlers fixes.
* Covers: WsClientIndexMu race (PR #119 / this branch), OOB bounds guard
* on ReturnWsClientIndex, MaxBytesReader on HTTP POST body.
**/

package utils

import (
	"bytes"
	"net/http/httptest"
	"sync"
	"testing"
)

// snapshotWsClientIndexList captures and restores WsClientIndexList around a
// test so we don't pollute other tests in the package.
func snapshotWsClientIndexList(t *testing.T) func() {
	t.Helper()
	saved := make([]bool, len(WsClientIndexList))
	copy(saved, WsClientIndexList)
	return func() { copy(WsClientIndexList, saved) }
}

// --------------------------------------------------------------------------
// getWsClientIndex / ReturnWsClientIndex — happy path
// --------------------------------------------------------------------------

func TestGetWsClientIndex_AllocatesFirstFree(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	idx := getWsClientIndex()
	if idx != 0 {
		t.Errorf("first allocation: got %d; want 0", idx)
	}
	if WsClientIndexList[0] {
		t.Errorf("slot 0 should be marked occupied")
	}
}

func TestReturnWsClientIndex_FreesSlot(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	idx := getWsClientIndex()
	ReturnWsClientIndex(idx)
	if !WsClientIndexList[idx] {
		t.Errorf("slot %d not freed", idx)
	}
}

// TestReturnWsClientIndex_MakesSlotReclaimable is the basic-semantics
// check: a returned slot can be claimed again.
func TestReturnWsClientIndex_MakesSlotReclaimable(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
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

func TestGetWsClientIndex_ExhaustionReturnsMinusOne(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = false
	}
	if got := getWsClientIndex(); got != -1 {
		t.Errorf("exhausted pool: got %d; want -1", got)
	}
}

func TestGetWsClientIndex_AllocatesInOrder(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	for want := 0; want < len(WsClientIndexList); want++ {
		if got := getWsClientIndex(); got != want {
			t.Errorf("iter %d: got %d", want, got)
		}
	}
	if got := getWsClientIndex(); got != -1 {
		t.Errorf("after full drain: got %d", got)
	}
}

// --------------------------------------------------------------------------
// ReturnWsClientIndex — OOB guard (pre-fix would slice-panic)
// --------------------------------------------------------------------------

func TestReturnWsClientIndex_NegativeIndexSafe(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on -1: %v", r)
		}
	}()
	ReturnWsClientIndex(-1)
}

func TestReturnWsClientIndex_TooLargeIndexSafe(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on len+: %v", r)
		}
	}()
	ReturnWsClientIndex(len(WsClientIndexList))
	ReturnWsClientIndex(len(WsClientIndexList) + 100)
}

// --------------------------------------------------------------------------
// Race regression — pre-fix this would trip -race because getWsClientIndex
// and ReturnWsClientIndex both read+wrote WsClientIndexList with no mutex.
// --------------------------------------------------------------------------

// TestGetWsClientIndex_ConcurrentClaimsAreUnique is the regression test
// for the WsClientIndexMu. Without the mutex, two concurrent WS
// upgrades could both observe the same slot as free and both claim it,
// causing request/response cross-talk between unrelated clients.
//
// Run with: go test -race
func TestGetWsClientIndex_ConcurrentClaimsAreUnique(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
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

func TestWsClientIndex_RaceFree(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				idx := getWsClientIndex()
				if idx >= 0 {
					ReturnWsClientIndex(idx)
				}
			}
		}()
	}
	wg.Wait()
	// After all goroutines finish, every slot should be free again.
	for i, free := range WsClientIndexList {
		if !free {
			t.Errorf("slot %d not free after balanced alloc/return", i)
		}
	}
}

// --------------------------------------------------------------------------
// frontendHttpAppSession — MaxBytesReader regression (oversize POST body)
// --------------------------------------------------------------------------

// TestFrontendHttpAppSession_RejectsOversizedBody is the regression test
// for the MaxBytesReader on the VISS HTTP POST handler. Before
// the fix, an unauthenticated peer could OOM the daemon by sending a
// giant POST body.
func TestFrontendHttpAppSession_RejectsOversizedBody(t *testing.T) {
	clientChannel := make(chan string, 4)

	body := bytes.NewReader(make([]byte, 256*1024)) // >> 64 KiB cap
	req := httptest.NewRequest("POST", "/Vehicle.Speed", body)
	rec := httptest.NewRecorder()

	// frontendHttpAppSession sits behind the MaxBytesReader; the
	// oversize path returns via backendHttpAppSession which writes
	// a JSON error body. Pass if the response carries the "too large"
	// message, regardless of HTTP status code.
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

// --------------------------------------------------------------------------
// Integration-only entry points (documented, not unit-tested)
//
// backendHttpAppSession, backendWSAppSession, frontendWSAppSession,
// HttpChannel.makeappClientHandler, WsChannel.makeappClientHandler,
// HttpServer.InitClientServer and WsServer.InitClientServer all bind to a
// real *http.ResponseWriter or *websocket.Conn and exchange data over those
// connections. They're exercised end-to-end by the integration test suite.
// The deterministic building blocks they depend on are all covered above.
// --------------------------------------------------------------------------
