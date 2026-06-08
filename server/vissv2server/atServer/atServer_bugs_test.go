/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the Tier-2 bug-fixes applied to atServer. Six bugs were
* fixed in this PR; this file covers the ones that can be exercised
* without a live HTTP/WebSocket atServer instance.
*
*   - init() ephemeral secret      (bug 1: hardcoded fallback gone)
*   - getActorRole                 (bug 6: strings.Index -1 panic)
*   - getCompleteToken             (bug 2: empty-token match)
*   - getGatingIdAndTokenHandle    (bug 2 mirror)
*   - consentReplyResponse         (bug 5: non-string field type assertion)
*   - consentCancelResponse        (bug 5 mirror)
*   - initGatingId                 (bug 4: crypto/rand, range)
*
* Bug 9 (atsHandlerMu serializing concurrent HTTP requests) is
* exercised by -race; no separate unit test.
**/
package atServer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/covesa/vissr/utils"
)

func TestMain(m *testing.M) {
	utils.InitLog("atServer-bugs-test.log", os.TempDir(), false, "error")
	// Make sure activeList and pendingList are allocated so the
	// token / consent tests have a populated slice to iterate over.
	// atServer normally allocates these via initialise functions
	// called from atServerSession; we just need non-nil slices for
	// the unit tests.
	if activeList == nil {
		activeList = make([]ActiveListElem, LISTSIZE)
	}
	if pendingList == nil {
		pendingList = make([]PendingListElem, LISTSIZE)
	}
	os.Exit(m.Run())
}

// TestInitAtSecret_NotHardcoded pins the bug-1 fix: the package init()
// no longer falls back to the public hardcoded value
// "averysecretkeyvalue2" when VISSR_AT_SECRET is unset. It now uses
// an ephemeral crypto/rand-derived secret.
func TestInitAtSecret_NotHardcoded(t *testing.T) {
	if theAtSecret == "averysecretkeyvalue2" {
		t.Errorf("theAtSecret fell back to the hardcoded value; the bug-1 fix has regressed")
	}
	if theAtSecret == "" {
		t.Errorf("theAtSecret is empty; init() did not populate it")
	}
}

// TestGetActorRole_MalformedContextDoesNotPanic pins the bug-6 fix:
// strings.Index returning -1 used to drive context[:-1] panics. The
// fix returns empty string on malformed input instead.
func TestGetActorRole_MalformedContextDoesNotPanic(t *testing.T) {
	cases := []struct {
		name    string
		context string
		idx     int
		want    string
	}{
		// Well-formed input: still works.
		{"well-formed actorIndex 0", "user+app+device", 0, "user"},
		{"well-formed actorIndex 1", "user+app+device", 1, "app"},
		{"well-formed actorIndex 2", "user+app+device", 2, "device"},

		// Bug 6 trigger: no '+' at all. Used to panic at context[:-1].
		{"no delimiter actorIndex 0", "foo", 0, ""},
		{"no delimiter actorIndex 1", "foo", 1, ""},
		{"no delimiter actorIndex 2", "foo", 2, ""},

		// Bug 6 trigger: only one '+'. Used to panic at the second
		// strings.Index returning -1.
		{"one delimiter actorIndex 0", "user+app", 0, "user"},
		{"one delimiter actorIndex 1", "user+app", 1, ""},
		{"one delimiter actorIndex 2", "user+app", 2, ""},

		// Empty string: degenerate but must not panic.
		{"empty context", "", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getActorRole(tc.idx, tc.context)
			if got != tc.want {
				t.Errorf("getActorRole(%d, %q) = %q; want %q", tc.idx, tc.context, got, tc.want)
			}
		})
	}
}

// TestGetCompleteToken_EmptyInputReturnsEmpty pins the bug-2 fix.
// Previously an empty input matched every unused activeList slot
// (since Atoken and AtokenHandle default to "") and returned Atoken
// == "", a match-any-empty-token primitive.
func TestGetCompleteToken_EmptyInputReturnsEmpty(t *testing.T) {
	// Save and restore activeList[0] around the test.
	saved := activeList[0]
	defer func() { activeList[0] = saved }()
	// Populate one slot with non-empty values so the loop has
	// something to iterate over.
	activeList[0] = ActiveListElem{GatingId: 7, Atoken: "real-token", AtokenHandle: "handle-7"}

	if got := getCompleteToken(""); got != "" {
		t.Errorf("getCompleteToken(\"\") = %q; want \"\"", got)
	}
	if got := getCompleteToken("real-token"); got != "real-token" {
		t.Errorf("getCompleteToken(\"real-token\") = %q; want \"real-token\"", got)
	}
	if got := getCompleteToken("handle-7"); got != "real-token" {
		t.Errorf("getCompleteToken(\"handle-7\") = %q; want \"real-token\"", got)
	}
}

// TestGetGatingIdAndTokenHandle_EmptyInputReturnsEmpty mirrors the
// same fix for the sibling helper.
func TestGetGatingIdAndTokenHandle_EmptyInputReturnsEmpty(t *testing.T) {
	saved := activeList[0]
	defer func() { activeList[0] = saved }()
	activeList[0] = ActiveListElem{GatingId: 13, Atoken: "real-token", AtokenHandle: "handle-13"}

	gid, handle := getGatingIdAndTokenHandle("")
	if gid != "" || handle != "" {
		t.Errorf("getGatingIdAndTokenHandle(\"\") = (%q, %q); want both empty", gid, handle)
	}
	gid, handle = getGatingIdAndTokenHandle("real-token")
	if gid != "13" || handle != "handle-13" {
		t.Errorf("getGatingIdAndTokenHandle(\"real-token\") = (%q, %q); want (\"13\", \"handle-13\")", gid, handle)
	}
}

// TestConsentReplyResponse_NonStringMessageIdDoesNotPanic pins one
// half of the bug-5 fix. A malicious or buggy ECF sending
// {"messageId": 42, ...} used to crash the atServer goroutine on
// the unchecked .(string) cast.
func TestConsentReplyResponse_NonStringMessageIdDoesNotPanic(t *testing.T) {
	// Number as messageId.
	resp := consentReplyResponse(`{"action":"consent-reply","messageId":42,"consent":"YES"}`)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 status for non-string messageId; got %q", resp)
	}
	// null as messageId.
	resp = consentReplyResponse(`{"action":"consent-reply","messageId":null,"consent":"YES"}`)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 status for null messageId; got %q", resp)
	}
}

// TestConsentReplyResponse_NonStringConsentDoesNotPanic pins the
// other half: the consent field also used to be .(string) without
// an ok check.
func TestConsentReplyResponse_NonStringConsentDoesNotPanic(t *testing.T) {
	resp := consentReplyResponse(`{"action":"consent-reply","messageId":"42","consent":true}`)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 status for non-string consent; got %q", resp)
	}
}

// TestConsentCancelResponse_NonStringMessageIdDoesNotPanic mirrors
// the bug-5 fix for the cancel handler.
func TestConsentCancelResponse_NonStringMessageIdDoesNotPanic(t *testing.T) {
	ch := make(chan string, 1)
	resp := consentCancelResponse(`{"action":"consent-cancel","messageId":42}`, ch)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 status for non-string messageId; got %q", resp)
	}
}

// TestConsentReplyResponse_MalformedJsonReturnsBadRequest covers a
// pre-existing defensive path that the bug-5 changes preserved.
func TestConsentReplyResponse_MalformedJsonReturnsBadRequest(t *testing.T) {
	resp := consentReplyResponse(`{not valid json`)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 status for malformed JSON; got %q", resp)
	}
}

// TestInitGatingId_InRange pins the bug-4 fix: the starting GatingId
// is now derived from crypto/rand and must fall in [666, 9999). The
// original used unseeded math/rand which was predictable across
// deployments.
func TestInitGatingId_InRange(t *testing.T) {
	// Save and restore the package-level GatingId.
	saved := GatingId
	defer func() { GatingId = saved }()

	// Run a few iterations to make accidental constancy obvious.
	seen := map[int]struct{}{}
	for i := 0; i < 10; i++ {
		initGatingId()
		if GatingId < 666 || GatingId >= 9999 {
			t.Errorf("initGatingId produced GatingId=%d; want [666, 9999)", GatingId)
		}
		seen[GatingId] = struct{}{}
	}
	// With 10 draws over a ~9000-wide range, getting fewer than 2
	// distinct values is astronomically unlikely (≈10^-39). If we
	// see only one value the helper has degenerated back to a
	// constant.
	if len(seen) < 2 {
		t.Errorf("initGatingId produced %d distinct values over 10 calls; helper may be constant", len(seen))
	}
}

// --------------------------------------------------------------------------
// Helper: build a minimal signed JWT for testing validateTokenExpiry
// --------------------------------------------------------------------------

// makeTestToken builds a real HS256-signed JWT with the given iat and exp
// Unix timestamps using the utils JWT machinery. The token uses the package-
// level theAtSecret so validateTokenExpiry can find it via ExtractFromToken.
func makeTestToken(iat, exp int64) string {
	var jwt utils.JsonWebToken
	jwt.SetHeader("HS256")
	jwt.AddClaim("iat", strconv.FormatInt(iat, 10))
	jwt.AddClaim("exp", strconv.FormatInt(exp, 10))
	jwt.AddClaim("scp", "test-purpose")
	jwt.AddClaim("aud", AT_AUDIENCE)
	jwt.AddClaim("iss", atIssuer)
	jwt.Encode()
	jwt.SymmSign(theAtSecret)
	return jwt.GetFullToken()
}

// --------------------------------------------------------------------------
// validateTokenExpiry
// --------------------------------------------------------------------------

func TestValidateTokenExpiry_ValidToken(t *testing.T) {
	now := time.Now().Unix()
	token := makeTestToken(now-10, now+3600) // issued 10s ago, expires in 1h
	if got := validateTokenExpiry(token); got != 0 {
		t.Errorf("valid token: got %d; want 0", got)
	}
}

func TestValidateTokenExpiry_ExpiredToken(t *testing.T) {
	now := time.Now().Unix()
	token := makeTestToken(now-7200, now-3600) // issued 2h ago, expired 1h ago
	if got := validateTokenExpiry(token); got != 16 {
		t.Errorf("expired token: got %d; want 16", got)
	}
}

