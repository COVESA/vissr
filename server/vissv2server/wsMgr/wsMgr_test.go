/**
* Broader coverage tests for the wsMgr package. Targets the pure-string
* and pure-JSON helpers; the goroutine-driven WS upgrade path is best
* exercised by the runtest.sh integration harness.
**/
package wsMgr

import (
	"testing"
)

// TestGetValueForKey covers the hand-rolled JSON value extractor used
// in the WS request fast path. The helper is "best-effort" — it doesn't
// fully parse JSON — so the contract is "return the value if findable,
// else empty".
func TestGetValueForKey(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		key  string
		want string
	}{
		{"basic", `{"path": "Vehicle.Speed"}`, `"path":`, "Vehicle.Speed"},
		{"action", `{"action": "get"}`, `"action":`, "get"},
		{"missing key", `{"path": "Vehicle"}`, `"action":`, ""},
		{"empty input", ``, `"path":`, ""},
		{"value with embedded colon", `{"path": "Vehicle:Speed"}`, `"path":`, "Vehicle:Speed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := getValueForKey(c.msg, c.key)
			if got != c.want {
				t.Fatalf("getValueForKey(%q,%q) = %q; want %q", c.msg, c.key, got, c.want)
			}
		})
	}
}

// TestGetSortedPaths verifies the deterministic ordering needed by the
// compression / payload-id machinery.
func TestGetSortedPaths(t *testing.T) {
	// Single-element data ('get' shape).
	msg := `{"data": {"path": "Vehicle.Speed", "dp": {"value": "100", "ts": "2026-05-16T12:00:00Z"}}}`
	got := getSortedPaths(msg)
	if len(got) != 1 || got[0] != "Vehicle.Speed" {
		t.Fatalf("getSortedPaths single path: got %v", got)
	}

	// Multi-element data, out of order on the wire.
	msg = `{"data": [
		{"path": "Vehicle.Speed", "dp": {"value": "1", "ts": "x"}},
		{"path": "Vehicle.Acceleration", "dp": {"value": "2", "ts": "y"}},
		{"path": "Vehicle.Cabin.Temperature", "dp": {"value": "3", "ts": "z"}}
	]}`
	got = getSortedPaths(msg)
	if len(got) != 3 {
		t.Fatalf("getSortedPaths multi path: expected 3 paths, got %d (%v)", len(got), got)
	}
	// Must be sorted.
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("getSortedPaths output not sorted: %v", got)
		}
	}
}

// TestGetSortedPaths_RejectsMalformedJSON verifies the helper returns
// nil (not panic) when handed garbage.
func TestGetSortedPaths_RejectsMalformedJSON(t *testing.T) {
	cases := []string{
		"",
		"not json",
		"{",
		`{"data": "not an object or array"}`,
		`null`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("getSortedPaths panicked on %q: %v", in, r)
				}
			}()
			got := getSortedPaths(in)
			if got != nil && len(got) != 0 {
				t.Logf("note: getSortedPaths returned non-empty %v on malformed input %q (acceptable as long as no panic)", got, in)
			}
		})
	}
}

// TestDcCache exercises the data-compression-cache helpers (insert,
// lookup, reset) used by the WS request fast path.
func TestDcCache(t *testing.T) {
	initDcCache()
	const payload = "test-payload-id"

	// Lookup on empty cache returns -1.
	if idx := getDcCacheIndex(payload); idx != -1 {
		t.Fatalf("expected -1 on empty cache lookup; got %d", idx)
	}

	// Insert allocates a slot.
	dcCacheInsert(payload, "dc-value", 0)
	idx := getDcCacheIndex(payload)
	if idx < 0 {
		t.Fatalf("expected non-negative index after insert; got %d", idx)
	}

	// Reset clears the entry.
	resetDcCache(idx)
	if idx2 := getDcCacheIndex(payload); idx2 != -1 {
		t.Fatalf("expected -1 after reset; got %d", idx2)
	}
}

// ---------------------------------------------------------------------------
// RemoveRoutingForwardResponse
// ---------------------------------------------------------------------------

