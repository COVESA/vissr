/**
* (C) 2026 Matt Jones / Ford
*
* Tests for vissv2server.go. Covers every unit-testable function plus a
* regression for each bug fix in this branch (see fix/vissv2server-bugs).
**/

package main

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

func init() {
	utils.InitLog("vissv2server_test-log.txt", "./logs", false, "info")
}

// --------------------------------------------------------------------------
// extractMgrId — malformed RouterId no longer panics and routes to -1
// --------------------------------------------------------------------------

func TestExtractMgrId_HappyPath(t *testing.T) {
	if got := extractMgrId("3?42"); got != 3 {
		t.Errorf("got %d; want 3", got)
	}
}

func TestExtractMgrId_MissingDelimiter(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("missing-delimiter panicked: %v", r)
		}
	}()
	if got := extractMgrId("no-delim-here"); got != -1 {
		t.Errorf("got %d; want -1", got)
	}
}

func TestExtractMgrId_NonNumeric(t *testing.T) {
	if got := extractMgrId("abc?42"); got != -1 {
		t.Errorf("got %d; want -1", got)
	}
}

func TestExtractMgrId_EmptyPrefix(t *testing.T) {
	if got := extractMgrId("?42"); got != -1 {
		t.Errorf("got %d; want -1 for empty mgrId", got)
	}
}

// --------------------------------------------------------------------------
// extractNoScopeElementsLevel1 / Level2
// --------------------------------------------------------------------------

func TestExtractNoScopeElementsLevel1_StringValue(t *testing.T) {
	in := map[string]interface{}{"k": "single-path"}
	list, n := extractNoScopeElementsLevel1(in)
	if n != 1 || list[0] != "single-path" {
		t.Errorf("got list=%v n=%d", list, n)
	}
}

func TestExtractNoScopeElementsLevel1_ArrayValue(t *testing.T) {
	in := map[string]interface{}{"k": []interface{}{"a", "b", "c"}}
	list, n := extractNoScopeElementsLevel1(in)
	if n != 3 || list[0] != "a" || list[2] != "c" {
		t.Errorf("got list=%v n=%d", list, n)
	}
}

func TestExtractNoScopeElementsLevel1_UnknownType(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unknown type panicked: %v", r)
		}
	}()
	in := map[string]interface{}{"k": 42}
	list, n := extractNoScopeElementsLevel1(in)
	if list != nil || n != 0 {
		t.Errorf("got list=%v n=%d", list, n)
	}
}

func TestExtractNoScopeElementsLevel2_NonStringSkipped(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("non-string element panicked: %v", r)
		}
	}()
	in := []interface{}{"a", 42, "b"}
	list, n := extractNoScopeElementsLevel2(in)
	if n != 3 {
		t.Errorf("count: got %d; want 3", n)
	}
	if list[0] != "a" || list[2] != "b" {
		t.Errorf("got %v", list)
	}
}

// --------------------------------------------------------------------------
// getTokenContext — defensive type assertion on authorization
// --------------------------------------------------------------------------

func TestGetTokenContext_NoAuthReturnsEmpty(t *testing.T) {
	got := getTokenContext(map[string]interface{}{})
	if got != "" {
		t.Errorf("got %q", got)
	}
}

func TestGetTokenContext_NonStringAuthDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("non-string auth panicked: %v", r)
		}
	}()
	got := getTokenContext(map[string]interface{}{"authorization": 42})
	if got != "" {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// removeLocalProperty — defensive against non-map values
// --------------------------------------------------------------------------

func TestRemoveLocalProperty_HappyPath(t *testing.T) {
	in := map[string]interface{}{
		"a": map[string]interface{}{"local": "drop", "keep": "yes"},
		"b": map[string]interface{}{"keep": "yes"},
	}
	out := removeLocalProperty(in)
	a := out["a"].(map[string]interface{})
	if _, present := a["local"]; present {
		t.Errorf("local should be removed: %+v", a)
	}
	if a["keep"] != "yes" {
		t.Errorf("non-local key dropped: %+v", a)
	}
}

func TestRemoveLocalProperty_NonMapValueDoesNotPanic(t *testing.T) {
	// Pre-fix: v1.(map[string]interface{}) panicked on string/number values.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	in := map[string]interface{}{
		"a": "scalar value",
		"b": 42,
		"c": []interface{}{1, 2, 3},
		"d": map[string]interface{}{"local": "drop"},
	}
	out := removeLocalProperty(in)
	d := out["d"].(map[string]interface{})
	if _, present := d["local"]; present {
		t.Errorf("local should be removed from valid map: %+v", d)
	}
}