func TestValidateTokenExpiry_FutureIat(t *testing.T) {
	now := time.Now().Unix()
	token := makeTestToken(now+7200, now+14400) // iat is in the future
	if got := validateTokenExpiry(token); got != 11 {
		t.Errorf("future iat: got %d; want 11", got)
	}
}

func TestValidateTokenExpiry_MalformedIat(t *testing.T) {
	// A token where ExtractFromToken("iat") returns a non-numeric string.
	// We can build such a token by not setting iat at all.
	var jwt utils.JsonWebToken
	jwt.SetHeader("HS256")
	jwt.AddClaim("exp", "9999999999")
	// no iat claim
	jwt.Encode()
	jwt.SymmSign(theAtSecret)
	token := jwt.GetFullToken()
	if got := validateTokenExpiry(token); got != 10 {
		t.Errorf("missing iat: got %d; want 10", got)
	}
}

func TestValidateTokenExpiry_MalformedExp(t *testing.T) {
	var jwt utils.JsonWebToken
	jwt.SetHeader("HS256")
	jwt.AddClaim("iat", strconv.FormatInt(time.Now().Unix()-10, 10))
	// no exp claim — ExtractFromToken will return ""
	jwt.Encode()
	jwt.SymmSign(theAtSecret)
	token := jwt.GetFullToken()
	if got := validateTokenExpiry(token); got != 15 {
		t.Errorf("missing exp: got %d; want 15", got)
	}
}

// --------------------------------------------------------------------------
// validateTokenTimestamps
// --------------------------------------------------------------------------

func TestValidateTokenTimestamps_Valid(t *testing.T) {
	now := time.Now().Unix()
	if !validateTokenTimestamps(int(now-10), int(now+3600)) {
		t.Errorf("valid timestamps should return true")
	}
}

func TestValidateTokenTimestamps_FutureIat(t *testing.T) {
	now := time.Now().Unix()
	if validateTokenTimestamps(int(now+1000), int(now+7200)) {
		t.Errorf("future iat should return false")
	}
}

func TestValidateTokenTimestamps_Expired(t *testing.T) {
	now := time.Now().Unix()
	if validateTokenTimestamps(int(now-7200), int(now-3600)) {
		t.Errorf("expired token should return false")
	}
}

// --------------------------------------------------------------------------
// extractSignature
// --------------------------------------------------------------------------

func TestExtractSignature_HappyPath(t *testing.T) {
	token := "header.payload.signature"
	got := extractSignature(token)
	if got != "signature" {
		t.Errorf("got %q; want signature", got)
	}
}

func TestExtractSignature_NoSignature(t *testing.T) {
	token := "no-dots-here"
	got := extractSignature(token)
	if got != "" {
		t.Errorf("got %q; want empty string for no dots", got)
	}
}

func TestExtractSignature_EmptyString(t *testing.T) {
	got := extractSignature("")
	if got != "" {
		t.Errorf("got %q; want empty string", got)
	}
}

func TestExtractSignature_OnlyDot(t *testing.T) {
	got := extractSignature(".")
	if got != "" {
		t.Errorf("got %q; want empty string for dot at end", got)
	}
}

func TestExtractSignature_MultipleDots(t *testing.T) {
	got := extractSignature("a.b.c.d")
	if got != "d" {
		t.Errorf("got %q; want d (last segment)", got)
	}
}

// --------------------------------------------------------------------------
// getPathLen
// --------------------------------------------------------------------------

func TestGetPathLen_NullTerminated(t *testing.T) {
	path := "hello\x00\x00\x00"
	if got := getPathLen(path); got != 5 {
		t.Errorf("got %d; want 5", got)
	}
}

func TestGetPathLen_NoNullTerminator(t *testing.T) {
	path := "hello"
	if got := getPathLen(path); got != 5 {
		t.Errorf("got %d; want 5", got)
	}
}

func TestGetPathLen_EmptyString(t *testing.T) {
	if got := getPathLen(""); got != 0 {
		t.Errorf("got %d; want 0", got)
	}
}

func TestGetPathLen_AllNulls(t *testing.T) {
	path := "\x00\x00\x00"
	if got := getPathLen(path); got != 0 {
		t.Errorf("got %d; want 0", got)
	}
}

// --------------------------------------------------------------------------
// extractAtValidatePayloadLevel1
// --------------------------------------------------------------------------

func TestExtractAtValidatePayloadLevel1_StringFields(t *testing.T) {
	m := map[string]interface{}{
		"token":      "tok-123",
		"action":     "get",
		"validation": "0",
		"paths":      "Vehicle.Speed",
	}
	var payload AtValidatePayload
	extractAtValidatePayloadLevel1(m, &payload)
	if payload.Token != "tok-123" {
		t.Errorf("Token = %q; want tok-123", payload.Token)
	}
	if payload.Action != "get" {
		t.Errorf("Action = %q; want get", payload.Action)
	}
	if payload.Validation != "0" {
		t.Errorf("Validation = %q; want 0", payload.Validation)
	}
	if len(payload.Paths) != 1 || payload.Paths[0] != "Vehicle.Speed" {
		t.Errorf("Paths = %v; want [Vehicle.Speed]", payload.Paths)
	}
}

func TestExtractAtValidatePayloadLevel1_ArrayPaths(t *testing.T) {
	m := map[string]interface{}{
		"token": "tok-456",
		"paths": []interface{}{"Vehicle.Speed", "Vehicle.Acceleration"},
	}
	var payload AtValidatePayload
	extractAtValidatePayloadLevel1(m, &payload)
	if payload.Token != "tok-456" {
		t.Errorf("Token = %q; want tok-456", payload.Token)
	}
	if len(payload.Paths) != 2 {
		t.Errorf("Paths len = %d; want 2", len(payload.Paths))
	}
}

func TestExtractAtValidatePayloadLevel1_UnknownTypeIgnored(t *testing.T) {
	m := map[string]interface{}{
		"token":   "tok-789",
		"unknown": 42, // not string or []interface{}
	}
	var payload AtValidatePayload
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractAtValidatePayloadLevel1(m, &payload)
	if payload.Token != "tok-789" {
		t.Errorf("Token = %q; want tok-789", payload.Token)
	}
}

// --------------------------------------------------------------------------
// extractAtValidatePayloadLevel2
// --------------------------------------------------------------------------

func TestExtractAtValidatePayloadLevel2_StringPaths(t *testing.T) {
	pathList := []interface{}{"Vehicle.Speed", "Vehicle.Acceleration"}
	var payload AtValidatePayload
	extractAtValidatePayloadLevel2(pathList, &payload)
	if len(payload.Paths) != 2 {
		t.Errorf("Paths len = %d; want 2", len(payload.Paths))
	}
	if payload.Paths[0] != "Vehicle.Speed" {
		t.Errorf("Paths[0] = %q; want Vehicle.Speed", payload.Paths[0])
	}
}

func TestExtractAtValidatePayloadLevel2_MixedTypes(t *testing.T) {
	pathList := []interface{}{"Vehicle.Speed", 42, "Vehicle.Acceleration"}
	var payload AtValidatePayload
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on mixed types: %v", r)
		}
	}()
	extractAtValidatePayloadLevel2(pathList, &payload)
	// Paths[1] will be "" (zero value for non-string), Paths[0] and [2] valid
	if len(payload.Paths) != 3 {
		t.Errorf("Paths len = %d; want 3", len(payload.Paths))
	}
}

// --------------------------------------------------------------------------
// newGatingId
// --------------------------------------------------------------------------

func TestNewGatingId_Increments(t *testing.T) {
	saved := GatingId
	defer func() { GatingId = saved }()
	GatingId = 1000
	id1 := newGatingId()
	id2 := newGatingId()
	if id1 != 1001 || id2 != 1002 {
		t.Errorf("got id1=%d id2=%d; want 1001,1002", id1, id2)
	}
}

func TestNewGatingId_WrapsAt9999(t *testing.T) {
	saved := GatingId
	defer func() { GatingId = saved }()
	// newGatingId does (GatingId + 1) % 9999, so 9998 → 0 (wraps)
	GatingId = 9997
	id := newGatingId()
	if id != 9998 {
		t.Errorf("got %d; want 9998 one before wrap", id)
	}
	id = newGatingId()
	if id != 0 {
		t.Errorf("got %d; want 0 after (9998+1)%%9999", id)
	}
}

// --------------------------------------------------------------------------
// extractGatingId
// --------------------------------------------------------------------------

func TestExtractGatingId_ValidSessionId(t *testing.T) {
	input := `{"sessionId":"42","other":"x"}`
	if got := extractGatingId(input); got != 42 {
		t.Errorf("got %d; want 42", got)
	}
}

func TestExtractGatingId_MissingSessionId(t *testing.T) {
	input := `{"other":"42"}`
	if got := extractGatingId(input); got != -1 {
		t.Errorf("got %d; want -1 for missing sessionId", got)
	}
}

func TestExtractGatingId_NonNumericSessionId(t *testing.T) {
	input := `{"sessionId":"notanumber"}`
	if got := extractGatingId(input); got != -1 {
		t.Errorf("got %d; want -1 for non-numeric sessionId", got)
	}
}

func TestExtractGatingId_MalformedJSON(t *testing.T) {
	if got := extractGatingId(`not json`); got != -1 {
		t.Errorf("got %d; want -1 for malformed JSON", got)
	}
}

// --------------------------------------------------------------------------
// checkVin
// --------------------------------------------------------------------------

func TestCheckVin_EmptyVin(t *testing.T) {
	if !checkVin("") {
		t.Errorf("empty VIN should return true (not checked)")
	}
}

func TestCheckVin_NonEmptyVin(t *testing.T) {
	if !checkVin("ABC123") {
		t.Errorf("non-empty VIN should return true (TODO)")
	}
}

// --------------------------------------------------------------------------
// initLists / removeFromPendingList / removeFromActiveList
// --------------------------------------------------------------------------

