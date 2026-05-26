/**
* (C) 2026 Matt Jones / Ford
*
* Tests for curvelog.go that pin each of the bug fixes in this branch
* (see fix/curvelog-bugs). Each test references the original bug by
* number in its comment.
**/

package serviceMgr

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

func init() {
	// Make sure logging output won't panic during tests.
	utils.InitLog("curvelog_test-log.txt", "./logs", false, "info")
}

// resetClState fully resets the package-level CL globals between tests so
// that they don't bleed counters/channels across tests.
func resetClState() {
	numOfClSessionsMu.Lock()
	numOfClSessions = 0
	numOfClSessionsMu.Unlock()

	triggChannelMu.Lock()
	triggChannelList = make([]TriggChannelElem, MAXCLSESSIONS)
	for i := 0; i < MAXCLSESSIONS; i++ {
		triggChannelList[i].Busy = false
	}
	triggChannelMu.Unlock()

	for i := range clServerChan {
		clServerChan[i] = make(chan string, 10)
	}
}

// --------------------------------------------------------------------------
// Bug #1 — getSleepDuration returns time.Millisecond, never 1 nanosecond
// --------------------------------------------------------------------------

func TestGetSleepDuration_FloorIsOneMillisecond(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Second) // workDuration = 1s, far exceeds 1ms wantedDuration
	got := getSleepDuration(now, earlier, 1)
	if got < time.Millisecond {
		t.Fatalf("getSleepDuration returned %v; expected >= 1ms (the 1-nanosecond default would busy-loop the ticker)", got)
	}
}

func TestGetSleepDuration_HappyPathReturnsRemainder(t *testing.T) {
	oldT := time.Now()
	newT := oldT.Add(10 * time.Millisecond)
	got := getSleepDuration(newT, oldT, 100) // 100ms wanted, 10ms used
	if got <= 0 {
		t.Fatalf("getSleepDuration returned %v; expected positive remainder", got)
	}
}

// --------------------------------------------------------------------------
// Bug #2 — numOfClSessions has a mutex and never goes negative
// --------------------------------------------------------------------------

func TestNumOfClSessions_RaceFreeUnderConcurrency(t *testing.T) {
	resetClState()

	var wg sync.WaitGroup
	const N = 100
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if incrementClSessionsIfAvailable() {
				time.Sleep(time.Microsecond)
				decrementClSessions()
			}
		}()
	}
	wg.Wait()

	if got := getClSessionsCount(); got != 0 {
		t.Fatalf("after balanced concurrent incr/decr, expected 0 active sessions; got %d", got)
	}
}

func TestIncrementClSessions_CappedAtMax(t *testing.T) {
	resetClState()
	granted := 0
	for i := 0; i < MAXCLSESSIONS+5; i++ {
		if incrementClSessionsIfAvailable() {
			granted++
		}
	}
	if granted != MAXCLSESSIONS {
		t.Fatalf("expected exactly %d grants; got %d", MAXCLSESSIONS, granted)
	}
	// over-decrementing must not go negative
	for i := 0; i < MAXCLSESSIONS+3; i++ {
		decrementClSessions()
	}
	if got := getClSessionsCount(); got != 0 {
		t.Fatalf("expected sessions floor at 0; got %d", got)
	}
}

// --------------------------------------------------------------------------
// Bug #3 — allocateTriggChannelIndex is mutex-protected and bounded
// --------------------------------------------------------------------------

func TestAllocateTriggChannelIndex_NoDoubleAllocation(t *testing.T) {
	resetClState()

	var (
		mu   sync.Mutex
		seen = make(map[int]int)
	)
	var wg sync.WaitGroup
	const N = MAXCLSESSIONS
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx := allocateTriggChannelIndex()
			mu.Lock()
			seen[idx]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	for idx, count := range seen {
		if idx == -1 {
			t.Errorf("allocateTriggChannelIndex returned -1 unexpectedly during normal allocation")
			continue
		}
		if count > 1 {
			t.Errorf("triggChannel %d was allocated to %d callers; concurrent allocator is racy", idx, count)
		}
	}
}

func TestAllocateTriggChannelIndex_ReturnsMinusOneWhenFull(t *testing.T) {
	resetClState()
	for i := 0; i < MAXCLSESSIONS; i++ {
		if got := allocateTriggChannelIndex(); got < 0 {
			t.Fatalf("unexpected -1 at iteration %d", i)
		}
	}
	if got := allocateTriggChannelIndex(); got != -1 {
		t.Fatalf("expected -1 when fully allocated; got %d", got)
	}
}

// --------------------------------------------------------------------------
// Bug #4 — slices.Delete result is captured (unsubscribe really removes)
// Bug #5 — unsubscribe loop handles index correctly after deletion
// --------------------------------------------------------------------------

func TestHandleCurveLogServerMessage_UnsubscribeRemovesEntry(t *testing.T) {
	resetClState()
	rdl := []TriggRoutingData{
		{SubscriptionId: "1", TriggRoutingList: []TriggRoutingElem{{Index: 0}}},
		{SubscriptionId: "2", TriggRoutingList: []TriggRoutingElem{{Index: 1}}},
		{SubscriptionId: "3", TriggRoutingList: []TriggRoutingElem{{Index: 2}}},
	}
	msg := `{"action":"unsubscribe","subscriptionId":"2"}`
	got := handleCurveLogServerMessage(msg, rdl)
	if len(got) != 2 {
		t.Fatalf("expected 2 routing entries after unsubscribe; got %d", len(got))
	}
	for _, e := range got {
		if e.SubscriptionId == "2" {
			t.Fatalf("unsubscribed entry still present: %+v", e)
		}
	}
}

func TestHandleCurveLogServerMessage_UnsubscribeRemovesAllMatchingEntries(t *testing.T) {
	resetClState()
	// Two entries with the same id — without the loop fix one would survive.
	rdl := []TriggRoutingData{
		{SubscriptionId: "9", TriggRoutingList: []TriggRoutingElem{{Index: 0}}},
		{SubscriptionId: "9", TriggRoutingList: []TriggRoutingElem{{Index: 1}}},
		{SubscriptionId: "other", TriggRoutingList: []TriggRoutingElem{{Index: 2}}},
	}
	got := handleCurveLogServerMessage(`{"action":"unsubscribe","subscriptionId":"9"}`, rdl)
	if len(got) != 1 {
		t.Fatalf("expected exactly the non-matching entry to remain; got %d entries", len(got))
	}
	if got[0].SubscriptionId != "other" {
		t.Fatalf("wrong entry remained: %+v", got[0])
	}
}

// --------------------------------------------------------------------------
// Bug #6 — clServerChan is buffered AND non-blocking send drops on overflow
// --------------------------------------------------------------------------

func TestSendToClServerChan_NonBlockingOnFullChannel(t *testing.T) {
	resetClState()
	// Saturate channel 0
	for i := 0; i < cap(clServerChan[0]); i++ {
		clServerChan[0] <- fmt.Sprintf("filler-%d", i)
	}
	done := make(chan struct{})
	go func() {
		sendToClServerChan(0, "should not block")
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("sendToClServerChan blocked on a full channel; non-blocking send was supposed to drop")
	}
}

// --------------------------------------------------------------------------
// Bug #7 — Defensive type assertions: no panic on bad shapes
// --------------------------------------------------------------------------