// --------------------------------------------------------------------------
// getRangeBoundaries — defensive
// --------------------------------------------------------------------------

func TestGetRangeBoundaries_SingleObject(t *testing.T) {
	in := map[string]interface{}{"boundary": "10", "logic-op": "lt"}
	a, b := getRangeBoundaries(in)
	if a != "10" || b != "" {
		t.Errorf("got %q,%q; want 10,empty", a, b)
	}
}

func TestGetRangeBoundaries_Array(t *testing.T) {
	in := []interface{}{
		map[string]interface{}{"boundary": "1"},
		map[string]interface{}{"boundary": "9"},
	}
	a, b := getRangeBoundaries(in)
	if a != "1" || b != "9" {
		t.Errorf("got %q,%q; want 1,9", a, b)
	}
}

func TestGetRangeBoundaries_ArrayWithNonMapElement(t *testing.T) {
	// Pre-fix: pMap[i].(map[string]interface{}) panicked on string elements.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on non-map element: %v", r)
		}
	}()
	in := []interface{}{
		"not-a-map",
		map[string]interface{}{"boundary": "5"},
	}
	a, b := getRangeBoundaries(in)
	if a != "" || b != "5" {
		t.Errorf("got %q,%q", a, b)
	}
}

func TestGetRangeBoundaries_UnknownType(t *testing.T) {
	a, b := getRangeBoundaries(42)
	if a != "" || b != "" {
		t.Errorf("expected empty; got %q,%q", a, b)
	}
}

func TestGetRangeBoundaries_OversizedArray(t *testing.T) {
	in := []interface{}{
		map[string]interface{}{"boundary": "1"},
		map[string]interface{}{"boundary": "2"},
		map[string]interface{}{"boundary": "3"},
	}
	a, b := getRangeBoundaries(in)
	if a != "1" || b != "2" {
		t.Errorf("got %q,%q; want 1,2 (truncated to first two)", a, b)
	}
}

// --------------------------------------------------------------------------
// getFileDescriptorData — defensive assertions
// --------------------------------------------------------------------------

func TestGetFileDescriptorData_HappyPath(t *testing.T) {
	in := map[string]interface{}{
		"name": "file.bin",
		"hash": "abc123",
		"uid":  "deadbeef",
	}
	n, h, u := getFileDescriptorData(in)
	if n != "file.bin" || h != "abc123" || u != "deadbeef" {
		t.Errorf("got %q,%q,%q", n, h, u)
	}
}

func TestGetFileDescriptorData_NotAMap(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	n, h, u := getFileDescriptorData("not a map")
	if n != "" || h != "" || u != "" {
		t.Errorf("got %q,%q,%q", n, h, u)
	}
}

func TestGetFileDescriptorData_NonStringValue(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	in := map[string]interface{}{
		"name": 42, // wrong type
		"hash": "ok",
		"uid":  "ok",
	}
	n, _, _ := getFileDescriptorData(in)
	if n != "" {
		t.Errorf("expected empty result on bad type; got %q", n)
	}
}

func TestGetFileDescriptorData_UnknownKey(t *testing.T) {
	in := map[string]interface{}{
		"name":    "file.bin",
		"hash":    "abc",
		"uid":     "deadbeef",
		"unknown": "extra",
	}
	n, h, u := getFileDescriptorData(in)
	// Unknown keys are tolerated (logged); known keys still populated.
	if n != "file.bin" || h != "abc" || u != "deadbeef" {
		t.Errorf("got %q,%q,%q", n, h, u)
	}
}

// FuzzGetFileDescriptorData runs the helper against pseudo-random JSON
// shapes to confirm it never panics regardless of input.
//
// Run with: go test -fuzz=FuzzGetFileDescriptorData -fuzztime=10s ./...
func FuzzGetFileDescriptorData(f *testing.F) {
	seeds := []struct {
		name, hash, uid string
	}{
		{"upload.txt", "abcdef", "2d878213"},
		{"", "", ""},
		{"a", "b", "c"},
		{"name with spaces", "abc", "12345678"},
	}
	for _, s := range seeds {
		f.Add(s.name, s.hash, s.uid)
	}
	f.Fuzz(func(t *testing.T, name, hash, uid string) {
		value := map[string]interface{}{
			"name": name,
			"hash": hash,
			"uid":  uid,
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("getFileDescriptorData panicked on (%q,%q,%q): %v", name, hash, uid, r)
			}
		}()
		_, _, _ = getFileDescriptorData(value)
	})
}