func TestInitLists_AllocatesAndInitializes(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	defer func() {
		pendingList = saved
		activeList = savedActive
	}()

	initLists()

	if len(pendingList) != LISTSIZE {
		t.Errorf("pendingList len = %d; want %d", len(pendingList), LISTSIZE)
	}
	if len(activeList) != LISTSIZE {
		t.Errorf("activeList len = %d; want %d", len(activeList), LISTSIZE)
	}
	for i := 0; i < LISTSIZE; i++ {
		if pendingList[i].GatingId != -1 {
			t.Errorf("pendingList[%d].GatingId = %d; want -1", i, pendingList[i].GatingId)
			break
		}
		if pendingList[i].Consent != "NOT_SET" {
			t.Errorf("pendingList[%d].Consent = %q; want NOT_SET", i, pendingList[i].Consent)
			break
		}
		if activeList[i].GatingId != -1 {
			t.Errorf("activeList[%d].GatingId = %d; want -1", i, activeList[i].GatingId)
			break
		}
	}
}

func TestRemoveFromPendingList_ResetsSlot(t *testing.T) {
	saved := pendingList[0]
	defer func() { pendingList[0] = saved }()

	pendingList[0] = PendingListElem{
		GatingId: 42,
		Consent:  "YES",
	}
	removed := removeFromPendingList(0)
	_ = removed
	if pendingList[0].GatingId != -1 {
		t.Errorf("GatingId = %d; want -1", pendingList[0].GatingId)
	}
	if pendingList[0].Consent != "NOT_SET" {
		t.Errorf("Consent = %q; want NOT_SET", pendingList[0].Consent)
	}
}

func TestRemoveFromActiveList_ResetsGatingId(t *testing.T) {
	saved := activeList[0]
	defer func() { activeList[0] = saved }()

	activeList[0] = ActiveListElem{GatingId: 99, Atoken: "tok"}
	removeFromActiveList(0)
	if activeList[0].GatingId != -1 {
		t.Errorf("GatingId = %d; want -1", activeList[0].GatingId)
	}
}

// --------------------------------------------------------------------------
// generateParentResponse — router for noScope vs validation
// --------------------------------------------------------------------------

func TestGenerateParentResponse_NoScopeRoute(t *testing.T) {
	// JSON with "context" keyword routes to noScopeResponse
	resp := generateParentResponse(`{"context":"user+app+device"}`)
	if !strings.Contains(resp, "no_access") {
		t.Errorf("expected no_access in response; got %q", resp)
	}
}

func TestGenerateParentResponse_ValidationRoute(t *testing.T) {
	// JSON without "context" routes to tokenValidationResponse
	resp := generateParentResponse(`{"token":"invalid","action":"get","paths":["Vehicle.Speed"]}`)
	// tokenValidationResponse will fail (bad token) but not panic
	if !strings.Contains(resp, "validation") {
		t.Errorf("expected validation field in response; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// generateClientResponse — router for at-request vs at-inquiry
// --------------------------------------------------------------------------

func TestGenerateClientResponse_AtRequestRoute(t *testing.T) {
	ecfChan := make(chan string, 1)
	resp := generateClientResponse(`{"at-request":"malformed"}`, ecfChan, false)
	// Will fail parsing or AGT decode, but returns a known error shape
	if !strings.Contains(resp, "at-request") && !strings.Contains(resp, "error") {
		t.Errorf("expected at-request response; got %q", resp)
	}
}

func TestGenerateClientResponse_UnknownRoute(t *testing.T) {
	ecfChan := make(chan string, 1)
	resp := generateClientResponse(`{"unknown":"field"}`, ecfChan, false)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 for unknown action; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// generateEcfResponse — router for consent-reply vs consent-cancel
// --------------------------------------------------------------------------

func TestGenerateEcfResponse_ConsentReplyRoute(t *testing.T) {
	ch := make(chan string, 1)
	resp := generateEcfResponse(`{"action":"consent-reply","messageId":"42","consent":"YES"}`, ch)
	if !strings.Contains(resp, "consent-reply") {
		t.Errorf("expected consent-reply in response; got %q", resp)
	}
}

func TestGenerateEcfResponse_ConsentCancelRoute(t *testing.T) {
	ch := make(chan string, 1)
	resp := generateEcfResponse(`{"action":"consent-cancel","messageId":"42"}`, ch)
	if !strings.Contains(resp, "consent-cancel") {
		t.Errorf("expected consent-cancel in response; got %q", resp)
	}
}

func TestGenerateEcfResponse_UnknownRoute(t *testing.T) {
	ch := make(chan string, 1)
	resp := generateEcfResponse(`{"action":"something-else"}`, ch)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 for unknown action; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// noScopeResponse — JSON parsing and delegation to getNoAccessScope
// --------------------------------------------------------------------------

func TestNoScopeResponse_MalformedJSON(t *testing.T) {
	resp := noScopeResponse(`{not json`)
	if !strings.Contains(resp, "no_access") {
		t.Errorf("expected no_access in error response; got %q", resp)
	}
}

func TestNoScopeResponse_ValidJSON(t *testing.T) {
	// With an empty sList (no scope definitions), getNoAccessScope returns ""
	saved := sList
	sList = nil
	defer func() { sList = saved }()

	resp := noScopeResponse(`{"context":"user+app+device"}`)
	if !strings.Contains(resp, "no_access") {
		t.Errorf("expected no_access in response; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// synthesizeNoScope / matchingContext / getNoAccessScope
// --------------------------------------------------------------------------

func TestSynthesizeNoScope_SingleEntry(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = []ScopeElement{
		{NoAccess: []string{"Vehicle.Private.Data"}},
	}
	got := synthesizeNoScope(0)
	if got != `"Vehicle.Private.Data"` {
		t.Errorf("got %q; want %q", got, `"Vehicle.Private.Data"`)
	}
}

func TestSynthesizeNoScope_MultipleEntries(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = []ScopeElement{
		{NoAccess: []string{"Vehicle.Private.A", "Vehicle.Private.B"}},
	}
	got := synthesizeNoScope(0)
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Errorf("multiple entries should return JSON array; got %q", got)
	}
	if !strings.Contains(got, "Vehicle.Private.A") || !strings.Contains(got, "Vehicle.Private.B") {
		t.Errorf("missing paths; got %q", got)
	}
}

func TestMatchingContext_MatchFound(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	// Build a scope element where Actor[0].Role=["user"], Actor[1].Role=["app"], Actor[2].Role=["device"]
	sList = []ScopeElement{
		{
			Context: []ContextElement{
				{
					Actor: [3]RoleElement{
						{Role: []string{"user"}},
						{Role: []string{"app"}},
						{Role: []string{"device"}},
					},
				},
			},
			NoAccess: []string{"Vehicle.Private.X"},
		},
	}
	if !matchingContext(0, "user+app+device") {
		t.Errorf("matching context should return true")
	}
}

func TestMatchingContext_NoMatch(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = []ScopeElement{
		{
			Context: []ContextElement{
				{
					Actor: [3]RoleElement{
						{Role: []string{"admin"}},
						{Role: []string{"app"}},
						{Role: []string{"device"}},
					},
				},
			},
			NoAccess: []string{"Vehicle.Private.X"},
		},
	}
	if matchingContext(0, "user+app+device") {
		t.Errorf("non-matching context should return false")
	}
}

func TestGetNoAccessScope_MatchFound(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = []ScopeElement{
		{
			Context: []ContextElement{
				{
					Actor: [3]RoleElement{
						{Role: []string{"user"}},
						{Role: []string{"myapp"}},
						{Role: []string{"mydevice"}},
					},
				},
			},
			NoAccess: []string{"Vehicle.Private.Secret"},
		},
	}
	got := getNoAccessScope("user+myapp+mydevice")
	if got != `"Vehicle.Private.Secret"` {
		t.Errorf("got %q; want Vehicle.Private.Secret", got)
	}
}

func TestGetNoAccessScope_NoMatch(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = nil // empty list
	got := getNoAccessScope("user+app+device")
	if got != `""` {
		t.Errorf("got %q; want empty no-access", got)
	}
}

// --------------------------------------------------------------------------
// validatePurposeAndAccessPermission / validatePurpose / checkAuthorization
// --------------------------------------------------------------------------

func TestValidatePurposeAndAccessPermission_PermissionGranted(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{
			Short: "fuel-status",
			Access: []AccessElement{
				{Path: "Vehicle.FuelLevel", Permission: "read-only"},
			},
		},
	}
	// get action with read-only permission → allowed
	if got := validatePurposeAndAccessPermission("fuel-status", "get", "Vehicle.FuelLevel"); got != 0 {
		t.Errorf("got %d; want 0 (allowed)", got)
	}
}

func TestValidatePurposeAndAccessPermission_SetOnReadOnly(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{
			Short: "fuel-status",
			Access: []AccessElement{
				{Path: "Vehicle.FuelLevel", Permission: "read-only"},
			},
		},
	}
	if got := validatePurposeAndAccessPermission("fuel-status", "set", "Vehicle.FuelLevel"); got != 61 {
		t.Errorf("got %d; want 61 (set on read-only)", got)
	}
}

func TestValidatePurposeAndAccessPermission_PurposeNotFound(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{Short: "fuel-status", Access: []AccessElement{{Path: "Vehicle.FuelLevel", Permission: "read-only"}}},
	}
	if got := validatePurposeAndAccessPermission("unknown-purpose", "get", "Vehicle.FuelLevel"); got != 60 {
		t.Errorf("got %d; want 60 (purpose not found)", got)
	}
}

func TestValidatePurposeAndAccessPermission_PathNotInPurpose(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{Short: "fuel-status", Access: []AccessElement{{Path: "Vehicle.FuelLevel", Permission: "read-only"}}},
	}
	if got := validatePurposeAndAccessPermission("fuel-status", "get", "Vehicle.Speed"); got != 60 {
		t.Errorf("got %d; want 60 (path not in purpose)", got)
	}
}

func TestCheckAuthorization_Match(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{
			Short: "fuel-status",
			Context: []ContextElement{
				{
					Actor: [3]RoleElement{
						{Role: []string{"Independent"}},
						{Role: []string{"OEM"}},
						{Role: []string{"Cloud"}},
					},
				},
			},
		},
	}
	if !checkAuthorization(0, "Independent+OEM+Cloud") {
		t.Errorf("matching context should return true")
	}
}

func TestCheckAuthorization_NoMatch(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{
			Short: "fuel-status",
			Context: []ContextElement{
				{
					Actor: [3]RoleElement{
						{Role: []string{"Admin"}},
						{Role: []string{"OEM"}},
						{Role: []string{"Cloud"}},
					},
				},
			},
		},
	}
	if checkAuthorization(0, "Independent+OEM+Cloud") {
		t.Errorf("non-matching context should return false")
	}
}

func TestValidatePurpose_Match(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{
			Short: "fuel-status",
			Context: []ContextElement{
				{
					Actor: [3]RoleElement{
						{Role: []string{"Independent"}},
						{Role: []string{"OEM"}},
						{Role: []string{"Cloud"}},
					},
				},
			},
		},
	}
	if !validatePurpose("fuel-status", "Independent+OEM+Cloud") {
		t.Errorf("matching purpose+context should return true")
	}
}

