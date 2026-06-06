/**
* Regression tests for the agt_server fixes shipped in PR #119
* (jtiCacheMu race fix; MaxBytesReader on body).
**/
package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises utils.Info / utils.Error so the agt_server
// handler can log without nil-deref under test conditions.
func TestMain(m *testing.M) {
	utils.InitLog("agtServer-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// TestAgtServerHandler_RejectsOversizedBody is the regression test for
// the PR #119 MaxBytesReader on /agts. Before the fix, the AGT endpoint
// (which is reachable pre-auth — its purpose is to issue access-grant
// tokens) used io.ReadAll on the bare req.Body and any anonymous peer
// could OOM the daemon by sending a giant or chunked body.
func TestAgtServerHandler_RejectsOversizedBody(t *testing.T) {
	// The handler does serverChannel <- bodyBytes then waits on the
	// channel. The oversize path returns early without sending, so the
	// channel buffer can be tiny.
	handler := makeAgtServerHandler(make(chan string, 4))

	// 256 KiB — well over the 64 KiB MaxBytesReader cap.
	body := bytes.NewReader(make([]byte, 256*1024))
	req := httptest.NewRequest("POST", "/agts", body)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected %d (Request Entity Too Large); got %d: %s",
			http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	}
}

// TestAgtServerHandler_RejectsWrongPath sanity-checks the early-404
// path (orthogonal to body-size, but exercises the same closure).
func TestAgtServerHandler_RejectsWrongPath(t *testing.T) {
	handler := makeAgtServerHandler(make(chan string, 4))
	req := httptest.NewRequest("POST", "/wrong-path", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong path; got %d", rec.Code)
	}
}

// ── allowedOriginHeader ──────────────────────────────────────────────────────

func TestAllowedOriginHeader_EmptyListReturnsWildcard(t *testing.T) {
	saved := agtAllowedOrigins
	agtAllowedOrigins = nil
	defer func() { agtAllowedOrigins = saved }()

	req, _ := http.NewRequest("GET", "/agts", nil)
	if got := allowedOriginHeader(req); got != "*" {
		t.Errorf("want *, got %q", got)
	}
}

func TestAllowedOriginHeader_MatchingOriginReturned(t *testing.T) {
	saved := agtAllowedOrigins
	agtAllowedOrigins = []string{"https://example.com", "https://other.com"}
	defer func() { agtAllowedOrigins = saved }()

	req, _ := http.NewRequest("GET", "/agts", nil)
	req.Header.Set("Origin", "https://example.com")
	if got := allowedOriginHeader(req); got != "https://example.com" {
		t.Errorf("want https://example.com, got %q", got)
	}
}

func TestAllowedOriginHeader_NonMatchingReturnsEmpty(t *testing.T) {
	saved := agtAllowedOrigins
	agtAllowedOrigins = []string{"https://example.com"}
	defer func() { agtAllowedOrigins = saved }()

	req, _ := http.NewRequest("GET", "/agts", nil)
	req.Header.Set("Origin", "https://evil.com")
	if got := allowedOriginHeader(req); got != "" {
		t.Errorf("want empty string, got %q", got)
	}
}

// ── role checkers ─────────────────────────────────────────────────────────────

func TestCheckUserRole(t *testing.T) {
	valid := []string{"OEM", "Dealer", "Independent", "Owner", "Driver", "Passenger"}
	for _, r := range valid {
		if !checkUserRole(r) {
			t.Errorf("checkUserRole(%q) = false, want true", r)
		}
	}
	if checkUserRole("Hacker") {
		t.Error("checkUserRole(Hacker) = true, want false")
	}
}

func TestCheckAppRole(t *testing.T) {
	if !checkAppRole("OEM") || !checkAppRole("Third party") {
		t.Error("valid app roles rejected")
	}
	if checkAppRole("Unknown") {
		t.Error("invalid app role accepted")
	}
}

func TestCheckDeviceRole(t *testing.T) {
	valid := []string{"Vehicle", "Nomadic", "Cloud"}
	for _, r := range valid {
		if !checkDeviceRole(r) {
			t.Errorf("checkDeviceRole(%q) = false, want true", r)
		}
	}
	if checkDeviceRole("Server") {
		t.Error("checkDeviceRole(Server) = true, want false")
	}
}

func TestCheckRoles_Valid(t *testing.T) {
	cases := []string{
		"Owner+OEM+Vehicle",
		"Driver+Third party+Nomadic",
		"OEM+OEM+Cloud",
	}
	for _, c := range cases {
		if !checkRoles(c) {
			t.Errorf("checkRoles(%q) = false, want true", c)
		}
	}
}

func TestCheckRoles_Invalid(t *testing.T) {
	cases := []string{
		"",
		"NoPlusAtAll",
		"Owner+OEM",             // only one +
		"BadUser+OEM+Vehicle",
		"Owner+BadApp+Vehicle",
		"Owner+OEM+BadDevice",
	}
	for _, c := range cases {
		if checkRoles(c) {
			t.Errorf("checkRoles(%q) = true, want false", c)
		}
	}
}

// ── authenticateClient ────────────────────────────────────────────────────────

func TestAuthenticateClient_NoDevKey(t *testing.T) {
	saved := agtDevKey
	agtDevKey = ""
	defer func() { agtDevKey = saved }()

	p := Payload{Context: "Owner+OEM+Vehicle", Proof: "anything"}
	if authenticateClient(p) {
		t.Error("should refuse when VISSR_AGT_DEV_KEY is unset")
	}
}

func TestAuthenticateClient_BadContext(t *testing.T) {
	saved := agtDevKey
	agtDevKey = "secret"
	defer func() { agtDevKey = saved }()

	p := Payload{Context: "invalid-context", Proof: "secret"}
	if authenticateClient(p) {
		t.Error("should refuse bad context")
	}
}

func TestAuthenticateClient_WrongProof(t *testing.T) {
	saved := agtDevKey
	agtDevKey = "secret"
	defer func() { agtDevKey = saved }()

	p := Payload{Context: "Owner+OEM+Vehicle", Proof: "wrong"}
	if authenticateClient(p) {
		t.Error("should refuse wrong proof")
	}
}

func TestAuthenticateClient_ValidCredentials(t *testing.T) {
	saved := agtDevKey
	agtDevKey = "correct-key"
	defer func() { agtDevKey = saved }()

	p := Payload{Context: "Owner+OEM+Vehicle", Proof: "correct-key"}
	if !authenticateClient(p) {
		t.Error("should accept valid context and proof")
	}
}

// ── deleteJtiNow ──────────────────────────────────────────────────────────────

func TestDeleteJtiNow_RemovesFromCache(t *testing.T) {
	jtiCacheMu.Lock()
	jtiCache = map[string]struct{}{"del-me": {}}
	jtiCacheOrder = []string{"del-me"}
	jtiCacheMu.Unlock()

	deleteJtiNow("del-me")

	jtiCacheMu.Lock()
	_, exists := jtiCache["del-me"]
	jtiCacheMu.Unlock()
	if exists {
		t.Error("deleteJtiNow did not remove the JTI")
	}
}

func TestDeleteJtiNow_MissingJtiIsNoop(t *testing.T) {
	jtiCacheMu.Lock()
	jtiCache = map[string]struct{}{}
	jtiCacheOrder = nil
	jtiCacheMu.Unlock()

	// Must not panic on missing key.
	deleteJtiNow("ghost")
}

// ── getUUID ───────────────────────────────────────────────────────────────────

func TestGetUUID_NonEmpty(t *testing.T) {
	id := getUUID()
	if id == "" {
		t.Error("getUUID returned empty string")
	}
}

func TestGetUUID_UniquePerCall(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := getUUID()
		if seen[id] {
			t.Fatalf("duplicate UUID on iteration %d: %q", i, id)
		}
		seen[id] = true
	}
}

