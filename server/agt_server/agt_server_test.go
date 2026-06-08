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
	"os/exec"
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

// ── generateResponse — pop != "" path calls generateLTAgt ────────────────────
//
// generateLTAgt requires an RSA private key for signing (integration-only).
// We exercise the pop != "" branch up to the PopToken.Unmarshal error path,
// which returns immediately with a malformed-pop error and does not need
// the RSA key.

func TestGenerateResponse_WithMalformedPop(t *testing.T) {
	saved := agtDevKey
	agtDevKey = "mykey"
	defer func() { agtDevKey = saved }()

	input := `{"vin":"VIN1","context":"Owner+OEM+Vehicle","proof":"mykey","key":""}`
	// A non-empty pop that cannot be unmarshalled.
	got := generateResponse(input, "not-a-valid-pop-token")
	if !strings.Contains(got, "error") {
		t.Errorf("malformed pop should return error response; got %q", got)
	}
}

// ── checkRoles — defensive delimiter branches ─────────────────────────────────
//
// The strings.Count guard in checkRoles ensures we always have exactly two '+'
// before the delimiter parsing, so delimiter1 == -1 and delimiter2 == -1 are
// unreachable in practice. The existing TestCheckRoles_Invalid table already
// exercises the Count != 2 early-return path. We add one extra case to make
// sure the "only one +" path is not accidentally passing for the wrong reason.

func TestCheckRoles_OnePlus_Returns_False(t *testing.T) {
	// Only one '+' → Count != 2 → return false immediately.
	if checkRoles("Owner+OEM") {
		t.Error("checkRoles(\"Owner+OEM\") should return false (only one '+')")
	}
}

func TestCheckRoles_ThreePlus_Returns_False(t *testing.T) {
	// Three '+' → Count != 2 → return false immediately.
	if checkRoles("Owner+OEM+Vehicle+Extra") {
		t.Error("checkRoles with three '+' should return false")
	}
}

// ── allowedOriginHeader — with-list path for POST response ───────────────────