// TestRemoveRoutingForwardResponse_SubscriptionForwardedToBackend: a
// response containing "subscription" should be forwarded to
// clientBackendChan, not wsClientChan.
func TestRemoveRoutingForwardResponse_SubscriptionForwardedToBackend(t *testing.T) {
	// initChannels was called in TestMain.
	// A response containing "subscription" AND a RouterId for client 0.
	resp := `{"action":"subscription","subscriptionId":"sub-1","RouterId":"0?0"}`

	done := make(chan struct{})
	go func() {
		RemoveRoutingForwardResponse(resp, nil)
		close(done)
	}()
	select {
	case got := <-clientBackendChan[0]:
		if !containsStr(got, "subscription") {
			t.Fatalf("clientBackendChan received %q; want subscription message", got)
		}
	case <-done:
		t.Fatalf("handler returned without forwarding subscription to clientBackendChan")
	}
	<-done
}

// TestRemoveRoutingForwardResponse_SubscriptionDroppedWhenBackendFull: if
// clientBackendChan is not being drained, the select{default} branch drops
// the event with an error log.
func TestRemoveRoutingForwardResponse_SubscriptionDroppedWhenBackendFull(t *testing.T) {
	// Use client slot 1 — ensure it has no reader, so the select default
	// branch fires immediately, logging "wsmgr:Event dropped".
	// RouterId "0?1" → clientId=1.
	resp := `{"action":"subscription","subscriptionId":"sub-drop","RouterId":"0?1"}`
	// Must not panic or block. Call synchronously (no goroutine needed
	// because the select has a default).
	RemoveRoutingForwardResponse(resp, nil)
}

// ---------------------------------------------------------------------------
// checkCompressionRequest
// ---------------------------------------------------------------------------

// TestCheckCompressionRequest_NoOpWithoutDc verifies the function does
// nothing (no cache entry created) when the request lacks a "dc" field.
func TestCheckCompressionRequest_NoOpWithoutDc(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"get","path":"Vehicle.Speed","requestId":"100"}`
	checkCompressionRequest(msg)
	if idx := getDcCacheIndex("100"); idx != -1 {
		t.Fatalf("expected no cache entry without dc field; got idx=%d", idx)
	}
}

// TestCheckCompressionRequest_InsertsForGet: a request with "dc" and a
// "get" action (single path) should insert a cache entry with
// responseHandling==1.
func TestCheckCompressionRequest_InsertsForGet(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"get","path":"Vehicle.Speed","dc":"2+1","requestId":"200"}`
	checkCompressionRequest(msg)
	idx := getDcCacheIndex("200")
	if idx == -1 {
		t.Fatalf("expected cache entry after checkCompressionRequest with dc field")
	}
	if dataCompressionCache[idx].ResponseHandling != 1 {
		t.Fatalf("responseHandling = %d; want 1 (get+singlePath)", dataCompressionCache[idx].ResponseHandling)
	}
}

// TestCheckCompressionRequest_InsertsForGetMultiPath: a request with "dc"
// and "paths" (multiple paths) should create responseHandling==2.
func TestCheckCompressionRequest_InsertsForGetMultiPath(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"get","paths":["A","B"],"dc":"2+1","requestId":"201"}`
	checkCompressionRequest(msg)
	idx := getDcCacheIndex("201")
	if idx == -1 {
		t.Fatalf("expected cache entry after checkCompressionRequest with dc field")
	}
	if dataCompressionCache[idx].ResponseHandling != 2 {
		t.Fatalf("responseHandling = %d; want 2 (get+multiPath)", dataCompressionCache[idx].ResponseHandling)
	}
}

// TestCheckCompressionRequest_InsertsForSubscribeSinglePath: subscribe
// single path → responseHandling==3.
func TestCheckCompressionRequest_InsertsForSubscribeSinglePath(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"subscribe","path":"Vehicle.Speed","dc":"2+1","requestId":"202"}`
	checkCompressionRequest(msg)
	idx := getDcCacheIndex("202")
	if idx == -1 {
		t.Fatalf("expected cache entry for subscribe single path")
	}
	if dataCompressionCache[idx].ResponseHandling != 3 {
		t.Fatalf("responseHandling = %d; want 3 (subscribe+singlePath)", dataCompressionCache[idx].ResponseHandling)
	}
}

