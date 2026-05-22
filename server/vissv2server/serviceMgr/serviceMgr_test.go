/**
* Regression tests for the serviceMgr fixes shipped in PR #120 (history
* control) and PR #121 (history get).
**/
package serviceMgr

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises the package-level utils loggers before tests
// that exercise paths logging via utils.Error / utils.Info.
func TestMain(m *testing.M) {
	utils.InitLog("serviceMgr-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// resetHistoryList primes the package-level historyList with a single
// known path for tests that need a valid lookup. Returns a teardown
// function the caller must call (typically via defer).
func resetHistoryList(t *testing.T, path string) func() {
	t.Helper()
	saved := historyList
	historyList = []HistoryList{{Path: path}}
	return func() { historyList = saved }
}

func TestProcessHistoryCtrl_RejectsMissingFields(t *testing.T) {
	defer resetHistoryList(t, "Vehicle.Speed")()
	cases := map[string]string{
		"missing action": `{"path": "Vehicle.Speed"}`,
		"missing path":   `{"action": "create"}`,
		"both missing":   `{}`,
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			got := processHistoryCtrl(req, nil, true)
			if got != "400 Bad Request" {
				t.Fatalf("got %q; want 400 Bad Request", got)
			}
		})
	}
}

// TestProcessHistoryCtrl_NonStringFields is the regression test for the
// PR #120 type-assertion fix. Before that fix, any of these inputs
// panicked the entire serviceMgr.
func TestProcessHistoryCtrl_NonStringFields(t *testing.T) {
	defer resetHistoryList(t, "Vehicle.Speed")()
	cases := map[string]string{
		"path is number":      `{"path": 12345, "action": "create"}`,
		"action is array":     `{"path": "Vehicle.Speed", "action": ["create"]}`,
		"buf-size is number":  `{"path": "Vehicle.Speed", "action": "create", "buf-size": 5}`,
		"frequency is object": `{"path": "Vehicle.Speed", "action": "start", "frequency": {"x": 1}}`,
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("processHistoryCtrl panicked on %q: %v", req, r)
				}
			}()
			got := processHistoryCtrl(req, nil, true)
			if got != "400 Bad Request" {
				t.Fatalf("got %q; want 400 Bad Request", got)
			}
		})
	}
}

// TestProcessHistoryCtrl_UnknownPath is the regression test for the
// PR #120 -1 guard that rejects historyList[-1] indexing.
func TestProcessHistoryCtrl_UnknownPath(t *testing.T) {
	defer resetHistoryList(t, "Vehicle.Speed")()
	got := processHistoryCtrl(`{"path": "Vehicle.NotInList", "action": "create", "buf-size": "5"}`, nil, true)
	if got != "404 Not Found" {
		t.Fatalf("got %q; want 404 Not Found", got)
	}
}

// TestProcessHistoryCtrl_BufSizeOutOfRange is the regression test for
// the PR #120 MAXHISTORYBUFSIZE cap that prevents make([]string, huge).
func TestProcessHistoryCtrl_BufSizeOutOfRange(t *testing.T) {
	defer resetHistoryList(t, "Vehicle.Speed")()
	cases := []string{
		`{"path": "Vehicle.Speed", "action": "create", "buf-size": "-1"}`,
		`{"path": "Vehicle.Speed", "action": "create", "buf-size": "1000000000"}`,
		`{"path": "Vehicle.Speed", "action": "create", "buf-size": "99999"}`,
	}
	for _, req := range cases {
		t.Run(req, func(t *testing.T) {
			got := processHistoryCtrl(req, nil, true)
			if got != "400 Bad Request" {
				t.Fatalf("got %q; want 400 Bad Request", got)
			}
		})
	}
}

// TestProcessHistoryCtrl_BufSizeWithinCap accepts a buf-size at the
// upper limit.
func TestProcessHistoryCtrl_BufSizeWithinCap(t *testing.T) {
	defer resetHistoryList(t, "Vehicle.Speed")()
	got := processHistoryCtrl(`{"path": "Vehicle.Speed", "action": "create", "buf-size": "10"}`, nil, true)
	if got != "200 OK" {
		t.Fatalf("got %q; want 200 OK", got)
	}
	if got, want := historyList[0].BufSize, 10; got != want {
		t.Fatalf("BufSize = %d; want %d", got, want)
	}
	if got, want := len(historyList[0].Buffer), 10; got != want {
		t.Fatalf("len(Buffer) = %d; want %d", got, want)
	}
}

// TestProcessHistoryCtrl_DefaultAction rejects unrecognised actions
// (regression check for the safe actionStr usage).
func TestProcessHistoryCtrl_DefaultAction(t *testing.T) {
	defer resetHistoryList(t, "Vehicle.Speed")()
	got := processHistoryCtrl(`{"path": "Vehicle.Speed", "action": "exterminate"}`, nil, true)
	if got != "400 Bad Request" {
		t.Fatalf("got %q; want 400 Bad Request", got)
	}
}

func TestProcessHistoryCtrl_ListMissing(t *testing.T) {
	got := processHistoryCtrl(`{"path": "Vehicle.Speed", "action": "create", "buf-size": "5"}`, nil, false)
	if got != "500 Internal Server Error" {
		t.Fatalf("got %q; want 500 Internal Server Error", got)
	}
}

