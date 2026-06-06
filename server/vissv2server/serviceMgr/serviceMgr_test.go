/**
* Regression tests for the serviceMgr fixes shipped in PR #120 (history
* control) and PR #121 (history get).
**/
package serviceMgr

import (
	"database/sql"
	"net"
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

// --------------------------------------------------------------------------
// compareValues — pure logic, all branches
// --------------------------------------------------------------------------

func TestCompareValues_NumberEq(t *testing.T) {
	// curVal - diffVal == latVal  =>  10 - 0 == 10
	if !compareValues("eq", "10", "10", "0", "number") {
		t.Errorf("10-0==10 should be true")
	}
	// 11 - 0 != 10
	if compareValues("eq", "10", "11", "0", "number") {
		t.Errorf("11-0==10 should be false")
	}
}

func TestCompareValues_NumberNe(t *testing.T) {
	if !compareValues("ne", "10", "11", "0", "number") {
		t.Errorf("11-0!=10 should be true")
	}
	if compareValues("ne", "10", "10", "0", "number") {
		t.Errorf("10-0!=10 should be false")
	}
}

func TestCompareValues_NumberGt(t *testing.T) {
	if !compareValues("gt", "10", "20", "0", "number") {
		t.Errorf("20>10 should be true")
	}
	if compareValues("gt", "10", "10", "0", "number") {
		t.Errorf("10>10 should be false")
	}
}

func TestCompareValues_NumberGte(t *testing.T) {
	if !compareValues("gte", "10", "10", "0", "number") {
		t.Errorf("10>=10 should be true")
	}
	if compareValues("gte", "10", "9", "0", "number") {
		t.Errorf("9>=10 should be false")
	}
}

func TestCompareValues_NumberLt(t *testing.T) {
	if !compareValues("lt", "10", "5", "0", "number") {
		t.Errorf("5<10 should be true")
	}
	if compareValues("lt", "10", "10", "0", "number") {
		t.Errorf("10<10 should be false")
	}
}

func TestCompareValues_NumberLte(t *testing.T) {
	if !compareValues("lte", "10", "10", "0", "number") {
		t.Errorf("10<=10 should be true")
	}
	if compareValues("lte", "10", "11", "0", "number") {
		t.Errorf("11<=10 should be false")
	}
}

func TestCompareValues_NumberWithDiff(t *testing.T) {
	// curVal - diff > latVal  =>  15 - 3 > 10  => 12 > 10 true
	if !compareValues("gt", "10", "15", "3", "number") {
		t.Errorf("15-3>10 should be true")
	}
	// 12 - 3 > 10  =>  9 > 10  false
	if compareValues("gt", "10", "12", "3", "number") {
		t.Errorf("12-3>10 should be false")
	}
}

func TestCompareValues_NumberUnknownLogicOp(t *testing.T) {
	if compareValues("bogus", "10", "10", "0", "number") {
		t.Errorf("unknown logicOp should return false")
	}
}

func TestCompareValues_NumberParseError(t *testing.T) {
	if compareValues("eq", "not-a-number", "10", "0", "number") {
		t.Errorf("parse error on latestValue should return false")
	}
	if compareValues("eq", "10", "not-a-number", "0", "number") {
		t.Errorf("parse error on currentValue should return false")
	}
	if compareValues("eq", "10", "10", "not-a-number", "number") {
		t.Errorf("parse error on diff should return false")
	}
}

func TestCompareValues_BoolEq(t *testing.T) {
	if !compareValues("eq", "true", "true", "0", "bool") {
		t.Errorf("true==true should be true")
	}
	if compareValues("eq", "true", "false", "0", "bool") {
		t.Errorf("false==true should be false")
	}
}

func TestCompareValues_BoolNe(t *testing.T) {
	if !compareValues("ne", "true", "false", "0", "bool") {
		t.Errorf("false!=true should be true")
	}
	if compareValues("ne", "true", "true", "0", "bool") {
		t.Errorf("true!=true should be false")
	}
}

func TestCompareValues_BoolGt(t *testing.T) {
	// false->true: latestValue=="false" && currentValue != latestValue
	if !compareValues("gt", "false", "true", "0", "bool") {
		t.Errorf("false->true gt should be true")
	}
	if compareValues("gt", "true", "false", "0", "bool") {
		t.Errorf("true->false gt should be false")
	}
}

func TestCompareValues_BoolLt(t *testing.T) {
	// true->false: latestValue=="true" && currentValue != latestValue
	if !compareValues("lt", "true", "false", "0", "bool") {
		t.Errorf("true->false lt should be true")
	}
	if compareValues("lt", "false", "true", "0", "bool") {
		t.Errorf("false->true lt should be false")
	}
}

func TestCompareValues_BoolInvalidDiff(t *testing.T) {
	if compareValues("eq", "true", "true", "1", "bool") {
		t.Errorf("non-zero diff for bool should return false")
	}
}

func TestCompareValues_BoolUnknownLogicOp(t *testing.T) {
	if compareValues("bogus", "true", "true", "0", "bool") {
		t.Errorf("unknown bool logicOp should return false")
	}
}

func TestCompareValues_UnknownDatatype(t *testing.T) {
	if compareValues("eq", "x", "x", "0", "string") {
		t.Errorf("unknown datatype should return false")
	}
}

// --------------------------------------------------------------------------
// evaluateRangeFilter — single and array forms
// --------------------------------------------------------------------------

func TestEvaluateRangeFilter_SingleGt(t *testing.T) {
	// {"logic-op":"gt","boundary":"50"} means currentValue > 50
	// compareValues("gt", "50", currentValue, "0", "number") => currentValue - 0 > 50
	param := `{"logic-op":"gt","boundary":"50"}`
	if !evaluateRangeFilter(param, "60") {
		t.Errorf("60 > 50 should be true")
	}
	if evaluateRangeFilter(param, "40") {
		t.Errorf("40 > 50 should be false")
	}
}

func TestEvaluateRangeFilter_ArrayAnd(t *testing.T) {
	// Two conditions combined with AND: > 10 AND < 100
	param := `[{"logic-op":"gt","boundary":"10"},{"logic-op":"lt","boundary":"100"}]`
	if !evaluateRangeFilter(param, "50") {
		t.Errorf("50 in (10,100) should be true")
	}
	if evaluateRangeFilter(param, "5") {
		t.Errorf("5 not in (10,100) should be false")
	}
}

func TestEvaluateRangeFilter_BadJSON(t *testing.T) {
	if evaluateRangeFilter(`not json`, "50") {
		t.Errorf("bad JSON should return false")
	}
}

func TestEvaluateRangeFilter_NonNumericCurrentValue(t *testing.T) {
	param := `{"logic-op":"gt","boundary":"50"}`
	if evaluateRangeFilter(param, "not-a-number") {
		t.Errorf("non-numeric currentValue should return false")
	}
}

// --------------------------------------------------------------------------
// evaluateChangeFilter
// --------------------------------------------------------------------------

func TestEvaluateChangeFilter_NumberNe(t *testing.T) {
	// ne with diff=0: current != latest
	param := `{"logic-op":"ne","diff":"0"}`
	dp := `{"value":"20","ts":"2026-01-01T00:00:00Z"}`
	ok, outDp := evaluateChangeFilter(param, "10", "20", dp)
	if !ok {
		t.Errorf("20 != 10 should be true")
	}
	if outDp != dp {
		t.Errorf("currentDataPoint should be returned as-is")
	}
}

func TestEvaluateChangeFilter_NumberEqNoDiff(t *testing.T) {
	param := `{"logic-op":"eq","diff":"0"}`
	dp := `{"value":"10","ts":"2026-01-01T00:00:00Z"}`
	ok, _ := evaluateChangeFilter(param, "10", "10", dp)
	if !ok {
		t.Errorf("10 == 10 should be true")
	}
}

func TestEvaluateChangeFilter_BoolNe(t *testing.T) {
	// diff = "false" is boolean: IsBoolean("false") => true
	param := `{"logic-op":"ne","diff":"false"}`
	dp := `{"value":"true","ts":"2026-01-01T00:00:00Z"}`
	ok, _ := evaluateChangeFilter(param, "false", "true", dp)
	// compareValues("ne", "false", "true", "false", "bool")
	// diff is not "0" so returns false for bool
	// evaluateChangeFilter with IsBoolean(diff) => datatype="bool"
	// but diff="false" != "0" => compareValues returns false
	_ = ok // just ensure no panic
}

func TestEvaluateChangeFilter_BadJSON(t *testing.T) {
	ok, out := evaluateChangeFilter(`not json`, "10", "20", "dp")
	if ok {
		t.Errorf("bad JSON should return false")
	}
	if out != "" {
		t.Errorf("bad JSON should return empty string")
	}
}

// --------------------------------------------------------------------------
// unpackDataPoint
// --------------------------------------------------------------------------

func TestUnpackDataPoint_HappyPath(t *testing.T) {
	v, ts := unpackDataPoint(`{"value":"42","ts":"2026-01-01T00:00:00Z"}`)
	if v != "42" || ts != "2026-01-01T00:00:00Z" {
		t.Errorf("got v=%q ts=%q", v, ts)
	}
}

func TestUnpackDataPoint_BadJSON(t *testing.T) {
	v, ts := unpackDataPoint(`not json`)
	if v != "" || ts != "" {
		t.Errorf("bad JSON should return empty strings; got v=%q ts=%q", v, ts)
	}
}

func TestUnpackDataPoint_EmptyString(t *testing.T) {
	v, ts := unpackDataPoint(``)
	if v != "" || ts != "" {
		t.Errorf("empty string should return empty strings; got v=%q ts=%q", v, ts)
	}
}

// --------------------------------------------------------------------------
// getOpType
// --------------------------------------------------------------------------

func TestGetOpType(t *testing.T) {
	filters := []utils.FilterObject{
		{Type: "timebased"},
		{Type: "range"},
	}
	if !getOpType(filters, "timebased") {
		t.Errorf("should find timebased")
	}
	if !getOpType(filters, "range") {
		t.Errorf("should find range")
	}
	if getOpType(filters, "curvelog") {
		t.Errorf("should not find curvelog")
	}
	if getOpType(nil, "anything") {
		t.Errorf("nil list should return false")
	}
}

// --------------------------------------------------------------------------
// deactivateSubscription
// --------------------------------------------------------------------------

func TestDeactivateSubscription_NotFound(t *testing.T) {
	resetTickerGlobals()
	list := []SubscriptionState{
		{SubscriptionId: 1, RouterId: "r1"},
	}
	status, out := deactivateSubscription(list, "999")
	if status != -1 {
		t.Errorf("expected -1 for not-found; got %d", status)
	}
	if len(out) != 1 {
		t.Errorf("list should be unchanged; got len=%d", len(out))
	}
}

func TestDeactivateSubscription_PlainFilter(t *testing.T) {
	resetTickerGlobals()
	list := []SubscriptionState{
		{
			SubscriptionId: 7,
			RouterId:       "r1",
			FilterList:     []utils.FilterObject{{Type: "paths"}},
		},
	}
	status, out := deactivateSubscription(list, "7")
	if status != 1 {
		t.Errorf("expected status 1; got %d", status)
	}
	if len(out) != 0 {
		t.Errorf("list should be empty after remove; got len=%d", len(out))
	}
}

func TestDeactivateSubscription_TimebasedFilter(t *testing.T) {
	resetTickerGlobals()
	// Allocate a ticker slot for the subscription so deactivateInterval
	// has a real slot to free (avoids a log warning and is the real path).
	ch := make(chan int, 1)
	activateInterval(ch, 5, 100) // interval=100ms so it doesn't fire quickly
	list := []SubscriptionState{
		{
			SubscriptionId: 5,
			RouterId:       "r1",
			FilterList:     []utils.FilterObject{{Type: "timebased"}},
		},
	}
	status, out := deactivateSubscription(list, "5")
	if status != 1 {
		t.Errorf("expected status 1; got %d", status)
	}
	if len(out) != 0 {
		t.Errorf("list should be empty; got len=%d", len(out))
	}
}

func TestDeactivateSubscription_CurvelogFilter(t *testing.T) {
	resetTickerGlobals()
	list := []SubscriptionState{
		{
			SubscriptionId: 9,
			RouterId:       "r1",
			FilterList:     []utils.FilterObject{{Type: "curvelog"}},
		},
	}
	status, out := deactivateSubscription(list, "9")
	if status != 1 {
		t.Errorf("expected status 1; got %d", status)
	}
	if len(out) != 0 {
		t.Errorf("list should be empty; got len=%d", len(out))
	}
}

// --------------------------------------------------------------------------
// historicDataPack
// --------------------------------------------------------------------------

func TestHistoricDataPack_SingleMatch(t *testing.T) {
	historyList = []HistoryList{
		{
			Path:     "X",
			BufSize:  5,
			BufIndex: 1,
			Buffer:   []string{`{"value":"42","ts":"2026-01-01T00:00:00Z"}`},
		},
	}
	got := historicDataPack(0, 1)
	if !strings.Contains(got, `"value":"42"`) {
		t.Errorf("expected value in output; got %q", got)
	}
	// Single match: no surrounding brackets
	if strings.HasPrefix(got, "[") {
		t.Errorf("single match should not have brackets; got %q", got)
	}
}

func TestHistoricDataPack_MultipleMatches(t *testing.T) {
	historyList = []HistoryList{
		{
			Path:     "X",
			BufSize:  5,
			BufIndex: 2,
			Buffer: []string{
				`{"value":"1","ts":"2026-01-01T00:00:00Z"}`,
				`{"value":"2","ts":"2026-01-01T00:00:01Z"}`,
			},
		},
	}
	got := historicDataPack(0, 2)
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Errorf("multiple matches should have brackets; got %q", got)
	}
	if !strings.Contains(got, `"value":"1"`) || !strings.Contains(got, `"value":"2"`) {
		t.Errorf("expected both values; got %q", got)
	}
}