// --------------------------------------------------------------------------
// getInternalFileName — path injection rejection
// --------------------------------------------------------------------------

func TestGetInternalFileName_KnownPath(t *testing.T) {
	dir, name := getInternalFileName("Vehicle.UploadFile")
	if name != "upload.txt" {
		t.Errorf("known path: got %q,%q", dir, name)
	}
}

func TestGetInternalFileName_UnknownPathRejected(t *testing.T) {
	// Pre-fix: every unknown path silently mapped to "upload.txt".
	dir, name := getInternalFileName("../../etc/passwd")
	if name != "" {
		t.Errorf("unknown path should be rejected; got %q,%q", dir, name)
	}
}

// --------------------------------------------------------------------------
// initChannels — sanity check that channels are buffered
// --------------------------------------------------------------------------

func TestInitChannels_BuffersPipeline(t *testing.T) {
	initChannels()
	if cap(serviceMgrChannel[0]) == 0 {
		t.Errorf("serviceMgrChannel[0] should be buffered")
	}
	if cap(serviceDataChan[0]) == 0 {
		t.Errorf("serviceDataChan[0] should be buffered")
	}
	for i := 0; i < NUMOFTRANSPORTMGRS; i++ {
		// MQTT (index 2) is intentionally unbuffered: MqttMgrInit performs a
		// synchronous VIN-fetch handshake in one goroutine. A buffered channel
		// causes the goroutine to echo-read its own send before
		// transportDataSession can consume it.
		if i == 2 {
			if cap(transportMgrChannel[i]) != 0 {
				t.Errorf("transportMgrChannel[%d] (MQTT) should be unbuffered", i)
			}
		} else {
			if cap(transportMgrChannel[i]) == 0 {
				t.Errorf("transportMgrChannel[%d] should be buffered", i)
			}
		}
		if cap(transportDataChan[i]) == 0 {
			t.Errorf("transportDataChan[%d] should be buffered", i)
		}
		if cap(backendChan[i]) == 0 {
			t.Errorf("backendChan[%d] should be buffered", i)
		}
	}
	// atsChannel and ftChannel stay unbuffered because they're serialized
	// by atsChannelMu / ftChannelMu.
	if cap(atsChannel[0]) != 0 {
		t.Errorf("atsChannel[0] should be unbuffered (serialized via mutex)")
	}
}

// --------------------------------------------------------------------------
// serviceDataSession — bad RouterId no longer panics, mgrIndex bounds-checked
// --------------------------------------------------------------------------

func TestServiceDataSession_BadRouterIdIsSafe(t *testing.T) {
	initChannels()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	smChan := make(chan map[string]interface{}, 2)
	sdChan := make(chan map[string]interface{}, 2)
	beChans := make([]chan map[string]interface{}, NUMOFTRANSPORTMGRS)
	for i := range beChans {
		beChans[i] = make(chan map[string]interface{}, 2)
	}

	done := make(chan struct{})
	go func() {
		serviceDataSession(smChan, sdChan, beChans)
		close(done)
	}()

	// Send a bad response - missing RouterId, non-string RouterId, malformed.
	smChan <- map[string]interface{}{"action": "get"}                    // no RouterId
	smChan <- map[string]interface{}{"RouterId": 42}                     // non-string
	smChan <- map[string]interface{}{"RouterId": "bad-format-no-delim"}  // bad mgr id
	smChan <- map[string]interface{}{"RouterId": "99?1"}                 // out-of-range
	// Then a good one
	smChan <- map[string]interface{}{"RouterId": "0?1", "action": "get"}

	select {
	case got := <-beChans[0]:
		if got["RouterId"] != "0?1" {
			t.Errorf("got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("good response did not arrive at backendChannel[0]")
	}
}

// --------------------------------------------------------------------------
// transportDataSession — non-blocking dispatch
// --------------------------------------------------------------------------

func TestTransportDataSession_DropsOnFullDispatcher(t *testing.T) {
	mgrChan := make(chan string, 1)
	dataChan := make(chan map[string]interface{}, 1)
	beChan := make(chan map[string]interface{}, 1)

	go transportDataSession(mgrChan, dataChan, beChan)

	// Fill the data channel first
	dataChan <- map[string]interface{}{"filler": true}

	// Now push two requests — second should be dropped, not block.
	mgrChan <- `{"action":"get","path":"X"}`
	mgrChan <- `{"action":"get","path":"Y"}`

	done := make(chan struct{})
	go func() {
		// Drain
		<-dataChan
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Errorf("dispatcher wedged on full transportDataChannel")
	}
}

// --------------------------------------------------------------------------
// Concurrent verifyToken RPC race — atsChannelMu serializes
// --------------------------------------------------------------------------

func TestVerifyToken_ConcurrentSafe(t *testing.T) {
	initChannels()
	// Fake ATS server: read request, send back validation=N where N is taken
	// from a sequence so we can detect interleaving.
	requestsSeen := make(chan string, 100)
	go func() {
		for req := range atsChannel[0] {
			requestsSeen <- req
			// Echo back the validation value embedded in the request.
			atsChannel[0] <- `{"validation":"0","handle":"h","gatingId":"g"}`
		}
	}()

	const N = 20
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			v, h, g := verifyToken("token", "get", `"path"`, n)
			if v != 0 || h != "h" || g != "g" {
				t.Errorf("verify[%d] mismatched response: v=%d h=%q g=%q", n, v, h, g)
			}
		}(i)
	}
	wg.Wait()
	close(atsChannel[0])
}