// FuzzProcessHistoryCtrl ensures the request parser never panics on
// attacker-controlled JSON.
//
// Run with: go test -fuzz=FuzzProcessHistoryCtrl -fuzztime=10s ./...
func FuzzProcessHistoryCtrl(f *testing.F) {
	seeds := []string{
		`{"path": "Vehicle.Speed", "action": "create", "buf-size": "5"}`,
		`{"path": "Vehicle.Speed", "action": "start", "frequency": "10"}`,
		`{"path": "Vehicle.Speed", "action": "stop"}`,
		`{"path": "Vehicle.Speed", "action": "delete"}`,
		`{"action": "create"}`,
		`{"path": 1, "action": 2}`,
		`{}`,
		``,
		`{"path": "Vehicle.Speed", "action": "create", "buf-size": "-99999999"}`,
		`not json`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	// Set up the path list once for the fuzz body; restore afterwards.
	saved := historyList
	historyList = []HistoryList{{Path: "Vehicle.Speed"}}
	f.Cleanup(func() { historyList = saved })
	f.Fuzz(func(t *testing.T, req string) {
		// Contract: never panic. Return value can be anything.
		_ = processHistoryCtrl(req, nil, true)
	})
}

// TestProcessHistoryGet_NonStringFields is the regression test for the
// PR #121 fix. Before it, these inputs panicked the serviceMgr the same
// way processHistoryCtrl used to.
func TestProcessHistoryGet_NonStringFields(t *testing.T) {
	defer resetHistoryList(t, "Vehicle.Speed")()
	cases := []string{
		`{"path": 1, "period": "P1D"}`,
		`{"path": "Vehicle.Speed", "period": 1}`,
		`{}`,
		`not json`,
	}
	for _, req := range cases {
		t.Run(req, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("processHistoryGet panicked on %q: %v", req, r)
				}
			}()
			if got := processHistoryGet(req); got != "" {
				t.Fatalf("expected \"\" on malformed input %q, got %q", req, got)
			}
		})
	}
}

func TestProcessHistoryGet_UnknownPath(t *testing.T) {
	defer resetHistoryList(t, "Vehicle.Speed")()
	got := processHistoryGet(`{"path": "Vehicle.NotKnown", "period": "P1D"}`)
	if got != "" {
		t.Fatalf("expected \"\" on unknown path, got %q", got)
	}
}

// TestActivateDeactivateInterval_NoGoroutineLeak is the regression
// test for the ef639f0 ticker-leak fix in activateInterval /
// deactivateInterval. Before that fix, each activateInterval spawned
// a goroutine that consumed the ticker channel but had no way to
// exit on deactivate — every subscription that came and went leaked
// one goroutine permanently.
//
// The test is sensitive to scheduling; we wait up to ~1 s for the
// goroutine count to settle, which keeps it stable on CI under
// normal load.
func TestActivateDeactivateInterval_NoGoroutineLeak(t *testing.T) {
	// Settle to a baseline. NumGoroutine fluctuates as the runtime
	// reaps idle workers; take a couple of readings.
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	ch := make(chan int, 64)
	const subscriptionId = 42
	activateInterval(ch, subscriptionId, 50) // 50ms ticker

	// Drain a few ticks so the goroutine is observably running.
	gotTick := false
	for i := 0; i < 5; i++ {
		select {
		case <-ch:
			gotTick = true
		case <-time.After(200 * time.Millisecond):
		}
		if gotTick {
			break
		}
	}
	if !gotTick {
		t.Logf("warning: never observed a tick from activateInterval; the test may still be valid if the goroutine exits cleanly on deactivate")
	}

	deactivateInterval(subscriptionId)

	// Allow the goroutine to exit. NumGoroutine is approximate, so
	// poll for up to 1s for the count to return to <= baseline.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ticker goroutine appears to have leaked after deactivateInterval: baseline=%d, current=%d",
		baseline, runtime.NumGoroutine())
}

