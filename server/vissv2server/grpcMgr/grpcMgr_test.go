/**
* Regression tests for the grpcMgr fixes shipped in PR #120
* (grpcStateMu around grpcRoutingDataList / grpcClientIndexList).
**/
package grpcMgr

import (
	"sync"
	"testing"
)

// initLists sets up the package-level slices to a known empty state.
// The production setup of this is buried inside the gRPC Init path; we
// replicate the minimum here so the test is self-contained.
func initLists() {
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	if len(grpcClientIndexList) != MAXGRPCCLIENTS {
		grpcClientIndexList = make([]bool, MAXGRPCCLIENTS)
	}
	if len(grpcRoutingDataList) != MAXGRPCCLIENTS {
		grpcRoutingDataList = make([]GrpcRoutingData, MAXGRPCCLIENTS)
	}
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcClientIndexList[i] = false
		grpcRoutingDataList[i].ClientId = -1
	}
}

// TestGetClientId_AllocatesUniqueSlots is the basic-semantics check.
func TestGetClientId_AllocatesUniqueSlots(t *testing.T) {
	initLists()
	defer initLists()

	first := getClientId()
	second := getClientId()
	if first == -1 || second == -1 {
		t.Fatalf("expected two free slots; got %d, %d", first, second)
	}
	if first == second {
		t.Fatalf("two sequential getClientId calls returned the same slot %d", first)
	}
}

// TestGetClientId_Exhaustion verifies the function returns -1 when the
// pool is full.
func TestGetClientId_Exhaustion(t *testing.T) {
	initLists()
	defer initLists()

	grpcStateMu.Lock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcClientIndexList[i] = true
	}
	grpcStateMu.Unlock()

	if got := getClientId(); got != -1 {
		t.Fatalf("expected -1 when pool full; got %d", got)
	}
}

// TestGetClientId_ConcurrentClaimsAreUnique is the regression test for
// the PR #120 grpcStateMu mutex. Before that fix, per-RPC handler
// goroutines and the manager-loop goroutine concurrently mutated
// grpcClientIndexList / grpcRoutingDataList; the result was slot leaks,
// cross-talk between unrelated subscribers, or runtime panics on
// concurrent slice mutation.
//
// Run with: go test -race
func TestGetClientId_ConcurrentClaimsAreUnique(t *testing.T) {
	initLists()
	defer initLists()

	n := MAXGRPCCLIENTS
	results := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = getClientId()
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
			t.Fatalf("slot %d was claimed %d times concurrently; grpcStateMu is missing or broken", idx, count)
		}
	}
}

// TestSetAndResetGrpcRoutingData_ConcurrentSafe exercises the routing-
// data accessors concurrently. Designed to be run with -race.
func TestSetAndResetGrpcRoutingData_ConcurrentSafe(t *testing.T) {
	initLists()
	defer initLists()

	// Claim a handful of client ids first so the routing accessors have
	// values to mutate.
	const n = 8
	ids := make([]int, n)
	for i := range ids {
		ids[i] = getClientId()
		if ids[i] == -1 {
			t.Fatalf("expected at least %d free client slots", n)
		}
		if !setGrpcRoutingData(ids[i], make(chan string, 1), false) {
			t.Fatalf("setGrpcRoutingData failed for clientId %d", ids[i])
		}
	}

	// Spawn the same number of mutators that race set/get/reset.
	var wg sync.WaitGroup
	wg.Add(n * 3)
	for i := 0; i < n; i++ {
		clientId := ids[i]
		subId := "sub-" + string(rune('A'+i))
		go func() {
			defer wg.Done()
			updateGrpcRoutingData(clientId, subId)
		}()
		go func() {
			defer wg.Done()
			_, _ = getGrpcRoutingData(clientId)
		}()
		go func() {
			defer wg.Done()
			resetGrpcRoutingData(clientId)
		}()
	}
	wg.Wait()
}