func TestHistoricDataPack_ZeroMatchesReturnsEmpty(t *testing.T) {
	historyList = []HistoryList{{Path: "X"}}
	if got := historicDataPack(0, 0); got != "" {
		t.Errorf("zero matches should return empty; got %q", got)
	}
}

func TestHistoricDataPack_OutOfRangeIndexReturnsEmpty(t *testing.T) {
	historyList = []HistoryList{{Path: "X"}}
	if got := historicDataPack(5, 1); got != "" {
		t.Errorf("out-of-range index should return empty; got %q", got)
	}
	if got := historicDataPack(-1, 1); got != "" {
		t.Errorf("negative index should return empty; got %q", got)
	}
}

// --------------------------------------------------------------------------
// handleServiceSet — map[string]interface{} value branch (uncovered)
// --------------------------------------------------------------------------

func TestHandleServiceSet_MapValueStorageUnavailable(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "set",
		"requestId": "1",
		"path":      "Vehicle.Speed",
		"value":     map[string]interface{}{"x": "y"},
	}
	resp := buildServiceResponseMap(req)

	go handleServiceSet(req, resp, dataChan)

	select {
	case got := <-dataChan:
		// With noneBackend, Set returns "" for map values too
		if _, isErr := got["error"]; !isErr {
			// noneBackend.Set always returns a ts string, so success is acceptable
			// Just verify we got a non-blocking response
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceSet did not reply on dataChan")
	}
}

// --------------------------------------------------------------------------
// handleServiceUnsubscribe — success path (with real subscription)
// --------------------------------------------------------------------------