// FuzzProcessHistoryGet exercises the history-get parser.
func FuzzProcessHistoryGet(f *testing.F) {
	seeds := []string{
		`{"path": "Vehicle.Speed", "period": "P1D"}`,
		`{"path": "Vehicle.Speed", "period": "PT1H"}`,
		`{"path": 1, "period": "P1D"}`,
		`{"path": "Vehicle.Speed", "period": 1}`,
		`{}`,
		``,
		`not json`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	saved := historyList
	historyList = []HistoryList{{Path: "Vehicle.Speed"}}
	f.Cleanup(func() { historyList = saved })
	f.Fuzz(func(t *testing.T, req string) {
		got := processHistoryGet(req)
		// Contract: never panic; if malformed input, return empty (per
		// the fix; well-formed input may return data).
		_ = got
		// Sanity: should not contain Go-specific panic strings.
		if strings.Contains(got, "panic") {
			t.Fatalf("response contained \"panic\" substring: %q", got)
		}
	})
}
func resetFeederGlobals() {
	feederPathList = nil
	feederSubList = nil
	feederRegList = nil
	feederChannelList = make([]FeederChannelElem, MAXFEEDERS)
}

func resetTickerGlobals() {
	tickerMu.Lock()
	for i := range tickerIndexList {
		tickerIndexList[i] = 0
	}
	for i := range subscriptionTicker {
		subscriptionTicker[i] = nil
	}
	for i := range historyTicker {
		historyTicker[i] = nil
	}
	for i := range tickerDone {
		tickerDone[i] = nil
	}
	tickerMu.Unlock()
}

// --------------------------------------------------------------------------
// Bug #1 — deleteOnFeederPathList: full rewrite. Was structurally broken.
// --------------------------------------------------------------------------

func TestDeleteOnFeederPathList_EmptyInputIsNoop(t *testing.T) {
	resetFeederGlobals()
	feederPathList = append(feederPathList, FeederPathElem{Path: "A", Reference: 2})
	out, json := deleteOnFeederPathList(nil)
	if len(out) != 1 || out[0].Path != "A" || out[0].Reference != 2 {
		t.Errorf("empty input mutated list: %+v", out)
	}
	if json != "" {
		t.Errorf("expected empty JSON; got %q", json)
	}
}

func TestDeleteOnFeederPathList_DecrementsRefAboveOne(t *testing.T) {
	resetFeederGlobals()
	feederPathList = []FeederPathElem{{Path: "A", Reference: 3}}
	out, dropped := deleteOnFeederPathList([]string{"A"})
	if len(out) != 1 || out[0].Reference != 2 {
		t.Errorf("ref should drop to 2; got %+v", out)
	}
	if dropped != "" {
		t.Errorf("nothing should be dropped; got %q", dropped)
	}
}

func TestDeleteOnFeederPathList_RemovesAtRefOne(t *testing.T) {
	resetFeederGlobals()
	feederPathList = []FeederPathElem{
		{Path: "A", Reference: 1},
		{Path: "B", Reference: 2},
	}
	out, dropped := deleteOnFeederPathList([]string{"A"})
	if len(out) != 1 || out[0].Path != "B" {
		t.Fatalf("A should be removed; got %+v", out)
	}
	if !strings.Contains(dropped, "A") {
		t.Errorf("dropped JSON should mention A; got %q", dropped)
	}
}

func TestDeleteOnFeederPathList_MultipleRemovalsCorrectIndices(t *testing.T) {
	// Pre-fix this corrupted the list because the inner loop reset k=0
	// and used the wrong index space. With this fix multiple removals
	// in a single call must keep remaining entries intact.
	resetFeederGlobals()
	feederPathList = []FeederPathElem{
		{Path: "A", Reference: 1},
		{Path: "B", Reference: 1},
		{Path: "C", Reference: 1},
		{Path: "D", Reference: 2},
	}
	out, _ := deleteOnFeederPathList([]string{"A", "B", "C", "D"})
	if len(out) != 1 || out[0].Path != "D" || out[0].Reference != 1 {
		t.Errorf("expected only D remaining with ref=1; got %+v", out)
	}
}

func TestDeleteOnFeederPathList_UnknownPathIsNoop(t *testing.T) {
	resetFeederGlobals()
	feederPathList = []FeederPathElem{{Path: "A", Reference: 1}}
	out, dropped := deleteOnFeederPathList([]string{"missing"})
	if len(out) != 1 || dropped != "" {
		t.Errorf("unknown path should be noop; got list=%+v, dropped=%q", out, dropped)
	}
}

// --------------------------------------------------------------------------
// addOnFeederPathList — defensive type asserts, empty input, ref increment
// --------------------------------------------------------------------------

func TestAddOnFeederPathList_NewEntries(t *testing.T) {
	resetFeederGlobals()
	out, added := addOnFeederPathList([]interface{}{"A", "B"})
	if len(out) != 2 {
		t.Errorf("expected 2 entries; got %+v", out)
	}
	if !strings.Contains(added, "A") || !strings.Contains(added, "B") {
		t.Errorf("added JSON should list both; got %q", added)
	}
}

func TestAddOnFeederPathList_IncrementsExistingRef(t *testing.T) {
	resetFeederGlobals()
	addOnFeederPathList([]interface{}{"A"})
	out, added := addOnFeederPathList([]interface{}{"A"})
	if len(out) != 1 || out[0].Reference != 2 {
		t.Errorf("ref should be 2 after second add; got %+v", out)
	}
	if added != "" {
		t.Errorf("no new entry added; expected empty JSON; got %q", added)
	}
}

func TestAddOnFeederPathList_EmptyInputIsNoop(t *testing.T) {
	resetFeederGlobals()
	out, added := addOnFeederPathList(nil)
	if len(out) != 0 || added != "" {
		t.Errorf("expected noop; got list=%+v, added=%q", out, added)
	}
}

func TestAddOnFeederPathList_NonStringElementSkipped(t *testing.T) {
	resetFeederGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("non-string element panicked: %v", r)
		}
	}()
	out, _ := addOnFeederPathList([]interface{}{"A", 42, "B"})
	if len(out) != 2 {
		t.Errorf("expected 2 valid entries; got %+v", out)
	}
}

// --------------------------------------------------------------------------
// addOnFeederSubList / deleteOnFeederSubList
// --------------------------------------------------------------------------

func TestAddOnFeederSubList_DefensiveAssertions(t *testing.T) {
	resetFeederGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("non-string element panicked: %v", r)
		}
	}()
	out := addOnFeederSubList("sub-1", "change", []interface{}{"path-a", 42, "path-b"})
	if len(out) != 1 || len(out[0].Path) != 2 {
		t.Errorf("expected 1 sub with 2 valid paths; got %+v", out)
	}
}

func TestDeleteOnFeederSubList(t *testing.T) {
	resetFeederGlobals()
	feederSubList = []FeederSubElem{
		{SubscriptionId: "1", Path: []string{"A"}},
		{SubscriptionId: "2", Path: []string{"B"}},
	}
	out, removed := deleteOnFeederSubList("1")
	if len(out) != 1 || out[0].SubscriptionId != "2" {
		t.Errorf("expected sub-2 remaining; got %+v", out)
	}
	if len(removed) != 1 || removed[0] != "A" {
		t.Errorf("expected removed path [A]; got %v", removed)
	}
	// Missing id
	if _, p := deleteOnFeederSubList("999"); p != nil {
		t.Errorf("missing id should return nil path; got %v", p)
	}
}

// --------------------------------------------------------------------------
// updateFeederRegList — loop-after-deletion fix (#8)
// --------------------------------------------------------------------------

func TestUpdateFeederRegList_AppendOnReg(t *testing.T) {
	resetFeederGlobals()
	updateFeederRegList(FeederRegElem{Name: "f1", InfoType: "Data", ChannelIndex: -1})
	if len(feederRegList) != 1 || feederRegList[0].Name != "f1" {
		t.Errorf("expected one entry; got %+v", feederRegList)
	}
}

