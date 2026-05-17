/**
* (C) 2026 Matt Jones / Ford
*
* Tests for udsMgr.go that pin each fix in this branch (fix/udsMgr-bugs)
* and cover every unit-testable function.
**/

package udsMgr

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("udsMgr_test-log.txt", "./logs", false, "info")
}

// resetChannels rebuilds the package-global channel arrays for the tests
// that exercise the channel-routing functions.
func resetChannels(t *testing.T) {
	t.Helper()
	initChannels()
}

// --------------------------------------------------------------------------
// mapString
// --------------------------------------------------------------------------

func TestMapString(t *testing.T) {
	m := map[string]interface{}{
		"a": "hello",
		"b": 42,
		"c": nil,
	}
	if v, ok := mapString(m, "a"); !ok || v != "hello" {
		t.Errorf("a: got %q,%v", v, ok)
	}
	if _, ok := mapString(m, "b"); ok {
		t.Errorf("non-string ok=true")
	}
	if _, ok := mapString(m, "c"); ok {
		t.Errorf("nil ok=true")
	}
	if _, ok := mapString(m, "missing"); ok {
		t.Errorf("missing ok=true")
	}
}

// --------------------------------------------------------------------------
// getValueForKey — pre-fix would underflow when key was absent.
// --------------------------------------------------------------------------

func TestGetValueForKey_HappyPath(t *testing.T) {
	msg := `{"action":"set","requestId":"req-1"}`
	if got := getValueForKey(msg, `"action"`); got != "set" {
		t.Errorf("got %q", got)
	}
	if got := getValueForKey(msg, `"requestId"`); got != "req-1" {
		t.Errorf("got %q", got)
	}
}

func TestGetValueForKey_MissingKey(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on missing key: %v", r)
		}
	}()
	if got := getValueForKey(`{"action":"set"}`, `"requestId"`); got != "" {
		t.Errorf("got %q; want \"\"", got)
	}
}

func TestGetValueForKey_KeyAtVeryEnd(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	// Key is present but no value after it.
	if got := getValueForKey(`{"x":"y","action"`, `"action"`); got != "" {
		t.Errorf("got %q; want \"\"", got)
	}
}