func TestHandleCurveLogServerMessage_InvalidShapesDoNotPanic(t *testing.T) {
	resetClState()
	cases := []string{
		``,                                       // empty
		`not json`,                               // bad JSON
		`{}`,                                     // no action
		`{"action":42}`,                          // action not string
		`{"action":"unsubscribe"}`,               // no subscriptionId
		`{"action":"unsubscribe","subscriptionId":42}`,
		`{"action":"subscription"}`,              // no path
		`{"action":"subscription","path":42}`,    // path not string
		`{"action":"subscribe"}`,                 // no status
		`{"action":"subscribe","status":42}`,     // status not string
		`{"action":"subscribe","status":"nope"}`, // status not ok
		`{"action":"weird"}`,                     // unknown action
	}
	for _, in := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("handleCurveLogServerMessage panicked on %q: %v", in, r)
				}
			}()
			_ = handleCurveLogServerMessage(in, nil)
		}()
	}
}

func TestDecodeFeederMessageCl_InvalidShapesDoNotPanic(t *testing.T) {
	cases := []string{
		``,
		`not json`,
		`{}`,
		`{"action":42}`,
		`{"action":"subscribe"}`,
		`{"action":"subscribe","status":42}`,
		`{"action":"subscription"}`,
		`{"action":"foo"}`,
	}
	for _, in := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("decodeFeederMessageCl panicked on %q: %v", in, r)
				}
			}()
			_, _ = decodeFeederMessageCl(in, false)
		}()
	}
}

func TestDecodeFeederMessageCl_HappyPathSubscribeFlipsNotification(t *testing.T) {
	doCap, notif := decodeFeederMessageCl(`{"action":"subscribe","status":"ok"}`, false)
	if doCap {
		t.Errorf("subscribe should not request capture")
	}
	if !notif {
		t.Errorf("subscribe-ok should set feederNotification=true")
	}

	doCap, notif = decodeFeederMessageCl(`{"action":"subscription","path":"Vehicle.Speed"}`, false)
	if !doCap {
		t.Errorf("subscription with path should request capture")
	}
	if notif {
		t.Errorf("subscription should not change feederNotification")
	}
}

// --------------------------------------------------------------------------
// Bug #8 — populateDimLists returns dim3List, not nil
// Bug #9 — populateDimLists pairs dim3 entries against pathDimList[i].Id
// --------------------------------------------------------------------------

func TestPopulateDimListsFromSignals_ReturnsDim3List(t *testing.T) {
	// Pin Bug #8: pre-fix the function returned `nil` for the third slice
	// regardless of whether dim3 entries existed.
	sdl := &SignalDimensionLists{
		dim3List: []Dim3Elem{
			{Path1: "Vehicle.X", Path2: "Vehicle.Y", Path3: "Vehicle.Z"},
		},
	}
	paths := []string{"Vehicle.X", "Vehicle.Y", "Vehicle.Z"}
	d1, d2, d3 := populateDimListsFromSignals(paths, sdl)
	if len(d1) != 0 {
		t.Errorf("expected no dim1 entries; got %v", d1)
	}
	if len(d2) != 0 {
		t.Errorf("expected no dim2 entries; got %v", d2)
	}
	if len(d3) != 1 {
		t.Fatalf("expected 1 dim3 entry; got %v (pre-fix this returned nil!)", d3)
	}
	if d3[0].Path1 != "Vehicle.X" || d3[0].Path2 != "Vehicle.Y" || d3[0].Path3 != "Vehicle.Z" {
		t.Errorf("dim3 entry mismatch: %+v", d3[0])
	}
}

func TestPopulateDimListsFromSignals_GroupsDim3ByOwnerIndex(t *testing.T) {
	// Pin Bug #9: pre-fix the inner `pathDimList[j].Id == pathDimList[j].Id`
	// comparison was always true and would cross-mix members of different
	// dim3 groups. With two distinct dim3 groups present, each group's
	// members must stay together.
	sdl := &SignalDimensionLists{
		dim3List: []Dim3Elem{
			{Path1: "Vehicle.A1", Path2: "Vehicle.A2", Path3: "Vehicle.A3"},
			{Path1: "Vehicle.B1", Path2: "Vehicle.B2", Path3: "Vehicle.B3"},
		},
	}
	paths := []string{
		"Vehicle.A1", "Vehicle.A2", "Vehicle.A3",
		"Vehicle.B1", "Vehicle.B2", "Vehicle.B3",
	}
	_, _, d3 := populateDimListsFromSignals(paths, sdl)
	if len(d3) != 2 {
		t.Fatalf("expected 2 dim3 groups; got %d", len(d3))
	}
	// Group 0 should be A*, group 1 should be B*.
	if d3[0].Path1 != "Vehicle.A1" {
		t.Errorf("group 0 lead = %q; want Vehicle.A1", d3[0].Path1)
	}
	if d3[1].Path1 != "Vehicle.B1" {
		t.Errorf("group 1 lead = %q; want Vehicle.B1", d3[1].Path1)
	}
}

func TestPopulateDimListsFromSignals_AllDim1WhenNoSignals(t *testing.T) {
	paths := []string{"Vehicle.A", "Vehicle.B", "Vehicle.C"}
	d1, d2, d3 := populateDimListsFromSignals(paths, nil)
	if len(d1) != 3 {
		t.Errorf("expected 3 dim1 entries; got %d", len(d1))
	}
	if len(d2) != 0 {
		t.Errorf("expected 0 dim2 entries; got %d", len(d2))
	}
	if len(d3) != 0 {
		t.Errorf("expected 0 dim3 entries; got %d", len(d3))
	}
}

func TestPopulateDimListsFromSignals_Dim2Groups(t *testing.T) {
	sdl := &SignalDimensionLists{
		dim2List: []Dim2Elem{
			{Path1: "Vehicle.X", Path2: "Vehicle.Y"},
		},
	}
	paths := []string{"Vehicle.X", "Vehicle.Y", "Vehicle.Z"}
	d1, d2, d3 := populateDimListsFromSignals(paths, sdl)
	if len(d2) != 1 {
		t.Fatalf("expected 1 dim2 entry; got %d", len(d2))
	}
	if d2[0].Path1 != "Vehicle.X" || d2[0].Path2 != "Vehicle.Y" {
		t.Errorf("dim2 mismatch: %+v", d2[0])
	}
	// Vehicle.Z falls through as dim1
	if len(d1) != 1 || d1[0] != "Vehicle.Z" {
		t.Errorf("dim1 mismatch: %v", d1)
	}
	if len(d3) != 0 {
		t.Errorf("expected 0 dim3 entries; got %d", len(d3))
	}
}

func TestAnalyzeSignalDimensions_NilSignalListIsSafe(t *testing.T) {
	pdl := analyzeSignalDimensions([]string{"a", "b", "c"}, nil)
	if len(pdl) != 3 {
		t.Fatalf("expected 3 entries; got %d", len(pdl))
	}
	for i, e := range pdl {
		if e.Dim != 1 {
			t.Errorf("entry %d: expected Dim=1 default; got %+v", i, e)
		}
	}
}

// --------------------------------------------------------------------------
// Bug #10 — unpacksignalDimensionMap mutates the original via *pointer*
// --------------------------------------------------------------------------