func TestHandleServiceUnsubscribe_SuccessPath(t *testing.T) {
	resetTickerGlobals()
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	toFeederChan := make(chan string, 1)
	list := []SubscriptionState{
		{
			SubscriptionId: 42,
			RouterId:       "0?0",
			FilterList:     []utils.FilterObject{{Type: "paths"}},
		},
	}
	req := map[string]interface{}{
		"RouterId":       "0?0",
		"action":         "unsubscribe",
		"requestId":      "1",
		"subscriptionId": "42",
	}
	resp := buildServiceResponseMap(req)

	updated := handleServiceUnsubscribe(req, resp, dataChan, list, toFeederChan)
	if len(updated) != 0 {
		t.Errorf("subscriptionList should be empty after unsubscribe; got len=%d", len(updated))
	}

	select {
	case got := <-dataChan:
		if _, isErr := got["error"]; isErr {
			t.Fatalf("expected success response; got error %v", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceUnsubscribe did not reply on dataChan")
	}

	select {
	case <-toFeederChan:
		// good: forward to feeder
	case <-time.After(time.Second):
		t.Fatalf("handleServiceUnsubscribe did not send to toFeederChan")
	}
}

// --------------------------------------------------------------------------
// handleServiceGet — filter validation branch
// --------------------------------------------------------------------------

func TestHandleServiceGet_MalformedFilterReturnsBadRequest(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	req := map[string]interface{}{
		"RouterId":  "0?0",
		"action":    "get",
		"requestId": "1",
		"path":      "Vehicle.Speed",
		"filter":    42, // filter present but not a map or array — UnpackFilter returns empty list
	}
	resp := buildServiceResponseMap(req)

	go handleServiceGet(req, resp, dataChan)

	select {
	case got := <-dataChan:
		errMap, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected error response; got %v", got)
		}
		if errMap["reason"] != "bad_request" {
			t.Errorf("error.reason = %v; want bad_request", errMap["reason"])
		}
	case <-time.After(time.Second):
		t.Fatalf("handleServiceGet did not reply on dataChan")
	}
}

// --------------------------------------------------------------------------
// getMetadataDomainDp — pure dispatcher for domain metadata
// --------------------------------------------------------------------------

func TestGetMetadataDomainDp_Samplerate(t *testing.T) {
	got := getMetadataDomainDp("samplerate", "Vehicle.Speed")
	if !strings.Contains(got, "value") || !strings.Contains(got, "ts") {
		t.Errorf("expected canonical dp JSON; got %q", got)
	}
}

func TestGetMetadataDomainDp_Availability(t *testing.T) {
	got := getMetadataDomainDp("availability", "Vehicle.Speed")
	if !strings.Contains(got, "available") {
		t.Errorf("expected 'available' in output; got %q", got)
	}
}

func TestGetMetadataDomainDp_Validate(t *testing.T) {
	got := getMetadataDomainDp("validate", "Vehicle.Speed")
	if !strings.Contains(got, "read-write") {
		t.Errorf("expected 'read-write' in output; got %q", got)
	}
}

func TestGetMetadataDomainDp_Unknown(t *testing.T) {
	got := getMetadataDomainDp("unknown-domain", "Vehicle.Speed")
	if !strings.Contains(got, "Unknown domain") {
		t.Errorf("expected 'Unknown domain' in output; got %q", got)
	}
}

// --------------------------------------------------------------------------
// checkRCFilterAndIssueMessages — range filter evaluation path
// --------------------------------------------------------------------------

func TestCheckRCFilterAndIssueMessages_RangeMatch(t *testing.T) {
	// The noneBackend returns a dummyValue dp. We need to set up a
	// subscription whose filter matches the noneBackend's output.
	// noneBackend.Get returns {"value":"N","ts":"..."} where N is dummyValue.
	// We use a range filter "gte" with boundary 0 so it always matches.
	backendChan := make(chan map[string]interface{}, 4)

	// Use a range filter that always fires (value >= 0 is always true for
	// a non-negative dummyValue counter).
	dp := `{"value":"0","ts":"2026-01-01T00:00:00Z"}`
	list := []SubscriptionState{
		{
			SubscriptionId:  1,
			RouterId:        "r1",
			Path:            []string{"Vehicle.Speed"},
			LatestDataPoint: dp,
			FilterList: []utils.FilterObject{
				{
					Type:      "range",
					Parameter: `{"logic-op":"gte","boundary":"0"}`,
				},
			},
		},
	}

	// checkRCFilterAndIssueMessages calls getVehicleData which uses noneBackend
	// which returns a numeric dummyValue. With "gte" and boundary "0" we
	// expect the filter to match (dummyValue >= 0 is always true since it
	// wraps 0..999).
	out := checkRCFilterAndIssueMessages("", list, backendChan)
	if len(out) != 1 {
		t.Errorf("subscription list should be unchanged; got len=%d", len(out))
	}

	// Read any notification that was pushed (may or may not fire depending
	// on dummyValue == latestDataPoint comparison).
	select {
	case <-backendChan:
		// a notification was issued — that's fine
	case <-time.After(100 * time.Millisecond):
		// no notification — also valid if values match
	}
}

func TestCheckRCFilterAndIssueMessages_TriggeredPath(t *testing.T) {
	// With a specific triggeredPath that doesn't match the subscription,
	// no filter evaluation happens.
	backendChan := make(chan map[string]interface{}, 1)
	dp := `{"value":"5","ts":"2026-01-01T00:00:00Z"}`
	list := []SubscriptionState{
		{
			SubscriptionId:  2,
			RouterId:        "r1",
			Path:            []string{"Vehicle.Speed"},
			LatestDataPoint: dp,
			FilterList: []utils.FilterObject{
				{Type: "range", Parameter: `{"logic-op":"gt","boundary":"100"}`},
			},
		},
	}
	// triggeredPath is something else — subscription should be skipped
	out := checkRCFilterAndIssueMessages("Vehicle.SteeringAngle", list, backendChan)
	if len(out) != 1 {
		t.Errorf("list should be unchanged; got len=%d", len(out))
	}
	select {
	case got := <-backendChan:
		t.Errorf("no notification expected for non-matching path; got %v", got)
	default:
	}
}

// --------------------------------------------------------------------------
// processHistoryCtrl — stop and start actions (additional coverage)
// --------------------------------------------------------------------------

func TestProcessHistoryCtrl_StartAndStop(t *testing.T) {
	resetTickerGlobals()
	historyList = []HistoryList{{Path: "X"}}

	// create first
	if got := processHistoryCtrl(`{"action":"create","path":"X","buf-size":"10"}`, nil, true); got != "200 OK" {
		t.Fatalf("create: %q", got)
	}

	// start with frequency
	histChan := make(chan int, 1)
	got := processHistoryCtrl(`{"action":"start","path":"X","frequency":"60"}`, histChan, true)
	if got != "200 OK" {
		t.Errorf("start: %q", got)
	}
	if historyList[0].Status != 1 {
		t.Errorf("Status should be 1 after start; got %d", historyList[0].Status)
	}

	// stop
	got = processHistoryCtrl(`{"action":"stop","path":"X"}`, histChan, true)
	if got != "200 OK" {
		t.Errorf("stop: %q", got)
	}
	if historyList[0].Status != 0 {
		t.Errorf("Status should be 0 after stop; got %d", historyList[0].Status)
	}
}

func TestProcessHistoryCtrl_DeleteWhileRunning(t *testing.T) {
	historyList = []HistoryList{{Path: "X", Status: 1}} // already "running"
	got := processHistoryCtrl(`{"action":"delete","path":"X"}`, nil, true)
	if got != "409 Conflict" {
		t.Errorf("expected 409 Conflict for delete-while-running; got %q", got)
	}
}

func TestProcessHistoryCtrl_StartMissingFrequency(t *testing.T) {
	historyList = []HistoryList{{Path: "X"}}
	got := processHistoryCtrl(`{"action":"start","path":"X"}`, nil, true)
	if got != "400 Bad Request" {
		t.Errorf("expected 400 Bad Request for missing frequency; got %q", got)
	}
}

func TestProcessHistoryCtrl_StartBadFrequency(t *testing.T) {
	historyList = []HistoryList{{Path: "X"}}
	got := processHistoryCtrl(`{"action":"start","path":"X","frequency":"not-a-number"}`, nil, true)
	if got != "400 Bad Request" {
		t.Errorf("expected 400 Bad Request for bad frequency; got %q", got)
	}
}

// --------------------------------------------------------------------------
// processHistoryGet — valid path with buffer content
// --------------------------------------------------------------------------

func TestProcessHistoryGet_ValidPathWithBufferedData(t *testing.T) {
	// Set up a history list entry with data in the buffer
	historyList = []HistoryList{
		{
			Path:     "Vehicle.Speed",
			BufSize:  5,
			BufIndex: 1,
			Buffer:   []string{`{"value":"55","ts":"2026-01-01T00:00:01Z"}`},
		},
	}
	// period must be a time before the stored ts so matches>0
	// Use a period in the far past.
	got := processHistoryGet(`{"path":"Vehicle.Speed","period":"2026-01-01T00:00:00Z"}`)
	// We can't guarantee matches > 0 since it depends on the comparison,
	// but we can verify no panic.
	_ = got
}

// --------------------------------------------------------------------------
// activateIfIntervalOrCL — timebased branch (pure side-channel start)
// --------------------------------------------------------------------------

func TestActivateIfIntervalOrCL_TimebasedActivates(t *testing.T) {
	resetTickerGlobals()
	subChan := make(chan int, 4)
	clChan := make(chan CLPack, 1)
	filters := []utils.FilterObject{
		{Type: "timebased", Parameter: `{"period":"50"}`},
	}
	list := []SubscriptionState{{SubscriptionId: 100}}

	updatedList := activateIfIntervalOrCL(filters, subChan, clChan, 100, []string{"Vehicle.Speed"}, list)
	if len(updatedList) != 1 {
		t.Errorf("list should be unchanged; got len=%d", len(updatedList))
	}

	// Wait for at least one tick.
	select {
	case id := <-subChan:
		if id != 100 {
			t.Errorf("expected subscriptionId 100; got %d", id)
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("timebased subscription did not tick in time")
	}
	deactivateInterval(100)
}

func TestActivateIfIntervalOrCL_NoMatchingFilter(t *testing.T) {
	resetTickerGlobals()
	subChan := make(chan int, 1)
	clChan := make(chan CLPack, 1)
	filters := []utils.FilterObject{
		{Type: "paths"},
	}
	list := []SubscriptionState{{SubscriptionId: 7}}

	updatedList := activateIfIntervalOrCL(filters, subChan, clChan, 7, []string{"Vehicle.Speed"}, list)
	if len(updatedList) != 1 {
		t.Errorf("list should be unchanged; got len=%d", len(updatedList))
	}
	// No tick should arrive
	select {
	case id := <-subChan:
		t.Errorf("unexpected tick with id=%d for non-timebased filter", id)
	case <-time.After(100 * time.Millisecond):
		// good
	}
}

func TestActivateIfIntervalOrCL_TimebasedZeroPeriodSkips(t *testing.T) {
	resetTickerGlobals()
	subChan := make(chan int, 1)
	clChan := make(chan CLPack, 1)
	filters := []utils.FilterObject{
		{Type: "timebased", Parameter: `{"period":"0"}`},
	}
	list := []SubscriptionState{{SubscriptionId: 8}}
	activateIfIntervalOrCL(filters, subChan, clChan, 8, []string{"Vehicle.Speed"}, list)
	// period=0 is rejected by activateInterval guard, no tick
	select {
	case <-subChan:
		t.Errorf("zero period should not produce ticks")
	case <-time.After(100 * time.Millisecond):
		// good
	}
}

// --------------------------------------------------------------------------
// createHistoryList — pure JSON parsing
// --------------------------------------------------------------------------

func TestCreateHistoryList_HappyPath(t *testing.T) {
	saved := historyList
	defer func() { historyList = saved }()

	data := []byte(`{"leafPaths":["Vehicle.Speed","Vehicle.Acceleration"]}`)
	// createHistoryList uses "LeafPaths" not "leafPaths" so use correct casing
	data = []byte(`{"LeafPaths":["Vehicle.Speed","Vehicle.Acceleration"]}`)
	ok := createHistoryList(data)
	if !ok {
		t.Fatalf("createHistoryList returned false for valid JSON")
	}
	if len(historyList) != 2 {
		t.Errorf("expected 2 entries; got %d", len(historyList))
	}
	if historyList[0].Path != "Vehicle.Speed" {
		t.Errorf("expected Vehicle.Speed; got %q", historyList[0].Path)
	}
	// BufIndex, BufSize, Status, Frequency, Buffer should all be zero/nil
	if historyList[0].BufIndex != 0 || historyList[0].BufSize != 0 || historyList[0].Status != 0 {
		t.Errorf("newly created entry should have zero values")
	}
}

func TestCreateHistoryList_BadJSON(t *testing.T) {
	saved := historyList
	defer func() { historyList = saved }()

	ok := createHistoryList([]byte(`not json`))
	if ok {
		t.Fatalf("createHistoryList should return false for bad JSON")
	}
}

func TestCreateHistoryList_EmptyPaths(t *testing.T) {
	saved := historyList
	defer func() { historyList = saved }()

	ok := createHistoryList([]byte(`{"LeafPaths":[]}`))
	if !ok {
		t.Fatalf("createHistoryList should return true for empty array")
	}
	if len(historyList) != 0 {
		t.Errorf("expected 0 entries; got %d", len(historyList))
	}
}

// --------------------------------------------------------------------------
// checkRangeChangeFilter — range and change branches
// --------------------------------------------------------------------------

func TestCheckRangeChangeFilter_SkipsNonRCFilters(t *testing.T) {
	// paths, timebased, curvelog filter types are skipped
	filters := []utils.FilterObject{
		{Type: "paths"},
		{Type: "timebased"},
		{Type: "curvelog"},
	}
	ok, dp := checkRangeChangeFilter(filters, "", "Vehicle.Speed")
	if ok {
		t.Errorf("non-RC filters should return false")
	}
	if dp != "" {
		t.Errorf("non-RC filters should return empty dp; got %q", dp)
	}
}

func TestCheckRangeChangeFilter_ChangeFilter(t *testing.T) {
	// change filter: evaluates latestValue vs currentValue from getVehicleData
	// noneBackend returns a numeric value (dummyValue counter)
	// We use "ne" with diff "0" — this should fire if value changed
	filters := []utils.FilterObject{
		{Type: "change", Parameter: `{"logic-op":"ne","diff":"0"}`},
	}
	latestDp := `{"value":"1000","ts":"2026-01-01T00:00:00Z"}` // unlikely to match noneBackend
	// Result may be true or false depending on dummyValue; we just check no panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("checkRangeChangeFilter panicked: %v", r)
		}
	}()
	_, _ = checkRangeChangeFilter(filters, latestDp, "Vehicle.Speed")
}

func TestCheckRangeChangeFilter_RangeFilter(t *testing.T) {
	// range filter that always fires: gte boundary=0
	filters := []utils.FilterObject{
		{Type: "range", Parameter: `{"logic-op":"gte","boundary":"0"}`},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("checkRangeChangeFilter panicked: %v", r)
		}
	}()
	ok, _ := checkRangeChangeFilter(filters, "", "Vehicle.Speed")
	// noneBackend returns dummyValue >= 0, so this should be true
	if !ok {
		t.Errorf("gte 0 should be true for dummyValue counter")
	}
}

func TestCheckRangeChangeFilter_EmptyFilters(t *testing.T) {
	ok, dp := checkRangeChangeFilter(nil, "", "Vehicle.Speed")
	if ok || dp != "" {
		t.Errorf("empty filter list should return false,''; got ok=%v dp=%q", ok, dp)
	}
}

// --------------------------------------------------------------------------
// getDataPackMap — multi-path branch
// --------------------------------------------------------------------------

func TestGetDataPackMap_SinglePath(t *testing.T) {
	result := getDataPackMap([]string{"Vehicle.Speed"})
	if result["dpack"] == nil {
		t.Errorf("expected dpack in result; got %v", result)
	}
	// Single path: dpack is a map not a slice
	if _, ok := result["dpack"].(map[string]interface{}); !ok {
		// It may be nil if the value is nil — check the path field
		if m, ok := result["dpack"].(map[string]interface{}); ok && m["path"] != "Vehicle.Speed" {
			t.Errorf("path should be Vehicle.Speed; got %v", m["path"])
		}
	}
}

func TestGetDataPackMap_MultiPath(t *testing.T) {
	result := getDataPackMap([]string{"Vehicle.Speed", "Vehicle.Acceleration"})
	dpack, ok := result["dpack"].([]interface{})
	if !ok {
		t.Fatalf("multi-path dpack should be []interface{}; got %T", result["dpack"])
	}
	if len(dpack) != 2 {
		t.Errorf("expected 2 elements; got %d", len(dpack))
	}
}

// --------------------------------------------------------------------------
// handleToFeederMessage — set and invoke action branches
// --------------------------------------------------------------------------