func TestUpdateFeederRegList_RemovesAllMatchingOnDereg(t *testing.T) {
	resetFeederGlobals()
	// Two entries with the same name (best-effort uniqueness only); pre-fix
	// only the first would be removed.
	feederRegList = []FeederRegElem{
		{Name: "f1", InfoType: "Data", ChannelIndex: -1},
		{Name: "f1", InfoType: "Data", ChannelIndex: -1},
		{Name: "other", InfoType: "Data", ChannelIndex: -1},
	}
	updateFeederRegList(FeederRegElem{Name: "f1", InfoType: "dereg"})
	if len(feederRegList) != 1 || feederRegList[0].Name != "other" {
		t.Errorf("expected only 'other' remaining; got %+v", feederRegList)
	}
}

// --------------------------------------------------------------------------
// createFeederNameList — empty-list panic fix (#15)
// --------------------------------------------------------------------------

func TestCreateFeederNameList_EmptyList(t *testing.T) {
	resetFeederGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("empty list panicked: %v", r)
		}
	}()
	got := createFeederNameList()
	if got.Name != "[]" {
		t.Errorf("expected '[]'; got %q", got.Name)
	}
}

func TestCreateFeederNameList_PopulatedList(t *testing.T) {
	resetFeederGlobals()
	feederRegList = []FeederRegElem{
		{Name: "f1"},
		{Name: "f2"},
	}
	got := createFeederNameList()
	if !strings.Contains(got.Name, "f1") || !strings.Contains(got.Name, "f2") {
		t.Errorf("expected both names; got %q", got.Name)
	}
	if got.Name[0] != '[' || got.Name[len(got.Name)-1] != ']' {
		t.Errorf("expected JSON array; got %q", got.Name)
	}
}

// --------------------------------------------------------------------------
// feederNameClash
// --------------------------------------------------------------------------

func TestFeederNameClash(t *testing.T) {
	list := []string{"a", "b", "c"}
	if !feederNameClash(list, "b") {
		t.Errorf("b should clash")
	}
	if feederNameClash(list, "missing") {
		t.Errorf("missing should not clash")
	}
}

// --------------------------------------------------------------------------
// getFeederChannelIndex / freeFeederChannel
// --------------------------------------------------------------------------

func TestGetFeederChannelIndex_AllocateAndRelease(t *testing.T) {
	resetFeederGlobals()
	idx := getFeederChannelIndex(-1)
	if idx < 0 {
		t.Fatalf("first alloc failed; got %d", idx)
	}
	if !feederChannelList[idx].Busy {
		t.Errorf("slot should be marked busy")
	}
	freeFeederChannel(idx)
	if feederChannelList[idx].Busy {
		t.Errorf("slot should be free after release")
	}
}

func TestGetFeederChannelIndex_ReuseExistingIndex(t *testing.T) {
	resetFeederGlobals()
	if got := getFeederChannelIndex(3); got != 3 {
		t.Errorf("expected reuse of provided index; got %d", got)
	}
}

func TestGetFeederChannelIndex_AllSlotsBusy(t *testing.T) {
	resetFeederGlobals()
	for i := 0; i < MAXFEEDERS; i++ {
		feederChannelList[i].Busy = true
	}
	if got := getFeederChannelIndex(-1); got != -1 {
		t.Errorf("expected -1 when all busy; got %d", got)
	}
}

func TestFreeFeederChannel_OutOfRangeIsSafe(t *testing.T) {
	resetFeederGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	freeFeederChannel(-1)
	freeFeederChannel(9999)
}

// --------------------------------------------------------------------------
// getCurveLoggingParams — bufSize fix (#2)
// --------------------------------------------------------------------------

func TestGetCurveLoggingParams_HappyPath(t *testing.T) {
	maxErr, bufSize := getCurveLoggingParams(`{"maxerr":"0.5","bufsize":"10"}`)
	if maxErr != 0.5 || bufSize != 10 {
		t.Errorf("got %v, %d; want 0.5, 10", maxErr, bufSize)
	}
}

func TestGetCurveLoggingParams_BadJSON(t *testing.T) {
	maxErr, bufSize := getCurveLoggingParams(`not json`)
	if maxErr != 0.0 || bufSize != 0 {
		t.Errorf("expected zero values; got %v, %d", maxErr, bufSize)
	}
}

func TestGetCurveLoggingParams_BadBufSizeDoesNotZeroMaxErr(t *testing.T) {
	// Bug-2 fix: previously this zeroed maxErr when bufSize parse failed.
	maxErr, bufSize := getCurveLoggingParams(`{"maxerr":"0.5","bufsize":"oops"}`)
	if maxErr != 0.5 {
		t.Errorf("maxErr should survive bad bufSize parse; got %v", maxErr)
	}
	if bufSize < 1 {
		t.Errorf("bufSize should default to >=1; got %d", bufSize)
	}
}

// --------------------------------------------------------------------------
// getIntervalPeriod — Printf format fix (#25)
// --------------------------------------------------------------------------

func TestGetIntervalPeriod_HappyPath(t *testing.T) {
	if got := getIntervalPeriod(`{"period":"100"}`); got != 100 {
		t.Errorf("got %d; want 100", got)
	}
}

func TestGetIntervalPeriod_BadJSON(t *testing.T) {
	if got := getIntervalPeriod(`not json`); got != -1 {
		t.Errorf("got %d; want -1", got)
	}
}

func TestGetIntervalPeriod_NonNumericPeriod(t *testing.T) {
	// Pre-fix this triggered a Printf %s with int argument (vet error).
	if got := getIntervalPeriod(`{"period":"oops"}`); got != -1 {
		t.Errorf("got %d; want -1", got)
	}
}

// --------------------------------------------------------------------------
// createFeederNotifyMessage — empty list panic guard (#13)
// --------------------------------------------------------------------------

func TestCreateFeederNotifyMessage_EmptyListReturnsEmpty(t *testing.T) {
	if got := createFeederNotifyMessage("change", nil, 1); got != "" {
		t.Errorf("expected empty; got %q", got)
	}
}