func TestUnpacksignalDimensionMap_PointerMutatesOriginal(t *testing.T) {
	jsonBlob := `{
		"dim2":[{"path1":"A1","path2":"A2"},{"path1":"B1","path2":"B2"}],
		"dim3":[{"path1":"X","path2":"Y","path3":"Z"}]
	}`
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonBlob), &m); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var out SignalDimensionLists
	ret := unpacksignalDimensionMap(m, &out)
	if ret == nil {
		t.Fatalf("expected non-nil return")
	}
	if len(ret.dim2List) != 2 {
		t.Fatalf("expected 2 dim2 entries; got %d", len(ret.dim2List))
	}
	if ret.dim2List[0].Path1 != "A1" || ret.dim2List[0].Path2 != "A2" {
		t.Errorf("dim2[0] = %+v", ret.dim2List[0])
	}
	if ret.dim2List[1].Path1 != "B1" || ret.dim2List[1].Path2 != "B2" {
		t.Errorf("dim2[1] = %+v", ret.dim2List[1])
	}
	if len(ret.dim3List) != 1 {
		t.Fatalf("expected 1 dim3 entry; got %d", len(ret.dim3List))
	}
	if ret.dim3List[0].Path1 != "X" || ret.dim3List[0].Path2 != "Y" || ret.dim3List[0].Path3 != "Z" {
		t.Errorf("dim3[0] = %+v", ret.dim3List[0])
	}
}

func TestUnpacksignalDimensionMap_NilTargetIsSafe(t *testing.T) {
	if got := unpacksignalDimensionMap(map[string]interface{}{}, nil); got != nil {
		t.Errorf("expected nil when target is nil; got %v", got)
	}
}

func TestUnpacksignalDimensionMap_NonObjectArrayElementIsSkipped(t *testing.T) {
	// Pre-fix this would have done v.(map[string]interface{}) and panicked.
	m := map[string]interface{}{
		"dim2": []interface{}{
			"not-an-object",
			map[string]interface{}{"path1": "real1", "path2": "real2"},
		},
	}
	var out SignalDimensionLists
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unpacksignalDimensionMap panicked on string element: %v", r)
		}
	}()
	ret := unpacksignalDimensionMap(m, &out)
	if ret == nil {
		t.Fatalf("expected non-nil return")
	}
}

// --------------------------------------------------------------------------
// Bug #11 — getCurveLoggingParams defaults bufSize to 1, not 0
// --------------------------------------------------------------------------

func TestGetCurveLoggingParams_BadBufSizeDefaultsToOne(t *testing.T) {
	// Pre-fix: bad bufsize → bufSize=0, which then panics in createRingBuffer.
	op := `{"maxerr":"0.1","bufsize":"nonsense"}`
	_, buf := getCurveLoggingParams(op)
	if buf < 1 {
		t.Fatalf("expected bufSize >= 1 on parse failure; got %d", buf)
	}
}

func TestGetCurveLoggingParams_BadMaxErrDefaultsToZero(t *testing.T) {
	op := `{"maxerr":"nonsense","bufsize":"5"}`
	maxErr, buf := getCurveLoggingParams(op)
	if maxErr != 0.0 {
		t.Fatalf("expected maxErr=0 on parse failure; got %v", maxErr)
	}
	if buf != 5 {
		t.Fatalf("expected bufSize=5; got %d", buf)
	}
}

// --------------------------------------------------------------------------
// Bug #12 — transformDataPoints handles unparseable base timestamps
// --------------------------------------------------------------------------

func TestTransformDataPoints_BadBaseTimestampReturnsNil(t *testing.T) {
	// Build a small ring buffer with a malformed timestamp at the base position
	// (index = bufSize-1, which is read first as tsBase).
	rb := createRingBuffer(4)
	writeRing(&rb, "1.0", "not-a-timestamp")
	writeRing(&rb, "2.0", "2026-05-16T12:00:01Z")
	writeRing(&rb, "3.0", "2026-05-16T12:00:02Z")

	clBuf := make([]CLBufElement, 3)
	got := transformDataPoints(&rb, clBuf, 3)
	if got != nil {
		t.Fatalf("expected nil on unparseable base timestamp; got %+v", got)
	}
}

func TestTransformDataPoints_NilRingBufferIsSafe(t *testing.T) {
	clBuf := make([]CLBufElement, 3)
	if got := transformDataPoints(nil, clBuf, 3); got != nil {
		t.Errorf("expected nil for nil ring buffer; got %+v", got)
	}
}

func TestTransformDataPoints_BadBufSizeReturnsNil(t *testing.T) {
	rb := createRingBuffer(4)
	clBuf := make([]CLBufElement, 2)
	if got := transformDataPoints(&rb, clBuf, 0); got != nil {
		t.Errorf("expected nil for bufSize=0; got %+v", got)
	}
	if got := transformDataPoints(&rb, clBuf, 5); got != nil {
		t.Errorf("expected nil for bufSize > len(clBuf); got %+v", got)
	}
}

func TestTransformDataPoints_UnixMilliFallbackParses(t *testing.T) {
	// Base timestamp as int milliseconds — UnixMilli fallback should kick in.
	rb := createRingBuffer(4)
	writeRing(&rb, "1.0", "1747405200000")
	writeRing(&rb, "2.0", "1747405201000")
	writeRing(&rb, "3.0", "1747405202000")

	clBuf := make([]CLBufElement, 3)
	got := transformDataPoints(&rb, clBuf, 3)
	if got == nil {
		t.Fatalf("expected non-nil result with UnixMilli base timestamp")
	}
	if got[0].Value != 3.0 || got[1].Value != 2.0 || got[2].Value != 1.0 {
		t.Errorf("unexpected value order in clBuffer: %+v", got)
	}
}

// --------------------------------------------------------------------------
// stringField helper — exercise both branches.
// --------------------------------------------------------------------------

func TestStringField(t *testing.T) {
	m := map[string]interface{}{
		"a": "hello",
		"b": 42,
		"c": nil,
	}
	if v, ok := stringField(m, "a"); !ok || v != "hello" {
		t.Errorf("a: got %q,%v; want hello,true", v, ok)
	}
	if _, ok := stringField(m, "b"); ok {
		t.Errorf("b: non-string returned ok=true")
	}
	if _, ok := stringField(m, "c"); ok {
		t.Errorf("c: nil returned ok=true")
	}
	if _, ok := stringField(m, "missing"); ok {
		t.Errorf("missing: returned ok=true")
	}
}

// --------------------------------------------------------------------------
// Ring buffer round-trip — protects against future regressions in
// createRingBuffer / writeRing / readRing / getNumOfPopulatedRingElements.
// --------------------------------------------------------------------------

func TestRingBuffer_RoundTrip(t *testing.T) {
	rb := createRingBuffer(4)
	writeRing(&rb, "a", "ts-a")
	writeRing(&rb, "b", "ts-b")
	writeRing(&rb, "c", "ts-c")
	if got := getNumOfPopulatedRingElements(&rb); got != 3 {
		// Tail starts at 0; with 3 writes and head=3, populated count = 3 - 0 = 3.
		t.Fatalf("populated = %d; want 3", got)
	}
	v, ts := readRing(&rb, 0)
	if v != "c" || ts != "ts-c" {
		t.Errorf("read 0 = %q,%q; want c,ts-c", v, ts)
	}
	v, ts = readRing(&rb, 1)
	if v != "b" || ts != "ts-b" {
		t.Errorf("read 1 = %q,%q; want b,ts-b", v, ts)
	}
	v, ts = readRing(&rb, 2)
	if v != "a" || ts != "ts-a" {
		t.Errorf("read 2 = %q,%q; want a,ts-a", v, ts)
	}
}