func TestGetValueForKey_NoSecondQuote(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := getValueForKey(`"action":"unterminated`, `"action"`); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestGetValueForKey_EmptyMessage(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := getValueForKey("", `"action"`); got != "" {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// signedTimeDiff — pre-fix would panic on empty diffMsStr via diffMsStr[1:].
// --------------------------------------------------------------------------

func TestSignedTimeDiff_Positive(t *testing.T) {
	if got := signedTimeDiff("100", 100); got != "-100" {
		t.Errorf("got %q", got)
	}
}

func TestSignedTimeDiff_Zero(t *testing.T) {
	if got := signedTimeDiff("0", 0); got != "+0" {
		t.Errorf("got %q", got)
	}
}

func TestSignedTimeDiff_Negative(t *testing.T) {
	if got := signedTimeDiff("-100", -100); got != "+100" {
		t.Errorf("got %q", got)
	}
}

func TestSignedTimeDiff_EmptyString(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := signedTimeDiff("", -1); got != "+0" {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// dc-cache helpers
// --------------------------------------------------------------------------

func TestDcCache_RoundTrip(t *testing.T) {
	initDcCache()
	dcCacheInsert("pid-1", "2+1", 1)
	idx := getDcCacheIndex("pid-1")
	if idx == -1 {
		t.Fatalf("payloadId not found")
	}
	if dataCompressionCache[idx].ResponseHandling != 1 {
		t.Errorf("got handling=%d", dataCompressionCache[idx].ResponseHandling)
	}
	if dataCompressionCache[idx].Dc.Pc != 2 || dataCompressionCache[idx].Dc.Tsc != 1 {
		t.Errorf("got dc=%+v", dataCompressionCache[idx].Dc)
	}
	resetDcCache(idx)
	if dataCompressionCache[idx].ResponseHandling != -1 {
		t.Errorf("reset failed")
	}
}

func TestDcCache_UpdatePayloadId(t *testing.T) {
	initDcCache()
	dcCacheInsert("req-1", "0+0", 3)
	updatepayloadId("req-1", "sub-1")
	if getDcCacheIndex("sub-1") == -1 {
		t.Errorf("payloadId not updated")
	}
	if getDcCacheIndex("req-1") != -1 {
		t.Errorf("old payloadId still present")
	}
}

func TestSetDcValue_RejectsUnsupportedPc(t *testing.T) {
	initDcCache()
	if got := setDcValue("99+1", 0); got {
		t.Errorf("unsupported pc accepted")
	}
}

func TestSetDcValue_RejectsUnsupportedTsc(t *testing.T) {
	initDcCache()
	if got := setDcValue("2+99", 0); got {
		t.Errorf("unsupported tsc accepted")
	}
}

func TestSetDcValue_RejectsMissingPlus(t *testing.T) {
	initDcCache()
	if got := setDcValue("just-a-string", 0); got {
		t.Errorf("missing-plus accepted")
	}
}

func TestGetDcCacheIndex_NotFound(t *testing.T) {
	initDcCache()
	if got := getDcCacheIndex("never-inserted"); got != -1 {
		t.Errorf("got %d", got)
	}
}

// --------------------------------------------------------------------------
// checkCompressionRequest / getDcConfig
// --------------------------------------------------------------------------

func TestCheckCompressionRequest_Path(t *testing.T) {
	initDcCache()
	// singleResponse=true (`get`), singlePath=true -> responseHandling=1
	req := `{"action":"get","path":"V","requestId":"r-1","dc":"2+1"}`
	checkCompressionRequest(req)
	idx := getDcCacheIndex("r-1")
	if idx == -1 {
		t.Fatalf("not cached")
	}
	if dataCompressionCache[idx].ResponseHandling != 1 {
		t.Errorf("got handling=%d", dataCompressionCache[idx].ResponseHandling)
	}
}

func TestCheckCompressionRequest_NoDc(t *testing.T) {
	initDcCache()
	checkCompressionRequest(`{"action":"get","path":"V","requestId":"r-1"}`)
	if getDcCacheIndex("r-1") != -1 {
		t.Errorf("non-dc request should not be cached")
	}
}

func TestGetDcConfig_PathsMultiple(t *testing.T) {
	req := `{"action":"get","paths":["A","B"],"requestId":"r","dc":"2+1"}`
	dcValue, _, isGet, singlePath := getDcConfig(req)
	if dcValue != "2+1" || !isGet || singlePath {
		t.Errorf("got dc=%q isGet=%v singlePath=%v", dcValue, isGet, singlePath)
	}
}

// --------------------------------------------------------------------------
// checkCompressionResponse — branch coverage for every action +
// ResponseHandling case.
// --------------------------------------------------------------------------

func TestCheckCompressionResponse_ErrorPassThrough(t *testing.T) {
	initDcCache()
	msg := `{"action":"get","error":{"number":400}}`
	if got := checkCompressionResponse(msg); got != msg {
		t.Errorf("error response should be untouched; got %q", got)
	}
}

func TestCheckCompressionResponse_UnknownActionPassThrough(t *testing.T) {
	initDcCache()
	msg := `{"action":"weird"}`
	if got := checkCompressionResponse(msg); got != msg {
		t.Errorf("unknown action should pass through; got %q", got)
	}
}

func TestCheckCompressionResponse_GetNotInCache(t *testing.T) {
	initDcCache()
	msg := `{"action":"get","requestId":"unknown-id"}`
	if got := checkCompressionResponse(msg); got != msg {
		t.Errorf("uncached id should pass through; got %q", got)
	}
}

func TestCheckCompressionResponse_GetHandling1_CompressesAndResets(t *testing.T) {
	initDcCache()
	// Seed cache: handling=1, Pc=2, Tsc=1
	dcCacheInsert("r-1", "2+1", 1)
	msg := `{"action":"get","requestId":"r-1","ts":"1970-01-01T00:00:01Z","data":[{"path":"Vehicle.A","dp":[{"value":"1","ts":"1970-01-01T00:00:00Z"}]}]}`
	got := checkCompressionResponse(msg)
	if !strings.Contains(got, `"path":"0"`) {
		t.Errorf("expected path compressed; got %q", got)
	}
	// Cache should be reset (handling=1 resets).
	if dataCompressionCache[0].ResponseHandling != -1 {
		t.Errorf("expected handling=-1 after reset; got %d", dataCompressionCache[0].ResponseHandling)
	}
}

func TestCheckCompressionResponse_GetHandling2_PassesThrough(t *testing.T) {
	initDcCache()
	dcCacheInsert("r-1", "2+1", 2)
	msg := `{"action":"get","requestId":"r-1","data":[{"path":"Vehicle.A","dp":[]}]}`
	got := checkCompressionResponse(msg)
	if got != msg {
		t.Errorf("handling=2 should pass through; got %q", got)
	}
}

func TestCheckCompressionResponse_SubscribeUpdatesPayloadId(t *testing.T) {
	initDcCache()
	dcCacheInsert("req-1", "2+1", 3)
	msg := `{"action":"subscribe","requestId":"req-1","subscriptionId":"sub-1"}`
	// Subscribe path doesn't return modified message - it updates the cache
	// id mapping so subsequent "subscription" events find the entry.
	_ = checkCompressionResponse(msg)
	if getDcCacheIndex("sub-1") == -1 {
		t.Errorf("subscribe should rename req-1 -> sub-1")
	}
	if getDcCacheIndex("req-1") != -1 {
		t.Errorf("old payloadId still present")
	}
}

func TestCheckCompressionResponse_SubscriptionHandling3_Compresses(t *testing.T) {
	initDcCache()
	dcCacheInsert("sub-1", "2+1", 3)
	dataCompressionCache[0].SortedList = []string{"Vehicle.A"}
	msg := `{"action":"subscription","subscriptionId":"sub-1","ts":"1970-01-01T00:00:01Z","data":[{"path":"Vehicle.A","dp":[{"value":"1","ts":"1970-01-01T00:00:00Z"}]}]}`
	got := checkCompressionResponse(msg)
	if !strings.Contains(got, `"path":"0"`) {
		t.Errorf("expected path compressed; got %q", got)
	}
	// Cache should NOT be reset for handling=3.
	if dataCompressionCache[0].ResponseHandling != 3 {
		t.Errorf("expected handling unchanged; got %d", dataCompressionCache[0].ResponseHandling)
	}
}

func TestCheckCompressionResponse_SubscriptionHandling4_TransitionsTo3(t *testing.T) {
	initDcCache()
	dcCacheInsert("sub-1", "0+1", 4) // Pc=0 so paths are not compressed
	msg := `{"action":"subscription","subscriptionId":"sub-1","ts":"1970-01-01T00:00:01Z","data":[{"path":"Vehicle.A","dp":[{"value":"1","ts":"1970-01-01T00:00:00Z"}]}]}`
	_ = checkCompressionResponse(msg)
	if dataCompressionCache[0].ResponseHandling != 3 {
		t.Errorf("expected transition 4->3; got %d", dataCompressionCache[0].ResponseHandling)
	}
}

func TestCheckCompressionResponse_UnsubscribeResets(t *testing.T) {
	initDcCache()
	dcCacheInsert("r-1", "2+1", 3)
	msg := `{"action":"unsubscribe","requestId":"r-1"}`
	_ = checkCompressionResponse(msg)
	if dataCompressionCache[0].ResponseHandling != -1 {
		t.Errorf("unsubscribe should reset cache; got %d", dataCompressionCache[0].ResponseHandling)
	}
}

func TestCheckCompressionResponse_NoDcEffectWhenPcZero(t *testing.T) {
	initDcCache()
	// Pc=0 -> path compression skipped; Tsc=1 -> ts compression applied.
	dcCacheInsert("r-1", "0+1", 1)
	msg := `{"action":"get","requestId":"r-1","ts":"1970-01-01T00:00:01Z","data":[{"path":"Vehicle.A","dp":[{"value":"1","ts":"1970-01-01T00:00:00Z"}]}]}`
	got := checkCompressionResponse(msg)
	// Path "Vehicle.A" should still be present (no path compression).
	if !strings.Contains(got, "Vehicle.A") {
		t.Errorf("Pc=0 should leave path intact; got %q", got)
	}
}

func TestCheckCompressionResponse_MalformedMessageDoesNotPanic(t *testing.T) {
	initDcCache()
	dcCacheInsert("r-1", "2+1", 1)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	_ = checkCompressionResponse(`{"action":"get","requestId":"r-1","ts":"bogus","data":"not-an-array"}`)
}

// --------------------------------------------------------------------------
// getSortedPaths — pre-fix had naked .(map[string]interface{}) and .(string)
// --------------------------------------------------------------------------

func TestGetSortedPaths_DataIsArray(t *testing.T) {
	msg := `{"action":"get","data":[{"path":"Vehicle.B","dp":{"value":"1","ts":"t"}},{"path":"Vehicle.A","dp":{"value":"2","ts":"t"}}]}`
	got := getSortedPaths(msg)
	if len(got) != 2 || got[0] != "Vehicle.A" || got[1] != "Vehicle.B" {
		t.Errorf("got %v", got)
	}
}

func TestGetSortedPaths_DataIsObject(t *testing.T) {
	msg := `{"action":"get","data":{"path":"Vehicle.X","dp":{"value":"1","ts":"t"}}}`
	got := getSortedPaths(msg)
	if len(got) != 1 || got[0] != "Vehicle.X" {
		t.Errorf("got %v", got)
	}
}

func TestGetSortedPaths_MalformedDoesNotPanic(t *testing.T) {
	cases := []string{
		`not-json`,
		`{"action":"get"}`,
		`{"data":"not-an-array-or-object"}`,
		`{"data":[42,"x"]}`,
		`{"data":[{"path":99}]}`,
		`{"data":{"path":42}}`,
		`{"data":[{},{"path":"Vehicle.A"}]}`,
	}
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked on %q: %v", c, r)
				}
			}()
			_ = getSortedPaths(c)
		})
	}
}