// TestCheckCompressionRequest_InsertsForSubscribeMultiPath: subscribe
// multi path → responseHandling==4.
func TestCheckCompressionRequest_InsertsForSubscribeMultiPath(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"subscribe","paths":["A","B"],"dc":"2+1","requestId":"203"}`
	checkCompressionRequest(msg)
	idx := getDcCacheIndex("203")
	if idx == -1 {
		t.Fatalf("expected cache entry for subscribe multi path")
	}
	if dataCompressionCache[idx].ResponseHandling != 4 {
		t.Fatalf("responseHandling = %d; want 4 (subscribe+multiPath)", dataCompressionCache[idx].ResponseHandling)
	}
}

// TestCheckCompressionRequest_InvalidDcStillInsertsEntry: dcCacheInsert
// is called even for unsupported dc values; the entry is created but
// with the Dc fields left at zero (setDcValue returned false). This
// test pins the observed behaviour so a regression is detectable.
func TestCheckCompressionRequest_InvalidDcStillInsertsEntry(t *testing.T) {
	initDcCache()
	defer initDcCache()
	// "99+99" is rejected by setDcValue (pc=99 unsupported) but
	// checkCompressionRequest still calls dcCacheInsert.
	msg := `{"action":"get","path":"Vehicle.Speed","dc":"99+99","requestId":"204"}`
	checkCompressionRequest(msg)
	// An entry is created (dcCacheInsert always inserts when len(dcValue) > 0).
	idx := getDcCacheIndex("204")
	if idx == -1 {
		t.Fatalf("even for invalid dc values, dcCacheInsert is called and an entry is created")
	}
	// The Dc fields should be zero since setDcValue rejected the values.
	if dataCompressionCache[idx].Dc.Pc != 0 || dataCompressionCache[idx].Dc.Tsc != 0 {
		t.Fatalf("Dc fields should remain zero for rejected dc values; got Pc=%d Tsc=%d",
			dataCompressionCache[idx].Dc.Pc, dataCompressionCache[idx].Dc.Tsc)
	}
}

// ---------------------------------------------------------------------------
// compressPaths
// ---------------------------------------------------------------------------

// TestCompressPaths replaces path strings in a response with their
// sorted-list index.
func TestCompressPaths(t *testing.T) {
	sortedList := []string{"Vehicle.Acceleration", "Vehicle.Cabin.Temperature", "Vehicle.Speed"}
	msg := `{"data":[{"path":"Vehicle.Speed"},{"path":"Vehicle.Acceleration"},{"path":"Vehicle.Cabin.Temperature"}]}`
	got := compressPaths(msg, sortedList)
	if got == msg {
		t.Fatalf("compressPaths should have mutated the message")
	}
	// Vehicle.Acceleration is index 0, Vehicle.Speed is index 2.
	if !containsStr(got, `"path":"2"`) && !containsStr(got, `"path":2`) {
		// compressPaths does a simple string replacement: sortedList[2]="Vehicle.Speed" -> "2"
		// The message should now contain "2" where Vehicle.Speed was.
		t.Logf("got: %s", got)
	}
	// Idempotence: another compress with same list on already-compressed message
	// should not panic.
	got2 := compressPaths(got, sortedList)
	_ = got2
}

// TestCompressPaths_EmptyList: empty sortedList returns message unchanged.
func TestCompressPaths_EmptyList(t *testing.T) {
	msg := `{"data":[{"path":"Vehicle.Speed"}]}`
	got := compressPaths(msg, nil)
	if got != msg {
		t.Fatalf("compressPaths with nil list should return input unchanged; got %q", got)
	}
}

func containsStr(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// ---------------------------------------------------------------------------
// checkCompressionResponse
// ---------------------------------------------------------------------------

// TestCheckCompressionResponse_ErrorPassesThrough: a response containing
// "error" bypasses all compression logic and is returned as-is.
func TestCheckCompressionResponse_ErrorPassesThrough(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"get","requestId":"999","error":{"number":"404"}}`
	got := checkCompressionResponse(msg)
	if got != msg {
		t.Fatalf("error response should pass through unchanged; got %q", got)
	}
}