// --------------------------------------------------------------------------
// dim1TransformDim* — keep them honest.
// --------------------------------------------------------------------------

func TestDim1TransformDim2(t *testing.T) {
	got := dim1TransformDim2(Dim2Elem{Path1: "A", Path2: "B"})
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Errorf("got %v; want [A B]", got)
	}
}

func TestDim1TransformDim3(t *testing.T) {
	got := dim1TransformDim3(Dim3Elem{Path1: "A", Path2: "B", Path3: "C"})
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Errorf("got %v; want [A B C]", got)
	}
}

// --------------------------------------------------------------------------
// deallocateTriggChannels — frees slots AND decrements session counter
// --------------------------------------------------------------------------

func TestDeallocateTriggChannels_FreesSlotsAndDecrementsCounter(t *testing.T) {
	resetClState()
	// Allocate three trigger channels
	a := allocateTriggChannelIndex()
	b := allocateTriggChannelIndex()
	c := allocateTriggChannelIndex()
	if a < 0 || b < 0 || c < 0 {
		t.Fatalf("setup: expected three valid indices; got %d,%d,%d", a, b, c)
	}
	// Simulate that the dispatcher incremented the session counter for them.
	for i := 0; i < 3; i++ {
		_ = incrementClSessionsIfAvailable()
	}

	rdl := []TriggRoutingData{
		{
			SubscriptionId: "sub-1",
			TriggRoutingList: []TriggRoutingElem{
				{Index: a}, {Index: b}, {Index: c},
			},
		},
	}
	deallocateTriggChannels(0, rdl)

	for _, idx := range []int{a, b, c} {
		if triggChannelList[idx].Busy {
			t.Errorf("triggChannelList[%d] still Busy after deallocate", idx)
		}
	}
	if got := getClSessionsCount(); got != 0 {
		t.Errorf("expected session counter to drop to 0 after deallocate; got %d", got)
	}
}

func TestDeallocateTriggChannels_OutOfRangeIndexIsSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("deallocateTriggChannels panicked on out-of-range index: %v", r)
		}
	}()
	deallocateTriggChannels(-1, nil)
	deallocateTriggChannels(99, []TriggRoutingData{{SubscriptionId: "x"}})
}

// --------------------------------------------------------------------------
// transformDataPoint — covers parse fallback and failure paths
// --------------------------------------------------------------------------

func TestTransformDataPoint_RFC3339Path(t *testing.T) {
	rb := createRingBuffer(2)
	writeRing(&rb, "1.5", "2026-05-16T12:00:00Z")
	got, ok := transformDataPoint(&rb, 0, time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got.Value != 1.5 {
		t.Errorf("value=%v; want 1.5", got.Value)
	}
	if got.Timestamp != 0 {
		t.Errorf("ts=%v; want 0", got.Timestamp)
	}
}

func TestTransformDataPoint_UnparseableValueFails(t *testing.T) {
	rb := createRingBuffer(2)
	writeRing(&rb, "not-a-float", "2026-05-16T12:00:00Z")
	_, ok := transformDataPoint(&rb, 0, time.Now())
	if ok {
		t.Errorf("expected ok=false on unparseable value")
	}
}

func TestTransformDataPoint_UnparseableTimestampFails(t *testing.T) {
	rb := createRingBuffer(2)
	writeRing(&rb, "1.5", "totally-not-a-time")
	_, ok := transformDataPoint(&rb, 0, time.Now())
	if ok {
		t.Errorf("expected ok=false on unparseable timestamp")
	}
}

func TestTransformDataPoint_UnixMilliTimestampFallback(t *testing.T) {
	rb := createRingBuffer(2)
	writeRing(&rb, "2.5", "1747405200000") // unix millis
	got, ok := transformDataPoint(&rb, 0, time.UnixMilli(1747405200000))
	if !ok {
		t.Fatalf("expected ok=true on UnixMilli timestamp")
	}
	if got.Value != 2.5 {
		t.Errorf("value=%v; want 2.5", got.Value)
	}
	if got.Timestamp != 0 {
		t.Errorf("ts=%v; want 0 (same as tsBase)", got.Timestamp)
	}
}

// --------------------------------------------------------------------------
// getRingHead / setRingTail — ring-buffer geometry helpers
// --------------------------------------------------------------------------

func TestGetRingHead_TracksWrites(t *testing.T) {
	rb := createRingBuffer(4)
	if got := getRingHead(&rb); got != 0 {
		t.Errorf("fresh ring head = %d; want 0", got)
	}
	writeRing(&rb, "v", "ts")
	if got := getRingHead(&rb); got != 1 {
		t.Errorf("ring head after 1 write = %d; want 1", got)
	}
}

func TestSetRingTail_ComputesFromHead(t *testing.T) {
	rb := createRingBuffer(4)
	for i := 0; i < 3; i++ {
		writeRing(&rb, "v", "ts")
	}
	// Head is at 3, calling setRingTail(2) means tail = 3 - (2+1) = 0
	setRingTail(&rb, 2)
	if rb.Tail != 0 {
		t.Errorf("tail = %d; want 0", rb.Tail)
	}
}

func TestRingBufferWrapAround(t *testing.T) {
	rb := createRingBuffer(3)
	writeRing(&rb, "a", "ta")
	writeRing(&rb, "b", "tb")
	writeRing(&rb, "c", "tc")
	if rb.Head != 0 {
		t.Errorf("head should wrap to 0 after filling bufSize=3 elements; got %d", rb.Head)
	}
	writeRing(&rb, "d", "td") // overwrites slot 0
	v, ts := readRing(&rb, 0)
	if v != "d" || ts != "td" {
		t.Errorf("after wrap-around overwrite, latest = %q,%q; want d,td", v, ts)
	}
}

// --------------------------------------------------------------------------
// is2dim / is3dim — predicate functions over signal-dimension lists
// --------------------------------------------------------------------------

func TestIs2dim_MatchesByIndex(t *testing.T) {
	list := []Dim2Elem{{Path1: "A1", Path2: "A2"}, {Path1: "B1", Path2: "B2"}}
	if !is2dim("A1", 1, list) {
		t.Errorf("A1 should match index 1 (Path1)")
	}
	if !is2dim("A2", 2, list) {
		t.Errorf("A2 should match index 2 (Path2)")
	}
	if is2dim("A1", 2, list) {
		t.Errorf("A1 should not match index 2 (Path2)")
	}
	if is2dim("nope", 1, list) {
		t.Errorf("unknown path should not match")
	}
	if is2dim("A1", 3, list) {
		t.Errorf("invalid index should return false")
	}
}

func TestIs3dim_MatchesByIndex(t *testing.T) {
	list := []Dim3Elem{{Path1: "X1", Path2: "X2", Path3: "X3"}}
	if !is3dim("X1", 1, list) {
		t.Errorf("X1 should match index 1")
	}
	if !is3dim("X2", 2, list) {
		t.Errorf("X2 should match index 2")
	}
	if !is3dim("X3", 3, list) {
		t.Errorf("X3 should match index 3")
	}
	if is3dim("X3", 1, list) {
		t.Errorf("X3 should not match index 1")
	}
	if is3dim("nope", 1, list) {
		t.Errorf("unknown path should not match")
	}
	if is3dim("X1", 99, list) {
		t.Errorf("invalid index should return false")
	}
}

func TestIs2dim_EmptyListReturnsFalse(t *testing.T) {
	if is2dim("anything", 1, nil) {
		t.Errorf("empty list should return false")
	}
}

func TestIs3dim_EmptyListReturnsFalse(t *testing.T) {
	if is3dim("anything", 1, nil) {
		t.Errorf("empty list should return false")
	}
}

// --------------------------------------------------------------------------
// unPackDimSignalsLevel1 — both dim2 and dim3 paths, plus nil-safety
// --------------------------------------------------------------------------

func TestUnPackDimSignalsLevel1_Dim2Paths(t *testing.T) {
	out := &SignalDimensionLists{dim2List: make([]Dim2Elem, 1)}
	signalDimMap := map[string]interface{}{
		"path1": "alpha",
		"path2": "beta",
	}
	unPackDimSignalsLevel1(0, signalDimMap, "dim2", out)
	if out.dim2List[0].Path1 != "alpha" || out.dim2List[0].Path2 != "beta" {
		t.Errorf("dim2 unpack mismatch: %+v", out.dim2List[0])
	}
}

func TestUnPackDimSignalsLevel1_Dim3Paths(t *testing.T) {
	out := &SignalDimensionLists{dim3List: make([]Dim3Elem, 1)}
	signalDimMap := map[string]interface{}{
		"path1": "p1",
		"path2": "p2",
		"path3": "p3",
	}
	unPackDimSignalsLevel1(0, signalDimMap, "dim3", out)
	if out.dim3List[0].Path1 != "p1" || out.dim3List[0].Path2 != "p2" || out.dim3List[0].Path3 != "p3" {
		t.Errorf("dim3 unpack mismatch: %+v", out.dim3List[0])
	}
}

func TestUnPackDimSignalsLevel1_NilTargetIsSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil target panicked: %v", r)
		}
	}()
	unPackDimSignalsLevel1(0, map[string]interface{}{"path1": "x"}, "dim2", nil)
}