// --------------------------------------------------------------------------
// compressTs — pre-fix panicked on missing/wrong-type ts and on bad data shape
// --------------------------------------------------------------------------

func TestCompressTs_HappyPathArray(t *testing.T) {
	// messageTs at 1000ms, datapoint at 0ms. signedTimeDiff convention:
	// diffMs = refMs - dpMs = +1000 -> output prefixed "-" (dp is 1000ms
	// OLDER than ref).
	msg := `{"action":"get","ts":"1970-01-01T00:00:01Z","data":[{"path":"V","dp":[{"value":"1","ts":"1970-01-01T00:00:00Z"}]}]}`
	got := compressTs(msg)
	if !strings.Contains(got, `"ts":"-1000"`) {
		t.Errorf("got %q", got)
	}
}

func TestCompressTs_MissingTsLeavesUnchanged(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	msg := `{"action":"get","data":[{"path":"V","dp":[{"value":"1","ts":"1970-01-01T00:00:00Z"}]}]}`
	got := compressTs(msg)
	if got != msg {
		t.Errorf("expected unchanged; got %q", got)
	}
}

func TestCompressTs_MalformedDoesNotPanic(t *testing.T) {
	cases := []string{
		`not-json`,
		`{"ts":42}`,
		`{"ts":"1970-01-01T00:00:01Z","data":"not-an-array"}`,
		`{"ts":"1970-01-01T00:00:01Z","data":[42]}`,
		`{"ts":"1970-01-01T00:00:01Z","data":[{"dp":"not-an-object"}]}`,
		`{"ts":"bogus-time","data":[{"dp":[{"ts":"1970-01-01T00:00:00Z"}]}]}`,
	}
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked on %q: %v", c, r)
				}
			}()
			_ = compressTs(c)
		})
	}
}