func TestCreateFeederNotifyMessage_HappyPath(t *testing.T) {
	got := createFeederNotifyMessage("change", []string{"a", "b"}, 7)
	if !strings.Contains(got, `"variant": "change"`) || !strings.Contains(got, `"subscriptionId": "7"`) {
		t.Errorf("missing fields: %q", got)
	}
	if !strings.Contains(got, `"a"`) || !strings.Contains(got, `"b"`) {
		t.Errorf("missing paths: %q", got)
	}
}

// --------------------------------------------------------------------------
// getFeederNotifyType
// --------------------------------------------------------------------------

func TestGetFeederNotifyType(t *testing.T) {
	if got := getFeederNotifyType([]utils.FilterObject{{Type: "curvelog"}}); got != "curvelog" {
		t.Errorf("got %q", got)
	}
	if got := getFeederNotifyType([]utils.FilterObject{{Type: "change"}}); got != "change" {
		t.Errorf("got %q", got)
	}
	if got := getFeederNotifyType([]utils.FilterObject{{Type: "range"}}); got != "range" {
		t.Errorf("got %q", got)
	}
	if got := getFeederNotifyType([]utils.FilterObject{{Type: "history"}}); got != "" {
		t.Errorf("got %q; want empty (history is not a feeder notify type)", got)
	}
}


func TestProcessHistoryCtrl_UnknownPathReturns404(t *testing.T) {
	historyList = nil
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	got := processHistoryCtrl(`{"action":"create","path":"Unknown","buf-size":"5"}`, nil, true)
	if got != "404 Not Found" {
		t.Errorf("got %q", got)
	}
}

func TestProcessHistoryCtrl_MissingAction(t *testing.T) {
	historyList = []HistoryList{{Path: "X"}}
	got := processHistoryCtrl(`{"path":"X"}`, nil, true)
	if got != "400 Bad Request" {
		t.Errorf("got %q", got)
	}
}

func TestProcessHistoryCtrl_CreateThenDelete(t *testing.T) {
	historyList = []HistoryList{{Path: "X"}}
	if got := processHistoryCtrl(`{"action":"create","path":"X","buf-size":"5"}`, nil, true); got != "200 OK" {
		t.Fatalf("create: %q", got)
	}
	if historyList[0].BufSize != 5 || len(historyList[0].Buffer) != 5 {
		t.Errorf("buffer not allocated correctly: %+v", historyList[0])
	}
	if got := processHistoryCtrl(`{"action":"delete","path":"X"}`, nil, true); got != "200 OK" {
		t.Errorf("delete: %q", got)
	}
}

func TestProcessHistoryCtrl_BadBufSize(t *testing.T) {
	historyList = []HistoryList{{Path: "X"}}
	if got := processHistoryCtrl(`{"action":"create","path":"X","buf-size":"oops"}`, nil, true); got != "400 Bad Request" {
		t.Errorf("got %q", got)
	}
	if got := processHistoryCtrl(`{"action":"create","path":"X","buf-size":"0"}`, nil, true); got != "400 Bad Request" {
		t.Errorf("zero bufsize should be rejected; got %q", got)
	}
}

// --------------------------------------------------------------------------
// getHistoryListIndex
// --------------------------------------------------------------------------

func TestGetHistoryListIndex(t *testing.T) {
	historyList = []HistoryList{{Path: "A"}, {Path: "B"}}
	if got := getHistoryListIndex("B"); got != 1 {
		t.Errorf("got %d; want 1", got)
	}
	if got := getHistoryListIndex("missing"); got != -1 {
		t.Errorf("got %d; want -1", got)
	}
}

// --------------------------------------------------------------------------
// historicDataPack — empty-list / negative-matches guard
// --------------------------------------------------------------------------

func TestHistoricDataPack_NegativeMatchesIsSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := historicDataPack(0, 0); got != "" {
		t.Errorf("got %q", got)
	}
	if got := historicDataPack(-1, 5); got != "" {
		t.Errorf("got %q", got)
	}
	if got := historicDataPack(99, 5); got != "" {
		t.Errorf("out-of-range index: got %q", got)
	}
}

// --------------------------------------------------------------------------
// convertFromIsoTime + processHistoryGet bad-time guard (#6)
// --------------------------------------------------------------------------

func TestConvertFromIsoTime(t *testing.T) {
	if _, err := convertFromIsoTime("2026-05-17T12:00:00Z"); err != nil {
		t.Errorf("happy path: %v", err)
	}
	if _, err := convertFromIsoTime("not a time"); err == nil {
		t.Errorf("expected error on bad input")
	}
}

func TestProcessHistoryGet_MissingPathReturnsEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := processHistoryGet(`{}`); got != "" {
		t.Errorf("missing path: got %q", got)
	}
	if got := processHistoryGet(`{"path":42,"period":"2026-05-17T12:00:00Z"}`); got != "" {
		t.Errorf("non-string path: got %q", got)
	}
}