// ── generateResponse (malformed input path) ───────────────────────────────────

func TestGenerateResponse_MalformedJSON(t *testing.T) {
	got := generateResponse("not-valid-json", "")
	if !strings.Contains(got, "error") {
		t.Errorf("expected error response for malformed JSON, got %q", got)
	}
}

func TestGenerateResponse_NoDevKeyRefuses(t *testing.T) {
	saved := agtDevKey
	agtDevKey = ""
	defer func() { agtDevKey = saved }()

	input := `{"vin":"VIN1","context":"Owner+OEM+Vehicle","proof":"anything","key":""}`
	got := generateResponse(input, "")
	if !strings.Contains(got, "error") {
		t.Errorf("expected error response when dev key unset, got %q", got)
	}
}

// ── addCheckJti_AcceptsNewRejectsReplay is the basic-semantics check
// on the jti replay cache (mirrors the equivalent test in atServer).
func TestAddCheckJti_AcceptsNewRejectsReplay(t *testing.T) {
	jtiCacheMu.Lock()
	jtiCache = nil
	jtiCacheMu.Unlock()

	if !addCheckJti("jti-1") {
		t.Fatalf("first addCheckJti must accept a new jti")
	}
	if addCheckJti("jti-1") {
		t.Fatalf("second addCheckJti must reject a replayed jti")
	}
	if !addCheckJti("jti-2") {
		t.Fatalf("a different jti must be accepted")
	}
}