// --------------------------------------------------------------------------
// getDpTsList
// --------------------------------------------------------------------------

func TestGetDpTsList_ArrayShape(t *testing.T) {
	dp := []interface{}{
		map[string]interface{}{"ts": "a", "value": "1"},
		map[string]interface{}{"ts": "b", "value": "2"},
	}
	got := getDpTsList(dp)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v", got)
	}
}

func TestGetDpTsList_ObjectShape(t *testing.T) {
	dp := map[string]interface{}{"ts": "a", "value": "1"}
	got := getDpTsList(dp)
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("got %v", got)
	}
}

func TestGetDpTsList_MalformedDoesNotPanic(t *testing.T) {
	cases := []interface{}{
		nil,
		"string",
		42,
		[]interface{}{42, "x"},
		[]interface{}{map[string]interface{}{"ts": 42}},
		map[string]interface{}{"ts": 42},
	}
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			_ = getDpTsList(c)
		})
	}
}

// --------------------------------------------------------------------------
// replaceTs — verifies time.Parse error handling
// --------------------------------------------------------------------------

func TestReplaceTs_BadMessageTsReturnsUnchanged(t *testing.T) {
	msg := `{"ts":"bogus","data":[{"dp":[{"ts":"1970-01-01T00:00:00Z"}]}]}`
	if got := replaceTs(msg, "bogus", []string{"1970-01-01T00:00:00Z"}); got != msg {
		t.Errorf("expected unchanged on bad messageTs; got %q", got)
	}
}