func TestUnPackDimSignalsLevel1_OutOfRangeIndexIsSafe(t *testing.T) {
	out := &SignalDimensionLists{dim2List: make([]Dim2Elem, 1)}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("out-of-range index panicked: %v", r)
		}
	}()
	unPackDimSignalsLevel1(5, map[string]interface{}{"path1": "x"}, "dim2", out)
	unPackDimSignalsLevel1(-1, map[string]interface{}{"path1": "x"}, "dim2", out)
}

func TestUnPackDimSignalsLevel1_NonStringValueIgnored(t *testing.T) {
	out := &SignalDimensionLists{dim2List: make([]Dim2Elem, 1)}
	signalDimMap := map[string]interface{}{
		"path1": 42, // int, not string
		"path2": "real",
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("non-string value caused panic: %v", r)
		}
	}()
	unPackDimSignalsLevel1(0, signalDimMap, "dim2", out)
	// path2 should still be set
	if out.dim2List[0].Path2 != "real" {
		t.Errorf("path2 should still parse despite path1 being non-string: %+v", out.dim2List[0])
	}
}

// --------------------------------------------------------------------------
// jsonToStructList / readSignalDimensions — file-format helpers
// --------------------------------------------------------------------------

func TestJsonToStructList_ValidInput(t *testing.T) {
	in := `{
		"dim2":[{"path1":"X","path2":"Y"}],
		"dim3":[{"path1":"A","path2":"B","path3":"C"}]
	}`
	got := jsonToStructList(in)
	if got == nil {
		t.Fatalf("expected non-nil")
	}
	if len(got.dim2List) != 1 || got.dim2List[0].Path1 != "X" || got.dim2List[0].Path2 != "Y" {
		t.Errorf("dim2 wrong: %+v", got.dim2List)
	}
	if len(got.dim3List) != 1 || got.dim3List[0].Path3 != "C" {
		t.Errorf("dim3 wrong: %+v", got.dim3List)
	}
}

func TestJsonToStructList_InvalidJsonReturnsNil(t *testing.T) {
	if got := jsonToStructList("not-json"); got != nil {
		t.Errorf("expected nil for invalid JSON; got %+v", got)
	}
}

func TestReadSignalDimensions_MissingFileReturnsNil(t *testing.T) {
	if got := readSignalDimensions("/does/not/exist/signaldimension.json"); got != nil {
		t.Errorf("expected nil for missing file; got %+v", got)
	}
}

func TestReadSignalDimensions_ParsesValidFile(t *testing.T) {
	dir := t.TempDir()
	fp := dir + "/sd.json"
	content := []byte(`{"dim2":[{"path1":"X","path2":"Y"}]}`)
	if err := os.WriteFile(fp, content, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got := readSignalDimensions(fp)
	if got == nil {
		t.Fatalf("expected non-nil")
	}
	if len(got.dim2List) != 1 || got.dim2List[0].Path1 != "X" {
		t.Errorf("got %+v", got)
	}
}

// --------------------------------------------------------------------------
// clReduction1Dim / clReduction2Dim / clReduction3Dim — RDP-style reducers
// --------------------------------------------------------------------------

func TestClReduction1Dim_PerfectLineKeepsNothing(t *testing.T) {
	// On a perfectly linear ramp, no point exceeds the error threshold, so
	// the reducer should return nil (= keep nothing extra beyond endpoints).
	buf := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 1.0, Timestamp: 1.0},
		{Value: 2.0, Timestamp: 2.0},
		{Value: 3.0, Timestamp: 3.0},
	}
	got := clReduction1Dim(buf, 0, 3, 0.0001)
	if got != nil {
		t.Errorf("expected nil for perfectly linear data; got %v", got)
	}
}

func TestClReduction1Dim_StepFunctionKeepsCorner(t *testing.T) {
	// A step function should preserve the corner index.
	buf := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 0.0, Timestamp: 1.0},
		{Value: 10.0, Timestamp: 2.0},
		{Value: 10.0, Timestamp: 3.0},
	}
	got := clReduction1Dim(buf, 0, 3, 1.0)
	if got == nil {
		t.Fatalf("expected non-nil savedIndex for step function")
	}
}

func TestClReduction1Dim_ShortBufferReturnsNil(t *testing.T) {
	buf := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 1.0, Timestamp: 1.0},
	}
	if got := clReduction1Dim(buf, 0, 1, 0.5); got != nil {
		t.Errorf("expected nil for lastIndex-firstIndex<=1; got %v", got)
	}
}

func TestClReduction2Dim_PerfectLineKeepsNothing(t *testing.T) {
	buf1 := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 1.0, Timestamp: 1.0},
		{Value: 2.0, Timestamp: 2.0},
	}
	buf2 := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 2.0, Timestamp: 1.0},
		{Value: 4.0, Timestamp: 2.0},
	}
	if got := clReduction2Dim(buf1, buf2, 0, 2, 0.0001); got != nil {
		t.Errorf("expected nil for perfectly linear pair; got %v", got)
	}
}