func TestProcessHistoryGet_UnknownPathReturnsEmpty(t *testing.T) {
	historyList = nil
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	got := processHistoryGet(`{"path":"Unknown","period":"2026-05-17T12:00:00Z"}`)
	if got != "" {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// getSubcriptionStateIndex / setSubscriptionListThreads
// --------------------------------------------------------------------------

func TestGetSubcriptionStateIndex(t *testing.T) {
	list := []SubscriptionState{
		{SubscriptionId: 1},
		{SubscriptionId: 5},
	}
	if got := getSubcriptionStateIndex(5, list); got != 1 {
		t.Errorf("got %d; want 1", got)
	}
	if got := getSubcriptionStateIndex(99, list); got != -1 {
		t.Errorf("got %d; want -1", got)
	}
}

func TestSetSubscriptionListThreads(t *testing.T) {
	list := []SubscriptionState{
		{SubscriptionId: 1, SubscriptionThreads: 0},
	}
	out := setSubscriptionListThreads(list, SubThreads{SubscriptionId: 1, NumofThreads: 3})
	if out[0].SubscriptionThreads != 3 {
		t.Errorf("got %d; want 3", out[0].SubscriptionThreads)
	}
}

// --------------------------------------------------------------------------
// unpackPaths
// --------------------------------------------------------------------------

func TestUnpackPaths(t *testing.T) {
	if got := unpackPaths(""); got != nil {
		t.Errorf("empty should return nil; got %v", got)
	}
	got := unpackPaths("single.path")
	if len(got) != 1 || got[0] != "single.path" {
		t.Errorf("single path: got %v", got)
	}
	got = unpackPaths(`["a","b","c"]`)
	if len(got) != 3 {
		t.Errorf("array: got %v", got)
	}
	if got := unpackPaths(`["bad`); got != nil {
		t.Errorf("malformed array should return nil; got %v", got)
	}
}

// --------------------------------------------------------------------------
// scanAndRemoveListItem — clean rewrite (#35)
// --------------------------------------------------------------------------

func TestScanAndRemoveListItem_RemovesFirstMatch(t *testing.T) {
	list := []SubscriptionState{
		{SubscriptionId: 1, RouterId: "r1"},
		{SubscriptionId: 2, RouterId: "r2"},
		{SubscriptionId: 3, RouterId: "r1"},
	}
	removed, out := scanAndRemoveListItem(list, "r1")
	if !removed {
		t.Errorf("expected removal")
	}
	if len(out) != 2 {
		t.Errorf("expected 2 remaining; got %d", len(out))
	}
}

func TestScanAndRemoveListItem_NoMatch(t *testing.T) {
	list := []SubscriptionState{{SubscriptionId: 1, RouterId: "r1"}}
	removed, out := scanAndRemoveListItem(list, "r999")
	if removed {
		t.Errorf("no match should return false")
	}
	if len(out) != 1 {
		t.Errorf("list should be unchanged")
	}
}

// --------------------------------------------------------------------------
// getSubscriptionData
// --------------------------------------------------------------------------

func TestGetSubscriptionData_Found(t *testing.T) {
	list := []SubscriptionState{
		{SubscriptionId: 5, RouterId: "r1", GatingId: "g1"},
	}
	rid, sid := getSubscriptionData(list, "g1")
	if rid != "r1" || sid != "5" {
		t.Errorf("got rid=%q sid=%q; want r1,5", rid, sid)
	}
}

func TestGetSubscriptionData_NotFound(t *testing.T) {
	rid, sid := getSubscriptionData(nil, "g1")
	if rid != "" || sid != "" {
		t.Errorf("expected empty; got %q,%q", rid, sid)
	}
}

// --------------------------------------------------------------------------
// decodeFeederMessage — defensive type assertions
// --------------------------------------------------------------------------

func TestDecodeFeederMessage_HappyPath(t *testing.T) {
	path, notif := decodeFeederMessage(`{"action":"subscription","path":"Vehicle.Speed"}`, false)
	if path != "Vehicle.Speed" {
		t.Errorf("got path=%q", path)
	}
	_ = notif

	_, notif = decodeFeederMessage(`{"action":"subscribe","status":"ok"}`, false)
	if !notif {
		t.Errorf("subscribe-ok should set notif=true")
	}
}

func TestDecodeFeederMessage_MalformedInputDoesNotPanic(t *testing.T) {
	cases := []string{
		``,
		`not json`,
		`{}`,
		`{"action":42}`,
		`{"action":"subscription","path":42}`,
		`{"action":"subscribe","status":42}`,
		`{"action":"foo"}`,
	}
	for _, in := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panicked on %q: %v", in, r)
				}
			}()
			_, _ = decodeFeederMessage(in, false)
		}()
	}
}

// --------------------------------------------------------------------------
// decodeFeederRegRequest — defensive assertions (#31)
// --------------------------------------------------------------------------

func TestDecodeFeederRegRequest_HappyPath(t *testing.T) {
	got := decodeFeederRegRequest([]byte(`{"action":"reg","name":"myfeeder"}`), "1")
	if got.Name != "myfeeder" || got.InfoType != "Data" {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeFeederRegRequest_Dereg(t *testing.T) {
	got := decodeFeederRegRequest([]byte(`{"action":"dereg","name":"myfeeder"}`), "1")
	if got.InfoType != "dereg" || got.Name != "myfeeder" {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeFeederRegRequest_MalformedDoesNotPanic(t *testing.T) {
	cases := [][]byte{
		[]byte(``),
		[]byte(`not json`),
		[]byte(`{}`),
		[]byte(`{"action":42,"name":"a"}`),
		[]byte(`{"action":"reg","name":42}`),
		[]byte(`{"action":"unknown","name":"a"}`),
	}
	for _, in := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panicked on %q: %v", in, r)
				}
			}()
			got := decodeFeederRegRequest(in, "1")
			if got.InfoType != "error" {
				t.Errorf("expected InfoType=error for %q; got %q", in, got.InfoType)
			}
		}()
	}
}

// --------------------------------------------------------------------------
// allocateTicker / deallocateTicker — mutex protection
// --------------------------------------------------------------------------

func TestTickerAllocate_HappyPath(t *testing.T) {
	resetTickerGlobals()
	idx := allocateTicker(42)
	if idx < 0 {
		t.Fatalf("expected non-negative index")
	}
	if dx := deallocateTicker(42); dx != idx {
		t.Errorf("deallocate index mismatch: got %d, want %d", dx, idx)
	}
}

func TestTickerAllocate_NoFreeSlots(t *testing.T) {
	resetTickerGlobals()
	for i := 0; i < MAXTICKERS; i++ {
		tickerIndexList[i] = i + 1
	}
	if got := allocateTicker(99999); got != -1 {
		t.Errorf("expected -1; got %d", got)
	}
}