func TestReplaceTs_MissingMessageTsReturnsUnchanged(t *testing.T) {
	msg := `{"ts":"1970-01-01T00:00:01Z","data":{}}`
	if got := replaceTs(msg, "this-substring-is-not-in-msg", nil); got != msg {
		t.Errorf("expected unchanged; got %q", got)
	}
}

// --------------------------------------------------------------------------
// compressPaths
// --------------------------------------------------------------------------

func TestCompressPaths(t *testing.T) {
	msg := `{"data":[{"path":"Vehicle.A"},{"path":"Vehicle.B"}]}`
	sorted := []string{"Vehicle.A", "Vehicle.B"}
	got := compressPaths(msg, sorted)
	if !strings.Contains(got, `"path":"0"`) || !strings.Contains(got, `"path":"1"`) {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// isKillSubscriptions — the security regression. Pre-fix used
// strings.Contains, allowing any feeder to bypass JSON-schema validation by
// embedding "internal-killsubscriptions" inside a path or value.
// --------------------------------------------------------------------------

func TestIsKillSubscriptions_ActualAction(t *testing.T) {
	if !isKillSubscriptions(`{"action":"internal-killsubscriptions"}`) {
		t.Errorf("expected true")
	}
}

func TestIsKillSubscriptions_SubstringInPathDoesNotBypass(t *testing.T) {
	// Pre-fix this returned true (substring contains the literal) and
	// skipped JSON schema validation.
	if isKillSubscriptions(`{"action":"set","path":"injected-internal-killsubscriptions-here"}`) {
		t.Errorf("substring should NOT trigger; this is the security regression")
	}
}

func TestIsKillSubscriptions_MalformedJSON(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if isKillSubscriptions(`not-json`) {
		t.Errorf("malformed JSON should not match")
	}
}

func TestIsKillSubscriptions_MissingAction(t *testing.T) {
	if isKillSubscriptions(`{}`) {
		t.Errorf("missing action should not match")
	}
}

func TestIsKillSubscriptions_NonStringAction(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if isKillSubscriptions(`{"action":42}`) {
		t.Errorf("non-string action should not match")
	}
}

// --------------------------------------------------------------------------
// getUdsClientIndex / returnUdsClientIndex — race-safe slot allocator.
// --------------------------------------------------------------------------

func TestGetUdsClientIndex_ExhaustionReturnsMinusOne(t *testing.T) {
	resetChannels(t)
	// Drain all slots.
	taken := make([]int, 0, NUMOFUDSCLIENTS)
	for i := 0; i < NUMOFUDSCLIENTS; i++ {
		idx := getUdsClientIndex()
		if idx == -1 {
			t.Fatalf("got -1 on iteration %d (NUMOFUDSCLIENTS=%d)", i, NUMOFUDSCLIENTS)
		}
		taken = append(taken, idx)
	}
	if got := getUdsClientIndex(); got != -1 {
		t.Errorf("expected -1 on exhaustion; got %d", got)
	}
	for _, i := range taken {
		returnUdsClientIndex(i)
	}
}

func TestReturnUdsClientIndex_OOBSafe(t *testing.T) {
	resetChannels(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on OOB return: %v", r)
		}
	}()
	returnUdsClientIndex(-1)
	returnUdsClientIndex(NUMOFUDSCLIENTS)
	returnUdsClientIndex(NUMOFUDSCLIENTS + 100)
}

func TestUdsClientIndex_RaceFree(t *testing.T) {
	// Mutex-protected; race detector should be happy.
	resetChannels(t)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				idx := getUdsClientIndex()
				if idx >= 0 {
					returnUdsClientIndex(idx)
				}
			}
		}()
	}
	wg.Wait()
}

// --------------------------------------------------------------------------
// RemoveRoutingForwardResponse — bounds-check + timeout fix.
// --------------------------------------------------------------------------