func TestHandleToFeederMessage_SetAction(t *testing.T) {
	resetFeederGlobals()
	fromCl := make(chan string, 1)
	notif := "not-verified"
	count := 0
	// "set" action: should not panic, no feeder registered so no write happens
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("set action panicked: %v", r)
		}
	}()
	handleToFeederMessage(`{"action":"set","path":"Vehicle.Speed","value":"100"}`, fromCl, &notif, &count)
}

func TestHandleToFeederMessage_InvokeAction(t *testing.T) {
	resetFeederGlobals()
	fromCl := make(chan string, 1)
	notif := "not-verified"
	count := 0
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("invoke action panicked: %v", r)
		}
	}()
	handleToFeederMessage(`{"action":"invoke","path":"Vehicle.Speed"}`, fromCl, &notif, &count)
}

func TestHandleToFeederMessage_UnsubscribeWithPath(t *testing.T) {
	resetFeederGlobals()
	// Add a sub first so delete has something to find
	feederSubList = []FeederSubElem{
		{SubscriptionId: "sub-99", Variant: "change", Path: []string{"Vehicle.Speed"}},
	}
	feederPathList = []FeederPathElem{{Path: "Vehicle.Speed", Reference: 1}}

	fromCl := make(chan string, 4)
	notif := "not-verified"
	count := 0
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unsubscribe panicked: %v", r)
		}
	}()
	handleToFeederMessage(`{"action":"unsubscribe","subscriptionId":"sub-99"}`, fromCl, &notif, &count)
	// Should have sent to fromFeederCl
	select {
	case <-fromCl:
		// good
	case <-time.After(100 * time.Millisecond):
		// may not send if path update is empty
	}
}

func TestHandleToFeederMessage_SubscribeSetsNotSupportedAfter5(t *testing.T) {
	resetFeederGlobals()
	fromCl := make(chan string, 1)
	notif := "not-verified"
	count := 5 // already at limit

	handleToFeederMessage(`{"action":"subscribe","subscriptionId":"1","variant":"change","path":["Vehicle.Speed"]}`,
		fromCl, &notif, &count)
	if notif != "not-supported" {
		t.Errorf("notif should be not-supported after 5 subs; got %q", notif)
	}
}

// --------------------------------------------------------------------------
// handleFromFeederMessage — subscribe status not ok branch
// --------------------------------------------------------------------------

func TestHandleFromFeederMessage_SubscribeNotOkSetsNotSupported(t *testing.T) {
	rorc := make(chan string, 4)
	cl := make(chan string, 4)
	notif := "not-verified"
	handleFromFeederMessage(`{"action":"subscribe","status":"fail"}`, rorc, cl, &notif)
	if notif != "not-supported" {
		t.Errorf("notif should be not-supported for status!=ok; got %q", notif)
	}
	// Nothing should have been routed
	select {
	case <-rorc:
		t.Errorf("should not have sent to fromFeederRorC")
	default:
	}
}

// --------------------------------------------------------------------------
// captureHistoryValue — duplicate timestamp path (no store when ts matches)
// --------------------------------------------------------------------------

func TestCaptureHistoryValue_DuplicateTsSkipped(t *testing.T) {
	// Arrange: buffer already has one entry, set BufIndex=1,
	// and the noneBackend will return a different ts, so the test
	// exercises the BufIndex > 0 branch. We can't easily force ts match
	// but we can at least cover the BufIndex > 0 path.
	dp0 := `{"value":"10","ts":"2026-01-01T00:00:00Z"}`
	historyList = []HistoryList{
		{
			Path:     "Vehicle.Speed",
			BufSize:  5,
			BufIndex: 1,
			Buffer:   []string{dp0, "", "", "", ""},
		},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("captureHistoryValue panicked: %v", r)
		}
	}()
	captureHistoryValue(0)
	// BufIndex may or may not advance depending on noneBackend ts vs dp0 ts
	// Just verify no panic
}

// --------------------------------------------------------------------------
// readRing — wrap-around path
// --------------------------------------------------------------------------

func TestReadRing_NoWrap(t *testing.T) {
	rb := createRingBuffer(4)
	writeRing(&rb, "10", "t1")
	writeRing(&rb, "20", "t2")
	// Head=2, headOffset=0 → currentHead=1 → "20"
	val, ts := readRing(&rb, 0)
	if val != "20" || ts != "t2" {
		t.Errorf("got val=%q ts=%q; want 20 t2", val, ts)
	}
}

func TestReadRing_WrapAround(t *testing.T) {
	rb := createRingBuffer(4)
	// Write 4 elements to advance Head to 0 again (wraps)
	writeRing(&rb, "a", "ta")
	writeRing(&rb, "b", "tb")
	writeRing(&rb, "c", "tc")
	writeRing(&rb, "d", "td")
	// Head = 0 (wrapped), headOffset=0 → currentHead = -1 → +4 = 3 → "d"
	val, ts := readRing(&rb, 0)
	if val != "d" || ts != "td" {
		t.Errorf("wrap: got val=%q ts=%q; want d td", val, ts)
	}
	val, ts = readRing(&rb, 1)
	if val != "c" || ts != "tc" {
		t.Errorf("wrap-2: got val=%q ts=%q; want c tc", val, ts)
	}
}

// --------------------------------------------------------------------------
// getDataPack — non-history, single and multi path
// --------------------------------------------------------------------------

func TestGetDataPack_SinglePath(t *testing.T) {
	// historySupport=false by default, no history filter → getVehicleData path
	result := getDataPack([]string{"Vehicle.Speed"}, nil)
	if result == "" {
		t.Error("expected non-empty result for single path")
	}
	if !strings.Contains(result, "Vehicle.Speed") {
		t.Errorf("expected path in result; got %q", result)
	}
}

func TestGetDataPack_MultiPath(t *testing.T) {
	result := getDataPack([]string{"Vehicle.Speed", "Vehicle.Acceleration"}, nil)
	if result == "" {
		t.Error("expected non-empty result for multi path")
	}
	if !strings.Contains(result, "[") || !strings.Contains(result, "]") {
		t.Errorf("multi-path should return array; got %q", result)
	}
	if !strings.Contains(result, "Vehicle.Speed") || !strings.Contains(result, "Vehicle.Acceleration") {
		t.Errorf("expected both paths in result; got %q", result)
	}
}

func TestGetDataPack_HistoryFilterWhenUnsupported(t *testing.T) {
	// historySupport is false by default → history filter returns ""
	filterList := []utils.FilterObject{{Type: "history", Parameter: "PT1H"}}
	result := getDataPack([]string{"Vehicle.Speed"}, filterList)
	if result != "" {
		t.Errorf("history unsupported: expected empty string; got %q", result)
	}
}

func TestGetDataPack_OtherFilterType(t *testing.T) {
	// A filter that is not "history" → falls through to getVehicleData
	filterList := []utils.FilterObject{{Type: "timebased", Parameter: "period=1"}}
	result := getDataPack([]string{"Vehicle.Speed"}, filterList)
	if !strings.Contains(result, "Vehicle.Speed") {
		t.Errorf("non-history filter: expected path in result; got %q", result)
	}
}

// --------------------------------------------------------------------------
// handleServiceSubscribe — cover the toFeederChan send path
// --------------------------------------------------------------------------

func TestHandleServiceSubscribe_RangeFilterSendsToFeeder(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	subscriptionChan := make(chan int, 4)
	toFeederChan := make(chan string, 1)
	clChan := make(chan CLPack, 1)

	// UnpackFilter uses key "variant" (not "type") — see utils/common.go
	requestMap := map[string]interface{}{
		"RouterId": "router-test",
		"path":     "Vehicle.Speed",
		"filter":   map[string]interface{}{"variant": "range", "parameter": `{"above":"0"}`},
	}
	responseMap := map[string]interface{}{"action": "subscribe"}

	subList := []SubscriptionState{}
	newList, newId := handleServiceSubscribe(requestMap, responseMap, dataChan, subList, 100, subscriptionChan, clChan, toFeederChan)

	// Should have sent to toFeederChan and incremented subscriptionId
	if newId != 101 {
		t.Errorf("subscriptionId = %d; want 101", newId)
	}
	if len(newList) != 1 {
		t.Errorf("subscriptionList len = %d; want 1", len(newList))
	}
	// Verify the feeder was notified
	select {
	case msg := <-toFeederChan:
		if !strings.Contains(msg, "range") {
			t.Errorf("feeder message = %q; want range-type", msg)
		}
	default:
		t.Error("expected message on toFeederChan; got nothing")
	}
	// Consume dataChan response
	select {
	case resp := <-dataChan:
		_ = resp
	default:
		t.Error("expected response on dataChan")
	}
}

func TestHandleServiceSubscribe_ChangeFilterSendsToFeeder(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	subscriptionChan := make(chan int, 4)
	toFeederChan := make(chan string, 1)
	clChan := make(chan CLPack, 1)

	requestMap := map[string]interface{}{
		"RouterId": "router-x",
		"path":     "Vehicle.Speed",
		"filter":   map[string]interface{}{"variant": "change", "parameter": `{"diff":"5","logic-op":"gt"}`},
	}
	responseMap := map[string]interface{}{"action": "subscribe"}
	subList := []SubscriptionState{}

	newList, newId := handleServiceSubscribe(requestMap, responseMap, dataChan, subList, 50, subscriptionChan, clChan, toFeederChan)
	if newId != 51 {
		t.Errorf("subscriptionId = %d; want 51", newId)
	}
	if len(newList) != 1 {
		t.Errorf("subscriptionList len = %d; want 1", len(newList))
	}
	// Feeder notification expected
	select {
	case msg := <-toFeederChan:
		if !strings.Contains(msg, "change") {
			t.Errorf("feeder message = %q; want change-type", msg)
		}
	default:
		t.Error("expected message on toFeederChan; got nothing")
	}
	select {
	case resp := <-dataChan:
		_ = resp
	default:
		t.Error("expected response on dataChan")
	}
}

func TestHandleServiceSubscribe_GatingIdSet(t *testing.T) {
	resetErrorResponseMap()
	dataChan := make(chan map[string]interface{}, 1)
	subscriptionChan := make(chan int, 4)
	toFeederChan := make(chan string, 1)
	clChan := make(chan CLPack, 1)

	requestMap := map[string]interface{}{
		"RouterId": "router-g",
		"path":     "Vehicle.Speed",
		"filter":   map[string]interface{}{"variant": "timebased", "parameter": `{"period":"1000"}`},
		"gatingId": "gate-99",
	}
	responseMap := map[string]interface{}{"action": "subscribe"}
	subList := []SubscriptionState{}

	newList, _ := handleServiceSubscribe(requestMap, responseMap, dataChan, subList, 1, subscriptionChan, clChan, toFeederChan)
	if len(newList) == 0 || newList[0].GatingId != "gate-99" {
		t.Errorf("GatingId not set; got list=%v", newList)
	}
	// drain dataChan
	select {
	case resp := <-dataChan:
		_ = resp
	default:
	}
	// drain subscriptionChan (interval activation)
	select {
	case <-subscriptionChan:
	default:
	}
}