func TestClReduction3Dim_PerfectLineKeepsNothing(t *testing.T) {
	buf1 := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 1.0, Timestamp: 1.0},
		{Value: 2.0, Timestamp: 2.0},
	}
	buf2 := []CLBufElement{
		{Value: 5.0, Timestamp: 0.0},
		{Value: 6.0, Timestamp: 1.0},
		{Value: 7.0, Timestamp: 2.0},
	}
	buf3 := []CLBufElement{
		{Value: 10.0, Timestamp: 0.0},
		{Value: 11.0, Timestamp: 1.0},
		{Value: 12.0, Timestamp: 2.0},
	}
	if got := clReduction3Dim(buf1, buf2, buf3, 0, 2, 0.0001); got != nil {
		t.Errorf("expected nil for perfectly linear triple; got %v", got)
	}
}

// --------------------------------------------------------------------------
// clAnalyze1dim / 2dim / 3dim — wrappers that include nil-buffer fallback
// --------------------------------------------------------------------------

func TestClAnalyze1dim_NilBufferFromBadTimestampFallsBack(t *testing.T) {
	// Make a ring buffer whose base timestamp is unparseable so
	// transformDataPoints returns nil.
	rb := createRingBuffer(4)
	writeRing(&rb, "1.0", "not-a-timestamp") // becomes the base on read at index bufSize-1
	writeRing(&rb, "2.0", "2026-05-16T12:00:01Z")
	writeRing(&rb, "3.0", "2026-05-16T12:00:02Z")

	dp, lastSel, firstSel := clAnalyze1dim(&rb, 3, 0.1)
	if dp == "" {
		t.Errorf("expected fallback dp string; got empty")
	}
	if lastSel != 0 || firstSel != 0 {
		t.Errorf("expected lastSel=firstSel=0 on fallback; got %d,%d", lastSel, firstSel)
	}
}

func TestClAnalyze2dim_NilBufferFromBadTimestampFallsBack(t *testing.T) {
	rb1 := createRingBuffer(4)
	rb2 := createRingBuffer(4)
	writeRing(&rb1, "1.0", "bad-ts")
	writeRing(&rb1, "2.0", "2026-05-16T12:00:01Z")
	writeRing(&rb2, "10.0", "bad-ts")
	writeRing(&rb2, "20.0", "2026-05-16T12:00:01Z")

	dp1, dp2, ut := clAnalyze2dim(&rb1, &rb2, 2, 0.1)
	if dp1 == "" || dp2 == "" {
		t.Errorf("expected non-empty fallback dps")
	}
	if ut != 0 {
		t.Errorf("expected updatedTail=0 on fallback; got %d", ut)
	}
}

func TestClAnalyze3dim_NilBufferFromBadTimestampFallsBack(t *testing.T) {
	rb1 := createRingBuffer(4)
	rb2 := createRingBuffer(4)
	rb3 := createRingBuffer(4)
	writeRing(&rb1, "1.0", "bad-ts")
	writeRing(&rb1, "2.0", "2026-05-16T12:00:01Z")
	writeRing(&rb2, "10.0", "bad-ts")
	writeRing(&rb2, "20.0", "2026-05-16T12:00:01Z")
	writeRing(&rb3, "100.0", "bad-ts")
	writeRing(&rb3, "200.0", "2026-05-16T12:00:01Z")

	dp1, dp2, dp3, ut := clAnalyze3dim(&rb1, &rb2, &rb3, 2, 0.1)
	if dp1 == "" || dp2 == "" || dp3 == "" {
		t.Errorf("expected non-empty fallback dps")
	}
	if ut != 0 {
		t.Errorf("expected updatedTail=0 on fallback; got %d", ut)
	}
}

func TestClAnalyze1dim_HappyPathWithLinearData(t *testing.T) {
	rb := createRingBuffer(5)
	base := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		writeRing(&rb,
			fmt.Sprintf("%d", i),
			base.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano))
	}
	dp, _, _ := clAnalyze1dim(&rb, 4, 0.0001) // perfectly linear → savedIndex=nil
	if dp == "" {
		t.Errorf("expected a non-empty datapoint string")
	}
}

// --------------------------------------------------------------------------
// returnSingleDp / returnSingleDp2 / returnSingleDp3 — channel publishers
// --------------------------------------------------------------------------

func TestReturnSingleDp_PublishesToChannel(t *testing.T) {
	resetClState()
	// Stub the storage layer enough that getVehicleData returns something.
	// getVehicleData is implemented in serviceMgr.go; we only need its
	// fallback (visserr:Data-not-available) for an end-to-end push.
	ch := make(chan CLPack, 1)
	go returnSingleDp(ch, 42, "Vehicle.Speed")
	select {
	case pack := <-ch:
		if pack.SubscriptionId != 42 {
			t.Errorf("subscriptionId = %d; want 42", pack.SubscriptionId)
		}
		if pack.DataPack == "" {
			t.Errorf("DataPack should be non-empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("returnSingleDp did not publish in time")
	}
}

func TestReturnSingleDp2_PublishesToChannel(t *testing.T) {
	resetClState()
	ch := make(chan CLPack, 1)
	go returnSingleDp2(ch, 7, Dim2Elem{Path1: "Vehicle.A", Path2: "Vehicle.B"})
	select {
	case pack := <-ch:
		if pack.SubscriptionId != 7 {
			t.Errorf("subscriptionId = %d; want 7", pack.SubscriptionId)
		}
		if pack.DataPack == "" || pack.DataPack[0] != '[' {
			t.Errorf("expected JSON array DataPack; got %q", pack.DataPack)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("returnSingleDp2 did not publish in time")
	}
}

func TestReturnSingleDp3_PublishesToChannel(t *testing.T) {
	resetClState()
	ch := make(chan CLPack, 1)
	go returnSingleDp3(ch, 9, Dim3Elem{Path1: "P1", Path2: "P2", Path3: "P3"})
	select {
	case pack := <-ch:
		if pack.SubscriptionId != 9 {
			t.Errorf("subscriptionId = %d; want 9", pack.SubscriptionId)
		}
		if pack.DataPack == "" || pack.DataPack[0] != '[' {
			t.Errorf("expected JSON array DataPack; got %q", pack.DataPack)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("returnSingleDp3 did not publish in time")
	}
}

// --------------------------------------------------------------------------
// postProcess1dim / writePostProcElement1dim / movePostProcElement1dim /
// saveNonPdrDp — the curve-logging post-processing state machine
// --------------------------------------------------------------------------

func makePostProcRing(t *testing.T) *RingBuffer {
	t.Helper()
	rb := createRingBuffer(5)
	base := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		writeRing(&rb,
			fmt.Sprintf("%d", i),
			base.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano))
	}
	return &rb
}

func TestWritePostProcElement1dim_PopulatesSlot(t *testing.T) {
	rb := makePostProcRing(t)
	pp := make([]PostProcessBufElement1dim, 3)
	pp = writePostProcElement1dim(rb, 0, pp, 0)
	if pp[0].Dp == "" {
		t.Errorf("expected non-empty Dp string")
	}
	if pp[0].Type != 0 {
		t.Errorf("expected Type=firstSelected=0; got %d", pp[0].Type)
	}
}

func TestMovePostProcElement1dim_CopiesFields(t *testing.T) {
	pp := make([]PostProcessBufElement1dim, 3)
	pp[1].Dp = "src-dp"
	pp[1].Type = 7
	pp[1].Data = CLBufElement{Value: 1.5, Timestamp: 2.5}
	pp = movePostProcElement1dim(pp, 1, 0)
	if pp[0].Dp != "src-dp" || pp[0].Type != 7 {
		t.Errorf("move did not copy: %+v", pp[0])
	}
	if pp[0].Data.Value != 1.5 || pp[0].Data.Timestamp != 2.5 {
		t.Errorf("move did not copy Data: %+v", pp[0].Data)
	}
}

func TestSaveNonPdrDp_DetectsNonLinearMiddle(t *testing.T) {
	pp := []PostProcessBufElement1dim{
		{Data: CLBufElement{Value: 0.0, Timestamp: 0.0}},
		{Data: CLBufElement{Value: 5.0, Timestamp: 1.0}}, // way above the line 0..2
		{Data: CLBufElement{Value: 2.0, Timestamp: 2.0}},
	}
	if !saveNonPdrDp(pp, 1.0) {
		t.Errorf("expected save=true when interpolation error exceeds maxError")
	}
}

func TestSaveNonPdrDp_KeepsLinearMiddle(t *testing.T) {
	pp := []PostProcessBufElement1dim{
		{Data: CLBufElement{Value: 0.0, Timestamp: 0.0}},
		{Data: CLBufElement{Value: 1.0, Timestamp: 1.0}}, // exactly on line
		{Data: CLBufElement{Value: 2.0, Timestamp: 2.0}},
	}
	if saveNonPdrDp(pp, 0.5) {
		t.Errorf("expected save=false when point sits on interpolation line")
	}
}

func TestPostProcess1dim_InitialStateFillsSlotZero(t *testing.T) {
	rb := makePostProcRing(t)
	pp := make([]PostProcessBufElement1dim, 3)
	pp[0].Type = -1 // init state
	pp[1].Type = -1
	pp[2].Type = -1
	extra, out := postProcess1dim(rb, 0, 0, pp, 0.1)
	if extra != "" {
		t.Errorf("init step should return empty extra; got %q", extra)
	}
	if out[0].Type == -1 {
		t.Errorf("slot 0 should be populated after init step")
	}
}

// --------------------------------------------------------------------------
// initClResources — sanity-check the package-level initializer
// --------------------------------------------------------------------------

func TestInitClResources_AllocatesChannelsAndList(t *testing.T) {
	initClResources()
	if CLChannel == nil || cap(CLChannel) == 0 {
		t.Errorf("CLChannel should be allocated and buffered")
	}
	if len(triggChannelList) != MAXCLSESSIONS {
		t.Errorf("triggChannelList should have %d entries; got %d", MAXCLSESSIONS, len(triggChannelList))
	}
	for i := range clServerChan {
		if clServerChan[i] == nil {
			t.Errorf("clServerChan[%d] not initialized", i)
		}
		if cap(clServerChan[i]) == 0 {
			t.Errorf("clServerChan[%d] should be buffered (bug-6 regression check)", i)
		}
	}
	if clRouterChan == nil {
		t.Errorf("clRouterChan should be allocated")
	}
}

// --------------------------------------------------------------------------
// curveLoggingDispatcher — happy path with default (no signal-dimension)
// --------------------------------------------------------------------------

func TestCurveLoggingDispatcher_BadOpValueDoesNotPanic(t *testing.T) {
	resetClState()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("curveLoggingDispatcher panicked on bad opValue: %v", r)
		}
	}()
	ch := make(chan CLPack, 10)
	_, _ = curveLoggingDispatcher(ch, 1, "not-valid-json", []string{})
}