// TestAddCheckJti_ConcurrentSafe is the regression test for the PR #119
// jtiCacheMu mutex. Before that fix, two concurrent AGT requests racing
// the map would abort the daemon with "concurrent map read and map
// write" — deterministic remote crash from any anonymous network peer.
//
// Run with: go test -race
func TestAddCheckJti_ConcurrentSafe(t *testing.T) {
	jtiCacheMu.Lock()
	jtiCache = nil
	jtiCacheMu.Unlock()

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	results := make([]bool, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				results[i] = addCheckJti("shared-jti")
			} else {
				results[i] = addCheckJti("jti-other")
			}
		}(i)
	}
	wg.Wait()

	acceptedShared := 0
	for i, ok := range results {
		if i%2 == 0 && ok {
			acceptedShared++
		}
	}
	if acceptedShared != 1 {
		t.Fatalf("expected exactly 1 acceptance of shared-jti across %d concurrent calls; got %d", n/2, acceptedShared)
	}
}

// TestAddCheckJti_EvictsOldestWhenFull covers the cache-eviction path:
// when the map already has MAX_JTI_CACHE_SIZE entries, adding a new JTI
// removes the oldest one so the cap is maintained.
func TestAddCheckJti_EvictsOldestWhenFull(t *testing.T) {
	jtiCacheMu.Lock()
	jtiCache = make(map[string]struct{}, MAX_JTI_CACHE_SIZE)
	jtiCacheOrder = make([]string, 0, MAX_JTI_CACHE_SIZE)
	for i := 0; i < MAX_JTI_CACHE_SIZE; i++ {
		key := fmt.Sprintf("jti-fill-%d", i)
		jtiCache[key] = struct{}{}
		jtiCacheOrder = append(jtiCacheOrder, key)
	}
	jtiCacheMu.Unlock()

	oldest := "jti-fill-0"
	result := addCheckJti("brand-new-jti")
	if !result {
		t.Fatal("addCheckJti should return true for a new JTI when evicting")
	}

	jtiCacheMu.Lock()
	size := len(jtiCache)
	_, oldestPresent := jtiCache[oldest]
	jtiCacheMu.Unlock()

	if size != MAX_JTI_CACHE_SIZE {
		t.Errorf("cache size = %d after eviction; want %d", size, MAX_JTI_CACHE_SIZE)
	}
	if oldestPresent {
		t.Error("oldest JTI should have been evicted but is still present")
	}
}

// ── makeAgtServerHandler additional paths ────────────────────────────────────