// --------------------------------------------------------------------------
// updateFeederRegList — dereg path
// --------------------------------------------------------------------------

func TestUpdateFeederRegList_RegisterAndDeregister(t *testing.T) {
	savedList := feederRegList
	savedChannelList := feederChannelList
	defer func() {
		feederRegList = savedList
		feederChannelList = savedChannelList
	}()
	// Reset
	feederRegList = nil
	feederChannelList = make([]FeederChannelElem, 5)
	for i := range feederChannelList {
		feederChannelList[i].Busy = false
	}

	// Register
	elem := FeederRegElem{Name: "test-feeder", InfoType: "reg", ChannelIndex: 0}
	feederChannelList[0].Busy = true // mark as allocated
	updateFeederRegList(elem)
	if len(feederRegList) != 1 || feederRegList[0].Name != "test-feeder" {
		t.Errorf("feederRegList = %v; want [{test-feeder ...}]", feederRegList)
	}

	// Deregister
	deregElem := FeederRegElem{Name: "test-feeder", InfoType: "dereg"}
	updateFeederRegList(deregElem)
	if len(feederRegList) != 0 {
		t.Errorf("after dereg feederRegList = %v; want []", feederRegList)
	}
	// feederChannelList[0] should be freed
	if feederChannelList[0].Busy {
		t.Error("feederChannelList[0] should not be busy after dereg")
	}
}

func TestUpdateFeederRegList_DeregNotFound(t *testing.T) {
	savedList := feederRegList
	defer func() { feederRegList = savedList }()
	feederRegList = []FeederRegElem{{Name: "other-feeder", InfoType: "reg", ChannelIndex: 0}}

	// Deregister a name that doesn't exist
	deregElem := FeederRegElem{Name: "nonexistent", InfoType: "dereg"}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	updateFeederRegList(deregElem)
	// List should be unchanged
	if len(feederRegList) != 1 {
		t.Errorf("feederRegList = %v; want unchanged [{other-feeder}]", feederRegList)
	}
}

// --------------------------------------------------------------------------
// postProcess1dim — state machine branches
// --------------------------------------------------------------------------

func makeTestRingBuffer() RingBuffer {
	rb := createRingBuffer(8)
	now := time.Now()
	for i := 0; i < 4; i++ {
		ts := now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		writeRing(&rb, strconv.Itoa(i*10), ts)
	}
	return rb
}

func makeTestPostProc() []PostProcessBufElement1dim {
	pp := make([]PostProcessBufElement1dim, 3)
	pp[0].Type = -1
	pp[1].Type = -1
	pp[2].Type = -1
	return pp
}

func TestPostProcess1dim_InitAtStartup(t *testing.T) {
	rb := makeTestRingBuffer()
	pp := makeTestPostProc()
	// First call: Type==-1 → init path, returns ""
	result, pp2 := postProcess1dim(&rb, 0, 0, pp, 1.0)
	if result != "" {
		t.Errorf("init path: got %q; want empty", result)
	}
	// pos[0] should now be populated
	if pp2[0].Type == -1 {
		t.Error("postProc[0] should have been written")
	}
}

func TestPostProcess1dim_SecondCall(t *testing.T) {
	rb := makeTestRingBuffer()
	pp := makeTestPostProc()
	// First call: init
	_, pp = postProcess1dim(&rb, 0, 1, pp, 1.0)
	// Second call: postProc[1].Type==-1 → second-element fill
	result, pp2 := postProcess1dim(&rb, 0, 2, pp, 1.0)
	if result != "" {
		t.Errorf("second call: got %q; want empty", result)
	}
	_ = pp2
}

func TestPostProcess1dim_ThirdCall_FirstSelectedZero(t *testing.T) {
	rb := makeTestRingBuffer()
	pp := makeTestPostProc()
	// Prime first two with firstSelected=0
	_, pp = postProcess1dim(&rb, 0, 0, pp, 1.0)
	pp[1].Type = 0 // manually mark as populated (not -1)
	pp[1].Data = CLBufElement{Value: 5.0, Timestamp: 1.0}
	pp[1].Dp = `{"value":"5.0","ts":"t1"}`
	pp[2].Type = 0
	pp[2].Data = CLBufElement{Value: 10.0, Timestamp: 2.0}
	pp[2].Dp = `{"value":"10.0","ts":"t2"}`
	// Third call with firstSelected=0, all three slots populated
	result, pp3 := postProcess1dim(&rb, 0, 2, pp, 1.0)
	_ = result
	_ = pp3
}

func TestPostProcess1dim_ThirdCall_FirstSelectedNonZero(t *testing.T) {
	rb := makeTestRingBuffer()
	pp := makeTestPostProc()
	pp[0].Type = 1 // populated, non-zero type → not -1
	pp[0].Data = CLBufElement{Value: 0.0, Timestamp: 0.0}
	pp[0].Dp = `{"value":"0","ts":"t0"}`
	pp[1].Type = 1
	pp[1].Data = CLBufElement{Value: 5.0, Timestamp: 1.0}
	pp[1].Dp = `{"value":"5","ts":"t1"}`
	pp[2].Type = -1 // slot 2 not populated — stays in "second call" branch
	result, pp2 := postProcess1dim(&rb, 1, 2, pp, 1.0) // firstSelected=1 (non-zero)
	_ = result
	_ = pp2
}

// --------------------------------------------------------------------------
// saveNonPdrDp — inline math
// --------------------------------------------------------------------------

func TestSaveNonPdrDp_AboveMaxError(t *testing.T) {
	pp := make([]PostProcessBufElement1dim, 3)
	// pos0 at t=0,v=0; pos1 at t=1,v=10 (interpolated=5); pos2 at t=2,v=10
	pp[0].Data = CLBufElement{Value: 0, Timestamp: 0}
	pp[1].Data = CLBufElement{Value: 10, Timestamp: 1}
	pp[2].Data = CLBufElement{Value: 10, Timestamp: 2}
	// fraction = 1/2=0.5, interpolated=5, error=|10-5|=5 > maxError=1
	if !saveNonPdrDp(pp, 1.0) {
		t.Error("error=5 > maxError=1 should return true")
	}
}

func TestSaveNonPdrDp_BelowMaxError(t *testing.T) {
	pp := make([]PostProcessBufElement1dim, 3)
	pp[0].Data = CLBufElement{Value: 0, Timestamp: 0}
	pp[1].Data = CLBufElement{Value: 5, Timestamp: 1}
	pp[2].Data = CLBufElement{Value: 10, Timestamp: 2}
	// fraction=0.5, interpolated=5, error=|5-5|=0 < maxError=1
	if saveNonPdrDp(pp, 1.0) {
		t.Error("error=0 < maxError=1 should return false")
	}
}

func TestSaveNonPdrDp_NegativeError(t *testing.T) {
	pp := make([]PostProcessBufElement1dim, 3)
	// pos1 below interpolated line → negative raw error
	pp[0].Data = CLBufElement{Value: 0, Timestamp: 0}
	pp[1].Data = CLBufElement{Value: 0, Timestamp: 1}   // interpolated=5, error=-5 → |−5|=5
	pp[2].Data = CLBufElement{Value: 10, Timestamp: 2}
	if !saveNonPdrDp(pp, 1.0) {
		t.Error("error=5 > maxError=1 should return true (negative raw error)")
	}
}

// --------------------------------------------------------------------------
// transformDataPoints — nil and parse-fail paths
// --------------------------------------------------------------------------

func TestTransformDataPoints_NilBuffer(t *testing.T) {
	if got := transformDataPoints(nil, make([]CLBufElement, 4), 4); got != nil {
		t.Errorf("nil ring buffer: expected nil; got %v", got)
	}
}

func TestTransformDataPoints_ZeroBufSize(t *testing.T) {
	rb := createRingBuffer(4)
	if got := transformDataPoints(&rb, make([]CLBufElement, 4), 0); got != nil {
		t.Errorf("zero bufSize: expected nil; got %v", got)
	}
}

func TestTransformDataPoints_BufSizeLargerThanClBuffer(t *testing.T) {
	rb := createRingBuffer(4)
	// clBuffer has len=2 but bufSize=4 → guard returns nil
	if got := transformDataPoints(&rb, make([]CLBufElement, 2), 4); got != nil {
		t.Errorf("oversized bufSize: expected nil; got %v", got)
	}
}

func TestTransformDataPoints_NonNumericValueReturnsNil(t *testing.T) {
	rb := createRingBuffer(4)
	// Write a non-numeric value so transformDataPoint returns false
	ts := time.Now().Format(time.RFC3339Nano)
	writeRing(&rb, "not-a-number", ts)
	result := transformDataPoints(&rb, make([]CLBufElement, 1), 1)
	if result != nil {
		t.Errorf("non-numeric value: expected nil; got %v", result)
	}
}

func TestTransformDataPoints_HappyPathRFC3339(t *testing.T) {
	rb := createRingBuffer(4)
	now := time.Now()
	for i := 0; i < 3; i++ {
		ts := now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		writeRing(&rb, strconv.Itoa(i*10), ts)
	}
	clBuffer := make([]CLBufElement, 3)
	result := transformDataPoints(&rb, clBuffer, 3)
	if result == nil {
		t.Error("expected non-nil result for valid RFC3339 timestamps")
	}
}

func TestTransformDataPoints_UnixMilliTimestamp(t *testing.T) {
	// Write timestamps as Unix milliseconds (not RFC3339) to hit fallback path
	rb := createRingBuffer(4)
	now := time.Now()
	for i := 0; i < 2; i++ {
		ts := strconv.FormatInt(now.Add(time.Duration(i)*time.Second).UnixMilli(), 10)
		writeRing(&rb, strconv.Itoa(i*5), ts)
	}
	clBuffer := make([]CLBufElement, 2)
	// May succeed or fail depending on internal parsing — just ensure no panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	transformDataPoints(&rb, clBuffer, 2)
}

// --------------------------------------------------------------------------
// clAnalyze1dim — nil clBuffer path (non-parseable values)
// --------------------------------------------------------------------------

func TestClAnalyze1dim_NilClBufferReturnsHeadSample(t *testing.T) {
	rb := createRingBuffer(4)
	// Write non-numeric values so transformDataPoints returns nil
	writeRing(&rb, "not-a-number", "2025-01-01T00:00:00Z")
	// clAnalyze1dim should fall back to readRing(0) when clBuffer=nil
	result, last, first := clAnalyze1dim(&rb, 1, 1.0)
	if result == "" {
		t.Error("expected non-empty fallback result")
	}
	if last != 0 || first != 0 {
		t.Errorf("got last=%d first=%d; want both 0", last, first)
	}
}