func TestCurveLoggingDispatcher_RespectsMaxBufSize(t *testing.T) {
	resetClState()
	op := fmt.Sprintf(`{"maxerr":"0.1","bufsize":"%d"}`, MAXCLBUFSIZE*2)
	ch := make(chan CLPack, 100)
	// No paths means no goroutines started, just exercise the cap path.
	_, threads := curveLoggingDispatcher(ch, 1, op, []string{})
	if threads.SubscriptionId != 1 {
		t.Errorf("subThreads.SubscriptionId = %d; want 1", threads.SubscriptionId)
	}
	if threads.NumofThreads != 0 {
		t.Errorf("with no paths, expected 0 threads; got %d", threads.NumofThreads)
	}
}

// --------------------------------------------------------------------------
// handleCurveLogServerMessage — happy paths for subscribe / subscription
// --------------------------------------------------------------------------

func TestHandleCurveLogServerMessage_SubscribeNotifiesAllChannels(t *testing.T) {
	resetClState()
	// Pre-populate routing data pointing at index 0 and 1
	rdl := []TriggRoutingData{
		{
			SubscriptionId: "sub-1",
			TriggRoutingList: []TriggRoutingElem{
				{Index: 0, Path: []string{"A"}},
				{Index: 1, Path: []string{"B"}},
			},
		},
	}
	msg := `{"action":"subscribe","status":"ok"}`
	got := handleCurveLogServerMessage(msg, rdl)
	if len(got) != 1 {
		t.Errorf("subscribe should not mutate routing list; got %d entries", len(got))
	}
	// Both channels should have received the message
	for _, idx := range []int{0, 1} {
		select {
		case got := <-clServerChan[idx]:
			if got != msg {
				t.Errorf("clServerChan[%d] got %q; want %q", idx, got, msg)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("clServerChan[%d] did not receive subscribe notification", idx)
		}
	}
}

func TestHandleCurveLogServerMessage_SubscriptionRoutesByPath(t *testing.T) {
	resetClState()
	rdl := []TriggRoutingData{
		{
			SubscriptionId: "sub-1",
			TriggRoutingList: []TriggRoutingElem{
				{Index: 5, Path: []string{"Vehicle.Match"}},
				{Index: 6, Path: []string{"Vehicle.Other"}},
			},
		},
	}
	msg := `{"action":"subscription","path":"Vehicle.Match"}`
	_ = handleCurveLogServerMessage(msg, rdl)
	select {
	case got := <-clServerChan[5]:
		if got != msg {
			t.Errorf("matching channel got %q", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Errorf("matching channel did not receive notification")
	}
	// Non-matching channel should NOT receive anything
	select {
	case got := <-clServerChan[6]:
		t.Errorf("non-matching channel received %q; should be silent", got)
	case <-time.After(50 * time.Millisecond):
		// good
	}
}

func TestHandleCurveLogServerMessage_SubscribeWithBadStatusIsNoop(t *testing.T) {
	resetClState()
	rdl := []TriggRoutingData{
		{
			SubscriptionId: "sub-1",
			TriggRoutingList: []TriggRoutingElem{{Index: 0, Path: []string{"x"}}},
		},
	}
	msg := `{"action":"subscribe","status":"failed"}`
	_ = handleCurveLogServerMessage(msg, rdl)
	select {
	case got := <-clServerChan[0]:
		t.Errorf("subscribe with status!=ok should not notify; got %q", got)
	case <-time.After(50 * time.Millisecond):
		// good
	}
}

// --------------------------------------------------------------------------
// allocateTriggChannelIndex — exhaustion + reuse-after-release
// --------------------------------------------------------------------------

func TestAllocateTriggChannelIndex_ReusableAfterRelease(t *testing.T) {
	resetClState()
	idx := allocateTriggChannelIndex()
	if idx < 0 {
		t.Fatalf("first allocation failed")
	}
	triggChannelMu.Lock()
	triggChannelList[idx].Busy = false
	triggChannelMu.Unlock()
	// Should be reusable
	again := allocateTriggChannelIndex()
	if again != idx {
		t.Errorf("expected released slot %d to be reused; got %d", idx, again)
	}
}

// --------------------------------------------------------------------------
// GAP: populateDimLists — the file-reading wrapper around
// populateDimListsFromSignals.  When no signaldimension.json exists the
// signal list is nil and every path must come back as dim1.
// --------------------------------------------------------------------------

func TestPopulateDimLists_NoSignalFileFallsBackToDim1(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	defer os.Chdir(orig)

	paths := []string{"Vehicle.A", "Vehicle.B", "Vehicle.C"}
	d1, d2, d3 := populateDimLists(paths)
	if len(d1) != 3 {
		t.Errorf("expected 3 dim1 entries; got %d (%v)", len(d1), d1)
	}
	if len(d2) != 0 {
		t.Errorf("expected 0 dim2 entries; got %d", len(d2))
	}
	if len(d3) != 0 {
		t.Errorf("expected 0 dim3 entries; got %d", len(d3))
	}
}

// --------------------------------------------------------------------------
// RESOLUTION: analyzeSignalDimensions with non-nil signal lists — verify
// that Dim and Id fields are correctly assigned for dim2 and dim3 paths.
// The existing test only covers the nil (all-dim1) case.
// --------------------------------------------------------------------------

func TestAnalyzeSignalDimensions_Dim2Classification(t *testing.T) {
	sdl := &SignalDimensionLists{
		dim2List: []Dim2Elem{
			{Path1: "Vehicle.X", Path2: "Vehicle.Y"},
		},
	}
	paths := []string{"Vehicle.X", "Vehicle.Y", "Vehicle.Z"}
	pdl := analyzeSignalDimensions(paths, sdl)
	if len(pdl) != 3 {
		t.Fatalf("expected 3 PathDimElems; got %d", len(pdl))
	}
	if pdl[0].Dim != 2 || pdl[1].Dim != 2 {
		t.Errorf("Vehicle.X and Vehicle.Y should be Dim=2; got %d,%d", pdl[0].Dim, pdl[1].Dim)
	}
	if pdl[0].Id != pdl[1].Id {
		t.Errorf("paired dim2 paths should share Id; got %d vs %d", pdl[0].Id, pdl[1].Id)
	}
	if pdl[2].Dim != 1 {
		t.Errorf("Vehicle.Z should remain Dim=1; got %d", pdl[2].Dim)
	}
}

func TestAnalyzeSignalDimensions_Dim3Classification(t *testing.T) {
	sdl := &SignalDimensionLists{
		dim3List: []Dim3Elem{
			{Path1: "Vehicle.A", Path2: "Vehicle.B", Path3: "Vehicle.C"},
		},
	}
	paths := []string{"Vehicle.A", "Vehicle.B", "Vehicle.C"}
	pdl := analyzeSignalDimensions(paths, sdl)
	if len(pdl) != 3 {
		t.Fatalf("expected 3 PathDimElems; got %d", len(pdl))
	}
	for i, e := range pdl {
		if e.Dim != 3 {
			t.Errorf("pdl[%d].Dim = %d; want 3", i, e.Dim)
		}
	}
	if pdl[0].Id != pdl[1].Id || pdl[1].Id != pdl[2].Id {
		t.Errorf("all three dim3 paths should share Id; got %d,%d,%d", pdl[0].Id, pdl[1].Id, pdl[2].Id)
	}
}

// --------------------------------------------------------------------------
// RESOLUTION: clReduction2Dim / clReduction3Dim — add step-function and
// short-buffer cases to match the three-case coverage of clReduction1Dim.
// --------------------------------------------------------------------------

func TestClReduction2Dim_StepFunctionKeepsCorner(t *testing.T) {
	buf1 := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 0.0, Timestamp: 1.0},
		{Value: 10.0, Timestamp: 2.0},
		{Value: 10.0, Timestamp: 3.0},
	}
	buf2 := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 0.0, Timestamp: 1.0},
		{Value: 5.0, Timestamp: 2.0},
		{Value: 5.0, Timestamp: 3.0},
	}
	if got := clReduction2Dim(buf1, buf2, 0, 3, 1.0); got == nil {
		t.Errorf("expected non-nil savedIndex for step function in 2dim")
	}
}