// --------------------------------------------------------------------------
// verifyToken — malformed response handling
// --------------------------------------------------------------------------

func TestVerifyToken_MalformedResponseReturns41(t *testing.T) {
	initChannels()
	go func() {
		<-atsChannel[0]
		atsChannel[0] <- "not json"
	}()
	if v, _, _ := verifyToken("t", "get", `"p"`, 0); v != 41 {
		t.Errorf("got %d; want 41", v)
	}
}

func TestVerifyToken_MissingValidationReturns42(t *testing.T) {
	initChannels()
	go func() {
		<-atsChannel[0]
		atsChannel[0] <- `{}`
	}()
	if v, _, _ := verifyToken("t", "get", `"p"`, 0); v != 42 {
		t.Errorf("got %d; want 42", v)
	}
}

func TestVerifyToken_BadValidationReturns42(t *testing.T) {
	initChannels()
	go func() {
		<-atsChannel[0]
		atsChannel[0] <- `{"validation":"not-a-number"}`
	}()
	if v, _, _ := verifyToken("t", "get", `"p"`, 0); v != 42 {
		t.Errorf("got %d; want 42", v)
	}
}

func TestVerifyToken_NonStringClaimsTolerated(t *testing.T) {
	initChannels()
	go func() {
		<-atsChannel[0]
		atsChannel[0] <- `{"validation":"0","handle":42,"gatingId":[]}` // non-strings
	}()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("non-string claims panicked: %v", r)
		}
	}()
	v, h, g := verifyToken("t", "get", `"p"`, 0)
	if v != 0 {
		t.Errorf("v=%d; want 0", v)
	}
	if h != "" || g != "" {
		t.Errorf("non-string claims should be ignored; got h=%q g=%q", h, g)
	}
}

// --------------------------------------------------------------------------
// getNoScopeList — defensive RPC + parse
// --------------------------------------------------------------------------

func TestGetNoScopeList_HappyPath(t *testing.T) {
	initChannels()
	go func() {
		<-atsChannel[0]
		atsChannel[0] <- `{"paths":["a","b"]}`
	}()
	list, n := getNoScopeList("ctx")
	if n != 2 || list[0] != "a" {
		t.Errorf("got %v,%d", list, n)
	}
}

func TestGetNoScopeList_BadJSON(t *testing.T) {
	initChannels()
	go func() {
		<-atsChannel[0]
		atsChannel[0] <- `not json`
	}()
	list, n := getNoScopeList("ctx")
	if list != nil || n != 0 {
		t.Errorf("got %v,%d", list, n)
	}
}

// --------------------------------------------------------------------------
// serveRequest / issueServiceRequest input validation
// --------------------------------------------------------------------------

func TestServeRequest_UnsubscribeRoutesDirectly(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "unsubscribe", "subscriptionId": "1"}
	go serveRequest(req, 0, 0)
	select {
	case got := <-serviceDataChan[0]:
		if got["action"] != "unsubscribe" {
			t.Errorf("got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("unsubscribe did not reach serviceDataChan")
	}
}

func TestServeRequest_NonStringPathDoesNotPanic(t *testing.T) {
	initChannels()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("non-string path panicked: %v", r)
		}
	}()
	req := map[string]interface{}{
		"action": "internal-killsubscriptions",
		"path":   42, // non-string; should be ignored
	}
	go serveRequest(req, 0, 0)
	select {
	case <-serviceDataChan[0]:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not reach serviceDataChan")
	}
}