func TestAgtServerHandler_PostWithAllowedOrigin_SetsCorsHeader(t *testing.T) {
	saved := agtAllowedOrigins
	agtAllowedOrigins = []string{"https://allowed.com"}
	defer func() { agtAllowedOrigins = saved }()

	ch := make(chan string)
	go func() {
		<-ch // body
		<-ch // pop
		ch <- `{"action":"agt-request","token":"tok"}`
	}()

	handler := makeAgtServerHandler(ch)
	body := strings.NewReader(`{"vin":"V","context":"Owner+OEM+Vehicle","proof":"p","key":""}`)
	req := httptest.NewRequest("POST", "/agts", body)
	req.Header.Set("Origin", "https://allowed.com")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != 201 {
		t.Errorf("POST returned %d; want 201", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.com" {
		t.Errorf("CORS header = %q; want https://allowed.com", got)
	}
}

func TestAgtServerHandler_PostWithBlockedOrigin_NoCorsHeader(t *testing.T) {
	saved := agtAllowedOrigins
	agtAllowedOrigins = []string{"https://allowed.com"}
	defer func() { agtAllowedOrigins = saved }()

	ch := make(chan string)
	go func() {
		<-ch // body
		<-ch // pop
		ch <- `{"action":"agt-request","token":"tok"}`
	}()

	handler := makeAgtServerHandler(ch)
	body := strings.NewReader(`{"vin":"V","context":"Owner+OEM+Vehicle","proof":"p","key":""}`)
	req := httptest.NewRequest("POST", "/agts", body)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != 201 {
		t.Errorf("POST returned %d; want 201", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("blocked origin should not get CORS header; got %q", got)
	}
}

// ── init() — VISSR_AGT_ALLOWED_ORIGIN branch ─────────────────────────────────
//
// init() runs exactly once at package load time, which means we cannot set the
// env var after the fact in the same process. To cover the
// `VISSR_AGT_ALLOWED_ORIGIN != ""` branch (lines 98-103) we re-exec the test
// binary as a child process with the env var set. The child sets the var before
// the package init runs, giving us coverage of the origins-parsing loop.
//
// The subprocess signals success by printing "INIT_SUBPROCESS_OK" to stdout and
// exiting 0. Any panic or unexpected exit causes the parent test to fail.

func TestInit_AllowedOriginBranch_Subprocess(t *testing.T) {
	if os.Getenv("AGT_INIT_SUBPROCESS") == "1" {
		// We are the child: the package init() already ran with
		// VISSR_AGT_ALLOWED_ORIGIN set (set in the child env below).
		// Verify the package variable was populated.
		if len(agtAllowedOrigins) == 0 {
			fmt.Println("INIT_SUBPROCESS_FAIL: agtAllowedOrigins is empty")
			os.Exit(1)
		}
		found := false
		for _, o := range agtAllowedOrigins {
			if o == "https://example.com" {
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("INIT_SUBPROCESS_FAIL: expected https://example.com in %v\n", agtAllowedOrigins)
			os.Exit(1)
		}
		fmt.Println("INIT_SUBPROCESS_OK")
		os.Exit(0)
	}

	// Parent: re-exec this specific test in a child process with the env var set.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := &exec.Cmd{
		Path: exe,
		Args: []string{exe, "-test.run=TestInit_AllowedOriginBranch_Subprocess", "-test.v"},
		Env: append(os.Environ(),
			"AGT_INIT_SUBPROCESS=1",
			"VISSR_AGT_ALLOWED_ORIGIN=https://example.com, https://other.com",
		),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\nOutput:\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "INIT_SUBPROCESS_OK") {
		t.Fatalf("subprocess did not print INIT_SUBPROCESS_OK:\n%s", string(out))
	}
}

// ── init() — VISSR_AGT_ALLOWED_ORIGIN with trimming of whitespace-only entries ──
//
// A comma-separated list like " , https://a.com , " should only add
// non-empty trimmed entries. We verify this via the same subprocess approach.

func TestInit_AllowedOriginTrimming_Subprocess(t *testing.T) {
	if os.Getenv("AGT_INIT_TRIM_SUBPROCESS") == "1" {
		// Only "https://a.com" should appear — empty segments dropped.
		if len(agtAllowedOrigins) != 1 || agtAllowedOrigins[0] != "https://a.com" {
			fmt.Printf("INIT_TRIM_FAIL: got %v\n", agtAllowedOrigins)
			os.Exit(1)
		}
		fmt.Println("INIT_TRIM_OK")
		os.Exit(0)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := &exec.Cmd{
		Path: exe,
		Args: []string{exe, "-test.run=TestInit_AllowedOriginTrimming_Subprocess", "-test.v"},
		Env: append(os.Environ(),
			"AGT_INIT_TRIM_SUBPROCESS=1",
			"VISSR_AGT_ALLOWED_ORIGIN= , https://a.com , ",
		),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\nOutput:\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "INIT_TRIM_OK") {
		t.Fatalf("subprocess did not print INIT_TRIM_OK:\n%s", string(out))
	}
}

// ── checkRoles — defensive delimiter branches ─────────────────────────────────
//
// The two `if delimiter == -1 { return false }` guards at lines 285 and 289–290
// are unreachable in practice: the `strings.Count(context, "+") != 2` gate at
// line 281 guarantees that strings.Index will always find '+' for a valid
// 2-plus string. The existing TestCheckRoles_Invalid table already tests the
// strings.Count path and all invalid role combinations. These branches are
// intentionally left uncovered (they are defensive dead code against future
// refactors), documented here for the coverage audit record.

// ── getUUID error path ────────────────────────────────────────────────────────
//
// getUUID calls uuid.NewRandom() which relies on crypto/rand. The error path
// (return "") is only triggered when the OS random source fails — practically
// unreachable and not testable without mocking the uuid package. We document it
// rather than add an artificial injection mechanism.

// ── generateResponse — generateAgt / generateLTAgt paths ─────────────────────
//
// When authenticateClient succeeds and pop == "", generateResponse calls
// generateAgt, which requires a valid RSA private key stored in privKey.
// When pop != "" and the PoP token is valid, it calls generateLTAgt, which
// also requires privKey. Both are integration-only (the key is loaded/generated
// by initKey from the filesystem at startup). These branches are documented
// rather than tested without a live key.

// ── Integration-only functions — documented as such (not unit-tested) ────────
//
//   - main              calls initKey + initAgtServer (blocks on http.ListenAndServe)
//   - initKey           imports or generates an RSA private key from the filesystem
//   - initAgtServer     calls http.ListenAndServeTLS / http.ListenAndServe (blocks)
//   - generateLTAgt     requires a valid RSA private key for RS256 signing
//   - generateAgt       requires a valid RSA private key for RS256 signing
//   - deleteJti         sleeps (GAP + LIFETIME + 5) seconds before evicting