func TestRemoveRoutingForwardResponse_OOBClientIdDropped(t *testing.T) {
	resetChannels(t)
	// Craft a response with RouterId=99?99 (out of range) - clientId becomes 99.
	resp := `{"action":"get","RouterId":"0?99","data":{}}`
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on OOB clientId: %v", r)
		}
	}()
	done := make(chan struct{})
	go func() {
		RemoveRoutingForwardResponse(resp, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("blocked too long on OOB clientId path")
	}
}

func TestRemoveRoutingForwardResponse_SubscriptionRouted(t *testing.T) {
	resetChannels(t)
	resp := `{"action":"subscription","RouterId":"0?0","subscriptionId":"s1","data":{}}`
	// The subscription path uses select-with-default so the send drops if
	// the consumer goroutine hasn't reached the receive yet. Capture the
	// channel into a local so any leaked goroutine doesn't race with the
	// next test's resetChannels() (which reassigns the slice slot).
	target := clientBackendChan[0]
	got := make(chan string, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case v := <-target:
			got <- v
		case <-time.After(2 * time.Second):
		}
	}()
	// Give the consumer a beat to reach the receive (select-with-default
	// has no other synchronisation primitive).
	time.Sleep(50 * time.Millisecond)
	RemoveRoutingForwardResponse(resp, nil)
	select {
	case msg := <-got:
		if !strings.Contains(msg, `"subscription"`) {
			t.Errorf("got %q", msg)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("subscription not routed")
	}
	wg.Wait()
}

func TestRemoveRoutingForwardResponse_ResponseRouted(t *testing.T) {
	resetChannels(t)
	resp := `{"action":"get","RouterId":"0?0","data":{}}`
	// Capture target chan locally to avoid racing with later resetChannels.
	target := udsClientChan[0]
	got := make(chan string, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case v := <-target:
			got <- v
		case <-time.After(2 * time.Second):
		}
	}()
	RemoveRoutingForwardResponse(resp, nil)
	select {
	case msg := <-got:
		if !strings.Contains(msg, `"action":"get"`) {
			t.Errorf("got %q", msg)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("response not routed")
	}
	wg.Wait()
}

func TestRemoveRoutingForwardResponse_DeadReaderTimesOut(t *testing.T) {
	// Pre-fix would block forever. Now bounded by channelSendTimeout.
	// We use a very short test by overriding the const in the test only
	// indirectly: we just observe the function returns before our outer
	// timeout. The real channelSendTimeout is 5s, but the test will not
	// hit it on the dead-reader path because there's no consumer, so we
	// expect ~5s. Skip in short mode.
	if testing.Short() {
		t.Skip("skipping 5s timeout test in short mode")
	}
	resetChannels(t)
	resp := `{"action":"get","RouterId":"0?0","data":{}}`
	done := make(chan struct{})
	go func() {
		RemoveRoutingForwardResponse(resp, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(channelSendTimeout + 2*time.Second):
		t.Fatalf("did not time out within %s", channelSendTimeout+2*time.Second)
	}
}

// --------------------------------------------------------------------------
// udsWriter — forwards channel messages to the conn until backendTermination
// or a write error.
// --------------------------------------------------------------------------

func TestUdsWriter_WritesMessage(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientChan := make(chan string, 4)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		udsWriter(server, clientChan, quit)
		close(done)
	}()
	clientChan <- "hello-feeder"
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read err: %v", err)
	}
	if string(buf[:n]) != "hello-feeder" {
		t.Errorf("got %q", string(buf[:n]))
	}
	close(quit)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("udsWriter did not exit on quit")
	}
}

func TestUdsWriter_ExitsOnBackendTermination(t *testing.T) {
	server, _ := net.Pipe()
	defer server.Close()
	clientChan := make(chan string, 4)
	quit := make(chan struct{})
	defer close(quit)
	done := make(chan struct{})
	go func() {
		udsWriter(server, clientChan, quit)
		close(done)
	}()
	clientChan <- backendTermination
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not exit on backendTermination")
	}
}

func TestUdsWriter_ExitsOnWriteError(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	clientChan := make(chan string, 4)
	quit := make(chan struct{})
	defer close(quit)
	client.Close() // peer closed -> Write will error
	done := make(chan struct{})
	go func() {
		udsWriter(server, clientChan, quit)
		close(done)
	}()
	clientChan <- "anything"
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not exit on Write error")
	}
}