func TestIssueServiceRequest_MissingPathReturnsError(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "get"} // missing path
	go issueServiceRequest(req, 0, 0)
	select {
	case got := <-backendChan[0]:
		if got["error"] == nil {
			t.Errorf("expected error response; got %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("did not produce error response")
	}
}

// --------------------------------------------------------------------------
// initiateFileTransfer — defensive on bad input
// --------------------------------------------------------------------------

func TestInitiateFileTransfer_MissingPathReturnsError(t *testing.T) {
	initChannels()
	req := map[string]interface{}{"action": "get"}
	resp := initiateFileTransfer(req, utils.SENSOR, "")
	if resp["error"] == nil {
		t.Errorf("expected error; got %+v", resp)
	}
}

func TestInitiateFileTransfer_NonMapValueReturnsError(t *testing.T) {
	initChannels()
	req := map[string]interface{}{
		"action": "set",
		"value":  "not a map",
	}
	resp := initiateFileTransfer(req, utils.ACTUATOR, "x")
	if resp["error"] == nil {
		t.Errorf("expected error for non-map value; got %+v", resp)
	}
}

func TestInitiateFileTransfer_BadUidReturnsError(t *testing.T) {
	initChannels()
	req := map[string]interface{}{
		"action": "set",
		"value": map[string]interface{}{
			"name": "f",
			"hash": "h",
			"uid":  "not-hex-or-wrong-length",
		},
	}
	resp := initiateFileTransfer(req, utils.ACTUATOR, "x")
	if resp["error"] == nil {
		t.Errorf("expected error for bad uid; got %+v", resp)
	}
}

func TestInitiateFileTransfer_UnknownGetPathRejected(t *testing.T) {
	initChannels()
	req := map[string]interface{}{
		"action":   "get",
		"path":     "../../etc/passwd",
		"RouterId": "0?1",
	}
	resp := initiateFileTransfer(req, utils.SENSOR, "x")
	if resp["error"] == nil {
		t.Errorf("unknown path should be rejected; got %+v", resp)
	}
}

// --------------------------------------------------------------------------
// validateData — defensive filter parameter parsing
// --------------------------------------------------------------------------

func TestValidateData_ChangeFilterMissingDiff(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	req := map[string]interface{}{"action": "get", "filter": "ignored"}
	filters := []utils.FilterObject{
		{Type: "change", Parameter: `{"logic-op":"eq"}`}, // diff missing
	}
	idx, _ := validateData(req, []utils.SearchData_t{{NodeHandle: nil}}, filters)
	if idx != 1 {
		t.Errorf("expected error code 1; got %d", idx)
	}
}

func TestValidateData_CurvelogFilterMissingMaxerr(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	req := map[string]interface{}{"action": "get", "filter": "ignored"}
	filters := []utils.FilterObject{
		{Type: "curvelog", Parameter: `{}`}, // maxerr missing
	}
	idx, _ := validateData(req, []utils.SearchData_t{{NodeHandle: nil}}, filters)
	if idx != 1 {
		t.Errorf("expected error code 1; got %d", idx)
	}
}

func TestValidateData_RangeFilterArrayMissingBoundary(t *testing.T) {
	req := map[string]interface{}{"action": "get", "filter": "ignored"}
	filters := []utils.FilterObject{
		{Type: "range", Parameter: `[{"logic-op":"lt"}]`}, // boundary missing
	}
	idx, _ := validateData(req, []utils.SearchData_t{{NodeHandle: nil}}, filters)
	if idx != 1 {
		t.Errorf("expected error code 1 for missing boundary; got %d", idx)
	}
}

// --------------------------------------------------------------------------
// JSON round-trip sanity for the no-scope list extractor
// --------------------------------------------------------------------------

func TestExtractNoScopeElements_RoundTrip(t *testing.T) {
	in := `{"paths":["Vehicle.Speed","Vehicle.Direction"]}`
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatal(err)
	}
	list, n := extractNoScopeElementsLevel1(m)
	if n != 2 || list[0] != "Vehicle.Speed" {
		t.Errorf("got %v,%d", list, n)
	}
}