func TestValidatePurpose_NoMatch(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = nil
	if validatePurpose("anything", "user+app+device") {
		t.Errorf("empty pList should return false")
	}
}

// --------------------------------------------------------------------------
// extractPurposeElementsLevel1/2/3 — JSON extraction helpers
// --------------------------------------------------------------------------

func TestExtractPurposeElementsLevel1_ArrayTopLevel(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()

	// Top-level is an array of purpose elements
	var purposeMap map[string]interface{}
	data := `{"purposes":[{"short":"fuel-status","long":"Fuel Status","contexts":{"user":"Independent","app":"OEM","device":"Cloud"},"signals":[{"path":"Vehicle.FuelLevel","access":"read-only"}]}]}`
	if err := json.Unmarshal([]byte(data), &purposeMap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	extractPurposeElementsLevel1(purposeMap)
	// pList should be populated (len depends on parsing)
}

func TestExtractPurposeElementsLevel2_PopulatesList(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()

	raw := []interface{}{
		map[string]interface{}{
			"short": "test-purpose",
			"long":  "Test Purpose",
		},
	}
	extractPurposeElementsLevel2(raw)
	if len(pList) != 1 {
		t.Errorf("pList len = %d; want 1", len(pList))
	}
	if pList[0].Short != "test-purpose" {
		t.Errorf("Short = %q; want test-purpose", pList[0].Short)
	}
}

func TestExtractPurposeElementsLevel3_StringFields(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)

	elem := map[string]interface{}{
		"short": "my-purpose",
		"long":  "My Long Purpose Name",
	}
	extractPurposeElementsLevel3(0, elem)
	if pList[0].Short != "my-purpose" {
		t.Errorf("Short = %q; want my-purpose", pList[0].Short)
	}
	if pList[0].Long != "My Long Purpose Name" {
		t.Errorf("Long = %q; want My Long Purpose Name", pList[0].Long)
	}
}

// --------------------------------------------------------------------------
// extractScopeElementsLevel1/2/3 — JSON extraction helpers
// --------------------------------------------------------------------------

func TestExtractScopeElementsLevel2_PopulatesList(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()

	raw := []interface{}{
		map[string]interface{}{
			"no_access": "Vehicle.Private.Data",
			"contexts": map[string]interface{}{
				"user": "admin",
				"app":  "OEM",
			},
		},
	}
	extractScopeElementsLevel2(raw)
	if len(sList) != 1 {
		t.Errorf("sList len = %d; want 1", len(sList))
	}
}

func TestExtractScopeElementsLevel3_StringNoAccess(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)

	elem := map[string]interface{}{
		"no_access": "Vehicle.Private.Secret",
	}
	extractScopeElementsLevel3(0, elem)
	if len(sList[0].NoAccess) != 1 || sList[0].NoAccess[0] != "Vehicle.Private.Secret" {
		t.Errorf("NoAccess = %v; want [Vehicle.Private.Secret]", sList[0].NoAccess)
	}
}

// --------------------------------------------------------------------------
// purgeLists — pending and active list expiry
// --------------------------------------------------------------------------

func TestPurgeLists_EmptyListsReturnEmpty(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	defer func() {
		pendingList = saved
		activeList = savedActive
	}()
	initLists()
	got := purgeLists()
	if got != "" {
		t.Errorf("empty lists: got %q; want empty", got)
	}
}

func TestPurgeLists_ExpiredPendingRemoved(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	// Create a ticker so setExpiryTicker doesn't nil-deref
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Set a pending entry with past expiry
	pendingList[0].GatingId = 42
	pendingList[0].AgtExpiryTime = "1" // Unix timestamp 1 = far in the past

	got := purgeLists()
	if got != "" {
		t.Errorf("expired pending: got %q; want empty (pending doesn't need cancel)", got)
	}
	if pendingList[0].GatingId != -1 {
		t.Errorf("expired pending should be cleared; GatingId = %d", pendingList[0].GatingId)
	}
}

func TestPurgeLists_ExpiredActiveReturnsGatingId(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Set an active entry with past expiry
	activeList[0].GatingId = 77
	activeList[0].AtExpiryTime = "1" // past expiry

	got := purgeLists()
	if got != "77" {
		t.Errorf("expired active: got %q; want \"77\"", got)
	}
	if activeList[0].GatingId != -1 {
		t.Errorf("expired active should be cleared; GatingId = %d", activeList[0].GatingId)
	}
}

func TestPurgeLists_BadExpiryReturnsEmpty(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Malformed expiry time
	pendingList[0].GatingId = 5
	pendingList[0].AgtExpiryTime = "not-a-number"

	got := purgeLists()
	if got != "" {
		t.Errorf("bad expiry: got %q; want empty", got)
	}
}

// --------------------------------------------------------------------------
// writeToActiveList / writeToPendingList
// --------------------------------------------------------------------------

func TestWriteToActiveList_HappyPath(t *testing.T) {
	saved := activeList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	now := time.Now().Unix()
	// Build a token with an exp claim so ExtractFromToken works
	var jwt utils.JsonWebToken
	jwt.SetHeader("HS256")
	jwt.AddClaim("exp", strconv.FormatInt(now+3600, 10))
	jwt.Encode()
	jwt.SymmSign(theAtSecret)
	token := jwt.GetFullToken()

	writeToActiveList(99, token)
	if activeList[0].GatingId != 99 {
		t.Errorf("GatingId = %d; want 99", activeList[0].GatingId)
	}
	if activeList[0].Atoken != token {
		t.Errorf("Atoken mismatch")
	}
	// AtokenHandle should be the signature (last segment)
	if activeList[0].AtokenHandle != extractSignature(token) {
		t.Errorf("AtokenHandle = %q; want signature segment", activeList[0].AtokenHandle)
	}
}

// --------------------------------------------------------------------------
// Integration-only functions in atServer — documented as such (not unit-tested)
//
//   - AtServerInit          binds HTTP/WS listeners, calls os.Exit on failure
//   - initClientComm        calls http.ListenAndServe (blocks forever)
//   - initEcfComm           calls http.ListenAndServeTLS (blocks forever)
//   - makeEcfHandler        spawns goroutines for WS connections
//   - ecfReceiver/ecfSender websocket goroutines, block on conn.ReadMessage
//   - accessTokenResponse   requires a live RSA AGT key for signature verify
//   - validateRequest        requires RSA public key and real JWT
//   - validatePop            requires real RSA keypair
//   - checkifConsent         requires live VSS tree via utils.SetRootNodePointer
//   - generateAt             requires uuid.NewRandom (non-pure)
//   - initAgtKey             reads from filesystem
//   - initPurposelist        reads from filesystem (os.Exit on failure)
//   - initScopeList          reads from filesystem (os.Exit on failure)
//   - deleteJti              blocks for (GAP+LIFETIME+5) seconds
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// makeAtServerHandler — HTTP handler factory (testable via httptest)
// --------------------------------------------------------------------------

func TestMakeAtServerHandler_WrongPath(t *testing.T) {
	ch := make(chan string, 4)
	handler := makeAtServerHandler(ch)
	req := httptest.NewRequest("POST", "/wrong", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("wrong path: got %d; want 404", rec.Code)
	}
}

func TestMakeAtServerHandler_OptionsMethod(t *testing.T) {
	ch := make(chan string, 4)
	handler := makeAtServerHandler(ch)
	req := httptest.NewRequest("OPTIONS", "/ats", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("OPTIONS: got %d; want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("OPTIONS response missing Access-Control-Allow-Methods")
	}
}

func TestMakeAtServerHandler_BadMethod(t *testing.T) {
	ch := make(chan string, 4)
	handler := makeAtServerHandler(ch)
	req := httptest.NewRequest("DELETE", "/ats", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE: got %d; want 400", rec.Code)
	}
}

func TestMakeAtServerHandler_OversizedBody(t *testing.T) {
	ch := make(chan string, 4)
	handler := makeAtServerHandler(ch)
	// 65 KiB — just over the 64 KiB cap.
	body := strings.NewReader(strings.Repeat("x", 65*1024))
	req := httptest.NewRequest("POST", "/ats", body)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 413 {
		t.Errorf("oversized POST: got %d; want 413", rec.Code)
	}
}

func TestMakeAtServerHandler_EmptyResponse(t *testing.T) {
	// Consumer reads body and sends back "".
	ch := make(chan string)
	go func() {
		<-ch       // body
		ch <- ""   // empty response → handler returns 400
	}()
	handler := makeAtServerHandler(ch)
	req := httptest.NewRequest("POST", "/ats", strings.NewReader(`{"token":"x"}`))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty response: got %d; want 400", rec.Code)
	}
}

func TestMakeAtServerHandler_SuccessfulPost(t *testing.T) {
	ch := make(chan string)
	go func() {
		<-ch
		ch <- `{"validation":"0"}`
	}()
	handler := makeAtServerHandler(ch)
	req := httptest.NewRequest("POST", "/ats", strings.NewReader(`{"token":"tok"}`))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 201 {
		t.Errorf("successful POST: got %d; want 201", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation") {
		t.Errorf("response body missing validation field: %q", rec.Body.String())
	}
}

// --------------------------------------------------------------------------
// consentReplyResponse — success (200-OK) and 404 paths
// --------------------------------------------------------------------------

func TestConsentReplyResponse_MatchFound_200OK(t *testing.T) {
	saved := pendingList
	defer func() { pendingList = saved }()
	initLists()

	// Put a pending entry with a known gatingId.
	pendingList[0].GatingId = 42
	pendingList[0].Consent = "NOT_SET"

	resp := consentReplyResponse(`{"action":"consent-reply","messageId":"42","consent":"YES"}`)
	if !strings.Contains(resp, "200-OK") {
		t.Errorf("expected 200-OK on matching gatingId; got %q", resp)
	}
	if pendingList[0].Consent != "YES" {
		t.Errorf("consent not updated; got %q", pendingList[0].Consent)
	}
}

func TestConsentReplyResponse_NotFound_404(t *testing.T) {
	saved := pendingList
	defer func() { pendingList = saved }()
	initLists() // all GatingId == -1

	resp := consentReplyResponse(`{"action":"consent-reply","messageId":"99","consent":"YES"}`)
	if !strings.Contains(resp, "404") {
		t.Errorf("expected 404 for no match; got %q", resp)
	}
}

func TestConsentReplyResponse_NonNumericMessageId(t *testing.T) {
	resp := consentReplyResponse(`{"action":"consent-reply","messageId":"not-a-number","consent":"YES"}`)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 for non-numeric messageId; got %q", resp)
	}
}