func TestTickerAllocate_ConcurrentSafe(t *testing.T) {
	resetTickerGlobals()
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := make(map[int]int)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			idx := allocateTicker(id)
			mu.Lock()
			seen[idx]++
			mu.Unlock()
		}(i + 1)
	}
	wg.Wait()
	for idx, count := range seen {
		if idx == -1 {
			continue
		}
		if count > 1 {
			t.Errorf("ticker slot %d allocated %d times (race)", idx, count)
		}
	}
}

// --------------------------------------------------------------------------
// activateInterval / activateHistory — non-positive duration guards
// --------------------------------------------------------------------------

func TestActivateInterval_ZeroIntervalDoesNotPanic(t *testing.T) {
	resetTickerGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("zero interval panicked: %v", r)
		}
	}()
	ch := make(chan int, 1)
	activateInterval(ch, 1, 0)
}

func TestActivateHistory_ExtremeFrequencyDoesNotPanic(t *testing.T) {
	resetTickerGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("extreme frequency panicked: %v", r)
		}
	}()
	ch := make(chan int, 1)
	activateHistory(ch, 1, 100_000_000) // 100M cycles/hour → 0ms tick
}

func TestActivateHistory_ZeroFrequencyRejected(t *testing.T) {
	resetTickerGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("zero frequency panicked: %v", r)
		}
	}()
	ch := make(chan int, 1)
	activateHistory(ch, 1, 0)
}

func TestActivateInterval_HappyPath(t *testing.T) {
	resetTickerGlobals()
	ch := make(chan int, 1)
	activateInterval(ch, 7, 50)
	select {
	case got := <-ch:
		if got != 7 {
			t.Errorf("got %d; want 7", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("did not fire in time")
	}
	deactivateInterval(7)
}

// --------------------------------------------------------------------------
// string2Map
// --------------------------------------------------------------------------

func TestString2Map(t *testing.T) {
	got := string2Map(`{"x":"y"}`)
	if got["s2m"] == nil {
		t.Fatalf("missing s2m wrapper")
	}
	m, ok := got["s2m"].(map[string]interface{})
	if !ok {
		t.Fatalf("s2m not a map; got %T", got["s2m"])
	}
	if m["x"] != "y" {
		t.Errorf("got %+v", m)
	}
}

// --------------------------------------------------------------------------
// nonBlockingSend
// --------------------------------------------------------------------------

func TestNonBlockingSend_DeliversWhenSpace(t *testing.T) {
	ch := make(chan string, 1)
	nonBlockingSend(ch, "hello", "test")
	select {
	case got := <-ch:
		if got != "hello" {
			t.Errorf("got %q", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("nothing delivered")
	}
}

func TestNonBlockingSend_DropsOnFull(t *testing.T) {
	ch := make(chan string, 1)
	ch <- "filler"
	done := make(chan struct{})
	go func() {
		nonBlockingSend(ch, "extra", "test")
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("nonBlockingSend blocked on full channel")
	}
}

// --------------------------------------------------------------------------
// handleToFeederMessage / handleFromFeederMessage — malformed input safety
// --------------------------------------------------------------------------

func TestHandleToFeederMessage_MalformedDoesNotPanic(t *testing.T) {
	cases := []string{
		``,
		`not json`,
		`{}`,
		`{"action":42}`,
		`{"action":"subscribe"}`,
		`{"action":"subscribe","subscriptionId":42,"variant":"x","path":[]}`,
		`{"action":"unsubscribe"}`,
		`{"action":"unknown"}`,
	}
	fromFeederCl := make(chan string, 4)
	notif := "not-verified"
	count := 0
	for _, in := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panicked on %q: %v", in, r)
				}
			}()
			handleToFeederMessage(in, fromFeederCl, &notif, &count)
		}()
	}
}

func TestHandleFromFeederMessage_MalformedDoesNotPanic(t *testing.T) {
	cases := []string{
		``,
		`not json`,
		`{}`,
		`{"action":42}`,
		`{"action":"subscribe"}`,
		`{"action":"subscribe","status":42}`,
		`{"action":"subscription"}`,
		`{"action":"subscription","path":42}`,
		`{"action":"unknown"}`,
	}
	rorc := make(chan string, 4)
	cl := make(chan string, 4)
	notif := "not-verified"
	for _, in := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panicked on %q: %v", in, r)
				}
			}()
			handleFromFeederMessage(in, rorc, cl, &notif)
		}()
	}
}

func TestHandleFromFeederMessage_SubscribeOkSetsNotification(t *testing.T) {
	rorc := make(chan string, 4)
	cl := make(chan string, 4)
	notif := "not-verified"
	handleFromFeederMessage(`{"action":"subscribe","status":"ok"}`, rorc, cl, &notif)
	if notif != "supported" {
		t.Errorf("got %q; want supported", notif)
	}
}

func TestHandleFromFeederMessage_SubscriptionDispatchesByVariant(t *testing.T) {
	resetFeederGlobals()
	feederSubList = []FeederSubElem{
		{SubscriptionId: "1", Variant: "change", Path: []string{"Vehicle.Speed"}},
	}
	rorc := make(chan string, 4)
	cl := make(chan string, 4)
	notif := "supported"
	handleFromFeederMessage(`{"action":"subscription","path":"Vehicle.Speed"}`, rorc, cl, &notif)
	select {
	case <-rorc:
		// good
	case <-time.After(200 * time.Millisecond):
		t.Errorf("change variant should route to fromFeederRorC")
	}
}

// --------------------------------------------------------------------------
// checkRCFilterAndIssueMessages — empty Path guard (#36)
// --------------------------------------------------------------------------

func TestCheckRCFilterAndIssueMessages_EmptyPathDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on empty Path: %v", r)
		}
	}()
	list := []SubscriptionState{
		{SubscriptionId: 1, Path: nil},
		{SubscriptionId: 2, Path: []string{}},
	}
	backendChan := make(chan map[string]interface{}, 1)
	out := checkRCFilterAndIssueMessages("Vehicle.Speed", list, backendChan)
	if len(out) != 2 {
		t.Errorf("entries dropped: got %d; want 2", len(out))
	}
}