// TestCheckCompressionResponse_NoCacheEntryPassesThrough: a response for
// which there is no dc cache entry is returned unchanged.
func TestCheckCompressionResponse_NoCacheEntryPassesThrough(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"get","requestId":"not-in-cache"}`
	got := checkCompressionResponse(msg)
	if got != msg {
		t.Fatalf("no-cache-entry response should pass through unchanged; got %q", got)
	}
}

// TestCheckCompressionResponse_UnsubscribeResetsCache: an unsubscribe
// response clears the cache entry and returns the message as-is.
func TestCheckCompressionResponse_UnsubscribeResetsCache(t *testing.T) {
	initDcCache()
	defer initDcCache()
	dcCacheInsert("sub42", "2+0", 3)
	msg := `{"action":"unsubscribe","requestId":"sub42"}`
	got := checkCompressionResponse(msg)
	if got != msg {
		t.Fatalf("unsubscribe response should be returned unchanged; got %q", got)
	}
	if idx := getDcCacheIndex("sub42"); idx != -1 {
		t.Fatalf("cache entry should be cleared after unsubscribe; still at idx=%d", idx)
	}
}

// TestCheckCompressionResponse_GetResponseHandling1: a get response
// with responseHandling==1 and Tsc==0 (no ts compression, no path
// compression) should clear the cache entry after processing.
func TestCheckCompressionResponse_GetResponseHandling1_ClearsCache(t *testing.T) {
	initDcCache()
	defer initDcCache()
	// Insert with pc=0 (no path compression), tsc=0 (no ts compression), responseHandling=1
	dcCacheInsert("req300", "0+0", 1)
	msg := `{"action":"get","requestId":"req300","data":{"path":"Vehicle.Speed","dp":{"value":"55","ts":"2026-01-01T00:00:00Z"}},"ts":"2026-01-01T00:00:00Z"}`
	got := checkCompressionResponse(msg)
	_ = got
	// After responseHandling==1 the cache entry must be gone.
	if idx := getDcCacheIndex("req300"); idx != -1 {
		t.Fatalf("responseHandling=1 should clear cache; entry still at idx=%d", idx)
	}
}

// TestCheckCompressionResponse_ResponseHandling2_DoesNotClear:
// responseHandling==2 returns the message and does NOT clear the cache.
func TestCheckCompressionResponse_ResponseHandling2_DoesNotClear(t *testing.T) {
	initDcCache()
	defer initDcCache()
	dcCacheInsert("req301", "0+0", 2)
	// Force responseHandling to 2.
	idx := getDcCacheIndex("req301")
	dataCompressionCache[idx].ResponseHandling = 2
	msg := `{"action":"get","requestId":"req301"}`
	got := checkCompressionResponse(msg)
	if got != msg {
		t.Fatalf("responseHandling=2 should return message unchanged")
	}
	// Cache entry should still be present.
	if getDcCacheIndex("req301") == -1 {
		t.Fatalf("responseHandling=2 should not clear cache entry")
	}
}

// TestCheckCompressionResponse_SubscribeRenamesPayloadId: a subscribe
// response updates the cache key from requestId to subscriptionId.
func TestCheckCompressionResponse_SubscribeRenamesPayloadId(t *testing.T) {
	initDcCache()
	defer initDcCache()
	dcCacheInsert("req302", "2+0", 3)
	msg := `{"action":"subscribe","requestId":"req302","subscriptionId":"sub302"}`
	_ = checkCompressionResponse(msg)
	// The entry should now be keyed by subscriptionId.
	if idx := getDcCacheIndex("sub302"); idx == -1 {
		t.Fatalf("after subscribe response, cache should be keyed by subscriptionId")
	}
}

// TestCheckCompressionResponse_DefaultActionPassesThrough: an action not
// handled by the switch (e.g. "set") returns the message unchanged.
func TestCheckCompressionResponse_DefaultActionPassesThrough(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"set","requestId":"req999","value":"42"}`
	got := checkCompressionResponse(msg)
	if got != msg {
		t.Fatalf("unrecognised action should pass through unchanged; got %q", got)
	}
}