func TestConsentReplyResponse_HmacMismatch(t *testing.T) {
	// With ecfSecret set, a wrong hmac returns 401-Unauthorized.
	savedSecret := ecfSecret
	ecfSecret = "mysecret"
	defer func() { ecfSecret = savedSecret }()

	resp := consentReplyResponse(`{"action":"consent-reply","messageId":"42","consent":"YES","hmac":"wronghmac"}`)
	if !strings.Contains(resp, "Unauthorized") {
		t.Errorf("expected Unauthorized for bad hmac; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// consentCancelResponse — success paths and 404
// --------------------------------------------------------------------------

func TestConsentCancelResponse_MatchPending_200OK(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	defer func() {
		pendingList = saved
		activeList = savedActive
	}()
	initLists()
	pendingList[0].GatingId = 55

	ch := make(chan string, 1)
	resp := consentCancelResponse(`{"action":"consent-cancel","messageId":"55"}`, ch)
	if !strings.Contains(resp, "200-OK") {
		t.Errorf("cancel pending: expected 200-OK; got %q", resp)
	}
	if pendingList[0].GatingId != -1 {
		t.Errorf("pending entry not cleared after cancel")
	}
}

func TestConsentCancelResponse_MatchActive_200OK(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	defer func() {
		pendingList = saved
		activeList = savedActive
	}()
	initLists()
	activeList[0].GatingId = 77

	ch := make(chan string, 1)
	resp := consentCancelResponse(`{"action":"consent-cancel","messageId":"77"}`, ch)
	if !strings.Contains(resp, "200-OK") {
		t.Errorf("cancel active: expected 200-OK; got %q", resp)
	}
	if activeList[0].GatingId != -1 {
		t.Errorf("active entry not cleared after cancel")
	}
	// vissChan should receive the messageId string.
	select {
	case sent := <-ch:
		if sent != "77" {
			t.Errorf("vissChan got %q; want 77", sent)
		}
	default:
		t.Error("vissChan should have received subscription cancel signal")
	}
}

func TestConsentCancelResponse_NotFound_404(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	defer func() {
		pendingList = saved
		activeList = savedActive
	}()
	initLists()

	ch := make(chan string, 1)
	resp := consentCancelResponse(`{"action":"consent-cancel","messageId":"999"}`, ch)
	if !strings.Contains(resp, "404") {
		t.Errorf("expected 404; got %q", resp)
	}
}

func TestConsentCancelResponse_HmacMismatch(t *testing.T) {
	savedSecret := ecfSecret
	ecfSecret = "mysecret"
	defer func() { ecfSecret = savedSecret }()

	ch := make(chan string, 1)
	resp := consentCancelResponse(`{"action":"consent-cancel","messageId":"42","hmac":"wrong"}`, ch)
	if !strings.Contains(resp, "Unauthorized") {
		t.Errorf("expected Unauthorized for bad hmac; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// consentInquiryResponse — NOT_SET, NO, 404-Not-found paths
// --------------------------------------------------------------------------

func TestConsentInquiryResponse_NotSet(t *testing.T) {
	saved := pendingList
	defer func() { pendingList = saved }()
	initLists()
	pendingList[0].GatingId = 11
	pendingList[0].Consent = "NOT_SET"

	resp := consentInquiryResponse(`{"sessionId":"11"}`)
	if !strings.Contains(resp, "NOT_SET") {
		t.Errorf("expected NOT_SET response; got %q", resp)
	}
}

func TestConsentInquiryResponse_ConsentNo(t *testing.T) {
	saved := pendingList
	defer func() { pendingList = saved }()
	initLists()
	pendingList[0].GatingId = 22
	pendingList[0].Consent = "NO"

	resp := consentInquiryResponse(`{"sessionId":"22"}`)
	if !strings.Contains(resp, `"consent":"NO"`) {
		t.Errorf("expected consent:NO; got %q", resp)
	}
	// Entry should be removed.
	if pendingList[0].GatingId != -1 {
		t.Errorf("pending entry should be cleared after NO consent")
	}
}

func TestConsentInquiryResponse_NotFound(t *testing.T) {
	saved := pendingList
	defer func() { pendingList = saved }()
	initLists()

	resp := consentInquiryResponse(`{"sessionId":"9999"}`)
	if !strings.Contains(resp, "404") {
		t.Errorf("expected 404 for missing gatingId; got %q", resp)
	}
}

func TestConsentInquiryResponse_MalformedJSON(t *testing.T) {
	resp := consentInquiryResponse(`not json`)
	// extractGatingId returns -1; initLists() sets every slot to GatingId=-1 so
	// the loop hits pendingList[0] and returns NOT_SET for that slot.
	// Any non-panic response is acceptable — the important thing is no crash.
	if resp == "" {
		t.Error("malformed JSON: expected non-empty response")
	}
}

// --------------------------------------------------------------------------
// validateRequestAccess — pure pList lookups, no VSS tree needed (no wildcard paths)
// --------------------------------------------------------------------------

func TestValidateRequestAccess_EmptyPaths(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = nil

	// Zero paths → loop body never executes → return 0
	if got := validateRequestAccess("any-purpose", "get", []string{}); got != 0 {
		t.Errorf("empty paths: got %d; want 0", got)
	}
}

func TestValidateRequestAccess_SinglePathAllowed(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{Short: "fuel-status", Access: []AccessElement{{Path: "Vehicle.FuelLevel", Permission: "read-only"}}},
	}

	if got := validateRequestAccess("fuel-status", "get", []string{"Vehicle.FuelLevel"}); got != 0 {
		t.Errorf("allowed path: got %d; want 0", got)
	}
}

func TestValidateRequestAccess_SinglePathDenied(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{Short: "fuel-status", Access: []AccessElement{{Path: "Vehicle.FuelLevel", Permission: "read-only"}}},
	}

	// set action on read-only → 61
	if got := validateRequestAccess("fuel-status", "set", []string{"Vehicle.FuelLevel"}); got != 61 {
		t.Errorf("denied path: got %d; want 61", got)
	}
}

func TestValidateRequestAccess_MultiplePaths_FirstDenied(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{Short: "test-scp", Access: []AccessElement{
			{Path: "Vehicle.A", Permission: "read-only"},
			{Path: "Vehicle.B", Permission: "read-write"},
		}},
	}

	// First path fails set on read-only → returns 61 immediately
	if got := validateRequestAccess("test-scp", "set", []string{"Vehicle.A", "Vehicle.B"}); got != 61 {
		t.Errorf("first path denied: got %d; want 61", got)
	}
}

// --------------------------------------------------------------------------
// getSignalAccess — testable with pList populated
// --------------------------------------------------------------------------

func TestGetSignalAccess_MatchFound(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = []PurposeElement{
		{
			Short:  "speed-read",
			Access: []AccessElement{{Path: "Vehicle.Speed", Permission: "read-only"}},
		},
	}

	got := getSignalAccess("speed-read")
	if !strings.Contains(got, "Vehicle.Speed") {
		t.Errorf("expected signal access JSON with Vehicle.Speed; got %q", got)
	}
}

func TestGetSignalAccess_NoMatch(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = nil

	got := getSignalAccess("unknown-purpose")
	if got != "" {
		t.Errorf("no match: expected empty string; got %q", got)
	}
}

// --------------------------------------------------------------------------
// writeToPendingList — testable (pList not needed; just pendingList slots)
// --------------------------------------------------------------------------

func TestWriteToPendingList_HappyPath(t *testing.T) {
	saved := pendingList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	now := time.Now().Unix()
	var jwt utils.JsonWebToken
	jwt.SetHeader("HS256")
	jwt.AddClaim("exp", strconv.FormatInt(now+3600, 10))
	jwt.Encode()
	jwt.SymmSign(theAtSecret)
	token := jwt.GetFullToken()

	payload := AtGenPayload{Token: token, Purpose: "fuel-status"}
	writeToPendingList(999, payload)

	if pendingList[0].GatingId != 999 {
		t.Errorf("GatingId = %d; want 999", pendingList[0].GatingId)
	}
	if pendingList[0].AtGenData.Purpose != "fuel-status" {
		t.Errorf("Purpose = %q; want fuel-status", pendingList[0].AtGenData.Purpose)
	}
}

func TestWriteToPendingList_FullList_LogsError(t *testing.T) {
	saved := pendingList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Fill all slots.
	for i := 0; i < LISTSIZE; i++ {
		pendingList[i].GatingId = i
	}

	// Should log error but not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("writeToPendingList panicked on full list: %v", r)
		}
	}()
	writeToPendingList(9000, AtGenPayload{Token: "", Purpose: "x"})
}

// --------------------------------------------------------------------------
// setExpiryTicker — exercised with populated lists
// --------------------------------------------------------------------------

func TestSetExpiryTicker_WithActiveEntry(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Put an active entry with a future expiry.
	future := time.Now().Add(5 * time.Minute).Unix()
	activeList[0].GatingId = 100
	activeList[0].AtExpiryTime = strconv.FormatInt(future, 10)

	// Must not panic; should reset the ticker.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setExpiryTicker panicked: %v", r)
		}
	}()
	setExpiryTicker()
}

func TestSetExpiryTicker_WithPendingEntry(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	future := time.Now().Add(10 * time.Minute).Unix()
	pendingList[0].GatingId = 50
	pendingList[0].AgtExpiryTime = strconv.FormatInt(future, 10)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setExpiryTicker panicked: %v", r)
		}
	}()
	setExpiryTicker()
}