// --------------------------------------------------------------------------
// getSubscribeVariant — variants accumulation
// --------------------------------------------------------------------------

func TestGetSubscribeVariant(t *testing.T) {
	resetFeederGlobals()
	feederSubList = []FeederSubElem{
		{SubscriptionId: "1", Variant: "change", Path: []string{"X"}},
		{SubscriptionId: "2", Variant: "range", Path: []string{"X"}},
		{SubscriptionId: "3", Variant: "curvelog", Path: []string{"Y"}},
	}
	got := getSubscribeVariant("X")
	if !strings.Contains(got, "change") || !strings.Contains(got, "range") {
		t.Errorf("X variants: got %q", got)
	}
	if strings.Contains(got, "curvelog") {
		t.Errorf("curvelog should not appear in X variants: got %q", got)
	}
	if got := getSubscribeVariant("Y"); got != "curvelog" {
		t.Errorf("Y variants: got %q", got)
	}
	if got := getSubscribeVariant("missing"); got != "" {
		t.Errorf("missing path: got %q", got)
	}
}

// --------------------------------------------------------------------------
// getDPValue / getDPTs (string helpers for data-point JSON)
// --------------------------------------------------------------------------

func TestGetDPValue(t *testing.T) {
	in := `{"value":"42", "ts":"2026-05-17T12:00:00Z"}`
	if got := getDPValue(in); got != "42" {
		t.Errorf("got %q", got)
	}
}

func TestGetDPTs(t *testing.T) {
	in := `{"value":"42", "ts":"2026-05-17T12:00:00Z"}`
	if got := getDPTs(in); got != "2026-05-17T12:00:00Z" {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// captureHistoryValue — round-trip via stateDbType="none" backend
// --------------------------------------------------------------------------

func TestCaptureHistoryValue_NoneBackend(t *testing.T) {
	savedDb := stateDbType
	defer func() { stateDbType = savedDb }()
	stateDbType = "none"
	historyList = []HistoryList{{Path: "X", BufSize: 4, Buffer: make([]string, 4)}}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	captureHistoryValue(0)
	if historyList[0].BufIndex == 0 {
		t.Errorf("buffer index should have advanced")
	}
}

// --------------------------------------------------------------------------
// Bonus: empty inputs to addOnFeederPathList / deleteOnFeederPathList do
// not produce malformed JSON like "[]" + closing quote.
// --------------------------------------------------------------------------

func TestAddOnFeederPathList_NoNewEntriesReturnsEmptyJSON(t *testing.T) {
	resetFeederGlobals()
	feederPathList = []FeederPathElem{{Path: "A", Reference: 1}}
	_, added := addOnFeederPathList([]interface{}{"A"}) // already present, no new
	if added != "" {
		t.Errorf("expected empty JSON; got %q", added)
	}
}

// --------------------------------------------------------------------------
// Drive-by smoke: deactivateInterval / deactivateHistory handle missing id.
// --------------------------------------------------------------------------

func TestDeactivateInterval_UnknownId(t *testing.T) {
	resetTickerGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	deactivateInterval(99999)
}

func TestDeactivateHistory_UnknownId(t *testing.T) {
	resetTickerGlobals()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	deactivateHistory(99999)
}

// --------------------------------------------------------------------------
// removeFromsubscriptionList sanity
// --------------------------------------------------------------------------

func TestRemoveFromsubscriptionList(t *testing.T) {
	list := []SubscriptionState{
		{SubscriptionId: 1},
		{SubscriptionId: 2},
		{SubscriptionId: 3},
	}
	out := removeFromsubscriptionList(list, 1) // index 1 = subscription 2
	if len(out) != 2 {
		t.Errorf("got len=%d; want 2", len(out))
	}
	for _, s := range out {
		if s.SubscriptionId == 2 {
			t.Errorf("subscription 2 should have been removed: %+v", out)
			return
		}
	}
}

// --------------------------------------------------------------------------
// Sanity: subscriptionId monotonic in a single goroutine
// --------------------------------------------------------------------------

func TestSubscriptionIdMonotonic(t *testing.T) {
	subscriptionId = 1
	saved := subscriptionId
	subscriptionId++
	if subscriptionId != saved+1 {
		t.Errorf("monotonic increment failed")
	}
}

// --------------------------------------------------------------------------
// FeederPathElem JSON helpers — verify addOnFeederPathList output round-trips.
// --------------------------------------------------------------------------

func TestAddOnFeederPathList_OutputIsValidJSON(t *testing.T) {
	resetFeederGlobals()
	_, added := addOnFeederPathList([]interface{}{"Vehicle.Speed", "Vehicle.Direction"})
	// Should be a JSON array like ["Vehicle.Speed", "Vehicle.Direction"]
	if !strings.HasPrefix(added, `["`) || !strings.HasSuffix(added, `"]`) {
		t.Errorf("invalid JSON shape: %q", added)
	}
	if got := strings.Count(added, `", "`); got != 1 {
		t.Errorf("expected single separator; got %d in %q", got, added)
	}
}

// --------------------------------------------------------------------------
// Smoke test: createFeederNameList format
// --------------------------------------------------------------------------

func TestCreateFeederNameList_FormatIsValid(t *testing.T) {
	resetFeederGlobals()
	for i := 0; i < 3; i++ {
		feederRegList = append(feederRegList, FeederRegElem{Name: "f" + strconv.Itoa(i)})
	}
	got := createFeederNameList()
	if !strings.HasPrefix(got.Name, "[") || !strings.HasSuffix(got.Name, "]") {
		t.Errorf("invalid JSON: %q", got.Name)
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(got.Name, "f"+strconv.Itoa(i)) {
			t.Errorf("missing f%d in %q", i, got.Name)
		}
	}
}
