/**
* Regression tests for the udsMgr fixes shipped in PR #120
* (-1 panic protection in initClientServer; udsClientIndexMu race fix).
**/
package udsMgr

import (
	"sync"
	"testing"
)

// initUdsClientIndexList ensures the package-level free-list has the
// expected size and shape. The production setup of this is buried inside
// UdsMgrInit; we replicate the minimum here so the test is self-
// contained.
func initUdsClientIndexList() {
	if len(UdsClientIndexList) != NUMOFUDSCLIENTS {
		UdsClientIndexList = make([]bool, NUMOFUDSCLIENTS)
	}
	udsClientIndexMu.Lock()
	for i := range UdsClientIndexList {
		UdsClientIndexList[i] = true
	}
	udsClientIndexMu.Unlock()
}

// TestGetUdsClientIndex_ExhaustionReturnsMinusOne verifies the helper
// returns -1 cleanly when every slot is taken. The PR #120 fix added
// the -1 check in initClientServer.Accept that consumes this return
// value — see the integration-style invariant in the comment there.
func TestGetUdsClientIndex_ExhaustionReturnsMinusOne(t *testing.T) {
	initUdsClientIndexList()
	defer initUdsClientIndexList()

	// Claim everything.
	udsClientIndexMu.Lock()
	for i := range UdsClientIndexList {
		UdsClientIndexList[i] = false
	}
	udsClientIndexMu.Unlock()

	if got := getUdsClientIndex(); got != -1 {
		t.Fatalf("expected -1 when pool fully claimed; got %d", got)
	}
}

// TestUdsClientIndex_ConcurrentClaimsAreUnique is the regression test
// for the PR #120 udsClientIndexMu mutex. Without it, two concurrent
// Accept goroutines could observe the same slot as free and both claim
// it, causing UDS request/response cross-talk between unrelated clients.
//
// Run with: go test -race
func TestUdsClientIndex_ConcurrentClaimsAreUnique(t *testing.T) {
	initUdsClientIndexList()
	defer initUdsClientIndexList()

	n := NUMOFUDSCLIENTS
	results := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = getUdsClientIndex()
		}(i)
	}
	wg.Wait()

	seen := make(map[int]int)
	for _, idx := range results {
		if idx == -1 {
			t.Fatalf("concurrent claim returned -1 even though %d slots were free", n)
		}
		seen[idx]++
	}
	for idx, count := range seen {
		if count > 1 {
			t.Fatalf("slot %d claimed by %d goroutines concurrently; udsClientIndexMu is missing or broken", idx, count)
		}
	}
}

// TestReturnUdsClientIndex_MakesSlotReclaimable confirms basic semantics.
func TestReturnUdsClientIndex_MakesSlotReclaimable(t *testing.T) {
	initUdsClientIndexList()
	defer initUdsClientIndexList()

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