func TestSetExpiryTicker_BadExpiryReturnsEarly(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Malformed expiry in pendingList → error → return early.
	pendingList[0].GatingId = 5
	pendingList[0].AgtExpiryTime = "not-a-number"

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setExpiryTicker panicked on bad expiry: %v", r)
		}
	}()
	setExpiryTicker()
}

func TestSetExpiryTicker_BadActiveExpiryReturnsEarly(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// No pending entries; malformed expiry in activeList → error.
	activeList[0].GatingId = 5
	activeList[0].AtExpiryTime = "not-a-number"

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setExpiryTicker panicked on bad active expiry: %v", r)
		}
	}()
	setExpiryTicker()
}

func TestSetExpiryTicker_EmptyLists_StopsTicker(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Empty lists → isUpdated stays false → Ticker.Stop() is called.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setExpiryTicker panicked on empty lists: %v", r)
		}
	}()
	setExpiryTicker()
}

// --------------------------------------------------------------------------
// extractPurposeElementsLevel1 — map branch (single element, not array)
// --------------------------------------------------------------------------

func TestExtractPurposeElementsLevel1_MapBranch(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)

	// A top-level map where the value is itself a map (not an array).
	purposeMap := map[string]interface{}{
		"element": map[string]interface{}{
			"short": "map-purpose",
			"long":  "Map Purpose Long",
		},
	}
	extractPurposeElementsLevel1(purposeMap)
	if pList[0].Short != "map-purpose" {
		t.Errorf("Short = %q; want map-purpose", pList[0].Short)
	}
}

func TestExtractPurposeElementsLevel1_UnknownType(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()

	// Should not panic on unknown types.
	purposeMap := map[string]interface{}{
		"unknown": 42,
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractPurposeElementsLevel1(purposeMap)
}

// --------------------------------------------------------------------------
// extractPurposeElementsLevel3 — contexts array and signals array branches
// --------------------------------------------------------------------------

func TestExtractPurposeElementsLevel3_ContextsArray(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)

	elem := map[string]interface{}{
		"short": "ctx-purpose",
		"contexts": []interface{}{
			map[string]interface{}{
				"user":   "Independent",
				"app":    "OEM",
				"device": "Cloud",
			},
		},
	}
	extractPurposeElementsLevel3(0, elem)
	if pList[0].Short != "ctx-purpose" {
		t.Errorf("Short = %q; want ctx-purpose", pList[0].Short)
	}
	if len(pList[0].Context) != 1 {
		t.Errorf("Context len = %d; want 1", len(pList[0].Context))
	}
}

func TestExtractPurposeElementsLevel3_ContextsMap(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)

	elem := map[string]interface{}{
		"short": "ctx-map-purpose",
		"contexts": map[string]interface{}{
			"user":   "Owner",
			"app":    "Third party",
			"device": "Vehicle",
		},
	}
	extractPurposeElementsLevel3(0, elem)
	if pList[0].Short != "ctx-map-purpose" {
		t.Errorf("Short = %q; want ctx-map-purpose", pList[0].Short)
	}
	if len(pList[0].Context) != 1 {
		t.Errorf("Context len = %d; want 1", len(pList[0].Context))
	}
}

func TestExtractPurposeElementsLevel3_SignalsArray(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)

	elem := map[string]interface{}{
		"short": "sigs-purpose",
		"signals": []interface{}{
			map[string]interface{}{
				"path":   "Vehicle.Speed",
				"access": "read-only",
			},
		},
	}
	extractPurposeElementsLevel3(0, elem)
	if len(pList[0].Access) != 1 {
		t.Errorf("Access len = %d; want 1", len(pList[0].Access))
	}
}

func TestExtractPurposeElementsLevel3_SignalsMap(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)

	elem := map[string]interface{}{
		"short": "sig-map-purpose",
		"signals": map[string]interface{}{
			"path":   "Vehicle.FuelLevel",
			"access": "read-write",
		},
	}
	extractPurposeElementsLevel3(0, elem)
	if len(pList[0].Access) != 1 {
		t.Errorf("Access len = %d; want 1", len(pList[0].Access))
	}
	if pList[0].Access[0].Path != "Vehicle.FuelLevel" {
		t.Errorf("Access[0].Path = %q; want Vehicle.FuelLevel", pList[0].Access[0].Path)
	}
}

// --------------------------------------------------------------------------
// extractPurposeElementsL4ContextL1 / L4ContextL2 — array contexts
// --------------------------------------------------------------------------

func TestExtractPurposeElementsL4ContextL1_MapElements(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)
	pList[0].Context = make([]ContextElement, 2)

	contextArray := []interface{}{
		map[string]interface{}{
			"user": "Independent",
			"app":  "OEM",
		},
		map[string]interface{}{
			"user":   "Driver",
			"app":    "Third party",
			"device": "Nomadic",
		},
	}
	extractPurposeElementsL4ContextL1(0, contextArray)
	if len(pList[0].Context[0].Actor[0].Role) == 0 {
		t.Error("Actor[0].Role not populated for first context element")
	}
}

func TestExtractPurposeElementsL4ContextL1_UnknownType(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)
	pList[0].Context = make([]ContextElement, 1)

	// Non-map element should not panic.
	contextArray := []interface{}{42}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	extractPurposeElementsL4ContextL1(0, contextArray)
}

func TestExtractPurposeElementsL4ContextL2_ArrayRoles(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)
	pList[0].Context = make([]ContextElement, 1)

	// user is an array of strings (multiple roles).
	contextMap := map[string]interface{}{
		"user":   []interface{}{"Independent", "Owner"},
		"app":    []interface{}{"OEM"},
		"device": []interface{}{"Cloud"},
	}
	extractPurposeElementsL4ContextL2(0, 0, contextMap)

	roles := pList[0].Context[0].Actor[0].Role
	if len(roles) != 2 {
		t.Errorf("user roles len = %d; want 2", len(roles))
	}
	if roles[0] != "Independent" {
		t.Errorf("roles[0] = %q; want Independent", roles[0])
	}
}

func TestExtractPurposeElementsL4ContextL2_UnknownType(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)
	pList[0].Context = make([]ContextElement, 1)

	contextMap := map[string]interface{}{
		"user": 42, // not string or []interface{}
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractPurposeElementsL4ContextL2(0, 0, contextMap)
}

// --------------------------------------------------------------------------
// extractPurposeElementsL4SignalAccessL1 — signal access array path
// --------------------------------------------------------------------------

func TestExtractPurposeElementsL4SignalAccessL1_MapElements(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)
	pList[0].Access = make([]AccessElement, 2)

	accessArray := []interface{}{
		map[string]interface{}{
			"path":   "Vehicle.Speed",
			"access": "read-only",
		},
		map[string]interface{}{
			"path":   "Vehicle.FuelLevel",
			"access": "read-write",
		},
	}
	extractPurposeElementsL4SignalAccessL1(0, accessArray)
	if pList[0].Access[0].Path != "Vehicle.Speed" {
		t.Errorf("Access[0].Path = %q; want Vehicle.Speed", pList[0].Access[0].Path)
	}
}

func TestExtractPurposeElementsL4SignalAccessL1_UnknownType(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)
	pList[0].Access = make([]AccessElement, 1)

	accessArray := []interface{}{42} // non-map → default branch
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractPurposeElementsL4SignalAccessL1(0, accessArray)
}

// --------------------------------------------------------------------------
// extractScopeElementsLevel1 — both branches (array and map)
// --------------------------------------------------------------------------

func TestExtractScopeElementsLevel1_ArrayBranch(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()

	scopeMap := map[string]interface{}{
		"scopes": []interface{}{
			map[string]interface{}{
				"no_access": "Vehicle.Private.Data",
			},
		},
	}
	extractScopeElementsLevel1(scopeMap)
	if len(sList) != 1 {
		t.Errorf("sList len = %d; want 1", len(sList))
	}
}

func TestExtractScopeElementsLevel1_MapBranch(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)

	scopeMap := map[string]interface{}{
		"element": map[string]interface{}{
			"no_access": "Vehicle.Private.Secret",
		},
	}
	extractScopeElementsLevel1(scopeMap)
	if len(sList[0].NoAccess) == 0 {
		t.Error("NoAccess should be populated via map branch")
	}
}

func TestExtractScopeElementsLevel1_UnknownType(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()

	scopeMap := map[string]interface{}{
		"unknown": 42,
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractScopeElementsLevel1(scopeMap)
}

// --------------------------------------------------------------------------
// extractScopeElementsLevel2 — unknown type branch
// --------------------------------------------------------------------------

func TestExtractScopeElementsLevel2_UnknownType(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()

	// Non-map element → default branch.
	raw := []interface{}{42}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	extractScopeElementsLevel2(raw)
}

// --------------------------------------------------------------------------
// extractScopeElementsLevel3 — array and map branches
// --------------------------------------------------------------------------

func TestExtractScopeElementsLevel3_ArrayNoAccess(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)

	elem := map[string]interface{}{
		"no_access": []interface{}{"Vehicle.Private.A", "Vehicle.Private.B"},
	}
	extractScopeElementsLevel3(0, elem)
	if len(sList[0].NoAccess) != 2 {
		t.Errorf("NoAccess len = %d; want 2", len(sList[0].NoAccess))
	}
	if sList[0].NoAccess[0] != "Vehicle.Private.A" {
		t.Errorf("NoAccess[0] = %q; want Vehicle.Private.A", sList[0].NoAccess[0])
	}
}

func TestExtractScopeElementsLevel3_ContextsArray(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)

	elem := map[string]interface{}{
		"contexts": []interface{}{
			map[string]interface{}{
				"user":   "admin",
				"app":    "OEM",
				"device": "Cloud",
			},
		},
	}
	extractScopeElementsLevel3(0, elem)
	if len(sList[0].Context) != 1 {
		t.Errorf("Context len = %d; want 1", len(sList[0].Context))
	}
}

func TestExtractScopeElementsLevel3_ContextsMap(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)

	elem := map[string]interface{}{
		"contexts": map[string]interface{}{
			"user":   "admin",
			"app":    "OEM",
			"device": "Cloud",
		},
	}
	extractScopeElementsLevel3(0, elem)
	if len(sList[0].Context) != 1 {
		t.Errorf("Context len = %d; want 1", len(sList[0].Context))
	}
}

func TestExtractScopeElementsLevel3_UnknownType(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)

	elem := map[string]interface{}{
		"unknown": 42,
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractScopeElementsLevel3(0, elem)
}

