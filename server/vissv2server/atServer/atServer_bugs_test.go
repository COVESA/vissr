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
	"os"
	"strings"
	"testing"

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