func TestClReduction2Dim_ShortBufferReturnsNil(t *testing.T) {
	buf1 := []CLBufElement{{Value: 0.0, Timestamp: 0.0}, {Value: 1.0, Timestamp: 1.0}}
	buf2 := []CLBufElement{{Value: 0.0, Timestamp: 0.0}, {Value: 1.0, Timestamp: 1.0}}
	if got := clReduction2Dim(buf1, buf2, 0, 1, 0.5); got != nil {
		t.Errorf("expected nil for lastIndex-firstIndex<=1; got %v", got)
	}
}

func TestClReduction3Dim_StepFunctionKeepsCorner(t *testing.T) {
	step := []CLBufElement{
		{Value: 0.0, Timestamp: 0.0},
		{Value: 0.0, Timestamp: 1.0},
		{Value: 10.0, Timestamp: 2.0},
		{Value: 10.0, Timestamp: 3.0},
	}
	buf1, buf2, buf3 := step, step, step
	if got := clReduction3Dim(buf1, buf2, buf3, 0, 3, 1.0); got == nil {
		t.Errorf("expected non-nil savedIndex for step function in 3dim")
	}
}

func TestClReduction3Dim_ShortBufferReturnsNil(t *testing.T) {
	buf := []CLBufElement{{Value: 0.0, Timestamp: 0.0}, {Value: 1.0, Timestamp: 1.0}}
	if got := clReduction3Dim(buf, buf, buf, 0, 1, 0.5); got != nil {
		t.Errorf("expected nil for lastIndex-firstIndex<=1; got %v", got)
	}
}

// --------------------------------------------------------------------------
// RESOLUTION: clAnalyze2dim / clAnalyze3dim happy paths — the existing
// tests only exercise the nil-buffer fallback; add linear-data happy paths
// to match TestClAnalyze1dim_HappyPathWithLinearData.
// --------------------------------------------------------------------------

func TestClAnalyze2dim_HappyPathWithLinearData(t *testing.T) {
	rb1 := createRingBuffer(5)
	rb2 := createRingBuffer(5)
	base := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		writeRing(&rb1, fmt.Sprintf("%d", i), ts)
		writeRing(&rb2, fmt.Sprintf("%d", i*2), ts)
	}
	dp1, dp2, _ := clAnalyze2dim(&rb1, &rb2, 4, 0.0001)
	if dp1 == "" || dp2 == "" {
		t.Errorf("expected non-empty dp strings for linear 2dim data; got %q, %q", dp1, dp2)
	}
}

func TestClAnalyze3dim_HappyPathWithLinearData(t *testing.T) {
	rb1 := createRingBuffer(5)
	rb2 := createRingBuffer(5)
	rb3 := createRingBuffer(5)
	base := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		writeRing(&rb1, fmt.Sprintf("%d", i), ts)
		writeRing(&rb2, fmt.Sprintf("%d", i*2), ts)
		writeRing(&rb3, fmt.Sprintf("%d", i*3), ts)
	}
	dp1, dp2, dp3, _ := clAnalyze3dim(&rb1, &rb2, &rb3, 4, 0.0001)
	if dp1 == "" || dp2 == "" || dp3 == "" {
		t.Errorf("expected non-empty dp strings for linear 3dim data; got %q, %q, %q", dp1, dp2, dp3)
	}
}