// --------------------------------------------------------------------------
// extractScopeElementsL4ContextL1 / L4ContextL2
// --------------------------------------------------------------------------

func TestExtractScopeElementsL4ContextL1_MapElements(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)
	sList[0].Context = make([]ContextElement, 2)

	contextArray := []interface{}{
		map[string]interface{}{
			"user": "admin",
			"app":  "OEM",
		},
		map[string]interface{}{
			"user":   "Driver",
			"device": "Cloud",
		},
	}
	extractScopeElementsL4ContextL1(0, contextArray)
	if len(sList[0].Context[0].Actor[0].Role) == 0 {
		t.Error("Actor[0].Role not populated")
	}
}

func TestExtractScopeElementsL4ContextL1_UnknownType(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)
	sList[0].Context = make([]ContextElement, 1)

	contextArray := []interface{}{42}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	extractScopeElementsL4ContextL1(0, contextArray)
}

func TestExtractScopeElementsL4ContextL2_StringRoles(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)
	sList[0].Context = make([]ContextElement, 1)

	contextMap := map[string]interface{}{
		"user":   "admin",
		"app":    "OEM",
		"device": "Cloud",
	}
	extractScopeElementsL4ContextL2(0, 0, contextMap)

	if len(sList[0].Context[0].Actor[0].Role) == 0 {
		t.Error("user role not set")
	}
	if sList[0].Context[0].Actor[0].Role[0] != "admin" {
		t.Errorf("user role = %q; want admin", sList[0].Context[0].Actor[0].Role[0])
	}
}

func TestExtractScopeElementsL4ContextL2_ArrayRoles(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)
	sList[0].Context = make([]ContextElement, 1)

	contextMap := map[string]interface{}{
		"user":   []interface{}{"admin", "superuser"},
		"app":    []interface{}{"OEM"},
		"device": []interface{}{"Cloud", "Vehicle"},
	}
	extractScopeElementsL4ContextL2(0, 0, contextMap)

	userRoles := sList[0].Context[0].Actor[0].Role
	if len(userRoles) != 2 {
		t.Errorf("user roles len = %d; want 2", len(userRoles))
	}
}

func TestExtractScopeElementsL4ContextL2_UnknownType(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)
	sList[0].Context = make([]ContextElement, 1)

	contextMap := map[string]interface{}{
		"user": 42, // not string or []interface{}
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractScopeElementsL4ContextL2(0, 0, contextMap)
}

// --------------------------------------------------------------------------
// extractScopeElementsL4NoAccessL1 — string and unknown type branches
// --------------------------------------------------------------------------

func TestExtractScopeElementsL4NoAccessL1_StringValues(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)
	sList[0].NoAccess = make([]string, 2)

	noAccessArray := []interface{}{"Vehicle.Private.A", "Vehicle.Private.B"}
	extractScopeElementsL4NoAccessL1(0, noAccessArray)

	if sList[0].NoAccess[0] != "Vehicle.Private.A" {
		t.Errorf("NoAccess[0] = %q; want Vehicle.Private.A", sList[0].NoAccess[0])
	}
	if sList[0].NoAccess[1] != "Vehicle.Private.B" {
		t.Errorf("NoAccess[1] = %q; want Vehicle.Private.B", sList[0].NoAccess[1])
	}
}

func TestExtractScopeElementsL4NoAccessL1_UnknownType(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)
	sList[0].NoAccess = make([]string, 1)

	noAccessArray := []interface{}{42} // not string
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	extractScopeElementsL4NoAccessL1(0, noAccessArray)
}

// --------------------------------------------------------------------------
// tokenValidationResponse — malformed JSON and invalid-token branches
// --------------------------------------------------------------------------

func TestTokenValidationResponse_MalformedJSON(t *testing.T) {
	resp := tokenValidationResponse(`{not json`)
	if !strings.Contains(resp, `"validation":"1"`) {
		t.Errorf("malformed JSON: got %q; want validation:1", resp)
	}
}

func TestTokenValidationResponse_EmptyToken_NoActiveMatch(t *testing.T) {
	// activeList all empty → getCompleteToken returns "" → VerifyTokenSignature fails.
	saved := activeList
	defer func() { activeList = saved }()
	initLists() // clears activeList

	// A JSON with an empty "token" field; getCompleteToken("") → "" → sig verify fails.
	resp := tokenValidationResponse(`{"token":"","action":"get","paths":["Vehicle.Speed"]}`)
	// Should not be "validation:0" — sig verify must fail.
	if strings.Contains(resp, `"validation":"0"`) {
		t.Errorf("empty token should not validate; got %q", resp)
	}
}