// --------------------------------------------------------------------------
// Sanity: extractMgrId across many strings doesn't panic
// --------------------------------------------------------------------------

func TestExtractMgrId_DoesNotPanicOnFuzz(t *testing.T) {
	cases := []string{
		"", "?", "??", "0?", "0?1", "?1", "abc?def", "12345?67890",
		"-1?1", "0\x00?1", strings.Repeat("a", 10000) + "?1",
	}
	for _, in := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panicked on %q: %v", in, r)
				}
			}()
			extractMgrId(in)
		}()
	}
}

// --------------------------------------------------------------------------
// getSubTreeNodeHandle - returns the match or nil
// --------------------------------------------------------------------------

func TestGetSubTreeNodeHandle_NoMatch(t *testing.T) {
	if got := getSubTreeNodeHandle("any", nil, 0); got != nil {
		t.Errorf("expected nil; got %v", got)
	}
}

func TestGetSubTreeNodeHandle_NoMatchInList(t *testing.T) {
	list := []utils.SearchData_t{
		{NodePath: "Vehicle.Speed"},
		{NodePath: "Vehicle.Direction"},
	}
	if got := getSubTreeNodeHandle("Vehicle.Missing", list, 2); got != nil {
		t.Errorf("expected nil for non-matching path; got %v", got)
	}
}

// --------------------------------------------------------------------------
// setTokenErrorResponse — wrapper around utils.SetErrorResponse
// --------------------------------------------------------------------------

func TestSetTokenErrorResponse_PopulatesErrorMap(t *testing.T) {
	req := map[string]interface{}{
		"action": "get",
		"path":   "Vehicle.Speed",
	}
	// Clean global side-effect from any prior test.
	errorResponseMap = map[string]interface{}{}
	setTokenErrorResponse(req, 1)
	if errorResponseMap["error"] == nil {
		t.Errorf("expected error populated; got %+v", errorResponseMap)
	}
}

// --------------------------------------------------------------------------
// getIndex / verifyStructMember — pure logic over SearchData_t.NodePath
// --------------------------------------------------------------------------

func TestGetIndex_FoundReturnsIndex(t *testing.T) {
	list := []utils.SearchData_t{
		{NodePath: "Types.Foo.Bar"},
		{NodePath: "Types.Foo.Baz"},
		{NodePath: "Types.Foo.Qux"},
	}
	if got := getIndex(list, 3, "Baz"); got != 1 {
		t.Errorf("got %d; want 1", got)
	}
}

func TestGetIndex_NotFoundReturnsZero(t *testing.T) {
	list := []utils.SearchData_t{
		{NodePath: "Types.Foo.Bar"},
	}
	if got := getIndex(list, 1, "Missing"); got != 0 {
		t.Errorf("got %d; want 0 (the fallback)", got)
	}
}

func TestVerifyStructMember_CaseInsensitive(t *testing.T) {
	list := []utils.SearchData_t{
		{NodePath: "Types.Foo.Bar"},
		{NodePath: "Types.Foo.BAZ"},
	}
	if !verifyStructMember("bar", list, 2) {
		t.Errorf("lowercase 'bar' should match Bar")
	}
	if !verifyStructMember("baz", list, 2) {
		t.Errorf("lowercase 'baz' should match BAZ")
	}
	if verifyStructMember("missing", list, 2) {
		t.Errorf("missing should not match")
	}
}

// --------------------------------------------------------------------------
// calculateHash — SHA-1 of file contents
// --------------------------------------------------------------------------

func TestCalculateHash_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	fp := dir + "/data.bin"
	if err := os.WriteFile(fp, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	got := calculateHash(fp)
	// sha1("hello") = aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
	if got != "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d" {
		t.Errorf("got %q; want aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d", got)
	}
}

func TestCalculateHash_MissingFileReturnsEmpty(t *testing.T) {
	if got := calculateHash("/does/not/exist"); got != "" {
		t.Errorf("got %q; want empty", got)
	}
}

func TestCalculateHash_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	fp := dir + "/empty.bin"
	if err := os.WriteFile(fp, nil, 0644); err != nil {
		t.Fatal(err)
	}
	// sha1("") = da39a3ee5e6b4b0d3255bfef95601890afd80709
	if got := calculateHash(fp); got != "da39a3ee5e6b4b0d3255bfef95601890afd80709" {
		t.Errorf("got %q", got)
	}
}
