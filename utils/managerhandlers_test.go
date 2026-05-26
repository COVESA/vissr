/**
* (C) 2026 Matt Jones / Ford
*
* Tests for the non-TLS helpers in managerhandlers.go. The TLS surface
* (ReadTransportSecConfig, validateSecConfig, safeCertPath, CertOptToInt,
* GetTLSConfig) lives in tls_test.go.
*
* Also covers: WsClientIndexMu race regression, OOB bounds guard on
* ReturnWsClientIndex, MaxBytesReader on HTTP POST body.
**/

package utils

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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
// createRouterIdProperty — formats "RouterId":"mgrId?clientId".
// --------------------------------------------------------------------------

func TestCreateRouterIdProperty(t *testing.T) {
	got := createRouterIdProperty(3, 7)
	want := `"RouterId":"3?7"`
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

func TestCreateRouterIdProperty_Zero(t *testing.T) {
	got := createRouterIdProperty(0, 0)
	want := `"RouterId":"0?0"`
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// --------------------------------------------------------------------------
// AddRoutingForwardRequest — prepends RouterId + origin into the first "{" of
// the request and pushes onto transportMgrChan.
// --------------------------------------------------------------------------

func TestAddRoutingForwardRequest_PrependsRouterIdAndOrigin(t *testing.T) {
	ch := make(chan string, 1)
	AddRoutingForwardRequest(`{"action":"get"}`, 1, 5, ch)
	select {
	case got := <-ch:
		if !strings.Contains(got, `"RouterId":"1?5"`) {
			t.Errorf("missing RouterId; got %q", got)
		}
		if !strings.Contains(got, `"origin":"external"`) {
			t.Errorf("missing origin; got %q", got)
		}
		if !strings.Contains(got, `"action":"get"`) {
			t.Errorf("dropped original action; got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("channel never received")
	}
}

func TestAddRoutingForwardRequest_OnlyFirstBraceReplaced(t *testing.T) {
	// Verifies the strings.Replace(..., 1) hardcoded count - nested objects
	// keep their braces.
	ch := make(chan string, 1)
	AddRoutingForwardRequest(`{"action":"set","data":{"path":"V"}}`, 0, 0, ch)
	got := <-ch
	// "{"action" should have been replaced exactly once with the prefix block.
	if strings.Count(got, `"RouterId"`) != 1 {
		t.Errorf("RouterId appeared %d times; expected 1: %q", strings.Count(got, `"RouterId"`), got)
	}
}

// --------------------------------------------------------------------------
// RemoveInternalData — pulls "RouterId":"mgrId?clientId" back out and returns
// the trimmed response + the parsed clientId.
// --------------------------------------------------------------------------

func TestRemoveInternalData_RoundTrip(t *testing.T) {
	ch := make(chan string, 1)
	AddRoutingForwardRequest(`{"action":"get","data":{}}`, 2, 11, ch)
	withRouterId := <-ch
	trimmed, clientId := RemoveInternalData(withRouterId)
	if clientId != 11 {
		t.Errorf("clientId: got %d; want 11", clientId)
	}
	if strings.Contains(trimmed, "RouterId") {
		t.Errorf("RouterId not stripped: %q", trimmed)
	}
	if !strings.Contains(trimmed, `"action":"get"`) {
		t.Errorf("payload corrupted: %q", trimmed)
	}
}

func TestRemoveInternalData_MultiDigitClientId(t *testing.T) {
	ch := make(chan string, 1)
	AddRoutingForwardRequest(`{"x":1}`, 3, 12345, ch)
	_, clientId := RemoveInternalData(<-ch)
	if clientId != 12345 {
		t.Errorf("multi-digit clientId: got %d", clientId)
	}
}

// --------------------------------------------------------------------------
// splitToPathQueryKeyValue — splits "path?key=value" into the three pieces.
// --------------------------------------------------------------------------

func TestSplitToPathQueryKeyValue_NoQuery(t *testing.T) {
	p, k, v := splitToPathQueryKeyValue("/Vehicle.Speed")
	if p != "/Vehicle.Speed" || k != "" || v != "" {
		t.Errorf("got (%q,%q,%q)", p, k, v)
	}
}

func TestSplitToPathQueryKeyValue_Filter(t *testing.T) {
	p, k, v := splitToPathQueryKeyValue("/Vehicle.Speed?filter={\"type\":\"timebased\"}")
	if p != "/Vehicle.Speed" || k != "filter" {
		t.Errorf("path/key: got (%q,%q)", p, k)
	}
	if !strings.HasPrefix(v, "{") {
		t.Errorf("filter value: got %q", v)
	}
}

func TestSplitToPathQueryKeyValue_Metadata(t *testing.T) {
	p, k, v := splitToPathQueryKeyValue("/Vehicle?metadata=static")
	if p != "/Vehicle" || k != "metadata" || v != "static" {
		t.Errorf("got (%q,%q,%q)", p, k, v)
	}
}

func TestSplitToPathQueryKeyValue_UnknownKey(t *testing.T) {
	_, k, v := splitToPathQueryKeyValue("/Vehicle?bogus=42")
	if k != "filter" {
		t.Errorf("unknown key path returns key=%q; want filter (sentinel)", k)
	}
	if v != "incorrect http query key" {
		t.Errorf("value: got %q", v)
	}
}

func TestSplitToPathQueryKeyValue_NoEquals(t *testing.T) {
	p, k, v := splitToPathQueryKeyValue("/Vehicle?just-key")
	// `?` present but no `=` -> early-return on missing equals leaves k/v empty.
	if k != "" || v != "" {
		t.Errorf("got (%q,%q,%q)", p, k, v)
	}
}

// --------------------------------------------------------------------------
// getWsClientIndex / ReturnWsClientIndex — slot allocator for WS clients.
// --------------------------------------------------------------------------

func TestGetWsClientIndex_AllocatesAndReturns(t *testing.T) {
	defer snapshotWsClientIndexList(t)()

	// Ensure all slots free.
	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	idx := getWsClientIndex()
	if idx != 0 {
		t.Errorf("first free index: got %d; want 0", idx)
	}
	if WsClientIndexList[0] != false {
		t.Errorf("slot 0 not marked occupied")
	}
	ReturnWsClientIndex(0)
	if WsClientIndexList[0] != true {
		t.Errorf("slot 0 not freed by ReturnWsClientIndex")
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
		t.Logf("note: second claim landed on slot %d, not the returned %d (acceptable but unusual)", second, first)
	}
}

func TestGetWsClientIndex_ExhaustionReturnsMinusOne(t *testing.T) {
	defer snapshotWsClientIndexList(t)()

	for i := range WsClientIndexList {
		WsClientIndexList[i] = false
	}
	if got := getWsClientIndex(); got != -1 {
		t.Errorf("exhausted pool should return -1; got %d", got)
	}
}

func TestGetWsClientIndex_AllocateAllReturnsExpectedOrder(t *testing.T) {
	defer snapshotWsClientIndexList(t)()

	for i := range WsClientIndexList {
		WsClientIndexList[i] = true
	}
	for want := 0; want < len(WsClientIndexList); want++ {
		got := getWsClientIndex()
		if got != want {
			t.Errorf("iteration %d: got %d", want, got)
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
// for the WsClientIndexMu. Without the mutex, two concurrent WS upgrades
// could both observe the same slot as free and both claim it.
func TestGetWsClientIndex_ConcurrentClaimsAreUnique(t *testing.T) {
	defer snapshotWsClientIndexList(t)()
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

// --------------------------------------------------------------------------
// frontendHttpAppSession — MaxBytesReader regression (oversize POST body)
// --------------------------------------------------------------------------

// TestFrontendHttpAppSession_RejectsOversizedBody is the regression test
// for the MaxBytesReader on the VISS HTTP POST handler. Before the fix,
// an unauthenticated peer could OOM the daemon by sending a giant POST body.
func TestFrontendHttpAppSession_RejectsOversizedBody(t *testing.T) {
	clientChannel := make(chan string, 4)

	body := bytes.NewReader(make([]byte, 256*1024)) // >> 64 KiB cap
	req := httptest.NewRequest("POST", "/Vehicle.Speed", body)
	rec := httptest.NewRecorder()

	frontendHttpAppSession(rec, req, clientChannel)

	got := rec.Body.String()
	if !bytes.Contains([]byte(got), []byte("too large")) {
		t.Fatalf("expected response body to mention 'too large'; got %q (status %d)", got, rec.Code)
	}
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

	go func() {
		req := <-clientChannel
		respChannel <- req
		clientChannel <- `{"action":"get","value":"ok"}`
	}()

	req := httptest.NewRequest("GET", "/Vehicle.Speed", nil)
	rec := httptest.NewRecorder()
	frontendHttpAppSession(rec, req, clientChannel)

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