func TestUdsWriter_ExitsOnQuit(t *testing.T) {
	server, _ := net.Pipe()
	defer server.Close()
	clientChan := make(chan string)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		udsWriter(server, clientChan, quit)
		close(done)
	}()
	close(quit)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not exit on quit")
	}
}

// --------------------------------------------------------------------------
// udsReader — synchronous request/response loop on net.Pipe.
// --------------------------------------------------------------------------

func TestUdsReader_ForwardsRequestAndForwardsResponse(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	clientChan := make(chan string, 4)
	backendChan := make(chan string, 4)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		udsReader(server, clientChan, backendChan, 0, quit)
		close(done)
	}()
	// Feeder sends a request.
	go func() { client.Write([]byte(`{"action":"get"}`)) }()
	// Reader forwards to clientChan.
	select {
	case got := <-clientChan:
		if got != `{"action":"get"}` {
			t.Errorf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request not forwarded")
	}
	// Hub sends response.
	clientChan <- `{"action":"get","data":{}}`
	// Reader forwards to backendChan.
	select {
	case got := <-backendChan:
		if !strings.Contains(got, `"action":"get"`) {
			t.Errorf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("response not forwarded to backendChan")
	}
	// Close the read side so the next conn.Read returns and the reader exits.
	// quit alone won't unblock conn.Read (that's a limitation of net.Pipe -
	// in production serveConn closes the conn before signalling quit).
	server.Close()
	// Drain the error-path messages the reader emits before returning.
	<-clientChan    // internal-killsubscriptions
	<-backendChan   // backendTermination
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not exit")
	}
	close(quit)
}

func TestUdsReader_TruncationDropped(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	clientChan := make(chan string, 4)
	backendChan := make(chan string, 4)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		udsReader(server, clientChan, backendChan, 0, quit)
		close(done)
	}()
	// Fill the buffer exactly -> dropped as likely-truncated.
	big := make([]byte, udsReadBuf)
	for i := range big {
		big[i] = 'A'
	}
	go func() { client.Write(big) }()
	time.Sleep(100 * time.Millisecond)
	if len(clientChan) != 0 {
		t.Errorf("oversized frame should be dropped")
	}
	server.Close()
	<-clientChan  // drain kill-subscriptions
	<-backendChan // drain backendTermination
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not exit")
	}
	close(quit)
}

func TestUdsReader_EOFSendsKillSubscriptions(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	clientChan := make(chan string, 4)
	backendChan := make(chan string, 4)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		udsReader(server, clientChan, backendChan, 0, quit)
		close(done)
	}()
	client.Close() // -> read error
	// Expect kill-subscriptions sent.
	select {
	case got := <-clientChan:
		if !strings.Contains(got, "internal-killsubscriptions") {
			t.Errorf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kill-subscriptions not sent on EOF")
	}
	// Expect backendTermination.
	select {
	case got := <-backendChan:
		if got != backendTermination {
			t.Errorf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backendTermination not sent on EOF")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not exit after EOF")
	}
	close(quit)
}

func TestUdsReader_ExitsWhenConnClosedExternally(t *testing.T) {
	// Closing the conn under the reader (as serveConn does on shutdown)
	// must cause the reader to return rather than wedge in conn.Read.
	server, _ := net.Pipe()
	clientChan := make(chan string, 4)
	backendChan := make(chan string, 4)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		udsReader(server, clientChan, backendChan, 0, quit)
		close(done)
	}()
	server.Close() // -> conn.Read returns error -> reader exits
	// Reader will try kill-subscriptions + backendTermination first; drain
	// both so it can return.
	<-clientChan
	<-backendChan
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not exit on conn close")
	}
	close(quit)
}

// --------------------------------------------------------------------------
// serveConn — full reader+writer lifecycle, slot reclaim, conn cleanup.
// --------------------------------------------------------------------------

func TestServeConn_ReclaimsSlotOnExit(t *testing.T) {
	resetChannels(t)
	idx := getUdsClientIndex()
	if idx < 0 {
		t.Fatalf("no free slot")
	}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		serveConn(server, idx)
		close(done)
	}()
	// Consume the kill-subscriptions message the reader sends on EOF so the
	// reader's send doesn't have to wait channelSendTimeout. The writer
	// consumes backendTermination directly. udsClientChan is unbuffered;
	// without a consumer the reader's send blocks for the full 5s timeout.
	go func() {
		select {
		case <-udsClientChan[idx]:
		case <-time.After(3 * time.Second):
		}
	}()
	client.Close() // EOF -> reader exits -> writer signaled -> serveConn returns
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serveConn did not return")
	}
	// Slot should now be free again.
	indexListMu.Lock()
	free := UdsClientIndexList[idx]
	indexListMu.Unlock()
	if !free {
		t.Errorf("slot %d not reclaimed", idx)
	}
}