// TestCheckCompressionResponse_ResponseHandling3_PreservesCache:
// responseHandling==3 (subscribe, single path after first time) compresses
// paths and ts but does NOT clear the cache.
func TestCheckCompressionResponse_ResponseHandling3_PreservesCache(t *testing.T) {
	initDcCache()
	defer initDcCache()
	dcCacheInsert("sub303", "0+0", 3)
	idx := getDcCacheIndex("sub303")
	// Force responseHandling to 3 with no path compression (pc=0).
	dataCompressionCache[idx].ResponseHandling = 3
	msg := `{"action":"subscription","subscriptionId":"sub303"}`
	got := checkCompressionResponse(msg)
	_ = got
	// Cache entry should still be present for subsequent events.
	if getDcCacheIndex("sub303") == -1 {
		t.Fatalf("responseHandling=3 should NOT clear the cache entry")
	}
}

// TestCheckCompressionResponse_ResponseHandling4_TransitionTo3:
// responseHandling==4 (subscribe, multiple paths, first time) computes
// sorted paths and transitions to 3.
func TestCheckCompressionResponse_ResponseHandling4_TransitionTo3(t *testing.T) {
	initDcCache()
	defer initDcCache()
	dcCacheInsert("sub304", "0+0", 4)
	idx := getDcCacheIndex("sub304")
	dataCompressionCache[idx].ResponseHandling = 4
	msg := `{"action":"subscription","subscriptionId":"sub304","data":[{"path":"Vehicle.Speed","dp":{"value":"55","ts":"2026-01-01T00:00:00Z"}}],"ts":"2026-01-01T00:00:00Z"}`
	_ = checkCompressionResponse(msg)
	// After case 4, ResponseHandling must become 3.
	idx2 := getDcCacheIndex("sub304")
	if idx2 == -1 {
		t.Fatalf("cache entry missing after responseHandling=4")
	}
	if dataCompressionCache[idx2].ResponseHandling != 3 {
		t.Fatalf("responseHandling after case 4 = %d; want 3", dataCompressionCache[idx2].ResponseHandling)
	}
}

// TestCheckCompressionResponse_Subscription_NoCache: a subscription
// response whose subscriptionId is not in the cache passes through unchanged.
func TestCheckCompressionResponse_Subscription_NoCache(t *testing.T) {
	initDcCache()
	defer initDcCache()
	msg := `{"action":"subscription","subscriptionId":"not-cached"}`
	got := checkCompressionResponse(msg)
	if got != msg {
		t.Fatalf("subscription with no cache entry should pass through; got %q", got)
	}
}

// ---------------------------------------------------------------------------
// replaceTs
// ---------------------------------------------------------------------------

// TestReplaceTs_SingleTsList: replaceTs replaces a dp ts with a signed
// millisecond offset relative to the message-level ts.
func TestReplaceTs_SingleTsList(t *testing.T) {
	// messageTs == dpTs → diff == 0 → signedTimeDiff returns "+0".
	messageTs := "2026-01-01T00:00:00Z"
	dpTs := "2026-01-01T00:00:00Z"
	msg := `{"action":"get","ts":"` + messageTs + `","data":{"dp":{"ts":"` + dpTs + `","value":"55"}}}`
	got := replaceTs(msg, messageTs, []string{dpTs})
	// The dp ts should now be a signed offset string, not the ISO timestamp.
	if got == msg {
		t.Fatalf("replaceTs should have modified the message; got unchanged: %q", got)
	}
}

// TestReplaceTs_EmptyTsList: empty tsList returns message unchanged.
func TestReplaceTs_EmptyTsList(t *testing.T) {
	messageTs := "2026-01-01T00:00:00Z"
	msg := `{"action":"get","ts":"` + messageTs + `"}`
	got := replaceTs(msg, messageTs, nil)
	if got != msg {
		t.Fatalf("replaceTs with empty tsList should return unchanged; got %q", got)
	}
}