func TestAgtServerHandler_OptionsMethod_SetsCORSHeaders(t *testing.T) {
	// The OPTIONS preflight path sets CORS headers and returns 200.
	ch := make(chan string, 4)
	handler := makeAgtServerHandler(ch)
	req := httptest.NewRequest("OPTIONS", "/agts", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("OPTIONS returned %d; want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("OPTIONS response missing Access-Control-Allow-Methods header")
	}
}

func TestAgtServerHandler_OptionsBlockedOrigin_NoCORSHeader(t *testing.T) {
	// When agtAllowedOrigins is set but the request Origin doesn't match,
	// the CORS header is NOT added (allowedOriginHeader returns "").
	saved := agtAllowedOrigins
	agtAllowedOrigins = []string{"https://allowed.com"}
	defer func() { agtAllowedOrigins = saved }()

	ch := make(chan string, 4)
	handler := makeAgtServerHandler(ch)
	req := httptest.NewRequest("OPTIONS", "/agts", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("OPTIONS returned %d; want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("blocked origin should not appear in CORS header")
	}
}

func TestAgtServerHandler_BadMethod_Returns400(t *testing.T) {
	ch := make(chan string, 4)
	handler := makeAgtServerHandler(ch)
	req := httptest.NewRequest("DELETE", "/agts", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE returned %d; want 400", rec.Code)
	}
}

func TestAgtServerHandler_PostWithPopHeader(t *testing.T) {
	// PoP header logging path: pop != "" → Info.Printf.
	ch := make(chan string)
	go func() {
		<-ch // body
		pop := <-ch
		if pop == "" {
			ch <- "" // signal failure so test can detect it
			return
		}
		ch <- `{"action":"agt-request","pop-covered":true}`
	}()

	handler := makeAgtServerHandler(ch)
	body := strings.NewReader(`{"vin":"V","context":"Owner+OEM+Vehicle","proof":"p","key":""}`)
	req := httptest.NewRequest("POST", "/agts", body)
	req.Header.Set("PoP", "somepoptoken")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 201 {
		t.Errorf("POST with PoP returned %d; want 201", rec.Code)
	}
}

// errReader is an io.Reader that always returns a non-MaxBytes error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, fmt.Errorf("simulated read error") }

func TestAgtServerHandler_ReadErrorNotMaxBytes_Returns400(t *testing.T) {
	ch := make(chan string, 4)
	handler := makeAgtServerHandler(ch)
	req := httptest.NewRequest("POST", "/agts", errReader{})
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("read error returned %d; want 400", rec.Code)
	}
}

func TestAgtServerHandler_PostWithEmptyResponse_Returns400(t *testing.T) {
	// Consumer sends back "" — handler must return 400 bad input.
	ch := make(chan string)
	go func() {
		<-ch // body
		<-ch // pop
		ch <- "" // empty response
	}()

	handler := makeAgtServerHandler(ch)
	body := strings.NewReader(`{"vin":"VIN1","context":"Owner+OEM+Vehicle","proof":"p","key":""}`)
	req := httptest.NewRequest("POST", "/agts", body)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty response returned %d; want 400", rec.Code)
	}
}

func TestAgtServerHandler_SuccessfulPost_ReturnsResponse(t *testing.T) {
	// The handler uses the channel as a synchronous body→pop→response protocol.
	// An unbuffered channel ensures the goroutine consumer and handler stay in
	// lockstep; with a buffered channel the handler would read back its own body.
	ch := make(chan string)
	go func() {
		<-ch // body
		<-ch // pop
		ch <- `{"action":"agt-request","token":"faketoken"}`
	}()

	handler := makeAgtServerHandler(ch)
	body := strings.NewReader(`{"vin":"VIN1","context":"Owner+OEM+Vehicle","proof":"p","key":""}`)
	req := httptest.NewRequest("POST", "/agts", body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != 201 {
		t.Errorf("POST returned %d; want 201", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "agt-request") {
		t.Errorf("response body missing expected token: %q", rec.Body.String())
	}
}