// --------------------------------------------------------------------------
// Verify the structural pieces of the package wire up correctly.
// --------------------------------------------------------------------------

func TestInitChannels_AllChannelsAllocated(t *testing.T) {
	initChannels()
	if len(udsClientChan) != NUMOFUDSCLIENTS {
		t.Errorf("udsClientChan len=%d", len(udsClientChan))
	}
	if len(clientBackendChan) != NUMOFUDSCLIENTS {
		t.Errorf("clientBackendChan len=%d", len(clientBackendChan))
	}
	if len(UdsClientIndexList) != NUMOFUDSCLIENTS {
		t.Errorf("UdsClientIndexList len=%d", len(UdsClientIndexList))
	}
	for i := 0; i < NUMOFUDSCLIENTS; i++ {
		if udsClientChan[i] == nil || clientBackendChan[i] == nil {
			t.Errorf("nil channel at %d", i)
		}
		if !UdsClientIndexList[i] {
			t.Errorf("slot %d not marked free at init", i)
		}
	}
}

// TestReturnUdsClientIndex_MakesSlotReclaimable confirms basic semantics:
// a returned slot is claimable again.
func TestReturnUdsClientIndex_MakesSlotReclaimable(t *testing.T) {
	resetChannels(t)

	first := getUdsClientIndex()
	if first == -1 {
		t.Fatalf("initial getUdsClientIndex returned -1 with all slots free")
	}
	returnUdsClientIndex(first)
	second := getUdsClientIndex()
	if second == -1 {
		t.Fatalf("after returning slot %d, expected it claimable again; got -1", first)
	}
}

func TestInitDcCache_AllSlotsMinusOne(t *testing.T) {
	initDcCache()
	if len(dataCompressionCache) != DCCACHESIZE {
		t.Fatalf("cache len=%d", len(dataCompressionCache))
	}
	for i := 0; i < DCCACHESIZE; i++ {
		if dataCompressionCache[i].ResponseHandling != -1 {
			t.Errorf("slot %d not initialised to -1", i)
		}
	}
}

// --------------------------------------------------------------------------
// Smoke: errorResponseMap should be a fresh map each iteration so leaked
// keys (e.g. subscriptionId from a previous error response) don't show up
// in the next one. We verify the marshalled output is valid JSON for a
// representative error shape; the per-iteration freshness is a property of
// the rewritten hub (UdsMgrInit) which is itself integration-only.
// --------------------------------------------------------------------------

func TestErrorResponseMap_FinalizeIsValidJSON(t *testing.T) {
	errorResponseMap := map[string]interface{}{}
	requestMap := map[string]interface{}{
		"action":    "set",
		"requestId": "r-1",
	}
	utils.SetErrorResponse(requestMap, errorResponseMap, 0, "test error")
	out := utils.FinalizeMessage(errorResponseMap)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("FinalizeMessage produced non-JSON: %v / %s", err, out)
	}
	if parsed["error"] == nil {
		t.Errorf("missing error key: %s", out)
	}
}

// --------------------------------------------------------------------------
// Integration-only entry point
//
// initClientServer and UdsMgrInit are top-level goroutine drivers. They bind
// the fixed UDS socket at /var/tmp/vissv2/udsMgr.sock, run an unbounded for/
// select loop, and call utils.JsonSchemaValidate against the embedded schema.
// They are exercised end-to-end by the server's integration tests.
//
// Every inner helper they call - mapString, getValueForKey, getSortedPaths,
// compressTs, getDpTsList, replaceTs, signedTimeDiff, compressPaths,
// checkCompressionRequest, checkCompressionResponse, getDcConfig,
// dcCacheInsert, setDcValue, updatepayloadId, getDcCacheIndex, resetDcCache,
// isKillSubscriptions, getUdsClientIndex, returnUdsClientIndex,
// RemoveRoutingForwardResponse, udsReader, udsWriter, serveConn,
// initChannels, initDcCache - is covered above.
// --------------------------------------------------------------------------