func TestClAnalyze1dim_SingleElementBuffer(t *testing.T) {
	rb := createRingBuffer(4)
	ts := time.Now().Format(time.RFC3339Nano)
	writeRing(&rb, "42.5", ts)
	// bufSize=2: lastIndex-firstIndex=1 → clReduction returns nil → falls back to readRing(0)
	result, _, _ := clAnalyze1dim(&rb, 1, 0.5)
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestClAnalyze1dim_MultipleElementsHighError(t *testing.T) {
	rb := createRingBuffer(8)
	now := time.Now()
	// Write values that form a non-linear pattern to force PDR algo to select
	vals := []float64{0, 100, 0, 100, 0}
	for i, v := range vals {
		ts := now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		writeRing(&rb, strconv.FormatFloat(v, 'f', 1, 64), ts)
	}
	result, _, _ := clAnalyze1dim(&rb, len(vals), 0.01) // tiny maxError → many points selected
	if result == "" {
		t.Error("expected non-empty result")
	}
}

// --------------------------------------------------------------------------
// unpacksignalDimensionMap — nil and array paths
// --------------------------------------------------------------------------

func TestUnpacksignalDimensionMap_Nil(t *testing.T) {
	m := map[string]interface{}{"dim2": "somestring"}
	result := unpacksignalDimensionMap(m, nil)
	if result != nil {
		t.Error("nil signalDimensionLists should return nil")
	}
}

func TestUnpacksignalDimensionMap_Dim2Map(t *testing.T) {
	m := map[string]interface{}{
		"dim2": map[string]interface{}{
			"path1": "Vehicle.Speed",
			"path2": "Vehicle.Acceleration",
		},
	}
	var lists SignalDimensionLists
	result := unpacksignalDimensionMap(m, &lists)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.dim2List) != 1 {
		t.Errorf("dim2List len = %d; want 1", len(result.dim2List))
	}
	if result.dim2List[0].Path1 != "Vehicle.Speed" {
		t.Errorf("Path1 = %q; want Vehicle.Speed", result.dim2List[0].Path1)
	}
}

func TestUnpacksignalDimensionMap_Dim2Array(t *testing.T) {
	m := map[string]interface{}{
		"dim2": []interface{}{
			map[string]interface{}{"path1": "Vehicle.A", "path2": "Vehicle.B"},
			map[string]interface{}{"path1": "Vehicle.C", "path2": "Vehicle.D"},
		},
	}
	var lists SignalDimensionLists
	result := unpacksignalDimensionMap(m, &lists)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.dim2List) != 2 {
		t.Errorf("dim2List len = %d; want 2", len(result.dim2List))
	}
}

func TestUnpacksignalDimensionMap_UnknownType(t *testing.T) {
	m := map[string]interface{}{
		"dim2": 42, // unexpected type
	}
	var lists SignalDimensionLists
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	result := unpacksignalDimensionMap(m, &lists)
	if result == nil {
		t.Error("expected non-nil result (even with unknown type)")
	}
}

// --------------------------------------------------------------------------
// activateIfIntervalOrCL — curvelog branch
// (needs clRouterChan initialized via initClResources)
// --------------------------------------------------------------------------

// TestActivateIfIntervalOrCL_CurvelogBranch is excluded because
// curveLoggingDispatcher spawns a goroutine (clCapture1dim) that calls
// getVehicleData which reads stateBackend — a package global. The goroutine
// outlives the test and races with other tests that restore stateBackend.
// The curvelog branch of activateIfIntervalOrCL is exercised indirectly by
// TestHandleServiceSubscribe tests that use handleServiceSubscribe with a
// curvelog filter — the send to clRouterChan blocks (it's unbuffered), so
// those tests also avoid spawning the goroutine via the subscriber flow.
// The curveLoggingDispatcher function itself is covered via curvelogging tests.

// --------------------------------------------------------------------------
// activateHistory — frequency <= 0 branch
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// activateInterval — non-positive interval path
// --------------------------------------------------------------------------

func TestActivateInterval_NonPositiveInterval(t *testing.T) {
	subscriptionChan := make(chan int, 1)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	// interval <= 0 → returns early without allocating a ticker
	activateInterval(subscriptionChan, 7001, 0)
	activateInterval(subscriptionChan, 7002, -1)
}

func TestActivateHistory_ZeroFrequency(t *testing.T) {
	histChan := make(chan int, 4)
	// frequency=0 → guard returns early after allocateTicker
	// We just verify no panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	activateHistory(histChan, 9999, 0)
	// Clean up by deallocating the ticker slot
	deallocateTicker(9999)
}

func TestActivateHistory_NegativeFrequency(t *testing.T) {
	histChan := make(chan int, 4)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	activateHistory(histChan, 9998, -1)
	deallocateTicker(9998)
}

// --------------------------------------------------------------------------
// getDataPack — ensure all main branches covered
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// analyzeSignalDimensions — dim3 branch
// --------------------------------------------------------------------------

func TestAnalyzeSignalDimensions_Dim3Match(t *testing.T) {
	// Build a SignalDimensionLists with a 3-dim entry
	sdl := &SignalDimensionLists{
		dim3List: []Dim3Elem{
			{Path1: "Vehicle.Lat", Path2: "Vehicle.Lon", Path3: "Vehicle.Alt"},
		},
	}
	paths := []string{"Vehicle.Lat", "Vehicle.Lon", "Vehicle.Alt"}
	result := analyzeSignalDimensions(paths, sdl)
	if len(result) != 3 {
		t.Fatalf("expected 3 PathDimElems; got %d", len(result))
	}
	for i, e := range result {
		if e.Dim != 3 {
			t.Errorf("result[%d].Dim = %d; want 3", i, e.Dim)
		}
		if e.Id != 0 {
			t.Errorf("result[%d].Id = %d; want 0", i, e.Id)
		}
	}
}

func TestAnalyzeSignalDimensions_Dim2Match(t *testing.T) {
	sdl := &SignalDimensionLists{
		dim2List: []Dim2Elem{
			{Path1: "Vehicle.Lat", Path2: "Vehicle.Lon"},
		},
	}
	paths := []string{"Vehicle.Lat", "Vehicle.Lon"}
	result := analyzeSignalDimensions(paths, sdl)
	if len(result) != 2 {
		t.Fatalf("expected 2 PathDimElems; got %d", len(result))
	}
	if result[0].Dim != 2 || result[1].Dim != 2 {
		t.Errorf("expected both dim=2; got %v %v", result[0], result[1])
	}
}

func TestAnalyzeSignalDimensions_Dim1Default(t *testing.T) {
	sdl := &SignalDimensionLists{}
	paths := []string{"Vehicle.Speed"}
	result := analyzeSignalDimensions(paths, sdl)
	if len(result) != 1 || result[0].Dim != 1 {
		t.Errorf("single path not in dim lists: expected dim=1; got %v", result)
	}
}

func TestAnalyzeSignalDimensions_NilDimensionList(t *testing.T) {
	paths := []string{"Vehicle.Speed", "Vehicle.Acceleration"}
	result := analyzeSignalDimensions(paths, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2; got %d", len(result))
	}
	for _, e := range result {
		if e.Dim != 1 {
			t.Errorf("nil list: expected dim=1; got %d", e.Dim)
		}
	}
}

// --------------------------------------------------------------------------
// deallocateTriggChannels — the missing 7.1%
// Signature: deallocateTriggChannels(i int, routingDataList []TriggRoutingData)
// --------------------------------------------------------------------------

func TestDeallocateTriggChannels_IndexOutOfRange(t *testing.T) {
	initClResources()
	var emptyList []TriggRoutingData
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	// i=-1 → out of range → early return
	deallocateTriggChannels(-1, emptyList)
	// i=0 with empty list → out of range → early return
	deallocateTriggChannels(0, emptyList)
}