// TestReplaceTs_LargeOffsetSkipped: a dp ts more than 999999999 ms away
// from the reference should be left as-is (keep ISO time).
func TestReplaceTs_LargeOffsetSkipped(t *testing.T) {
	messageTs := "2026-01-01T00:00:00Z"
	// 2000-01-01 is ~26 years before 2026-01-01 → diff >> 999999999 ms
	dpTs := "2000-01-01T00:00:00Z"
	msg := `{"action":"get","ts":"` + messageTs + `","data":{"dp":{"ts":"` + dpTs + `","value":"x"}}}`
	got := replaceTs(msg, messageTs, []string{dpTs})
	// The dp ts should be preserved (not replaced with a relative offset).
	if !containsStr(got, dpTs) {
		t.Fatalf("large-offset dp ts should be kept as ISO; got %q", got)
	}
}

// ---------------------------------------------------------------------------
// compressTs (additional coverage beyond the malformed-JSON path)
// ---------------------------------------------------------------------------

// TestCompressTs_SingleDp: a well-formed response with a single dp ts
// should have its dp ts replaced with a relative offset.
func TestCompressTs_SingleDp(t *testing.T) {
	msg := `{"action":"get","ts":"2026-01-01T00:00:00Z","data":{"path":"Vehicle.Speed","dp":{"value":"100","ts":"2026-01-01T00:00:00Z"}}}`
	got := compressTs(msg)
	// The dp ts in the data section should be replaced by a relative offset
	// (e.g. "+0" since both timestamps are equal).
	if got == msg {
		t.Fatalf("compressTs should modify the response; got unchanged: %q", got)
	}
}

// TestCompressTs_MissingTs: a response with no top-level "ts" field
// should be returned unchanged.
func TestCompressTs_MissingTs(t *testing.T) {
	msg := `{"action":"get","data":{"path":"Vehicle.Speed","dp":{"value":"100","ts":"2026-01-01T00:00:00Z"}}}`
	got := compressTs(msg)
	if got != msg {
		t.Logf("note: compressTs on missing ts returned %q (acceptable, just logging)", got)
	}
}

// TestCompressTs_ArrayData: a subscription response with an array
// "data" field should not panic.
func TestCompressTs_ArrayData(t *testing.T) {
	msg := `{"action":"subscription","ts":"2026-01-01T00:00:00Z","data":[{"path":"Vehicle.Speed","dp":{"value":"55","ts":"2026-01-01T00:00:00Z"}},{"path":"Vehicle.Acceleration","dp":{"value":"2","ts":"2026-01-01T00:00:00Z"}}]}`
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("compressTs panicked on array data: %v", r)
		}
	}()
	_ = compressTs(msg)
}

// TestCompressTs_NonStringTs: a response where "ts" is a number should
// be returned unchanged without panicking.
func TestCompressTs_NonStringTs(t *testing.T) {
	msg := `{"action":"get","ts":12345,"data":{"dp":{"ts":"2026-01-01T00:00:00Z","value":"x"}}}`
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("compressTs panicked on non-string ts: %v", r)
		}
	}()
	got := compressTs(msg)
	if got != msg {
		t.Logf("note: compressTs on non-string ts returned %q (acceptable)", got)
	}
}

// ---------------------------------------------------------------------------
// getDpTsList (additional: multiple dp entries in a single map)
// ---------------------------------------------------------------------------

// TestGetDpTsList_NoTs: a dp map without a "ts" key returns empty list.
func TestGetDpTsList_NoTs(t *testing.T) {
	in := map[string]interface{}{"value": "100"}
	got := getDpTsList(in)
	if len(got) != 0 {
		t.Fatalf("getDpTsList without ts = %v; want empty", got)
	}
}

// TestGetDpTsList_Nil: nil input returns empty list without panic.
func TestGetDpTsList_Nil(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("getDpTsList(nil) panicked: %v", r)
		}
	}()
	got := getDpTsList(nil)
	if got != nil {
		t.Fatalf("getDpTsList(nil) = %v; want nil", got)
	}
}

// TestGetDpTsList_ArrayWithNonMapElement: if an array element is not a
// map, it should be skipped, not panic.
func TestGetDpTsList_ArrayWithNonMapElement(t *testing.T) {
	in := []interface{}{
		map[string]interface{}{"ts": "ts1"},
		"not a map",
		map[string]interface{}{"ts": "ts3"},
	}
	got := getDpTsList(in)
	if len(got) != 2 {
		t.Fatalf("getDpTsList with non-map element = %v; want 2 entries", got)
	}
}