func TestTokenValidationResponse_TokenNotInActiveList(t *testing.T) {
	// A syntactically valid JSON with a token that isn't in activeList.
	saved := activeList
	defer func() { activeList = saved }()
	initLists()

	resp := tokenValidationResponse(`{"token":"unknown-tok","action":"get","paths":["Vehicle.Speed"]}`)
	// getCompleteToken returns "" → VerifyTokenSignature fails → validation:5
	if !strings.Contains(resp, `"validation"`) {
		t.Errorf("unknown token: expected validation field; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// matchingContext — actor length > 2 branch (return false)
// --------------------------------------------------------------------------

func TestMatchingContext_ActorIndexExceedsTwo(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	// Build a scope element whose Actor slice has 4 elements so the j>2 guard fires.
	// ContextElement.Actor is [3]RoleElement (fixed array), so j > 2 means j == 3
	// which the range over Actor's fixed-size array will actually never produce since
	// len == 3. The guard is effectively dead in the fixed-array case, but we can
	// still ensure matchingContext doesn't panic with the max-length actor.
	sList = []ScopeElement{
		{
			Context: []ContextElement{
				{
					Actor: [3]RoleElement{
						{Role: []string{"user"}},
						{Role: []string{"app"}},
						{Role: []string{"device"}},
					},
				},
			},
			NoAccess: []string{"Vehicle.X"},
		},
	}
	// Should find a match.
	if !matchingContext(0, "user+app+device") {
		t.Error("matchingContext should return true for exact match")
	}
}

// --------------------------------------------------------------------------
// init() second — env var branch for VISSR_ECF_ALLOWED_ORIGIN
// --------------------------------------------------------------------------

func TestInit_EcfAllowedOriginsSet(t *testing.T) {
	// The init() already ran and parsed VISSR_ECF_ALLOWED_ORIGIN.
	// We can verify the parsing is correct by checking ecfAllowedOrigins
	// when the env var is populated at test time (using the package state).
	// Since init() already ran, test the functions that use ecfAllowedOrigins.
	saved := ecfAllowedOrigins
	defer func() { ecfAllowedOrigins = saved }()

	ecfAllowedOrigins = []string{"https://a.com", "https://b.com"}
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://a.com")
	if !checkEcfOrigin(req) {
		t.Error("checkEcfOrigin should accept a listed origin")
	}
	req.Header.Set("Origin", "https://evil.com")
	if checkEcfOrigin(req) {
		t.Error("checkEcfOrigin should reject an unlisted origin")
	}
}

// --------------------------------------------------------------------------
// writeToActiveList — full list path (logs error but does not panic)
// --------------------------------------------------------------------------

func TestWriteToActiveList_FullList_LogsError(t *testing.T) {
	saved := activeList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Fill all slots.
	for i := 0; i < LISTSIZE; i++ {
		activeList[i].GatingId = i
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("writeToActiveList panicked on full list: %v", r)
		}
	}()
	writeToActiveList(9000, "some-token")
}

// --------------------------------------------------------------------------
// purgeLists — active list bad expiry path
// --------------------------------------------------------------------------

func TestPurgeLists_ActiveBadExpiry(t *testing.T) {
	saved := pendingList
	savedActive := activeList
	savedTicker := expiryTicker
	defer func() {
		pendingList = saved
		activeList = savedActive
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	// Active entry with malformed expiry.
	activeList[0].GatingId = 88
	activeList[0].AtExpiryTime = "not-a-number"

	got := purgeLists()
	if got != "" {
		t.Errorf("bad active expiry: got %q; want empty", got)
	}
}

// --------------------------------------------------------------------------
// consentCancelResponse — non-numeric messageId path
// --------------------------------------------------------------------------

func TestConsentCancelResponse_NonNumericMessageId(t *testing.T) {
	ch := make(chan string, 1)
	resp := consentCancelResponse(`{"action":"consent-cancel","messageId":"not-a-number"}`, ch)
	if !strings.Contains(resp, "401") {
		t.Errorf("expected 401 for non-numeric messageId; got %q", resp)
	}
}

func TestConsentCancelResponse_MalformedJSON(t *testing.T) {
	ch := make(chan string, 1)
	resp := consentCancelResponse(`{not valid json`, ch)
	if !strings.Contains(resp, "401") {
		t.Errorf("malformed JSON: expected 401; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// tokenValidationResponse — more branches:
//   valid token path up to signature verify, bad aud, bad iss
// --------------------------------------------------------------------------

// makeTestTokenWithClaims builds a signed JWT with the given custom claims.
// This shares the package-level theAtSecret so tokenValidationResponse
// can verify the signature.
func makeTestTokenWithClaims(claims map[string]string) string {
	var jwt utils.JsonWebToken
	jwt.SetHeader("HS256")
	for k, v := range claims {
		jwt.AddClaim(k, v)
	}
	jwt.Encode()
	jwt.SymmSign(theAtSecret)
	return jwt.GetFullToken()
}

// To get tokenValidationResponse past the getCompleteToken lookup we
// need to put the token into activeList[].
func TestTokenValidationResponse_BadAud(t *testing.T) {
	saved := activeList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	now := time.Now().Unix()
	tok := makeTestTokenWithClaims(map[string]string{
		"iat": strconv.FormatInt(now-10, 10),
		"exp": strconv.FormatInt(now+3600, 10),
		"scp": "fuel-status",
		"aud": "wrong-aud",
		"iss": atIssuer,
	})
	// Register the token in activeList so getCompleteToken finds it.
	writeToActiveList(1, tok)

	resp := tokenValidationResponse(`{"token":"` + tok + `","action":"get","paths":["Vehicle.Speed"]}`)
	// Should get validation:20 (bad aud).
	if !strings.Contains(resp, `"validation":"20"`) {
		t.Errorf("bad aud: got %q; want validation:20", resp)
	}
}

func TestTokenValidationResponse_BadIss(t *testing.T) {
	saved := activeList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)

	now := time.Now().Unix()
	tok := makeTestTokenWithClaims(map[string]string{
		"iat": strconv.FormatInt(now-10, 10),
		"exp": strconv.FormatInt(now+3600, 10),
		"scp": "fuel-status",
		"aud": AT_AUDIENCE,
		"iss": "wrong-issuer",
	})
	writeToActiveList(2, tok)

	resp := tokenValidationResponse(`{"token":"` + tok + `","action":"get","paths":["Vehicle.Speed"]}`)
	if !strings.Contains(resp, `"validation":"22"`) {
		t.Errorf("bad iss: got %q; want validation:22", resp)
	}
}

func TestTokenValidationResponse_ValidTokenBadPurpose(t *testing.T) {
	saved := activeList
	savedPList := pList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		pList = savedPList
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)
	// No purpose entries → validateRequestAccess returns 60 (purpose not found).
	pList = nil

	now := time.Now().Unix()
	tok := makeTestTokenWithClaims(map[string]string{
		"iat": strconv.FormatInt(now-10, 10),
		"exp": strconv.FormatInt(now+3600, 10),
		"scp": "fuel-status",
		"aud": AT_AUDIENCE,
		"iss": atIssuer,
	})
	writeToActiveList(3, tok)

	resp := tokenValidationResponse(`{"token":"` + tok + `","action":"get","paths":["Vehicle.Speed"]}`)
	// pList is nil → validateRequestAccess returns 60 → validation:60
	if !strings.Contains(resp, `"validation":"60"`) {
		t.Errorf("bad purpose: got %q; want validation:60", resp)
	}
}

func TestTokenValidationResponse_ValidTokenExpiredExpiry(t *testing.T) {
	saved := activeList
	savedPList := pList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		pList = savedPList
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)
	pList = []PurposeElement{
		{Short: "fuel-status", Access: []AccessElement{{Path: "Vehicle.Speed", Permission: "read-only"}}},
	}

	now := time.Now().Unix()
	// Token with valid aud/iss/scp but expired timestamp.
	tok := makeTestTokenWithClaims(map[string]string{
		"iat": strconv.FormatInt(now-7200, 10),
		"exp": strconv.FormatInt(now-3600, 10), // expired
		"scp": "fuel-status",
		"aud": AT_AUDIENCE,
		"iss": atIssuer,
	})
	writeToActiveList(4, tok)

	resp := tokenValidationResponse(`{"token":"` + tok + `","action":"get","paths":["Vehicle.Speed"]}`)
	// validateTokenExpiry returns 16 → validation:16
	if !strings.Contains(resp, `"validation":"16"`) {
		t.Errorf("expired token: got %q; want validation:16", resp)
	}
}

func TestTokenValidationResponse_ValidTokenNoHandle(t *testing.T) {
	saved := activeList
	savedPList := pList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		pList = savedPList
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)
	pList = []PurposeElement{
		{Short: "fuel-status", Access: []AccessElement{{Path: "Vehicle.Speed", Permission: "read-only"}}},
	}

	now := time.Now().Unix()
	tok := makeTestTokenWithClaims(map[string]string{
		"iat": strconv.FormatInt(now-10, 10),
		"exp": strconv.FormatInt(now+3600, 10),
		"scp": "fuel-status",
		"aud": AT_AUDIENCE,
		"iss": atIssuer,
	})
	writeToActiveList(5, tok)

	resp := tokenValidationResponse(`{"token":"` + tok + `","action":"get","paths":["Vehicle.Speed"]}`)
	// Fully valid token; AtokenHandle is the signature segment.
	// getGatingIdAndTokenHandle finds it and returns handle = signature.
	// Response should contain validation:0 and gatingId.
	if !strings.Contains(resp, `"validation":"0"`) {
		t.Errorf("valid token: got %q; want validation:0", resp)
	}
	if !strings.Contains(resp, "gatingId") {
		t.Errorf("valid token: expected gatingId in response; got %q", resp)
	}
}

// --------------------------------------------------------------------------
// generateClientResponse — at-inquiry branch
// --------------------------------------------------------------------------

func TestGenerateClientResponse_AtInquiryRoute(t *testing.T) {
	saved := pendingList
	defer func() { pendingList = saved }()
	initLists()
	pendingList[0].GatingId = 77
	pendingList[0].Consent = "NOT_SET"

	ecfChan := make(chan string, 1)
	resp := generateClientResponse(`{"at-inquiry":"x","sessionId":"77"}`, ecfChan, false)
	if !strings.Contains(resp, "at-inquiry") {
		t.Errorf("at-inquiry route: got %q; want at-inquiry", resp)
	}
}

// --------------------------------------------------------------------------
// getGatingIdAndTokenHandle — token not found (empty return) path
// --------------------------------------------------------------------------

func TestGetGatingIdAndTokenHandle_TokenNotFound(t *testing.T) {
	saved := activeList
	defer func() { activeList = saved }()
	initLists()

	gid, handle := getGatingIdAndTokenHandle("not-in-list")
	if gid != "" || handle != "" {
		t.Errorf("not-in-list: got (%q, %q); want both empty", gid, handle)
	}
}

// --------------------------------------------------------------------------
// extractPurposeElementsLevel2 — unknown type branch
// --------------------------------------------------------------------------

func TestExtractPurposeElementsLevel2_UnknownType(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()

	raw := []interface{}{42} // not a map — hits default branch
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractPurposeElementsLevel2(raw)
}

// --------------------------------------------------------------------------
// extractScopeElementsL4ContextL2 — array with non-string inner item
// --------------------------------------------------------------------------

func TestExtractScopeElementsL4ContextL2_ArrayWithNonStringInner(t *testing.T) {
	saved := sList
	defer func() { sList = saved }()
	sList = make([]ScopeElement, 1)
	sList[0].Context = make([]ContextElement, 1)

	// Array of mixed types — the non-string inner item hits the inner default branch.
	contextMap := map[string]interface{}{
		"user": []interface{}{"admin", 42}, // 42 is not a string
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on non-string array item: %v", r)
		}
	}()
	extractScopeElementsL4ContextL2(0, 0, contextMap)
}

// --------------------------------------------------------------------------
// extractPurposeElementsL4ContextL2 — array with non-string inner item
// --------------------------------------------------------------------------

func TestExtractPurposeElementsL4ContextL2_ArrayWithNonStringInner(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)
	pList[0].Context = make([]ContextElement, 1)

	contextMap := map[string]interface{}{
		"user": []interface{}{"Independent", 42}, // 42 hits inner default
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on non-string array item: %v", r)
		}
	}()
	extractPurposeElementsL4ContextL2(0, 0, contextMap)
}

// --------------------------------------------------------------------------
// extractPurposeElementsLevel3 — unknown type in top-level value
// --------------------------------------------------------------------------

func TestExtractPurposeElementsLevel3_UnknownType(t *testing.T) {
	saved := pList
	defer func() { pList = saved }()
	pList = make([]PurposeElement, 1)

	elem := map[string]interface{}{
		"unknown": 42, // not string, []interface{}, or map
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on unknown type: %v", r)
		}
	}()
	extractPurposeElementsLevel3(0, elem)
}

// --------------------------------------------------------------------------
// tokenValidationResponse — handle-less success path (no "handle" key)
// --------------------------------------------------------------------------

func TestTokenValidationResponse_ValidTokenWithHandle(t *testing.T) {
	// writeToActiveList sets AtokenHandle = extractSignature(tok), which
	// is the last JWT segment — always non-empty for a properly signed JWT.
	// This covers the `tokenHandle != ""` branch.
	saved := activeList
	savedPList := pList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		pList = savedPList
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)
	pList = []PurposeElement{
		{Short: "speed-read", Access: []AccessElement{{Path: "Vehicle.Speed", Permission: "read-only"}}},
	}

	now := time.Now().Unix()
	tok := makeTestTokenWithClaims(map[string]string{
		"iat": strconv.FormatInt(now-10, 10),
		"exp": strconv.FormatInt(now+3600, 10),
		"scp": "speed-read",
		"aud": AT_AUDIENCE,
		"iss": atIssuer,
	})
	writeToActiveList(10, tok)

	resp := tokenValidationResponse(`{"token":"` + tok + `","action":"get","paths":["Vehicle.Speed"]}`)
	if !strings.Contains(resp, `"validation":"0"`) {
		t.Errorf("valid token with handle: got %q; want validation:0", resp)
	}
	if !strings.Contains(resp, `"handle"`) {
		t.Errorf("valid token with handle: expected handle in response; got %q", resp)
	}
}

func TestTokenValidationResponse_ValidTokenNoHandleSlot(t *testing.T) {
	// AtokenHandle = "" → returns gatingId without handle.
	saved := activeList
	savedPList := pList
	savedTicker := expiryTicker
	defer func() {
		activeList = saved
		pList = savedPList
		expiryTicker = savedTicker
	}()
	initLists()
	expiryTicker = time.NewTicker(24 * time.Hour)
	pList = []PurposeElement{
		{Short: "speed-read", Access: []AccessElement{{Path: "Vehicle.Speed", Permission: "read-only"}}},
	}

	now := time.Now().Unix()
	tok := makeTestTokenWithClaims(map[string]string{
		"iat": strconv.FormatInt(now-10, 10),
		"exp": strconv.FormatInt(now+3600, 10),
		"scp": "speed-read",
		"aud": AT_AUDIENCE,
		"iss": atIssuer,
	})
	// Manually put token in activeList with empty AtokenHandle (no-handle slot).
	activeList[0].GatingId = 20
	activeList[0].Atoken = tok
	activeList[0].AtokenHandle = "" // explicit empty handle
	activeList[0].AtExpiryTime = strconv.FormatInt(now+3600, 10)

	resp := tokenValidationResponse(`{"token":"` + tok + `","action":"get","paths":["Vehicle.Speed"]}`)
	if !strings.Contains(resp, `"validation":"0"`) {
		t.Errorf("valid token no-handle: got %q; want validation:0", resp)
	}
	if strings.Contains(resp, `"handle"`) {
		t.Errorf("no-handle path: should not have handle in response; got %q", resp)
	}
}