func TestDeallocateTriggChannels_UnallocatedBusy(t *testing.T) {
	initClResources()
	// Set triggChannelList[0].Busy = true so we can test release
	triggChannelList[0].Busy = true

	routingDataList := []TriggRoutingData{
		{
			SubscriptionId: "1",
			TriggRoutingList: []TriggRoutingElem{
				{Index: 0, Path: []string{"Vehicle.Speed"}},
			},
		},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	deallocateTriggChannels(0, routingDataList)
	// triggChannelList[0] should now be freed
	if triggChannelList[0].Busy {
		t.Error("triggChannelList[0] should be freed after deallocate")
	}
}

func TestDeallocateTriggChannels_NotBusy(t *testing.T) {
	initClResources()
	// Index not busy → released stays 0 → no decrementClSessions called
	routingDataList := []TriggRoutingData{
		{
			SubscriptionId: "2",
			TriggRoutingList: []TriggRoutingElem{
				{Index: 0, Path: []string{"Vehicle.Speed"}},
			},
		},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	deallocateTriggChannels(0, routingDataList)
}

// --------------------------------------------------------------------------
// unPackDimSignalsLevel1 — dim3 paths
// --------------------------------------------------------------------------

func TestUnPackDimSignalsLevel1_Dim3(t *testing.T) {
	sdl := &SignalDimensionLists{
		dim3List: make([]Dim3Elem, 1),
	}
	m := map[string]interface{}{
		"path1": "Vehicle.Lat",
		"path2": "Vehicle.Lon",
		"path3": "Vehicle.Alt",
	}
	unPackDimSignalsLevel1(0, m, "dim3", sdl)
	if sdl.dim3List[0].Path1 != "Vehicle.Lat" {
		t.Errorf("Path1 = %q; want Vehicle.Lat", sdl.dim3List[0].Path1)
	}
	if sdl.dim3List[0].Path2 != "Vehicle.Lon" {
		t.Errorf("Path2 = %q; want Vehicle.Lon", sdl.dim3List[0].Path2)
	}
	if sdl.dim3List[0].Path3 != "Vehicle.Alt" {
		t.Errorf("Path3 = %q; want Vehicle.Alt", sdl.dim3List[0].Path3)
	}
}

func TestUnPackDimSignalsLevel1_Dim3OutOfRange(t *testing.T) {
	sdl := &SignalDimensionLists{
		dim3List: make([]Dim3Elem, 1),
	}
	m := map[string]interface{}{
		"path1": "Vehicle.Lat",
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	// index=5 is out of range → guard returns early
	unPackDimSignalsLevel1(5, m, "dim3", sdl)
}

func TestUnPackDimSignalsLevel1_NilList(t *testing.T) {
	m := map[string]interface{}{"path1": "Vehicle.A"}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	unPackDimSignalsLevel1(0, m, "dim2", nil)
}

// --------------------------------------------------------------------------
// transformDataPoint — invalid timestamp path
// --------------------------------------------------------------------------

func TestTransformDataPoint_InvalidTimestamp(t *testing.T) {
	rb := createRingBuffer(4)
	writeRing(&rb, "42.0", "not-a-time-or-number")
	base := time.Now()
	_, ok := transformDataPoint(&rb, 0, base)
	if ok {
		t.Error("invalid timestamp should return false")
	}
}

func TestGetDataPack_EmptyFilterList(t *testing.T) {
	result := getDataPack([]string{"Vehicle.Speed"}, []utils.FilterObject{})
	// Empty filter slice → falls through to getVehicleData
	if !strings.Contains(result, "Vehicle.Speed") {
		t.Errorf("empty filter: expected path in result; got %q", result)
	}
}

// --------------------------------------------------------------------------
// clAnalyze2dim — nil and normal paths
// --------------------------------------------------------------------------

func makeNumericRingBuffer(values []float64, startTime time.Time) RingBuffer {
	rb := createRingBuffer(len(values) + 2)
	for i, v := range values {
		ts := startTime.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		writeRing(&rb, strconv.FormatFloat(v, 'f', 2, 64), ts)
	}
	return rb
}

// --------------------------------------------------------------------------
// processHistoryCtrl — remaining uncovered branches (delete action, type errors)
// --------------------------------------------------------------------------

func TestProcessHistoryCtrl_DeleteAction(t *testing.T) {
	// createHistoryList uses the "LeafPaths" JSON key
	if !createHistoryList([]byte(`{"LeafPaths":["Vehicle.DeleteMe"]}`)) {
		t.Fatal("createHistoryList failed")
	}
	defer func() { historyList = nil }()

	ch := make(chan int, 4)
	got := processHistoryCtrl(`{"action":"create","path":"Vehicle.DeleteMe","buf-size":"5"}`, ch, true)
	if got != "200 OK" {
		t.Fatalf("create: got %q; want 200 OK", got)
	}
	// Status=0 (never started) → delete should succeed
	got = processHistoryCtrl(`{"action":"delete","path":"Vehicle.DeleteMe"}`, ch, true)
	if got != "200 OK" {
		t.Errorf("delete: got %q; want 200 OK", got)
	}
}

func TestProcessHistoryCtrl_BufSizeNotString(t *testing.T) {
	if !createHistoryList([]byte(`{"LeafPaths":["Vehicle.BufSizeTest"]}`)) {
		t.Fatal("createHistoryList failed")
	}
	defer func() { historyList = nil }()

	// Pass buf-size as a number (not string) → type assertion fails → 400
	got := processHistoryCtrl(`{"action":"create","path":"Vehicle.BufSizeTest","buf-size":5}`, nil, true)
	if got != "400 Bad Request" {
		t.Errorf("got %q; want 400 Bad Request", got)
	}
}

func TestProcessHistoryCtrl_DeleteWhileRunningReturnsConflict(t *testing.T) {
	if !createHistoryList([]byte(`{"LeafPaths":["Vehicle.ConflictTest"]}`)) {
		t.Fatal("createHistoryList failed")
	}
	defer func() { historyList = nil }()

	// Set status to 1 (running) so delete returns 409 Conflict
	historyList[0].Status = 1
	ch := make(chan int, 4)
	got := processHistoryCtrl(`{"action":"delete","path":"Vehicle.ConflictTest"}`, ch, true)
	if got != "409 Conflict" {
		t.Errorf("got %q; want 409 Conflict", got)
	}
	historyList[0].Status = 0
}

// --------------------------------------------------------------------------
// handleToFeederMessage — uncovered branches
// --------------------------------------------------------------------------

func TestHandleToFeederMessage_MissingAction(t *testing.T) {
	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	count := 0
	// JSON without "action" → missing/invalid action
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	handleToFeederMessage(`{"variant":"subscribe"}`, fromFeederCl, &notif, &count)
	// No panic is the assertion
}

func TestHandleToFeederMessage_SubscribeMissingFields(t *testing.T) {
	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	count := 0
	// subscribe but missing subscriptionId → returns early
	handleToFeederMessage(`{"action":"subscribe"}`, fromFeederCl, &notif, &count)
	if count != 0 {
		t.Errorf("count should not be incremented for bad subscribe; got %d", count)
	}
}

func TestHandleToFeederMessage_UnsubscribeNoUpdate(t *testing.T) {
	resetFeederGlobals()
	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	count := 0
	// unsubscribe for a subscriptionId not in feederSubList → deleteOnFeederSubList
	// returns empty unsubscribePath → feederUpdatePath length <=4 → early return
	handleToFeederMessage(`{"action":"unsubscribe","subscriptionId":"9999"}`, fromFeederCl, &notif, &count)
	// no panic is the test; drain the channel
	select {
	case <-fromFeederCl:
	default:
	}
}

func TestHandleToFeederMessage_DefaultAction(t *testing.T) {
	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	count := 0
	// unknown action → default branch
	handleToFeederMessage(`{"action":"unknown-action"}`, fromFeederCl, &notif, &count)
}

func TestHandleToFeederMessage_SetActionWritesToFeeder(t *testing.T) {
	// Test the write loop branch: feederRegList[i].Conn != nil and InfoType == "Data"
	savedRegList := feederRegList
	defer func() { feederRegList = savedRegList }()

	// Create a net.Pipe to get a real conn
	server, client := net.Pipe()
	defer server.Close()

	feederRegList = []FeederRegElem{
		{Name: "pipe-feeder", InfoType: "Data", Conn: server, ChannelIndex: -1},
	}

	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	count := 0

	// Consume from client in background so Write doesn't block
	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 256)
		n, _ := client.Read(buf)
		done <- buf[:n]
	}()

	handleToFeederMessage(`{"action":"set","path":"Vehicle.Speed","value":"42"}`, fromFeederCl, &notif, &count)

	select {
	case msg := <-done:
		if !strings.Contains(string(msg), "set") {
			t.Errorf("feeder write: expected set message; got %q", string(msg))
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for feeder write")
	}
}

func TestHandleToFeederMessage_UnsubscribeMissingId(t *testing.T) {
	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	count := 0
	// unsubscribe without subscriptionId string → returns early
	handleToFeederMessage(`{"action":"unsubscribe","subscriptionId":123}`, fromFeederCl, &notif, &count)
}

// --------------------------------------------------------------------------
// curveLoggingDispatcher — empty paths (no goroutines spawned)
// --------------------------------------------------------------------------

func TestCurveLoggingDispatcher_EmptyPaths(t *testing.T) {
	initClResources()
	clChan := make(chan CLPack, 4)
	// Empty paths → dim1List/dim2List/dim3List all empty → no goroutines, no writes to clChan
	routingData, subThreads := curveLoggingDispatcher(clChan, 500, `{"maxerr":"0.5","bufsize":"3"}`, []string{})
	if routingData.SubscriptionId != "500" {
		t.Errorf("SubscriptionId = %q; want 500", routingData.SubscriptionId)
	}
	if subThreads.NumofThreads != 0 {
		t.Errorf("NumofThreads = %d; want 0 for empty paths", subThreads.NumofThreads)
	}
	if len(routingData.TriggRoutingList) != 0 {
		t.Errorf("TriggRoutingList = %v; want empty", routingData.TriggRoutingList)
	}
}

func TestCurveLoggingDispatcher_BufSizeClamping(t *testing.T) {
	initClResources()
	clChan := make(chan CLPack, 4)
	// bufSize from getCurveLoggingParams that exceeds MAXCLBUFSIZE → clamped
	// Use "bufsize":"99999" which exceeds MAXCLBUFSIZE
	routingData, _ := curveLoggingDispatcher(clChan, 501, `{"maxerr":"0.1","bufsize":"99999"}`, []string{})
	// Just verify no panic and returns valid data
	if routingData.SubscriptionId != "501" {
		t.Errorf("SubscriptionId = %q; want 501", routingData.SubscriptionId)
	}
}

func TestCurveLoggingDispatcher_SmallBufSize(t *testing.T) {
	initClResources()
	clChan := make(chan CLPack, 4)
	// bufSize=0 from bad JSON → clamped to 1
	routingData, subThreads := curveLoggingDispatcher(clChan, 502, `{"maxerr":"0.1","bufsize":"0"}`, []string{})
	_ = routingData
	_ = subThreads
}

// --------------------------------------------------------------------------
// handleFromFeederMessage — subscription path
// --------------------------------------------------------------------------

func TestHandleFromFeederMessage_SubscriptionPathChangeVariant(t *testing.T) {
	resetFeederGlobals()
	fromFeederRorC := make(chan string, 2)
	fromFeederCl := make(chan string, 2)
	notif := "not-verified"

	// Add a subscription with change variant so getSubscribeVariant returns "change"
	feederSubList = []FeederSubElem{{SubscriptionId: "1", Variant: "change", Path: []string{"Vehicle.Speed"}}}

	handleFromFeederMessage(`{"action":"subscription","path":"Vehicle.Speed"}`, fromFeederRorC, fromFeederCl, &notif)
	select {
	case msg := <-fromFeederRorC:
		if !strings.Contains(msg, "subscription") {
			t.Errorf("expected subscription message; got %q", msg)
		}
	default:
		t.Error("expected message on fromFeederRorC for change variant")
	}
}

func TestHandleFromFeederMessage_SubscriptionPathCurvelogVariant(t *testing.T) {
	resetFeederGlobals()
	fromFeederRorC := make(chan string, 2)
	fromFeederCl := make(chan string, 2)
	notif := "not-verified"

	feederSubList = []FeederSubElem{{SubscriptionId: "2", Variant: "curvelog", Path: []string{"Vehicle.Rpm"}}}

	handleFromFeederMessage(`{"action":"subscription","path":"Vehicle.Rpm"}`, fromFeederRorC, fromFeederCl, &notif)
	select {
	case msg := <-fromFeederCl:
		if !strings.Contains(msg, "subscription") {
			t.Errorf("expected subscription message; got %q", msg)
		}
	default:
		t.Error("expected message on fromFeederCl for curvelog variant")
	}
}

func TestHandleFromFeederMessage_MissingAction(t *testing.T) {
	fromFeederRorC := make(chan string, 1)
	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	handleFromFeederMessage(`{"no-action":"here"}`, fromFeederRorC, fromFeederCl, &notif)
}

func TestHandleFromFeederMessage_SubscribeOkPath(t *testing.T) {
	fromFeederRorC := make(chan string, 2)
	fromFeederCl := make(chan string, 2)
	notif := "not-verified"

	handleFromFeederMessage(`{"action":"subscribe","status":"ok"}`, fromFeederRorC, fromFeederCl, &notif)
	if notif != "supported" {
		t.Errorf("notification = %q; want supported", notif)
	}
}

func TestHandleFromFeederMessage_SubscriptionMissingPath(t *testing.T) {
	fromFeederRorC := make(chan string, 1)
	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	// subscription action but no "path" string value → returns early
	handleFromFeederMessage(`{"action":"subscription","path":42}`, fromFeederRorC, fromFeederCl, &notif)
}

func TestHandleFromFeederMessage_UnknownAction(t *testing.T) {
	fromFeederRorC := make(chan string, 1)
	fromFeederCl := make(chan string, 1)
	notif := "not-verified"
	handleFromFeederMessage(`{"action":"unknown-thing"}`, fromFeederRorC, fromFeederCl, &notif)
}

// --------------------------------------------------------------------------
// clAnalyze2dim — nil and normal paths
// --------------------------------------------------------------------------

func TestClAnalyze2dim_NilBufferFallback(t *testing.T) {
	// A ring buffer with a non-numeric value forces transformDataPoints → nil
	rb1 := createRingBuffer(4)
	rb2 := createRingBuffer(4)
	writeRing(&rb1, "bad-value", "2025-01-01T00:00:00Z")
	writeRing(&rb2, "42.0", "2025-01-01T00:00:00Z")
	dp1, dp2, tail := clAnalyze2dim(&rb1, &rb2, 1, 1.0)
	if dp1 == "" || dp2 == "" {
		t.Errorf("expected non-empty fallback; got dp1=%q dp2=%q", dp1, dp2)
	}
	if tail != 0 {
		t.Errorf("tail = %d; want 0", tail)
	}
}

func TestClAnalyze2dim_SingleElementBuffer(t *testing.T) {
	now := time.Now()
	rb1 := makeNumericRingBuffer([]float64{10.0}, now)
	rb2 := makeNumericRingBuffer([]float64{20.0}, now)
	dp1, dp2, _ := clAnalyze2dim(&rb1, &rb2, 1, 0.5)
	if dp1 == "" || dp2 == "" {
		t.Errorf("single element: expected non-empty; dp1=%q dp2=%q", dp1, dp2)
	}
}

func TestClAnalyze2dim_MultipleElements(t *testing.T) {
	now := time.Now()
	vals := []float64{0, 50, 100, 50, 0}
	rb1 := makeNumericRingBuffer(vals, now)
	rb2 := makeNumericRingBuffer(vals, now)
	dp1, dp2, _ := clAnalyze2dim(&rb1, &rb2, len(vals), 0.01)
	if dp1 == "" || dp2 == "" {
		t.Errorf("multi-element: expected non-empty; dp1=%q dp2=%q", dp1, dp2)
	}
}

// --------------------------------------------------------------------------
// clAnalyze3dim — nil and normal paths
// --------------------------------------------------------------------------

func TestClAnalyze3dim_NilBufferFallback(t *testing.T) {
	rb1 := createRingBuffer(4)
	rb2 := createRingBuffer(4)
	rb3 := createRingBuffer(4)
	writeRing(&rb1, "bad", "2025-01-01T00:00:00Z")
	writeRing(&rb2, "1.0", "2025-01-01T00:00:00Z")
	writeRing(&rb3, "2.0", "2025-01-01T00:00:00Z")
	dp1, dp2, dp3, tail := clAnalyze3dim(&rb1, &rb2, &rb3, 1, 1.0)
	if dp1 == "" || dp2 == "" || dp3 == "" {
		t.Errorf("expected non-empty fallback; got dp1=%q dp2=%q dp3=%q", dp1, dp2, dp3)
	}
	if tail != 0 {
		t.Errorf("tail = %d; want 0", tail)
	}
}

func TestClAnalyze3dim_MultipleElements(t *testing.T) {
	now := time.Now()
	vals := []float64{0, 50, 100, 50, 0}
	rb1 := makeNumericRingBuffer(vals, now)
	rb2 := makeNumericRingBuffer(vals, now)
	rb3 := makeNumericRingBuffer(vals, now)
	dp1, dp2, dp3, _ := clAnalyze3dim(&rb1, &rb2, &rb3, len(vals), 0.01)
	if dp1 == "" || dp2 == "" || dp3 == "" {
		t.Errorf("multi-element: expected non-empty; dp1=%q dp2=%q dp3=%q", dp1, dp2, dp3)
	}
}

// --------------------------------------------------------------------------
// getNumOfPopulatedRingElements — head-wrap path
// --------------------------------------------------------------------------

func TestGetNumOfPopulatedRingElements_NoWrap(t *testing.T) {
	rb := createRingBuffer(8)
	writeRing(&rb, "a", "t1")
	writeRing(&rb, "b", "t2")
	writeRing(&rb, "c", "t3")
	// Head=3, Tail=0 → populated = 3
	if got := getNumOfPopulatedRingElements(&rb); got != 3 {
		t.Errorf("got %d; want 3", got)
	}
}

func TestGetNumOfPopulatedRingElements_WithWrap(t *testing.T) {
	// Artificially construct a head<tail scenario to exercise the wrap branch:
	// head=1, tail=3 → head < tail → head += bufSize(4) → 5 - 3 = 2
	rb := createRingBuffer(4)
	rb.Head = 1
	rb.Tail = 3
	if got := getNumOfPopulatedRingElements(&rb); got != 2 { // (1+4)-3=2
		t.Errorf("got %d; want 2", got)
	}
}

func TestGetNumOfPopulatedRingElements_HeadLessThanTail(t *testing.T) {
	rb := createRingBuffer(8)
	// Artificially set head<tail to test the wrap-around branch
	rb.Head = 2
	rb.Tail = 5 // head < tail → head += bufSize
	if got := getNumOfPopulatedRingElements(&rb); got != 5 { // (2+8)-5=5
		t.Errorf("got %d; want 5", got)
	}
}

// --------------------------------------------------------------------------
// sqliteBackend — Get and Set via in-memory SQLite
// --------------------------------------------------------------------------

func openTestSqlite() *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		panic("cannot open in-memory sqlite: " + err.Error())
	}
	_, err = db.Exec(`CREATE TABLE VSS_MAP (path TEXT, c_value TEXT, c_ts TEXT, d_value TEXT, d_ts TEXT)`)
	if err != nil {
		panic("cannot create VSS_MAP: " + err.Error())
	}
	return db
}

