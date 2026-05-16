/**
* Regression tests for the serviceMgr fixes shipped in PR #120 (history
* control) and PR #121 (history get).
**/
package serviceMgr

import (
	"os"
	"runtime"
	"strings"
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