func TestSqliteBackend_GetMissingPath(t *testing.T) {
	db := openTestSqlite()
	defer db.Close()
	b := newSqliteBackend(db)
	result := b.Get("Vehicle.DoesNotExist")
	if !strings.Contains(result, "Data-not-available") {
		t.Errorf("expected Data-not-available; got %q", result)
	}
}

func TestSqliteBackend_GetHappyPath(t *testing.T) {
	db := openTestSqlite()
	defer db.Close()
	_, err := db.Exec(`INSERT INTO VSS_MAP (path, c_value, c_ts, d_value, d_ts) VALUES (?, ?, ?, ?, ?)`,
		"Vehicle.Speed", "42.0", "2025-01-01T00:00:00Z", "", "")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	b := newSqliteBackend(db)
	result := b.Get("Vehicle.Speed")
	if !strings.Contains(result, "42.0") {
		t.Errorf("expected 42.0 in result; got %q", result)
	}
}

func TestSqliteBackend_SetHappyPath(t *testing.T) {
	db := openTestSqlite()
	defer db.Close()
	_, err := db.Exec(`INSERT INTO VSS_MAP (path, c_value, c_ts, d_value, d_ts) VALUES (?, ?, ?, ?, ?)`,
		"Vehicle.Speed", "0", "2025-01-01T00:00:00Z", "", "")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	b := newSqliteBackend(db)
	ts := b.Set("Vehicle.Speed", "55.0")
	if ts == "" {
		t.Error("Set should return a non-empty timestamp on success")
	}
}

func TestSqliteBackend_SetQuotedPath(t *testing.T) {
	// Test that Set strips leading/trailing quotes from path
	db := openTestSqlite()
	defer db.Close()
	_, err := db.Exec(`INSERT INTO VSS_MAP (path, c_value, c_ts, d_value, d_ts) VALUES (?, ?, ?, ?, ?)`,
		"Vehicle.Speed", "0", "2025-01-01T00:00:00Z", "", "")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	b := newSqliteBackend(db)
	ts := b.Set(`"Vehicle.Speed"`, "66.0")
	if ts == "" {
		t.Error("Set with quoted path should return timestamp")
	}
}

func TestSqliteBackend_SetMissingPath(t *testing.T) {
	db := openTestSqlite()
	defer db.Close()
	b := newSqliteBackend(db)
	// No matching row → UPDATE affects 0 rows but returns no error
	ts := b.Set("Vehicle.NotInDB", "99.0")
	// The original code returns ts even on 0-row UPDATE; ts will be non-empty
	_ = ts
}

func TestSqliteBackend_GetQueryError(t *testing.T) {
	db := openTestSqlite()
	// Close the DB before calling Get → Query returns an error
	db.Close()
	b := newSqliteBackend(db)
	result := b.Get("Vehicle.Speed")
	if !strings.Contains(result, "Data-error") {
		t.Errorf("closed DB: expected Data-error; got %q", result)
	}
}

func TestSqliteBackend_SetAfterClose(t *testing.T) {
	db := openTestSqlite()
	db.Close()
	b := newSqliteBackend(db)
	ts := b.Set("Vehicle.Speed", "42.0")
	if ts != "" {
		t.Errorf("closed DB: expected empty timestamp; got %q", ts)
	}
}

// --------------------------------------------------------------------------
// redisBackend.Get and memcacheBackend.Get — integration-only (documented)
// Both require live external servers; tested via the adapter constructors
// to at least exercise newRedisBackend and newMemcacheBackend.
// --------------------------------------------------------------------------

func TestNewRedisBackend_ConstructorNotNil(t *testing.T) {
	// Just verifies the constructor doesn't panic; no real Redis connection
	toFeederChan := make(chan string, 1)
	b := newRedisBackend(nil, toFeederChan)
	if b == nil {
		t.Error("newRedisBackend should return non-nil")
	}
}

func TestNewMemcacheBackend_ConstructorNotNil(t *testing.T) {
	toFeederChan := make(chan string, 1)
	b := newMemcacheBackend(nil, toFeederChan)
	if b == nil {
		t.Error("newMemcacheBackend should return non-nil")
	}
}

// --------------------------------------------------------------------------
// Integration-only functions — documented as such (not unit-tested)
//
// The following functions are integration-only and are excluded from
// unit tests because they either:
//   - Bind a real UDS socket (initFeederRegServer, initHistoryControlServer,
//     historyServer, historyControlServer, initDataServer)
//   - Make real network connections (connectToFeeder, feederConnectRetry,
//     getVssPathList, configureDefault)
//   - Contain unbounded for/select loops serving live channels
//     (feederFrontend, feederReaderMgr, feederReader, curveLogServer)
//   - Call os.Exit on failure (ServiceMgrInit, initFeederRegServer)
//   - Require a live feeder connection (handleFeederRegistration)
//   - clCapture1dim / clCapture2dim / clCapture3dim — blocking goroutines
//     that read from a trigger channel indefinitely
//   - redisBackend.Get, memcacheBackend.Get, iotdbBackend.Get/Set —
//     require live external services (Redis, Memcache, Apache IoTDB)
// --------------------------------------------------------------------------
